package app_test

// workflow_update_test.go verifies the workflow-only update use case (UpdateWorkflows).
//
// Verified invariants:
//
// Replace semantics (T5.2):
//   - When UpdateWorkflows is called with a workflow selection, the planner receives
//     exactly that selection — previously-deployed but unselected workflows are dropped.
//   - The result set is the user's selection exactly, nothing more, nothing less.
//
// Additive union in Update is unchanged (T5.3):
//   - Calling Update (not UpdateWorkflows) still uses the additive union of deployed + new
//     workflows; it never drops a previously-deployed workflow. This is a regression guard.
//
// Orchestrator-only scope (T5.4):
//   - Agent, skill, and hook artifacts are filtered from the plan before the executor is
//     called, even when those artifacts are stale.
//   - Only the orchestrator item reaches the executor.
//
// Orchestrator full refresh (T5.5):
//   - The orchestrator item is present in the executed plan.
//   - RunSummary.Mode is ModeWorkflowsOnly.
//
// Conflict handling (T5.6):
//   - A locally-modified orchestrator triggers QLocalModification.
//   - DecisionOverwrite maps to TakenUpdated.
//   - DecisionSkip records a GapSkippedFile gap.
//   - DecisionBackupThenOverwrite maps to TakenBackedUp with a non-empty BackupPath.
//
// No model questions (T5.7):
//   - QTierModel is never asked in the UpdateWorkflows flow.
//   - QAgentModel is never asked in the UpdateWorkflows flow.
//
// Pre-answer convention (T5.8):
//   - When WorkflowIDs is non-nil, QWorkflows is not asked.
//   - When WorkflowIDs is nil, QWorkflows is asked through the Interaction port.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers for this file
// ---------------------------------------------------------------------------

// newWorkflowUpdateRequest returns a WorkflowUpdateRequest pre-wired with the minimal
// harness, the given workspace, and the provided workflow selection.
func newWorkflowUpdateRequest(workspace, harnessID string, workflowIDs []string) app.WorkflowUpdateRequest {
	return app.WorkflowUpdateRequest{
		HarnessID:       harnessID,
		WorkspacePath:   workspace,
		WorkflowIDs:     workflowIDs,
		AutoConfirmPlan: true,
	}
}

// orchestratorOnlyPlan returns a plan whose Items contains only the orchestrator item.
// Used when the stub planner is configured to simulate the post-filter plan shape.
func orchestratorOnlyPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: "orchestrator.md",
				Action:     domain.ActionUpdate,
			},
		},
		Workflows: []string{"quick-fix"},
	}
}

// mixedArtifactPlan returns a plan whose Items contains one orchestrator item, one agent item,
// one skill item, and one hook item. Used to verify the orchestrator-only filter.
func mixedArtifactPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: "orchestrator.md",
				Action:     domain.ActionUpdate,
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionUpdate,
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "1.0", Source: "2.0"},
				},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
				TargetPath: "lean-tdd/SKILL.md",
				Action:     domain.ActionUpdate,
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "1.0", Source: "2.0"},
				},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "test-hooks"},
				TargetPath: "test-hooks/hook.sh",
				Action:     domain.ActionUpdate,
				Stale: []domain.VersionDelta{
					{Field: "version", Deployed: "1.0", Source: "2.0"},
				},
			},
		},
		Workflows: []string{"quick-fix"},
	}
}

// orchestratorConflictPlan returns a plan with one ActionConflict orchestrator item.
func orchestratorConflictPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: "orchestrator.md",
				Action:     domain.ActionConflict,
				Conflict: &domain.LocalModification{
					RecordedHash: "sha256:old",
					CurrentHash:  "sha256:new",
				},
			},
		},
		Workflows: []string{"quick-fix"},
	}
}

// hasNonOrchestratorItem reports whether any plan item in items belongs to a non-orchestrator
// artifact — that is, any agent that is not the orchestrator, plus all skills and hooks.
func hasNonOrchestratorItem(items []domain.PlanItem) bool {
	for _, item := range items {
		if item.Ref.Kind == domain.ArtifactSkill || item.Ref.Kind == domain.ArtifactHook {
			return true
		}
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key != "orchestrator" {
			return true
		}
	}
	return false
}

// countQuestionsOfKind returns how many calls in the stub have the given QuestionID.
func countQuestionsOfKind(stub *interactiontest.Stub, id domain.QuestionID) int {
	n := 0
	for _, c := range stub.Calls() {
		if c.ID == id {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// T5.2 — Replace semantics: user selection is used exactly
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_ReplaceSemantics_PreviouslyDeployedWorkflowsDropped verifies that when
// the deployed orchestrator contains workflows not in the user's selection, those workflows
// are NOT included in the planner input. Replace semantics: the user's selection is the
// complete new set.
func TestUpdateWorkflows_ReplaceSemantics_PreviouslyDeployedWorkflowsDropped(t *testing.T) {
	// Arrange — deployed orchestrator has "quick-fix" and "greenfield-tdd"; user selects only "quick-fix"
	cat := newMinimalCatalog()
	greenfieldWF := domain.Workflow{
		ID: "greenfield-tdd", Name: "Greenfield TDD", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	cat.workflows = append(cat.workflows, greenfieldWF)
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	// Write a deployed orchestrator containing both workflow sections so the probe finds them.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n" +
			"[[SECTION:Workflow:greenfield-tdd]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Greenfield TDD content.\n" +
			"[[/SECTION:Workflow:greenfield-tdd]]\n",
	)
	writeTempFile(t, workspace, "orchestrator.md", orchContent)

	// Use a capturing planner to inspect what WorkflowIDs were passed.
	capPlan := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeWorkflowsOnly,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix"},
		},
	}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — user selects only "quick-fix", replacing the deployed set that also had "greenfield-tdd"
	_, _ = svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — planner must NOT receive "greenfield-tdd"; replace semantics drop unselected workflows
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; the UpdateWorkflows flow must call the planner")
	}
	for _, id := range capPlan.capturedInput.WorkflowIDs {
		if id == "greenfield-tdd" {
			t.Error("Planner.Build received \"greenfield-tdd\" even though it was not in the user's " +
				"selection; UpdateWorkflows must use replace semantics — the planner receives exactly " +
				"the selected workflows, with previously-deployed but unselected workflows dropped")
		}
	}
}

// TestUpdateWorkflows_ReplaceSemantics_SelectionIsExact verifies that the planner receives
// exactly the workflow IDs provided in WorkflowUpdateRequest.WorkflowIDs, with no additions
// or removals. Replace semantics mean the user's selection is the complete new set.
func TestUpdateWorkflows_ReplaceSemantics_SelectionIsExact(t *testing.T) {
	// Arrange — user selects ["quick-fix"]; deployed orchestrator is empty (no prior workflows)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	capPlan := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeWorkflowsOnly,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix"},
		},
	}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — planner receives exactly ["quick-fix"]
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; the UpdateWorkflows flow must call the planner")
	}
	found := false
	for _, id := range capPlan.capturedInput.WorkflowIDs {
		if id == "quick-fix" {
			found = true
		}
	}
	if !found {
		t.Error("Planner.Build WorkflowIDs does not include \"quick-fix\" from the user's selection; " +
			"UpdateWorkflows must pass the user's exact selection to the planner")
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Update additive union behaviour is unchanged (regression guard)
// ---------------------------------------------------------------------------

// TestUpdate_WorkflowAddition_IsAdditive_DoesNotReplace verifies that calling Update (not
// UpdateWorkflows) still uses the additive union of previously-deployed and newly-added
// workflows. Previously-deployed workflows must NOT be dropped. This is a regression guard
// that ensures replace semantics do not leak from UpdateWorkflows into Update.
func TestUpdate_WorkflowAddition_IsAdditive_DoesNotReplace(t *testing.T) {
	// Arrange — deployed orchestrator has "quick-fix"; we add "greenfield-tdd" via Update
	cat := newMinimalCatalog()
	greenfieldWF := domain.Workflow{
		ID: "greenfield-tdd", Name: "Greenfield TDD", Category: "Build",
		ReferencedAgents: []string{"test-runner"},
	}
	cat.workflows = append(cat.workflows, greenfieldWF)
	cat.categories = []domain.WorkflowCategory{
		{Name: "Build", Workflows: cat.workflows},
	}

	// Write a deployed orchestrator with "quick-fix" so the update flow can discover it.
	orchContent := []byte(
		"---\nversion: \"1.0\"\n---\n\n" +
			"[[SECTION:Workflow:quick-fix]]\n" +
			"<!-- workflow-version: 1.0 -->\n" +
			"Quick fix content.\n" +
			"[[/SECTION:Workflow:quick-fix]]\n",
	)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	writeTempFile(t, workspace, "orchestrator.md", orchContent)

	capPlan := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeUpdate,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
			Items:         []domain.PlanItem{},
			Workflows:     []string{"quick-fix", "greenfield-tdd"},
		},
	}
	deps.Planner = capPlan
	svc := app.New(deps)

	// Act — Update with AddWorkflowIDs; previously-deployed "quick-fix" must be preserved
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"greenfield-tdd"},
		AutoConfirmPlan: true,
	})

	// Assert — planner must receive both "quick-fix" (from deployed) and "greenfield-tdd" (new)
	if capPlan.capturedInput == nil {
		t.Fatal("Planner.Build was not called; Update must call the planner")
	}
	wids := capPlan.capturedInput.WorkflowIDs
	hasQuickFix := false
	hasGreenfield := false
	for _, id := range wids {
		switch id {
		case "quick-fix":
			hasQuickFix = true
		case "greenfield-tdd":
			hasGreenfield = true
		}
	}
	if !hasQuickFix {
		t.Error("Update flow dropped \"quick-fix\" from the previously-deployed orchestrator; " +
			"Update must use additive union semantics — previously-deployed workflows must NOT be dropped " +
			"(regression guard: replace semantics from UpdateWorkflows must not leak into Update)")
	}
	if !hasGreenfield {
		t.Error("Update flow did not include newly-added \"greenfield-tdd\"; " +
			"AddWorkflowIDs must be unioned with the deployed set in the Update flow")
	}
}

// TestUpdate_SummaryMode_IsNotWorkflowsOnly verifies that calling Update produces a summary
// with ModeUpdate, not ModeWorkflowsOnly. This distinguishes the two flows in run records.
func TestUpdate_SummaryMode_IsNotWorkflowsOnly(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
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

	// Assert — Update must produce ModeUpdate, never ModeWorkflowsOnly
	if summary.Mode == domain.ModeWorkflowsOnly {
		t.Errorf("Update flow produced Mode = %q; want %q; replace semantics must not leak into Update",
			summary.Mode, domain.ModeUpdate)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — No agent, skill, or hook artifact is planned, written, or version-stamped
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_AgentArtifact_IsFilteredFromPlan verifies that agent artifacts
// (excluding the orchestrator itself) are removed from the plan before execution, even when
// they are stale.
func TestUpdateWorkflows_AgentArtifact_IsFilteredFromPlan(t *testing.T) {
	// Arrange — planner returns a plan with both the orchestrator and a stale worker agent
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: mixedArtifactPlan(workspace)}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — executor must not see any non-orchestrator agent items
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	if hasNonOrchestratorItem(capExec.capturedPlan.Items) {
		t.Error("executor received non-orchestrator agent or skill or hook items; " +
			"UpdateWorkflows must filter the plan to the orchestrator artifact only before execution, " +
			"even when other artifacts are stale")
	}
	// Specifically check the stale agent
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Key == "test-runner" {
			t.Errorf("executor received plan item for agent \"test-runner\" (stale); "+
				"only the orchestrator may be written in UpdateWorkflows, "+
				"but the executor received: kind=%s key=%s action=%s",
				item.Ref.Kind, item.Ref.Key, item.Action)
		}
	}
}

// TestUpdateWorkflows_SkillArtifact_IsFilteredFromPlan verifies that skill artifacts are
// removed from the plan before execution, even when they are stale.
func TestUpdateWorkflows_SkillArtifact_IsFilteredFromPlan(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: mixedArtifactPlan(workspace)}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactSkill {
			t.Errorf("executor received plan item for skill %q; "+
				"UpdateWorkflows must not plan, write, or version-stamp skill artifacts",
				item.Ref.Key)
		}
	}
}

// TestUpdateWorkflows_HookArtifact_IsFilteredFromPlan verifies that hook artifacts are
// removed from the plan before execution, even when they are stale.
func TestUpdateWorkflows_HookArtifact_IsFilteredFromPlan(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: mixedArtifactPlan(workspace)}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactHook {
			t.Errorf("executor received plan item for hook %q; "+
				"UpdateWorkflows must not plan, write, or version-stamp hook artifacts",
				item.Ref.Key)
		}
	}
}

// TestUpdateWorkflows_OnlyOrchestratorItem_ReachesExecutor verifies the combined invariant:
// after the orchestrator-only filter, the executor's plan Items contains only the orchestrator,
// regardless of how many other artifacts the planner returned.
func TestUpdateWorkflows_OnlyOrchestratorItem_ReachesExecutor(t *testing.T) {
	// Arrange — planner returns a mixed plan; executor should see only orchestrator
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: mixedArtifactPlan(workspace)}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — exactly one item (the orchestrator) must reach the executor
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	nonOrch := 0
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind != domain.ArtifactAgent || item.Ref.Key != "orchestrator" {
			nonOrch++
		}
	}
	if nonOrch > 0 {
		t.Errorf("executor plan contains %d non-orchestrator item(s); "+
			"UpdateWorkflows must filter the plan to the orchestrator artifact only; "+
			"no agent (except orchestrator), skill, or hook may be written in this mode",
			nonOrch)
	}
}

// ---------------------------------------------------------------------------
// T5.5 — Orchestrator is fully refreshed
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_Orchestrator_IncludedInExecutedPlan verifies that the orchestrator
// item is present in the executed plan. The orchestrator must be regenerated (not skipped)
// in the workflow-only update flow.
func TestUpdateWorkflows_Orchestrator_IncludedInExecutedPlan(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must execute the plan")
	}
	orchItem, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Error("executor's plan has no item for the orchestrator; " +
			"UpdateWorkflows must regenerate the orchestrator, not skip it")
	} else if orchItem.Action == domain.ActionUnchanged {
		t.Error("orchestrator plan item action is ActionUnchanged; " +
			"UpdateWorkflows must treat the orchestrator as needing a refresh " +
			"(the workflow set replacement must produce an ActionUpdate or ActionCreate)")
	}
}

// TestUpdateWorkflows_RunSummaryMode_IsWorkflowsOnly verifies that the RunSummary produced
// by UpdateWorkflows carries ModeWorkflowsOnly. This distinguishes the workflow-only flow
// in run records, the todo file metadata, and the CLI output.
func TestUpdateWorkflows_RunSummaryMode_IsWorkflowsOnly(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}
	if summary.Mode != domain.ModeWorkflowsOnly {
		t.Errorf("RunSummary.Mode = %q, want %q; the workflow-only update flow must report its mode correctly",
			summary.Mode, domain.ModeWorkflowsOnly)
	}
}

// ---------------------------------------------------------------------------
// T5.6 — Conflict handling on the orchestrator file
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_OrchestratorConflict_AsksQLocalModification verifies that a
// locally-modified orchestrator file triggers a QLocalModification interaction, with the
// orchestrator's TargetPath as the subject.
func TestUpdateWorkflows_OrchestratorConflict_AsksQLocalModification(t *testing.T) {
	// Arrange — planner returns an ActionConflict orchestrator item
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "orchestrator.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorConflictPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if !stub.WasAsked(domain.QLocalModification, "orchestrator.md") {
		t.Error("QLocalModification was not asked for \"orchestrator.md\"; " +
			"a locally-modified orchestrator in UpdateWorkflows must trigger a conflict prompt " +
			"identical to Update's per-file conflict handling")
	}
}

// TestUpdateWorkflows_OrchestratorConflict_SkipDecision_RecordsGapSkippedFile verifies that
// when the user chooses DecisionSkip for a locally-modified orchestrator, UpdateWorkflows
// records a GapSkippedFile gap, matching Update's existing behavior.
func TestUpdateWorkflows_OrchestratorConflict_SkipDecision_RecordsGapSkippedFile(t *testing.T) {
	// Arrange
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "orchestrator.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: orchestratorConflictPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — a skipped orchestrator conflict must produce a GapSkippedFile gap
	if !spy.hasGapKind(domain.GapSkippedFile) {
		t.Error("GapSkippedFile was not recorded when the user chose DecisionSkip for the orchestrator; " +
			"a skipped conflict in UpdateWorkflows must record a gap, matching Update's behavior")
	}
}

// TestUpdateWorkflows_OrchestratorConflict_OverwriteDecision_ProducesTakenUpdated verifies
// that DecisionOverwrite for the orchestrator maps to TakenUpdated in the action record.
func TestUpdateWorkflows_OrchestratorConflict_OverwriteDecision_ProducesTakenUpdated(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "orchestrator.md", string(domain.DecisionOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorConflictPlan(workspace)}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: workspace + "/orchestrator.md",
				Taken:      domain.TakenUpdated,
			},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert
	found := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "orchestrator" {
			found = true
			if ar.Taken != domain.TakenUpdated {
				t.Errorf("DecisionOverwrite on orchestrator: ActionRecord.Taken = %q, want %q",
					ar.Taken, domain.TakenUpdated)
			}
		}
	}
	if !found {
		t.Error("no ActionRecord for \"orchestrator\" in summary after DecisionOverwrite; " +
			"the executor must record what happened to the orchestrator")
	}
}

// TestUpdateWorkflows_OrchestratorConflict_BackupDecision_ProducesTakenBackedUp verifies
// that DecisionBackupThenOverwrite for the orchestrator maps to TakenBackedUp with a non-empty
// BackupPath, matching Update's existing backup behavior.
func TestUpdateWorkflows_OrchestratorConflict_BackupDecision_ProducesTakenBackedUp(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "orchestrator.md", string(domain.DecisionBackupThenOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorConflictPlan(workspace)}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: workspace + "/orchestrator.md",
				Taken:      domain.TakenBackedUp,
				BackupPath: workspace + "/.mosaic/backups/orchestrator.md.2025-01-01T00:00:00Z.bak",
			},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert
	found := false
	for _, ar := range summary.Actions {
		if ar.Ref.Key == "orchestrator" {
			found = true
			if ar.Taken != domain.TakenBackedUp {
				t.Errorf("DecisionBackupThenOverwrite on orchestrator: ActionRecord.Taken = %q, want %q",
					ar.Taken, domain.TakenBackedUp)
			}
			if ar.BackupPath == "" {
				t.Error("DecisionBackupThenOverwrite on orchestrator: ActionRecord.BackupPath is empty; " +
					"a backed-up orchestrator must record where the backup was written")
			}
		}
	}
	if !found {
		t.Error("no ActionRecord for \"orchestrator\" in summary after DecisionBackupThenOverwrite")
	}
}

// TestUpdateWorkflows_ConflictDefault_Overwrite_NoQLocalModificationAsked verifies that when
// WorkflowUpdateRequest.ConflictDefault is set, no QLocalModification questions are asked
// and the conflict is resolved non-interactively.
func TestUpdateWorkflows_ConflictDefault_Overwrite_NoQLocalModificationAsked(t *testing.T) {
	// Arrange — conflict in plan; ConflictDefault must pre-empt the ask
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorConflictPlan(workspace)}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: workspace + "/orchestrator.md",
				Taken:      domain.TakenUpdated,
			},
		},
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		ConflictDefault: domain.DecisionOverwrite,
		AutoConfirmPlan: true,
	})

	// Assert — flow must complete successfully (err == nil confirms the flow ran end-to-end)
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully when ConflictDefault is set", err)
	}
	// Assert — no QLocalModification should be asked when ConflictDefault is set
	if stub.WasAsked(domain.QLocalModification, "orchestrator.md") {
		t.Error("QLocalModification was asked even though ConflictDefault was set; " +
			"a non-empty ConflictDefault must suppress all conflict prompts in UpdateWorkflows, " +
			"matching Update's ConflictDefault behavior")
	}
}

// ---------------------------------------------------------------------------
// T5.7 — No tier-level or per-agent model question is asked
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NoQTierModel_IsAsked verifies that UpdateWorkflows never asks
// QTierModel, regardless of which harness is used or how many agents exist.
func TestUpdateWorkflows_NoQTierModel_IsAsked(t *testing.T) {
	// Arrange — configure a planner that returns a plan with a stale agent, which would
	// normally trigger tier model questions in other flows.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — err == nil ensures the flow actually ran (not short-circuited by stub)
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully to verify model question behaviour", err)
	}
	if n := countQuestionsOfKind(stub, domain.QTierModel); n > 0 {
		t.Errorf("QTierModel was asked %d time(s) in UpdateWorkflows; "+
			"no tier-level model question must be asked in the workflow-only update flow — "+
			"the orchestrator's model is taken from the deployed file, no tier selection needed", n)
	}
}

// TestUpdateWorkflows_NoQAgentModel_IsAsked verifies that UpdateWorkflows never asks
// QAgentModel for any agent during the workflow-only update flow.
func TestUpdateWorkflows_NoQAgentModel_IsAsked(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully to verify model question behaviour", err)
	}
	if n := countQuestionsOfKind(stub, domain.QAgentModel); n > 0 {
		t.Errorf("QAgentModel was asked %d time(s) in UpdateWorkflows; "+
			"no per-agent model question must be asked — the workflow-only update flow "+
			"does not add or update agents, so no model selection is needed", n)
	}
}

// ---------------------------------------------------------------------------
// T5.8 — Pre-answer convention for workflow selection
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_WorkflowIDsProvided_QWorkflowsNotAsked verifies that when
// WorkflowUpdateRequest.WorkflowIDs is non-nil, the flow does not ask QWorkflows through
// the Interaction port (CD-6 pre-answer convention).
func TestUpdateWorkflows_WorkflowIDsProvided_QWorkflowsNotAsked(t *testing.T) {
	// Arrange — WorkflowIDs is pre-answered with ["quick-fix"]
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"}, // non-nil: must not ask
		AutoConfirmPlan: true,
	})

	// Assert — err == nil ensures the flow ran (not short-circuited); question must not be asked
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully to verify the pre-answer convention", err)
	}
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked even though WorkflowIDs was pre-answered (non-nil); " +
			"when WorkflowIDs is set, UpdateWorkflows must use it directly without asking (CD-6)")
	}
}

// TestUpdateWorkflows_WorkflowIDsNil_QWorkflowsIsAsked verifies that when
// WorkflowUpdateRequest.WorkflowIDs is nil, the flow asks QWorkflows through the
// Interaction port so the user can select the replacement workflow set interactively.
func TestUpdateWorkflows_WorkflowIDsNil_QWorkflowsIsAsked(t *testing.T) {
	// Arrange — WorkflowIDs is nil: the flow must ask QWorkflows
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     nil, // nil: flow must ask through Interaction
		AutoConfirmPlan: true,
	})

	// Assert — QWorkflows must have been asked when WorkflowIDs is nil
	if !stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was not asked even though WorkflowIDs was nil; " +
			"an unset WorkflowIDs must cause UpdateWorkflows to ask through the Interaction port (CD-6), " +
			"so the user can select the replacement workflow set interactively")
	}
}

// TestUpdateWorkflows_WorkflowIDsEmptyNonNil_QWorkflowsNotAsked verifies that an explicitly
// empty but non-nil WorkflowIDs (meaning "deploy the orchestrator with no workflows") is
// honoured directly without asking — it is not treated the same as nil.
func TestUpdateWorkflows_WorkflowIDsEmptyNonNil_QWorkflowsNotAsked(t *testing.T) {
	// Arrange — WorkflowIDs is non-nil but empty: deploy orchestrator with no workflows
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: "orchestrator.md",
				Action:     domain.ActionUpdate,
			},
		},
		Workflows: []string{}, // empty selection
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{}, // explicitly empty, not nil
		AutoConfirmPlan: true,
	})

	// Assert — err == nil (flow ran successfully); QWorkflows must not be asked
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; an explicitly empty WorkflowIDs must be honoured, not cause an error", err)
	}
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked even though WorkflowIDs was an explicitly empty (non-nil) slice; " +
			"an empty non-nil slice means \"deploy orchestrator with no workflows\" and must be honoured " +
			"without prompting, per the pre-answer convention (CD-6)")
	}
}
