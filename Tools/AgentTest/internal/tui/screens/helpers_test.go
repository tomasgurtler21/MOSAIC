package screens_test

// helpers_test.go provides shared key-message constructors and style helpers
// used across all screen test files.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/tui/screens"
)

// plainStyles returns a Styles with zero-value lipgloss styles, producing
// plain text without ANSI escape codes. This makes View assertions reliable.
func plainStyles() screens.Styles {
	return screens.Styles{} // zero-value lipgloss.Style renders plain text
}

// key helpers — match the naming pattern from Deployment's harness_test.go

func enterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func escKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func spaceKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}} }

func digitKey(d rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{d}}
}

func runeKey(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// collapseWhitespace joins the words in s with single spaces, removing
// newlines and extra whitespace. Used to compare View output that lipgloss
// may have wrapped across multiple lines.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
