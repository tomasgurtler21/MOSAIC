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
	"mosaic-run/internal/harness"
	"mosaic-run/internal/runselect"
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

// normalizeOrchestratorPath applies the standard normalisation rules to a raw
// orchestrator file path:
//  1. Trim surrounding whitespace (including trailing newlines from paste).
//  2. If the result is at least two characters long and begins and ends with
//     the same quote character (" or '), remove that matched pair.
//  3. No further processing — interior quotes, separators, etc. are left intact.
func normalizeOrchestratorPath(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' || first == '\'') && first == last {
			s = s[1 : len(s)-1]
		}
	}
	return s
}

func validateOrchestratorFile(path string) error {
	path = normalizeOrchestratorPath(path)
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
func (s *OrchestratorFileScreen) FilePath() string {
	return normalizeOrchestratorPath(s.input.Value())
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
			ID:     string(wf.Info.ID),
			Label:  string(wf.Info.ID),
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
// suffix) so it can never collide with a real run_id. Equal to
// runselect.NewRunChoiceID.
const NewRunSentinelID = runselect.NewRunChoiceID

// RunSelectScreen lets the user select a resumable run or start a new one.
//
// Navigation contract:
//   - Enter on a selectable choice -> Done() == true, SelectedChoiceID() returns its ID.
//   - Enter on the "Start new run" item -> Done() == true, IsNewRun() == true.
//   - Esc -> Back() == true.
type RunSelectScreen struct {
	list    *widgets.List
	choices []runselect.Choice
	width   int
	height  int
	styles  Styles
}

// NewRunSelectScreen creates the run selection screen from a selection
// question.
//
// The list is: the "Start a new run" item first, then every selectable
// choice in question order, then every non-selectable choice, each rendered
// with the reason it cannot be resumed. Non-selectable items are skipped by
// cursor movement and cannot be activated.
//
// question.Choices may contain zero selectable entries; the screen is still
// valid and shows only "Start a new run" plus any unresumable entries.
func NewRunSelectScreen(question runselect.Question, width, height int, styles Styles) *RunSelectScreen {
	var selectable []runselect.Choice
	var unresumable []runselect.Choice
	for _, c := range question.Choices {
		switch c.Kind {
		case runselect.ChoiceResume:
			selectable = append(selectable, c)
		case runselect.ChoiceUnresumable:
			unresumable = append(unresumable, c)
		}
	}

	items := make([]widgets.ListItem, 0, len(selectable)+1)

	// Prepend the "Start new run" entry.
	items = append(items, widgets.ListItem{
		ID:    NewRunSentinelID,
		Label: "Start a new run",
	})

	// Append each resumable choice.
	for _, c := range selectable {
		label := c.Run.RunID
		detail := ""
		if c.Run.Workflow != "" {
			detail += c.Run.Workflow
		}
		if c.Run.Task != "" {
			if detail != "" {
				detail += " — "
			}
			detail += c.Run.Task
		}
		if !c.Run.LastUpdated.IsZero() {
			if detail != "" {
				detail += "  "
			}
			detail += c.Run.LastUpdated.Format("2006-01-02 15:04:05 UTC")
		}
		items = append(items, widgets.ListItem{
			ID:     c.ID,
			Label:  label,
			Detail: detail,
		})
	}

	// Append each unresumable choice, disabled, with its reason shown.
	for _, c := range unresumable {
		items = append(items, widgets.ListItem{
			ID:             c.ID,
			Label:          c.Run.RunID,
			Disabled:       true,
			DisabledReason: c.Reason.Description(),
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
		list:    list,
		choices: question.Choices,
		width:   width,
		height:  height,
		styles:  styles,
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

// SelectedChoiceID returns the ID of the activated choice. Valid only when
// Done() is true. Equals NewRunSentinelID when the user chose a new run.
func (s *RunSelectScreen) SelectedChoiceID() string { return s.list.SelectedID() }

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

// ---------------------------------------------------------------------------
// ConfigScreen
// ---------------------------------------------------------------------------

// ConfigSelection holds the user's choices from the configuration screen.
type ConfigSelection struct {
	// Settings holds the run configuration collected by the wizard. It is the
	// same type the CLI produces from its flags, which is what makes surface
	// parity a single struct comparison rather than a field-by-field audit.
	Settings domain.RunSettings

	AllowVersionDrift bool

	// Harness names the harness adapter. The TUI produces any CLI-backed
	// harness identity from harness.CLISelections() (the selection the user's
	// cursor rested on). The CLI --harness flag additionally accepts the
	// tool-local "fake" test double, and an unrecognised or zero value maps
	// to the fake adapter for test and backward-compatibility paths.
	Harness string

	Timeout time.Duration // invocation timeout

	// InfraClassSelections maps gated infrastructure class names (e.g. "checkpoint",
	// "commit") to the selected agent name for this run. Populated by the
	// configStepInfraClass step when multiple agents of the same gated class are
	// declared. Nil when no filtering is needed (single agent per class or no gated
	// class agents declared).
	InfraClassSelections map[string]string
}

// configStep identifies which configuration prompt is currently active.
type configStep int

const (
	// configStepMode is the first step: the user selects the execution mode.
	// No option is preselected; the step cannot advance without an explicit choice.
	configStepMode configStep = iota

	configStepHarness        // harness adapter selection
	configStepHarnessTimeout // timeout entry (shown for any CLI-backed harness)
	configStepVersionDrift
	configStepCheckpoints

	// configStepCommits is shown only when a Class=commit agent is declared.
	// Default: disabled.
	configStepCommits

	// configStepCommitBranch is shown only when commits are enabled at configStepCommits.
	// Default: mosaic-owned (presented as recommended).
	configStepCommitBranch

	// configStepPreConsult is shown only when mode is auto or auto-review.
	// Default: disabled.
	configStepPreConsult

	// configStepManualResolution is shown always. Default: disabled.
	configStepManualResolution

	configStepInfraClass // agent-per-class selection (only when multiple same-class gated agents)
	configStepDone
)

// infraClassEntry holds the class name and the in-declaration-order list of
// agent names for one gated class that has multiple declared agents.
type infraClassEntry struct {
	class  string
	agents []string
}

// ConfigScreen presents the run configuration prompts sequentially.
//
// Navigation contract:
//   - After all prompts are answered -> Done() == true, Selection() returns the choices.
//   - Esc on first prompt (mode step) -> Back() == true.
//   - Esc on subsequent prompts -> goes back to the previous prompt.
//   - The harness timeout step is shown for any CLI-backed harness selection,
//     since every CLI harness spawns a timed subprocess.
//   - The infra-class step is only shown for gated classes with multiple declared agents.
//   - Conditional steps (commits, commit-branch, pre-consult) are skipped in both
//     navigation directions when their conditions are not met.
type ConfigScreen struct {
	step           configStep
	back           bool
	sel            ConfigSelection
	cursor         int
	width          int
	height         int
	styles         Styles
	timeoutInput   *widgets.TextInput
	declaredAgents []domain.DeclaredInfraAgent // populated by SetDeclaredAgents

	// infraClassQueue holds the gated classes needing user selection, in the
	// order they were encountered in declaredAgents. Populated when
	// configStepCheckpoints advances and multiple same-class gated agents exist.
	infraClassQueue []infraClassEntry
	// infraClassIdx is the index into infraClassQueue for the current prompt.
	infraClassIdx int
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
		step:   configStepMode,
		cursor: -1, // mode step starts with no option preselected
		sel: ConfigSelection{
			Settings: domain.RunSettings{
				CommitBranchVariant: domain.CommitBranchMOSAICOwned,
			},
		},
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
		case configStepMode:
			if s.cursor < 2 {
				s.cursor++
			}
		case configStepVersionDrift, configStepCheckpoints,
			configStepCommits, configStepCommitBranch,
			configStepPreConsult, configStepManualResolution:
			if s.cursor < 1 {
				s.cursor++
			}
		case configStepHarness:
			if max := len(harness.CLISelections()) - 1; s.cursor < max {
				s.cursor++
			}
		case configStepInfraClass:
			if s.infraClassIdx < len(s.infraClassQueue) {
				max := len(s.infraClassQueue[s.infraClassIdx].agents) - 1
				if s.cursor < max {
					s.cursor++
				}
			}
		}
	case "enter":
		return s.advance()
	case "esc":
		if s.step == configStepMode {
			s.back = true
		} else {
			prev, prevCursor := s.prevStepAndCursor()
			s.step = prev
			s.cursor = prevCursor
			if prev == configStepHarnessTimeout {
				return s.timeoutInput.Init()
			}
		}
	}
	return nil
}

// advance commits the current selection and moves to the next step.
// It returns a tea.Cmd when the harness timeout text input needs to start blinking.
func (s *ConfigScreen) advance() tea.Cmd {
	switch s.step {
	case configStepMode:
		// Require an explicit cursor movement before Enter is accepted.
		if s.cursor < 0 {
			return nil
		}
		modes := domain.ExecutionModes()
		if s.cursor < len(modes) {
			s.sel.Settings.Mode = modes[s.cursor]
		}
		s.step = configStepHarness
		s.cursor = 0
	case configStepHarness:
		sels := harness.CLISelections()
		if s.cursor >= 0 && s.cursor < len(sels) {
			s.sel.Harness = sels[s.cursor].ID
		}
		s.step = configStepHarnessTimeout
		s.timeoutInput.Reset()
		s.cursor = 0
		return s.timeoutInput.Init() // start cursor blink in text input
	case configStepVersionDrift:
		s.sel.AllowVersionDrift = s.cursor == 0
		s.step = configStepCheckpoints
		s.cursor = 0
	case configStepCheckpoints:
		s.sel.Settings.Checkpoints = s.cursor == 1
		s.infraClassQueue = s.buildInfraClassQueue()
		s.infraClassIdx = 0
		s.cursor = 0
		s.step = s.nextStepAfterCheckpoints()
	case configStepCommits:
		s.sel.Settings.Commits = s.cursor == 1
		s.cursor = 0
		if s.sel.Settings.Commits {
			s.step = configStepCommitBranch
		} else {
			s.step = s.nextAfterCommitSection()
		}
	case configStepCommitBranch:
		variants := domain.CommitBranchVariants()
		if s.cursor < len(variants) {
			s.sel.Settings.CommitBranchVariant = variants[s.cursor]
		}
		s.cursor = 0
		s.step = s.nextAfterCommitSection()
	case configStepPreConsult:
		s.sel.Settings.PreConsultation = s.cursor == 1
		s.step = configStepManualResolution
		s.cursor = 0
	case configStepManualResolution:
		s.sel.Settings.ManualResolution = s.cursor == 1
		s.cursor = 0
		if len(s.infraClassQueue) > 0 {
			s.step = configStepInfraClass
		} else {
			s.step = configStepDone
		}
	case configStepInfraClass:
		entry := s.infraClassQueue[s.infraClassIdx]
		selected := entry.agents[s.cursor]
		if s.sel.InfraClassSelections == nil {
			s.sel.InfraClassSelections = make(map[string]string)
		}
		s.sel.InfraClassSelections[entry.class] = selected
		s.infraClassIdx++
		s.cursor = 0
		if s.infraClassIdx >= len(s.infraClassQueue) {
			s.step = configStepDone
		}
	}
	return nil
}

// hasCommitAgent reports whether any declared infrastructure agent has Class == "commit".
func (s *ConfigScreen) hasCommitAgent() bool {
	for _, a := range s.declaredAgents {
		if a.Class == "commit" {
			return true
		}
	}
	return false
}

// nextStepAfterCheckpoints returns the next applicable step after checkpoints.
func (s *ConfigScreen) nextStepAfterCheckpoints() configStep {
	if s.hasCommitAgent() {
		return configStepCommits
	}
	return s.nextAfterCommitSection()
}

// nextAfterCommitSection returns the first applicable step after the commits/commit-branch
// section. For non-orchestrated modes it is pre-consultation; otherwise manual resolution.
func (s *ConfigScreen) nextAfterCommitSection() configStep {
	mode := s.sel.Settings.Mode
	if mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview {
		return configStepPreConsult
	}
	return configStepManualResolution
}

// modeIndex returns the cursor index corresponding to the currently selected mode,
// or -1 if no mode has been selected yet.
func (s *ConfigScreen) modeIndex() int {
	modes := domain.ExecutionModes()
	for i, m := range modes {
		if m == s.sel.Settings.Mode {
			return i
		}
	}
	return -1
}

// prevStepAndCursor returns the previous applicable step and its cursor position,
// skipping conditional steps that would not be shown in the forward direction.
func (s *ConfigScreen) prevStepAndCursor() (configStep, int) {
	switch s.step {
	case configStepHarness:
		return configStepMode, s.modeIndex()
	case configStepVersionDrift:
		// configStepHarnessTimeout is handled by the text input widget's Back(),
		// but if we reach here via Esc on version-drift, go to timeout.
		return configStepHarnessTimeout, 0
	case configStepCheckpoints:
		return configStepVersionDrift, 0
	case configStepCommits:
		return configStepCheckpoints, 0
	case configStepCommitBranch:
		return configStepCommits, 0
	case configStepPreConsult:
		// If commits were enabled, go back to commit-branch.
		if s.sel.Settings.Commits {
			return configStepCommitBranch, 0
		}
		// If a commit agent is declared (but commits were disabled), go back to commits.
		if s.hasCommitAgent() {
			return configStepCommits, 0
		}
		return configStepCheckpoints, 0
	case configStepManualResolution:
		mode := s.sel.Settings.Mode
		if mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview {
			return configStepPreConsult, 0
		}
		if s.sel.Settings.Commits {
			return configStepCommitBranch, 0
		}
		if s.hasCommitAgent() {
			return configStepCommits, 0
		}
		return configStepCheckpoints, 0
	case configStepInfraClass:
		if s.infraClassIdx > 0 {
			s.infraClassIdx--
			return configStepInfraClass, 0
		}
		return configStepManualResolution, 0
	}
	return configStepMode, -1
}

// buildInfraClassQueue returns one infraClassEntry per gated class that has
// more than one declared agent, in the order classes first appear in
// declaredAgents. Classes with a single agent are omitted (auto-selected).
func (s *ConfigScreen) buildInfraClassQueue() []infraClassEntry {
	// Preserve declaration order using a slice of class names and a map.
	seen := make(map[string]bool)
	classOrder := []string{}
	classAgents := make(map[string][]string)
	for _, a := range s.declaredAgents {
		if !isGatedInfraClass(a.Class) {
			continue
		}
		if !seen[a.Class] {
			seen[a.Class] = true
			classOrder = append(classOrder, a.Class)
		}
		classAgents[a.Class] = append(classAgents[a.Class], a.Name)
	}
	var queue []infraClassEntry
	for _, class := range classOrder {
		agents := classAgents[class]
		if len(agents) > 1 {
			queue = append(queue, infraClassEntry{class: class, agents: agents})
		}
	}
	return queue
}

// isGatedInfraClass reports whether the given class name is a gated
// infrastructure class (checkpoint, commit, or restore). Non-gated classes
// (e.g. "review") have all declared agents evaluated unconditionally.
// Note: gatedInfraClasses in Tools/Runner/internal/session/selection.go
// maintains an equivalent set for the session layer. Keep both in sync when
// adding new gated classes.
func isGatedInfraClass(class string) bool {
	return class == "checkpoint" || class == "commit" || class == "restore"
}

// View renders the current configuration prompt.
func (s *ConfigScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Configuration")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Configure run options.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")

	var body strings.Builder
	switch s.step {
	case configStepMode:
		body.WriteString(s.styles.Body.Width(s.width).Render("Execution mode:") + "\n")
		for i, mode := range domain.ExecutionModes() {
			body.WriteString(s.renderOptionCursor(i, string(mode)))
		}
	case configStepHarness:
		body.WriteString(s.styles.Body.Width(s.width).Render("Harness adapter:") + "\n")
		for i, sel := range harness.CLISelections() {
			body.WriteString(s.renderOption(i, sel.Label))
		}
	case configStepHarnessTimeout:
		body.WriteString(s.timeoutInput.View())
	case configStepVersionDrift:
		body.WriteString(s.styles.Body.Width(s.width).Render("Allow workflow version drift:") + "\n")
		body.WriteString(s.renderOption(0, "Yes"))
		body.WriteString(s.renderOption(1, "No (default)"))
	case configStepCheckpoints:
		body.WriteString(s.styles.Body.Width(s.width).Render("Checkpoints:") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepCommits:
		body.WriteString(s.styles.Body.Width(s.width).Render("Commits:") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepCommitBranch:
		body.WriteString(s.styles.Body.Width(s.width).Render("Commit branch variant:") + "\n")
		body.WriteString(s.renderOption(0,
			"Create branch mosaic/run/{run_id} for this run; an abandoned attempt is discarded by deleting the branch. (Recommended)",
		))
		body.WriteString(s.renderOption(1,
			"Commit to the branch you are already on; a failed attempt and its undo both stay in history permanently.",
		))
	case configStepPreConsult:
		body.WriteString(s.styles.Body.Width(s.width).Render("Pre-consultation (one-shot run-start consultation):") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepManualResolution:
		body.WriteString(s.styles.Body.Width(s.width).Render("Manual resolution (user resolves routing decisions):") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepInfraClass:
		if s.infraClassIdx < len(s.infraClassQueue) {
			entry := s.infraClassQueue[s.infraClassIdx]
			prompt := fmt.Sprintf("Select %s agent for this run:", entry.class)
			body.WriteString(s.styles.Body.Width(s.width).Render(prompt) + "\n")
			for i, agentName := range entry.agents {
				body.WriteString(s.renderOption(i, agentName))
			}
		}
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

// renderOptionCursor renders one selectable option, treating cursor == -1 as "no selection"
// (no option is highlighted). Used for the mode step which starts with no preselection.
func (s *ConfigScreen) renderOptionCursor(idx int, label string) string {
	if s.cursor >= 0 && idx == s.cursor {
		return "▶ " + s.styles.Selected.Render(label) + "\n"
	}
	return "  " + s.styles.Body.Render(label) + "\n"
}

// Done reports whether all configuration prompts have been answered.
func (s *ConfigScreen) Done() bool { return s.step == configStepDone }

// Back reports whether the user pressed Esc on the first prompt.
func (s *ConfigScreen) Back() bool { return s.back }

// Selection returns the collected configuration. Only valid when Done() is true.
func (s *ConfigScreen) Selection() ConfigSelection { return s.sel }

// Reset clears the done, back flags and returns to the first step.
func (s *ConfigScreen) Reset() {
	s.step = configStepMode
	s.back = false
	s.cursor = -1 // mode step starts with no option preselected
	s.sel = ConfigSelection{
		Settings: domain.RunSettings{
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		},
	}
	s.timeoutInput.Reset()
	s.infraClassQueue = nil
	s.infraClassIdx = 0
}

// Resize updates the screen dimensions.
func (s *ConfigScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.timeoutInput.Resize(width)
}

// SetDeclaredAgents injects the declared infrastructure agents into the
// ConfigScreen so that the configStepInfraClass step can determine which
// gated classes have multiple agents and need a selection prompt.
//
// Must be called before the screen is shown. When multiple agents of the
// same gated class are declared, the configStepInfraClass step prompts the
// user to select one. When only one agent per class is declared, the step is
// skipped and the single agent is auto-selected.
func (s *ConfigScreen) SetDeclaredAgents(agents []domain.DeclaredInfraAgent) {
	s.declaredAgents = agents
}
