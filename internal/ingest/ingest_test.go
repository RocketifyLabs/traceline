package ingest

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

const validLogs = `{"ts":"2026-10-16T10:00:01Z","service":"api-edge","level":"error","msg":"upstream timeout","trace_id":"t1"}
{"ts":"2026-10-16T10:00:02Z","service":"db-proxy","level":"warn","msg":"pool nearly exhausted","trace_id":"t2"}
{"ts":"2026-10-16T10:00:03Z","service":"api-edge","level":"info","msg":"request ok"}
`

func TestReadLogsAcceptsValidRecords(t *testing.T) {
	res, err := ReadLogs(strings.NewReader(validLogs))
	if err != nil {
		t.Fatalf("ReadLogs: %v", err)
	}
	if len(res.Records) != 3 {
		t.Fatalf("got %d records, want 3", len(res.Records))
	}
	if res.Quality.LogsAccepted != 3 || res.Quality.Skipped != 0 {
		t.Fatalf("quality = %+v, want 3 accepted 0 skipped", res.Quality)
	}
	first := res.Records[0]
	if first.Service != "api-edge" || first.Level != model.LevelError || first.Msg != "upstream timeout" {
		t.Fatalf("first record = %+v", first)
	}
	if !first.Level.IsError() {
		t.Fatal("error level must classify as error")
	}
}

// FR-1: a fixture with ~5% malformed lines analyzes successfully, the skipped
// count is reported, and nothing crashes.
func TestReadLogsToleratesMalformedLines(t *testing.T) {
	var b strings.Builder
	b.WriteString(validLogs)
	// 3 bad lines out of ~20 = 15%, harsher than the 5% requirement.
	b.WriteString("this is not json\n")
	b.WriteString(`{"ts":"2026-10-16T10:00:04Z","service":"x","level":"info"}` + "\n") // missing msg
	b.WriteString(`{"ts":"not-a-timestamp","service":"x","level":"info","msg":"m"}` + "\n")
	b.WriteString("\n")
	for i := 0; i < 14; i++ {
		b.WriteString(`{"ts":"2026-10-16T10:00:05Z","service":"ok","level":"info","msg":"fine"}` + "\n")
	}
	res, err := ReadLogs(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("ReadLogs must not fail on malformed input: %v", err)
	}
	if res.Quality.LogsAccepted != 17 {
		t.Errorf("accepted = %d, want 17", res.Quality.LogsAccepted)
	}
	if res.Quality.Skipped != 4 {
		t.Errorf("skipped = %d, want 4", res.Quality.Skipped)
	}
	for _, reason := range []string{ReasonInvalidJSON, ReasonMissingField, ReasonBadTimestamp, ReasonEmpty} {
		if res.Quality.SkipReasons[reason] == 0 {
			t.Errorf("skip reason %q not recorded: %+v", reason, res.Quality.SkipReasons)
		}
	}
}

func TestReadLogsNeverPanicsOnGarbage(t *testing.T) {
	inputs := []string{
		"",
		"\n\n\n",
		"{",
		`[]`,
		`null`,
		`"just a string"`,
		`{"ts":null,"service":null,"level":null,"msg":null}`,
		strings.Repeat("x", 5000),
		`{"ts":"2026-10-16T10:00:00Z","service":"s","level":"info","msg":"ok","fields":{"nested":{"a":1}}}`,
	}
	for _, in := range inputs {
		if _, err := ReadLogs(strings.NewReader(in)); err != nil {
			t.Errorf("input %.40q returned error: %v", in, err)
		}
	}
}

// FR-1 acceptance: shuffling the input line order produces byte-identical
// output. Ids are content-derived, so this must hold exactly.
func TestReadLogsShuffledOrderGivesIdenticalOutput(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, `{"ts":"2026-10-16T10:00:`+
			pad(i%60)+`Z","service":"svc`+pad(i%7)+`","level":"info","msg":"msg `+pad(i)+`"}`)
	}

	baseline, err := ReadLogs(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	baselineIDs := ids(baseline.Records)

	r := rand.New(rand.NewSource(99))
	for attempt := 0; attempt < 5; attempt++ {
		shuffled := append([]string(nil), lines...)
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got, err := ReadLogs(strings.NewReader(strings.Join(shuffled, "\n")))
		if err != nil {
			t.Fatalf("shuffled %d: %v", attempt, err)
		}
		if !equalStrings(ids(got.Records), baselineIDs) {
			t.Fatalf("attempt %d: shuffled input produced different ids\n got: %v\nwant: %v",
				attempt, ids(got.Records), baselineIDs)
		}
		if got.Quality.LogsAccepted != baseline.Quality.LogsAccepted {
			t.Fatalf("attempt %d: accepted %d vs %d", attempt, got.Quality.LogsAccepted, baseline.Quality.LogsAccepted)
		}
	}
}

func TestReadLogsIDsAreUniquePerContent(t *testing.T) {
	res, err := ReadLogs(strings.NewReader(validLogs))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, rec := range res.Records {
		if seen[rec.ID] {
			t.Fatalf("duplicate id %q", rec.ID)
		}
		seen[rec.ID] = true
	}
}

func TestReadLogsSortsByTimestampThenServiceThenID(t *testing.T) {
	in := `{"ts":"2026-10-16T10:00:03Z","service":"b","level":"info","msg":"3"}
{"ts":"2026-10-16T10:00:01Z","service":"z","level":"info","msg":"1"}
{"ts":"2026-10-16T10:00:01Z","service":"a","level":"info","msg":"1"}
`
	res, err := ReadLogs(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if res.Records[0].Service != "a" || res.Records[1].Service != "z" || res.Records[2].Service != "b" {
		t.Fatalf("order = %s %s %s, want a z b",
			res.Records[0].Service, res.Records[1].Service, res.Records[2].Service)
	}
}

// Clock skew is reported, never repaired: the record is kept and counted.
func TestReadLogsFlagsClockSkewWithoutDropping(t *testing.T) {
	in := `{"ts":"2026-10-16T10:00:00Z","service":"a","level":"info","msg":"m"}
{"ts":"2026-10-16T10:00:01Z","service":"a","level":"info","msg":"m"}
{"ts":"2026-10-16T09:30:00Z","service":"a","level":"info","msg":"skewed"}
`
	res, err := ReadLogs(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 3 {
		t.Fatalf("skewed record was dropped: got %d records, want 3", len(res.Records))
	}
	if res.Quality.ClockSkewLines != 1 {
		t.Fatalf("ClockSkewLines = %d, want 1", res.Quality.ClockSkewLines)
	}
}

func TestLogLineFieldAccess(t *testing.T) {
	in := `{"ts":"2026-10-16T10:00:00Z","service":"a","level":"info","msg":"m","fields":{"event":"deploy","count":3,"flag":true,"nested":{"x":1},"nothing":null}}`
	res, err := ReadLogs(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	l := res.Records[0]
	if got := l.Event(); got != "deploy" {
		t.Errorf("Event() = %q, want deploy", got)
	}
	if got := l.Field("count"); got != "3" {
		t.Errorf("Field(count) = %q, want 3", got)
	}
	if got := l.Field("flag"); got != "true" {
		t.Errorf("Field(flag) = %q, want true", got)
	}
	// A nested object must not leak Go's map formatting into an output.
	if got := l.Field("nested"); got != "" {
		t.Errorf("Field(nested) = %q, want empty", got)
	}
	if got := l.Field("missing"); got != "" {
		t.Errorf("Field(missing) = %q, want empty", got)
	}
}

// --- spans ---

const validSpans = `{"trace_id":"t1","span_id":"root","parent_span_id":null,"service":"edge","name":"GET","start":"2026-10-16T10:00:00Z","end":"2026-10-16T10:00:02Z","status":"error"}
{"trace_id":"t1","span_id":"child","parent_span_id":"root","service":"db","name":"query","start":"2026-10-16T10:00:01Z","end":"2026-10-16T10:00:02Z","status":"ok"}
`

func TestReadSpansAcceptsValidRecords(t *testing.T) {
	res, err := ReadSpans(strings.NewReader(validSpans))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 2 {
		t.Fatalf("got %d spans, want 2", len(res.Records))
	}
	if !res.Records[0].IsRoot() {
		t.Error("first span should be a root")
	}
	if res.Records[1].IsRoot() {
		t.Error("second span should not be a root")
	}
	if res.Records[0].Duration() != 2*time.Second {
		t.Errorf("duration = %v, want 2s", res.Records[0].Duration())
	}
	if !res.Records[0].IsError() || res.Records[1].IsError() {
		t.Error("error status misread")
	}
}

// FR-2: a span with no parent produces no edge; an orphan parent id is counted,
// not invented.
func TestReadSpansCountsRootsAndOrphans(t *testing.T) {
	in := validSpans +
		`{"trace_id":"t2","span_id":"lonely","parent_span_id":"does-not-exist","service":"x","name":"n","start":"2026-10-16T10:00:00Z","end":"2026-10-16T10:00:01Z","status":"ok"}` + "\n"
	res, err := ReadSpans(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if res.Quality.RootSpans != 1 {
		t.Errorf("RootSpans = %d, want 1", res.Quality.RootSpans)
	}
	if res.Quality.OrphanSpans != 1 {
		t.Errorf("OrphanSpans = %d, want 1", res.Quality.OrphanSpans)
	}
	if len(res.Records) != 3 {
		t.Errorf("orphan must be kept, got %d spans", len(res.Records))
	}
}

func TestReadSpansRejectsEndBeforeStart(t *testing.T) {
	in := `{"trace_id":"t","span_id":"s","service":"x","name":"n","start":"2026-10-16T10:00:05Z","end":"2026-10-16T10:00:00Z","status":"ok"}`
	res, err := ReadSpans(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 0 {
		t.Fatalf("inverted span accepted: %+v", res.Records)
	}
	if res.Quality.SkipReasons[ReasonBadSpanRange] != 1 {
		t.Errorf("reason tally = %+v, want one end_before_start", res.Quality.SkipReasons)
	}
}

func TestReadSpansAcceptsZeroLengthSpan(t *testing.T) {
	in := `{"trace_id":"t","span_id":"s","service":"x","name":"n","start":"2026-10-16T10:00:00Z","end":"2026-10-16T10:00:00Z","status":"ok"}`
	res, err := ReadSpans(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 1 {
		t.Fatalf("zero-length span rejected: %+v", res.Quality.SkipReasons)
	}
}

func TestReadSpansToleratesMalformed(t *testing.T) {
	in := validSpans + "not json\n" + `{"trace_id":"t"}` + "\n"
	res, err := ReadSpans(strings.NewReader(in))
	if err != nil {
		t.Fatalf("must not fail: %v", err)
	}
	if res.Quality.SpansAccepted != 2 {
		t.Errorf("accepted = %d, want 2", res.Quality.SpansAccepted)
	}
	if res.Quality.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", res.Quality.Skipped)
	}
}

func TestReadSpansShuffledOrderGivesIdenticalOutput(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(validSpans), "\n")
	for i := 0; i < 20; i++ {
		lines = append(lines, `{"trace_id":"t`+pad(i)+`","span_id":"s`+pad(i)+`","service":"svc","name":"n","start":"2026-10-16T10:00:`+
			pad(i%60)+`Z","end":"2026-10-16T10:01:`+pad(i%60)+`Z","status":"ok"}`)
	}
	baseline, err := ReadSpans(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	want := spanIDs(baseline.Records)

	r := rand.New(rand.NewSource(7))
	for attempt := 0; attempt < 5; attempt++ {
		sh := append([]string(nil), lines...)
		r.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		got, err := ReadSpans(strings.NewReader(strings.Join(sh, "\n")))
		if err != nil {
			t.Fatal(err)
		}
		if !equalStrings(spanIDs(got.Records), want) {
			t.Fatalf("attempt %d: shuffled span order changed ids", attempt)
		}
	}
}

// --- helpers ---

func ids(lines []model.LogLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.ID
	}
	return out
}

func spanIDs(spans []model.Span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = s.ID
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pad(n int) string {
	s := "00" + itoa(n)
	return s[len(s)-2:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
