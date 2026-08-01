package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// screenID identifies which screen the root model is currently showing.
type screenID int

const (
	screenHarness   screenID = iota // harness selection (entry screen 1)
	screenMode                      // deploy-new vs update (entry screen 2)
	screenWorkspace                 // workspace path entry (entry screen 3)
	screenQuestion                  // inline question overlay from the Interaction port
	screenRunning                   // service call in progress
	screenDone                      // run completed; showing final status / summary
)

// Options configures the TUI run. All fields are optional; Run substitutes defaults.
type Options struct {
	Theme          Theme
	MouseEnabled   bool
	InitialRequest *app.DeployRequest // optional pre-fill from global flags

	// Interaction is the ProgramRef created by the entry point and passed as Deps.Interaction
	// to the app service. Run() wires the tea.Program into it so that questions from the
	// service reach the TUI overlay system. If nil, service questions are silently skipped.
	Interaction *ProgramRef
}

// Run owns the terminal for the lifetime of the call. It presents the three entry screens to
// collect harness, mode, and workspace, then calls the app service. Questions arising during
// the service call are presented through the Interaction port. Returns when the user exits.
func Run(ctx context.Context, svc app.Service, opts Options) error {
	if opts.Theme.Styles == nil {
		opts.Theme = DefaultTheme()
	}

	m := newRootModel(ctx, svc, opts)

	var programOpts []tea.ProgramOption
	programOpts = append(programOpts, tea.WithAltScreen())
	if opts.MouseEnabled {
		programOpts = append(programOpts, tea.WithMouseCellMotion())
	}

	p := tea.NewProgram(m, programOpts...)

	// Wire the interaction so questions from the service goroutine reach this program.
	// The ProgramRef was created by the caller before app.New(), so the service already
	// holds a reference to it. Storing the program here connects them.
	if opts.Interaction != nil {
		opts.Interaction.set(p)
	}

	_, err := p.Run()
	return err
}

// entrySelections holds the data gathered from the three entry screens.
type entrySelections struct {
	harnessID     string
	mode          domain.RunMode
	workspacePath string
}

// rootModel is the top-level Bubble Tea model. It owns the navigation state machine and
// delegates rendering and input handling to whichever screen is currently active. Screens are
// stored as concrete fields so their state is preserved across back-navigation; the user never
// loses a prior valid selection by pressing Esc.
type rootModel struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	svc       app.Service
	theme     Theme
	screen    screenID
	width     int
	height    int

	// Entry screens (concrete types, not interfaces, so back-navigation preserves state).
	harnessScreen *screens.HarnessScreen
	modeScreen    *screens.ModeScreen
	wsScreen      *screens.WorkspaceScreen

	// Collected entry selections.
	selections entrySelections

	// Question overlay shown during screenQuestion. Exactly one of these is non-nil at a time.
	activeQuestion   *questionMsg
	selectOverlay    *inlineSelectOne                   // generic SelectOne/SelectMany fallback
	confirmOverlay   *inlineConfirm                     // Confirm questions
	modelOverlay     *screens.ModelSelectScreen         // QTierModel, QAgentModel
	textOverlay      *screens.TextPromptScreen          // AskText questions (QCustomTool, QCustomModel)
	conflictOverlay  *screens.ConflictScreen            // QLocalModification
	reviewOverlay    *screens.ReviewScreen              // Review() calls
	workflowOverlay  *screens.WorkflowBrowserScreen     // QWorkflows multi-select
	agentOverlay     *screens.UtilityAgentScreen        // QUtilityAgents multi-select
	infraAgentOverlay *screens.InfrastructureAgentScreen // QInfrastructureAgents multi-select
	hookOverlay      *screens.HookScreen                // QHooks multi-select

	// Post-run summary screen (shown on screenDone after a successful or partial run).
	summaryScreen *screens.SummaryScreen

	// Status / error shown during screenRunning.
	statusMsg string
	errMsg    string
}

func newRootModel(ctx context.Context, svc app.Service, opts Options) *rootModel {
	w := defaultWidth
	h := defaultHeight
	style := stylesFromTheme(opts.Theme)

	// Wrap with cancel so ctrl+c can cancel the service goroutine.
	ctx, cancel := context.WithCancel(ctx)

	harnesses := svc.ListHarnesses()
	hScreen := screens.NewHarnessScreen(harnesses, w, h, style)

	mScreen := screens.NewModeScreen(w, h, style)

	wsScreen := screens.NewWorkspaceScreen(w, h, style)
	if opts.InitialRequest != nil && opts.InitialRequest.WorkspacePath != "" {
		wsScreen.SetPrefilledPath(opts.InitialRequest.WorkspacePath)
	}

	startScreen := screenHarness
	preHarness := ""
	if opts.InitialRequest != nil {
		preHarness = opts.InitialRequest.HarnessID
	}
	if preHarness != "" {
		// A harness was pre-selected; skip to mode selection.
		startScreen = screenMode
	}

	return &rootModel{
		ctx:           ctx,
		ctxCancel:     cancel,
		svc:           svc,
		theme:         opts.Theme,
		screen:        startScreen,
		width:         w,
		height:        h,
		harnessScreen: hScreen,
		modeScreen:    mScreen,
		wsScreen:      wsScreen,
		selections: entrySelections{
			harnessID: preHarness,
		},
	}
}

// stylesFromTheme resolves the active Theme into the concrete Styles expected by screens.
func stylesFromTheme(t Theme) screens.Styles {
	return screens.Styles{
		Title:    t.Style(RoleTitle),
		Subtitle: t.Style(RoleSubtitle),
		Body:     t.Style(RoleBody),
		Muted:    t.Style(RoleMuted),
		Selected: t.Style(RoleSelected),
		Checked:  t.Style(RoleChecked),
		Success:  t.Style(RoleSuccess),
		Warning:  t.Style(RoleWarning),
		Error:    t.Style(RoleError),
		Help:     t.Style(RoleHelp),
		Border:   t.Style(RoleBorder),
	}
}

// Init is called once when the Bubble Tea program starts.
func (m *rootModel) Init() tea.Cmd {
	return nil
}

// Update processes an incoming message and returns the updated model plus any commands.
// The root model handles global keys and delegates screen-specific input to the active screen.
func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global ctrl+c: quit from anywhere, cancelling the service goroutine and unblocking any
	// pending Interaction call.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
		m.ctxCancel()
		if m.activeQuestion != nil && m.activeQuestion.reply != nil {
			m.activeQuestion.reply <- answerMsg{
				confirmAns: domain.ConfirmAnswer{Status: domain.Cancelled},
			}
			m.activeQuestion = nil
		}
		return m, tea.Quit
	}

	// Window resize: propagate to all screens.
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
		m.harnessScreen.Resize(m.width, m.height)
		m.modeScreen.Resize(m.width, m.height)
		m.wsScreen.Resize(m.width, m.height)
		m.resizeQuestionOverlays(m.width, m.height)
		if m.summaryScreen != nil {
			m.summaryScreen.Resize(m.width, m.height)
		}
		return m, nil
	}

	// Question from the Interaction port (sent by the service goroutine).
	if qMsg, ok := msg.(questionMsg); ok {
		return m.handleQuestionMsg(qMsg)
	}

	// Service completed.
	if doneMsg, ok := msg.(runDoneMsg); ok {
		m.screen = screenDone
		style := stylesFromTheme(m.theme)
		m.summaryScreen = screens.NewSummaryScreen(doneMsg.summary, m.width, m.height, style)
		return m, nil
	}
	if errMsg, ok := msg.(runErrorMsg); ok {
		if errors.Is(errMsg.err, app.ErrPlanNotConfirmed) {
			// The user declined or cancelled the plan review; restart the selection flow
			// in the same session rather than showing the done/error state.
			m.screen = screenHarness
			m.errMsg = ""
			m.selections = entrySelections{}
			m.activeQuestion = nil
			m.reviewOverlay = nil
			return m, nil
		}
		m.screen = screenDone
		m.errMsg = errMsg.err.Error()
		return m, nil
	}

	// Delegate to the current screen.
	switch m.screen {
	case screenHarness:
		return m.updateHarness(msg)
	case screenMode:
		return m.updateMode(msg)
	case screenWorkspace:
		return m.updateWorkspace(msg)
	case screenQuestion:
		return m.updateQuestion(msg)
	case screenRunning:
		// No navigation while running.
		return m, nil
	case screenDone:
		if m.summaryScreen != nil {
			done := m.summaryScreen.Update(msg)
			if done {
				return m, tea.Quit
			}
			return m, nil
		}
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "q", "enter", "esc":
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// resizeQuestionOverlays propagates a terminal resize to whichever question overlay is active.
func (m *rootModel) resizeQuestionOverlays(width, height int) {
	if m.selectOverlay != nil {
		m.selectOverlay.width = width
		m.selectOverlay.height = height
	}
	if m.confirmOverlay != nil {
		m.confirmOverlay.width = width
	}
	if m.modelOverlay != nil {
		m.modelOverlay.Resize(width, height)
	}
	if m.textOverlay != nil {
		m.textOverlay.Resize(width, height)
	}
	if m.conflictOverlay != nil {
		m.conflictOverlay.Resize(width, height)
	}
	if m.reviewOverlay != nil {
		m.reviewOverlay.Resize(width, height)
	}
	if m.workflowOverlay != nil {
		m.workflowOverlay.Resize(width, height)
	}
	if m.agentOverlay != nil {
		m.agentOverlay.Resize(width, height)
	}
	if m.infraAgentOverlay != nil {
		m.infraAgentOverlay.Resize(width, height)
	}
	if m.hookOverlay != nil {
		m.hookOverlay.Resize(width, height)
	}
}

func (m *rootModel) updateHarness(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.harnessScreen.Update(msg)
	if m.harnessScreen.Back() {
		// Back from the first screen exits the TUI (nothing to go back to).
		m.harnessScreen.Reset()
		return m, tea.Quit
	}
	if m.harnessScreen.Done() {
		m.selections.harnessID = m.harnessScreen.SelectedID()
		m.harnessScreen.Reset()
		m.screen = screenMode
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.modeScreen.Update(msg)
	if m.modeScreen.Back() {
		// Back from mode returns to harness selection, preserving the harness choice.
		m.modeScreen.Reset()
		m.screen = screenHarness
		return m, nil
	}
	if m.modeScreen.Done() {
		m.selections.mode = m.modeScreen.SelectedMode()
		m.modeScreen.Reset()
		m.screen = screenWorkspace
		// Start the text input cursor blink.
		return m, m.wsScreen.InputInit()
	}
	return m, cmd
}

func (m *rootModel) updateWorkspace(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.wsScreen.Update(msg)
	if m.wsScreen.Back() {
		// Back from workspace returns to mode selection, preserving what was typed.
		m.wsScreen.Reset()
		m.screen = screenMode
		return m, nil
	}
	if m.wsScreen.Done() {
		m.selections.workspacePath = m.wsScreen.WorkspacePath()
		m.wsScreen.Reset()
		m.screen = screenRunning
		m.statusMsg = "Running deployment…"
		return m, m.startService()
	}
	return m, cmd
}

func (m *rootModel) updateQuestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ModelSelectScreen overlay (QTierModel, QAgentModel).
	if m.modelOverlay != nil {
		done := m.modelOverlay.Update(msg)
		if done {
			ans := m.modelOverlay.Answer()
			m.dispatchChoiceAnswer(ans)
			m.modelOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	// TextPromptScreen overlay (QCustomTool, QCustomModel, other AskText).
	if m.textOverlay != nil {
		done := m.textOverlay.Update(msg)
		if done {
			ans := m.textOverlay.Answer()
			if m.activeQuestion != nil {
				m.activeQuestion.reply <- answerMsg{textAns: ans}
				m.activeQuestion = nil
			}
			m.textOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	// ConflictScreen overlay (QLocalModification).
	if m.conflictOverlay != nil {
		done := m.conflictOverlay.Update(msg)
		if done {
			ans := m.conflictOverlay.Answer()
			m.dispatchChoiceAnswer(ans)
			m.conflictOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	// ReviewScreen overlay (Review() call).
	if m.reviewOverlay != nil {
		done := m.reviewOverlay.Update(msg)
		if done {
			ans := m.reviewOverlay.Answer()
			if m.activeQuestion != nil {
				m.activeQuestion.reply <- answerMsg{confirmAns: ans}
				m.activeQuestion = nil
			}
			m.reviewOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	// WorkflowBrowserScreen overlay (QWorkflows).
	if m.workflowOverlay != nil {
		cmd := m.workflowOverlay.Update(msg)
		if m.workflowOverlay.Done() {
			ids := m.workflowOverlay.SelectedIDs()
			m.dispatchMultiChoiceAnswer(ids)
			m.workflowOverlay = nil
			m.screen = screenRunning
		} else if m.workflowOverlay.Back() {
			m.dispatchMultiChoiceCancelled()
			m.workflowOverlay = nil
			m.screen = screenRunning
		}
		return m, cmd
	}

	// UtilityAgentScreen overlay (QUtilityAgents).
	if m.agentOverlay != nil {
		cmd := m.agentOverlay.Update(msg)
		if m.agentOverlay.Done() {
			ids := m.agentOverlay.SelectedIDs()
			m.dispatchMultiChoiceAnswer(ids)
			m.agentOverlay = nil
			m.screen = screenRunning
		} else if m.agentOverlay.Back() {
			m.dispatchMultiChoiceCancelled()
			m.agentOverlay = nil
			m.screen = screenRunning
		}
		return m, cmd
	}

	// InfrastructureAgentScreen overlay (QInfrastructureAgents).
	if m.infraAgentOverlay != nil {
		cmd := m.infraAgentOverlay.Update(msg)
		if m.infraAgentOverlay.Done() {
			ids := m.infraAgentOverlay.SelectedIDs()
			m.dispatchMultiChoiceAnswer(ids)
			m.infraAgentOverlay = nil
			m.screen = screenRunning
		} else if m.infraAgentOverlay.Back() {
			m.dispatchMultiChoiceCancelled()
			m.infraAgentOverlay = nil
			m.screen = screenRunning
		}
		return m, cmd
	}

	// HookScreen overlay (QHooks).
	if m.hookOverlay != nil {
		cmd := m.hookOverlay.Update(msg)
		if m.hookOverlay.Done() {
			ids := m.hookOverlay.SelectedIDs()
			m.dispatchMultiChoiceAnswer(ids)
			m.hookOverlay = nil
			m.screen = screenRunning
		} else if m.hookOverlay.Back() {
			m.dispatchMultiChoiceCancelled()
			m.hookOverlay = nil
			m.screen = screenRunning
		}
		return m, cmd
	}

	// Generic SelectOne / SelectMany overlay (fallback for non-specialised question IDs).
	if m.selectOverlay != nil {
		done := m.selectOverlay.update(msg)
		if done {
			ans := m.selectOverlay.answer()
			if m.activeQuestion != nil {
				switch m.activeQuestion.kind {
				case questionSelectMany:
					ids := []string{}
					if ans.Status == domain.Answered {
						ids = []string{ans.OptionID}
					}
					m.activeQuestion.reply <- answerMsg{
						multiChoiceAns: domain.MultiChoiceAnswer{
							Status:    ans.Status,
							OptionIDs: ids,
						},
					}
				default:
					m.activeQuestion.reply <- answerMsg{choiceAns: ans}
				}
				m.activeQuestion = nil
			}
			m.selectOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	// ConfirmOverlay (Confirm questions and the review fallback).
	if m.confirmOverlay != nil {
		done := m.confirmOverlay.update(msg)
		if done {
			ans := m.confirmOverlay.answer()
			if m.activeQuestion != nil {
				m.activeQuestion.reply <- answerMsg{confirmAns: ans}
				m.activeQuestion = nil
			}
			m.confirmOverlay = nil
			m.screen = screenRunning
		}
		return m, nil
	}

	return m, nil
}

// dispatchChoiceAnswer sends a ChoiceAnswer to the active question's reply channel, handling
// SelectMany questions that expect MultiChoiceAnswer instead.
func (m *rootModel) dispatchChoiceAnswer(ans domain.ChoiceAnswer) {
	if m.activeQuestion == nil {
		return
	}
	switch m.activeQuestion.kind {
	case questionSelectMany:
		ids := []string{}
		if ans.Status == domain.Answered && ans.OptionID != "" {
			ids = []string{ans.OptionID}
		}
		m.activeQuestion.reply <- answerMsg{
			multiChoiceAns: domain.MultiChoiceAnswer{
				Status:    ans.Status,
				OptionIDs: ids,
			},
		}
	default:
		m.activeQuestion.reply <- answerMsg{choiceAns: ans}
	}
	m.activeQuestion = nil
}

// dispatchMultiChoiceAnswer sends a MultiChoiceAnswer to the active question's reply channel.
// When ids is non-empty the status is Answered; when ids is empty the status is SkippedOne,
// consistent with AllowSkip being true on all three SelectMany questions.
func (m *rootModel) dispatchMultiChoiceAnswer(ids []string) {
	if m.activeQuestion == nil {
		return
	}
	status := domain.SkippedOne
	if len(ids) > 0 {
		status = domain.Answered
	}
	m.activeQuestion.reply <- answerMsg{
		multiChoiceAns: domain.MultiChoiceAnswer{
			Status:    status,
			OptionIDs: ids,
		},
	}
	m.activeQuestion = nil
}

// dispatchMultiChoiceCancelled sends a Cancelled MultiChoiceAnswer to the active question's
// reply channel. Used when the user presses Esc/Back on a dedicated multi-select screen.
func (m *rootModel) dispatchMultiChoiceCancelled() {
	if m.activeQuestion == nil {
		return
	}
	m.activeQuestion.reply <- answerMsg{
		multiChoiceAns: domain.MultiChoiceAnswer{
			Status: domain.Cancelled,
		},
	}
	m.activeQuestion = nil
}

func (m *rootModel) handleQuestionMsg(qMsg questionMsg) (tea.Model, tea.Cmd) {
	style := stylesFromTheme(m.theme)

	switch qMsg.kind {
	case questionNotice:
		m.statusMsg = fmt.Sprintf("[%s] %s: %s",
			qMsg.notice.Level, qMsg.notice.Title, qMsg.notice.Message)
		return m, nil

	case questionProgress:
		e := qMsg.progress
		if e.Total > 0 {
			m.statusMsg = fmt.Sprintf("%s  %d/%d  %s", e.Phase, e.Current, e.Total, e.Subject)
		} else {
			m.statusMsg = fmt.Sprintf("%s  %s", e.Phase, e.Subject)
		}
		return m, nil

	case questionSelectOne:
		m.activeQuestion = &qMsg
		// Route model-selection questions to the dedicated overlay.
		switch qMsg.choiceQ.ID {
		case domain.QTierModel, domain.QAgentModel:
			m.modelOverlay = screens.NewModelSelectScreen(qMsg.choiceQ, m.width, m.height, style)
		case domain.QLocalModification:
			m.conflictOverlay = screens.NewConflictScreen(qMsg.choiceQ, m.width, m.height, style)
		default:
			m.selectOverlay = newInlineSelectOne(qMsg.choiceQ, m.theme, m.width, m.height)
		}
		m.screen = screenQuestion
		return m, nil

	case questionSelectMany:
		m.activeQuestion = &qMsg
		switch qMsg.choiceQ.ID {
		case domain.QWorkflows:
			cats := optionsToWorkflowCategories(qMsg.choiceQ.Options)
			m.workflowOverlay = screens.NewWorkflowBrowserScreen(cats, m.width, m.height, style, "")
		case domain.QUtilityAgents:
			agents := optionsToAgents(qMsg.choiceQ.Options)
			m.agentOverlay = screens.NewUtilityAgentScreen(agents, m.width, m.height, style, "")
		case domain.QInfrastructureAgents:
			agents := optionsToAgents(qMsg.choiceQ.Options)
			m.infraAgentOverlay = screens.NewInfrastructureAgentScreen(agents, m.width, m.height, style)
		case domain.QHooks:
			bundles, supported := optionsToHookBundles(qMsg.choiceQ.Options)
			m.hookOverlay = screens.NewHookScreen(bundles, supported, m.width, m.height, style, "")
		default:
			m.selectOverlay = newInlineSelectOne(qMsg.choiceQ, m.theme, m.width, m.height)
		}
		m.screen = screenQuestion
		return m, nil

	case questionConfirm:
		m.activeQuestion = &qMsg
		m.confirmOverlay = newInlineConfirm(qMsg.confirmQ, m.theme, m.width)
		m.screen = screenQuestion
		return m, nil

	case questionReview:
		m.activeQuestion = &qMsg
		m.reviewOverlay = screens.NewReviewScreen(qMsg.plan, m.width, m.height, style)
		m.screen = screenQuestion
		return m, nil

	case questionAskText:
		m.activeQuestion = &qMsg
		m.textOverlay = screens.NewTextPromptScreen(qMsg.textQ, m.width, m.height, style)
		m.screen = screenQuestion
		return m, nil
	}
	return m, nil
}

// startService launches the app service in a background goroutine and returns a tea.Cmd that
// delivers the result back to the program as a runDoneMsg or runErrorMsg. The goroutine uses
// the model's context so that ctrl+c (which calls m.ctxCancel) propagates to the service.
func (m *rootModel) startService() tea.Cmd {
	sel := m.selections
	svc := m.svc
	ctx := m.ctx // capture once; ctrl+c cancels this context via m.ctxCancel

	return func() tea.Msg {
		switch sel.mode {
		case domain.ModeDeployNew:
			summary, err := svc.DeployNew(ctx, app.DeployRequest{
				HarnessID:     sel.harnessID,
				WorkspacePath: sel.workspacePath,
			})
			if err != nil {
				return runErrorMsg{err: err}
			}
			return runDoneMsg{summary: summary}

		case domain.ModeUpdate:
			summary, err := svc.Update(ctx, app.UpdateRequest{
				HarnessID:     sel.harnessID,
				WorkspacePath: sel.workspacePath,
			})
			if err != nil {
				return runErrorMsg{err: err}
			}
			return runDoneMsg{summary: summary}

		default:
			return runErrorMsg{err: fmt.Errorf("unknown mode: %q", sel.mode)}
		}
	}
}

// runDoneMsg signals that the service call completed and carries the run summary.
type runDoneMsg struct {
	summary domain.RunSummary
}

// runErrorMsg signals that the service call returned an error.
type runErrorMsg struct{ err error }

// View renders the current screen to a string. It is called after every Update.
func (m *rootModel) View() string {
	switch m.screen {
	case screenHarness:
		return m.harnessScreen.View()
	case screenMode:
		return m.modeScreen.View()
	case screenWorkspace:
		return m.wsScreen.View()
	case screenQuestion:
		return m.viewQuestion()
	case screenRunning:
		return m.viewRunning()
	case screenDone:
		return m.viewDone()
	}
	return ""
}

func (m *rootModel) viewQuestion() string {
	if m.modelOverlay != nil {
		return m.modelOverlay.View()
	}
	if m.textOverlay != nil {
		return m.textOverlay.View()
	}
	if m.conflictOverlay != nil {
		return m.conflictOverlay.View()
	}
	if m.reviewOverlay != nil {
		return m.reviewOverlay.View()
	}
	if m.workflowOverlay != nil {
		return m.workflowOverlay.View()
	}
	if m.agentOverlay != nil {
		return m.agentOverlay.View()
	}
	if m.infraAgentOverlay != nil {
		return m.infraAgentOverlay.View()
	}
	if m.hookOverlay != nil {
		return m.hookOverlay.View()
	}
	if m.selectOverlay != nil {
		return m.selectOverlay.view()
	}
	if m.confirmOverlay != nil {
		return m.confirmOverlay.view()
	}
	return m.theme.Style(RoleMuted).Render("Waiting for question…")
}

func (m *rootModel) viewRunning() string {
	status := m.statusMsg
	if status == "" {
		status = "Running…"
	}
	return m.theme.Style(RoleBody).Width(m.width).Render(status) + "\n" +
		m.theme.Style(RoleHelp).Width(m.width).Render("ctrl+c to cancel")
}

func (m *rootModel) viewDone() string {
	if m.summaryScreen != nil {
		return m.summaryScreen.View()
	}
	if m.errMsg != "" {
		return m.theme.Style(RoleError).Width(m.width).Render(
			"Deployment failed:\n"+m.errMsg,
		) + "\n" + m.theme.Style(RoleHelp).Width(m.width).Render("press q or enter to exit")
	}
	return m.theme.Style(RoleSuccess).Width(m.width).Render("Deployment complete.") +
		"\n" + m.theme.Style(RoleHelp).Width(m.width).Render("press q or enter to exit")
}

// renderPlanSummary produces a brief human-readable description of a deployment plan for the
// plan-review confirm overlay.
func (m *rootModel) renderPlanSummary(p domain.Plan) string {
	counts := p.Counts()
	var parts []string
	if n := counts[domain.ActionCreate]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", n))
	}
	if n := counts[domain.ActionUpdate]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", n))
	}
	if n := counts[domain.ActionUnchanged]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", n))
	}
	if n := counts[domain.ActionConflict]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d locally modified", n))
	}
	summary := strings.Join(parts, ", ")
	if summary == "" {
		summary = "nothing to do"
	}
	return fmt.Sprintf(
		"Harness: %s\nWorkspace: %s\n\nPlan: %s\n\nProceed with deployment?",
		p.Harness.DisplayName, p.WorkspacePath, summary,
	)
}
