package cli_test

// model_flags_test.go covers the --subject-model and --stub-model flags added
// to the command surface:
//
//   Flags resolve: both flags reach preflight.Input.Overrides.SubjectModel and
//   .StubModel, in both the space-separated and equals-separated forms, on both
//   the run and validate commands.
//
//   Validation: a model value absent from the resolved harness's catalog is a
//   usage error naming the rejected value and the harness; a model valid for a
//   different harness is rejected on the same terms; an unsupplied or
//   harness-less catalog rejects nothing.
//
//   Defaults: omitting both flags leaves both override values as the empty string
//   and never blocks on input.

import (
	"bytes"
	"context"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// modelCatalogOptions returns base options extended with a small, predictable
// harness catalog and a matching model catalog. The resolved harness is
// "claude-code" (the DefaultHarness baseOptions sets). A second harness
// "opencode" carries a non-overlapping model set, so tests can verify that a
// model valid for one harness is rejected when a different harness is resolved.
func modelCatalogOptions(stdout, stderr *bytes.Buffer, pf cli.PreflightFunc, sr cli.SuiteRunner) cli.Options {
	o := baseOptions(stdout, stderr, pf, sr)
	o.Harnesses = []commonharness.CLIHarness{
		{ID: "claude-code", Label: "Claude Code CLI"},
		{ID: "opencode", Label: "OpenCode CLI"},
	}
	o.Models = []commonharness.ModelCatalog{
		{HarnessID: "claude-code", IDs: []string{"claude-sonnet-4-5", "claude-opus-4-5"}},
		{HarnessID: "opencode", IDs: []string{"github-copilot/claude-sonnet", "github-copilot/claude-opus"}},
	}
	return o
}

// ---------------------------------------------------------------------------
// Flag resolution: T11.1
// ---------------------------------------------------------------------------

// TestRunCommand_SubjectModelFlag_ResolvesIntoOverrides asserts that
// --subject-model, in both the space-separated and equals-separated forms,
// reaches preflight.Input.Overrides.SubjectModel and is not rejected as a
// usage error.
func TestRunCommand_SubjectModelFlag_ResolvesIntoOverrides(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--subject-model", "claude-sonnet-4-5"},
		"equals": {"run", "suite.yaml", "--subject-model=claude-sonnet-4-5"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			var captured preflight.Input
			runner := &fakeSuiteRunner{result: passingResult("subject-model-resolution")}
			opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Fatalf("%s form: --subject-model was rejected as a usage error (stderr=%s)", name, stderr.String())
			}
			if captured.Overrides.SubjectModel != "claude-sonnet-4-5" {
				t.Errorf("%s form: preflight.Input.Overrides.SubjectModel = %q, want %q",
					name, captured.Overrides.SubjectModel, "claude-sonnet-4-5")
			}
		})
	}
}

// TestRunCommand_StubModelFlag_ResolvesIntoOverrides asserts that --stub-model,
// in both the space-separated and equals-separated forms, reaches
// preflight.Input.Overrides.StubModel and is not rejected as a usage error.
func TestRunCommand_StubModelFlag_ResolvesIntoOverrides(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--subject-model", "claude-sonnet-4-5", "--stub-model", "claude-opus-4-5"},
		"equals": {"run", "suite.yaml", "--subject-model=claude-sonnet-4-5", "--stub-model=claude-opus-4-5"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			var captured preflight.Input
			runner := &fakeSuiteRunner{result: passingResult("stub-model-resolution")}
			opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

			got := cli.Execute(context.Background(), args, opts)

			if got == cli.ExitUsage {
				t.Fatalf("%s form: --stub-model was rejected as a usage error (stderr=%s)", name, stderr.String())
			}
			if captured.Overrides.StubModel != "claude-opus-4-5" {
				t.Errorf("%s form: preflight.Input.Overrides.StubModel = %q, want %q",
					name, captured.Overrides.StubModel, "claude-opus-4-5")
			}
		})
	}
}

// TestValidateCommand_SubjectModelFlag_IsRecognised asserts --subject-model is
// accepted on the validate command. Both commands share one flag parser, so
// both must accept the flag without a usage error.
func TestValidateCommand_SubjectModelFlag_IsRecognised(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml", "--subject-model", "claude-sonnet-4-5"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--subject-model on validate was rejected as unknown (stderr=%s)", stderr.String())
	}
}

// TestValidateCommand_StubModelFlag_IsRecognised asserts --stub-model is
// accepted on the validate command.
func TestValidateCommand_StubModelFlag_IsRecognised(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml", "--subject-model", "claude-sonnet-4-5", "--stub-model", "claude-opus-4-5"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--stub-model on validate was rejected as unknown (stderr=%s)", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Catalog validation: T11.2
// ---------------------------------------------------------------------------

// TestRunCommand_UnrecognisedSubjectModel_IsUsageError asserts that a
// --subject-model value absent from the resolved harness's catalog is a usage
// error. The error message must name both the rejected value and the harness.
// Pre-flight and the suite runner must not be reached.
func TestRunCommand_UnrecognisedSubjectModel_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var preflightCalled bool
	pf := func(in preflight.Input) (preflight.Plan, authoring.Report) {
		preflightCalled = true
		return preflight.Plan{}, authoring.Report{}
	}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, pf, runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model", "not-a-real-model"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --subject-model value absent from catalog", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked despite invalid --subject-model")
	}
	if preflightCalled {
		t.Error("pre-flight was invoked despite invalid --subject-model; model validation must happen before pre-flight")
	}
	errOut := stderr.String()
	if !containsAll(errOut, "not-a-real-model", "claude-code") {
		t.Errorf("error message does not name both the rejected value and the harness:\n%s", errOut)
	}
}

// TestRunCommand_UnrecognisedStubModel_IsUsageError asserts that a --stub-model
// value absent from the resolved harness's catalog is a usage error naming both
// the rejected value and the harness.
func TestRunCommand_UnrecognisedStubModel_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(),
		[]string{"run", "suite.yaml", "--subject-model", "claude-sonnet-4-5", "--stub-model", "not-a-real-model"},
		opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --stub-model value absent from catalog", got, cli.ExitUsage)
	}
	errOut := stderr.String()
	if !containsAll(errOut, "not-a-real-model", "claude-code") {
		t.Errorf("error message does not name both the rejected value and the harness:\n%s", errOut)
	}
}

// TestRunCommand_ModelValidForDifferentHarness_IsUsageError asserts that a
// model identifier valid for one harness is rejected when the resolved harness
// is different. Validation keys on the resolved harness only, never on the
// union of all catalogs.
func TestRunCommand_ModelValidForDifferentHarness_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)
	// "github-copilot/claude-sonnet" is valid for "opencode", not for the
	// resolved harness "claude-code" (the DefaultHarness baseOptions sets).

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model", "github-copilot/claude-sonnet"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d); model valid for opencode must be rejected when claude-code is the resolved harness (stderr=%s)",
			got, cli.ExitUsage, stderr.String())
	}
}

// TestRunCommand_EmptyModelCatalog_RejectsNothing asserts that when
// Options.Models is nil, any model flag value passes through without
// validation. An unwired context must not reject every value.
func TestRunCommand_EmptyModelCatalog_RejectsNothing(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("no-catalog-suite")}
	// baseOptions does not set Models (nil); no harness catalog either.
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model", "any-value-whatsoever"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("nil Options.Models rejected a model value; an unwired catalog must never reject (stderr=%s)", stderr.String())
	}
}

// TestRunCommand_ModelCatalogHasNoEntryForHarness_RejectsNothing asserts that
// when Options.Models has no entry for the resolved harness, model flag values
// pass through without validation. A miss in the catalog is treated the same as
// an absent catalog: no rejection.
func TestRunCommand_ModelCatalogHasNoEntryForHarness_RejectsNothing(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("no-entry-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)
	// Only "opencode" has a catalog entry; the resolved harness is "claude-code".
	opts.Models = []commonharness.ModelCatalog{
		{HarnessID: "opencode", IDs: []string{"github-copilot/claude-sonnet"}},
	}

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model", "any-value-whatsoever"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("Options.Models with no entry for the resolved harness rejected a model value; want no rejection (stderr=%s)", stderr.String())
	}
}

// TestRunCommand_ModelValidation_UsesResolvedHarness asserts that validation
// keys on the harness resolved from --harness, not on the default harness.
// A model valid for the explicitly selected harness must pass even when it
// would be rejected for the default harness.
func TestRunCommand_ModelValidation_UsesResolvedHarness(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("resolved-harness-suite")}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)
	// "github-copilot/claude-sonnet" is valid only for "opencode"; selecting
	// --harness opencode must let it through.

	got := cli.Execute(context.Background(),
		[]string{"run", "suite.yaml", "--harness", "opencode", "--subject-model", "github-copilot/claude-sonnet"},
		opts)

	if got == cli.ExitUsage {
		t.Errorf("--subject-model valid for the resolved harness was rejected as a usage error (stderr=%s)", stderr.String())
	}
}

// TestRunCommand_SubjectModelEmptyEqualsValue_IsUsageError asserts that
// --subject-model= (equals form with an empty value) is a usage error.
// Present-and-empty must be rejected: no harness accepts an empty model
// identifier, and absent-flag is the "not chosen" signal, not empty string.
func TestRunCommand_SubjectModelEmptyEqualsValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model="}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --subject-model= (empty value); stderr=%s",
			got, cli.ExitUsage, stderr.String())
	}
}

// TestRunCommand_StubModelEmptyEqualsValue_IsUsageError asserts that
// --stub-model= (equals form with an empty value) is a usage error.
func TestRunCommand_StubModelEmptyEqualsValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--stub-model="}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --stub-model= (empty value); stderr=%s",
			got, cli.ExitUsage, stderr.String())
	}
}

// TestRunCommand_SubjectModelMissingValue_IsUsageError asserts that
// --subject-model supplied as the final argument with no following value is a
// usage error, consistent with every other value-consuming flag.
func TestRunCommand_SubjectModelMissingValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--subject-model"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --subject-model with no value; stderr=%s",
			got, cli.ExitUsage, stderr.String())
	}
}

// TestRunCommand_StubModelMissingValue_IsUsageError asserts that --stub-model
// supplied as the final argument with no following value is a usage error.
func TestRunCommand_StubModelMissingValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--stub-model"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --stub-model with no value; stderr=%s",
			got, cli.ExitUsage, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Defaults: T11.3
// ---------------------------------------------------------------------------

// TestRunCommand_OmittingModelFlags_LeavesOverridesEmpty asserts that when
// neither --subject-model nor --stub-model is supplied, both override fields
// are the empty string (the "not chosen" state) and the run proceeds normally.
func TestRunCommand_OmittingModelFlags_LeavesOverridesEmpty(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var captured preflight.Input
	runner := &fakeSuiteRunner{result: passingResult("no-model-flags-suite")}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), &captured), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got == cli.ExitUsage {
		t.Fatalf("run without model flags returned ExitUsage (stderr=%s)", stderr.String())
	}
	if captured.Overrides.SubjectModel != "" {
		t.Errorf("SubjectModel = %q when --subject-model was not supplied; want empty string (not chosen)", captured.Overrides.SubjectModel)
	}
	if captured.Overrides.StubModel != "" {
		t.Errorf("StubModel = %q when --stub-model was not supplied; want empty string (not chosen)", captured.Overrides.StubModel)
	}
}

// TestRunCommand_OmittingModelFlags_DoesNotBlockOnInput asserts that a run
// without model flags completes without waiting for input. A frontend that
// waits for a human is a frontend that hangs a pipeline; this test pins that
// no interactive prompt is ever issued when the flags are absent.
func TestRunCommand_OmittingModelFlags_DoesNotBlockOnInput(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("no-block-suite")}
	opts := modelCatalogOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	// Execute must return without blocking. The fake runner returns immediately.
	// If Execute were to prompt for a model the fake runner would not unblock it
	// and the test would time out — the successful return here is the proof.
	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("Execute returned ExitUsage when no model flags were given (stderr=%s)", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Flag surface documentation
// ---------------------------------------------------------------------------

// TestFlags_IncludesModelFlags asserts the shared flag specification
// (cli.Flags()) documents both --subject-model and --stub-model, so a user
// running --help learns they exist.
func TestFlags_IncludesModelFlags(t *testing.T) {
	names := map[string]bool{}
	for _, f := range cli.Flags() {
		names[f.Name] = true
	}
	for _, want := range []string{"--subject-model", "--stub-model"} {
		if !names[want] {
			t.Errorf("cli.Flags() does not include %q", want)
		}
	}
}
