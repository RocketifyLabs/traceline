package templates

import (
	"math/rand"
	"strings"
	"testing"
)

func TestTemplateTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "connection refused", "connection refused"},
		{"integer", "retry attempt 3 failed", "retry attempt <num> failed"},
		{"decimal", "cpu usage 87.5 percent", "cpu usage <num> percent"},
		{"duration ms", "upstream timeout after 3000ms", "upstream timeout after <duration>"},
		{"duration spaced", "upstream timeout after 3000 ms", "upstream timeout after <duration>"},
		{"duration seconds", "took 1.5 s to respond", "took <duration> to respond"},
		{"uuid", "trace a1b2c3d4-e5f6-7890-abcd-ef1234567890 failed", "trace <uuid> failed"},
		{"ip", "dial tcp 10.0.3.17:5432 refused", "dial tcp <ip>:<num> refused"},
		{"hex", "checksum 0xDEADBEEF mismatch", "checksum <hex> mismatch"},
		{"long hex", "id deadbeefdeadbeef not found", "id <hex> not found"},
		{"email", "notify ops@example.com failed", "notify <email> failed"},
		{"quoted", `user "alice" not permitted`, "user <str> not permitted"},
		{"single quoted", `key 'abc' invalid`, "key <str> invalid"},
		{"whitespace collapse", "too   many    spaces", "too many spaces"},
		{"leading trailing", "  padded  ", "padded"},
		{"empty", "", ""},
		{"only numbers", "404", "<num>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Template(c.in); got != c.want {
				t.Errorf("Template(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// FR-5: similar messages group even when their variable parts differ.
func TestSimilarMessagesCollapseToSameTemplate(t *testing.T) {
	msgs := []string{
		"upstream timeout after 3000ms",
		"upstream timeout after 812ms",
		"upstream timeout after 42ms",
		"upstream timeout after 999999ms",
	}
	want := Template(msgs[0])
	for _, m := range msgs[1:] {
		if got := Template(m); got != want {
			t.Errorf("Template(%q) = %q, want %q (grouping failed)", m, got, want)
		}
	}
}

// FR-5: distinct messages must NOT collapse into one template.
func TestDifferentMessagesStaySeparate(t *testing.T) {
	a := Template("upstream timeout after 3000ms")
	b := Template("connection refused by peer")
	if a == b {
		t.Fatalf("distinct messages collapsed to %q", a)
	}
}

func TestTemplateIsDeterministic(t *testing.T) {
	msgs := []string{
		"req a1b2c3d4-e5f6-7890-abcd-ef1234567890 to 10.0.0.1 took 250ms",
		`auth "bob@example.com" failed at 0x1F`,
		"retry 7 of 10 after 1.5s",
	}
	for _, m := range msgs {
		first := Template(m)
		for i := 0; i < 50; i++ {
			if got := Template(m); got != first {
				t.Fatalf("Template(%q) unstable: %q then %q", m, first, got)
			}
		}
	}
}

// Order independence: templates are computed per message, so processing
// messages in any order yields the same mapping.
func TestTemplateMappingIndependentOfOrder(t *testing.T) {
	msgs := []string{
		"timeout after 100ms",
		"timeout after 200ms",
		"pool 10.0.0.5 exhausted",
		"uuid a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}
	baseline := make(map[string]string, len(msgs))
	for _, m := range msgs {
		baseline[m] = Template(m)
	}

	r := rand.New(rand.NewSource(3))
	for attempt := 0; attempt < 10; attempt++ {
		sh := append([]string(nil), msgs...)
		r.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		for _, m := range sh {
			if got := Template(m); got != baseline[m] {
				t.Fatalf("order changed the template of %q: %q vs %q", m, got, baseline[m])
			}
		}
	}
}

func TestKeyLowercasesAndGroups(t *testing.T) {
	a := Key("Timeout after 3000ms")
	b := Key("timeout after 812ms")
	if a != b {
		t.Fatalf("Key differs by casing/values: %q vs %q", a, b)
	}
	if strings.ToLower(a) != a {
		t.Fatalf("Key is not lowercase: %q", a)
	}
}

// The specific tokens must win over the generic number rule.
func TestTokenPrecedence(t *testing.T) {
	if got := Template("addr 127.0.0.1"); strings.Contains(got, "<num>.<num>") {
		t.Errorf("IP was eaten by the number rule: %q", got)
	}
	if got := Template("at 3000ms"); strings.Contains(got, "<num>") {
		t.Errorf("duration was eaten by the number rule: %q", got)
	}
	if got := Template("id a1b2c3d4-e5f6-7890-abcd-ef1234567890"); strings.Contains(got, "<num>") {
		t.Errorf("UUID was eaten by the number rule: %q", got)
	}
}

func TestTemplatePreservesReadableText(t *testing.T) {
	got := Template("upstream timeout after 3000ms on route /checkout")
	want := "upstream timeout after <duration> on route /checkout"
	if got != want {
		t.Fatalf("Template = %q, want %q", got, want)
	}
}

// A very long message must not panic or blow up quadratically in a way that
// matters. 20 KB of text is well past any realistic log line.
func TestTemplateHandlesLongInput(t *testing.T) {
	long := strings.Repeat("word 12345 ", 2000)
	got := Template(long)
	if got == "" {
		t.Fatal("long input produced an empty template")
	}
	if strings.Contains(got, "12345") {
		t.Error("numbers not masked in long input")
	}
}
