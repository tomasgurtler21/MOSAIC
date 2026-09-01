package screens

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"mosaic-common/tui/pathutil"
)

// CatalogFolderScreen accepts inline text input for the catalog folder override.
// Enter confirms the current text and sets Done; Esc sets Back without
// changing the confirmed folder.
type CatalogFolderScreen struct {
	confirmed string // last confirmed folder (or initial)
	draft     string // in-progress text input; empty means no active edit
	width     int
	styles    Styles
	done      bool
	back      bool
}

// NewCatalogFolderScreen creates a CatalogFolderScreen initialized to initial.
func NewCatalogFolderScreen(initial string, width int, styles Styles) *CatalogFolderScreen {
	return &CatalogFolderScreen{
		confirmed: initial,
		width:     width,
		styles:    styles,
	}
}

// Update processes a key message. Rune keys append to the draft; Enter
// commits the draft and sets Done; Esc sets Back.
func (s *CatalogFolderScreen) Update(msg tea.Msg) tea.Cmd {
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
	case tea.KeyRunes:
		for _, r := range key.Runes {
			s.draft += string(r)
		}
	}
	return nil
}

// View renders the catalog folder screen with theme-resolved styles.
func (s *CatalogFolderScreen) View() string {
	display := s.draft
	if display == "" {
		display = s.confirmed
	}
	title := s.styles.Title.Render("Catalog Folder")
	value := s.styles.Selected.Render(display)
	help := s.styles.Help.Render("type folder  enter confirm  esc back")
	content := title + "\n\n  " + value + "\n\n" + help
	if s.width > 0 {
		return lipgloss.NewStyle().Width(s.width).Render(content)
	}
	return content
}

// Done reports whether the user confirmed the catalog folder.
func (s *CatalogFolderScreen) Done() bool { return s.done }

// Back reports whether the user pressed Esc to navigate backward.
func (s *CatalogFolderScreen) Back() bool { return s.back }

// Reset clears the Done and Back flags and discards any in-progress draft so
// the screen behaves as if freshly entered.
func (s *CatalogFolderScreen) Reset() {
	s.done = false
	s.back = false
	s.draft = ""
}

// Resize updates the available width without affecting Done, Back, or the
// current folder.
func (s *CatalogFolderScreen) Resize(width int) {
	s.width = width
}

// Folder returns the currently confirmed catalog folder override, normalized.
func (s *CatalogFolderScreen) Folder() string { return pathutil.NormalizeInput(s.confirmed) }
