// Package templates groups log messages that differ only in their variable
// parts, so that "timeout after 3000ms" and "timeout after 812ms" count as the
// same event instead of two rare ones.
//
// Determinism is the whole point: the same message must always produce the same
// template, on every platform and every run. Every rule is a fixed regexp
// applied in a fixed order, with no maps, no sorting of user data and no
// locale-dependent behaviour.
package templates

import (
	"regexp"
	"strings"
)

// Token names the masked class. They are stable strings because they appear in
// reports and in golden files.
const (
	TokUUID     = "<uuid>"
	TokIP       = "<ip>"
	TokHex      = "<hex>"
	TokDuration = "<duration>"
	TokNumber   = "<num>"
	TokQuoted   = "<str>"
	TokEmail    = "<email>"
)

// rule is one masking pass. Order matters and is fixed by the slice below.
type rule struct {
	re    *regexp.Regexp
	token string
}

// rules are applied in this exact order. Longest and most specific patterns
// come first so that, for example, a UUID is not partially eaten by the number
// rule and an IP is not eaten by the number rule before it can be recognised.
//
// Go's regexp is leftmost-longest for alternations but these are independent
// passes, so the ordering here is what protects the more specific tokens.
var rules = []rule{
	// Quoted strings first: an error message that embeds user data should
	// collapse even when the embedded value is not a number.
	{regexp.MustCompile(`"(?:[^"\\]|\\.)*"`), TokQuoted},
	{regexp.MustCompile(`'(?:[^'\\]|\\.)*'`), TokQuoted},

	// Identifiers before numbers, or the hex digits inside them get eaten.
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), TokUUID},
	{regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`), TokHex},
	{regexp.MustCompile(`(?i)\b0x[0-9a-f]+\b`), TokHex},

	{regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`), TokIP},
	{regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.-]+\b`), TokEmail},

	// Durations before bare numbers so the unit is preserved and the value is
	// not replaced by <num> with the unit stranded after it.
	{regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s*(?:ns|us|µs|ms|s|m|h)\b`), TokDuration},

	// Catch-all for remaining numbers, including decimals and integers.
	{regexp.MustCompile(`\b\d+(?:\.\d+)?\b`), TokNumber},
}

// Template returns the masked form of msg.
//
// Consecutive whitespace is collapsed to a single space so that formatting
// differences do not create distinct templates. Leading and trailing space is
// trimmed. The result is a pure function of msg.
func Template(msg string) string {
	s := msg
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.token)
	}
	return strings.Join(strings.Fields(s), " ")
}

// Key is the identity of a template: the masked text, lowercased.
//
// Lowercasing groups "Timeout after <num>ms" with "timeout after <num>ms", which
// is almost always the same event produced by two code paths. The original
// casing is preserved in the TemplateText of the first occurrence.
func Key(msg string) string {
	return strings.ToLower(Template(msg))
}
