package app_test

// selectioncancel_test.go verifies that pressing Esc (domain.Cancelled) at any of the four
// multi-select selection questions — workflows, utility agents, infrastructure agents,
// hook bundles — aborts the entire run, matching the behaviour Esc already has on
// model-selection questions. It also covers the consolidated DeployAgents flow, where Esc
// at QDeployAgents must abort with the same sentinel.
//
// Verified invariants:
//
// Abort on cancellation:
//   - Esc at QWorkflows aborts DeployNew with an error matching
//     errors.Is(err, app.ErrSelectionCancelled).
//   - Esc at QUtilityAgents aborts DeployNew with the same sentinel.
//   - Esc at QInfrastructureAgents aborts DeployNew.
//   - Esc at QHooks aborts DeployNew.
//   - Esc at QDeployAgents aborts DeployAgents.
//   - A cancelled run asks no subsequent question and performs no write.
//
// Unchanged skip semantics (non-regression):
//   - SkippedOne at any selection question still yields an empty selection and the run
//     proceeds to plan review and execution (no error).
//   - SkippedAll at any selection question still yields an empty selection and the run
//     proceeds (no error).
//   - Answered with zero selected IDs still proceeds with an empty selection (no error).

import (
	"context"
	"testing"

	"errors"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers shared by this file
// ---------------------------------------------------------------------------

// catalogWithHooks returns a catalog that has one hook bundle, so QHooks is asked when
// HookIDs is nil in the request.
func catalogWithHooks() *stubCatalog {
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{
		{Key: "safety-hooks", Description: "Safety and validation hooks"},
	}
	return cat
}

// catalogWithUtilityAgents returns a minimal catalog extended with one utility agent so
// QUtilityAgents is asked when UtilityAgentIDs is nil.
func catalogWithSelectionTestUtilityAgent() *stubCatalog {
	cat := newMinimalCatalog()
	cat.utilityAgents = []domain.Agent{
		{
			Key:             "lint-agent",
			NumericID:       "50",
			Version:         "1.0",
			Name:            "Lint Agent",
			Description:     "Runs linting checks",
			Role:            domain.RoleWorker,
			RecommendedTier: "LOW",
		},
	}
	return cat
}

// catalogWithInfraAgents returns a minimal catalog extended with one infrastructure agent
// so QInfrastructureAgents is asked when InfrastructureAgentIDs is nil.
func catalogWithSelectionTestInfraAgent() *stubCatalog {
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{
		{
			Key:             "monitor-agent",
			NumericID:       "51",
			Version:         "1.0",
			Name:            "Monitor Agent",
			Description:     "Monitors deployment health",
			Role:            domain.RoleWorker,
			Infrastructure:  "monitoring",
			RecommendedTier: "HIGH",
		},
	}
	return cat
}

// ---------------------------------------------------------------------------
// DeployNew — Esc at QWorkflows aborts the run
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtWorkflows_ReturnsErrSelectionCancelled verifies that pressing Esc at the
// workflow multi-select screen aborts DeployNew with an error matching
// errors.Is(err, app.ErrSelectionCancelled).
func TestDeployNew_EscAtWorkflows_ReturnsErrSelectionCancelled(t *testing.T) {
	// Arrange — cancel the QWorkflows question; utility/infra/hook pre-answered as empty.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QWorkflows, "").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QWorkflows; expected ErrSelectionCancelled")
	}
	if !errors.Is(err, app.ErrSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrSelectionCancelled) = false; "+
			"Esc at QWorkflows must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtWorkflows_DoesNotAskSubsequentQuestions verifies that once QWorkflows is
// cancelled, no subsequent selection question is asked — the flow returns immediately.
func TestDeployNew_EscAtWorkflows_DoesNotAskSubsequentQuestions(t *testing.T) {
	// Arrange — cancel workflows; the catalog has utility agents and hooks so their questions
	// would be asked if the flow continued past the cancelled workflow step.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QWorkflows, "").
		Build()
	cat := newMinimalCatalog()
	cat.utilityAgents = []domain.Agent{{
		Key: "lint-agent", NumericID: "50", Version: "1.0",
		Name: "Lint Agent", Role: domain.RoleWorker, RecommendedTier: "LOW",
	}}
	cat.hooks = []domain.HookBundle{{Key: "safety-hooks"}}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		// WorkflowIDs nil so QWorkflows is asked (and cancelled)
		// UtilityAgentIDs nil so QUtilityAgents would be asked if flow continued
		// HookIDs nil so QHooks would be asked if flow continued
		AutoConfirmPlan: true,
	})

	// Assert — QUtilityAgents and QHooks must not have been asked after QWorkflows was cancelled.
	if stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was asked after QWorkflows was cancelled; " +
			"a cancelled run must ask no subsequent question")
	}
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked after QWorkflows was cancelled; " +
			"a cancelled run must ask no subsequent question")
	}
}

// TestDeployNew_EscAtWorkflows_DoesNotReachExecutor verifies that a QWorkflows cancellation
// never causes the executor to be called — no plan is executed.
func TestDeployNew_EscAtWorkflows_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QWorkflows, "").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at QWorkflows; " +
			"a cancelled run must never reach plan execution")
	}
}

// ---------------------------------------------------------------------------
// DeployNew — Esc at QUtilityAgents aborts the run
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtUtilityAgents_ReturnsErrSelectionCancelled verifies that pressing Esc
// at the utility-agent multi-select screen aborts DeployNew with ErrSelectionCancelled.
func TestDeployNew_EscAtUtilityAgents_ReturnsErrSelectionCancelled(t *testing.T) {
	// Arrange — workflows pre-answered; cancel the utility-agents question.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QUtilityAgents, "").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestUtilityAgent()
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QUtilityAgents; expected ErrSelectionCancelled")
	}
	if !errors.Is(err, app.ErrSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrSelectionCancelled) = false; "+
			"Esc at QUtilityAgents must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtUtilityAgents_DoesNotReachExecutor verifies that a QUtilityAgents
// cancellation never reaches plan execution.
func TestDeployNew_EscAtUtilityAgents_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QUtilityAgents, "").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestUtilityAgent()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at QUtilityAgents; " +
			"a cancelled run must never reach plan execution")
	}
}

// ---------------------------------------------------------------------------
// DeployNew — Esc at QInfrastructureAgents aborts the run
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtInfrastructureAgents_ReturnsErrSelectionCancelled verifies that pressing
// Esc at the infrastructure-agent multi-select screen aborts DeployNew with ErrSelectionCancelled.
func TestDeployNew_EscAtInfrastructureAgents_ReturnsErrSelectionCancelled(t *testing.T) {
	// Arrange — workflows and utility agents pre-answered; cancel infrastructure agents.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QInfrastructureAgents, "").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestInfraAgent()
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QInfrastructureAgents; expected ErrSelectionCancelled")
	}
	if !errors.Is(err, app.ErrSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrSelectionCancelled) = false; "+
			"Esc at QInfrastructureAgents must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtInfrastructureAgents_DoesNotReachExecutor verifies that a
// QInfrastructureAgents cancellation never reaches plan execution.
func TestDeployNew_EscAtInfrastructureAgents_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QInfrastructureAgents, "").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestInfraAgent()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at QInfrastructureAgents; " +
			"a cancelled run must never reach plan execution")
	}
}

// ---------------------------------------------------------------------------
// DeployNew — Esc at QHooks aborts the run
// ---------------------------------------------------------------------------

// TestDeployNew_EscAtHooks_ReturnsErrSelectionCancelled verifies that pressing Esc at the
// hook-bundle multi-select screen aborts DeployNew with ErrSelectionCancelled.
func TestDeployNew_EscAtHooks_ReturnsErrSelectionCancelled(t *testing.T) {
	// Arrange — workflows, utility agents, and infrastructure agents pre-answered; cancel hooks.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QHooks, "").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHooks()
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after Esc at QHooks; expected ErrSelectionCancelled")
	}
	if !errors.Is(err, app.ErrSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrSelectionCancelled) = false; "+
			"Esc at QHooks must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployNew_EscAtHooks_DoesNotReachExecutor verifies that a QHooks cancellation never
// reaches plan execution.
func TestDeployNew_EscAtHooks_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QHooks, "").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHooks()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at QHooks; " +
			"a cancelled run must never reach plan execution")
	}
}

// ---------------------------------------------------------------------------
// DeployAgents — Esc at QDeployAgents aborts the run
// ---------------------------------------------------------------------------

// TestDeployAgents_EscAtQDeployAgents_ReturnsErrSelectionCancelled verifies that pressing
// Esc at the merged deploy-agents multi-select screen aborts DeployAgents with an error
// matching errors.Is(err, app.ErrSelectionCancelled).
func TestDeployAgents_EscAtQDeployAgents_ReturnsErrSelectionCancelled(t *testing.T) {
	// Arrange — all ID slices nil so QDeployAgents is asked; cancel it.
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QDeployAgents, "").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployAgentQuestions()
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("DeployAgents returned nil error after Esc at QDeployAgents; expected ErrSelectionCancelled")
	}
	if !errors.Is(err, app.ErrSelectionCancelled) {
		t.Errorf("errors.Is(err, app.ErrSelectionCancelled) = false; "+
			"Esc at QDeployAgents must abort the run with the distinguishable sentinel; got: %v", err)
	}
}

// TestDeployAgents_EscAtQDeployAgents_DoesNotReachExecutor verifies that a QDeployAgents
// cancellation never causes the executor to be called — no plan is executed.
func TestDeployAgents_EscAtQDeployAgents_DoesNotReachExecutor(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerCancelledChoice(domain.QDeployAgents, "").
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployAgentQuestions()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if spy.called {
		t.Error("Executor.Execute was called after Esc at QDeployAgents; " +
			"a cancelled run must never reach plan execution")
	}
}

// ---------------------------------------------------------------------------
// Non-regression — SkippedOne at any selection question still proceeds
// ---------------------------------------------------------------------------

// TestDeployNew_WorkflowsSkippedOne_RunProceeds verifies that answering SkippedOne for
// QWorkflows yields an empty workflow selection and lets the run proceed — the skip semantics
// are unchanged.
func TestDeployNew_WorkflowsSkippedOne_RunProceeds(t *testing.T) {
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
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QWorkflows: %v; "+
			"SkippedOne must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne workflows answer; " +
			"a SkippedOne answer must still let the run proceed to execution")
	}
}

// TestDeployNew_UtilityAgentsSkippedOne_RunProceeds verifies that answering SkippedOne for
// QUtilityAgents lets the run continue with an empty utility-agent selection.
func TestDeployNew_UtilityAgentsSkippedOne_RunProceeds(t *testing.T) {
	// Arrange — the default stub answer for QUtilityAgents is SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestUtilityAgent()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QUtilityAgents: %v; "+
			"SkippedOne must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne utility-agents answer; " +
			"a SkippedOne answer must still let the run proceed to execution")
	}
}

// TestDeployNew_InfraAgentsSkippedOne_RunProceeds verifies that answering SkippedOne for
// QInfrastructureAgents lets the run continue with an empty infrastructure selection.
func TestDeployNew_InfraAgentsSkippedOne_RunProceeds(t *testing.T) {
	// Arrange — the default stub answer for QInfrastructureAgents is SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestInfraAgent()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		UtilityAgentIDs: []string{},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QInfrastructureAgents: %v; "+
			"SkippedOne must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne infra-agents answer; " +
			"a SkippedOne answer must still let the run proceed to execution")
	}
}

// TestDeployNew_HooksSkippedOne_RunProceeds verifies that answering SkippedOne for QHooks
// lets the run continue with an empty hook selection.
func TestDeployNew_HooksSkippedOne_RunProceeds(t *testing.T) {
	// Arrange — the default stub answer for QHooks is SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithHooks()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedOne at QHooks: %v; "+
			"SkippedOne must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne hooks answer; " +
			"a SkippedOne answer must still let the run proceed to execution")
	}
}

// TestDeployAgents_QDeployAgentsSkippedOne_RunProceeds verifies that answering SkippedOne
// for QDeployAgents lets DeployAgents continue with an empty selection — the skip semantics
// are unchanged.
func TestDeployAgents_QDeployAgentsSkippedOne_RunProceeds(t *testing.T) {
	// Arrange — the default stub answer for an unscripted question is SkippedOne.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployAgentQuestions()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployAgents returned an error after SkippedOne at QDeployAgents: %v; "+
			"SkippedOne must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedOne QDeployAgents answer; " +
			"a SkippedOne answer must still let the run proceed to execution")
	}
}

// ---------------------------------------------------------------------------
// Non-regression — SkippedAll at a selection question still proceeds
// ---------------------------------------------------------------------------

// TestDeployNew_WorkflowsSkippedAll_RunProceeds verifies that answering SkippedAll for
// QWorkflows lets the run continue with an empty workflow selection.
func TestDeployNew_WorkflowsSkippedAll_RunProceeds(t *testing.T) {
	// Arrange — answer SkippedAll for workflows.
	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QWorkflows, "").
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after SkippedAll at QWorkflows: %v; "+
			"SkippedAll must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after a SkippedAll workflows answer; " +
			"a SkippedAll answer must still let the run proceed to execution")
	}
}

// ---------------------------------------------------------------------------
// Non-regression — Answered with zero IDs still proceeds
// ---------------------------------------------------------------------------

// TestDeployNew_WorkflowsAnsweredEmpty_RunProceeds verifies that an Answered answer with
// zero selected workflow IDs (the user confirmed with nothing checked) still proceeds —
// it is not a cancellation.
func TestDeployNew_WorkflowsAnsweredEmpty_RunProceeds(t *testing.T) {
	// Arrange — answer QWorkflows with an empty selection (Answered, no IDs).
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QWorkflows, "", []string{}).
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after Answered(empty) at QWorkflows: %v; "+
			"an explicit empty selection must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after an Answered(empty) workflows answer; " +
			"an explicit empty selection must still let the run proceed to execution")
	}
}

// TestDeployNew_UtilityAgentsAnsweredEmpty_RunProceeds verifies that an Answered answer
// with zero utility-agent IDs still proceeds.
func TestDeployNew_UtilityAgentsAnsweredEmpty_RunProceeds(t *testing.T) {
	// Arrange — answer QUtilityAgents with zero IDs.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogWithSelectionTestUtilityAgent()
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned an error after Answered(empty) at QUtilityAgents: %v; "+
			"an explicit empty selection must not become an abort (non-regression)", err)
	}
	if !spy.called {
		t.Error("Executor.Execute was not called after an Answered(empty) utility-agents answer; " +
			"an explicit empty selection must still let the run proceed to execution")
	}
}

// ---------------------------------------------------------------------------
// Plan review gate still reached on a non-cancelled run
// ---------------------------------------------------------------------------

// TestDeployNew_NoCancellation_PlanReviewIsReached verifies that when no selection question
// is cancelled, the plan review gate is still presented to the user before execution.
func TestDeployNew_NoCancellation_PlanReviewIsReached(t *testing.T) {
	// Arrange — script all selection questions with non-cancelled answers; script the review.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		HookIDs:                []string{},
		// AutoConfirmPlan is false so the Review call must be made through Interaction.
		AutoConfirmPlan: false,
	})

	// Assert — no error, executor reached, and the review question was asked.
	if err != nil {
		t.Errorf("DeployNew returned an unexpected error on a non-cancelled run: %v", err)
	}
	if !stub.WasAsked(domain.QPlanConfirm, "") {
		t.Error("QPlanConfirm (plan review) was not asked on a non-cancelled run; " +
			"the plan review gate must still be presented when no selection was cancelled")
	}
	if !spy.called {
		t.Error("Executor.Execute was not called on a non-cancelled run; " +
			"execution must proceed when the plan review gate is passed")
	}
}

// TestDeployAgents_NoCancellation_PlanReviewIsReached verifies the plan-review-gate
// invariant for the consolidated deploy-agents flow.
func TestDeployAgents_NoCancellation_PlanReviewIsReached(t *testing.T) {
	// Arrange — answer QDeployAgents with an empty selection; script the review gate.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	spy := &deploySpyExecutor{}
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployAgentQuestions()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		// AutoConfirmPlan is false so the Review call must be made through Interaction.
		AutoConfirmPlan: false,
	})

	// Assert
	if err != nil {
		t.Errorf("DeployAgents returned an unexpected error on a non-cancelled run: %v", err)
	}
	if !stub.WasAsked(domain.QPlanConfirm, "") {
		t.Error("QPlanConfirm (plan review) was not asked on a non-cancelled DeployAgents run; " +
			"the plan review gate must still be presented when no selection was cancelled")
	}
}
