// Package ingest reads JSONL logs and spans without ever crashing on bad input.
//
// Two rules govern everything here:
//
//  1. Bad data is counted, never fatal. A malformed line is skipped and its
//     reason tallied, so the report can state exactly what was ignored.
//  2. IDs come from content, not from position. Shuffling the input file must
//     produce byte-identical output, which is only possible if a record's
//     identity does not depend on where it sat in the file.
package ingest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// Skip reasons, used as keys in the data-quality tally. They are a closed set
// so the report can group them without guessing.
const (
	ReasonEmpty        = "empty_line"
	ReasonInvalidJSON  = "invalid_json"
	ReasonMissingField = "missing_required_field"
	ReasonBadTimestamp = "unparseable_timestamp"
	ReasonBadSpanRange = "end_before_start"
)

// maxLineBytes bounds a single line. A line longer than this is skipped as
// invalid rather than consuming unbounded memory.
const maxLineBytes = 1 << 20 // 1 MiB

// Result carries the accepted records plus everything skipped along the way.
type Result[T any] struct {
	Records []T
	Quality model.DataQuality
	// FirstSeen and LastSeen bound the timestamps actually observed, before any
	// clock-skew accounting. They are the reference for skew detection.
	FirstSeen time.Time
	LastSeen  time.Time
}

// clockSkewBound is how far outside the file's observed range a record may sit
// before it is flagged. Flagged records are still accepted: they are evidence
// of a real problem, not noise to hide.
const clockSkewBound = 10 * time.Minute

// Logs reads a JSONL log file from path.
func Logs(path string) (Result[model.LogLine], error) {
	f, err := os.Open(path)
	if err != nil {
		return Result[model.LogLine]{}, fmt.Errorf("open logs: %w", err)
	}
	defer f.Close()
	return ReadLogs(f)
}

// Spans reads a JSONL span file from path.
func Spans(path string) (Result[model.Span], error) {
	f, err := os.Open(path)
	if err != nil {
		return Result[model.Span]{}, fmt.Errorf("open spans: %w", err)
	}
	defer f.Close()
	return ReadSpans(f)
}

// rawLog mirrors the documented log schema. Pointers distinguish "absent" from
// "present but empty", which matters for required-field reporting.
type rawLog struct {
	TS      *string        `json:"ts"`
	Service *string        `json:"service"`
	Level   *string        `json:"level"`
	Msg     *string        `json:"msg"`
	TraceID string         `json:"trace_id"`
	Fields  map[string]any `json:"fields"`
	time    time.Time      // parsed ts, filled after validation
}

// ReadLogs parses JSONL logs from r.
func ReadLogs(r io.Reader) (Result[model.LogLine], error) {
	res := Result[model.LogLine]{Quality: model.DataQuality{SkipReasons: map[string]int{}}}

	raws, err := scanJSONL[rawLog](r, &res.Quality, "logs")
	if err != nil {
		return res, err
	}

	// Accept records, validating required fields and resolving timestamps.
	accepted := make([]rawLog, 0, len(raws))
	for _, raw := range raws {
		if raw.TS == nil || raw.Service == nil || raw.Level == nil || raw.Msg == nil {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonMissingField]++
			continue
		}
		ts, err := parseTS(*raw.TS)
		if err != nil {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonBadTimestamp]++
			continue
		}
		raw.time = ts
		accepted = append(accepted, raw)
	}

	res.Quality.LogsAccepted = len(accepted)
	res.FirstSeen, res.LastSeen = bounds(accepted, func(r rawLog) time.Time { return r.time })

	// The median timestamp, not the min/max, is the reference for skew: one wild
	// record cannot drag a median to itself and look normal.
	ref := referenceTime(timesOf(accepted, func(r rawLog) time.Time { return r.time }))

	// Compute ids and count clock skew.
	res.Records = make([]model.LogLine, 0, len(accepted))
	for _, raw := range accepted {
		line := model.LogLine{
			TS:      raw.time,
			Service: *raw.Service,
			Level:   model.Level(*raw.Level),
			Msg:     *raw.Msg,
			TraceID: raw.TraceID,
			Fields:  raw.Fields,
			ID:      logID(raw),
		}
		if isSkewed(raw.time, ref) {
			res.Quality.ClockSkewLines++
		}
		res.Records = append(res.Records, line)
	}

	sortLogs(res.Records)
	return res, nil
}

// rawSpan mirrors the documented span schema.
type rawSpan struct {
	TraceID      *string        `json:"trace_id"`
	SpanID       *string        `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id"`
	Service      *string        `json:"service"`
	Name         *string        `json:"name"`
	Start        *string        `json:"start"`
	End          *string        `json:"end"`
	Status       *string        `json:"status"`
	Attrs        map[string]any `json:"attrs"`
	start        time.Time
	end          time.Time
}

// ReadSpans parses JSONL spans from r.
func ReadSpans(r io.Reader) (Result[model.Span], error) {
	res := Result[model.Span]{Quality: model.DataQuality{SkipReasons: map[string]int{}}}

	raws, err := scanJSONL[rawSpan](r, &res.Quality, "spans")
	if err != nil {
		return res, err
	}

	accepted := make([]rawSpan, 0, len(raws))
	for _, raw := range raws {
		if raw.TraceID == nil || raw.SpanID == nil || raw.Service == nil ||
			raw.Name == nil || raw.Start == nil || raw.End == nil || raw.Status == nil {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonMissingField]++
			continue
		}
		start, err := parseTS(*raw.Start)
		if err != nil {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonBadTimestamp]++
			continue
		}
		end, err := parseTS(*raw.End)
		if err != nil {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonBadTimestamp]++
			continue
		}
		// A span that ends before it starts is a clock problem, not a duration.
		// Zero-length spans are legitimate and accepted.
		if end.Before(start) {
			res.Quality.Skipped++
			res.Quality.SkipReasons[ReasonBadSpanRange]++
			continue
		}
		raw.start, raw.end = start, end
		accepted = append(accepted, raw)
	}

	res.Quality.SpansAccepted = len(accepted)
	res.FirstSeen, res.LastSeen = bounds(accepted, func(r rawSpan) time.Time { return r.start })

	res.Records = make([]model.Span, 0, len(accepted))
	for _, raw := range accepted {
		span := model.Span{
			TraceID:      *raw.TraceID,
			SpanID:       *raw.SpanID,
			ParentSpanID: raw.ParentSpanID,
			Service:      *raw.Service,
			Name:         *raw.Name,
			Start:        raw.start,
			End:          raw.end,
			Status:       model.Status(strings.ToLower(strings.TrimSpace(*raw.Status))),
			Attrs:        raw.Attrs,
			ID:           spanID(raw),
		}
		if span.IsRoot() {
			res.Quality.RootSpans++
		}
		res.Records = append(res.Records, span)
	}

	// Orphans: a parent id that never appears in the file. Counted, not guessed.
	seen := make(map[string]struct{}, len(res.Records))
	for _, s := range res.Records {
		seen[s.SpanID] = struct{}{}
	}
	// Iterate a deterministic slice, never the map: the count is the same either
	// way but the habit is what keeps this package honest.
	for _, s := range res.Records {
		if !s.IsRoot() {
			if _, ok := seen[s.ParentSpanID]; !ok {
				res.Quality.OrphanSpans++
			}
		}
	}

	sortSpans(res.Records)
	return res, nil
}

// scanJSONL reads lines and unmarshals each into T, tallying every rejection.
// Required fields are validated by the caller through the pointers, so this
// function only enforces the rules that apply to both logs and spans.
func scanJSONL[T any](r io.Reader, q *model.DataQuality, kind string) ([]T, error) {
	var out []T
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if kind == "logs" {
			q.LogLinesRead++
		} else {
			q.SpanLinesRead++
		}
		if line == "" {
			q.Skipped++
			q.SkipReasons[ReasonEmpty]++
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			q.Skipped++
			q.SkipReasons[ReasonInvalidJSON]++
			continue
		}
		out = append(out, v)
	}
	// A read error (not a parse error) is real and must surface: a truncated
	// file or an unreadable stream cannot be analyzed honestly.
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read %s: %w", kind, err)
	}
	return out, nil
}
