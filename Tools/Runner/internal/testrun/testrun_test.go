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
	"path/filepath"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// ---------------------------------------------------------------------------
// Fake implementations of the four dependency interfaces.
// ---------------------------------------------------------------------------

// fakeCatalog is a CatalogPort fake that returns canned data.
type fakeCatalog struct {
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

func (f *fakeCatalog) Workflows() []testcatalog.CatalogEntry    { return f.fullSuite }
func (f *fakeCatalog) SmokeSet() []testcatalog.CatalogEntry     { return f.smokeSet }
func (f *fakeCatalog) FullSuite() []testcatalog.CatalogEntry    { return f.fullSuite }
func (f *fakeCatalog) WorkflowIDs() []string                    { return f.workflowIDs }

func (f *fakeCatalog) WorkflowByID(id string) ([]testcatalog.CatalogEntry, error) {
	if f.workflowByIDFn != nil {
		return f.workflowByIDFn(id)
	}
	return nil, errors.New("fakeCatalog: WorkflowByID not configured")
}

func (f *fakeCatalog) WorkflowModes(id string) ([]string, error) {
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

func (f *fakeCatalog) SidecarPath(workflowID string, mode string) string {
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
func (f *fakeCatalog) UnionInfrastructureAgentKeys() []string {
	if f.unionInfraKeys == nil {
		return []string{}
	}
	return f.unionInfraKeys
}

// fakeDeployer is a DeployerPort fake. It records calls and returns a
// configured error.
type fakeDeployer struct {
	callCount    int
	capturedArgs []deployCall
	err          error
}

type deployCall struct {
	catalogFolder     string
	mosaicRoot        string
	workspace         string
	harnesses         []string
	workflows         []string
	infrastructureKeys []string
}

func (f *fakeDeployer) Deploy(ctx context.Context, catalogFolder string,
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

// fakeRunInvoker is a RunInvoker fake. It records calls and returns canned
// results via a per-call function or a fixed result.
type fakeRunInvoker struct {
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

func (f *fakeRunInvoker) Invoke(ctx context.Context, inv testrun.RunInvocation) (testrun.InvokeResult, error) {
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

// fakeChecker is a CheckerPort fake. It records LoadExpected paths and returns
// canned results via per-call slices.
type fakeChecker struct {
	loadExpectedPaths []string
	loadResults       []*testcheck.ExpectedOutcome // one per LoadExpected call
	loadErrors        []error                      // one per LoadExpected call
	checkResults      []testcheck.CheckResult      // one per Check call
}

func (f *fakeChecker) LoadExpected(path string) (*testcheck.ExpectedOutcome, error) {
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

func (f *fakeChecker) Check(actual testcheck.CheckInput, expected *testcheck.ExpectedOutcome) testcheck.CheckResult {
	idx := len(f.loadExpectedPaths) - 1
	if idx < len(f.checkResults) {
		return f.checkResults[idx]
	}
	return testcheck.CheckResult{Pass: true}
}

// fakeReporter records every ProgressReporter call.
type fakeReporter struct {
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

func (f *fakeReporter) OnDeployStart() {
	f.events = append(f.events, progressEvent{kind: "deploy_start"})
}

func (f *fakeReporter) OnDeployDone(err error) {
	f.events = append(f.events, progressEvent{kind: "deploy_done", err: err})
}

func (f *fakeReporter) OnTestStart(harness, workflow, mode string) {
	f.events = append(f.events, progressEvent{
		kind: "test_start", harness: harness, workflow: workflow, mode: mode,
	})
}

func (f *fakeReporter) OnTestDone(harness, workflow, mode string, result testrun.TestRunResult) {
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
func newOrchestrator(cat testrun.CatalogPort, dep *fakeDeployer, inv *fakeRunInvoker, chk *fakeChecker, rep testrun.ProgressReporter) *testrun.Orchestrator {
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
// =============================================================================

// TestRun_SmokeScope_DeployCalledOnce_ThenAllRunsExecuted verifies that the
// orchestrator deploys exactly once before running smoke-set entries.
func TestRun_SmokeScope_DeployCalledOnce_ThenAllRunsExecuted(t *testing.T) {
	entries := smokeEntries() // 4 entries
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"smoke-single", "smoke-double", "findings-loop"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
		},
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true}, {Pass: true}, {Pass: true}, {Pass: true},
		},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if dep.callCount != 1 {
		t.Errorf("Deploy called %d times, want 1", dep.callCount)
	}
	if len(inv.calls) != 4 {
		t.Errorf("RunInvoker.Invoke called %d times, want 4", len(inv.calls))
	}
	if summary == nil {
		t.Fatal("Run returned nil summary")
	}
}

// TestRun_SmokeScope_AllPass_SummaryAllPassTrue verifies that AllPass is true
// when every smoke-set run passes.
func TestRun_SmokeScope_AllPass_SummaryAllPassTrue(t *testing.T) {
	entries := smokeEntries() // 4 entries
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"smoke-single", "smoke-double", "findings-loop"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
		},
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true}, {Pass: true}, {Pass: true}, {Pass: true},
		},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !summary.AllPass {
		t.Errorf("AllPass = false, want true when all runs pass")
	}
	if summary.TotalPass != 4 {
		t.Errorf("TotalPass = %d, want 4", summary.TotalPass)
	}
	if summary.TotalFail != 0 {
		t.Errorf("TotalFail = %d, want 0", summary.TotalFail)
	}
	if summary.TotalError != 0 {
		t.Errorf("TotalError = %d, want 0", summary.TotalError)
	}
}

// =============================================================================
// Single workflow run: pass and fail
// =============================================================================

// TestRun_SingleScope_Pass_ResultIsPass verifies a single-workflow single-harness
// run that passes.
func TestRun_SingleScope_Pass_ResultIsPass(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
		InSmokeSet:  true,
	}
	cat := &fakeCatalog{
		workflowByIDFn: func(id string) ([]testcatalog.CatalogEntry, error) {
			if id == "smoke-single" {
				return []testcatalog.CatalogEntry{entry}, nil
			}
			return nil, errors.New("not found")
		},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSingle,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
		Workflows:  []string{"smoke-single"},
		Mode:       "auto",
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !summary.AllPass {
		t.Errorf("AllPass = false, want true")
	}
	if summary.TotalPass != 1 {
		t.Errorf("TotalPass = %d, want 1", summary.TotalPass)
	}
	if len(summary.HarnessResults) != 1 {
		t.Fatalf("HarnessResults len = %d, want 1", len(summary.HarnessResults))
	}
	hr := summary.HarnessResults[0]
	if len(hr.Results) != 1 {
		t.Fatalf("HarnessResults[0].Results len = %d, want 1", len(hr.Results))
	}
	if !hr.Results[0].Pass {
		t.Errorf("Results[0].Pass = false, want true")
	}
}

// TestRun_SingleScope_Fail_MismatchDetailPreserved verifies that when a run
// fails, the mismatch detail is preserved in the TestRunResult.
func TestRun_SingleScope_Fail_MismatchDetailPreserved(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
	}
	cat := &fakeCatalog{
		workflowByIDFn: func(id string) ([]testcatalog.CatalogEntry, error) {
			return []testcatalog.CatalogEntry{entry}, nil
		},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	wantMismatch := &testcheck.Mismatch{
		Kind:          testcheck.MismatchExitCode,
		Index:         -1,
		Message:       "exit code mismatch: expected 0, got 1",
		ExpectedValue: "0",
		ActualValue:   "1",
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{{Pass: false, Mismatch: wantMismatch}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSingle,
		Harnesses: []string{"auto"},
		Workflows: []string{"smoke-single"},
		Mode:      "auto",
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if summary.AllPass {
		t.Errorf("AllPass = true, want false when run fails")
	}
	if summary.TotalFail != 1 {
		t.Errorf("TotalFail = %d, want 1", summary.TotalFail)
	}
	if len(summary.HarnessResults) == 0 {
		t.Fatal("HarnessResults is empty")
	}
	if len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("HarnessResults[0].Results is empty")
	}
	result := summary.HarnessResults[0].Results[0]
	if result.Mismatch == nil {
		t.Fatal("Results[0].Mismatch is nil, want mismatch detail")
	}
	if result.Mismatch.Kind != testcheck.MismatchExitCode {
		t.Errorf("Mismatch.Kind = %v, want MismatchExitCode", result.Mismatch.Kind)
	}
	if result.Mismatch.Message != wantMismatch.Message {
		t.Errorf("Mismatch.Message = %q, want %q", result.Mismatch.Message, wantMismatch.Message)
	}
}

// =============================================================================
// Multi-harness execution
// =============================================================================

// TestRun_MultiHarness_RunsExecutedPerHarness verifies that for two harnesses
// with two smoke entries, four run invocations are made (2 entries x 2 harnesses).
func TestRun_MultiHarness_RunsExecutedPerHarness(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"harness-a", "harness-b"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// 2 entries x 2 harnesses = 4 invocations.
	if len(inv.calls) != 4 {
		t.Errorf("RunInvoker.Invoke called %d times, want 4 (2 entries x 2 harnesses)", len(inv.calls))
	}

	// The first two calls belong to harness-a (wf-a then wf-b), so they share
	// the same Harness value but have different WorkflowIDs.
	if len(inv.calls) >= 2 {
		if inv.calls[0].Harness != inv.calls[1].Harness {
			t.Errorf("calls[0].Harness = %q, calls[1].Harness = %q; want same harness for first group",
				inv.calls[0].Harness, inv.calls[1].Harness)
		}
		if inv.calls[0].WorkflowID == inv.calls[1].WorkflowID {
			t.Errorf("calls[0].WorkflowID = calls[1].WorkflowID = %q; want different workflows within one harness group",
				inv.calls[0].WorkflowID)
		}
	}
	_ = summary
}

// TestRun_MultiHarness_ResultsGroupedPerHarness verifies that HarnessResults
// has one entry per harness.
func TestRun_MultiHarness_ResultsGroupedPerHarness(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"harness-a", "harness-b"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(summary.HarnessResults) != 2 {
		t.Errorf("HarnessResults len = %d, want 2 (one per harness)", len(summary.HarnessResults))
	}

	// Order of HarnessResults must match order of harnesses in cfg.Harnesses.
	if len(summary.HarnessResults) >= 1 && summary.HarnessResults[0].Harness != "harness-a" {
		t.Errorf("HarnessResults[0].Harness = %q, want %q", summary.HarnessResults[0].Harness, "harness-a")
	}
	if len(summary.HarnessResults) >= 2 && summary.HarnessResults[1].Harness != "harness-b" {
		t.Errorf("HarnessResults[1].Harness = %q, want %q", summary.HarnessResults[1].Harness, "harness-b")
	}
}

// TestRun_MultiHarness_EachHarnessGetsSeparateResults verifies that each
// harness group contains only the results for that harness.
func TestRun_MultiHarness_EachHarnessGetsSeparateResults(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"harness-a", "harness-b"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(summary.HarnessResults) != 2 {
		t.Fatalf("HarnessResults len = %d, want 2", len(summary.HarnessResults))
	}

	// Each harness group should have 2 results (one per catalog entry).
	for _, hr := range summary.HarnessResults {
		if len(hr.Results) != 2 {
			t.Errorf("HarnessResults[%q].Results len = %d, want 2", hr.Harness, len(hr.Results))
		}
		for _, r := range hr.Results {
			if r.Harness != hr.Harness {
				t.Errorf("result in harness group %q has Harness = %q", hr.Harness, r.Harness)
			}
		}
	}
}

// =============================================================================
// Deploy failure
// =============================================================================

// TestRun_DeployFailure_NoRunsExecuted verifies that when the deployer returns
// an error, no run invocations are made.
func TestRun_DeployFailure_NoRunsExecuted(t *testing.T) {
	entries := smokeEntries()
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"smoke-single", "smoke-double", "findings-loop"},
	}
	deployErr := errors.New("mosaic-deploy: deploy failed: some error")
	dep := &fakeDeployer{err: deployErr}
	inv := &fakeRunInvoker{}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error (want nil err, deploy error in summary): %v", err)
	}
	if len(inv.calls) != 0 {
		t.Errorf("RunInvoker.Invoke called %d times after deploy failure, want 0", len(inv.calls))
	}
	if summary == nil {
		t.Fatal("Run returned nil summary on deploy failure")
	}
}

// TestRun_DeployFailure_SummaryDeployErrorSet verifies that TestSummary.DeployError
// is set to the deployment error.
func TestRun_DeployFailure_SummaryDeployErrorSet(t *testing.T) {
	cat := &fakeCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	deployErr := errors.New("deploy: some failure")
	dep := &fakeDeployer{err: deployErr}
	inv := &fakeRunInvoker{}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, _ := o.Run(context.Background(), cfg)
	if summary.DeployError == nil {
		t.Fatal("TestSummary.DeployError is nil, want deploy error")
	}
	if !errors.Is(summary.DeployError, deployErr) {
		t.Errorf("TestSummary.DeployError = %v, want to wrap %v", summary.DeployError, deployErr)
	}
}

// TestRun_DeployFailure_AllPassIsFalse verifies that AllPass is false when
// deployment fails.
func TestRun_DeployFailure_AllPassIsFalse(t *testing.T) {
	cat := &fakeCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{err: errors.New("deploy failed")}
	inv := &fakeRunInvoker{}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, _ := o.Run(context.Background(), cfg)
	if summary.AllPass {
		t.Errorf("AllPass = true, want false when deployment fails")
	}
}

// =============================================================================
// Partial test failure: no short-circuit
// =============================================================================

// TestRun_PartialFailure_AllRunsExecuted verifies that when one run fails, the
// orchestrator continues executing the remaining runs.
func TestRun_PartialFailure_AllRunsExecuted(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
		{WorkflowID: "wf-c", Mode: "auto", FixturePath: "/fixtures/wf-c"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	failMismatch := &testcheck.Mismatch{Kind: testcheck.MismatchExitCode, Index: -1}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
			{Pass: true},
		},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// All 3 runs must have been invoked despite the second one failing.
	if len(inv.calls) != 3 {
		t.Errorf("RunInvoker.Invoke called %d times, want 3 (no short-circuit)", len(inv.calls))
	}
	if summary.TotalPass != 2 {
		t.Errorf("TotalPass = %d, want 2", summary.TotalPass)
	}
	if summary.TotalFail != 1 {
		t.Errorf("TotalFail = %d, want 1", summary.TotalFail)
	}
	if summary.AllPass {
		t.Errorf("AllPass = true, want false")
	}
}

// TestRun_PartialFailure_FailedResultHasMismatchDetail verifies that the
// failed result carries the mismatch detail from the checker.
func TestRun_PartialFailure_FailedResultHasMismatchDetail(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	failMismatch := &testcheck.Mismatch{
		Kind:          testcheck.MismatchAgent,
		Index:         0,
		Message:       "dispatch[0]: agent mismatch",
		ExpectedValue: "expected-agent",
		ActualValue:   "actual-agent",
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
		},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(summary.HarnessResults) == 0 {
		t.Fatal("HarnessResults is empty")
	}
	results := summary.HarnessResults[0].Results
	if len(results) < 2 {
		t.Fatalf("Results len = %d, want >= 2", len(results))
	}
	// Second result (index 1) should be the failing one.
	failed := results[1]
	if failed.Pass {
		t.Errorf("Results[1].Pass = true, want false")
	}
	if failed.Mismatch == nil {
		t.Fatal("Results[1].Mismatch is nil, want mismatch detail")
	}
	if failed.Mismatch.Kind != testcheck.MismatchAgent {
		t.Errorf("Mismatch.Kind = %v, want MismatchAgent", failed.Mismatch.Kind)
	}
}

// =============================================================================
// Run invocation failure
// =============================================================================

// TestRun_InvocationFailure_ErrorCapturedInResult verifies that when the
// RunInvoker returns an error (e.g. subprocess crash before creating a run
// folder), the error is captured in TestRunResult.Error and remaining tests
// continue.
func TestRun_InvocationFailure_ErrorCapturedInResult(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	invokeErr := errors.New("subprocess: binary not found")
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{err: invokeErr}, // first call fails (no run folder created)
			defaultPassingInvokeResult(1),
		},
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Both runs must have been attempted.
	if len(inv.calls) != 2 {
		t.Errorf("RunInvoker.Invoke called %d times, want 2", len(inv.calls))
	}

	if len(summary.HarnessResults) == 0 {
		t.Fatal("HarnessResults is empty")
	}
	results := summary.HarnessResults[0].Results
	if len(results) < 1 {
		t.Fatal("Results is empty")
	}
	// First result should carry the invocation error.
	if results[0].Error == nil {
		t.Errorf("Results[0].Error is nil, want invocation error")
	}
	if results[0].Pass {
		t.Errorf("Results[0].Pass = true, want false when invocation errors")
	}
	if summary.TotalError != 1 {
		t.Errorf("TotalError = %d, want 1", summary.TotalError)
	}
}

// TestRun_InvocationFailure_RemainingTestsContinue verifies that an invocation
// failure does not short-circuit remaining runs.
func TestRun_InvocationFailure_RemainingTestsContinue(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/fixtures/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/fixtures/wf-b"},
		{WorkflowID: "wf-c", Mode: "auto", FixturePath: "/fixtures/wf-c"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{err: errors.New("crash")},
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 3 {
		t.Errorf("RunInvoker.Invoke called %d times, want 3 (no short-circuit on error)", len(inv.calls))
	}
}

// TestRun_NonZeroExitMatchesExpected_IsPass verifies that a non-zero exit code
// that matches the expected exit code is treated as a pass.
func TestRun_NonZeroExitMatchesExpected_IsPass(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "deviation-blocked",
		Mode:        "auto",
		FixturePath: "/fixtures/deviation-blocked",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"deviation-blocked"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{exitCode: 1, runFolder: "/workspace/Orchestration-run-1", dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log"},
		},
	}
	// Checker returns Pass=true: expected exit code is 1 and actual is 1.
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !summary.AllPass {
		t.Errorf("AllPass = false, want true when non-zero exit code matches expected")
	}
}

// =============================================================================
// Progress reporting
// =============================================================================

// TestRun_ProgressReporting_EventsInCorrectOrder verifies that the reporter
// receives events in the required sequence: OnDeployStart, OnDeployDone,
// then for each test: OnTestStart, OnTestDone.
func TestRun_ProgressReporting_EventsInCorrectOrder(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Expected sequence: deploy_start, deploy_done, test_start, test_done.
	wantKinds := []string{"deploy_start", "deploy_done", "test_start", "test_done"}
	if len(rep.events) != len(wantKinds) {
		t.Fatalf("reporter received %d events, want %d; events: %v", len(rep.events), len(wantKinds), rep.events)
	}
	for i, want := range wantKinds {
		if rep.events[i].kind != want {
			t.Errorf("events[%d].kind = %q, want %q", i, rep.events[i].kind, want)
		}
	}
}

// TestRun_ProgressReporting_DeployDoneCalledWithNilOnSuccess verifies that
// OnDeployDone is called exactly once with nil when deployment succeeds.
func TestRun_ProgressReporting_DeployDoneCalledWithNilOnSuccess(t *testing.T) {
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/f"}},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, err := o.Run(context.Background(), testrun.TestConfig{
		Scope: testrun.ScopeSmoke, Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployDoneCount := 0
	for _, ev := range rep.events {
		if ev.kind == "deploy_done" {
			deployDoneCount++
			if ev.err != nil {
				t.Errorf("OnDeployDone called with err = %v, want nil on success", ev.err)
			}
		}
	}
	if deployDoneCount == 0 {
		t.Error("OnDeployDone was never called, want exactly one call on success")
	}
}

// TestRun_ProgressReporting_DeployDoneCalledWithErrorOnFailure verifies that
// OnDeployDone is called with the deploy error when deployment fails.
func TestRun_ProgressReporting_DeployDoneCalledWithErrorOnFailure(t *testing.T) {
	cat := &fakeCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	deployErr := errors.New("deploy: something broke")
	dep := &fakeDeployer{err: deployErr}
	inv := &fakeRunInvoker{}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, _ = o.Run(context.Background(), testrun.TestConfig{
		Scope: testrun.ScopeSmoke, Harnesses: []string{"auto"},
	})

	found := false
	for _, ev := range rep.events {
		if ev.kind == "deploy_done" {
			found = true
			if ev.err == nil {
				t.Errorf("OnDeployDone called with nil err, want deploy error")
			}
		}
	}
	if !found {
		t.Error("OnDeployDone was not called")
	}
}

// TestRun_ProgressReporting_TestStartArguments verifies that OnTestStart
// receives the correct harness, workflow, and mode.
func TestRun_ProgressReporting_TestStartArguments(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"my-harness"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var testStarts []progressEvent
	for _, ev := range rep.events {
		if ev.kind == "test_start" {
			testStarts = append(testStarts, ev)
		}
	}
	if len(testStarts) != 1 {
		t.Fatalf("OnTestStart called %d times, want 1", len(testStarts))
	}
	ev := testStarts[0]
	if ev.harness != "my-harness" {
		t.Errorf("OnTestStart harness = %q, want %q", ev.harness, "my-harness")
	}
	if ev.workflow != "smoke-single" {
		t.Errorf("OnTestStart workflow = %q, want %q", ev.workflow, "smoke-single")
	}
	if ev.mode != "auto" {
		t.Errorf("OnTestStart mode = %q, want %q", ev.mode, "auto")
	}
}

// TestRun_ProgressReporting_TestDoneResultMatchesSummary verifies that the
// result passed to OnTestDone matches what ends up in the summary.
func TestRun_ProgressReporting_TestDoneResultMatchesSummary(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	var testDones []progressEvent
	for _, ev := range rep.events {
		if ev.kind == "test_done" {
			testDones = append(testDones, ev)
		}
	}
	if len(testDones) != 1 {
		t.Fatalf("OnTestDone called %d times, want 1", len(testDones))
	}

	// The result reported must match what the summary records.
	if len(summary.HarnessResults) == 0 || len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("Summary has no results to compare")
	}
	summaryResult := summary.HarnessResults[0].Results[0]
	reportedResult := testDones[0].result
	if summaryResult.Pass != reportedResult.Pass {
		t.Errorf("OnTestDone result.Pass = %v, summary result.Pass = %v; must match",
			reportedResult.Pass, summaryResult.Pass)
	}
}

// =============================================================================
// GHCP permission mode forwarding
// =============================================================================

// TestRun_GHCPPermissionMode_ForwardedForGHCPHarness verifies that when the
// harness is "ghcp-cli" and GHCPPermissionMode is set, the RunInvocation
// carries the permission mode.
func TestRun_GHCPPermissionMode_ForwardedForGHCPHarness(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:              testrun.ScopeSmoke,
		Harnesses:          []string{"ghcp-cli"},
		GHCPPermissionMode: "blanket",
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.GHCPPermissionMode != "blanket" {
		t.Errorf("RunInvocation.GHCPPermissionMode = %q, want %q", call.GHCPPermissionMode, "blanket")
	}
}

// TestRun_GHCPPermissionMode_NotSetForNonGHCPHarness verifies that
// GHCPPermissionMode is empty in RunInvocation for non-ghcp-cli harnesses.
func TestRun_GHCPPermissionMode_NotSetForNonGHCPHarness(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
		// GHCPPermissionMode not set because harness is not ghcp-cli.
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.GHCPPermissionMode != "" {
		t.Errorf("RunInvocation.GHCPPermissionMode = %q, want empty for non-ghcp-cli harness", call.GHCPPermissionMode)
	}
}

// TestRun_GHCPPermissionMode_ForwardedToGHCPHarnessOnly verifies that when
// two harnesses run (one ghcp-cli, one other), only the ghcp-cli invocations
// carry the permission mode.
func TestRun_GHCPPermissionMode_ForwardedToGHCPHarnessOnly(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:              testrun.ScopeSmoke,
		Harnesses:          []string{"auto", "ghcp-cli"},
		GHCPPermissionMode: "allowlist",
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 2 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 2", len(inv.calls))
	}

	// Find the ghcp-cli call and the non-ghcp-cli call.
	for _, call := range inv.calls {
		if call.Harness == "ghcp-cli" {
			if call.GHCPPermissionMode != "allowlist" {
				t.Errorf("ghcp-cli invocation GHCPPermissionMode = %q, want %q",
					call.GHCPPermissionMode, "allowlist")
			}
		} else {
			if call.GHCPPermissionMode != "" {
				t.Errorf("non-ghcp-cli invocation GHCPPermissionMode = %q, want empty",
					call.GHCPPermissionMode)
			}
		}
	}
}

// =============================================================================
// Sidecar paths under catalog root (FR-26)
// =============================================================================

// TestRun_SidecarPath_FromCatalogNotWorkspace verifies that LoadExpected is
// always called with the path returned by CatalogPort.SidecarPath, which is
// rooted under the catalog directory, never derived from the deployed workspace.
func TestRun_SidecarPath_FromCatalogNotWorkspace(t *testing.T) {
	const catalogSidecarPath = "/catalog/Workflows/MosaicTest/smoke-single-auto.expected.json"
	const workspace = "/workspace"

	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
		sidecarPaths: map[string]string{
			"smoke-single:auto": catalogSidecarPath,
		},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{
		loadResults: []*testcheck.ExpectedOutcome{{ExitCode: 0}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
		Workspace: workspace,
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(chk.loadExpectedPaths) == 0 {
		t.Fatal("LoadExpected was never called")
	}
	for _, path := range chk.loadExpectedPaths {
		if path != catalogSidecarPath {
			t.Errorf("LoadExpected called with path %q, want %q (catalog root path, not workspace path)",
				path, catalogSidecarPath)
		}
		// Extra guard: the path must not contain the workspace directory.
		if len(path) > len(workspace) && path[:len(workspace)] == workspace {
			t.Errorf("LoadExpected path %q starts with workspace %q -- sidecar must come from catalog root, not workspace",
				path, workspace)
		}
	}
}

// TestRun_SidecarPath_UsedFromCatalogPortMethod verifies that the sidecar path
// is sourced from CatalogPort.SidecarPath, not constructed manually by the
// orchestrator.
func TestRun_SidecarPath_UsedFromCatalogPortMethod(t *testing.T) {
	const customSidecarPath = "/custom-catalog-location/Workflows/MosaicTest/wf-x-auto.expected.json"

	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-x",
		Mode:        "auto",
		FixturePath: "/custom-catalog-location/Workflows/MosaicTest/Fixtures/wf-x",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-x"},
		sidecarPaths: map[string]string{
			"wf-x:auto": customSidecarPath,
		},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{
		loadResults: []*testcheck.ExpectedOutcome{{ExitCode: 0}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(chk.loadExpectedPaths) == 0 {
		t.Fatal("LoadExpected was never called")
	}
	if chk.loadExpectedPaths[0] != customSidecarPath {
		t.Errorf("LoadExpected called with %q, want %q", chk.loadExpectedPaths[0], customSidecarPath)
	}
}

// =============================================================================
// Summary counters
// =============================================================================

// TestRun_SummaryCounters_PerHarnessCountsMatchResults verifies that each
// HarnessResults group's PassCount/FailCount/ErrorCount reflects actual outcomes.
func TestRun_SummaryCounters_PerHarnessCountsMatchResults(t *testing.T) {
	entries := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/f/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/f/wf-b"},
		{WorkflowID: "wf-c", Mode: "auto", FixturePath: "/f/wf-c"},
	}
	cat := &fakeCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &fakeDeployer{}
	invokeErr := errors.New("crash")
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			{err: invokeErr},
		},
	}
	failMismatch := &testcheck.Mismatch{Kind: testcheck.MismatchExitCode, Index: -1}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
			// No check for third (invocation error bypasses checker).
		},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if summary.TotalPass != 1 {
		t.Errorf("TotalPass = %d, want 1", summary.TotalPass)
	}
	if summary.TotalFail != 1 {
		t.Errorf("TotalFail = %d, want 1", summary.TotalFail)
	}
	if summary.TotalError != 1 {
		t.Errorf("TotalError = %d, want 1", summary.TotalError)
	}

	if len(summary.HarnessResults) != 1 {
		t.Fatalf("HarnessResults len = %d, want 1", len(summary.HarnessResults))
	}
	hr := summary.HarnessResults[0]
	if hr.PassCount != 1 {
		t.Errorf("HarnessResults[0].PassCount = %d, want 1", hr.PassCount)
	}
	if hr.FailCount != 1 {
		t.Errorf("HarnessResults[0].FailCount = %d, want 1", hr.FailCount)
	}
	if hr.ErrorCount != 1 {
		t.Errorf("HarnessResults[0].ErrorCount = %d, want 1", hr.ErrorCount)
	}
}

// TestRun_AllPass_FalseWhenAnyError verifies that AllPass is false when any
// run has an infrastructure error.
func TestRun_AllPass_FalseWhenAnyError(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{{err: errors.New("infra error")}},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	summary, err := o.Run(context.Background(), testrun.TestConfig{
		Scope: testrun.ScopeSmoke, Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if summary.AllPass {
		t.Errorf("AllPass = true, want false when infrastructure error occurs")
	}
}

// =============================================================================
// DispatchLogPath utility function
// =============================================================================

// TestDispatchLogPath_Convention verifies that DispatchLogPath follows the
// convention: {workspace}/RunnerLogs/{run_id}/{run_id}-dispatch.log.
func TestDispatchLogPath_Convention(t *testing.T) {
	const workspace = "/workspace"
	const runID = "run-20260914T120000Z-abc1"
	got := testrun.DispatchLogPath(workspace, runID)
	want := filepath.Join(workspace, "RunnerLogs", runID, runID+"-dispatch.log")
	if got != want {
		t.Errorf("DispatchLogPath = %q, want %q", got, want)
	}
}

// TestDispatchLogPath_RunIDAppearsInFilename verifies the run ID appears in
// the final filename segment.
func TestDispatchLogPath_RunIDAppearsInFilename(t *testing.T) {
	runID := "my-run-42"
	got := testrun.DispatchLogPath("/ws", runID)
	if len(got) == 0 {
		t.Fatal("DispatchLogPath returned empty string")
	}
	// The filename segment must be "{runID}-dispatch.log".
	wantFilename := runID + "-dispatch.log"
	// Extract last path component.
	lastSlash := 0
	for i, ch := range got {
		if ch == '/' || ch == '\\' {
			lastSlash = i
		}
	}
	filename := got[lastSlash+1:]
	if filename != wantFilename {
		t.Errorf("DispatchLogPath filename = %q, want %q", filename, wantFilename)
	}
}

// =============================================================================
// RunInvocation fields set correctly
// =============================================================================

// TestRun_RunInvocation_WorkflowIDAndModeAndFixturePath verifies that the
// RunInvocation passed to the invoker carries the workflow ID, mode, and
// fixture path from the catalog entry.
func TestRun_RunInvocation_WorkflowIDAndModeAndFixturePath(t *testing.T) {
	const fixturePath = "/catalog/Workflows/MosaicTest/Fixtures/smoke-single"
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: fixturePath,
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"my-harness"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.WorkflowID != "smoke-single" {
		t.Errorf("RunInvocation.WorkflowID = %q, want %q", call.WorkflowID, "smoke-single")
	}
	if call.Mode != "auto" {
		t.Errorf("RunInvocation.Mode = %q, want %q", call.Mode, "auto")
	}
	if call.FixturePath != fixturePath {
		t.Errorf("RunInvocation.FixturePath = %q, want %q", call.FixturePath, fixturePath)
	}
	if call.Harness != "my-harness" {
		t.Errorf("RunInvocation.Harness = %q, want %q", call.Harness, "my-harness")
	}
	wantTask := "Test: smoke-single / auto / my-harness"
	if call.Task != wantTask {
		t.Errorf("RunInvocation.Task = %q, want %q", call.Task, wantTask)
	}
}

// TestRun_RunInvocation_HarnessMatchesConfigHarness verifies the harness name
// is taken from the config, not hardcoded.
func TestRun_RunInvocation_HarnessMatchesConfigHarness(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker never called")
	}
	if inv.calls[0].Harness != "claude-code" {
		t.Errorf("RunInvocation.Harness = %q, want %q", inv.calls[0].Harness, "claude-code")
	}
}

// TestRun_RunInvocation_PreConsultForwardedFromCatalogEntry verifies that the
// invocation construction site copies CatalogEntry.PreConsult into
// RunInvocation.PreConsult. Two sub-cases cover both boolean values: a catalog
// entry with PreConsult=true must produce an invocation with PreConsult=true,
// and an entry with PreConsult=false must produce one with PreConsult=false.
func TestRun_RunInvocation_PreConsultForwardedFromCatalogEntry(t *testing.T) {
	for _, wantPreConsult := range []bool{true, false} {
		entry := testcatalog.CatalogEntry{
			WorkflowID:  "wf-a",
			Mode:        "auto",
			FixturePath: "/f/wf-a",
			PreConsult:  wantPreConsult,
		}
		cat := &fakeCatalog{
			smokeSet:    []testcatalog.CatalogEntry{entry},
			workflowIDs: []string{"wf-a"},
		}
		dep := &fakeDeployer{}
		inv := &fakeRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
		chk := &fakeChecker{}
		rep := &fakeReporter{}

		o := newOrchestrator(cat, dep, inv, chk, rep)
		_, err := o.Run(context.Background(), testrun.TestConfig{
			Scope:     testrun.ScopeSmoke,
			Harnesses: []string{"auto"},
		})
		if err != nil {
			t.Fatalf("PreConsult=%v: Run returned unexpected error: %v", wantPreConsult, err)
		}

		if len(inv.calls) == 0 {
			t.Fatalf("PreConsult=%v: RunInvoker never called", wantPreConsult)
		}
		if inv.calls[0].PreConsult != wantPreConsult {
			t.Errorf("PreConsult=%v: RunInvocation.PreConsult = %v, want %v\n(invocation construction site must forward CatalogEntry.PreConsult into RunInvocation.PreConsult)",
				wantPreConsult, inv.calls[0].PreConsult, wantPreConsult)
		}
	}
}

// =============================================================================
// Scope resolution: full suite
// =============================================================================

// TestRun_FullScope_UsesFullSuiteEntries verifies that ScopeFull runs all
// entries from CatalogPort.FullSuite().
func TestRun_FullScope_UsesFullSuiteEntries(t *testing.T) {
	fullSuite := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/f/wf-a"},
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/f/wf-b"},
		{WorkflowID: "wf-b", Mode: "auto-review", FixturePath: "/f/wf-b"},
	}
	cat := &fakeCatalog{
		fullSuite:   fullSuite,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	_, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeFull,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 3 {
		t.Errorf("RunInvoker called %d times, want 3 (full suite has 3 entries)", len(inv.calls))
	}
}

// =============================================================================
// Scope resolution: custom selection
// =============================================================================

// TestRun_CustomScope_RunsEachListedWorkflow verifies that ScopeCustom calls
// WorkflowByID for each workflow listed in cfg.Workflows and executes one run
// per returned entry per harness.
func TestRun_CustomScope_RunsEachListedWorkflow(t *testing.T) {
	entriesA := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/f/wf-a"},
	}
	entriesB := []testcatalog.CatalogEntry{
		{WorkflowID: "wf-b", Mode: "auto", FixturePath: "/f/wf-b"},
	}
	cat := &fakeCatalog{
		workflowByIDFn: func(id string) ([]testcatalog.CatalogEntry, error) {
			switch id {
			case "wf-a":
				return entriesA, nil
			case "wf-b":
				return entriesB, nil
			}
			return nil, errors.New("not found: " + id)
		},
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeCustom,
		Harnesses: []string{"auto"},
		Workflows: []string{"wf-a", "wf-b"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// One entry per workflow ID x one harness = 2 invocations.
	if len(inv.calls) != 2 {
		t.Errorf("RunInvoker.Invoke called %d times, want 2 (one per listed workflow)", len(inv.calls))
	}

	// Each invocation must reference one of the listed workflow IDs.
	wfIDs := map[string]bool{}
	for _, c := range inv.calls {
		wfIDs[c.WorkflowID] = true
	}
	if !wfIDs["wf-a"] {
		t.Errorf("no invocation for workflow %q", "wf-a")
	}
	if !wfIDs["wf-b"] {
		t.Errorf("no invocation for workflow %q", "wf-b")
	}
}

// =============================================================================
// Deploy argument correctness
// =============================================================================

// =============================================================================
// Orchestrator InvokeResult handling
// =============================================================================

// TestRun_InvokeResult_SuccessPath_ChildStderrPopulated verifies that when
// Invoke returns (result, nil), the orchestrator populates TestRunResult.ChildStderr
// from InvokeResult.ChildStderr.
func TestRun_InvokeResult_SuccessPath_ChildStderrPopulated(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	wantStderr := []byte("warning: something happened during run\n")
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{
				exitCode:        0,
				runFolder:       "/workspace/Orchestration-run-1",
				dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log",
				childStderr:     wantStderr,
			},
		},
	}
	chk := &fakeChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	summary, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(summary.HarnessResults) == 0 || len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("no results in summary")
	}
	result := summary.HarnessResults[0].Results[0]
	if result.ChildStderr != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q (orchestrator must populate from InvokeResult on success path)",
			result.ChildStderr, string(wantStderr))
	}
}

// TestRun_InvokeResult_SuccessPath_ActualExitCodePopulated verifies that when
// Invoke returns (result, nil), the orchestrator populates
// TestRunResult.ActualExitCode from InvokeResult.ExitCode.
func TestRun_InvokeResult_SuccessPath_ActualExitCodePopulated(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{
				exitCode:        3,
				runFolder:       "/workspace/Orchestration-run-1",
				dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log",
			},
		},
	}
	chk := &fakeChecker{
		// Checker passes even with exit code 3 (expected is 3).
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	summary, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(summary.HarnessResults) == 0 || len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("no results in summary")
	}
	result := summary.HarnessResults[0].Results[0]
	if result.ActualExitCode != 3 {
		t.Errorf("ActualExitCode = %d, want 3 (orchestrator must populate from InvokeResult.ExitCode on success path)",
			result.ActualExitCode)
	}
}

// TestRun_InvokeResult_ErrorPath_ChildStderrStillPopulated verifies that when
// Invoke returns (result, err), the orchestrator STILL populates
// TestRunResult.ChildStderr from InvokeResult.ChildStderr (not zero-valued).
func TestRun_InvokeResult_ErrorPath_ChildStderrStillPopulated(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	wantStderr := []byte("fatal: could not create run folder\n")
	invokeErr := errors.New("testrun: run folder discovery failed: no Orchestration-* directory found")
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{
				exitCode:    1,
				childStderr: wantStderr,
				err:         invokeErr,
			},
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	summary, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(summary.HarnessResults) == 0 || len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("no results in summary")
	}
	result := summary.HarnessResults[0].Results[0]

	// Error must be set from invokeErr.
	if result.Error == nil {
		t.Errorf("Error is nil, want non-nil invoke error")
	}
	// ChildStderr must be populated from InvokeResult even though error occurred.
	if result.ChildStderr != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q (orchestrator must populate ChildStderr from InvokeResult even on error path)",
			result.ChildStderr, string(wantStderr))
	}
}

// TestRun_InvokeResult_ErrorPath_ActualExitCodeStillPopulated verifies that
// when Invoke returns (result, err), the orchestrator STILL populates
// TestRunResult.ActualExitCode from InvokeResult.ExitCode (not zero-valued).
func TestRun_InvokeResult_ErrorPath_ActualExitCodeStillPopulated(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		FixturePath: "/f/wf-a",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &fakeDeployer{}
	invokeErr := errors.New("testrun: run folder discovery failed: no Orchestration-* directory found")
	inv := &fakeRunInvoker{
		results: []invokeResult{
			{
				exitCode: 2, // subprocess exited with code 2 before discovery succeeded
				err:      invokeErr,
			},
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	summary, err := o.Run(context.Background(), testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(summary.HarnessResults) == 0 || len(summary.HarnessResults[0].Results) == 0 {
		t.Fatal("no results in summary")
	}
	result := summary.HarnessResults[0].Results[0]

	// ActualExitCode must be populated from InvokeResult even though error occurred.
	if result.ActualExitCode != 2 {
		t.Errorf("ActualExitCode = %d, want 2 (orchestrator must populate ActualExitCode from InvokeResult even on error path)",
			result.ActualExitCode)
	}
}

// =============================================================================
// Deploy argument correctness
// =============================================================================

// TestRun_DeployArguments_CorrectValuesPassedToDeployer verifies that the
// orchestrator passes the correct arguments to Deployer.Deploy: harnesses from
// cfg.Harnesses, workflows from CatalogPort.WorkflowIDs() (the critical guard
// that prevents silent empty deployments), and catalogFolder joined from
// cfg.MosaicRoot and "Tools/Runner/TestCatalog".
func TestRun_DeployArguments_CorrectValuesPassedToDeployer(t *testing.T) {
	const mosaicRoot = "/mosaic"
	const workspace = "/workspace"
	wantCatalogFolder := filepath.Join(mosaicRoot, "Tools/Runner/TestCatalog")
	wantHarnesses := []string{"auto", "ghcp-cli"}
	wantWorkflows := []string{"smoke-single", "smoke-double", "findings-loop"}

	cat := &fakeCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: wantWorkflows,
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
			defaultPassingInvokeResult(4),
			defaultPassingInvokeResult(5),
			defaultPassingInvokeResult(6),
			defaultPassingInvokeResult(7),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  wantHarnesses,
		MosaicRoot: mosaicRoot,
		Workspace:  workspace,
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if dep.callCount != 1 {
		t.Fatalf("Deploy called %d times, want 1", dep.callCount)
	}

	args := dep.capturedArgs[0]

	// catalogFolder must be MosaicRoot joined with the known sub-path.
	if args.catalogFolder != wantCatalogFolder {
		t.Errorf("Deploy catalogFolder = %q, want %q", args.catalogFolder, wantCatalogFolder)
	}

	// harnesses must match cfg.Harnesses exactly.
	if len(args.harnesses) != len(wantHarnesses) {
		t.Errorf("Deploy harnesses = %v, want %v", args.harnesses, wantHarnesses)
	} else {
		for i, h := range wantHarnesses {
			if args.harnesses[i] != h {
				t.Errorf("Deploy harnesses[%d] = %q, want %q", i, args.harnesses[i], h)
			}
		}
	}

	// workflows must be sourced from CatalogPort.WorkflowIDs() -- this is the
	// critical guard that prevents the deploy tool from silently deploying nothing.
	if len(args.workflows) != len(wantWorkflows) {
		t.Errorf("Deploy workflows = %v, want %v (from CatalogPort.WorkflowIDs())", args.workflows, wantWorkflows)
	} else {
		for i, wf := range wantWorkflows {
			if args.workflows[i] != wf {
				t.Errorf("Deploy workflows[%d] = %q, want %q", i, args.workflows[i], wf)
			}
		}
	}
}

// =============================================================================
// T3.2: Orchestrator.Run -- ExecutablePath forwarding from ResolvedPaths
// =============================================================================

// TestRun_ExecutablePath_ForwardedFromResolvedPaths verifies that when
// TestConfig.ResolvedPaths contains an entry for a harness, each RunInvocation
// passed to RunInvoker.Invoke carries the correct ExecutablePath for that harness.
func TestRun_ExecutablePath_ForwardedFromResolvedPaths(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	wantPath := "/usr/local/bin/claude"
	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"claude-code"},
		ResolvedPaths: map[string]string{
			"claude-code": wantPath,
		},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.ExecutablePath != wantPath {
		t.Errorf("RunInvocation.ExecutablePath = %q, want %q\n(orchestrator must populate ExecutablePath from cfg.ResolvedPaths[harness])",
			call.ExecutablePath, wantPath)
	}
}

// TestRun_ExecutablePath_EmptyWhenResolvedPathsNil verifies that when
// TestConfig.ResolvedPaths is nil, RunInvocation.ExecutablePath is empty for
// every invocation. This is the backwards-compatible zero-value path: callers
// that do not perform resolution continue to work unchanged.
func TestRun_ExecutablePath_EmptyWhenResolvedPathsNil(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:         testrun.ScopeSmoke,
		Harnesses:     []string{"claude-code"},
		ResolvedPaths: nil, // no resolution performed
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.ExecutablePath != "" {
		t.Errorf("RunInvocation.ExecutablePath = %q, want empty when cfg.ResolvedPaths is nil\n(nil map must not panic and must produce empty ExecutablePath)",
			call.ExecutablePath)
	}
}

// TestRun_ExecutablePath_EmptyWhenNoEntryForHarness verifies that when
// TestConfig.ResolvedPaths is non-nil but does not contain an entry for the
// invoked harness, RunInvocation.ExecutablePath is empty. This allows a
// partial-resolution map where only some harnesses are resolved.
func TestRun_ExecutablePath_EmptyWhenNoEntryForHarness(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"claude-code"},
		// ResolvedPaths exists but contains a different harness, not claude-code.
		ResolvedPaths: map[string]string{
			"opencode": "/usr/local/bin/opencode",
		},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 1 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 1", len(inv.calls))
	}
	call := inv.calls[0]
	if call.ExecutablePath != "" {
		t.Errorf("RunInvocation.ExecutablePath = %q, want empty when cfg.ResolvedPaths has no entry for harness %q",
			call.ExecutablePath, "claude-code")
	}
}

// TestRun_ExecutablePath_CorrectPathPerHarness verifies that when two harnesses
// are configured with different resolved paths, each RunInvocation receives the
// path for its own harness (not the other harness's path).
func TestRun_ExecutablePath_CorrectPathPerHarness(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	claudePath := "/usr/local/bin/claude"
	opencodePath := "/usr/local/bin/opencode"

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"claude-code", "opencode"},
		ResolvedPaths: map[string]string{
			"claude-code": claudePath,
			"opencode":    opencodePath,
		},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) != 2 {
		t.Fatalf("RunInvoker.Invoke called %d times, want 2", len(inv.calls))
	}

	// First call is for claude-code (harnesses slice order).
	if inv.calls[0].Harness != "claude-code" {
		t.Fatalf("calls[0].Harness = %q, want %q", inv.calls[0].Harness, "claude-code")
	}
	if inv.calls[0].ExecutablePath != claudePath {
		t.Errorf("calls[0].ExecutablePath = %q, want %q (must match claude-code resolved path)",
			inv.calls[0].ExecutablePath, claudePath)
	}

	// Second call is for opencode.
	if inv.calls[1].Harness != "opencode" {
		t.Fatalf("calls[1].Harness = %q, want %q", inv.calls[1].Harness, "opencode")
	}
	if inv.calls[1].ExecutablePath != opencodePath {
		t.Errorf("calls[1].ExecutablePath = %q, want %q (must match opencode resolved path)",
			inv.calls[1].ExecutablePath, opencodePath)
	}
}

// =============================================================================
// TestSummary.ResolvedPaths field existence guard
// =============================================================================

// TestSummary_ResolvedPaths_FieldAccessible verifies that TestSummary has a
// ResolvedPaths field. Accessing summary.ResolvedPaths after o.Run without
// panicking is sufficient; the field is expected to be nil when
// TestConfig.ResolvedPaths was nil. Behavioral population of the field
// (mirroring cfg.ResolvedPaths into the summary) is a wiring concern deferred
// to the implementation stage.
func TestSummary_ResolvedPaths_FieldAccessible(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Fixtures/smoke-single",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Access summary.ResolvedPaths. The zero value (nil) is acceptable here;
	// this test exists solely as a compile-time guard that the field is declared
	// on TestSummary. If the field is missing or renamed, this test fails to
	// compile and the stage cannot be considered complete.
	_ = summary.ResolvedPaths
}

// =============================================================================
// CatalogPort.UnionInfrastructureAgentKeys forwarding to DeployerPort
// =============================================================================

// TestRun_UnionInfrastructureAgentKeys_ForwardedToDeployer verifies that
// Orchestrator.Run reads UnionInfrastructureAgentKeys() from the catalog and
// passes the result as the infrastructureKeys argument to Deployer.Deploy.
//
// This test is in the TDD RED phase. It compiles and runs, but fails because
// Orchestrator.Run currently calls InfrastructureAgentKeys() (the old method),
// not UnionInfrastructureAgentKeys(). The fake returns different values from
// each method so the mismatch is observable. It will pass once I3.4 updates
// the deploy call site.
func TestRun_UnionInfrastructureAgentKeys_ForwardedToDeployer(t *testing.T) {
	// Arrange: catalog's union keys differ from the old InfrastructureAgentKeys
	// value so the test can detect which method Orchestrator.Run calls.
	unionKeys := []string{"mosaictest-checkpoint", "mosaictest-review"}
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
		InSmokeSet:  true,
	}
	cat := &fakeCatalog{
		smokeSet:       []testcatalog.CatalogEntry{entry},
		workflowIDs:    []string{"smoke-single"},
		infraKeys:      nil,      // old method: returns nil (the wrong value)
		unionInfraKeys: unionKeys, // new method: returns the expected value
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	// Act
	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Assert: the deployer must have been called with the union keys.
	if dep.callCount == 0 {
		t.Fatal("Deploy was not called")
	}
	got := dep.capturedArgs[0].infrastructureKeys
	if len(got) != len(unionKeys) {
		t.Fatalf("Deploy infrastructureKeys = %v, want %v; "+
			"Orchestrator.Run must forward catalog.UnionInfrastructureAgentKeys() to "+
			"Deployer.Deploy (not the deprecated InfrastructureAgentKeys())",
			got, unionKeys)
	}
	for i, want := range unionKeys {
		if got[i] != want {
			t.Errorf("Deploy infrastructureKeys[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestRun_UnionInfrastructureAgentKeys_NoWorkflowAgents_NonNilEmptyForwardedToDeployer
// verifies that when no workflow declares any infrastructure agents,
// Orchestrator.Run forwards a non-nil empty slice (not nil) to Deployer.Deploy.
// A non-nil empty slice causes buildDeployArgs to emit --infrastructure ""
// (deploying zero agents), whereas nil would omit the flag entirely (deploying
// the default set).
//
// This test is in the TDD RED phase. It will fail because Orchestrator.Run
// currently calls InfrastructureAgentKeys() which returns nil, but the test
// expects non-nil empty. It will pass once I3.4 updates the call site to use
// UnionInfrastructureAgentKeys(), which the fake returns as []string{}.
func TestRun_UnionInfrastructureAgentKeys_NoWorkflowAgents_NonNilEmptyForwardedToDeployer(t *testing.T) {
	// Arrange: no workflows declare infrastructure agents.
	// The fake's UnionInfrastructureAgentKeys() returns []string{} (non-nil empty)
	// when unionInfraKeys is nil (the zero value), matching the real implementation.
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
		InSmokeSet:  true,
	}
	cat := &fakeCatalog{
		smokeSet:       []testcatalog.CatalogEntry{entry},
		workflowIDs:    []string{"smoke-single"},
		infraKeys:      nil, // old method returns nil
		unionInfraKeys: nil, // zero value: UnionInfrastructureAgentKeys() returns []string{}
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	// Act
	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Assert: the deployer must receive a non-nil empty slice, not nil.
	if dep.callCount == 0 {
		t.Fatal("Deploy was not called")
	}
	got := dep.capturedArgs[0].infrastructureKeys
	if got == nil {
		t.Errorf("Deploy infrastructureKeys = nil, want non-nil empty slice; "+
			"Orchestrator.Run must forward catalog.UnionInfrastructureAgentKeys() "+
			"which returns []string{} (not nil) when no workflows declare agents. "+
			"nil would cause buildDeployArgs to omit --infrastructure entirely.")
	}
	if len(got) != 0 {
		t.Errorf("Deploy infrastructureKeys = %v (len %d), want empty slice; "+
			"no workflows declared any infrastructure agents", got, len(got))
	}
}

// =============================================================================
// Orchestrator.Run: RunInvocation population from CatalogEntry
// =============================================================================

// TestRun_PopulatesRunInvocation_InfrastructureKeys_FromCatalogEntry verifies
// that Orchestrator.Run copies CatalogEntry.InfrastructureAgents into
// RunInvocation.InfrastructureKeys when building each subprocess invocation.
//
// TDD RED: fails because Orchestrator.Run does not yet populate InfrastructureKeys.
func TestRun_PopulatesRunInvocation_InfrastructureKeys_FromCatalogEntry(t *testing.T) {
	wantKeys := []string{"mosaictest-checkpoint", "mosaictest-review"}
	entry := testcatalog.CatalogEntry{
		WorkflowID:           "smoke-single",
		Mode:                 "auto",
		FixturePath:          "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:             []string{"auto"},
		InSmokeSet:           true,
		InfrastructureAgents: wantKeys,
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker was not called")
	}
	got := inv.calls[0].InfrastructureKeys
	if len(got) != len(wantKeys) {
		t.Fatalf("RunInvocation.InfrastructureKeys = %v, want %v; "+
			"Orchestrator.Run must set InfrastructureKeys from CatalogEntry.InfrastructureAgents",
			got, wantKeys)
	}
	for i, want := range wantKeys {
		if got[i] != want {
			t.Errorf("RunInvocation.InfrastructureKeys[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestRun_PopulatesRunInvocation_InfrastructureKeys_NonNilEmpty_WhenNoAgentsDeclared
// verifies that when a CatalogEntry has InfrastructureAgents == []string{} (the
// non-nil empty value Stage 2 guarantees for workflows without the frontmatter
// field), Orchestrator.Run sets RunInvocation.InfrastructureKeys to that same
// non-nil empty slice.
//
// TDD RED: fails because Orchestrator.Run does not yet populate InfrastructureKeys.
func TestRun_PopulatesRunInvocation_InfrastructureKeys_NonNilEmpty_WhenNoAgentsDeclared(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:           "smoke-single",
		Mode:                 "auto",
		FixturePath:          "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:             []string{"auto"},
		InSmokeSet:           true,
		InfrastructureAgents: []string{}, // non-nil empty: workflow declared no agents
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker was not called")
	}
	got := inv.calls[0].InfrastructureKeys
	if got == nil {
		t.Errorf("RunInvocation.InfrastructureKeys = nil, want non-nil empty slice; "+
			"CatalogEntry.InfrastructureAgents is non-nil empty (workflow declared no agents), "+
			"and Orchestrator.Run must preserve the nil/non-nil distinction: "+
			"nil means omit --infrastructure; non-nil empty means emit --infrastructure=")
	}
	if len(got) != 0 {
		t.Errorf("RunInvocation.InfrastructureKeys = %v, want empty slice", got)
	}
}

// TestRun_PopulatesRunInvocation_Checkpoints_FromCatalogEntry verifies that
// Orchestrator.Run copies CatalogEntry.Checkpoints into RunInvocation.Checkpoints.
//
// TDD RED: fails because Orchestrator.Run does not yet populate Checkpoints.
func TestRun_PopulatesRunInvocation_Checkpoints_FromCatalogEntry(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:           "smoke-single",
		Mode:                 "auto",
		FixturePath:          "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:             []string{"auto"},
		InSmokeSet:           true,
		InfrastructureAgents: []string{},
		Checkpoints:          "enabled",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker was not called")
	}
	got := inv.calls[0].Checkpoints
	if got != "enabled" {
		t.Errorf("RunInvocation.Checkpoints = %q, want %q; "+
			"Orchestrator.Run must set Checkpoints from CatalogEntry.Checkpoints",
			got, "enabled")
	}
}

// TestRun_PopulatesRunInvocation_Commits_FromCatalogEntry verifies that
// Orchestrator.Run copies CatalogEntry.Commits into RunInvocation.Commits.
//
// TDD RED: fails because Orchestrator.Run does not yet populate Commits.
func TestRun_PopulatesRunInvocation_Commits_FromCatalogEntry(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:           "smoke-single",
		Mode:                 "auto",
		FixturePath:          "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:             []string{"auto"},
		InSmokeSet:           true,
		InfrastructureAgents: []string{},
		Commits:              "enabled",
	}
	cat := &fakeCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &fakeDeployer{}
	inv := &fakeRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &fakeChecker{}
	rep := &fakeReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker was not called")
	}
	got := inv.calls[0].Commits
	if got != "enabled" {
		t.Errorf("RunInvocation.Commits = %q, want %q; "+
			"Orchestrator.Run must set Commits from CatalogEntry.Commits",
			got, "enabled")
	}
}
