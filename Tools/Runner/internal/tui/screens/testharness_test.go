package screens_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// TestHarnessScreen — harness multi-select and HasGHCPCLI
// ---------------------------------------------------------------------------

// pressHarnessKey sends a single named key to the TestHarnessScreen.
func pressHarnessKey(s *screens.TestHarnessScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// pressHarnessRune sends a single rune key to the TestHarnessScreen.
func pressHarnessRune(s *screens.TestHarnessScreen, r rune) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// TestTestHarnessScreen_InitialState verifies that a freshly created
// TestHarnessScreen starts with Done() and Back() both false.
func TestTestHarnessScreen_InitialState(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	if s.Done() {
		t.Error("Done() is true on construction; screen must start in pending state")
	}
	if s.Back() {
		t.Error("Back() is true on construction")
	}
}

// TestTestHarnessScreen_ConfirmSetsDone verifies that pressing Enter confirms
// the selection and sets Done() to true.
func TestTestHarnessScreen_ConfirmSetsDown(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	pressHarnessKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after Enter; confirming the harness selection must set Done")
	}
	if s.Back() {
		t.Error("Back() is true after Enter; expected false")
	}
}

// TestTestHarnessScreen_EscSetsBack verifies that pressing Esc sets Back() to
// true without setting Done().
func TestTestHarnessScreen_EscSetsBack(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	pressHarnessKey(s, tea.KeyEsc)
	if !s.Back() {
		t.Error("Back() is false after Esc; expected true")
	}
	if s.Done() {
		t.Error("Done() is true after Esc; expected false")
	}
}

// TestTestHarnessScreen_SelectClaudeCode_HasGHCPCLIFalse verifies that
// selecting a non-ghcp-cli harness (claude-code, the first item) does not
// set HasGHCPCLI() to true.
func TestTestHarnessScreen_SelectClaudeCode_HasGHCPCLIFalse(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	// Cursor starts at index 0 (claude-code). Toggle it and confirm.
	pressHarnessRune(s, ' ') // space to toggle claude-code
	pressHarnessKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Fatal("Done() is false after Enter; prerequisite: confirm must set Done")
	}
	if s.HasGHCPCLI() {
		t.Error("HasGHCPCLI() is true after selecting claude-code only; expected false")
	}
}

// TestTestHarnessScreen_SelectGHCPCLI_HasGHCPCLITrue verifies that selecting
// ghcp-cli sets HasGHCPCLI() to true.
func TestTestHarnessScreen_SelectGHCPCLI_HasGHCPCLITrue(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	// CLISelections order: claude-code (0), opencode (1), ghcp-cli (2).
	// Navigate down twice to reach ghcp-cli.
	pressHarnessKey(s, tea.KeyDown)
	pressHarnessKey(s, tea.KeyDown)
	pressHarnessRune(s, ' ') // toggle ghcp-cli
	pressHarnessKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Fatal("Done() is false after Enter")
	}
	if !s.HasGHCPCLI() {
		t.Error("HasGHCPCLI() is false after selecting ghcp-cli; expected true")
	}
}

// TestTestHarnessScreen_SelectedHarnesses_ReturnsSelectedIDs verifies that
// SelectedHarnesses() returns the IDs of all toggled-on harnesses in
// declaration order.
func TestTestHarnessScreen_SelectedHarnesses_ReturnsSelectedIDs(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	// Toggle claude-code (index 0).
	pressHarnessRune(s, ' ')
	// Move to opencode (index 1) — skip toggle so only claude-code is checked.
	// Move to ghcp-cli (index 2) and toggle it.
	pressHarnessKey(s, tea.KeyDown)
	pressHarnessKey(s, tea.KeyDown)
	pressHarnessRune(s, ' ')
	pressHarnessKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Fatal("Done() is false after Enter")
	}
	got := s.SelectedHarnesses()
	if len(got) != 2 {
		t.Fatalf("SelectedHarnesses() = %v, want 2 entries", got)
	}
	// Declaration order: claude-code before ghcp-cli.
	if got[0] != "claude-code" {
		t.Errorf("SelectedHarnesses()[0] = %q, want %q", got[0], "claude-code")
	}
	if got[1] != "ghcp-cli" {
		t.Errorf("SelectedHarnesses()[1] = %q, want %q", got[1], "ghcp-cli")
	}
}

// TestTestHarnessScreen_HasGHCPCLIFalse_WhenNoGHCPSelected verifies that
// HasGHCPCLI() is false when only non-ghcp-cli harnesses are selected.
func TestTestHarnessScreen_HasGHCPCLIFalse_WhenNoGHCPSelected(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	// Toggle claude-code and opencode but not ghcp-cli.
	pressHarnessRune(s, ' ') // toggle claude-code
	pressHarnessKey(s, tea.KeyDown)
	pressHarnessRune(s, ' ') // toggle opencode
	pressHarnessKey(s, tea.KeyEnter)

	if !s.Done() {
		t.Fatal("Done() is false after Enter")
	}
	if s.HasGHCPCLI() {
		t.Error("HasGHCPCLI() is true with only claude-code and opencode selected; expected false")
	}
}

// TestTestHarnessScreen_Reset_ClearsState verifies that Reset() clears Done
// and Back so the screen can be re-entered cleanly.
func TestTestHarnessScreen_Reset_ClearsState(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	pressHarnessKey(s, tea.KeyEsc)
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

// TestTestHarnessScreen_Resize_DoesNotPanic verifies that Resize does not
// panic when called with a new terminal size.
func TestTestHarnessScreen_Resize_DoesNotPanic(t *testing.T) {
	s := screens.NewTestHarnessScreen(80, 24, screens.Styles{})
	s.Resize(120, 40)
}
