package screens

// progress_confirm_test.go verifies ProgressScreen's stop-confirmation state
// machine (ConfirmPending / GracefulStop), which replaces the prior one-shot
// 's' key -> immediate stop-arm behavior with an inline "stop after current
// step? (y/n)" prompt.
//
// RED phase: Update()'s key handling has not yet been changed to enter a
// pending-confirmation state before arming GracefulStop() -- pressing 's'
// today arms the stop immediately -- so every test below fails until I2.1/
// I2.2 implement the state machine described in ContractsDesign.md.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressProgressKey sends a single-rune key press to the screen, matching the
// tea.KeyMsg shape produced by Bubble Tea for a plain character key.
func pressProgressKey(s *ProgressScreen, key string) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// TestProgressScreen_PressS_EntersPendingConfirmation asserts that pressing
// 's' shows a pending-confirmation state without immediately arming the stop.
func TestProgressScreen_PressS_EntersPendingConfirmation(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")

	if !s.ConfirmPending() {
		t.Error("ConfirmPending() = false after 's' pressed; want true")
	}
	if s.GracefulStop() {
		t.Error("GracefulStop() = true after only 's' pressed; want false until confirmed")
	}
}

// TestProgressScreen_PressY_WhilePending_ConfirmsStop asserts that an
// explicit affirmative response while pending arms the stop.
func TestProgressScreen_PressY_WhilePending_ConfirmsStop(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "y")

	if !s.GracefulStop() {
		t.Error("GracefulStop() = false after 'y' confirms a pending stop; want true")
	}
	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after the stop was confirmed; want false")
	}
}

// TestProgressScreen_PressN_WhilePending_CancelsConfirmation asserts that an
// explicit 'n' response while pending cancels the pending confirmation and
// leaves the run running.
func TestProgressScreen_PressN_WhilePending_CancelsConfirmation(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "n")

	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after 'n' cancels the pending confirmation; want false")
	}
	if s.GracefulStop() {
		t.Error("GracefulStop() = true after 'n' cancels the pending confirmation; want false")
	}
}

// TestProgressScreen_PressUnrelatedKey_WhilePending_CancelsConfirmation
// asserts that any key other than the affirmative response cancels the
// pending confirmation, not just 'n'.
func TestProgressScreen_PressUnrelatedKey_WhilePending_CancelsConfirmation(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "x")

	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after an unrelated key cancels the pending confirmation; want false")
	}
	if s.GracefulStop() {
		t.Error("GracefulStop() = true after an unrelated key cancels the pending confirmation; want false")
	}
}

// TestProgressScreen_ReenterPendingAfterCancel_Repeatable asserts that the
// confirmation flow is repeatable: pressing 's' again after a cancelled
// confirmation re-enters the pending state.
func TestProgressScreen_ReenterPendingAfterCancel_Repeatable(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "n") // cancel
	pressProgressKey(s, "s") // re-enter

	if !s.ConfirmPending() {
		t.Error("ConfirmPending() = false after re-pressing 's' following a cancelled confirmation; want true (flow must be repeatable)")
	}
	if s.GracefulStop() {
		t.Error("GracefulStop() = true after re-entering pending confirmation; want false")
	}
}

// TestProgressScreen_View_ShowsConfirmationPrompt_WhilePending asserts that
// View() renders the confirmation prompt while pending.
func TestProgressScreen_View_ShowsConfirmationPrompt_WhilePending(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")

	view := s.View()
	if !strings.Contains(view, "Stop after current step?") {
		t.Errorf("View() does not show the confirmation prompt while pending:\n%s", view)
	}
}

// TestProgressScreen_View_DoesNotShowStopNotice_WhilePending asserts that the
// "Stopping after current step completes…" notice does not appear merely
// because a confirmation is pending -- it must wait for actual confirmation.
func TestProgressScreen_View_DoesNotShowStopNotice_WhilePending(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")

	view := s.View()
	if strings.Contains(view, "Stopping after current step completes") {
		t.Errorf("View() shows the confirmed-stop notice while only pending (unconfirmed):\n%s", view)
	}
}

// TestProgressScreen_View_ShowsStopNotice_OnceConfirmed asserts that the
// existing stop notice still appears once the stop is actually confirmed.
func TestProgressScreen_View_ShowsStopNotice_OnceConfirmed(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "y")

	view := s.View()
	if !strings.Contains(view, "Stopping after current step completes") {
		t.Errorf("View() does not show the stop notice once confirmed:\n%s", view)
	}
}

// TestProgressScreen_KeyPressAfterConfirmed_IsInert asserts that once a stop
// is confirmed, further key presses (including another 's') do not change
// GracefulStop() or ConfirmPending() -- mirrors today's one-way stopRequest
// field per ContractsDesign.md's key-handling contract.
func TestProgressScreen_KeyPressAfterConfirmed_IsInert(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	pressProgressKey(s, "y")
	pressProgressKey(s, "s")

	if !s.GracefulStop() {
		t.Error("GracefulStop() = false after a key press following confirmation; want true (stays confirmed)")
	}
	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after a key press following confirmation; want false (inert)")
	}
}

// TestProgressScreen_UppercaseKeys_EnterPendingAndConfirm asserts that the
// uppercase key variants ('S' to enter pending, 'Y' to confirm) behave
// identically to their lowercase counterparts.
func TestProgressScreen_UppercaseKeys_EnterPendingAndConfirm(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "S")

	if !s.ConfirmPending() {
		t.Error("ConfirmPending() = false after 'S' pressed; want true")
	}

	pressProgressKey(s, "Y")

	if !s.GracefulStop() {
		t.Error("GracefulStop() = false after 'Y' confirms a pending stop; want true")
	}
	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after the stop was confirmed via 'Y'; want false")
	}
}
