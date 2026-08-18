package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestRunCommand_PreflightFailure_ReportsAllErrorsInOneRun asserts the whole
// point of pre-flight: several unrelated authored-file errors are reported
// together, in one run, in the deterministic order authoring.Report.Sorted
// defines, with a non-zero exit — and, critically, before the expensive
// part ever starts.
func TestRunCommand_PreflightFailure_ReportsAllErrorsInOneRun(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	rep := failingReport()
	sorted := rep.Sorted()
	if len(sorted) < 3 {
		t.Fatalf("test fixture must carry several diagnostics; got %d", len(sorted))
	}

	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, rep, nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitPreflight {
		t.Fatalf("exit code = %d, want ExitPreflight (%d)", got, cli.ExitPreflight)
	}

	output := stdout.String() + stderr.String()

	positions := make([]int, len(sorted))
	for i, d := range sorted {
		idx := strings.Index(output, d.Message)
		if idx < 0 {
			t.Errorf("diagnostic %q (%s) missing from output; got:\n%s", d.Message, d.Code, output)
			continue
		}
		positions[i] = idx
	}

	for i := 1; i < len(positions); i++ {
		if positions[i-1] > positions[i] {
			t.Errorf("diagnostics are not reported in Report.Sorted order: %q appears after %q", sorted[i-1].Message, sorted[i].Message)
		}
	}
}

// TestRunCommand_PreflightFailure_NoSandboxNoAgent asserts the negative
// space directly: a rejected authoring pass never reaches the suite, so no
// sandbox is created and no agent is spawned. Neither the suite factory nor
// the runner it would build are the only components in this package's
// reach that could do either — the factory itself constructs the sandbox
// manager, so "no workspace created" requires the factory to go uncalled,
// not merely the runner it would have returned.
func TestRunCommand_PreflightFailure_NoSandboxNoAgent(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{}}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, failingReport(), nil), factory.build)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	// Both must hold together: a rejection that also forgot to exit
	// non-zero would be just as broken as one that spawned anyway.
	if got != cli.ExitPreflight {
		t.Errorf("exit code = %d, want ExitPreflight (%d)", got, cli.ExitPreflight)
	}
	if factory.callCount() != 0 {
		t.Errorf("suite factory called %d times after pre-flight rejected the authored files, want 0", factory.callCount())
	}
	if factory.runner.(*fakeSuiteRunner).wasCalled() {
		t.Error("suite runner was invoked after pre-flight rejected the authored files")
	}
}

// TestValidateCommand_PreflightFailure_SharesTheSameReportRendering asserts
// AC15.5/I15.5: `validate` renders a pre-flight failure through the same
// path as `run`'s pre-flight failure, so the two never drift into two
// different diagnostic vocabularies.
func TestValidateCommand_PreflightFailure_SharesTheSameReportRendering(t *testing.T) {
	rep := failingReport()

	runStdout, runStderr := &bytes.Buffer{}, &bytes.Buffer{}
	runRunner := &fakeSuiteRunner{}
	runOpts := baseOptions(runStdout, runStderr, scriptedPreflight(preflight.Plan{}, rep, nil), runRunner)
	runCode := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, runOpts)

	valStdout, valStderr := &bytes.Buffer{}, &bytes.Buffer{}
	valRunner := &fakeSuiteRunner{}
	valOpts := baseOptions(valStdout, valStderr, scriptedPreflight(preflight.Plan{}, rep, nil), valRunner)
	valCode := cli.Execute(context.Background(), []string{"validate", "suite.yaml"}, valOpts)

	if runCode != cli.ExitPreflight || valCode != cli.ExitPreflight {
		t.Fatalf("run exit = %d, validate exit = %d; both want ExitPreflight (%d)", runCode, valCode, cli.ExitPreflight)
	}

	runOutput := runStdout.String() + runStderr.String()
	valOutput := valStdout.String() + valStderr.String()
	if runOutput != valOutput {
		t.Errorf("run and validate rendered the same pre-flight failure differently:\nrun:\n%s\nvalidate:\n%s", runOutput, valOutput)
	}
}

// TestRunCommand_PreflightSuccess_RunsTheSuite is the mirror case: a clean
// report must not be treated as a rejection.
func TestRunCommand_PreflightSuccess_RunsTheSuite(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got == cli.ExitPreflight {
		t.Fatal("a clean pre-flight report was treated as a rejection")
	}
	if !runner.wasCalled() {
		t.Error("suite runner was not invoked after a clean pre-flight report")
	}
}
