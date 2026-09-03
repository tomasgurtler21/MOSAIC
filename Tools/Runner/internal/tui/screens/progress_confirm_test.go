package screens

// progress_confirm_test.go verifies ProgressScreen's stop-confirmation state
// machine (ConfirmPending / GracefulStop): pressing 's' opens an inline
// "stop after current step? (y/n)" gate, and only an explicit confirm or
// cancel key resolves it.
//
// The gate is deliberately sticky. Only 'y'/'Y' confirm and only 'n'/'N'/esc
// cancel; every other key leaves the gate pending rather than silently
// dismissing it. Force-quit stays available at the root model, so a user
// facing the gate is never trapped -- that assertion lives in the tui package,
// since ctrl+c is handled above screen delegation and is not observable here.

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

// runeKey builds the tea.KeyMsg Bubble Tea produces for a plain character key.
func runeKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
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

// TestProgressScreen_PressUnrecognisedKey_WhilePending_StaysPending asserts
// that a key which is neither a confirm nor a cancel key leaves the gate
// pending, instead of silently dismissing it.
//
// The 'a'/'A'/'i'/'I' cases carry extra weight: those keys have a competing
// meaning on this screen (artifact view), so a fall-through from the pending
// gate into the artifact-view branch would surface there first. Those cases
// therefore assert both that the gate is still pending and that no
// artifact-view request was raised while it was.
func TestProgressScreen_PressUnrecognisedKey_WhilePending_StaysPending(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"unrelated rune", runeKey("x")},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}},
		{"second stop key", runeKey("s")},
		{"uppercase stop key", runeKey("S")},
		{"arrow key", tea.KeyMsg{Type: tea.KeyUp}},
		{"artifact-view a", runeKey("a")},
		{"artifact-view A", runeKey("A")},
		{"artifact-view i", runeKey("i")},
		{"artifact-view I", runeKey("I")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewProgressScreen(80, 24, progressStyles())

			pressProgressKey(s, "s")
			s.Update(tc.key)

			if !s.ConfirmPending() {
				t.Errorf("ConfirmPending() = false after %q while the gate was pending; want true (only confirm/cancel keys resolve the gate)", tc.key.String())
			}
			if s.GracefulStop() {
				t.Errorf("GracefulStop() = true after %q while the gate was pending; want false (the gate was never confirmed)", tc.key.String())
			}
			if s.ArtifactViewRequested() {
				t.Errorf("ArtifactViewRequested() = true after %q while the gate was pending; want false (keys must not fall through to the artifact-view branch while the gate is open)", tc.key.String())
			}
		})
	}
}

// TestProgressScreen_ArtifactKeys_WhileIdle_RequestArtifactView asserts the
// positive half of the artifact-view contract: with no gate open, 'a'/'A'/'i'/'I'
// each raise an artifact-view request and leave the stop state untouched.
//
// This is the counterpart to the pending-state assertion above. Making the gate
// sticky means rewriting the same key-handling branch that artifact view lives
// in, and "no artifact view while pending" is trivially satisfiable by breaking
// artifact view outright -- so the idle behaviour has to be pinned too.
func TestProgressScreen_ArtifactKeys_WhileIdle_RequestArtifactView(t *testing.T) {
	for _, key := range []string{"a", "A", "i", "I"} {
		t.Run(key, func(t *testing.T) {
			s := NewProgressScreen(80, 24, progressStyles())

			pressProgressKey(s, key)

			if !s.ArtifactViewRequested() {
				t.Errorf("ArtifactViewRequested() = false after %q with no gate pending; want true (the artifact-view binding must survive the gate rewrite)", key)
			}
			if s.ConfirmPending() {
				t.Errorf("ConfirmPending() = true after %q; want false (an artifact key must not open the stop gate)", key)
			}
			if s.GracefulStop() {
				t.Errorf("GracefulStop() = true after %q; want false (an artifact key must not arm the stop)", key)
			}
		})
	}
}

// TestProgressScreen_PendingGate_StillResolvableAfterIgnoredKeys asserts that
// ignored keys leave the gate usable: a confirm key pressed afterwards still
// resolves it. This is what makes the sticky gate an improvement rather than a
// trap.
func TestProgressScreen_PendingGate_StillResolvableAfterIgnoredKeys(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())

	pressProgressKey(s, "s")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pressProgressKey(s, "x")
	pressProgressKey(s, "y")

	if !s.GracefulStop() {
		t.Error("GracefulStop() = false after 'y' following ignored keys; want true (ignored keys must leave the gate resolvable)")
	}
	if s.ConfirmPending() {
		t.Error("ConfirmPending() = true after the gate was confirmed; want false")
	}
}

// TestProgressScreen_ConfirmKeys_WhilePending_ConfirmStop asserts that both
// confirm keys resolve the gate affirmatively.
func TestProgressScreen_ConfirmKeys_WhilePending_ConfirmStop(t *testing.T) {
	for _, key := range []string{"y", "Y"} {
		t.Run(key, func(t *testing.T) {
			s := NewProgressScreen(80, 24, progressStyles())

			pressProgressKey(s, "s")
			pressProgressKey(s, key)

			if !s.GracefulStop() {
				t.Errorf("GracefulStop() = false after %q resolved a pending gate; want true", key)
			}
			if s.ConfirmPending() {
				t.Errorf("ConfirmPending() = true after %q confirmed the gate; want false", key)
			}
		})
	}
}

// TestProgressScreen_CancelKeys_WhilePending_CancelConfirmation asserts that
// every cancel key -- including esc, which is not a rune key -- dismisses the
// gate without arming the stop.
func TestProgressScreen_CancelKeys_WhilePending_CancelConfirmation(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"n", runeKey("n")},
		{"N", runeKey("N")},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewProgressScreen(80, 24, progressStyles())

			pressProgressKey(s, "s")
			s.Update(tc.key)

			if s.ConfirmPending() {
				t.Errorf("ConfirmPending() = true after %q cancelled the gate; want false", tc.key.String())
			}
			if s.GracefulStop() {
				t.Errorf("GracefulStop() = true after %q cancelled the gate; want false", tc.key.String())
			}
		})
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
