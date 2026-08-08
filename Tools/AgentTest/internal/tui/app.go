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
	"context"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// PreflightFunc resolves suite input into a validated plan. Same shape as
// the CLI's, so a scripted preflight can be handed to either frontend
// interchangeably in the equivalence test.
type PreflightFunc func(preflight.Input) (preflight.Plan, authoring.Report)

// SuiteRunner is the same interface the CLI drives the suite through. The
// TUI constructs no scheduling, no verdicts and no infrastructure of its
// own; it only calls Run and folds what Progress emits.
type SuiteRunner interface {
	Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error)
}

// Options is the pre-wired dependency set the composition root hands in.
type Options struct {
	Preflight PreflightFunc
	Suite     SuiteRunner
	// Suites are the discovered suite paths offered for selection on the
	// suite-select screen.
	Suites  []string
	Harness string
}

// Screen names one of the four screens this frontend presents.
type Screen string

const (
	ScreenSuiteSelect Screen = "suite_select"
	ScreenProgress    Screen = "progress"
	ScreenResults     Screen = "results"
	ScreenDetail      Screen = "detail"
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

	// Suite-select screen state.
	suiteCursor int
	running     bool

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
	resultsCursor int

	statusMsg   string
	statusError bool
}

// Compile-time assertion that Model satisfies tea.Model.
var _ tea.Model = Model{}

// NewModel constructs the initial Model on the suite-select screen. The
// suite has not started; Suites lists what a user may pick from.
func NewModel(o Options) Model {
	return Model{
		opts:    o,
		theme:   tuicommon.DefaultTheme(),
		screen:  ScreenSuiteSelect,
		width:   tuicommon.DefaultWidth,
		height:  tuicommon.DefaultHeight,
		ctx:     context.Background(),
		sinkBox: newSinkBox(),
	}
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

// updateSuiteSelect handles suite-cursor movement and starting the chosen
// suite.
func (m Model) updateSuiteSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	case tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Select):
		return m.startSelectedSuite()
	}
	return m, nil
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

	var plan preflight.Plan
	if m.opts.Preflight != nil {
		resolved, rpt := m.opts.Preflight(preflight.Input{
			SuitePath: suitePath,
			HarnessID: m.opts.Harness,
		})
		if rpt.HasErrors() {
			m.statusMsg = "pre-flight failed for " + suitePath
			m.statusError = true
			return m, nil
		}
		plan = resolved
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
		result, err := runner.Run(ctx, plan, sink)
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
func (m Model) handleSuiteFinished(msg SuiteFinishedMsg) Model {
	m.running = false
	m.resultErr = msg.Err
	if msg.Err == nil {
		r := msg.Result
		m.result = &r
	}
	if msg.Err != nil {
		m.statusMsg = msg.Err.Error()
		m.statusError = true
	}
	m.screen = ScreenResults
	m.resultsCursor = 0
	return m
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
		// placeholder when there is nothing to show.
		m.screen = ScreenDetail
		return m, nil
	}
	return m, nil
}

// updateDetail handles returning to the results screen.
func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if tuicommon.MatchesKey(msg, tuicommon.GlobalKeys.Back) {
		m.screen = ScreenResults
	}
	return m, nil
}

// View renders the current screen from the folded Model, using the shared
// theme, key set and scaffold. It degrades legibly at a narrow width rather
// than wrapping into noise.
func (m Model) View() string {
	switch m.screen {
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
