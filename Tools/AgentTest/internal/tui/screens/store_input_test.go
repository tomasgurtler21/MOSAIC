package screens_test

// store_input_test.go verifies the StoreInputScreen behavioral contract:
// when constructed with a non-empty initial path the screen displays that path
// in View() before any draft is typed, Enter without typing returns the initial
// path, rune keys append to a draft which Enter commits, Backspace removes the
// last draft character, Esc cancels and sets Back without changing the
// confirmed path, Reset clears Done and Back flags and discards any draft, and
// Resize does not corrupt state.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newStoreInputScreen(initial string) *screens.StoreInputScreen {
	return screens.NewStoreInputScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state — non-empty initial path
// ---------------------------------------------------------------------------

// TestStoreInputScreen_InitialPath_IsPreserved verifies that the screen is
// constructed with the initial path available before any input.
func TestStoreInputScreen_InitialPath_IsPreserved(t *testing.T) {
	// Arrange / Act
	s := newStoreInputScreen("/default/reports")

	// Assert
	if s.Path() != "/default/reports" {
		t.Errorf("Path() = %q immediately after construction; want initial path %q", s.Path(), "/default/reports")
	}
}

// TestStoreInputScreen_View_ShowsInitialPath verifies that View displays the
// initial path when no draft has been typed. The caller passes the current
// working directory as the initial path so the user can see the default before
// making any changes.
func TestStoreInputScreen_View_ShowsInitialPath(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act — no input; View must reflect the initial path
	view := collapseWhitespace(s.View())

	// Assert — the rendered output must contain the initial path string
	if !strings.Contains(view, "/default/reports") {
		t.Errorf("View() = %q; want it to contain the initial path \"/default/reports\" before any user input", view)
	}
}

// ---------------------------------------------------------------------------
// Enter without typing — returns the initial path
// ---------------------------------------------------------------------------

// TestStoreInputScreen_EnterWithoutTyping_ReturnsInitialPath verifies that
// pressing Enter without typing any characters leaves Path() equal to the
// initial path. This is the contract that makes the CWD default useful: the
// user can press Enter to accept it without retyping the path.
func TestStoreInputScreen_EnterWithoutTyping_ReturnsInitialPath(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act — confirm without typing anything
	s.Update(enterKey())

	// Assert
	if s.Path() != "/default/reports" {
		t.Errorf("Path() = %q after Enter without typing; want initial path %q", s.Path(), "/default/reports")
	}
}

// TestStoreInputScreen_EnterWithoutTyping_SetsDone verifies that pressing Enter
// without typing sets Done so the root model can advance to the next screen.
func TestStoreInputScreen_EnterWithoutTyping_SetsDone(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter without typing; Enter must set Done even when no draft was typed")
	}
}

// ---------------------------------------------------------------------------
// Text input — builds the draft and confirms on Enter
// ---------------------------------------------------------------------------

// TestStoreInputScreen_TypedPath_OverridesInitial verifies that characters
// typed in the inline editor become the confirmed path when Enter is pressed,
// replacing the initial value.
func TestStoreInputScreen_TypedPath_OverridesInitial(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act — type a new path character-by-character, then confirm
	for _, r := range "/custom/output" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())

	// Assert
	if s.Path() != "/custom/output" {
		t.Errorf("Path() = %q after typing and Enter; want %q", s.Path(), "/custom/output")
	}
}

// TestStoreInputScreen_TypedPath_SetsDone verifies that pressing Enter after
// typing a path sets Done.
func TestStoreInputScreen_TypedPath_SetsDone(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("")

	// Act
	s.Update(runeKey('/'))
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after typing and Enter; Enter must confirm the path and set Done")
	}
}

// ---------------------------------------------------------------------------
// Esc key — back navigation without committing the draft
// ---------------------------------------------------------------------------

// TestStoreInputScreen_Esc_SetsBack verifies that pressing Esc sets Back.
func TestStoreInputScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestStoreInputScreen_Esc_DiscardsTypedDraft verifies that pressing Esc
// discards any text typed since the screen was last entered, leaving the
// confirmed path unchanged.
func TestStoreInputScreen_Esc_DiscardsTypedDraft(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act — type some text then cancel
	for _, r := range "/edited/path" {
		s.Update(runeKey(r))
	}
	s.Update(escKey())

	// Assert — the confirmed path must still be the initial value
	if s.Path() != "/default/reports" {
		t.Errorf("Path() = %q after typing then Esc; Esc must discard draft, want initial path %q", s.Path(), "/default/reports")
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestStoreInputScreen_Reset_ClearsDone verifies that Reset clears the Done flag.
func TestStoreInputScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")
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

// TestStoreInputScreen_Reset_ClearsBack verifies that Reset clears the Back flag.
func TestStoreInputScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")
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

// TestStoreInputScreen_Reset_ClearsDraftState verifies that Reset discards any
// characters typed before Reset so that a fresh entry after Reset reflects only
// post-Reset input. An implementation that clears only Done/Back but retains
// the in-progress text buffer would concatenate pre- and post-Reset characters.
// A non-empty initial path is used so the test also verifies that Reset does
// not accidentally clear the confirmed path — the initial value must survive Reset.
func TestStoreInputScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — use a non-empty initial path to verify Reset does not clear confirmed.
	s := newStoreInputScreen("/default/reports")
	for _, r := range "/pre-reset" {
		s.Update(runeKey(r))
	}

	// Act
	s.Reset()

	// Assert — Reset must not have cleared the confirmed path; the initial must remain.
	if s.Path() != "/default/reports" {
		t.Errorf("Path() = %q immediately after Reset; Reset must not clear confirmed, want initial path %q", s.Path(), "/default/reports")
	}

	// Act (post-reset) — type a fresh path and confirm.
	// If Reset correctly cleared the draft, Path() reflects only "/post-reset.json".
	// If Reset did NOT clear the draft, Path() would reflect "/pre-reset/post-reset.json".
	for _, r := range "/post-reset.json" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert — only the post-reset characters must be reflected in the confirmed path.
	if s.Path() != "/post-reset.json" {
		t.Errorf("Path() = %q after Reset + type %q + Enter; want %q — Reset must discard the pre-Reset draft", s.Path(), "/post-reset.json", "/post-reset.json")
	}
}

// ---------------------------------------------------------------------------
// Backspace — removes last character from draft
// ---------------------------------------------------------------------------

// TestStoreInputScreen_Backspace_RemovesLastCharacter verifies that pressing
// Backspace after typing characters removes the last character from the draft
// so that Enter confirms only the remaining characters.
func TestStoreInputScreen_Backspace_RemovesLastCharacter(t *testing.T) {
	// Arrange — type "/ab" so the draft is "/ab"
	s := newStoreInputScreen("")
	for _, r := range "/ab" {
		s.Update(runeKey(r))
	}

	// Act — erase the 'b', leaving "/a"
	s.Update(backspaceKey())

	// Confirm so we can observe the committed path
	s.Update(enterKey())

	// Assert — only "/a" must have been committed
	if s.Path() != "/a" {
		t.Errorf("Path() = %q after typing %q, Backspace, Enter; want %q (last character erased)", s.Path(), "/ab", "/a")
	}
}

// TestStoreInputScreen_Backspace_OnEmptyDraft_IsNoOp verifies that pressing
// Backspace when the draft is empty does not crash and does not alter the
// confirmed path, Done, or Back flags.
func TestStoreInputScreen_Backspace_OnEmptyDraft_IsNoOp(t *testing.T) {
	// Arrange — screen with no characters typed (draft is empty)
	s := newStoreInputScreen("/default/reports")

	// Act — backspace on an empty draft must be a silent no-op
	s.Update(backspaceKey())

	// Assert — flags unchanged
	if s.Done() {
		t.Error("Done() = true after Backspace on empty draft; Backspace must not set Done")
	}
	if s.Back() {
		t.Error("Back() = true after Backspace on empty draft; Backspace must not set Back")
	}
	// Assert — confirmed path unchanged (still the initial, nothing committed)
	if s.Path() != "/default/reports" {
		t.Errorf("Path() = %q after Backspace on empty draft; want initial path %q (no-op must leave path unchanged)", s.Path(), "/default/reports")
	}
}

// TestStoreInputScreen_Backspace_DoesNotSetDoneOrBack verifies that a Backspace
// key press alone does not accidentally advance or cancel the screen.
func TestStoreInputScreen_Backspace_DoesNotSetDoneOrBack(t *testing.T) {
	// Arrange — type one character so the draft is non-empty
	s := newStoreInputScreen("")
	s.Update(runeKey('/'))

	// Act
	s.Update(backspaceKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true after Backspace; Backspace must not set Done")
	}
	if s.Back() {
		t.Error("Back() = true after Backspace; Backspace must not set Back")
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestStoreInputScreen_Resize_DoesNotTriggerDoneOrBack verifies that a terminal
// resize event does not accidentally set Done or Back.
func TestStoreInputScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

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

// TestStoreInputScreen_View_ReturnsNonEmptyString verifies that View returns
// a non-empty string so the root model has content to display.
func TestStoreInputScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newStoreInputScreen("/default/reports")

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the store input screen must render visible content")
	}
}
