// Package timeline builds the chronological narrative of an incident.
//
// A ranked root cause is an answer to "what broke"; the timeline is the answer
// to "how did it unfold", which is what a postmortem actually needs. It merges
// three sources into one ordered list: the first anomaly on each service,
// lifecycle events (deploy, config_change), and the error cascade.
package timeline

import (
	"sort"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// Kinds, stable because they appear in reports and golden files.
const (
	KindFirstAnomaly = "first_anomaly"
	KindDeploy       = "deploy"
	KindConfigChange = "config_change"
	KindCascade      = "cascade"
	KindSkippedData  = "skipped_data"
)

// Build assembles the timeline from anomalies, logs and the window bounds.
//
// Ordering is by timestamp, then by kind, then by service, then by detail — a
// total order, so two runs on the same input produce the same rows in the same
// sequence.
func Build(anomalies []model.Anomaly, logs []model.LogLine, incidentStart, incidentEnd time.Time) []model.TimelineEvent {
	var events []model.TimelineEvent

	// 1. Lifecycle events first: a deploy immediately before symptoms is one of
	//    the strongest signals in the whole analysis.
	for _, l := range logs {
		switch l.Event() {
		case "deploy":
			events = append(events, model.TimelineEvent{
				At:          l.TS,
				Kind:        KindDeploy,
				Service:     l.Service,
				Detail:      firstNonEmpty(l.Field("version"), l.Msg),
				EvidenceIDs: []string{l.ID},
			})
		case "config_change":
			events = append(events, model.TimelineEvent{
				At:          l.TS,
				Kind:        KindConfigChange,
				Service:     l.Service,
				Detail:      firstNonEmpty(l.Field("config"), l.Msg),
				EvidenceIDs: []string{l.ID},
			})
		}
	}

	// 2. First anomaly per service. One row per service, not one per anomaly:
	//    the timeline says when a service first went bad, not every time.
	firstPerService := map[string]model.Anomaly{}
	for _, a := range anomalies {
		if a.Onset.IsZero() {
			continue
		}
		cur, ok := firstPerService[a.Service]
		if !ok || a.Onset.Before(cur.Onset) {
			firstPerService[a.Service] = a
		}
	}
	// Sorted keys: the map above must not decide the output order.
	services := make([]string, 0, len(firstPerService))
	for svc := range firstPerService {
		services = append(services, svc)
	}
	sort.Strings(services)
	for _, svc := range services {
		a := firstPerService[svc]
		events = append(events, model.TimelineEvent{
			At:          a.Onset,
			Kind:        KindFirstAnomaly,
			Service:     svc,
			Detail:      a.Kind,
			EvidenceIDs: evidenceOf(a),
		})
	}

	// 3. Cascade: error onset per service, which shows the propagation order.
	//    This is separate from the anomaly rows because a service can show errors
	//    before any statistical detector is confident.
	errOnset := map[string]time.Time{}
	errIDs := map[string][]string{}
	for _, l := range logs {
		if !l.Level.IsError() {
			continue
		}
		cur, ok := errOnset[l.Service]
		if !ok || l.TS.Before(cur) {
			errOnset[l.Service] = l.TS
		}
		errIDs[l.Service] = append(errIDs[l.Service], l.ID)
	}
	errServices := make([]string, 0, len(errOnset))
	for svc := range errOnset {
		errServices = append(errServices, svc)
	}
	sort.Strings(errServices)
	for _, svc := range errServices {
		events = append(events, model.TimelineEvent{
			At:          errOnset[svc],
			Kind:        KindCascade,
			Service:     svc,
			Detail:      "first error",
			EvidenceIDs: sortedIDs(errIDs[svc]),
		})
	}

	sortEvents(events)
	return events
}

// sortEvents imposes the documented total order.
func sortEvents(events []model.TimelineEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.Detail < b.Detail
	})
}

func evidenceOf(a model.Anomaly) []string {
	out := make([]string, 0, len(a.LogIDs)+len(a.SpanIDs))
	out = append(out, a.LogIDs...)
	out = append(out, a.SpanIDs...)
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20] // a report row does not need every id
	}
	return out
}

func sortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
