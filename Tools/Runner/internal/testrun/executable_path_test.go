package testrun_test

import (
	"context"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
)

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
