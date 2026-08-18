package tui_test

// scaffold_test.go verifies the scaffold helpers in the mosaic-common/tui
// package. The tests below cover the Wrap helper — a RED-phase specification
// that fails while Wrap is a stub, and that the implementation task must satisfy.
//
// Covered:
//   - Text longer than the width comes back as multiple lines each fitting the width.
//   - Every rune from the input is present in the output (no text lost).
//   - An unbroken token wider than the limit is hard-broken rather than left overflowing.
//   - A non-positive width is handled without panicking.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tuicommon "mosaic-common/tui"
)

// ---------------------------------------------------------------------------
// Wrap: multiple lines, each fitting the width
// ---------------------------------------------------------------------------

// TestWrap_LongerThanWidth_ProducesMultipleFittingLines verifies that text
// wider than the given width is wrapped into multiple lines and that every
// output line is at most maxLen runes wide.
func TestWrap_LongerThanWidth_ProducesMultipleFittingLines(t *testing.T) {
	const width = 10
	input := "one two three four five six"
	result := tuicommon.Wrap(input, width)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatalf("Wrap(%q, %d) returned %q — only %d line(s); expected multiple lines for text wider than %d",
			input, width, result, len(lines), width)
	}
	for i, line := range lines {
		if w := utf8.RuneCountInString(line); w > width {
			t.Errorf("line %d is %d runes wide, want <= %d: %q", i, w, width, line)
		}
	}
}

// ---------------------------------------------------------------------------
// Wrap: no text lost
// ---------------------------------------------------------------------------

// TestWrap_NoTextLost verifies that every word from the input appears in the
// output and that the words appear in the same order. Word-wrapping must not
// drop, reorder, or duplicate any content.
func TestWrap_NoTextLost(t *testing.T) {
	input := "the quick brown fox jumps over the lazy dog"
	result := tuicommon.Wrap(input, 15)
	// Normalise both sides to a single-space-separated word sequence for
	// comparison, stripping the newlines that wrapping introduces.
	got := strings.Join(strings.Fields(result), " ")
	want := strings.Join(strings.Fields(input), " ")
	if got != want {
		t.Errorf("Wrap(%q, 15) produced %q; all words must be preserved in the same order without duplication or omission",
			input, result)
	}
}

// ---------------------------------------------------------------------------
// Wrap: long unbroken token hard-broken at the width
// ---------------------------------------------------------------------------

// TestWrap_LongUnbrokenToken_IsHardBroken verifies that a single token wider
// than the limit (such as a long file path with no spaces or separators) is
// split at exactly the width limit rather than left overflowing.
func TestWrap_LongUnbrokenToken_IsHardBroken(t *testing.T) {
	const width = 10
	// A path-like string with no spaces — cannot be word-wrapped, must be hard-broken.
	longToken := "C:\\Users\\tgurtler21\\AppData\\Local\\Temp\\VeryLongFileName"
	result := tuicommon.Wrap(longToken, width)

	for i, line := range strings.Split(result, "\n") {
		if w := utf8.RuneCountInString(line); w > width {
			t.Errorf("line %d is %d runes wide, want <= %d (long unbroken token must be hard-broken at the limit): %q",
				i, w, width, line)
		}
	}
	// Reassemble by removing the inserted newlines; the original token must be
	// preserved exactly — no characters added or dropped by the hard-break.
	flat := strings.ReplaceAll(result, "\n", "")
	if flat != longToken {
		t.Errorf("Wrap(%q, %d): reassembled hard-broken output %q does not match the original; no characters must be added or dropped",
			longToken, width, flat)
	}
}

// ---------------------------------------------------------------------------
// Wrap: non-positive width is safe (no panic)
// ---------------------------------------------------------------------------

// TestWrap_NonPositiveWidth_NoPanic verifies that calling Wrap with width <= 0
// does not panic and returns a usable string. This is a safety contract for
// callers that compute their width from layout arithmetic that may produce zero.
func TestWrap_NonPositiveWidth_NoPanic(t *testing.T) {
	for _, w := range []int{0, -1, -100} {
		t.Run(fmt.Sprintf("width=%d", w), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Wrap(%q, %d) panicked: %v", "hello world", w, r)
				}
			}()
			got := tuicommon.Wrap("hello world", w)
			// Result must be a non-nil string — empty is fine, as is the
			// original; what is tested is only that no panic occurs.
			_ = got
		})
	}
}
