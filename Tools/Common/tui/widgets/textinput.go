package widgets

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-common/tui/pastesafe"
)

// TextInputStyles holds the resolved styles used by a TextInput.
type TextInputStyles struct {
	Label  lipgloss.Style
	Input  lipgloss.Style
	ErrMsg lipgloss.Style
}

// DefaultTextInputStyles returns a plain style set usable in tests.
func DefaultTextInputStyles() TextInputStyles {
	return TextInputStyles{
		Label:  lipgloss.NewStyle(),
		Input:  lipgloss.NewStyle(),
		ErrMsg: lipgloss.NewStyle(),
	}
}

// TextInput wraps bubbles/textinput with an optional validation function and clear error
// feedback. When Validate is non-nil, the entered value must pass before Done is set.
//
// Entry is paste-safe: a pasted line break never submits and becomes a space (see package
// pastesafe).
type TextInput struct {
	field  *pastesafe.Field
	label  string
	styles TextInputStyles
	width  int
}

// NewTextInput creates a TextInput with the given label and optional placeholder.
func NewTextInput(label, placeholder string, width int, styles TextInputStyles) *TextInput {
	return &TextInput{
		field:  pastesafe.NewField(pastesafe.WithPlaceholder(placeholder)),
		label:  label,
		styles: styles,
		width:  width,
	}
}

// SetValidate attaches a validation function. When non-nil, Enter only sets Done when the
// function returns nil; otherwise the error message is shown inline.
func (t *TextInput) SetValidate(fn func(string) error) { t.field.SetValidate(fn) }

// SetValue pre-fills the input field. Call before the first Update.
func (t *TextInput) SetValue(v string) { t.field.SetValue(v) }

// Init returns the blink command required by bubbles/textinput.
func (t *TextInput) Init() tea.Cmd { return t.field.Init() }

// Update processes a message. Keyboard messages are handled for navigation; all messages
// are forwarded to the underlying textinput model to maintain cursor blinking.
func (t *TextInput) Update(msg tea.Msg) tea.Cmd { return t.field.Update(msg) }

// View renders the label, the input field, and any validation error on separate lines.
func (t *TextInput) View() string {
	label := t.styles.Label.Width(t.width).Render(t.label)
	input := t.field.View()
	if errMsg := t.field.ErrMsg(); errMsg != "" {
		return label + "\n" + input + "\n" + t.styles.ErrMsg.Width(t.width).Render(errMsg)
	}
	return label + "\n" + input
}

// Value returns the current text in the input field.
func (t *TextInput) Value() string { return t.field.Value() }

// ErrMsg returns the current validation error message, or empty when the last value was valid.
func (t *TextInput) ErrMsg() string { return t.field.ErrMsg() }

// Done reports whether the user confirmed valid input.
func (t *TextInput) Done() bool { return t.field.Done() }

// Back reports whether the user pressed Esc.
func (t *TextInput) Back() bool { return t.field.Back() }

// Reset clears the done, back, and error flags without clearing the text value.
func (t *TextInput) Reset() { t.field.Reset() }

// Resize updates the render width.
func (t *TextInput) Resize(width int) { t.width = width }

// SetClock sets the paste-detection clock of this input (nil = process-wide clock).
func (t *TextInput) SetClock(c pastesafe.Clock) { t.field.SetClock(c) }
