package determinism

import (
	"strings"
	"testing"
)

func TestSortedKeysAscending(t *testing.T) {
	m := map[string]int{"delta": 4, "alpha": 1, "charlie": 3, "bravo": 2}
	got := SortedKeys(m)
	want := []string{"alpha", "bravo", "charlie", "delta"}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSortedKeysEmptyAndNil(t *testing.T) {
	if got := SortedKeys(map[string]int{}); len(got) != 0 {
		t.Fatalf("empty map returned %v", got)
	}
	if got := SortedKeys[string, int](nil); len(got) != 0 {
		t.Fatalf("nil map returned %v", got)
	}
}

// This is the test that proves the hazard is real rather than theoretical.
// It demonstrates that ranging a map directly does NOT give a stable order,
// which is why SortedKeys exists at all.
func TestMapRangeOrderIsNotStable(t *testing.T) {
	m := make(map[string]int)
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		m[k] = 1
	}

	// Go's runtime randomizes the starting point of map iteration. Over enough
	// runs the raw range order must differ at least once; if it never does,
	// this test is not observing the hazard and the guard below is untested.
	seen := make(map[string]bool)
	for run := 0; run < 200; run++ {
		var order strings.Builder
		for k := range m {
			order.WriteString(k)
		}
		seen[order.String()] = true
	}
	if len(seen) < 2 {
		t.Skipf("map iteration appeared stable in 200 runs (%d orderings); "+
			"runtime behaviour changed, revisit the determinism policy", len(seen))
	}

	// SortedKeys must remove the hazard entirely.
	first := strings.Join(SortedKeys(m), "")
	for run := 0; run < 200; run++ {
		if got := strings.Join(SortedKeys(m), ""); got != first {
			t.Fatalf("SortedKeys is not stable: %q then %q", first, got)
		}
	}
	if first != "abcdefgh" {
		t.Fatalf("SortedKeys returned %q, want abcdefgh", first)
	}
}

func TestAssertSorted(t *testing.T) {
	if err := AssertSorted([]string{"a", "b", "c"}); err != nil {
		t.Fatalf("sorted slice rejected: %v", err)
	}
	if err := AssertSorted([]string{"b", "a"}); err == nil {
		t.Fatal("unsorted slice accepted")
	}
	if err := AssertSorted(nil); err != nil {
		t.Fatalf("empty slice rejected: %v", err)
	}
}
