// Package anomaly detects which services changed behaviour between the baseline
// window and the incident window.
//
// Four detectors, all built on robust statistics from internal/stats:
//
//	error_rate           the share of error logs rose
//	latency_p95          the 95th-percentile span duration rose
//	template_burst       one log template appeared far more often
//	new_error_template   an error template appeared that was absent from baseline
//
// Every anomaly carries its onset, its strength (robust z-score), and the ids of
// the records that produced it, so a ranking can be audited rather than trusted.
package anomaly

import (
	"sort"
	"time"

	"github.com/RocketifyLabs/traceline/internal/determinism"
	"github.com/RocketifyLabs/traceline/internal/model"
	"github.com/RocketifyLabs/traceline/internal/stats"
	"github.com/RocketifyLabs/traceline/internal/templates"
)

// Kinds, stable because they appear in reports and golden files.
const (
	KindErrorRate        = "error_rate"
	KindLatencyP95       = "latency_p95"
	KindTemplateBurst    = "template_burst"
	KindNewErrorTemplate = "new_error_template"
)

// Config controls detection thresholds.
type Config struct {
	// ZThreshold is the minimum robust z-score to report an anomaly.
	ZThreshold float64
	// MinEvents is the smallest number of records a service needs in the
	// incident window before a rate comparison is meaningful.
	MinEvents int
	// MinErrorRateDelta is the absolute rise in error rate required, on top of
	// the z-score. Without it a perfectly uniform baseline (MAD = 0) turns any
	// single error into an eight-sigma event.
	MinErrorRateDelta float64
	// MinLatencyRatio is the required proportional rise in p95.
	MinLatencyRatio float64
}

// DefaultConfig matches traceline/rca_weights.json.
func DefaultConfig() Config {
	return Config{
		ZThreshold:        3.5,
		MinEvents:         3,
		MinErrorRateDelta: 0.10,
		MinLatencyRatio:   1.5,
	}
}

// Input is everything the detectors need.
type Input struct {
	BaselineLogs  []model.LogLine
	IncidentLogs  []model.LogLine
	BaselineSpans []model.Span
	IncidentSpans []model.Span
	IncidentStart time.Time
}

// Detect runs every detector and returns the anomalies, sorted deterministically
// by (service, kind).
func Detect(in Input, cfg Config) []model.Anomaly {
	if cfg.ZThreshold <= 0 {
		cfg = DefaultConfig()
	}

	var out []model.Anomaly
	out = append(out, detectErrorRate(in, cfg)...)
	out = append(out, detectLatencyP95(in, cfg)...)
	out = append(out, detectTemplateBurst(in, cfg)...)
	out = append(out, detectNewErrorTemplate(in, cfg)...)

	// Sorted, not map order: two runs must agree.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if !out[i].Onset.Equal(out[j].Onset) {
			return out[i].Onset.Before(out[j].Onset)
		}
		return out[i].Strength > out[j].Strength
	})
	return out
}

// --- error rate ---

func detectErrorRate(in Input, cfg Config) []model.Anomaly {
	base := groupLogs(in.BaselineLogs)
	inc := groupLogs(in.IncidentLogs)

	var out []model.Anomaly
	for _, svc := range determinism.SortedKeys(inc) {
		cur := inc[svc]
		if cur.total < cfg.MinEvents {
			continue
		}
		incRate := rate(cur)
		baseRate := rate(base[svc])

		// Two independent gates. The z-score says "unusual relative to the other
		// services"; the absolute delta says "actually worse". A uniform baseline
		// makes MAD zero, and without the second gate any single error would
		// score 1e8 sigma.
		if incRate-baseRate < cfg.MinErrorRateDelta {
			continue
		}

		// Baseline sample for the z-score: per-service rates are a single value,
		// so the comparison distribution is the cross-service baseline rates.
		// This is what makes a service that was already erroring look normal.
		z := stats.RobustZ(incRate, allRates(base))
		if z < cfg.ZThreshold {
			continue
		}
		out = append(out, model.Anomaly{
			Service:  svc,
			Kind:     KindErrorRate,
			Onset:    onsetOf(cur.lines, baseRate, cfg),
			Strength: z,
			LogIDs:   idsOf(cur.lines),
		})
	}
	return out
}

// --- latency p95 ---

func detectLatencyP95(in Input, cfg Config) []model.Anomaly {
	base := groupSpans(in.BaselineSpans)
	inc := groupSpans(in.IncidentSpans)

	var out []model.Anomaly
	for _, svc := range determinism.SortedKeys(inc) {
		cur := inc[svc]
		if len(cur) < cfg.MinEvents {
			continue
		}
		baseDurations := durationsOf(base[svc])
		if len(baseDurations) == 0 {
			continue
		}
		incDurations := durationsOf(cur)
		baseP95 := stats.Percentile(baseDurations, 95)
		incP95 := stats.Percentile(incDurations, 95)

		// Same two-gate discipline as the error-rate detector: a proportional
		// floor guards against a zero-MAD baseline turning 10ms into 11ms into a
		// 1e14 sigma event.
		if baseP95 <= 0 {
			continue
		}
		if incP95/baseP95 < cfg.MinLatencyRatio {
			continue
		}

		// Score the incident p95 against the baseline's own spread, using each
		// baseline duration as a sample of normal behaviour.
		z := stats.RobustZ(incP95, baseDurations)
		if z < cfg.ZThreshold {
			continue
		}
		out = append(out, model.Anomaly{
			Service:  svc,
			Kind:     KindLatencyP95,
			Onset:    firstSlow(cur, time.Duration(baseP95)),
			Strength: z,
			SpanIDs:  spanIDsOf(cur),
		})
	}
	return out
}

// --- template burst ---

func detectTemplateBurst(in Input, cfg Config) []model.Anomaly {
	baseCounts := templateCounts(in.BaselineLogs)
	incCounts := templateCounts(in.IncidentLogs)

	var out []model.Anomaly
	for _, svc := range determinism.SortedKeys(incCounts) {
		cur := incCounts[svc]
		total := 0
		for _, n := range cur {
			total += n
		}
		if total < cfg.MinEvents {
			continue
		}
		prev := baseCounts[svc]

		var bestTemplate string
		var bestZ float64
		var bestLogs []model.LogLine

		for _, tmpl := range determinism.SortedKeys(cur) {
			count := cur[tmpl]
			// Compare the incident count to the baseline count for the same
			// template. A template absent from baseline has a zero count and is
			// handled by the new-error-template detector instead.
			baseCount := prev[tmpl]
			// Distribution of baseline template counts on this service.
			z := stats.RobustZ(float64(count), templateCountValues(prev))
			if baseCount == 0 && len(prev) == 0 {
				// No baseline at all for this service: nothing to compare to.
				continue
			}
			if z > bestZ {
				bestZ = z
				bestTemplate = tmpl
				bestLogs = logsForTemplate(in.IncidentLogs, svc, tmpl)
			}
		}
		if bestZ < cfg.ZThreshold {
			continue
		}
		out = append(out, model.Anomaly{
			Service:  svc,
			Kind:     KindTemplateBurst,
			Onset:    firstOf(bestLogs),
			Strength: bestZ,
			LogIDs:   idsOf(bestLogs),
			Template: bestTemplate,
		})
	}
	return out
}

// --- new error template ---

func detectNewErrorTemplate(in Input, cfg Config) []model.Anomaly {
	baseTemplates := errorTemplates(in.BaselineLogs)
	incLogs := errorLogsByService(in.IncidentLogs)

	var out []model.Anomaly
	for _, svc := range determinism.SortedKeys(incLogs) {
		lines := incLogs[svc]
		if len(lines) < 1 {
			continue
		}
		byTemplate := map[string][]model.LogLine{}
		for _, l := range lines {
			k := templates.Key(l.Msg)
			byTemplate[k] = append(byTemplate[k], l)
		}

		for _, tmpl := range determinism.SortedKeys(byTemplate) {
			if baseTemplates[svc][tmpl] {
				continue // already present before the incident
			}
			group := byTemplate[tmpl]
			// A brand-new error template is strong evidence even at low counts;
			// strength scales with how many times it appeared.
			strength := cfg.ZThreshold + float64(len(group))
			out = append(out, model.Anomaly{
				Service:  svc,
				Kind:     KindNewErrorTemplate,
				Onset:    firstOf(group),
				Strength: strength,
				LogIDs:   idsOf(group),
				Template: tmpl,
			})
		}
	}
	return out
}

// --- helpers ---

type logStat struct {
	total int
	errs  int
	lines []model.LogLine
}

func groupLogs(logs []model.LogLine) map[string]*logStat {
	m := map[string]*logStat{}
	for _, l := range logs {
		s := m[l.Service]
		if s == nil {
			s = &logStat{}
			m[l.Service] = s
		}
		s.total++
		if l.Level.IsError() {
			s.errs++
		}
		s.lines = append(s.lines, l)
	}
	return m
}

func rate(s *logStat) float64 {
	if s == nil || s.total == 0 {
		return 0
	}
	return float64(s.errs) / float64(s.total)
}

// allRates returns every service's baseline error rate, the comparison sample.
func allRates(base map[string]*logStat) []float64 {
	out := make([]float64, 0, len(base))
	for _, svc := range determinism.SortedKeys(base) {
		out = append(out, rate(base[svc]))
	}
	if len(out) == 0 {
		out = append(out, 0)
	}
	return out
}

// onsetOf returns when a service's error rate actually became elevated, not
// simply when its first stray error appeared.
//
// Taking the first error line is wrong in the presence of background noise: a
// healthy caller can emit one unrelated error seconds before the real cause
// starts, and it then looks like the earliest symptom of all. The onset is
// therefore the start of the first bin whose error rate is materially higher than
// the service's own baseline rate.
func onsetOf(lines []model.LogLine, baselineRate float64, cfg Config) time.Time {
	if len(lines) == 0 {
		return time.Time{}
	}
	first := lines[0].TS
	for _, l := range lines {
		if l.TS.Before(first) {
			first = l.TS
		}
	}

	// Bin the error lines and find the first bin that is clearly worse than
	// baseline. The bin width matches the window change-point detector so the two
	// agree about what "elevated" means.
	const binWidth = 2 * time.Second
	buckets := map[int]int{}
	for _, l := range lines {
		if !l.Level.IsError() {
			continue
		}
		idx := int(l.TS.Sub(first) / binWidth)
		buckets[idx]++
	}
	if len(buckets) == 0 {
		return first
	}

	// A bin is elevated when its error count is above what the baseline rate
	// would predict for that width, with a floor of two errors. One error is not
	// a trend.
	expected := baselineRate * binWidth.Seconds()
	threshold := expected * 3
	if threshold < 2 {
		threshold = 2
	}

	indices := make([]int, 0, len(buckets))
	for idx := range buckets {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		if float64(buckets[idx]) > threshold {
			return first.Add(time.Duration(idx) * binWidth)
		}
	}
	// No bin stood out: fall back to the first error, which is the honest answer
	// when the rate never clearly rose.
	for _, l := range lines {
		if l.Level.IsError() {
			return l.TS
		}
	}
	return first
}

func idsOf(lines []model.LogLine) []string {
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if _, ok := seen[l.ID]; ok {
			continue
		}
		seen[l.ID] = struct{}{}
		out = append(out, l.ID)
	}
	sort.Strings(out)
	return out
}

func groupSpans(spans []model.Span) map[string][]model.Span {
	m := map[string][]model.Span{}
	for _, s := range spans {
		m[s.Service] = append(m[s.Service], s)
	}
	return m
}

func durationsOf(spans []model.Span) []float64 {
	out := make([]float64, len(spans))
	for i, s := range spans {
		out[i] = float64(s.Duration())
	}
	return out
}

func spanIDsOf(spans []model.Span) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.ID)
	}
	sort.Strings(out)
	return out
}

func firstSlow(spans []model.Span, threshold time.Duration) time.Time {
	var best time.Time
	for _, s := range spans {
		if s.Duration() >= threshold {
			if best.IsZero() || s.Start.Before(best) {
				best = s.Start
			}
		}
	}
	if best.IsZero() && len(spans) > 0 {
		best = spans[0].Start
	}
	return best
}

func templateCounts(logs []model.LogLine) map[string]map[string]int {
	m := map[string]map[string]int{}
	for _, l := range logs {
		if m[l.Service] == nil {
			m[l.Service] = map[string]int{}
		}
		m[l.Service][templates.Key(l.Msg)]++
	}
	return m
}

func templateCountValues(counts map[string]int) []float64 {
	if len(counts) == 0 {
		return []float64{0}
	}
	out := make([]float64, 0, len(counts))
	for _, tmpl := range determinism.SortedKeys(counts) {
		out = append(out, float64(counts[tmpl]))
	}
	return out
}

func logsForTemplate(logs []model.LogLine, svc, tmplKey string) []model.LogLine {
	var out []model.LogLine
	for _, l := range logs {
		if l.Service == svc && templates.Key(l.Msg) == tmplKey {
			out = append(out, l)
		}
	}
	return out
}

func firstOf(lines []model.LogLine) time.Time {
	if len(lines) == 0 {
		return time.Time{}
	}
	best := lines[0].TS
	for _, l := range lines[1:] {
		if l.TS.Before(best) {
			best = l.TS
		}
	}
	return best
}

func errorTemplates(logs []model.LogLine) map[string]map[string]bool {
	m := map[string]map[string]bool{}
	for _, l := range logs {
		if !l.Level.IsError() {
			continue
		}
		if m[l.Service] == nil {
			m[l.Service] = map[string]bool{}
		}
		m[l.Service][templates.Key(l.Msg)] = true
	}
	return m
}

func errorLogsByService(logs []model.LogLine) map[string][]model.LogLine {
	m := map[string][]model.LogLine{}
	for _, l := range logs {
		if l.Level.IsError() {
			m[l.Service] = append(m[l.Service], l)
		}
	}
	return m
}
