package testrun

import (
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
