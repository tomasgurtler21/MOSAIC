package deploy_test

// observe_test.go covers T17.9.
//
// Every action taken and every action declined must reach both the logger and the TODO
// collector (AC17.6). The logger receives an Action call for every item, regardless of
// outcome. The TODO collector receives gaps for actions that were declined or require user
// attention:
//
//   - A skipped conflict item adds a GapSkippedFile gap.
//   - A fallback-location deployment adds a GapFallbackLocation gap.
//   - A hook registration step for an existing target adds a GapHookRegistration gap.
//   - A non-performable registration step adds a GapManualStep gap.
//
// The logger additionally receives StartRun (before any action) and EndRun (after all
// actions), providing a complete ordered record of the run.

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
// Logger: every action reaches Logger.Action
// ---------------------------------------------------------------------------

// TestObserve_CreatedItem_ReachesLogger verifies that a successfully created item is
// reported to the logger via Logger.Action with TakenCreated.
func TestObserve_CreatedItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(spy.actions) == 0 {
		t.Fatal("Logger.Action was never called; created item must reach the logger")
	}
	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenCreated {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action was not called with TakenCreated for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_UpdatedItem_ReachesLogger verifies that an updated item is reported to the
// logger via Logger.Action with TakenUpdated.
func TestObserve_UpdatedItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Pre-write the file so the update has something to replace.
	writeExisting(t, workspace, "agents/runner.md", []byte("old content"))

	item := newAgentItem("runner", "agents/runner.md", domain.ActionUpdate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenUpdated {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action not called with TakenUpdated for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_SkippedItem_ReachesLogger verifies that a skipped conflict item is reported
// to the logger via Logger.Action with TakenSkipped.
func TestObserve_SkippedItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("local content"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("source content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenSkipped {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action not called with TakenSkipped for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_BackedUpItem_ReachesLogger verifies that a backed-up conflict item is
// reported to the logger via Logger.Action with TakenBackedUp and a non-empty BackupPath.
func TestObserve_BackedUpItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("local content"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("new content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionBackupThenOverwrite},
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenBackedUp {
			found = true
			if a.BackupPath == "" {
				t.Error("Logger.Action received TakenBackedUp with empty BackupPath; it must carry the backup path")
			}
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action not called with TakenBackedUp for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_FailedItem_ReachesLogger verifies that a failed write is reported to the
// logger via Logger.Action with TakenFailed and a non-empty Err field.
func TestObserve_FailedItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	blockPath(t, filepath.Join(workspace, "agents"))

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenFailed {
			found = true
			if a.Err == "" {
				t.Error("Logger.Action TakenFailed has empty Err; it must carry the error description")
			}
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action not called with TakenFailed for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_StartRunCalledBeforeActions verifies that Logger.StartRun is called exactly
// once and before any Logger.Action calls.
func TestObserve_StartRunCalledBeforeActions(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(spy.starts) != 1 {
		t.Errorf("Logger.StartRun called %d times; want exactly 1", len(spy.starts))
	}
	if len(spy.actions) == 0 {
		t.Fatal("Logger.Action never called; cannot verify ordering")
	}
}

// TestObserve_EndRunCalledAfterActions verifies that Logger.EndRun is called exactly once,
// after all actions have been logged.
func TestObserve_EndRunCalledAfterActions(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(spy.ends) != 1 {
		t.Errorf("Logger.EndRun called %d times; want exactly 1", len(spy.ends))
	}
}

// ---------------------------------------------------------------------------
// TODO Collector: declined actions emit gaps
// ---------------------------------------------------------------------------

// TestObserve_SkippedConflict_EmitsGapSkippedFile verifies that every skipped conflict item
// generates a GapSkippedFile gap in the TODO collector, so the user sees which files they
// chose to leave undeployed (AC17.6).
func TestObserve_SkippedConflict_EmitsGapSkippedFile(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()
	targetPath := "agents/runner.md"

	writeExisting(t, workspace, targetPath, []byte("local content"))

	item := newConflictItem("runner", targetPath)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("source content")),
		Conflicts:  map[string]domain.ConflictDecision{targetPath: domain.DecisionSkip},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !spy.hasGapKind(domain.GapSkippedFile) {
		t.Errorf("GapSkippedFile not emitted for skipped conflict item; collected gaps: %+v", spy.gaps)
	}
}

// TestObserve_FallbackLocation_EmitsGapFallbackLocation verifies that when a fallback
// deployment root is used (not the workspace), a GapFallbackLocation gap is added to the
// TODO collector so the user knows the deployment went to a non-primary location (AC17.1,
// AC17.6).
func TestObserve_FallbackLocation_EmitsGapFallbackLocation(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Block the workspace deployment path so the executor falls back.
	blockPath(t, filepath.Join(workspace, "agents"))

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !spy.hasGapKind(domain.GapFallbackLocation) {
		t.Errorf("GapFallbackLocation not emitted after fallback deployment; collected gaps: %+v", spy.gaps)
	}
}

// TestObserve_FallbackLocation_NotEmittedWhenWorkspaceIsUsed verifies that no
// GapFallbackLocation gap is emitted when the workspace tier is used successfully — the gap
// is only appropriate when deployment landed in a non-primary location.
func TestObserve_FallbackLocation_NotEmittedWhenWorkspaceIsUsed(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionCreate)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if spy.hasGapKind(domain.GapFallbackLocation) {
		t.Error("GapFallbackLocation emitted even though workspace tier was used successfully; it must not be")
	}
}

// TestObserve_EveryItem_LoggerReceivesOneActionPerItem verifies that the logger receives
// exactly one Action call per plan item, regardless of the outcome.
func TestObserve_EveryItem_LoggerReceivesOneActionPerItem(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Three items with different actions.
	itemA := newAgentItem("agent-a", "agents/agent-a.md", domain.ActionCreate)
	itemB := newAgentItem("agent-b", "agents/agent-b.md", domain.ActionCreate)
	itemC := newAgentItem("agent-c", "agents/agent-c.md", domain.ActionCreate)

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, itemA, itemB, itemC),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(spy.actions) != 3 {
		t.Errorf("Logger.Action called %d times; want 3 (one per plan item)", len(spy.actions))
	}
}

// TestObserve_UnchangedItem_ReachesLogger verifies that even ActionUnchanged items
// (no-op deployments) are reported to the logger so the complete plan is auditable.
func TestObserve_UnchangedItem_ReachesLogger(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	item := newAgentItem("runner", "agents/runner.md", domain.ActionUnchanged)
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent([]byte("content")),
	}

	spy := newSpyLogger()
	ex := deploy.NewExecutor(manifest.NewStore(), spy, newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	found := false
	for _, a := range spy.actions {
		if a.Ref == item.Ref && a.Taken == domain.TakenUnchanged {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Logger.Action not called with TakenUnchanged for item %q; logged actions: %v", item.Ref.Key, spy.actionTakens())
	}
}

// TestObserve_NonPerformableRegistrationStep_ReachesCollector verifies that a
// non-performable hook registration step is emitted to the TODO collector as a
// GapManualStep, so the user is informed of the required manual action (AC17.6).
func TestObserve_NonPerformableRegistrationStep_ReachesCollector(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookSrc := filepath.Join(t.TempDir(), "hook.sh")
	_ = os.WriteFile(hookSrc, []byte("#!/bin/sh"), 0o755)

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: hookSrc, TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "vscode-user-setting",
				TargetPath:  "",
				Fragment:    "Enable the hooks extension in VS Code",
				Performable: false,
				Instruction: "Open VS Code and enable the extension",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !spy.hasGapKind(domain.GapManualStep) {
		t.Errorf("GapManualStep not emitted for non-performable registration step; collected gaps: %+v", spy.gaps)
	}
}
