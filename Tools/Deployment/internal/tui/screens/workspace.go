package screens

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/pathinput"
	"mosaic-deploy/internal/tui/widgets"
)

// PathKind selects what kind of filesystem object a path-entry screen accepts.
// The zero value is PathKindDirectory, so a screen that is never told otherwise keeps
// the directory behaviour it has always had.
type PathKind int

const (
	// PathKindDirectory accepts an existing, writable directory.
	PathKindDirectory PathKind = iota
	// PathKindFile accepts an existing, readable regular file.
	PathKindFile
	// PathKindFileOrDirectory accepts either an existing readable regular file or an
	// existing readable directory. Used by the harness-transform mode, whose input may be
	// a single agent file or a folder of them.
	//
	// Screen wording: label "Agent file or folder path:", placeholder
	// "/path/to/agent.md or /path/to/folder". Validation: non-empty after quote stripping;
	// the path exists; it is a readable regular file or a readable directory. Writability
	// is not required — this mode's output goes to the target harness's own resolved path.
	PathKindFileOrDirectory

	// PathKindCatalogFolder accepts an existing, READABLE directory. Writability is NOT
	// required — a catalogue is only ever read by the deploy flow.
	//
	// Screen wording: title "Catalogue Folder", label "Catalogue folder path:",
	// placeholder "/path/to/catalog". Guidance states that the folder supplies the agents
	// and workflows offered in this session and may override individual skills and hooks by
	// supplying same-keyed ones, and that the protocol document, the deployed-sections
	// bundle, and SourceFilesFormat.md always come from the MOSAIC root regardless.
	PathKindCatalogFolder
)

// WorkspaceScreen prompts the user for a workspace directory path and validates it before
// proceeding. Feedback is shown inline so the user never sees a silent failure or a crash.
//
// Validation rules (PathKindDirectory, the default):
//   1. Path must not be empty.
//   2. Path must point to an existing directory.
//   3. The directory must be writable (checked by probing a temporary file).
//
// Validation rules (PathKindFile):
//   1. Path must not be empty.
//   2. Path must point to an existing regular file (not a directory).
//   3. The file must be readable (checked by opening it for reading).
//
// Validation rules (PathKindCatalogFolder):
//   1. Path must not be empty.
//   2. Path must point to an existing directory (not a file).
//   3. The directory must be readable. Writability is NOT required.
//
// Navigation contract:
//   - Enter on a valid path -> Done() == true, WorkspacePath() returns the entered path.
//   - Esc                   -> Back() == true (caller returns to mode selection, preserving
//                              the previously entered value so prior valid input is not lost).
type WorkspaceScreen struct {
	input         *widgets.TextInput
	lastPath      string   // the last value the user entered, preserved across back navigation
	pathKind      PathKind // selects validation rules and on-screen wording; zero value is directory
	width         int
	height        int
	styles        Styles
	externalError string // shown inline after a caller-detected failure; cleared on next Update
}

// NewWorkspaceScreen creates the workspace path entry screen.
func NewWorkspaceScreen(width, height int, styles Styles) *WorkspaceScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Workspace directory path:",
		"/path/to/workspace",
		width,
		inputStyles,
	)
	input.SetValidate(validateWorkspacePath)

	return &WorkspaceScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

// SetPathKind selects the validation rules and the wording used by the screen.
// Call it before the first Update, and before or after SetPrefilledPath — the value
// currently held in the input field is preserved across the call.
// Calling it repeatedly is allowed; the last call before the next Update wins.
func (s *WorkspaceScreen) SetPathKind(kind PathKind) {
	s.pathKind = kind

	// Rebuild the input widget with label, placeholder, and validator matching the new kind.
	// The currently entered text is carried over so any pre-filled path is not lost.
	currentValue := s.input.Value()

	inputStyles := widgets.TextInputStyles{
		Label:  s.styles.Subtitle,
		Input:  s.styles.Body,
		ErrMsg: s.styles.Error,
	}

	var label, placeholder string
	var validateFn func(string) error

	switch kind {
	case PathKindFile:
		label = "Agent file path:"
		placeholder = "/path/to/agent.md"
		validateFn = validateFilePath
	case PathKindFileOrDirectory:
		label = "Agent file or folder path:"
		placeholder = "/path/to/agent.md or /path/to/folder"
		validateFn = validateFileOrDirPath
	case PathKindCatalogFolder:
		label = "Catalogue folder path:"
		placeholder = "/path/to/catalog"
		validateFn = validateCatalogFolderPath
	default: // PathKindDirectory
		label = "Workspace directory path:"
		placeholder = "/path/to/workspace"
		validateFn = validateWorkspacePath
	}

	input := widgets.NewTextInput(label, placeholder, s.width, inputStyles)
	input.SetValidate(validateFn)
	input.SetValue(currentValue)
	s.input = input
}

// validateWorkspacePath runs the three-step validation on the path the user typed.
func validateWorkspacePath(path string) error {
	path = pathinput.Unquote(strings.TrimSpace(path))
	if path == "" {
		return errors.New("path cannot be empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.New("path is not valid: " + err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("directory does not exist: " + abs)
		}
		return errors.New("cannot access path: " + err.Error())
	}
	if !info.IsDir() {
		return errors.New("path is not a directory: " + abs)
	}

	// Probe writability by creating and immediately deleting a temporary file.
	probe := filepath.Join(abs, ".mosaic-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("directory is not writable: " + abs)
	}
	f.Close()
	os.Remove(probe)

	return nil
}

// validateFilePath runs the three-step validation on the file path the user typed.
func validateFilePath(path string) error {
	path = pathinput.Unquote(strings.TrimSpace(path))
	if path == "" {
		return errors.New("path cannot be empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.New("path is not valid: " + err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("file does not exist: " + abs)
		}
		return errors.New("cannot access path: " + err.Error())
	}
	if info.IsDir() {
		return errors.New("path is not a file: " + abs)
	}

	// Probe readability by opening the file for reading and immediately closing it.
	f, err := os.Open(abs)
	if err != nil {
		return errors.New("file is not readable: " + abs)
	}
	f.Close()

	return nil
}

// validateFileOrDirPath runs validation for PathKindFileOrDirectory: the path must be
// non-empty, exist on disk, and be either a readable regular file or a readable directory.
// Writability is not required — the transform mode writes to the target harness's own path.
func validateFileOrDirPath(path string) error {
	path = pathinput.Unquote(strings.TrimSpace(path))
	if path == "" {
		return errors.New("path cannot be empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.New("path is not valid: " + err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("path does not exist: " + abs)
		}
		return errors.New("cannot access path: " + err.Error())
	}

	if info.IsDir() {
		// Directory: probe readability by opening it.
		f, openErr := os.Open(abs)
		if openErr != nil {
			return errors.New("directory is not readable: " + abs)
		}
		f.Close()
		return nil
	}

	// Regular file: probe readability by opening for reading.
	f, openErr := os.Open(abs)
	if openErr != nil {
		return errors.New("file is not readable: " + abs)
	}
	f.Close()
	return nil
}

// validateCatalogFolderPath validates that the path is an existing, readable directory.
// Writability is not checked — a catalogue folder is only ever read by the deploy flow.
func validateCatalogFolderPath(path string) error {
	path = pathinput.Unquote(strings.TrimSpace(path))
	if path == "" {
		return errors.New("path cannot be empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return errors.New("path is not valid: " + err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("directory does not exist: " + abs)
		}
		return errors.New("cannot access path: " + err.Error())
	}
	if !info.IsDir() {
		return errors.New("path is not a directory: " + abs)
	}

	// Probe readability by opening the directory — no writability check for catalogue folders.
	f, openErr := os.Open(abs)
	if openErr != nil {
		return errors.New("directory is not readable: " + abs)
	}
	f.Close()
	return nil
}

// Update processes a key message and delegates to the text input widget.
func (s *WorkspaceScreen) Update(msg tea.Msg) tea.Cmd {
	s.externalError = "" // clear any caller-injected error on the first user interaction
	cmd := s.input.Update(msg)
	if s.input.Done() || s.input.Back() {
		s.lastPath = s.input.Value()
	}
	return cmd
}

// View renders the path entry screen with wording appropriate to the active path kind.
func (s *WorkspaceScreen) View() string {
	var title, subtitle, guidanceText string

	switch s.pathKind {
	case PathKindFile:
		title = "Agent File Path"
		subtitle = "Enter the path to the harness-only agent file to promote."
		guidanceText = "The path must be an existing, readable regular file.\n" +
			"This is typically a .md file describing the agent.\n"
	case PathKindFileOrDirectory:
		title = "Agent File or Folder Path"
		subtitle = "Enter the path to an agent file or a folder of agent files."
		guidanceText = "The path must be an existing, readable file or directory.\n" +
			"For a folder, only files directly under it are processed (non-recursive).\n"
	case PathKindCatalogFolder:
		title = "Catalogue Folder"
		subtitle = "Enter the catalogue directory supplying agents and workflows for this session."
		guidanceText = "The folder supplies the agents and workflows offered in this session.\n" +
			"It may also override individual skills and hooks by supplying same-keyed ones.\n" +
			"The protocol document, the deployed-sections bundle, and SourceFilesFormat.md\n" +
			"always come from the MOSAIC root regardless of the catalogue folder.\n"
	default: // PathKindDirectory
		title = "Workspace Path"
		subtitle = "Enter the directory where agents will be deployed."
		guidanceText = "The workspace must be an existing, writable directory.\n" +
			"This is typically the root of your project repository.\n"
	}

	titleLine := s.styles.Title.Width(s.width).Render(title)
	subtitleLine := s.styles.Subtitle.Width(s.width).Render(subtitle)
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	guidance := s.styles.Muted.Width(s.width).Render(guidanceText)
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")

	parts := []string{titleLine, subtitleLine, border, guidance, inputView}
	if s.externalError != "" {
		errLine := s.styles.Error.Width(s.width).Render(s.externalError)
		parts = append(parts, errLine)
	}
	parts = append(parts, border, help)
	return strings.Join(parts, "\n")
}

// Done reports whether the user confirmed a valid workspace path.
func (s *WorkspaceScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc to go back.
func (s *WorkspaceScreen) Back() bool { return s.input.Back() }

// WorkspacePath returns the validated workspace path. Only valid when Done() is true.
func (s *WorkspaceScreen) WorkspacePath() string {
	p := pathinput.Unquote(strings.TrimSpace(s.input.Value()))
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// SetPrefilledPath sets the initial value of the text field. Call before the first Update.
func (s *WorkspaceScreen) SetPrefilledPath(path string) {
	s.input.SetValue(path)
	s.lastPath = path
}

// Resize updates the screen dimensions.
func (s *WorkspaceScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}

// Reset clears the done and back flags without clearing the entered path.
func (s *WorkspaceScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start the text input cursor blink. The root
// model calls this when it transitions to the workspace screen.
func (s *WorkspaceScreen) InputInit() tea.Cmd { return s.input.Init() }

// SetExternalError displays msg as the screen's inline error and holds the user on the
// screen. Used to surface a failure that only the caller can detect — a failed catalogue
// reload. The message is cleared on the next Update call.
func (s *WorkspaceScreen) SetExternalError(msg string) {
	s.externalError = msg
}
