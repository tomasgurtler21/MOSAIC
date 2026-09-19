// Package testrun: shared stderr truncation helper and constants.
package testrun

import "fmt"

const (
	// MaxStderrErrorBytes is the maximum number of stderr bytes included in
	// error text by errWithStderr. Used by errWithStderr in runinvoker.go.
	MaxStderrErrorBytes = 1024

	// MaxStderrDisplayBytes is the maximum number of stderr bytes shown by
	// reporters. Used by CLI and TUI reporters when calling TruncateTail.
	MaxStderrDisplayBytes = 8192
)

// TruncateTail returns the last maxBytes of s, cut on a rune boundary, with a
// truncation marker prepended when truncation occurs. When len(s) <= maxBytes,
// returns s unchanged.
//
// The marker format is:
//
//	[truncated, showing last <actual> of <total> bytes]
//
// followed by a newline. <actual> is the byte count after the rune-boundary
// adjustment, which may be less than maxBytes.
//
// Used by both CLI and TUI reporters for stderr display truncation.
func TruncateTail(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	total := len(s)

	// Take the last maxBytes bytes.
	tail := s[total-maxBytes:]

	// Rune-boundary adjustment: scan forward past any UTF-8 continuation bytes
	// (0x80-0xBF) so the output starts on a valid rune boundary.
	start := 0
	for start < len(tail) {
		b := tail[start]
		if b < 0x80 || b > 0xBF {
			// Valid leading byte (ASCII or multi-byte lead).
			break
		}
		start++
	}

	adjusted := tail[start:]
	actual := len(adjusted)

	if actual == 0 {
		// All bytes in the tail window were continuation bytes (malformed UTF-8).
		return fmt.Sprintf("[truncated, showing last 0 of %d bytes]\n", total)
	}

	return fmt.Sprintf("[truncated, showing last %d of %d bytes]\n%s", actual, total, adjusted)
}
