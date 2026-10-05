package testrun_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
)

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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
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
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a"},
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
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"wf-a", "wf-b"},
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
	chk := &MockChecker{}
	rep := &MockReporter{}

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
	cat := &MockCatalog{
		smokeSet:    []testcatalog.CatalogEntry{entry},
		workflowIDs: []string{"smoke-single"},
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
