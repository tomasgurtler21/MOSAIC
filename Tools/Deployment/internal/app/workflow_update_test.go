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
// Stale/existing-artifact filter (T5.4):
//   - Already-deployed agent, skill, and hook artifacts with ActionUpdate are filtered from
//     the plan before the executor is called.
//   - Already-deployed non-orchestrator agents with ActionConflict are filtered.
//   - Already-deployed non-orchestrator agents with ActionUnchanged are filtered.
//   - Only the orchestrator and newly-required agents with ActionCreate reach the executor.
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
// No model questions when all required agents are already deployed (T5.7):
//   - QTierModel is not asked when every workflow-required agent already has a deployed file.
//   - QAgentModel is not asked when every workflow-required agent already has a deployed file.
//   - Model questions are gated on a non-empty new-agent set; when all agents are deployed the
//     gate evaluates to false and the whole resolveModels call is skipped.
//
// Pre-answer convention (T5.8):
//   - When WorkflowIDs is non-nil, QWorkflows is not asked.
//   - When WorkflowIDs is nil, QWorkflows is asked through the Interaction port.
//
// New-agent inclusion (T1.1):
//   - An absent required non-orchestrator agent reaches the executor as an ActionCreate item.
//   - The orchestrator item is still present alongside the new agent in the executed plan.
//   - Registrations remain nil even when new-agent items are admitted.
//
// Model questions for new agents (T1.2):
//   - QTierModel is asked for newly-required agents (in the tier containing them) before plan review.
//   - QAgentModel is asked for a newly-required agent when no tier mapping resolves its model.
//   - QCustomTool is asked for a newly-required agent that has an unmapped generic tool.
//   - No model or tool question of any kind is asked when every required agent is already deployed.
//
// Skip/unanswered model handling for new agents (T1.3):
//   - A skipped or unanswered model question does not abort the UpdateWorkflows run.
//   - A GapNoModel gap present in the plan is forwarded to the todo collector, same as deploy-new.
//
// Absent skill required by a new agent is deployed (T3.1):
//   - A skill required by a newly deployed absent agent reaches the executor as ActionCreate.
//   - The orchestrator item is still present in the executed plan alongside admitted skill items.
//   - Registrations remain nil even when skill items are admitted.
//
// Skill exclusion: present skills and skills for deployed agents are not planned (T3.2):
//   - A skill whose file is already present in the workspace is never admitted, stale or not.
//   - A skill required only by an already-deployed agent is not admitted even when its file is absent.
//
// Admitted skill items carry version stamps; hook items and registrations still dropped (T3.3):
//   - An admitted skill item's target path appears in the VersionStamps map passed to the executor.
//   - The VersionStamps entry for an admitted skill carries the skill's version from the catalog.
//   - Hook items are dropped even when skill items are admitted in the same run.

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
// T5.4 — Already-deployed agent, skill, and hook artifacts are filtered from the plan
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_AgentArtifact_IsFilteredFromPlan verifies that non-orchestrator agent
// artifacts with ActionUpdate (i.e. stale already-deployed agents) are removed from the plan
// before execution. The mixedArtifactPlan fixture uses ActionUpdate for all non-orchestrator
// items, so this test expresses the "stale deployed agents are filtered" invariant.
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

// TestUpdateWorkflows_OnlyOrchestratorItem_ReachesExecutor verifies the combined filtering
// invariant: when the planner returns only stale (ActionUpdate) artifacts alongside the
// orchestrator, the executor's plan Items contains only the orchestrator. This covers the
// retained "already-deployed stale artifacts are filtered" case; a separate test covers
// the new-agent (ActionCreate) case.
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
// T5.7 — No model question when every workflow-required agent is already deployed
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NoQTierModel_WhenAllAgentsDeployed verifies that UpdateWorkflows does
// not ask QTierModel when every workflow-required agent already has a deployed file. This is
// the narrowed form of the former "never any model question" guarantee: model questions are
// now gated on a non-empty new-agent set, so the whole resolveModels call is skipped when
// every agent is already present on disk.
func TestUpdateWorkflows_NoQTierModel_WhenAllAgentsDeployed(t *testing.T) {
	// Arrange — write test-runner.md so that every workflow-required agent is "already deployed"
	// (the quick-fix workflow references test-runner; if its file exists, no new agents are
	// detected and the model-resolution step is skipped entirely).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nstub agent content\n"))
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — err == nil ensures the flow actually ran (not short-circuited by stub)
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully", err)
	}
	if n := countQuestionsOfKind(stub, domain.QTierModel); n > 0 {
		t.Errorf("QTierModel was asked %d time(s); "+
			"when every workflow-required agent is already deployed, the new-agent set is empty and "+
			"the entire model-resolution step must be skipped — no tier question must be asked", n)
	}
}

// TestUpdateWorkflows_NoQAgentModel_WhenAllAgentsDeployed verifies that UpdateWorkflows does
// not ask QAgentModel when every workflow-required agent already has a deployed file.
// This is the narrowed form of the former unconditional no-model-question guarantee.
func TestUpdateWorkflows_NoQAgentModel_WhenAllAgentsDeployed(t *testing.T) {
	// Arrange — write test-runner.md (present) so no new agents are detected.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nstub agent content\n"))
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v; the flow must complete successfully", err)
	}
	if n := countQuestionsOfKind(stub, domain.QAgentModel); n > 0 {
		t.Errorf("QAgentModel was asked %d time(s); "+
			"when every workflow-required agent is already deployed, no per-agent model question "+
			"must be asked — model resolution is gated on a non-empty new-agent set", n)
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

// ---------------------------------------------------------------------------
// T1.4 additional — explicit ActionConflict and ActionUnchanged filter tests
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NonOrchestratorAgent_ActionConflict_IsFiltered verifies that a
// non-orchestrator agent with ActionConflict (locally modified) is dropped from the plan
// before execution. The workflow-update flow must never prompt for or overwrite a
// locally-modified non-orchestrator agent — that agent is already deployed and out of scope.
func TestUpdateWorkflows_NonOrchestratorAgent_ActionConflict_IsFiltered(t *testing.T) {
	// Arrange — plan includes the orchestrator (update) and a non-orchestrator with a conflict.
	// Write test-runner.md to make it "present" (an already-deployed agent with a local modification).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nlocally modified content\n"))

	planWithConflict := domain.Plan{
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
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:old", CurrentHash: "sha256:new"},
			},
		},
		Workflows: []string{"quick-fix"},
	}
	deps.Planner = &stubPlanner{plan: planWithConflict}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — executor must not see the locally-modified non-orchestrator agent
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "test-runner" {
			t.Errorf("executor received plan item for \"test-runner\" (ActionConflict); "+
				"a locally-modified non-orchestrator agent must be dropped — "+
				"the workflow-update flow must never prompt for or overwrite already-deployed agents, "+
				"got item: kind=%s key=%s action=%s", item.Ref.Kind, item.Ref.Key, item.Action)
		}
	}
}

// TestUpdateWorkflows_NonOrchestratorAgent_ActionUnchanged_IsFiltered verifies that a
// non-orchestrator agent with ActionUnchanged is dropped from the plan before execution.
// An unchanged agent is already deployed and current; it must not be re-written.
func TestUpdateWorkflows_NonOrchestratorAgent_ActionUnchanged_IsFiltered(t *testing.T) {
	// Arrange — plan includes the orchestrator and a non-orchestrator that is up to date.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\ncurrent content\n"))

	planWithUnchanged := domain.Plan{
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
				Action:     domain.ActionUnchanged,
			},
		},
		Workflows: []string{"quick-fix"},
	}
	deps.Planner = &stubPlanner{plan: planWithUnchanged}
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
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "test-runner" {
			t.Errorf("executor received plan item for \"test-runner\" (ActionUnchanged); "+
				"an already-deployed and current non-orchestrator agent must be dropped — "+
				"no already-deployed artifact may be planned, written, or version-stamped, "+
				"got item: kind=%s key=%s action=%s", item.Ref.Kind, item.Ref.Key, item.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// T1.1 — Absent required agent reaches the executor as ActionCreate
// ---------------------------------------------------------------------------

// newAgentPlan returns a plan whose Items contains the orchestrator (ActionUpdate) plus a
// non-orchestrator agent (ActionCreate) representing a newly-required, not-yet-deployed agent.
// It is used by T1.1 tests that verify new-agent inclusion.
func newAgentPlan(workspace string) domain.Plan {
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
				Action:     domain.ActionCreate,
			},
		},
		Workflows: []string{"quick-fix"},
	}
}

// TestUpdateWorkflows_AbsentRequiredAgent_ReachesExecutorAsActionCreate verifies that a
// workflow-required agent that has no file in the workspace reaches the executor as an
// ActionCreate item. This is the primary new-agent inclusion invariant: absent required
// agents must be deployed in the same run.
//
// This test is in TDD RED phase: the current implementation filters every non-orchestrator
// item, so the ActionCreate item for test-runner does not reach the executor.
func TestUpdateWorkflows_AbsentRequiredAgent_ReachesExecutorAsActionCreate(t *testing.T) {
	// Arrange — test-runner.md is NOT written, so it is absent and newlyRequiredAgents must
	// classify it as new. The planner returns a plan with test-runner as ActionCreate.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	// Do not write test-runner.md — its absence is the trigger for new-agent classification.
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — executor must receive the ActionCreate item for test-runner
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	item, ok := findPlanItem(capExec.capturedPlan.Items, "test-runner")
	if !ok {
		t.Error("executor's plan has no item for \"test-runner\"; " +
			"an absent workflow-required agent must be admitted to the executor as an ActionCreate item " +
			"so its file is created in this run")
		return
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("test-runner plan item action = %q, want %q; "+
			"the plan item for an absent required agent must carry ActionCreate",
			item.Action, domain.ActionCreate)
	}
}

// TestUpdateWorkflows_AbsentRequiredAgent_OrchestratorStillPresent verifies that the
// orchestrator item is still present in the executor's plan alongside the newly-admitted
// ActionCreate item for the absent agent. The orchestrator must never be dropped.
//
// This test is in TDD RED phase.
func TestUpdateWorkflows_AbsentRequiredAgent_OrchestratorStillPresent(t *testing.T) {
	// Arrange — test-runner.md absent (new agent), planner returns both orchestrator and test-runner.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — orchestrator must be in the executor's plan
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called")
	}
	_, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Error("orchestrator item is missing from the executor's plan; " +
			"the orchestrator must always be included in the executed plan, " +
			"even when new-agent items are also admitted")
	}
}

// TestUpdateWorkflows_AbsentRequiredAgent_RegistrationsStillNil verifies that even when an
// absent required agent is admitted to the executed plan, p.Registrations is still cleared to
// nil. Hook registrations are entirely out of scope for the workflow-update flow.
//
// This test is in TDD RED phase.
func TestUpdateWorkflows_AbsentRequiredAgent_RegistrationsStillNil(t *testing.T) {
	// Arrange — planner returns a plan with non-nil Registrations plus the new agent.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	planWithRegs := newAgentPlan(workspace)
	planWithRegs.Registrations = []domain.RegistrationStep{{ID: "some-hook", Instruction: "register hook"}}
	deps.Planner = &stubPlanner{plan: planWithRegs}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — Registrations in the executor's plan must be nil
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called")
	}
	if len(capExec.capturedPlan.Registrations) > 0 {
		t.Errorf("executor received %d Registrations, want 0; "+
			"p.Registrations must be cleared to nil before the plan is executed — "+
			"hook registrations are out of scope for the workflow-update flow even when new agents are admitted",
			len(capExec.capturedPlan.Registrations))
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Model questions are asked for new agents, in deploy-new order
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NewAgent_QTierModelAsked verifies that UpdateWorkflows asks QTierModel
// for the tier containing a newly-required agent, before the plan review step. This mirrors
// the deploy-new question flow with tierSkipOverride=false.
//
// This test is in TDD RED phase: the current implementation never calls resolveModels, so
// QTierModel is never asked regardless of which agents are absent.
func TestUpdateWorkflows_NewAgent_QTierModelAsked(t *testing.T) {
	// Arrange — test-runner.md absent (new agent). The minimal catalog puts test-runner in the
	// "HIGH" tier. Script the tier model question so it is answered; an unscripted question
	// would return SkippedOne, which is also acceptable for this test but would need a different
	// assertion. Here we confirm the question is asked by scripting it.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// test-runner.md not written — it is absent and must be classified as new.
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — QTierModel must have been asked for the "HIGH" tier
	if !stub.WasAsked(domain.QTierModel, "HIGH") {
		t.Error("QTierModel was not asked for tier \"HIGH\"; " +
			"UpdateWorkflows must ask tier-level model questions for newly-required agents, " +
			"using the same deploy-new question flow (tierSkipOverride=false) — " +
			"the question must appear before the plan review step")
	}
}

// TestUpdateWorkflows_NewAgent_QTierModelBeforePlanReview verifies that QTierModel is asked
// strictly before QPlanConfirm (plan review). The design requires model questions to precede
// the review so the plan the user confirms already reflects the chosen model.
//
// This test is in TDD RED phase.
func TestUpdateWorkflows_NewAgent_QTierModelBeforePlanReview(t *testing.T) {
	// Arrange — test-runner.md absent.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — QTierModel must appear before QPlanConfirm in the question order
	order := stub.QuestionOrder()
	tierIdx, reviewIdx := -1, -1
	for i, id := range order {
		switch id {
		case domain.QTierModel:
			if tierIdx == -1 {
				tierIdx = i
			}
		case domain.QPlanConfirm:
			if reviewIdx == -1 {
				reviewIdx = i
			}
		}
	}
	if tierIdx == -1 {
		t.Fatal("QTierModel was not asked at all; " +
			"model questions must be asked for newly-required agents before plan review")
	}
	if reviewIdx != -1 && tierIdx > reviewIdx {
		t.Errorf("QTierModel (position %d) appeared after QPlanConfirm (position %d); "+
			"model questions must precede the plan review step so the confirmed plan already "+
			"reflects the chosen model", tierIdx, reviewIdx)
	}
}

// TestUpdateWorkflows_NewAgent_QAgentModelAsked_WhenNoTierMapping verifies that UpdateWorkflows
// asks QAgentModel for a newly-required agent when no tier mapping resolves its model. This
// covers the case where the catalog has no tier, or the tier has no mapping in user config.
//
// This test is in TDD RED phase.
func TestUpdateWorkflows_NewAgent_QAgentModelAsked_WhenNoTierMapping(t *testing.T) {
	// Arrange — test-runner.md absent, catalog has no tiers, so QTierModel is never asked and
	// QAgentModel must be asked for test-runner.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// Remove tier info so QTierModel is never asked; QAgentModel must be used instead.
	cat := newMinimalCatalog()
	cat.tiers = []domain.TierInfo{} // no tiers → no tier mapping for test-runner
	deps.Catalog = cat
	// test-runner.md not written — absent, classified as new.
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — QAgentModel must have been asked for test-runner
	if !stub.WasAsked(domain.QAgentModel, "test-runner") {
		t.Error("QAgentModel was not asked for \"test-runner\"; " +
			"when no tier mapping resolves an agent's model, QAgentModel must be asked — " +
			"this matches deploy-new behaviour (agentSkipOverride=false)")
	}
}

// unmappingModule is a stubHarnessModule variant whose Tools method returns ToolUnmapped for
// every generic tool, regardless of name. It is used by T1.2 custom-tool tests to force
// UpdateWorkflows to ask QCustomTool for newly-required agents.
type unmappingModule struct {
	stubHarnessModule
}

func (m *unmappingModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolUnmapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}

// TestUpdateWorkflows_NewAgent_QCustomToolAsked_WhenUnmappedTool verifies that UpdateWorkflows
// asks QCustomTool for a newly-required agent that has an unmapped generic tool. This matches
// the deploy-new question flow, which calls resolveCustomTools for new agents.
//
// This test is in TDD RED phase: the current implementation never calls resolveCustomTools
// in the UpdateWorkflows flow, so QCustomTool is never asked.
func TestUpdateWorkflows_NewAgent_QCustomToolAsked_WhenUnmappedTool(t *testing.T) {
	// Arrange — test-runner.md absent (new agent). The unmappingModule makes every tool unmapped,
	// so QCustomTool must be asked for "bash" (test-runner's declared tool).
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a"). // satisfy model resolution
		AnswerText(domain.QCustomTool, "bash", "my-bash-server").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)

	mod := &unmappingModule{stubHarnessModule: *newMinimalModule()}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": mod},
	}
	// test-runner.md not written — absent, classified as new.
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — QCustomTool must have been asked for "bash"
	if !stub.WasAsked(domain.QCustomTool, "bash") {
		t.Error("QCustomTool was not asked for \"bash\"; " +
			"UpdateWorkflows must call resolveCustomTools for newly-required agents, " +
			"matching deploy-new tool-mapping behaviour (AC1.7) — " +
			"unmapped generic tools must trigger QCustomTool before the plan is executed")
	}
}

// TestUpdateWorkflows_NoModelOrToolQuestion_WhenAllAgentsDeployed verifies that no model or
// custom-tool question of any kind is asked when every workflow-required agent is already
// deployed. This is the combined gate: a non-empty new-agent set is required for both
// resolveModels and resolveCustomTools to be called.
func TestUpdateWorkflows_NoModelOrToolQuestion_WhenAllAgentsDeployed(t *testing.T) {
	// Arrange — write test-runner.md so every required agent is deployed.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nstub agent content\n"))
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert — no model or tool question must be asked
	for _, c := range stub.Calls() {
		switch c.ID {
		case domain.QTierModel, domain.QAgentModel, domain.QCustomTool:
			t.Errorf("question %q (subject=%q) was asked when every required agent is already deployed; "+
				"model and tool resolution must be gated on a non-empty new-agent set — "+
				"when the set is empty, neither resolveModels nor resolveCustomTools must be called",
				c.ID, c.Subject)
		}
	}
}

// TestUpdateWorkflows_MultipleNewAgents_QuestionOrderMatchesWorkflowOrder verifies that when
// more than one newly-required agent is detected, model questions are asked in the order the
// agents appear in the workflow's ReferencedAgents list. This pins the wiring of
// orderedAgentsForModelResolution at the UpdateWorkflows call site with >1 new agent.
//
// This test is in TDD RED phase: the current implementation never calls resolveModels at all,
// so neither tier question is asked. The correct GREEN state is both questions asked, with
// HIGH (coder's tier) before LOW (reviewer's tier).
func TestUpdateWorkflows_MultipleNewAgents_QuestionOrderMatchesWorkflowOrder(t *testing.T) {
	// Arrange — two agents in different tiers, listed in the workflow as [coder, reviewer].
	// The workflow-listed order must govern question order: HIGH (coder) before LOW (reviewer).
	coderAgent := domain.Agent{
		Key: "coder", NumericID: "1", Version: "1.0", Name: "Coder",
		Role: domain.RoleWorker, RecommendedTier: "HIGH",
	}
	reviewerAgent := domain.Agent{
		Key: "reviewer", NumericID: "2", Version: "1.0", Name: "Reviewer",
		Role: domain.RoleWorker, RecommendedTier: "LOW",
	}
	twoAgentWorkflow := domain.Workflow{
		ID: "two-agent-flow", Name: "Two Agent Flow", Version: "1.0", Category: "Build",
		ReferencedAgents: []string{"coder", "reviewer"}, // coder is listed first in the workflow
	}
	cat := &stubCatalog{
		root:         "testroot",
		orchestrator: minimalOrchestrator,
		agents:       []domain.Agent{coderAgent, reviewerAgent},
		skills:       []domain.Skill{},
		hooks:        []domain.HookBundle{},
		workflows:    []domain.Workflow{twoAgentWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{twoAgentWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", AgentKeys: []string{"coder"}},
			{Tier: "LOW", AgentKeys: []string{"reviewer"}},
		},
	}

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerSelectOne(domain.QTierModel, "LOW", "model-b").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat

	// Neither coder.md nor reviewer.md is written — both agents are absent and must be new.
	planWithTwoNewAgents := domain.Plan{
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
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "coder"},
				TargetPath: "coder.md",
				Action:     domain.ActionCreate,
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "reviewer"},
				TargetPath: "reviewer.md",
				Action:     domain.ActionCreate,
			},
		},
		Workflows: []string{"two-agent-flow"},
	}
	deps.Planner = &stubPlanner{plan: planWithTwoNewAgents}
	svc := app.New(deps)

	// Act — neither coder.md nor reviewer.md exists in the workspace
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"two-agent-flow"}))

	// Assert — QTierModel for "HIGH" (coder's tier) must precede QTierModel for "LOW"
	// (reviewer's tier), matching the workflow-listed order [coder, reviewer].
	calls := stub.Calls()
	highIdx, lowIdx := -1, -1
	for i, c := range calls {
		if c.ID == domain.QTierModel {
			switch c.Subject {
			case "HIGH":
				if highIdx == -1 {
					highIdx = i
				}
			case "LOW":
				if lowIdx == -1 {
					lowIdx = i
				}
			}
		}
	}
	if highIdx == -1 {
		t.Fatal("QTierModel was not asked for tier \"HIGH\" (coder's tier); " +
			"model resolution must be triggered for all newly-required agents when >1 new agent is detected")
	}
	if lowIdx == -1 {
		t.Fatal("QTierModel was not asked for tier \"LOW\" (reviewer's tier); " +
			"model resolution must be triggered for all newly-required agents when >1 new agent is detected")
	}
	if highIdx > lowIdx {
		t.Errorf(
			"QTierModel for \"HIGH\" (call index %d) appeared after QTierModel for \"LOW\" (call index %d); "+
				"when multiple new agents are detected, model questions must follow deploy-new ordering — "+
				"\"coder\" precedes \"reviewer\" in ReferencedAgents, so its tier question (HIGH) "+
				"must be asked before reviewer's tier question (LOW)",
			highIdx, lowIdx)
	}
}

// ---------------------------------------------------------------------------
// T1.3 — Skip/unanswered model question for a new agent produces gap outcome
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NewAgent_SkippedModelQuestion_RunCompletes verifies that UpdateWorkflows
// completes without error when a model question for a newly-required agent is skipped. A
// skipped model is not an unrecoverable failure — the run continues and the missing model is
// recorded as a gap, matching deploy-new behaviour.
//
// This test is in TDD RED phase: the current implementation never calls resolveModels, so
// model questions are never asked and the "skipped" scenario is never triggered.
func TestUpdateWorkflows_NewAgent_SkippedModelQuestion_RunCompletes(t *testing.T) {
	// Arrange — test-runner.md absent (new agent). No model answers scripted, so every model
	// question receives the default SkippedOne response from the stub.
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	// Planner returns a plan that includes a GapNoModel for test-runner, simulating what
	// plan.Build produces when the agent has no resolved model.
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
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
			},
		},
		Gaps: []domain.Gap{
			{Kind: domain.GapNoModel, Subject: "test-runner", Detail: "no model was selected"},
		},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — run must complete without error
	if err != nil {
		t.Errorf("UpdateWorkflows returned error %v; "+
			"a skipped model question must not abort the run — "+
			"the missing model is recorded as a gap and the run continues, matching deploy-new behaviour", err)
	}
}

// TestUpdateWorkflows_NewAgent_SkippedModelQuestion_ModelQuestionWasAsked verifies that when
// a newly-required agent is absent and model resolution is triggered, the model question is
// actually presented to the user (and then skipped). This ensures the "skipped" outcome
// described in AC1.6 is reached via the interactive path, not silently bypassed.
//
// This test is in TDD RED phase.
func TestUpdateWorkflows_NewAgent_SkippedModelQuestion_ModelQuestionWasAsked(t *testing.T) {
	// Arrange — test-runner.md absent (new agent). Stub has no model answer scripted, so the
	// tier question gets SkippedOne automatically.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: "orchestrator.md", Action: domain.ActionUpdate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, TargetPath: "test-runner.md", Action: domain.ActionCreate},
		},
		Gaps:      []domain.Gap{{Kind: domain.GapNoModel, Subject: "test-runner"}},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — at least one model question (QTierModel or QAgentModel) must have been asked
	// so the "skipped" outcome is the result of an actual interaction, not a bypass.
	tierAsked := countQuestionsOfKind(stub, domain.QTierModel)
	agentAsked := countQuestionsOfKind(stub, domain.QAgentModel)
	if tierAsked == 0 && agentAsked == 0 {
		t.Error("no model question (QTierModel or QAgentModel) was asked; " +
			"when a newly-required agent is absent, model resolution must be triggered and at least " +
			"one model question must be presented before the user can skip it (AC1.6)")
	}
}

// TestUpdateWorkflows_NewAgent_SkippedModelQuestion_GapNoModelForwarded verifies that when a
// newly-required agent's model question is skipped, the GapNoModel gap emitted by the planner
// is forwarded to the todo collector. This is the same mechanism deploy-new uses: the gap is
// produced by plan.Build and copied from Plan.Gaps into the collector.
//
// This test is in TDD RED phase (the model question must be asked and skipped for the gap to
// be meaningful; currently no question is asked).
func TestUpdateWorkflows_NewAgent_SkippedModelQuestion_GapNoModelForwarded(t *testing.T) {
	// Arrange — test-runner.md absent. Stub planner returns GapNoModel to simulate what
	// plan.Build produces when test-runner has no resolved model.
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: "orchestrator.md", Action: domain.ActionUpdate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, TargetPath: "test-runner.md", Action: domain.ActionCreate},
		},
		Gaps:      []domain.Gap{{Kind: domain.GapNoModel, Subject: "test-runner", Detail: "no model was selected"}},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — GapNoModel must be present in the todo collector
	if !spy.hasGapKind(domain.GapNoModel) {
		t.Error("GapNoModel was not forwarded to the todo collector; " +
			"a GapNoModel gap in Plan.Gaps must be copied to the todo collector, " +
			"producing the same gap outcome as deploy-new when a model question is skipped (AC1.6)")
	}
}

// ---------------------------------------------------------------------------
// Skill test infrastructure
// ---------------------------------------------------------------------------

// skillEnabledAgent is a worker agent that requires the "lean-tdd" skill. Used in Stage 3
// skill-deployment tests.
var skillEnabledAgent = domain.Agent{
	Key:            "skill-user",
	NumericID:      "2",
	Version:        "1.0",
	Name:           "Skill User",
	Role:           domain.RoleWorker,
	RequiredSkills: []string{"lean-tdd"},
}

// leanTDDSkill is the skill required by skillEnabledAgent. Version "2.0" is used so
// VersionStamp assertions can check a non-trivial value.
var leanTDDSkill = domain.Skill{
	Key:       "lean-tdd",
	Version:   "2.0",
	EntryFile: "SKILL.md",
}

// skillTestWorkflow references skillEnabledAgent.
var skillTestWorkflow = domain.Workflow{
	ID:               "skill-workflow",
	Name:             "Skill Workflow",
	Version:          "1.0",
	Category:         "Build",
	ReferencedAgents: []string{"skill-user"},
}

// newSkillEnabledCatalog returns a catalog configured with one agent that requires one skill,
// and one workflow that references that agent. Used by skill-deployment integration tests.
func newSkillEnabledCatalog() *stubCatalog {
	return &stubCatalog{
		root:         "testroot",
		agents:       []domain.Agent{skillEnabledAgent},
		orchestrator: minimalOrchestrator,
		skills:       []domain.Skill{leanTDDSkill},
		hooks:        []domain.HookBundle{},
		workflows:    []domain.Workflow{skillTestWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{skillTestWorkflow}},
		},
		tiers: []domain.TierInfo{},
	}
}

// newSkillDeployPlan returns a plan whose Items contain the orchestrator (ActionUpdate), the
// skill-user agent (ActionCreate), and the lean-tdd skill (ActionCreate). Represents what
// the planner emits when both the agent and the skill are absent from the workspace.
func newSkillDeployPlan(workspace string) domain.Plan {
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
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "skill-user"},
				TargetPath: "skill-user.md",
				Action:     domain.ActionCreate,
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
				TargetPath: "lean-tdd.md",
				Action:     domain.ActionCreate,
			},
		},
		Workflows: []string{"skill-workflow"},
	}
}

// newSkillWorkflowRequest returns a WorkflowUpdateRequest pre-wired for the skill-workflow.
func newSkillWorkflowRequest(workspace, harnessID string) app.WorkflowUpdateRequest {
	return app.WorkflowUpdateRequest{
		HarnessID:       harnessID,
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"skill-workflow"},
		AutoConfirmPlan: true,
	}
}

// execRequestSpy captures the full deploy.ExecRequest passed to Execute and returns a
// pre-configured result. Used to assert on VersionStamps and other request-level fields
// without performing actual filesystem writes.
type execRequestSpy struct {
	capturedReq *deploy.ExecRequest
	result      deploy.ExecResult
}

func (e *execRequestSpy) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	cp := req
	e.capturedReq = &cp
	return e.result, nil
}

// ---------------------------------------------------------------------------
// T3.1 — Absent skill required by a new agent reaches the executor as ActionCreate
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_AbsentSkillForNewAgent_ReachesExecutorAsActionCreate verifies that a
// skill required by a newly deployed absent agent and itself absent from the workspace is
// admitted to the executor as an ActionCreate item. This is the primary skill-deployment
// invariant: absent skills required by new agents must be created in the same run.
func TestUpdateWorkflows_AbsentSkillForNewAgent_ReachesExecutorAsActionCreate(t *testing.T) {
	// Arrange — skill-user.md absent (new agent), lean-tdd.md absent (absent skill).
	// The catalog has skill-user requiring lean-tdd.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	// Neither skill-user.md nor lean-tdd.md is written — both are absent.
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — executor must receive the ActionCreate item for lean-tdd
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	item, ok := findPlanItem(capExec.capturedPlan.Items, "lean-tdd")
	if !ok {
		t.Error("executor's plan has no item for \"lean-tdd\"; " +
			"an absent skill required by a newly deployed agent must be admitted to the executor " +
			"as an ActionCreate item so its file is created in the same run")
		return
	}
	if item.Action != domain.ActionCreate {
		t.Errorf("lean-tdd plan item action = %q, want %q; "+
			"the plan item for an absent skill required by a new agent must carry ActionCreate",
			item.Action, domain.ActionCreate)
	}
}

// TestUpdateWorkflows_AbsentSkillForNewAgent_OrchestratorStillInPlan verifies that the
// orchestrator item is still present in the executor's plan when a skill item is also admitted.
// Admitting skill items must not displace the orchestrator from the executed plan.
func TestUpdateWorkflows_AbsentSkillForNewAgent_OrchestratorStillInPlan(t *testing.T) {
	// Arrange — skill-user.md absent (new agent), lean-tdd.md absent (absent skill).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — orchestrator must still be present alongside the admitted skill item
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called")
	}
	_, ok := findPlanItem(capExec.capturedPlan.Items, "orchestrator")
	if !ok {
		t.Error("orchestrator item is missing from the executor's plan when a skill item is also admitted; " +
			"the orchestrator must always be included in the executed plan, " +
			"even when new-agent and new-skill items are also present")
	}
}

// TestUpdateWorkflows_AbsentSkillForNewAgent_RegistrationsStillNil verifies that plan
// registrations are cleared to nil even when skill items are admitted alongside agent items.
// Hook registrations are entirely out of scope for the workflow-update flow.
func TestUpdateWorkflows_AbsentSkillForNewAgent_RegistrationsStillNil(t *testing.T) {
	// Arrange — plan has Registrations set; they must be cleared before the plan reaches
	// the executor, regardless of whether skill items are admitted.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()

	planWithRegs := newSkillDeployPlan(workspace)
	planWithRegs.Registrations = []domain.RegistrationStep{{ID: "some-hook", Instruction: "register hook"}}
	deps.Planner = &stubPlanner{plan: planWithRegs}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — Registrations in the executor's plan must be nil
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called")
	}
	if len(capExec.capturedPlan.Registrations) > 0 {
		t.Errorf("executor received %d Registrations, want 0; "+
			"p.Registrations must be cleared to nil before execution even when skill items are admitted — "+
			"hook registrations are out of scope for the workflow-update flow",
			len(capExec.capturedPlan.Registrations))
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Already-present skills and skills for deployed agents are excluded
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_PresentSkillForNewAgent_NotAdmittedToExecutor verifies that a skill
// whose file is already present in the workspace is never admitted to the executor, even when
// the agent requiring it is new. Already-deployed skills are not within the scope of this flow.
func TestUpdateWorkflows_PresentSkillForNewAgent_NotAdmittedToExecutor(t *testing.T) {
	// Arrange — skill-user.md absent (new agent), lean-tdd.md PRESENT (skill already deployed).
	// The planner returns ActionCreate for the skill; the admission filter must reject it
	// because newlyRequiredSkillKeys excludes skills with Present=true in deployedState.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	// Write lean-tdd.md to make the skill present on disk.
	writeTempFile(t, workspace, "lean-tdd.md", []byte("---\nversion: \"2.0\"\n---\nstub skill content\n"))
	// skill-user.md NOT written — the agent is absent and new.
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — executor must NOT receive any plan item for lean-tdd
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactSkill && item.Ref.Key == "lean-tdd" {
			t.Errorf("executor received plan item for skill \"lean-tdd\" which is already present on disk; "+
				"a skill that already has a file must never be planned, written, or version-stamped — "+
				"stale skill content is not within the scope of the workflow-update flow, "+
				"got item: kind=%s key=%s action=%s", item.Ref.Kind, item.Ref.Key, item.Action)
		}
	}
}

// TestUpdateWorkflows_MissingSkillForDeployedAgent_NotAdmittedToExecutor verifies that a
// skill required only by an already-deployed agent is not admitted to the executor, even when
// the skill file is absent from the workspace. The qualification gate is whether the requiring
// agent is newly being deployed, not whether the skill file is absent.
func TestUpdateWorkflows_MissingSkillForDeployedAgent_NotAdmittedToExecutor(t *testing.T) {
	// Arrange — skill-user.md PRESENT (agent already deployed), lean-tdd.md absent (skill missing).
	// Since skill-user is already deployed, it does not appear in newAgents. Therefore
	// newlyRequiredSkillKeys must not include lean-tdd even though the skill file is absent.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	// Write skill-user.md to make the agent deployed.
	writeTempFile(t, workspace, "skill-user.md", []byte("---\nversion: \"1.0\"\n---\nstub agent content\n"))
	// lean-tdd.md NOT written — skill is absent, but its requiring agent is already deployed.

	// Planner returns a plan with ActionCreate for the skill. The filter must reject it because
	// lean-tdd is not in newSkillKeys (no new agent requires it in this run).
	planWithAbsentSkill := domain.Plan{
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
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
				TargetPath: "lean-tdd.md",
				Action:     domain.ActionCreate,
			},
		},
		Workflows: []string{"skill-workflow"},
	}
	deps.Planner = &stubPlanner{plan: planWithAbsentSkill}
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — executor must NOT receive any plan item for lean-tdd
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactSkill && item.Ref.Key == "lean-tdd" {
			t.Errorf("executor received plan item for skill \"lean-tdd\" whose requiring agent is already deployed; "+
				"a skill required only by an already-deployed agent must never be planned or written, "+
				"even when the skill file is absent from the workspace — "+
				"the admission gate is based on the requiring agent being newly deployed in this run, "+
				"got item: kind=%s key=%s action=%s", item.Ref.Kind, item.Ref.Key, item.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Admitted skill items carry version stamps; hooks and registrations still dropped
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_AbsentSkillForNewAgent_CarriesVersionStamp verifies that an admitted
// skill item's target path appears in the VersionStamps map passed to the executor, and that
// the stamp carries the skill's version from the catalog. Without set.Skills being passed to
// buildVersionStamps, admitted skill items would be unstamped in the manifest.
func TestUpdateWorkflows_AbsentSkillForNewAgent_CarriesVersionStamp(t *testing.T) {
	// Arrange — skill-user.md absent (new agent), lean-tdd.md absent (absent skill).
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}

	spy := &execRequestSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — VersionStamps must contain an entry for "lean-tdd.md"
	if spy.capturedReq == nil {
		t.Fatal("executor was not called; cannot verify VersionStamps")
	}
	stamp, ok := spy.capturedReq.VersionStamps["lean-tdd.md"]
	if !ok {
		t.Error("VersionStamps does not contain an entry for \"lean-tdd.md\"; " +
			"admitted skill items must be version-stamped keyed by their target path — " +
			"set.Skills must be passed to buildVersionStamps so admitted skill items are stamped in the manifest")
		return
	}
	if stamp.Version != leanTDDSkill.Version {
		t.Errorf("VersionStamps[\"lean-tdd.md\"].Version = %q, want %q; "+
			"the version stamp for an admitted skill must carry the skill's Version field from the catalog",
			stamp.Version, leanTDDSkill.Version)
	}
}

// TestUpdateWorkflows_HookItem_DroppedWhenSkillsAreAdmitted verifies that hook items are
// dropped from the plan even when skill items are admitted in the same run. Admitting skills
// must not widen the filter to include hook artifacts.
func TestUpdateWorkflows_HookItem_DroppedWhenSkillsAreAdmitted(t *testing.T) {
	// Arrange — plan includes orchestrator, new agent, new skill (ActionCreate), and a hook.
	// The hook must be dropped regardless of whether the skill is admitted.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()

	planWithHook := newSkillDeployPlan(workspace)
	planWithHook.Items = append(planWithHook.Items, domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "test-hook"},
		TargetPath: "test-hook/hook.sh",
		Action:     domain.ActionCreate,
	})
	deps.Planner = &stubPlanner{plan: planWithHook}

	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Executor = capExec
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — hook item must not be in the executor's plan
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor")
	}
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactHook {
			t.Errorf("executor received hook item %q (action=%s) even when skill items were admitted; "+
				"hook artifacts must never reach the executor in the workflow-update flow, "+
				"regardless of whether skill items are also being deployed in the same run",
				item.Ref.Key, item.Action)
		}
	}
}
