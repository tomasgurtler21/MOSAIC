// Tests for the exported TruncateTail function in truncate.go.
// Package testrun_test (blackbox) is sufficient since TruncateTail is exported
// and the constants MaxStderrDisplayBytes / MaxStderrErrorBytes are exported.
package testrun_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

// TestTruncateTail_UnderLimit_ReturnedUnchanged verifies that when the input
// length is at or below maxBytes, the string is returned exactly unchanged with
// no truncation marker.
func TestTruncateTail_UnderLimit_ReturnedUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		maxBytes int
	}{
		{"exactly at limit", "ABCD", 4},
		{"under limit", "hello", 100},
		{"single byte at limit", "X", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := testrun.TruncateTail(tc.input, tc.maxBytes)
			if got != tc.input {
				t.Errorf("TruncateTail(%q, %d) = %q, want input unchanged", tc.input, tc.maxBytes, got)
			}
			if strings.Contains(got, "[truncated") {
				t.Errorf("TruncateTail(%q, %d) contains truncation marker; want no marker for under-limit input",
					tc.input, tc.maxBytes, )
			}
		})
	}
}

// TestTruncateTail_EmptyInput_ReturnedUnchanged verifies that empty input is
// returned unchanged regardless of maxBytes.
func TestTruncateTail_EmptyInput_ReturnedUnchanged(t *testing.T) {
	got := testrun.TruncateTail("", 10)
	if got != "" {
		t.Errorf("TruncateTail(%q, 10) = %q, want %q", "", got, "")
	}
}

// TestTruncateTail_NonPositiveMaxBytes_ReturnedUnchanged verifies that when
// maxBytes is zero or negative, the input is returned unchanged with no marker.
func TestTruncateTail_NonPositiveMaxBytes_ReturnedUnchanged(t *testing.T) {
	input := "some content"
	for _, maxBytes := range []int{0, -1, -100} {
		got := testrun.TruncateTail(input, maxBytes)
		if got != input {
			t.Errorf("TruncateTail(%q, %d) = %q, want input unchanged for non-positive maxBytes",
				input, maxBytes, got)
		}
	}
}

// TestTruncateTail_OverLimit_AllASCII_MarkerAndTailPresent verifies the basic
// over-limit case with all-ASCII input: the result has the truncation marker
// followed by the last maxBytes bytes of the input.
func TestTruncateTail_OverLimit_AllASCII_MarkerAndTailPresent(t *testing.T) {
	// "ABCDEFGHIJ" (10 bytes), maxBytes = 4 -> tail "GHIJ", marker shows 4 of 10.
	input := "ABCDEFGHIJ"
	maxBytes := 4
	got := testrun.TruncateTail(input, maxBytes)

	wantMarker := "[truncated, showing last 4 of 10 bytes]"
	if !strings.Contains(got, wantMarker) {
		t.Errorf("TruncateTail(%q, %d) = %q, want it to contain marker %q",
			input, maxBytes, got, wantMarker)
	}
	// The marker must be followed by a newline and then the tail.
	wantSuffix := "[truncated, showing last 4 of 10 bytes]\nGHIJ"
	if !strings.Contains(got, wantSuffix) {
		t.Errorf("TruncateTail(%q, %d) = %q, want it to contain %q",
			input, maxBytes, got, wantSuffix)
	}
}

// TestTruncateTail_OverLimit_MarkerFormat verifies that the truncation marker
// format is exactly: "[truncated, showing last <actual> of <total> bytes]\n"
// followed immediately by the tail content, with no extra content before the
// marker.
func TestTruncateTail_OverLimit_MarkerFormat(t *testing.T) {
	// 10-byte ASCII input, maxBytes = 4.
	input := "ABCDEFGHIJ"
	maxBytes := 4
	got := testrun.TruncateTail(input, maxBytes)

	wantPrefix := "[truncated, showing last 4 of 10 bytes]\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("TruncateTail(%q, %d) = %q, want it to start with %q\n(marker must be the very first content)",
			input, maxBytes, got, wantPrefix)
	}
}

// TestTruncateTail_OverLimit_MultiByteRuneAtCutPoint verifies that when a
// multi-byte UTF-8 rune straddles the cut point, the rune-boundary adjustment
// drops the continuation byte(s) so the result starts on a valid UTF-8
// boundary, and actual < maxBytes.
//
// Input: "Hello " + "\xc3\xa9" + "world" (13 bytes), maxBytes = 6.
// The last 6 bytes start at index 7: "\xa9world". \xa9 is a continuation byte.
// Adjusted start advances to 'w' (index 1 of the 6-byte slice), giving "world"
// (5 bytes). actual = 5, total = 13.
func TestTruncateTail_OverLimit_MultiByteRuneAtCutPoint(t *testing.T) {
	// \xc3\xa9 is the UTF-8 encoding of U+00E9 (e with acute).
	input := "Hello " + "\xc3\xa9" + "world" // 6 + 2 + 5 = 13 bytes
	maxBytes := 6
	got := testrun.TruncateTail(input, maxBytes)

	// After rune-boundary adjustment, actual = 5 (the continuation byte is dropped).
	wantMarker := "[truncated, showing last 5 of 13 bytes]"
	if !strings.Contains(got, wantMarker) {
		t.Errorf("TruncateTail with multi-byte rune at cut: got %q, want marker %q\n(continuation byte must be dropped to preserve rune boundary)",
			got, wantMarker)
	}
	if !strings.HasSuffix(got, "world") {
		t.Errorf("TruncateTail with multi-byte rune at cut: got %q, want suffix %q", got, "world")
	}
}

// TestTruncateTail_OverLimit_TrailingNewlinePreserved verifies that trailing
// newlines in the tail slice are NOT trimmed by TruncateTail (unlike errWithStderr,
// which strips the trailing newline). The full tail including its newlines is
// returned after the marker.
func TestTruncateTail_OverLimit_TrailingNewlinePreserved(t *testing.T) {
	// 12 bytes ending in newline, maxBytes = 5.
	input := "ABCDEFGHIJ\n\n" // 12 bytes
	maxBytes := 5
	got := testrun.TruncateTail(input, maxBytes)

	// The result must contain the truncation marker -- without this, the test
	// would trivially pass on a stub that returns input unchanged.
	if !strings.Contains(got, "[truncated") {
		t.Errorf("TruncateTail(%q, %d) = %q, want it to contain truncation marker %q\n(stub that returns input unchanged must not satisfy this test)",
			input, maxBytes, got, "[truncated")
	}

	// The tail is the last 5 bytes: "HIJ\n\n".
	wantSuffix := "HIJ\n\n"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("TruncateTail(%q, %d) = %q, want suffix %q\n(trailing newlines in tail must not be stripped)",
			input, maxBytes, got, wantSuffix)
	}
}

// TestTruncateTail_OverLimit_AllContinuationBytesTail verifies the edge case
// where all bytes in the maxBytes tail window are UTF-8 continuation bytes.
// After advancing past all continuation bytes, actual = 0, and the output is
// the marker line only (no tail content).
func TestTruncateTail_OverLimit_AllContinuationBytesTail(t *testing.T) {
	// 8 ASCII bytes + 3 continuation bytes = 11 bytes total; maxBytes = 3.
	// The last 3 bytes (\x80\x80\x80) are all continuation bytes.
	// After rune-boundary scan advances past all of them, actual = 0.
	input := strings.Repeat("A", 8) + "\x80\x80\x80" // 11 bytes
	maxBytes := 3
	got := testrun.TruncateTail(input, maxBytes)

	wantMarker := "[truncated, showing last 0 of 11 bytes]"
	if !strings.Contains(got, wantMarker) {
		t.Errorf("TruncateTail with all-continuation tail: got %q, want marker %q\n(all continuation bytes: actual must be 0)",
			got, wantMarker)
	}
	// Only the marker line should be present; no tail content follows.
	wantExact := "[truncated, showing last 0 of 11 bytes]\n"
	if got != wantExact {
		t.Errorf("TruncateTail with all-continuation tail: got %q, want exactly %q\n(marker-only output when actual=0)",
			got, wantExact)
	}
}
