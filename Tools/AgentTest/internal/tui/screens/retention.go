package screens

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-agent-test/internal/domain"
)

// RetentionScreen presents the retention policy as a single cyclable value.
//
// Key bindings:
//   - Space cycles through RetentionPolicy values:
//     RetainNever -> RetainOnFailure -> RetainAlways -> RetainNever (wrapping).
//   - Enter confirms the currently displayed value and sets Done() true.
//     It does NOT cycle; it accepts the value as-is.
//   - Esc sets Back() true without changing the value.
type RetentionScreen struct {
	initial domain.RetentionPolicy
	policy  domain.RetentionPolicy
	width   int
	styles  Styles
	done    bool
	back    bool
}

// cycleOrder defines the wrapping cycle sequence for retention policy selection.
var cycleOrder = []domain.RetentionPolicy{
	domain.RetainNever,
	domain.RetainOnFailure,
	domain.RetainAlways,
}

// NewRetentionScreen creates a RetentionScreen initialized to initial.
func NewRetentionScreen(initial domain.RetentionPolicy, width int, styles Styles) *RetentionScreen {
	return &RetentionScreen{
		initial: initial,
		policy:  initial,
		width:   width,
		styles:  styles,
	}
}

// Update processes a key message. Space cycles the policy; Enter sets Done;
// Esc sets Back.
func (s *RetentionScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Type == tea.KeyEnter:
		s.done = true
	case key.Type == tea.KeyEsc:
		s.back = true
	case key.Type == tea.KeyRunes && len(key.Runes) == 1 && key.Runes[0] == ' ':
		s.policy = nextRetentionPolicy(s.policy)
	}
	return nil
}

// nextRetentionPolicy advances the policy to the next value in the cycle.
func nextRetentionPolicy(current domain.RetentionPolicy) domain.RetentionPolicy {
	for i, p := range cycleOrder {
		if p == current {
			return cycleOrder[(i+1)%len(cycleOrder)]
		}
	}
	// Unknown value: start from beginning.
	return cycleOrder[0]
}

// View renders the retention screen with theme-resolved styles.
func (s *RetentionScreen) View() string {
	title := s.styles.Title.Render("Retention Policy")
	value := s.styles.Selected.Render(fmt.Sprintf("%s", s.policy))
	help := s.styles.Help.Render("space cycle  enter confirm  esc back")
	content := title + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the current policy value.
func (s *RetentionScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *RetentionScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags and restores the policy to its initial
// value so the screen behaves as if freshly entered.
func (s *RetentionScreen) Reset() {
	s.done = false
	s.back = false
	s.policy = s.initial
}

// Resize updates the available width without affecting Done, Back, or the
// current policy value.
func (s *RetentionScreen) Resize(width int) {
	s.width = width
}

// Policy returns the currently selected RetentionPolicy.
func (s *RetentionScreen) Policy() domain.RetentionPolicy { return s.policy }
