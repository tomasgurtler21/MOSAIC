package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
)

// runTestCLI is a helper that invokes cli.RunTestCommand and captures output.
func runTestCLI(t *testing.T, args []string, workDir string) (exitCode int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.RunTestCommand(context.Background(), args, workDir, &out, &errOut)
	return code, out.String(), errOut.String()
}

// makeCatalogRoot creates a minimal test catalog directory structure in a temp
// dir and returns the MOSAIC root path (the --catalog flag value). The catalog
// contains one workflow with the given ID and modes, and a fixture directory.
func makeCatalogRoot(t *testing.T, workflowID string, modes []string, smokeSet []string) string {
	t.Helper()
	root := t.TempDir()

	// Create the catalog directory structure.
	wfDir := filepath.Join(root, "Tools", "Runner", "TestCatalog", "Workflows", "MosaicTest")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", wfDir, err)
	}

	// Create a fixture directory.
	fixDir := filepath.Join(wfDir, "Fixtures", workflowID)
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", fixDir, err)
	}

	// Write the workflow .md file with proper frontmatter.
	modesYAML := "  - " + strings.Join(modes, "\n  - ")
	smokeYAML := ""
	if len(smokeSet) > 0 {
		smokeYAML = "\nsmoke_set:\n  - " + strings.Join(smokeSet, "\n  - ")
	}
	content := fmt.Sprintf("---\nid: %s\nname: Test Workflow\nmodes:\n%s%s\n---\n",
		workflowID, modesYAML, smokeYAML)

	mdPath := filepath.Join(wfDir, workflowID+".md")
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", mdPath, err)
	}

	return root
}

// ---------------------------------------------------------------------------
// Flag validation: missing required flags
// ---------------------------------------------------------------------------

func TestRunTestCommand_MissingCatalog_ReturnsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, []string{"test", "--suite", "smoke"}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--catalog") {
		t.Errorf("stderr %q does not mention --catalog", errOut)
	}
}

func TestRunTestCommand_NeitherSuiteNorWorkflow_ReturnsUsageError(t *testing.T) {
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{"test", "--catalog", root}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--suite") && !strings.Contains(errOut, "--workflow") {
		t.Errorf("stderr %q does not mention --suite or --workflow", errOut)
	}
}

// ---------------------------------------------------------------------------
// Flag validation: --suite and --workflow mutual exclusivity
// ---------------------------------------------------------------------------

func TestRunTestCommand_SuiteAndWorkflowTogether_ReturnsUsageError(t *testing.T) {
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--suite", "smoke", "--workflow", "smoke-single",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not say \"mutually exclusive\"", errOut)
	}
}

// ---------------------------------------------------------------------------
// Flag validation: --suite value must be "smoke" or "full"
// ---------------------------------------------------------------------------

func TestRunTestCommand_InvalidSuiteValue_ReturnsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", "/does-not-matter", "--suite", "invalid",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "smoke") || !strings.Contains(errOut, "full") {
		t.Errorf("stderr %q does not mention valid suite values (smoke, full)", errOut)
	}
}

// ---------------------------------------------------------------------------
// Flag validation: --mode constraints
// ---------------------------------------------------------------------------

func TestRunTestCommand_ModeWithSuite_ReturnsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", "/does-not-matter", "--suite", "smoke", "--mode", "auto",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--mode") {
		t.Errorf("stderr %q does not mention --mode", errOut)
	}
}

func TestRunTestCommand_ModeWithMultipleWorkflows_ReturnsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", "/does-not-matter",
		"--workflow", "w1", "--workflow", "w2", "--mode", "auto",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--mode") {
		t.Errorf("stderr %q does not mention --mode", errOut)
	}
}

// ---------------------------------------------------------------------------
// Catalog path validation
// ---------------------------------------------------------------------------

func TestRunTestCommand_CatalogPathMissingTestCatalogDir_ReturnsUsageError(t *testing.T) {
	// Point --catalog at a directory that exists but has no Tools/Runner/TestCatalog/.
	emptyRoot := t.TempDir()
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", emptyRoot, "--suite", "smoke",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "catalog not found") && !strings.Contains(errOut, "TestCatalog") {
		t.Errorf("stderr %q does not mention catalog not found or TestCatalog", errOut)
	}
}

func TestRunTestCommand_CatalogPathDoesNotExist_ReturnsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", "/definitely/does/not/exist", "--suite", "smoke",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "catalog not found") && !strings.Contains(errOut, "TestCatalog") {
		t.Errorf("stderr %q does not mention catalog not found or TestCatalog", errOut)
	}
}

// ---------------------------------------------------------------------------
// Catalog-based workflow validation
// ---------------------------------------------------------------------------

func TestRunTestCommand_UnknownWorkflowID_ReturnsUsageError(t *testing.T) {
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "does-not-exist",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "does-not-exist") {
		t.Errorf("stderr %q does not name the unknown workflow", errOut)
	}
	if !strings.Contains(errOut, "available") {
		t.Errorf("stderr %q does not list available workflows", errOut)
	}
}

func TestRunTestCommand_SingleWorkflowMultiModeNoMode_RequiresModeFlag(t *testing.T) {
	// Workflow declares two modes but --mode is not supplied.
	root := makeCatalogRoot(t, "smoke-single", []string{"auto", "auto-review"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "smoke-single",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--mode") {
		t.Errorf("stderr %q does not mention --mode", errOut)
	}
	if !strings.Contains(errOut, "auto") {
		t.Errorf("stderr %q does not list declared modes", errOut)
	}
}

func TestRunTestCommand_SingleWorkflowSingleMode_InfersModeAutomatically(t *testing.T) {
	// Single-mode workflow: --mode should be auto-inferred, not required.
	// We won't actually run the orchestrator (it would need a binary), so we
	// verify that no usage error about --mode is returned. Any error that
	// appears comes from the deploy step (binary not found), which exits with
	// ExitFailure, not ExitUsage.
	//
	// Limitation: this test confirms only that mode auto-inference does not
	// produce a --mode usage error. It cannot confirm that the inferred mode
	// value reaches the orchestrator correctly without end-to-end coverage.
	// End-to-end validation is deferred to manual testing per the stage plan.
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "smoke-single",
		"--harness", "fake",
	}, t.TempDir())
	// Must not return a usage error at all — validation should have passed.
	if code == cli.ExitUsage {
		t.Errorf("single-mode workflow should pass flag validation without --mode; "+
			"got ExitUsage with stderr: %q", errOut)
	}
	// Specifically, the --mode requirement error must not appear.
	if strings.Contains(errOut, "--mode") && strings.Contains(errOut, "required") {
		t.Errorf("stderr indicates --mode is required for a single-mode workflow; "+
			"mode should have been auto-inferred; stderr: %q", errOut)
	}
}

func TestRunTestCommand_SingleWorkflowModeNotDeclared_ReturnsUsageError(t *testing.T) {
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "smoke-single", "--mode", "orchestrated",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "orchestrated") {
		t.Errorf("stderr %q does not name the invalid mode", errOut)
	}
}

// ---------------------------------------------------------------------------
// --ghcp-permission-mode validation (FR-19)
// ---------------------------------------------------------------------------

func TestRunTestCommand_GHCPPermissionModeWithoutGHCPCLI_ReturnsUsageError(t *testing.T) {
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--suite", "smoke",
		"--harness", "fake",
		"--ghcp-permission-mode", "blanket",
	}, t.TempDir())
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "ghcp-cli") {
		t.Errorf("stderr %q does not mention ghcp-cli", errOut)
	}
}

func TestRunTestCommand_GHCPPermissionModeWithGHCPCLIHarness_PassesValidation(t *testing.T) {
	// --ghcp-permission-mode + --harness ghcp-cli should pass validation.
	// The orchestrator will fail (no binary), but validation itself passes.
	root := makeCatalogRoot(t, "smoke-single", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--suite", "smoke",
		"--harness", "ghcp-cli",
		"--ghcp-permission-mode", "blanket",
	}, t.TempDir())
	// The combination must pass flag validation: no ExitUsage for any reason.
	if code == cli.ExitUsage {
		t.Errorf("--ghcp-permission-mode with --harness ghcp-cli should pass flag validation; "+
			"got ExitUsage with stderr: %q", errOut)
	}
	// Specifically, the ghcp-permission-mode validation error must not appear.
	if strings.Contains(errOut, "ghcp-permission-mode") {
		t.Errorf("stderr contains \"ghcp-permission-mode\" validation error; "+
			"this error must not fire when ghcp-cli is selected; stderr: %q", errOut)
	}
}

// ---------------------------------------------------------------------------
// Pre-scan compatibility: AllValueBearingFlagNames covers test flags
// ---------------------------------------------------------------------------

// TestAllValueBearingFlagNames_IncludesTestFlags verifies that
// cli.AllValueBearingFlagNames() includes the value-bearing flags specific to
// the test subcommand (--catalog, --suite). This prevents main.go's pre-scan
// from misidentifying their values as positional arguments.
func TestAllValueBearingFlagNames_IncludesTestFlags(t *testing.T) {
	names := cli.AllValueBearingFlagNames()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	testOnlyFlags := []string{"--catalog", "--suite"}
	for _, flag := range testOnlyFlags {
		if !nameSet[flag] {
			t.Errorf("AllValueBearingFlagNames() does not contain %q; "+
				"test subcommand value-bearing flags must be included so "+
				"--catalog /path is not misidentified as a positional argument", flag)
		}
	}
}

// TestAllValueBearingFlagNames_IncludesRunFlags verifies that
// cli.AllValueBearingFlagNames() is a superset of the run subcommand's
// value-bearing flags.
func TestAllValueBearingFlagNames_IncludesRunFlags(t *testing.T) {
	all := cli.AllValueBearingFlagNames()
	allSet := make(map[string]bool, len(all))
	for _, n := range all {
		allSet[n] = true
	}

	for _, s := range cli.RunFlagSpecs() {
		if s.TakesValue && !allSet[s.Name] {
			t.Errorf("AllValueBearingFlagNames() is missing %q from RunFlagSpecs()", s.Name)
		}
	}
}

// TestAllValueBearingFlagNames_NoBooleanFlags verifies that
// cli.AllValueBearingFlagNames() does not include boolean flags from either
// subcommand. Including a boolean flag would cause hasPositionalArg to skip the
// following token, misidentifying it.
func TestAllValueBearingFlagNames_NoBooleanFlags(t *testing.T) {
	allNames := cli.AllValueBearingFlagNames()
	nameSet := make(map[string]bool, len(allNames))
	for _, n := range allNames {
		nameSet[n] = true
	}

	// Boolean flags from both subcommands should not appear.
	boolFlags := []string{
		"--allow-version-drift",
		"--manual-resolution",
		"--new-run",
		"--pre-consult",
		"--tui",
	}
	for _, flag := range boolFlags {
		if nameSet[flag] {
			t.Errorf("AllValueBearingFlagNames() contains boolean flag %q; "+
				"boolean flags must not be in the value-bearing set", flag)
		}
	}
}

// ---------------------------------------------------------------------------
// TestFlagSpecs arity consistency
// ---------------------------------------------------------------------------

// TestTestFlagSpecs_MatchRegistration verifies that TestFlagSpecs() reports the
// correct arity for every flag registered by RegisterTestFlags. A mismatch
// means the two functions have drifted.
func TestTestFlagSpecs_MatchRegistration(t *testing.T) {
	specs := cli.TestFlagSpecs()
	specMap := make(map[string]bool, len(specs))
	for _, s := range specs {
		specMap[s.Name] = s.TakesValue
	}

	// These are the test-subcommand flags and their expected arity.
	want := map[string]bool{
		"--catalog":             true,
		"--suite":               true,
		"--workflow":            true,
		"--mode":                true,
		"--harness":             true,
		"--ghcp-permission-mode": true,
	}
	for name, wantTakesValue := range want {
		gotTakesValue, ok := specMap[name]
		if !ok {
			t.Errorf("TestFlagSpecs() does not contain %q", name)
			continue
		}
		if gotTakesValue != wantTakesValue {
			t.Errorf("TestFlagSpecs()[%q].TakesValue = %v, want %v", name, gotTakesValue, wantTakesValue)
		}
	}
}

// ---------------------------------------------------------------------------
// Exit code contract
// ---------------------------------------------------------------------------

// TestRunTestCommand_FlagValidationOnly_SmokeScope verifies that a valid
// --suite smoke invocation passes flag validation (does not return ExitUsage).
// The actual exit code will be ExitFailure because no real binary is present
// for the deploy step, but the important invariant is that validation succeeds.
//
// NOTE: The ExitSuccess path (summary.AllPass == true) is covered separately
// in exitcode_whitebox_test.go via a direct call to printTestSummary. An
// end-to-end test asserting code 0 on all-pass requires dependency injection
// into RunTestCommand so a fake orchestrator can return AllPass=true; that is
// a future implementation-level change.
func TestRunTestCommand_FlagValidationOnly_SmokeScope(t *testing.T) {
	root := makeCatalogRoot(t, "w1", []string{"auto"}, []string{"auto"})
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--suite", "smoke",
	}, t.TempDir())
	// The error here will come from the deploy step (binary not found), not from
	// flag validation. So we should NOT see ExitUsage.
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage (%d); flag validation should have passed, got: %q",
			cli.ExitUsage, errOut)
	}
}

func TestRunTestCommand_FlagValidationOnly_FullScope(t *testing.T) {
	root := makeCatalogRoot(t, "w1", []string{"auto"}, nil)
	code, _, _ := runTestCLI(t, []string{
		"test", "--catalog", root, "--suite", "full",
	}, t.TempDir())
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage (%d) for valid --suite=full", cli.ExitUsage)
	}
}

func TestRunTestCommand_FlagValidationOnly_SingleWorkflowWithExplicitMode(t *testing.T) {
	root := makeCatalogRoot(t, "w1", []string{"auto", "auto-review"}, nil)
	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "w1", "--mode", "auto",
	}, t.TempDir())
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage (%d) for valid single-workflow+mode; stderr: %q",
			cli.ExitUsage, errOut)
	}
}

func TestRunTestCommand_FlagValidationOnly_CustomScope(t *testing.T) {
	root := makeCatalogRoot(t, "w1", []string{"auto"}, nil)
	// Add a second workflow to the catalog.
	wfDir := filepath.Join(root, "Tools", "Runner", "TestCatalog", "Workflows", "MosaicTest")
	fixDir := filepath.Join(wfDir, "Fixtures", "w2")
	if err := os.MkdirAll(fixDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", fixDir, err)
	}
	content := "---\nid: w2\nname: Workflow Two\nmodes:\n  - auto\n---\n"
	if err := os.WriteFile(filepath.Join(wfDir, "w2.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	code, _, errOut := runTestCLI(t, []string{
		"test", "--catalog", root, "--workflow", "w1", "--workflow", "w2",
	}, t.TempDir())
	if code == cli.ExitUsage {
		t.Errorf("exit code = ExitUsage (%d) for valid custom scope; stderr: %q",
			cli.ExitUsage, errOut)
	}
}
