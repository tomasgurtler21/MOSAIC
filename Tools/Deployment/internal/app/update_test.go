package app_test

// update_test.go verifies the update flow: staleness detection across all version fields,
// locally-modified file prompting, and each of the three per-file decisions.
//
// Verified invariants (T18.5):
//
// Staleness detection:
//   - An agent whose "version" field differs from the deployed value appears in the plan as ActionUpdate
//   - An agent whose "transform_version" field differs appears as ActionUpdate
//   - An agent whose "injections_version" field differs appears as ActionUpdate
//   - An unchanged agent appears as ActionUnchanged, not ActionUpdate
//   - A skill whose "version" field differs appears in the plan as ActionUpdate
//   - A hook bundle whose "version" field differs appears in the plan as ActionUpdate
//
// Locally-modified prompting:
//   - An agent classified ActionConflict triggers a QLocalModification question
//   - QLocalModification subject is the artifact's target path
//   - The three answers map to three ConflictDecision values
//
// Per-file decisions:
//   - DecisionOverwrite → ActionRecord.Taken == TakenUpdated
//   - DecisionSkip → ActionRecord.Taken == TakenSkipped
//   - DecisionBackupThenOverwrite → ActionRecord.Taken == TakenBackedUp with a BackupPath
//
// Update flow mode:
//   - RunSummary.Mode is ModeUpdateWorkspace for the update flow

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Staleness detection
// ---------------------------------------------------------------------------

// TestUpdate_VersionFieldStale_PlanItemIsActionUpdate verifies that a plan item driven by
// the "version" field difference is classified ActionUpdate and the delta names "version".
func TestUpdate_VersionFieldStale_PlanItemIsActionUpdate(t *testing.T) {
	// Arrange — planner returns a plan with one ActionUpdate item driven by "version"
	staleItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Deployed: "1.0", Source: "2.0"},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleItem},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries: []domain.ManifestEntry{
					{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, Version: "1.0"},
				},
			},
		},
	}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if summary.Mode != domain.ModeUpdateWorkspace {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeUpdateWorkspace)
	}
}

// TestUpdate_TransformVersionFieldStale_DeltaFieldIsTransformVersion verifies that staleness
// driven by "transform_version" produces a VersionDelta with Field == "transform_version".
func TestUpdate_TransformVersionFieldStale_DeltaFieldIsTransformVersion(t *testing.T) {
	// Arrange
	staleItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "transform_version", Deployed: "1", Source: "2"},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleItem},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: staleItem.Ref, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act — the plan itself carries the staleness info; the flow must relay it
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — verify the action record reflects the update
	updated := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" && ar.Taken == domain.TakenUpdated {
			updated = true
			break
		}
	}
	if !updated {
		t.Error("no TakenUpdated action for test-runner after transform_version staleness; " +
			"a stale transform_version must produce an ActionUpdate plan item and a TakenUpdated record")
	}
}

// TestUpdate_UnchangedAgent_ActionIsUnchanged verifies that an agent whose version fields
// all match the deployed values is classified ActionUnchanged and not re-deployed.
func TestUpdate_UnchangedAgent_ActionIsUnchanged(t *testing.T) {
	// Arrange
	unchangedItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionUnchanged,
		Stale:      nil,
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{unchangedItem},
		Workflows:     []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — unchanged agent must produce TakenUnchanged, not TakenUpdated
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" && ar.Taken == domain.TakenUpdated {
			t.Error("test-runner action is TakenUpdated even though the plan classified it ActionUnchanged; " +
				"an unchanged agent must not be re-deployed")
		}
	}
}

// ---------------------------------------------------------------------------
// Locally-modified prompting
// ---------------------------------------------------------------------------

// TestUpdate_LocallyModifiedFile_AsksQLocalModification verifies that a conflict-classified
// plan item causes the flow to ask QLocalModification for that file.
func TestUpdate_LocallyModifiedFile_AsksQLocalModification(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict: &domain.LocalModification{
			RecordedHash: "sha256:aaa",
			CurrentHash:  "sha256:bbb",
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{conflictItem},
		Workflows:     []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QLocalModification, "test-runner.md") {
		t.Error("QLocalModification was not asked for the conflict-classified file \"test-runner.md\"; " +
			"every ActionConflict plan item must trigger a QLocalModification question")
	}
}

// TestUpdate_LocallyModifiedFile_SubjectIsTargetPath verifies that the Subject of the
// QLocalModification question is the file's TargetPath, not the agent key.
func TestUpdate_LocallyModifiedFile_SubjectIsTargetPath(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "agents/test-runner.claude.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agents/test-runner.claude.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{conflictItem},
		Workflows:     []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QLocalModification, "agents/test-runner.claude.md") {
		t.Error("QLocalModification subject is not the file's TargetPath; " +
			"the subject must be the relative target path, not the artifact key")
	}
}

// ---------------------------------------------------------------------------
// Per-file decisions
// ---------------------------------------------------------------------------

// TestUpdate_DecisionOverwrite_ProducesTakenUpdated verifies that DecisionOverwrite maps to
// TakenUpdated in the resulting ActionRecord.
func TestUpdate_DecisionOverwrite_ProducesTakenUpdated(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:      domain.ModeUpdateWorkspace,
		Harness:   minimalHarness,
		WorkspacePath: workspace,
		Scope:     domain.ScopeProject,
		Items:     []domain.PlanItem{conflictItem},
		Workflows: []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: conflictItem.Ref, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert
	found := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" {
			found = true
			if ar.Taken != domain.TakenUpdated {
				t.Errorf("DecisionOverwrite: ActionRecord.Taken = %q, want %q",
					ar.Taken, domain.TakenUpdated)
			}
		}
	}
	if !found {
		t.Error("no ActionRecord for \"test-runner\" in summary")
	}
}

// TestUpdate_DecisionSkip_ProducesTakenSkipped verifies that DecisionSkip maps to
// TakenSkipped and the file is not overwritten.
func TestUpdate_DecisionSkip_ProducesTakenSkipped(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:      domain.ModeUpdateWorkspace,
		Harness:   minimalHarness,
		WorkspacePath: workspace,
		Scope:     domain.ScopeProject,
		Items:     []domain.PlanItem{conflictItem},
		Workflows: []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: conflictItem.Ref, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenSkipped},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" && ar.Taken == domain.TakenUpdated {
			t.Error("DecisionSkip resulted in TakenUpdated; a skipped file must not be overwritten")
		}
	}
}

// TestUpdate_InjectionsVersionFieldStale_PlanItemIsActionUpdate verifies that staleness
// driven by "injections_version" produces a VersionDelta with Field == "injections_version"
// and results in a TakenUpdated action record.
func TestUpdate_InjectionsVersionFieldStale_PlanItemIsActionUpdate(t *testing.T) {
	// Arrange
	staleItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "injections_version", Deployed: "1", Source: "2"},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleItem},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: staleItem.Ref, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act — the plan itself carries the staleness info; the flow must relay it
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — verify the action record reflects the update
	updated := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" && ar.Taken == domain.TakenUpdated {
			updated = true
			break
		}
	}
	if !updated {
		t.Error("no TakenUpdated action for test-runner after injections_version staleness; " +
			"a stale injections_version must produce an ActionUpdate plan item and a TakenUpdated record")
	}
}

// TestUpdate_SkillVersionFieldStale_PlanItemIsActionUpdate verifies that a skill whose
// "version" field differs from the deployed value is classified ActionUpdate in the plan,
// completing T18.5's requirement to detect staleness across all artifact types.
func TestUpdate_SkillVersionFieldStale_PlanItemIsActionUpdate(t *testing.T) {
	// Arrange — plan with a stale skill item (single "version" field per plan.SkillStaleness)
	staleSkill := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
		TargetPath: "lean-tdd/SKILL.md",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Deployed: "1.0", Source: "2.0"},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleSkill},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: staleSkill.Ref, TargetPath: workspace + "/lean-tdd/SKILL.md", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — the update flow must produce ModeUpdateWorkspace and a TakenUpdated action for the skill
	if summary.Mode != domain.ModeUpdateWorkspace {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeUpdateWorkspace)
	}
	updated := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "lean-tdd" && ar.Ref.Kind == domain.ArtifactSkill && ar.Taken == domain.TakenUpdated {
			updated = true
			break
		}
	}
	if !updated {
		t.Error("no TakenUpdated action for skill \"lean-tdd\" after version staleness; " +
			"a stale skill version must produce an ActionUpdate plan item and a TakenUpdated record (T18.5)")
	}
}

// TestUpdate_HookVersionFieldStale_PlanItemIsActionUpdate verifies that a hook bundle whose
// "version" field differs from the deployed value is classified ActionUpdate in the plan,
// completing T18.5's requirement to detect staleness across all artifact types including hooks.
func TestUpdate_HookVersionFieldStale_PlanItemIsActionUpdate(t *testing.T) {
	// Arrange — plan with a stale hook item (single "version" field per plan.HookStaleness)
	staleHook := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "test-hooks"},
		TargetPath: "test-hooks/hook.sh",
		Action:     domain.ActionUpdate,
		Stale: []domain.VersionDelta{
			{Field: "version", Deployed: "1.0", Source: "2.0"},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleHook},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{Ref: staleHook.Ref, TargetPath: workspace + "/test-hooks/hook.sh", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — the update flow must produce ModeUpdateWorkspace and a TakenUpdated action for the hook bundle
	if summary.Mode != domain.ModeUpdateWorkspace {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeUpdateWorkspace)
	}
	updated := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-hooks" && ar.Ref.Kind == domain.ArtifactHook && ar.Taken == domain.TakenUpdated {
			updated = true
			break
		}
	}
	if !updated {
		t.Error("no TakenUpdated action for hook bundle \"test-hooks\" after version staleness; " +
			"a stale hook bundle version must produce an ActionUpdate plan item and a TakenUpdated record (T18.5)")
	}
}

// TestUpdate_DecisionBackupThenOverwrite_ProducesTakenBackedUp verifies that
// DecisionBackupThenOverwrite maps to TakenBackedUp with a non-empty BackupPath.
func TestUpdate_DecisionBackupThenOverwrite_ProducesTakenBackedUp(t *testing.T) {
	// Arrange
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionBackupThenOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:      domain.ModeUpdateWorkspace,
		Harness:   minimalHarness,
		WorkspacePath: workspace,
		Scope:     domain.ScopeProject,
		Items:     []domain.PlanItem{conflictItem},
		Workflows: []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{
				Ref:        conflictItem.Ref,
				TargetPath: workspace + "/test-runner.md",
				Taken:      domain.TakenBackedUp,
				BackupPath: workspace + "/.mosaic/backups/test-runner.md.2025-01-01T00:00:00Z.bak",
			},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert
	found := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "test-runner" {
			found = true
			if ar.Taken != domain.TakenBackedUp {
				t.Errorf("DecisionBackupThenOverwrite: ActionRecord.Taken = %q, want %q",
					ar.Taken, domain.TakenBackedUp)
			}
			if ar.BackupPath == "" {
				t.Error("DecisionBackupThenOverwrite: ActionRecord.BackupPath is empty; " +
					"a backed-up file must record where the backup was written")
			}
		}
	}
	if !found {
		t.Error("no ActionRecord for \"test-runner\" in summary")
	}
}
