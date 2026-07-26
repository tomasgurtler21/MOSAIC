package screens

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/tui/widgets"
)

// WorkspaceScreen prompts the user for a workspace directory path and validates it before
// proceeding. Feedback is shown inline so the user never sees a silent failure or a crash.
//
// Validation rules:
//   1. Path must not be empty.
//   2. Path must point to an existing directory.
//   3. The directory must be writable (checked by probing a temporary file).
//
// Navigation contract:
//   - Enter on a valid path -> Done() == true, WorkspacePath() returns the entered path.
//   - Esc                   -> Back() == true (caller returns to mode selection, preserving
//                              the previously entered value so prior valid input is not lost).
type WorkspaceScreen struct {
	input    *widgets.TextInput
	lastPath string // the last value the user entered, preserved across back navigation
	width    int
	height   int
	styles   Styles
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

// validateWorkspacePath runs the three-step validation on the path the user typed.
func validateWorkspacePath(path string) error {
	path = strings.TrimSpace(path)
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

// Update processes a key message and delegates to the text input widget.
func (s *WorkspaceScreen) Update(msg tea.Msg) tea.Cmd {
	cmd := s.input.Update(msg)
	if s.input.Done() || s.input.Back() {
		s.lastPath = s.input.Value()
	}
	return cmd
}

// View renders the workspace entry screen.
func (s *WorkspaceScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Workspace Path")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Enter the directory where agents will be deployed.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	guidance := s.styles.Muted.Width(s.width).Render(
		"The workspace must be an existing, writable directory.\n" +
			"This is typically the root of your project repository.\n",
	)

	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")

	return strings.Join([]string{title, subtitle, border, guidance, inputView, border, help}, "\n")
}

// Done reports whether the user confirmed a valid workspace path.
func (s *WorkspaceScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc to go back.
func (s *WorkspaceScreen) Back() bool { return s.input.Back() }

// WorkspacePath returns the validated workspace path. Only valid when Done() is true.
func (s *WorkspaceScreen) WorkspacePath() string {
	p := strings.TrimSpace(s.input.Value())
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
