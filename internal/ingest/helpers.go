package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andrelbuzzo/itm/internal/model"
)

// timeLayouts are the timestamp formats accepted, in priority order. RFC 3339
// comes first; the others cover the variants real exporters emit. Parsing is
// explicit rather than clever so an ambiguous date never silently means
// something different on a machine with another locale.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999", // no zone: treated as UTC below
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTS parses a timestamp to UTC. A value without a zone is taken as UTC —
// the same choice everywhere, so two runs cannot disagree.
func parseTS(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	// Unix epoch in seconds or milliseconds, as some exporters emit.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(n).UTC(), nil
		}
		return time.Unix(n, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", s)
}

// logID derives an id from the record's content.
//
// Position is deliberately excluded: shuffling the file must not change any id,
// or shuffled input could not produce identical output. Required fields plus the
// message are enough to make collisions vanishingly unlikely without hashing the
// whole record (which would make a trivial field reordering change the id).
func logID(r rawLog) string {
	return contentID(
		valueOr(r.TS),
		valueOr(r.Service),
		valueOr(r.Level),
		valueOr(r.Msg),
		r.TraceID,
	)
}

func spanID(r rawSpan) string {
	id := contentID(
		valueOr(r.TraceID),
		valueOr(r.SpanID),
		r.ParentSpanID,
		valueOr(r.Service),
		valueOr(r.Name),
		valueOr(r.Start),
	)
	// Two spans can share every field (a retried identical call). Disambiguate
	// with the end time, still content-derived.
	return contentID(id, valueOr(r.End))
}

// contentID joins parts with a separator that cannot appear in the parts and
// hashes the result. The sha256 prefix keeps ids short and stable.
func contentID(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0x1f}) // unit separator
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func valueOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// bounds returns the earliest and latest timestamp in rs.
func bounds[T any](rs []T, at func(T) time.Time) (first, last time.Time) {
	if len(rs) == 0 {
		return time.Time{}, time.Time{}
	}
	first, last = at(rs[0]), at(rs[0])
	for _, r := range rs[1:] {
		t := at(r)
		if t.Before(first) {
			first = t
		}
		if t.After(last) {
			last = t
		}
	}
	return first, last
}

// timesOf projects a slice to its timestamps.
func timesOf[T any](rs []T, at func(T) time.Time) []time.Time {
	out := make([]time.Time, len(rs))
	for i, r := range rs {
		out[i] = at(r)
	}
	return out
}

// referenceTime is the robust centre of the file's timeline.
//
// Min/max would be wrong: a single wildly-skewed record drags the bounds to
// itself and then looks perfectly normal. The median is unmoved by a minority
// of outliers, which is exactly the property needed to detect them.
func referenceTime(times []time.Time) time.Time {
	if len(times) == 0 {
		return time.Time{}
	}
	c := make([]time.Time, len(times))
	copy(c, times)
	sort.Slice(c, func(i, j int) bool { return c[i].Before(c[j]) })
	return c[len(c)/2]
}

// isSkewed reports whether t sits more than clockSkewBound away from the
// file's median timestamp.
func isSkewed(t, reference time.Time) bool {
	if reference.IsZero() {
		return false
	}
	d := t.Sub(reference)
	if d < 0 {
		d = -d
	}
	return d > clockSkewBound
}

// sortLogs orders records so that a shuffled input produces the same slice.
// Timestamp first, then service, then id: the id is content-derived, so the
// order is total and independent of the file.
func sortLogs(lines []model.LogLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if !a.TS.Equal(b.TS) {
			return a.TS.Before(b.TS)
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.ID < b.ID
	})
}

// sortSpans orders spans by start, then trace, then span id.
func sortSpans(spans []model.Span) {
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i], spans[j]
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if a.TraceID != b.TraceID {
			return a.TraceID < b.TraceID
		}
		return a.SpanID < b.SpanID
	})
}
