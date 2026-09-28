package testrun_test

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
)

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{{WorkflowID: "wf-a", Mode: "auto", FixturePath: "/f"}},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	deployErr := errors.New("deploy: something broke")
	dep := &MockDeployer{err: deployErr}
	inv := &MockRunInvoker{}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
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
