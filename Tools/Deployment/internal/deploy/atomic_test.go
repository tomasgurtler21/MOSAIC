package deploy_test

// atomic_test.go covers the all-or-nothing execution mode introduced by ExecRequest.Atomic.
//
// When Atomic is true and any write fails mid-plan, the executor reverses every write it
// performed before the failure: files the run created are deleted, files the run overwrote
// are restored to their pre-run bytes, and the manifest and checklist file are left as they
// were before the run. A reversed run sets ExecResult.Reverted to true and ExecResult.Partial
// to the triggering error, preserving backward compatibility for callers that only inspect
// Partial.
//
// Four categories are tested:
//   1. Reversal of created files: files written before a failure are removed from disk.
//   2. Restoration of overwritten files: pre-existing files are restored byte-for-byte.
//   3. Manifest and checklist preservation, and the result signal.
//   4. Default (non-atomic), dry-run, and fallback paths are unchanged.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Reversal of created files
// ---------------------------------------------------------------------------

// TestAtomicReversal_CreatedFileNotOnDiskAfterFailure verifies that when atomic mode is
// enabled and a mid-plan write failure occurs, a file the run successfully created before
// the failure is removed from disk as part of the reversal.
func TestAtomicReversal_CreatedFileNotOnDiskAfterFailure(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	// Block skills directory so itemB fails mid-plan.
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Reverted set: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true after atomic reversal")
	}

	// itemA was created before the failure; after reversal it must not be on disk.
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("created file %q still exists after atomic reversal; should have been deleted", pathA)
	}
}

// TestAtomicReversal_AllCreatedFilesRemovedWhenFailureIsLate verifies that all files
// the run created before a late-plan failure are removed from disk after reversal, not
// only the most recently created one.
func TestAtomicReversal_AllCreatedFilesRemovedWhenFailureIsLate(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)
	itemC := newAgentItem("agent-c", "skills/agent-c.md", domain.ActionCreate)
	// Block skills directory so itemC fails; itemA and itemB succeed first.
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB, itemC),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# agent")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true after atomic reversal")
	}

	for _, item := range []domain.PlanItem{itemA, itemB} {
		p := filepath.Join(workspace, item.TargetPath)
		if fileExistsAt(t, p) {
			t.Errorf("file %q still exists after atomic reversal; all created files must be removed", p)
		}
	}
}

// TestAtomicReversal_OnlyRunCreatedFilesAreRemoved verifies that the reversal deletes
// only files the run itself created, not pre-existing files at unrelated paths in the
// same workspace.
func TestAtomicReversal_OnlyRunCreatedFilesAreRemoved(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Pre-create a file that the run does not touch.
	preExisting := filepath.Join(workspace, "agents", "pre-existing.md")
	if err := os.MkdirAll(filepath.Dir(preExisting), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(preExisting, []byte("# pre-existing"), 0o644); err != nil {
		t.Fatalf("write pre-existing: %v", err)
	}

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	// The pre-existing file must still be on disk after reversal.
	if !fileExistsAt(t, preExisting) {
		t.Errorf("pre-existing file %q was removed by reversal; only run-created files should be deleted", preExisting)
	}
}

// ---------------------------------------------------------------------------
// Restoration of overwritten files
// ---------------------------------------------------------------------------

// TestAtomicReversal_OverwrittenFileRestoredToOriginalContent verifies that a file that
// existed before the run and was overwritten by an early plan item has its original
// byte-exact content restored after a reversal triggered by a later item failing.
func TestAtomicReversal_OverwrittenFileRestoredToOriginalContent(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	originalContent := []byte("# original agent content — must survive reversal")

	// Pre-create the target file with known original content.
	targetRelPath := "agents/existing-agent.md"
	targetAbsPath := filepath.Join(workspace, targetRelPath)
	if err := os.MkdirAll(filepath.Dir(targetAbsPath), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(targetAbsPath, originalContent, 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// itemA overwrites the pre-existing file; itemB targets a blocked path and fails.
	itemA := newAgentItem("existing-agent", targetRelPath, domain.ActionUpdate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# new content — must not survive reversal")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	got := readFile(t, targetAbsPath)
	if !bytes.Equal(got, originalContent) {
		t.Errorf("restored file content = %q; want %q (original bytes)", got, originalContent)
	}
}

// TestAtomicReversal_MultipleOverwrittenFilesAllRestored verifies that when more than one
// pre-existing file is overwritten before a failure, all of them are restored to their
// respective original content after reversal.
func TestAtomicReversal_MultipleOverwrittenFilesAllRestored(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	origA := []byte("# original-a")
	origB := []byte("# original-b")

	pathA := filepath.Join(workspace, "agents", "agent-a.md")
	pathB := filepath.Join(workspace, "agents", "agent-b.md")

	if err := os.MkdirAll(filepath.Dir(pathA), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pathA, origA, 0o644); err != nil {
		t.Fatalf("write pathA: %v", err)
	}
	if err := os.WriteFile(pathB, origB, 0o644); err != nil {
		t.Fatalf("write pathB: %v", err)
	}

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionUpdate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionUpdate)
	itemC := newAgentItem("agent-c", "skills/agent-c.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB, itemC),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# replaced content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	if got := readFile(t, pathA); !bytes.Equal(got, origA) {
		t.Errorf("pathA content after reversal = %q; want original %q", got, origA)
	}
	if got := readFile(t, pathB); !bytes.Equal(got, origB) {
		t.Errorf("pathB content after reversal = %q; want original %q", got, origB)
	}
}

// TestAtomicReversal_NewlyCreatedFileIsDeletedNotRestored verifies that a file that did
// not exist before the run and was created by an early item is fully deleted (not merely
// zeroed) after reversal — the path must not exist at all.
func TestAtomicReversal_NewlyCreatedFileIsDeletedNotRestored(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	newFilePath := filepath.Join(workspace, "agents", "brand-new.md")

	itemA := newAgentItem("brand-new", "agents/brand-new.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# brand new")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	if fileExistsAt(t, newFilePath) {
		t.Errorf("newly created file %q still exists after reversal; it should have been deleted", newFilePath)
	}
}

// ---------------------------------------------------------------------------
// Manifest and checklist preservation, result signalling
// ---------------------------------------------------------------------------

// TestAtomicReversal_ManifestFileUnchangedAfterReversal verifies that a reverted atomic
// run leaves the on-disk manifest file byte-identical to what it was before the run started.
func TestAtomicReversal_ManifestFileUnchangedAfterReversal(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Establish a manifest on disk via a successful seed run.
	seedItem := newAgentItem("seed-agent", "agents/seed-agent.md", domain.ActionCreate)
	seedReq := deploy.ExecRequest{
		Plan:       newPlan(workspace, seedItem),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# seed")),
	}
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), seedReq); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	manifestPath := filepath.Join(workspace, ".mosaic", "manifest.yaml")
	manifestBefore := readFile(t, manifestPath)

	// Now run an atomic plan that will fail mid-plan.
	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# new agent")),
		Atomic:     true,
	}

	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	manifestAfter := readFile(t, manifestPath)
	if !bytes.Equal(manifestBefore, manifestAfter) {
		t.Errorf("manifest file changed after atomic reversal; want byte-identical to pre-run state")
	}
}

// TestAtomicReversal_ChecklistFileNotWrittenOnReversal verifies that a reverted run does
// not write the deployment checklist file to the workspace.
func TestAtomicReversal_ChecklistFileNotWrittenOnReversal(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}

	// TodoFilePath must be empty — no checklist was written on a reverted run.
	if result.TodoFilePath != "" {
		t.Errorf("result.TodoFilePath = %q; want empty string (no checklist on a reverted run)", result.TodoFilePath)
	}

	// Confirm the checklist file is not present on disk.
	checklistPath := filepath.Join(workspace, "MOSAIC-DEPLOYMENT-TODO.md")
	if fileExistsAt(t, checklistPath) {
		t.Errorf("checklist file %q exists on disk after atomic reversal; it must not be written", checklistPath)
	}
}

// TestAtomicReversal_PartialFieldCarriesOriginalError verifies that when a run is reverted,
// ExecResult.Partial is non-nil and carries the triggering error. This preserves backward
// compatibility for callers that only inspect Partial to detect failure.
func TestAtomicReversal_PartialFieldCarriesOriginalError(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}
	if result.Partial == nil {
		t.Error("ExecResult.Partial is nil on a reverted run; want non-nil to carry the triggering error")
	}
}

// TestAtomicReversal_RevertedDistinguishesFromUnreversedPartialRun verifies that a reverted
// atomic run and an unreversed partial run produce different result signals: both have Partial
// non-nil (the triggering error), but only the reverted run has Reverted true.
func TestAtomicReversal_RevertedDistinguishesFromUnreversedPartialRun(t *testing.T) {
	makeReq := func(workspace, mosaicRoot string, atomic bool) deploy.ExecRequest {
		itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
		itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
		blockPath(t, filepath.Join(workspace, "skills"))
		return deploy.ExecRequest{
			Plan:       newPlan(workspace, itemA, itemB),
			MosaicRoot: mosaicRoot,
			Content:    fixedContent([]byte("# content")),
			Atomic:     atomic,
		}
	}

	t.Run("non-atomic partial run has Reverted false", func(t *testing.T) {
		workspace := t.TempDir()
		mosaicRoot := t.TempDir()
		req := makeReq(workspace, mosaicRoot, false)
		ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
		result, err := ex.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Partial == nil {
			t.Error("Partial is nil; expected non-nil for a partial run")
		}
		if result.Reverted {
			t.Error("Reverted is true for a non-atomic run; want false")
		}
	})

	t.Run("atomic reverted run has Reverted true", func(t *testing.T) {
		workspace := t.TempDir()
		mosaicRoot := t.TempDir()
		req := makeReq(workspace, mosaicRoot, true)
		ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
		result, err := ex.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Partial == nil {
			t.Error("Partial is nil on a reverted run; want non-nil")
		}
		if !result.Reverted {
			t.Error("Reverted is false for an atomic run that failed; want true")
		}
	})
}

// TestAtomicReversal_RevertFailuresEmptyOnCleanReversal verifies that when the reversal
// itself succeeds (all journal entries are applied), RevertFailures is empty.
func TestAtomicReversal_RevertFailuresEmptyOnCleanReversal(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true")
	}
	if len(result.RevertFailures) != 0 {
		t.Errorf("RevertFailures has %d entries; want 0 for a clean reversal", len(result.RevertFailures))
	}
}

// ---------------------------------------------------------------------------
// Default (non-atomic), dry-run, and fallback paths are unchanged
// ---------------------------------------------------------------------------

// TestAtomicDisabled_PartialFailureLeavesFilesOnDisk verifies that when Atomic is false
// (the default), a partial failure leaves successfully-created files on disk, exactly as
// the pre-existing executor behaviour specifies.
func TestAtomicDisabled_PartialFailureLeavesFilesOnDisk(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "skills/agent-b.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "skills"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		// Atomic is not set — zero value is false, the default.
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Default mode: file created before failure must remain on disk (not reverted).
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if !fileExistsAt(t, pathA) {
		t.Errorf("itemA file %q not on disk after partial run; default mode must not revert", pathA)
	}

	if result.Reverted {
		t.Error("ExecResult.Reverted is true in default (non-atomic) mode; want false")
	}
	if result.Partial == nil {
		t.Error("ExecResult.Partial is nil; want non-nil for a partial run")
	}
}

// TestAtomicDisabled_RevertFieldsAreZeroValue verifies that in default (non-atomic) mode,
// Reverted is false and RevertFailures is empty even when the run fails partway.
func TestAtomicDisabled_RevertFieldsAreZeroValue(t *testing.T) {
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

	if result.Reverted {
		t.Error("ExecResult.Reverted is true in default mode; want false")
	}
	if len(result.RevertFailures) != 0 {
		t.Errorf("ExecResult.RevertFailures has %d entries in default mode; want 0", len(result.RevertFailures))
	}
}

// TestAtomicDryRun_NotAffectedByAtomicFlag verifies that when DryRun is true, setting
// Atomic has no effect: no writes occur, no reversal is performed, and Reverted is false.
func TestAtomicDryRun_NotAffectedByAtomicFlag(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		DryRun:     true,
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// DryRun writes nothing — there is nothing to revert.
	if result.Reverted {
		t.Error("ExecResult.Reverted is true for a dry-run; dry-run performs no writes and must not revert")
	}
	if result.Partial != nil {
		t.Errorf("ExecResult.Partial is non-nil for a dry-run: %v", result.Partial)
	}

	// No files should appear on disk.
	for _, item := range []domain.PlanItem{itemA, itemB} {
		p := filepath.Join(workspace, item.TargetPath)
		if fileExistsAt(t, p) {
			t.Errorf("file %q found on disk after dry-run; dry-run must not write files", p)
		}
	}
}

// TestAtomicFallback_NotAffectedByAtomicFlag verifies that when the workspace is unwritable
// and a fallback tier is used, setting Atomic has no effect: the run completes in the
// fallback location without reversal, and Reverted is false.
func TestAtomicFallback_NotAffectedByAtomicFlag(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Block the deployment directory inside the workspace so the probe write fails and
	// the executor falls back to the MOSAIC-root tier.
	item := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "agents"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// A fallback run is not a mid-plan stop and must never be reversed.
	if result.Reverted {
		t.Error("ExecResult.Reverted is true for a fallback run; fallback runs must never revert")
	}
	// Confirm a fallback tier was actually used (otherwise the test would give a false green
	// on the Reverted assertion above if the workspace was writable).
	if result.Fallback == domain.FallbackNone {
		t.Error("ExecResult.Fallback is FallbackNone; expected a fallback tier because the workspace probe was blocked")
	}
}

// ---------------------------------------------------------------------------
// Manifest save failure triggers reversal
// ---------------------------------------------------------------------------

// failSaveStore is a manifest.Store stub that delegates Load to the real store but always
// returns saveErr from Save. It is used to simulate manifest save failures in atomic tests
// without touching the filesystem or coupling to the store's internal write path.
type failSaveStore struct {
	inner   manifest.Store
	saveErr error
}

func (s *failSaveStore) Load(workspaceRoot string) (manifest.Snapshot, error) {
	return s.inner.Load(workspaceRoot)
}

func (s *failSaveStore) Save(workspaceRoot string, m domain.Manifest) error {
	return s.saveErr
}

// TestAtomicReversal_ManifestSaveFailureTriggersReversal verifies that when all plan items
// succeed but the manifest save fails in atomic mode, the run is reversed: files created by
// the run are deleted, ExecResult.Reverted is true, and ExecResult.Partial carries the save
// error. This pins the design contract that manifest save failure is a triggering failure
// equal in weight to an item write failure.
func TestAtomicReversal_ManifestSaveFailureTriggersReversal(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)

	store := &failSaveStore{
		inner:   manifest.NewStore(),
		saveErr: errors.New("simulated manifest save failure"),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Reverted set: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false after manifest save failure in atomic mode; want true")
	}
	if result.Partial == nil {
		t.Error("ExecResult.Partial is nil; want non-nil carrying the manifest save error")
	}

	// The item file was written before the manifest save failed; reversal must delete it.
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("created file %q still exists after reversal from manifest save failure; want deleted", pathA)
	}
}

// TestAtomicReversal_HookFilesReversedOnManifestSaveFailure verifies that hook bundle files
// written before a manifest save failure are included in the reversal. This pins the design
// contract that hook bundle file writes are journaled and therefore undone by a subsequent
// reversal, leaving the workspace in its pre-run state even when hooks succeeded but the
// manifest save then failed.
func TestAtomicReversal_HookFilesReversedOnManifestSaveFailure(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Write a real hook source file so deployHooks can read it.
	hookSrcDir := t.TempDir()
	hookSrcFile := filepath.Join(hookSrcDir, "hook.sh")
	if err := os.WriteFile(hookSrcFile, []byte("#!/bin/sh\necho hook"), 0o644); err != nil {
		t.Fatalf("write hook source file: %v", err)
	}

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: "hooks/my-hook",
		Files: []domain.HookFile{
			{SourcePath: hookSrcFile, TargetName: "hook.sh"},
		},
	}

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)

	store := &failSaveStore{
		inner:   manifest.NewStore(),
		saveErr: errors.New("simulated manifest save failure"),
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Hooks:      []domain.HookPlan{hookPlan},
		Atomic:     true,
	}

	ex := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Reverted set: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false after manifest save failure with hooks; want true")
	}

	// The hook file was written before the manifest save failed; reversal must delete it.
	hookDest := filepath.Join(workspace, "hooks/my-hook/hook.sh")
	if fileExistsAt(t, hookDest) {
		t.Errorf("hook file %q still exists after reversal; want deleted (hook writes must be journaled)", hookDest)
	}

	// The plan item file must also be reversed.
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("item file %q still exists after reversal; want deleted", pathA)
	}
}

// truncateThenFailStore is a manifest.Store that delegates Load to the real store but, on
// Save, truncates the manifest file to zero bytes (simulating the start of an os.WriteFile
// write that is interrupted after the file is opened and truncated) and then returns an
// error. This exercises the real-world scenario where a disk-full or I/O error occurs
// mid-write, leaving the manifest file in a corrupted (empty or partial) state on disk.
// The atomic journal, which records the pre-run manifest bytes before Save is called, must
// restore the manifest file to its original state during reversal.
type truncateThenFailStore struct {
	inner   manifest.Store
	saveErr error
}

func (s *truncateThenFailStore) Load(workspaceRoot string) (manifest.Snapshot, error) {
	return s.inner.Load(workspaceRoot)
}

func (s *truncateThenFailStore) Save(workspaceRoot string, _ domain.Manifest) error {
	// Truncate the manifest file to simulate a partial os.WriteFile: the destination is
	// opened and cleared before writing begins, then the write fails. Any existing content
	// is lost unless the journal restores it.
	manifestPath := filepath.Join(workspaceRoot, ".mosaic", "manifest.yaml")
	_ = os.WriteFile(manifestPath, []byte{}, 0o644)
	return s.saveErr
}

// TestAtomicReversal_ManifestRestoredWhenSavePartiallyWritesAndFails verifies that when the
// manifest save begins writing (truncating the file) and then fails, the atomic journal
// restores the manifest file byte-exactly to its pre-run state. This covers the real-world
// scenario of a disk-full or I/O error mid-write that leaves the manifest empty or corrupt.
// The existing TestAtomicReversal_ManifestSaveFailureTriggersReversal test uses a stub that
// returns an error without touching the filesystem; this test uses a real filesystem operation
// (truncation) to confirm that the journal entry recorded before store.Save() is called covers
// the manifest path itself and is applied during reversal.
func TestAtomicReversal_ManifestRestoredWhenSavePartiallyWritesAndFails(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Establish a manifest on disk via a successful seed run.
	seedItem := newAgentItem("seed-agent", "agents/seed-agent.md", domain.ActionCreate)
	seedReq := deploy.ExecRequest{
		Plan:       newPlan(workspace, seedItem),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# seed")),
	}
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), seedReq); err != nil {
		t.Fatalf("seed Execute: %v", err)
	}

	manifestPath := filepath.Join(workspace, ".mosaic", "manifest.yaml")
	manifestBefore := readFile(t, manifestPath)
	if len(manifestBefore) == 0 {
		t.Fatal("seed manifest is empty; cannot verify restoration")
	}

	// Run an atomic plan that writes items successfully but fails at manifest save with a
	// real filesystem truncation, leaving the manifest file empty on disk.
	store := &truncateThenFailStore{
		inner:   manifest.NewStore(),
		saveErr: errors.New("simulated partial-write failure"),
	}

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex2 := deploy.NewExecutor(store, newSpyLogger(), newSpyCollector())
	result, err := ex2.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Reverted set: %v", err)
	}

	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false after manifest save failure; want true")
	}
	if len(result.RevertFailures) != 0 {
		t.Errorf("RevertFailures has %d entries; want 0 for a clean reversal: %v", len(result.RevertFailures), result.RevertFailures)
	}

	// The journal recorded the manifest path before store.Save() was called. After reversal,
	// the manifest file must be byte-identical to its pre-run state — not empty or corrupt.
	manifestAfter := readFile(t, manifestPath)
	if !bytes.Equal(manifestAfter, manifestBefore) {
		t.Errorf("manifest file after reversal = %q; want pre-run bytes %q (journal must restore manifest from partial write)", manifestAfter, manifestBefore)
	}

	// The item file written before the manifest save must also be reversed.
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("item file %q still exists after reversal; want deleted", pathA)
	}
}

// TestAtomicSuccessfulRun_RevertedIsFalse verifies that a successful atomic run (all items
// written without error) does not set Reverted — reversal applies only when the run fails.
func TestAtomicSuccessfulRun_RevertedIsFalse(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# content")),
		Atomic:     true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Reverted {
		t.Error("ExecResult.Reverted is true after a successful atomic run; want false")
	}
	if result.Partial != nil {
		t.Errorf("ExecResult.Partial is non-nil after a successful run: %v", result.Partial)
	}

	// All files must be on disk after a successful run.
	for _, item := range []domain.PlanItem{itemA, itemB} {
		p := filepath.Join(workspace, item.TargetPath)
		if !fileExistsAt(t, p) {
			t.Errorf("file %q missing after successful atomic run", p)
		}
	}
}
