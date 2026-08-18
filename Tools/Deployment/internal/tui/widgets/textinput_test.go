package widgets_test

// textinput_test.go verifies the TextInput widget's validation gate and navigation signals.
// All tests drive the widget through Update/View with no real terminal.

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/widgets"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// rejectAll is a validator that always rejects with a fixed error message.
func rejectAll(s string) error {
	return errors.New("always invalid: " + s)
}

// acceptAll is a validator that always passes.
func acceptAll(s string) error { return nil }

// rejectEmpty is a validator that rejects empty strings.
func rejectEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("value cannot be empty")
	}
	return nil
}

// newTestInput creates a TextInput with the default styles and a given width.
func newTestInput(label string) *widgets.TextInput {
	return widgets.NewTextInput(label, "placeholder", 80, widgets.DefaultTextInputStyles())
}

// ---------------------------------------------------------------------------
// No validation — Enter always confirms
// ---------------------------------------------------------------------------

// TestTextInput_Enter_WithNoValidator_SetsDone verifies that when no validator is attached,
// any value (including an empty one) is accepted on Enter.
func TestTextInput_Enter_WithNoValidator_SetsDone(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValue("some text")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !ti.Done() {
		t.Error("Done() = false after Enter with no validator; want true")
	}
}

// ---------------------------------------------------------------------------
// Validation — validator rejects
// ---------------------------------------------------------------------------

// TestTextInput_Enter_WithRejectingValidator_DoesNotSetDone verifies that when the validator
// returns an error, Enter does not set Done, preventing the user from advancing with invalid
// input.
func TestTextInput_Enter_WithRejectingValidator_DoesNotSetDone(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(rejectAll)
	ti.SetValue("any value")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if ti.Done() {
		t.Error("Done() = true after Enter with rejecting validator; want false (invalid input must not advance)")
	}
}

// TestTextInput_Enter_WithRejectingValidator_ShowsErrMsg verifies that when the validator
// rejects the input, the error message is visible in the rendered view.
func TestTextInput_Enter_WithRejectingValidator_ShowsErrMsg(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(rejectAll)
	ti.SetValue("bad value")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := ti.View()

	// Assert
	if !strings.Contains(view, "always invalid") {
		t.Errorf("view does not show the validation error message:\n%s", view)
	}
}

// TestTextInput_ErrMsg_ReturnedAfterRejection verifies that ErrMsg() returns the error text
// after a failed validation, allowing callers to inspect the error without parsing the view.
func TestTextInput_ErrMsg_ReturnedAfterRejection(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(rejectAll)
	ti.SetValue("bad value")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if ti.ErrMsg() == "" {
		t.Error("ErrMsg() is empty after validation failure; want non-empty error text")
	}
}

// ---------------------------------------------------------------------------
// Validation — validator accepts
// ---------------------------------------------------------------------------

// TestTextInput_Enter_WithAcceptingValidator_SetsDone verifies that when the validator passes,
// Enter sets Done so the caller can advance to the next screen.
func TestTextInput_Enter_WithAcceptingValidator_SetsDone(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(acceptAll)
	ti.SetValue("valid text")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !ti.Done() {
		t.Error("Done() = false after Enter with accepting validator; want true")
	}
}

// TestTextInput_Enter_WithAcceptingValidator_ClearsErrMsg verifies that a successful
// validation clears any previous error message so the UI returns to a clean state.
func TestTextInput_Enter_WithAcceptingValidator_ClearsErrMsg(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(rejectAll)
	ti.SetValue("bad")
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter}) // produce an error

	// Now attach an accepting validator and try again
	ti.SetValidate(acceptAll)
	ti.SetValue("good")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if ti.ErrMsg() != "" {
		t.Errorf("ErrMsg() = %q after valid submission; want empty string", ti.ErrMsg())
	}
}

// ---------------------------------------------------------------------------
// Esc — back navigation
// ---------------------------------------------------------------------------

// TestTextInput_Esc_SetsBack verifies that pressing Esc signals back navigation, allowing
// the user to return to the previous screen without leaving the keyboard.
func TestTextInput_Esc_SetsBack(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	if !ti.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// TestTextInput_Esc_DoesNotSetDone verifies that Esc and Done are mutually exclusive:
// pressing Esc must not accidentally confirm the current value.
func TestTextInput_Esc_DoesNotSetDone(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValue("some text")

	// Act
	ti.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	if ti.Done() {
		t.Error("Done() = true after Esc; want false (Esc must not confirm input)")
	}
}

// ---------------------------------------------------------------------------
// Value preservation
// ---------------------------------------------------------------------------

// TestTextInput_Value_ReturnsCurrentText verifies that Value() reflects the text that was
// set via SetValue, making it available to the caller after Done() is set.
func TestTextInput_Value_ReturnsCurrentText(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValue("hello world")

	// Act
	v := ti.Value()

	// Assert
	if v != "hello world" {
		t.Errorf("Value() = %q, want %q", v, "hello world")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestTextInput_Reset_ClearsDoneAndBack verifies that Reset clears both Done and Back flags,
// allowing the widget to be reused after back navigation.
func TestTextInput_Reset_ClearsDoneAndBack(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(acceptAll)
	ti.SetValue("valid")
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter}) // set Done

	// Act
	ti.Reset()

	// Assert
	if ti.Done() {
		t.Error("Done() = true after Reset; want false")
	}
	if ti.Back() {
		t.Error("Back() = true after Reset; want false")
	}
}

// TestTextInput_Reset_ClearsErrMsg verifies that Reset clears the current error message so
// the input re-displays without stale error text.
func TestTextInput_Reset_ClearsErrMsg(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValidate(rejectAll)
	ti.SetValue("bad")
	ti.Update(tea.KeyMsg{Type: tea.KeyEnter}) // produce error

	// Act
	ti.Reset()

	// Assert
	if ti.ErrMsg() != "" {
		t.Errorf("ErrMsg() = %q after Reset; want empty string", ti.ErrMsg())
	}
}

// TestTextInput_Reset_PreservesTextValue verifies that Reset does not clear the typed text,
// so the user does not have to re-enter a value after back navigation.
func TestTextInput_Reset_PreservesTextValue(t *testing.T) {
	// Arrange
	ti := newTestInput("Enter value")
	ti.SetValue("preserved text")
	ti.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back

	// Act
	ti.Reset()

	// Assert
	if ti.Value() != "preserved text" {
		t.Errorf("Value() = %q after Reset; want %q (text must survive reset)", ti.Value(), "preserved text")
	}
}
