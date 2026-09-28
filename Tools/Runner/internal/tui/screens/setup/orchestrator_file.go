package setup

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/tui/screens"
)

// OrchestratorFileScreen prompts the user to enter the path to the orchestrator agent file.
//
// Navigation contract:
//   - Enter on a non-empty path that exists -> Done() == true, FilePath() returns the path.
//   - Esc -> Back() == true.
type OrchestratorFileScreen struct {
	input       *widgets.TextInput
	width       int
	height      int
	styles      screens.Styles
	toolVersion string
}

// NewOrchestratorFileScreen creates the orchestrator file path entry screen.
func NewOrchestratorFileScreen(width, height int, styles screens.Styles) *OrchestratorFileScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Orchestrator agent file path:",
		"/path/to/orchestrator.md",
		width,
		inputStyles,
	)
	input.SetValidate(validateOrchestratorFile)
	return &OrchestratorFileScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

func validateOrchestratorFile(path string) error {
	path = screens.NormalizePath(path)
	if path == "" {
		return errors.New("path cannot be empty")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		return fmt.Errorf("cannot access file: %v", err)
	}
	return nil
}

// SetToolVersion sets the tool version string displayed in the screen title.
// Call before the first View() for the version to appear on entry.
func (s *OrchestratorFileScreen) SetToolVersion(v string) {
	s.toolVersion = v
}

// Update processes a key message and delegates to the text input widget.
func (s *OrchestratorFileScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the orchestrator file entry screen.
func (s *OrchestratorFileScreen) View() string {
	titleText := "Orchestrator File"
	if s.toolVersion != "" {
		titleText = "MOSAIC Runner v" + s.toolVersion + " -- Orchestrator File"
	}
	title := s.styles.Title.Width(s.width).Render(titleText)
	subtitle := s.styles.Subtitle.Width(s.width).Render("Enter the path to the orchestrator agent file.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	guidance := s.styles.Muted.Width(s.width).Render("The file must be an existing .md agent file.\n")
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, guidance, inputView, border, help}, "\n")
}

// Done reports whether the user confirmed a valid file path.
func (s *OrchestratorFileScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc.
func (s *OrchestratorFileScreen) Back() bool { return s.input.Back() }

// FilePath returns the entered file path. Only valid when Done() is true.
func (s *OrchestratorFileScreen) FilePath() string {
	return screens.NormalizePath(s.input.Value())
}

// Reset clears the done and back flags.
func (s *OrchestratorFileScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start cursor blinking.
func (s *OrchestratorFileScreen) InputInit() tea.Cmd { return s.input.Init() }

// Resize updates the screen dimensions.
func (s *OrchestratorFileScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}
