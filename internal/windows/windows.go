// Package windows decides which part of the timeline is the incident and which
// part is the baseline it is compared against.
//
// Getting this wrong makes every downstream number meaningless: a baseline that
// overlaps the incident hides the anomaly, and a baseline that is too short
// makes MAD collapse to zero and every service look broken.
package windows

import (
	"sort"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// Config holds the defaults documented in docs/spec/spec.md FR-3.
type Config struct {
	// BaselineCap bounds how far back the baseline reaches.
	BaselineCap time.Duration
	// BinSize is the change-point bucket width.
	BinSize time.Duration
	// MADK is how many baseline MADs above the median an error rate must be.
	MADK float64
	// ConsecutiveBins is how many bins must stay elevated. A single spike is
	// noise, not an incident.
	ConsecutiveBins int
}

// DefaultConfig matches the values in traceline/rca_weights.json.
func DefaultConfig() Config {
	return Config{
		BaselineCap:     20 * time.Minute,
		BinSize:         10 * time.Second,
		MADK:            5.0,
		ConsecutiveBins: 2,
	}
}

// Result is the chosen analysis window.
type Result struct {
	BaselineStart time.Time
	BaselineEnd   time.Time
	IncidentStart time.Time
	IncidentEnd   time.Time
	// Detected is false when an explicit window was supplied.
	Detected bool
	// Scanned is the number of bins considered; zero means detection had no
	// data to work with and the caller must report insufficient evidence.
	Scanned int
}

// FromExplicit builds a window from user-supplied bounds, disabling detection.
//
// The baseline is the pre-incident window capped at BaselineCap, exactly as in
// the detected case, so behaviour does not change based on how the window was
// chosen.
func FromExplicit(start, end time.Time, cfg Config) Result {
	if cfg.BaselineCap <= 0 {
		cfg = DefaultConfig()
	}
	baselineEnd := start
	baselineStart := start.Add(-cfg.BaselineCap)
	return Result{
		BaselineStart: baselineStart,
		BaselineEnd:   baselineEnd,
		IncidentStart: start,
		IncidentEnd:   end,
		Detected:      false,
	}
}

// bin is one time bucket with its error counts.
type bin struct {
	start time.Time
	total int
	errs  int
}

// Detect finds the incident start from the log stream.
//
// Method, fixed by the spec: bucket logs into BinSize bins by error rate, take
// the median error rate of the baseline bins, and report the first bin whose
// rate exceeds median + MADK*MAD and stays elevated for ConsecutiveBins bins.
//
// The median/MAD reference is deliberately robust: a single catastrophic bin
// would drag a mean to itself and then look ordinary, which is the same bug
// that min/max caused in clock-skew detection.
func Detect(logs []model.LogLine, cfg Config) Result {
	if cfg.BinSize <= 0 {
		cfg = DefaultConfig()
	}
	if len(logs) == 0 {
		return Result{}
	}

	if cfg.BaselineCap <= 0 {
		cfg.BaselineCap = DefaultConfig().BaselineCap
	}
	if cfg.ConsecutiveBins < 1 {
		cfg.ConsecutiveBins = 1
	}
	if cfg.MADK <= 0 {
		cfg.MADK = DefaultConfig().MADK
	}

	first, last := logs[0].TS, logs[len(logs)-1].TS
	for _, l := range logs {
		if l.TS.Before(first) {
			first = l.TS
		}
		if l.TS.After(last) {
			last = l.TS
		}
	}

	bins := makeBins(logs, first, last, cfg.BinSize)
	if len(bins) < cfg.ConsecutiveBins+1 {
		// Too few bins to establish a baseline and a change. Say so rather than
		// inventing a start.
		return Result{
			BaselineStart: first,
			BaselineEnd:   first,
			IncidentStart: first,
			IncidentEnd:   last,
			Scanned:       len(bins),
		}
	}

	// Baseline reference: the median and MAD of the HEALTHY bins.
	//
	// Computing it over every bin is wrong whenever a large share of the capture
	// is incident. With half the bins elevated the median lands halfway up, the
	// MAD becomes the size of the shift itself, and the threshold ends up above
	// the maximum observed rate — no bin can ever be flagged. In a short capture
	// (a few minutes) that is the common case, not the edge case.
	//
	// The fix is to build the reference from the lower half of the rates. If up
	// to half the capture is anomalous, those bins are baseline by construction;
	// if the capture is mostly healthy, the lower half is a conservative estimate
	// that only makes the detector slightly more sensitive.
	rates := ratesOf(bins)
	reference := lowerHalf(rates)
	med := medianOf(reference)
	mad := madOf(reference, med)
	if mad <= 0 {
		// A perfectly flat healthy baseline still needs a floor, or the first bin
		// with a single error counts as a 5-sigma event.
		mad = 0.01
	}
	threshold := med + cfg.MADK*mad

	// Find the first run of ConsecutiveBins elevated bins.
	for i := 0; i+cfg.ConsecutiveBins <= len(bins); i++ {
		ok := true
		for j := 0; j < cfg.ConsecutiveBins; j++ {
			if rates[i+j] <= threshold {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		start := bins[i].start
		baselineStart := start.Add(-cfg.BaselineCap)
		if baselineStart.Before(first) {
			baselineStart = first
		}
		return Result{
			BaselineStart: baselineStart,
			BaselineEnd:   start,
			IncidentStart: start,
			IncidentEnd:   last,
			Detected:      true,
			Scanned:       len(bins),
		}
	}

	// No change point: everything is baseline and the incident is the tail.
	//
	// The tail must have real width, not collapse to a point. A zero-width
	// window makes every downstream comparison produce nothing and quietly
	// reports "insufficient evidence" for a capture that simply had a steady
	// error rate — which is a wrong diagnosis, not an honest one.
	//
	// The tail is a quarter of the observed range, not BaselineCap/4: a capture
	// shorter than the cap would otherwise have its tail start before the first
	// record, which silently produced a zero-width baseline.
	total := last.Sub(first)
	if total <= 0 {
		// A single instant of data: give both windows a nominal width so the
		// caller never divides by zero.
		return Result{
			BaselineStart: first,
			BaselineEnd:   first,
			IncidentStart: first,
			IncidentEnd:   first,
			Detected:      false,
			Scanned:       len(bins),
		}
	}
	tail := total / 4
	if tail > cfg.BaselineCap/4 {
		tail = cfg.BaselineCap / 4
	}
	if tail < time.Second {
		tail = time.Second
	}
	if tail >= total {
		tail = total / 2
	}
	incidentStart := last.Add(-tail)
	if !incidentStart.After(first) {
		incidentStart = first.Add(total / 2)
	}
	return Result{
		BaselineStart: first,
		BaselineEnd:   incidentStart,
		IncidentStart: incidentStart,
		IncidentEnd:   last,
		Detected:      false,
		Scanned:       len(bins),
	}
}

// ratesOf returns each bin's error rate.
func ratesOf(bins []bin) []float64 {
	rates := make([]float64, len(bins))
	for i, b := range bins {
		if b.total > 0 {
			rates[i] = float64(b.errs) / float64(b.total)
		}
	}
	return rates
}

// lowerHalf returns the lower half of the rates, which is the baseline candidate
// set.
//
// At least one element is always returned, so a single-bin capture still has a
// reference.
func lowerHalf(rates []float64) []float64 {
	if len(rates) == 0 {
		return []float64{0}
	}
	c := append([]float64(nil), rates...)
	sort.Float64s(c)
	n := len(c) / 2
	if n < 1 {
		n = 1
	}
	return c[:n]
}

// makeBins buckets logs by BinSize starting at first.
func makeBins(logs []model.LogLine, first, last time.Time, size time.Duration) []bin {
	count := int(last.Sub(first)/size) + 1
	bins := make([]bin, count)
	for i := range bins {
		bins[i].start = first.Add(time.Duration(i) * size)
	}
	for _, l := range logs {
		idx := int(l.TS.Sub(first) / size)
		if idx < 0 {
			idx = 0
		}
		if idx >= len(bins) {
			idx = len(bins) - 1
		}
		bins[idx].total++
		if l.Level.IsError() {
			bins[idx].errs++
		}
	}
	return bins
}

func medianOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func madOf(xs []float64, med float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	dev := make([]float64, len(xs))
	for i, x := range xs {
		d := x - med
		if d < 0 {
			d = -d
		}
		dev[i] = d
	}
	return medianOf(dev)
}

// Split partitions logs into baseline and incident slices using the window.
// Both slices are returned in their original (already sorted) order.
func Split(logs []model.LogLine, r Result) (baseline, incident []model.LogLine) {
	for _, l := range logs {
		switch {
		case !l.TS.Before(r.BaselineStart) && l.TS.Before(r.BaselineEnd):
			baseline = append(baseline, l)
		case !l.TS.Before(r.IncidentStart) && !l.TS.After(r.IncidentEnd):
			incident = append(incident, l)
		}
	}
	return baseline, incident
}

// SplitSpans partitions spans by their start time.
func SplitSpans(spans []model.Span, r Result) (baseline, incident []model.Span) {
	for _, s := range spans {
		switch {
		case !s.Start.Before(r.BaselineStart) && s.Start.Before(r.BaselineEnd):
			baseline = append(baseline, s)
		case !s.Start.Before(r.IncidentStart) && !s.Start.After(r.IncidentEnd):
			incident = append(incident, s)
		}
	}
	return baseline, incident
}
