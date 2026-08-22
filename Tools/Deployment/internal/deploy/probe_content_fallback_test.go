package deploy_test

// probe_content_fallback_test.go covers the writability probe fallback behavior when
// req.Content fails for one or more probe-eligible items.
//
// Scenario (a): when the first probe-eligible item's Content callback returns an error,
// the probe tries the next eligible item and succeeds; the overall run completes.
//
// Scenario (b): when all probe-eligible items fail Content rendering, resolveDeploymentRoot
// fails and the run returns an error. This scenario is a regression guard: both the current
// and new implementations return a non-nil error, so the test documents the contract without
// being a RED test. The test is still valuable as a boundary condition spec.
//
// Scenario (c): the item whose Content callback failed is surfaced in ExecResult.Actions as
// a TakenFailed record rather than being silently dropped. The TargetPath appears at most once
// in Actions (the item is not double-processed in the main execution loop).
//
// Scenario (d): when the first probe-eligible item's Content succeeds, behavior is identical
// to the pre-change implementation (regression guard).
//
// Atomic-mode journal: when the first probe candidate's Content fails and the second is used
// for the actual probe write, the journal pre-record must cover the item that was actually
// written (the second), not the item that failed content (the first). This ensures atomic-mode
// reversal correctly removes the probe write if the run fails later.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Helpers local to this file
// ---------------------------------------------------------------------------

// contentWithItemErrors returns a Content function that returns an error for any item
// whose TargetPath is a key in errForPaths. All other items receive validData and no error.
func contentWithItemErrors(errForPaths map[string]error, validData []byte) func(domain.PlanItem) ([]byte, error) {
	return func(item domain.PlanItem) ([]byte, error) {
		if err, ok := errForPaths[item.TargetPath]; ok {
			return nil, err
		}
		return validData, nil
	}
}

// findActionByRef returns the first ActionRecord whose Ref equals ref, or (zero, false).
func findActionByRef(actions []domain.ActionRecord, ref domain.ArtifactRef) (domain.ActionRecord, bool) {
	for _, a := range actions {
		if a.Ref == ref {
			return a, true
		}
	}
	return domain.ActionRecord{}, false
}

// countActionsByRef counts how many ActionRecords have a Ref equal to ref.
func countActionsByRef(actions []domain.ActionRecord, ref domain.ArtifactRef) int {
	n := 0
	for _, a := range actions {
		if a.Ref == ref {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Scenario (a) — first probe item's Content fails, second probe item succeeds
// ---------------------------------------------------------------------------

// TestProbeContentFallback_FirstItemFails_RunSucceedsWithSecondItem verifies that when the
// first probe-eligible item's Content callback returns an error, the probe falls back to the
// next eligible item and the overall run completes without a fatal error. The run must not
// abort simply because a single item cannot produce content.
func TestProbeContentFallback_FirstItemFails_RunSucceedsWithSecondItem(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			[]byte("# agent content"),
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: expected nil error when second probe item succeeds, got: %v", err)
	}
}

// TestProbeContentFallback_FirstItemFails_SecondItemWrittenToDeploymentRoot verifies that
// when the first probe item's Content fails and the second succeeds, the second item is
// physically written to the deployment root. The probe must not leave the second item
// undeployed simply because it was used as the probe candidate.
func TestProbeContentFallback_FirstItemFails_SecondItemWrittenToDeploymentRoot(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	agentContent := []byte("# agent-b content")
	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			agentContent,
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	expectedPath := filepath.Join(result.DeploymentRoot, itemB.TargetPath)
	if !fileExistsAt(t, expectedPath) {
		t.Errorf("expected item-b at %q after successful probe with second item; file not found", expectedPath)
	}
}

// ---------------------------------------------------------------------------
// Scenario (c) — failed probe item surfaced as TakenFailed in result
// ---------------------------------------------------------------------------

// TestProbeContentFallback_FirstItemFails_FailedItemReportedAsTakenFailed verifies that
// when the first probe-eligible item's Content callback fails, that item appears in
// ExecResult.Actions with Taken == TakenFailed. The item must not be silently dropped; it
// must be reportable to the user as a failed deployment item.
func TestProbeContentFallback_FirstItemFails_FailedItemReportedAsTakenFailed(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			[]byte("# agent content"),
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	action, found := findActionByRef(result.Actions, itemA.Ref)
	if !found {
		t.Fatalf("item-a has no ActionRecord in result.Actions; expected a TakenFailed entry")
	}
	if action.Taken != domain.TakenFailed {
		t.Errorf("item-a ActionRecord.Taken = %q; want %q", action.Taken, domain.TakenFailed)
	}
	if action.Err == "" {
		t.Errorf("item-a ActionRecord.Err is empty; want a non-empty error description for the content-render failure")
	}
}

// TestProbeContentFallback_FirstItemFails_FailedItemNotDoubleProcessed verifies that a
// probe-skipped item recorded as TakenFailed from the content error is not also processed
// in the main execution loop. The item must appear exactly once in result.Actions.
func TestProbeContentFallback_FirstItemFails_FailedItemNotDoubleProcessed(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			[]byte("# agent content"),
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	count := countActionsByRef(result.Actions, itemA.Ref)
	if count != 1 {
		t.Errorf("item-a appears %d time(s) in result.Actions; want exactly 1 (must not be double-processed)", count)
	}
}

// TestProbeContentFallback_FirstItemFails_FailedItemFileNotCreated verifies that the item
// whose Content failed is not present on disk. No file must be created for an item whose
// content could not be rendered, even if the overall run succeeds via a subsequent probe item.
func TestProbeContentFallback_FirstItemFails_FailedItemFileNotCreated(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			[]byte("# agent content"),
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	// item-a must not be on disk; its content could not be produced.
	pathA := filepath.Join(result.DeploymentRoot, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("item-a file found at %q; no file should be created for an item whose content failed", pathA)
	}
}

// ---------------------------------------------------------------------------
// Scenario (b) — all probe-eligible items fail Content (regression guard)
// ---------------------------------------------------------------------------

// TestProbeContentFallback_AllItemsFailContent_RunReturnsError verifies that when every
// probe-eligible item's Content callback returns an error, resolveDeploymentRoot cannot
// establish a deployment root and the run returns a non-nil error. This is a regression
// guard: the current implementation also fails when the single probe item fails, so this
// test passes both before and after the multi-item iteration change.
func TestProbeContentFallback_AllItemsFailContent_RunReturnsError(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{
				itemA.TargetPath: errors.New("content render error for agent-a"),
				itemB.TargetPath: errors.New("content render error for agent-b"),
			},
			nil, // validData unreachable when all items error
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if err == nil {
		t.Error("Execute: expected non-nil error when all probe-eligible items fail Content; got nil")
	}
}

// TestProbeContentFallback_AllItemsFailContent_ErrorIsNotNoWritableLocation verifies that
// when all items fail Content rendering, the returned error is NOT ErrNoWritableLocation.
// A content-render failure is a different kind of problem from every tier being unwritable,
// and the error must be distinguishable so callers can report the correct root cause.
func TestProbeContentFallback_AllItemsFailContent_ErrorIsNotNoWritableLocation(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{
				itemA.TargetPath: errors.New("content render error for agent-a"),
				itemB.TargetPath: errors.New("content render error for agent-b"),
			},
			nil,
		),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	_, err := ex.Execute(context.Background(), req)
	if errors.Is(err, deploy.ErrNoWritableLocation) {
		t.Error("Execute: error is ErrNoWritableLocation; content-render failures must produce a distinct error")
	}
}

// ---------------------------------------------------------------------------
// Scenario (d) — first probe item succeeds (regression guard)
// ---------------------------------------------------------------------------

// TestProbeContentFallback_FirstItemSucceeds_RunBehaviorUnchanged verifies that when the
// first probe-eligible item's Content callback succeeds, the run completes successfully and
// its outcome is identical to the pre-change behavior. This regression guard ensures the
// multi-item iteration does not affect the common path.
func TestProbeContentFallback_FirstItemSucceeds_RunBehaviorUnchanged(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# agent-a content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	// Deployment root is the workspace (probe succeeded on first try).
	if result.DeploymentRoot != workspace {
		t.Errorf("DeploymentRoot = %q; want workspace %q", result.DeploymentRoot, workspace)
	}
	if result.Fallback != domain.FallbackNone {
		t.Errorf("Fallback = %q; want %q (no fallback when first probe item succeeds)", result.Fallback, domain.FallbackNone)
	}

	// item-a must be present on disk.
	expectedPath := filepath.Join(workspace, itemA.TargetPath)
	if !fileExistsAt(t, expectedPath) {
		t.Errorf("expected item-a at %q; file not found", expectedPath)
	}
}

// TestProbeContentFallback_FirstItemSucceeds_NoTakenFailedActions verifies that when the
// first probe-eligible item succeeds, there are no TakenFailed action records produced by
// the probe phase. The regression guard ensures the new probe iteration does not incorrectly
// inject failure records on the happy path.
func TestProbeContentFallback_FirstItemSucceeds_NoTakenFailedActions(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("# agent content")),
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	for _, action := range result.Actions {
		if action.Taken == domain.TakenFailed {
			t.Errorf("unexpected TakenFailed action for %q; no content failures expected on happy path", action.Ref.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Atomic-mode journal alignment
// ---------------------------------------------------------------------------

// TestProbeContentFallback_AtomicMode_UsesActuallyWrittenItemForJournal verifies that in
// atomic mode, when the first probe candidate's Content fails and the second candidate is
// used for the probe write, the atomic-mode journal covers the item that was actually
// written (the second). If the run fails later and reversal is triggered, the second item's
// probe write is undone — it must not remain on disk after reversal.
//
// Setup:
//   - itemA: Content returns an error (probe skips it, writes nothing to disk)
//   - itemB: Content succeeds (used as the actual probe, file written to workspace)
//   - itemC: write is blocked (triggers atomic reversal when main loop tries to write it)
//
// Expected: Execute returns nil error, result.Reverted is true, itemB's file does not exist.
//
// Current behavior: Execute returns a non-nil error (current code fails the run the moment
// itemA's Content callback fails), so this test is in RED phase.
func TestProbeContentFallback_AtomicMode_UsesActuallyWrittenItemForJournal(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)
	itemC := newAgentItem("agent-c", "skills/agent-c.md", domain.ActionCreate)

	// Block itemC's write path so the main execution loop fails mid-plan.
	blockPath(t, filepath.Join(workspace, "skills"))

	contentErr := errors.New("content render error for agent-a")
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB, itemC),
		MosaicRoot: mosaicRoot,
		Content: contentWithItemErrors(
			map[string]error{itemA.TargetPath: contentErr},
			[]byte("# agent content"),
		),
		Atomic: true,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error; expected nil with Reverted set: %v", err)
	}

	// The run must have reverted due to itemC's write failure.
	if !result.Reverted {
		t.Fatal("ExecResult.Reverted is false; expected true after atomic reversal triggered by itemC write failure")
	}

	// itemB was written by the probe; after reversal it must not be on disk.
	// If the journal incorrectly pre-recorded itemA's path (which was never written),
	// itemB's probe file would survive the reversal — the test would catch that.
	pathB := filepath.Join(workspace, itemB.TargetPath)
	if fileExistsAt(t, pathB) {
		t.Errorf("itemB file found at %q after atomic reversal; the probe write must be journaled and undone on reversal", pathB)
	}

	// itemA must not be on disk (its content failed; nothing was written).
	pathA := filepath.Join(workspace, itemA.TargetPath)
	if fileExistsAt(t, pathA) {
		t.Errorf("itemA file found at %q; nothing should be written for an item whose content callback failed", pathA)
	}

	// Sanity: itemC's directory is a blocking file, so itemC's target file does not exist.
	// (This is a setup sanity check, not a behavioral assertion about the probe.)
	pathC := filepath.Join(workspace, itemC.TargetPath)
	if fileExistsAt(t, pathC) {
		t.Errorf("itemC file found at %q; write should have been blocked by the skills-directory blocker", pathC)
	}

	// itemA must appear as TakenFailed (content error surfaced regardless of reversal).
	actionA, found := findActionByRef(result.Actions, itemA.Ref)
	if !found {
		t.Error("itemA has no ActionRecord in result.Actions; content-error items must be surfaced even when the run reverts")
	} else if actionA.Taken != domain.TakenFailed {
		t.Errorf("itemA ActionRecord.Taken = %q; want %q", actionA.Taken, domain.TakenFailed)
	}
}
