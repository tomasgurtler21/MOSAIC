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

// SuiteRunner is the interface the TUI drives the suite through. Unlike
// cli.SuiteRunner (which the CLI's factory rebuilds per invocation, already
// carrying its resolved retention), the TUI wires one long-lived Suite for
// the whole process, so retention cannot be baked in at construction: the
// suite-select screen's toggle can change it on every run. Run therefore
// takes the live retention policy as a call-time argument — the value
// startSelectedSuite passes is Model.retention at the moment the run starts,
// not whatever Options.Suite was built with.
type SuiteRunner interface {
	Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink, retention domain.RetentionPolicy) (report.Result, error)
}

// Options is the pre-wired dependency set the composition root hands in.
type Options struct {
	Preflight PreflightFunc
	Suite     SuiteRunner
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
}

// Screen names one of the screens this frontend presents.
type Screen string

const (
	// ScreenHarnessSelect offers the harness-selection affordance —
	// Stage 5's TUI equivalent of the CLI's --harness flag — following the
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
	suiteCursor int
	running     bool

	// retention starts as Options.Retention and is updated by the
	// suite-select screen's toggle affordance (Stage 7).
	retention domain.RetentionPolicy

	// repetitions is the user-configured override for the number of repetitions
	// per test. When nil, the suite's authored default applies. The suite-select
	// screen shows this value; it threads through preflight.Overrides.Repetitions
	// when a run starts, following the same pattern as selectedSubjectModel and
	// selectedStubModel.
	repetitions *int

	// catalogFolder is the catalog folder currently in force. Starts as
	// Options.CatalogFolder; the suite-select screen shows it and allows
	// editing before a run starts. When it differs from Options.CatalogFolder
	// at run-start, it flows as a per-run override through
	// preflight.Overrides.CatalogFolder.
	catalogFolder string

	// reportPath is the JSON report file path currently in force. Starts as
	// Options.ReportPath; the suite-select screen's inline-edit affordance may
	// change it before a run starts. The value at run-start is what WriteFile
	// receives. An empty value suppresses the write.
	reportPath string

	// editingReportPath is true while the suite-select screen's inline edit
	// mode is active. While true, list navigation keys are text input rather
	// than navigation.
	editingReportPath bool

	// reportPathDraft accumulates the typed value during inline edit mode.
	reportPathDraft string

	// Folded progress state (AC16.2, AC16.8): every field here is set only
	// by Fold, from an event or the terminal result model, and nothing in
	// this package aggregates a number of its own.
	totalTests          int
	runningTestID       string
	runningRepetition   int
	runningRepetitions  int
	observedInvocations int
	finished            []report.RunReport
	counts              map[domain.Verdict]int
	totalCost           domain.CostReport
	result              *report.Result
	resultErr           error

	// Results/detail screen state.
	resultsCursor      int
	detailScrollOffset int // scroll position on the test-detail screen; reset on every drill-in

	statusMsg   string
	statusError bool

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

// NewModel constructs the initial Model on the suite-select screen. The
// suite has not started; Suites lists what a user may pick from.
func NewModel(o Options) Model {
	m := Model{
		opts:            o,
		theme:           tuicommon.DefaultTheme(),
		screen:          ScreenSuiteSelect,
		width:           tuicommon.DefaultWidth,
		height:          tuicommon.DefaultHeight,
		ctx:             context.Background(),
		sinkBox:         newSinkBox(),
		selectedHarness: o.Harness,
		retention:       o.Retention,
		reportPath:      o.ReportPath,
		catalogFolder:   o.CatalogFolder,
	}
	if len(o.Harnesses) > 0 {
		m.screen = ScreenHarnessSelect
	}
	paneH, paneW := m.paneGeometry()
	m.detailPane = widgets.NewDetailPane(paneH, paneW, widgets.DefaultDetailPaneStyles())
	return m
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
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(normalizeEnter(msg))

	case ProgressMsg:
		m = m.Fold(msg.Event)
		return m, nil

	case SuiteFinishedMsg:
		return m.handleSuiteFinished(msg), nil
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
	case ScreenHarnessSelect:
		return m.updateHarnessSelect(msg)
	case ScreenModelSelect:
		return m.updateModelSelect(msg)
	case ScreenSuiteSelect:
		return m.updateSuiteSelect(msg)
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

// isEditKey reports whether msg is the report-path inline-edit activation key.
// 'e' is chosen so it does not conflict with any existing navigation binding.
func isEditKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && string(msg.Runes) == "e"
}

// updateSuiteSelect handles suite-cursor movement, the retention toggle,
// the report-path inline editor activation, and starting the chosen suite.
func (m Model) updateSuiteSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the report-path inline editor is active, route all keys to it so
	// navigation keys become text input rather than cursor movement.
	if m.editingReportPath {
		return m.updateReportPathEdit(msg)
	}

	switch {
	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Down):
		if m.suiteCursor < len(m.opts.Suites)-1 {
			m.suiteCursor++
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Up):
		if m.suiteCursor > 0 {
			m.suiteCursor--
		}
		return m, nil

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Space):
		m.retention = nextRetention(m.retention)
		return m, nil

	case isEditKey(msg):
		m.editingReportPath = true
		m.reportPathDraft = ""
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

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		return m.startSelectedSuite()
	}
	return m, nil
}

// updateReportPathEdit handles key input while the report-path inline editor
// is active. Enter commits the draft, Escape cancels and restores the
// previous value, Backspace removes the last character, and any rune
// appends to the draft. Other special keys (Down, Up, etc.) are consumed
// without effect so they cannot drive navigation behind the editor.
func (m Model) updateReportPathEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.reportPath = m.reportPathDraft
		m.editingReportPath = false
		m.reportPathDraft = ""
		return m, nil

	case tea.KeyEsc:
		// Cancel: reportPath is left unchanged; only the draft is discarded.
		m.editingReportPath = false
		m.reportPathDraft = ""
		return m, nil

	case tea.KeyBackspace:
		if len(m.reportPathDraft) > 0 {
			m.reportPathDraft = m.reportPathDraft[:len(m.reportPathDraft)-1]
		}
		return m, nil

	case tea.KeyRunes:
		m.reportPathDraft += string(msg.Runes)
		return m, nil
	}

	// Other special keys (Down, Up, Space, etc.) are consumed but do not
	// produce text and do not drive navigation.
	return m, nil
}

// nextRetention advances p one step around the cycle the suite-select
// screen's Space binding walks: RetainNever -> RetainOnFailure ->
// RetainAlways -> RetainNever, wrapping. Ascending retention, so repeated
// presses walk from least to most retained before wrapping. A zero
// RetentionPolicy (the unset value) is treated as RetainNever, so the first
// press from an unset Options.Retention still advances predictably.
func nextRetention(p domain.RetentionPolicy) domain.RetentionPolicy {
	switch p {
	case domain.RetainOnFailure:
		return domain.RetainAlways
	case domain.RetainAlways:
		return domain.RetainNever
	default:
		return domain.RetainOnFailure
	}
}

// startSelectedSuite pre-flights the cursor's suite and, when it validates,
// moves to the progress screen and starts the suite in the background. The
// suite runs through a context this Model owns, so a later quit can cancel
// it (AC16.7).
func (m Model) startSelectedSuite() (tea.Model, tea.Cmd) {
	if len(m.opts.Suites) == 0 || m.suiteCursor >= len(m.opts.Suites) {
		m.statusMsg = "no suite available to select"
		m.statusError = true
		return m, nil
	}

	suitePath := m.opts.Suites[m.suiteCursor]

	// Recompute the report path from the selected suite name when the composition
	// root has supplied a path function and the user has not manually edited the
	// path (i.e. it still matches the initial Options value). This gives each run
	// a suite-named, timestamped report filename instead of the placeholder that
	// was shown on the suite-select screen before a suite was chosen.
	if m.opts.ReportPathFor != nil && m.reportPath == m.opts.ReportPath {
		m.reportPath = m.opts.ReportPathFor(suitePath)
	}

	var plan preflight.Plan
	if m.opts.Preflight != nil {
		// Build the catalog-folder override: only set it when the user
		// changed the value from the initial Options.CatalogFolder default,
		// so a nil pointer means "use the process-wide default".
		var catOverride *string
		if m.catalogFolder != m.opts.CatalogFolder {
			v := m.catalogFolder
			catOverride = &v
		}

		resolved, rpt := m.opts.Preflight(preflight.Input{
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

	runner := m.opts.Suite
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
// Model once Suite.Run returns, and moves to the results screen. This is
// where the richer per-test detail (assertions, reasons, conditions) the
// detail screen needs arrives; the live folded state Fold produces from the
// event stream is what the progress screen showed while the suite ran.
//
// When a report path is in force and the run succeeded, the JSON report is
// written through opts.WriteFile. A write failure is surfaced via statusMsg
// so the user can see why the report file is missing; it never masks the
// run's own result.
func (m Model) handleSuiteFinished(msg SuiteFinishedMsg) Model {
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
				return m
			}
		}
		// Success: clear any failure detail from a previous operation.
		m = m.clearFailureDetail()
	} else {
		m = m.withFailureDetail("Suite run failed", authoring.RenderReport(authoring.RunFailureReport("", msg.Err)))
		m.statusMsg = msg.Err.Error()
		m.statusError = true
	}
	m.screen = ScreenResults
	m.resultsCursor = 0
	return m
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
			TestID: run.Key.TestID,
			Runs:   []report.RunReport{run},
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

// View renders the current screen from the folded Model, using the shared
// theme, key set and scaffold. It degrades legibly at a narrow width rather
// than wrapping into noise.
func (m Model) View() string {
	switch m.screen {
	case ScreenHarnessSelect:
		return m.viewHarnessSelect()
	case ScreenModelSelect:
		return m.viewModelSelect()
	case ScreenSuiteSelect:
		return m.viewSuiteSelect()
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
	switch ev.Kind {
	case domain.ProgressSuiteStarted:
		m.totalTests = ev.TotalTests
		m.running = true

	case domain.ProgressTestStarted:
		m.runningTestID = ev.TestID
		m.runningRepetition = ev.Repetition
		m.runningRepetitions = ev.Repetitions
		m.observedInvocations = 0

	case domain.ProgressInvocation:
		m.observedInvocations++

	case domain.ProgressTestFinished:
		m.finished = append(m.finished, report.RunReport{
			Key: domain.RunKey{
				TestID:    ev.TestID,
				RunNumber: ev.Repetition,
			},
			Verdict:  ev.Verdict,
			Duration: ev.Duration,
			Cost:     ev.Cost,
		})
		m.runningTestID = ""
		m.runningRepetition = 0
		m.runningRepetitions = 0
		m.observedInvocations = 0

	case domain.ProgressSuiteFinished:
		m.counts = ev.Counts
		m.totalCost = ev.TotalCost
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

// EditingReportPath reports whether the suite-select screen's inline report-
// path editor is currently active.
func (m Model) EditingReportPath() bool {
	return m.editingReportPath
}

// TotalTests reports the suite's declared total, learned from
// ProgressSuiteStarted.
func (m Model) TotalTests() int {
	return m.totalTests
}

// Running reports the test currently executing and which repetition of how
// many, learned from ProgressTestStarted / ProgressTestFinished. ok is false
// when no test is currently running.
func (m Model) Running() (testID string, repetition, repetitions int, ok bool) {
	return m.runningTestID, m.runningRepetition, m.runningRepetitions, m.runningTestID != ""
}

// ObservedInvocations reports the count of ProgressInvocation events
// observed for the currently running test.
func (m Model) ObservedInvocations() int {
	return m.observedInvocations
}

// Finished reports every finished repetition's outcome so far, in the order
// their ProgressTestFinished events were folded.
func (m Model) Finished() []report.RunReport {
	return m.finished
}

// Counts reports the running verdict tally, learned from
// ProgressSuiteFinished.
func (m Model) Counts() map[domain.Verdict]int {
	return m.counts
}

// TotalCost reports the suite's total cost, learned from
// ProgressSuiteFinished.
func (m Model) TotalCost() domain.CostReport {
	return m.totalCost
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
