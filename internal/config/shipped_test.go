package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// projectFile locates the rca_weights.json that ships in the repository root.
func projectFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine the test file path")
	}
	// internal/config/ → repository root
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "rca_weights.json")
}

// The shipped file must parse. It is documentation as much as configuration: if
// it stops loading, the example users copy is broken.
func TestShippedFileLoads(t *testing.T) {
	s, err := Load(projectFile(t))
	if err != nil {
		t.Fatalf("the repository's own rca_weights.json does not load: %v", err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("the repository's own rca_weights.json is invalid: %v", err)
	}
}

// The load-bearing claim, asserted rather than asserted-in-a-comment: loading the
// shipped file produces exactly the built-in defaults. That is what makes the
// flag honest — a run with --weights rca_weights.json and a run without it must
// agree, or one of the two is lying about what it used.
func TestShippedFileEqualsBuiltInDefaults(t *testing.T) {
	got, err := Load(projectFile(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Default()

	if got.Windows != want.Windows {
		t.Errorf("windows differ:\n got %+v\nwant %+v", got.Windows, want.Windows)
	}
	if got.Anomaly != want.Anomaly {
		t.Errorf("anomaly differs:\n got %+v\nwant %+v", got.Anomaly, want.Anomaly)
	}
	if got.Weights != want.Weights {
		t.Errorf("weights differ:\n got %+v\nwant %+v", got.Weights, want.Weights)
	}
	if got.Confidence != want.Confidence {
		t.Errorf("confidence differs:\n got %+v\nwant %+v", got.Confidence, want.Confidence)
	}
}

// The documentation keys must not be mistaken for settings, and must not be
// rejected either — the file is meant to be read and copied.
func TestCommentKeysAreAccepted(t *testing.T) {
	p := writeTemp(t, `{
	  "_comment": "top level",
	  "windows": {"_comment": "a section"},
	  "anomaly": {"_comment": "another", "z_threshold": 4.0},
	  "weights": {"_comment": "w", "onset": 0.35, "strength": 0.30, "explains_downstream": 0.25, "not_explained": 0.10},
	  "confidence": {"_comment": "c", "high_score": 0.6, "high_margin": 0.15, "medium": 0.4}
	}`)
	if _, err := Load(p); err != nil {
		t.Fatalf("_comment keys were rejected: %v", err)
	}
}
