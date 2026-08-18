package screens_test

// workspace_catalog_test.go verifies the PathKindCatalogFolder behaviour of WorkspaceScreen:
// readable-directory validation (no writability requirement), rejection of non-existent paths
// and regular files, back-navigation with path preservation, pre-fill, and the external-error
// surface used by the root model to render a reload failure inline.
//
// These tests are in the TDD RED phase: PathKindCatalogFolder exists as a declared constant
// but the validation logic and view wording are not yet implemented. Key tests fail because
// the screen falls back to PathKindDirectory behaviour (which rejects non-writable directories
// and shows "Workspace Path" title instead of "Catalogue Folder").

import (
	"os"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/screens"
)

// newWSCatalogScreen returns a WorkspaceScreen configured for catalogue-folder validation.
func newWSCatalogScreen() *screens.WorkspaceScreen {
	s := screens.NewWorkspaceScreen(80, 24, plainStyles())
	s.SetPathKind(screens.PathKindCatalogFolder)
	return s
}

// ---------------------------------------------------------------------------
// Valid readable directory — accepted
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Enter_ReadableDir_SetsDone verifies that a path pointing
// to an existing, readable directory passes validation and sets Done().
func TestWorkspaceScreen_CatalogKind_Enter_ReadableDir_SetsDone(t *testing.T) {
	// Arrange
	readableDir := t.TempDir() // readable (and writable) — both should be accepted
	s := newWSCatalogScreen()
	s.SetPrefilledPath(readableDir)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for readable directory %q with PathKindCatalogFolder; want true", readableDir)
	}
}

// TestWorkspaceScreen_CatalogKind_WorkspacePath_ReturnsAbsolutePath verifies that
// WorkspacePath() returns the absolute form of the entered catalogue folder path.
func TestWorkspaceScreen_CatalogKind_WorkspacePath_ReturnsAbsolutePath(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s := newWSCatalogScreen()
	s.SetPrefilledPath(dir)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Act
	got := s.WorkspacePath()

	// Assert
	if !strings.HasPrefix(got, "/") && (len(got) < 3 || got[1] != ':') {
		// Quick cross-platform absoluteness check: starts with '/' (POSIX) or has drive letter (Windows)
		t.Errorf("WorkspacePath() = %q; want an absolute path", got)
	}
}

// ---------------------------------------------------------------------------
// Valid readable, non-writable directory — accepted (key differentiator from PathKindDirectory)
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Enter_NonWritableReadableDir_SetsDone verifies that a
// directory that exists and is readable but NOT writable is accepted by PathKindCatalogFolder.
// This is the critical difference from PathKindDirectory, which probes writability and rejects
// non-writable directories. A catalogue folder needs only to be readable.
//
// On Windows, directory write permissions are governed by ACLs. However, writability is never
// probed for PathKindCatalogFolder on any platform, so a TempDir (which Windows ACLs allow
// read access to) is accepted. The POSIX case is covered by the chmod below; the Windows
// case is implicitly covered because any accessible directory should be accepted.
func TestWorkspaceScreen_CatalogKind_Enter_NonWritableReadableDir_SetsDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		// On Windows, skip the chmod-based setup. A TempDir is readable, and since
		// PathKindCatalogFolder must not probe writability, it should accept any readable dir.
		// The regular readableDir test above already covers this on Windows.
		t.Skip("chmod-based non-writable directory setup is not portable to Windows; covered by the readable-dir test on this platform")
	}

	// Arrange — create a directory, remove its write permission.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission from temp dir: %v", err)
	}
	// Restore write permission so TempDir cleanup can remove entries.
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	s := newWSCatalogScreen()
	s.SetPrefilledPath(dir)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: catalogue kind must accept a readable-but-not-writable directory.
	// Fails RED because the current stub uses PathKindDirectory validation, which rejects
	// non-writable directories.
	if !s.Done() {
		t.Errorf("Done() = false for non-writable but readable directory %q with PathKindCatalogFolder; "+
			"want true — catalogue folders only need to be readable, not writable; "+
			"PathKindCatalogFolder must not probe writability", dir)
	}
}

// TestWorkspaceScreen_CatalogKind_Vs_DirKind_NonWritableDir verifies the distinction between
// PathKindCatalogFolder and PathKindDirectory: a non-writable directory that PathKindDirectory
// rejects must be accepted by PathKindCatalogFolder, proving the two kinds have different rules.
//
// Skipped on Windows for the same reason as the companion test.
func TestWorkspaceScreen_CatalogKind_Vs_DirKind_NonWritableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based non-writable directory setup is not portable to Windows")
	}

	// Arrange — a directory with no write permission.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	// PathKindDirectory should reject it (writability probe fails).
	dirKind := newWSScreen()
	dirKind.SetPathKind(screens.PathKindDirectory)
	dirKind.SetPrefilledPath(dir)
	dirKind.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if dirKind.Done() {
		t.Error("PathKindDirectory accepted a non-writable directory; want rejection (regression check)")
	}

	// PathKindCatalogFolder should accept it (no writability probe).
	catalogKind := newWSCatalogScreen()
	catalogKind.SetPrefilledPath(dir)
	catalogKind.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Fails RED because stub uses PathKindDirectory validation.
	if !catalogKind.Done() {
		t.Errorf("PathKindCatalogFolder rejected a non-writable but readable directory; "+
			"want acceptance — PathKindCatalogFolder must not inherit the writability probe "+
			"from PathKindDirectory")
	}
}

// ---------------------------------------------------------------------------
// Invalid path — non-existent
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Enter_NonExistentPath_DoesNotSetDone verifies that a path
// that does not exist on the filesystem is rejected by PathKindCatalogFolder.
func TestWorkspaceScreen_CatalogKind_Enter_NonExistentPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	nonExistent := t.TempDir() + "/no-such-catalog"
	s := newWSCatalogScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for non-existent path %q; want false (catalogue folder must exist)", nonExistent)
	}
}

// TestWorkspaceScreen_CatalogKind_Enter_NonExistentPath_ShowsErrorInView verifies that a
// non-existent path produces visible inline error feedback.
func TestWorkspaceScreen_CatalogKind_Enter_NonExistentPath_ShowsErrorInView(t *testing.T) {
	// Arrange
	nonExistent := t.TempDir() + "/no-such-catalog"
	s := newWSCatalogScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := s.View()

	// Assert
	if !strings.Contains(view, "does not exist") && !strings.Contains(view, "not exist") {
		t.Errorf("view does not mention that the catalogue folder does not exist:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Invalid path — regular file (not a directory)
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Enter_FilePath_DoesNotSetDone verifies that providing a
// path to a regular file is rejected because the catalogue folder must be a directory.
func TestWorkspaceScreen_CatalogKind_Enter_FilePath_DoesNotSetDone(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "catalog.txt", "not-a-directory")
	s := newWSCatalogScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for regular file path %q; want false (catalogue folder must be a directory)", filePath)
	}
}

// TestWorkspaceScreen_CatalogKind_Enter_FilePath_ShowsNotDirectoryError verifies that
// providing a file path produces feedback indicating a directory is expected.
func TestWorkspaceScreen_CatalogKind_Enter_FilePath_ShowsNotDirectoryError(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "catalog.txt", "not-a-directory")
	s := newWSCatalogScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := s.View()

	// Assert
	if !strings.Contains(view, "not a directory") && !strings.Contains(view, "directory") {
		t.Errorf("view does not mention that the path is not a directory:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Invalid path — empty and whitespace-only
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Enter_EmptyPath_DoesNotSetDone verifies that an empty
// catalogue folder path is rejected.
func TestWorkspaceScreen_CatalogKind_Enter_EmptyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()
	s.SetPrefilledPath("")

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if s.Done() {
		t.Error("Done() = true for empty path; want false (empty catalogue folder must be rejected)")
	}
}

// TestWorkspaceScreen_CatalogKind_Enter_WhitespaceOnlyPath_DoesNotSetDone verifies that a
// whitespace-only path is treated as empty and rejected.
func TestWorkspaceScreen_CatalogKind_Enter_WhitespaceOnlyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()
	s.SetPrefilledPath("   ")

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if s.Done() {
		t.Error("Done() = true for whitespace-only path; want false")
	}
}

// ---------------------------------------------------------------------------
// View wording — catalogue-specific content
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_View_ContainsCatalogueTitle verifies that when the screen
// is in PathKindCatalogFolder mode, the rendered view contains catalogue-specific wording
// ("Catalogue" or "Catalog") and the screen title "Catalogue Folder".
//
// Fails RED because the stub uses PathKindDirectory behaviour, which renders "Workspace Path".
func TestWorkspaceScreen_CatalogKind_View_ContainsCatalogueTitle(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()

	// Act
	view := s.View()
	lower := strings.ToLower(view)

	// Assert — the view must contain catalogue-specific wording.
	if !strings.Contains(lower, "catalogue") && !strings.Contains(lower, "catalog") {
		t.Errorf("catalogue-kind view contains neither 'catalogue' nor 'catalog' wording; "+
			"want 'Catalogue Folder' title — PathKindCatalogFolder must set its own title and label:\n%s", view)
	}
}

// TestWorkspaceScreen_CatalogKind_View_DoesNotContainWorkspaceWording verifies that the
// catalogue screen does not use workspace-specific wording, so the user is not confused about
// what kind of path is expected.
//
// Fails RED because the stub renders "Workspace Path" / "Workspace directory path:" copy.
func TestWorkspaceScreen_CatalogKind_View_DoesNotContainWorkspaceWording(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()

	// Act
	view := s.View()
	lower := strings.ToLower(view)

	// Assert
	if strings.Contains(lower, "workspace") {
		t.Errorf("catalogue-kind view contains 'workspace' wording; want catalogue-specific copy:\n%s", view)
	}
}

// TestWorkspaceScreen_CatalogKind_View_ContainsGuidanceAboutCatalogueContent verifies that
// the screen's guidance text mentions what the folder controls. Specifically, it must
// reference "skills" or "hooks" — catalogue-specific resources that the workspace guidance
// does not mention. The workspace guidance only mentions deployment; the catalogue guidance
// must describe sourcing (agents, workflows, skills, hooks).
//
// Fails RED because the stub renders workspace directory guidance which does not mention
// "skills" or "hooks".
func TestWorkspaceScreen_CatalogKind_View_ContainsGuidanceAboutCatalogueContent(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()

	// Act
	view := s.View()
	lower := strings.ToLower(view)

	// Assert — guidance must mention catalogue-specific resources (skills or hooks).
	// The workspace view says "Enter the directory where agents will be deployed." which
	// mentions agents but NOT skills or hooks; the catalogue view must do better.
	if !strings.Contains(lower, "skill") && !strings.Contains(lower, "hook") {
		t.Errorf("catalogue-kind view does not mention 'skill' or 'hook' in guidance; "+
			"want catalogue-specific copy explaining that the folder supplies agents, workflows, "+
			"skills, and hooks (skills/hooks are the distinguishing terms not in workspace guidance):\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Back navigation and path preservation
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_Esc_SetsBack verifies that pressing Esc on the catalogue
// folder screen signals back navigation.
func TestWorkspaceScreen_CatalogKind_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc on catalogue-kind screen; want true")
	}
}

// TestWorkspaceScreen_CatalogKind_Esc_DoesNotSetDone verifies that Esc and Done are mutually
// exclusive on the catalogue screen.
func TestWorkspaceScreen_CatalogKind_Esc_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Assert
	if s.Done() {
		t.Error("Done() = true after Esc on catalogue-kind screen; want false")
	}
}

// TestWorkspaceScreen_CatalogKind_Reset_PreservesEnteredPath verifies that after Esc and
// Reset() the previously entered catalogue folder path is still present. This mirrors the
// behaviour required of every other path-entry screen: the user's input survives back
// navigation so it does not need to be retyped.
func TestWorkspaceScreen_CatalogKind_Reset_PreservesEnteredPath(t *testing.T) {
	// Arrange
	dir := t.TempDir() // readable+writable, should be accepted on re-confirm
	s := newWSCatalogScreen()
	s.SetPrefilledPath(dir)
	s.Update(tea.KeyMsg{Type: tea.KeyEsc}) // user presses back
	s.Reset()                               // root model resets the screen flags

	// Re-confirm: if the path was preserved, Enter should set Done().
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if !s.Done() {
		t.Error("Done() = false after re-confirming a path that survived Reset; "+
			"the entered catalogue folder must be preserved across back navigation")
	}
}

// TestWorkspaceScreen_CatalogKind_SetPrefilledPath_ValueAppearsInView verifies that a path
// set via SetPrefilledPath is visible in the screen view, confirming the pre-fill takes effect
// and is visible to the user before they interact with the screen.
func TestWorkspaceScreen_CatalogKind_SetPrefilledPath_ValueAppearsInView(t *testing.T) {
	// Arrange — use a recognisably unique path segment so we can find it in the rendered view.
	dir := t.TempDir()
	s := newWSCatalogScreen()

	// Act
	s.SetPrefilledPath(dir)
	view := s.View()

	// Assert — the view must contain the path or at least a recognisable portion of it.
	// We use a substring of the absolute path; the dir name is unique enough.
	if !strings.Contains(view, dir) {
		// Some renderers truncate long paths; check for the last path segment as a fallback.
		parts := strings.Split(dir, string(os.PathSeparator))
		lastSeg := parts[len(parts)-1]
		if !strings.Contains(view, lastSeg) {
			t.Errorf("view does not contain pre-filled path %q or its last segment %q:\n%s",
				dir, lastSeg, view)
		}
	}
}

// ---------------------------------------------------------------------------
// SetExternalError — inline error surface for reload failures
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_CatalogKind_SetExternalError_ShowsMessageInView verifies that after the
// root model calls SetExternalError (e.g., because a catalogue reload failed), the error
// message is visible in the rendered view. This is the mechanism for surfacing the failure
// inline on the screen without a crash.
//
// Fails RED because SetExternalError is currently a no-op stub.
func TestWorkspaceScreen_CatalogKind_SetExternalError_ShowsMessageInView(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()
	errMsg := "catalogue reload failed: could not read Agents directory"

	// Act
	s.SetExternalError(errMsg)
	view := s.View()

	// Assert — the error message must be present in the rendered view.
	if !strings.Contains(view, "catalogue reload failed") {
		t.Errorf("view does not contain the external error message %q after SetExternalError; "+
			"want the error to be shown inline so the user knows the reload failed:\n%s", errMsg, view)
	}
}

// TestWorkspaceScreen_CatalogKind_SetExternalError_ViewDoesNotContainErrorAfterUpdate verifies
// that the external error is cleared after the next Update call. This prevents stale error
// messages from persisting when the user corrects the path and retries.
func TestWorkspaceScreen_CatalogKind_SetExternalError_ViewDoesNotContainErrorAfterUpdate(t *testing.T) {
	// Arrange
	s := newWSCatalogScreen()
	errMsg := "catalogue reload failed: unique-sentinel-error-text"
	s.SetExternalError(errMsg)

	// Act — any Update call should clear the external error.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}) // a no-op keystroke
	view := s.View()

	// Assert — the error must be gone after the first Update.
	// Note: this test may not fail RED (the no-op stub never sets the error, so it's also
	// absent after Update). The SetExternalError_ShowsMessageInView test is the primary RED test.
	if strings.Contains(view, "unique-sentinel-error-text") {
		t.Errorf("view still contains the external error text after Update; "+
			"SetExternalError must be cleared on the next Update call:\n%s", view)
	}
}

// TestWorkspaceScreen_DirKind_StillRejectsNonWritableDir_AfterCatalogKindAdded is a
// regression test confirming that adding PathKindCatalogFolder does not accidentally relax
// the writability requirement that PathKindDirectory still needs. The workspace screen's
// directory kind must remain unchanged.
//
// Skipped on Windows for the same reason as other chmod-based tests.
func TestWorkspaceScreen_DirKind_StillRejectsNonWritableDir_AfterCatalogKindAdded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions use Windows ACLs; chmod-based setup is not portable")
	}

	// Arrange — non-writable directory.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	s := newWSScreen()
	// PathKindDirectory is the zero value; explicit set is for clarity.
	s.SetPathKind(screens.PathKindDirectory)
	s.SetPrefilledPath(dir)

	// Act
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert — PathKindDirectory must still reject non-writable directories.
	if s.Done() {
		t.Errorf("PathKindDirectory accepted a non-writable directory after PathKindCatalogFolder was added; "+
			"want rejection — adding a new PathKind must not weaken existing PathKind validation rules")
	}
}
