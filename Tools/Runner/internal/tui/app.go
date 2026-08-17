// Package tui implements the TUI frontend for mosaic-run. It uses Bubble Tea
// and the shared theme/keys/scaffold from mosaic-common/tui.
//
// Entry point: Run(). The caller supplies the workflow regions (enumerated from the
// orchestrator file), a started session, and Options. Run() collects the run setup
// inputs interactively, starts the session in a background goroutine, shows live
// progress, and optionally lets the user inspect the artifact. When the run ends,
// it shows the outcome and waits for the user to quit.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens"
)

// ArtifactNotYetCreatedMessage is shown in the artifact view when the run's
// Orchestration.md does not exist yet — either because no run folder has been
// resolved or because the run was refused before the store was created.
//
// The runner never guesses an artifact path. Tests assert on the stable
// substring "not yet created", not on the full string.
const ArtifactNotYetCreatedMessage = "Orchestration.md not yet created for this run."

// screenID identifies the currently active screen.
type screenID int

const (
	screenRunSelect      screenID = iota // run selection (shown when multiple resumable runs exist)
	screenSetupFile                      // orchestrator file path entry
	screenSetupWorkflow                  // workflow selection
	screenSetupTask                      // task description entry
	screenSetupSeedInput                 // seed-input path entry (new runs only)
	screenSetupConfig                    // run configuration prompts
	screenProgress                       // live execution progress
	screenArtifact                       // read-only artifact inspection
	screenQuestion                       // generic overlay from Interaction port
	screenStop                           // stop recovery (retry / manual dispatch) — shown on RunStoppedByConsultant
	screenExecOverride                   // executable-override recovery — shown on harness launch failure
	screenDone                           // completion/error summary
)

// Options configures the TUI run. All fields are optional.
type Options struct {
	// Interaction is the ProgramRef wired as the session Interact dependency.
	// Run() stores the tea.Program into it once the program is created so that
	// questions from the session goroutine reach the TUI overlay system.
	Interaction *ProgramRef

	// MouseEnabled enables mouse support.
	MouseEnabled bool

	// Theme sets the colour scheme. DefaultTheme() is used when zero.
	Theme tuicommon.Theme

	// Selection carries the unanswered selection question built by
	// runselect.Resolve when run identity was not settled before launch.
	// When non-nil, it is used directly to build the run-select screen and
	// takes precedence over ScanResult -- the production entry point
	// (cmd/mosaic-run) always supplies it, so the screen is built from the
	// same runselect decision the CLI refuses on, not a second copy of the
	// question-building rules.
	Selection *runselect.Question

	// ScanResult carries the run folder scan results. When Candidates has
	// more than one entry, the RunSelectScreen is shown before setup.
	// When nil or empty, the screen is skipped and a new run is assumed.
	//
	// Retained alongside Selection for callers (and this package's own
	// tests) that construct Options directly from a scan without going
	// through runselect.Resolve first. When Selection is nil, ScanResult is
	// adapted locally via candidatesToQuestion.
	ScanResult *runscan.ScanResult

	// ResolvedRunID is set when --run or --new-run resolved identity before
	// the TUI launched. When non-empty, the RunSelectScreen is skipped.
	ResolvedRunID string

	// IsNewRun is true when --new-run was given or the scan yielded zero candidates.
	IsNewRun bool

	// InitialRunFolder is the resolved run-scoped folder path when --run or
	// single-candidate auto-resume resolved run identity before TUI launch.
	// It is carried into m.selections.runFolder so that readArtifactContent
	// and the COMPLETED-marker write in a later stage use the correct path.
	InitialRunFolder string

	// SessionFactory, when non-nil, is called after run identity is resolved to
	// construct the session with the correct run-scoped artifact store and harness
	// adapter. orchFile is the path entered by the user on the orchestrator file
	// screen (empty when called before setup completes). cfg carries the harness
	// adapter selection and timeout from the config screen (zero value = fake adapter).
	// When nil, the session passed to Run() is used directly (test/backward-compat path).
	SessionFactory func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session

	// MintRunIdentity, when non-nil, is called when the user chooses "new run"
	// on the run-select screen, to resolve run identity before the session is
	// reconstructed. When nil, the previous behaviour is preserved: identity
	// is left empty and the session factory is called with an empty run folder.
	// Production callers always supply it; tests may omit it.
	MintRunIdentity RunIdentityMinter

	// ArtifactStoreFactory, when non-nil, builds the artifact store used for the
	// terminal COMPLETED phase-marker write, given the run's resolved run-scoped
	// folder. It is called at most once per terminal outcome, only when the
	// outcome status is domain.RunCompleted and the resolved folder is non-empty.
	//
	// The folder is not known at Options-construction time for a multi-candidate
	// run (it is settled on the run-select screen), which is why this is a
	// factory over a folder rather than a store.
	//
	// When nil, the TUI resolves the store itself as
	// artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md")),
	// mirroring the CLI's nil-store fallback in internal/cli/run.go.
	ArtifactStoreFactory func(runFolder string) domain.ArtifactStore

	// Clock supplies the timestamp handed to ArtifactStore.SetPhase for the
	// completion-marker write. When nil, a real UTC clock is used.
	Clock domain.Clock
}

// runSetupSelections holds all inputs collected during the setup phase.
type runSetupSelections struct {
	orchestratorFile string
	workflowID       domain.WorkflowID
	task             string

	// seedInput is the optional seed-input path collected on the seed screen.
	// Empty means no seeding. Only ever populated for a new run; a resumed run
	// skips the seed screen and leaves this empty.
	seedInput string

	config screens.ConfigSelection

	// Run identity resolved by RunSelectScreen or pre-launch flags.
	runID     string // resolved run_id; empty if not yet resolved
	runFolder string // resolved run-scoped folder path (absolute)
	isNewRun  bool   // true = create new artifact; false = resume existing

	// manualDispatch is set to true when the user chooses Manual Dispatch on
	// the stop recovery screen. It is carried into the next RunConfig so the
	// session layer can use ManualResolver for the first routing decision.
	// Cleared to false for a Retry choice.
	manualDispatch bool
}

// runDoneMsg is sent by the session goroutine when the run completes.
type runDoneMsg struct {
	outcome domain.RunOutcome
}

// runErrorMsg is sent by the session goroutine when Start returns a non-nil error.
type runErrorMsg struct{ err error }

// stepCompleteMsg is sent by the session's Notify call to inform the progress screen.
type stepCompleteMsg struct {
	agentInstance string
	phase         string
	stage         string
	status        string
}

// gracefulStopRequestMsg is sent by the progress screen when the user presses 's'.
type gracefulStopRequestMsg struct{}

// artifactContentMsg carries the current artifact file content to the artifact screen.
type artifactContentMsg struct{ content string }

// rootModel is the top-level Bubble Tea model. It owns the navigation state machine.
type rootModel struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	theme     tuicommon.Theme
	screen    screenID
	width     int
	height    int

	// Session dependencies.
	sess            session.Session
	sessionFactory  func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session
	mintRunIdentity RunIdentityMinter
	interact        *ProgramRef

	// Completion-marker write seam. When artifactStoreFactory is nil the TUI
	// constructs the store itself from the resolved run folder. When clock is
	// nil a real UTC clock is used.
	artifactStoreFactory func(runFolder string) domain.ArtifactStore
	clock                domain.Clock

	// Enumerated workflow regions (populated after orchestrator file is loaded).
	workflows []domain.WorkflowRegion

	// Run selection screen (shown when multiple resumable candidates exist).
	runSelectScreen *screens.RunSelectScreen

	// runSelectQuestion is the runselect.Question the run-select screen was
	// built from. A chosen Choice.ID is resolved back to a full Identity via
	// runselect.Answer, the same function the CLI's non-interactive path
	// would use to interpret an explicit --run value -- the screen never
	// holds a second copy of the selection rules.
	runSelectQuestion *runselect.Question

	// Entry screens (concrete types so back-navigation preserves state).
	fileScreen      *screens.OrchestratorFileScreen
	workflowScreen  *screens.WorkflowSelectScreen
	taskScreen      *screens.TaskScreen
	seedInputScreen *screens.SeedInputScreen
	configScreen    *screens.ConfigScreen

	// Collected setup selections.
	selections runSetupSelections

	// Progress screen (constructed when execution starts).
	progressScreen *screens.ProgressScreen

	// Artifact inspection screen.
	artifactScreen *screens.ArtifactScreen
	prevScreen     screenID // screen to return to after artifact inspection

	// Generic question overlays from the Interaction port.
	activeQuestion *questionMsg
	selectOverlay  *inlineSelectOne
	textOverlay    *inlineText
	confirmOverlay *inlineConfirm

	// Stop recovery screen (shown when RunStoppedByConsultant).
	stopScreen *screens.StopScreen

	// Executable-override recovery screen (shown on harness launch failure).
	execOverrideScreen *screens.ExecOverrideScreen

	// launchFailureAttempt counts consecutive launch failures in this process.
	// Incremented each time a launch failure is detected; reset to zero when
	// any invocation completes without a launch failure. Passed to
	// NewExecOverrideScreen as the attempt number so repeated failures can be
	// displayed as such rather than looking like the first.
	launchFailureAttempt int

	// lastLaunchFailure holds the terminal outcome or error from the most
	// recent launch failure so that ExecOverrideChoiceAbandon can build the
	// done screen from it rather than showing a blank outcome.
	lastLaunchFailureOutcome *domain.RunOutcome
	lastLaunchFailureErr     error

	// Done screen.
	doneScreen *screens.DoneScreen
}

// Run owns the terminal for the lifetime of the call. It presents the setup screens,
// starts the session in a background goroutine, and shows live progress.
func Run(ctx context.Context, sess session.Session, opts Options) error {
	if opts.Theme.Styles == nil {
		opts.Theme = tuicommon.DefaultTheme()
	}

	m := newRootModel(ctx, sess, opts)

	var programOpts []tea.ProgramOption
	programOpts = append(programOpts, tea.WithAltScreen())
	if opts.MouseEnabled {
		programOpts = append(programOpts, tea.WithMouseCellMotion())
	}

	p := tea.NewProgram(m, programOpts...)

	if opts.Interaction != nil {
		opts.Interaction.set(p)
	}

	_, err := p.Run()
	return err
}

func newRootModel(ctx context.Context, sess session.Session, opts Options) *rootModel {
	w := tuicommon.DefaultWidth
	h := tuicommon.DefaultHeight
	style := stylesFromTheme(opts.Theme)
	ctx, cancel := context.WithCancel(ctx)

	fileScreen := screens.NewOrchestratorFileScreen(w, h, style)
	taskScreen := screens.NewTaskScreen(w, h, style)
	seedInputScreen := screens.NewSeedInputScreen(w, h, style)
	configScreen := screens.NewConfigScreen(w, h, style)

	interact := opts.Interaction
	if interact == nil {
		interact = NewProgramRef()
	}

	// Determine the initial screen and resolve run identity from pre-launch options.
	initialScreen := screenSetupFile
	var runSelectScreen *screens.RunSelectScreen
	preRunID := opts.ResolvedRunID
	preIsNewRun := opts.IsNewRun

	var runSelectQuestion *runselect.Question
	haveCandidates := opts.Selection != nil || (opts.ScanResult != nil && len(opts.ScanResult.Candidates) >= 1)
	if opts.ResolvedRunID == "" && !opts.IsNewRun && haveCandidates {
		// Any resumable candidate with no pre-resolved run: show the selection
		// screen. The number of candidates never decides whether the screen is
		// shown -- only an explicit pre-resolved identity does.
		//
		// opts.Selection, when supplied, is the runselect.Question the
		// production entry point built via runselect.Resolve -- the single
		// decision shared with the CLI. opts.ScanResult is adapted locally
		// only for callers that construct Options directly from a scan.
		var q runselect.Question
		if opts.Selection != nil {
			q = *opts.Selection
		} else {
			q = candidatesToQuestion(*opts.ScanResult)
		}
		runSelectQuestion = &q
		runSelectScreen = screens.NewRunSelectScreen(q, w, h, style)
		initialScreen = screenRunSelect
	}
	// Zero candidates (or pre-resolved): skip run select, go straight to setup.

	m := &rootModel{
		ctx:                  ctx,
		ctxCancel:            cancel,
		theme:                opts.Theme,
		screen:               initialScreen,
		width:                w,
		height:               h,
		sess:                 sess,
		sessionFactory:       opts.SessionFactory,
		mintRunIdentity:      opts.MintRunIdentity,
		interact:             interact,
		runSelectScreen:      runSelectScreen,
		runSelectQuestion:    runSelectQuestion,
		fileScreen:           fileScreen,
		taskScreen:           taskScreen,
		seedInputScreen:      seedInputScreen,
		configScreen:         configScreen,
		artifactStoreFactory: opts.ArtifactStoreFactory,
		clock:                opts.Clock,
	}

	// Always propagate InitialRunFolder so readArtifactContent and the COMPLETED-marker
	// write target the correct path regardless of whether other identity fields are set.
	m.selections.runFolder = opts.InitialRunFolder
	// Pre-populate run identity when already resolved (--run / --new-run / single candidate).
	if preRunID != "" || preIsNewRun {
		m.selections.runID = preRunID
		m.selections.isNewRun = preIsNewRun
	}

	return m
}

// candidatesToQuestion adapts a scan result into the runselect.Question shape
// screens.RunSelectScreen takes: the always-present new-run choice, then
// every resumable candidate as a selectable ChoiceResume, then every
// unresumable run as a non-selectable ChoiceUnresumable carrying its reason.
// It is a data-shape adapter only, not a selection decision.
func candidatesToQuestion(scan runscan.ScanResult) runselect.Question {
	choices := make([]runselect.Choice, 0, len(scan.Candidates)+len(scan.Unresumable)+1)
	choices = append(choices, runselect.Choice{
		ID:         runselect.NewRunChoiceID,
		Kind:       runselect.ChoiceNewRun,
		Selectable: true,
	})
	for _, c := range scan.Candidates {
		choices = append(choices, runselect.Choice{
			ID:         c.RunID,
			Kind:       runselect.ChoiceResume,
			Run:        c.RunInfo,
			Selectable: true,
		})
	}
	for _, u := range scan.Unresumable {
		choices = append(choices, runselect.Choice{
			ID:         u.RunID,
			Kind:       runselect.ChoiceUnresumable,
			Run:        u.RunInfo,
			Selectable: false,
			Reason:     u.Reason,
		})
	}
	return runselect.Question{Choices: choices}
}

// stylesFromTheme converts a tuicommon.Theme to screens.Styles.
func stylesFromTheme(t tuicommon.Theme) screens.Styles {
	return screens.Styles{
		Title:    t.Style(tuicommon.RoleTitle),
		Subtitle: t.Style(tuicommon.RoleSubtitle),
		Body:     t.Style(tuicommon.RoleBody),
		Muted:    t.Style(tuicommon.RoleMuted),
		Selected: t.Style(tuicommon.RoleSelected),
		Checked:  t.Style(tuicommon.RoleChecked),
		Success:  t.Style(tuicommon.RoleSuccess),
		Warning:  t.Style(tuicommon.RoleWarning),
		Error:    t.Style(tuicommon.RoleError),
		Help:     t.Style(tuicommon.RoleHelp),
		Border:   t.Style(tuicommon.RoleBorder),
	}
}

// Init is called once when the Bubble Tea program starts.
func (m *rootModel) Init() tea.Cmd {
	if m.screen == screenRunSelect {
		return nil
	}
	return m.fileScreen.InputInit()
}

// Update processes an incoming message and drives the screen state machine.
func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global ctrl+c: cancel context and quit.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && matchesKey(keyMsg, globalKeys.Cancel) {
		m.ctxCancel()
		m.replyToPendingQuestion(answerMsg{
			confirmAns: interaction.ConfirmAnswer{Status: interaction.Cancelled},
		})
		return m, tea.Quit
	}

	// Window resize: propagate to all screens.
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
		m.resizeScreens()
		return m, nil
	}

	// Question from the Interaction port (service goroutine).
	if qMsg, ok := msg.(questionMsg); ok {
		return m.handleQuestionMsg(qMsg)
	}

	// Session completed.
	if doneMsg, ok := msg.(runDoneMsg); ok {
		style := stylesFromTheme(m.theme)
		// Check for a launch-failure cause before any status-based branching.
		// A RunRefused or RunStoppedByConsultant outcome caused by a harness
		// launch failure routes to the override screen instead of the normal
		// done or stop screens, because the user may be able to fix it by
		// supplying a working executable path.
		var launchErr *domain.HarnessLaunchError
		if errors.As(doneMsg.outcome.Cause, &launchErr) {
			m.launchFailureAttempt++
			m.lastLaunchFailureOutcome = &doneMsg.outcome
			m.lastLaunchFailureErr = nil
			m.execOverrideScreen = screens.NewExecOverrideScreen(
				launchErr.Harness, launchErr.Executable,
				m.launchFailureAttempt, m.width, m.height, style,
			)
			m.screen = screenExecOverride
			return m, m.execOverrideScreen.InputInit()
		}
		// Non-launch-failure terminal outcome: reset the consecutive-failure counter.
		m.launchFailureAttempt = 0
		if doneMsg.outcome.Status == domain.RunStoppedByConsultant {
			m.stopScreen = screens.NewStopScreen(doneMsg.outcome.StopReason, m.width, m.height, style)
			m.screen = screenStop
			return m, nil
		}
		// Write the COMPLETED phase marker when the run finished successfully.
		// A non-empty run folder is required; a failed write is non-fatal — the
		// TUI proceeds to the done screen and surfaces the error as a warning.
		markerErrMsg := ""
		if doneMsg.outcome.Status == domain.RunCompleted && m.selections.runFolder != "" {
			store := m.resolveArtifactStore(m.selections.runFolder)
			now := m.resolveClockTime()
			if _, err := store.SetPhase(m.ctx, domain.ArtifactState{}, "COMPLETED", now); err != nil {
				markerErrMsg = err.Error()
			}
		}
		m.doneScreen = screens.NewDoneScreen(doneMsg.outcome, markerErrMsg, m.width, m.height, style)
		m.screen = screenDone
		return m, nil
	}
	if errMsg, ok := msg.(runErrorMsg); ok {
		style := stylesFromTheme(m.theme)
		// Route launch failures to the override screen; all other errors continue
		// to the existing done screen, because a path override cannot fix them.
		var launchErr *domain.HarnessLaunchError
		if errors.As(errMsg.err, &launchErr) {
			m.launchFailureAttempt++
			m.lastLaunchFailureErr = errMsg.err
			m.lastLaunchFailureOutcome = nil
			m.execOverrideScreen = screens.NewExecOverrideScreen(
				launchErr.Harness, launchErr.Executable,
				m.launchFailureAttempt, m.width, m.height, style,
			)
			m.screen = screenExecOverride
			return m, m.execOverrideScreen.InputInit()
		}
		// Non-launch-failure error: reset the consecutive-failure counter.
		m.launchFailureAttempt = 0
		m.doneScreen = screens.NewDoneScreen(domain.RunOutcome{}, errMsg.err.Error(), m.width, m.height, style)
		m.screen = screenDone
		return m, nil
	}

	// Artifact content update.
	if artMsg, ok := msg.(artifactContentMsg); ok {
		if m.artifactScreen != nil {
			m.artifactScreen.SetContent(artMsg.content)
		}
		return m, nil
	}

	// Delegate to current screen.
	switch m.screen {
	case screenRunSelect:
		return m.updateRunSelect(msg)
	case screenSetupFile:
		return m.updateSetupFile(msg)
	case screenSetupWorkflow:
		return m.updateSetupWorkflow(msg)
	case screenSetupTask:
		return m.updateSetupTask(msg)
	case screenSetupSeedInput:
		return m.updateSetupSeedInput(msg)
	case screenSetupConfig:
		return m.updateSetupConfig(msg)
	case screenProgress:
		return m.updateProgress(msg)
	case screenArtifact:
		return m.updateArtifact(msg)
	case screenQuestion:
		return m.updateQuestion(msg)
	case screenStop:
		return m.updateStop(msg)
	case screenExecOverride:
		return m.updateExecOverride(msg)
	case screenDone:
		return m.updateDone(msg)
	}
	return m, nil
}

func (m *rootModel) resizeScreens() {
	if m.runSelectScreen != nil {
		m.runSelectScreen.Resize(m.width, m.height)
	}
	if m.fileScreen != nil {
		m.fileScreen.Resize(m.width, m.height)
	}
	if m.workflowScreen != nil {
		m.workflowScreen.Resize(m.width, m.height)
	}
	if m.taskScreen != nil {
		m.taskScreen.Resize(m.width, m.height)
	}
	if m.seedInputScreen != nil {
		m.seedInputScreen.Resize(m.width, m.height)
	}
	if m.configScreen != nil {
		m.configScreen.Resize(m.width, m.height)
	}
	if m.progressScreen != nil {
		m.progressScreen.Resize(m.width, m.height)
	}
	if m.artifactScreen != nil {
		m.artifactScreen.Resize(m.width, m.height)
	}
	if m.stopScreen != nil {
		m.stopScreen.Resize(m.width, m.height)
	}
	if m.execOverrideScreen != nil {
		m.execOverrideScreen.Resize(m.width, m.height)
	}
	if m.doneScreen != nil {
		m.doneScreen.Resize(m.width, m.height)
	}
}

// ---------------------------------------------------------------------------
// Run select screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateRunSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.runSelectScreen == nil {
		m.screen = screenSetupFile
		return m, m.fileScreen.InputInit()
	}
	m.runSelectScreen.Update(msg)
	if m.runSelectScreen.Back() {
		m.runSelectScreen.Reset()
		return m, tea.Quit
	}
	if m.runSelectScreen.Done() {
		if m.runSelectScreen.IsNewRun() {
			m.selections.isNewRun = true
			if m.mintRunIdentity != nil {
				runID, runFolder := m.mintRunIdentity()
				m.selections.runID = runID
				m.selections.runFolder = runFolder
			} else {
				m.selections.runID = ""
				m.selections.runFolder = ""
			}
		} else if m.runSelectQuestion != nil {
			choiceID := m.runSelectScreen.SelectedChoiceID()
			// Resolve the chosen ID back to a full Identity via the same
			// runselect.Answer function the CLI would use for an explicit
			// --run value -- the screen never holds a second copy of the
			// selection rules. mint is only reachable for NewRunChoiceID,
			// already handled above, so a nil-safe fallback is passed here.
			mint := m.mintRunIdentity
			if mint == nil {
				mint = func() (string, string) { return "", "" }
			}
			id, err := runselect.Answer(*m.runSelectQuestion, choiceID, runselect.Minter(mint))
			if err == nil {
				m.selections.runID = id.RunID
				m.selections.runFolder = id.RunFolder
				m.selections.isNewRun = false
				// Reconstruct the session with the correct run-scoped store if a factory is available.
				// Harness config is not yet known (config screen has not run); defaults to fake adapter.
				if m.sessionFactory != nil {
					m.sess = m.sessionFactory(id.RunFolder, false, "", screens.ConfigSelection{})
				}
			}
		}
		// When "new run" is selected, the session factory is called with the minted
		// run folder (or empty string when no minter is provided, for backward compat).
		if m.selections.isNewRun && m.sessionFactory != nil {
			m.sess = m.sessionFactory(m.selections.runFolder, true, "", screens.ConfigSelection{})
		}
		m.runSelectScreen.Reset()
		m.screen = screenSetupFile
		return m, m.fileScreen.InputInit()
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Setup screen handlers
// ---------------------------------------------------------------------------

func (m *rootModel) updateSetupFile(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.fileScreen.Update(msg)
	if m.fileScreen.Back() {
		m.fileScreen.Reset()
		return m, tea.Quit
	}
	if m.fileScreen.Done() {
		path := m.fileScreen.FilePath()
		m.selections.orchestratorFile = path
		m.fileScreen.Reset()

		// Enumerate workflow regions from the file.
		regions, err := orchfile.EnumerateWorkflows(path)
		if err != nil {
			// Show error on the file screen by re-entering with an error overlay.
			// For simplicity, just clear and let the user retry with a new path.
			style := stylesFromTheme(m.theme)
			m.doneScreen = screens.NewDoneScreen(
				domain.RunOutcome{Status: domain.RunRefused, Message: err.Error()},
				"",
				m.width, m.height, style,
			)
			m.screen = screenDone
			return m, nil
		}
		m.workflows = regions

		// Enumerate infrastructure agents so the config screen can prompt the
		// user to select one when multiple agents of the same gated class are
		// declared. A parse error is treated as zero declared agents; the
		// session layer will enforce a refusal at run start if needed.
		infraAgents, err := orchfile.EnumerateInfrastructureAgents(path)
		if err != nil {
			infraAgents = nil
		}
		m.configScreen.SetDeclaredAgents(infraAgents)

		style := stylesFromTheme(m.theme)
		m.workflowScreen = screens.NewWorkflowSelectScreen(regions, m.width, m.height, style)
		m.screen = screenSetupWorkflow
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupWorkflow(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.workflowScreen.Update(msg)
	if m.workflowScreen.Back() {
		m.workflowScreen.Reset()
		m.screen = screenSetupFile
		return m, m.fileScreen.InputInit()
	}
	if m.workflowScreen.Done() {
		selectedID := m.workflowScreen.SelectedID()
		m.selections.workflowID = domain.WorkflowID(selectedID)

		m.workflowScreen.Reset()
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	return m, nil
}

func (m *rootModel) updateSetupTask(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.taskScreen.Update(msg)
	if m.taskScreen.Back() {
		m.taskScreen.Reset()
		m.screen = screenSetupWorkflow
		return m, nil
	}
	if m.taskScreen.Done() {
		m.selections.task = m.taskScreen.Task()
		m.taskScreen.Reset()
		if m.selections.isNewRun {
			m.screen = screenSetupSeedInput
			return m, m.seedInputScreen.InputInit()
		}
		m.screen = screenSetupConfig
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupSeedInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.seedInputScreen.Update(msg)
	if m.seedInputScreen.Back() {
		m.seedInputScreen.Reset()
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	if m.seedInputScreen.Done() {
		m.selections.seedInput = m.seedInputScreen.SeedInput()
		m.seedInputScreen.Reset()
		m.screen = screenSetupConfig
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.configScreen.Update(msg)
	if m.configScreen.Back() {
		m.configScreen.Reset()
		if m.selections.isNewRun {
			m.screen = screenSetupSeedInput
			return m, m.seedInputScreen.InputInit()
		}
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	if m.configScreen.Done() {
		m.selections.config = m.configScreen.Selection()
		m.configScreen.Reset()

		// Reconstruct the session with the harness adapter selected in the config screen.
		// This replaces the placeholder session (which used the fake adapter) with one
		// using the real adapter when "Claude Code CLI" was chosen.
		if m.sessionFactory != nil {
			m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
		}

		// Transition to progress screen and start the session. The chosen run
		// is stated as the initial status line before dispatch (AC2.7),
		// mirroring the CLI's stdout announcement -- the same runselect.Announce
		// renderer, so the wording is the single source for both frontends.
		style := stylesFromTheme(m.theme)
		m.progressScreen = screens.NewProgressScreen(m.width, m.height, style)
		m.progressScreen.SetStatus(runselect.Announce(m.announceIdentity()), false)
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
}

// announceIdentity builds the runselect.Identity used to render the
// chosen-run announcement (AC2.7) from the currently selected run. For a
// resumed run it attempts to read the recorded position from the run's
// artifact; a read or parse failure simply leaves Position nil, which
// runselect.Announce handles without panicking.
func (m *rootModel) announceIdentity() runselect.Identity {
	id := runselect.Identity{
		RunID:     m.selections.runID,
		RunFolder: m.selections.runFolder,
		IsNewRun:  m.selections.isNewRun,
	}
	if id.IsNewRun || id.RunFolder == "" {
		return id
	}
	data, err := os.ReadFile(filepath.Join(id.RunFolder, "Orchestration.md"))
	if err != nil {
		return id
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return id
	}
	id.Position = &runselect.Position{
		Phase:       state.CurrentState.Phase,
		Stage:       state.CurrentState.Stage,
		LastAgent:   state.CurrentState.LastAgent,
		LastUpdated: state.LastUpdated,
	}
	return id
}

// ---------------------------------------------------------------------------
// Progress screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.progressScreen.Update(msg)

	if m.progressScreen.GracefulStop() {
		// Cancel the context so the session's next harness.Invoke sees ctx.Err().
		m.ctxCancel()
	}

	if m.progressScreen.ArtifactViewRequested() {
		m.progressScreen.ClearArtifactViewRequest()
		m.prevScreen = screenProgress
		content := m.readArtifactContent()
		style := stylesFromTheme(m.theme)
		m.artifactScreen = screens.NewArtifactScreen(content, m.width, m.height, style)
		m.screen = screenArtifact
		return m, nil
	}

	return m, cmd
}

// extractStatus parses the status from a session notice message.
// Format: "phase=X stage=\"Y\" status=Z"
func extractStatus(msg string) string {
	return extractField(msg, "status")
}

// extractField parses a specific key=value pair from a session notice message.
// Values may be optionally double-quoted (e.g. stage="Stage-1"); quotes are stripped.
func extractField(msg, key string) string {
	prefix := key + "="
	for _, part := range strings.Fields(msg) {
		if strings.HasPrefix(part, prefix) {
			val := strings.TrimPrefix(part, prefix)
			return strings.Trim(val, "\"")
		}
	}
	return ""
}

// readArtifactContent reads the Orchestration.md file from the canonical
// run-scoped path. The orchestrator file's directory is never consulted.
//
// Decision table:
//   - runFolder empty              → ArtifactNotYetCreatedMessage
//   - runFolder set, file exists   → file content verbatim
//   - runFolder set, file missing  → ArtifactNotYetCreatedMessage
//   - runFolder set, other error   → "(could not read artifact: {err})"
func (m *rootModel) readArtifactContent() string {
	if m.selections.runFolder == "" {
		return ArtifactNotYetCreatedMessage
	}
	artPath := filepath.Join(m.selections.runFolder, "Orchestration.md")
	data, err := os.ReadFile(artPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ArtifactNotYetCreatedMessage
		}
		return fmt.Sprintf("(could not read artifact: %v)", err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// Artifact inspection screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateArtifact(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.artifactScreen == nil {
		return m, nil
	}
	m.artifactScreen.Update(msg)
	if m.artifactScreen.Done() {
		m.artifactScreen.Reset()
		m.screen = m.prevScreen
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Generic question overlay handler (Interaction port)
// ---------------------------------------------------------------------------

func (m *rootModel) handleQuestionMsg(qMsg questionMsg) (tea.Model, tea.Cmd) {
	switch qMsg.kind {
	case questionNotice:
		// Route notices to the progress screen if active.
		if m.progressScreen != nil {
			notice := qMsg.notice
			if notice.Level == interaction.NoticeInfo {
				status := extractStatus(notice.Message)
				if status == "running" {
					// Step is starting — append a new progress row.
					m.progressScreen.AppendRow(screens.ProgressRow{
						AgentInstance: notice.Title,
						Phase:         extractField(notice.Message, "phase"),
						Stage:         extractField(notice.Message, "stage"),
						Status:        "running",
					})
				} else if status != "" {
					// Step completed — mark the current row complete.
					m.progressScreen.CompleteRow(status)
				}
				m.progressScreen.SetStatus(notice.Title+": "+notice.Message, false)
			} else {
				m.progressScreen.SetStatus(notice.Message, notice.Level == interaction.NoticeError)
			}
		}
		return m, nil

	case questionProgress:
		if m.progressScreen != nil {
			e := qMsg.progress
			label := e.Phase
			if e.Total > 0 {
				label = fmt.Sprintf("%s %d/%d %s", e.Phase, e.Current, e.Total, e.Subject)
			} else if e.Subject != "" {
				label = fmt.Sprintf("%s %s", e.Phase, e.Subject)
			}
			m.progressScreen.SetStatus(label, false)
		}
		return m, nil

	case questionSelectOne:
		m.activeQuestion = &qMsg
		m.selectOverlay = newInlineSelectOne(qMsg.choiceQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, nil

	case questionAskText:
		m.activeQuestion = &qMsg
		m.textOverlay = newInlineText(qMsg.textQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, m.textOverlay.init()

	case questionConfirm:
		m.activeQuestion = &qMsg
		m.confirmOverlay = newInlineConfirm(qMsg.confirmQ, m.theme, m.width)
		m.screen = screenQuestion
		return m, nil
	}
	return m, nil
}

func (m *rootModel) updateQuestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.selectOverlay != nil {
		done := m.selectOverlay.update(msg)
		if done {
			ans := m.selectOverlay.answer()
			m.replyToPendingQuestion(answerMsg{choiceAns: ans})
			m.selectOverlay = nil
			m.screen = screenProgress
		}
		return m, nil
	}
	if m.textOverlay != nil {
		cmd := m.textOverlay.update(msg)
		if m.textOverlay.done {
			ans := m.textOverlay.answer()
			m.replyToPendingQuestion(answerMsg{textAns: ans})
			m.textOverlay = nil
			m.screen = screenProgress
		}
		return m, cmd
	}
	if m.confirmOverlay != nil {
		done := m.confirmOverlay.update(msg)
		if done {
			ans := m.confirmOverlay.answer()
			m.replyToPendingQuestion(answerMsg{confirmAns: ans})
			m.confirmOverlay = nil
			m.screen = screenProgress
		}
		return m, nil
	}
	return m, nil
}

// replyToPendingQuestion sends an answer to the active question and clears it.
func (m *rootModel) replyToPendingQuestion(ans answerMsg) {
	if m.activeQuestion != nil && m.activeQuestion.reply != nil {
		m.activeQuestion.reply <- ans
		m.activeQuestion = nil
	}
}

// ---------------------------------------------------------------------------
// Stop recovery screen handler
// ---------------------------------------------------------------------------

// updateStop handles input for the stop recovery screen. When the user chooses
// Retry, the session is re-started from the progress screen. When the user
// chooses Manual dispatch, the progress screen is shown so the session goroutine
// can present the ManualResolver question overlay.
func (m *rootModel) updateStop(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.stopScreen == nil {
		return m, nil
	}
	m.stopScreen.Update(msg)
	if m.stopScreen.Back() {
		// Treat Esc on the stop screen as a terminal quit.
		return m, tea.Quit
	}
	if m.stopScreen.Done() {
		// Record which recovery action the user chose before clearing the screen,
		// so startSession() can include ManualDispatch in the RunConfig.
		if m.stopScreen.Choice() == screens.StopChoiceManualDispatch {
			m.selections.manualDispatch = true
		} else {
			m.selections.manualDispatch = false
		}
		style := stylesFromTheme(m.theme)
		if m.progressScreen == nil {
			m.progressScreen = screens.NewProgressScreen(m.width, m.height, style)
		}
		m.stopScreen = nil
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Executable-override screen handler
// ---------------------------------------------------------------------------

// updateExecOverride handles input for the executable-override recovery screen.
// When the user confirms a non-empty path (Choice() == ExecOverrideChoiceRetry),
// the session is rebuilt with the override path and restarted against the same
// run folder. When the user abandons (Choice() == ExecOverrideChoiceAbandon via Esc),
// the TUI transitions to the done screen with the original failure.
//
// Routing to this handler is performed in the runDoneMsg and runErrorMsg handlers,
// which check errors.As(outcome.Cause, &launchErr) / errors.As(err, &launchErr) and
// transition to screenExecOverride on a *domain.HarnessLaunchError.
func (m *rootModel) updateExecOverride(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.execOverrideScreen == nil {
		return m, nil
	}
	m.execOverrideScreen.Update(msg)
	if m.execOverrideScreen.Back() {
		// Abandon: show the done screen with the original failure.
		style := stylesFromTheme(m.theme)
		if m.lastLaunchFailureOutcome != nil {
			m.doneScreen = screens.NewDoneScreen(*m.lastLaunchFailureOutcome, "", m.width, m.height, style)
		} else if m.lastLaunchFailureErr != nil {
			m.doneScreen = screens.NewDoneScreen(domain.RunOutcome{}, m.lastLaunchFailureErr.Error(), m.width, m.height, style)
		} else {
			m.doneScreen = screens.NewDoneScreen(domain.RunOutcome{}, "launch failed", m.width, m.height, style)
		}
		m.execOverrideScreen = nil
		m.screen = screenDone
		return m, nil
	}
	if m.execOverrideScreen.Done() {
		// Retry: hold the override path, rebuild the session, restart.
		m.selections.config.ExecutablePath = m.execOverrideScreen.Path()
		if m.sessionFactory != nil {
			m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
		}
		m.execOverrideScreen = nil
		style := stylesFromTheme(m.theme)
		if m.progressScreen == nil {
			m.progressScreen = screens.NewProgressScreen(m.width, m.height, style)
		}
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Done screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.doneScreen != nil {
		m.doneScreen.Update(msg)
		if m.doneScreen.Done() {
			return m, tea.Quit
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Completion-marker helpers
// ---------------------------------------------------------------------------

// resolveArtifactStore returns the artifact store to use for the COMPLETED
// marker write. When an ArtifactStoreFactory was injected it is called with
// the run folder; otherwise the default file-based store is used.
func (m *rootModel) resolveArtifactStore(runFolder string) domain.ArtifactStore {
	if m.artifactStoreFactory != nil {
		return m.artifactStoreFactory(runFolder)
	}
	return artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md"))
}

// resolveClockTime returns the current timestamp for the COMPLETED marker
// write. When a Clock was injected it is used; otherwise a real UTC clock
// is used.
func (m *rootModel) resolveClockTime() time.Time {
	if m.clock != nil {
		return m.clock.Now()
	}
	return time.Now().UTC()
}

// ---------------------------------------------------------------------------
// Session starter
// ---------------------------------------------------------------------------

// startSession launches the session in a background goroutine and returns a tea.Cmd
// that delivers the result as a runDoneMsg or runErrorMsg.
func (m *rootModel) startSession() tea.Cmd {
	sel := m.selections
	sess := m.sess
	ctx := m.ctx

	return func() tea.Msg {
		var seedInputs []string
		if sel.isNewRun && sel.seedInput != "" {
			seedInputs = []string{sel.seedInput}
		}
		config := domain.RunConfig{
			OrchestratorFilePath: sel.orchestratorFile,
			WorkflowID:           sel.workflowID,
			Task:                 sel.task,
			RunID:                sel.runID,
			RunFolder:            sel.runFolder,
			IsNewRun:             sel.isNewRun,
			AllowVersionDrift:    sel.config.AllowVersionDrift,
			RunSettings:          sel.config.Settings,
			InfraClassSelections: sel.config.InfraClassSelections,
			SeedInputs:           seedInputs,
			ManualDispatch:       sel.manualDispatch,
		}
		outcome, err := sess.Start(ctx, config)
		if err != nil {
			return runErrorMsg{err: err}
		}
		return runDoneMsg{outcome: outcome}
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View renders the current screen to a string.
func (m *rootModel) View() string {
	switch m.screen {
	case screenRunSelect:
		if m.runSelectScreen != nil {
			return m.runSelectScreen.View()
		}
	case screenSetupFile:
		return m.fileScreen.View()
	case screenSetupWorkflow:
		if m.workflowScreen != nil {
			return m.workflowScreen.View()
		}
	case screenSetupTask:
		return m.taskScreen.View()
	case screenSetupSeedInput:
		return m.seedInputScreen.View()
	case screenSetupConfig:
		return m.configScreen.View()
	case screenProgress:
		if m.progressScreen != nil {
			return m.progressScreen.View()
		}
	case screenArtifact:
		if m.artifactScreen != nil {
			return m.artifactScreen.View()
		}
	case screenQuestion:
		return m.viewQuestion()
	case screenStop:
		if m.stopScreen != nil {
			return m.stopScreen.View()
		}
	case screenExecOverride:
		if m.execOverrideScreen != nil {
			return m.execOverrideScreen.View()
		}
	case screenDone:
		if m.doneScreen != nil {
			return m.doneScreen.View()
		}
	}
	return ""
}

func (m *rootModel) viewQuestion() string {
	if m.selectOverlay != nil {
		return m.selectOverlay.view()
	}
	if m.textOverlay != nil {
		return m.textOverlay.view()
	}
	if m.confirmOverlay != nil {
		return m.confirmOverlay.view()
	}
	return m.theme.Style(tuicommon.RoleMuted).Render("Waiting for question…")
}

