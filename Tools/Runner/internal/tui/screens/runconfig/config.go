package runconfig

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/widgets"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// ConfigScreen
// ---------------------------------------------------------------------------

// ConfigSelection holds the user's choices from the configuration screen.
type ConfigSelection struct {
	// Settings holds the run configuration collected by the wizard. It is the
	// same type the CLI produces from its flags, which is what makes surface
	// parity a single struct comparison rather than a field-by-field audit.
	Settings domain.RunSettings

	// Supplied records which of the settings the user answered explicitly in
	// this wizard pass. On a resume only supplied values are compared against
	// the artifact.
	Supplied domain.SuppliedSettings

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

	// ExecutablePath is the executable-path override for the selected harness,
	// or "" when no override applies. It is not collected by the configuration
	// wizard; it is set by the executable-override recovery screen and persists
	// for the remaining lifetime of the TUI process, including runs started
	// after the one that failed.
	//
	// The composition root prefers a non-empty value here over its own
	// command-line pre-scan result, and passes "" onward when both are empty
	// so that buildAdapter's per-harness default applies.
	ExecutablePath string

	// GHCPCLIMode is the user's per-run GHCP CLI permission-mode selection.
	// Populated by the GHCP CLI mode-selection screen or by the
	// --ghcp-permission-mode CLI flag. Empty when the harness is not GHCP CLI.
	// Accepted values: "blanket", "allowlist".
	GHCPCLIMode string
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
	// Default: enabled.
	configStepPreConsult

	// configStepManualResolution is shown always. Default: disabled.
	configStepManualResolution

	configStepInfraClass // agent-per-class selection (only when multiple same-class gated agents)

	// configStepReviewLimit is the last prompt of a new run: the review loop
	// limit, suggested as 3, accepting a positive integer or "no limit".
	configStepReviewLimit

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
	step               configStep
	back               bool
	sel                ConfigSelection
	cursor             int
	width              int
	height             int
	styles             screens.Styles
	timeoutInput       *widgets.TextInput
	limitInput         *widgets.TextInput
	declaredAgents     []domain.DeclaredInfraAgent // populated by SetDeclaredAgents
	harnessPreselected bool                        // true when harness was selected before config wizard

	// infraClassQueue holds the gated classes needing user selection, in the
	// order they were encountered in declaredAgents. Populated when
	// configStepCheckpoints advances and multiple same-class gated agents exist.
	infraClassQueue []infraClassEntry
	// infraClassIdx is the index into infraClassQueue for the current prompt.
	infraClassIdx int

	// Run-mode awareness for the conditional version-drift prompt.
	isNewRun         bool   // true: new run; false: resumed run
	recordedVersion  string // workflow version from artifact frontmatter (empty if not available)
	currentVersion   string // workflow version from current orchestrator file (empty if not available)
	versionsInjected bool   // true once SetVersionDriftInfo has supplied both versions

	// needsAdoption is true for a resumed run whose artifact records no runner
	// settings (a native-created artifact): the mode, pre-consultation and
	// manual-resolution prompts are asked once so the Runner can record them.
	needsAdoption bool

	// commitSetupPending is true for a resumed run that enables commits but
	// records no commit branch, so the variant must be chosen explicitly.
	commitSetupPending bool
}

// suggestedReviewLoopLimit is the value the review loop limit prompt suggests.
const suggestedReviewLoopLimit = "3"

// NewConfigScreen creates the configuration screen.
func NewConfigScreen(width, height int, styles screens.Styles) *ConfigScreen {
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
	limitInput := widgets.NewTextInput(
		"Review loop limit (a positive integer or \"no limit\"; suggested "+suggestedReviewLoopLimit+"):",
		suggestedReviewLoopLimit,
		width,
		inputStyles,
	)
	limitInput.SetValidate(func(v string) error {
		if _, err := domain.ParseReviewLoopLimit(v); err != nil {
			return errors.New("enter a positive integer, or \"no limit\"")
		}
		return nil
	})
	limitInput.SetValue(suggestedReviewLoopLimit)
	return &ConfigScreen{
		limitInput:      limitInput,
		step:            configStepMode,
		cursor:          -1, // mode step starts with no option preselected
		sel:             ConfigSelection{},
		width:           width,
		height:          height,
		styles:          styles,
		timeoutInput:    timeoutInput,
		isNewRun:        true, // safe default
		recordedVersion: "",
		currentVersion:  "",
	}
}

// Update processes a key message for the current configuration prompt.
func (s *ConfigScreen) Update(msg tea.Msg) tea.Cmd {
	// The harness timeout step delegates all key handling to the text input widget.
	if s.step == configStepHarnessTimeout {
		cmd := s.timeoutInput.Update(msg)
		if s.timeoutInput.Back() {
			s.timeoutInput.Reset()
			if s.firstStep() == configStepHarnessTimeout {
				s.back = true
			} else if s.harnessPreselected {
				s.step = configStepMode
				s.cursor = s.modeIndex()
			} else {
				s.step = configStepHarness
				s.cursor = 0
			}
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
			if s.showsVersionDrift() {
				s.step = configStepVersionDrift
			} else {
				s.step = configStepCheckpoints
			}
			s.cursor = 0
			return nil
		}
		return cmd
	}

	// The review loop limit step delegates key handling to its text input.
	if s.step == configStepReviewLimit {
		cmd := s.limitInput.Update(msg)
		if s.limitInput.Back() {
			s.limitInput.Reset()
			if len(s.infraClassQueue) > 0 {
				s.infraClassIdx = len(s.infraClassQueue) - 1
				s.step = configStepInfraClass
			} else {
				s.step = configStepManualResolution
			}
			s.cursor = 0
			return nil
		}
		if s.limitInput.Done() {
			limit, _ := domain.ParseReviewLoopLimit(s.limitInput.Value())
			s.sel.Settings.ReviewLoopLimit = limit
			s.sel.Supplied.ReviewLoopLimit = true
			s.limitInput.Reset()
			s.step = configStepDone
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
		if s.step == s.firstStep() {
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
	before := s.step
	cmd := s.advanceStep()
	if s.step == configStepReviewLimit && before != configStepReviewLimit {
		s.limitInput.SetValue(suggestedReviewLoopLimit)
		s.limitInput.Reset()
	}
	return cmd
}

// advanceStep commits the current selection and moves to the next step.
func (s *ConfigScreen) advanceStep() tea.Cmd {
	switch s.step {
	case configStepMode:
		// Require an explicit cursor movement before Enter is accepted.
		if s.cursor < 0 {
			return nil
		}
		modes := domain.ExecutionModes()
		if s.cursor < len(modes) {
			s.sel.Settings.Mode = modes[s.cursor]
			s.sel.Supplied.Mode = true
		}
		if s.harnessPreselected {
			// Harness was selected on the dedicated harness screen; skip the
			// harness step and go directly to the timeout input.
			s.step = configStepHarnessTimeout
			s.timeoutInput.Reset()
			s.cursor = 0
			return s.timeoutInput.Init()
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
		if s.step == configStepPreConsult {
			s.cursor = 1
		}
	case configStepCommits:
		s.sel.Settings.Commits = s.cursor == 1
		s.cursor = 0
		if s.sel.Settings.Commits {
			s.step = configStepCommitBranch
		} else {
			// CommitBranchVariant is only meaningful when commits are enabled;
			// reset to zero value so it does not linger from a prior selection.
			s.sel.Settings.CommitBranchVariant = domain.CommitBranchVariant("")
			s.step = s.nextAfterCommitSection()
			if s.step == configStepPreConsult {
				s.cursor = 1
			}
		}
	case configStepCommitBranch:
		// A commit-setup retry has no preselected variant: require an
		// explicit cursor movement before Enter is accepted.
		if s.cursor < 0 {
			return nil
		}
		variants := domain.CommitBranchVariants()
		if s.cursor < len(variants) {
			s.sel.Settings.CommitBranchVariant = variants[s.cursor]
			s.sel.Supplied.CommitBranchVariant = true
		}
		s.cursor = 0
		s.step = s.nextAfterCommitSection()
		if s.step == configStepPreConsult {
			s.cursor = 1
		}
	case configStepPreConsult:
		s.sel.Settings.PreConsultation = s.cursor == 1
		s.sel.Supplied.PreConsultation = true
		s.step = configStepManualResolution
		s.cursor = 0
	case configStepManualResolution:
		s.sel.Settings.ManualResolution = s.cursor == 1
		s.sel.Supplied.ManualResolution = true
		s.cursor = 0
		s.step = s.nextAfterRunnerSettings()
	case configStepInfraClass:
		entry := s.infraClassQueue[s.infraClassIdx]
		selected := entry.agents[s.cursor]
		if s.sel.InfraClassSelections == nil {
			s.sel.InfraClassSelections = make(map[string]string)
		}
		s.sel.InfraClassSelections[entry.class] = selected
		s.sel.Supplied.InfraClassSelections = true
		s.infraClassIdx++
		s.cursor = 0
		if s.infraClassIdx >= len(s.infraClassQueue) {
			s.step = s.nextAfterInfraClass()
		}
	}
	return nil
}

// hasCommitAgent reports whether any declared infrastructure agent has Class == "commit".
func (s *ConfigScreen) Done() bool { return s.step == configStepDone }

// Back reports whether the user pressed Esc on the first prompt.
func (s *ConfigScreen) Back() bool { return s.back }

// Selection returns the collected configuration. Only valid when Done() is true.
func (s *ConfigScreen) Selection() ConfigSelection { return s.sel }

// Reset clears the done, back flags and returns to the first step.
func (s *ConfigScreen) Reset() {
	s.sel = ConfigSelection{}
	s.back = false
	s.moveToFirstStep()
	s.timeoutInput.Reset()
	s.limitInput.Reset()
	s.limitInput.SetValue(suggestedReviewLoopLimit)
	s.infraClassQueue = nil
	s.infraClassIdx = 0
}

// Resize updates the screen dimensions.
func (s *ConfigScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	s.timeoutInput.Resize(width)
	s.limitInput.Resize(width)
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

// SetPreselectedHarness records that the harness was already chosen on the
// harness-selection screen. The config wizard skips the configStepHarness
// prompt and the back path from configStepHarnessTimeout goes directly to
// configStepMode rather than configStepHarness.
func (s *ConfigScreen) SetPreselectedHarness(id string) {
	s.sel.Harness = id
	s.harnessPreselected = true
	s.moveToFirstStep()
}

// SetIsNewRun injects the run-mode flag before setup wizard begins.
// Must be called by rootModel before any screen transitions occur.
// isNew=true indicates a new run; false indicates a resumed run.
func (s *ConfigScreen) SetIsNewRun(isNew bool) {
	s.isNewRun = isNew
	s.moveToFirstStep()
}

// SetNeedsRunnerAdoption tells the wizard that the resumed run records no
// runner settings (a native-created artifact). Must be called with
// SetIsNewRun before the screen shows.
func (s *ConfigScreen) SetNeedsRunnerAdoption(needs bool) {
	s.needsAdoption = needs
	s.moveToFirstStep()
}

// SetCommitSetupPending tells the wizard that the resumed run will retry
// commit setup, so the commit-branch variant must be chosen explicitly. Must
// be called with SetIsNewRun before the screen shows.
func (s *ConfigScreen) SetCommitSetupPending(pending bool) {
	s.commitSetupPending = pending
	s.moveToFirstStep()
}

// SetVersionDriftInfo injects version information before setup wizard begins.
// Must be called by rootModel after loading artifact state and after workflow is resolved,
// but before any screen transitions occur.
// recordedVersion: workflow version from artifact frontmatter (empty if not available)
// currentVersion: workflow version from current orchestrator file (empty if not available)
func (s *ConfigScreen) SetVersionDriftInfo(recordedVersion, currentVersion string) {
	s.recordedVersion = recordedVersion
	s.currentVersion = currentVersion
	s.versionsInjected = true
}

// showsVersionDrift reports whether the version-drift prompt is meaningful for
// this run, and so whether the step machine visits it in either direction.
//
// A new run has no recorded version to drift from, so the question is never
// asked. A resumed run is asked only when the recorded version differs from the
// version the current orchestrator file declares; a missing value on one side
// only counts as a difference, so an unknown version errs towards asking.
//
// A caller that never supplied the versions gets the prompt: without them the
// screen cannot tell a genuine match from two absent values, and asking a
// redundant question is safer than silently defaulting the answer to "no".
func (s *ConfigScreen) showsVersionDrift() bool {
	if !s.versionsInjected {
		return true
	}
	return !s.isNewRun && s.recordedVersion != s.currentVersion
}
