package tui

// exec_override_routing_test.go verifies routing from a launch failure
// (via runErrorMsg or runDoneMsg.outcome.Cause) to screenExecOverride, and the
// regression-guarded non-launch-failure paths that must keep routing to
// screenDone or screenStop instead.
//
// T6.3 (routing): all routing-to-screenExecOverride tests are RED because the
//   Update handler's runErrorMsg and runDoneMsg branches do not perform
//   errors.As checks.

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ShowsOverrideScreen verifies
// that a runErrorMsg carrying a *domain.HarnessLaunchError routes the TUI to
// screenExecOverride, not to the existing done screen.
//
// RED: the runErrorMsg handler currently routes all errors to screenDone without
// checking errors.As for *domain.HarnessLaunchError.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runErrorMsg with *domain.HarnessLaunchError, "+
			"want screenExecOverride (%v); "+
			"a launch failure must show the override screen so the user can supply a working path",
			m.screen, screenExecOverride)
	}
	if m.execOverrideScreen == nil {
		t.Error("execOverrideScreen = nil; must be constructed when a launch failure is detected")
	}
}

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsHarnessID verifies
// that the override screen's view includes the harness identifier extracted from
// the *domain.HarnessLaunchError.
//
// RED: routing not wired; precondition (screen == screenExecOverride) fails.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsHarnessID(t *testing.T) {
	const harnessID = "ghcp-cli"
	m := newOverrideTestModel()
	le := launchFailureErr(harnessID, "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}
	if !containsStr(m.View(), harnessID) {
		t.Errorf("override screen view does not contain harness ID %q:\n%s", harnessID, m.View())
	}
}

// TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsAttemptedPath verifies
// that the override screen includes the executable path that was tried.
//
// RED: routing not wired.
func TestExecOverrideRouting_RunErrorMsg_LaunchFailure_ScreenShowsAttemptedPath(t *testing.T) {
	const exe = "/usr/bin/copilot"
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", exe)
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}
	if !containsStr(m.View(), exe) {
		t.Errorf("override screen view does not contain attempted path %q:\n%s", exe, m.View())
	}
}

// TestExecOverrideRouting_RunDoneMsg_LaunchFailureCause_ShowsOverrideScreen
// verifies that a runDoneMsg whose outcome.Cause is a *domain.HarnessLaunchError
// routes to screenExecOverride, covering the session-refusal path.
//
// RED: the runDoneMsg handler does not yet check outcome.Cause.
func TestExecOverrideRouting_RunDoneMsg_LaunchFailureCause_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "pre-consultation failed: " + le.Error(),
		Cause:   le,
	}
	m.Update(runDoneMsg{outcome: outcome})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runDoneMsg with launch-failure Cause, "+
			"want screenExecOverride (%v); "+
			"a refusal caused by a launch failure must show the override screen, "+
			"not the done screen — the user may be able to fix it",
			m.screen, screenExecOverride)
	}
}

// TestExecOverrideRouting_RunErrorMsg_NonZeroExit_ShowsDoneScreen verifies
// that a non-zero exit error routes to the existing done screen.
// Regression guard (GREEN today; must stay GREEN after routing is wired).
func TestExecOverrideRouting_RunErrorMsg_NonZeroExit_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runErrorMsg{err: fmt.Errorf("process failed: exit status 1")})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after non-zero exit; " +
			"want screenDone — only launch failures may show the override screen")
	}
	if m.screen != screenDone {
		t.Errorf("screen = %v after non-zero exit, want screenDone (%v)", m.screen, screenDone)
	}
}

// TestExecOverrideRouting_RunDoneMsg_RefusedNoCause_ShowsDoneScreen verifies
// that RunRefused without a launch-failure Cause routes to the existing done
// screen. Regression guard (GREEN today).
func TestExecOverrideRouting_RunDoneMsg_RefusedNoCause_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: "workflow file not found",
		Cause:   nil,
	}})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after RunRefused with nil Cause; " +
			"want screenDone — only a launch-failure cause redirects to the override screen")
	}
	if m.screen != screenDone {
		t.Errorf("screen = %v after RunRefused without launch-failure cause, want screenDone (%v)",
			m.screen, screenDone)
	}
}

// TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_WithLaunchFailureCause_ShowsOverrideScreen
// verifies that a runDoneMsg with RunStoppedByConsultant status and a
// *domain.HarnessLaunchError in outcome.Cause routes to screenExecOverride,
// not to screenStop. This covers the routing-table row distinct from RunRefused:
// the consultant may stop a run because the harness failed to launch, and both
// outcome statuses must be checked for a launch-failure Cause.
//
// RED: the runDoneMsg handler does not yet check outcome.Cause for
// *domain.HarnessLaunchError on the RunStoppedByConsultant path.
func TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_WithLaunchFailureCause_ShowsOverrideScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	outcome := domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "pre-consultation failed: launch failure",
		Cause:      le,
	}
	m.Update(runDoneMsg{outcome: outcome})

	if m.screen != screenExecOverride {
		t.Errorf("screen = %v after runDoneMsg with RunStoppedByConsultant and launch-failure Cause, "+
			"want screenExecOverride (%v); "+
			"a consultant stop caused by a launch failure must show the override screen, "+
			"not the stop-recovery screen — the user needs a path to fix the executable",
			m.screen, screenExecOverride)
	}
}

// TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_NoCause_ShowsStopScreen
// verifies that RunStoppedByConsultant without a launch-failure Cause still
// routes to screenStop. Regression guard (GREEN today).
func TestExecOverrideRouting_RunDoneMsg_StoppedByConsultant_NoCause_ShowsStopScreen(t *testing.T) {
	m := newOverrideTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{
		Status:     domain.RunStoppedByConsultant,
		StopReason: "quota exceeded",
		Cause:      nil,
	}})

	if m.screen == screenExecOverride {
		t.Error("screen = screenExecOverride after RunStoppedByConsultant with nil Cause; " +
			"want screenStop — a consultant stop without launch failure uses the stop recovery screen")
	}
	if m.screen != screenStop {
		t.Errorf("screen = %v after RunStoppedByConsultant without cause, want screenStop (%v)",
			m.screen, screenStop)
	}
}

// TestExecOverrideRouting_EscOnOverrideScreen_ShowsDoneScreen verifies that
// pressing Esc on the override screen transitions to screenDone so the user
// sees the normal error summary. This differs from StopScreen's Esc (which
// quits) because a launch failure is worth showing.
//
// RED: routing to screenExecOverride is not wired; precondition fails.
func TestExecOverrideRouting_EscOnOverrideScreen_ShowsDoneScreen(t *testing.T) {
	m := newOverrideTestModel()
	le := launchFailureErr("ghcp-cli", "/usr/bin/copilot")
	m.Update(runErrorMsg{err: le})

	if m.screen != screenExecOverride {
		t.Fatalf("precondition: screen = %v, want screenExecOverride", m.screen)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.screen != screenDone {
		t.Errorf("screen = %v after Esc on override screen, want screenDone (%v); "+
			"abandoning the override must show the normal error summary, not quit abruptly",
			m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Error("doneScreen = nil after Esc on override screen; must be constructed on abandon")
	}
}
