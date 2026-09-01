package screens_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// GHCPCLIModeScreen — mode selection and navigation
// ---------------------------------------------------------------------------

// TestGHCPCLIModeScreen_DefaultSelectionIsBlanket verifies that the cursor
// starts on Blanket (the first option) when the screen is constructed.
func TestGHCPCLIModeScreen_DefaultSelectionIsBlanket(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	// No keys pressed; Done() must be false and Mode() should be the default.
	if s.Done() {
		t.Fatal("Done() is true before any key is pressed; screen must start in pending state")
	}
	if s.Back() {
		t.Fatal("Back() is true before any key is pressed")
	}
}

// TestGHCPCLIModeScreen_EnterOnFirstOptionYieldsBlanket verifies that pressing
// Enter on the default (first) selection produces GHCPCLIModeBlanket.
func TestGHCPCLIModeScreen_EnterOnFirstOptionYieldsBlanket(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("Done() is false after Enter; expected true")
	}
	if got := s.Mode(); got != screens.GHCPCLIModeBlanket {
		t.Errorf("Mode() = %q after Enter on default position; want %q", got, screens.GHCPCLIModeBlanket)
	}
}

// TestGHCPCLIModeScreen_DownThenEnterYieldsAllowlist verifies that pressing
// Down then Enter selects GHCPCLIModeAllowlist (the second option).
func TestGHCPCLIModeScreen_DownThenEnterYieldsAllowlist(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("Done() is false after Down+Enter; expected true")
	}
	if got := s.Mode(); got != screens.GHCPCLIModeAllowlist {
		t.Errorf("Mode() = %q after Down+Enter; want %q", got, screens.GHCPCLIModeAllowlist)
	}
}

// TestGHCPCLIModeScreen_JKeyNavigatesDown verifies that "j" (vim-style) also
// moves the cursor down, mirroring the stop screen pattern.
func TestGHCPCLIModeScreen_JKeyNavigatesDown(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := s.Mode(); got != screens.GHCPCLIModeAllowlist {
		t.Errorf("Mode() = %q after j+Enter; want %q", got, screens.GHCPCLIModeAllowlist)
	}
}

// TestGHCPCLIModeScreen_KKeyNavigatesUp verifies that "k" moves the cursor up
// (no-op at position 0 but verifies no panic or wrap-around).
func TestGHCPCLIModeScreen_KKeyNavigatesUp(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}) // already at top
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := s.Mode(); got != screens.GHCPCLIModeBlanket {
		t.Errorf("Mode() = %q after k+Enter from top; want %q (no wrap-around)", got, screens.GHCPCLIModeBlanket)
	}
}

// TestGHCPCLIModeScreen_DownThenUpRestoresFirst verifies that Down followed by
// Up brings the cursor back to the first option.
func TestGHCPCLIModeScreen_DownThenUpRestoresFirst(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := s.Mode(); got != screens.GHCPCLIModeBlanket {
		t.Errorf("Mode() = %q after Down+Up+Enter; want %q", got, screens.GHCPCLIModeBlanket)
	}
}

// TestGHCPCLIModeScreen_EscSetsBack verifies that pressing Esc sets Back() to
// true (navigation back to the previous screen).
func TestGHCPCLIModeScreen_EscSetsBack(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Fatal("Back() is false after Esc; expected true")
	}
	if s.Done() {
		t.Fatal("Done() is true after Esc; expected false")
	}
}

// TestGHCPCLIModeScreen_ResetClearsState verifies that Reset() clears Done,
// Back, and cursor so the screen can be re-entered cleanly.
func TestGHCPCLIModeScreen_ResetClearsState(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("expected Done() before Reset")
	}
	s.Reset()
	if s.Done() {
		t.Error("Done() is true after Reset")
	}
	if s.Back() {
		t.Error("Back() is true after Reset")
	}
	// After Reset, cursor returns to first option.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := s.Mode(); got != screens.GHCPCLIModeBlanket {
		t.Errorf("Mode() = %q after Reset+Enter; want %q (cursor must reset to first option)", got, screens.GHCPCLIModeBlanket)
	}
}

// TestGHCPCLIModeScreen_ViewContainsBothOptionLabels verifies that View()
// renders the labels for both Blanket and Partial Allowlist.
func TestGHCPCLIModeScreen_ViewContainsBothOptionLabels(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	view := s.View()
	if !strings.Contains(view, "Blanket") {
		t.Error("View() does not contain 'Blanket'; both options must be visible")
	}
	if !strings.Contains(view, "Partial Allowlist") {
		t.Error("View() does not contain 'Partial Allowlist'; both options must be visible")
	}
}

// TestGHCPCLIModeScreen_ViewContainsAgentSkillWarning verifies that View()
// includes the warning about agent/skill tools having no gating effect.
func TestGHCPCLIModeScreen_ViewContainsAgentSkillWarning(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	view := s.View()
	if !strings.Contains(view, "agent/skill") {
		t.Error("View() does not contain 'agent/skill'; the warning about agent/skill limitations must be present")
	}
}

// TestGHCPCLIModeScreen_ViewContainsReadSearchWarning verifies that View()
// includes the warning about read/search agents being unaffected by mode choice.
func TestGHCPCLIModeScreen_ViewContainsReadSearchWarning(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	view := s.View()
	if !strings.Contains(view, "read/search") {
		t.Error("View() does not contain 'read/search'; the warning about read/search auto-allowing must be present")
	}
}

// TestGHCPCLIModeScreen_ViewIsNonEmpty verifies that View() produces a
// non-empty string (basic sanity check).
func TestGHCPCLIModeScreen_ViewIsNonEmpty(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	if s.View() == "" {
		t.Error("View() returned empty string; the mode selection screen must render content")
	}
}

// TestGHCPCLIModeScreen_ResizeDoesNotPanic verifies that Resize does not panic.
func TestGHCPCLIModeScreen_ResizeDoesNotPanic(t *testing.T) {
	s := screens.NewGHCPCLIModeScreen(80, 24, screens.Styles{})
	s.Resize(120, 40) // must not panic
}

// TestGHCPCLIModeBlanket_StringValue verifies the sentinel value for blanket mode.
func TestGHCPCLIModeBlanket_StringValue(t *testing.T) {
	if screens.GHCPCLIModeBlanket != "blanket" {
		t.Errorf("GHCPCLIModeBlanket = %q; want %q", screens.GHCPCLIModeBlanket, "blanket")
	}
}

// TestGHCPCLIModeAllowlist_StringValue verifies the sentinel value for allowlist mode.
func TestGHCPCLIModeAllowlist_StringValue(t *testing.T) {
	if screens.GHCPCLIModeAllowlist != "allowlist" {
		t.Errorf("GHCPCLIModeAllowlist = %q; want %q", screens.GHCPCLIModeAllowlist, "allowlist")
	}
}
