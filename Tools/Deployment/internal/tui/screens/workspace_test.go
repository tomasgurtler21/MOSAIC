package screens_test

// workspace_test.go verifies the WorkspaceScreen's three-step path validation: empty path,
// non-existent path, non-directory path, and a valid writable directory. It also verifies
// that the entered path is preserved across back navigation.
//
// Validation is exercised entirely through the model/update cycle. No terminal is required.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func wsEnterKey() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} }
func wsEscKey() tea.Msg   { return tea.KeyMsg{Type: tea.KeyEsc} }

func newWSScreen() *screens.WorkspaceScreen {
	return screens.NewWorkspaceScreen(80, 24, plainStyles())
}

// ---------------------------------------------------------------------------
// Valid path — writable directory
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_ValidWritableDir_SetsDone verifies that a path pointing to an
// existing, writable directory passes all three validation steps and sets Done().
func TestWorkspaceScreen_Enter_ValidWritableDir_SetsDone(t *testing.T) {
	// Arrange
	validDir := t.TempDir() // guaranteed to exist and be writable
	s := newWSScreen()
	s.SetPrefilledPath(validDir)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for valid writable directory %q; want true", validDir)
	}
}

// TestWorkspaceScreen_WorkspacePath_ReturnsAbsolutePath verifies that WorkspacePath() returns
// the absolute form of the entered path so the caller always works with an unambiguous path.
func TestWorkspaceScreen_WorkspacePath_ReturnsAbsolutePath(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(validDir)
	s.Update(wsEnterKey())

	// Act
	got := s.WorkspacePath()

	// Assert
	if !filepath.IsAbs(got) {
		t.Errorf("WorkspacePath() = %q; want an absolute path", got)
	}
}

// ---------------------------------------------------------------------------
// Invalid path — empty
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_EmptyPath_DoesNotSetDone verifies that an empty path is
// rejected: the user must not be able to advance without entering a value.
func TestWorkspaceScreen_Enter_EmptyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath("")

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true for empty path; want false (empty path must be rejected)")
	}
}

// TestWorkspaceScreen_Enter_EmptyPath_ShowsErrorInView verifies that an empty path produces
// a visible error message so the user understands why the screen did not advance.
func TestWorkspaceScreen_Enter_EmptyPath_ShowsErrorInView(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath("")

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert
	if !strings.Contains(view, "empty") {
		t.Errorf("view does not mention 'empty' after empty path rejection:\n%s", view)
	}
}

// TestWorkspaceScreen_Enter_WhitespaceOnlyPath_DoesNotSetDone verifies that a path
// consisting only of whitespace is treated as empty and rejected.
func TestWorkspaceScreen_Enter_WhitespaceOnlyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath("   ")

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true for whitespace-only path; want false (whitespace must be treated as empty)")
	}
}

// ---------------------------------------------------------------------------
// Invalid path — non-existent directory
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_NonExistentPath_DoesNotSetDone verifies that a path to a
// directory that does not exist is rejected with clear feedback, not a silent failure.
func TestWorkspaceScreen_Enter_NonExistentPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist", "subdir")
	s := newWSScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for non-existent path %q; want false", nonExistent)
	}
}

// TestWorkspaceScreen_Enter_NonExistentPath_ShowsErrorInView verifies that a non-existent
// path produces a view containing error feedback so the user knows how to fix the problem.
func TestWorkspaceScreen_Enter_NonExistentPath_ShowsErrorInView(t *testing.T) {
	// Arrange
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	s := newWSScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the error must mention the problem so the user can act on it.
	if !strings.Contains(view, "does not exist") && !strings.Contains(view, "not exist") {
		t.Errorf("view does not mention that the directory does not exist:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Invalid path — path exists but is a file, not a directory
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_FilePath_DoesNotSetDone verifies that providing the path to an
// existing file (not a directory) is rejected, because the workspace must be a directory.
func TestWorkspaceScreen_Enter_FilePath_DoesNotSetDone(t *testing.T) {
	// Arrange — create a temporary file
	dir := t.TempDir()
	filePath := filepath.Join(dir, "regular-file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	s := newWSScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for file path %q; want false (workspace must be a directory)", filePath)
	}
}

// TestWorkspaceScreen_Enter_FilePath_ShowsDirectoryErrorInView verifies that providing a
// file path produces feedback indicating that a directory is expected.
func TestWorkspaceScreen_Enter_FilePath_ShowsDirectoryErrorInView(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	filePath := filepath.Join(dir, "regular-file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	s := newWSScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert
	if !strings.Contains(view, "not a directory") && !strings.Contains(view, "directory") {
		t.Errorf("view does not mention 'directory' after file-path rejection:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Back navigation — path preservation
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Esc_SetsBack verifies that pressing Esc signals back navigation so
// the root model can return to the mode selection screen.
func TestWorkspaceScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath(t.TempDir())

	// Act
	s.Update(wsEscKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// TestWorkspaceScreen_Reset_PreservesEnteredPath verifies that after the user presses Esc
// and the root model calls Reset(), the previously entered path is still available. This
// preserves prior valid input so the user does not have to retype on back navigation.
func TestWorkspaceScreen_Reset_PreservesEnteredPath(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(validDir)
	s.Update(wsEscKey()) // user presses back
	// Root model calls Reset() after receiving Back()
	s.Reset()

	// The screen is shown again; the path must still be the one typed before.
	// We verify by pressing Enter again and checking Done(), which only sets when the
	// text input's current value passes validation.
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after re-confirming a path that survived Reset; prior valid input must be preserved on back navigation")
	}
}

// TestWorkspaceScreen_Esc_DoesNotSetDone verifies that Esc and Done are mutually exclusive.
func TestWorkspaceScreen_Esc_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSScreen()

	// Act
	s.Update(wsEscKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true after Esc; want false (back and confirm must be mutually exclusive)")
	}
}

// ---------------------------------------------------------------------------
// Keyboard-only operability
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_KeyboardOnly_EnterConfirmsValidPath verifies that the workspace screen
// completes entirely via keyboard: type a path (via SetPrefilledPath in the test) and press
// Enter. No mouse event is required.
func TestWorkspaceScreen_KeyboardOnly_EnterConfirmsValidPath(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath(t.TempDir())

	// Act — keyboard-only: Enter confirms
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after keyboard Enter on valid path; the workspace screen must be fully operable by keyboard")
	}
}

// TestWorkspaceScreen_KeyboardOnly_EscNavigatesBack verifies that the Esc key alone
// triggers back navigation on the workspace screen.
func TestWorkspaceScreen_KeyboardOnly_EscNavigatesBack(t *testing.T) {
	// Arrange
	s := newWSScreen()

	// Act
	s.Update(wsEscKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after keyboard Esc; the workspace screen must be fully navigable by keyboard")
	}
}

// ---------------------------------------------------------------------------
// Invalid path — non-writable directory
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_NonWritableDir_DoesNotSetDone verifies that a directory that
// exists but is not writable is rejected by the third validation step. AC20.4 names
// "non-writable" as a required invalid-path case alongside non-existent and non-directory.
//
// On Windows, directory write permissions are governed by ACLs which chmod cannot modify
// portably, so this test is skipped on that platform.
func TestWorkspaceScreen_Enter_NonWritableDir_DoesNotSetDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions use Windows ACLs; chmod-based setup is not portable to this platform")
	}

	// Arrange — create a directory, remove its write permission, then restore on cleanup.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission from temp dir: %v", err)
	}
	// Restore write permission so TempDir cleanup can remove entries.
	// t.Cleanup runs LIFO, so this runs before the TempDir removal registered by t.TempDir.
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	s := newWSScreen()
	s.SetPrefilledPath(dir)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for a non-writable directory %q; want false (non-writable path must be rejected)", dir)
	}
}

// TestWorkspaceScreen_Enter_NonWritableDir_ShowsErrorInView verifies that a non-writable
// directory produces a visible error message so the user knows why the screen did not advance.
//
// On Windows, this test is skipped for the same reason as the companion Done() test.
func TestWorkspaceScreen_Enter_NonWritableDir_ShowsErrorInView(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions use Windows ACLs; chmod-based setup is not portable to this platform")
	}

	// Arrange
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission from temp dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	s := newWSScreen()
	s.SetPrefilledPath(dir)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the view must mention writability so the user understands how to fix it.
	if !strings.Contains(view, "writable") && !strings.Contains(view, "write") {
		t.Errorf("view does not mention writability after non-writable directory rejection:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Quote stripping — matched pair, unmatched, double-nested, whitespace combinations
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_Enter_QuotedValidPath_SetsDone verifies that a path wrapped in a
// matched pair of double quotes (the format produced by Windows Explorer "Copy as path")
// is accepted after the outer quote pair is stripped. Without stripping the literal "
// characters the path does not exist on the filesystem, causing a spurious validation failure.
func TestWorkspaceScreen_Enter_QuotedValidPath_SetsDone(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(`"` + validDir + `"`)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for quoted path %q; want true (outer quotes must be stripped before validation)", `"`+validDir+`"`)
	}
}

// TestWorkspaceScreen_WorkspacePath_QuotedInput_ReturnsUnquotedAbsPath verifies that
// WorkspacePath() returns the absolute form of the path with surrounding double quotes removed
// after the user enters a Windows-Explorer-style "Copy as path" value.
func TestWorkspaceScreen_WorkspacePath_QuotedInput_ReturnsUnquotedAbsPath(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(`"` + validDir + `"`)
	s.Update(wsEnterKey())

	// Act
	got := s.WorkspacePath()

	// Assert — returned path must equal the absolute form of the bare directory.
	want, _ := filepath.Abs(validDir)
	if got != want {
		t.Errorf("WorkspacePath() = %q; want %q (outer quotes must be stripped and path resolved to absolute)", got, want)
	}
}

// TestWorkspaceScreen_Enter_WhitespacePlusQuotedPath_SetsDone verifies that surrounding
// whitespace padded around a quoted path is handled: trim whitespace first, then strip the
// matched outer quote pair, so `  "<dir>"  ` resolves to <dir> and passes validation.
func TestWorkspaceScreen_Enter_WhitespacePlusQuotedPath_SetsDone(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(`  "` + validDir + `"  `)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for whitespace-padded quoted path; want true (whitespace and outer quotes must both be stripped)")
	}
}

// TestWorkspaceScreen_Enter_EmptyQuotedInput_ShowsEmptyError verifies that the input `""` —
// two double-quote characters that yield an empty path after stripping — produces the
// "path cannot be empty" feedback rather than a "does not exist" error for a path named `""`.
func TestWorkspaceScreen_Enter_EmptyQuotedInput_ShowsEmptyError(t *testing.T) {
	// Arrange
	s := newWSScreen()
	s.SetPrefilledPath(`""`)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the view must mention emptiness so the user knows the input was blank.
	if !strings.Contains(view, "empty") {
		t.Errorf("view does not contain 'empty' after \"\" (empty-after-stripping) input; want 'path cannot be empty' feedback:\n%s", view)
	}
}

// TestWorkspaceScreen_Enter_UnmatchedLeadingQuote_DoesNotSetDone verifies that a path with
// only a leading double-quote and no matching trailing quote is left unchanged. Because the
// literal " character makes the path non-existent on the filesystem, validation must fail
// rather than silently strip the unmatched character.
func TestWorkspaceScreen_Enter_UnmatchedLeadingQuote_DoesNotSetDone(t *testing.T) {
	// Arrange — leading " present, no trailing "
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(`"` + validDir) // intentionally no closing "

	// Act
	s.Update(wsEnterKey())

	// Assert — the unmatched quote is preserved, so the path does not exist, and Done stays false.
	if s.Done() {
		t.Error("Done() = true for path with unmatched leading quote; want false (unmatched quote must not be stripped)")
	}
}

// TestWorkspaceScreen_Enter_UnmatchedTrailingQuote_DoesNotSetDone verifies that a path with
// only a trailing double-quote and no matching leading quote is left unchanged.
func TestWorkspaceScreen_Enter_UnmatchedTrailingQuote_DoesNotSetDone(t *testing.T) {
	// Arrange — no leading ", trailing " present
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(validDir + `"`) // intentionally no opening "

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true for path with unmatched trailing quote; want false (unmatched quote must not be stripped)")
	}
}

// TestWorkspaceScreen_Enter_DoubleNestedQuotes_DoesNotSetDone verifies that only one outer
// matched pair of quotes is stripped. Input `""<dir>""` (two leading, two trailing) strips to
// `"<dir>"` which still contains quotes and therefore cannot be an existing directory path.
func TestWorkspaceScreen_Enter_DoubleNestedQuotes_DoesNotSetDone(t *testing.T) {
	// Arrange — two leading and two trailing quotes around a real directory
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPrefilledPath(`""` + validDir + `""`)

	// Act
	s.Update(wsEnterKey())

	// Assert — after stripping one pair the remaining inner quotes prevent the path from being found.
	if s.Done() {
		t.Error("Done() = true after double-nested quote input; want false (only one outer matched pair must be stripped, leaving inner quotes intact)")
	}
}

// ---------------------------------------------------------------------------
// File-kind path entry — T1.1
//
// These tests exercise SetPathKind(PathKindFile), which selects file validation
// rules: the screen must accept an existing readable regular file and reject
// directories, non-existent paths, empty paths, and whitespace-only paths.
// ---------------------------------------------------------------------------

// newWSFileScreen returns a WorkspaceScreen configured for file-kind validation.
func newWSFileScreen() *screens.WorkspaceScreen {
	s := screens.NewWorkspaceScreen(80, 24, plainStyles())
	s.SetPathKind(screens.PathKindFile)
	return s
}

// createTempFile creates a regular file with the given name and content in dir.
func createTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create temp file %q: %v", p, err)
	}
	return p
}

// TestWorkspaceScreen_FileKind_Enter_ExistingFile_SetsDone verifies that a file-kind screen
// accepts an existing, readable regular file and sets Done().
func TestWorkspaceScreen_FileKind_Enter_ExistingFile_SetsDone(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	s := newWSFileScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for existing regular file %q with file-kind validation; want true", filePath)
	}
}

// TestWorkspaceScreen_FileKind_WorkspacePath_ExistingFile_ReturnsAbsolutePath verifies that
// WorkspacePath() returns the absolute form of the confirmed file path so the caller always
// works with an unambiguous path.
func TestWorkspaceScreen_FileKind_WorkspacePath_ExistingFile_ReturnsAbsolutePath(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	s := newWSFileScreen()
	s.SetPrefilledPath(filePath)
	s.Update(wsEnterKey())

	// Act
	got := s.WorkspacePath()

	// Assert
	if !filepath.IsAbs(got) {
		t.Errorf("WorkspacePath() = %q; want an absolute path after file-kind confirmation", got)
	}
}

// TestWorkspaceScreen_FileKind_Enter_DirectoryPath_DoesNotSetDone verifies that a file-kind
// screen rejects a directory path because a directory is never a valid file input.
func TestWorkspaceScreen_FileKind_Enter_DirectoryPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s := newWSFileScreen()
	s.SetPrefilledPath(dir)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for directory path %q with file-kind validation; want false (a directory is not a file)", dir)
	}
}

// TestWorkspaceScreen_FileKind_Enter_DirectoryPath_ShowsNotAFileError verifies that when a
// directory path is entered in file-kind, the inline error names the path-is-not-a-file
// condition so the user understands exactly what is wrong.
func TestWorkspaceScreen_FileKind_Enter_DirectoryPath_ShowsNotAFileError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s := newWSFileScreen()
	s.SetPrefilledPath(dir)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the error must name the not-a-file condition, not the legacy not-a-directory wording.
	if !strings.Contains(view, "not a file") {
		t.Errorf("view does not contain 'not a file' after directory-path rejection in file-kind mode:\n%s", view)
	}
}

// TestWorkspaceScreen_FileKind_Enter_NonExistentPath_DoesNotSetDone verifies that a
// file-kind screen rejects a path that does not exist on the filesystem.
func TestWorkspaceScreen_FileKind_Enter_NonExistentPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	nonExistent := filepath.Join(t.TempDir(), "no-such-agent.md")
	s := newWSFileScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for non-existent path %q with file-kind validation; want false", nonExistent)
	}
}

// TestWorkspaceScreen_FileKind_Enter_NonExistentPath_ShowsFileDoesNotExistError verifies
// that a missing path in file-kind produces feedback that names the file-does-not-exist
// condition, not the directory equivalent, so the user knows which rule was violated.
func TestWorkspaceScreen_FileKind_Enter_NonExistentPath_ShowsFileDoesNotExistError(t *testing.T) {
	// Arrange
	nonExistent := filepath.Join(t.TempDir(), "no-such-agent.md")
	s := newWSFileScreen()
	s.SetPrefilledPath(nonExistent)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the error should mention that the file does not exist (not a directory).
	if !strings.Contains(view, "file does not exist") {
		t.Errorf("view does not contain 'file does not exist' after missing-path rejection in file-kind mode:\n%s", view)
	}
}

// TestWorkspaceScreen_FileKind_Enter_EmptyPath_DoesNotSetDone verifies that an empty path
// is rejected in file-kind mode, matching the directory-kind empty-path rule.
func TestWorkspaceScreen_FileKind_Enter_EmptyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSFileScreen()
	s.SetPrefilledPath("")

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true for empty path with file-kind validation; want false (empty path must be rejected)")
	}
}

// TestWorkspaceScreen_FileKind_Enter_WhitespaceOnlyPath_DoesNotSetDone verifies that a
// whitespace-only path is treated as empty and rejected in file-kind mode.
func TestWorkspaceScreen_FileKind_Enter_WhitespaceOnlyPath_DoesNotSetDone(t *testing.T) {
	// Arrange
	s := newWSFileScreen()
	s.SetPrefilledPath("   ")

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Error("Done() = true for whitespace-only path with file-kind validation; want false (whitespace must be treated as empty)")
	}
}

// TestWorkspaceScreen_FileKind_Enter_QuotedFilePath_SetsDone verifies that a quoted file
// path (as produced by Windows Explorer "Copy as path") is accepted in file-kind mode after
// the outer double quotes are stripped.
func TestWorkspaceScreen_FileKind_Enter_QuotedFilePath_SetsDone(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	s := newWSFileScreen()
	s.SetPrefilledPath(`"` + filePath + `"`)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for quoted file path %q with file-kind validation; want true (outer quotes must be stripped before validation)", `"`+filePath+`"`)
	}
}

// TestWorkspaceScreen_FileKind_WorkspacePath_QuotedFilePath_ReturnsUnquotedAbsPath verifies
// that WorkspacePath() returns the absolute, unquoted path when the user entered a
// Windows-Explorer-style quoted value in file-kind mode.
func TestWorkspaceScreen_FileKind_WorkspacePath_QuotedFilePath_ReturnsUnquotedAbsPath(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	s := newWSFileScreen()
	s.SetPrefilledPath(`"` + filePath + `"`)
	s.Update(wsEnterKey())

	// Act
	got := s.WorkspacePath()

	// Assert — the returned path must equal the absolute form of the bare file path.
	want, _ := filepath.Abs(filePath)
	if got != want {
		t.Errorf("WorkspacePath() = %q; want %q (outer quotes must be stripped and path resolved to absolute in file-kind mode)", got, want)
	}
}

// TestWorkspaceScreen_FileKind_View_ContainsNoDirectoryWording verifies that when the screen
// is in file-kind mode, the rendered view contains neither "directory" nor "workspace" wording.
// The design's Path-Kind Presentation Contract requires the file-kind input label to name an
// agent file path and contain neither of those words — a label such as "Enter workspace file
// path:" would violate the contract even though it omits "directory".
func TestWorkspaceScreen_FileKind_View_ContainsNoDirectoryWording(t *testing.T) {
	// Arrange
	s := newWSFileScreen()

	// Act
	view := s.View()
	lower := strings.ToLower(view)

	// Assert — neither "directory" nor "workspace" must appear anywhere in the file-kind view.
	if strings.Contains(lower, "directory") {
		t.Errorf("file-kind view contains 'directory' wording; want no occurrence of 'directory':\n%s", view)
	}
	if strings.Contains(lower, "workspace") {
		t.Errorf("file-kind view contains 'workspace' wording; want no occurrence of 'workspace':\n%s", view)
	}
}

// TestWorkspaceScreen_FileKind_Enter_UnreadableFile_DoesNotSetDone verifies that a file that
// exists but is not readable is rejected by the file-kind access probe. This exercises the
// "file is not readable: <abs>" error branch, mirroring the non-writable-directory test for
// the directory kind.
//
// On Windows, file read permissions are governed by ACLs which chmod cannot modify portably,
// so this test is skipped on that platform.
func TestWorkspaceScreen_FileKind_Enter_UnreadableFile_DoesNotSetDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file read permissions use Windows ACLs; chmod-based setup is not portable to this platform")
	}

	// Arrange — create a regular file, remove its read permission, then restore on cleanup.
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	if err := os.Chmod(filePath, 0o222); err != nil { // write-only, not readable
		t.Fatalf("failed to remove read permission from temp file: %v", err)
	}
	// Restore read permission so TempDir cleanup can read and remove the file.
	t.Cleanup(func() { os.Chmod(filePath, 0o644) }) //nolint:errcheck

	s := newWSFileScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for unreadable file %q; want false (unreadable file must be rejected by the access probe)", filePath)
	}
}

// TestWorkspaceScreen_FileKind_Enter_UnreadableFile_ShowsNotReadableError verifies that when
// a file exists but is not readable, the inline error names the not-readable condition so the
// user understands exactly why the path was rejected.
//
// On Windows, this test is skipped for the same reason as the companion Done() test.
func TestWorkspaceScreen_FileKind_Enter_UnreadableFile_ShowsNotReadableError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file read permissions use Windows ACLs; chmod-based setup is not portable to this platform")
	}

	// Arrange
	filePath := createTempFile(t, t.TempDir(), "agent.md", "content")
	if err := os.Chmod(filePath, 0o222); err != nil {
		t.Fatalf("failed to remove read permission from temp file: %v", err)
	}
	t.Cleanup(func() { os.Chmod(filePath, 0o644) }) //nolint:errcheck

	s := newWSFileScreen()
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())
	view := s.View()

	// Assert — the view must mention readability so the user understands how to fix it.
	if !strings.Contains(view, "readable") && !strings.Contains(view, "read") {
		t.Errorf("view does not mention readability after unreadable-file rejection:\n%s", view)
	}
}

// TestWorkspaceScreen_FileKind_Enter_SymlinkToDirectory_DoesNotSetDone verifies that a
// symlink pointing to a directory is rejected in file-kind mode. The design states: "Both
// kinds resolve symlinks by stat-ing the effective target, so a symlink to a directory fails
// the file kind." This test closes that explicitly named design edge case.
func TestWorkspaceScreen_FileKind_Enter_SymlinkToDirectory_DoesNotSetDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows requires elevated privileges; skipping on this platform")
	}

	// Arrange — create a real directory and a symlink pointing to it.
	targetDir := t.TempDir()
	linkPath := filepath.Join(t.TempDir(), "link-to-dir")
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink to directory: %v", err)
	}

	s := newWSFileScreen()
	s.SetPrefilledPath(linkPath)

	// Act
	s.Update(wsEnterKey())

	// Assert — stat follows the symlink and sees a directory; file-kind must reject it.
	if s.Done() {
		t.Errorf("Done() = true for symlink-to-directory %q in file-kind mode; want false (symlink resolves to a directory, which is not a regular file)", linkPath)
	}
}

// ---------------------------------------------------------------------------
// Directory-kind regression tests — T1.2
//
// These tests call SetPathKind(PathKindDirectory) explicitly to confirm that the
// default directory-kind behaviour is preserved when a path kind is given
// explicitly. They complement the pre-existing tests that omit SetPathKind
// entirely and confirm both entry points produce identical behaviour.
// ---------------------------------------------------------------------------

// TestWorkspaceScreen_DirKind_Explicit_ValidDir_SetsDone verifies that calling
// SetPathKind(PathKindDirectory) explicitly still accepts an existing writable directory,
// confirming that the default directory behaviour is not altered by setting it explicitly.
func TestWorkspaceScreen_DirKind_Explicit_ValidDir_SetsDone(t *testing.T) {
	// Arrange
	validDir := t.TempDir()
	s := newWSScreen()
	s.SetPathKind(screens.PathKindDirectory) // explicit, not relying on zero value
	s.SetPrefilledPath(validDir)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if !s.Done() {
		t.Errorf("Done() = false for valid writable directory %q with explicit PathKindDirectory; want true", validDir)
	}
}

// TestWorkspaceScreen_DirKind_Explicit_FilePath_DoesNotSetDone verifies that calling
// SetPathKind(PathKindDirectory) explicitly still rejects a regular file, confirming the
// directory-kind rule "must be a directory" is not accidentally removed.
func TestWorkspaceScreen_DirKind_Explicit_FilePath_DoesNotSetDone(t *testing.T) {
	// Arrange
	filePath := createTempFile(t, t.TempDir(), "regular-file.txt", "content")
	s := newWSScreen()
	s.SetPathKind(screens.PathKindDirectory) // explicit
	s.SetPrefilledPath(filePath)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for file path %q with explicit PathKindDirectory; want false (directory-kind must reject files)", filePath)
	}
}

// TestWorkspaceScreen_DirKind_Explicit_NonWritableDir_DoesNotSetDone verifies that calling
// SetPathKind(PathKindDirectory) explicitly still rejects a non-writable directory, confirming
// the writability probe is not accidentally removed.
//
// On Windows, directory write permissions are governed by ACLs which chmod cannot modify
// portably, so this test is skipped on that platform.
func TestWorkspaceScreen_DirKind_Explicit_NonWritableDir_DoesNotSetDone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions use Windows ACLs; chmod-based setup is not portable to this platform")
	}

	// Arrange
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("failed to remove write permission from temp dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	s := newWSScreen()
	s.SetPathKind(screens.PathKindDirectory) // explicit
	s.SetPrefilledPath(dir)

	// Act
	s.Update(wsEnterKey())

	// Assert
	if s.Done() {
		t.Errorf("Done() = true for non-writable directory %q with explicit PathKindDirectory; want false", dir)
	}
}
