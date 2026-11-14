// Package traceline reconstructs what happened during an outage: a timeline, a
// dependency graph, and a ranked list of probable root causes with evidence.
//
// Determinism is a product requirement, not a preference. Go randomizes map
// iteration order by design, so every map that can reach an output MUST be
// iterated through an explicitly sorted key slice. See docs/DECISIONS.md D-001
// and the rules in docs/spec/constitution.md.
package traceline

// Version is the released version, overridable at build time:
//
//	go build -ldflags "-X github.com/RocketifyLabs/traceline.Version=1.2.3"
var Version = "0.1.0"
