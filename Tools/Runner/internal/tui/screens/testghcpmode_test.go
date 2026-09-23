package screens_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// TestGHCPModeScreen — permission mode picker
// ---------------------------------------------------------------------------

// pressGHCPModeKey sends a single named key to the TestGHCPModeScreen.
func pressGHCPModeKey(s *screens.TestGHCPModeScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

// TestTestGHCPModeScreen_InitialState verifies that a freshly created
// TestGHCPModeScreen starts with Done() and Back() both false and that the
// default selection is "blanket" (cursor 0).
func TestTestGHCPModeScreen_InitialState(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	if s.Done() {
		t.Error("Done() is true on construction; screen must start in pending state")
	}
	if s.Back() {
		t.Error("Back() is true on construction")
	}
	if got := s.Value(); got != "blanket" {
		t.Errorf("Value() = %q on construction, want %q (blanket is the default)", got, "blanket")
	}
}

// TestTestGHCPModeScreen_SelectBlanket_ValueIsBlanket verifies that pressing
// Enter without moving the cursor selects "blanket" and sets Done().
func TestTestGHCPModeScreen_SelectBlanket_ValueIsBlanket(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	pressGHCPModeKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after Enter; expected true")
	}
	if s.Back() {
		t.Error("Back() is true after Enter; expected false")
	}
	if got := s.Value(); got != "blanket" {
		t.Errorf("Value() = %q after selecting blanket, want %q", got, "blanket")
	}
}

// TestTestGHCPModeScreen_NavigateToAllowlist_SelectIt_ValueIsAllowlist verifies
// that navigating down to "allowlist" and pressing Enter sets Done() and returns
// Value() == "allowlist".
func TestTestGHCPModeScreen_NavigateToAllowlist_SelectIt_ValueIsAllowlist(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	// Move cursor down from blanket (0) to allowlist (1).
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	pressGHCPModeKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after selecting allowlist; expected true")
	}
	if got := s.Value(); got != "allowlist" {
		t.Errorf("Value() = %q after selecting allowlist, want %q", got, "allowlist")
	}
}

// TestTestGHCPModeScreen_DownKey_SelectAllowlist verifies that the down-arrow
// key also moves the cursor to "allowlist".
func TestTestGHCPModeScreen_DownKey_SelectAllowlist(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	pressGHCPModeKey(s, tea.KeyDown)
	pressGHCPModeKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("Done() is false after selecting allowlist via down-arrow; expected true")
	}
	if got := s.Value(); got != "allowlist" {
		t.Errorf("Value() = %q, want %q", got, "allowlist")
	}
}

// TestTestGHCPModeScreen_EscSetsBack verifies that pressing Esc sets Back() to
// true without setting Done().
func TestTestGHCPModeScreen_EscSetsBack(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	pressGHCPModeKey(s, tea.KeyEsc)
	if !s.Back() {
		t.Error("Back() is false after Esc; expected true")
	}
	if s.Done() {
		t.Error("Done() is true after Esc; expected false")
	}
}

// TestTestGHCPModeScreen_Reset_ClearsState verifies that Reset() clears Done
// and Back and resets the cursor to the default (blanket).
func TestTestGHCPModeScreen_Reset_ClearsState(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	// Navigate to allowlist and confirm.
	pressGHCPModeKey(s, tea.KeyDown)
	pressGHCPModeKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after selecting allowlist")
	}
	s.Reset()
	if s.Done() {
		t.Error("Done() is true after Reset; expected false")
	}
	if s.Back() {
		t.Error("Back() is true after Reset; expected false")
	}
	// After reset, the cursor returns to 0, so Value() should be "blanket".
	if got := s.Value(); got != "blanket" {
		t.Errorf("Value() = %q after Reset, want %q (cursor must reset to blanket)", got, "blanket")
	}
}

// TestTestGHCPModeScreen_Resize_DoesNotPanic verifies that Resize does not
// panic when called with a new terminal size.
func TestTestGHCPModeScreen_Resize_DoesNotPanic(t *testing.T) {
	s := screens.NewTestGHCPModeScreen(80, 24, screens.Styles{})
	s.Resize(120, 40)
}
