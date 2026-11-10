package cli

import (
	"encoding/json"
	"os"
	"time"

	"github.com/andrelbuzzo/itm/internal/model"
)

// logJSON is the on-disk shape of a log line, mirroring the documented schema.
type logJSON struct {
	TS      string         `json:"ts"`
	Service string         `json:"service"`
	Level   string         `json:"level"`
	Msg     string         `json:"msg"`
	TraceID string         `json:"trace_id,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// spanJSON is the on-disk shape of a span.
type spanJSON struct {
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id"`
	Service      string         `json:"service"`
	Name         string         `json:"name"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	Status       string         `json:"status"`
	Attrs        map[string]any `json:"attrs,omitempty"`
}

// writeJSONL writes one JSON object per line, in the given order.
//
// The order is the caller's, which is already sorted by the generator, so the
// file bytes are a pure function of the scenario seed.
func writeJSONL(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// logLines converts logs to JSONL strings.
func logLines(logs []model.LogLine) []string {
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		blob, err := json.Marshal(logJSON{
			TS:      l.TS.UTC().Format(time.RFC3339Nano),
			Service: l.Service,
			Level:   string(l.Level),
			Msg:     l.Msg,
			TraceID: l.TraceID,
			Fields:  l.Fields,
		})
		if err != nil {
			continue
		}
		out = append(out, string(blob))
	}
	return out
}

// spanLines converts spans to JSONL strings.
func spanLines(spans []model.Span) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		blob, err := json.Marshal(spanJSON{
			TraceID:      s.TraceID,
			SpanID:       s.SpanID,
			ParentSpanID: s.ParentSpanID,
			Service:      s.Service,
			Name:         s.Name,
			Start:        s.Start.UTC().Format(time.RFC3339Nano),
			End:          s.End.UTC().Format(time.RFC3339Nano),
			Status:       string(s.Status),
			Attrs:        s.Attrs,
		})
		if err != nil {
			continue
		}
		out = append(out, string(blob))
	}
	return out
}
