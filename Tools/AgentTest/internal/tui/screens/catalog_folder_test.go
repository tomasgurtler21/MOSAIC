package screens_test

// catalog_folder_test.go verifies the CatalogFolderScreen behavioral contract:
// rune keys append to the draft folder, Enter confirms the draft and sets Done,
// Esc cancels and sets Back without changing the confirmed folder, Reset clears
// the Done and Back flags, and Reset discards any partially typed draft so the
// screen behaves as if freshly entered.

import (
	"testing"

	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newCatalogFolderScreen(initial string) *screens.CatalogFolderScreen {
	return screens.NewCatalogFolderScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_InitialFolder_IsPreserved verifies that the screen is
// constructed with the initial folder path available before any input.
func TestCatalogFolderScreen_InitialFolder_IsPreserved(t *testing.T) {
	// Arrange / Act
	s := newCatalogFolderScreen("/my/catalog")

	// Assert
	if s.Folder() != "/my/catalog" {
		t.Errorf("Folder() = %q immediately after construction; want initial folder %q", s.Folder(), "/my/catalog")
	}
}

// ---------------------------------------------------------------------------
// Text input — builds the draft and confirms on Enter
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_TypedText_ConfirmsOnEnter verifies that characters typed
// in the inline editor become the confirmed folder when Enter is pressed.
func TestCatalogFolderScreen_TypedText_ConfirmsOnEnter(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("")

	// Act — type a folder path character-by-character, then confirm
	for _, r := range "/catalog/override" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())

	// Assert
	if s.Folder() != "/catalog/override" {
		t.Errorf("Folder() = %q after typing and Enter; want %q", s.Folder(), "/catalog/override")
	}
}

// ---------------------------------------------------------------------------
// Enter key — confirms the draft
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_Enter_SetsDone verifies that pressing Enter sets Done
// so the root model can advance to the next screen.
func TestCatalogFolderScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")

	// Act
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter; Enter must confirm the folder and set Done")
	}
}

// ---------------------------------------------------------------------------
// Esc key — back navigation without committing the draft
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_Esc_SetsBack verifies that pressing Esc sets Back.
func TestCatalogFolderScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestCatalogFolderScreen_Esc_DiscardsTypedDraft verifies that pressing Esc
// discards any text typed since the screen was last entered, leaving the
// confirmed folder unchanged.
func TestCatalogFolderScreen_Esc_DiscardsTypedDraft(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/original/catalog")

	// Act — type some text then cancel
	for _, r := range "/edited/catalog" {
		s.Update(runeKey(r))
	}
	s.Update(escKey())

	// Assert — the confirmed folder must still be the original initial value
	if s.Folder() != "/original/catalog" {
		t.Errorf("Folder() = %q after typing then Esc; Esc must discard draft, want %q", s.Folder(), "/original/catalog")
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_Reset_ClearsDone verifies that Reset clears the Done flag.
func TestCatalogFolderScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")
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

// TestCatalogFolderScreen_Reset_ClearsBack verifies that Reset clears the Back flag.
func TestCatalogFolderScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")
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

// TestCatalogFolderScreen_Reset_ClearsDraftState verifies that Reset discards
// any characters typed before Reset so that a fresh entry after Reset reflects
// only post-Reset input. An implementation that clears only Done/Back but
// retains the in-progress text buffer would concatenate pre- and post-Reset
// characters, producing the wrong confirmed folder.
func TestCatalogFolderScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — type some characters without confirming to accumulate draft state.
	s := newCatalogFolderScreen("")
	for _, r := range "/pre-reset" {
		s.Update(runeKey(r))
	}

	// Act
	s.Reset()

	// Act (post-reset) — type a fresh folder path and confirm.
	// If Reset correctly cleared the draft, Folder() reflects only "/post-reset/catalog".
	// If Reset did NOT clear the draft, Folder() would reflect a concatenation
	// of pre- and post-Reset characters.
	for _, r := range "/post-reset/catalog" {
		s.Update(runeKey(r))
	}
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert
	if s.Folder() != "/post-reset/catalog" {
		t.Errorf("Folder() = %q after Reset + type %q + Enter; want %q — Reset must discard the pre-Reset draft", s.Folder(), "/post-reset/catalog", "/post-reset/catalog")
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestCatalogFolderScreen_Resize_DoesNotTriggerDoneOrBack verifies that a
// terminal resize event does not accidentally set Done or Back.
func TestCatalogFolderScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")

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

// TestCatalogFolderScreen_View_ReturnsNonEmptyString verifies that View returns
// a non-empty string so the root model has content to display.
func TestCatalogFolderScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newCatalogFolderScreen("/catalog")

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the catalog folder screen must render visible content")
	}
}
