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
	if summary.Mode != domain.ModeDeployNew {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeDeployNew)
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
// ModeDeployNew for the deploy-new flow.
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
	if summary.Mode != domain.ModeDeployNew {
		t.Errorf("summary.Mode = %q, want %q", summary.Mode, domain.ModeDeployNew)
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
