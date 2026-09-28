package testrun_test

// Tests for the test orchestrator package. The test suite covers:
//
//   - Full smoke-set run: deploy once, then runs for every smoke entry, each
//     checked, all pass -> overall pass with correct counts.
//   - Single workflow run, single harness, passing: deploy, run, check -> pass.
//   - Single workflow run, single harness, failing: deploy, run, check -> fail
//     with mismatch detail preserved in the summary.
//   - Multi-harness execution: deploy once covering all harnesses, then each
//     harness's entries are run sequentially; results are grouped per harness.
//   - Deploy failure: deployer returns error -> orchestrator stops, no run
//     invocations are made, TestSummary.DeployError is set.
//   - Partial test failure: some runs pass, some fail -> all runs execute, the
//     summary records each pass/fail with the correct mismatch detail for failures.
//   - Run invocation failure before folder creation: error is captured in
//     TestRunResult.Error; remaining tests continue.
//   - Non-zero exit code matched by expected: when the checker returns Pass=true
//     for a non-zero exit code, the result is a pass.
//   - Progress reporting: OnDeployStart, OnDeployDone, OnTestStart, OnTestDone
//     are called in the correct order with the correct arguments.
//   - GHCP permission mode forwarded: RunInvocation.GHCPPermissionMode matches
//     TestConfig.GHCPPermissionMode for ghcp-cli harness runs.
//   - Checker receives sidecar paths from catalog root, not workspace: the path
//     passed to CheckerPort.LoadExpected is the value returned by
//     CatalogPort.SidecarPath, never a path derived from the workspace.
//   - Summary counters: TotalPass, TotalFail, TotalError and per-harness
//     PassCount, FailCount, ErrorCount reflect actual outcomes.
//   - AllPass is false when any test fails or errors.
//   - AllPass is true when every test passes across all harnesses.
//   - HarnessResults order matches the order of harnesses in TestConfig.
//
// All external dependencies are fakes. No real subprocesses, catalog files, or
// sidecar JSON files are needed.

import (
	"context"
	"errors"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// ---------------------------------------------------------------------------
// Fake implementations of the four dependency interfaces.
// ---------------------------------------------------------------------------

// MockCatalog is a CatalogPort fake that returns canned data.
type MockCatalog struct {
	smokeSet       []testcatalog.CatalogEntry
	fullSuite      []testcatalog.CatalogEntry
	workflowByIDFn func(id string) ([]testcatalog.CatalogEntry, error)
	workflowIDs    []string
	// sidecarPaths maps "workflowID:mode" to the sidecar path to return.
	sidecarPaths map[string]string
	// infraKeys is the value returned by InfrastructureAgentKeys (deprecated).
	// nil means "don't know" (not set); []string{} means "explicitly none".
	infraKeys []string
	// unionInfraKeys is the value returned by UnionInfrastructureAgentKeys.
	// When nil (zero value), UnionInfrastructureAgentKeys returns []string{}
	// to match the real implementation's non-nil contract.
	unionInfraKeys []string
}

func (f *MockCatalog) Workflows() []testcatalog.CatalogEntry { return f.fullSuite }
func (f *MockCatalog) SmokeSet() []testcatalog.CatalogEntry  { return f.smokeSet }
func (f *MockCatalog) FullSuite() []testcatalog.CatalogEntry { return f.fullSuite }
func (f *MockCatalog) WorkflowIDs() []string                 { return f.workflowIDs }

func (f *MockCatalog) WorkflowByID(id string) ([]testcatalog.CatalogEntry, error) {
	if f.workflowByIDFn != nil {
		return f.workflowByIDFn(id)
	}
	return nil, errors.New("MockCatalog: WorkflowByID not configured")
}

func (f *MockCatalog) WorkflowModes(id string) ([]string, error) {
	entries, err := f.WorkflowByID(id)
	if err != nil {
		return nil, err
	}
	modes := make([]string, len(entries))
	for i, e := range entries {
		modes[i] = e.Mode
	}
	return modes, nil
}

func (f *MockCatalog) SidecarPath(workflowID string, mode string) string {
	if f.sidecarPaths != nil {
		key := workflowID + ":" + mode
		if p, ok := f.sidecarPaths[key]; ok {
			return p
		}
	}
	return "/catalog/Workflows/MosaicTest/" + workflowID + "-" + mode + ".expected.json"
}

// UnionInfrastructureAgentKeys returns the union infra keys configured on the
// fake. When unionInfraKeys is nil (the zero value), returns []string{} to
// match the real Catalog.UnionInfrastructureAgentKeys() non-nil contract.
func (f *MockCatalog) UnionInfrastructureAgentKeys() []string {
	if f.unionInfraKeys == nil {
		return []string{}
	}
	return f.unionInfraKeys
}

// MockDeployer is a DeployerPort fake. It records calls and returns a
// configured error.
type MockDeployer struct {
	callCount    int
	capturedArgs []deployCall
	err          error
}

type deployCall struct {
	catalogFolder      string
	mosaicRoot         string
	workspace          string
	harnesses          []string
	workflows          []string
	infrastructureKeys []string
}

func (f *MockDeployer) Deploy(ctx context.Context, catalogFolder string,
	mosaicRoot string, workspace string, harnesses []string,
	workflows []string, infrastructureKeys []string) error {
	f.callCount++
	var infraCopy []string
	if infrastructureKeys != nil {
		infraCopy = make([]string, len(infrastructureKeys))
		copy(infraCopy, infrastructureKeys)
	}
	f.capturedArgs = append(f.capturedArgs, deployCall{
		catalogFolder:      catalogFolder,
		mosaicRoot:         mosaicRoot,
		workspace:          workspace,
		harnesses:          append([]string(nil), harnesses...),
		workflows:          append([]string(nil), workflows...),
		infrastructureKeys: infraCopy,
	})
	return f.err
}

// MockRunInvoker is a RunInvoker fake. It records calls and returns canned
// results via a per-call function or a fixed result.
type MockRunInvoker struct {
	calls   []testrun.RunInvocation
	results []invokeResult // one result per call, in order
}

type invokeResult struct {
	exitCode        int
	runFolder       string
	dispatchLogPath string
	childStderr     []byte
	err             error
}

func (f *MockRunInvoker) Invoke(ctx context.Context, inv testrun.RunInvocation) (testrun.InvokeResult, error) {
	f.calls = append(f.calls, inv)
	idx := len(f.calls) - 1
	if idx < len(f.results) {
		r := f.results[idx]
		return testrun.InvokeResult{
			ExitCode:        r.exitCode,
			RunFolder:       r.runFolder,
			DispatchLogPath: r.dispatchLogPath,
			ChildStderr:     r.childStderr,
		}, r.err
	}
	// Default: success, empty paths.
	return testrun.InvokeResult{
		ExitCode:        0,
		RunFolder:       "/workspace/Orchestration-run-1",
		DispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log",
	}, nil
}

// MockChecker is a CheckerPort fake. It records LoadExpected paths and returns
// canned results via per-call slices.
type MockChecker struct {
	loadExpectedPaths []string
	loadResults       []*testcheck.ExpectedOutcome // one per LoadExpected call
	loadErrors        []error                      // one per LoadExpected call
	checkResults      []testcheck.CheckResult      // one per Check call
}

func (f *MockChecker) LoadExpected(path string) (*testcheck.ExpectedOutcome, error) {
	f.loadExpectedPaths = append(f.loadExpectedPaths, path)
	idx := len(f.loadExpectedPaths) - 1
	var outcome *testcheck.ExpectedOutcome
	var err error
	if idx < len(f.loadResults) {
		outcome = f.loadResults[idx]
	}
	if idx < len(f.loadErrors) {
		err = f.loadErrors[idx]
	}
	if outcome == nil && err == nil {
		outcome = &testcheck.ExpectedOutcome{ExitCode: 0}
	}
	return outcome, err
}

func (f *MockChecker) Check(actual testcheck.CheckInput, expected *testcheck.ExpectedOutcome) testcheck.CheckResult {
	idx := len(f.loadExpectedPaths) - 1
	if idx < len(f.checkResults) {
		return f.checkResults[idx]
	}
	return testcheck.CheckResult{Pass: true}
}

// MockReporter records every ProgressReporter call.
type MockReporter struct {
	events []progressEvent
}

type progressEvent struct {
	kind     string
	harness  string
	workflow string
	mode     string
	err      error
	result   testrun.TestRunResult
}

func (f *MockReporter) OnDeployStart() {
	f.events = append(f.events, progressEvent{kind: "deploy_start"})
}

func (f *MockReporter) OnDeployDone(err error) {
	f.events = append(f.events, progressEvent{kind: "deploy_done", err: err})
}

func (f *MockReporter) OnTestStart(harness, workflow, mode string) {
	f.events = append(f.events, progressEvent{
		kind: "test_start", harness: harness, workflow: workflow, mode: mode,
	})
}

func (f *MockReporter) OnTestDone(harness, workflow, mode string, result testrun.TestRunResult) {
	f.events = append(f.events, progressEvent{
		kind: "test_done", harness: harness, workflow: workflow, mode: mode, result: result,
	})
}

// ---------------------------------------------------------------------------
// Test helpers.
// ---------------------------------------------------------------------------

// smokeEntries returns a slice of CatalogEntry values representing a typical
// smoke set: two workflows, each with one smoke-set mode.
func smokeEntries() []testcatalog.CatalogEntry {
	return []testcatalog.CatalogEntry{
		{
			WorkflowID:  "smoke-single",
			Mode:        "auto",
			FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
			AllModes:    []string{"auto"},
			InSmokeSet:  true,
		},
		{
			WorkflowID:  "smoke-double",
			Mode:        "auto",
			FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-double",
			AllModes:    []string{"auto", "auto-review"},
			InSmokeSet:  true,
		},
		{
			WorkflowID:  "smoke-double",
			Mode:        "auto-review",
			FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-double",
			AllModes:    []string{"auto", "auto-review"},
			InSmokeSet:  true,
		},
		{
			WorkflowID:  "findings-loop",
			Mode:        "auto",
			FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/findings-loop",
			AllModes:    []string{"auto"},
			InSmokeSet:  true,
		},
	}
}

// defaultPassingInvokeResult returns an invokeResult that simulates a
// successful subprocess invocation.
func defaultPassingInvokeResult(index int) invokeResult {
	runID := "run-" + string(rune('0'+index))
	return invokeResult{
		exitCode:        0,
		runFolder:       "/workspace/Orchestration-" + runID,
		dispatchLogPath: "/workspace/RunnerLogs/" + runID + "/" + runID + "-dispatch.log",
	}
}

// newOrchestrator constructs an Orchestrator with the given fakes.
func newOrchestrator(cat testrun.CatalogPort, dep *MockDeployer, inv *MockRunInvoker, chk *MockChecker, rep testrun.ProgressReporter) *testrun.Orchestrator {
	return testrun.NewOrchestrator(testrun.OrchestratorDeps{
		Catalog:    cat,
		Deployer:   dep,
		RunInvoker: inv,
		Checker:    chk,
		Reporter:   rep,
	})
}

// =============================================================================
// Full smoke-set run: all pass
