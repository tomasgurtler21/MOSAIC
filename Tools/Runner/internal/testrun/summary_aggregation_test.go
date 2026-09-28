package testrun_test

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
)

// TestRun_SmokeScope_AllPass_SummaryAllPassTrue verifies that AllPass is true
// when every smoke-set run passes.
func TestRun_SmokeScope_AllPass_SummaryAllPassTrue(t *testing.T) {
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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b", "wf-c"},
	}
	dep := &MockDeployer{}
	invokeErr := errors.New("crash")
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			{err: invokeErr},
		},
	}
	failMismatch := &testcheck.Mismatch{Kind: testcheck.MismatchExitCode, Index: -1}
	chk := &MockChecker{
		checkResults: []testcheck.CheckResult{
			{Pass: true},
			{Pass: false, Mismatch: failMismatch},
			// No check for third (invocation error bypasses checker).
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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{{err: errors.New("infra error")}},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &MockChecker{}
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

	// Access summary.ResolvedPaths. The zero value (nil) is acceptable here;
	// this test exists solely as a compile-time guard that the field is declared
	// on TestSummary. If the field is missing or renamed, this test fails to
	// compile and the stage cannot be considered complete.
	_ = summary.ResolvedPaths
}
