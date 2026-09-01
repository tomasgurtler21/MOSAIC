package pathutil

import (
	"strings"
	"unicode"
)

// NormalizeInput applies the standard normalization rules to a raw text value
// entered in a TUI text-entry screen:
//
//  1. Trim surrounding whitespace (including trailing newlines from paste).
//  2. If the result is at least two characters long and begins and ends with
//     a double-quote character ("), remove that one matched pair.
//  3. Filter out control characters: strip any rune r where unicode.IsControl(r)
//     is true, preserving all printable characters (letters, digits, path
//     separators, spaces, punctuation, interior quotes).
//
// Single-quote stripping is deliberately omitted to align with the existing
// Deployment pathinput.Unquote and Runner normalizePath conventions
// (double-quote-only).
//
// The function is safe for both filesystem paths and general text values
// (e.g. version-filter strings). It never returns an error -- invalid input
// is cleaned, not rejected.
func NormalizeInput(raw string) string {
	// Step 1: trim surrounding whitespace.
	s := strings.TrimSpace(raw)

	// Step 2: strip one matched pair of surrounding double-quote characters.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	// Step 3: filter out control characters, preserving all printable runes.
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
