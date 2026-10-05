package testrun

import (
	"context"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
)

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
