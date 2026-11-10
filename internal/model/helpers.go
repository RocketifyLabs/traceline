package model

import (
	"strconv"
	"strings"
)

// normalize lowercases and trims, so " ERROR " and "error" compare equal.
// Level matching must not depend on how a producer spelled its severity.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// trimFloat renders a float without a trailing ".0" and without exponent
// notation, so the same number always formats identically across platforms.
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if s == "" {
		return "0"
	}
	return s
}

// fieldString extracts a scalar field as a string. Non-scalar values return "",
// deliberately: formatting a nested object with Go's default map printing would
// put unstable key order into an output.
func fieldString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return trimFloat(t)
	case nil:
		return ""
	}
	return ""
}
