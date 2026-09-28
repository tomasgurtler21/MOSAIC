package setup

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// TaskScreen
// ---------------------------------------------------------------------------

// TaskScreen prompts the user to enter the task description.
//
// Navigation contract:
//   - Enter on a non-empty description -> Done() == true, Task() returns the description.
//   - Esc -> Back() == true.
type TaskScreen struct {
	input  *widgets.TextInput
	width  int
	height int
	styles screens.Styles
}

// NewTaskScreen creates the task description entry screen.
func NewTaskScreen(width, height int, styles screens.Styles) *TaskScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Task description:",
		"A short label identifying this run (shown in run history, not sent to agents)",
		width,
		inputStyles,
	)
	input.SetValidate(func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("task description cannot be empty")
		}
		return nil
	})
	return &TaskScreen{
		input:  input,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes a key message.
func (s *TaskScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the task entry screen.
func (s *TaskScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Task Description")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Enter a short label to identify this run in history. This text is not sent to agents as an instruction.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	inputView := s.input.View()
	help := s.styles.Help.Width(s.width).Render("enter confirm  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, inputView, border, help}, "\n")
}

// Done reports whether the user confirmed the task description.
func (s *TaskScreen) Done() bool { return s.input.Done() }

// Back reports whether the user pressed Esc.
func (s *TaskScreen) Back() bool { return s.input.Back() }

// Task returns the entered task description. Only valid when Done() is true.
func (s *TaskScreen) Task() string { return strings.TrimSpace(s.input.Value()) }

// Reset clears the done and back flags.
func (s *TaskScreen) Reset() { s.input.Reset() }

// InputInit returns the command required to start cursor blinking.
func (s *TaskScreen) InputInit() tea.Cmd { return s.input.Init() }

// Resize updates the screen dimensions.
func (s *TaskScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.input.Resize(width)
}
