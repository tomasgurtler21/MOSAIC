package screens

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"mosaic-common/tui/pathutil"
)

// ReportPathScreen accepts inline text input for the report file path.
// Enter confirms the current text and sets Done; Esc sets Back without
// changing the confirmed path.
type ReportPathScreen struct {
	confirmed string // last confirmed path (or initial)
	entry     *draftEntry
	width     int
	styles    Styles
	done      bool
	back      bool
}

// NewReportPathScreen creates a ReportPathScreen initialized to initial.
func NewReportPathScreen(initial string, width int, styles Styles) *ReportPathScreen {
	return &ReportPathScreen{
		entry:     newDraftEntry(),
		confirmed: initial,
		width:     width,
		styles:    styles,
	}
}

// Update processes a key message. Rune keys append to the draft; Enter
// commits the draft and sets Done; Esc sets Back.
func (s *ReportPathScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	res := s.entry.update(key)
	switch {
	case res.submitted:
		if res.draft != "" {
			s.confirmed = res.draft
		}
		s.done = true
	case res.back:
		s.back = true
	}
	return nil
}

// View renders the report path screen with theme-resolved styles.
func (s *ReportPathScreen) View() string {
	display := s.entry.draft()
	if display == "" {
		display = s.confirmed
	}
	title := s.styles.Title.Render("Report Path")
	value := s.styles.Selected.Render(display)
	help := s.styles.Help.Render("type path  backspace delete  enter confirm  esc back")
	content := title + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the report path.
func (s *ReportPathScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *ReportPathScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags and discards any in-progress draft so
// the screen behaves as if freshly entered.
func (s *ReportPathScreen) Reset() {
	s.done = false
	s.back = false
	s.entry.reset()
}

// Resize updates the available width without affecting Done, Back, or the
// current path.
func (s *ReportPathScreen) Resize(width int) {
	s.width = width
}

// Path returns the currently confirmed report file path, normalized.
func (s *ReportPathScreen) Path() string { return pathutil.NormalizeInput(s.confirmed) }
