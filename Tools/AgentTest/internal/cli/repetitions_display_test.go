package cli_test

// repetitions_display_test.go covers the CLI's pre-run repetitions display
// (Stage 3):
//
//   T3.2 (CLI) - Before a run starts, the CLI prints the effective repetitions
//                count — either the user's --repetitions override or the suite's
//                authored default — so the user can confirm the value without
//                inspecting the suite file or the run output.
//
// These tests are in package cli_test and use the shared helpers from
// testhelpers_test.go. No real suite is executed; fakeSuiteRunner delivers a
// scripted result immediately.
//
// NOTE: These tests are in TDD RED phase. The current runCommand does not print
// the effective repetitions value before calling runner.Run. The tests assert
// behaviour that does not exist yet, so they will fail at runtime. That is the
// expected RED state — they specify the target behaviour for implementation
// task I3.4.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// T3.2 (CLI): pre-run repetitions display
// ---------------------------------------------------------------------------

// TestRunCommand_RepetitionsFlag_DisplayedBeforeRun verifies that when
// --repetitions is provided, the CLI prints the effective repetitions count to
// stdout alongside a term identifying it as a repetitions-related value, so
// the user can confirm the value is in effect before results scroll by.
func TestRunCommand_RepetitionsFlag_DisplayedBeforeRun(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("my-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	args := []string{"run", "suite.yaml", "--repetitions", "13"}
	cli.Execute(context.Background(), args, opts)

	// The effective repetitions count must appear in the output alongside a term
	// associated with repetitions. We use 13 to avoid false positives from
	// single-digit values that might coincidentally appear in cost strings
	// (e.g., "$0.42"). The exact phrasing is the implementer's choice.
	out := strings.ToLower(stdout.String())
	hasCount := strings.Contains(out, "13")
	hasLabel := strings.Contains(out, "repetition") ||
		strings.Contains(out, "reps") ||
		strings.Contains(out, "repeat")
	if !hasCount || !hasLabel {
		t.Errorf("CLI stdout does not display the effective repetitions count 13 with a recognisable label (hasCount=%v hasLabel=%v);\nstdout:\n%s\nstderr:\n%s", hasCount, hasLabel, stdout.String(), stderr.String())
	}
}

// TestRunCommand_NoRepetitionsFlag_DefaultRepetitionsDisplayed verifies that
// when --repetitions is not provided, the CLI still displays the effective
// repetitions value (the suite's authored default, carried in the resolved
// plan) before a run starts. A user who does not set --repetitions must be
// able to see the default that was used.
//
// The scripted plan does not carry a repetitions value (zero means "use
// package default"), so the display must show whatever the effective value is
// without panicking or omitting the line entirely.
func TestRunCommand_NoRepetitionsFlag_DefaultRepetitionsDisplayed(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("my-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	args := []string{"run", "suite.yaml"}
	cli.Execute(context.Background(), args, opts)

	// The stdout should contain some indication of the repetitions count. The
	// exact wording is the implementer's choice; we require a term associated
	// with repetitions to appear.
	out := strings.ToLower(stdout.String())
	hasRepetitionDisplay := strings.Contains(out, "repetition") ||
		strings.Contains(out, "reps") ||
		strings.Contains(out, "repeat")
	if !hasRepetitionDisplay {
		t.Errorf("CLI stdout does not contain any indication of the effective repetitions count (checked for 'repetition', 'reps', 'repeat');\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

// TestRunCommand_RepetitionsDisplay_AppearsBeforeResultsSummary verifies that
// the effective repetitions indication appears in the output before the final
// results summary header, so the user sees the configuration before the verdict
// lines. The final summary starts with "Suite:" in the text renderer.
func TestRunCommand_RepetitionsDisplay_AppearsBeforeResultsSummary(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("my-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	// Use 17 — an unambiguous value that does not appear in cost strings like
	// "$0.42" or in default timestamps.
	args := []string{"run", "suite.yaml", "--repetitions", "17"}
	cli.Execute(context.Background(), args, opts)

	out := stdout.String()

	// First, check that a repetitions indication is present at all.
	outLower := strings.ToLower(out)
	hasLabel := strings.Contains(outLower, "repetition") ||
		strings.Contains(outLower, "reps") ||
		strings.Contains(outLower, "repeat")
	if !hasLabel {
		t.Fatalf("CLI stdout does not contain any repetitions indication (checked for 'repetition', 'reps', 'repeat');\nstdout:\n%s\nstderr:\n%s", out, stderr.String())
	}
	if !strings.Contains(out, "17") {
		t.Fatalf("CLI stdout does not contain the repetitions count 17;\nstdout:\n%s\nstderr:\n%s", out, stderr.String())
	}

	// The repetitions indication must appear before the results summary header
	// ("Suite:" in the text renderer). If "Suite:" is not present at all, the
	// ordering constraint cannot be checked and the test skips it.
	idxSuite := strings.Index(out, "Suite:")
	if idxSuite < 0 {
		t.Logf("'Suite:' header not found in stdout; skipping ordering assertion")
		return
	}

	// Find the first occurrence of "17" in the output.
	idx17 := strings.Index(out, "17")
	if idx17 > idxSuite {
		t.Errorf("repetitions count '17' first appears at position %d, after 'Suite:' at position %d; want the repetitions display to precede the results summary;\nstdout:\n%s", idx17, idxSuite, out)
	}
}
