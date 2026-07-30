package screens

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
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
// RunSelectScreen
// ---------------------------------------------------------------------------

// NewRunSentinelID is the list-item ID used for the synthetic "Start new run"
// entry. It is deliberately not a valid run_id (contains no timestamp or hex
// suffix) so it can never collide with a real RunCandidate's RunID.
const NewRunSentinelID = "__new_run__"

// RunSelectScreen lets the user select a resumable run or start a new one.
//
// Navigation contract:
//   - Enter on a candidate -> Done() == true, SelectedCandidate() returns the chosen RunCandidate.
//   - Enter on the "Start new run" item -> Done() == true, IsNewRun() == true.
//   - Esc -> Back() == true.
type RunSelectScreen struct {
	list       *widgets.List
	candidates []runscan.RunCandidate
	width      int
	height     int
	styles     Styles
}

// NewRunSelectScreen creates the run selection screen.
// candidates must be non-empty (caller should skip this screen when len == 0 or 1).
// A synthetic "Start new run" item is prepended to the list with ID = NewRunSentinelID.
func NewRunSelectScreen(candidates []runscan.RunCandidate, width, height int, styles Styles) *RunSelectScreen {
	items := make([]widgets.ListItem, 0, len(candidates)+1)

	// Prepend the "Start new run" entry.
	items = append(items, widgets.ListItem{
		ID:    NewRunSentinelID,
		Label: "Start a new run",
	})

	// Append each resumable candidate.
	for _, c := range candidates {
		label := c.RunID
		detail := ""
		if c.Workflow != "" {
			detail += c.Workflow
		}
		if c.Task != "" {
			if detail != "" {
				detail += " — "
			}
			detail += c.Task
		}
		if !c.LastUpdated.IsZero() {
			if detail != "" {
				detail += "  "
			}
			detail += c.LastUpdated.Format("2006-01-02 15:04:05 UTC")
		}
		if c.ParseError != nil {
			detail = "(unreadable: " + c.ParseError.Error() + ")"
		}
		items = append(items, widgets.ListItem{
			ID:     c.RunID,
			Label:  label,
			Detail: detail,
		})
	}

	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	contentH := height - 6
	if contentH < 1 {
		contentH = 1
	}
	list := widgets.NewList(items, contentH, width, listStyles)

	return &RunSelectScreen{
		list:       list,
		candidates: candidates,
		width:      width,
		height:     height,
		styles:     styles,
	}
}

// Update processes a key message and delegates to the list widget.
func (s *RunSelectScreen) Update(msg tea.Msg) tea.Cmd {
	s.list.Update(msg)
	return nil
}

// View renders the run selection screen with title, candidate list, and help bar.
func (s *RunSelectScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Run")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Choose a resumable run or start a new one.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	listView := s.list.View()
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc quit  ctrl+c quit")
	return strings.Join([]string{title, subtitle, border, listView, border, help}, "\n")
}

// Done reports whether the user selected an item.
func (s *RunSelectScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed Esc.
func (s *RunSelectScreen) Back() bool { return s.list.Back() }

// SelectedID returns the ID of the selected list item. Only valid when Done() is true.
func (s *RunSelectScreen) SelectedID() string { return s.list.SelectedID() }

// SelectedCandidate returns the selected RunCandidate.
// Only valid when Done() == true and IsNewRun() == false.
// Returns nil when IsNewRun() is true.
func (s *RunSelectScreen) SelectedCandidate() *runscan.RunCandidate {
	if s.IsNewRun() {
		return nil
	}
	id := s.list.SelectedID()
	for i := range s.candidates {
		if s.candidates[i].RunID == id {
			return &s.candidates[i]
		}
	}
	return nil
}

// IsNewRun reports whether the user chose the "Start new run" option.
// Only valid when Done() == true.
func (s *RunSelectScreen) IsNewRun() bool {
	return s.list.SelectedID() == NewRunSentinelID
}

// Reset clears the done and back flags.
func (s *RunSelectScreen) Reset() { s.list.Reset() }

// Resize updates the screen dimensions and reflows the list.
func (s *RunSelectScreen) Resize(width, height int) {
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
	AllowVersionDrift bool
	Checkpoints       bool
	Harness           string        // "fake" or "claude-code"
	Timeout           time.Duration // invocation timeout (only relevant when Harness == "claude-code")
}

// configStep identifies which configuration prompt is currently active.
type configStep int

const (
	configStepDeviation     configStep = iota
	configStepHarness                  // harness adapter selection
	configStepHarnessTimeout           // timeout entry (only when claude-code is selected)
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
//   - The harness timeout step is only shown when "Claude Code CLI" is selected.
type ConfigScreen struct {
	step         configStep
	back         bool
	sel          ConfigSelection
	cursor       int
	width        int
	height       int
	styles       Styles
	timeoutInput *widgets.TextInput
}

// NewConfigScreen creates the configuration screen.
func NewConfigScreen(width, height int, styles Styles) *ConfigScreen {
	inputStyles := widgets.TextInputStyles{
		Label:  styles.Subtitle,
		Input:  styles.Body,
		ErrMsg: styles.Error,
	}
	timeoutInput := widgets.NewTextInput(
		"Invocation timeout (e.g. 30m, 1h):",
		"30m",
		width,
		inputStyles,
	)
	timeoutInput.SetValidate(func(v string) error {
		v = strings.TrimSpace(v)
		if v == "" {
			return errors.New("timeout cannot be empty")
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid duration %q: must be like 30m, 1h, 90m", v)
		}
		if d <= 0 {
			return errors.New("timeout must be greater than zero")
		}
		return nil
	})
	return &ConfigScreen{
		step:         configStepDeviation,
		sel:          ConfigSelection{DeviationMode: domain.DeviationDelegate},
		width:        width,
		height:       height,
		styles:       styles,
		timeoutInput: timeoutInput,
	}
}

// Update processes a key message for the current configuration prompt.
func (s *ConfigScreen) Update(msg tea.Msg) tea.Cmd {
	// The harness timeout step delegates all key handling to the text input widget.
	if s.step == configStepHarnessTimeout {
		cmd := s.timeoutInput.Update(msg)
		if s.timeoutInput.Back() {
			s.timeoutInput.Reset()
			s.step = configStepHarness
			s.cursor = 0
			return nil
		}
		if s.timeoutInput.Done() {
			durStr := strings.TrimSpace(s.timeoutInput.Value())
			if d, err := time.ParseDuration(durStr); err == nil && d > 0 {
				s.sel.Timeout = d
			} else {
				s.sel.Timeout = 30 * time.Minute
			}
			s.timeoutInput.Reset()
			s.step = configStepVersionDrift
			s.cursor = 0
			return nil
		}
		return cmd
	}

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
		case configStepDeviation, configStepHarness, configStepVersionDrift, configStepCheckpoints:
			if s.cursor < 1 {
				s.cursor++
			}
		}
	case "enter":
		return s.advance()
	case "esc":
		if s.step == configStepDeviation {
			s.back = true
		} else if s.step == configStepVersionDrift && s.sel.Harness != "claude-code" {
			// When fake harness was chosen, the timeout step was skipped; go back to harness.
			s.step = configStepHarness
			s.cursor = 0
		} else {
			s.step--
			s.cursor = 0
		}
	}
	return nil
}

// advance commits the current selection and moves to the next step.
// It returns a tea.Cmd when the harness timeout text input needs to start blinking.
func (s *ConfigScreen) advance() tea.Cmd {
	switch s.step {
	case configStepDeviation:
		if s.cursor == 0 {
			s.sel.DeviationMode = domain.DeviationDelegate
		} else {
			s.sel.DeviationMode = domain.DeviationStop
		}
		s.step = configStepHarness
		s.cursor = 0
	case configStepHarness:
		if s.cursor == 0 {
			s.sel.Harness = "fake"
			s.step = configStepVersionDrift // skip timeout for fake harness
		} else {
			s.sel.Harness = "claude-code"
			s.step = configStepHarnessTimeout
			s.timeoutInput.Reset()
			return s.timeoutInput.Init() // start cursor blink in text input
		}
		s.cursor = 0
	case configStepVersionDrift:
		s.sel.AllowVersionDrift = s.cursor == 0
		s.step = configStepCheckpoints
		s.cursor = 0
	case configStepCheckpoints:
		s.sel.Checkpoints = s.cursor == 0
		s.step = configStepDone
	}
	return nil
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
	case configStepHarness:
		body.WriteString(s.styles.Body.Width(s.width).Render("Harness adapter:") + "\n")
		body.WriteString(s.renderOption(0, "Fake (scripted) (default)"))
		body.WriteString(s.renderOption(1, "Claude Code CLI"))
	case configStepHarnessTimeout:
		body.WriteString(s.timeoutInput.View())
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
	s.sel = ConfigSelection{DeviationMode: domain.DeviationDelegate}
	s.timeoutInput.Reset()
}

// Resize updates the screen dimensions.
func (s *ConfigScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.timeoutInput.Resize(width)
}
