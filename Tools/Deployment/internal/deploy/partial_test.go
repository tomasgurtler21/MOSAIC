package deploy_test

// partial_test.go covers T17.5 and T17.6.
//
// T17.5: When a write fails partway through a plan, the executor does not abort silently.
// It records what was actually written and what failed, writes the manifest to reflect real
// on-disk state (not intent), and sets ExecResult.Partial to the first write error. Items
// processed before the failure have TakenCreated/TakenUpdated; the failing item has
// TakenFailed with a non-empty Err field (AC17.4).
//
// T17.6: The executor creates directories as needed, including nested paths that do not
// exist. An existing directory is not an error.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// T17.5 — Partial failure: manifest reflects reality
// ---------------------------------------------------------------------------

// TestPartialFailure_CompletedItemsAreOnDisk verifies that plan items processed before a
// write failure are actually present on disk after the run.
func TestPartialFailure_CompletedItemsAreOnDisk(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// itemA is writable; itemB will fail because its target directory is blocked.
	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)

	// Block skills directory so itemB will fail.
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	// Partial failure should not return a top-level error; only Partial is set.
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Partial set: %v", err)
	}

	// itemA must be on disk even though itemB failed.
	expectedA := filepath.Join(workspace, itemA.TargetPath)
	if !fileExistsAt(t, expectedA) {
		t.Errorf("completed item %q not found at %q", itemA.Ref.Key, expectedA)
	}

	// ExecResult.Partial must be non-nil to signal the partial run.
	if result.Partial == nil {
		t.Error("ExecResult.Partial is nil; expected non-nil to indicate a partial run")
	}
}

// TestPartialFailure_FailedItemHasTakenFailed verifies that the ActionRecord for a failing
// item carries TakenFailed and a non-empty Err string.
func TestPartialFailure_FailedItemHasTakenFailed(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Locate the action record for itemB.
	var failedAction *domain.ActionRecord
	for i := range result.Actions {
		if result.Actions[i].Ref == itemB.Ref {
			failedAction = &result.Actions[i]
			break
		}
	}
	if failedAction == nil {
		t.Fatal("no action record found for the failing item")
	}
	if failedAction.Taken != domain.TakenFailed {
		t.Errorf("ActionRecord.Taken = %q; want %q", failedAction.Taken, domain.TakenFailed)
	}
	if failedAction.Err == "" {
		t.Error("ActionRecord.Err is empty; expected a non-empty error description for a failed item")
	}
}

// TestPartialFailure_ManifestReflectsCompletedItemsOnly verifies that after a partial run,
// the manifest contains entries for items that were successfully written and no entry for
// the item that failed (AC17.4: manifest reflects actual disk state, not intent).
func TestPartialFailure_ManifestReflectsCompletedItemsOnly(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// itemA must be in the manifest.
	_, foundA := result.Manifest.Lookup(itemA.Ref)
	if !foundA {
		t.Errorf("manifest does not contain entry for successfully deployed item %q", itemA.Ref.Key)
	}

	// itemB must NOT be in the manifest (it failed).
	_, foundB := result.Manifest.Lookup(itemB.Ref)
	if foundB {
		t.Errorf("manifest contains entry for failed item %q; it should not be present", itemB.Ref.Key)
	}
}

// TestPartialFailure_ManifestWrittenToDisk verifies that the manifest file is written to
// <workspace>/.mosaic/manifest.yaml even after a partial failure, so future runs have
// accurate state information (AC17.4).
func TestPartialFailure_ManifestWrittenToDisk(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	manifestPath := filepath.Join(workspace, ".mosaic", "manifest.yaml")
	if !fileExistsAt(t, manifestPath) {
		t.Errorf("manifest file not found at %q after partial run", manifestPath)
	}
}

// TestPartialFailure_PartialFieldCarriesError verifies that ExecResult.Partial is non-nil
// when a content write fails mid-plan (workspace mode, no fallback).
//
// A writable item is added first so the workspace probe succeeds and no fallback is triggered.
// The second item targets a blocked directory and fails mid-plan — this is a genuine partial
// failure that must set Partial. A complete fallback deployment (all items land in fallback
// root) is not a mid-plan stop and must NOT set Partial.
func TestPartialFailure_PartialFieldCarriesError(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// itemA writes to an unblocked path so the workspace probe succeeds.
	// itemB targets agents/, which is blocked as a file, causing a mid-plan write failure.
	itemA := newAgentItem("agent-ok", "ok/agent-ok.md", domain.ActionCreate)
	itemB := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "agents"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned top-level error; expected nil with Partial set: %v", err)
	}
	if result.Partial == nil {
		t.Error("ExecResult.Partial is nil; expected non-nil when a content write fails mid-plan")
	}
}

// ---------------------------------------------------------------------------
// T17.6 — Directory creation
// ---------------------------------------------------------------------------

// TestDirectoryCreation_NestedPathCreated verifies that the executor creates nested
// directories that do not yet exist before writing a plan item to them.
func TestDirectoryCreation_NestedPathCreated(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Use a deeply nested target path that does not exist.
	item := newAgentItem("deep-agent", "harness/subdir/agents/deep-agent.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# deep agent")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	expectedPath := filepath.Join(workspace, "harness", "subdir", "agents", "deep-agent.md")
	if !fileExistsAt(t, expectedPath) {
		t.Errorf("file at nested path %q not found; directories were not created", expectedPath)
	}
}

// TestDirectoryCreation_ExistingDirectoryIsNotAnError verifies that the executor does not
// return an error when a target directory already exists.
func TestDirectoryCreation_ExistingDirectoryIsNotAnError(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Pre-create the target directory.
	targetDir := filepath.Join(workspace, "agents")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("pre-create target dir: %v", err)
	}

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# runner")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Errorf("Execute returned error for pre-existing directory: %v", err)
	}
}

// TestDirectoryCreation_MultipleItemsWithDifferentDirs verifies that when a plan contains
// items in different directories, the executor creates each directory independently.
func TestDirectoryCreation_MultipleItemsWithDifferentDirs(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("skill-b", "skills/skill-b.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, item := range []domain.PlanItem{itemA, itemB} {
		expected := filepath.Join(workspace, item.TargetPath)
		if !fileExistsAt(t, expected) {
			t.Errorf("item %q not found at %q; directory creation may have failed", item.Ref.Key, expected)
		}
	}
}

// TestDirectoryCreation_HookNestedPath verifies that nested directories for hook bundle
// files are also created. Hook files target a TargetDir (from HookPlan) which may not exist.
func TestDirectoryCreation_HookNestedPath(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Create a source file for the hook.
	hookSrc := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hookSrc, []byte("#!/bin/sh\necho hook"), 0o755); err != nil {
		t.Fatalf("create hook source: %v", err)
	}

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files: []domain.HookFile{
			{SourcePath: hookSrc, TargetName: "hook.sh"},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	expected := filepath.Join(workspace, ".claude", "hooks", "hook.sh")
	if !fileExistsAt(t, expected) {
		t.Errorf("hook file not found at %q; nested hook directory was not created", expected)
	}
}
