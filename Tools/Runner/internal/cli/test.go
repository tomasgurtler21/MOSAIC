// Package cli — test subcommand for the mosaic-run CLI.
//
// RunTestCommand implements the "test" subcommand, gated behind the --dev flag
// in main.go. It parses and validates the test-specific flags, loads the MOSAIC
// test catalog, constructs the test orchestrator, runs all selected tests, and
// exits with code 0 (all pass) or 1 (any failure or error).
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mosaic-run/internal/debuglog"
	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testdeploy"
	"mosaic-run/internal/testrun"
)

// resolveAndAnnounce is the package-level seam for harness binary resolution.
// Tests override this variable to inject failures without executing real resolution.
var resolveAndAnnounce = testrun.ResolveAndAnnounce

// RunTestCommand executes the "test" subcommand entry point.
//
// workDir is the working directory used as the test workspace (where deployed
// files and run logs are written). When empty, os.Getwd() is called.
//
// args must not contain the entry-point-only "--dev" flag (strip it in main.go
// before calling this function so cobra does not reject it as unknown).
//
// Returns an exit code: ExitSuccess (0) when all tests pass, ExitFailure (1)
// when any test fails or an infrastructure error occurs, ExitUsage (3) for
// flag or validation errors.
func RunTestCommand(ctx context.Context, args []string, workDir string, out, errOut io.Writer) int {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "error: getting working directory: %v\n", err)
			return ExitFailure
		}
	}

	// exitCode is captured by the RunE closure and returned after Execute().
	exitCode := ExitUsage

	root := &cobra.Command{
		Use:           "mosaic-run",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("a subcommand is required: test")
		},
	}
	root.SetOut(errOut)
	root.SetErr(errOut)

	testCmd := &cobra.Command{
		Use:           "test",
		Short:         "Run automated tests against the MosaicTest catalog (requires --dev)",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exitCode = runTestSubcmd(ctx, cmd, workDir, out, errOut)
			return nil
		},
	}
	RegisterTestFlags(testCmd.Flags())

	root.AddCommand(testCmd)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return ExitUsage
	}
	return exitCode
}

// runTestSubcmd implements the body of the test subcommand's RunE. It reads and
// validates flags, loads the catalog, builds dependencies, runs the orchestrator,
// prints a structured summary, and returns an exit code.
func runTestSubcmd(ctx context.Context, cmd *cobra.Command, workDir string, out, errOut io.Writer) int {
	// Read all flags.
	catalogPath, _ := cmd.Flags().GetString("catalog")
	suite, _ := cmd.Flags().GetString("suite")
	workflowsRaw, _ := cmd.Flags().GetStringArray("workflow")
	mode, _ := cmd.Flags().GetString("mode")
	harnessesRaw, _ := cmd.Flags().GetStringArray("harness")
	ghcpPermissionMode, _ := cmd.Flags().GetString("ghcp-permission-mode")

	// pflag's StringArray returns a non-nil empty slice for unset flags with a
	// nil default. Filter empty strings so "not supplied" is a nil or empty slice.
	workflows := filterEmptyStrings(workflowsRaw)
	harnesses := filterEmptyStrings(harnessesRaw)

	// Validate: --catalog is required.
	if catalogPath == "" {
		fmt.Fprintf(errOut, "error: --catalog is required\n")
		return ExitUsage
	}

	// Validate: --suite and --workflow are mutually exclusive.
	if suite != "" && len(workflows) > 0 {
		fmt.Fprintf(errOut, "error: --suite and --workflow are mutually exclusive\n")
		return ExitUsage
	}

	// Validate: at least one of --suite or --workflow is required.
	if suite == "" && len(workflows) == 0 {
		fmt.Fprintf(errOut, "error: --suite or --workflow is required\n")
		return ExitUsage
	}

	// Validate: --mode is not valid with --suite.
	if mode != "" && suite != "" {
		fmt.Fprintf(errOut, "error: --mode is not valid with --suite\n")
		return ExitUsage
	}

	// Validate: --mode requires exactly one --workflow.
	if mode != "" && len(workflows) > 1 {
		fmt.Fprintf(errOut, "error: --mode is only valid with exactly one --workflow\n")
		return ExitUsage
	}

	// Validate: --suite must be "smoke" or "full".
	if suite != "" && suite != "smoke" && suite != "full" {
		fmt.Fprintf(errOut, "error: --suite must be \"smoke\" or \"full\", got %q\n", suite)
		return ExitUsage
	}

	// Validate: catalog path must contain Tools/Runner/TestCatalog/.
	catalogRoot := filepath.Join(catalogPath, "Tools", "Runner", "TestCatalog")
	if _, err := os.Stat(catalogRoot); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, "error: catalog not found: %s does not exist under %q\n",
				filepath.Join("Tools", "Runner", "TestCatalog"), catalogPath)
		} else {
			fmt.Fprintf(errOut, "error: checking catalog path %q: %v\n", catalogRoot, err)
		}
		return ExitUsage
	}

	// Resolve harnesses: default to all CLI harnesses when none are specified.
	// DefaultTestHarnesses() is defined in flagspecs.go which is already
	// whitelisted for internal/harness imports.
	if len(harnesses) == 0 {
		harnesses = DefaultTestHarnesses()
	}

	// Validate: --ghcp-permission-mode requires ghcp-cli to be among selected harnesses.
	if ghcpPermissionMode != "" {
		if !containsGHCPCLI(harnesses) {
			fmt.Fprintf(errOut, "error: --ghcp-permission-mode requires ghcp-cli to be among the selected harnesses\n")
			return ExitUsage
		}
	}

	// Load the catalog to validate workflow IDs and mode declarations.
	cat, err := testcatalog.Load(catalogRoot)
	if err != nil {
		fmt.Fprintf(errOut, "error: loading catalog: %v\n", err)
		return ExitUsage
	}

	// Determine scope and validate workflow/mode combinations against the catalog.
	scope, effectiveMode, validErr := resolveTestScope(suite, workflows, mode, cat, errOut)
	if validErr {
		return ExitUsage
	}

	// Build TestConfig from validated inputs.
	cfg := testrun.TestConfig{
		Scope:              scope,
		Harnesses:          harnesses,
		MosaicRoot:         catalogPath,
		Workspace:          workDir,
		Workflows:          workflows,
		Mode:               effectiveMode,
		GHCPPermissionMode: ghcpPermissionMode,
	}

	// Construct the orchestrator dependencies.
	deployer := testdeploy.New(testdeploy.Options{})
	logger := debuglog.New(workDir)

	// Resolve harness binaries before constructing the orchestrator. On failure,
	// print the error to stderr and return early (fail-fast: no tests run).
	resolvedPaths, resolveErr := resolveAndAnnounce(cfg.Harnesses, exec.LookPath, out, logger)
	if resolveErr != nil {
		fmt.Fprintf(errOut, "error: resolving harness binaries: %v\n", resolveErr)
		return ExitFailure
	}
	cfg.ResolvedPaths = resolvedPaths

	invoker := testrun.NewSubprocessRunInvoker(testrun.RunInvokerOptions{
		WorkingDir:  workDir,
		DebugLogger: logger,
	})
	checker := &testCheckerAdapter{}
	reporter := &cliProgressReporter{out: out}

	orch := testrun.NewOrchestrator(testrun.OrchestratorDeps{
		Catalog:    cat,
		Deployer:   deployer,
		RunInvoker: invoker,
		Checker:    checker,
		Reporter:   reporter,
	})

	summary, runErr := orch.Run(ctx, cfg)
	if runErr != nil {
		fmt.Fprintf(errOut, "error: test orchestration failed: %v\n", runErr)
		return ExitFailure
	}

	printTestSummary(out, summary)
	printLogPath(out, summary, logger.Path())

	if summary.AllPass {
		return ExitSuccess
	}
	return ExitFailure
}

// resolveTestScope determines the TestScope and effective mode from the
// validated flag combination and catalog. It writes error messages to errOut
// and returns true for validErr when validation fails.
func resolveTestScope(suite string, workflows []string, mode string, cat *testcatalog.Catalog, errOut io.Writer) (scope testrun.TestScope, effectiveMode string, validErr bool) {
	switch {
	case suite == "smoke":
		return testrun.ScopeSmoke, "", false

	case suite == "full":
		return testrun.ScopeFull, "", false

	case len(workflows) == 1:
		// Single workflow: validate the workflow ID exists and that mode is correct.
		modes, modeErr := cat.WorkflowModes(workflows[0])
		if modeErr != nil {
			ids := cat.WorkflowIDs()
			fmt.Fprintf(errOut, "error: unknown workflow %q; available: %s\n",
				workflows[0], strings.Join(ids, ", "))
			return 0, "", true
		}

		if mode != "" {
			// Validate that the specified mode is declared by the workflow.
			if !containsString(modes, mode) {
				fmt.Fprintf(errOut, "error: workflow %q does not declare mode %q; declared modes: %s\n",
					workflows[0], mode, strings.Join(modes, ", "))
				return 0, "", true
			}
			return testrun.ScopeSingle, mode, false
		}

		// No mode specified.
		if len(modes) > 1 {
			// Workflow declares multiple modes: --mode is required.
			fmt.Fprintf(errOut, "error: workflow %q declares multiple modes (%s); specify one with --mode\n",
				workflows[0], strings.Join(modes, ", "))
			return 0, "", true
		}

		// Single-mode workflow: auto-infer the mode.
		return testrun.ScopeSingle, modes[0], false

	default:
		// Multiple workflows: ScopeCustom. Validate all workflow IDs.
		for _, wf := range workflows {
			if _, modeErr := cat.WorkflowModes(wf); modeErr != nil {
				ids := cat.WorkflowIDs()
				fmt.Fprintf(errOut, "error: unknown workflow %q; available: %s\n",
					wf, strings.Join(ids, ", "))
				return 0, "", true
			}
		}
		return testrun.ScopeCustom, "", false
	}
}

// filterEmptyStrings removes empty strings from a slice, returning nil when the
// result is empty. This normalises pflag's StringArray behaviour where an unset
// flag with a nil default returns []string{} rather than nil.
func filterEmptyStrings(ss []string) []string {
	var result []string
	for _, s := range ss {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// containsGHCPCLI reports whether "ghcp-cli" is present in the harness list
// (case-insensitive).
func containsGHCPCLI(harnesses []string) bool {
	for _, h := range harnesses {
		if strings.EqualFold(h, "ghcp-cli") {
			return true
		}
	}
	return false
}

// containsString reports whether s is present in the slice.
func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// printTestSummary prints a structured summary of the test run to out.
func printTestSummary(out io.Writer, summary *testrun.TestSummary) {
	fmt.Fprintf(out, "\n--- Test Summary ---\n")
	if summary.DeployError != nil {
		fmt.Fprintf(out, "DEPLOY FAILED: %v\n", summary.DeployError)
		return
	}
	for _, hr := range summary.HarnessResults {
		fmt.Fprintf(out, "\nHarness: %s\n", hr.Harness)
		fmt.Fprintf(out, "  Pass:  %d\n", hr.PassCount)
		fmt.Fprintf(out, "  Fail:  %d\n", hr.FailCount)
		fmt.Fprintf(out, "  Error: %d\n", hr.ErrorCount)
		for _, r := range hr.Results {
			status := "PASS"
			detail := ""
			if r.Error != nil {
				status = "ERROR"
				detail = fmt.Sprintf(": %v", r.Error)
			} else if !r.Pass {
				status = "FAIL"
				if r.Mismatch != nil {
					detail = fmt.Sprintf(": %s", r.Mismatch.Message)
				}
			}
			fmt.Fprintf(out, "  [%s] %s/%s%s\n", status, r.WorkflowID, r.Mode, detail)
		}
	}
	fmt.Fprintf(out, "\nTotal: %d pass, %d fail, %d error\n",
		summary.TotalPass, summary.TotalFail, summary.TotalError)
	if summary.AllPass {
		fmt.Fprintf(out, "Result: PASS\n")
	} else {
		fmt.Fprintf(out, "Result: FAIL\n")
	}
}

// printLogPath prints the debug log file path when there were failures and a
// log file exists. All guards (!AllPass, non-empty path, non-nil summary) are
// inside this function so the call site is unconditional.
//
// Mandatory output substring when printed: "Log file: <path>"
func printLogPath(out io.Writer, summary *testrun.TestSummary, logPath string) {
	if summary == nil || summary.AllPass || logPath == "" {
		return
	}
	fmt.Fprintf(out, "Log file: %s\n", logPath)
}

// testCheckerAdapter wraps the testcheck package-level functions as a
// testrun.CheckerPort. This bridges the stateless package API to the interface
// that the orchestrator requires.
type testCheckerAdapter struct{}

func (c *testCheckerAdapter) Check(actual testcheck.CheckInput, expected *testcheck.ExpectedOutcome) testcheck.CheckResult {
	return testcheck.Check(actual, expected)
}

func (c *testCheckerAdapter) LoadExpected(path string) (*testcheck.ExpectedOutcome, error) {
	return testcheck.LoadExpected(path)
}

// cliProgressReporter implements testrun.ProgressReporter for CLI (non-interactive)
// output. Each state transition is printed as a line to out.
type cliProgressReporter struct {
	out io.Writer
}

func (r *cliProgressReporter) OnDeployStart() {
	fmt.Fprintf(r.out, "Deploying test catalog...\n")
}

func (r *cliProgressReporter) OnDeployDone(err error) {
	if err != nil {
		fmt.Fprintf(r.out, "Deploy failed: %v\n", err)
	} else {
		fmt.Fprintf(r.out, "Deploy complete.\n")
	}
}

func (r *cliProgressReporter) OnTestStart(harnessID string, workflow string, mode string) {
	fmt.Fprintf(r.out, "  Running: %s / %s / %s...\n", harnessID, workflow, mode)
}

func (r *cliProgressReporter) OnTestDone(harnessID string, workflow string, mode string, result testrun.TestRunResult) {
	switch {
	case result.Error != nil:
		fmt.Fprintf(r.out, "  ERROR:   %s / %s / %s: %v\n", harnessID, workflow, mode, result.Error)
		// Always show exit code on ERROR (even when 0: diagnostic clue).
		fmt.Fprintf(r.out, "  exit code: %d\n", result.ActualExitCode)
		// Show stderr block when non-empty.
		if result.ChildStderr != "" {
			fmt.Fprintf(r.out, "  stderr:\n")
			content := testrun.TruncateTail(result.ChildStderr, testrun.MaxStderrDisplayBytes)
			fmt.Fprintf(r.out, "%s", content)
			if len(content) > 0 && content[len(content)-1] != '\n' {
				fmt.Fprintf(r.out, "\n")
			}
		}
	case result.Pass:
		fmt.Fprintf(r.out, "  PASS:    %s / %s / %s\n", harnessID, workflow, mode)
	default:
		msg := ""
		if result.Mismatch != nil {
			msg = result.Mismatch.Message
		}
		fmt.Fprintf(r.out, "  FAIL:    %s / %s / %s: %s\n", harnessID, workflow, mode, msg)
		// Show exit code on FAIL when non-zero.
		if result.ActualExitCode != 0 {
			fmt.Fprintf(r.out, "  exit code: %d\n", result.ActualExitCode)
		}
		// Show stderr block when non-empty (independent of exit code).
		if result.ChildStderr != "" {
			fmt.Fprintf(r.out, "  stderr:\n")
			content := testrun.TruncateTail(result.ChildStderr, testrun.MaxStderrDisplayBytes)
			fmt.Fprintf(r.out, "%s", content)
			if len(content) > 0 && content[len(content)-1] != '\n' {
				fmt.Fprintf(r.out, "\n")
			}
		}
	}
}
