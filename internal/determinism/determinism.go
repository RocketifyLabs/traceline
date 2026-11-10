// Package determinism enforces the product's hardest requirement: the same
// input must produce byte-identical output, on every run and every machine.
//
// Go randomizes map iteration order by design, so a program that ranges over a
// map can emit a different order on every execution. That is the single most
// likely source of non-deterministic output in this codebase, and it cannot be
// caught by reading code carefully.
//
// The policy: any map that can reach an output is iterated through
// SortedKeys. This package makes that policy testable.
package determinism

import (
	"fmt"
	"sort"
)

// SortedKeys returns the keys of m in ascending order.
//
// Use it for every range over a map whose iteration order can reach any
// output: reports, JSON, HTML, PDF, logs, benchmark tables. Ranging over the
// map directly is a bug even when it "looks fine" on the current machine.
//
//	keys := determinism.SortedKeys(counts)
//	for _, k := range keys {
//	    use(k, counts[k])
//	}
func SortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// AssertSorted panics if keys is not in ascending order.
//
// It is a development-time guard for code paths where a slice of keys is built
// by hand. A panic in the product would be unacceptable, so callers only use
// this in tests and in debug assertions.
func AssertSorted(keys []string) error {
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			return fmt.Errorf("keys not sorted at index %d: %q > %q", i, keys[i-1], keys[i])
		}
	}
	return nil
}
