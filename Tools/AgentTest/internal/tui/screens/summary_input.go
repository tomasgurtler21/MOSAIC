package screens

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"mosaic-common/tui/pathutil"
)

// SummaryInputScreen accepts an optional version filter for the summary
// operation. Empty input means "all versions". The --for-version flag name
// from the CLI corresponds to this screen's version filter concept. Follows
// the same Update/View/Done/Back/Reset/Resize contract as ReportPathScreen.
type SummaryInputScreen struct {
	confirmed string // last confirmed filter (or initial); empty = all versions
	draft     string // in-progress text input
	width     int
	styles    Styles
	done      bool
	back      bool
}

// NewSummaryInputScreen creates a SummaryInputScreen initialized to
// initialFilter. An empty initialFilter means "all versions".
func NewSummaryInputScreen(initialFilter string, width int, styles Styles) *SummaryInputScreen {
	return &SummaryInputScreen{
		confirmed: initialFilter,
		width:     width,
		styles:    styles,
	}
}

// Update processes a key message. Rune keys append to the draft; Enter commits
// the draft (or accepts the existing confirmed value) and sets Done; Esc sets
// Back; Backspace deletes the last character of the draft.
func (s *SummaryInputScreen) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.Type {
	case tea.KeyEnter:
		if s.draft != "" {
			s.confirmed = s.draft
			s.draft = ""
		}
		s.done = true
	case tea.KeyEsc:
		s.draft = ""
		s.back = true
	case tea.KeyBackspace:
		if len(s.draft) > 0 {
			s.draft = s.draft[:len(s.draft)-1]
		}
	case tea.KeyRunes:
		for _, r := range key.Runes {
			s.draft += string(r)
		}
	}
	return nil
}

// View renders the summary input screen with theme-resolved styles.
func (s *SummaryInputScreen) View() string {
	display := s.draft
	if display == "" {
		if s.confirmed != "" {
			display = s.confirmed
		} else {
			display = "(all versions)"
		}
	}
	title := s.styles.Title.Render("Generate Summary")
	subtitle := s.styles.Muted.Render("Enter a version filter (--for-version) or leave empty to summarise all versions")
	value := s.styles.Selected.Render(display)
	help := s.styles.Help.Render("type version  backspace delete  enter confirm  esc back")
	content := title + "\n" + subtitle + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the filter.
func (s *SummaryInputScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *SummaryInputScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags and discards any in-progress draft so
// the screen behaves as if freshly entered.
func (s *SummaryInputScreen) Reset() {
	s.done = false
	s.back = false
	s.draft = ""
}

// Resize updates the available width without affecting Done, Back, or the
// current filter.
func (s *SummaryInputScreen) Resize(width int) {
	s.width = width
}

// VersionFilter returns the currently confirmed version filter, normalized.
// An empty string means "all versions".
func (s *SummaryInputScreen) VersionFilter() string { return pathutil.NormalizeInput(s.confirmed) }
