package screens_test

// wrapping_test.go verifies that each settings screen's View() output wraps its
// content so no rendered line exceeds the configured terminal width. All tests
// pass GREEN against the current implementation: lipgloss v1.1.0's Width() call
// in each screen's View() performs character-level line breaking, which is
// sufficient to satisfy the acceptance criterion (no line exceeds the terminal
// width). No additional implementation changes are required.
//
// Covered settings screens:
//   - RepetitionsScreen
//   - MaxConcurrentRunsScreen
//   - ReportPathScreen
//   - StoreInputScreen
//   - RetentionScreen
//   - CatalogFolderScreen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/tui/screens"
)

// assertScreenLinesDoNotExceedWidth verifies that every line in the rendered
// view fits within maxWidth visible cells. lipgloss.Width is used to measure
// each line so that ANSI escape sequences in styled output are excluded from
// the character count.
func assertScreenLinesDoNotExceedWidth(t *testing.T, screenName, view string, maxWidth int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > maxWidth {
			t.Errorf("[%s] line %d is %d cells wide, want <= %d: %q",
				screenName, i, w, maxWidth, line)
		}
	}
}

// ---------------------------------------------------------------------------
// RepetitionsScreen
// ---------------------------------------------------------------------------

// TestWrapping_RepetitionsScreen_NoLineExceedsTerminalWidth verifies that
// RepetitionsScreen.View() wraps its content so no line exceeds the configured
// terminal width.
//
// At width=40, the help text "type digits  backspace delete  enter confirm  esc back"
// (54 characters) would overflow without wrapping. lipgloss v1.1.0's Width()
// performs character-level line breaking so the rendered output fits within the
// terminal width.
func TestWrapping_RepetitionsScreen_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	s := screens.NewRepetitionsScreen(1, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "RepetitionsScreen", view, termWidth)
}

// ---------------------------------------------------------------------------
// MaxConcurrentRunsScreen
// ---------------------------------------------------------------------------

// TestWrapping_MaxConcurrentRunsScreen_NoLineExceedsTerminalWidth verifies that
// MaxConcurrentRunsScreen.View() wraps its content so no line exceeds the
// configured terminal width.
//
// At width=40, the help text "type digits  backspace delete  enter confirm  esc back"
// (54 characters) would overflow without wrapping. lipgloss v1.1.0's Width()
// performs character-level line breaking so the rendered output fits within the
// terminal width.
func TestWrapping_MaxConcurrentRunsScreen_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	s := screens.NewMaxConcurrentRunsScreen(1, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "MaxConcurrentRunsScreen", view, termWidth)
}

// ---------------------------------------------------------------------------
// ReportPathScreen
// ---------------------------------------------------------------------------

// TestWrapping_ReportPathScreen_LongPath_NoLineExceedsTerminalWidth verifies
// that ReportPathScreen.View() wraps a long confirmed file path so no line
// exceeds the configured terminal width.
//
// A path of 69 characters at terminal width 40 would overflow without wrapping.
// lipgloss v1.1.0's Width() performs character-level line breaking so the path
// is broken to fit within the terminal width.
func TestWrapping_ReportPathScreen_LongPath_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	// A long, space-free path — 69 characters — that definitively overflows at
	// width=40 without word-wrapping.
	longPath := strings.Repeat("/very-long-path-segment", 3)

	s := screens.NewReportPathScreen(longPath, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "ReportPathScreen", view, termWidth)
}

// TestWrapping_ReportPathScreen_HelpText_NoLineExceedsTerminalWidth verifies
// that ReportPathScreen.View() wraps its help text so no line exceeds the
// configured terminal width when the path value itself is short.
//
// The help text "type path  backspace delete  enter confirm  esc back" (52
// characters) would overflow at width=40 without wrapping. lipgloss v1.1.0's
// Width() provides character-level line breaking that keeps the output within
// the terminal width.
func TestWrapping_ReportPathScreen_HelpText_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	s := screens.NewReportPathScreen("", termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "ReportPathScreen (help text)", view, termWidth)
}

// ---------------------------------------------------------------------------
// StoreInputScreen
// ---------------------------------------------------------------------------

// TestWrapping_StoreInputScreen_Subtitle_NoLineExceedsTerminalWidth verifies
// that StoreInputScreen.View() wraps its subtitle so no line exceeds the
// configured terminal width.
//
// At width=40, the subtitle "Enter a file path or directory path containing report JSON files"
// (63 characters) would overflow without wrapping. This test uses an empty
// initial path so the subtitle is the primary overflow source. lipgloss v1.1.0's
// Width() performs character-level line breaking so the rendered output fits
// within the terminal width.
func TestWrapping_StoreInputScreen_Subtitle_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	s := screens.NewStoreInputScreen("", termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "StoreInputScreen (subtitle)", view, termWidth)
}

// TestWrapping_StoreInputScreen_LongPath_NoLineExceedsTerminalWidth verifies
// that StoreInputScreen.View() wraps a long confirmed path so no line exceeds
// the configured terminal width.
//
// A path of 64 characters at terminal width 40 would overflow without wrapping.
// lipgloss v1.1.0's Width() performs character-level line breaking that keeps
// each rendered line within the terminal width.
func TestWrapping_StoreInputScreen_LongPath_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	// A long, space-free path — 64 characters — that overflows at width=40.
	longPath := strings.Repeat("/reports/segment", 4)

	s := screens.NewStoreInputScreen(longPath, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "StoreInputScreen (long path)", view, termWidth)
}

// ---------------------------------------------------------------------------
// RetentionScreen
// ---------------------------------------------------------------------------

// TestWrapping_RetentionScreen_NoLineExceedsTerminalWidth verifies that
// RetentionScreen.View() wraps its content so no line exceeds the configured
// terminal width.
//
// At width=30, the help text "space cycle  enter confirm  esc back" (36
// characters) would overflow without wrapping. lipgloss v1.1.0's Width()
// performs character-level line breaking so the rendered output fits within
// the terminal width.
func TestWrapping_RetentionScreen_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 30
	s := screens.NewRetentionScreen(domain.RetainNever, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "RetentionScreen", view, termWidth)
}

// ---------------------------------------------------------------------------
// CatalogFolderScreen
// ---------------------------------------------------------------------------

// TestWrapping_CatalogFolderScreen_LongPath_NoLineExceedsTerminalWidth verifies
// that CatalogFolderScreen.View() wraps a long confirmed folder path so no line
// exceeds the configured terminal width.
//
// A folder path of 64 characters at terminal width 40 would overflow without
// wrapping. lipgloss v1.1.0's Width() performs character-level line breaking
// so the path is broken to fit within the terminal width.
func TestWrapping_CatalogFolderScreen_LongPath_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 40
	// A long, space-free folder path — 64 characters — that overflows at width=40.
	longFolder := strings.Repeat("/catalog/section", 4)

	s := screens.NewCatalogFolderScreen(longFolder, termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "CatalogFolderScreen (long path)", view, termWidth)
}

// TestWrapping_CatalogFolderScreen_HelpText_NoLineExceedsTerminalWidth verifies
// that CatalogFolderScreen.View() wraps its help text so no line exceeds a
// narrow terminal width when the folder value itself is short.
//
// At width=30, the help text "type folder  enter confirm  esc back" (36
// characters) would overflow without wrapping. lipgloss v1.1.0's Width()
// performs character-level line breaking so the rendered output fits within
// width=30.
func TestWrapping_CatalogFolderScreen_HelpText_NoLineExceedsTerminalWidth(t *testing.T) {
	const termWidth = 30
	s := screens.NewCatalogFolderScreen("", termWidth, plainStyles())

	view := s.View()

	assertScreenLinesDoNotExceedWidth(t, "CatalogFolderScreen (help text)", view, termWidth)
}
