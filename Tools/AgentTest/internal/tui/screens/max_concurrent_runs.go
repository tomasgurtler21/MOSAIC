package screens

import (
	"fmt"
	"strconv"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MaxConcurrentRunsScreen accepts numeric input for the concurrency bound.
// Only digit characters are accepted; non-digit keys are silently ignored.
// Enter confirms the current draft value and sets Done; Esc sets Back.
type MaxConcurrentRunsScreen struct {
	value  int    // last confirmed value (or initial)
	draft  string // in-progress digit input; empty means no active edit
	width  int
	styles Styles
	done   bool
	back   bool
}

// NewMaxConcurrentRunsScreen creates a MaxConcurrentRunsScreen initialized to initial.
func NewMaxConcurrentRunsScreen(initial int, width int, styles Styles) *MaxConcurrentRunsScreen {
	return &MaxConcurrentRunsScreen{
		value:  initial,
		width:  width,
		styles: styles,
	}
}

// Update processes a key message. Digit keys append to the draft; Enter
// commits the draft and sets Done; Esc sets Back; non-digit keys are ignored.
func (s *MaxConcurrentRunsScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.Type {
	case tea.KeyEnter:
		if s.draft != "" {
			if n, err := strconv.Atoi(s.draft); err == nil {
				s.value = n
			}
			s.draft = ""
		}
		s.done = true
	case tea.KeyEsc:
		s.draft = ""
		s.back = true
	case tea.KeyRunes:
		for _, r := range key.Runes {
			if unicode.IsDigit(r) {
				s.draft += string(r)
			}
		}
	}
	return nil
}

// View renders the max concurrent runs screen with theme-resolved styles.
func (s *MaxConcurrentRunsScreen) View() string {
	display := s.draft
	if display == "" {
		display = fmt.Sprintf("%d", s.value)
	}
	title := s.styles.Title.Render("Max Concurrent Runs")
	value := s.styles.Selected.Render(display)
	help := s.styles.Help.Render("type digits  enter confirm  esc back")
	content := title + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the concurrency bound.
func (s *MaxConcurrentRunsScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *MaxConcurrentRunsScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags and discards any in-progress digit
// draft so the screen behaves as if freshly entered.
func (s *MaxConcurrentRunsScreen) Reset() {
	s.done = false
	s.back = false
	s.draft = ""
}

// Resize updates the available width without affecting Done, Back, or the
// current value.
func (s *MaxConcurrentRunsScreen) Resize(width int) {
	s.width = width
}

// Value returns the currently confirmed concurrency bound.
func (s *MaxConcurrentRunsScreen) Value() int { return s.value }
