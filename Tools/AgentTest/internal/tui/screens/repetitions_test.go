package screens_test

// repetitions_test.go verifies the RepetitionsScreen behavioral contract:
// digit keys build a draft value, non-digit keys are silently ignored,
// Enter confirms the draft, Esc sets Back, Reset clears Done and Back, and
// Reset discards any partially typed draft so the screen behaves as if freshly entered.

import (
	"strings"
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
// Backspace — removes last digit from draft
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_Backspace_RemovesLastDigit verifies that pressing
// Backspace after typing digits removes the last digit from the draft so that
// Enter confirms only the remaining digits.
func TestRepetitionsScreen_Backspace_RemovesLastDigit(t *testing.T) {
	// Arrange — type two digits so the draft is "42"
	s := newRepetitionsScreen(1)
	s.Update(digitKey('4'))
	s.Update(digitKey('2'))

	// Act — erase the '2', leaving "4"
	s.Update(backspaceKey())

	// Confirm so we can observe the committed value
	s.Update(enterKey())

	// Assert — only the remaining digit "4" must have been committed
	if s.Value() != 4 {
		t.Errorf("Value() = %d after typing '42', Backspace, Enter; want 4 (last digit erased)", s.Value())
	}
}

// TestRepetitionsScreen_Backspace_OnEmptyDraft_IsNoOp verifies that pressing
// Backspace when the draft is empty does not crash and does not alter the
// confirmed value, Done, or Back flags.
func TestRepetitionsScreen_Backspace_OnEmptyDraft_IsNoOp(t *testing.T) {
	// Arrange — screen with no digits typed (draft is empty)
	s := newRepetitionsScreen(3)

	// Act — backspace on an empty draft must be a silent no-op
	s.Update(backspaceKey())

	// Assert — flags unchanged
	if s.Done() {
		t.Error("Done() = true after Backspace on empty draft; Backspace must not set Done")
	}
	if s.Back() {
		t.Error("Back() = true after Backspace on empty draft; Backspace must not set Back")
	}
	// Assert — confirmed value unchanged (initial is still 3, nothing was confirmed)
	s.Update(enterKey()) // confirm whatever the draft holds
	if s.Value() != 3 {
		t.Errorf("Value() = %d after Backspace on empty draft + Enter; want initial value 3 (no-op must leave value unchanged)", s.Value())
	}
}

// TestRepetitionsScreen_InPlaceEdit_TypeBackspaceTypeThenEnter_ConfirmsCorrectValue
// verifies the complete in-place correction workflow described in AC2.3: type
// digits, use backspace to erase the last digit, type more digits, then confirm
// with Enter. The confirmed value must reflect only the final draft content.
func TestRepetitionsScreen_InPlaceEdit_TypeBackspaceTypeThenEnter_ConfirmsCorrectValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(1)

	// Act — type "42" (draft = "42"), backspace once (draft = "4"),
	// type '3' (draft = "43"), then confirm.
	s.Update(digitKey('4'))
	s.Update(digitKey('2'))
	s.Update(backspaceKey()) // removes '2'; draft = "4"
	s.Update(digitKey('3'))  // draft = "43"
	s.Update(enterKey())

	// Assert — the confirmed value must be 43, not 42 or 423.
	if s.Value() != 43 {
		t.Errorf("Value() = %d after typing '4','2', Backspace, '3', Enter; want 43 (in-place correction must yield the final draft digits)", s.Value())
	}
}

// TestRepetitionsScreen_Backspace_DoesNotSetDoneOrBack verifies that a Backspace
// key press alone does not accidentally advance or cancel the screen.
func TestRepetitionsScreen_Backspace_DoesNotSetDoneOrBack(t *testing.T) {
	// Arrange — type one digit so the draft is non-empty
	s := newRepetitionsScreen(1)
	s.Update(digitKey('7'))

	// Act
	s.Update(backspaceKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true after Backspace; Backspace must not set Done")
	}
	if s.Back() {
		t.Error("Back() = true after Backspace; Backspace must not set Back")
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

// TestRepetitionsScreen_View_ContainsInitialValue verifies that View shows the
// initial value when no draft has been typed, so the user can see the resolved
// default before making any changes.
func TestRepetitionsScreen_View_ContainsInitialValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(7)

	// Act
	view := collapseWhitespace(s.View())

	// Assert — the rendered output must contain "7" before any digit is typed
	if !strings.Contains(view, "7") {
		t.Errorf("View() = %q; want it to contain the initial value \"7\" before any user input", view)
	}
}

// ---------------------------------------------------------------------------
// Enter without typing — preserves the initial value
// ---------------------------------------------------------------------------

// TestRepetitionsScreen_EnterWithoutTyping_PreservesInitialValue verifies that
// pressing Enter without typing any digits leaves Value() equal to the initial
// value. This is the screen-level contract that allows the caller to pass the
// resolved suite default as the initial so pressing Enter alone accepts it.
func TestRepetitionsScreen_EnterWithoutTyping_PreservesInitialValue(t *testing.T) {
	// Arrange
	s := newRepetitionsScreen(5)

	// Act — confirm without typing anything
	s.Update(enterKey())

	// Assert
	if s.Value() != 5 {
		t.Errorf("Value() = %d after Enter without typing; want initial value 5 (no typed draft must not overwrite the initial)", s.Value())
	}
}
