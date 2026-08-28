// Package tui implements the interactive frontend for mosaic-agent-test. It
// folds the identical progress-event stream the CLI renders as lines into a
// Model, and draws that Model on the shared theme, key set and scaffold
// every MOSAIC frontend builds on (mosaic-common/tui).
//
// Model, Update, View and Fold are the testable core: they run with no real
// terminal, which is what makes the navigation, folding, drill-down,
// rendering, equivalence and responsiveness tests in this package possible
// without one.
package tui

import (
	"bytes"
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	commonharness "mosaic-common/harness"
	tuicommon "mosaic-common/tui"
	"mosaic-common/tui/widgets"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
	"mosaic-agent-test/internal/runprogress"
	"mosaic-agent-test/internal/suite"
	"mosaic-agent-test/internal/tui/screens"
)

// PreflightFunc resolves suite input into a validated plan. Same shape as
// the CLI's, so a scripted preflight can be handed to either frontend
// interchangeably in the equivalence test.
type PreflightFunc func(preflight.Input) (preflight.Plan, authoring.Report)

// WriteFileFunc writes data to path, creating the file or truncating an
// existing one, and creating any missing parent directories. Supplied by the
// composition root; this package constructs nothing and touches no real
// filesystem of its own, so a test observes report writing without one.
//
// A nil WriteFileFunc means "no report file can be written": the frontend
// reports that as a report-write failure rather than silently skipping the
// write, so a wiring omission is visible.
type WriteFileFunc func(path string, data []byte) error

// StoreFunc processes report files into the TestResults tree. The composition
// root supplies a function that calls resultstore.StoreFromPaths with a real
// filesystem and the resolved TestResults root.
//
// Following the PreflightFunc precedent, this type references resultstore types
// directly rather than defining mirror types.
type StoreFunc func(req resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error)

// SummaryFunc generates summary Markdown files from stored reports. The
// composition root supplies a function that calls resultsummary.Generate with a
// real filesystem and the resolved TestResults root.
type SummaryFunc func(req resultsummary.SummaryRequest) (resultsummary.SummaryResult, error)

// StoreFinishedMsg carries the store operation's outcome into the Bubble Tea
// message loop. Follows the same pattern as SuiteFinishedMsg.
type StoreFinishedMsg struct {
	Result resultstore.StoreResult
	Err    error
}

// SummaryFinishedMsg carries the summary generation's outcome into the Bubble
// Tea message loop.
type SummaryFinishedMsg struct {
	Result resultsummary.SummaryResult
	Err    error
}

// processReportsState groups the Model fields for the "Process Test Reports"
// flow, keeping them namespaced rather than adding flat fields to Model's
// already large field set.
type processReportsState struct {
	// mode tracks whether the user is in "Store Reports" or "Generate Summary".
	mode string // "store" or "summary"

	// cursor is the selected item index on mode-select and process-select screens.
	cursor int

	// Store flow state.
	storeInput  *screens.StoreInputScreen
	storeResult *resultstore.StoreResult
	storeErr    error

	// Summary flow state.
	summaryInput  *screens.SummaryInputScreen
	summaryResult *resultsummary.SummaryResult
	summaryErr    error
}

// SuiteRunner is the interface the TUI drives the suite through. Unlike
// cli.SuiteRunner (which the CLI's factory rebuilds per invocation, already
// carrying its resolved retention), the TUI wires a per-run runner through
// Options.NewSuiteRunner so that both retention and the max-concurrent-runs
// bound — each of which the settings screen can change between runs — reach
// the suite with their live values. Run takes the live retention policy as a
// call-time argument. The max-concurrent-runs bound is resolved from the
// model and supplied when constructing the runner (via Options.NewSuiteRunner)
// rather than as a further call-time argument.
type SuiteRunner interface {
	Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink, retention domain.RetentionPolicy) (report.Result, error)
}

// Options is the pre-wired dependency set the composition root hands in.
type Options struct {
	Preflight PreflightFunc

	// Suite is the suite runner used when NewSuiteRunner is nil. Tests supply
	// a fake runner here; production code sets NewSuiteRunner instead so that
	// the live max-concurrent-runs bound is forwarded on every run.
	Suite SuiteRunner

	// NewSuiteRunner, when non-nil, constructs a fresh SuiteRunner and a
	// per-harness PreflightFunc for each run, incorporating the caller-supplied
	// max-concurrent-runs bound and the harness selection. The composition root
	// resolves the per-harness adapter, decoder, and environment from harnessID
	// and returns both a runner and a preflight function that validates against
	// that harness's environment, ensuring validation and execution agree on the
	// harness by construction.
	//
	// An error return means the harness selection cannot be honored (unknown
	// harness, CheckEnvironment failure). The TUI surfaces this as a run
	// failure with a diagnostic rather than falling back to a default.
	//
	// When nil, Suite and the top-level Preflight are used instead (fallback
	// for tests that supply a fake runner directly).
	NewSuiteRunner func(maxConcurrentRuns int, harnessID string) (SuiteRunner, PreflightFunc, error)

	// Suites are the discovered suite paths offered for selection on the
	// suite-select screen.
	Suites []string

	// Harness is the initially selected harness identity, from the CLI flag
	// or its default. The harness-select screen may change it.
	Harness string

	// Harnesses is the selectable catalog, supplied by the composition root
	// from the same mosaic-common/harness source the CLI uses. The TUI
	// restates no harness identity of its own.
	Harnesses []commonharness.CLIHarness

	// Models is the per-harness model catalog, supplied by the composition
	// root from the same mosaic-common/harness source the CLI uses. The TUI
	// restates no model identity of its own.
	Models []commonharness.ModelCatalog

	// Retention is the initial retention policy; the suite-select screen's
	// toggle affordance may change it. Both frontends resolve to the same
	// domain.RetentionPolicy value reaching the same runner field.
	Retention domain.RetentionPolicy

	// ReportPath is the initial JSON report file location, shown on the
	// suite-select screen and editable there before a run starts. The
	// composition root resolves the same default the CLI uses.
	ReportPath string

	// ReportPathFor, when non-nil, is called with the selected suite path when
	// a run is about to start to compute the actual report file location. It is
	// only called when the user has not manually edited the path since the model
	// was constructed (i.e. the current path still matches the initial
	// ReportPath). This lets the composition root supply a function that encodes
	// the suite name and current timestamp in the filename for each run, so
	// repeated runs of the same suite produce distinct report files.
	ReportPathFor func(suitePath string) string

	// WriteFile writes the JSON report file. See WriteFileFunc.
	WriteFile WriteFileFunc

	// CatalogFolder is the initial/default catalog folder path, shown on
	// the suite-select screen and editable there before a run starts. The
	// composition root resolves the same default the CLI uses
	// (WiringConfig.CatalogFolder).
	CatalogFolder string

	// ResolveSuiteDefaults returns the declared defaults for a suite file.
	// Called lazily when the selected suite changes, not on every render.
	// A nil function or an error result degrades to ProvenanceUnknown display
	// rather than blocking navigation. The composition root wires this to a
	// function that reads and parses the suite file's defaults block using the
	// same schema preflight uses, ensuring the displayed value agrees with the
	// one preflight applies at run start.
	ResolveSuiteDefaults func(suitePath string) (SuiteDefaults, error)

	// Store processes report files into TestResults/. Nil means the
	// "Store Reports" flow is unavailable (the TUI disables the option
	// rather than crashing).
	Store StoreFunc

	// Summary generates summary Markdown. Nil means the "Generate
	// Summary" flow is unavailable.
	Summary SummaryFunc

	// TestResultsRoot is the absolute path to TestResults/, shown read-only
	// in the store/summary input screens for user confirmation.
	TestResultsRoot string
}

// Screen names one of the screens this frontend presents.
type Screen string

const (
	// ScreenModeSelect is the new entry point: "Run Tests" vs
	// "Process Test Reports". Replaces ScreenHarnessSelect/ScreenSuiteSelect
	// as the initial screen.
	ScreenModeSelect Screen = "mode_select"

	// ScreenProcessSelect offers "Store Reports" and "Generate Summary"
	// under the "Process Test Reports" mode.
	ScreenProcessSelect Screen = "process_select"

	// ScreenStoreInput collects file/directory path for the store operation.
	ScreenStoreInput Screen = "store_input"

	// ScreenStoreResult displays the outcome of the store operation.
	ScreenStoreResult Screen = "store_result"

	// ScreenSummaryInput collects the optional version filter for summary.
	ScreenSummaryInput Screen = "summary_input"

	// ScreenSummaryResult displays the outcome of summary generation.
	ScreenSummaryResult Screen = "summary_result"

	// ScreenHarnessSelect offers the harness-selection affordance —
	// the TUI's equivalent of the CLI's --harness flag — following the
	// same cursor-and-select pattern as ScreenSuiteSelect, sourced from
	// Options.Harnesses.
	ScreenHarnessSelect Screen = "harness_select"

	// ScreenModelSelect offers subject-model then stub-model selection from
	// Options.Models, keyed on the currently selected harness. It is one
	// screen with two phases rather than two screens, because the two
	// selections share a list, a cursor convention and an escape route, and
	// because the stub phase's list is the subject phase's list plus one
	// entry.
	ScreenModelSelect Screen = "model_select"

	ScreenSuiteSelect Screen = "suite_select"
	ScreenProgress    Screen = "progress"
	ScreenResults     Screen = "results"
	ScreenDetail      Screen = "detail"

	// Setting screens for the sequential run-configuration flow. Each screen
	// is self-contained and driven by Done()/Back() checks in the root model.
	ScreenRetention         Screen = "retention"
	ScreenRepetitions       Screen = "repetitions"
	ScreenReportPath        Screen = "report_path"
	ScreenCatalogFolder     Screen = "catalog_folder"
	ScreenMaxConcurrentRuns Screen = "max_concurrent_runs"
)

// modelSelectPhase tracks which phase of the model-selection screen is active.
type modelSelectPhase int

const (
	modelPhaseSubject modelSelectPhase = iota
	modelPhaseStub
)

// ProgressMsg carries one folded progress event into the Bubble Tea message
// loop.
type ProgressMsg struct{ Event domain.ProgressEvent }

// SuiteFinishedMsg carries the suite's terminal outcome into the message
// loop, once Suite.Run returns.
type SuiteFinishedMsg struct {
	Result report.Result
	Err    error
}

// Model is the TUI's state, folded from the progress-event stream and
// driven by key input. It is a value type, like every Bubble Tea model in
// this ecosystem: Update and Fold both return a new Model rather than
// mutating one in place.
//
// Fields are unexported per the design; this package's own tests construct
// and inspect Model directly (they are compiled into package tui, not
// tui_test), and every other consumer reads it only through the exported
// accessors below.
type Model struct {
	opts   Options
	theme  tuicommon.Theme
	screen Screen
	width  int
	height int

	ctx    context.Context
	cancel context.CancelFunc

	// sinkBox is a pointer so that Run can install the real, program-backed
	// sink after tea.NewProgram constructs its own copy of the initial
	// Model: every copy shares the same box. NewModel installs a discard
	// sink so a Model driven directly by tests (no Run, no real program)
	// never dereferences a nil sink.
	sinkBox *sinkBox

	// Harness-select screen state (Stage 5). selectedHarness starts as
	// Options.Harness (the CLI-flag-or-default initial selection) and is
	// updated once a harness-select entry is chosen.
	harnessCursor   int
	selectedHarness string

	// Model-select screen state (Stage 12). The screen has two sequential
	// phases: subject then stub. modelPhase tracks which phase is active.
	// modelCursor is the cursor position within the current phase's list.
	// pendingSubjectModel holds the model confirmed on the subject phase
	// before the stub phase completes; both are committed to
	// selectedSubjectModel / selectedStubModel when the stub phase is
	// accepted and control moves to suite-select.
	modelPhase           modelSelectPhase
	modelCursor          int
	pendingSubjectModel  string
	selectedSubjectModel string
	selectedStubModel    string

	// Suite-select screen state.
	suiteCursor     int
	running         bool
	suiteMultiSelect *widgets.MultiSelect

	// selectedSuites is the ordered list of suite paths confirmed by the
	// multiselect widget when the user presses Enter on the suite-select screen.
	// For a single-press-Enter selection (no Space toggles), it holds one entry.
	selectedSuites []string

	// selectedSuiteIdx is the zero-based index into selectedSuites of the
	// suite currently running (or the last one that finished). It advances
	// after each suite completes so the next suite in the queue can start.
	selectedSuiteIdx int

	// resolvedSuiteDefaults holds the declared defaults from the currently
	// selected suite file, resolved lazily on construction and on each
	// cursor change. A zero SuiteDefaults (nil Repetitions) means either
	// the suite declares no default or the resolver is nil/returned an error;
	// both cases degrade to the "suite default" label.
	resolvedSuiteDefaults SuiteDefaults

	// retention starts as Options.Retention and is updated by the
	// suite-select screen's toggle affordance (Stage 7).
	retention domain.RetentionPolicy

	// repetitions is the user-configured override for the number of repetitions
	// per test. When nil, the suite's authored default applies. The suite-select
	// screen shows this value; it threads through preflight.Overrides.Repetitions
	// when a run starts, following the same pattern as selectedSubjectModel and
	// selectedStubModel.
	repetitions *int

	// maxConcurrentRuns is the user-configured override for the maximum number
	// of runs executing concurrently across the suite. When nil, the suite
	// applies suite.DefaultMaxConcurrentRuns. Follows the same nil-means-default
	// pattern as repetitions. The value is threaded into the suite's options at
	// run-start by the composition root (I12.5).
	maxConcurrentRuns *int

	// catalogFolder is the catalog folder currently in force. Starts as
	// Options.CatalogFolder; the suite-select screen shows it and allows
	// editing before a run starts. When it differs from Options.CatalogFolder
	// at run-start, it flows as a per-run override through
	// preflight.Overrides.CatalogFolder.
	catalogFolder string

	// reportPath is the JSON report file path currently in force. Starts as
	// Options.ReportPath; the settings screen's inline-edit affordance may
	// change it before a run starts. The value at run-start is what WriteFile
	// receives. An empty value suppresses the write.
	reportPath string

	// Screen instances for the sequential run-configuration flow.
	// Nil until the user confirms a suite via Enter from ScreenSuiteSelect.
	// Each screen is initialized in initSettingScreens() at suite-confirmation
	// time and holds its own input/display state through the flow.
	retentionScr         *screens.RetentionScreen
	repetitionsScr       *screens.RepetitionsScreen
	reportPathScr        *screens.ReportPathScreen
	catalogFolderScr     *screens.CatalogFolderScreen
	maxConcurrentRunsScr *screens.MaxConcurrentRunsScreen

	// Folded progress state: every field here is set only by Fold, from an
	// event or the terminal result model, and nothing in this package
	// aggregates a number of its own.
	//
	// progress is the shared pure-core fold that both frontends import,
	// providing the multi-run in-flight set, the running/finished/remaining
	// tally, and per-run invocation counts. It replaces the single-run
	// scalar fields (runningTestID, runningRepetition, runningRepetitions,
	// observedInvocations) that could not represent more than one run at a
	// time.
	progress runprogress.Model

	// finished holds each finished run's outcome for the results and detail
	// screens. Populated from ProgressTestFinished events in Fold; kept
	// separately from progress because the report display needs the full
	// RunReport shape, not just the in-flight display shape.
	finished  []report.RunReport
	result    *report.Result
	resultErr error

	// Results/detail screen state.
	resultsCursor      int
	detailScrollOffset int // scroll position on the test-detail screen; reset on every drill-in

	statusMsg   string
	statusError bool

	// processReports holds state for the "Process Test Reports" flow.
	// Grouped into a single struct to keep Model's flat field count manageable.
	processReports processReportsState

	// detailPane renders failure detail (pre-flight diagnostics, run-failure
	// and report-write-failure text) that will not fit the one-line status bar.
	// It is a pointer for the same reason sinkBox is: Model is a value type
	// whose Update and Fold return copies, and the pane owns mutable scroll
	// state that must survive those copies.
	detailPane *widgets.DetailPane

	// showFailureDetail reports whether detailPane currently holds live failure
	// detail that the active screen should render. It is a value field so that
	// a Model copy which has not been given failure detail does not show a
	// stale pane.
	showFailureDetail bool

	// failureTitle and failureBody hold the exact strings last handed to
	// detailPane.SetContent. They are the Model's own record of that content,
	// because widgets.DetailPane stores its title and body unexported and offers
	// no getter — SetContent is write-only and View() is scroll-clamped, so the
	// pane cannot be read back from another package. These fields are what
	// FailureDetail() returns. They are value fields, so they copy with the Model
	// exactly as showFailureDetail does.
	failureTitle string
	failureBody  string
}

// Compile-time assertion that Model satisfies tea.Model.
var _ tea.Model = Model{}

// resolveForSuite calls opts.ResolveSuiteDefaults for the suite at the given
// cursor position and returns the result. When the resolver is nil, the cursor
// is out of range, or the resolver returns an error, it returns a zero
// SuiteDefaults (nil Repetitions), which SettingsEntries() treats as
// ProvenanceUnknown and renders as the generic "suite default" label.
func resolveForSuite(o Options, cursor int) SuiteDefaults {
	if o.ResolveSuiteDefaults == nil || cursor >= len(o.Suites) {
		return SuiteDefaults{}
	}
	d, err := o.ResolveSuiteDefaults(o.Suites[cursor])
	if err != nil {
		return SuiteDefaults{}
	}
	return d
}

// NewModel constructs the initial Model on the mode-select screen. The user
// chooses between "Run Tests" (entering the existing harness/suite-select flow)
// and "Process Test Reports" (store or summary operations).
func NewModel(o Options) Model {
	m := Model{
		opts:            o,
		theme:           tuicommon.DefaultTheme(),
		screen:          ScreenModeSelect,
		width:           tuicommon.DefaultWidth,
		height:          tuicommon.DefaultHeight,
		ctx:             context.Background(),
		sinkBox:         newSinkBox(),
		selectedHarness: o.Harness,
		retention:       o.Retention,
		reportPath:      o.ReportPath,
		catalogFolder:   o.CatalogFolder,
	}
	paneH, paneW := m.paneGeometry()
	m.detailPane = widgets.NewDetailPane(paneH, paneW, widgets.DefaultDetailPaneStyles())
	m.suiteMultiSelect = buildSuiteMultiSelect(o)
	return m
}

// buildSuiteMultiSelect constructs the multiselect widget for the suite-select
// screen from the given options. Items use the suite path as both ID and label.
func buildSuiteMultiSelect(o Options) *widgets.MultiSelect {
	items := make([]widgets.ListItem, len(o.Suites))
	for i, s := range o.Suites {
		items[i] = widgets.ListItem{ID: s, Label: s}
	}
	h := tuicommon.ContentHeight(tuicommon.DefaultHeight, false)
	if h <= 0 {
		h = 10
	}
	return widgets.NewMultiSelect(items, h, tuicommon.DefaultWidth, widgets.DefaultMultiSelectStyles())
}

// paneGeometry returns the height and width for the failure-detail pane,
// derived from the model's current terminal dimensions. The pane is guaranteed
// at least 3 lines. The width is set to the full content width so that lipgloss
// word-wraps long diagnostic lines to fit the terminal rather than rendering
// them as a single overflowing line.
func (m Model) paneGeometry() (height, width int) {
	h := m.height
	if h <= 0 {
		h = tuicommon.DefaultHeight
	}
	contentH := tuicommon.ContentHeight(h, false)
	paneH := contentH / 2
	if paneH < 3 {
		paneH = 3
	}
	return paneH, m.contentWidth()
}

// withFailureDetail sets the failure-detail pane content and marks it live.
// title is a short label naming the failing operation; body is the full
// detail text, never truncated. It updates the pane and the Model's own
// readable copies together — neither is updated without the other.
func (m Model) withFailureDetail(title, body string) Model {
	m.detailPane.SetContent(title, body)
	m.failureTitle = title
	m.failureBody = body
	m.showFailureDetail = true
	return m
}

// clearFailureDetail clears the failure-detail pane and marks it not live.
// It updates the pane and the Model's own readable copies together.
func (m Model) clearFailureDetail() Model {
	m.detailPane.SetContent("", "")
	m.failureTitle = ""
	m.failureBody = ""
	m.showFailureDetail = false
	return m
}

// FailureDetail returns the failure detail currently held for display: a
// short title naming the failing operation, the full untruncated body, and
// whether any failure detail is live at all. When shown is false, title and
// body are empty.
func (m Model) FailureDetail() (title string, body string, shown bool) {
	if !m.showFailureDetail {
		return "", "", false
	}
	return m.failureTitle, m.failureBody, true
}

// Init is called once when the Bubble Tea program starts. Nothing needs to
// happen before the first user input: the suite-select screen renders
// straight from Options.Suites.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update processes one incoming message and drives the screen state
// machine: suite selection to live progress to results, drill into a test's
// detail and back, and quit from any screen. Quitting or interrupting
// during a run must cancel through the suite's context so the per-test
// lifecycle's guaranteed teardown still runs.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		paneH, paneW := m.paneGeometry()
		m.detailPane.Resize(paneH, paneW)
		m.resizeActiveSettingScreen(msg.Width)
		if m.suiteMultiSelect != nil {
			h := tuicommon.ContentHeight(msg.Height, false)
			if h <= 0 {
				h = 10
			}
			m.suiteMultiSelect.Resize(h, msg.Width)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(normalizeEnter(msg))

	case ProgressMsg:
		m = m.Fold(msg.Event)
		return m, nil

	case SuiteFinishedMsg:
		return m.handleSuiteFinished(msg)

	case StoreFinishedMsg:
		return m.handleStoreFinished(msg), nil

	case SummaryFinishedMsg:
		return m.handleSummaryFinished(msg), nil
	}

	return m, nil
}

// normalizeEnter maps the literal carriage-return rune (as this package's
// own tests send it, matching how a raw terminal in cooked/raw mode may
// deliver Enter) onto tea.KeyEnter, so a single check against the shared
// GlobalKeys.Select binding recognises both forms.
func normalizeEnter(msg tea.KeyMsg) tea.KeyMsg {
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "\r" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return msg
}

// handleKey routes a key press. The shared Cancel binding quits from any
// screen; while a suite is running it cancels through the suite's context
// first, so the per-test lifecycle's guaranteed teardown removes the
// workspace instead of the process simply exiting (AC16.7).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Cancel) {
		cancel := m.cancel
		return m, func() tea.Msg {
			if cancel != nil {
				cancel()
			}
			return tea.Quit()
		}
	}

	switch m.screen {
	case ScreenModeSelect:
		return m.updateModeSelect(msg)
	case ScreenProcessSelect:
		return m.updateProcessSelect(msg)
	case ScreenStoreInput:
		return m.updateStoreInput(msg)
	case ScreenStoreResult:
		return m.updateStoreResult(msg)
	case ScreenSummaryInput:
		return m.updateSummaryInput(msg)
	case ScreenSummaryResult:
		return m.updateSummaryResult(msg)
	case ScreenHarnessSelect:
		return m.updateHarnessSelect(msg)
	case ScreenModelSelect:
		return m.updateModelSelect(msg)
	case ScreenSuiteSelect:
		return m.updateSuiteSelect(msg)
	case ScreenRetention:
		return m.updateRetentionScreen(msg)
	case ScreenRepetitions:
		return m.updateRepetitionsScreen(msg)
	case ScreenReportPath:
		return m.updateReportPathScreen(msg)
	case ScreenCatalogFolder:
		return m.updateCatalogFolderScreen(msg)
	case ScreenMaxConcurrentRuns:
		return m.updateMaxConcurrentRunsScreen(msg)
	case ScreenResults:
		return m.updateResults(msg)
	case ScreenDetail:
		return m.updateDetail(msg)
	default:
		// ScreenProgress accepts no navigation keys beyond the shared
		// Cancel binding handled above; the suite is driving the screen.
		return m, nil
	}
}

// updateHarnessSelect handles harness-cursor movement and recording the
// chosen harness, following the same cursor-and-select pattern as
// updateSuiteSelect. Selecting moves to the suite-select screen — the
// harness-select screen is always the entry point when a catalog is
// offered, so this is the only transition into suite selection on that
// path.
func (m Model) updateHarnessSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
		if m.harnessCursor < len(m.opts.Harnesses)-1 {
			m.harnessCursor++
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
		if m.harnessCursor > 0 {
			m.harnessCursor--
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		if len(m.opts.Harnesses) == 0 || m.harnessCursor >= len(m.opts.Harnesses) {
			return m, nil
		}
		m.selectedHarness = m.opts.Harnesses[m.harnessCursor].ID
		// Reset model-select state so a new harness starts clean: any selection
		// made for the previous harness cannot survive into this one.
		m.modelPhase = modelPhaseSubject
		m.modelCursor = 0
		m.pendingSubjectModel = ""
		m.selectedSubjectModel = ""
		m.selectedStubModel = ""
		m.screen = ScreenModelSelect
		return m, nil
	}
	return m, nil
}

// currentModelIDs returns the model identifiers available for the currently
// selected harness, from Options.Models. Returns nil when no catalog entry
// exists for the selected harness — an unwired catalog must not block the
// frontend, so callers treat nil and empty the same way.
func (m Model) currentModelIDs() []string {
	for _, mc := range m.opts.Models {
		if mc.HarnessID == m.selectedHarness {
			return mc.IDs
		}
	}
	return nil
}

// updateModelSelect handles the model-selection screen's two sequential phases.
//
// Subject phase: the user selects which model the agent under test runs on,
// from the harness's catalog. Enter advances to the stub phase; Escape returns
// to harness-select.
//
// Stub phase: the user selects which model every stub collaborator runs on.
// The list is "same as subject" (position 0, resolves to empty StubModel
// override) followed by the harness's catalog. Enter accepts the selection and
// advances to suite-select; Escape returns to the subject phase.
func (m Model) updateModelSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.currentModelIDs()

	switch m.modelPhase {
	case modelPhaseSubject:
		switch {
		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
			if m.modelCursor < len(ids)-1 {
				m.modelCursor++
			}
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
			if m.modelCursor > 0 {
				m.modelCursor--
			}
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
			// Confirm the subject model at the current cursor position.
			if m.modelCursor < len(ids) {
				m.pendingSubjectModel = ids[m.modelCursor]
			} else {
				m.pendingSubjectModel = ""
			}
			// Advance to stub phase with cursor reset to position 0 ("same as subject").
			m.modelPhase = modelPhaseStub
			m.modelCursor = 0
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
			// Escape: return to harness-select.
			m.modelPhase = modelPhaseSubject
			m.modelCursor = 0
			m.pendingSubjectModel = ""
			m.screen = ScreenHarnessSelect
			return m, nil
		}

	case modelPhaseStub:
		// Stub list: ["same as subject"] + ids.
		stubLen := 1 + len(ids)
		switch {
		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
			if m.modelCursor < stubLen-1 {
				m.modelCursor++
			}
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
			if m.modelCursor > 0 {
				m.modelCursor--
			}
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
			// Commit selections from both phases.
			m.selectedSubjectModel = m.pendingSubjectModel
			if m.modelCursor == 0 {
				// "Same as subject" resolves to empty StubModel, matching
				// the CLI's "omit --stub-model" case.
				m.selectedStubModel = ""
			} else {
				stubIdx := m.modelCursor - 1
				if stubIdx < len(ids) {
					m.selectedStubModel = ids[stubIdx]
				} else {
					m.selectedStubModel = ""
				}
			}
			m.pendingSubjectModel = ""
			m.modelPhase = modelPhaseSubject
			m.modelCursor = 0
			m.screen = ScreenSuiteSelect
			return m, nil

		case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
			// Escape: return to subject phase with cursor reset.
			m.modelPhase = modelPhaseSubject
			m.modelCursor = 0
			// Clear the pending subject model since the user may re-select.
			m.pendingSubjectModel = ""
			return m, nil
		}
	}

	return m, nil
}

// updateSuiteSelect handles suite selection using the multiselect widget.
// Space toggles the suite under the cursor; Enter confirms the selection and
// transitions to the first settings screen (ScreenRetention). When Enter is
// pressed without any suite toggled, the suite under the cursor is treated as
// the selection, preserving the single-press-Enter behavior.
func (m Model) updateSuiteSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle PgDown/PgUp for the failure-detail pane before the widget sees them.
	switch {
	case msg.Type == tea.KeyPgDown:
		if m.showFailureDetail {
			m.detailPane.ScrollDown()
		}
		return m, nil
	case msg.Type == tea.KeyPgUp:
		if m.showFailureDetail {
			m.detailPane.ScrollUp()
		}
		return m, nil
	}

	if m.suiteMultiSelect == nil {
		return m, nil
	}
	m.suiteMultiSelect.Update(msg)
	// Keep suiteCursor in sync with the widget cursor so SettingsEntries()
	// and resolveForSuite() see the current cursor position while navigating.
	m.suiteCursor = m.suiteMultiSelect.CursorIndex()

	if m.suiteMultiSelect.Done() {
		m.suiteMultiSelect.Reset()
		selectedIDs := m.suiteMultiSelect.SelectedIDs()
		// When no suite was toggled, fall back to the cursor item so that a
		// single Enter press (without Space) still confirms the current suite.
		if len(selectedIDs) == 0 && m.suiteCursor < len(m.opts.Suites) {
			selectedIDs = []string{m.opts.Suites[m.suiteCursor]}
		}
		if len(selectedIDs) == 0 {
			return m, nil
		}
		m.selectedSuites = selectedIDs

		// Resolve suite-scoped defaults from the first selected suite.
		// A nil resolver is silently skipped; an error keeps the user on
		// ScreenSuiteSelect so they can choose a different suite.
		suitePath := selectedIDs[0]
		if m.opts.ResolveSuiteDefaults != nil {
			defaults, err := m.opts.ResolveSuiteDefaults(suitePath)
			if err != nil {
				m.statusMsg = "cannot load suite defaults"
				m.statusError = true
				return m, nil
			}
			m.resolvedSuiteDefaults = defaults
		}
		// Recompute the report path from the first selected suite when the
		// composition root has supplied a path function and the user has not
		// manually edited the path. This must happen before initSettingScreens
		// so the settings flow displays the real output location.
		if m.opts.ReportPathFor != nil && m.reportPath == m.opts.ReportPath {
			m.reportPath = m.opts.ReportPathFor(suitePath)
		}
		m = m.initSettingScreens()
		m.screen = ScreenRetention
		return m, nil
	}

	if m.suiteMultiSelect.Back() {
		m.suiteMultiSelect.Reset()
		// Esc from suite-select: no parent screen to return to; stay here.
	}

	return m, nil
}

// initSettingScreens constructs fresh screen instances for all five per-run
// settings, initialized from the current model state and resolved suite defaults.
// Suite-scoped defaults (repetitions) are used as the initial value for the
// corresponding screen only when the user has not already set an explicit
// override. Screens are pointer types so their state persists across model
// copies during the settings flow.
func (m Model) initSettingScreens() Model {
	var styles screens.Styles // zero-value: no lipgloss styling; suitable for headless tests
	width := m.width

	// Normalize empty retention to RetainNever so the cycle (Space key)
	// starts at a known position rather than treating "" as an unknown value.
	retentionInitial := m.retention
	if retentionInitial == "" {
		retentionInitial = domain.RetainNever
	}
	m.retentionScr = screens.NewRetentionScreen(retentionInitial, width, styles)

	// RepetitionsScreen: when the user has set an explicit override, start from
	// that value. When no override is set, display the resolved suite default so
	// the user sees the effective value before making any changes. Pressing Enter
	// without typing is detected via WasEdited() in updateRepetitionsScreen and
	// leaves m.repetitions nil (no override committed).
	repsInitial := 0
	if m.repetitions != nil {
		repsInitial = *m.repetitions
	} else if m.resolvedSuiteDefaults.Repetitions != nil {
		repsInitial = *m.resolvedSuiteDefaults.Repetitions
	}
	m.repetitionsScr = screens.NewRepetitionsScreen(repsInitial, width, styles)

	m.reportPathScr = screens.NewReportPathScreen(m.reportPath, width, styles)
	m.catalogFolderScr = screens.NewCatalogFolderScreen(m.catalogFolder, width, styles)

	// MaxConcurrentRunsScreen: when the user has set an explicit override, start
	// from that value. Otherwise display the documented conservative default so
	// the user sees what bound will apply before making any changes. Pressing
	// Enter without typing is detected via WasEdited() in
	// updateMaxConcurrentRunsScreen and leaves m.maxConcurrentRuns nil.
	maxInitial := suite.DefaultMaxConcurrentRuns
	if m.maxConcurrentRuns != nil {
		maxInitial = *m.maxConcurrentRuns
	}
	m.maxConcurrentRunsScr = screens.NewMaxConcurrentRunsScreen(maxInitial, width, styles)

	return m
}

// ---------------------------------------------------------------------------
// Sequential settings screen handlers
//
// Each handler forwards the key message to the active setting screen and
// checks Done()/Back() to drive the sequential navigation state machine
// described in the design. The pattern is identical across all five screens:
//   - Done: Reset the screen (clears flags, preserves value), advance forward.
//   - Back: Reset the screen, go backward.
//   - Neither: return the model unchanged (key consumed by the screen).
// ---------------------------------------------------------------------------

func (m Model) updateRetentionScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.retentionScr == nil {
		return m, nil
	}
	cmd := m.retentionScr.Update(msg)
	if m.retentionScr.Done() {
		// Capture the confirmed value into the model field BEFORE Reset()
		// restores the screen's policy to its initial value.
		m.retention = m.retentionScr.Policy()
		m.retentionScr.Reset()
		m.screen = ScreenRepetitions
	} else if m.retentionScr.Back() {
		m.retentionScr.Reset()
		m.screen = ScreenSuiteSelect
	}
	return m, cmd
}

func (m Model) updateRepetitionsScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.repetitionsScr == nil {
		return m, nil
	}
	cmd := m.repetitionsScr.Update(msg)
	if m.repetitionsScr.Done() {
		// Only commit a new override when the user actually typed something.
		// Pressing Enter without typing preserves the existing m.repetitions
		// value (nil when no override is set) so the suite applies its own default.
		if m.repetitionsScr.WasEdited() {
			v := m.repetitionsScr.Value()
			if v > 0 {
				m.repetitions = &v
			} else {
				m.repetitions = nil
			}
		}
		m.repetitionsScr.Reset()
		m.screen = ScreenReportPath
	} else if m.repetitionsScr.Back() {
		m.repetitionsScr.Reset()
		m.screen = ScreenRetention
	}
	return m, cmd
}

func (m Model) updateReportPathScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.reportPathScr == nil {
		return m, nil
	}
	cmd := m.reportPathScr.Update(msg)
	if m.reportPathScr.Done() {
		m.reportPath = m.reportPathScr.Path()
		m.reportPathScr.Reset()
		m.screen = ScreenCatalogFolder
	} else if m.reportPathScr.Back() {
		m.reportPathScr.Reset()
		m.screen = ScreenRepetitions
	}
	return m, cmd
}

func (m Model) updateCatalogFolderScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.catalogFolderScr == nil {
		return m, nil
	}
	cmd := m.catalogFolderScr.Update(msg)
	if m.catalogFolderScr.Done() {
		m.catalogFolder = m.catalogFolderScr.Folder()
		m.catalogFolderScr.Reset()
		m.screen = ScreenMaxConcurrentRuns
	} else if m.catalogFolderScr.Back() {
		m.catalogFolderScr.Reset()
		m.screen = ScreenReportPath
	}
	return m, cmd
}

// updateMaxConcurrentRunsScreen handles the final settings screen. When the
// user confirms (Done), it captures the value, updates the model's setting field,
// and calls startSelectedSuite() to begin the run. All prior screens' values
// were captured into model fields as each screen was confirmed.
func (m Model) updateMaxConcurrentRunsScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.maxConcurrentRunsScr == nil {
		return m, nil
	}
	cmd := m.maxConcurrentRunsScr.Update(msg)
	if m.maxConcurrentRunsScr.Done() {
		// Only commit a new override when the user actually typed something.
		// Pressing Enter without typing preserves the existing m.maxConcurrentRuns
		// value (nil when no override is set) so the suite applies its own default.
		if m.maxConcurrentRunsScr.WasEdited() {
			v := m.maxConcurrentRunsScr.Value()
			if v > 0 {
				m.maxConcurrentRuns = &v
			} else {
				m.maxConcurrentRuns = nil
			}
		}
		return m.startSelectedSuites()
	} else if m.maxConcurrentRunsScr.Back() {
		m.maxConcurrentRunsScr.Reset()
		m.screen = ScreenCatalogFolder
	}
	return m, cmd
}

// SettingsEntries returns the current state of all five per-run settings as a
// slice of SettingsEntry instances. The slice order is fixed: retention,
// repetitions, report path, catalog folder, max-concurrent-runs.
// SettingsEntries is callable from any screen.
func (m Model) SettingsEntries() []SettingsEntry {
	retDisplay := string(m.retention)

	// Build the repetitions display with a provenance marker so the user can
	// tell whether the shown value comes from the suite file or is their own
	// override. Call the resolver live for the current cursor position so the
	// display updates as the suite cursor moves — even before Enter is pressed.
	// When the resolver is nil, returns an error, or the suite declares no
	// default, the generic "suite default" label degrades gracefully.
	suiteDefaults := resolveForSuite(m.opts, m.suiteCursor)
	repDisplay := "suite default"
	if m.repetitions != nil {
		repDisplay = fmt.Sprintf("%d (override)", *m.repetitions)
	} else if suiteDefaults.Repetitions != nil {
		repDisplay = fmt.Sprintf("%d (suite default)", *suiteDefaults.Repetitions)
	}

	// Build the max-concurrent-runs display. When no override is set, show the
	// effective default so the user knows what bound will be applied without
	// having to look it up.
	maxRunsDisplay := fmt.Sprintf("%d (default)", suite.DefaultMaxConcurrentRuns)
	if m.maxConcurrentRuns != nil {
		maxRunsDisplay = fmt.Sprintf("%d", *m.maxConcurrentRuns)
	}

	return []SettingsEntry{
		{
			Kind:     SettingRetention,
			Label:    "Retain sandbox",
			Display:  retDisplay,
			EditMode: EditCycle,
		},
		{
			Kind:     SettingRepetitions,
			Label:    "Repetitions",
			Display:  repDisplay,
			EditMode: EditNumeric,
		},
		{
			Kind:     SettingReportPath,
			Label:    "Report path",
			Display:  m.reportPath,
			EditMode: EditInline,
		},
		{
			Kind:     SettingCatalog,
			Label:    "Catalog folder",
			Display:  m.catalogFolder,
			EditMode: EditInline,
		},
		{
			Kind:     SettingMaxConcurrentRuns,
			Label:    "Max concurrent runs",
			Display:  maxRunsDisplay,
			EditMode: EditNumeric,
		},
	}
}

// startSelectedSuites begins executing the suites confirmed by the multiselect
// widget. It initialises the suite index and delegates to startSuiteAt for the
// first suite. When selectedSuites is empty (which should not occur in normal
// operation), it falls back to the cursor item as a safety measure.
func (m Model) startSelectedSuites() (tea.Model, tea.Cmd) {
	if len(m.selectedSuites) == 0 {
		if len(m.opts.Suites) == 0 || m.suiteCursor >= len(m.opts.Suites) {
			m.statusMsg = "no suite available to select"
			m.statusError = true
			return m, nil
		}
		m.selectedSuites = []string{m.opts.Suites[m.suiteCursor]}
	}
	m.selectedSuiteIdx = 0
	return m.startSuiteAt(0)
}

// startSuiteAt pre-flights the suite at selectedSuites[idx] and, when it
// validates, moves to the progress screen and starts the suite in the
// background. The suite runs through a context this Model owns, so a later
// quit can cancel it.
func (m Model) startSuiteAt(idx int) (tea.Model, tea.Cmd) {
	if idx >= len(m.selectedSuites) {
		m.statusMsg = "no suite available to select"
		m.statusError = true
		return m, nil
	}

	suitePath := m.selectedSuites[idx]

	// Resolve the max-concurrent-runs bound from the model. A nil override
	// means the suite should apply its documented default; zero carries that
	// signal through to the runner so the suite — not the composition root —
	// resolves the default, keeping that logic in one place.
	bound := 0
	if v := m.MaxConcurrentRuns(); v != nil {
		bound = *v
	}

	// Resolve the runner and per-run preflight together, so validation and
	// execution agree on the harness by construction. When NewSuiteRunner is
	// nil (tests that supply a fake runner directly), fall back to Suite and
	// the top-level Preflight.
	var runner SuiteRunner
	var activePreflight PreflightFunc

	if m.opts.NewSuiteRunner != nil {
		r, pf, err := m.opts.NewSuiteRunner(bound, m.selectedHarness)
		if err != nil {
			m.statusMsg = fmt.Sprintf("cannot use harness %q: %v", m.selectedHarness, err)
			m.statusError = true
			return m, nil
		}
		runner = r
		activePreflight = pf
	} else {
		runner = m.opts.Suite
		activePreflight = m.opts.Preflight
	}

	var plan preflight.Plan
	if activePreflight != nil {
		// Build the catalog-folder override: only set it when the user
		// changed the value from the initial Options.CatalogFolder default,
		// so a nil pointer means "use the process-wide default".
		var catOverride *string
		if m.catalogFolder != m.opts.CatalogFolder {
			v := m.catalogFolder
			catOverride = &v
		}

		resolved, rpt := activePreflight(preflight.Input{
			SuitePath:     suitePath,
			HarnessID:     m.selectedHarness,
			CatalogFolder: m.opts.CatalogFolder,
			Overrides: preflight.Overrides{
				SubjectModel:  m.selectedSubjectModel,
				StubModel:     m.selectedStubModel,
				Repetitions:   m.repetitions,
				CatalogFolder: catOverride,
			},
		})
		if rpt.HasErrors() {
			m = m.withFailureDetail("Pre-flight failed: "+suitePath, authoring.RenderReport(rpt))
			m.statusMsg = "pre-flight failed for " + suitePath
			m.statusError = true
			// Return the user to suite-select so they can choose a different
			// suite or retry after fixing the problem. This matches the original
			// behavior where the suite never left suite-select before the
			// settings flow was introduced.
			m.screen = ScreenSuiteSelect
			return m, nil
		}
		if len(rpt.Diagnostics) > 0 {
			// Warning-only report: surface the warnings but do not block the run.
			m = m.withFailureDetail("Pre-flight warnings: "+suitePath, authoring.RenderReport(rpt))
			m.statusMsg = fmt.Sprintf("pre-flight passed with %d warning(s)", len(rpt.Diagnostics))
			m.statusError = false
		} else {
			m = m.clearFailureDetail()
		}
		plan = resolved
	} else {
		m = m.clearFailureDetail()
	}

	base := m.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	m.ctx = ctx
	m.cancel = cancel
	m.running = true
	m.screen = ScreenProgress
	m.statusMsg = ""
	m.statusError = false
	sink := m.sinkBox.get()
	// The live toggle value, not Options.Retention: this is what makes the
	// suite-select screen's affordance actually reach the run it starts,
	// rather than only the on-screen label and Model.Retention().
	retention := m.retention

	// The suite starts now, on its own goroutine, rather than when the
	// returned tea.Cmd is eventually invoked: quitting must be able to
	// cancel a suite that is already under way (AC16.7), and starting the
	// goroutine here — synchronously within Update — is what makes that
	// true regardless of how promptly the runtime gets around to running
	// the returned Cmd. Update waits for the goroutine to have actually
	// begun (not for the run to progress, just for the runtime to have
	// scheduled it) before returning, so a quit that follows immediately
	// cancels a context the goroutine is already holding rather than racing
	// its own startup.
	started := make(chan struct{})
	done := make(chan SuiteFinishedMsg, 1)
	go func() {
		close(started)
		if runner == nil {
			done <- SuiteFinishedMsg{}
			return
		}
		result, err := runner.Run(ctx, plan, sink, retention)
		done <- SuiteFinishedMsg{Result: result, Err: err}
	}()
	<-started

	return m, func() tea.Msg {
		return <-done
	}
}

// handleSuiteFinished carries the suite's terminal report.Result into the
// Model once Suite.Run returns. For the last (or only) suite in the queue it
// moves to the results screen. For all earlier suites it starts the next
// suite in the queue, staying on the progress screen.
//
// When a report path is in force and the run succeeded, the JSON report is
// written through opts.WriteFile. A write failure is surfaced via statusMsg
// so the user can see why the report file is missing; it never masks the
// run's own result. A write failure also stops subsequent suites.
func (m Model) handleSuiteFinished(msg SuiteFinishedMsg) (tea.Model, tea.Cmd) {
	m.running = false
	m.resultErr = msg.Err
	if msg.Err == nil {
		r := msg.Result
		m.result = &r
		// Write the JSON report file if a path is configured.
		if m.reportPath != "" {
			if err := m.writeReportFile(msg.Result); err != nil {
				writeErrText := fmt.Sprintf("cannot write report to %q: %v", m.reportPath, err)
				writeBody := authoring.RenderReport(authoring.ReportWriteFailureReport(m.reportPath, err))
				m = m.withFailureDetail("Cannot write report to "+m.reportPath, writeBody)
				m.statusMsg = writeErrText
				m.statusError = true
				m.screen = ScreenResults
				m.resultsCursor = 0
				return m, nil
			}
		}
		// If there are more suites in the queue, start the next one. Each
		// suite runs with its own report path (computed fresh from ReportPathFor)
		// and a clean progress state so the progress screen reflects only the
		// current suite's activity.
		if m.selectedSuiteIdx < len(m.selectedSuites)-1 {
			m.selectedSuiteIdx++
			nextSuite := m.selectedSuites[m.selectedSuiteIdx]
			if m.opts.ReportPathFor != nil {
				m.reportPath = m.opts.ReportPathFor(nextSuite)
			}
			// Reset per-suite progress state for the incoming suite.
			m.progress = runprogress.Model{}
			m.finished = nil
			m.result = nil
			return m.startSuiteAt(m.selectedSuiteIdx)
		}
		// Last (or only) suite completed successfully.
		m = m.clearFailureDetail()
	} else {
		// An error in any suite stops the queue; subsequent suites do not start.
		m = m.withFailureDetail("Suite run failed", authoring.RenderReport(authoring.RunFailureReport("", msg.Err)))
		m.statusMsg = msg.Err.Error()
		m.statusError = true
	}
	m.screen = ScreenResults
	m.resultsCursor = 0
	return m, nil
}

// writeReportFile renders result to JSON and writes the bytes through
// opts.WriteFile to m.reportPath. A nil WriteFile is reported as an error.
func (m Model) writeReportFile(result report.Result) error {
	if m.opts.WriteFile == nil {
		return authoring.ErrReportWriterUnconfigured
	}
	var buf bytes.Buffer
	if err := report.RenderJSON(&buf, result); err != nil {
		return err
	}
	return m.opts.WriteFile(m.reportPath, buf.Bytes())
}

// resultTests returns the tests the results and detail screens list. The
// terminal report.Result (once the suite has finished with no error) is
// preferred: it carries the per-run assertions, reasons and conditions the
// detail screen needs. Before that arrives — or when it carries no tests,
// as a caller supplying only a scripted event stream and no explicit result
// does — the folded progress stream (Fold's Finished()) is grouped into the
// same shape, one synthesized TestReport per finished run, so navigation
// still works from live-folded state alone.
func (m Model) resultTests() []report.TestReport {
	if m.result != nil && len(m.result.Tests) > 0 {
		return m.result.Tests
	}
	if len(m.finished) == 0 {
		return nil
	}
	tests := make([]report.TestReport, 0, len(m.finished))
	for _, run := range m.finished {
		tests = append(tests, report.TestReport{
			TestName: run.Key.TestName,
			Runs:     []report.RunReport{run},
		})
	}
	return tests
}

// updateResults handles cursor movement and drilling into a finished test's
// detail.
func (m Model) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tests := m.resultTests()
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
		if m.resultsCursor < len(tests)-1 {
			m.resultsCursor++
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
		if m.resultsCursor > 0 {
			m.resultsCursor--
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		// Drilling in is a navigation transition, independent of whether
		// data has arrived yet; the detail screen itself renders a
		// placeholder when there is nothing to show. Reset the scroll
		// position so the detail view always starts at the top on entry.
		m.screen = ScreenDetail
		m.detailScrollOffset = 0
		return m, nil

	case msg.Type == tea.KeyPgDown:
		if m.showFailureDetail {
			m.detailPane.ScrollDown()
		}
		return m, nil

	case msg.Type == tea.KeyPgUp:
		if m.showFailureDetail {
			m.detailPane.ScrollUp()
		}
		return m, nil
	}
	return m, nil
}

// updateDetail handles key input on the test-detail screen. Back returns to
// the results screen; PgDown and PgUp scroll the wrapped diagnostic content.
func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back):
		m.screen = ScreenResults
	case msg.Type == tea.KeyPgDown:
		m.detailScrollOffset++
	case msg.Type == tea.KeyPgUp:
		if m.detailScrollOffset > 0 {
			m.detailScrollOffset--
		}
	}
	return m, nil
}

// resizeActiveSettingScreen forwards a terminal width change to whichever
// setting screen is currently active, so layout adapts without losing edit state.
func (m Model) resizeActiveSettingScreen(width int) {
	switch m.screen {
	case ScreenStoreInput:
		if m.processReports.storeInput != nil {
			m.processReports.storeInput.Resize(width)
		}
	case ScreenSummaryInput:
		if m.processReports.summaryInput != nil {
			m.processReports.summaryInput.Resize(width)
		}
	case ScreenRetention:
		if m.retentionScr != nil {
			m.retentionScr.Resize(width)
		}
	case ScreenRepetitions:
		if m.repetitionsScr != nil {
			m.repetitionsScr.Resize(width)
		}
	case ScreenReportPath:
		if m.reportPathScr != nil {
			m.reportPathScr.Resize(width)
		}
	case ScreenCatalogFolder:
		if m.catalogFolderScr != nil {
			m.catalogFolderScr.Resize(width)
		}
	case ScreenMaxConcurrentRuns:
		if m.maxConcurrentRunsScr != nil {
			m.maxConcurrentRunsScr.Resize(width)
		}
	}
}

// View renders the current screen from the folded Model, using the shared
// theme, key set and scaffold. It degrades legibly at a narrow width rather
// than wrapping into noise.
func (m Model) View() string {
	switch m.screen {
	case ScreenModeSelect:
		return m.viewModeSelect()
	case ScreenProcessSelect:
		return m.viewProcessSelect()
	case ScreenStoreInput:
		return m.viewStoreInput()
	case ScreenStoreResult:
		return m.viewStoreResult()
	case ScreenSummaryInput:
		return m.viewSummaryInput()
	case ScreenSummaryResult:
		return m.viewSummaryResult()
	case ScreenHarnessSelect:
		return m.viewHarnessSelect()
	case ScreenModelSelect:
		return m.viewModelSelect()
	case ScreenSuiteSelect:
		return m.viewSuiteSelect()
	case ScreenRetention:
		if m.retentionScr != nil {
			return m.retentionScr.View()
		}
		return ""
	case ScreenRepetitions:
		if m.repetitionsScr != nil {
			return m.repetitionsScr.View()
		}
		return ""
	case ScreenReportPath:
		if m.reportPathScr != nil {
			return m.reportPathScr.View()
		}
		return ""
	case ScreenCatalogFolder:
		if m.catalogFolderScr != nil {
			return m.catalogFolderScr.View()
		}
		return ""
	case ScreenMaxConcurrentRuns:
		if m.maxConcurrentRunsScr != nil {
			return m.maxConcurrentRunsScr.View()
		}
		return ""
	case ScreenProgress:
		return m.viewProgress()
	case ScreenResults:
		return m.viewResults()
	case ScreenDetail:
		return m.viewDetail()
	}
	return ""
}

// Fold applies one progress event to the Model and returns the new Model.
// It is pure: folding rules are testable against a scripted sequence with
// no terminal and no program loop, and the cross-frontend equivalence test
// drives this and the CLI's FormatEvent from the same sequence.
func (m Model) Fold(ev domain.ProgressEvent) Model {
	// Delegate multi-run progress tracking to the shared pure core.
	// This is what makes CLI/TUI equivalence for the multi-run display
	// structural: both frontends import the same Model.Fold and call it here.
	m.progress = m.progress.Fold(ev)

	switch ev.Kind {
	case domain.ProgressSuiteStarted:
		m.running = true

	case domain.ProgressTestFinished:
		// Build the RunReport key from ev.Run when it is populated (the normal
		// case), falling back to ev.TestID / ev.Repetition for legacy event
		// shapes that do not carry a Run field.
		key := ev.Run
		if key.TestName == "" {
			key = domain.RunKey{TestName: ev.TestID, RunNumber: ev.Repetition}
		}
		m.finished = append(m.finished, report.RunReport{
			Key:      key,
			Verdict:  ev.Verdict,
			Duration: ev.Duration,
			Cost:     ev.Cost,
		})

	case domain.ProgressSuiteFinished:
		m.running = false
	}

	return m
}

// Screen reports the currently active screen.
func (m Model) Screen() Screen {
	return m.screen
}

// SelectedHarness reports the harness identity currently selected —
// Options.Harness until the harness-select screen changes it.
func (m Model) SelectedHarness() string {
	return m.selectedHarness
}

// Retention reports the sandbox retention policy currently selected —
// Options.Retention until the suite-select screen's toggle affordance
// changes it (Stage 7).
func (m Model) Retention() domain.RetentionPolicy {
	return m.retention
}

// Repetitions reports the override repetitions count configured on this
// Model, or nil when no override is set (the suite's authored default applies).
func (m Model) Repetitions() *int {
	return m.repetitions
}

// MaxConcurrentRuns reports the override max-concurrent-runs bound configured
// on this Model, or nil when no override is set (suite.DefaultMaxConcurrentRuns
// applies). Mirrors the nil-means-default pattern that Repetitions uses.
func (m Model) MaxConcurrentRuns() *int {
	return m.maxConcurrentRuns
}

// ReportPath reports the JSON report file path currently in force —
// Options.ReportPath until the suite-select screen's inline-edit affordance
// changes it. An empty string means "suppressed: no file will be written".
func (m Model) ReportPath() string {
	return m.reportPath
}

// CatalogFolder reports the catalog folder currently in force —
// Options.CatalogFolder until the suite-select screen's edit affordance
// changes it. An empty string means "deploy tool resolves its own catalogue".
func (m Model) CatalogFolder() string {
	return m.catalogFolder
}

// TotalTests reports the suite's declared total, learned from
// ProgressSuiteStarted.
func (m Model) TotalTests() int {
	return m.progress.TotalTests()
}

// Running returns every run currently in flight, ordered by StartedAt and
// then by RunID. A finished run leaves the set without clearing the others.
// Replaces the previous single-run scalars that could not represent more
// than one run at a time.
func (m Model) Running() []runprogress.RunProgress {
	return m.progress.Running()
}

// ObservedInvocations reports the invocations observed for one run, keyed
// by its RunKey. Approximate by design and never authoritative: invocation
// events are the only kind the async sink drops under saturation, so this
// can under-report. Lifecycle events are never dropped, which is why the
// tally is sound and this count is not.
func (m Model) ObservedInvocations(key domain.RunKey) int {
	for _, rp := range m.progress.Running() {
		if rp.Key.RunID == key.RunID {
			return rp.ObservedInvocations
		}
	}
	return 0
}

// Tally returns the display-only running/finished/remaining counts.
func (m Model) Tally() runprogress.Tally {
	return m.progress.Tally()
}

// Finished reports every finished repetition's outcome so far, in the order
// their ProgressTestFinished events were folded.
func (m Model) Finished() []report.RunReport {
	return m.finished
}

// Counts reports the verdict counts, learned from ProgressSuiteFinished.
// The figures are the report's own authoritative values carried through the
// stream; this accessor delegates to the shared runprogress core so both
// frontends read from the same source.
func (m Model) Counts() map[domain.Verdict]int {
	return m.progress.Counts()
}

// TotalCost reports the suite's total cost, learned from
// ProgressSuiteFinished.
func (m Model) TotalCost() domain.CostReport {
	return m.progress.TotalCost()
}

// Result reports the terminal result model once the suite has finished. ok
// is false while the suite is still running.
func (m Model) Result() (report.Result, bool) {
	if m.result == nil {
		return report.Result{}, false
	}
	return *m.result, true
}

// Run owns the terminal for the lifetime of the call: it presents the
// suite-select screen, runs the chosen suite in the background, and shows
// live progress folded from its event stream.
//
// The program's own Send method becomes the progress sink once the program
// exists, so events reach the message loop as they arrive (I16.2) rather
// than being discovered by polling the suite.
func Run(ctx context.Context, o Options) error {
	m := NewModel(o)
	m.ctx = ctx

	p := tea.NewProgram(m, tea.WithAltScreen())
	m.sinkBox.set(NewProgramSink(p.Send))

	_, err := p.Run()
	return err
}
