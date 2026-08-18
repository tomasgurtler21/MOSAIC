package widgets

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
type TextInput struct {
	model      textinput.Model
	label      string
	validateFn func(string) error
	errMsg     string
	done       bool
	back       bool
	styles     TextInputStyles
	width      int
}

// NewTextInput creates a TextInput with the given label and optional placeholder.
func NewTextInput(label, placeholder string, width int, styles TextInputStyles) *TextInput {
	m := textinput.New()
	m.Placeholder = placeholder
	m.Focus()

	return &TextInput{
		model:  m,
		label:  label,
		styles: styles,
		width:  width,
	}
}

// SetValidate attaches a validation function. When non-nil, Enter only sets Done when the
// function returns nil; otherwise the error message is shown inline.
func (t *TextInput) SetValidate(fn func(string) error) { t.validateFn = fn }

// SetValue pre-fills the input field. Call before the first Update.
func (t *TextInput) SetValue(v string) { t.model.SetValue(v) }

// Init returns the blink command required by bubbles/textinput.
func (t *TextInput) Init() tea.Cmd { return textinput.Blink }

// Update processes a message. Keyboard messages are handled for navigation; all messages
// are forwarded to the underlying textinput model to maintain cursor blinking.
func (t *TextInput) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			val := t.model.Value()
			if t.validateFn != nil {
				if err := t.validateFn(val); err != nil {
					t.errMsg = err.Error()
					// Don't forward enter to the model — it has no special meaning there.
					return nil
				}
			}
			t.errMsg = ""
			t.done = true
			return nil
		case tea.KeyEsc:
			t.back = true
			return nil
		}
	}

	t.model, cmd = t.model.Update(msg)
	return cmd
}

// View renders the label, the input field, and any validation error on separate lines.
func (t *TextInput) View() string {
	label := t.styles.Label.Width(t.width).Render(t.label)
	input := t.model.View()
	if t.errMsg != "" {
		return label + "\n" + input + "\n" + t.styles.ErrMsg.Width(t.width).Render(t.errMsg)
	}
	return label + "\n" + input
}

// Value returns the current text in the input field.
func (t *TextInput) Value() string { return t.model.Value() }

// ErrMsg returns the current validation error message, or empty when the last value was valid.
func (t *TextInput) ErrMsg() string { return t.errMsg }

// Done reports whether the user confirmed valid input.
func (t *TextInput) Done() bool { return t.done }

// Back reports whether the user pressed Esc.
func (t *TextInput) Back() bool { return t.back }

// Reset clears the done, back, and error flags without clearing the text value.
func (t *TextInput) Reset() {
	t.done = false
	t.back = false
	t.errMsg = ""
}

// Resize updates the render width.
func (t *TextInput) Resize(width int) { t.width = width }
