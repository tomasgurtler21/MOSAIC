package cli_test

// Stage 5 (Harness-Agnostic Suites and Selection): AC5.2's usage-error half
// for the CLI frontend. --harness already selects the adapter (see
// TestRunCommand_FlagForms_AreEquivalent in command_test.go, which drives
// both flag forms into preflight.Input.HarnessID); what this file adds is
// the validation ContractsDesign.md's cli.Options.Harnesses field exists
// for: a --harness value naming no entry in the supplied catalog is a usage
// error, never a silent pass-through to preflight.

import (
	"bytes"
	"context"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// catalogOptions is baseOptions extended with a two-entry harness catalog,
// so this file's tests can drive --harness against a known, small set
// without depending on mosaic-common/harness's real catalog membership.
func catalogOptions(stdout, stderr *bytes.Buffer, pf cli.PreflightFunc, sr cli.SuiteRunner) cli.Options {
	o := baseOptions(stdout, stderr, pf, sr)
	o.Harnesses = []commonharness.CLIHarness{
		{ID: "claude-code", Label: "Claude Code CLI"},
		{ID: "opencode", Label: "OpenCode CLI"},
	}
	return o
}

// TestRunCommand_UnrecognisedHarness_IsUsageError is AC5.2's negative case:
// a --harness value that names no entry in Options.Harnesses must produce
// ExitUsage and must never reach pre-flight or the suite runner — an
// unrecognised selection is a usage error, not a silent default harness.
func TestRunCommand_UnrecognisedHarness_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var preflightCalled bool
	captureCalled := func(in preflight.Input) (preflight.Plan, authoring.Report) {
		preflightCalled = true
		return preflight.Plan{}, authoring.Report{}
	}
	runner := &fakeSuiteRunner{}
	opts := catalogOptions(stdout, stderr, captureCalled, runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--harness", "not-a-real-harness"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for a --harness value absent from the catalog", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for an unrecognised --harness selection")
	}
	if preflightCalled {
		t.Error("pre-flight was invoked for an unrecognised --harness selection; validation must happen before pre-flight runs")
	}
}

// TestRunCommand_RecognisedHarness_BothFlagForms_AreAccepted is AC5.2's
// positive case: a --harness value naming a catalog entry, in either flag
// form, is accepted and reaches preflight.Input.HarnessID unchanged — never
// rejected as a usage error merely because the catalog is now consulted.
func TestRunCommand_RecognisedHarness_BothFlagForms_AreAccepted(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--harness", "opencode"},
		"equals": {"run", "suite.yaml", "--harness=opencode"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			var captured preflight.Input
			runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
			opts := catalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Fatalf("%s form: a catalog-recognised --harness value was rejected as a usage error", name)
			}
			if captured.HarnessID != "opencode" {
				t.Errorf("%s form: preflight.Input.HarnessID = %q, want %q", name, captured.HarnessID, "opencode")
			}
		})
	}
}
