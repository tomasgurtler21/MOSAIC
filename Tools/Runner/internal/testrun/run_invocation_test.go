package testrun_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
)
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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"wf-a"},
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
	chk := &MockChecker{}
	rep := &MockReporter{}

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
		cat := &MockCatalog{
			smokeSet:    []testcatalog.CatalogEntry{entry},
			workflowIDs: []string{"wf-a"},
		}
		dep := &MockDeployer{}
		inv := &MockRunInvoker{results: []invokeResult{defaultPassingInvokeResult(0)}}
		chk := &MockChecker{}
		rep := &MockReporter{}

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

	if len(inv.calls) == 0 {
		t.Fatal("RunInvoker was not called")
	}
	got := inv.calls[0].InfrastructureKeys
	if got == nil {
		t.Errorf("RunInvocation.InfrastructureKeys = nil, want non-nil empty slice; " +
			"CatalogEntry.InfrastructureAgents is non-nil empty (workflow declared no agents), " +
			"and Orchestrator.Run must preserve the nil/non-nil distinction: " +
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
