package app_test

// conflict_test.go verifies the conflict-resolution apply-to-all latch behavior (T2.1)
// and the DeployNew ConflictDefault non-interactive mechanism (T2.2).
//
// Verified invariants:
//
// Apply-to-all latch (T2.1):
//   - When the user selects a conflict decision with one of the "all:" compound options,
//     all subsequent conflict items in the same run receive that decision without being asked.
//   - The latch suppresses asking for each of the three decision types:
//     Overwrite, Skip, and Backup-then-overwrite.
//   - File-by-file resolution continues to work when an "all:" option is not chosen;
//     each conflict item is still asked individually.
//   - The latch behavior is identical in the DeployNew and Update flows.
//
// DeployNew ConflictDefault (T2.2):
//   - DeployRequest carries a ConflictDefault field.
//   - When ConflictDefault is set, no QLocalModification questions are asked and all
//     conflict items receive the pre-set decision.
//   - A ConflictDefault of DecisionSkip adds GapSkippedFile entries, matching
//     Update's existing behavior.
//   - Update's existing ConflictDefault mechanism is unchanged.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// threeConflictDeployPlan returns a ModeDeployWorkspace plan with three ActionConflict items.
func threeConflictDeployPlan(workspace string) domain.Plan {
	mkConflict := func(key, path string) domain.PlanItem {
		return domain.PlanItem{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
			TargetPath: path,
			Action:     domain.ActionConflict,
			Conflict: &domain.LocalModification{
				RecordedHash: "sha256:old",
				CurrentHash:  "sha256:new",
			},
		}
	}
	return domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			mkConflict("agent-a", "agent-a.md"),
			mkConflict("agent-b", "agent-b.md"),
			mkConflict("agent-c", "agent-c.md"),
		},
		Workflows: []string{"quick-fix"},
	}
}

// threeConflictUpdatePlan returns the same plan in ModeUpdateWorkspace.
func threeConflictUpdatePlan(workspace string) domain.Plan {
	p := threeConflictDeployPlan(workspace)
	p.Mode = domain.ModeUpdateWorkspace
	return p
}

// countConflictQuestions returns the total number of QLocalModification calls recorded
// by the stub, regardless of subject (target path).
func countConflictQuestions(stub *interactiontest.Stub) int {
	n := 0
	for _, c := range stub.Calls() {
		if c.ID == domain.QLocalModification {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Apply-to-all latch — DeployNew flow (T2.1a, T2.1b, T2.1c)
// ---------------------------------------------------------------------------

// TestDeployNew_ApplyToAllOverwrite_RemainingConflictsNotAsked verifies that when the user
// selects the "all:overwrite" compound option on the first conflict, all remaining conflict
// items in the same run are resolved without prompting.
//
// RED: the current DeployNew conflict loop calls askLocalModification for every item and does
// not interpret the "all:" prefix as a latch signal.
func TestDeployNew_ApplyToAllOverwrite_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange — first conflict answered with compound "apply to all" option
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:overwrite").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — only the first conflict should have been asked
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md even though the user chose " +
			"'all:overwrite' on agent-a.md; the apply-to-all latch must suppress remaining conflict prompts")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md even though the user chose " +
			"'all:overwrite' on agent-a.md; the apply-to-all latch must suppress remaining conflict prompts")
	}
}

// TestDeployNew_ApplyToAllSkip_RemainingConflictsNotAsked verifies that the apply-to-all latch
// works when the user selects the "all:skip" compound option.
//
// RED: same root cause as the Overwrite variant above.
func TestDeployNew_ApplyToAllSkip_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:skip").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first conflict must be asked, then the latch suppresses the rest
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md; " +
			"'all:skip' must latch the skip decision and suppress subsequent conflict prompts")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md; " +
			"'all:skip' must latch the skip decision and suppress subsequent conflict prompts")
	}
}

// TestDeployNew_ApplyToAllBackup_RemainingConflictsNotAsked verifies that the apply-to-all latch
// works when the user selects the "all:backup-then-overwrite" compound option.
//
// RED: same root cause as the Overwrite and Skip variants above.
func TestDeployNew_ApplyToAllBackup_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:backup-then-overwrite").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first conflict must be asked, then the latch suppresses the rest
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md; " +
			"'all:backup-then-overwrite' must latch and suppress subsequent conflict prompts")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md; " +
			"'all:backup-then-overwrite' must latch and suppress subsequent conflict prompts")
	}
}

// TestDeployNew_FileByFile_EachConflictAsked verifies that file-by-file conflict resolution
// still works when no "all:" compound option is chosen: each conflict item is asked individually.
func TestDeployNew_FileByFile_EachConflictAsked(t *testing.T) {
	// Arrange — all three answered with per-file options (no apply-to-all)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", string(domain.DecisionOverwrite)).
		AnswerSelectOne(domain.QLocalModification, "agent-b.md", string(domain.DecisionSkip)).
		AnswerSelectOne(domain.QLocalModification, "agent-c.md", string(domain.DecisionBackupThenOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — every conflict item must prompt when no apply-to-all option is selected
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md; " +
			"file-by-file resolution must ask every conflict when no apply-to-all option is chosen")
	}
	if !stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was not asked for agent-b.md; " +
			"file-by-file resolution must ask every conflict when no apply-to-all option is chosen")
	}
	if !stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was not asked for agent-c.md; " +
			"file-by-file resolution must ask every conflict when no apply-to-all option is chosen")
	}
}

// ---------------------------------------------------------------------------
// Apply-to-all latch — Update flow (T2.1d)
// ---------------------------------------------------------------------------

// TestUpdate_ApplyToAllOverwrite_RemainingConflictsNotAsked verifies that the apply-to-all
// latch works identically in the Update flow: selecting "all:overwrite" on the first conflict
// suppresses all subsequent conflict prompts.
//
// RED: the current Update conflict loop calls askLocalModification for every item and does not
// implement an apply-to-all latch.
func TestUpdate_ApplyToAllOverwrite_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:overwrite").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first conflict must be asked, then the latch suppresses the rest
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md in the Update flow; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md in the Update flow even though " +
			"'all:overwrite' was selected on agent-a.md; the apply-to-all latch must work " +
			"identically in DeployNew and Update")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md in the Update flow even though " +
			"'all:overwrite' was selected on agent-a.md; the latch must suppress all remaining conflicts")
	}
}

// TestUpdate_ApplyToAllSkip_RemainingConflictsNotAsked verifies the latch works in Update
// when "all:skip" is selected.
//
// RED: same root cause as the Overwrite variant above.
func TestUpdate_ApplyToAllSkip_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:skip").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first conflict must be asked, then the latch suppresses the rest
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md in the Update flow; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md in Update; " +
			"'all:skip' latch must suppress remaining conflict prompts in the Update flow")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md in Update; " +
			"'all:skip' latch must suppress remaining conflict prompts in the Update flow")
	}
}

// TestUpdate_ApplyToAllBackup_RemainingConflictsNotAsked verifies that the apply-to-all latch
// works in the Update flow when the user selects the "all:backup-then-overwrite" compound option.
//
// RED: the current Update conflict loop has no apply-to-all latch; it asks every conflict
// regardless of the compound option selected.
func TestUpdate_ApplyToAllBackup_RemainingConflictsNotAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", "all:backup-then-overwrite").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first conflict must be asked, then the latch suppresses the rest
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md in the Update flow; " +
			"the first conflict in a run must always be prompted")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was asked for agent-b.md in Update; " +
			"'all:backup-then-overwrite' latch must suppress remaining conflict prompts in the Update flow")
	}
	if stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was asked for agent-c.md in Update; " +
			"'all:backup-then-overwrite' latch must suppress remaining conflict prompts in the Update flow")
	}
}

// TestUpdate_FileByFile_EachConflictAsked verifies that file-by-file conflict resolution
// continues to work in the Update flow when no "all:" option is chosen.
func TestUpdate_FileByFile_EachConflictAsked(t *testing.T) {
	// Arrange — all three conflicts answered with per-file options
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agent-a.md", string(domain.DecisionOverwrite)).
		AnswerSelectOne(domain.QLocalModification, "agent-b.md", string(domain.DecisionSkip)).
		AnswerSelectOne(domain.QLocalModification, "agent-c.md", string(domain.DecisionBackupThenOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — every conflict must be asked in file-by-file mode
	if !stub.WasAsked(domain.QLocalModification, "agent-a.md") {
		t.Error("QLocalModification was not asked for agent-a.md in Update flow")
	}
	if !stub.WasAsked(domain.QLocalModification, "agent-b.md") {
		t.Error("QLocalModification was not asked for agent-b.md in Update flow; " +
			"file-by-file mode must ask every conflict")
	}
	if !stub.WasAsked(domain.QLocalModification, "agent-c.md") {
		t.Error("QLocalModification was not asked for agent-c.md in Update flow; " +
			"file-by-file mode must ask every conflict")
	}
}

// ---------------------------------------------------------------------------
// DeployNew ConflictDefault — non-interactive (T2.2)
// ---------------------------------------------------------------------------

// TestDeployNew_ConflictDefault_Overwrite_NoQLocalModificationAsked verifies that when
// DeployRequest.ConflictDefault is set, the flow never asks QLocalModification for any
// conflict item in the plan.
//
// RED: DeployNew does not yet check req.ConflictDefault before calling askLocalModification.
func TestDeployNew_ConflictDefault_Overwrite_NoQLocalModificationAsked(t *testing.T) {
	// Arrange — three conflicts, no scripted answers; ConflictDefault should pre-empt all asks
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionOverwrite,
	})

	// Assert
	total := countConflictQuestions(stub)
	if total != 0 {
		t.Errorf("QLocalModification was asked %d time(s) with ConflictDefault set to %q; "+
			"want 0; a non-empty ConflictDefault must suppress all conflict prompts in DeployNew",
			total, domain.DecisionOverwrite)
	}
}

// TestDeployNew_ConflictDefault_Skip_NoQLocalModificationAsked verifies that ConflictDefault
// DecisionSkip also suppresses all conflict prompts in DeployNew.
//
// RED: same root cause as the Overwrite variant.
func TestDeployNew_ConflictDefault_Skip_NoQLocalModificationAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionSkip,
	})

	// Assert
	total := countConflictQuestions(stub)
	if total != 0 {
		t.Errorf("QLocalModification was asked %d time(s) with ConflictDefault = %q; want 0",
			total, domain.DecisionSkip)
	}
}

// TestDeployNew_ConflictDefault_Backup_NoQLocalModificationAsked verifies ConflictDefault
// DecisionBackupThenOverwrite suppresses all conflict prompts in DeployNew.
//
// RED: same root cause as the Overwrite variant.
func TestDeployNew_ConflictDefault_Backup_NoQLocalModificationAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictDeployPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionBackupThenOverwrite,
	})

	// Assert
	total := countConflictQuestions(stub)
	if total != 0 {
		t.Errorf("QLocalModification was asked %d time(s) with ConflictDefault = %q; want 0",
			total, domain.DecisionBackupThenOverwrite)
	}
}

// TestDeployNew_ConflictDefault_Skip_AddsGapSkippedFile verifies that when ConflictDefault
// is DecisionSkip, DeployNew records a GapSkippedFile gap for each skipped conflict item,
// matching Update's existing behavior.
//
// RED: DeployNew does not check ConflictDefault, so the gap is never added via the non-interactive
// path.
func TestDeployNew_ConflictDefault_Skip_AddsGapSkippedFile(t *testing.T) {
	// Arrange — single conflict item; ConflictDefault = Skip must add a gap
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"},
				TargetPath: "agent-a.md",
				Action:     domain.ActionConflict,
				Conflict: &domain.LocalModification{
					RecordedHash: "sha256:old",
					CurrentHash:  "sha256:new",
				},
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionSkip,
	})

	// Assert — the non-interactive path must not ask any conflict questions
	if n := countConflictQuestions(stub); n != 0 {
		t.Errorf("QLocalModification was asked %d time(s) with ConflictDefault = %q; "+
			"want 0; a non-empty ConflictDefault must suppress all conflict prompts in DeployNew",
			n, domain.DecisionSkip)
	}
	// Assert — a skipped conflict must produce a GapSkippedFile, matching Update's behavior
	if !spy.hasGapKind(domain.GapSkippedFile) {
		t.Error("GapSkippedFile was not recorded when ConflictDefault is DecisionSkip in DeployNew; " +
			"a skipped conflict must produce a gap in both DeployNew and Update flows")
	}
}

// ---------------------------------------------------------------------------
// Update ConflictDefault — existing behavior (T2.2c reference)
// ---------------------------------------------------------------------------

// TestUpdate_ConflictDefault_Overwrite_NoQLocalModificationAsked verifies that Update's
// existing ConflictDefault mechanism continues to suppress conflict prompts. This test
// documents the behavior that DeployNew will mirror and guards against regression.
func TestUpdate_ConflictDefault_Overwrite_NoQLocalModificationAsked(t *testing.T) {
	// Arrange — three conflicts, ConflictDefault pre-empts all asks
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: threeConflictUpdatePlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionOverwrite,
	})

	// Assert
	total := countConflictQuestions(stub)
	if total != 0 {
		t.Errorf("QLocalModification was asked %d time(s) in Update with ConflictDefault = %q; "+
			"want 0; Update's ConflictDefault must suppress all conflict prompts",
			total, domain.DecisionOverwrite)
	}
}

// TestUpdate_ConflictDefault_Skip_AddsGapSkippedFile verifies that Update's ConflictDefault
// DecisionSkip records GapSkippedFile gaps (existing behavior, guards against regression).
func TestUpdate_ConflictDefault_Skip_AddsGapSkippedFile(t *testing.T) {
	// Arrange
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"},
				TargetPath: "agent-a.md",
				Action:     domain.ActionConflict,
				Conflict: &domain.LocalModification{
					RecordedHash: "sha256:old",
					CurrentHash:  "sha256:new",
				},
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: domain.DecisionSkip,
	})

	// Assert
	if !spy.hasGapKind(domain.GapSkippedFile) {
		t.Error("GapSkippedFile was not recorded in Update when ConflictDefault is DecisionSkip; " +
			"a skipped conflict must produce a gap")
	}
}
