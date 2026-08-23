package screens_test

// repetitions_test.go verifies the RepetitionsScreen behavioral contract:
// digit keys build a draft value, non-digit keys are silently ignored,
// Enter confirms the draft, Esc sets Back, Reset clears Done and Back, and
// Reset discards any partially typed draft so the screen behaves as if freshly entered.

import (
	"testing"

	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newRepetitionsScreen(initial int) *screens.RepetitionsScreen {
	return screens.NewRepetitionsScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_InitialValue_IsPreserved verifies that the screen is
// constructed with the initial value visible before any input.
func TestRepetitionsScreen_InitialValue_IsPreserved(t *testing.T) {
	// Arrange / Act
	s := newRepetitionsScreen(3)

	// Assert
	if s.Value() != 3 {
		t.Errorf("Value() = %d immediately after construction; want initial value 3", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Digit input — builds the confirmed value
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_SingleDigit_ConfirmsCorrectValue verifies that typing a
// single digit and pressing Enter produces the corresponding integer value.
func TestRepetitionsScreen_SingleDigit_ConfirmsCorrectValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

	// Act
	s.Update(digitKey('5'))
	s.Update(enterKey())

	// Assert
	if s.Value() != 5 {
		t.Errorf("Value() = %d after typing '5' and Enter; want 5", s.Value())
	}
}

// TestRepetitionsScreen_MultiDigit_ConfirmsCorrectValue verifies that typing
// multiple digits produces the correct multi-digit integer.
func TestRepetitionsScreen_MultiDigit_ConfirmsCorrectValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

	// Act
	s.Update(digitKey('1'))
	s.Update(digitKey('0'))
	s.Update(enterKey())

	// Assert
	if s.Value() != 10 {
		t.Errorf("Value() = %d after typing '10' and Enter; want 10", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Non-digit input — silently ignored
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_NonDigitKey_IsIgnored verifies that pressing a non-digit
// key does not corrupt the draft and the confirmed value is unchanged after Enter.
func TestRepetitionsScreen_NonDigitKey_IsIgnored(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(2)

	// Act — type a valid digit, then an alpha key that must be ignored, then confirm
	s.Update(digitKey('7'))
	s.Update(runeKey('a'))
	s.Update(enterKey())

	// Assert — only the digit '7' should be present in the confirmed value
	if s.Value() != 7 {
		t.Errorf("Value() = %d after '7'+'a'+Enter; non-digit must be ignored, want 7", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Enter key — confirms the draft
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_Enter_SetsDone verifies that pressing Enter sets Done
// so the root model can advance to the next screen.
func TestRepetitionsScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

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

// TestRepetitionsScreen_Esc_SetsBack verifies that pressing Esc sets Back.
func TestRepetitionsScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestRepetitionsScreen_Esc_DoesNotChangValue verifies that Esc does not alter
// the confirmed value.
func TestRepetitionsScreen_Esc_DoesNotChangeValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(3)

	// Act — type a digit, then cancel with Esc
	s.Update(digitKey('9'))
	s.Update(escKey())

	// Assert — the confirmed value must still be the initial value
	if s.Value() != 3 {
		t.Errorf("Value() = %d after typing then Esc; Esc must discard the draft, want initial value 3", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_Reset_ClearsDone verifies that Reset clears the Done flag.
func TestRepetitionsScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)
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

// TestRepetitionsScreen_Reset_ClearsBack verifies that Reset clears the Back flag.
func TestRepetitionsScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)
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

// TestRepetitionsScreen_Reset_ClearsDraftState verifies that Reset discards any
// digits typed before Reset so that a fresh entry after Reset reflects only
// post-Reset input. An implementation that clears only Done/Back but retains
// the in-progress digit buffer would accumulate pre- and post-Reset digits,
// producing the wrong confirmed value.
func TestRepetitionsScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — type a digit without confirming to accumulate draft state.
	s := newRepetitionsScreen(1)
	s.Update(digitKey('9')) // draft buffer now contains '9'

	// Act
	s.Reset()

	// Act (post-reset) — type a single digit and confirm.
	// If Reset correctly cleared the draft, Value() reflects only '5'.
	// If Reset did NOT clear the draft ('9' still in buffer), Value() would
	// reflect '95' (pre-reset '9' concatenated with post-reset '5').
	s.Update(digitKey('5'))
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert
	if s.Value() != 5 {
		t.Errorf("Value() = %d after Reset + type '5' + Enter; want 5 — Reset must discard the pre-Reset digit draft", s.Value())
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_Resize_DoesNotTriggerDoneOrBack verifies that a terminal
// resize event does not accidentally set Done or Back.
func TestRepetitionsScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

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

// TestRepetitionsScreen_View_ReturnsNonEmptyString verifies that View returns
// a non-empty string so the root model has content to display.
func TestRepetitionsScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the repetitions screen must render visible content")
	}
}
