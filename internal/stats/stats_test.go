package stats

import (
	"math"
	"testing"
)

func TestMedianOdd(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("Median([3 1 2]) = %v, want 2", got)
	}
}

func TestMedianEven(t *testing.T) {
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Fatalf("Median([4 1 3 2]) = %v, want 2.5", got)
	}
}

func TestMedianEmpty(t *testing.T) {
	if got := Median(nil); got != 0 {
		t.Fatalf("Median(nil) = %v, want 0", got)
	}
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	in := []float64{3, 1, 2}
	Median(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Fatalf("Median mutated its input: %v", in)
	}
}

func TestMAD(t *testing.T) {
	// Deviations from median 3 are [1,0,1]; their median is 1.
	if got := MAD([]float64{2, 3, 4}); math.Abs(got-1) > 1e-12 {
		t.Fatalf("MAD([2 3 4]) = %v, want 1", got)
	}
}

func TestMADIsLegitimatelyZero(t *testing.T) {
	// Median is 10, so deviations are [0,1,0,2,0] and the median deviation is 0.
	// A zero MAD is correct here; the divisor guard belongs in RobustZ.
	if got := MAD([]float64{10, 11, 10, 12, 10}); got != 0 {
		t.Fatalf("MAD = %v, want 0", got)
	}
}

func TestRobustZConstantBaselineDoesNotDivideByZero(t *testing.T) {
	z := RobustZ(100, []float64{5, 5, 5, 5})
	if math.IsInf(z, 0) || math.IsNaN(z) {
		t.Fatalf("RobustZ = %v, must be finite", z)
	}
	if z <= 0 {
		t.Fatalf("RobustZ = %v, a upward spike must score positive", z)
	}
}

func TestRobustZFlatSeriesIsNotAnomalous(t *testing.T) {
	// The negative case: a series with no real shift must stay below the
	// documented threshold of 3.5.
	if z := RobustZ(10, []float64{10, 11, 10, 12, 10}); z >= 3.5 {
		t.Fatalf("RobustZ = %v on a flat series, want < 3.5", z)
	}
}

func TestRobustZSymmetric(t *testing.T) {
	up := RobustZ(20, []float64{10, 11, 12, 13, 14})
	down := RobustZ(4, []float64{10, 11, 12, 13, 14})
	if math.Abs(up+down) > 1e-9 {
		t.Fatalf("RobustZ is not symmetric: up=%v down=%v", up, down)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	cases := []struct {
		p    float64
		want float64
	}{
		{50, 5},
		{95, 10},
		{100, 10},
		{10, 1},
	}
	for _, c := range cases {
		if got := Percentile(xs, c.p); got != c.want {
			t.Errorf("Percentile(p=%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestPercentileSingleValue(t *testing.T) {
	if got := Percentile([]float64{42}, 95); got != 42 {
		t.Fatalf("Percentile of one value = %v, want 42", got)
	}
}

func TestPercentileEmptyAndInvalid(t *testing.T) {
	if got := Percentile(nil, 95); got != 0 {
		t.Fatalf("Percentile(nil) = %v, want 0", got)
	}
	if got := Percentile([]float64{1, 2}, 0); got != 0 {
		t.Fatalf("Percentile(p=0) = %v, want 0", got)
	}
}

func TestPercentileDoesNotMutateInput(t *testing.T) {
	in := []float64{5, 1, 3}
	Percentile(in, 95)
	if in[0] != 5 || in[1] != 1 || in[2] != 3 {
		t.Fatalf("Percentile mutated its input: %v", in)
	}
}
