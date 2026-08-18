package screens_test

// testhelpers_test.go provides shared helpers used across all screen test files.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// collapseWhitespace joins the words in s with single spaces, removing all newlines and
// extra whitespace. This is used to compare view output that lipgloss may have wrapped
// across multiple lines, without coupling tests to the exact terminal width used in rendering.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ---------------------------------------------------------------------------
// Mode navigation helpers (shared across mode-related test files)
// ---------------------------------------------------------------------------

// maxModeNavigations is an upper bound on how many Down presses might be needed to reach
// any single mode in the mode screen list. Set conservatively larger than the expected list
// length so that position-independent tests remain valid even if additional modes are added.
const maxModeNavigations = 15

// navigateToMode presses Down on the screen until SelectedMode() equals target or
// maxSteps is reached. It returns the number of Down presses actually performed and
// whether the target was found.
func navigateToMode(s *screens.ModeScreen, target domain.RunMode, maxSteps int) (steps int, found bool) {
	for i := 0; i < maxSteps; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
		steps++
		s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Done() && s.SelectedMode() == target {
			return steps, true
		}
		// Reset so we can keep navigating.
		// After Reset, list cursor position is preserved — only Done/Back flags are cleared.
		s.Reset()
	}
	return steps, false
}

// positionOf navigates the screen forward up to maxSteps times and returns the
// number of Down presses required to land on the target mode. Returns -1 if not found.
// It does NOT commit the selection; it uses Enter + Reset to peek at each position.
func positionOf(s *screens.ModeScreen, target domain.RunMode, maxSteps int) int {
	// Start at position 0 (no Down presses yet). Check if the initial item is the target.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() && s.SelectedMode() == target {
		s.Reset()
		return 0
	}
	s.Reset()

	for i := 1; i <= maxSteps; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
		s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Done() && s.SelectedMode() == target {
			s.Reset()
			return i
		}
		s.Reset()
	}
	return -1
}

// collectAllModes navigates the screen through all positions up to maxSteps and returns
// every unique RunMode found. Because pressing Down past the last item keeps the cursor
// at the last item, deduplication ensures each mode is counted once regardless of how
// many extra navigations happen at the end.
func collectAllModes(maxSteps int) []domain.RunMode {
	seen := make(map[domain.RunMode]bool)
	var modes []domain.RunMode
	for pos := 0; pos <= maxSteps; pos++ {
		s := newModeScreen()
		for j := 0; j < pos; j++ {
			s.Update(tea.KeyMsg{Type: tea.KeyDown})
		}
		s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if s.Done() {
			mode := s.SelectedMode()
			if !seen[mode] {
				seen[mode] = true
				modes = append(modes, mode)
			}
		}
	}
	return modes
}
