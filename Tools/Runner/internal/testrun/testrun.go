// Package testrun orchestrates the full test execution flow for Runner test
// automation: deploy the catalog, run selected tests sequentially per harness,
// check results, and produce a structured summary.
//
// The orchestrator ties together the catalog reader (testcatalog), deploy
// invoker (testdeploy), and result checker (testcheck). All external
// dependencies are injected as interfaces so unit tests can use fakes without
// touching real subprocesses or the filesystem.
//
// This package sits at the cli/tui peer layer. It must not be imported by
// session, engine, or domain.
package testrun

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
)

// TestScope identifies which subset of the catalog to run.
type TestScope int

const (
	ScopeSmoke  TestScope = iota // Smoke Set
	ScopeFull                    // Full Suite
	ScopeSingle                  // Single Workflow
	ScopeCustom                  // Custom Selection
)

// TestConfig carries the validated user selections for a test run.
type TestConfig struct {
	// Scope is the selected test scope.
	Scope TestScope

	// Harnesses is the list of harness IDs to test.
	Harnesses []string

	// MosaicRoot is the absolute path to the MOSAIC repo root.
	// The CLI/TUI layer joins MosaicRoot + "Tools/Runner/TestCatalog" to produce
	// the catalogRoot passed to Catalog.Load() and the catalogFolder passed to
	// Deployer.Deploy().
	MosaicRoot string

	// Workspace is the working directory (cwd) for test runs.
	Workspace string

	// Workflows is populated for ScopeSingle and ScopeCustom.
	// For ScopeSingle, contains exactly one entry.
	// For ScopeCustom, contains the user's multi-selection.
	Workflows []string

	// Mode is populated for ScopeSingle when the user explicitly selects a mode.
	// Empty means the catalog declares only one mode (auto-inferred).
	Mode string

	// GHCPPermissionMode is the --ghcp-permission-mode value for ghcp-cli
	// harness runs. Empty when ghcp-cli is not selected.
	GHCPPermissionMode string

	// ResolvedPaths maps harness IDs to their resolved executable paths.
	// When non-nil, Orchestrator.Run populates RunInvocation.ExecutablePath
	// for each harness from this map. A nil map or a missing entry produces
	// an empty ExecutablePath (backwards-compatible zero-value path).
	ResolvedPaths map[string]string
}

// RunInvocation describes one mosaic-run run subprocess invocation.
type RunInvocation struct {
	// WorkflowID is the --workflow flag value.
	WorkflowID string

	// Mode is the --mode flag value.
	Mode string

	// Harness is the --harness flag value.
	Harness string

	// FixturePath is the --input flag value (absolute path to the
	// Fixtures/{workflow}/ directory).
	FixturePath string

	// GHCPPermissionMode is the --ghcp-permission-mode flag value.
	// Empty for non-ghcp-cli harnesses.
	GHCPPermissionMode string

	// ExecutablePath is the path to the harness executable (e.g. claude,
	// opencode). When non-empty, buildRunArgs appends --executable-path
	// followed by this value. Empty means auto-discovery (no flag emitted).
	ExecutablePath string

	// Task is the --task flag value. Set to the format
	// "Test: <workflowID> / <mode> / <harness>" at the construction site.
	Task string

	// PreConsult indicates whether the subprocess should enable
	// pre-consultation. Forwarded as --pre-consult=true or
	// --pre-consult=false to the mosaic-run run subprocess.
	PreConsult bool

	// InfrastructureKeys lists infrastructure agent keys for this workflow.
	// Follows nil/empty/populated semantics matching buildDeployArgs:
	//   - nil:        omit --infrastructure entirely (backwards-compat)
	//   - []string{}: emit --infrastructure= (empty value, no agents active)
	//   - populated:  emit --infrastructure=k1,k2
	// Populated by Orchestrator.Run from CatalogEntry.InfrastructureAgents.
	// Since Stage 2 guarantees CatalogEntry.InfrastructureAgents is always
	// non-nil, InfrastructureKeys is always non-nil in practice.
	InfrastructureKeys []string

	// Checkpoints is the --checkpoints flag value: "enabled" or "disabled".
	// When empty, buildRunArgs emits "disabled" (defense-in-depth; Stage 2
	// guarantees CatalogEntry.Checkpoints is already "disabled" when absent).
	Checkpoints string

	// Commits is the --commits flag value: "enabled" or "disabled".
	// When empty, buildRunArgs emits "disabled" (same defense-in-depth note).
	Commits string
}

// InvokeResult carries the outcome of a single subprocess invocation.
// ExitCode and ChildStderr are populated on every path that ran a subprocess.
// RunFolder and DispatchLogPath are populated only on the success path.
type InvokeResult struct {
	// ExitCode is the child process exit code. Zero when the subprocess never
	// started (binary not found, OS error).
	ExitCode int

	// RunFolder is the path to the Orchestration-{run_id}/ directory created
	// by the subprocess. Empty on failure.
	RunFolder string

	// DispatchLogPath is the path to the dispatch log file for this run.
	// Empty on failure.
	DispatchLogPath string

	// ChildStderr is the raw stderr output captured from the child process.
	// Empty when the subprocess never started. No truncation at the data layer;
	// display truncation is handled by reporters.
	ChildStderr []byte
}

// TestRunResult is the outcome of one (workflow, mode, harness) test run.
type TestRunResult struct {
	// WorkflowID is the workflow that was run.
	WorkflowID string

	// Mode is the execution mode used.
	Mode string

	// Harness is the harness ID tested.
	Harness string

	// Pass is true if both exit code and dispatch sequence matched.
	Pass bool

	// Mismatch describes the first divergence. Nil when Pass is true.
	Mismatch *testcheck.Mismatch

	// RunFolder is the path to the run's Orchestration-{run_id}/ folder.
	// Empty if the run invocation failed before creating a folder.
	RunFolder string

	// DispatchLogPath is the path to the dispatch log file for this run.
	// Empty if the run invocation failed before creating a folder.
	DispatchLogPath string

	// ActualExitCode is the process exit code from the mosaic-run run subprocess.
	ActualExitCode int

	// Error is non-nil when the test infrastructure itself failed
	// (e.g. sidecar not found, dispatch log unreadable), as opposed to
	// a test mismatch (which is reported via Mismatch).
	Error error

	// ChildStderr is the child process stderr output, converted from []byte to
	// string by the Orchestrator. Stored in full with no truncation at the data
	// layer. Display truncation is handled by reporters.
	// Empty when no subprocess ran or stderr was empty.
	ChildStderr string
}

// HarnessResults groups test results for one harness.
type HarnessResults struct {
	// Harness is the harness ID.
	Harness string

	// Results is the list of individual test run results for this harness,
	// in the order they were run.
	Results []TestRunResult

	// PassCount is the number of passing tests.
	PassCount int

	// FailCount is the number of failing tests (mismatch, not infra error).
	FailCount int

	// ErrorCount is the number of infrastructure errors.
	ErrorCount int
}

// TestSummary is the complete result of a test orchestration run.
type TestSummary struct {
	// AllPass is true if every test in every harness passed.
	AllPass bool

	// DeployError is non-nil if deployment failed (no tests were run).
	DeployError error

	// HarnessResults is the per-harness result list, in the order harnesses
	// were tested.
	HarnessResults []HarnessResults

	// TotalPass is the count of passing tests across all harnesses.
	TotalPass int

	// TotalFail is the count of failing tests across all harnesses.
	TotalFail int

	// TotalError is the count of infrastructure errors across all harnesses.
	TotalError int

	// LogPath is the path to the test-flow debug log file.
	// Set by wiring; empty when unavailable.
	LogPath string

	// ResolvedPaths is the per-harness executable path map used for this run.
	// Populated by wiring from TestConfig.ResolvedPaths so the TUI can display
	// which binary was used for each harness. Nil when no resolution was performed.
	ResolvedPaths map[string]string
}

// CatalogPort is the interface the orchestrator uses for catalog queries.
// The production implementation is *testcatalog.Catalog. Tests inject a fake
// that returns canned entries without touching the filesystem.
type CatalogPort interface {
	// Workflows returns all catalog entries, sorted by workflow ID.
	Workflows() []testcatalog.CatalogEntry

	// SmokeSet returns only the (workflow, mode) combinations that belong
	// to the Smoke Set, sorted by workflow ID then mode.
	SmokeSet() []testcatalog.CatalogEntry

	// FullSuite returns every (workflow, mode) combination in the catalog,
	// sorted by workflow ID then mode.
	FullSuite() []testcatalog.CatalogEntry

	// WorkflowByID returns all CatalogEntry values for the given workflow
	// ID (one per declared mode, sorted by mode), or an error if not found.
	WorkflowByID(id string) ([]testcatalog.CatalogEntry, error)

	// WorkflowModes returns the declared modes for a workflow, or an error
	// if the workflow ID is not found.
	WorkflowModes(id string) ([]string, error)

	// WorkflowIDs returns the sorted list of all workflow IDs in the catalog.
	WorkflowIDs() []string

	// SidecarPath returns the expected-outcome sidecar path for a
	// (workflow, mode) pair. The path is under the catalog root (FR-26).
	SidecarPath(workflowID string, mode string) string

	// UnionInfrastructureAgentKeys returns the sorted, deduplicated union of
	// infrastructure_agents declared across all workflows in the catalog.
	// Always returns a non-nil slice: []string{} when no workflow declares any
	// agents. This is critical: nil would cause buildDeployArgs to omit
	// --infrastructure, deploying the default set instead of an empty one.
	UnionInfrastructureAgentKeys() []string
}

// DeployerPort is the interface the orchestrator uses for deployment.
type DeployerPort interface {
	// Deploy deploys the test catalog into the workspace for the given
	// harnesses. infrastructureKeys follows nil/empty/populated semantics:
	// nil omits --infrastructure (deploy tool asks interactively), non-nil
	// empty emits --infrastructure "", populated emits --infrastructure k1,k2,...
	Deploy(ctx context.Context, catalogFolder string,
		mosaicRoot string, workspace string, harnesses []string,
		workflows []string, infrastructureKeys []string) error
}

// CheckerPort is the interface the orchestrator uses for result checking.
type CheckerPort interface {
	Check(actual testcheck.CheckInput, expected *testcheck.ExpectedOutcome) testcheck.CheckResult
	LoadExpected(path string) (*testcheck.ExpectedOutcome, error)
}

// RunInvoker executes a single mosaic-run run invocation and returns the
// result. Implementations may shell out to the mosaic-run binary as a
// subprocess.
type RunInvoker interface {
	// Invoke runs a single test workflow and returns an InvokeResult and an
	// optional error.
	//
	// On success (err == nil): InvokeResult is fully populated with ExitCode,
	// RunFolder, DispatchLogPath, and ChildStderr.
	//
	// On failure (err != nil): InvokeResult is partially populated. ExitCode
	// and ChildStderr reflect whatever the subprocess produced (both zero-valued
	// when the subprocess never started). The error text includes a bounded
	// stderr excerpt for self-contained diagnostics.
	//
	// Callers MUST read InvokeResult fields on BOTH paths (err nil and non-nil)
	// to populate TestRunResult.ActualExitCode and TestRunResult.ChildStderr.
	Invoke(ctx context.Context, inv RunInvocation) (InvokeResult, error)
}

// ProgressReporter receives state transitions during test execution.
// Implementations may update a TUI, print to stdout (CLI), or record
// transitions for testing.
type ProgressReporter interface {
	// OnDeployStart is called when catalog deployment begins.
	OnDeployStart()

	// OnDeployDone is called when deployment completes (err is nil on success).
	OnDeployDone(err error)

	// OnTestStart is called when a single test run begins.
	OnTestStart(harness string, workflow string, mode string)

	// OnTestDone is called when a single test run completes with its result.
	OnTestDone(harness string, workflow string, mode string, result TestRunResult)
}

// ResolvedPathsReporter is an optional extension to ProgressReporter.
// Implementations that display resolved paths during the test-running phase
// implement this interface. The TUI factory type-asserts the reporter against
// it after resolution succeeds, before constructing the orchestrator.
//
// This is a separate interface (not added to ProgressReporter) to avoid
// breaking existing ProgressReporter implementers.
type ResolvedPathsReporter interface {
	// OnResolvedPaths is called once after harness binary resolution succeeds
	// and before any OnDeployStart call. paths is never nil but may be empty
	// (all-"fake" harnesses). harnessOrder is the display-order slice
	// (no "fake", no duplicates).
	OnResolvedPaths(paths map[string]string, harnessOrder []string)
}

// OrchestratorDeps bundles the injected dependencies for the Orchestrator.
// All fields are interfaces so unit tests can inject fakes without constructing
// real filesystem-backed objects.
type OrchestratorDeps struct {
	Catalog    CatalogPort
	Deployer   DeployerPort
	RunInvoker RunInvoker
	Checker    CheckerPort
	Reporter   ProgressReporter
}

// Orchestrator coordinates the full test execution flow: deploy, run, check,
// and summarize. Obtain via NewOrchestrator.
type Orchestrator struct {
	deps OrchestratorDeps
}

// NewOrchestrator creates an Orchestrator with the given dependencies.
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	return &Orchestrator{deps: deps}
}

// Run executes the test flow:
//  1. Deploy the catalog into the workspace for all selected harnesses.
//  2. For each harness, for each (workflow, mode) in the selected scope,
//     invoke mosaic-run run and check the result.
//  3. Collect all results into a TestSummary.
//
// Deploy failure stops execution immediately. Individual test failures do not
// short-circuit -- all remaining tests are run.
func (o *Orchestrator) Run(ctx context.Context, cfg TestConfig) (*TestSummary, error) {
	summary := &TestSummary{}

	// Step 1: Deploy the catalog once for all harnesses.
	catFolder := catalogFolder(cfg.MosaicRoot)
	workflowIDs := o.deps.Catalog.WorkflowIDs()

	o.deps.Reporter.OnDeployStart()
	deployErr := o.deps.Deployer.Deploy(ctx, catFolder, cfg.MosaicRoot, cfg.Workspace, cfg.Harnesses, workflowIDs, o.deps.Catalog.UnionInfrastructureAgentKeys())
	o.deps.Reporter.OnDeployDone(deployErr)

	if deployErr != nil {
		summary.DeployError = deployErr
		summary.AllPass = false
		return summary, nil
	}

	// Step 2: Resolve the selected scope to a list of (workflow, mode) entries.
	entries, err := resolveScope(o.deps.Catalog, cfg)
	if err != nil {
		return nil, err
	}

	// Step 3: Run each entry for each harness. Do not short-circuit on failure.
	allPass := true
	for _, harness := range cfg.Harnesses {
		hr := HarnessResults{Harness: harness}

		// GHCP permission mode is forwarded only to the ghcp-cli harness.
		ghcpMode := ""
		if isGHCPCLI(harness) {
			ghcpMode = cfg.GHCPPermissionMode
		}

		for _, entry := range entries {
			o.deps.Reporter.OnTestStart(harness, entry.WorkflowID, entry.Mode)

			result := TestRunResult{
				WorkflowID: entry.WorkflowID,
				Mode:       entry.Mode,
				Harness:    harness,
			}

			inv := RunInvocation{
				WorkflowID:         entry.WorkflowID,
				Mode:               entry.Mode,
				Harness:            harness,
				FixturePath:        entry.FixturePath,
				GHCPPermissionMode: ghcpMode,
				Task:               fmt.Sprintf("Test: %s / %s / %s", entry.WorkflowID, entry.Mode, harness),
				PreConsult:         entry.PreConsult,
				ExecutablePath:     cfg.ResolvedPaths[harness],
				InfrastructureKeys: entry.InfrastructureAgents,
				Checkpoints:        entry.Checkpoints,
				Commits:            entry.Commits,
			}

			invokeResult, invokeErr := o.deps.RunInvoker.Invoke(ctx, inv)
			// Always populate ActualExitCode and ChildStderr from InvokeResult,
			// regardless of whether an error occurred (FR-4).
			result.ActualExitCode = invokeResult.ExitCode
			result.ChildStderr = string(invokeResult.ChildStderr)
			if invokeErr != nil {
				// Infrastructure failure: the subprocess could not run or
				// discovery failed. Diagnostic fields are populated above.
				result.Error = invokeErr
				result.Pass = false
				hr.ErrorCount++
				allPass = false
			} else {
				result.RunFolder = invokeResult.RunFolder
				result.DispatchLogPath = invokeResult.DispatchLogPath

				// Load the expected outcome sidecar from the catalog root (FR-26).
				sidecarPath := o.deps.Catalog.SidecarPath(entry.WorkflowID, entry.Mode)
				expected, loadErr := o.deps.Checker.LoadExpected(sidecarPath)
				if loadErr != nil {
					// Authoring error: missing or malformed sidecar.
					result.Error = loadErr
					result.Pass = false
					hr.ErrorCount++
					allPass = false
				} else {
					checkInput := testcheck.CheckInput{
						ExitCode:    invokeResult.ExitCode,
						DispatchLog: invokeResult.DispatchLogPath,
					}
					checkResult := o.deps.Checker.Check(checkInput, expected)
					result.Pass = checkResult.Pass
					result.Mismatch = checkResult.Mismatch

					if checkResult.Pass {
						hr.PassCount++
					} else {
						hr.FailCount++
						allPass = false
					}
				}
			}

			o.deps.Reporter.OnTestDone(harness, entry.WorkflowID, entry.Mode, result)
			hr.Results = append(hr.Results, result)
		}

		summary.HarnessResults = append(summary.HarnessResults, hr)
		summary.TotalPass += hr.PassCount
		summary.TotalFail += hr.FailCount
		summary.TotalError += hr.ErrorCount
	}

	summary.AllPass = allPass
	return summary, nil
}

// CommandRunner is the injectable seam for subprocess execution.
// The workDir parameter sets the child process working directory (cmd.Dir).
type CommandRunner func(
	ctx context.Context,
	workDir string,
	path string,
	args []string,
) (stdout []byte, stderr []byte, exitCode int, err error)

// RunInvokerOptions configures the SubprocessRunInvoker.
type RunInvokerOptions struct {
	// ExecutablePath is the path to the mosaic-run binary.
	// When empty, auto-discovered via os.Executable().
	ExecutablePath string

	// WorkingDir is the workspace directory (cwd for the subprocess).
	WorkingDir string

	// Timeout per invocation. Zero means no timeout.
	// Note: on context cancellation or timeout, exec.CommandContext kills the
	// subprocess. The killed process exit code is platform-dependent (typically
	// -1 on Unix, 1 on Windows). Tests should not assert a specific exit code
	// value for the timeout/cancellation case.
	Timeout time.Duration

	// Invoke is the subprocess runner. When nil, uses execCommandRunnerRun.
	// Signature includes workDir for setting the child process cwd.
	Invoke CommandRunner

	// DebugLogger is the diagnostic logger for test-flow events.
	// When nil, defaults to domain.NopDebugLogger{} (no logging, no panic).
	DebugLogger domain.DebugLogger
}

// SubprocessRunInvoker implements RunInvoker by shelling out to the
// mosaic-run binary with run subcommand flags.
type SubprocessRunInvoker struct {
	opts RunInvokerOptions

	// discoverBinaryFn, when non-nil, replaces the call to discoverSelfBinary()
	// for determining the mosaic-run executable path. This field is unexported
	// and exists solely for testing the binary-discovery-failure path without
	// requiring os.Executable() to fail.
	//
	// When nil (the default, including all production construction paths),
	// discoverBinary() calls the real discoverSelfBinary() function.
	discoverBinaryFn func() (string, error)
}

// NewSubprocessRunInvoker creates a SubprocessRunInvoker with the given options.
func NewSubprocessRunInvoker(opts RunInvokerOptions) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{opts: opts}
}

// Invoke implements RunInvoker by executing mosaic-run run as a subprocess.
// The actual implementation lives in runinvoker.go.

// DispatchLogPath derives the dispatch log file path from a workspace and run
// ID. This encodes the convention from dispatchlog.go:
//
//	{workspace}/RunnerLogs/{run_id}/{run_id}-dispatch.log
//
// Exported so the orchestrator can also use it for diagnostic purposes.
func DispatchLogPath(workspace string, runID string) string {
	return filepath.Join(workspace, "RunnerLogs", runID, runID+"-dispatch.log")
}

// catalogFolder derives the catalog folder path from a MOSAIC root.
// The catalog lives at <mosaicRoot>/Tools/Runner/TestCatalog.
func catalogFolder(mosaicRoot string) string {
	return filepath.Join(mosaicRoot, "Tools", "Runner", "TestCatalog")
}

// isGHCPCLI reports whether a harness ID is the ghcp-cli harness.
func isGHCPCLI(harness string) bool {
	return strings.EqualFold(harness, "ghcp-cli")
}

// resolveScope returns the CatalogEntry list for the given TestConfig scope.
// For ScopeSmoke, returns cat.SmokeSet().
// For ScopeFull, returns cat.FullSuite().
// For ScopeSingle, returns the entries for the single workflow (filtered to
// the selected mode when cfg.Mode is non-empty).
// For ScopeCustom, returns entries for every workflow in cfg.Workflows.
func resolveScope(cat CatalogPort, cfg TestConfig) ([]testcatalog.CatalogEntry, error) {
	switch cfg.Scope {
	case ScopeSmoke:
		return cat.SmokeSet(), nil
	case ScopeFull:
		return cat.FullSuite(), nil
	case ScopeSingle:
		if len(cfg.Workflows) != 1 {
			return nil, fmt.Errorf("testrun: ScopeSingle requires exactly one workflow, got %d", len(cfg.Workflows))
		}
		entries, err := cat.WorkflowByID(cfg.Workflows[0])
		if err != nil {
			return nil, fmt.Errorf("testrun: workflow %q not found: %w", cfg.Workflows[0], err)
		}
		if cfg.Mode != "" {
			for _, e := range entries {
				if e.Mode == cfg.Mode {
					return []testcatalog.CatalogEntry{e}, nil
				}
			}
			return nil, fmt.Errorf("testrun: mode %q not declared for workflow %q", cfg.Mode, cfg.Workflows[0])
		}
		return entries, nil
	case ScopeCustom:
		var result []testcatalog.CatalogEntry
		for _, id := range cfg.Workflows {
			entries, err := cat.WorkflowByID(id)
			if err != nil {
				return nil, fmt.Errorf("testrun: workflow %q not found: %w", id, err)
			}
			result = append(result, entries...)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("testrun: unknown scope %d", cfg.Scope)
	}
}
