package windows

import (
	"math/rand"
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

func base() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func logAt(offset time.Duration, err bool) model.LogLine {
	level := model.LevelInfo
	if err {
		level = model.LevelError
	}
	return model.LogLine{TS: base().Add(offset), Service: "svc", Level: level, Msg: "m"}
}

// buildLogs creates a healthy baseline followed by an error burst starting at
// faultAfter, ending at faultEnd.
func buildLogs(healthyFrom, faultAfter, total time.Duration, perSecond int) []model.LogLine {
	var out []model.LogLine
	for t := healthyFrom; t < faultAfter; t += time.Second / time.Duration(perSecond) {
		out = append(out, logAt(t, false))
		if perSecond > 1 {
			out = append(out, logAt(t+time.Millisecond, false))
		}
	}
	for t := faultAfter; t < total; t += time.Second / time.Duration(perSecond) {
		out = append(out, logAt(t, true))
		if perSecond > 1 {
			out = append(out, logAt(t+time.Millisecond, true))
		}
	}
	return out
}

func TestDetectWithinTolerance(t *testing.T) {
	cfg := DefaultConfig()
	fault := 5 * time.Minute
	logs := buildLogs(0, fault, 8*time.Minute, 4)

	res := Detect(logs, cfg)
	if !res.Detected {
		t.Fatalf("no change point detected: %+v", res)
	}
	diff := res.IncidentStart.Sub(base().Add(fault))
	if diff < 0 {
		diff = -diff
	}
	// Spec FR-3: detected start within 60s of the seeded fault start.
	if diff > 60*time.Second {
		t.Fatalf("detected start off by %v (limit 60s); got %v want ~%v",
			diff, res.IncidentStart, base().Add(fault))
	}
}

func TestDetectIgnoresSingleSpike(t *testing.T) {
	cfg := DefaultConfig()
	// One error-heavy bin surrounded by clean bins: not an incident.
	var logs []model.LogLine
	for t := time.Duration(0); t < 4*time.Minute; t += 500 * time.Millisecond {
		logs = append(logs, logAt(t, false))
	}
	spike := 2 * time.Minute
	for i := 0; i < 20; i++ {
		logs = append(logs, logAt(spike+time.Duration(i)*100*time.Millisecond, true))
	}
	res := Detect(logs, cfg)
	// Either nothing is detected, or the start is the spike itself; it must not
	// be earlier, and a lone bin must not anchor a confident incident.
	if res.Detected && res.IncidentStart.Before(base().Add(spike)) {
		t.Fatalf("single spike detected before the spike: %+v", res)
	}
}

func TestDetectEmptyLogs(t *testing.T) {
	res := Detect(nil, DefaultConfig())
	if res.Detected || res.Scanned != 0 {
		t.Fatalf("Detect(nil) = %+v", res)
	}
}

func TestDetectTooFewBins(t *testing.T) {
	// Three logs inside one bin: not enough to establish a change.
	logs := []model.LogLine{logAt(0, false), logAt(time.Second, false), logAt(2*time.Second, true)}
	res := Detect(logs, DefaultConfig())
	if res.Detected {
		t.Fatalf("detected a change from three logs: %+v", res)
	}
}

func TestDetectConstantBaselineDoesNotDivideByZero(t *testing.T) {
	// Every bin has exactly the same error rate: MAD is zero.
	var logs []model.LogLine
	for t := time.Duration(0); t < 3*time.Minute; t += time.Second {
		logs = append(logs, logAt(t, false))
	}
	res := Detect(logs, DefaultConfig())
	// Must return something sane, not NaN-poisoned bounds.
	if res.IncidentStart.IsZero() {
		t.Fatal("IncidentStart is zero")
	}
	if res.BaselineStart.After(res.BaselineEnd) {
		t.Fatalf("baseline inverted: %+v", res)
	}
}

func TestFromExplicitHonorsWindowExactly(t *testing.T) {
	cfg := DefaultConfig()
	start := base().Add(10 * time.Minute)
	end := base().Add(20 * time.Minute)
	res := FromExplicit(start, end, cfg)
	if !res.IncidentStart.Equal(start) || !res.IncidentEnd.Equal(end) {
		t.Fatalf("window = %v..%v, want %v..%v", res.IncidentStart, res.IncidentEnd, start, end)
	}
	if res.Detected {
		t.Error("explicit window must not report detection")
	}
	if !res.BaselineEnd.Equal(start) {
		t.Errorf("BaselineEnd = %v, want incident start", res.BaselineEnd)
	}
}

func TestFromExplicitBaselineIsCapped(t *testing.T) {
	cfg := DefaultConfig()
	start := base().Add(2 * time.Hour)
	res := FromExplicit(start, start.Add(time.Hour), cfg)
	want := start.Add(-cfg.BaselineCap)
	if !res.BaselineStart.Equal(want) {
		t.Fatalf("BaselineStart = %v, want %v (capped at 20m)", res.BaselineStart, want)
	}
}

func TestDetectIsDeterministicUnderShuffle(t *testing.T) {
	logs := buildLogs(0, 4*time.Minute, 7*time.Minute, 4)
	want := Detect(logs, DefaultConfig())

	r := rand.New(rand.NewSource(5))
	for attempt := 0; attempt < 10; attempt++ {
		sh := append([]model.LogLine(nil), logs...)
		r.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		// Detect must not depend on input order: bins are time-derived.
		got := Detect(sh, DefaultConfig())
		if !got.IncidentStart.Equal(want.IncidentStart) {
			t.Fatalf("attempt %d: start = %v, want %v", attempt, got.IncidentStart, want.IncidentStart)
		}
	}
}

func TestSplitPartitionsWithoutOverlap(t *testing.T) {
	logs := buildLogs(0, 4*time.Minute, 7*time.Minute, 2)
	res := Detect(logs, DefaultConfig())
	baseline, incident := Split(logs, res)

	if len(baseline) == 0 || len(incident) == 0 {
		t.Fatalf("split produced baseline=%d incident=%d", len(baseline), len(incident))
	}
	// No record may appear in both.
	seen := make(map[string]bool, len(baseline))
	for _, l := range baseline {
		seen[l.TS.String()] = true
	}
	for _, l := range incident {
		if seen[l.TS.String()] {
			t.Fatalf("record %v appears in both windows", l.TS)
		}
	}
}

func TestSplitSpansByStart(t *testing.T) {
	res := FromExplicit(base().Add(time.Minute), base().Add(2*time.Minute), DefaultConfig())
	spans := []model.Span{
		{Service: "a", Start: base().Add(-time.Minute), End: base().Add(-time.Minute + time.Second)},
		{Service: "b", Start: base().Add(90 * time.Second), End: base().Add(91 * time.Second)},
	}
	baseline, incident := SplitSpans(spans, res)
	if len(baseline) != 1 || baseline[0].Service != "a" {
		t.Fatalf("baseline = %+v", baseline)
	}
	if len(incident) != 1 || incident[0].Service != "b" {
		t.Fatalf("incident = %+v", incident)
	}
}
