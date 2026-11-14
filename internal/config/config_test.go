package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rca_weights.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The shipped defaults must pass their own validation, or every run without
// --weights would be broken.
func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default settings are invalid: %v", err)
	}
}

// An empty path means "no file", and must equal the built-in defaults. This is
// the property that makes --weights trustworthy: a run with the flag pointing at
// the documented defaults file must produce the same numbers as a run without it.
func TestEmptyPathReturnsDefaults(t *testing.T) {
	got, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") errored: %v", err)
	}
	want := Default()
	if got != want {
		t.Fatalf("Load(\"\") = %+v, want %+v", got, want)
	}
}

func TestPartialOverrideKeepsOtherDefaults(t *testing.T) {
	p := writeTemp(t, `{"anomaly":{"z_threshold":4.25}}`)
	s, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Anomaly.ZThreshold != 4.25 {
		t.Errorf("z_threshold = %v, want 4.25", s.Anomaly.ZThreshold)
	}
	// Untouched fields keep their defaults.
	if s.Anomaly.MinLatencyRatio != Default().Anomaly.MinLatencyRatio {
		t.Errorf("min_latency_ratio changed to %v", s.Anomaly.MinLatencyRatio)
	}
	if s.Weights != Default().Weights {
		t.Errorf("weights changed to %+v", s.Weights)
	}
}

func TestDurationsParse(t *testing.T) {
	p := writeTemp(t, `{"windows":{"bin_size":"30s","baseline_cap":"1h"}}`)
	s, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Windows.BinSize.Seconds() != 30 {
		t.Errorf("bin_size = %s, want 30s", s.Windows.BinSize)
	}
	if s.Windows.BaselineCap.Minutes() != 60 {
		t.Errorf("baseline_cap = %s, want 1h", s.Windows.BaselineCap)
	}
}

func TestBadDurationIsAnError(t *testing.T) {
	p := writeTemp(t, `{"windows":{"bin_size":"30 seconds"}}`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected an error for an unparseable duration")
	} else if !strings.Contains(err.Error(), "bin_size") {
		t.Errorf("error should name the field, got: %v", err)
	}
}

// A misspelled key must not be ignored: the user believes they set a threshold
// and gets the default instead, with no indication anything was wrong.
func TestUnknownFieldIsAnError(t *testing.T) {
	p := writeTemp(t, `{"anomaly":{"z_treshold":4.0}}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("a misspelled key was accepted")
	}
	if !strings.Contains(err.Error(), "z_treshold") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}

func TestWeightsMustSumToOne(t *testing.T) {
	p := writeTemp(t, `{"weights":{"onset":0.5,"strength":0.5,"explains_downstream":0.5,"not_explained":0.5}}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("weights summing to 2.0 were accepted")
	}
	if !strings.Contains(err.Error(), "sum to 1") {
		t.Errorf("error should explain the sum, got: %v", err)
	}
}

// Float64 addition makes 0.35+0.30+0.25+0.10 slightly over 1.0; the documented
// defaults must not be rejected by their own validator.
func TestDocumentedWeightsPassFloatTolerance(t *testing.T) {
	p := writeTemp(t, `{"weights":{"onset":0.35,"strength":0.30,"explains_downstream":0.25,"not_explained":0.10}}`)
	if _, err := Load(p); err != nil {
		t.Fatalf("documented weights rejected: %v", err)
	}
}

func TestZeroBinSizeRejected(t *testing.T) {
	p := writeTemp(t, `{"windows":{"bin_size":"0s"}}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("bin_size 0 was accepted")
	}
	if !strings.Contains(err.Error(), "bin_size") {
		t.Errorf("error should name bin_size, got: %v", err)
	}
}

func TestLatencyRatioBelowOneRejected(t *testing.T) {
	p := writeTemp(t, `{"anomaly":{"min_latency_ratio":0.5}}`)
	if _, err := Load(p); err == nil {
		t.Fatal("min_latency_ratio 0.5 was accepted; it would fire on improvement")
	}
}

func TestConfidenceOrderingEnforced(t *testing.T) {
	p := writeTemp(t, `{"confidence":{"high_score":0.3,"high_margin":0.1,"medium":0.8}}`)
	if _, err := Load(p); err == nil {
		t.Fatal("medium above high_score was accepted")
	}
}

// Notepad on Windows writes a BOM; the error it produces otherwise names an
// invisible character and sends the user hunting for a typo that is not there.
func TestBOMIsTolerated(t *testing.T) {
	p := writeTemp(t, "\xEF\xBB\xBF"+`{"anomaly":{"z_threshold":3.75}}`)
	s, err := Load(p)
	if err != nil {
		t.Fatalf("a BOM before the JSON was rejected: %v", err)
	}
	if s.Anomaly.ZThreshold != 3.75 {
		t.Errorf("z_threshold = %v, want 3.75", s.Anomaly.ZThreshold)
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("a missing file was accepted")
	}
}
