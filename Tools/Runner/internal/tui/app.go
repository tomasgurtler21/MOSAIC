// Package tui implements the TUI frontend for mosaic-run. It uses Bubble Tea
// and the shared theme/keys/scaffold from mosaic-common/tui.
//
// Entry point: Run(). The caller supplies the workflow regions (enumerated from the
// orchestrator file), a started session, and Options. Run() collects the run setup
// inputs interactively, starts the session in a background goroutine, shows live
// progress, handles deviation resolution, and optionally lets the user inspect the
// artifact. When the run ends, it shows the outcome and waits for the user to quit.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens"
	"mosaic-run/internal/workflow"
)

// screenID identifies the currently active screen.
type screenID int

const (
	screenSetupFile     screenID = iota // orchestrator file path entry
	screenSetupWorkflow                 // workflow selection
	screenSetupTask                     // task description entry
	screenSetupConfig                   // run configuration prompts
	screenProgress                      // live execution progress
	screenDeviation                     // deviation resolution
	screenArtifact                      // read-only artifact inspection
	screenQuestion                      // generic overlay from Interaction port
	screenDone                          // completion/error summary
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
}

// runSetupSelections holds all inputs collected during the setup phase.
type runSetupSelections struct {
	orchestratorFile string
	workflowID       domain.WorkflowID
	task             string
	config           screens.ConfigSelection
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

// deviationRequestMsg is sent by the TUIDeviationResolver to present a deviation to the user.
type deviationRequestMsg struct {
	info  domain.DeviationInfo
	reply chan deviationReplyMsg
}

// deviationReplyMsg carries the user's deviation resolution back to the resolver.
type deviationReplyMsg struct {
	choice     screens.DeviationChoice
	resolution screens.ManualResolution
}

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
	sess session.Session
	interact *ProgramRef

	// Enumerated workflow regions (populated after orchestrator file is loaded).
	workflows []domain.WorkflowRegion

	// Routing table for the selected workflow (populated after workflow selection).
	// Passed to NewDeviationScreen so the manual deviation row list is populated.
	routingTable domain.RoutingTable

	// Entry screens (concrete types so back-navigation preserves state).
	fileScreen     *screens.OrchestratorFileScreen
	workflowScreen *screens.WorkflowSelectScreen
	taskScreen     *screens.TaskScreen
	configScreen   *screens.ConfigScreen

	// Collected setup selections.
	selections runSetupSelections

	// Progress screen (constructed when execution starts).
	progressScreen *screens.ProgressScreen

	// Deviation screen (constructed when a deviation occurs).
	deviationScreen *screens.DeviationScreen
	pendingDeviation *deviationRequestMsg

	// Artifact inspection screen.
	artifactScreen *screens.ArtifactScreen
	prevScreen     screenID // screen to return to after artifact inspection

	// Generic question overlays from the Interaction port.
	activeQuestion  *questionMsg
	selectOverlay   *inlineSelectOne
	textOverlay     *inlineText
	confirmOverlay  *inlineConfirm

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
	configScreen := screens.NewConfigScreen(w, h, style)

	interact := opts.Interaction
	if interact == nil {
		interact = NewProgramRef()
	}

	return &rootModel{
		ctx:        ctx,
		ctxCancel:  cancel,
		theme:      opts.Theme,
		screen:     screenSetupFile,
		width:      w,
		height:     h,
		sess:       sess,
		interact:   interact,
		fileScreen: fileScreen,
		taskScreen: taskScreen,
		configScreen: configScreen,
	}
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

	// Deviation from the TUIDeviationResolver.
	if devMsg, ok := msg.(deviationRequestMsg); ok {
		return m.handleDeviationMsg(devMsg)
	}

	// Session completed.
	if doneMsg, ok := msg.(runDoneMsg); ok {
		style := stylesFromTheme(m.theme)
		m.doneScreen = screens.NewDoneScreen(doneMsg.outcome, "", m.width, m.height, style)
		m.screen = screenDone
		return m, nil
	}
	if errMsg, ok := msg.(runErrorMsg); ok {
		style := stylesFromTheme(m.theme)
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
	case screenSetupFile:
		return m.updateSetupFile(msg)
	case screenSetupWorkflow:
		return m.updateSetupWorkflow(msg)
	case screenSetupTask:
		return m.updateSetupTask(msg)
	case screenSetupConfig:
		return m.updateSetupConfig(msg)
	case screenProgress:
		return m.updateProgress(msg)
	case screenDeviation:
		return m.updateDeviation(msg)
	case screenArtifact:
		return m.updateArtifact(msg)
	case screenQuestion:
		return m.updateQuestion(msg)
	case screenDone:
		return m.updateDone(msg)
	}
	return m, nil
}

func (m *rootModel) resizeScreens() {
	if m.fileScreen != nil {
		m.fileScreen.Resize(m.width, m.height)
	}
	if m.workflowScreen != nil {
		m.workflowScreen.Resize(m.width, m.height)
	}
	if m.taskScreen != nil {
		m.taskScreen.Resize(m.width, m.height)
	}
	if m.configScreen != nil {
		m.configScreen.Resize(m.width, m.height)
	}
	if m.progressScreen != nil {
		m.progressScreen.Resize(m.width, m.height)
	}
	if m.deviationScreen != nil {
		m.deviationScreen.Resize(m.width, m.height)
	}
	if m.artifactScreen != nil {
		m.artifactScreen.Resize(m.width, m.height)
	}
	if m.doneScreen != nil {
		m.doneScreen.Resize(m.width, m.height)
	}
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

		// Parse the routing table so the deviation screen can show meaningful row
		// choices if a deviation occurs later during execution.
		if region, err := orchfile.GetWorkflow(m.selections.orchestratorFile, selectedID); err == nil {
			if table, err := workflow.Parse(region.Content, region.Info); err == nil {
				m.routingTable = table
			}
		}

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
		m.screen = screenSetupConfig
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.configScreen.Update(msg)
	if m.configScreen.Back() {
		m.configScreen.Reset()
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	if m.configScreen.Done() {
		m.selections.config = m.configScreen.Selection()
		m.configScreen.Reset()

		// Transition to progress screen and start the session.
		style := stylesFromTheme(m.theme)
		m.progressScreen = screens.NewProgressScreen(m.width, m.height, style)
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
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

// readArtifactContent reads the Orchestration.md file at the canonical path.
func (m *rootModel) readArtifactContent() string {
	// The canonical path is in the same directory as the orchestrator file.
	path := m.selections.orchestratorFile
	dir := ""
	if idx := strings.LastIndexByte(path, '/'); idx >= 0 {
		dir = path[:idx+1]
	} else if idx := strings.LastIndexByte(path, '\\'); idx >= 0 {
		dir = path[:idx+1]
	}
	artPath := dir + "Orchestration.md"
	data, err := os.ReadFile(artPath)
	if err != nil {
		return fmt.Sprintf("(could not read artifact: %v)", err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// Deviation screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) handleDeviationMsg(devMsg deviationRequestMsg) (tea.Model, tea.Cmd) {
	m.pendingDeviation = &devMsg
	style := stylesFromTheme(m.theme)
	m.deviationScreen = screens.NewDeviationScreen(devMsg.info, m.buildRoutingTable(), m.width, m.height, style)
	m.screen = screenDeviation
	return m, nil
}

// buildRoutingTable returns the routing table parsed during workflow selection.
// This ensures the deviation screen's manual row list is populated with the
// actual workflow rows.
func (m *rootModel) buildRoutingTable() domain.RoutingTable {
	return m.routingTable
}

func (m *rootModel) updateDeviation(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.deviationScreen == nil {
		return m, nil
	}
	cmd := m.deviationScreen.Update(msg)
	if m.deviationScreen.Back() {
		// User pressed Esc on the first choice screen — treat as "stop".
		if m.pendingDeviation != nil {
			m.pendingDeviation.reply <- deviationReplyMsg{choice: screens.DeviationChoiceStop}
			m.pendingDeviation = nil
		}
		m.deviationScreen = nil
		m.screen = screenProgress
		return m, nil
	}
	if m.deviationScreen.Done() {
		choice := m.deviationScreen.Choice()
		res := m.deviationScreen.Resolution()
		if m.pendingDeviation != nil {
			m.pendingDeviation.reply <- deviationReplyMsg{choice: choice, resolution: res}
			m.pendingDeviation = nil
		}
		m.deviationScreen = nil
		m.screen = screenProgress
		return m, nil
	}
	return m, cmd
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
// Session starter
// ---------------------------------------------------------------------------

// startSession launches the session in a background goroutine and returns a tea.Cmd
// that delivers the result as a runDoneMsg or runErrorMsg.
func (m *rootModel) startSession() tea.Cmd {
	sel := m.selections
	sess := m.sess
	ctx := m.ctx

	return func() tea.Msg {
		config := domain.RunConfig{
			OrchestratorFilePath: sel.orchestratorFile,
			WorkflowID:           sel.workflowID,
			Task:                 sel.task,
			OnDeviation:          sel.config.DeviationMode,
			ExistingArtifact:     sel.config.ExistingArtifact,
			AllowVersionDrift:    sel.config.AllowVersionDrift,
			Checkpoints:          sel.config.Checkpoints,
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
	case screenSetupFile:
		return m.fileScreen.View()
	case screenSetupWorkflow:
		if m.workflowScreen != nil {
			return m.workflowScreen.View()
		}
	case screenSetupTask:
		return m.taskScreen.View()
	case screenSetupConfig:
		return m.configScreen.View()
	case screenProgress:
		if m.progressScreen != nil {
			return m.progressScreen.View()
		}
	case screenDeviation:
		if m.deviationScreen != nil {
			return m.deviationScreen.View()
		}
	case screenArtifact:
		if m.artifactScreen != nil {
			return m.artifactScreen.View()
		}
	case screenQuestion:
		return m.viewQuestion()
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

// ---------------------------------------------------------------------------
// TUIDeviationResolver
// ---------------------------------------------------------------------------

// TUIDeviationResolver implements domain.DeviationResolver by routing the deviation
// choice through the TUI's deviation screen. For "delegate" choices, it falls back
// to the orchestrator delegate. For "manual" choices, it collects the rejoin row and
// HITL override from the TUI.
type TUIDeviationResolver struct {
	// Delegate is the orchestrator-delegate resolver used when the user chooses "delegate".
	Delegate *deviation.OrchestratorDelegate
	// Program is the TUI program ref, used to send deviation messages.
	Program *ProgramRef
	// Table is the workflow's routing table, for the row-selection prompt.
	Table domain.RoutingTable
}

// Resolve implements domain.DeviationResolver.
func (r *TUIDeviationResolver) Resolve(ctx context.Context, info domain.DeviationInfo) (domain.RejoinInstruction, error) {
	p := r.Program.program()
	if p == nil {
		// No program running — fall back to stop.
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "TUI not running"}}, nil
	}

	reply := make(chan deviationReplyMsg, 1)
	p.Send(deviationRequestMsg{info: info, reply: reply})

	var result deviationReplyMsg
	select {
	case result = <-reply:
	case <-ctx.Done():
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "context cancelled during deviation resolution"}}, nil
	}

	switch result.choice {
	case screens.DeviationChoiceStop:
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "user requested stop"}}, nil

	case screens.DeviationChoiceManual:
		res := result.resolution
		if res.Agent != "" {
			// Compose a full custom dispatch invocation with the collected protocol fields.
			var inputArtifacts, outputArtifacts []string
			for _, a := range strings.Split(res.InputArtifacts, ",") {
				if a = strings.TrimSpace(a); a != "" {
					inputArtifacts = append(inputArtifacts, a)
				}
			}
			for _, a := range strings.Split(res.OutputArtifacts, ",") {
				if a = strings.TrimSpace(a); a != "" {
					outputArtifacts = append(outputArtifacts, a)
				}
			}
			req := domain.ProtocolRequest{
				AgentInstanceID: res.Agent + "#1",
				TaskDescription: res.Task,
				InputArtifacts:  inputArtifacts,
				OutputArtifacts: outputArtifacts,
				Constraints:     res.Constraints,
			}
			if res.HITLOverride != nil {
				req.HumanInTheLoop = *res.HITLOverride
			}
			return domain.RejoinInstruction{
				Custom: &domain.CustomDispatch{
					Agent:        domain.AgentReference{Identifier: res.Agent},
					Request:      req,
					HITLOverride: res.HITLOverride,
					RejoinRow:    res.RejoinRowIndex,
				},
			}, nil
		}
		// No custom agent specified — simple rejoin at the selected row.
		return domain.RejoinInstruction{
			Rejoin: &domain.RejoinAtRow{
				RowIndex:     res.RejoinRowIndex,
				HITLOverride: res.HITLOverride,
			},
		}, nil

	case screens.DeviationChoiceDelegate:
		if r.Delegate != nil {
			return r.Delegate.Resolve(ctx, info)
		}
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "no orchestrator delegate configured"}}, nil

	default:
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "unknown deviation choice"}}, nil
	}
}
