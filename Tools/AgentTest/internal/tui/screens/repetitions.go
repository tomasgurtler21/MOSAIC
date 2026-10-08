package screens

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RepetitionsScreen accepts numeric input for the repetition count.
// Only digit characters are accepted; non-digit keys are silently ignored.
// Enter confirms the current draft value and sets Done; Esc sets Back.
type RepetitionsScreen struct {
	value  int // last confirmed value (or initial)
	entry  *draftEntry
	width  int
	styles Styles
	done   bool
	back   bool
}

// NewRepetitionsScreen creates a RepetitionsScreen initialized to initial.
func NewRepetitionsScreen(initial int, width int, styles Styles) *RepetitionsScreen {
	return &RepetitionsScreen{
		entry:  newDigitEntry(),
		value:  initial,
		width:  width,
		styles: styles,
	}
}

// Update processes a key message. Digit keys append to the draft; Enter
// commits the draft and sets Done; Esc sets Back; non-digit keys are ignored.
func (s *RepetitionsScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	res := s.entry.update(key)
	switch {
	case res.submitted:
		if res.draft != "" {
			if n, err := strconv.Atoi(res.draft); err == nil {
				s.value = n
			}
		}
		s.done = true
	case res.back:
		s.back = true
	}
	return nil
}

// View renders the repetitions screen with theme-resolved styles.
func (s *RepetitionsScreen) View() string {
	display := s.entry.draft()
	if display == "" {
		display = fmt.Sprintf("%d", s.value)
	}
	title := s.styles.Title.Render("Repetitions")
	value := s.styles.Selected.Render(display)
	help := s.styles.Help.Render("type digits  backspace delete  enter confirm  esc back")
	content := title + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the repetition count.
func (s *RepetitionsScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *RepetitionsScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags, discards any in-progress digit
// draft, and clears the edited flag so the screen behaves as if freshly entered.
func (s *RepetitionsScreen) Reset() {
	s.done = false
	s.back = false
	s.entry.reset()
}

// WasEdited reports whether the user has pressed at least one digit or
// backspace key since the screen was created or last Reset. Callers use this
// to distinguish "user pressed Enter without typing" (WasEdited false, initial
// value should not be committed as an override) from "user typed something"
// (WasEdited true, the confirmed Value should be committed).
func (s *RepetitionsScreen) WasEdited() bool { return s.entry.edited }

// Resize updates the available width without affecting Done, Back, or the
// current value.
func (s *RepetitionsScreen) Resize(width int) {
	s.width = width
}

// Value returns the currently confirmed repetition count.
func (s *RepetitionsScreen) Value() int { return s.value }
