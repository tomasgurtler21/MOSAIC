package cli_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestExitCode_Success: every test passed.
func TestExitCode_Success(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d)", got, cli.ExitSuccess)
	}
}

// TestExitCode_TestsFailed: the suite ran but a test failed on its
// assertions — the subject regressed. Distinct from ExitFailure.
func TestExitCode_TestsFailed(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: failedTestResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitTestsFailed {
		t.Errorf("exit code = %d, want ExitTestsFailed (%d)", got, cli.ExitTestsFailed)
	}
	if got == cli.ExitFailure {
		t.Error("a subject regression must not be reported as ExitFailure")
	}
}

// TestExitCode_Failure_SuiteReturnedError: the tool itself could not
// complete the work.
func TestExitCode_Failure_SuiteReturnedError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{err: errors.New("sandbox creation failed: disk full")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", got, cli.ExitFailure)
	}
}

// TestExitCode_Failure_InfrastructureFault: the suite ran to completion but
// a test ended by a repeated state-integrity fault rather than by any
// assertion. This must read as ExitFailure, never as ExitTestsFailed —
// conflating the two is exactly the confusion this tool exists to remove.
func TestExitCode_Failure_InfrastructureFault(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: infrastructureFaultResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d)", got, cli.ExitFailure)
	}
	if got == cli.ExitTestsFailed {
		t.Error("an infrastructure fault must not be reported as ExitTestsFailed")
	}
}

// TestExitCode_Failure_SuiteFactoryError: the suite factory could not build
// a runner for the resolved RunConfig — an unusable workspace root, most of
// all. The invocation itself was well formed, so this is ExitFailure, never
// ExitUsage, and pre-flight having already succeeded is unaffected by it.
func TestExitCode_Failure_SuiteFactoryError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{err: errors.New("workspace root is not writable")}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) when the suite factory cannot build a runner", got, cli.ExitFailure)
	}
	if got == cli.ExitUsage {
		t.Error("a factory error is a wiring fault, not a malformed invocation")
	}
	if got == cli.ExitPreflight {
		t.Error("a factory error must not be reported as a pre-flight rejection; pre-flight already succeeded")
	}
	if factory.callCount() != 1 {
		t.Errorf("suite factory called %d times, want exactly 1", factory.callCount())
	}
}

// TestExitCode_Preflight: pre-flight rejected the authored files.
func TestExitCode_Preflight(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, failingReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitPreflight {
		t.Errorf("exit code = %d, want ExitPreflight (%d)", got, cli.ExitPreflight)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked after a pre-flight rejection")
	}
}

// TestExitCode_Usage: the invocation itself was malformed and pre-flight
// never ran at all.
func TestExitCode_Usage(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--unknown-flag"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for a malformed invocation")
	}
}

// TestExitCode_AllFiveCodesAreDistinct guards against two contract codes
// silently colliding on the same integer value.
func TestExitCode_AllFiveCodesAreDistinct(t *testing.T) {
	codes := map[string]int{
		"ExitSuccess":     cli.ExitSuccess,
		"ExitFailure":     cli.ExitFailure,
		"ExitTestsFailed": cli.ExitTestsFailed,
		"ExitUsage":       cli.ExitUsage,
		"ExitPreflight":   cli.ExitPreflight,
	}
	seen := map[int]string{}
	for name, code := range codes {
		if other, ok := seen[code]; ok {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}
		seen[code] = name
	}
}
