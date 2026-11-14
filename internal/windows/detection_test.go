package windows

import (
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// A capture where roughly half the timeline is incident must still be detected.
//
// This is the case that exposed a real bug: the reference median/MAD was computed
// over EVERY bin. With half the bins elevated the median lands halfway up, the MAD
// becomes the size of the shift itself, and the threshold ends up above the
// maximum observed rate — so no bin could ever be flagged and the window collapsed.
// Short captures (a few minutes) hit this constantly.
func TestDetectWhenHalfTheCaptureIsIncident(t *testing.T) {
	cfg := DefaultConfig()
	start := base()

	var logs []model.LogLine
	// 60 seconds clean at 1 line/second.
	for i := 0; i < 60; i++ {
		logs = append(logs, logAt(time.Duration(i)*time.Second, false))
	}
	// 60 seconds with a 50% error rate: half the capture is incident.
	faultAt := 60 * time.Second
	for i := 0; i < 60; i++ {
		err := i%2 == 0
		logs = append(logs, logAt(faultAt+time.Duration(i)*time.Second, err))
	}

	res := Detect(logs, cfg)
	if !res.Detected {
		t.Fatalf("no change point found when half the capture is incident: %+v", res)
	}

	want := start.Add(faultAt)
	diff := res.IncidentStart.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > 60*time.Second {
		t.Fatalf("detected start off by %v from %v; got %v", diff, want, res.IncidentStart)
	}
}

// The baseline must have real width, not collapse to a point.
//
// A zero-width baseline makes every rate comparison meaningless and quietly
// produces "insufficient evidence" for a capture that had an obvious incident.
func TestDetectedBaselineHasWidth(t *testing.T) {
	cfg := DefaultConfig()
	var logs []model.LogLine
	for i := 0; i < 30; i++ {
		logs = append(logs, logAt(time.Duration(i)*time.Second, false))
	}
	for i := 0; i < 30; i++ {
		logs = append(logs, logAt(time.Duration(30+i)*time.Second, true))
	}

	res := Detect(logs, cfg)
	if !res.Detected {
		t.Fatalf("not detected: %+v", res)
	}
	if !res.BaselineStart.Before(res.BaselineEnd) {
		t.Fatalf("baseline has zero or negative width: %v .. %v",
			res.BaselineStart, res.BaselineEnd)
	}
	if res.BaselineEnd.After(res.IncidentStart) {
		t.Fatalf("baseline overlaps the incident: base ends %v, incident starts %v",
			res.BaselineEnd, res.IncidentStart)
	}
	if !res.IncidentStart.Before(res.IncidentEnd) && !res.IncidentStart.Equal(res.IncidentEnd) {
		t.Fatalf("incident window is inverted: %v .. %v", res.IncidentStart, res.IncidentEnd)
	}
}

// A steady system must not have its incident window collapse to zero width.
func TestNoChangePointStillYieldsUsableWindow(t *testing.T) {
	var logs []model.LogLine
	for i := 0; i < 120; i++ {
		logs = append(logs, logAt(time.Duration(i)*time.Second, false))
	}
	res := Detect(logs, DefaultConfig())

	if !res.BaselineStart.Before(res.BaselineEnd) {
		t.Fatalf("baseline has no width: %v .. %v", res.BaselineStart, res.BaselineEnd)
	}
	if !res.IncidentStart.Before(res.IncidentEnd) {
		t.Fatalf("incident window has no width: %v .. %v", res.IncidentStart, res.IncidentEnd)
	}
}

// lowerHalf must always return something usable.
func TestLowerHalfIsTotal(t *testing.T) {
	cases := [][]float64{nil, {}, {0.5}, {0, 1}, {0, 0, 0, 1, 1, 1}}
	for _, in := range cases {
		got := lowerHalf(in)
		if len(got) == 0 {
			t.Errorf("lowerHalf(%v) returned nothing", in)
		}
		// Must be the LOW half, so its maximum cannot exceed the overall max.
		for _, v := range got {
			for _, orig := range in {
				if v > orig && len(in) > 0 {
					t.Errorf("lowerHalf(%v) = %v contains a value above the input maximum", in, got)
					break
				}
			}
		}
	}
}
