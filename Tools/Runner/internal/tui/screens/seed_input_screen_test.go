package screens_test

// seed_input_screen_test.go verifies the SeedInputScreen's own behaviour in isolation:
// value capture, blank acceptance, Esc-back, Reset, non-empty view, and resize safety.
//
// Tests drive the screen directly through tea.Msg key events with no root model attached.
// Because SeedInputScreen holds no filesystem dependency, no temp directories are needed.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// newSeedScreen creates a SeedInputScreen for testing with a zero-value Styles
// (produces unstyled but non-nil lipgloss renders, which is sufficient for behaviour tests).
func newSeedScreen() *screens.SeedInputScreen {
	return screens.NewSeedInputScreen(80, 24, screens.Styles{})
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// TestSeedInputScreen_ViewNonEmpty verifies that View() returns a non-empty string
// so the terminal shows meaningful content when the screen is active.
func TestSeedInputScreen_ViewNonEmpty(t *testing.T) {
	s := newSeedScreen()
	view := s.View()
	if view == "" {
		t.Error("View() returned empty string; want non-empty output")
	}
}

// TestSeedInputScreen_ViewContainsTitleIdentifier verifies that View() includes
// some stable identifier for the seed-input screen's purpose so the user knows
// what the screen is asking for. The exact wording is not asserted (prose is not
// a contract per design), but the rendered output must contain a stable title.
func TestSeedInputScreen_ViewContainsTitleIdentifier(t *testing.T) {
	s := newSeedScreen()
	view := s.View()
	if !seedContainsAny(view, "Seed", "seed") {
		t.Errorf("View() does not contain a seed-related title identifier:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Confirm (Enter) with typed path
// ---------------------------------------------------------------------------

// TestSeedInputScreen_EnterWithPath_DoneTrue verifies that typing a path and pressing
// Enter sets Done() to true.
func TestSeedInputScreen_EnterWithPath_DoneTrue(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/seed/data")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Error("Done() = false after typing a path and pressing Enter; want true")
	}
}

// TestSeedInputScreen_SeedInput_ReturnsEnteredPath verifies that SeedInput() returns
// the path the user typed when Done() is true.
func TestSeedInputScreen_SeedInput_ReturnsEnteredPath(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/seed/data")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput()")
	}
	got := s.SeedInput()
	want := "/seed/data"
	if got != want {
		t.Errorf("SeedInput() = %q, want %q", got, want)
	}
}

// TestSeedInputScreen_SeedInput_WhitespaceTrimmed verifies that SeedInput() trims
// leading and trailing whitespace from the entered value.
func TestSeedInputScreen_SeedInput_WhitespaceTrimmed(t *testing.T) {
	s := newSeedScreen()
	// Leading space, path, trailing space.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  /seed/dir  ")})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() trimming")
	}
	got := s.SeedInput()
	want := "/seed/dir"
	if got != want {
		t.Errorf("SeedInput() = %q, want %q (whitespace must be trimmed)", got, want)
	}
}

// ---------------------------------------------------------------------------
// Confirm (Enter) on empty — optional field accepts blank
// ---------------------------------------------------------------------------

// TestSeedInputScreen_EnterOnEmpty_DoneTrue verifies that pressing Enter on an empty
// field sets Done() to true. The seed path is optional; a blank confirmation is legal.
func TestSeedInputScreen_EnterOnEmpty_DoneTrue(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Error("Done() = false after pressing Enter on empty field; blank is a legal confirmation (field is optional)")
	}
}

// TestSeedInputScreen_SeedInput_EmptyString_WhenBlankConfirmed verifies that SeedInput()
// returns the empty string when the user confirmed without entering a path.
func TestSeedInputScreen_SeedInput_EmptyString_WhenBlankConfirmed(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() for blank confirmation")
	}
	got := s.SeedInput()
	if got != "" {
		t.Errorf("SeedInput() = %q after blank confirmation, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// Back (Esc)
// ---------------------------------------------------------------------------

// TestSeedInputScreen_Esc_BackTrue verifies that pressing Esc sets Back() to true.
func TestSeedInputScreen_Esc_BackTrue(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Error("Back() = false after pressing Esc; want true")
	}
}

// TestSeedInputScreen_Esc_DoneNotSet verifies that pressing Esc does not
// accidentally set Done().
func TestSeedInputScreen_Esc_DoneNotSet(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Done() {
		t.Error("Done() = true after pressing Esc; want false")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestSeedInputScreen_Reset_ClearsDone verifies that Reset() clears the Done flag
// so re-entering the screen does not immediately auto-advance.
func TestSeedInputScreen_Reset_ClearsDone(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false after Enter; cannot test Reset()")
	}
	s.Reset()
	if s.Done() {
		t.Error("Done() = true after Reset(); want false — stale Done flag must be cleared before re-entry")
	}
}

// TestSeedInputScreen_Reset_ClearsBack verifies that Reset() clears the Back flag
// so re-entering the screen does not immediately auto-return.
func TestSeedInputScreen_Reset_ClearsBack(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !s.Back() {
		t.Fatal("precondition: Back() = false after Esc; cannot test Reset()")
	}
	s.Reset()
	if s.Back() {
		t.Error("Back() = true after Reset(); want false — stale Back flag must be cleared before re-entry")
	}
}

// ---------------------------------------------------------------------------
// InputInit
// ---------------------------------------------------------------------------

// TestSeedInputScreen_InputInit_ReturnsNonNilCmd verifies that InputInit() returns
// a non-nil tea.Cmd so the root model can start cursor blinking when the screen is entered.
func TestSeedInputScreen_InputInit_ReturnsNonNilCmd(t *testing.T) {
	s := newSeedScreen()
	cmd := s.InputInit()
	if cmd == nil {
		t.Error("InputInit() = nil; want a non-nil tea.Cmd (cursor blink initialisation)")
	}
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

// TestSeedInputScreen_Resize_DoesNotPanic verifies that Resize() accepts new dimensions
// without panicking, so window-resize messages are safe.
func TestSeedInputScreen_Resize_DoesNotPanic(t *testing.T) {
	s := newSeedScreen()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize(120, 40) panicked: %v", r)
		}
	}()
	s.Resize(120, 40)
}

// ---------------------------------------------------------------------------
// SeedInput() — double-quote stripping
//
// These tests verify that SeedInput() strips a surrounding pair of double-quote
// characters after whitespace trimming, matching the normalizePath convention.
// Single quotes must NOT be stripped.
//
// TDD RED: SeedInput() currently returns strings.TrimSpace(s.input.Value()) with
// no quote stripping. All assertions below will fail until I3.2 applies normalizePath.
// ---------------------------------------------------------------------------

// TestSeedInputScreen_SeedInput_DoubleQuoteStripped verifies that SeedInput() strips
// a surrounding pair of double-quote characters from the entered path.
func TestSeedInputScreen_SeedInput_DoubleQuoteStripped(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(`"/seed/data"`)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() quote stripping")
	}
	got := s.SeedInput()
	want := "/seed/data"
	if got != want {
		t.Errorf("SeedInput() = %q, want %q; surrounding double quotes must be stripped", got, want)
	}
}

// TestSeedInputScreen_SeedInput_WhitespacePaddedDoubleQuote_TrimmedAndStripped verifies
// that SeedInput() trims whitespace first and then strips the surrounding double-quote
// pair, matching the normalizePath ordering (trim whitespace, then strip outer quotes).
func TestSeedInputScreen_SeedInput_WhitespacePaddedDoubleQuote_TrimmedAndStripped(t *testing.T) {
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(`  "/seed/data"  `)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() whitespace+quote normalisation")
	}
	got := s.SeedInput()
	want := "/seed/data"
	if got != want {
		t.Errorf("SeedInput() = %q, want %q; whitespace must be trimmed and outer double quotes stripped", got, want)
	}
}

// TestSeedInputScreen_SeedInput_SingleQuote_NotStripped verifies that SeedInput() does
// NOT strip surrounding single quotes. Single-quote stripping is not part of the
// normalization convention.
func TestSeedInputScreen_SeedInput_SingleQuote_NotStripped(t *testing.T) {
	const entered = `'/seed/data'`
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(entered)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() single-quote preservation")
	}
	got := s.SeedInput()
	want := entered
	if got != want {
		t.Errorf("SeedInput() = %q, want %q; single quotes must NOT be stripped", got, want)
	}
}

// TestSeedInputScreen_SeedInput_MismatchedQuotes_NotStripped verifies that SeedInput()
// does not strip a mismatched quote pair (leading double, trailing single).
func TestSeedInputScreen_SeedInput_MismatchedQuotes_NotStripped(t *testing.T) {
	const entered = `"/seed/data'`
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(entered)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() mismatched-quote handling")
	}
	got := s.SeedInput()
	want := entered
	if got != want {
		t.Errorf("SeedInput() = %q, want %q; mismatched quotes must not be stripped", got, want)
	}
}

// TestSeedInputScreen_SeedInput_OneSidedLeadingDoubleQuote_NotStripped verifies that
// SeedInput() does not strip an unmatched leading double quote.
func TestSeedInputScreen_SeedInput_OneSidedLeadingDoubleQuote_NotStripped(t *testing.T) {
	const entered = `"/seed/data`
	s := newSeedScreen()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(entered)})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() = false; cannot check SeedInput() one-sided quote handling")
	}
	got := s.SeedInput()
	want := entered
	if got != want {
		t.Errorf("SeedInput() = %q, want %q; an unmatched leading double quote must not be stripped", got, want)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func seedContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if seedContainsStr(s, sub) {
			return true
		}
	}
	return false
}

func seedContainsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
