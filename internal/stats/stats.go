// Package stats provides the robust statistics the analysis depends on.
//
// Every function is pure, deterministic and independent of iteration order.
// Median/MAD are computed on a sorted copy so the caller's slice is never
// mutated, which keeps a second run byte-identical to the first.
package stats

import (
	"math"
	"sort"
)

// MADFloor is the smallest MAD the robust z-score will divide by. Without it a
// perfectly constant baseline produces a division by zero and an infinite
// score, which would rank a healthy service first.
const MADFloor = 1e-9

// Median returns the middle value of a sorted copy of xs.
// An even-length slice averages the two central values. Empty input returns 0.
func Median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	c := sortedCopy(xs)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// MAD returns the median absolute deviation from the median.
// A zero result is legitimate (a majority of values equal to the median) and is
// why RobustZ applies MADFloor rather than trusting the divisor.
func MAD(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := Median(xs)
	dev := make([]float64, len(xs))
	for i, x := range xs {
		dev[i] = math.Abs(x - m)
	}
	return Median(dev)
}

// RobustZ returns the modified z-score of x against a baseline:
//
//	0.6745 * (x - median(baseline)) / MAD(baseline)
//
// The 0.6745 factor makes the score comparable to a standard z-score for
// normally distributed data. MADFloor prevents division by zero.
func RobustZ(x float64, baseline []float64) float64 {
	d := MAD(baseline)
	if d < MADFloor {
		d = MADFloor
	}
	return 0.6745 * (x - Median(baseline)) / d
}

// Percentile returns the nearest-rank percentile of a sorted copy of xs.
//
// Nearest-rank (no interpolation) was chosen in docs/spec/spec.md FR-2 because
// two implementations of the same input must agree exactly, and interpolation
// invites floating-point disagreement between languages and platforms.
// p must be in (0, 100]. Empty input returns 0.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 || p <= 0 {
		return 0
	}
	c := sortedCopy(xs)
	rank := int(math.Ceil(p / 100.0 * float64(len(c))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(c) {
		rank = len(c)
	}
	return c[rank-1]
}

func sortedCopy(xs []float64) []float64 {
	c := make([]float64, len(xs))
	copy(c, xs)
	sort.Float64s(c)
	return c
}
