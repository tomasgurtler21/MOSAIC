package screens_test

// exec_override_test.go verifies the ExecOverrideScreen's own behaviour in
// isolation: context rendering, input capture, Esc-abandon, empty rejection,
// Reset, non-empty view, and resize safety.
//
// Tests drive the screen directly through tea.Msg key events with no root model
// attached. Because ExecOverrideScreen holds no filesystem dependency, no temp
// directories are needed.
//
// RED status: all tests in this file are expected to pass once the screen stub
// in exec_override.go is correctly wired; they describe screen-level contracts
// only. The app-level routing (which failure triggers the screen and what
// happens after override confirmation) is covered by exec_override_test.go in
// the parent tui package.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// newOverrideScreen creates an ExecOverrideScreen for testing with a zero-value
// Styles (produces unstyled but non-nil lipgloss renders, sufficient for
// behaviour tests).
func newOverrideScreen(harnessID, attemptedPath string, attempt int) *screens.ExecOverrideScreen {
	return screens.NewExecOverrideScreen(harnessID, attemptedPath, attempt, 80, 24, screens.Styles{})
}

// ---------------------------------------------------------------------------
// View — content pinning
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_ViewNonEmpty verifies that View() returns a non-empty
// string so the terminal shows meaningful content when the screen is active.
func TestExecOverrideScreen_ViewNonEmpty(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	if s.View() == "" {
		t.Error("View() returned empty string; want non-empty output")
	}
}

// TestExecOverrideScreen_ViewContainsHarnessID verifies that View() includes
// the harness identifier supplied at construction, so the user knows which
// harness could not be launched.
func TestExecOverrideScreen_ViewContainsHarnessID(t *testing.T) {
	const harnessID = "ghcp-cli"
	s := newOverrideScreen(harnessID, "/usr/bin/copilot", 1)
	view := s.View()
	if !overrideContainsStr(view, harnessID) {
		t.Errorf("View() does not contain harness ID %q:\n%s", harnessID, view)
	}
}

// TestExecOverrideScreen_ViewContainsAttemptedPath verifies that View() includes
// the executable path that was tried, so the user knows what failed and what to
// correct.
func TestExecOverrideScreen_ViewContainsAttemptedPath(t *testing.T) {
	const path = "/usr/bin/copilot"
	s := newOverrideScreen("ghcp-cli", path, 1)
	view := s.View()
	if !overrideContainsStr(view, path) {
		t.Errorf("View() does not contain attempted path %q:\n%s", path, view)
	}
}

// TestExecOverrideScreen_ViewContainsHarnessIDForClaudeCode verifies the same
// contract for a different harness, ensuring the harness ID is not hardcoded.
func TestExecOverrideScreen_ViewContainsHarnessIDForClaudeCode(t *testing.T) {
	const harnessID = "claude-code"
	const path = "/home/user/.nvm/versions/node/v20.0.0/bin/claude"
	s := newOverrideScreen(harnessID, path, 1)
	view := s.View()
	if !overrideContainsStr(view, harnessID) {
		t.Errorf("View() does not contain harness ID %q:\n%s", harnessID, view)
	}
	if !overrideContainsStr(view, path) {
		t.Errorf("View() does not contain attempted path %q:\n%s", path, view)
	}
}

// TestExecOverrideScreen_ViewRepeatedAttempt_DifferentiatesFromFirst verifies
// that the View produced for attempt > 1 is different from attempt == 1, so
// the user understands the new override also failed rather than seeing the same
// screen with no change.
func TestExecOverrideScreen_ViewRepeatedAttempt_DifferentiatesFromFirst(t *testing.T) {
	s1 := newOverrideScreen("ghcp-cli", "/first/path", 1)
	s2 := newOverrideScreen("ghcp-cli", "/second/path", 2)
	view1 := s1.View()
	view2 := s2.View()
	if view1 == view2 {
		t.Error("View() for attempt 2 is identical to attempt 1; a repeated failure must be visually distinct")
	}
}

// ---------------------------------------------------------------------------
// Confirm (Enter) with typed path
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_EnterWithPath_DoneTrue verifies that typing a path
// and pressing Enter sets Done() to true.
func TestExecOverrideScreen_EnterWithPath_DoneTrue(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/custom/copilot")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Error("Done() = false after typing a path and pressing Enter; want true")
	}
}

// TestExecOverrideScreen_EnterWithPath_ChoiceIsRetry verifies that confirming
// a path sets Choice() to ExecOverrideChoiceRetry.
func TestExecOverrideScreen_EnterWithPath_ChoiceIsRetry(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/custom/copilot")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check Choice()")
	}
	if s.Choice() != screens.ExecOverrideChoiceRetry {
		t.Errorf("Choice() = %q, want %q", s.Choice(), screens.ExecOverrideChoiceRetry)
	}
}

// TestExecOverrideScreen_Path_ReturnsEnteredPath verifies that Path() returns
// the path the user typed when Done() is true.
func TestExecOverrideScreen_Path_ReturnsEnteredPath(t *testing.T) {
	const entered = "/custom/copilot"
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(entered)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check Path()")
	}
	got := s.Path()
	if got != entered {
		t.Errorf("Path() = %q, want %q", got, entered)
	}
}

// TestExecOverrideScreen_Path_WhitespaceTrimmed verifies that Path() trims
// leading and trailing whitespace from the entered value.
func TestExecOverrideScreen_Path_WhitespaceTrimmed(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  /custom/copilot  ")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check Path() trimming")
	}
	got := s.Path()
	want := "/custom/copilot"
	if got != want {
		t.Errorf("Path() = %q, want %q (whitespace must be trimmed)", got, want)
	}
}

// ---------------------------------------------------------------------------
// Empty input rejected
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_EnterOnEmpty_DoneNotSet verifies that pressing Enter
// on an empty field does NOT set Done(). The override screen must reject blank
// input because "" means "use the per-harness default", and the default is
// exactly what just failed.
func TestExecOverrideScreen_EnterOnEmpty_DoneNotSet(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() {
		t.Error("Done() = true after pressing Enter on empty field; want false — " +
			"an empty override means 'use the per-harness default' which already failed")
	}
}

// TestExecOverrideScreen_EnterOnWhitespace_DoneNotSet verifies that pressing
// Enter on a whitespace-only field also does not set Done(), since the trimmed
// result is empty.
func TestExecOverrideScreen_EnterOnWhitespace_DoneNotSet(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("   ")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() {
		t.Error("Done() = true after Enter on whitespace-only field; want false — " +
			"a whitespace-only path trims to empty and must be rejected")
	}
}

// ---------------------------------------------------------------------------
// Back (Esc) — abandon without retrying
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_Esc_BackTrue verifies that pressing Esc sets Back()
// to true so the caller can transition to the done screen.
func TestExecOverrideScreen_Esc_BackTrue(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Error("Back() = false after pressing Esc; want true")
	}
}

// TestExecOverrideScreen_Esc_DoneNotSet verifies that pressing Esc does not
// accidentally set Done().
func TestExecOverrideScreen_Esc_DoneNotSet(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Done() {
		t.Error("Done() = true after pressing Esc; want false")
	}
}

// TestExecOverrideScreen_Esc_ChoiceIsAbandon verifies that pressing Esc
// produces Choice() == ExecOverrideChoiceAbandon.
func TestExecOverrideScreen_Esc_ChoiceIsAbandon(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Fatal("precondition: Back() = false after Esc; cannot check Choice()")
	}
	if s.Choice() != screens.ExecOverrideChoiceAbandon {
		t.Errorf("Choice() = %q after Esc, want %q", s.Choice(), screens.ExecOverrideChoiceAbandon)
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_Reset_ClearsDone verifies that Reset() clears the
// Done flag so re-entering the screen does not immediately auto-advance.
func TestExecOverrideScreen_Reset_ClearsDone(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/some/path")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false after Enter; cannot test Reset()")
	}
	s.Reset()
	if s.Done() {
		t.Error("Done() = true after Reset(); want false — stale Done must be cleared before re-entry")
	}
}

// TestExecOverrideScreen_Reset_ClearsBack verifies that Reset() clears the
// Back flag so re-entering the screen does not immediately auto-return.
func TestExecOverrideScreen_Reset_ClearsBack(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Fatal("precondition: Back() = false after Esc; cannot test Reset()")
	}
	s.Reset()
	if s.Back() {
		t.Error("Back() = true after Reset(); want false — stale Back must be cleared before re-entry")
	}
}

// ---------------------------------------------------------------------------
// InputInit
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_InputInit_ReturnsNonNilCmd verifies that InputInit()
// returns a non-nil tea.Cmd so the root model can start cursor blinking when
// the screen is entered.
func TestExecOverrideScreen_InputInit_ReturnsNonNilCmd(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	cmd := s.InputInit()
	if cmd == nil {
		t.Error("InputInit() = nil; want a non-nil tea.Cmd (cursor blink initialisation)")
	}
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

// TestExecOverrideScreen_Resize_DoesNotPanic verifies that Resize() accepts
// new dimensions without panicking, so window-resize messages are safe.
func TestExecOverrideScreen_Resize_DoesNotPanic(t *testing.T) {
	s := newOverrideScreen("ghcp-cli", "/usr/bin/copilot", 1)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize(120, 40) panicked: %v", r)
		}
	}()
	s.Resize(120, 40)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func overrideContainsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
