package cli_test

// max_concurrent_runs_test.go covers the --max-concurrent-runs flag (T12.1,
// T12.4, T12.5).
//
// RED-phase notes:
//   - Tests checking RunConfig.MaxConcurrentRuns is set from the flag compile
//     (the field exists as a contract stub) but FAIL because the flag is not
//     yet parsed or wired into RunConfig (I12.2 and I12.5).
//   - Tests checking RunConfig.MaxConcurrentRuns when the flag is absent
//     compile but FAIL because the composition root does not yet apply
//     suite.DefaultMaxConcurrentRuns (I12.5).
//   - Tests checking the flag is recognised (not ExitUsage) compile but FAIL
//     because --max-concurrent-runs is not yet in valueFlags (I12.2).
//   - Tests checking --max-concurrent-runs in Flags() compile but FAIL because
//     the entry is not yet in flagSpecs (I12.2).
//   - Tests checking validation rejection (non-integer, zero, negative) compile
//     and currently PASS for the wrong reason (the flag is unknown → ExitUsage).
//     They will continue to pass after implementation, now for the right reason.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// T12.1 — CLI flag recognised and documented
// ---------------------------------------------------------------------------

// TestRunCommand_MaxConcurrentRunsFlag_IsRecognised asserts that supplying
// --max-concurrent-runs with a valid positive integer is not rejected as an
// unknown flag. This is the primary RED-phase test: until I12.2 adds the flag
// to valueFlags, every invocation with --max-concurrent-runs returns ExitUsage.
func TestRunCommand_MaxConcurrentRunsFlag_IsRecognised(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs", "4"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--max-concurrent-runs flag was rejected as unknown (exit = ExitUsage); stderr=%s\nAdd --max-concurrent-runs to valueFlags in cli/run.go (I12.2)", stderr.String())
	}
}

// TestRunCommand_MaxConcurrentRunsFlag_EqualsForm asserts the equals-separated
// form --max-concurrent-runs=4 is recognised. Every value flag accepts both
// forms; this one must too.
func TestRunCommand_MaxConcurrentRunsFlag_EqualsForm(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs=4"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--max-concurrent-runs=4 (equals-separated form) was rejected as unknown (exit = ExitUsage); stderr=%s", stderr.String())
	}
}

// TestFlags_IncludesMaxConcurrentRunsFlag asserts the shared flag specification
// that the parser, positional detection and usage text all derive from
// documents --max-concurrent-runs, so a user running --help can discover it.
func TestFlags_IncludesMaxConcurrentRunsFlag(t *testing.T) {
	names := map[string]bool{}
	for _, f := range cli.Flags() {
		names[f.Name] = true
	}
	if !names["--max-concurrent-runs"] {
		t.Errorf("cli.Flags() does not include --max-concurrent-runs; add it to flagSpecs in cli/help.go (I12.2) so it appears in --help output")
	}
}

// TestFlags_MaxConcurrentRunsFlag_ConsumesValue asserts the flag specification
// marks --max-concurrent-runs as a value-consuming flag, so the composition
// root's positional-detection logic skips its value token.
func TestFlags_MaxConcurrentRunsFlag_ConsumesValue(t *testing.T) {
	for _, f := range cli.Flags() {
		if f.Name == "--max-concurrent-runs" {
			if !f.ConsumesValue {
				t.Errorf("cli.Flags() entry for --max-concurrent-runs has ConsumesValue=false; it takes a required integer value and must consume it")
			}
			return
		}
	}
	// Flag absent: the preceding TestFlags_IncludesMaxConcurrentRunsFlag
	// catches this condition. Skip with a no-op.
}

// ---------------------------------------------------------------------------
// T12.1 — Validation: invalid values rejected
// ---------------------------------------------------------------------------

// TestRunCommand_MaxConcurrentRunsFlag_InvalidValue_RejectsWithUsageError asserts
// that a non-integer value produces a usage error naming the flag and the
// accepted range. The value must never be silently clamped.
func TestRunCommand_MaxConcurrentRunsFlag_InvalidValue_RejectsWithUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs", "notanint"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("--max-concurrent-runs with non-integer value should produce ExitUsage; got exit=%d stderr=%s", got, stderr.String())
	}
}

// TestRunCommand_MaxConcurrentRunsFlag_Zero_RejectsWithUsageError asserts that
// zero is rejected: the minimum bound is 1 (strictly sequential). A zero value
// must produce a clear usage error, not a silent clamp to 1.
func TestRunCommand_MaxConcurrentRunsFlag_Zero_RejectsWithUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs", "0"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("--max-concurrent-runs 0 should produce ExitUsage (zero is below the minimum of 1); got exit=%d stderr=%s", got, stderr.String())
	}
}

// TestRunCommand_MaxConcurrentRunsFlag_Negative_RejectsWithUsageError asserts that
// a negative value is rejected with a usage error. A negative bound is nonsensical
// and must never be silently coerced into a working value.
func TestRunCommand_MaxConcurrentRunsFlag_Negative_RejectsWithUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs", "-1"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("--max-concurrent-runs -1 should produce ExitUsage (negative is below the minimum of 1); got exit=%d stderr=%s", got, stderr.String())
	}
}

// TestRunCommand_MaxConcurrentRunsFlag_One_IsAccepted asserts that the value
// 1 is valid. A bound of 1 is the strictly-sequential mode that reproduces
// pre-concurrency behaviour; it must be accepted explicitly, not rejected as
// "too small".
func TestRunCommand_MaxConcurrentRunsFlag_One_IsAccepted(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--max-concurrent-runs", "1"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--max-concurrent-runs 1 (the minimum sequential bound) was rejected (ExitUsage); it must be accepted as the minimum valid value; stderr=%s", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// T12.4 — Resolved value reaches RunConfig (CLI path)
// ---------------------------------------------------------------------------

// TestRunCommand_MaxConcurrentRunsFlag_ReachesRunConfig asserts that the integer
// value supplied with --max-concurrent-runs is threaded into RunConfig.MaxConcurrentRuns
// exactly as parsed, so the composition root's suite-factory receives it
// field-for-field.
//
// RED-phase note: compiles (RunConfig.MaxConcurrentRuns is declared), but FAILS
// until I12.2 parses the flag and I12.5 sets the field.
func TestRunCommand_MaxConcurrentRunsFlag_ReachesRunConfig(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "value 1", args: []string{"run", "suite.yaml", "--max-concurrent-runs", "1"}, want: 1},
		{name: "value 3", args: []string{"run", "suite.yaml", "--max-concurrent-runs", "3"}, want: 3},
		{name: "value 8", args: []string{"run", "suite.yaml", "--max-concurrent-runs", "8"}, want: 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
			opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

			cli.Execute(context.Background(), tc.args, opts)

			got := factory.lastConfig().MaxConcurrentRuns
			if got != tc.want {
				t.Errorf("RunConfig.MaxConcurrentRuns = %d, want %d (flag value must be threaded field-for-field; stderr=%s)", got, tc.want, stderr.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T12.5 — Absent flag uses documented default
// ---------------------------------------------------------------------------

// TestRunCommand_MaxConcurrentRunsFlag_AbsentUsesDefault asserts that when
// --max-concurrent-runs is not supplied, RunConfig.MaxConcurrentRuns equals
// suite.DefaultMaxConcurrentRuns — the documented conservative default that
// does not require the user to know what a safe value is.
//
// RED-phase note: compiles (both fields exist), but FAILS until I12.5 applies
// the default: the field is currently zero-valued when the flag is absent.
func TestRunCommand_MaxConcurrentRunsFlag_AbsentUsesDefault(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	got := factory.lastConfig().MaxConcurrentRuns
	if got != suite.DefaultMaxConcurrentRuns {
		t.Errorf("RunConfig.MaxConcurrentRuns when --max-concurrent-runs is absent = %d, want suite.DefaultMaxConcurrentRuns (%d); the composition root must apply the default when no flag is supplied (I12.5)",
			got, suite.DefaultMaxConcurrentRuns)
	}
}

// TestRunCommand_MaxConcurrentRunsFlag_AbsentDoesNotProduceZero asserts that
// an absent flag never leaves RunConfig.MaxConcurrentRuns at zero. Zero would
// propagate to suite.Options.MaxConcurrentRuns where it resolves to the same
// default, but having zero travel through RunConfig hides which tier applied
// the default and makes the field-for-field assertion in RunConfig meaningless.
//
// RED-phase note: same as TestRunCommand_MaxConcurrentRunsFlag_AbsentUsesDefault —
// compiles but FAILS until I12.5.
func TestRunCommand_MaxConcurrentRunsFlag_AbsentDoesNotProduceZero(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	got := factory.lastConfig().MaxConcurrentRuns
	if got == 0 {
		t.Errorf("RunConfig.MaxConcurrentRuns when --max-concurrent-runs is absent = 0; the resolved value must be suite.DefaultMaxConcurrentRuns (%d), not zero (I12.5); stderr=%s",
			suite.DefaultMaxConcurrentRuns, stderr.String())
	}
}
