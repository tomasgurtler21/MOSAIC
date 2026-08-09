package app_test

// modelselectioncancel_test.go verifies that pressing Esc (domain.Cancelled) at a tier-model
// or agent-model question aborts the entire run, for the new-deploy flow, the update flow,
// and the workflow-update flow — instead of silently falling through to the next question as
// if the user had skipped it.
//
// Reachability note: the Update flow (svc.Update) always suppresses QTierModel by passing
// tierSkipOverride=true to resolveModels, so a tier-model cancellation can never occur on that
// flow's call site. Only agent-model cancellation is exercised for Update; tier-model
// cancellation is exercised for the new-deploy and workflow-update flows, both of which ask
// QTierModel (tierSkipOverride=false).
//
// Verified invariants:
//
// Abort on cancellation (AC2.1, AC2.2, AC2.3):
//   - Esc at a QTierModel question aborts the run with an error matching
//     errors.Is(err, app.ErrModelSelectionCancelled) — new-deploy and workflow-update flows.
//   - Esc at a QAgentModel question aborts the run the same way — all three flows, including
//     both resolveModels call sites in DeployNew (regular agents and infrastructure agents).
//   - No further model question is asked after the cancellation is observed.
//
// No persistence, no execution on an aborted run (AC2.4):
//   - UserConfigStore.Save is never called.
//   - The executor's Execute method is never called.
//
// Unchanged skip semantics (AC2.5, non-regression):
//   - SkippedOne at a tier or agent question leaves that item unresolved and the run still
//     proceeds to plan review and execution (no error).
//   - SkippedAll latches for that question kind and the run still proceeds to plan review and
//     execution (no error).
//   - AllowSkip and AllowSkipAll remain true on both QTierModel and QAgentModel questions.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// DeployNew — Esc at a tier-model question aborts the run (AC2.1)
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtTierModel_ReturnsErrModelSelectionCancelled verifies that pressing Esc
// (Cancelled) at a QTierModel question aborts DeployNew with an error matching
// errors.Is(err, app.ErrModelSelectionCancelled).
func TestDeployNew_EscAtTierModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTierModel, "HIGH").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QTierModel; expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false; "+
			"Esc at a tier-model question must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtTierModel_NoFurtherTierQuestionsAsked verifies that once QTierModel is
// cancelled, no further tier-model question is asked (AC2.1: "no further model questions").
func TestDeployNew_EscAtTierModel_NoFurtherTierQuestionsAsked(t *testing.T) {
	// Arrange — catalog with two tiers; cancel the first tier question.
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, domain.Agent{
		Key:             "planner",
		NumericID:       "2",
		Version:         "1.0",
		Name:            "Planner",
		Role:            domain.RoleWorker,
		Category:        "Planning",
		RecommendedTier: "MEDIUM",
		TierRationale:   "General purpose model",
	})
	cat.tiers = append(cat.tiers, domain.TierInfo{
		Tier:      "MEDIUM",
		Rationale: "General purpose model",
		AgentKeys: []string{"planner"},
	})

	cancelHigh := &cancelFirstTierModelInteraction{cancelTier: "HIGH"}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = cancelHigh
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if cancelHigh.tierCalls != 1 {
		t.Errorf("QTierModel was asked %d time(s) after Esc on the first tier; want 1; "+
			"a cancellation must stop the loop immediately, asking no further tier question", cancelHigh.tierCalls)
	}
}

// ---------------------------------------------------------------------------
// DeployNew — Esc at an agent-model question aborts the run (AC2.2)
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtAgentModel_ReturnsErrModelSelectionCancelled verifies that pressing Esc
// at a QAgentModel question aborts DeployNew with the same sentinel.
func TestDeployNew_EscAtAgentModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange — no tier mapping resolves, so QAgentModel is asked for "test-runner"; cancel it.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QAgentModel, "test-runner").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = &spyUserConfig{}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QAgentModel; expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false; "+
			"Esc at an agent-model question must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtAgentModel_NoFurtherAgentQuestionsAsked verifies that once QAgentModel is
// cancelled, no further agent-model question is asked.
func TestDeployNew_EscAtAgentModel_NoFurtherAgentQuestionsAsked(t *testing.T) {
	// Arrange — two agents needing QAgentModel (tier questions suppressed via SkipAll).
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, domain.Agent{
		Key:             "code-writer",
		NumericID:       "3",
		Version:         "1.0",
		Name:            "Code Writer",
		Role:            domain.RoleWorker,
		Category:        "Creation",
		RecommendedTier: "HIGH",
	})

	cancelFirst := &cancelFirstAgentModelInteraction{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = cancelFirst
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert
	if cancelFirst.agentCalls != 1 {
		t.Errorf("QAgentModel was asked %d time(s) after Esc on the first agent; want 1; "+
			"a cancellation must stop the loop immediately, asking no further agent question", cancelFirst.agentCalls)
	}
}

// ---------------------------------------------------------------------------
// DeployNew — both resolveModels call sites abort identically (AC2.3)
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtInfraAgentModel_ReturnsErrModelSelectionCancelled verifies that a
// cancellation in the infrastructure-agent batch (the second resolveModels call site,
// deploy.go's infra branch) aborts the run exactly as one in the regular-agent batch does.
func TestDeployNew_EscAtInfraAgentModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange — regular agents resolve cleanly; the infra agent's question is cancelled.
	cat := catalogWithOneInfraAgent()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "orchestrator", "model-a").
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-a").
		AnswerCancelledChoice(domain.QAgentModel, "checkpoint-agent").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at the infra-agent QAgentModel question; " +
			"expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false for an infra-agent-batch "+
			"cancellation; the infra batch (deploy.go's second resolveModels call) must abort the run "+
			"exactly as the regular-agent batch does (AC2.3); got: %v", err)
	}
}

// TestDeployNew_EscAtInfraAgentModel_DoesNotReachExecutor verifies that a cancellation in the
// infra-agent batch aborts before the merged model map is used and before execution.
func TestDeployNew_EscAtInfraAgentModel_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	cat := catalogWithOneInfraAgent()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "orchestrator", "model-a").
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-a").
		AnswerCancelledChoice(domain.QAgentModel, "checkpoint-agent").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc in the infra-agent batch; " +
			"an aborted run must never reach plan execution (AC2.4)")
	}
}

// ---------------------------------------------------------------------------
// DeployNew — an aborted run persists nothing (AC2.4)
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtModelQuestion_DoesNotCallUserConfigSave verifies that a cancellation at a
// model question never causes UserConfigStore.Save to be called.
func TestDeployNew_EscAtModelQuestion_DoesNotCallUserConfigSave(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTierModel, "HIGH").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if len(spy.saved) != 0 {
		t.Errorf("UserConfigStore.Save was called %d time(s) after Esc at a model question; "+
			"an Esc-aborted run must persist nothing (AC2.4)", len(spy.saved))
	}
}

// TestDeployNew_EscAtModelQuestion_DoesNotReachExecutor verifies that a cancellation at a
// model question never causes Executor.Execute to be called.
func TestDeployNew_EscAtModelQuestion_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTierModel, "HIGH").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at a model question; " +
			"an Esc-aborted run must never reach plan execution (AC2.4)")
	}
}

// ---------------------------------------------------------------------------
// Update flow — same distinguishable outcome (AC2.3)
// ---------------------------------------------------------------------------

// TestUpdate_EscAtAgentModel_ReturnsErrModelSelectionCancelled verifies that the Update flow
// aborts with the same sentinel when the user cancels an agent-model question for a newly
// added agent. Update always suppresses QTierModel (tierSkipOverride=true), so only the
// agent-model question is reachable on this flow.
func TestUpdate_EscAtAgentModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange — empty workspace, so "test-runner" (added via AddWorkflowIDs) is a new agent.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QAgentModel, "test-runner").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error after Esc at QAgentModel; expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false for Update; "+
			"Update must use the same sentinel as DeployNew so the TUI handles both flows "+
			"identically (AC2.3); got: %v", err)
	}
}

// TestUpdate_EscAtAgentModel_DoesNotCallUserConfigSaveOrExecutor verifies that an Esc-aborted
// Update run persists nothing and never reaches execution (AC2.4).
func TestUpdate_EscAtAgentModel_DoesNotCallUserConfigSaveOrExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QAgentModel, "test-runner").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	userSpy := &spyUserConfig{}
	deps.UserConfig = userSpy
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if len(userSpy.saved) != 0 {
		t.Errorf("UserConfigStore.Save was called %d time(s) after Esc in Update; "+
			"an Esc-aborted run must persist nothing (AC2.4)", len(userSpy.saved))
	}
	if spy.called {
		t.Error("Executor.Execute was called after Esc in Update; " +
			"an Esc-aborted run must never reach plan execution (AC2.4)")
	}
}

// ---------------------------------------------------------------------------
// Workflow-update flow — same distinguishable outcome (AC2.3)
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_EscAtTierModel_ReturnsErrModelSelectionCancelled verifies that the
// workflow-update flow aborts with the same sentinel when the user cancels a tier-model
// question for a newly required agent. Unlike Update, UpdateWorkflows asks QTierModel
// (tierSkipOverride=false), so tier-model cancellation is reachable here.
func TestUpdateWorkflows_EscAtTierModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange — "test-runner.md" absent, so it is newly required; cancel its tier question.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTierModel, "HIGH").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error after Esc at QTierModel; expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false for UpdateWorkflows; "+
			"got: %v", err)
	}
}

// TestUpdateWorkflows_EscAtAgentModel_ReturnsErrModelSelectionCancelled verifies the same
// outcome for a cancelled agent-model question.
func TestUpdateWorkflows_EscAtAgentModel_ReturnsErrModelSelectionCancelled(t *testing.T) {
	// Arrange — tier question skipped so QAgentModel is reached; cancel it.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QAgentModel, "test-runner").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	svc := app.New(deps)

	req := newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"})
	req.SkipAll = map[domain.QuestionID]bool{domain.QTierModel: true}

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error after Esc at QAgentModel; expected ErrModelSelectionCancelled")
	}
	if !errors.Is(err, app.ErrModelSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrModelSelectionCancelled) = false for UpdateWorkflows; "+
			"got: %v", err)
	}
}

// TestUpdateWorkflows_EscAtModelQuestion_DoesNotReachExecutor verifies that an Esc-aborted
// UpdateWorkflows run never reaches execution (AC2.4).
func TestUpdateWorkflows_EscAtModelQuestion_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QTierModel, "HIGH").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc in UpdateWorkflows; " +
			"an Esc-aborted run must never reach plan execution (AC2.4)")
	}
}

// ---------------------------------------------------------------------------
// Non-regression — SkippedOne / SkippedAll semantics unchanged (AC2.5)
// ---------------------------------------------------------------------------

// TestDeployNew_TierSkippedOne_StillProceedsToReviewAndExecution verifies that answering
// SkippedOne for a tier question is still treated as a skip, not a cancellation: the run
// proceeds to plan review and execution with no error.
func TestDeployNew_TierSkippedOne_StillProceedsToReviewAndExecution(t *testing.T) {
	// Arrange — the default stub answer for an unscripted question is SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QTierModel: %v; "+
			"SkippedOne must not become an abort (AC2.5, non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne tier answer; " +
			"a SkippedOne answer must still let the run proceed to execution (AC2.5)")
	}
}

// TestDeployNew_TierSkippedAll_StillProceedsToReviewAndExecution verifies that answering
// SkippedAll for a tier question is still treated as a latch, not a cancellation.
func TestDeployNew_TierSkippedAll_StillProceedsToReviewAndExecution(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QTierModel, "HIGH").
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedAll at QTierModel: %v; "+
			"SkippedAll must not become an abort (AC2.5, non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedAll tier answer; " +
			"a SkippedAll answer must still let the run proceed to execution (AC2.5)")
	}
}

// TestDeployNew_AgentSkippedOne_StillProceedsToReviewAndExecution verifies that answering
// SkippedOne for an agent-model question is still treated as a skip, not a cancellation.
func TestDeployNew_AgentSkippedOne_StillProceedsToReviewAndExecution(t *testing.T) {
	// Arrange — unscripted QAgentModel defaults to SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = &spyUserConfig{}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QAgentModel: %v; "+
			"SkippedOne must not become an abort (AC2.5, non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne agent answer; " +
			"a SkippedOne answer must still let the run proceed to execution (AC2.5)")
	}
}

// TestDeployNew_AgentSkippedAll_StillProceedsToReviewAndExecution verifies that answering
// SkippedAll for an agent-model question is still treated as a latch, not a cancellation.
func TestDeployNew_AgentSkippedAll_StillProceedsToReviewAndExecution(t *testing.T) {
	// Arrange — two agents; SkippedAll on the first agent-model question.
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, domain.Agent{
		Key:             "code-writer",
		NumericID:       "3",
		Version:         "1.0",
		Name:            "Code Writer",
		Role:            domain.RoleWorker,
		Category:        "Creation",
		RecommendedTier: "HIGH",
	})
	skipAllStub := &skipAllAgentInteraction{}
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = skipAllStub
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedAll at QAgentModel: %v; "+
			"SkippedAll must not become an abort (AC2.5, non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedAll agent answer; " +
			"a SkippedAll answer must still let the run proceed to execution (AC2.5)")
	}
}

// TestDeployNew_TierQuestion_AllowSkipFlagsRemainTrue verifies that QTierModel still declares
// AllowSkip and AllowSkipAll as true (AC2.5: the s/S keys must keep working).
func TestDeployNew_TierQuestion_AllowSkipFlagsRemainTrue(t *testing.T) {
	// Arrange
	var captured domain.ChoiceQuestion
	var got bool
	capture := &tierSubjectCapture{
		onTierModel:   func(q domain.ChoiceQuestion) { captured = q; got = true },
		defaultChoice: domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !got {
		t.Fatal("QTierModel was not asked; cannot verify AllowSkip/AllowSkipAll")
	}
	if !captured.AllowSkip {
		t.Error("QTierModel.AllowSkip is false; the 's' key must remain available (AC2.5)")
	}
	if !captured.AllowSkipAll {
		t.Error("QTierModel.AllowSkipAll is false; the 'S' key must remain available (AC2.5)")
	}
}

// TestDeployNew_AgentQuestion_AllowSkipFlagsRemainTrue verifies that QAgentModel still
// declares AllowSkip and AllowSkipAll as true.
func TestDeployNew_AgentQuestion_AllowSkipFlagsRemainTrue(t *testing.T) {
	// Arrange
	var captured domain.ChoiceQuestion
	var got bool
	capture := &agentModelCapture{
		onAgentModel: func(q domain.ChoiceQuestion) {
			if q.Subject == "test-runner" {
				captured = q
				got = true
			}
		},
		defaultAnswer: domain.ChoiceAnswer{Status: domain.SkippedOne},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !got {
		t.Fatal("QAgentModel was not asked for \"test-runner\"; cannot verify AllowSkip/AllowSkipAll")
	}
	if !captured.AllowSkip {
		t.Error("QAgentModel.AllowSkip is false; the 's' key must remain available (AC2.5)")
	}
	if !captured.AllowSkipAll {
		t.Error("QAgentModel.AllowSkipAll is false; the 'S' key must remain available (AC2.5)")
	}
}

// ---------------------------------------------------------------------------
// Helpers local to this file
// ---------------------------------------------------------------------------

// cancelFirstTierModelInteraction returns a Cancelled ChoiceAnswer for the configured tier on
// its first QTierModel call and records the total number of QTierModel calls it received.
type cancelFirstTierModelInteraction struct {
	cancelTier domain.Tier
	tierCalls  int
}

func (c *cancelFirstTierModelInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTierModel {
		c.tierCalls++
		if domain.Tier(q.Subject) == c.cancelTier {
			return domain.ChoiceAnswer{Status: domain.Cancelled}, nil
		}
	}
	return domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"}, nil
}
func (c *cancelFirstTierModelInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
}
func (c *cancelFirstTierModelInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub"}, nil
}
func (c *cancelFirstTierModelInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (c *cancelFirstTierModelInteraction) Notify(_ context.Context, n domain.Notice)          {}
func (c *cancelFirstTierModelInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *cancelFirstTierModelInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// cancelFirstAgentModelInteraction returns a Cancelled ChoiceAnswer on the first QAgentModel
// call it receives and records the total number of QAgentModel calls.
type cancelFirstAgentModelInteraction struct {
	agentCalls int
	cancelled  bool
}

func (c *cancelFirstAgentModelInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QAgentModel {
		c.agentCalls++
		if !c.cancelled {
			c.cancelled = true
			return domain.ChoiceAnswer{Status: domain.Cancelled}, nil
		}
	}
	return domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"}, nil
}
func (c *cancelFirstAgentModelInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
}
func (c *cancelFirstAgentModelInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub"}, nil
}
func (c *cancelFirstAgentModelInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (c *cancelFirstAgentModelInteraction) Notify(_ context.Context, n domain.Notice)          {}
func (c *cancelFirstAgentModelInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *cancelFirstAgentModelInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
