// Package config loads the analysis tuning knobs from one JSON file.
//
// The four config structs (windows, anomaly, rca, confidence) each carried a
// comment saying their defaults "match rca_weights.json" while no such file
// existed and no code read one. This package makes that promise true, and makes
// a typo in the file a loud error instead of a silently ignored setting —
// silently ignoring a tuning file is worse than having none, because the user
// believes the numbers they see came from their own weights.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/RocketifyLabs/traceline/internal/anomaly"
	"github.com/RocketifyLabs/traceline/internal/rca"
	"github.com/RocketifyLabs/traceline/internal/windows"
)

// Settings is the parsed file. Empty sections fall back to the built-in
// defaults, so a file may override only what it cares about.
type Settings struct {
	Windows    windows.Config `json:"windows"`
	Anomaly    anomaly.Config `json:"anomaly"`
	Weights    rca.Weights    `json:"weights"`
	Confidence rca.Confidence `json:"confidence"`
}

// file is the on-disk shape. It exists separately from Settings so durations can
// be written as human strings ("20m", "10s") instead of nanosecond counts, and so
// a missing section is distinguishable from a zero section.
// comment accepts a "_comment" key anywhere in the file. Documentation inside the
// JSON is worth having — the file is meant to be copied and edited — and without
// this field DisallowUnknownFields would reject the very file the project ships.
//
// It is `any` and not json.RawMessage on purpose: RawMessage is a []byte, so the
// decoder would try base64-decoding the string and fail with an error about
// "illegal base64 data" that says nothing about the real problem.
type comment any

type file struct {
	Comment    comment         `json:"_comment"`
	Windows    *windowsFile    `json:"windows"`
	Anomaly    *anomalyFile    `json:"anomaly"`
	Weights    *weightsFile    `json:"weights"`
	Confidence *confidenceFile `json:"confidence"`
}

// weightsFile and confidenceFile mirror the rca structs plus the optional
// "_comment" key. They are separate types rather than fields on the rca structs
// because the tuning file's documentation habit should not leak into the domain
// types used by the analysis.
type weightsFile struct {
	Comment            comment `json:"_comment"`
	Onset              float64 `json:"onset"`
	Strength           float64 `json:"strength"`
	ExplainsDownstream float64 `json:"explains_downstream"`
	NotExplained       float64 `json:"not_explained"`
}

type confidenceFile struct {
	Comment    comment `json:"_comment"`
	HighScore  float64 `json:"high_score"`
	HighMargin float64 `json:"high_margin"`
	Medium     float64 `json:"medium"`
}

type windowsFile struct {
	Comment         comment `json:"_comment"`
	BaselineCap     string  `json:"baseline_cap"`
	BinSize         string  `json:"bin_size"`
	MADK            float64 `json:"mad_k"`
	ConsecutiveBins int     `json:"consecutive_bins"`
}

type anomalyFile struct {
	Comment           comment `json:"_comment"`
	ZThreshold        float64 `json:"z_threshold"`
	MinEvents         int     `json:"min_events"`
	MinErrorRateDelta float64 `json:"min_error_rate_delta"`
	MinLatencyRatio   float64 `json:"min_latency_ratio"`
}

// Default returns the built-in settings. A run with no --weights is exactly a
// run with this file, which is the property that makes the flag trustworthy.
func Default() Settings {
	return Settings{
		Windows:    windows.DefaultConfig(),
		Anomaly:    anomaly.DefaultConfig(),
		Weights:    rca.DefaultWeights(),
		Confidence: rca.DefaultConfidence(),
	}
}

// Load reads path and overlays it on the defaults.
//
// Unknown fields are an error: a misspelled key must not leave a threshold at
// its default while the operator believes they set it. Missing fields keep the
// default, so a file can be a partial override.
func Load(path string) (Settings, error) {
	s := Default()
	if path == "" {
		return s, nil
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("read weights: %w", err)
	}

	var f file
	dec := json.NewDecoder(newTrimReader(blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}

	if f.Windows != nil {
		if f.Windows.BaselineCap != "" {
			d, err := time.ParseDuration(f.Windows.BaselineCap)
			if err != nil {
				return s, fmt.Errorf("%s: windows.baseline_cap: %w", path, err)
			}
			s.Windows.BaselineCap = d
		}
		if f.Windows.BinSize != "" {
			d, err := time.ParseDuration(f.Windows.BinSize)
			if err != nil {
				return s, fmt.Errorf("%s: windows.bin_size: %w", path, err)
			}
			s.Windows.BinSize = d
		}
		if f.Windows.MADK != 0 {
			s.Windows.MADK = f.Windows.MADK
		}
		if f.Windows.ConsecutiveBins != 0 {
			s.Windows.ConsecutiveBins = f.Windows.ConsecutiveBins
		}
	}

	if f.Anomaly != nil {
		// Compared against zero rather than "set", because every threshold here
		// is meaningful only above zero: a zero in the file is far more likely a
		// mistake than an intent to disable a detector.
		if f.Anomaly.ZThreshold != 0 {
			s.Anomaly.ZThreshold = f.Anomaly.ZThreshold
		}
		if f.Anomaly.MinEvents != 0 {
			s.Anomaly.MinEvents = f.Anomaly.MinEvents
		}
		if f.Anomaly.MinErrorRateDelta != 0 {
			s.Anomaly.MinErrorRateDelta = f.Anomaly.MinErrorRateDelta
		}
		if f.Anomaly.MinLatencyRatio != 0 {
			s.Anomaly.MinLatencyRatio = f.Anomaly.MinLatencyRatio
		}
	}

	if f.Weights != nil {
		s.Weights = rca.Weights{
			Onset:              f.Weights.Onset,
			Strength:           f.Weights.Strength,
			ExplainsDownstream: f.Weights.ExplainsDownstream,
			NotExplained:       f.Weights.NotExplained,
		}
	}
	if f.Confidence != nil {
		s.Confidence = rca.Confidence{
			HighScore:  f.Confidence.HighScore,
			HighMargin: f.Confidence.HighMargin,
			Medium:     f.Confidence.Medium,
		}
	}

	if err := s.Validate(); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Validate rejects settings that would make the analysis meaningless.
//
// Each check exists because the failure it prevents is silent: a zero bin size
// divides by zero, a non-summing weight set shifts every score, and a confidence
// threshold above 1 can never be reached so everything reads as low confidence.
func (s Settings) Validate() error {
	if s.Windows.BinSize <= 0 {
		return fmt.Errorf("windows.bin_size must be > 0, got %s", s.Windows.BinSize)
	}
	if s.Windows.BaselineCap <= 0 {
		return fmt.Errorf("windows.baseline_cap must be > 0, got %s", s.Windows.BaselineCap)
	}
	if s.Windows.ConsecutiveBins < 1 {
		return fmt.Errorf("windows.consecutive_bins must be >= 1, got %d", s.Windows.ConsecutiveBins)
	}
	if s.Windows.MADK <= 0 {
		return fmt.Errorf("windows.mad_k must be > 0, got %v", s.Windows.MADK)
	}
	if s.Anomaly.ZThreshold <= 0 {
		return fmt.Errorf("anomaly.z_threshold must be > 0, got %v", s.Anomaly.ZThreshold)
	}
	if s.Anomaly.MinEvents < 1 {
		return fmt.Errorf("anomaly.min_events must be >= 1, got %d", s.Anomaly.MinEvents)
	}
	if s.Anomaly.MinErrorRateDelta < 0 || s.Anomaly.MinErrorRateDelta > 1 {
		return fmt.Errorf("anomaly.min_error_rate_delta must be within [0,1], got %v", s.Anomaly.MinErrorRateDelta)
	}
	if s.Anomaly.MinLatencyRatio < 1 {
		return fmt.Errorf("anomaly.min_latency_ratio must be >= 1 (a ratio below 1 would fire on improvement), got %v", s.Anomaly.MinLatencyRatio)
	}

	sum := s.Weights.Onset + s.Weights.Strength + s.Weights.ExplainsDownstream + s.Weights.NotExplained
	// A tolerance, not equality: a hand-edited 0.35/0.30/0.25/0.10 sums to
	// 1.0000000000000002 in float64, and rejecting that would be pedantry.
	if sum < 0.99 || sum > 1.01 {
		return fmt.Errorf("weights must sum to 1.0 (±0.01), got %.4f", sum)
	}

	if s.Confidence.HighScore <= s.Confidence.Medium {
		return fmt.Errorf("confidence.high_score (%v) must be above confidence.medium (%v)",
			s.Confidence.HighScore, s.Confidence.Medium)
	}
	if s.Confidence.HighScore > 1 || s.Confidence.Medium < 0 {
		return fmt.Errorf("confidence thresholds must be within [0,1], got high_score=%v medium=%v",
			s.Confidence.HighScore, s.Confidence.Medium)
	}
	return nil
}

// Describe renders the effective settings for a log line, so a run can be
// audited against the file it claims to have used.
func (s Settings) Describe() string {
	return fmt.Sprintf(
		"window(bin=%s cap=%s mad_k=%.2f bins=%d) anomaly(z=%.2f min_events=%d err_delta=%.2f lat_ratio=%.2f) "+
			"weights(onset=%.2f strength=%.2f explains=%.2f not_explained=%.2f)",
		s.Windows.BinSize, s.Windows.BaselineCap, s.Windows.MADK, s.Windows.ConsecutiveBins,
		s.Anomaly.ZThreshold, s.Anomaly.MinEvents, s.Anomaly.MinErrorRateDelta, s.Anomaly.MinLatencyRatio,
		s.Weights.Onset, s.Weights.Strength, s.Weights.ExplainsDownstream, s.Weights.NotExplained)
}
