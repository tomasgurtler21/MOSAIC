package screens

// setup_test.go verifies the orchestrator file path normalisation behavior:
// quote stripping from Windows "Copy as path" output, whitespace trimming,
// and the guarantee that validateOrchestratorFile and FilePath() apply identical
// normalisation for every raw input they receive.
//
// Tests are in package screens (not screens_test) so they can call the unexported
// validateOrchestratorFile directly. FilePath() is an exported method on
// *OrchestratorFileScreen and is equally accessible here.
//
// TDD RED: tests that assert quote-stripping behavior will fail until the shared
// normalisation helper is added (Implementation tasks I1.1-I1.3). Tests that
// assert pre-existing behavior (empty rejection, unquoted paths, one-sided quotes
// left intact) are included to guard against regressions during implementation.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newOrchestratorScreen creates an OrchestratorFileScreen for testing with zero-value
// Styles (produces unstyled but non-nil lipgloss renders, sufficient for behaviour tests).
func newOrchestratorScreen() *OrchestratorFileScreen {
	return NewOrchestratorFileScreen(80, 24, Styles{})
}

// typeInput sends the given text into the screen as a single tea.KeyRunes event,
// matching the way the bubbletea test suite simulates typed or pasted input.
func typeInput(s *OrchestratorFileScreen, text string) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

// makeTempFile creates a temporary directory and a file named "orch.md" inside it.
// It returns the absolute path to the file. The directory is cleaned up automatically
// by the test runner via t.Cleanup.
func makeTempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "orch.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("setup: could not create temp file: %v", err)
	}
	f.Close()
	return path
}

// ---------------------------------------------------------------------------
// T1.1 — FilePath() normalisation (pure string, no filesystem required)
//
// These tests drive FilePath() directly (without pressing Enter) to verify the
// normalisation rules as pure string transformations. No real file needs to exist
// because FilePath() reads the input buffer; it does not call os.Stat.
// ---------------------------------------------------------------------------

// TestOrchestratorPath_UnquotedPath_Unchanged verifies that a plain, unquoted path is
// returned by FilePath() exactly as entered (only whitespace is trimmed, no other change).
func TestOrchestratorPath_UnquotedPath_Unchanged(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `C:\a\b.md`)
	got := s.FilePath()
	want := `C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; unquoted path must be returned unchanged", got, want)
	}
}

// TestOrchestratorPath_DoubleQuoted_Stripped verifies that a path surrounded by matching
// double quotes has those outer quotes stripped by FilePath().
// RED: currently FilePath() only trims whitespace and does not strip quotes.
func TestOrchestratorPath_DoubleQuoted_Stripped(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `"C:\a\b.md"`)
	got := s.FilePath()
	want := `C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; surrounding double quotes must be stripped", got, want)
	}
}

// TestOrchestratorPath_SingleQuoted_Stripped verifies that a path surrounded by matching
// single quotes has those outer quotes stripped by FilePath().
// RED: currently FilePath() only trims whitespace and does not strip quotes.
func TestOrchestratorPath_SingleQuoted_Stripped(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `'C:\a\b.md'`)
	got := s.FilePath()
	want := `C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; surrounding single quotes must be stripped", got, want)
	}
}

// TestOrchestratorPath_WhitespacePaddedDoubleQuoted_TrimmedAndStripped verifies that
// surrounding whitespace is trimmed before quote stripping, so a paste that carries
// leading spaces or a trailing newline outside the quotes is fully normalised.
// RED: currently FilePath() only trims whitespace and does not strip quotes.
func TestOrchestratorPath_WhitespacePaddedDoubleQuoted_TrimmedAndStripped(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `  "C:\a\b.md"  `)
	got := s.FilePath()
	want := `C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; whitespace must be trimmed before outer double quotes are stripped", got, want)
	}
}

// TestOrchestratorPath_WhitespacePaddedSingleQuoted_TrimmedAndStripped verifies the same
// whitespace-then-strip ordering for single-quoted input.
// RED: currently FilePath() only trims whitespace and does not strip quotes.
func TestOrchestratorPath_WhitespacePaddedSingleQuoted_TrimmedAndStripped(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `  'C:\a\b.md'  `)
	got := s.FilePath()
	want := `C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; whitespace must be trimmed before outer single quotes are stripped", got, want)
	}
}

// TestOrchestratorPath_OneSidedLeadingDoubleQuote_Unchanged verifies that a path with
// only a leading double quote (no matching closing quote) is not altered beyond
// whitespace trimming.
func TestOrchestratorPath_OneSidedLeadingDoubleQuote_Unchanged(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `"C:\a\b.md`)
	got := s.FilePath()
	want := `"C:\a\b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; an unmatched leading double quote must not be stripped", got, want)
	}
}

// TestOrchestratorPath_OneSidedTrailingDoubleQuote_Unchanged verifies that a path with
// only a trailing double quote (no matching leading quote) is not altered beyond
// whitespace trimming.
func TestOrchestratorPath_OneSidedTrailingDoubleQuote_Unchanged(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `C:\a\b.md"`)
	got := s.FilePath()
	want := `C:\a\b.md"`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; an unmatched trailing double quote must not be stripped", got, want)
	}
}

// TestOrchestratorPath_MismatchedQuotes_Unchanged verifies that a path whose first
// character is a single quote and last is a double quote (mismatched pair) is not
// altered beyond whitespace trimming.
func TestOrchestratorPath_MismatchedQuotes_Unchanged(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `'C:\a\b.md"`)
	got := s.FilePath()
	want := `'C:\a\b.md"`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; a mismatched quote pair must not be stripped", got, want)
	}
}

// TestOrchestratorPath_InteriorDoubleQuotes_OuterPairOnly verifies that only the
// outermost matched pair of double quotes is stripped and interior quotes are left intact.
// RED: currently FilePath() only trims whitespace and does not strip outer quotes.
func TestOrchestratorPath_InteriorDoubleQuotes_OuterPairOnly(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `"C:\a "quoted" b.md"`)
	got := s.FilePath()
	want := `C:\a "quoted" b.md`
	if got != want {
		t.Errorf("FilePath() = %q, want %q; only the outer matched quote pair must be stripped, interior quotes must be preserved", got, want)
	}
}

// TestOrchestratorPath_EmptyAfterQuoteStrip_ResultIsEmpty verifies that two adjacent
// double quotes normalise to an empty string (outer quotes stripped, nothing remains).
// RED: currently FilePath() leaves "" as-is; after implementation, quote stripping reduces it.
func TestOrchestratorPath_EmptyAfterQuoteStrip_ResultIsEmpty(t *testing.T) {
	s := newOrchestratorScreen()
	typeInput(s, `""`)
	got := s.FilePath()
	want := ``
	if got != want {
		t.Errorf("FilePath() = %q, want %q; \"\" must normalise to empty string after outer quotes are stripped", got, want)
	}
}

// ---------------------------------------------------------------------------
// T1.1 — validateOrchestratorFile: empty and whitespace-only inputs rejected
//
// These tests confirm that the validation gate rejects degenerate inputs even
// after normalisation. They exercise both pre-existing behavior (empty rejection)
// and the post-implementation behavior (normalised-to-empty rejection).
// ---------------------------------------------------------------------------

// TestValidateOrchestratorFile_EmptyString_Rejected verifies that an empty path is rejected.
func TestValidateOrchestratorFile_EmptyString_Rejected(t *testing.T) {
	err := validateOrchestratorFile("")
	if err == nil {
		t.Error("validateOrchestratorFile(\"\") = nil, want non-nil error; empty path must be rejected")
	}
}

// TestValidateOrchestratorFile_WhitespaceOnly_Rejected verifies that a whitespace-only
// path is rejected. Whitespace trimming must run before the empty check.
func TestValidateOrchestratorFile_WhitespaceOnly_Rejected(t *testing.T) {
	err := validateOrchestratorFile("   ")
	if err == nil {
		t.Error("validateOrchestratorFile(\"   \") = nil, want non-nil error; whitespace-only input must be rejected after trimming")
	}
}

// TestValidateOrchestratorFile_EmptyDoubleQuotes_Rejected verifies that the input "" (two
// adjacent double quotes) is rejected. After normalisation the path is empty, so the
// empty check must fire before os.Stat is called.
// RED: currently validateOrchestratorFile does not strip quotes, so "" falls through to
// os.Stat which fails — the test PASSES currently for the wrong reason. After implementation
// the empty check fires first, before os.Stat, and the error is returned earlier.
// The assertion (non-nil error) is correct in both states.
func TestValidateOrchestratorFile_EmptyDoubleQuotes_Rejected(t *testing.T) {
	err := validateOrchestratorFile(`""`)
	if err == nil {
		t.Error(`validateOrchestratorFile("\"\"") = nil, want non-nil error; "" normalises to empty string and must be rejected`)
	}
}

// TestValidateOrchestratorFile_DoubleQuotedExistingFile_Accepted verifies that a
// double-quoted path pointing at an existing file passes validation.
// RED: currently validateOrchestratorFile does not strip quotes, so os.Stat is called
// with the quotes as part of the path, which does not exist, and an error is returned.
func TestValidateOrchestratorFile_DoubleQuotedExistingFile_Accepted(t *testing.T) {
	realPath := makeTempFile(t)
	quoted := `"` + realPath + `"`
	if err := validateOrchestratorFile(quoted); err != nil {
		t.Errorf("validateOrchestratorFile(%q) = %v, want nil; double-quoted path to existing file must pass validation after quote stripping", quoted, err)
	}
}

// TestValidateOrchestratorFile_SingleQuotedExistingFile_Accepted verifies that a
// single-quoted path pointing at an existing file passes validation.
// RED: same as above — quotes are currently not stripped before os.Stat.
func TestValidateOrchestratorFile_SingleQuotedExistingFile_Accepted(t *testing.T) {
	realPath := makeTempFile(t)
	quoted := `'` + realPath + `'`
	if err := validateOrchestratorFile(quoted); err != nil {
		t.Errorf("validateOrchestratorFile(%q) = %v, want nil; single-quoted path to existing file must pass validation after quote stripping", quoted, err)
	}
}

// TestValidateOrchestratorFile_WhitespacePaddedDoubleQuoted_Accepted verifies that a
// double-quoted path surrounded by whitespace passes validation.
// RED: quotes are not currently stripped before os.Stat.
func TestValidateOrchestratorFile_WhitespacePaddedDoubleQuoted_Accepted(t *testing.T) {
	realPath := makeTempFile(t)
	padded := `  "` + realPath + `"  `
	if err := validateOrchestratorFile(padded); err != nil {
		t.Errorf("validateOrchestratorFile(%q) = %v, want nil; whitespace-padded double-quoted existing path must pass validation", padded, err)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Normalisation equivalence: validateOrchestratorFile and FilePath() agree
//
// These tests assert the central contract: for any raw input, the path that
// validation accepts is byte-identical to the path that FilePath() returns.
// Both must route through the same shared normalisation helper (I1.1-I1.3).
// ---------------------------------------------------------------------------

// TestOrchestratorPath_NormalisationEquivalence_DoubleQuoted verifies that for a
// double-quoted path to an existing file:
//   - validateOrchestratorFile accepts the raw quoted input
//   - FilePath() returns the same quote-free path that validation accepted
//
// RED: validateOrchestratorFile currently rejects the quoted input, causing t.Fatalf.
func TestOrchestratorPath_NormalisationEquivalence_DoubleQuoted(t *testing.T) {
	realPath := makeTempFile(t)
	rawInput := `"` + realPath + `"`

	if err := validateOrchestratorFile(rawInput); err != nil {
		t.Fatalf("validateOrchestratorFile(%q) = %v, want nil; prerequisite: quoted path to existing file must pass validation", rawInput, err)
	}

	s := newOrchestratorScreen()
	typeInput(s, rawInput)
	got := s.FilePath()
	if got != realPath {
		t.Errorf("FilePath() = %q, want %q; FilePath() must return the same normalised path that validateOrchestratorFile accepted", got, realPath)
	}
}

// TestOrchestratorPath_NormalisationEquivalence_SingleQuoted verifies the same
// equivalence for a single-quoted path to an existing file.
// RED: same as above.
func TestOrchestratorPath_NormalisationEquivalence_SingleQuoted(t *testing.T) {
	realPath := makeTempFile(t)
	rawInput := `'` + realPath + `'`

	if err := validateOrchestratorFile(rawInput); err != nil {
		t.Fatalf("validateOrchestratorFile(%q) = %v, want nil; prerequisite: single-quoted path to existing file must pass validation", rawInput, err)
	}

	s := newOrchestratorScreen()
	typeInput(s, rawInput)
	got := s.FilePath()
	if got != realPath {
		t.Errorf("FilePath() = %q, want %q; FilePath() must return the same normalised path that validateOrchestratorFile accepted", got, realPath)
	}
}

// TestOrchestratorPath_NormalisationEquivalence_WhitespacePadded verifies equivalence
// for an input with leading and trailing whitespace around double quotes, matching a
// typical "Copy as path" paste on Windows that may carry surrounding spaces.
// RED: validateOrchestratorFile currently rejects the quoted input.
func TestOrchestratorPath_NormalisationEquivalence_WhitespacePadded(t *testing.T) {
	realPath := makeTempFile(t)
	rawInput := `  "` + realPath + `"  `

	if err := validateOrchestratorFile(rawInput); err != nil {
		t.Fatalf("validateOrchestratorFile(%q) = %v, want nil; prerequisite: whitespace-padded double-quoted path must pass validation", rawInput, err)
	}

	s := newOrchestratorScreen()
	typeInput(s, rawInput)
	got := s.FilePath()
	if got != realPath {
		t.Errorf("FilePath() = %q, want %q; FilePath() must return the same normalised path that validateOrchestratorFile accepted", got, realPath)
	}
}

// TestOrchestratorPath_NormalisationEquivalence_Unquoted verifies that for an unquoted
// path to a real file, both validateOrchestratorFile and FilePath() return the path
// unchanged. This is an existing-behavior regression guard.
func TestOrchestratorPath_NormalisationEquivalence_Unquoted(t *testing.T) {
	realPath := makeTempFile(t)

	if err := validateOrchestratorFile(realPath); err != nil {
		t.Fatalf("validateOrchestratorFile(%q) = %v, want nil; prerequisite: unquoted existing path must pass validation", realPath, err)
	}

	s := newOrchestratorScreen()
	typeInput(s, realPath)
	got := s.FilePath()
	if got != realPath {
		t.Errorf("FilePath() = %q, want %q; unquoted path must be returned unchanged", got, realPath)
	}
}

// TestOrchestratorPath_NormalisationEquivalence_OneSidedLeadingQuote verifies that for
// a one-sided (unmatched) leading quote, neither validate nor FilePath() strips it.
//   - validateOrchestratorFile must reject the input (the quoted path does not exist on disk).
//   - FilePath() must return the input with the leading quote intact.
//
// This is a regression guard: both sites must agree to leave unmatched quotes alone.
func TestOrchestratorPath_NormalisationEquivalence_OneSidedLeadingQuote(t *testing.T) {
	realPath := makeTempFile(t)
	rawInput := `"` + realPath // leading quote only — no closing quote

	// The file with a leading quote in its name does not exist; validation must reject.
	err := validateOrchestratorFile(rawInput)
	if err == nil {
		t.Fatalf("validateOrchestratorFile(%q) = nil, want non-nil error; one-sided quoted path must not pass validation", rawInput)
	}

	// FilePath() must return the raw input unchanged (leading quote is unmatched, kept).
	s := newOrchestratorScreen()
	typeInput(s, rawInput)
	got := s.FilePath()
	if got != rawInput {
		t.Errorf("FilePath() = %q, want %q; unmatched leading quote must be left intact by FilePath()", got, rawInput)
	}
}

// TestOrchestratorPath_NormalisationEquivalence_OneSidedTrailingQuote verifies the same
// invariant for a one-sided trailing quote.
func TestOrchestratorPath_NormalisationEquivalence_OneSidedTrailingQuote(t *testing.T) {
	realPath := makeTempFile(t)
	rawInput := realPath + `"` // trailing quote only — no opening quote

	err := validateOrchestratorFile(rawInput)
	if err == nil {
		t.Fatalf("validateOrchestratorFile(%q) = nil, want non-nil error; one-sided trailing quote path must not pass validation", rawInput)
	}

	s := newOrchestratorScreen()
	typeInput(s, rawInput)
	got := s.FilePath()
	if got != rawInput {
		t.Errorf("FilePath() = %q, want %q; unmatched trailing quote must be left intact by FilePath()", got, rawInput)
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen — harness step (T4.5)
//
// These tests exercise ConfigScreen's harness configuration step, which
// currently renders exactly one hard-coded option ("Claude Code CLI") and
// assigns the literal "claude-code" regardless of cursor position
// (ConfigSelection.Harness's own doc comment states as much). They are RED
// until the step is reworked (I4.6) to enumerate harness.CLISelections() and
// assign the identity the cursor actually rests on.
//
// Tests are in package screens (not screens_test) so they can inspect
// ConfigScreen's unexported step/cursor fields directly, the same access
// pattern validateOrchestratorFile testing above relies on.
// ---------------------------------------------------------------------------

// pressKey sends a single key press to the screen's Update method.
func pressKey(s *ConfigScreen, keyType tea.KeyType) tea.Cmd {
	return s.Update(tea.KeyMsg{Type: keyType})
}

// newConfigScreenAtHarnessStep creates a ConfigScreen positioned at the
// harness step by driving past the mode step (which is now the first step).
// It selects the first mode option (orchestrated) so harness is the current step.
func newConfigScreenAtHarnessStep() *ConfigScreen {
	s := NewConfigScreen(80, 24, Styles{})
	driveConfigScreenModeSelect(s, 0) // advance past mode step; harness is now current
	return s
}

// TestConfigScreen_HarnessStep_OffersOneOptionPerAcceptedHarness verifies
// that the rendered view lists exactly one option per
// harness.CLISelections() entry, using each entry's Label — not a single
// hard-coded "Claude Code CLI" line.
func TestConfigScreen_HarnessStep_OffersOneOptionPerAcceptedHarness(t *testing.T) {
	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the mode step is not first, so driveConfigScreenModeSelect
	// inside the helper accidentally advances the harness step (selecting the second
	// harness option) and lands at the timeout step. Skip until I8.1 makes mode first.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	view := s.View()

	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections to distinguish from the old single-option render, got %d", len(sels))
	}
	for _, sel := range sels {
		if !strings.Contains(view, sel.Label) {
			t.Errorf("harness step view does not contain label %q for accepted harness %q; view:\n%s", sel.Label, sel.ID, view)
		}
	}
}

// TestConfigScreen_HarnessStep_SelectionAssignsCursorIdentity verifies that
// pressing Enter on the harness step assigns the identity the cursor
// actually rests on (harness.CLISelections()[cursor].ID), not a fixed
// literal. It moves the cursor down once, so unless there are at least 2
// CLI-backed selections, cursor movement would be clamped and this test
// would not distinguish the fixed-literal bug from a correct implementation.
func TestConfigScreen_HarnessStep_SelectionAssignsCursorIdentity(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections, got %d", len(sels))
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the mode step is not first, so driveConfigScreenModeSelect
	// inside the helper accidentally selects a second harness option (position 1) and
	// lands at the timeout step. Without this guard the test would pass coincidentally
	// (the accidentally-selected harness ID matches sels[1].ID), masking the missing
	// I4.6 implementation. Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step; this test cannot be verified until the mode step is the first step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	pressKey(s, tea.KeyDown) // move cursor to the second entry
	pressKey(s, tea.KeyEnter)

	want := sels[1].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q, want %q (the identity the cursor rested on, not a fixed literal)", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_FirstOptionSelection verifies the cursor-0
// case explicitly: selecting without moving the cursor assigns
// CLISelections()[0].ID.
func TestConfigScreen_HarnessStep_FirstOptionSelection(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) == 0 {
		t.Fatalf("test fixture assumption violated: want at least 1 CLI-backed selection, got 0")
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the helper positions at the timeout step, not the harness
	// step; pressing Enter would advance the timeout step, not confirm a harness option.
	// Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	pressKey(s, tea.KeyEnter)

	want := sels[0].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q, want %q", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_CursorBoundsMatchAcceptedSetLength verifies
// that cursor movement is bounded by the number of accepted CLI-backed
// harnesses rather than a hard-coded single option: pressing down
// (len(sels)-1) times should reach the last entry, and one more press must
// not move the cursor further.
func TestConfigScreen_HarnessStep_CursorBoundsMatchAcceptedSetLength(t *testing.T) {
	sels := harness.CLISelections()
	if len(sels) < 2 {
		t.Fatalf("test fixture assumption violated: want at least 2 CLI-backed selections, got %d", len(sels))
	}

	s := newConfigScreenAtHarnessStep()
	// In pre-I8.1 RED state the helper positions at the timeout step, not the harness
	// step. Skip until I8.1 makes mode the first step.
	if s.step != configStepHarness {
		t.Fatalf("I8.1 not yet implemented: mode step not first; harness helper positions at timeout step, not harness step. Do not ship I4.6 verification until I8.1 is complete.")
	}
	for i := 0; i < len(sels)+2; i++ { // deliberately over-press
		pressKey(s, tea.KeyDown)
	}
	pressKey(s, tea.KeyEnter)

	want := sels[len(sels)-1].ID
	if s.sel.Harness != want {
		t.Errorf("sel.Harness = %q after over-pressing down, want %q (cursor must clamp at the last accepted entry)", s.sel.Harness, want)
	}
}

// TestConfigScreen_HarnessStep_EscSetsBack verifies that pressing Esc on the
// mode step (the first step after I8.1 reorders the wizard) sets the screen's
// Back flag, signalling the caller to navigate away from the configuration
// screen entirely. The mode step replaces harness as the entry point.
func TestConfigScreen_HarnessStep_EscSetsBack(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	// Mode step is the first step; no advance needed.
	pressKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false; Esc on the mode step (the first step) must set the screen's Back flag")
	}
}

// TestConfigScreen_HarnessStep_EnterAdvancesToTimeoutStep verifies that
// pressing Enter on the harness step still advances to the harness-timeout
// step, preserving navigation behaviour once the step enumerates multiple
// options.
func TestConfigScreen_HarnessStep_EnterAdvancesToTimeoutStep(t *testing.T) {
	s := newConfigScreenAtHarnessStep()
	pressKey(s, tea.KeyEnter)

	if s.step != configStepHarnessTimeout {
		t.Errorf("step = %v after Enter on harness step, want configStepHarnessTimeout", s.step)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Mode step tests
//
// The mode step is the first configuration step. It presents three execution
// modes (orchestrated, auto, auto-review) with no preselected default. The
// wizard cannot advance until the user makes an explicit choice.
//
// All tests in this section are RED: the ConfigScreen currently starts at
// configStepHarness, not configStepMode, and nothing populates Settings.Mode.
// ---------------------------------------------------------------------------

// advanceConfigScreenTimeout types a valid timeout ("30m") into the ConfigScreen
// and confirms it. Must be called when the screen is at configStepHarnessTimeout.
func advanceConfigScreenTimeout(s *ConfigScreen) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	pressKey(s, tea.KeyEnter)
}

// driveConfigScreenPastPreModeSteps is a no-op in Stage 8: in GREEN the mode
// step is the first step and there are no steps before it. It exists here so
// that downstream helpers can insert steps before mode if a future stage adds
// them without breaking these tests.
func driveConfigScreenPastPreModeSteps(_ *ConfigScreen) {}

// driveConfigScreenModeSelect drives the mode step by moving the cursor to the
// given index (0=orchestrated, 1=auto, 2=auto-review) and pressing Enter.
// The mode step starts with no option selected (AC8.3): Enter alone is rejected.
// One Down press moves the cursor to the first option (orchestrated), so idx=0
// requires one Down press, idx=1 requires two, and so on.
func driveConfigScreenModeSelect(s *ConfigScreen, idx int) {
	// The mode step starts with no item selected. One Down press is needed
	// to reach the first item (idx 0); each additional Down moves one further.
	for i := 0; i <= idx; i++ {
		pressKey(s, tea.KeyDown)
	}
	pressKey(s, tea.KeyEnter)
}

// driveConfigScreenPastModeAndHarness drives through mode selection, harness,
// and the timeout step to leave the screen at configStepVersionDrift.
// The mode option at index 0 (orchestrated) is selected.
func driveConfigScreenPastModeAndHarness(s *ConfigScreen) {
	driveConfigScreenModeSelect(s, 0) // select first mode (orchestrated)
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout → version drift
}

// TestConfigScreen_ModeStep_IsFirstStep verifies that a freshly created
// ConfigScreen starts at configStepMode, making mode selection the first
// configuration prompt the user sees.
//
// RED: NewConfigScreen() currently starts at configStepHarness.
func TestConfigScreen_ModeStep_IsFirstStep(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	if s.step != configStepMode {
		t.Errorf("initial step = %v, want configStepMode (%v); the mode step must be the first configuration prompt",
			s.step, configStepMode)
	}
}

// TestConfigScreen_ModeStep_ViewShowsAllThreeModes verifies that the mode step
// renders all three valid execution modes: orchestrated, auto, and auto-review.
//
// RED: the initial view shows the harness step, not mode options.
func TestConfigScreen_ModeStep_ViewShowsAllThreeModes(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	if s.step != configStepMode {
		t.Fatalf("precondition: step = %v, want configStepMode; test cannot proceed", s.step)
	}
	view := s.View()
	for _, mode := range []string{"orchestrated", "auto", "auto-review"} {
		if !containsSubstr(view, mode) {
			t.Errorf("mode step view does not contain %q; all three execution modes must be shown:\n%s", mode, view)
		}
	}
}

// TestConfigScreen_ModeStep_NoPreselection verifies that the mode step begins
// with no option highlighted as a default — the cursor is logically unset so
// the user must make an explicit choice before the step advances.
//
// RED: the mode step does not exist yet; the screen starts at the harness step
// which always has option 0 preselected.
func TestConfigScreen_ModeStep_NoPreselection(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	if s.step != configStepMode {
		t.Fatalf("precondition: step = %v, want configStepMode; test cannot proceed", s.step)
	}
	// Pressing Enter immediately (no cursor movement) must not advance the step —
	// the step requires an explicit choice, no option is preselected.
	pressKey(s, tea.KeyEnter)
	if s.step != configStepMode {
		t.Errorf("step = %v after Enter with no cursor movement, want configStepMode; "+
			"the mode step must not advance without an explicit selection", s.step)
	}
}

// TestConfigScreen_ModeStep_SelectOrchestrated_ProducesOrchestratedMode verifies
// that selecting the first option on the mode step results in
// Selection().Settings.Mode == ExecutionModeOrchestrated after the wizard completes.
//
// RED: Nothing populates Settings.Mode; it remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectOrchestrated_ProducesOrchestratedMode(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil) // no declared agents — simplest case

	// Drive to done: mode (orchestrated = cursor 0), harness, timeout, version drift, checkpoints,
	// manual-resolution (always shown; orchestrated has no pre-consult step).
	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // manual-resolution (disabled default, no commit agent, no infra class)

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	sel := s.Selection()
	if sel.Settings.Mode != domain.ExecutionModeOrchestrated {
		t.Errorf("Settings.Mode = %v, want %v; selecting the orchestrated option must produce ExecutionModeOrchestrated",
			sel.Settings.Mode, domain.ExecutionModeOrchestrated)
	}
}

// TestConfigScreen_ModeStep_SelectAuto_ProducesAutoMode verifies that selecting
// the second option (auto) results in Settings.Mode == ExecutionModeAuto.
//
// RED: Settings.Mode remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectAuto_ProducesAutoMode(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // pre-consult (shown for auto mode; accept default)
	pressKey(s, tea.KeyEnter)          // manual-resolution (always shown; accept default)

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	if s.Selection().Settings.Mode != domain.ExecutionModeAuto {
		t.Errorf("Settings.Mode = %v, want %v",
			s.Selection().Settings.Mode, domain.ExecutionModeAuto)
	}
}

// TestConfigScreen_ModeStep_SelectAutoReview_ProducesAutoReviewMode verifies
// that selecting the third option (auto-review) results in
// Settings.Mode == ExecutionModeAutoReview.
//
// RED: Settings.Mode remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectAutoReview_ProducesAutoReviewMode(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 2) // auto-review
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // pre-consult (shown for auto-review mode; accept default)
	pressKey(s, tea.KeyEnter)          // manual-resolution (always shown; accept default)

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	if s.Selection().Settings.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Settings.Mode = %v, want %v",
			s.Selection().Settings.Mode, domain.ExecutionModeAutoReview)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Commits step conditional visibility
//
// The commits step is shown only when a Class=commit agent is declared.
// ---------------------------------------------------------------------------

// TestConfigScreen_CommitsStep_NotShown_WhenNoCommitAgentDeclared verifies that
// when no commit-class agent is declared, the commits step is skipped and the
// wizard reaches Done() without ever landing on configStepCommits.
//
// This test is a regression guard: it should pass in both RED and GREEN
// (the commits step is not yet implemented and is also correctly absent when
// no commit agent is declared). After I8.1 is implemented this test still
// passes — configStepCommits is still skipped for zero commit agents.
//
// Note: the always-shown configStepManualResolution step follows checkpoints in
// the orchestrated path; it is driven through here so Done() reflects completion
// of all applicable steps, not an unexpected stop at manual-resolution.
func TestConfigScreen_CommitsStep_NotShown_WhenNoCommitAgentDeclared(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "checkpoint-agent", Class: "checkpoint"}, // only checkpoint, no commit
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// The commits step must not appear when no commit-class agent is declared.
	if s.step == configStepCommits {
		t.Errorf("step = configStepCommits after checkpoints when no commit agent is declared; "+
			"the commits step must be skipped when no commit-class agent exists")
		return
	}

	// The always-shown manual-resolution step follows checkpoints (orchestrated mode,
	// no commit agent). Drive through it with the default selection so Done() reflects
	// completion of all applicable steps.
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // accept default (disabled)
	}

	if !s.Done() {
		t.Errorf("ConfigScreen did not reach Done() after all applicable steps (no commit agent, orchestrated mode); "+
			"current step = %v", s.step)
	}
}

// TestConfigScreen_CommitsStep_Shown_WhenCommitAgentDeclared verifies that
// when a Class=commit agent is declared, the commits step appears after the
// checkpoints step.
//
// RED: advance() after checkpoints goes to infraClass or Done, never to
// configStepCommits. The wizard completes without showing the commits step.
func TestConfigScreen_CommitsStep_Shown_WhenCommitAgentDeclared(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with a commit agent declared, the wizard must show the
	// commits step (configStepCommits), not jump to Done or infraClass.
	if s.Done() {
		t.Error("ConfigScreen reached Done() immediately after checkpoints when a commit agent is declared; " +
			"the commits step must be shown before finishing")
		return
	}
	if s.step != configStepCommits {
		t.Errorf("step = %v after checkpoints with commit agent, want configStepCommits (%v); "+
			"the commits step must be shown when a commit-class agent is declared",
			s.step, configStepCommits)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Commit branch variant step conditional visibility
//
// The commit branch variant step is shown only when commits are enabled.
// ---------------------------------------------------------------------------

// TestConfigScreen_CommitBranchStep_NotShown_WhenCommitsDisabled verifies that
// when commits are disabled at the commits step, the commit branch step is
// skipped.
//
// This test is a regression guard: it should pass in both RED and GREEN.
func TestConfigScreen_CommitBranchStep_NotShown_WhenCommitsDisabled(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step == configStepCommits {
		// Commits step is shown — cursor 0 = Disabled by default, just confirm.
		pressKey(s, tea.KeyEnter) // confirm: commits disabled

		// The commit branch step must not appear.
		if s.step == configStepCommitBranch {
			t.Errorf("step = configStepCommitBranch after selecting commits=disabled; "+
				"the commit branch step must be skipped when commits are disabled")
		}
	}
	// If configStepCommits was never shown (RED state), the regression guard
	// still passes — there is nothing here that can fail in the RED path.
}

// TestConfigScreen_CommitBranchStep_Shown_WhenCommitsEnabled verifies that
// when commits are enabled at the commits step, the commit branch step appears
// next.
//
// RED: configStepCommits is never reached, so this test fails at the
// configStepCommits check.
func TestConfigScreen_CommitBranchStep_Shown_WhenCommitsEnabled(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode (orchestrated)
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepCommits {
		t.Fatalf("step = %v after checkpoints with commit agent, want configStepCommits; "+
			"cannot test branch variant visibility without the commits step", s.step)
	}

	// Select commits=enabled: cursor 0 = Disabled (default), cursor 1 = Enabled.
	pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // select Enabled

	if s.step != configStepCommitBranch {
		t.Errorf("step = %v after commits=enabled, want configStepCommitBranch (%v); "+
			"the branch variant step must appear when commits are enabled",
			s.step, configStepCommitBranch)
	}
}

// TestConfigScreen_CommitBranchStep_MOSAICOwnedIsRecommended verifies that the
// mosaic-owned branch variant option is visually marked as recommended in the
// branch variant step.
//
// RED: configStepCommitBranch is never shown.
func TestConfigScreen_CommitBranchStep_MOSAICOwnedIsRecommended(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0)
	pressKey(s, tea.KeyEnter) // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	if s.step != configStepCommits {
		t.Fatalf("step = %v, want configStepCommits", s.step)
	}
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // commits enabled

	if s.step != configStepCommitBranch {
		t.Fatalf("step = %v, want configStepCommitBranch", s.step)
	}
	view := s.View()
	if !containsSubstr(view, "Recommended") && !containsSubstr(view, "recommended") {
		t.Errorf("commit branch step view does not mark mosaic-owned as recommended; "+
			"the branch variant step must present mosaic-owned as the recommended choice:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Pre-consultation step conditional visibility
//
// The pre-consultation step is shown only when mode is auto or auto-review.
// ---------------------------------------------------------------------------

// TestConfigScreen_PreConsultStep_NotShown_WhenModeOrchestrated verifies that
// the pre-consultation step is skipped when the selected mode is orchestrated.
//
// This test should pass in both RED and GREEN: the pre-consult step does not
// yet exist, and it is correctly absent for the orchestrated mode.
func TestConfigScreen_PreConsultStep_NotShown_WhenModeOrchestrated(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with orchestrated mode, the pre-consult step must NOT appear.
	if s.step == configStepPreConsult {
		t.Error("step = configStepPreConsult after orchestrated mode selection; " +
			"the pre-consultation step must be skipped when mode is orchestrated")
	}
}

// TestConfigScreen_PreConsultStep_Shown_WhenModeAuto verifies that the
// pre-consultation step appears after the checkpoints step when mode is auto.
//
// RED: mode selection does not produce a stored mode value, so the pre-consult
// conditional cannot fire. The step is never shown.
func TestConfigScreen_PreConsultStep_Shown_WhenModeAuto(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with mode=auto, the pre-consult step must appear.
	// In RED the wizard reaches Done() or stays at an unexpected step.
	if s.Done() {
		t.Error("ConfigScreen reached Done() before showing the pre-consultation step; " +
			"the pre-consult step must appear when mode is auto")
		return
	}
	if s.step != configStepPreConsult {
		t.Errorf("step = %v after checkpoints with mode=auto, want configStepPreConsult (%v)",
			s.step, configStepPreConsult)
	}
}

// TestConfigScreen_PreConsultStep_Shown_WhenModeAutoReview verifies that the
// pre-consultation step appears when mode is auto-review.
//
// RED: same as above — the step is never shown.
func TestConfigScreen_PreConsultStep_Shown_WhenModeAutoReview(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 2) // auto-review
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.Done() {
		t.Error("ConfigScreen reached Done() before the pre-consultation step; " +
			"the pre-consult step must appear when mode is auto-review")
		return
	}
	if s.step != configStepPreConsult {
		t.Errorf("step = %v after checkpoints with mode=auto-review, want configStepPreConsult (%v)",
			s.step, configStepPreConsult)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.2: Backward navigation through conditional steps
//
// Conditional steps must be skipped in BOTH navigation directions. When a step
// is not applicable, pressing Esc from the step after it must jump over it and
// land on the step before it.
// ---------------------------------------------------------------------------

// TestConfigScreen_BackNav_CommitBranchSkipped_WhenCommitsNotApplicable verifies
// that pressing Esc from the manual-resolution step lands on the pre-consult
// step (or checkpoints, if pre-consult is also not applicable) when no commit
// agent is declared — commits and commit-branch steps are skipped in both
// navigation directions.
//
// RED: configStepManualResolution is not yet wired into advance(); the test
// cannot reach it to press Esc.
func TestConfigScreen_BackNav_CommitBranchSkipped_WhenCommitsNotApplicable(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	// Drive forward: mode (orchestrated), harness, timeout, version drift, checkpoints,
	// manual resolution (if shown). Since no commit agent is declared, commits and
	// commit-branch steps must be skipped. Pre-consult is also skipped (orchestrated mode).
	driveConfigScreenModeSelect(s, 0)
	pressKey(s, tea.KeyEnter) // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	// After checkpoints without a commit agent and with orchestrated mode,
	// we should be at configStepManualResolution (always shown).
	if s.step != configStepManualResolution {
		t.Fatalf("step = %v after checkpoints (no commit, orchestrated mode), want configStepManualResolution (%v); "+
			"cannot test backward navigation without reaching that step", s.step, configStepManualResolution)
	}

	// Press Esc from manual-resolution — must go back to checkpoints (commits and
	// commit-branch are skipped, pre-consult is skipped for orchestrated mode).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepCheckpoints {
		t.Errorf("Esc from manual-resolution with no commit agent and orchestrated mode: "+
			"step = %v, want configStepCheckpoints (%v); "+
			"skipped conditional steps must not appear in backward navigation",
			s.step, configStepCheckpoints)
	}
}

// TestConfigScreen_BackNav_PreConsultSkipped_WhenModeOrchestrated verifies that
// pressing Esc from the manual-resolution step skips the pre-consultation step
// (not applicable for orchestrated mode) when going backward.
//
// RED: configStepManualResolution is never reached.
func TestConfigScreen_BackNav_PreConsultSkipped_WhenModeOrchestrated(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward skip", s.step)
	}

	// Esc must skip pre-consult (not applicable for orchestrated) and land on checkpoints.
	pressKey(s, tea.KeyEsc)
	if s.step == configStepPreConsult {
		t.Error("Esc from manual-resolution (orchestrated mode) landed on configStepPreConsult; " +
			"the pre-consult step must be skipped in backward navigation when mode is orchestrated")
	}
}

// TestConfigScreen_BackNav_PreConsultStepReachable_WhenModeAuto verifies that
// pressing Esc from the manual-resolution step DOES land on the pre-consult
// step when mode is auto (the step is applicable).
//
// RED: neither manual-resolution nor pre-consult steps are reachable.
func TestConfigScreen_BackNav_PreConsultStepReachable_WhenModeAuto(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	// Skip to manual-resolution: need to advance through pre-consult.
	if s.step == configStepPreConsult {
		pressKey(s, tea.KeyEnter) // accept pre-consult default (disabled)
	}

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward navigation", s.step)
	}

	// Esc must go back to pre-consult (applicable for auto mode).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepPreConsult {
		t.Errorf("Esc from manual-resolution (mode=auto) landed on step %v, want configStepPreConsult (%v); "+
			"Esc must go back to the pre-consult step when mode is auto",
			s.step, configStepPreConsult)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.4: Surface parity tests
//
// The wizard's ConfigSelection.Settings must be identical to what the CLI
// produces from the equivalent flags, so surface parity is a struct comparison.
// ---------------------------------------------------------------------------

// containsSubstr is a package-level helper for string containment checks in
// Stage 8 tests, following the same pattern as the tui-level containsStr.
func containsSubstr(s, sub string) bool {
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

// TestConfigScreen_Parity_OrchestratedMode_SettingsMatchCLIEquivalent verifies
// that completing the wizard with mode=orchestrated, checkpoints=disabled,
// commits=absent, pre-consult=absent, manual-resolution=disabled produces a
// Settings struct equivalent to what the CLI would produce from:
//   --mode orchestrated
//
// RED: Settings remains zero-valued (ExecutionModeUnset for Mode, false for all bools).
func TestConfigScreen_Parity_OrchestratedMode_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	// Drive through all steps with the simplest configuration:
	// mode=orchestrated, no commit agent, all other steps use defaults (disabled/no).
	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift: no drift (default)
	pressKey(s, tea.KeyEnter)          // checkpoints: disabled (default)
	// No commits step (no commit agent).
	// No pre-consult step (orchestrated mode).
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled (default)
	}
	// No infra class step (no multiple same-class agents).

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	// Equivalent CLI configuration for --mode orchestrated (all else default).
	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: domain.CommitBranchMOSAICOwned, // default
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_AutoModeWithPreConsult_SettingsMatchCLIEquivalent
// verifies that selecting mode=auto and enabling pre-consultation produces
// Settings equivalent to:
//   --mode auto --pre-consult
//
// RED: Settings.Mode is ExecutionModeUnset, Settings.PreConsultation is false.
func TestConfigScreen_Parity_AutoModeWithPreConsult_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// Pre-consult step should appear for mode=auto; enable it.
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepPreConsult {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // select Enabled
	}

	// Manual resolution step (if present): leave disabled.
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // disabled (default cursor position)
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeAuto,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		PreConsultation:     true,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v",
			got, wantSettings)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Manual-resolution step
//
// The manual-resolution step is always shown after the pre-consult step
// sequence. It presents enabled and disabled options with disabled as the
// default.
// ---------------------------------------------------------------------------

// TestConfigScreen_ManualResolutionStep_ViewShowsOptions verifies that when the
// wizard reaches configStepManualResolution, the view shows both enabled and
// disabled options so the user can make an explicit choice.
//
// RED: configStepManualResolution is not wired into advance(); the step is
// never reached.
func TestConfigScreen_ManualResolutionStep_ViewShowsOptions(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil) // no commit agent; orchestrated mode skips commits and pre-consult

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v after checkpoints (orchestrated, no commit agent), want configStepManualResolution (%v); "+
			"cannot verify view without reaching the step", s.step, configStepManualResolution)
	}

	view := s.View()
	// The step must present both options so the user can choose.
	if !containsSubstr(view, "enabled") && !containsSubstr(view, "Enabled") {
		t.Errorf("manual-resolution step view does not contain an enabled option; view:\n%s", view)
	}
	if !containsSubstr(view, "disabled") && !containsSubstr(view, "Disabled") {
		t.Errorf("manual-resolution step view does not contain a disabled option; view:\n%s", view)
	}
}

// TestConfigScreen_ManualResolutionStep_EnabledSetsManualResolutionTrue verifies
// that selecting the enabled option on the manual-resolution step results in
// Settings.ManualResolution == true after the wizard completes.
//
// RED: configStepManualResolution is not wired in; Settings.ManualResolution
// remains false regardless of input.
func TestConfigScreen_ManualResolutionStep_EnabledSetsManualResolutionTrue(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test selection", s.step)
	}

	// Select the enabled option. Convention: disabled is the default preselection at
	// cursor 0 (ContractsDesign.md: "configStepManualResolution | always | disabled").
	// Enabled is at cursor 1; one Down press moves to it before confirming.
	pressKey(s, tea.KeyDown)  // move cursor to enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // select enabled

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after accepting manual-resolution step")
	}

	if !s.Selection().Settings.ManualResolution {
		t.Error("Settings.ManualResolution = false after selecting enabled; " +
			"the manual-resolution step must set Settings.ManualResolution = true when enabled is chosen")
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.2: Backward navigation from configStepCommitBranch
// ---------------------------------------------------------------------------

// TestConfigScreen_BackNav_CommitBranchReachable_WhenCommitsEnabled verifies
// that pressing Esc from the manual-resolution step when commits are enabled
// lands on configStepCommitBranch, not on configStepCommits or any earlier step.
//
// RED: configStepManualResolution is not reachable.
func TestConfigScreen_BackNav_CommitBranchReachable_WhenCommitsEnabled(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated (no pre-consult step)
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// Enable commits so the commit-branch step appears.
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	// Accept the default branch variant (mosaic-owned).
	if s.step == configStepCommitBranch {
		pressKey(s, tea.KeyEnter) // branch: mosaic-owned (cursor 0)
	}

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward navigation", s.step)
	}

	// Pressing Esc from manual-resolution when commits are enabled must return
	// to configStepCommitBranch (not configStepCommits or configStepCheckpoints).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepCommitBranch {
		t.Errorf("Esc from manual-resolution (commits enabled) landed on step %v, want configStepCommitBranch (%v); "+
			"backward navigation must return to commit-branch when commits were enabled",
			s.step, configStepCommitBranch)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.4: Additional parity tests completing the parity matrix
// ---------------------------------------------------------------------------

// TestConfigScreen_Parity_CommitsEnabledUserOwn_SettingsMatchCLIEquivalent
// verifies that selecting commits=enabled with branch=user-own produces
// Settings equivalent to:
//   --mode orchestrated --commits enabled --commit-branch user-own
//
// RED: commits and commit-branch steps are not shown.
func TestConfigScreen_Parity_CommitsEnabledUserOwn_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	if s.step == configStepCommitBranch {
		// mosaic-owned is at cursor 0 (recommended/first); user-own is at cursor 1.
		pressKey(s, tea.KeyDown)  // move cursor to user-own
		pressKey(s, tea.KeyEnter) // select user-own
	}
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled (default)
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             true,
		CommitBranchVariant: domain.CommitBranchUserOwn,
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_ManualResolutionEnabled_SettingsMatchCLIEquivalent
// verifies that enabling manual resolution produces Settings equivalent to:
//   --mode orchestrated --manual-resolution
//
// RED: configStepManualResolution is not wired in; Settings.ManualResolution
// remains false.
func TestConfigScreen_Parity_ManualResolutionEnabled_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step == configStepManualResolution {
		// Disabled is the default preselection at cursor 0; enabled is at cursor 1.
		pressKey(s, tea.KeyDown)  // move cursor to enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // select enabled
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after accepting manual-resolution step")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		PreConsultation:     false,
		ManualResolution:    true,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_CommitsEnabledMOSAICOwned_SettingsMatchCLIEquivalent
// verifies that selecting commits=enabled with branch=mosaic-owned produces
// Settings equivalent to:
//   --mode orchestrated --commits enabled --commit-branch mosaic-owned
//
// RED: neither commits step nor commit-branch step is shown.
func TestConfigScreen_Parity_CommitsEnabledMOSAICOwned_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	if s.step == configStepCommitBranch {
		pressKey(s, tea.KeyEnter) // branch: mosaic-owned (cursor 0, the recommended default)
	}
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             true,
		CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}
