package timeline

import (
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

func t0() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func TestBuildOrdersDeployBeforeSymptoms(t *testing.T) {
	logs := []model.LogLine{
		{ID: "d1", TS: t0(), Service: "api", Level: model.LevelInfo, Msg: "deployed",
			Fields: map[string]any{"event": "deploy", "version": "v42"}},
		{ID: "e1", TS: t0().Add(time.Minute), Service: "api", Level: model.LevelError, Msg: "boom"},
	}
	anomalies := []model.Anomaly{
		{Service: "api", Kind: "error_rate", Onset: t0().Add(time.Minute), Strength: 5, LogIDs: []string{"e1"}},
	}
	got := Build(anomalies, logs, t0(), t0().Add(5*time.Minute))

	if len(got) < 2 {
		t.Fatalf("got %d events, want at least 2: %+v", len(got), got)
	}
	if got[0].Kind != KindDeploy {
		t.Fatalf("first event = %s, want %s (deploy precedes symptoms)", got[0].Kind, KindDeploy)
	}
	if got[0].Detail != "v42" {
		t.Errorf("deploy detail = %q, want v42", got[0].Detail)
	}
	// The deploy must come before the anomaly in time.
	if got[0].At.After(got[len(got)-1].At) {
		t.Error("events not in chronological order")
	}
}

func TestBuildIncludesConfigChange(t *testing.T) {
	logs := []model.LogLine{
		{ID: "c1", TS: t0(), Service: "db", Level: model.LevelInfo, Msg: "changed",
			Fields: map[string]any{"event": "config_change", "config": "pool_size"}},
	}
	got := Build(nil, logs, t0(), t0().Add(time.Minute))
	var found bool
	for _, e := range got {
		if e.Kind == KindConfigChange && e.Detail == "pool_size" {
			found = true
		}
	}
	if !found {
		t.Fatalf("config_change event missing: %+v", got)
	}
}

func TestBuildOneFirstAnomalyPerService(t *testing.T) {
	anomalies := []model.Anomaly{
		{Service: "api", Kind: "error_rate", Onset: t0().Add(30 * time.Second), Strength: 5},
		{Service: "api", Kind: "latency_p95", Onset: t0().Add(10 * time.Second), Strength: 4},
		{Service: "api", Kind: "template_burst", Onset: t0().Add(20 * time.Second), Strength: 6},
	}
	got := Build(anomalies, nil, t0(), t0().Add(time.Minute))
	var count int
	for _, e := range got {
		if e.Kind == KindFirstAnomaly {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d first_anomaly rows for one service, want 1", count)
	}
	// The earliest onset wins.
	for _, e := range got {
		if e.Kind == KindFirstAnomaly && !e.At.Equal(t0().Add(10*time.Second)) {
			t.Errorf("first_anomaly at %v, want the earliest onset 10s", e.At)
		}
	}
}

func TestBuildCascadeShowsPropagationOrder(t *testing.T) {
	logs := []model.LogLine{
		{ID: "e1", TS: t0(), Service: "db", Level: model.LevelError, Msg: "x"},
		{ID: "e2", TS: t0().Add(10 * time.Second), Service: "api", Level: model.LevelError, Msg: "y"},
		{ID: "e3", TS: t0().Add(20 * time.Second), Service: "web", Level: model.LevelError, Msg: "z"},
	}
	got := Build(nil, logs, t0(), t0().Add(time.Minute))
	var order []string
	for _, e := range got {
		if e.Kind == KindCascade {
			order = append(order, e.Service)
		}
	}
	if len(order) != 3 {
		t.Fatalf("cascade rows = %v, want 3 services", order)
	}
	if order[0] != "db" || order[1] != "api" || order[2] != "web" {
		t.Fatalf("cascade order = %v, want db, api, web", order)
	}
}

func TestBuildEmptyInput(t *testing.T) {
	got := Build(nil, nil, t0(), t0().Add(time.Minute))
	if len(got) != 0 {
		t.Fatalf("empty input produced events: %+v", got)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	logs := []model.LogLine{
		{ID: "a", TS: t0(), Service: "z", Level: model.LevelError, Msg: "m"},
		{ID: "b", TS: t0(), Service: "a", Level: model.LevelError, Msg: "m"},
	}
	anomalies := []model.Anomaly{
		{Service: "z", Kind: "error_rate", Onset: t0(), Strength: 5},
		{Service: "a", Kind: "error_rate", Onset: t0(), Strength: 5},
	}
	first := Build(anomalies, logs, t0(), t0().Add(time.Minute))
	for i := 0; i < 20; i++ {
		again := Build(anomalies, logs, t0(), t0().Add(time.Minute))
		if len(again) != len(first) {
			t.Fatalf("length changed between runs")
		}
		for j := range first {
			if first[j].Service != again[j].Service || first[j].Kind != again[j].Kind {
				t.Fatalf("run differs at %d: %+v vs %+v", j, first[j], again[j])
			}
		}
	}
}

func TestBuildEventsStrictlyOrdered(t *testing.T) {
	var logs []model.LogLine
	for i := 0; i < 30; i++ {
		logs = append(logs, model.LogLine{
			ID: "id" + itoa(i), TS: t0().Add(time.Duration(i) * time.Second),
			Service: "s" + itoa(i%3), Level: model.LevelError, Msg: "m",
		})
	}
	got := Build(nil, logs, t0(), t0().Add(time.Minute))
	for i := 1; i < len(got); i++ {
		if got[i].At.Before(got[i-1].At) {
			t.Fatalf("event %d at %v precedes event %d at %v", i, got[i].At, i-1, got[i-1].At)
		}
	}
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
