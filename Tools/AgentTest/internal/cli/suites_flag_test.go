package cli_test

// Tests for the --suites flag added to the command surface by this stage.
//
// --suites <path> names the suite root or suite file for interactive
// discovery. The CLI's own flag parser owes it the same treatment as
// --logger-bundle, --cost-tool and --tui: recognise it and do not reject it
// as unknown. The composition root pre-scans and consumes the value; this
// package only needs to not break an invocation that carries it.
//
// Until "suites" is added to the accepted valueFlags set in run.go, every
// invocation in this file will return ExitUsage (unknown flag), making these
// tests fail at runtime — the expected TDD RED state.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestRunCommand_SuitesFlag_IsRecognisedAndIgnored asserts that --suites
// never surfaces as an unknown flag on the `run` command, in either accepted
// form. The flag is pre-scanned and consumed by the composition root, so
// this package's only obligation is not to reject it as unknown.
func TestRunCommand_SuitesFlag_IsRecognisedAndIgnored(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--suites", "Tools/AgentTest/tests"},
		"equals": {"run", "suite.yaml", "--suites=Tools/AgentTest/tests"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{result: passingResult("suites-flag")}
			opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Errorf("%s form: --suites was rejected as an unknown flag (exit = ExitUsage); stderr: %s", name, stderr.String())
			}
		})
	}
}

// TestValidateCommand_SuitesFlag_IsRecognisedAndIgnored asserts the same for
// the `validate` command. Both commands share one flag parser, so both must
// accept --suites without rejecting it.
func TestValidateCommand_SuitesFlag_IsRecognisedAndIgnored(t *testing.T) {
	forms := map[string][]string{
		"space":  {"validate", "suite.yaml", "--suites", "Tools/AgentTest/tests"},
		"equals": {"validate", "suite.yaml", "--suites=Tools/AgentTest/tests"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{}
			opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Errorf("%s form: --suites was rejected as an unknown flag on validate (exit = ExitUsage); stderr: %s", name, stderr.String())
			}
		})
	}
}

// TestRunCommand_SuitesFlag_DoesNotAffectRunBehaviour asserts that --suites
// present on a `run` invocation does not alter how the run itself proceeds:
// the suite runner is still invoked exactly once and the exit code reflects
// the run result, not a flag-handling side effect.
func TestRunCommand_SuitesFlag_DoesNotAffectRunBehaviour(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("suites-flag")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--suites", "Tools/AgentTest/tests"}, opts)

	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); --suites must not interfere with a passing run", got, cli.ExitSuccess)
	}
	if !runner.wasCalled() {
		t.Error("suite runner was not invoked; --suites must not short-circuit the run")
	}
}

// TestRunCommand_SuitesFlag_CoexistsWithTUIFlag asserts that --suites and
// --tui may appear on the same invocation without either flag causing the
// other to be rejected. Both are consumed by the composition root, and this
// package recognises both independently.
func TestRunCommand_SuitesFlag_CoexistsWithTUIFlag(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("suites-flag")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--tui", "--suites", "Tools/AgentTest/tests"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--suites alongside --tui was rejected as a usage error; stderr: %s", stderr.String())
	}
}

// TestRunCommand_SuitesFlag_MissingValue_IsUsageError asserts that --suites
// stated as the final argument, with no following value, is a usage error —
// consistent with every other value-consuming flag's behaviour.
func TestRunCommand_SuitesFlag_MissingValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	// --suites is the last token: it declares a flag but supplies no value.
	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--suites"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --suites with no value; stderr: %s", got, cli.ExitUsage, stderr.String())
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for a --suites flag with no value")
	}
}
