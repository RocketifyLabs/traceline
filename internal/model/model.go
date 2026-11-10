// Package model holds the data structures that flow through the analysis.
//
// Everything here is a plain value: no pointers to shared state, no methods that
// mutate, no package-level caches. Passing a value around is always safe and a
// second run over the same input produces the same structures.
package model

import (
	"time"
)

// Level is a log severity. An unknown level is preserved as-is rather than
// guessed at, because dropping it would silently change error rates.
type Level string

// Log severities the analysis treats specially.
const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
	LevelInfo  Level = "info"
	LevelDebug Level = "debug"
)

// IsError reports whether the level counts toward an error rate. Fatal and
// critical are treated as errors: a service that crashes is not healthy.
func (l Level) IsError() bool {
	switch normalize(string(l)) {
	case "error", "err", "fatal", "critical", "crit", "panic", "severe":
		return true
	}
	return false
}

// LogLine is one log record from the input JSONL.
type LogLine struct {
	// ID is derived from the record's content, never from its position in the
	// file. Shuffling the input preserves it, which is what makes shuffled
	// input produce byte-identical output.
	ID      string
	TS      time.Time
	Service string
	Level   Level
	Msg     string
	TraceID string         // optional
	Fields  map[string]any // optional; "event" carries deploy/config_change
}

// Field returns a field as a string, or "" when absent or not a scalar.
// Non-scalar fields are ignored rather than formatted, so a nested object can
// never leak into an output as Go's default map printing.
func (l LogLine) Field(key string) string {
	return fieldString(l.Fields, key)
}

// Event returns the lifecycle-event kind of this line (deploy, config_change),
// or "" when it is an ordinary log line.
func (l LogLine) Event() string {
	return l.Field("event")
}

// Status is the outcome of a span.
type Status string

// Span statuses.
const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// Span is one span record from the input JSONL.
type Span struct {
	ID           string
	TraceID      string
	SpanID       string
	ParentSpanID string // empty for a root span
	Service      string
	Name         string
	Start        time.Time
	End          time.Time
	Status       Status
	Attrs        map[string]any
}

// Duration is the span's wall-clock duration. A span whose end precedes its
// start yields a negative duration; callers must decide whether to exclude it
// rather than have this silently return zero.
func (s Span) Duration() time.Duration {
	return s.End.Sub(s.Start)
}

// IsRoot reports whether the span has no parent and therefore starts no edge.
func (s Span) IsRoot() bool {
	return s.ParentSpanID == ""
}

// IsError reports whether the span failed.
func (s Span) IsError() bool {
	return s.Status == StatusError
}

// Attr returns an attribute as a string, or "" when absent or not a scalar.
func (s Span) Attr(key string) string {
	return fieldString(s.Attrs, key)
}

// Edge is a caller→callee dependency derived from parent/child spans.
type Edge struct {
	Caller string
	Callee string
	Calls  int
	Errors int
	P50    time.Duration
	P95    time.Duration
}

// ErrorRate is errors over calls, in [0, 1]. An edge with no calls is 0.
func (e Edge) ErrorRate() float64 {
	if e.Calls == 0 {
		return 0
	}
	return float64(e.Errors) / float64(e.Calls)
}

// Anomaly is one detected behavioural change on one service.
type Anomaly struct {
	Service  string
	Kind     string // error_rate, latency_p95, template_burst, new_error_template
	Onset    time.Time
	Strength float64 // robust z-score
	LogIDs   []string
	SpanIDs  []string
	Template string // for template-based kinds
}

// TimelineEvent is one row of the reconstructed timeline.
type TimelineEvent struct {
	At          time.Time
	Kind        string // first_anomaly, deploy, config_change, cascade, skipped_data
	Service     string
	Detail      string
	EvidenceIDs []string
}

// Candidate is one scored root-cause hypothesis.
type Candidate struct {
	Service     string
	Score       float64
	Confidence  string // high, medium, low
	Breakdown   ScoreBreakdown
	EvidenceIDs []string
}

// ScoreBreakdown exposes every factor of a candidate's score, so a ranking can
// be audited instead of trusted.
type ScoreBreakdown struct {
	Onset              float64
	Strength           float64
	ExplainsDownstream float64
	NotExplained       float64
}

// DataQuality records what the analysis had to skip or accept unchanged.
type DataQuality struct {
	LogLinesRead   int
	SpanLinesRead  int
	LogsAccepted   int
	SpansAccepted  int
	Skipped        int
	SkipReasons    map[string]int
	ClockSkewLines int
	OrphanSpans    int
	RootSpans      int
}

// Report is the complete analysis result, the single input to every writer.
type Report struct {
	IncidentStart time.Time
	IncidentEnd   time.Time
	BaselineStart time.Time
	BaselineEnd   time.Time
	Timeline      []TimelineEvent
	Edges         []Edge
	Anomalies     []Anomaly
	Candidates    []Candidate
	Insufficient  bool
	Quality       DataQuality
}
