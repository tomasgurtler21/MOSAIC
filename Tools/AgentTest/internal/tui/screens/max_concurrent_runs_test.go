package screens_test

// max_concurrent_runs_test.go verifies the MaxConcurrentRunsScreen behavioral
// contract: digit keys build a draft value, non-digit keys are silently
// ignored, Enter confirms the draft, Esc sets Back, Reset clears Done and Back,
// and Reset discards any partially typed draft so the screen behaves as if freshly entered.

import (
	"testing"

	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMaxConcurrentRunsScreen(initial int) *screens.MaxConcurrentRunsScreen {
	return screens.NewMaxConcurrentRunsScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_InitialValue_IsPreserved verifies that the screen
// is constructed with the initial value available before any input.
func TestMaxConcurrentRunsScreen_InitialValue_IsPreserved(t *testing.T) {
	// Arrange / Act
	s := newMaxConcurrentRunsScreen(4)

	// Assert
	if s.Value() != 4 {
		t.Errorf("Value() = %d immediately after construction; want initial value 4", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Digit input — builds the confirmed value
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_SingleDigit_ConfirmsCorrectValue verifies that
// typing a single digit and pressing Enter produces the corresponding integer.
func TestMaxConcurrentRunsScreen_SingleDigit_ConfirmsCorrectValue(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	s.Update(digitKey('3'))
	s.Update(enterKey())

	// Assert
	if s.Value() != 3 {
		t.Errorf("Value() = %d after typing '3' and Enter; want 3", s.Value())
	}
}

// TestMaxConcurrentRunsScreen_MultiDigit_ConfirmsCorrectValue verifies that
// typing multiple digits produces the correct multi-digit integer.
func TestMaxConcurrentRunsScreen_MultiDigit_ConfirmsCorrectValue(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	s.Update(digitKey('2'))
	s.Update(digitKey('0'))
	s.Update(enterKey())

	// Assert
	if s.Value() != 20 {
		t.Errorf("Value() = %d after typing '20' and Enter; want 20", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Non-digit input — silently ignored
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_NonDigitKey_IsIgnored verifies that pressing a
// non-digit key does not corrupt the draft.
func TestMaxConcurrentRunsScreen_NonDigitKey_IsIgnored(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(2)

	// Act — type a valid digit, then a letter that must be ignored, then confirm
	s.Update(digitKey('4'))
	s.Update(runeKey('x'))
	s.Update(enterKey())

	// Assert — only the digit '4' should contribute to the confirmed value
	if s.Value() != 4 {
		t.Errorf("Value() = %d after '4'+'x'+Enter; non-digit must be ignored, want 4", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Enter key — confirms the draft
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_Enter_SetsDone verifies that pressing Enter sets
// Done so the root model can advance to the next screen.
func TestMaxConcurrentRunsScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter; Enter must confirm and set Done")
	}
}

// ---------------------------------------------------------------------------
// Esc key — back navigation
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_Esc_SetsBack verifies that pressing Esc sets Back.
func TestMaxConcurrentRunsScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestMaxConcurrentRunsScreen_Esc_DoesNotChangeValue verifies that Esc discards
// the draft without changing the confirmed value.
func TestMaxConcurrentRunsScreen_Esc_DoesNotChangeValue(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(2)

	// Act — type a digit, then cancel with Esc
	s.Update(digitKey('9'))
	s.Update(escKey())

	// Assert — the confirmed value must still be the initial value
	if s.Value() != 2 {
		t.Errorf("Value() = %d after typing then Esc; Esc must discard draft, want initial value 2", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_Reset_ClearsDone verifies that Reset clears Done.
func TestMaxConcurrentRunsScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Done() {
		t.Error("Done() = true after Reset; Reset must clear the Done flag")
	}
}

// TestMaxConcurrentRunsScreen_Reset_ClearsBack verifies that Reset clears Back.
func TestMaxConcurrentRunsScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)
	s.Update(escKey())
	if !s.Back() {
		t.Fatal("precondition: Back() must be true after Esc before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Back() {
		t.Error("Back() = true after Reset; Reset must clear the Back flag")
	}
}

// TestMaxConcurrentRunsScreen_Reset_ClearsDraftState verifies that Reset
// discards any digits typed before Reset so that a fresh entry after Reset
// reflects only post-Reset input. An implementation that clears only Done/Back
// but retains the in-progress digit buffer would accumulate pre- and
// post-Reset digits, producing the wrong confirmed value.
func TestMaxConcurrentRunsScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — type a digit without confirming to accumulate draft state.
	s := newMaxConcurrentRunsScreen(1)
	s.Update(digitKey('8')) // draft buffer now contains '8'

	// Act
	s.Reset()

	// Act (post-reset) — type a single digit and confirm.
	// If Reset correctly cleared the draft, Value() reflects only '3'.
	// If Reset did NOT clear the draft ('8' still in buffer), Value() would
	// reflect '83' (pre-reset '8' concatenated with post-reset '3').
	s.Update(digitKey('3'))
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert
	if s.Value() != 3 {
		t.Errorf("Value() = %d after Reset + type '3' + Enter; want 3 — Reset must discard the pre-Reset digit draft", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_Resize_DoesNotTriggerDoneOrBack verifies that a
// terminal resize event does not accidentally set Done or Back.
func TestMaxConcurrentRunsScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	s.Resize(120)

	// Assert
	if s.Done() {
		t.Error("Done() = true after Resize; Resize must not trigger Done")
	}
	if s.Back() {
		t.Error("Back() = true after Resize; Resize must not trigger Back")
	}
}

// ---------------------------------------------------------------------------
// View — renders content
// ---------------------------------------------------------------------------

// TestMaxConcurrentRunsScreen_View_ReturnsNonEmptyString verifies that View
// returns a non-empty string so the root model has content to display.
func TestMaxConcurrentRunsScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newMaxConcurrentRunsScreen(1)

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the max-concurrent-runs screen must render visible content")
	}
}
