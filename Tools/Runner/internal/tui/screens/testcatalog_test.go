package screens_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// TestCatalogScreen — path validation and navigation
// ---------------------------------------------------------------------------

// makeMosaicRoot creates a temporary directory tree that mimics a MOSAIC repo
// root containing Tools/Runner/TestCatalog/. Returns the root path.
// Cleaned up automatically by the test runner via t.Cleanup.
func makeMosaicRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	catalogDir := filepath.Join(root, "Tools", "Runner", "TestCatalog")
	if err := os.MkdirAll(catalogDir, 0755); err != nil {
		t.Fatalf("setup: could not create TestCatalog directory: %v", err)
	}
	return root
}

// typeTestCatalogInput sends the given text into the TestCatalogScreen as a
// single KeyRunes event, simulating paste or typed input.
func typeTestCatalogInput(s *screens.TestCatalogScreen, text string) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

// TestTestCatalogScreen_InitialState verifies that a newly created
// TestCatalogScreen starts with Done() and Back() false.
func TestTestCatalogScreen_InitialState(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	if s.Done() {
		t.Error("Done() is true before any key is pressed; screen must start in pending state")
	}
	if s.Back() {
		t.Error("Back() is true before any key is pressed")
	}
}

// TestTestCatalogScreen_EmptyPath_Rejected verifies that pressing Enter on an
// empty input does not set Done() to true.
func TestTestCatalogScreen_EmptyPath_Rejected(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() {
		t.Error("Done() is true after Enter with empty input; empty path must be rejected")
	}
}

// TestTestCatalogScreen_MissingTestCatalog_Rejected verifies that a path that
// exists on disk but lacks Tools/Runner/TestCatalog/ is rejected.
func TestTestCatalogScreen_MissingTestCatalog_Rejected(t *testing.T) {
	// A temp dir exists but does NOT contain Tools/Runner/TestCatalog/.
	root := t.TempDir()
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	typeTestCatalogInput(s, root)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.Done() {
		t.Errorf("Done() is true for a path without Tools/Runner/TestCatalog/ (%s); validation must reject it", root)
	}
}

// TestTestCatalogScreen_ValidPath_Accepted verifies that a path containing
// Tools/Runner/TestCatalog/ is accepted by the screen.
func TestTestCatalogScreen_ValidPath_Accepted(t *testing.T) {
	root := makeMosaicRoot(t)
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	typeTestCatalogInput(s, root)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Errorf("Done() is false after entering a valid MOSAIC root (%s); expected acceptance", root)
	}
}

// TestTestCatalogScreen_ValueReturnsNormalizedPath verifies that Value() returns
// the normalized (whitespace-trimmed, outer-quote-stripped) path after Done().
func TestTestCatalogScreen_ValueReturnsNormalizedPath(t *testing.T) {
	root := makeMosaicRoot(t)
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	// Type the path with surrounding whitespace.
	typeTestCatalogInput(s, "  "+root+"  ")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatalf("Done() is false; prerequisite: valid path with padding must be accepted")
	}
	if got := s.Value(); got != root {
		t.Errorf("Value() = %q, want %q; whitespace padding must be trimmed", got, root)
	}
}

// TestTestCatalogScreen_DoubleQuotedPath_Accepted verifies that a double-quoted
// path pointing at a valid MOSAIC root is accepted (outer quotes stripped).
func TestTestCatalogScreen_DoubleQuotedPath_Accepted(t *testing.T) {
	root := makeMosaicRoot(t)
	quoted := `"` + root + `"`
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	typeTestCatalogInput(s, quoted)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Errorf("Done() is false for double-quoted valid path %q; outer quotes must be stripped before validation", quoted)
	}
	if got := s.Value(); got != root {
		t.Errorf("Value() = %q, want %q; outer double quotes must be stripped", got, root)
	}
}

// TestTestCatalogScreen_EscSetsBack verifies that pressing Esc sets Back() to
// true without setting Done().
func TestTestCatalogScreen_EscSetsBack(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Error("Back() is false after Esc; expected true")
	}
	if s.Done() {
		t.Error("Done() is true after Esc; expected false")
	}
}

// TestTestCatalogScreen_ViewContainsTitle verifies that View() renders a
// non-empty string that contains the screen title.
func TestTestCatalogScreen_ViewContainsTitle(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	view := s.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
	if !strings.Contains(view, "Test Catalog Path") {
		t.Errorf("View() does not contain 'Test Catalog Path'; got:\n%s", view)
	}
}

// TestTestCatalogScreen_ViewContainsTestCatalogGuidance verifies that View()
// mentions the expected directory so the user knows what the path must contain.
func TestTestCatalogScreen_ViewContainsTestCatalogGuidance(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	view := s.View()
	if !strings.Contains(view, "TestCatalog") {
		t.Errorf("View() does not mention 'TestCatalog'; users must know what the path must contain. Got:\n%s", view)
	}
}

// TestTestCatalogScreen_ResetClearsState verifies that Reset() clears Done and
// Back so the screen can be re-entered cleanly.
func TestTestCatalogScreen_ResetClearsState(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Fatal("precondition: Back() must be true after Esc")
	}
	s.Reset()
	if s.Back() {
		t.Error("Back() is true after Reset; expected false")
	}
	if s.Done() {
		t.Error("Done() is true after Reset; expected false")
	}
}

// TestTestCatalogScreen_ResizeDoesNotPanic verifies that Resize does not panic.
func TestTestCatalogScreen_ResizeDoesNotPanic(t *testing.T) {
	s := screens.NewTestCatalogScreen(80, 24, screens.Styles{})
	s.Resize(120, 40)
}
