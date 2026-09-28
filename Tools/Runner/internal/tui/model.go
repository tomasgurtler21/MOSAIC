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

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens"
	"mosaic-run/internal/tui/screens/runconfig"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens/decision"
	"mosaic-run/internal/tui/screens/runflow"
	"mosaic-run/internal/tui/screens/setup"
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
	screenSetupHarness                   // harness adapter selection — first step of the setup sequence
	screenSetupFile                      // orchestrator file path entry (legacy; no longer in active flow)
	screenSetupWorkflow                  // workflow selection
	screenSetupTask                      // task description entry
	screenSetupSeedInput                 // seed-input path entry (new runs only)
	screenSetupConfig                    // run configuration prompts
	screenSetupGHCPMode                  // GHCP CLI permission-mode selection (shown only for ghcp-cli harness)
	screenProgress                       // live execution progress
	screenArtifact                       // read-only artifact inspection
	screenQuestion                       // generic overlay from Interaction port
	screenStop                           // stop recovery (retry / manual dispatch) — shown on RunStoppedByConsultant
	screenExecOverride                   // executable-override recovery — shown on harness launch failure
	screenDone                           // completion/error summary

	// Test-flow screens (only reachable when DevMode is true).
	screenTestCatalog  // MOSAIC repo root path entry for test mode
	screenTestSuite    // suite/scope selection (+ workflow/mode sub-pickers)
	screenTestHarness  // harness multi-select
	screenTestGHCPMode // GHCP permission mode selection (conditional on ghcp-cli)
	screenTestProgress // live test execution progress
	screenTestResults  // results summary
)

// rootModel is the top-level Bubble Tea model. It owns the navigation state machine.
type rootModel struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	theme     tuicommon.Theme
	screen    screenID
	width     int
	height    int

	// Session dependencies.
	sess                   session.Session
	sessionFactory         func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session
	mintRunIdentity        RunIdentityMinter
	interact               *ProgramRef
	orchestratorDiscoverer func(workDir, harnessID string) (string, error)
	onRunIDResolved        func(runID string)

	// Completion-marker write seam. When artifactStoreFactory is nil the TUI
	// constructs the store itself from the resolved run folder. When clock is
	// nil a real UTC clock is used.
	artifactStoreFactory func(runFolder string) domain.ArtifactStore
	clock                domain.Clock

	// Enumerated workflow regions (populated after orchestrator file is loaded).
	workflows []domain.WorkflowRegion

	// Run selection screen (shown when multiple resumable candidates exist).
	runSelectScreen *setup.RunSelectScreen

	// runSelectQuestion is the runselect.Question the run-select screen was
	// built from. A chosen Choice.ID is resolved back to a full Identity via
	// runselect.Answer, the same function the CLI's non-interactive path
	// would use to interpret an explicit --run value -- the screen never
	// holds a second copy of the selection rules.
	runSelectQuestion *runselect.Question

	// Entry screens (concrete types so back-navigation preserves state).
	harnessScreen   *setup.HarnessSelectScreen
	fileScreen      *setup.OrchestratorFileScreen
	workflowScreen  *setup.WorkflowSelectScreen
	taskScreen      *setup.TaskScreen
	seedInputScreen *runconfig.SeedInputScreen
	configScreen    *runconfig.ConfigScreen
	ghcpModeScreen  *runconfig.GHCPCLIModeScreen

	// Collected setup selections.
	selections runSetupSelections

	// Progress screen (constructed when execution starts).
	progressScreen *runflow.ProgressScreen

	// Artifact inspection screen.
	artifactScreen *runflow.ArtifactScreen
	prevScreen     screenID // screen to return to after artifact inspection

	// Generic question overlays from the Interaction port.
	activeQuestion *questionMsg
	selectOverlay  *inlineSelectOne
	textOverlay    *inlineText
	confirmOverlay *inlineConfirm

	// Stop recovery screen (shown when RunStoppedByConsultant).
	stopScreen *decision.StopScreen

	// Executable-override recovery screen (shown on harness launch failure).
	execOverrideScreen *decision.ExecOverrideScreen

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
	doneScreen *runflow.DoneScreen

	// devMode mirrors Options.DevMode: when true, the test flow screens are
	// registered and reachable. When false, they are never constructed.
	devMode bool

	// testCatalogLoader is the injected function for loading the test catalog.
	testCatalogLoader func(catalogRoot string) (testrun.CatalogPort, error)

	// testRunnerFactory is the injected test execution function.
	testRunnerFactory func(ctx context.Context, cfg testrun.TestConfig, reporter testrun.ProgressReporter) (*testrun.TestSummary, error)

	// Test-flow input screens (only constructed when devMode is true and the
	// user enters the test flow).
	testCatalogScreen  *devtest.TestCatalogScreen
	testSuiteScreen    *devtest.TestSuiteScreen
	testHarnessScreen  *devtest.TestHarnessScreen
	testGHCPModeScreen *devtest.TestGHCPModeScreen

	// Test-flow progress and results screens.
	testProgressScreen *devtest.TestProgressScreen
	testResultsScreen  *devtest.TestResultsScreen

	// testSelections holds the inputs collected in the test flow.
	testSelections testFlowSelections

	// stopSignal is the shared graceful-stop flag (session.StopSignal). See
	// Options.StopSignal for the contract; the same instance is closed over
	// by SessionFactory so the TUI's Request()/Reset() calls reach the
	// running session's dispatch loop.
	stopSignal *session.StopSignal

	// debug records the TUI-side stop-lifecycle entries. It is the same
	// instance the session logs through, so both halves of the lifecycle land
	// in one ordered log. See Options.Debug for the contract; never nil after
	// newRootModel, which normalises an omitted logger to a no-op.
	debug domain.DebugLogger
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

	// When DevMode is enabled, the harness screen prepends a "Run Tests" option
	// so the user can enter the test flow without selecting a harness.
	var harnessScreen *setup.HarnessSelectScreen
	if opts.DevMode {
		harnessScreen = setup.NewHarnessSelectScreenDevMode(w, h, style)
	} else {
		harnessScreen = setup.NewHarnessSelectScreen(w, h, style)
	}
	harnessScreen.SetToolVersion(opts.ToolVersion)
	fileScreen := setup.NewOrchestratorFileScreen(w, h, style)
	fileScreen.SetToolVersion(opts.ToolVersion)
	taskScreen := setup.NewTaskScreen(w, h, style)
	seedInputScreen := runconfig.NewSeedInputScreen(w, h, style)
	configScreen := runconfig.NewConfigScreen(w, h, style)
	ghcpModeScreen := runconfig.NewGHCPCLIModeScreen(w, h, style)

	interact := opts.Interaction
	if interact == nil {
		interact = NewProgramRef()
	}

	// Determine the initial screen and resolve run identity from pre-launch options.
	initialScreen := screenSetupHarness
	var runSelectScreen *setup.RunSelectScreen
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
		runSelectScreen = setup.NewRunSelectScreen(q, w, h, style)
		runSelectScreen.SetToolVersion(opts.ToolVersion)
		initialScreen = screenRunSelect
	}
	// Zero candidates (or pre-resolved): skip run select, go straight to setup.

	m := &rootModel{
		ctx:                    ctx,
		ctxCancel:              cancel,
		theme:                  opts.Theme,
		screen:                 initialScreen,
		width:                  w,
		height:                 h,
		sess:                   sess,
		sessionFactory:         opts.SessionFactory,
		mintRunIdentity:        opts.MintRunIdentity,
		interact:               interact,
		orchestratorDiscoverer: opts.OrchestratorDiscoverer,
		runSelectScreen:        runSelectScreen,
		runSelectQuestion:      runSelectQuestion,
		harnessScreen:          harnessScreen,
		fileScreen:             fileScreen,
		taskScreen:             taskScreen,
		seedInputScreen:        seedInputScreen,
		configScreen:           configScreen,
		ghcpModeScreen:         ghcpModeScreen,
		artifactStoreFactory:   opts.ArtifactStoreFactory,
		clock:                  opts.Clock,
		onRunIDResolved:        opts.OnRunIDResolved,
		stopSignal:             opts.StopSignal,
		debug:                  opts.Debug,
		devMode:                opts.DevMode,
		testCatalogLoader:      opts.TestCatalogLoader,
		testRunnerFactory:      opts.TestRunnerFactory,
	}
	if m.stopSignal == nil {
		m.stopSignal = session.NewStopSignal()
	}
	if m.debug == nil {
		m.debug = domain.NopDebugLogger{}
	}

	// Always propagate InitialRunFolder so readArtifactContent and the COMPLETED-marker
	// write target the correct path regardless of whether other identity fields are set.
	m.selections.runFolder = opts.InitialRunFolder
	// Pre-populate run identity when already resolved (--run / --new-run / single candidate).
	if preRunID != "" || preIsNewRun {
		m.selections.runID = preRunID
		m.selections.isNewRun = preIsNewRun
	}
	// A run resolved before launch never reaches the run-select screen, which is
	// where a chosen run adopts its recorded workflow. Since the setup sequence
	// no longer asks a resumed run which workflow to run, the recorded value has
	// to enter here or not at all.
	if !preIsNewRun && opts.RecordedWorkflowID != "" {
		m.selections.workflowID = opts.RecordedWorkflowID
	}

	return m
}

// candidatesToQuestion adapts a scan result into the runselect.Question shape
// setup.RunSelectScreen takes: the always-present new-run choice, then
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
	// The harness-select screen (and the run-select screen) are list-based and
	// require no init command. Text-input screens call their own InputInit when
	// transitioning to them.
	return nil
}
