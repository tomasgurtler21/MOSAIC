package app_test

// plan_confirm_test.go verifies the distinguishable "plan not confirmed" outcome produced
// by the app layer when the user declines or cancels the deployment plan review.
//
// The sentinel app.ErrPlanNotConfirmed must be identifiable via errors.Is so the TUI can
// distinguish it from genuine failures and restart the selection flow rather than showing an
// error screen. All other errors (planner, executor, harness) must remain unchanged and must
// NOT satisfy errors.Is(err, app.ErrPlanNotConfirmed).
//
// The CLI path (AutoConfirmPlan semantics) must not be affected: when AutoConfirmPlan is true,
// a declining or cancelled review answer must not abort the run.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// DeployNew — declined plan returns ErrPlanNotConfirmed
// ---------------------------------------------------------------------------

// TestDeployNew_PlanDeclined_ReturnsErrPlanNotConfirmed verifies that when the user declines
// the plan review (Confirm=false, Status=Answered), DeployNew returns an error that satisfies
// errors.Is(err, app.ErrPlanNotConfirmed). The TUI relies on this to distinguish a voluntary
// cancellation from a genuine deployment failure.
func TestDeployNew_PlanDeclined_ReturnsErrPlanNotConfirmed(t *testing.T) {
	// Arrange: script the interaction stub to decline the plan review.
	stub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: false, // must honour the user's decline
	})

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after plan declined; expected ErrPlanNotConfirmed")
	}
	if !errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = false; "+
			"a declined plan must return the distinguishable sentinel so the TUI can restart; got: %v", err)
	}
}

// TestDeployNew_PlanCancelled_ReturnsErrPlanNotConfirmed verifies that a Cancelled status
// (e.g. the user presses Esc on the review screen) is also reported as ErrPlanNotConfirmed
// so the TUI treats cancellation and decline identically for restart purposes.
func TestDeployNew_PlanCancelled_ReturnsErrPlanNotConfirmed(t *testing.T) {
	// Arrange: interaction stub returns a Cancelled ConfirmAnswer for the review step.
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = &cancelReviewInteraction{}
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
		t.Fatal("DeployNew returned nil error after plan review cancelled; expected ErrPlanNotConfirmed")
	}
	if !errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = false for cancelled review; "+
			"cancellation and decline must both return the same sentinel; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeployNew — other failures are NOT ErrPlanNotConfirmed
// ---------------------------------------------------------------------------

// TestDeployNew_ExecutorError_IsNotErrPlanNotConfirmed verifies that a genuine executor
// failure is not reported as ErrPlanNotConfirmed. The two error kinds must stay distinct so
// the TUI can tell apart "user did not confirm" from "deployment actually failed".
func TestDeployNew_ExecutorError_IsNotErrPlanNotConfirmed(t *testing.T) {
	// Arrange: executor returns a real failure; review auto-confirms.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = &stubExecutor{err: errors.New("disk full: cannot write files")}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after executor failure; expected a non-nil error")
	}
	if errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = true for executor error; "+
			"genuine deployment failures must not masquerade as ErrPlanNotConfirmed; got: %v", err)
	}
}

// TestDeployNew_PlannerError_IsNotErrPlanNotConfirmed verifies that a planner failure is not
// reported as ErrPlanNotConfirmed.
func TestDeployNew_PlannerError_IsNotErrPlanNotConfirmed(t *testing.T) {
	// Arrange: planner returns an error; review would never be reached.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{err: errors.New("catalog read error: missing workflow manifest")}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Fatal("DeployNew returned nil error after planner failure; expected a non-nil error")
	}
	if errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = true for planner error; "+
			"planner failures must not be reported as ErrPlanNotConfirmed; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AutoConfirmPlan semantics unchanged (AC5.6)
// ---------------------------------------------------------------------------

// TestDeployNew_AutoConfirmTrue_DeclinedPlan_ProceedsToExecution verifies that when
// AutoConfirmPlan is true, a declining plan review does not abort the run — the executor
// must still be called. This preserves the headless / CLI contract: AutoConfirmPlan controls
// whether a declining answer aborts; the review is always shown but the answer is ignored
// when AutoConfirmPlan is true.
func TestDeployNew_AutoConfirmTrue_DeclinedPlan_ProceedsToExecution(t *testing.T) {
	// Arrange: plan is declined, but AutoConfirmPlan is true.
	spy := &deploySpyExecutor{}
	stub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true, // proceeds even though review declined
	})

	// Assert: run must succeed (executor was called, no error).
	if err != nil {
		t.Errorf("DeployNew returned error with AutoConfirmPlan=true after decline: %v; "+
			"AutoConfirmPlan must let the run proceed regardless of the review answer", err)
	}
	if !spy.called {
		t.Error("executor was not called with AutoConfirmPlan=true after decline; " +
			"AutoConfirmPlan must bypass the abort check so headless runs always execute")
	}
}

// TestDeployNew_AutoConfirmTrue_DeclinedPlan_DoesNotReturnErrPlanNotConfirmed verifies
// that ErrPlanNotConfirmed is never returned when AutoConfirmPlan is true.
func TestDeployNew_AutoConfirmTrue_DeclinedPlan_DoesNotReturnErrPlanNotConfirmed(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
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
	if errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Error("errors.Is(err, app.ErrPlanNotConfirmed) = true with AutoConfirmPlan=true; " +
			"when AutoConfirmPlan is true the run must proceed and not return the not-confirmed sentinel")
	}
}

// ---------------------------------------------------------------------------
// Update flow — same distinguishable outcome
// ---------------------------------------------------------------------------

// TestUpdate_PlanDeclined_ReturnsErrPlanNotConfirmed verifies that the Update flow produces
// the same distinguishable ErrPlanNotConfirmed sentinel when the user declines the plan
// review. Both flows must use the same sentinel for consistent TUI restart behaviour.
func TestUpdate_PlanDeclined_ReturnsErrPlanNotConfirmed(t *testing.T) {
	// Arrange: script the review to decline.
	stub := interactiontest.NewBuilder().
		AnswerConfirm(domain.QPlanConfirm, "", false).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: false,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error after plan declined; expected ErrPlanNotConfirmed")
	}
	if !errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = false for Update with declined plan; "+
			"Update must use the same sentinel as DeployNew so the TUI handles both flows identically; got: %v", err)
	}
}

// TestUpdate_PlanCancelled_ReturnsErrPlanNotConfirmed verifies that a Cancelled review
// status also returns ErrPlanNotConfirmed from the Update flow.
func TestUpdate_PlanCancelled_ReturnsErrPlanNotConfirmed(t *testing.T) {
	// Arrange
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = &cancelReviewInteraction{}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: false,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error after plan review cancelled; expected ErrPlanNotConfirmed")
	}
	if !errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Errorf("errors.Is(err, app.ErrPlanNotConfirmed) = false for Update cancelled review; "+
			"Update must report cancellation as ErrPlanNotConfirmed; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers — local to this file
// ---------------------------------------------------------------------------

// cancelReviewInteraction is a minimal domain.Interaction that returns a Cancelled answer
// for Review() and sensible defaults for everything else (skip / proceed). Used to simulate
// the user pressing Esc on the review overlay.
type cancelReviewInteraction struct{}

func (c *cancelReviewInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}
func (c *cancelReviewInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}
func (c *cancelReviewInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}
func (c *cancelReviewInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (c *cancelReviewInteraction) Notify(_ context.Context, n domain.Notice)          {}
func (c *cancelReviewInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *cancelReviewInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Cancelled}, nil
}

// deploySpyExecutor records whether Execute was called, returning a minimal success result.
// Used to assert that AutoConfirmPlan=true lets the run proceed past the review step.
type deploySpyExecutor struct {
	called bool
}

func (e *deploySpyExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.called = true
	return deploy.ExecResult{}, nil
}
