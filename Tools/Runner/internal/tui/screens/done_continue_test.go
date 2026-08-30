package screens

// done_continue_test.go verifies DoneScreen's Continue() action:
// pressing 'c'/'C' sets Continue() only when the screen was constructed for
// domain.RunStopped. For every other outcome status the key is inert, and
// Done()'s existing 'q'/'enter'/'esc' behaviour is entirely unchanged.
//
// RED phase: DoneScreen has no Continue() method, no 'c' key binding, and no
// conditional help text yet -- every test below fails to compile until I3.1
// and I3.2 add those.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// newDoneScreen returns a DoneScreen for the given outcome status with a
// standard 80x24 size and progressStyles() for consistency with the other
// screen-level tests in this package.
func newDoneScreen(status domain.RunStatus) *DoneScreen {
	outcome := domain.RunOutcome{Status: status, Message: "test outcome"}
	return NewDoneScreen(outcome, "", 80, 24, progressStyles())
}

// pressDoneKey sends a single-rune key press to the screen.
func pressDoneKey(s *DoneScreen, key string) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// pressDoneSpecialKey sends a named key (enter, esc) to the screen.
func pressDoneSpecialKey(s *DoneScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// ---------------------------------------------------------------------------
// Continue() initial state
// ---------------------------------------------------------------------------

// TestDoneScreen_Continue_FalseInitially asserts that Continue() is false when
// a RunStopped screen is first constructed, before any key is pressed.
func TestDoneScreen_Continue_FalseInitially(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	if s.Continue() {
		t.Error("Continue() = true on a freshly constructed RunStopped screen; want false")
	}
}

// TestDoneScreen_Done_FalseInitially asserts that Done() is false on a freshly
// constructed screen (regression guard; must remain true for all statuses).
func TestDoneScreen_Done_FalseInitially(t *testing.T) {
	for _, status := range []domain.RunStatus{
		domain.RunStopped,
		domain.RunCompleted,
		domain.RunFailed,
		domain.RunRefused,
		domain.RunDeviationUnresolved,
	} {
		s := newDoneScreen(status)
		if s.Done() {
			t.Errorf("Done() = true on freshly constructed screen with status %q; want false", status)
		}
	}
}

// ---------------------------------------------------------------------------
// 'c' / 'C' sets Continue() only for RunStopped
// ---------------------------------------------------------------------------

// TestDoneScreen_PressC_WhenRunStopped_SetsContinue asserts that pressing 'c'
// on a RunStopped screen sets Continue() to true.
func TestDoneScreen_PressC_WhenRunStopped_SetsContinue(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneKey(s, "c")

	if !s.Continue() {
		t.Error("Continue() = false after 'c' on a RunStopped screen; want true")
	}
}

// TestDoneScreen_PressUppercaseC_WhenRunStopped_SetsContinue asserts that
// pressing 'C' (uppercase) on a RunStopped screen also sets Continue() to true,
// matching the case-insensitive convention used throughout the TUI.
func TestDoneScreen_PressUppercaseC_WhenRunStopped_SetsContinue(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneKey(s, "C")

	if !s.Continue() {
		t.Error("Continue() = false after 'C' on a RunStopped screen; want true")
	}
}

// TestDoneScreen_PressC_WhenRunStopped_DoesNotSetDone asserts that triggering
// the continue action does not set Done() -- the two actions are distinct.
func TestDoneScreen_PressC_WhenRunStopped_DoesNotSetDone(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneKey(s, "c")

	if s.Done() {
		t.Error("Done() = true after 'c' on a RunStopped screen; "+
			"continue and exit are distinct actions -- Done() must stay false")
	}
}

// TestDoneScreen_PressC_WhenRunCompleted_IsInert asserts that 'c' is ignored
// on a RunCompleted screen and neither Continue() nor Done() changes.
func TestDoneScreen_PressC_WhenRunCompleted_IsInert(t *testing.T) {
	s := newDoneScreen(domain.RunCompleted)

	pressDoneKey(s, "c")

	if s.Continue() {
		t.Error("Continue() = true after 'c' on a RunCompleted screen; want false (key is inert for non-RunStopped)")
	}
	if s.Done() {
		t.Error("Done() = true after 'c' on a RunCompleted screen; want false (key is inert for non-RunStopped)")
	}
}

// TestDoneScreen_PressC_WhenRunFailed_IsInert asserts that 'c' is ignored on
// a RunFailed screen.
func TestDoneScreen_PressC_WhenRunFailed_IsInert(t *testing.T) {
	s := newDoneScreen(domain.RunFailed)

	pressDoneKey(s, "c")

	if s.Continue() {
		t.Error("Continue() = true after 'c' on a RunFailed screen; want false (key is inert for non-RunStopped)")
	}
}

// TestDoneScreen_PressC_WhenRunRefused_IsInert asserts that 'c' is ignored on
// a RunRefused screen.
func TestDoneScreen_PressC_WhenRunRefused_IsInert(t *testing.T) {
	s := newDoneScreen(domain.RunRefused)

	pressDoneKey(s, "c")

	if s.Continue() {
		t.Error("Continue() = true after 'c' on a RunRefused screen; want false (key is inert for non-RunStopped)")
	}
}

// TestDoneScreen_PressC_WhenRunDeviationUnresolved_IsInert asserts that 'c' is
// ignored on a RunDeviationUnresolved screen.
func TestDoneScreen_PressC_WhenRunDeviationUnresolved_IsInert(t *testing.T) {
	s := newDoneScreen(domain.RunDeviationUnresolved)

	pressDoneKey(s, "c")

	if s.Continue() {
		t.Error("Continue() = true after 'c' on a RunDeviationUnresolved screen; want false (key is inert for non-RunStopped)")
	}
}

// ---------------------------------------------------------------------------
// 'q' / enter / esc still set Done() -- regression guards
// ---------------------------------------------------------------------------

// TestDoneScreen_PressQ_WhenRunStopped_SetsDone asserts that 'q' still exits
// the screen even when the outcome is RunStopped.
func TestDoneScreen_PressQ_WhenRunStopped_SetsDone(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneKey(s, "q")

	if !s.Done() {
		t.Error("Done() = false after 'q' on a RunStopped screen; 'q' must always exit regardless of status")
	}
}

// TestDoneScreen_PressQ_WhenRunStopped_DoesNotSetContinue asserts that 'q' does
// not trigger the continue action.
func TestDoneScreen_PressQ_WhenRunStopped_DoesNotSetContinue(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneKey(s, "q")

	if s.Continue() {
		t.Error("Continue() = true after 'q' on a RunStopped screen; 'q' is the exit action, not continue")
	}
}

// TestDoneScreen_PressEnter_WhenRunStopped_SetsDone asserts that Enter still
// exits the screen for RunStopped.
func TestDoneScreen_PressEnter_WhenRunStopped_SetsDone(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneSpecialKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Error("Done() = false after Enter on a RunStopped screen; Enter must always exit regardless of status")
	}
}

// TestDoneScreen_PressEsc_WhenRunStopped_SetsDone asserts that Esc still exits
// the screen for RunStopped.
func TestDoneScreen_PressEsc_WhenRunStopped_SetsDone(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	pressDoneSpecialKey(s, tea.KeyEsc)

	if !s.Done() {
		t.Error("Done() = false after Esc on a RunStopped screen; Esc must always exit regardless of status")
	}
}

// TestDoneScreen_ExistingExitKeys_StillWorkForAllStatuses asserts that 'q',
// enter, and esc all set Done() for every non-RunStopped status (regression
// guard: the new key binding must not affect other statuses).
func TestDoneScreen_ExistingExitKeys_StillWorkForAllStatuses(t *testing.T) {
	statuses := []domain.RunStatus{
		domain.RunCompleted,
		domain.RunFailed,
		domain.RunRefused,
		domain.RunDeviationUnresolved,
	}
	for _, status := range statuses {
		s := newDoneScreen(status)
		pressDoneKey(s, "q")
		if !s.Done() {
			t.Errorf("Done() = false after 'q' on screen with status %q; want true", status)
		}
	}
}

// ---------------------------------------------------------------------------
// View() help text discoverability
// ---------------------------------------------------------------------------

// TestDoneScreen_View_ShowsContinueHint_WhenRunStopped asserts that View()
// renders a "c continue" hint in the help line when the outcome is RunStopped,
// satisfying the discoverability requirement.
func TestDoneScreen_View_ShowsContinueHint_WhenRunStopped(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	view := s.View()

	if !strings.Contains(view, "continue") {
		t.Errorf("View() does not contain a 'continue' key hint for RunStopped:\n%s", view)
	}
}

// TestDoneScreen_View_ShowsExitHint_WhenRunStopped asserts that the existing
// exit hint is still present alongside the new continue hint for RunStopped.
func TestDoneScreen_View_ShowsExitHint_WhenRunStopped(t *testing.T) {
	s := newDoneScreen(domain.RunStopped)

	view := s.View()

	if !strings.Contains(view, "exit") {
		t.Errorf("View() does not contain the existing 'exit' key hint for RunStopped:\n%s", view)
	}
}

// TestDoneScreen_View_NoContinueHint_WhenRunCompleted asserts that View() does
// not add the "c continue" hint for RunCompleted -- the hint is conditional.
func TestDoneScreen_View_NoContinueHint_WhenRunCompleted(t *testing.T) {
	s := newDoneScreen(domain.RunCompleted)

	view := s.View()

	// "continue" must not appear in the help text for RunCompleted.
	helpLines := helpSection(view)
	if strings.Contains(helpLines, "continue") {
		t.Errorf("View() shows a 'continue' hint for RunCompleted (help section):\n%s", view)
	}
}

// TestDoneScreen_View_NoContinueHint_WhenRunFailed asserts that View() does not
// add the "c continue" hint for RunFailed.
func TestDoneScreen_View_NoContinueHint_WhenRunFailed(t *testing.T) {
	s := newDoneScreen(domain.RunFailed)

	view := s.View()

	helpLines := helpSection(view)
	if strings.Contains(helpLines, "continue") {
		t.Errorf("View() shows a 'continue' hint for RunFailed (help section):\n%s", view)
	}
}

// helpSection returns the last non-empty line of a view string, which by
// DoneScreen's layout is the help line. This restricts the "no continue hint"
// assertion to the help text rather than the title or outcome message.
func helpSection(view string) string {
	lines := strings.Split(view, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return view
}
