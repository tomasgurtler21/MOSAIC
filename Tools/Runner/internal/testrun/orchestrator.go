package testrun

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
)

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
