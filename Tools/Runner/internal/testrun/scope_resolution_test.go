package testrun_test

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// =============================================================================

// TestRun_SmokeScope_DeployCalledOnce_ThenAllRunsExecuted verifies that the
// orchestrator deploys exactly once before running smoke-set entries.
func TestRun_SmokeScope_DeployCalledOnce_ThenAllRunsExecuted(t *testing.T) {
	entries := smokeEntries() // 4 entries
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"smoke-single", "smoke-double", "findings-loop"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
		},
	}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true}, {Pass: true}, {Pass: true}, {Pass: true},
		},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		workflowByIDFn: func(id string) ([]testcatalog.CatalogEntry, error) {
			if id == "smoke-single" {
				return []testcatalog.CatalogEntry{entry}, nil
			}
			return nil, errors.New("not found")
		},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		workflowByIDFn: func(id string) ([]testcatalog.CatalogEntry, error) {
			return []testcatalog.CatalogEntry{entry}, nil
		},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	wantMismatch := &testcheck.Mismatch{
		Kind:          testcheck.MismatchExitCode,
		Index:         -1,
		Message:       "exit code mismatch: expected 0, got 1",
		ExpectedValue: "0",
		ActualValue:   "1",
	}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{{Pass: false, Mismatch: wantMismatch}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		fullSuite:   fullSuite,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
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
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
