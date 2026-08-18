package cli_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestRunCommand_FlagForms_AreEquivalent drives `run` with every flag that
// resolves into preflight.Input once in space-separated form and once in
// equals-separated form, and requires both to resolve to the identical
// preflight.Input. The rest of the ecosystem accepts both styles, and a user
// who learns one should not discover it silently fails here.
//
// --format and --workspace-root are also documented flags covered by
// AC15.2's "every flag" equivalence claim, but neither resolves into
// preflight.Input: --format selects the output renderer (see
// TestFormat_EqualsAndSpaceForms_AreEquivalent in format_test.go) and
// --workspace-root has no corresponding preflight.Input field (see
// TestRunCommand_WorkspaceRootFlag_FormsAreEquivalent below). Each is
// covered by its own dedicated equivalence test rather than folded in here,
// because this test's assertion mechanism — comparing captured
// preflight.Input values — cannot express either.
func TestRunCommand_FlagForms_AreEquivalent(t *testing.T) {
	expected := preflight.Input{
		SuitePath:   "suite.yaml",
		FixtureRoot: "fixtures-dir",
		HarnessID:   "fake",
		Overrides: preflight.Overrides{
			TestIDs:     []string{"a", "b"},
			Timeout:     durationPtr(5 * time.Minute),
			Repetitions: intPtr(3),
		},
	}

	forms := map[string][]string{
		"space": {
			"run", "suite.yaml",
			"--tests", "a,b",
			"--harness", "fake",
			"--fixtures", "fixtures-dir",
			"--timeout", "5m",
			"--repetitions", "3",
		},
		"equals": {
			"run", "suite.yaml",
			"--tests=a,b",
			"--harness=fake",
			"--fixtures=fixtures-dir",
			"--timeout=5m",
			"--repetitions=3",
		},
	}

	captures := map[string]preflight.Input{}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			var captured preflight.Input
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
			opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

			cli.Execute(context.Background(), args, opts)

			captures[name] = captured
			if !reflect.DeepEqual(captured, expected) {
				t.Errorf("%s form: preflight.Input = %+v, want %+v", name, captured, expected)
			}
		})
	}

	if !reflect.DeepEqual(captures["space"], captures["equals"]) {
		t.Errorf("space and equals flag forms resolved to different preflight.Input: %+v vs %+v", captures["space"], captures["equals"])
	}
}

// TestRunCommand_UnknownFlag_IsUsageError covers an unrecognised flag.
func TestRunCommand_UnknownFlag_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var captured preflight.Input
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--bogus", "value"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for a malformed invocation")
	}
}

// TestRunCommand_MissingFlagValue_IsUsageError covers a flag stated without
// the value it requires.
func TestRunCommand_MissingFlagValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	// --harness is the last token: it declares a flag but supplies no value.
	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--harness"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", got, cli.ExitUsage)
	}
}

// TestRunCommand_UnknownCommand_IsUsageError covers a command name the
// surface does not define.
func TestRunCommand_UnknownCommand_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"frobnicate", "suite.yaml"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", got, cli.ExitUsage)
	}
}

// TestRunCommand_TUIFlag_IsRecognisedAndIgnored asserts the interactive-
// override flag never surfaces as an unknown flag here: it is consumed by
// the composition root, and a frontend that rejects it as unknown breaks
// the ecosystem's dispatch convention. A run carrying it must still execute
// normally.
func TestRunCommand_TUIFlag_IsRecognisedAndIgnored(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--tui"}, opts)

	if got == cli.ExitUsage {
		t.Fatalf("--tui was rejected as an unknown flag (exit = ExitUsage)")
	}
	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for a passing suite with --tui present", got, cli.ExitSuccess)
	}
	if !runner.wasCalled() {
		t.Error("suite runner was not invoked; --tui must not short-circuit the run")
	}
}

// TestRunCommand_WorkspaceRootFlag_FormsAreEquivalent drives run with
// --workspace-root in both flag forms. AC15.2's flag-form equivalence claim
// covers every accepted flag, including this one. --workspace-root has no
// corresponding field on preflight.Input, but it does reach cli.RunConfig:
// Execute resolves the root at its single flag-parse site and calls
// Options.Suite (a cli.SuiteFactory) with that RunConfig exactly once, on
// the run path. This test captures the RunConfig each form's factory call
// carried and requires both to resolve to the identical value.
func TestRunCommand_WorkspaceRootFlag_FormsAreEquivalent(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--workspace-root", "custom-root"},
		"equals": {"run", "suite.yaml", "--workspace-root=custom-root"},
	}

	results := map[string]int{}
	configs := map[string]cli.RunConfig{}
	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
			opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

			got := cli.Execute(context.Background(), args, opts)
			results[name] = got
			configs[name] = factory.lastConfig()

			if got == cli.ExitUsage {
				t.Errorf("%s form: --workspace-root was rejected as a usage error (exit = ExitUsage)", name)
			}
			if factory.callCount() != 1 {
				t.Errorf("%s form: suite factory called %d times, want exactly 1", name, factory.callCount())
			}
			if factory.lastConfig().WorkspaceRoot != "custom-root" {
				t.Errorf("%s form: RunConfig.WorkspaceRoot = %q, want %q", name, factory.lastConfig().WorkspaceRoot, "custom-root")
			}
		})
	}

	if results["space"] != results["equals"] {
		t.Errorf("space and equals forms of --workspace-root produced different exit codes: %d vs %d", results["space"], results["equals"])
	}
	if configs["space"] != configs["equals"] {
		t.Errorf("space and equals forms of --workspace-root resolved to different RunConfig: %+v vs %+v", configs["space"], configs["equals"])
	}
}

// TestRunCommand_WorkspaceRootFlag_DefaultsToOptionsWorkspaceRoot asserts
// the other half of the resolution rule: when --workspace-root is absent,
// the factory is still called exactly once, and RunConfig.WorkspaceRoot
// carries Options.WorkspaceRoot rather than an empty value.
func TestRunCommand_WorkspaceRootFlag_DefaultsToOptionsWorkspaceRoot(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", got, cli.ExitSuccess)
	}
	if factory.callCount() != 1 {
		t.Errorf("suite factory called %d times, want exactly 1", factory.callCount())
	}
	if factory.lastConfig().WorkspaceRoot != opts.WorkspaceRoot {
		t.Errorf("RunConfig.WorkspaceRoot = %q, want Options.WorkspaceRoot %q", factory.lastConfig().WorkspaceRoot, opts.WorkspaceRoot)
	}
}

// TestRunCommand_MalformedTimeout_IsUsageError covers a --timeout value that
// cannot be parsed as a duration — distinct from --timeout stated without a
// value at all (already covered by TestRunCommand_MissingFlagValue_IsUsageError),
// this is a value that is present but fails to parse.
func TestRunCommand_MalformedTimeout_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--timeout=notaduration"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for an unparseable --timeout value", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for a malformed --timeout value")
	}
}

// TestRunCommand_MalformedRepetitions_IsUsageError covers a --repetitions
// value that cannot be parsed as an integer, for the same reason.
func TestRunCommand_MalformedRepetitions_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--repetitions=abc"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for an unparseable --repetitions value", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for a malformed --repetitions value")
	}
}

// TestValidateCommand_DoesNotRunSuite asserts `validate` performs pre-flight
// only: it creates no sandbox and spawns no agent, which here means the
// suite runner is never invoked, success or failure.
func TestValidateCommand_DoesNotRunSuite(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml"}, opts)

	if runner.wasCalled() {
		t.Error("validate invoked the suite runner; validate must pre-flight only")
	}
	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for a clean validate", got, cli.ExitSuccess)
	}
}

// TestValidateCommand_NeverCallsSuiteFactory asserts the observable form of
// AC15.6's "no workspace created" now that suite construction is deferred
// to a cli.SuiteFactory: pre-flight creates no sandbox, so nothing may call
// the factory on the validate path, even for a clean report where a `run`
// invocation would call it.
func TestValidateCommand_NeverCallsSuiteFactory(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &fakeSuiteFactory{runner: &fakeSuiteRunner{result: passingResult("orchestrator-routing")}}
	opts := baseOptionsWithFactory(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), factory.build)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml"}, opts)

	if factory.callCount() != 0 {
		t.Errorf("suite factory called %d times on validate, want 0; validate must construct no sandbox manager", factory.callCount())
	}
	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for a clean validate", got, cli.ExitSuccess)
	}
}
