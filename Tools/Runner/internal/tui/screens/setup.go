package screens

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/domain"
)

// OrchestratorFileScreen prompts the user to enter the path to the orchestrator agent file.
//
// Navigation contract:
//   - Enter on a non-empty path that exists -> Done() == true, FilePath() returns the path.
//   - Esc -> Back() == true.
type OrchestratorFileScreen struct {
	input  *widgets.TextInput
	width  int
	height int
	styles Styles
}

// NewOrchestratorFileScreen creates the orchestrator file path entry screen.
func NewOrchestratorFileScreen(width, height int, styles Styles) *OrchestratorFileScreen {
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
	path = strings.TrimSpace(path)
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

// Update processes a key message and delegates to the text input widget.
func (s *OrchestratorFileScreen) Update(msg tea.Msg) tea.Cmd {
	return s.input.Update(msg)
}

// View renders the orchestrator file entry screen.
func (s *OrchestratorFileScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Orchestrator File")
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
func (s *OrchestratorFileScreen) FilePath() string { return strings.TrimSpace(s.input.Value()) }

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

// ---------------------------------------------------------------------------
// WorkflowSelectScreen
// ---------------------------------------------------------------------------

// WorkflowSelectScreen lets the user select a workflow from enumerated regions.
//
// Navigation contract:
//   - Enter on a workflow -> Done() == true, SelectedID() returns the workflow ID.
//   - Esc -> Back() == true.
type WorkflowSelectScreen struct {
	list   *widgets.List
	width  int
	height int
	styles Styles
}

// NewWorkflowSelectScreen creates the workflow selection screen.
func NewWorkflowSelectScreen(workflows []domain.WorkflowRegion, width, height int, styles Styles) *WorkflowSelectScreen {
	items := make([]widgets.ListItem, len(workflows))
	for i, wf := range workflows {
		items[i] = widgets.ListItem{
			ID:    string(wf.Info.ID),
			Label: string(wf.Info.ID),
			Detail: fmt.Sprintf("version: %s", wf.Info.Version),
		}
	}
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	contentH := height - 6 // reserve title + border + help
	if contentH < 1 {
		contentH = 1
	}
	list := widgets.NewList(items, contentH, width, listStyles)
	return &WorkflowSelectScreen{
		list:   list,
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes a key message.
func (s *WorkflowSelectScreen) Update(msg tea.Msg) tea.Cmd {
	s.list.Update(msg)
	return nil
}

// View renders the workflow selection screen.
func (s *WorkflowSelectScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Workflow")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose the workflow to run.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.list.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user selected a workflow.
func (s *WorkflowSelectScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed Esc.
func (s *WorkflowSelectScreen) Back() bool { return s.list.Back() }

// SelectedID returns the selected workflow ID. Only valid when Done() is true.
func (s *WorkflowSelectScreen) SelectedID() string { return s.list.SelectedID() }

// Reset clears the done and back flags.
func (s *WorkflowSelectScreen) Reset() { s.list.Reset() }

// Resize updates the screen dimensions.
func (s *WorkflowSelectScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	s.list.Resize(contentH, width)
}

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
	styles Styles
}

// NewTaskScreen creates the task description entry screen.
func NewTaskScreen(width, height int, styles Styles) *TaskScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	input := widgets.NewTextInput(
		"Task description:",
		"Describe what this workflow run should accomplish",
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
	subtitle := s.styles.Subtitle.Width(s.width).Render("Describe what this workflow run should accomplish.")
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

// ---------------------------------------------------------------------------
// ConfigScreen
// ---------------------------------------------------------------------------

// ConfigSelection holds the user's choices from the configuration screen.
type ConfigSelection struct {
	DeviationMode     domain.DeviationMode
	ExistingArtifact  domain.ExistingArtifactMode
	AllowVersionDrift bool
	Checkpoints       bool
}

// configStep identifies which configuration prompt is currently active.
type configStep int

const (
	configStepDeviation configStep = iota
	configStepExisting
	configStepVersionDrift
	configStepCheckpoints
	configStepDone
)

// ConfigScreen presents the run configuration prompts sequentially.
//
// Navigation contract:
//   - After all prompts are answered -> Done() == true, Selection() returns the choices.
//   - Esc on first prompt -> Back() == true.
//   - Esc on subsequent prompts -> goes back to the previous prompt.
type ConfigScreen struct {
	step      configStep
	back      bool
	sel       ConfigSelection
	cursor    int
	width     int
	height    int
	styles    Styles
}

// NewConfigScreen creates the configuration screen.
func NewConfigScreen(width, height int, styles Styles) *ConfigScreen {
	return &ConfigScreen{
		step:   configStepDeviation,
		sel:    ConfigSelection{
			DeviationMode:    domain.DeviationDelegate,
			ExistingArtifact: domain.ExistingResume,
		},
		width:  width,
		height: height,
		styles: styles,
	}
}

// Update processes a key message for the current configuration prompt.
func (s *ConfigScreen) Update(msg tea.Msg) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		switch s.step {
		case configStepDeviation:
			if s.cursor < 1 {
				s.cursor++
			}
		case configStepExisting:
			if s.cursor < 2 {
				s.cursor++
			}
		case configStepVersionDrift, configStepCheckpoints:
			if s.cursor < 1 {
				s.cursor++
			}
		}
	case "enter":
		s.advance()
	case "esc":
		if s.step == configStepDeviation {
			s.back = true
		} else {
			s.step--
			s.cursor = 0
		}
	}
	return nil
}

// advance commits the current selection and moves to the next step.
func (s *ConfigScreen) advance() {
	switch s.step {
	case configStepDeviation:
		if s.cursor == 0 {
			s.sel.DeviationMode = domain.DeviationDelegate
		} else {
			s.sel.DeviationMode = domain.DeviationStop
		}
		s.step = configStepExisting
		s.cursor = 0
	case configStepExisting:
		switch s.cursor {
		case 0:
			s.sel.ExistingArtifact = domain.ExistingResume
		case 1:
			s.sel.ExistingArtifact = domain.ExistingFresh
		case 2:
			s.sel.ExistingArtifact = domain.ExistingFail
		}
		s.step = configStepVersionDrift
		s.cursor = 0
	case configStepVersionDrift:
		s.sel.AllowVersionDrift = s.cursor == 0
		s.step = configStepCheckpoints
		s.cursor = 0
	case configStepCheckpoints:
		s.sel.Checkpoints = s.cursor == 0
		s.step = configStepDone
	}
}

// View renders the current configuration prompt.
func (s *ConfigScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Configuration")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Configure run options.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")

	var body strings.Builder
	switch s.step {
	case configStepDeviation:
		body.WriteString(s.styles.Body.Width(s.width).Render("Deviation handling:") + "\n")
		body.WriteString(s.renderOption(0, "Delegate to orchestrator (default)"))
		body.WriteString(s.renderOption(1, "Stop the run"))
	case configStepExisting:
		body.WriteString(s.styles.Body.Width(s.width).Render("Existing artifact:") + "\n")
		body.WriteString(s.renderOption(0, "Resume (default)"))
		body.WriteString(s.renderOption(1, "Start fresh"))
		body.WriteString(s.renderOption(2, "Fail if exists"))
	case configStepVersionDrift:
		body.WriteString(s.styles.Body.Width(s.width).Render("Allow workflow version drift:") + "\n")
		body.WriteString(s.renderOption(0, "Yes"))
		body.WriteString(s.renderOption(1, "No (default)"))
	case configStepCheckpoints:
		body.WriteString(s.styles.Body.Width(s.width).Render("Checkpoints:") + "\n")
		body.WriteString(s.renderOption(0, "Enabled"))
		body.WriteString(s.renderOption(1, "Disabled (default)"))
	}

	return strings.Join([]string{title, subtitle, border, body.String(), border, help}, "\n")
}

// renderOption renders one selectable option with the current cursor position highlighted.
func (s *ConfigScreen) renderOption(idx int, label string) string {
	prefix := "  "
	if idx == s.cursor {
		prefix = "▶ "
		return prefix + s.styles.Selected.Render(label) + "\n"
	}
	return prefix + s.styles.Body.Render(label) + "\n"
}

// Done reports whether all configuration prompts have been answered.
func (s *ConfigScreen) Done() bool { return s.step == configStepDone }

// Back reports whether the user pressed Esc on the first prompt.
func (s *ConfigScreen) Back() bool { return s.back }

// Selection returns the collected configuration. Only valid when Done() is true.
func (s *ConfigScreen) Selection() ConfigSelection { return s.sel }

// Reset clears the done, back flags and returns to the first step.
func (s *ConfigScreen) Reset() {
	s.step = configStepDeviation
	s.back = false
	s.cursor = 0
	s.sel = ConfigSelection{
		DeviationMode:    domain.DeviationDelegate,
		ExistingArtifact: domain.ExistingResume,
	}
}

// Resize updates the screen dimensions.
func (s *ConfigScreen) Resize(width, height int) {
	s.width = width
	s.height = height
}
