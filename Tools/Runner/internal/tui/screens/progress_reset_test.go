package screens

// progress_reset_test.go verifies ProgressScreen.ResetStopState: the operation
// that returns a reused screen to a clean stop state so a resumed run presents
// a live stop affordance and carries no notice from the run that just ended.
//
// Two of the session-restart paths reuse the existing ProgressScreen instance
// rather than building a fresh one, so the screen's own latched stop state is
// separate state from the shared stop signal and has to be cleared explicitly.
// A carried-over latch makes the stop key inert on the resumed run, because the
// confirmation gate is only entered when no stop is already latched.
//
// RED: ResetStopState does not exist yet, so this file does not compile until
// it is added.

import (
	"strings"
	"testing"
)

// armedProgressScreen returns a screen with a confirmed graceful stop latched,
// matching the state a run leaves behind when the user confirms a stop.
func armedProgressScreen() *ProgressScreen {
	s := NewProgressScreen(80, 24, progressStyles())
	pressProgressKey(s, "s")
	pressProgressKey(s, "y")
	return s
}

// TestProgressScreen_ResetStopState_ClearsConfirmedStop asserts that a screen
// carrying a confirmed stop no longer reports one after the reset, so the
// resumed run is not treated as already stopping.
func TestProgressScreen_ResetStopState_ClearsConfirmedStop(t *testing.T) {
	s := armedProgressScreen()

	if !s.GracefulStop() {
		t.Fatal("precondition: GracefulStop() = false after 's' then 'y'; " +
			"the fixture must latch a confirmed stop before the reset is asserted")
	}

	s.ResetStopState()

	if s.GracefulStop() {
		t.Error("GracefulStop() = true after ResetStopState(); want false -- " +
			"a resumed run must not inherit the previous run's confirmed stop")
	}
}

// TestProgressScreen_ResetStopState_ClearsPendingConfirmation asserts that an
// unresolved confirmation gate is closed by the reset, so the resumed run does
// not open on a prompt the user never answered.
func TestProgressScreen_ResetStopState_ClearsPendingConfirmation(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	pressProgressKey(s, "s")

	if !s.ConfirmPending() {
		t.Fatal("precondition: ConfirmPending() = false after 's'; " +
			"the fixture must leave the gate pending before the reset is asserted")
	}

	s.ResetStopState()

	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after ResetStopState(); want false -- " +
			"an unresolved gate from the previous run must not carry forward")
	}
}

// TestProgressScreen_ResetStopState_RemovesStopNoticeFromView asserts that the
// persistent "stopping after the current step" notice disappears from the
// rendered screen. Left in place it asserts a stop that has just been
// cancelled, contradicting the notice's role as proof the request was accepted.
func TestProgressScreen_ResetStopState_RemovesStopNoticeFromView(t *testing.T) {
	s := armedProgressScreen()

	if !strings.Contains(s.View(), "Stopping after current step completes") {
		t.Fatal("precondition: stop notice absent from the view of an armed screen; " +
			"the fixture must render the notice before the reset is asserted")
	}

	s.ResetStopState()

	if strings.Contains(s.View(), "Stopping after current step completes") {
		t.Errorf("stop notice still rendered after ResetStopState(); "+
			"a resumed run must show no carried-over stop notice:\n%s", s.View())
	}
}

// TestProgressScreen_ResetStopState_ClearsStopAcknowledgement asserts that the
// status slot no longer carries the previous run's stop acknowledgement. Any
// status held at a restart boundary is stale, and this one in particular states
// that the run is stopping when it has just been resumed.
func TestProgressScreen_ResetStopState_ClearsStopAcknowledgement(t *testing.T) {
	s := armedProgressScreen()

	if !strings.Contains(s.View(), confirmAckText) {
		t.Fatal("precondition: confirmation acknowledgement absent from the view; " +
			"the fixture must set the status slot before the reset is asserted")
	}

	s.ResetStopState()

	if strings.Contains(s.View(), confirmAckText) {
		t.Errorf("stop acknowledgement still rendered after ResetStopState(); "+
			"the status slot must not carry the previous run's stop message:\n%s", s.View())
	}
}

// TestProgressScreen_ResetStopState_StopKeyReEntersGate asserts the behaviour
// the reset exists to restore: 's' opens the confirmation gate again. Without
// the reset the latched stop makes the key inert, silently removing the stop
// feature from the resumed run.
func TestProgressScreen_ResetStopState_StopKeyReEntersGate(t *testing.T) {
	s := armedProgressScreen()

	s.ResetStopState()
	pressProgressKey(s, "s")

	if !s.ConfirmPending() {
		t.Error("ConfirmPending() = false after 's' following ResetStopState(); want true -- " +
			"the stop key must be live again on a resumed run")
	}
}

// TestProgressScreen_ResetStopState_OnUnstoppedScreen_LeavesStopKeyLive
// asserts the reset is harmless on a screen that never latched a stop, since it
// is called unconditionally on every restart path regardless of what happened
// in the run that ended.
func TestProgressScreen_ResetStopState_OnUnstoppedScreen_LeavesStopKeyLive(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	s.ResetStopState()
	s.ResetStopState()
	pressProgressKey(s, "s")

	if !s.ConfirmPending() {
		t.Error("ConfirmPending() = false after 's' following repeated ResetStopState() " +
			"on a screen that never latched a stop; want true -- the reset must be harmless")
	}
}

// TestProgressScreen_ResetStopState_PreservesRows asserts the reset is confined
// to stop state. The row history is not stop state, and the two paths that
// reuse the screen have no other way to keep the steps already rendered.
func TestProgressScreen_ResetStopState_PreservesRows(t *testing.T) {
	s := armedProgressScreen()
	s.AppendRow(ProgressRow{AgentInstance: "planner-tdd-soft#1", Status: "SUCCESS"})

	s.ResetStopState()

	if !strings.Contains(s.View(), "planner-tdd-soft#1") {
		t.Errorf("row missing from the view after ResetStopState(); "+
			"the reset must clear stop state only, not the row history:\n%s", s.View())
	}
}
