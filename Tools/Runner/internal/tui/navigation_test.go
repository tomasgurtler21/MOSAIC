package tui

// navigation_test.go verifies the rootModel navigation state machine: initial screen,
// window resize propagation, ctrl+c cancellation, and done-screen key handling.
//
// These tests are in package tui (internal) because screenID and rootModel fields are
// unexported. Tests drive the model through the Bubble Tea model/update cycle with no
// real terminal attached.

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stub session
// ---------------------------------------------------------------------------

type stubNavSession struct {
	outcome domain.RunOutcome
	err     error
}

func (s *stubNavSession) Start(_ context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
	return s.outcome, s.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestModel() *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

func sendKey(m *rootModel, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: keyType})
}

func newProgressScreen(m *rootModel) *screens.ProgressScreen {
	style := stylesFromTheme(m.theme)
	return screens.NewProgressScreen(m.width, m.height, style)
}

// makeChoiceQuestion creates a ChoiceQuestion with the given title and option IDs.
func makeChoiceQuestion(title string, optionIDs []string) interaction.ChoiceQuestion {
	opts := make([]interaction.Option, len(optionIDs))
	for i, id := range optionIDs {
		opts[i] = interaction.Option{ID: id, Label: id}
	}
	return interaction.ChoiceQuestion{
		Question: interaction.Question{Title: title},
		Options:  opts,
	}
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

func TestNavigation_InitialScreen_IsSetupFile(t *testing.T) {
	m := newTestModel()
	if m.screen != screenSetupFile {
		t.Errorf("initial screen = %v, want screenSetupFile (%v)", m.screen, screenSetupFile)
	}
}

func TestNavigation_FileScreen_ViewNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string on file screen; want non-empty output")
	}
}

func TestNavigation_FileScreen_ViewContainsTitleText(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if !containsAny(view, "Orchestrator", "orchestrator", "File", "file") {
		t.Errorf("file screen view does not contain expected title text:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// ctrl+c cancellation
// ---------------------------------------------------------------------------

func TestNavigation_CtrlC_CancelsContextAndQuits(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c; context must be cancelled")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c; want non-nil tea.Quit command")
	}
}

func TestNavigation_CtrlC_ModelHasCancellableContext(t *testing.T) {
	m := newTestModel()
	if m.ctxCancel == nil {
		t.Error("rootModel must have a non-nil ctxCancel function")
	}
	if m.ctx == nil {
		t.Error("rootModel must have a non-nil ctx")
	}
}

// ---------------------------------------------------------------------------
// Window resize propagation
// ---------------------------------------------------------------------------

func TestNavigation_WindowResize_UpdatesDimensions(t *testing.T) {
	m := newTestModel()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resize message caused a panic: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Errorf("after resize: width=%d height=%d, want 120/40", m.width, m.height)
	}
}

func TestNavigation_WindowResize_WithProgressScreenDoesNotPanic(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resize with progress screen caused a panic: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

// ---------------------------------------------------------------------------
// Esc from file screen
// ---------------------------------------------------------------------------

func TestNavigation_EscFromFileScreen_ReturnsQuitCommand(t *testing.T) {
	m := newTestModel()
	_, cmd := sendKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Error("cmd = nil after Esc from file screen; want tea.Quit (non-nil command)")
	}
}

// ---------------------------------------------------------------------------
// Done screen
// ---------------------------------------------------------------------------

func TestNavigation_RunDoneMsg_TransitionsToDoneScreen(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	if m.screen != screenDone {
		t.Errorf("screen = %v after runDoneMsg, want screenDone (%v)", m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Error("doneScreen = nil after runDoneMsg; must be populated")
	}
}

func TestNavigation_RunErrorMsg_TransitionsToDoneScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runErrorMsg{err: errors.New("session failed")})
	if m.screen != screenDone {
		t.Errorf("screen = %v after runErrorMsg, want screenDone (%v)", m.screen, screenDone)
	}
}

func TestNavigation_DoneScreen_QKeyQuits(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone (%v)", m.screen, screenDone)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("cmd = nil after 'q' on done screen; want tea.Quit")
	}
}

func TestNavigation_DoneScreen_EnterKeyQuits(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	_, cmd := sendKey(m, tea.KeyEnter)
	if cmd == nil {
		t.Error("cmd = nil after Enter on done screen; want tea.Quit")
	}
}

// ---------------------------------------------------------------------------
// Progress screen
// ---------------------------------------------------------------------------

func TestNavigation_ProgressScreen_GracefulStop_CancelsContext(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})

	if !m.progressScreen.GracefulStop() {
		t.Error("GracefulStop() = false after 's' key; want true")
	}
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after graceful stop; context must be cancelled")
	}
}

func TestNavigation_ProgressScreen_ArtifactView_TransitionsToArtifactScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	// Set the orchestrator file to something that will fail gracefully.
	m.selections.orchestratorFile = "/nonexistent/Orchestrator.md"

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	if m.screen != screenArtifact {
		t.Errorf("screen = %v after 'a' key, want screenArtifact (%v)", m.screen, screenArtifact)
	}
}

func TestNavigation_ArtifactScreen_EscReturnsToProgress(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	m.selections.orchestratorFile = "/nonexistent/Orchestrator.md"

	// Enter artifact screen.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.screen != screenArtifact {
		t.Fatalf("precondition: screen = %v, want screenArtifact", m.screen)
	}

	// Press Esc to return.
	sendKey(m, tea.KeyEsc)
	if m.screen != screenProgress {
		t.Errorf("screen = %v after Esc from artifact screen, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// ---------------------------------------------------------------------------
// Deviation screen
// ---------------------------------------------------------------------------

func TestNavigation_DeviationMsg_TransitionsToDeviationScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan deviationReplyMsg, 1)
	info := domain.DeviationInfo{
		Kind: domain.DeviationNonSuccess,
		Response: domain.ProtocolResponse{
			AgentInstanceID: "test-agent#1",
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   "blocked",
		},
		CurrentRow:   0,
		CurrentPhase: "PLANNING",
	}
	m.Update(deviationRequestMsg{info: info, reply: reply})

	if m.screen != screenDeviation {
		t.Errorf("screen = %v after deviation message, want screenDeviation (%v)", m.screen, screenDeviation)
	}
	if m.deviationScreen == nil {
		t.Error("deviationScreen = nil after deviation message; must be populated")
	}
}

func TestNavigation_DeviationScreen_ViewContainsBothResolutionPaths(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan deviationReplyMsg, 1)
	info := domain.DeviationInfo{
		Response: domain.ProtocolResponse{
			AgentInstanceID: "agent#1",
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   "error message",
		},
		CurrentRow:   0,
		CurrentPhase: "PLANNING",
	}
	m.Update(deviationRequestMsg{info: info, reply: reply})

	view := m.View()
	if !containsAny(view, "Delegate", "delegate", "orchestrator") {
		t.Errorf("deviation screen does not show delegate option:\n%s", view)
	}
	if !containsAny(view, "Manually", "manually", "manual", "Manual") {
		t.Errorf("deviation screen does not show manual resolution option:\n%s", view)
	}
}

func TestNavigation_DeviationScreen_EscSendsStopReply(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info:  domain.DeviationInfo{CurrentPhase: "PLANNING"},
		reply: reply,
	})

	// Esc from deviation choice screen -> send stop.
	sendKey(m, tea.KeyEsc)

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceStop {
			t.Errorf("deviation reply choice = %q after Esc, want %q", result.choice, screens.DeviationChoiceStop)
		}
	default:
		t.Error("no reply sent on reply channel after Esc on deviation screen")
	}

	if m.screen != screenProgress {
		t.Errorf("screen = %v after deviation Esc, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// ---------------------------------------------------------------------------
// Question overlay routing
// ---------------------------------------------------------------------------

func TestNavigation_SelectOneQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:    questionSelectOne,
		choiceQ: makeChoiceQuestion("Pick one", []string{"opt-a", "opt-b"}),
		reply:   reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after SelectOne question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.selectOverlay == nil {
		t.Error("selectOverlay = nil after SelectOne question; must be populated")
	}
}

func TestNavigation_SelectOneQuestion_EnterSendsAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:    questionSelectOne,
		choiceQ: makeChoiceQuestion("Pick one", []string{"opt-a", "opt-b"}),
		reply:   reply,
	})

	sendKey(m, tea.KeyEnter)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after Enter on select overlay, want screenProgress (%v)", m.screen, screenProgress)
	}
	select {
	case ans := <-reply:
		if ans.choiceAns.Status != interaction.Answered {
			t.Errorf("answer status = %q, want %q", ans.choiceAns.Status, interaction.Answered)
		}
	default:
		t.Error("no answer sent after Enter on select overlay")
	}
}

func TestNavigation_ConfirmQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:     questionConfirm,
		confirmQ: interaction.Question{Title: "Proceed?", Prompt: "Are you sure?"},
		reply:    reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after Confirm question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.confirmOverlay == nil {
		t.Error("confirmOverlay = nil after Confirm question; must be populated")
	}
}

func TestNavigation_ConfirmQuestion_EnterSendsAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:     questionConfirm,
		confirmQ: interaction.Question{Title: "Proceed?", Prompt: "Are you sure?"},
		reply:    reply,
	})

	sendKey(m, tea.KeyEnter)

	select {
	case ans := <-reply:
		if ans.confirmAns.Status != interaction.Answered {
			t.Errorf("confirm answer status = %q, want %q", ans.confirmAns.Status, interaction.Answered)
		}
	default:
		t.Error("no answer sent after Enter on confirm overlay")
	}
}

// ---------------------------------------------------------------------------
// AskText question overlay
// ---------------------------------------------------------------------------

// TestNavigation_AskTextQuestion_TransitionsToQuestionScreen verifies that a
// questionAskText message transitions the model to screenQuestion and populates
// the textOverlay field.
func TestNavigation_AskTextQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:  questionAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{Title: "Enter value:", Prompt: "type here"}},
		reply: reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after AskText question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.textOverlay == nil {
		t.Error("textOverlay = nil after AskText question; must be populated")
	}
}

// TestNavigation_AskTextQuestion_EnterSendsTextAnswer verifies that pressing Enter on the
// AskText overlay sends a TextAnswer with Status == Answered through the reply channel and
// returns the screen to screenProgress.
func TestNavigation_AskTextQuestion_EnterSendsTextAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:  questionAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{Title: "Enter value:", Prompt: "type here"}},
		reply: reply,
	})

	if m.screen != screenQuestion {
		t.Fatalf("precondition: screen = %v, want screenQuestion", m.screen)
	}

	// Confirm the text overlay.
	sendKey(m, tea.KeyEnter)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after Enter on text overlay, want screenProgress (%v)", m.screen, screenProgress)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered {
			t.Errorf("text answer Status = %q, want %q", ans.textAns.Status, interaction.Answered)
		}
	default:
		t.Error("no text answer sent after Enter on AskText overlay")
	}
}

// ---------------------------------------------------------------------------
// Setup screen back-navigation
// ---------------------------------------------------------------------------

// TestNavigation_WorkflowScreen_EscReturnsToFileScreen verifies that pressing Esc on the
// workflow selection screen transitions back to the orchestrator file entry screen.
func TestNavigation_WorkflowScreen_EscReturnsToFileScreen(t *testing.T) {
	m := newTestModel()
	style := stylesFromTheme(m.theme)
	m.workflowScreen = screens.NewWorkflowSelectScreen(
		[]domain.WorkflowRegion{{Info: domain.WorkflowInfo{ID: "wf1"}}},
		m.width, m.height, style,
	)
	m.screen = screenSetupWorkflow

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupFile {
		t.Errorf("screen = %v after Esc from workflow screen, want screenSetupFile (%v)", m.screen, screenSetupFile)
	}
}

// TestNavigation_TaskScreen_EscReturnsToWorkflowScreen verifies that pressing Esc on the
// task description screen transitions back to the workflow selection screen.
func TestNavigation_TaskScreen_EscReturnsToWorkflowScreen(t *testing.T) {
	m := newTestModel()
	m.screen = screenSetupTask

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupWorkflow {
		t.Errorf("screen = %v after Esc from task screen, want screenSetupWorkflow (%v)", m.screen, screenSetupWorkflow)
	}
}

// TestNavigation_ConfigScreen_EscReturnsToTaskScreen verifies that pressing Esc on the
// first configuration prompt transitions back to the task description screen.
func TestNavigation_ConfigScreen_EscReturnsToTaskScreen(t *testing.T) {
	m := newTestModel()
	m.screen = screenSetupConfig

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupTask {
		t.Errorf("screen = %v after Esc from config screen, want screenSetupTask (%v)", m.screen, screenSetupTask)
	}
}

// ---------------------------------------------------------------------------
// Setup sequence: forward navigation
// ---------------------------------------------------------------------------

// TestSetupSequence_ForwardNavigation_ReachesProgressScreen verifies the complete
// forward-navigation path: workflow selection → task entry → configuration → progress
// screen. The file-selection step is bypassed by pre-populating the model's workflow
// list and jumping directly to the workflow screen.
func TestSetupSequence_ForwardNavigation_ReachesProgressScreen(t *testing.T) {
	m := newTestModel()
	style := stylesFromTheme(m.theme)

	// Bypass file selection: directly populate workflow regions and transition to
	// the workflow screen, replicating what updateSetupFile would do after loading.
	testWorkflows := []domain.WorkflowRegion{
		{Info: domain.WorkflowInfo{ID: "test-workflow"}},
	}
	m.workflows = testWorkflows
	m.workflowScreen = screens.NewWorkflowSelectScreen(testWorkflows, m.width, m.height, style)
	m.screen = screenSetupWorkflow

	// Select the first (and only) workflow.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupTask {
		t.Fatalf("after workflow selection: screen = %v, want screenSetupTask", m.screen)
	}

	// Type one character to satisfy the non-empty task validator, then confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupConfig {
		t.Fatalf("after task entry: screen = %v, want screenSetupConfig", m.screen)
	}

	// Accept all four configuration prompts with their default selections.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // existing artifact
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → transitions to screenProgress

	if m.screen != screenProgress {
		t.Errorf("after config completion: screen = %v, want screenProgress", m.screen)
	}
	if m.progressScreen == nil {
		t.Error("progressScreen = nil after reaching progress screen; must be constructed")
	}
	if m.selections.workflowID != domain.WorkflowID("test-workflow") {
		t.Errorf("workflowID = %q, want %q", m.selections.workflowID, "test-workflow")
	}
	if m.selections.task != "T" {
		t.Errorf("task = %q, want %q", m.selections.task, "T")
	}
}

// ---------------------------------------------------------------------------
// Deviation screen: full manual flow
// ---------------------------------------------------------------------------

// TestNavigation_DeviationScreen_ManualFlow_NoAgent_SendsRejoinAtRow drives all nine
// sub-steps of the manual deviation resolution flow with no agent specified, and
// asserts that the reply channel receives a DeviationChoiceManual result with an empty
// Agent field (triggering the RejoinAtRow path in TUIDeviationResolver.Resolve).
func TestNavigation_DeviationScreen_ManualFlow_NoAgent_SendsRejoinAtRow(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	m.routingTable = domain.RoutingTable{
		Rows: []domain.RoutingRow{{Index: 0, Agent: "exec-agent", Phase: "EXECUTION"}},
	}

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info: domain.DeviationInfo{
			Response:     domain.ProtocolResponse{AgentInstanceID: "a#1", StatusCode: domain.StatusBLOCKED},
			CurrentPhase: "EXECUTION",
		},
		reply: reply,
	})
	if m.screen != screenDeviation {
		t.Fatalf("precondition: screen = %v, want screenDeviation", m.screen)
	}

	// Step 1: choice — press Down to move cursor to "Resolve manually" (index 1), Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Step 2: row selection — cursor starts on first row (exec-agent), Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Steps 3-7: agent, task, inputs, outputs, constraints — accept empty values.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // agent (empty → no custom dispatch)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // task
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // input artifacts
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // output artifacts
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // constraints

	// Step 8: HITL override — cursor=0 (No, keep as-is), Enter → done=true.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenProgress {
		t.Errorf("screen = %v after manual deviation completion, want screenProgress", m.screen)
	}
	if m.deviationScreen != nil {
		t.Error("deviationScreen must be nil after completion")
	}

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceManual {
			t.Errorf("choice = %q, want %q", result.choice, screens.DeviationChoiceManual)
		}
		if result.resolution.Agent != "" {
			t.Errorf("Agent = %q, want empty string (RejoinAtRow path)", result.resolution.Agent)
		}
		if result.resolution.RejoinRowIndex != 0 {
			t.Errorf("RejoinRowIndex = %d, want 0", result.resolution.RejoinRowIndex)
		}
		if result.resolution.HITLOverride != nil {
			t.Errorf("HITLOverride = %v, want nil (no override selected)", result.resolution.HITLOverride)
		}
	default:
		t.Error("no reply received on channel after manual deviation completion")
	}
}

// TestNavigation_DeviationScreen_ManualFlow_WithAgent_SendsCustomDispatch is the same
// as the no-agent test but types a non-empty agent name in step 3, which causes the
// resolution to carry Agent != "" (triggering the CustomDispatch path).
func TestNavigation_DeviationScreen_ManualFlow_WithAgent_SendsCustomDispatch(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	m.routingTable = domain.RoutingTable{
		Rows: []domain.RoutingRow{{Index: 0, Agent: "exec-agent", Phase: "EXECUTION"}},
	}

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info: domain.DeviationInfo{
			Response:     domain.ProtocolResponse{AgentInstanceID: "a#1", StatusCode: domain.StatusBLOCKED},
			CurrentPhase: "EXECUTION",
		},
		reply: reply,
	})

	// Step 1: choose "Resolve manually".
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Step 2: row selection.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Step 3: agent input — type 'R' (non-empty), then Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Steps 4-7: task, inputs, outputs, constraints — empty.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // task
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // inputs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // outputs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // constraints

	// Step 8: HITL — no override.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenProgress {
		t.Errorf("screen = %v after manual deviation with agent, want screenProgress", m.screen)
	}

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceManual {
			t.Errorf("choice = %q, want %q", result.choice, screens.DeviationChoiceManual)
		}
		if result.resolution.Agent != "R" {
			t.Errorf("Agent = %q, want %q (custom dispatch path)", result.resolution.Agent, "R")
		}
	default:
		t.Error("no reply received after manual deviation with agent")
	}
}

// TestNavigation_DeviationScreen_ManualFlow_HITLOverride_SetsValue drives the full manual
// flow and navigates the HITL sub-steps (steps 8-9) to select "Yes (override)" and then
// "true", asserting that the resolved HITLOverride is non-nil and set to true.
func TestNavigation_DeviationScreen_ManualFlow_HITLOverride_SetsValue(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	m.routingTable = domain.RoutingTable{
		Rows: []domain.RoutingRow{{Index: 0, Agent: "exec-agent", Phase: "EXECUTION"}},
	}

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info:  domain.DeviationInfo{CurrentPhase: "EXECUTION"},
		reply: reply,
	})

	// Steps 1-7: navigate to manual choice, select row, and accept all text fields.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // step 1a
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 1b select manual
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 2  select row
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 3  agent (empty)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 4  task
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 5  inputs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 6  outputs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // step 7  constraints

	// Step 8: HITL choice — press Down to move to "Yes (override)" (cursor=1), Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // advance to deviationStepManualHITLValue

	// Step 9: HITL value — press Up to select "true" (default index 1 = false → Up → 0 = true), Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // done = true, HITLOverride = &true

	if m.screen != screenProgress {
		t.Errorf("screen = %v after HITL override deviation, want screenProgress", m.screen)
	}

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceManual {
			t.Errorf("choice = %q, want %q", result.choice, screens.DeviationChoiceManual)
		}
		if result.resolution.HITLOverride == nil {
			t.Fatal("HITLOverride = nil, want non-nil (user selected override)")
		}
		if !*result.resolution.HITLOverride {
			t.Errorf("*HITLOverride = false, want true")
		}
	default:
		t.Error("no reply received after HITL override deviation flow")
	}
}

// ---------------------------------------------------------------------------
// Progress notices
// ---------------------------------------------------------------------------

func TestNavigation_NoticeMsg_UpdatesProgressStatus(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   "agent#1",
			Message: "phase=PLANNING stage=\"\" status=SUCCESS",
		},
	})

	// View should be non-empty (status was updated).
	view := m.progressScreen.View()
	if view == "" {
		t.Error("progress screen view is empty after notice update")
	}
}

// ---------------------------------------------------------------------------
// buildRoutingTable
// ---------------------------------------------------------------------------

// TestNavigation_BuildRoutingTable_ReturnsStoredTable verifies that buildRoutingTable()
// returns whatever routing table was stored on the model during workflow selection.
// This ensures NewDeviationScreen receives real row data, not an empty table.
func TestNavigation_BuildRoutingTable_ReturnsStoredTable(t *testing.T) {
	m := newTestModel()
	m.routingTable = domain.RoutingTable{
		Rows: []domain.RoutingRow{
			{Index: 0, Agent: "planner-agent", Phase: "PLANNING"},
			{Index: 1, Agent: "impl-agent", Phase: "EXECUTION"},
		},
	}

	table := m.buildRoutingTable()

	if len(table.Rows) != 2 {
		t.Fatalf("buildRoutingTable() returned %d rows, want 2", len(table.Rows))
	}
	if table.Rows[0].Agent != "planner-agent" {
		t.Errorf("row 0 agent = %q, want %q", table.Rows[0].Agent, "planner-agent")
	}
	if table.Rows[1].Agent != "impl-agent" {
		t.Errorf("row 1 agent = %q, want %q", table.Rows[1].Agent, "impl-agent")
	}
}

// TestNavigation_BuildRoutingTable_EmptyByDefault verifies that a freshly
// constructed model has an empty routing table before any workflow is selected.
func TestNavigation_BuildRoutingTable_EmptyByDefault(t *testing.T) {
	m := newTestModel()
	table := m.buildRoutingTable()
	if len(table.Rows) != 0 {
		t.Errorf("buildRoutingTable() on fresh model returned %d rows, want 0", len(table.Rows))
	}
}

// TestNavigation_DeviationScreen_RoutingTableRows_AppearsInManualView verifies that
// routing table rows set before a deviation event are visible in the manual row
// selection sub-screen of the deviation screen.
func TestNavigation_DeviationScreen_RoutingTableRows_AppearsInManualView(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	// Pre-populate routing table (as updateSetupWorkflow would after workflow selection).
	m.routingTable = domain.RoutingTable{
		Rows: []domain.RoutingRow{
			{Index: 0, Agent: "routing-test-agent", Phase: "PLANNING"},
		},
	}

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info: domain.DeviationInfo{
			Response:     domain.ProtocolResponse{AgentInstanceID: "x#1", StatusCode: domain.StatusBLOCKED},
			CurrentPhase: "PLANNING",
		},
		reply: reply,
	})

	// Navigate from the choice list to the manual row list:
	// press Down to move cursor to "Resolve manually" (index 1), then Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if !containsStr(view, "routing-test-agent") {
		t.Errorf("manual row-selection view does not contain the routing-table agent name %q:\n%s",
			"routing-test-agent", view)
	}
}

// ---------------------------------------------------------------------------
// Deviation delegate completion
// ---------------------------------------------------------------------------

// TestNavigation_DeviationDelegate_EnterSendsDelegateReply verifies that pressing
// Enter on the default choice (Delegate) sends a DeviationChoiceDelegate reply
// and returns the screen to screenProgress.
func TestNavigation_DeviationDelegate_EnterSendsDelegateReply(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info: domain.DeviationInfo{
			Response: domain.ProtocolResponse{
				AgentInstanceID: "agent#1",
				StatusCode:      domain.StatusBLOCKED,
				StatusMessage:   "test",
			},
			CurrentPhase: "PLANNING",
		},
		reply: reply,
	})

	if m.screen != screenDeviation {
		t.Fatalf("precondition: screen = %v, want screenDeviation", m.screen)
	}

	// The cursor starts on "Delegate to orchestrator" (index 0). Enter selects it.
	sendKey(m, tea.KeyEnter)

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceDelegate {
			t.Errorf("deviation reply = %q, want %q", result.choice, screens.DeviationChoiceDelegate)
		}
	default:
		t.Error("no reply on channel after Enter on deviation choice screen")
	}

	if m.screen != screenProgress {
		t.Errorf("screen = %v after deviation completion, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// TestNavigation_DeviationStop_ChoiceEnterSendsStopReply verifies that selecting
// "Stop the run" (index 2) from the deviation choice list sends DeviationChoiceStop.
func TestNavigation_DeviationStop_ChoiceEnterSendsStopReply(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan deviationReplyMsg, 1)
	m.Update(deviationRequestMsg{
		info:  domain.DeviationInfo{CurrentPhase: "PLANNING"},
		reply: reply,
	})

	// Navigate down twice to "Stop the run" (index 2), then Enter.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	sendKey(m, tea.KeyEnter)

	select {
	case result := <-reply:
		if result.choice != screens.DeviationChoiceStop {
			t.Errorf("deviation reply = %q, want %q", result.choice, screens.DeviationChoiceStop)
		}
	default:
		t.Error("no reply on channel after selecting Stop from deviation choice screen")
	}
}

// ---------------------------------------------------------------------------
// containsAny helper
// ---------------------------------------------------------------------------

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && containsStr(s, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
