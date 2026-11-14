package config

import "bytes"

// newTrimReader returns a reader that tolerates a leading UTF-8 BOM.
//
// A weights file edited on Windows with Notepad starts with EF BB BF, and
// encoding/json rejects that with `invalid character 'ï'`. The error names a
// character the user cannot see, which sends them looking for a typo that is not
// there — so the BOM is stripped before decoding instead.
func newTrimReader(b []byte) *bytes.Reader {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	return bytes.NewReader(b)
}
