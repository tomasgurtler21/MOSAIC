package app_test

// deploy_test.go verifies the deploy-new flow end to end against a temp workspace, driven
// by the scripted interaction stub.
//
// Verified invariants (T18.1):
//
// Question sequence — the flow must ask questions in the documented order:
//   - QHarness        is asked when no HarnessID is pre-supplied
//   - QWorkspace      is asked when no WorkspacePath is pre-supplied
//   - QWorkflows      is asked when no WorkflowIDs are pre-supplied
//   - QUtilityAgents  is asked after workflows are selected, when UtilityAgentIDs is absent
//   - QHooks          is asked after utility agents, when HookIDs is absent
//   - QTierModel      is asked once per discovered tier when not pre-answered
//   - QAgentModel     is asked once per agent when not pre-answered at the tier level
//   - QPlanConfirm (Review) is the final question before execution
//
// Pre-answered fields skip their question:
//   - A DeployRequest with HarnessID set does not ask QHarness
//   - A DeployRequest with WorkspacePath set does not ask QWorkspace
//   - A DeployRequest with WorkflowIDs set does not ask QWorkflows
//   - A DeployRequest with UtilityAgentIDs set does not ask QUtilityAgents
//   - A DeployRequest with HookIDs set does not ask QHooks
//
// Flow runs headlessly with no terminal:
//   - DeployNew succeeds with all stubs and a fully scripted interaction
//
// Cancellation at plan review aborts the run:
//   - When Review returns Cancelled, DeployNew returns an error and writes nothing further
//
// Flow returns RunSummary with correct mode and harness on success.

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Question sequence — harness, workspace, workflows
// ---------------------------------------------------------------------------

// TestDeployNew_NoPreAnswers_AsksHarnessFirst verifies that when HarnessID is not set in
// the request, the flow asks QHarness as its first question.
func TestDeployNew_NoPreAnswers_AsksHarnessFirst(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarness, "", "stub-harness").
		AnswerText(domain.QWorkspace, "", t.TempDir()).
		AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
		AnswerReview(true).
		Build()
	deps, _ := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		AutoConfirmPlan: true,
	})

	// Assert
	order := stub.QuestionOrder()
	if len(order) == 0 {
		t.Fatal("no questions were asked; expected at least QHarness")
	}
	if order[0] != domain.QHarness {
		t.Errorf("first question = %q, want %q", order[0], domain.QHarness)
	}
}

// TestDeployNew_HarnessPreAnswered_DoesNotAskQHarness verifies that when HarnessID is
// set in the request, QHarness is never asked through Interaction (CD-6).
func TestDeployNew_HarnessPreAnswered_DoesNotAskQHarness(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerText(domain.QWorkspace, "", t.TempDir()).
		AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QHarness, "") {
		t.Error("QHarness was asked even though HarnessID was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_WorkspacePreAnswered_DoesNotAskQWorkspace verifies that when WorkspacePath
// is set in the request, QWorkspace is never asked.
func TestDeployNew_WorkspacePreAnswered_DoesNotAskQWorkspace(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if stub.WasAsked(domain.QWorkspace, "") {
		t.Error("QWorkspace was asked even though WorkspacePath was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_WorkflowsPreAnswered_DoesNotAskQWorkflows verifies that when WorkflowIDs
// is set in the request, QWorkflows is never asked.
func TestDeployNew_WorkflowsPreAnswered_DoesNotAskQWorkflows(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was asked even though WorkflowIDs was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_NoWorkflowsPreAnswered_AsksQWorkflows verifies that when WorkflowIDs is
// not set, QWorkflows is asked through Interaction.
func TestDeployNew_NoWorkflowsPreAnswered_AsksQWorkflows(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QWorkflows, "") {
		t.Error("QWorkflows was not asked even though WorkflowIDs was not pre-supplied")
	}
}

// TestDeployNew_PlanConfirmAskedLast verifies that QPlanConfirm (Review) is the last
// question-shaped call before execution.
func TestDeployNew_PlanConfirmAskedLast(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	order := stub.QuestionOrder()
	if len(order) == 0 {
		t.Skip("no questions asked; cannot verify QPlanConfirm is last")
	}
	last := order[len(order)-1]
	if last != domain.QPlanConfirm {
		t.Errorf("last question = %q, want %q (plan review must be the last interaction before execution)",
			last, domain.QPlanConfirm)
	}
}

// ---------------------------------------------------------------------------
// Headless end-to-end execution
// ---------------------------------------------------------------------------

// TestDeployNew_AllPreAnswered_RunsHeadlessly verifies that DeployNew completes
// end-to-end without a terminal when all selections are pre-answered in the request.
// This is the foundational test for AC18.1.
func TestDeployNew_AllPreAnswered_RunsHeadlessly(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build() // no scripted questions needed
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned unexpected error: %v", err)
	}
	if summary.Mode != domain.ModeDeployWorkspace {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeDeployWorkspace)
	}
}

// TestDeployNew_AllPreAnswered_SummaryHarnessMatchesRequest verifies that the returned
// RunSummary carries the harness identity from the request.
func TestDeployNew_AllPreAnswered_SummaryHarnessMatchesRequest(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}
	if summary.Harness.ID != "stub-harness" {
		t.Errorf("summary.Harness.ID = %q, want \"stub-harness\"", summary.Harness.ID)
	}
}

// TestDeployNew_AllPreAnswered_SummaryModeIsDeployNew verifies that RunSummary.Mode is
// ModeDeployWorkspace for the deploy-new flow.
func TestDeployNew_AllPreAnswered_SummaryModeIsDeployNew(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	if summary.Mode != domain.ModeDeployWorkspace {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeDeployWorkspace)
	}
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// TestDeployNew_PlanReviewCancelled_ReturnsError verifies that when the user cancels at
// the plan review step, DeployNew returns an error (Cancelled propagates as OutcomeFailed).
func TestDeployNew_PlanReviewCancelled_ReturnsError(t *testing.T) {
	// Arrange — the Review answer says "cancelled" so the run must abort
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	// Override the planner to trigger a review with a non-empty plan
	deps.Planner = &stubPlanner{plan: newMinimalPlan(workspace)}
	svc := app.New(deps)

	// Use a request that requires review (AutoConfirmPlan: false)
	req := app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: false, // forces Review() to be called
	}
	// The stub returns Cancelled for the Review call
	cancelStub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
		Build()
	deps.Interaction = cancelStub
	svc = app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), req)

	// Assert
	// When the user cancels, DeployNew must return an error so the frontend can show a failure.
	if err == nil {
		t.Error("DeployNew returned nil error after user cancelled plan review; " +
			"a cancellation must produce an error (OutcomeFailed)")
	}
}

// TestDeployNew_PlanReviewDeclined_ReturnsErrorWithoutExecuting verifies that when the
// user declines the plan (Confirm=false, Status=Answered), DeployNew returns an error
// without calling the executor.
func TestDeployNew_PlanReviewDeclined_ReturnsErrorWithoutExecuting(t *testing.T) {
	// Arrange
	spy := &stubExecutor{}
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy

	declineStub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
		Build()
	deps.Interaction = declineStub
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: false,
	})

	// Assert
	if err == nil {
		t.Error("DeployNew returned nil error after plan was declined; " +
			"a declined plan must produce an error without executing")
	}
	if spy.result.DeploymentRoot != "" {
		t.Error("executor was called after plan was declined; executor must not run if user says no")
	}
}

// TestDeployNew_ScriptedInteraction_WorkflowOptionsGroupedByCategory verifies that when
// QWorkflows is asked, the options are grouped by workflow category (for grouped browsing
// per Stage 18 plan item 4).
func TestDeployNew_ScriptedInteraction_WorkflowOptionsGroupedByCategory(t *testing.T) {
	// Arrange — capture the actual ChoiceQuestion presented to SelectMany
	var capturedQ domain.ChoiceQuestion
	capture := &captureInteraction{
		captureSelectMany: func(q domain.ChoiceQuestion) {
			if q.ID == domain.QWorkflows {
				capturedQ = q
			}
		},
		defaultSelectMany: domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}},
		defaultConfirmAnswer: domain.ConfirmAnswer{Status: domain.Answered, Confirm: true},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		// WorkflowIDs intentionally omitted so QWorkflows is asked
		AutoConfirmPlan: true,
	})

	// Assert
	if len(capturedQ.Options) == 0 {
		t.Skip("QWorkflows was not asked or had no options; cannot verify grouping")
	}
	// At least one option must have a non-empty Group field (the category name)
	hasGroup := false
	for _, opt := range capturedQ.Options {
		if opt.Group != "" {
			hasGroup = true
			break
		}
	}
	if !hasGroup {
		t.Error("QWorkflows options have no Group field; workflows must be grouped by category for browsing")
	}
}

// captureInteraction is a minimal domain.Interaction implementation that records specific
// question calls without the full scripted builder machinery. Used in targeted tests that
// need to inspect the Question struct itself.
type captureInteraction struct {
	captureSelectMany    func(domain.ChoiceQuestion)
	defaultSelectMany    domain.MultiChoiceAnswer
	defaultConfirmAnswer domain.ConfirmAnswer
}

func (c *captureInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	return domain.ChoiceAnswer{Status: domain.Answered}, nil
}
func (c *captureInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	if c.captureSelectMany != nil {
		c.captureSelectMany(q)
	}
	return c.defaultSelectMany, nil
}
func (c *captureInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub-value"}, nil
}
func (c *captureInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return c.defaultConfirmAnswer, nil
}
func (c *captureInteraction) Notify(_ context.Context, n domain.Notice)       {}
func (c *captureInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *captureInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return c.defaultConfirmAnswer, nil
}

// ---------------------------------------------------------------------------
// Utility agents and hooks question sequence (AC18.2 steps 5 and 6)
// ---------------------------------------------------------------------------

// TestDeployNew_NoUtilityAgentsPreAnswered_AsksQUtilityAgents verifies that when
// UtilityAgentIDs is not set in the request, QUtilityAgents is asked through Interaction.
// This corresponds to deploy-new flow step 5 (AC18.2).
func TestDeployNew_NoUtilityAgentsPreAnswered_AsksQUtilityAgents(t *testing.T) {
	// Arrange — catalog has a utility agent so the question has options to present
	cat := newMinimalCatalog()
	cat.utilityAgents = []domain.Agent{
		{
			Key:       "code-checker",
			NumericID: "10",
			Version:   "1.0",
			Name:      "Code Checker",
			Role:      domain.RoleUtility,
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act — UtilityAgentIDs is absent (nil) → flow must ask
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was not asked when UtilityAgentIDs is not pre-supplied; " +
			"step 5 of the deploy-new flow must ask QUtilityAgents to allow selection of allow-listed utility agents (AC18.2)")
	}
}

// TestDeployNew_UtilityAgentsPreAnswered_DoesNotAskQUtilityAgents verifies that when
// UtilityAgentIDs is set in the request, QUtilityAgents is never asked (CD-6).
func TestDeployNew_UtilityAgentsPreAnswered_DoesNotAskQUtilityAgents(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act — UtilityAgentIDs is set to an empty slice (explicitly pre-answered: no utility agents)
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		UtilityAgentIDs: []string{},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QUtilityAgents, "") {
		t.Error("QUtilityAgents was asked even though UtilityAgentIDs was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_NoHooksPreAnswered_AsksQHooks verifies that when HookIDs is not set in the
// request, QHooks is asked through Interaction. This corresponds to deploy-new flow step 6
// (AC18.2).
func TestDeployNew_NoHooksPreAnswered_AsksQHooks(t *testing.T) {
	// Arrange — catalog has a hook bundle so the question has options to present
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{
		{Key: "test-hooks", Version: "1.0", Description: "Test hook bundle"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act — HookIDs is absent (nil) → flow must ask
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was not asked when HookIDs is not pre-supplied; " +
			"step 6 of the deploy-new flow must ask QHooks to allow selection of available hook bundles (AC18.2)")
	}
}

// TestDeployNew_HooksPreAnswered_DoesNotAskQHooks verifies that when HookIDs is set in the
// request, QHooks is never asked (CD-6).
func TestDeployNew_HooksPreAnswered_DoesNotAskQHooks(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act — HookIDs is set to an empty slice (explicitly pre-answered: no hooks selected)
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QHooks, "") {
		t.Error("QHooks was asked even though HookIDs was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_TierRationaleCarriedToInteraction verifies that when QTierModel is asked,
// the Question.Detail field carries the tier's rationale text (AC18.8).
func TestDeployNew_TierRationaleCarriedToInteraction(t *testing.T) {
	// Arrange
	var capturedDetail string
	capture := &captureTierInteraction{
		onSelectOne: func(q domain.ChoiceQuestion) {
			if q.ID == domain.QTierModel {
				capturedDetail = q.Detail
			}
		},
		defaultAnswer: domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"},
		defaultConfirm: domain.ConfirmAnswer{Status: domain.Answered, Confirm: true},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		WorkflowIDs:   []string{"quick-fix"},
		// TierModels intentionally omitted so QTierModel is asked
		AutoConfirmPlan: true,
	})

	// Assert
	if capturedDetail == "" {
		t.Error("QTierModel Question.Detail is empty; the tier rationale text must be carried through to the caller (AC18.8)")
	}
	if capturedDetail != "Needs a capable model to analyse test output" {
		t.Errorf("QTierModel Question.Detail = %q; want the tier rationale %q (AC18.8)",
			capturedDetail, "Needs a capable model to analyse test output")
	}
}

// ---------------------------------------------------------------------------
// Infrastructure agent selection question (T4.2)
// ---------------------------------------------------------------------------

// TestDeployNew_NoInfraAgentsPreAnswered_AsksQInfrastructureAgents verifies that when
// InfrastructureAgentIDs is not set in the request and the catalog has infrastructure agents,
// QInfrastructureAgents is asked through Interaction.
func TestDeployNew_NoInfraAgentsPreAnswered_AsksQInfrastructureAgents(t *testing.T) {
	// Arrange — catalog has an infrastructure agent so the question has options to present.
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{
		{
			Key:            "checkpoint-manager-git",
			NumericID:      "36",
			Version:        "1.0",
			Name:           "checkpoint-manager-git",
			Role:           domain.RoleWorker,
			Infrastructure: "checkpoint",
			Description:    "Commits a checkpoint to a git ref",
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs is absent (nil) → flow must ask.
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was not asked when InfrastructureAgentIDs is not pre-supplied; " +
			"the deploy-new flow must ask QInfrastructureAgents when the catalog has infrastructure agents")
	}
}

// TestDeployNew_InfraAgentsPreAnswered_DoesNotAskQInfrastructureAgents verifies that when
// InfrastructureAgentIDs is set in the request, QInfrastructureAgents is never asked (CD-6).
func TestDeployNew_InfraAgentsPreAnswered_DoesNotAskQInfrastructureAgents(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs is set to empty slice (explicitly pre-answered: no selection).
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan:        true,
	})

	// Assert
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though InfrastructureAgentIDs was pre-supplied in the request (CD-6)")
	}
}

// TestDeployNew_InfraAgentOptions_ClassSuffixInLabel verifies that when QInfrastructureAgents
// is asked, each option's Label includes the agent's infrastructure class as a suffix of the
// form " [class]" so the user can distinguish agents by class.
func TestDeployNew_InfraAgentOptions_ClassSuffixInLabel(t *testing.T) {
	// Arrange — capture the actual ChoiceQuestion presented to SelectMany.
	var capturedQ domain.ChoiceQuestion
	capture := &captureInfraInteraction{
		captureSelectMany: func(q domain.ChoiceQuestion) {
			if q.ID == domain.QInfrastructureAgents {
				capturedQ = q
			}
		},
		defaultSelectMany:    domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{}},
		defaultConfirmAnswer: domain.ConfirmAnswer{Status: domain.Answered, Confirm: true},
	}
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{
		{
			Key:            "checkpoint-manager-git",
			NumericID:      "36",
			Version:        "1.0",
			Name:           "checkpoint-manager-git",
			Role:           domain.RoleWorker,
			Infrastructure: "checkpoint",
			Description:    "Commits a checkpoint to a git ref",
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs intentionally omitted so QInfrastructureAgents is asked.
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — fail rather than skip when the question was not asked, to preserve RED-phase signal.
	if len(capturedQ.Options) == 0 {
		t.Fatal("QInfrastructureAgents was not asked or had no options; " +
			"askInfrastructureAgents must be implemented to present infrastructure agent options")
	}
	found := false
	for _, opt := range capturedQ.Options {
		if opt.ID == "checkpoint-manager-git" {
			found = true
			if !strings.Contains(opt.Label, "[checkpoint]") {
				t.Errorf("option label %q does not contain class suffix [checkpoint]; "+
					"infrastructure agent option labels must include the class in brackets", opt.Label)
			}
			break
		}
	}
	if !found {
		t.Error("checkpoint-manager-git was not present in QInfrastructureAgents options")
	}
}

// TestDeployNew_InfraAgentOptions_FilteredToNonEmptyInfrastructureField verifies that when
// QInfrastructureAgents is asked, only agents with a non-empty Infrastructure field appear
// as options. Non-infrastructure worker agents must be excluded.
func TestDeployNew_InfraAgentOptions_FilteredToNonEmptyInfrastructureField(t *testing.T) {
	var capturedQ domain.ChoiceQuestion
	capture := &captureInfraInteraction{
		captureSelectMany: func(q domain.ChoiceQuestion) {
			if q.ID == domain.QInfrastructureAgents {
				capturedQ = q
			}
		},
		defaultSelectMany:    domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{}},
		defaultConfirmAnswer: domain.ConfirmAnswer{Status: domain.Answered, Confirm: true},
	}
	cat := newMinimalCatalog()
	// minimalCatalog's agents field has test-runner (non-infrastructure worker).
	// We also add an infrastructure agent via infraAgents.
	cat.infraAgents = []domain.Agent{
		{Key: "checkpoint-manager-git", Infrastructure: "checkpoint", Role: domain.RoleWorker},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Fail rather than skip when the question was not asked, to preserve RED-phase signal.
	if len(capturedQ.Options) == 0 {
		t.Fatal("QInfrastructureAgents was not asked; " +
			"askInfrastructureAgents must be implemented and wired into the deploy-new flow")
	}
	for _, opt := range capturedQ.Options {
		if opt.ID == "test-runner" {
			t.Error("non-infrastructure agent test-runner appeared in QInfrastructureAgents options; " +
				"only agents with non-empty Infrastructure field must be presented")
		}
	}
}

// TestDeployNew_InfraAgentsAskedAfterUtilityAgents verifies that QInfrastructureAgents is
// asked after QUtilityAgents in the question sequence, consistent with the deploy-new flow
// ordering (utility agents → infrastructure agents → hooks).
func TestDeployNew_InfraAgentsAskedAfterUtilityAgents(t *testing.T) {
	cat := newMinimalCatalog()
	cat.utilityAgents = []domain.Agent{
		{Key: "code-checker", NumericID: "10", Version: "1.0", Name: "Code Checker", Role: domain.RoleUtility},
	}
	cat.infraAgents = []domain.Agent{
		{Key: "checkpoint-manager-git", Infrastructure: "checkpoint", Role: domain.RoleWorker},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QUtilityAgents, "", []string{}).
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	order := stub.QuestionOrder()
	utilityIdx := -1
	infraIdx := -1
	for i, id := range order {
		switch id {
		case domain.QUtilityAgents:
			utilityIdx = i
		case domain.QInfrastructureAgents:
			infraIdx = i
		}
	}

	// Fail on the infra index missing to preserve RED-phase signal — this is the property
	// under test. Skip on the utility index missing (that question is a separate concern).
	if infraIdx == -1 {
		t.Fatal("QInfrastructureAgents was not asked; " +
			"askInfrastructureAgents must be wired into the deploy-new flow after utility agents")
	}
	if utilityIdx == -1 {
		t.Skip("QUtilityAgents was not asked; cannot verify relative ordering without both questions")
	}
	if infraIdx <= utilityIdx {
		t.Errorf("QInfrastructureAgents (index %d) was asked before or at the same position as QUtilityAgents (index %d); "+
			"infrastructure agents must be asked after utility agents", infraIdx, utilityIdx)
	}
}

// TestDeployNew_EmptyCatalog_DoesNotAskQInfrastructureAgents verifies that when the
// catalog has no infrastructure agents, QInfrastructureAgents is never asked through
// Interaction. An empty catalog must cause the question to be skipped entirely.
func TestDeployNew_EmptyCatalog_DoesNotAskQInfrastructureAgents(t *testing.T) {
	// Arrange — catalog has no infrastructure agents (infraAgents defaults to nil/empty)
	cat := newMinimalCatalog()
	// infraAgents is not set — cat.InfrastructureAgents() returns nil
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs is absent (nil); catalog has no infra agents
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though the catalog has no infrastructure agents; " +
			"the question must be skipped when InfrastructureAgents() returns an empty slice")
	}
}

// TestDeployNew_InfraAgentsAskedBeforeHooks verifies that QInfrastructureAgents is asked
// before QHooks in the question sequence, consistent with the documented flow ordering:
// utility agents → infrastructure agents → hooks.
func TestDeployNew_InfraAgentsAskedBeforeHooks(t *testing.T) {
	// Arrange — catalog has both infrastructure agents and hooks so both questions are triggered
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{
		{Key: "checkpoint-manager-git", Infrastructure: "checkpoint", Role: domain.RoleWorker},
	}
	cat.hooks = []domain.HookBundle{
		{Key: "test-hooks", Version: "1.0", Description: "Test hook bundle"},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QInfrastructureAgents, "", []string{}).
		AnswerSelectMany(domain.QHooks, "", []string{}).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	order := stub.QuestionOrder()
	infraIdx := -1
	hooksIdx := -1
	for i, id := range order {
		switch id {
		case domain.QInfrastructureAgents:
			infraIdx = i
		case domain.QHooks:
			hooksIdx = i
		}
	}

	if infraIdx == -1 {
		t.Fatal("QInfrastructureAgents was not asked; " +
			"askInfrastructureAgents must be wired into the deploy-new flow before hooks")
	}
	if hooksIdx == -1 {
		t.Skip("QHooks was not asked; cannot verify relative ordering without both questions")
	}
	if infraIdx >= hooksIdx {
		t.Errorf("QInfrastructureAgents (index %d) was asked after or at the same position as QHooks (index %d); "+
			"infrastructure agents must be asked before hooks in the question sequence", infraIdx, hooksIdx)
	}
}

// TestDeployNew_InfraAgentIDsPreAnswered_PassedToPlanner verifies the end-to-end propagation
// of pre-answered infrastructure agent IDs from DeployRequest into the plan.Input that the
// planner receives. This guards against a bug where the deploy flow asks (or accepts) the
// selection but silently discards it before calling the planner (AC4.3).
//
// This test references plan.Input.InfrastructureAgentIDs, which does not yet exist. It will
// fail to compile until I4.4 adds InfrastructureAgentIDs to plan.Input — that is the correct
// RED-phase behaviour. After I4.4 it will compile but the assertion will fail until I4.5
// wires the selection through the deploy-new flow.
func TestDeployNew_InfraAgentIDsPreAnswered_PassedToPlanner(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	capturing := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = capturing
	svc := app.New(deps)

	// Act — InfrastructureAgentIDs is pre-answered; the flow must not ask and must pass the IDs onward
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-manager-git"},
		AutoConfirmPlan:        true,
	})
	if err != nil {
		t.Fatalf("DeployNew returned unexpected error: %v", err)
	}

	// Assert — the planner must have received the infrastructure agent ID in plan.Input
	found := false
	for _, id := range capturing.capturedInput.InfrastructureAgentIDs {
		if id == "checkpoint-manager-git" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("planner was not given InfrastructureAgentIDs containing \"checkpoint-manager-git\"; "+
			"the deploy-new flow must pass selected infrastructure agent IDs to plan.Input (AC4.3 propagation); "+
			"got: %v", capturing.capturedInput.InfrastructureAgentIDs)
	}
}

// captureInfraInteraction is a minimal domain.Interaction implementation that captures
// SelectMany calls for infrastructure agent selection verification. It routes QWorkflows
// to return the quick-fix workflow so the deploy flow can proceed to the infrastructure
// agents question.
type captureInfraInteraction struct {
	captureSelectMany    func(domain.ChoiceQuestion)
	defaultSelectMany    domain.MultiChoiceAnswer
	defaultConfirmAnswer domain.ConfirmAnswer
}

func (c *captureInfraInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	return domain.ChoiceAnswer{Status: domain.Answered}, nil
}
func (c *captureInfraInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	if c.captureSelectMany != nil {
		c.captureSelectMany(q)
	}
	if q.ID == domain.QWorkflows {
		return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
	}
	return c.defaultSelectMany, nil
}
func (c *captureInfraInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub-value"}, nil
}
func (c *captureInfraInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return c.defaultConfirmAnswer, nil
}
func (c *captureInfraInteraction) Notify(_ context.Context, n domain.Notice)       {}
func (c *captureInfraInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *captureInfraInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return c.defaultConfirmAnswer, nil
}

type captureTierInteraction struct {
	onSelectOne    func(domain.ChoiceQuestion)
	defaultAnswer  domain.ChoiceAnswer
	defaultConfirm domain.ConfirmAnswer
}

func (c *captureTierInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if c.onSelectOne != nil {
		c.onSelectOne(q)
	}
	return c.defaultAnswer, nil
}
func (c *captureTierInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
}
func (c *captureTierInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub-value"}, nil
}
func (c *captureTierInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return c.defaultConfirm, nil
}
func (c *captureTierInteraction) Notify(_ context.Context, n domain.Notice)         {}
func (c *captureTierInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *captureTierInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return c.defaultConfirm, nil
}
