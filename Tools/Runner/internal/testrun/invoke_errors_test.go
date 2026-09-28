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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
	}
	dep := &MockDeployer{}
	invokeErr := errors.New("subprocess: binary not found")
	inv := &MockRunInvoker{
		results: []invokeResult{
			{err: invokeErr}, // first call fails (no run folder created)
			defaultPassingInvokeResult(1),
		},
	}
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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			{err: errors.New("crash")},
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	wantStderr := []byte("warning: something happened during run\n")
	inv := &MockRunInvoker{
		results: []invokeResult{
			{
				exitCode:        0,
				runFolder:       "/workspace/Orchestration-run-1",
				dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log",
				childStderr:     wantStderr,
			},
		},
	}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			{
				exitCode:        3,
				runFolder:       "/workspace/Orchestration-run-1",
				dispatchLogPath: "/workspace/RunnerLogs/run-1/run-1-dispatch.log",
			},
		},
	}
	chk := &MockChecker{
		// Checker passes even with exit code 3 (expected is 3).
		checkResults: []testcheck.CheckResult{{Pass: true}},
	}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	wantStderr := []byte("fatal: could not create run folder\n")
	invokeErr := errors.New("testrun: run folder discovery failed: no Orchestration-* directory found")
	inv := &MockRunInvoker{
		results: []invokeResult{
			{
				exitCode:    1,
				childStderr: wantStderr,
				err:         invokeErr,
			},
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	invokeErr := errors.New("testrun: run folder discovery failed: no Orchestration-* directory found")
	inv := &MockRunInvoker{
		results: []invokeResult{
			{
				exitCode: 2, // subprocess exited with code 2 before discovery succeeded
				err:      invokeErr,
			},
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
