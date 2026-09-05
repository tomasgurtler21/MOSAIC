package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/tui/screens"
)

// Options is the fully-wired dependency set from the composition root.
type Options struct {
	Service     *app.Service
	InitialPath string // from --path, may be empty
	Version     string
	// Interaction and Questions, when both non-nil, are used instead of creating
	// a new pair internally. The composition root supplies them so the service and
	// the TUI model share the same Interaction channel.
	Interaction *Interaction
	Questions   <-chan Pending
}

// screenName identifies the currently active screen.
type screenName string

const (
	screenLoading   screenName = "loading"
	screenSource    screenName = "source"
	screenRuns      screenName = "runs"
	screenRunDetail screenName = "run-detail"
	screenPricing   screenName = "pricing"
	screenNoData    screenName = "no-data"
	screenError     screenName = "error"
	screenQuestion  screenName = "question"
)

// analysisDoneMsg is sent when app.Service.Analyze completes.
type analysisDoneMsg struct {
	result app.Result
	err    error
}

// pricingEditDoneMsg is sent when the app-layer pricing editor completes.
// It mirrors analysisDoneMsg exactly.
type pricingEditDoneMsg struct {
	result app.PricingEditResult
	err    error
}

// Model is the top-level bubbletea model: screen registry, window-size
// handling, key routing and screen transitions.
type Model struct {
	opts     Options
	theme    tuicommon.Theme
	interact *Interaction
	questions <-chan Pending
	ctx    context.Context    // cancelled when the model tears down
	cancel context.CancelFunc // call to stop in-flight analysis goroutines

	// analysisPath is the explicit path for the next analysis run.
	// An empty string means "no explicit path" (the service will prompt).
	analysisPath string

	// sourceFromRuns records that the source screen was opened from the Runs
	// list (via 's'), not in response to a pending Interaction question.
	// updateSource branches on this flag to take the correct action on Done/Back.
	sourceFromRuns bool

	screen  screenName
	width   int
	height  int

	// Analysis state.
	result  *app.Result
	report  domain.Report
	session *app.Session

	// Screens.
	sourceScreen    *screens.SourceScreen
	runsScreen      *screens.RunsScreen
	runDetailScreen *screens.RunDetailScreen
	pricingScreen   *screens.PricingScreen
	noDataScreen    *screens.NoDataScreen

	// Question overlays (one active at a time).
	pendingQuestion *Pending
	selectOverlay   *overlaySelectOne
	textOverlay     *overlayText

	// Status shown on loading/error screen.
	errMsg string

	// Process exit code (returned by Run).
	exitCode int
}

// New constructs the top-level model. The model owns the Interaction and its
// question channel; both are wired before the bubbletea program starts.
//
// When opts.Interaction and opts.Questions are both non-nil (supplied by the
// composition root), those values are used directly so the service and the model
// share the same channel. Otherwise a new pair is created internally.
func New(opts Options) Model {
	ctx, cancel := context.WithCancel(context.Background())
	theme := tuicommon.DefaultTheme()

	var interact *Interaction
	var questions <-chan Pending
	if opts.Interaction != nil && opts.Questions != nil {
		interact = opts.Interaction
		questions = opts.Questions
	} else {
		interact, questions = NewInteraction()
	}

	return Model{
		opts:         opts,
		theme:        theme,
		interact:     interact,
		questions:    questions,
		ctx:          ctx,
		cancel:       cancel,
		analysisPath: opts.InitialPath,
		screen:       screenLoading,
		width:        defaultWidth,
		height:       defaultHeight,
	}
}

// Run starts the bubbletea program and returns the process exit code, mapped
// to the same stable codes the CLI uses.
func Run(opts Options) int {
	m := New(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return 1
	}
	if fm, ok := finalModel.(Model); ok {
		return fm.exitCode
	}
	return 0
}

// Init is called once when the bubbletea program starts.
func (m Model) Init() tea.Cmd {
	// Start analysis and a single goroutine that waits for Interaction questions.
	// Only one waitForPending goroutine runs at a time: handlePending restarts it
	// each time a question is consumed, so the idle goroutine handles questions
	// from any future analysis run without a duplicate goroutine race.
	return tea.Batch(
		m.buildAnalysisCmd(),
		waitForPending(m.questions, m.interact.done),
	)
}

// buildAnalysisCmd returns a Cmd that runs Service.Analyze in a goroutine.
// The analysis runs under m.ctx so it is cancelled when the model tears down.
func (m Model) buildAnalysisCmd() tea.Cmd {
	svc := m.opts.Service
	path := m.analysisPath
	ctx := m.ctx
	return func() tea.Msg {
		result, err := svc.Analyze(ctx, app.Request{
			ExplicitPath:       path,
			ResolvePricingGaps: true,
		})
		return analysisDoneMsg{result: result, err: err}
	}
}

// waitForPending returns a Cmd that blocks until a Pending question arrives or
// the Interaction signals teardown. When done fires, it returns nil (no-op).
func waitForPending(questions <-chan Pending, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-questions:
			if !ok {
				return nil
			}
			return p
		case <-done:
			return nil
		}
	}
}

// Update processes an incoming message and drives the screen state machine.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global ctrl+c: cancel in-flight analysis and quit.
	if key, ok := msg.(tea.KeyMsg); ok && matchesKey(key, globalKeys.Cancel) {
		m.cancel()
		m.interact.Close()
		m.replyToPending(answerMsg{
			choiceAns: interaction.ChoiceAnswer{Status: interaction.Cancelled},
			textAns:   interaction.TextAnswer{Status: interaction.Cancelled},
		})
		return m, tea.Quit
	}

	// Window resize.
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
		return m, nil
	}

	// Analysis complete.
	if done, ok := msg.(analysisDoneMsg); ok {
		return m.handleAnalysisDone(done)
	}

	// Pricing editor complete.
	if done, ok := msg.(pricingEditDoneMsg); ok {
		return m.handlePricingEditDone(done)
	}

	// Pending question from the Interaction port.
	if p, ok := msg.(Pending); ok {
		return m.handlePending(p)
	}

	// Route to current screen.
	switch m.screen {
	case screenLoading:
		return m, nil
	case screenSource:
		return m.updateSource(msg)
	case screenRuns:
		return m.updateRuns(msg)
	case screenRunDetail:
		return m.updateRunDetail(msg)
	case screenPricing:
		return m.updatePricing(msg)
	case screenNoData:
		return m.updateNoData(msg)
	case screenQuestion:
		return m.updateQuestion(msg)
	case screenError:
		return m.updateError(msg)
	}
	return m, nil
}

// handleAnalysisDone transitions to the appropriate screen when analysis finishes.
func (m Model) handleAnalysisDone(done analysisDoneMsg) (tea.Model, tea.Cmd) {
	if done.err != nil {
		m.errMsg = done.err.Error()
		m.screen = screenError
		m.exitCode = 1
		return m, nil
	}

	m.result = &done.result
	m.session = done.result.Session

	switch done.result.Outcome {
	case app.OutcomeReport:
		m.report = done.result.Report
		style := stylesFromTheme(m.theme)
		m.runsScreen = screens.NewRunsScreen(m.report, m.width, m.height, style)
		m.screen = screenRuns

	case app.OutcomeNoData:
		style := stylesFromTheme(m.theme)
		m.noDataScreen = screens.NewNoDataScreen(done.result.Source, m.width, m.height, style)
		m.screen = screenNoData
		m.exitCode = 2

	case app.OutcomeSourceNotFound:
		m.errMsg = "No log source could be resolved. Use --path to specify a logs directory."
		m.screen = screenError
		m.exitCode = 3

	case app.OutcomeSourceUnusable:
		m.errMsg = "The specified path is not a usable logs tree: " + done.result.Source.Reason
		m.screen = screenError
		m.exitCode = 4
	}
	return m, nil
}

// handlePending routes an Interaction question to the appropriate overlay or screen.
// Every branch that handles a question MUST restart waitForPending so the next
// question from any subsequent analysis run is delivered.
func (m Model) handlePending(p Pending) (tea.Model, tea.Cmd) {
	m.pendingQuestion = &p

	switch p.kind {
	case pendingAskText:
		if p.id == app.QuestionLogSourcePath {
			// Surface the source screen for the log-source path question.
			style := stylesFromTheme(m.theme)
			m.sourceScreen = screens.NewSourceScreen(m.width, m.height, style)
			m.sourceScreen.SetVersion(m.opts.Version)
			m.screen = screenSource
			return m, tea.Batch(
				m.sourceScreen.Init(),
				waitForPending(m.questions, m.interact.done),
			)
		}
		// Generic text overlay for all other text questions (pricing rates, threshold).
		m.textOverlay = newOverlayText(p.textQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, tea.Batch(
			m.textOverlay.init(),
			waitForPending(m.questions, m.interact.done),
		)

	case pendingSelectOne:
		if p.id == app.QuestionPricingAction && m.result != nil {
			// Show pricing context screen first, then overlay on top.
			// TODO I5.6: verify background renders sensibly with reworked screen.
			style := stylesFromTheme(m.theme)
			var modelStates []app.ModelPricingStatus
			if m.session != nil {
				modelStates = app.ModelPricingStates(m.report, m.session.Table())
			}
			m.pricingScreen = screens.NewPricingScreen(
				modelStates,
				m.opts.Service.PricingPath(),
				m.width, m.height, style,
			)
			_ = m.pricingScreen // set for View
		}
		m.selectOverlay = newOverlaySelectOne(p.choiceQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, waitForPending(m.questions, m.interact.done)

	default:
		// Unsupported question kind — skip and continue.
		p.reply <- answerMsg{
			choiceAns: interaction.ChoiceAnswer{Status: interaction.SkippedOne},
		}
		m.pendingQuestion = nil
		return m, waitForPending(m.questions, m.interact.done)
	}
}

// replyToPending sends an answer to the pending question and clears it.
func (m *Model) replyToPending(ans answerMsg) {
	if m.pendingQuestion != nil {
		m.pendingQuestion.reply <- ans
		m.pendingQuestion = nil
	}
}

// ---------------------------------------------------------------------------
// Screen update handlers
// ---------------------------------------------------------------------------

func (m Model) updateSource(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.sourceScreen == nil {
		return m, nil
	}
	updated, cmd := m.sourceScreen.Update(msg)
	m.sourceScreen = updated.(*screens.SourceScreen)
	if m.sourceScreen.Back() {
		m.sourceScreen.Reset()
		if m.sourceFromRuns {
			// Runs-initiated cancel: return to the Runs list, no analysis restart.
			m.sourceFromRuns = false
			m.screen = screenRuns
			return m, nil
		}
		// Interaction-initiated cancel: reply Cancelled to the pending question.
		m.replyToPending(answerMsg{
			textAns: interaction.TextAnswer{Status: interaction.Cancelled},
		})
		m.screen = screenLoading
		return m, nil
	}
	if m.sourceScreen.Done() {
		path := m.sourceScreen.Path()
		m.sourceScreen.Reset()
		if m.sourceFromRuns {
			// Runs-initiated confirm: set the explicit path and restart analysis.
			m.sourceFromRuns = false
			m.analysisPath = path
			m.screen = screenLoading
			return m, m.buildAnalysisCmd()
		}
		// Interaction-initiated confirm: reply Answered to the pending question.
		m.replyToPending(answerMsg{
			textAns: interaction.TextAnswer{Status: interaction.Answered, Text: path},
		})
		m.screen = screenLoading
		return m, nil
	}
	return m, cmd
}

func (m Model) updateRuns(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.runsScreen == nil {
		return m, nil
	}
	// Intercept 's' to open the source-path entry screen from the Runs list.
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "s" {
		style := stylesFromTheme(m.theme)
		m.sourceScreen = screens.NewSourceScreen(m.width, m.height, style)
		m.sourceScreen.SetVersion(m.opts.Version)
		m.sourceFromRuns = true
		m.screen = screenSource
		return m, m.sourceScreen.Init()
	}

	// Intercept 'p' for pricing navigation.
	// Guard: require an active session; do nothing if analysis has not yet produced one.
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "p" {
		if m.result == nil || m.result.Session == nil {
			return m, nil
		}
		style := stylesFromTheme(m.theme)
		modelStates := app.ModelPricingStates(m.report, m.result.Session.Table())
		m.pricingScreen = screens.NewPricingScreen(
			modelStates,
			m.opts.Service.PricingPath(),
			m.width, m.height, style,
		)
		m.screen = screenPricing
		return m, nil
	}

	updated, cmd := m.runsScreen.Update(msg)
	m.runsScreen = updated.(*screens.RunsScreen)
	if m.runsScreen.Back() {
		m.runsScreen.Reset()
		m.interact.Close()
		return m, tea.Quit
	}
	if m.runsScreen.Done() {
		runID := m.runsScreen.SelectedRunID()
		m.runsScreen.Reset()
		if runID != "" {
			if run, ok := m.report.FindRun(runID); ok {
				style := stylesFromTheme(m.theme)
				m.runDetailScreen = screens.NewRunDetailScreen(run, m.width, m.height, style)
				m.screen = screenRunDetail
			}
		}
	}
	return m, cmd
}

func (m Model) updateRunDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.runDetailScreen == nil {
		return m, nil
	}
	updated, cmd := m.runDetailScreen.Update(msg)
	m.runDetailScreen = updated.(*screens.RunDetailScreen)
	if m.runDetailScreen.Back() {
		m.runDetailScreen.Reset()
		m.screen = screenRuns
		return m, nil
	}
	return m, cmd
}

func (m Model) updatePricing(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.pricingScreen == nil {
		m.screen = screenRuns
		return m, nil
	}
	updated, cmd := m.pricingScreen.Update(msg)
	m.pricingScreen = updated.(*screens.PricingScreen)
	if m.pricingScreen.Back() {
		m.pricingScreen.Reset()
		m.screen = screenRuns
		return m, nil
	}
	if m.pricingScreen.Done() {
		selected := m.pricingScreen.SelectedModel()
		m.pricingScreen.Reset()
		m.screen = screenLoading
		return m, m.buildPricingEditCmd(selected)
	}
	return m, cmd
}

// buildPricingEditCmd returns a Cmd that runs Service.EditModelPricing in a
// goroutine under m.ctx and returns a pricingEditDoneMsg.
//
// Binding rule: it returns ONLY that message. It must NOT start a second
// waitForPending goroutine — the single long-lived waiter delivers the
// editor's rate questions.
func (m Model) buildPricingEditCmd(model domain.ModelID) tea.Cmd {
	svc := m.opts.Service
	sess := m.session
	report := m.report
	ctx := m.ctx
	return func() tea.Msg {
		result, err := svc.EditModelPricing(ctx, sess, report, model)
		return pricingEditDoneMsg{result: result, err: err}
	}
}

// handlePricingEditDone applies the result of a completed pricing-edit flow.
// On PricingEditSaved, the repriced report is applied to the Runs screen (via
// SetReport so the cursor position survives) and a confirmation is surfaced on
// the pricing screen's status line. On other outcomes the report is unchanged.
func (m Model) handlePricingEditDone(done pricingEditDoneMsg) (tea.Model, tea.Cmd) {
	if done.err != nil {
		m.errMsg = done.err.Error()
		m.screen = screenError
		return m, nil
	}

	result := done.result
	m.report = result.Report

	if m.runsScreen != nil {
		m.runsScreen.SetReport(result.Report)
	}

	if result.Outcome == app.PricingEditSaved && m.pricingScreen != nil {
		m.pricingScreen.SetStatus("Price written to " + result.ConfigPath)
	}

	m.screen = screenRuns
	return m, nil
}

func (m Model) updateNoData(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.noDataScreen == nil {
		return m, nil
	}
	updated, cmd := m.noDataScreen.Update(msg)
	m.noDataScreen = updated.(*screens.NoDataScreen)
	if m.noDataScreen.Back() {
		m.noDataScreen.Reset()
		m.interact.Close()
		return m, tea.Quit
	}
	if m.noDataScreen.Done() {
		// User wants to choose a different source. Restart analysis with no
		// explicit path so the service will prompt via Interaction. The existing
		// idle waitForPending goroutine picks up the new question.
		m.noDataScreen.Reset()
		m.analysisPath = "" // clear explicit path so Interaction is consulted
		m.screen = screenLoading
		return m, m.buildAnalysisCmd()
	}
	return m, cmd
}

func (m Model) updateQuestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.selectOverlay != nil {
		done := m.selectOverlay.update(msg)
		if done {
			ans := m.selectOverlay.answer()
			m.replyToPending(answerMsg{choiceAns: ans})
			m.selectOverlay = nil
			m.screen = screenLoading
		}
		return m, nil
	}
	if m.textOverlay != nil {
		cmd := m.textOverlay.update(msg)
		if m.textOverlay.done || m.textOverlay.back {
			ans := m.textOverlay.answer()
			m.replyToPending(answerMsg{textAns: ans})
			m.textOverlay = nil
			m.screen = screenLoading
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) updateError(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View renders the current screen to a string.
func (m Model) View() string {
	switch m.screen {
	case screenLoading:
		return m.theme.Style(tuicommon.RoleMuted).Render("Analyzing logs…")

	case screenSource:
		if m.sourceScreen != nil {
			return m.sourceScreen.View(m.width, m.height)
		}

	case screenRuns:
		if m.runsScreen != nil {
			return m.runsScreen.View(m.width, m.height)
		}

	case screenRunDetail:
		if m.runDetailScreen != nil {
			return m.runDetailScreen.View(m.width, m.height)
		}

	case screenPricing:
		if m.pricingScreen != nil {
			return m.pricingScreen.View(m.width, m.height)
		}

	case screenNoData:
		if m.noDataScreen != nil {
			return m.noDataScreen.View(m.width, m.height)
		}

	case screenQuestion:
		return m.viewQuestion()

	case screenError:
		return strings.Join([]string{
			m.theme.Style(tuicommon.RoleTitle).Render("Log Analyzer — Error"),
			m.theme.Style(tuicommon.RoleError).Render(m.errMsg),
			"",
			m.theme.Style(tuicommon.RoleHelp).Render("Press any key to quit."),
		}, "\n")
	}
	return ""
}

// viewQuestion renders the active overlay (on top of any background screen).
func (m Model) viewQuestion() string {
	if m.selectOverlay != nil {
		return m.selectOverlay.view()
	}
	if m.textOverlay != nil {
		return m.textOverlay.view()
	}
	return m.theme.Style(tuicommon.RoleMuted).Render("Waiting for input…")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stylesFromTheme converts a tuicommon.Theme to a screens.Styles.
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
