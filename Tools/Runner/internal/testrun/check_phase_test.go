package testrun_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	failMismatch := &testcheck.Mismatch{Kind: testcheck.MismatchExitCode, Index: -1}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
			{Pass: true},
		},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
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
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
		},
	}
	rep := &MockReporter{}

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

// TestRun_NonZeroExitMatchesExpected_IsPass verifies that a non-zero exit code
// that matches the expected exit code is treated as a pass.
func TestRun_NonZeroExitMatchesExpected_IsPass(t *testing.T) {
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "deviation-blocked",
		Mode:        "auto",
		FixturePath: "/fixtures/deviation-blocked",
	}
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"deviation-blocked"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			{exitCode: 1, runFolder: "/workspace/Orchestration-run-1", dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log"},
		},
	}
	// Checker returns Pass=true: expected exit code is 1 and actual is 1.
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
		sidecarPaths: map[string]string{
			"smoke-single:auto": catalogSidecarPath,
		},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &MockChecker{
		loadResults: []*testcheck.ExpectedOutcome{{ExitCode: 0}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-x"},
		sidecarPaths: map[string]string{
			"wf-x:auto": customSidecarPath,
		},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &MockChecker{
		loadResults: []*testcheck.ExpectedOutcome{{ExitCode: 0}},
	}
	rep := &MockReporter{}

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
