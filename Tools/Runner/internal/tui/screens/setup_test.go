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
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
