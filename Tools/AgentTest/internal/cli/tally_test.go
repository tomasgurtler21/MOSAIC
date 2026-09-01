package cli_test

// tally_test.go verifies FormatTally: the pure rendering of the
// running/finished/remaining tally as one line. FormatTally is driven from
// the shared runprogress fold, which is what keeps the CLI and TUI displays
// structurally equivalent for the multi-run tally.
//
// All tests here are RED-phase: the stub FormatTally returns an empty string,
// so every assertion about non-empty output, mentioned counts, and labelled
// fields will fail until the real implementation is provided.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/runprogress"
)

// TestFormatTally_ProducesNonEmptyLine verifies that FormatTally returns a
// non-empty string. A tally line is always present in the output stream —
// omitting it entirely would leave the user with no aggregate view of how
// many runs are in what state.
func TestFormatTally_ProducesNonEmptyLine(t *testing.T) {
	tally := runprogress.Tally{Running: 2, Finished: 3, Remaining: 5}
	got := cli.FormatTally(tally)
	if got == "" {
		t.Fatal("FormatTally returned an empty string; a tally line is always present in the output")
	}
}

// TestFormatTally_NoTrailingNewline verifies that FormatTally follows the
// same convention as FormatEvent: no trailing newline. The caller (the sink's
// Emit method) adds the newline when writing to the output stream.
// The test also verifies the line is non-empty so the trailing-newline check
// is meaningful (an empty string trivially has no trailing newline).
func TestFormatTally_NoTrailingNewline(t *testing.T) {
	tally := runprogress.Tally{Running: 1, Finished: 2, Remaining: 3}
	got := cli.FormatTally(tally)
	if got == "" {
		t.Fatal("FormatTally returned an empty string; cannot check for trailing newline on an empty line")
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("FormatTally must not include a trailing newline; got %q", got)
	}
}

// TestFormatTally_MentionsRunningCount verifies that the rendered line
// includes the running count somewhere, with a label so a reader can
// understand what the number represents.
func TestFormatTally_MentionsRunningCount(t *testing.T) {
	tally := runprogress.Tally{Running: 7, Finished: 0, Remaining: 0}
	got := cli.FormatTally(tally)

	hasLabel := strings.Contains(got, "running") || strings.Contains(got, "Running")
	hasCounts := strings.Contains(got, "7")

	if !hasCounts {
		t.Errorf("FormatTally(%+v) = %q, does not mention the running count (7)", tally, got)
	}
	if !hasLabel {
		t.Errorf("FormatTally(%+v) = %q, does not label the running count", tally, got)
	}
}

// TestFormatTally_MentionsFinishedCount verifies that the rendered line
// includes the finished count with a label.
func TestFormatTally_MentionsFinishedCount(t *testing.T) {
	tally := runprogress.Tally{Running: 0, Finished: 4, Remaining: 0}
	got := cli.FormatTally(tally)

	hasLabel := strings.Contains(got, "finished") || strings.Contains(got, "Finished")
	hasCount := strings.Contains(got, "4")

	if !hasCount {
		t.Errorf("FormatTally(%+v) = %q, does not mention the finished count (4)", tally, got)
	}
	if !hasLabel {
		t.Errorf("FormatTally(%+v) = %q, does not label the finished count", tally, got)
	}
}

// TestFormatTally_MentionsRemainingCount verifies that the rendered line
// includes the remaining count with a label.
func TestFormatTally_MentionsRemainingCount(t *testing.T) {
	tally := runprogress.Tally{Running: 0, Finished: 0, Remaining: 9}
	got := cli.FormatTally(tally)

	hasLabel := strings.Contains(got, "remaining") || strings.Contains(got, "Remaining")
	hasCount := strings.Contains(got, "9")

	if !hasCount {
		t.Errorf("FormatTally(%+v) = %q, does not mention the remaining count (9)", tally, got)
	}
	if !hasLabel {
		t.Errorf("FormatTally(%+v) = %q, does not label the remaining count", tally, got)
	}
}

// TestFormatTally_AllCounts_PresentInOneLine verifies that a single call to
// FormatTally places all three counts on one line — not split across multiple
// lines, and not omitting any count.
func TestFormatTally_AllCounts_PresentInOneLine(t *testing.T) {
	tally := runprogress.Tally{Running: 2, Finished: 3, Remaining: 5}
	got := cli.FormatTally(tally)

	if strings.Contains(got, "\n") {
		t.Errorf("FormatTally must return one line, not multiple; got %q", got)
	}
	for _, want := range []string{"2", "3", "5"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatTally(%+v) = %q, does not mention count %q", tally, got, want)
		}
	}
}

// TestFormatTally_ZeroCounts_StillProducesLine verifies that an all-zero
// tally (before any run starts) still produces a non-empty, well-formed line.
// Silently omitting the tally when all counts are zero would leave the user
// with no context that the suite has started.
func TestFormatTally_ZeroCounts_StillProducesLine(t *testing.T) {
	tally := runprogress.Tally{Running: 0, Finished: 0, Remaining: 0}
	got := cli.FormatTally(tally)

	if got == "" {
		t.Error("FormatTally with all-zero counts must produce a non-empty line; silence is misleading when the suite has started")
	}
}
