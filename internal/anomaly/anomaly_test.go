package anomaly

import (
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

func t0() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func mkLog(svc string, ts time.Time, level model.Level, msg, id string) model.LogLine {
	return model.LogLine{ID: id, TS: ts, Service: svc, Level: level, Msg: msg}
}

func mkSpan(svc string, start time.Time, dur time.Duration, status model.Status, id string) model.Span {
	return model.Span{ID: id, Service: svc, Start: start, End: start.Add(dur), Status: status}
}

// healthyBaseline builds uniform info logs for the given services.
func healthyBaseline(services []string, n int) []model.LogLine {
	var out []model.LogLine
	for i, svc := range services {
		for j := 0; j < n; j++ {
			out = append(out, mkLog(svc, t0().Add(time.Duration(j)*time.Second),
				model.LevelInfo, "request ok", "b-"+svc+"-"+itoa(j)+"-"+itoa(i)))
		}
	}
	return out
}

// ---- error rate detector ----

func TestDetectErrorRateFindsSpike(t *testing.T) {
	base := healthyBaseline([]string{"api", "db", "cache"}, 20)
	inc := healthyBaseline([]string{"api", "db", "cache"}, 20)
	// api now fails 80% of the time.
	for i := 0; i < 20; i++ {
		inc = append(inc, mkLog("api", t0().Add(time.Duration(100+i)*time.Second),
			model.LevelError, "upstream timeout", "e-"+itoa(i)))
	}
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	if len(got) == 0 {
		t.Fatal("no anomaly detected for an 80% error rate")
	}
	if got[0].Service != "api" || got[0].Kind != KindErrorRate {
		t.Fatalf("first anomaly = %s/%s, want api/%s", got[0].Service, got[0].Kind, KindErrorRate)
	}
	if got[0].Strength < 3.5 {
		t.Errorf("strength = %v, want >= 3.5", got[0].Strength)
	}
	if len(got[0].LogIDs) == 0 {
		t.Error("anomaly carries no evidence ids")
	}
}

func TestDetectErrorRateNegativeCase(t *testing.T) {
	// Flat, healthy system: nothing must be reported.
	base := healthyBaseline([]string{"api", "db", "cache"}, 20)
	inc := healthyBaseline([]string{"api", "db", "cache"}, 20)
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	for _, a := range got {
		if a.Kind == KindErrorRate {
			t.Fatalf("false positive: %+v", a)
		}
	}
}

func TestDetectErrorRateIgnoresAlreadyBrokenService(t *testing.T) {
	// db was already failing in the baseline; it is not the new problem.
	base := healthyBaseline([]string{"api", "cache"}, 20)
	for i := 0; i < 20; i++ {
		base = append(base, mkLog("db", t0().Add(time.Duration(i)*time.Second),
			model.LevelError, "db down", "bd-"+itoa(i)))
	}
	inc := healthyBaseline([]string{"api", "cache"}, 20)
	for i := 0; i < 20; i++ {
		inc = append(inc, mkLog("db", t0().Add(time.Duration(100+i)*time.Second),
			model.LevelError, "db down", "id-"+itoa(i)))
	}
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	for _, a := range got {
		if a.Service == "db" && a.Kind == KindErrorRate {
			t.Fatalf("a service that was already broken must not be flagged as new: %+v", a)
		}
	}
}

func TestDetectErrorRateRespectsMinEvents(t *testing.T) {
	base := healthyBaseline([]string{"api"}, 20)
	// Only two incident records, both errors: too thin to call.
	inc := []model.LogLine{
		mkLog("api", t0().Add(100*time.Second), model.LevelError, "boom", "x1"),
		mkLog("api", t0().Add(101*time.Second), model.LevelError, "boom", "x2"),
	}
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	for _, a := range got {
		if a.Kind == KindErrorRate && a.Service == "api" {
			t.Fatalf("min events not respected: %+v", a)
		}
	}
}

// ---- latency detector ----

func TestDetectLatencyFindsShift(t *testing.T) {
	var base, inc []model.Span
	for i := 0; i < 20; i++ {
		base = append(base, mkSpan("db", t0().Add(time.Duration(i)*time.Second),
			10*time.Millisecond, model.StatusOK, "b"+itoa(i)))
		inc = append(inc, mkSpan("db", t0().Add(time.Duration(100+i)*time.Second),
			2*time.Second, model.StatusOK, "i"+itoa(i)))
	}
	got := Detect(Input{BaselineSpans: base, IncidentSpans: inc}, DefaultConfig())
	if len(got) == 0 {
		t.Fatal("no latency anomaly detected")
	}
	if got[0].Kind != KindLatencyP95 {
		t.Fatalf("kind = %s, want %s", got[0].Kind, KindLatencyP95)
	}
	if len(got[0].SpanIDs) == 0 {
		t.Error("latency anomaly carries no span evidence")
	}
}

func TestDetectLatencyNegativeCase(t *testing.T) {
	var base, inc []model.Span
	for i := 0; i < 20; i++ {
		base = append(base, mkSpan("db", t0().Add(time.Duration(i)*time.Second),
			10*time.Millisecond, model.StatusOK, "b"+itoa(i)))
		inc = append(inc, mkSpan("db", t0().Add(time.Duration(100+i)*time.Second),
			11*time.Millisecond, model.StatusOK, "i"+itoa(i)))
	}
	got := Detect(Input{BaselineSpans: base, IncidentSpans: inc}, DefaultConfig())
	for _, a := range got {
		if a.Kind == KindLatencyP95 {
			t.Fatalf("false positive on a 1ms change: %+v", a)
		}
	}
}

// ---- template detectors ----

func TestDetectNewErrorTemplate(t *testing.T) {
	base := healthyBaseline([]string{"api"}, 20)
	inc := healthyBaseline([]string{"api"}, 20)
	inc = append(inc, mkLog("api", t0().Add(200*time.Second), model.LevelError,
		"connection pool exhausted on host 10.0.0.7", "n1"))
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc, IncidentStart: t0()}, DefaultConfig())
	var found bool
	for _, a := range got {
		if a.Kind == KindNewErrorTemplate && a.Service == "api" {
			found = true
			if a.Template == "" {
				t.Error("new template anomaly has no template text")
			}
			if a.Onset.IsZero() {
				t.Error("new template anomaly has no onset")
			}
		}
	}
	if !found {
		t.Fatalf("new error template not detected; got %+v", got)
	}
}

func TestDetectTemplateBurst(t *testing.T) {
	var base, inc []model.LogLine
	// Baseline: two distinct templates, balanced.
	for i := 0; i < 20; i++ {
		base = append(base, mkLog("api", t0().Add(time.Duration(i)*time.Second),
			model.LevelInfo, "request ok", "ba"+itoa(i)))
		base = append(base, mkLog("api", t0().Add(time.Duration(i)*time.Second),
			model.LevelInfo, "cache hit", "bb"+itoa(i)))
	}
	// Incident: "retrying upstream" explodes.
	for i := 0; i < 20; i++ {
		inc = append(inc, mkLog("api", t0().Add(time.Duration(100+i)*time.Second),
			model.LevelWarn, "request ok", "ia"+itoa(i)))
	}
	for i := 0; i < 60; i++ {
		inc = append(inc, mkLog("api", t0().Add(time.Duration(100+i)*time.Second),
			model.LevelWarn, "retrying upstream", "ib"+itoa(i)))
	}
	got := Detect(Input{BaselineLogs: base, IncidentLogs: inc, IncidentStart: t0()}, DefaultConfig())
	var found bool
	for _, a := range got {
		if a.Kind == KindTemplateBurst && a.Service == "api" {
			found = true
		}
	}
	if !found {
		t.Fatalf("template burst not detected; got %+v", got)
	}
}

func TestDetectNoAnomalyOnEmptyInput(t *testing.T) {
	got := Detect(Input{}, DefaultConfig())
	if len(got) != 0 {
		t.Fatalf("empty input produced anomalies: %+v", got)
	}
}

func TestDetectNoAnomalyOnBaselineOnly(t *testing.T) {
	base := healthyBaseline([]string{"api", "db"}, 20)
	got := Detect(Input{BaselineLogs: base}, DefaultConfig())
	if len(got) != 0 {
		t.Fatalf("baseline-only input produced anomalies: %+v", got)
	}
}

func TestDetectIsSortedDeterministically(t *testing.T) {
	base := healthyBaseline([]string{"zeta", "alpha", "db"}, 20)
	inc := healthyBaseline([]string{"zeta", "alpha", "db"}, 20)
	for _, svc := range []string{"zeta", "alpha", "db"} {
		for i := 0; i < 30; i++ {
			inc = append(inc, mkLog(svc, t0().Add(time.Duration(200+i)*time.Second),
				model.LevelError, "boom", "e-"+svc+"-"+itoa(i)))
		}
	}
	first := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	second := Detect(Input{BaselineLogs: base, IncidentLogs: inc}, DefaultConfig())
	if len(first) != len(second) {
		t.Fatalf("length differs between runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Service != second[i].Service || first[i].Kind != second[i].Kind {
			t.Fatalf("run differs at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
	// And the order is the documented one.
	for i := 1; i < len(first); i++ {
		if first[i-1].Service > first[i].Service {
			t.Fatalf("not sorted by service: %s before %s", first[i-1].Service, first[i].Service)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
