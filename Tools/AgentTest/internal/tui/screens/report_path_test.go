package screens_test

// report_path_test.go verifies the ReportPathScreen behavioral contract:
// rune keys append to the draft path, Enter confirms the draft and sets Done,
// Esc cancels and sets Back without changing the confirmed path, Reset clears
// the Done and Back flags, and Reset discards any partially typed draft so the
// screen behaves as if freshly entered.

import (
	"testing"

	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newReportPathScreen(initial string) *screens.ReportPathScreen {
	return screens.NewReportPathScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestReportPathScreen_InitialPath_IsPreserved verifies that the screen is
// constructed with the initial path available before any input.
func TestReportPathScreen_InitialPath_IsPreserved(t *testing.T) {
	// Arrange / Act
	s := newReportPathScreen("/initial/report.json")

	// Assert
	if s.Path() != "/initial/report.json" {
		t.Errorf("Path() = %q immediately after construction; want initial path %q", s.Path(), "/initial/report.json")
	}
}

// ---------------------------------------------------------------------------
// Text input — builds the draft and confirms on Enter
// ---------------------------------------------------------------------------

// TestReportPathScreen_TypedText_ConfirmsOnEnter verifies that characters typed
// in the inline editor become the confirmed path when Enter is pressed.
func TestReportPathScreen_TypedText_ConfirmsOnEnter(t *testing.T) {
	// Arrange
	s := newReportPathScreen("")

	// Act — type a path character-by-character, then confirm
	for _, r := range "/run/report.json" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())

	// Assert
	if s.Path() != "/run/report.json" {
		t.Errorf("Path() = %q after typing and Enter; want %q", s.Path(), "/run/report.json")
	}
}

// ---------------------------------------------------------------------------
// Enter key — confirms the draft
// ---------------------------------------------------------------------------

// TestReportPathScreen_Enter_SetsDone verifies that pressing Enter sets Done
// so the root model can advance to the next screen.
func TestReportPathScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")

	// Act
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter; Enter must confirm the path and set Done")
	}
}

// ---------------------------------------------------------------------------
// Esc key — back navigation without committing the draft
// ---------------------------------------------------------------------------

// TestReportPathScreen_Esc_SetsBack verifies that pressing Esc sets Back.
func TestReportPathScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestReportPathScreen_Esc_DiscardsTypedDraft verifies that pressing Esc discards
// any text typed since the screen was last entered, leaving the confirmed path
// unchanged.
func TestReportPathScreen_Esc_DiscardsTypedDraft(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/original.json")

	// Act — type some text then cancel
	for _, r := range "/edited.json" {
		s.Update(runeKey(r))
	}
	s.Update(escKey())

	// Assert — the confirmed path must still be the original initial value
	if s.Path() != "/original.json" {
		t.Errorf("Path() = %q after typing then Esc; Esc must discard draft, want %q", s.Path(), "/original.json")
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestReportPathScreen_Reset_ClearsDone verifies that Reset clears the Done flag.
func TestReportPathScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Done() {
		t.Error("Done() = true after Reset; Reset must clear the Done flag")
	}
}

// TestReportPathScreen_Reset_ClearsBack verifies that Reset clears the Back flag.
func TestReportPathScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")
	s.Update(escKey())
	if !s.Back() {
		t.Fatal("precondition: Back() must be true after Esc before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Back() {
		t.Error("Back() = true after Reset; Reset must clear the Back flag")
	}
}

// TestReportPathScreen_Reset_ClearsDraftState verifies that Reset discards any
// characters typed before Reset so that a fresh entry after Reset reflects only
// post-Reset input. An implementation that clears only Done/Back but retains
// the in-progress text buffer would concatenate pre- and post-Reset characters,
// producing the wrong confirmed path.
func TestReportPathScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — type some characters without confirming to accumulate draft state.
	s := newReportPathScreen("")
	for _, r := range "/pre-reset" {
		s.Update(runeKey(r))
	}

	// Act
	s.Reset()

	// Act (post-reset) — type a fresh path and confirm.
	// If Reset correctly cleared the draft, Path() reflects only "/post-reset.json".
	// If Reset did NOT clear the draft, Path() would reflect "/pre-reset/post-reset.json"
	// (or some concatenation of pre- and post-Reset characters).
	for _, r := range "/post-reset.json" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert
	if s.Path() != "/post-reset.json" {
		t.Errorf("Path() = %q after Reset + type %q + Enter; want %q — Reset must discard the pre-Reset draft", s.Path(), "/post-reset.json", "/post-reset.json")
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestReportPathScreen_Resize_DoesNotTriggerDoneOrBack verifies that a terminal
// resize event does not accidentally set Done or Back.
func TestReportPathScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")

	// Act
	s.Resize(120)

	// Assert
	if s.Done() {
		t.Error("Done() = true after Resize; Resize must not trigger Done")
	}
	if s.Back() {
		t.Error("Back() = true after Resize; Resize must not trigger Back")
	}
}

// ---------------------------------------------------------------------------
// View — renders content
// ---------------------------------------------------------------------------

// TestReportPathScreen_View_ReturnsNonEmptyString verifies that View returns
// a non-empty string so the root model has content to display.
func TestReportPathScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newReportPathScreen("/report.json")

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the report path screen must render visible content")
	}
}
