package tui

// restart_test.go verifies that the root model restarts the selection flow when the app
// layer returns app.ErrPlanNotConfirmed, and continues to show the done/error screen for
// every other kind of error.
//
// The restart contract:
//   - screen resets to screenHarness (the first entry screen)
//   - errMsg is cleared (not set to the plan-not-confirmed message)
//   - activeQuestion and reviewOverlay are cleared (no stale overlay after restart)
//   - the model's context is NOT cancelled (the session continues)
//   - collected selections are cleared so the user re-enters them from scratch
//   - after restart the full selection flow is reachable again (screenRunning is reachable)
//
// The non-restart contract:
//   - any error other than app.ErrPlanNotConfirmed still produces screenDone with errMsg set

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newRestartModel creates a rootModel in screenRunning state, simulating the TUI state
// just before the service goroutine delivers its result. Uses newNavModel's single-harness
// setup so keyboard navigation tests can advance through all three entry screens.
func newRestartModel() *rootModel {
	svc := &stubNavService{harnesses: []domain.HarnessRef{navUsableHarness}}
	m := newRootModel(context.Background(), svc, Options{Theme: DefaultTheme()})
	m.screen = screenRunning
	return m
}

// advanceToRunning navigates a freshly created rootModel through all three entry screens
// (harness → mode → workspace) and returns the service command that would start execution.
// The model must have been created with newFlowModel so the workspace screen is pre-filled.
func advanceToRunning(m *rootModel) tea.Cmd {
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → workspace (deploy-new)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // workspace → running
	return cmd
}

// ---------------------------------------------------------------------------
// Restart on ErrPlanNotConfirmed
// ---------------------------------------------------------------------------

// TestRunError_PlanNotConfirmed_ResetsToHarnessScreen verifies the core restart behaviour:
// when runErrorMsg carries app.ErrPlanNotConfirmed, the root model must return to the first
// entry screen rather than showing the done/error state.
func TestRunError_PlanNotConfirmed_ResetsToHarnessScreen(t *testing.T) {
	// Arrange
	m := newRestartModel()

	// Act: deliver the plan-not-confirmed outcome from the service goroutine.
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert
	if m.screen != screenHarness {
		t.Errorf("screen = %v after ErrPlanNotConfirmed; want screenHarness — "+
			"a declined plan must restart the selection flow, not show the error state",
			m.screen)
	}
}

// TestRunError_PlanNotConfirmed_ClearsErrMsg verifies that the restart does not leave an
// error message behind. errMsg is shown on the screenDone error view; after a restart the
// model is on screenHarness, and showing a stale error message there would confuse the user.
func TestRunError_PlanNotConfirmed_ClearsErrMsg(t *testing.T) {
	// Arrange — prime errMsg to confirm the restart clears it.
	m := newRestartModel()
	m.errMsg = "leftover error from a prior run"

	// Act
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert
	if m.errMsg != "" {
		t.Errorf("errMsg = %q after ErrPlanNotConfirmed restart; want empty string — "+
			"the error message must be cleared when restarting to screenHarness", m.errMsg)
	}
}

// TestRunError_PlanNotConfirmed_ContextRemainsActive verifies that restarting does not cancel
// the model's context. The session continues in the same process; ctrl+c is the only operation
// that should cancel the context.
func TestRunError_PlanNotConfirmed_ContextRemainsActive(t *testing.T) {
	// Arrange
	m := newRestartModel()

	// Act
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert
	if m.ctx.Err() != nil {
		t.Errorf("ctx.Err() = %v after ErrPlanNotConfirmed restart; want nil — "+
			"the context must not be cancelled on a restart; only ctrl+c cancels the context",
			m.ctx.Err())
	}
}

// TestRunError_PlanNotConfirmed_ClearsSelections verifies that the restart clears the
// collected entry selections so the user is not locked into the previous harness, mode, and
// workspace choices. A clean restart means re-entering all selections from scratch.
func TestRunError_PlanNotConfirmed_ClearsSelections(t *testing.T) {
	// Arrange — populate selections to confirm they are cleared.
	m := newRestartModel()
	m.selections = entrySelections{
		harnessID:     "some-harness",
		mode:          domain.ModeDeployWorkspace,
		workspacePath: "/some/workspace",
	}

	// Act
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert: all selections must be cleared.
	if m.selections.harnessID != "" {
		t.Errorf("selections.harnessID = %q after restart; want empty string", m.selections.harnessID)
	}
	if m.selections.workspacePath != "" {
		t.Errorf("selections.workspacePath = %q after restart; want empty string", m.selections.workspacePath)
	}
}

// TestRunError_PlanNotConfirmed_ClearsAnyStaleReviewOverlay verifies that if a reviewOverlay
// is somehow still active when the restart is triggered, it is cleared. A stale overlay would
// be rendered over the harness screen, blocking the next run.
func TestRunError_PlanNotConfirmed_ClearsAnyStaleReviewOverlay(t *testing.T) {
	// Arrange: install a reviewOverlay to simulate a stale state.
	m := newRestartModel()
	qMsg := buildReviewMsg(domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/ws",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
	})
	m.Update(qMsg) // installs reviewOverlay and sets screen = screenQuestion

	// Act: deliver the not-confirmed error (bypasses the current screen handler).
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert
	if m.reviewOverlay != nil {
		t.Error("reviewOverlay is non-nil after ErrPlanNotConfirmed restart; " +
			"any stale overlay must be cleared so the harness screen renders correctly")
	}
}

// TestRunError_PlanNotConfirmed_ClearsAnyStaleActiveQuestion verifies that if activeQuestion
// is somehow still set when restart is triggered, it is cleared. Leaving it set would prevent
// the next run from being able to show its own question overlays.
func TestRunError_PlanNotConfirmed_ClearsAnyStaleActiveQuestion(t *testing.T) {
	// Arrange: install a reviewOverlay (which also sets activeQuestion).
	m := newRestartModel()
	qMsg := buildReviewMsg(domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/ws",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
	})
	m.Update(qMsg) // sets activeQuestion = &qMsg

	// Act
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	// Assert
	if m.activeQuestion != nil {
		t.Error("activeQuestion is non-nil after ErrPlanNotConfirmed restart; " +
			"any stale active-question reference must be cleared on restart")
	}
}

// ---------------------------------------------------------------------------
// Other errors keep today's done/error state
// ---------------------------------------------------------------------------

// TestRunError_OtherError_StillShowsDoneState verifies that a genuine error (not
// app.ErrPlanNotConfirmed) still transitions to screenDone, preserving existing behaviour.
// Only ErrPlanNotConfirmed must trigger a restart; all other errors must go to the error view.
func TestRunError_OtherError_StillShowsDoneState(t *testing.T) {
	// Arrange
	m := newRestartModel()
	someOtherErr := errors.New("executor failed: permission denied writing to workspace")

	// Act
	m.Update(runErrorMsg{err: someOtherErr})

	// Assert
	if m.screen != screenDone {
		t.Errorf("screen = %v after non-sentinel error; want screenDone — "+
			"only ErrPlanNotConfirmed triggers a restart; other errors must show the error view",
			m.screen)
	}
}

// TestRunError_OtherError_SetsErrMsg verifies that the errMsg field is populated from a
// genuine error so the user sees what went wrong on the done/error screen.
func TestRunError_OtherError_SetsErrMsg(t *testing.T) {
	// Arrange
	m := newRestartModel()
	someOtherErr := errors.New("executor failed: permission denied writing to workspace")

	// Act
	m.Update(runErrorMsg{err: someOtherErr})

	// Assert
	if m.errMsg == "" {
		t.Error("errMsg is empty after a non-sentinel error; " +
			"the error message must be set so the user sees why the run failed")
	}
}

// TestRunError_OtherError_DoesNotRestartToHarness verifies explicitly that a non-sentinel
// error does not return to screenHarness. There must be no accidental restart for real failures.
func TestRunError_OtherError_DoesNotRestartToHarness(t *testing.T) {
	// Arrange
	m := newRestartModel()

	// Act
	m.Update(runErrorMsg{err: errors.New("some other error")})

	// Assert
	if m.screen == screenHarness {
		t.Error("screen = screenHarness after a generic error; " +
			"the restart path must only activate for app.ErrPlanNotConfirmed")
	}
}

// ---------------------------------------------------------------------------
// Second run is reachable after restart
// ---------------------------------------------------------------------------

// TestRunError_PlanNotConfirmed_SecondRunCanReachRunning verifies that after a restart the
// user can navigate through all three entry screens again and reach screenRunning. This
// confirms the restart is genuinely re-runnable, not just visually resetting to screenHarness.
func TestRunError_PlanNotConfirmed_SecondRunCanReachRunning(t *testing.T) {
	// Arrange: use a flow service so screenRunning is reachable via the service command.
	workspace := t.TempDir()
	svc := newFlowSvc(workspace)
	m := newFlowModel(svc, workspace)

	// First run: navigate to screenRunning.
	cmd := advanceToRunning(m)
	if m.screen != screenRunning {
		t.Fatalf("precondition: screen = %v after first navigation; want screenRunning", m.screen)
	}
	// We don't need to execute cmd; just simulate the not-confirmed outcome arriving.
	_ = cmd

	// Simulate the not-confirmed outcome from the service.
	m.Update(runErrorMsg{err: app.ErrPlanNotConfirmed})

	if m.screen != screenHarness {
		t.Fatalf("screen = %v after ErrPlanNotConfirmed; want screenHarness before second run", m.screen)
	}

	// Second run: navigate again through all three entry screens.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	if m.screen != screenMode {
		t.Fatalf("screen = %v after harness Enter in second run; want screenMode", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // mode → workspace
	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after mode Enter in second run; want screenWorkspace", m.screen)
	}

	_, secondCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // workspace → running
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after workspace Enter in second run; want screenRunning", m.screen)
	}
	if secondCmd == nil {
		t.Error("cmd = nil after workspace Enter in second run; want a non-nil service command — " +
			"the restarted flow must produce a runnable service command, not a dead end")
	}
}
