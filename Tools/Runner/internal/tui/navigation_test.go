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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
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

	// Accept all five configuration prompts.  The timeout step is always present
	// now that the fake harness has been removed.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must advance to timeout

	// Verify we are at the timeout step (not version drift) before supplying the value.
	// This assertion fails until the fake-harness option is removed.
	configView := m.configScreen.View()
	if !containsAny(configView, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after harness Enter, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", configView)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
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
// Run selection screen
// ---------------------------------------------------------------------------

// makeCandidate creates a RunCandidate with the given runID for test use.
func makeCandidate(runID, folderPath string) runscan.RunCandidate {
	return runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folderPath,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Workflow:    "test-workflow",
			Task:        "test task",
		},
	}
}

// newModelWithScan creates a rootModel with the given scan candidates pre-loaded via Options.
func newModelWithScan(candidates []runscan.RunCandidate) *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		ScanResult: scanResult,
	})
}

// TestRunSelect_InitialScreen_IsRunSelectWithMultipleCandidates verifies that when the
// scan result carries more than one candidate and no run is pre-resolved, the TUI starts
// on the run-selection screen.
func TestRunSelect_InitialScreen_IsRunSelectWithMultipleCandidates(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Errorf("initial screen = %v, want screenRunSelect (%v)", m.screen, screenRunSelect)
	}
	if m.runSelectScreen == nil {
		t.Error("runSelectScreen = nil; must be constructed when multiple candidates exist")
	}
}

// TestRunSelect_InitialScreen_IsSetupFileWithZeroCandidates verifies that an empty scan
// result (no resumable runs) skips the run-selection screen and goes to the file screen.
func TestRunSelect_InitialScreen_IsSetupFileWithZeroCandidates(t *testing.T) {
	m := newModelWithScan(nil)
	if m.screen != screenSetupFile {
		t.Errorf("initial screen = %v, want screenSetupFile (%v) for zero candidates", m.screen, screenSetupFile)
	}
	if m.runSelectScreen != nil {
		t.Error("runSelectScreen should be nil when there are zero candidates")
	}
}

// TestRunSelect_InitialScreen_IsRunSelectWithOneCandidate is the TUI-side
// expression of the core defect this stage removes (AC2.2): a workspace with
// exactly one resumable run must reach the run-selection screen, not bypass
// it. The screen renders "start a new run" plus the single candidate, and
// the user's choice (not the candidate count) decides the outcome.
//
// Currently fails (RED): the TUI still skips the screen for a single
// candidate (the `len(...) > 1` gate in newRootModel).
func TestRunSelect_InitialScreen_IsRunSelectWithOneCandidate(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Errorf("initial screen = %v, want screenRunSelect (%v) for a single candidate; "+
			"exactly one resumable run must not bypass the selection screen", m.screen, screenRunSelect)
	}
	if m.runSelectScreen == nil {
		t.Error("runSelectScreen = nil; must be constructed when exactly one candidate exists")
	}
}

// TestRunSelect_SkippedWhenResolvedRunIDSet verifies that when ResolvedRunID is populated
// (i.e. --run was given), the TUI starts directly on the file screen.
func TestRunSelect_SkippedWhenResolvedRunIDSet(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:         tuicommon.DefaultTheme(),
		ScanResult:    &runscan.ScanResult{Candidates: candidates},
		ResolvedRunID: "20260701T120000Z-a3f9",
	})
	if m.screen != screenSetupFile {
		t.Errorf("initial screen = %v, want screenSetupFile (%v) when ResolvedRunID is set", m.screen, screenSetupFile)
	}
}

// TestRunSelect_SkippedWhenIsNewRunSet verifies that when IsNewRun is true
// (i.e. --new-run was given), the TUI starts directly on the file screen.
func TestRunSelect_SkippedWhenIsNewRunSet(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		ScanResult: &runscan.ScanResult{Candidates: candidates},
		IsNewRun:   true,
	})
	if m.screen != screenSetupFile {
		t.Errorf("initial screen = %v, want screenSetupFile (%v) when IsNewRun is set", m.screen, screenSetupFile)
	}
}

// TestRunSelect_ViewShowsCandidates verifies that the run-selection screen view contains
// each candidate's run_id and the "Start a new run" entry.
func TestRunSelect_ViewShowsCandidates(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}
	view := m.View()
	if !containsStr(view, "20260701T120000Z-a3f9") {
		t.Errorf("run select view does not contain first candidate run_id:\n%s", view)
	}
	if !containsStr(view, "20260702T120000Z-b4e8") {
		t.Errorf("run select view does not contain second candidate run_id:\n%s", view)
	}
	if !containsAny(view, "Start", "new run", "new") {
		t.Errorf("run select view does not contain 'Start a new run' entry:\n%s", view)
	}
}

// TestRunSelect_EscQuitsProgram verifies that Esc from the run-selection screen issues
// a quit command (there is no previous screen to go back to).
func TestRunSelect_EscQuitsProgram(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	_, cmd := sendKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Error("cmd = nil after Esc from run-selection screen; want tea.Quit (non-nil)")
	}
}

// TestRunSelect_EnterOnNewRun_SetsIsNewRunAndAdvances verifies that pressing Enter on
// the "Start a new run" entry (the first item, which is always selected initially) sets
// isNewRun=true and transitions to the file screen.
func TestRunSelect_EnterOnNewRun_SetsIsNewRunAndAdvances(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// The first item is "Start a new run" (NewRunSentinelID). Press Enter to select it.
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupFile {
		t.Errorf("screen = %v after selecting 'Start new run', want screenSetupFile (%v)", m.screen, screenSetupFile)
	}
	if !m.selections.isNewRun {
		t.Error("selections.isNewRun = false after selecting 'Start new run'; want true")
	}
}

// TestRunSelect_EnterOnCandidate_SetsRunIDAndAdvances verifies that pressing Enter on
// a candidate entry populates the run identity and transitions to the file screen.
func TestRunSelect_EnterOnCandidate_SetsRunIDAndAdvances(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Navigate down once to move past "Start a new run" to the first candidate.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupFile {
		t.Errorf("screen = %v after selecting candidate, want screenSetupFile (%v)", m.screen, screenSetupFile)
	}
	if m.selections.isNewRun {
		t.Error("selections.isNewRun = true after selecting an existing candidate; want false")
	}
	if m.selections.runID != "20260701T120000Z-a3f9" {
		t.Errorf("selections.runID = %q, want %q", m.selections.runID, "20260701T120000Z-a3f9")
	}
	if m.selections.runFolder == "" {
		t.Error("selections.runFolder is empty after selecting candidate; want the candidate's folder path")
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen: ExistingArtifact step removed; fake harness removed
// ---------------------------------------------------------------------------

// TestConfigScreen_PromptsCount verifies that the ConfigScreen presents exactly five
// prompts: deviation handling, harness selection, invocation timeout, version drift,
// and checkpoints.  The timeout step is always present now that the fake harness has
// been removed from the harness step.
func TestConfigScreen_PromptsCount(t *testing.T) {
	m := newTestModel()
	m.screen = screenSetupConfig

	// Step 1: accept deviation mode.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Step 2: accept harness selection — must advance to the timeout step, not version drift.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// After deviation + harness Enters, the config screen must be showing the
	// invocation-timeout entry step.  This assertion fails until the fake-harness
	// option is removed and advance() always proceeds to configStepHarnessTimeout.
	configView := m.configScreen.View()
	if !containsAny(configView, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after deviation+harness Enters, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", configView)
	}

	// Step 3: enter a valid invocation timeout and confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Steps 4–5: version drift and checkpoints.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → progress

	if m.screen != screenProgress {
		t.Errorf("screen = %v after five config steps, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen harness step: single option, no fake/scripted text (T2.1, T2.2)
// ---------------------------------------------------------------------------

// TestConfigScreen_HarnessStep_NoFakeOrScriptedText verifies that the harness
// step's rendered output contains no reference to the fake or scripted harness.
func TestConfigScreen_HarnessStep_NoFakeOrScriptedText(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	// Advance past deviation to reach the harness step.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := s.View()
	if containsAny(view, "fake", "Fake", "scripted", "Scripted") {
		t.Errorf("harness step view contains fake/scripted harness reference; want no such mention:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_ContainsRealHarnessOption verifies that the harness
// step shows the real harness option.
func TestConfigScreen_HarnessStep_ContainsRealHarnessOption(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation → harness step

	view := s.View()
	if !containsAny(view, "Claude Code CLI", "Claude Code", "claude-code") {
		t.Errorf("harness step view does not contain real harness option:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_CursorPinnedAtZero verifies that pressing Down on
// the harness step does not move the cursor, because there is only one option.
func TestConfigScreen_HarnessStep_CursorPinnedAtZero(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation → harness step

	viewBefore := s.View()
	s.Update(tea.KeyMsg{Type: tea.KeyDown}) // attempt to advance cursor
	viewAfter := s.View()

	if viewBefore != viewAfter {
		t.Errorf("pressing Down on the harness step changed the view; cursor must be pinned at 0 "+
			"(only one option exists):\nbefore:\n%s\nafter:\n%s", viewBefore, viewAfter)
	}
}

// TestConfigScreen_HarnessStep_AdvancesToTimeoutStep verifies that confirming the
// harness step transitions to the invocation-timeout entry step.
func TestConfigScreen_HarnessStep_AdvancesToTimeoutStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must go to timeout

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Errorf("after confirming harness step, expected invocation-timeout step; "+
			"fake harness may still be routing to version drift instead:\n%s", view)
	}
	if containsAny(view, "version drift", "Version drift", "drift") {
		t.Errorf("after confirming harness step, view shows version-drift content instead of timeout step:\n%s", view)
	}
}

// TestConfigScreen_HarnessStep_SelectionIsAlwaysClaudeCode verifies that after
// driving through the full config screen, Selection().Harness is "claude-code".
func TestConfigScreen_HarnessStep_SelectionIsAlwaysClaudeCode(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → timeout
	// Confirm a valid invocation timeout.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints → done

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps; cannot verify Selection()")
	}
	sel := s.Selection()
	if sel.Harness != "claude-code" {
		t.Errorf("Selection().Harness = %q, want %q; the harness step must always produce claude-code",
			sel.Harness, "claude-code")
	}
}

// ---------------------------------------------------------------------------
// ConfigScreen Esc backward navigation across harness/timeout/version-drift (T2.4)
// ---------------------------------------------------------------------------

// TestConfigScreen_EscBackNavigation_VersionDriftToTimeoutStep verifies that pressing
// Esc from the version-drift step returns to the invocation-timeout step, not the
// harness step.  This exercises the removal of the special-case Esc branch that
// existed only because the timeout step was skipped for the fake harness.
func TestConfigScreen_EscBackNavigation_VersionDriftToTimeoutStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	// Advance deviation → harness.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Confirm harness — must go to timeout step, not version drift.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("precondition: after harness Enter, expected timeout step; "+
			"fake harness skipping may still be present:\n%s", view)
	}

	// Type a valid timeout and advance to version drift.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift

	// Esc from version drift must return to the timeout step, not harness.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view = s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Errorf("Esc from version-drift did not return to timeout step; "+
			"the special-case harness branch may still be routing back to harness:\n%s", view)
	}
}

// TestConfigScreen_EscBackNavigation_TimeoutToHarnessStep verifies that pressing
// Esc from the invocation-timeout step returns to the harness step.
func TestConfigScreen_EscBackNavigation_TimeoutToHarnessStep(t *testing.T) {
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewConfigScreen(80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // deviation
	s.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → timeout

	view := s.View()
	if !containsAny(view, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("precondition: expected timeout step after harness Enter; view:\n%s", view)
	}

	// Esc from timeout → harness.
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view = s.View()
	if !containsAny(view, "Harness", "harness", "adapter", "Adapter") {
		t.Errorf("Esc from timeout step did not return to harness step; view:\n%s", view)
	}
	if s.Back() {
		t.Error("Back() = true after Esc from timeout to harness; must not exit the config screen")
	}
}

// ---------------------------------------------------------------------------
// Run selection screen: RunSelectScreen screen type
// ---------------------------------------------------------------------------

// TestRunSelectScreen_NewRunSentinelID verifies that the sentinel constant is defined
// and has the expected value.
func TestRunSelectScreen_NewRunSentinelID(t *testing.T) {
	const wantID = "__new_run__"
	if screens.NewRunSentinelID != wantID {
		t.Errorf("NewRunSentinelID = %q, want %q", screens.NewRunSentinelID, wantID)
	}
}

// makeResumeChoice builds a selectable runselect.Choice{Kind: ChoiceResume} for
// runID, carrying the same fixture metadata as makeCandidate.
func makeResumeChoice(runID string) runselect.Choice {
	return runselect.Choice{
		ID:         runID,
		Kind:       runselect.ChoiceResume,
		Run:        makeCandidate(runID, "/ws/Orchestration-"+runID).RunInfo,
		Selectable: true,
	}
}

// makeUnresumableChoice builds a non-selectable runselect.Choice{Kind: ChoiceUnresumable}
// for runID, carrying reason.
func makeUnresumableChoice(runID string, reason runscan.UnresumableReason) runselect.Choice {
	return runselect.Choice{
		ID:   runID,
		Kind: runselect.ChoiceUnresumable,
		Run: runscan.RunInfo{
			RunID:      runID,
			FolderPath: "/ws/Orchestration-" + runID,
		},
		Selectable: false,
		Reason:     reason,
	}
}

// newRunChoiceFixture is the new-run Choice every runselect.Question carries first.
func newRunChoiceFixture() runselect.Choice {
	return runselect.Choice{ID: runselect.NewRunChoiceID, Kind: runselect.ChoiceNewRun, Selectable: true}
}

// TestRunSelectScreen_IsNewRun_TrueOnFirstEntry verifies that the newly constructed
// RunSelectScreen has "Start a new run" as the first (selected) item, so IsNewRun()
// returns true without any navigation.
func TestRunSelectScreen_IsNewRun_TrueOnFirstEntry(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	// Simulate selection (Enter) without navigating.
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter; want true")
	}
	if !s.IsNewRun() {
		t.Error("IsNewRun() = false when first item was selected; want true (first item is 'Start new run')")
	}
	if s.SelectedChoiceID() != screens.NewRunSentinelID {
		t.Errorf("SelectedChoiceID() = %q when IsNewRun() is true; want %q", s.SelectedChoiceID(), screens.NewRunSentinelID)
	}
}

// TestRunSelectScreen_SelectedChoiceID_AfterNavigation verifies that navigating to a
// resumable choice and pressing Enter sets SelectedChoiceID correctly.
func TestRunSelectScreen_SelectedChoiceID_AfterNavigation(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	// Navigate past "Start new run" to the candidate.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on candidate; want true")
	}
	if s.IsNewRun() {
		t.Error("IsNewRun() = true after selecting candidate; want false")
	}
	if s.SelectedChoiceID() != "20260701T120000Z-a3f9" {
		t.Errorf("SelectedChoiceID() = %q, want %q", s.SelectedChoiceID(), "20260701T120000Z-a3f9")
	}
}

// TestRunSelectScreen_Back_TrueOnEsc verifies that pressing Esc sets Back() to true.
func TestRunSelectScreen_Back_TrueOnEsc(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Back() {
		t.Error("Back() = false after Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Run selection screen: unresumable choices (T2.4 / AC2.5, TUI side)
// ---------------------------------------------------------------------------

// TestRunSelectScreen_UnresumableChoice_RenderedWithReason verifies that an
// unresumable choice appears in the rendered list, carrying the reason it
// cannot be resumed (runscan.UnresumableReason.Description()).
//
// Currently fails (RED): NewRunSelectScreen does not yet render
// ChoiceUnresumable entries at all (see the RED placeholder comment on
// NewRunSelectScreen in screens/setup.go); the reason text is absent from
// the view entirely, not merely missing a specific word.
func TestRunSelectScreen_UnresumableChoice_RenderedWithReason(t *testing.T) {
	unresumableID := "20260601T090000Z-1234"
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeResumeChoice("20260701T120000Z-a3f9"),
		makeUnresumableChoice(unresumableID, runscan.ReasonCompleted),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	view := s.View()
	if !containsAny(view, unresumableID) {
		t.Errorf("unresumable run %q does not appear in the rendered view at all; it must be shown, not hidden.\nview:\n%s", unresumableID, view)
	}
	if !containsAny(view, runscan.ReasonCompleted.Description()) {
		t.Errorf("unresumable run's reason (%q) does not appear in the rendered view.\nview:\n%s", runscan.ReasonCompleted.Description(), view)
	}
}

// TestRunSelectScreen_UnresumableChoice_CannotBeActivated verifies that
// pressing Enter while the cursor would land on an unresumable choice is a
// no-op: cursor movement skips non-selectable items entirely (mirroring
// widgets.List's existing Disabled-item behaviour), so Done() never becomes
// true by landing on one.
//
// Currently fails (RED): the unresumable choice is not in the list at all
// (see TestRunSelectScreen_UnresumableChoice_RenderedWithReason), so this
// test cannot observe the intended "present but skipped" behaviour; as
// written it exercises the actually-reachable items only and documents the
// contract cursor movement must satisfy once I2.4 adds them.
func TestRunSelectScreen_UnresumableChoice_CannotBeActivated(t *testing.T) {
	unresumableID := "20260601T090000Z-1234"
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeUnresumableChoice(unresumableID, runscan.ReasonCompleted),
		makeResumeChoice("20260701T120000Z-a3f9"),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	// Move down once: with the unresumable entry present and non-selectable,
	// the cursor must skip it and land on the resumable candidate, not on the
	// unresumable entry.
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Fatal("Done() = false after Enter on the item following 'start new run'; want true (a selectable item was reachable)")
	}
	if s.SelectedChoiceID() == unresumableID {
		t.Errorf("SelectedChoiceID() = %q; the unresumable choice must never be selectable", unresumableID)
	}
	if s.SelectedChoiceID() != "20260701T120000Z-a3f9" {
		t.Errorf("SelectedChoiceID() = %q, want %q (cursor must skip the non-selectable unresumable entry)",
			s.SelectedChoiceID(), "20260701T120000Z-a3f9")
	}
}

// TestRunSelectScreen_NewRunReachable_WithUnresumableEntriesPresent verifies
// that "start a new run" remains the first, immediately activatable item
// even when the question also carries unresumable entries -- not just when
// every choice is resumable, which is the only shape the pre-existing
// reachability tests in this file exercise.
func TestRunSelectScreen_NewRunReachable_WithUnresumableEntriesPresent(t *testing.T) {
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		makeUnresumableChoice("20260601T090000Z-1234", runscan.ReasonCompleted),
		makeUnresumableChoice("20260602T090000Z-5678", runscan.ReasonCompleted),
	}}
	style := stylesFromTheme(tuicommon.DefaultTheme())
	s := screens.NewRunSelectScreen(q, 80, 24, style)

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Fatal("Done() = false after Enter on the first item; want true")
	}
	if !s.IsNewRun() {
		t.Error("IsNewRun() = false; want true -- 'start a new run' must be reachable and activatable " +
			"even when the question also contains only unresumable entries besides it")
	}
}

// ---------------------------------------------------------------------------
// Run selection screen: new-run identity minting (Stage 2 / T2.1)
// ---------------------------------------------------------------------------

// fixedMintedRunID and fixedMintedRunFolder are the deterministic values returned
// by fixedMinter(). They satisfy domain.IsValidRunID and the scoped-folder naming
// convention, making T2.1/T2.2 assertions precise and reproducible.
const (
	fixedMintedRunID     = "20260801T120000Z-ab12"
	fixedMintedRunFolder = "/test/ws/Orchestration-20260801T120000Z-ab12"
)

// fixedMinter returns a RunIdentityMinter that always yields the fixed pair
// (fixedMintedRunID, fixedMintedRunFolder). Use wherever tests must assert on
// exact minted values rather than only checking validity.
func fixedMinter() RunIdentityMinter {
	return func() (string, string) {
		return fixedMintedRunID, fixedMintedRunFolder
	}
}

// factoryCall records the arguments received by a capturing session factory on
// a single invocation.
type factoryCall struct {
	runFolder string
	isNewRun  bool
}

// newModelWithScanMinterFactory creates a rootModel pre-wired with:
//   - a multi-candidate scan result (shows the run-select screen),
//   - an injectable RunIdentityMinter, and
//   - a session factory that records each call in *calls and always returns capSess.
//
// This lets T2.1 tests drive the run-select "new run" path and inspect both
// selections state and factory call arguments.
func newModelWithScanMinterFactory(
	candidates []runscan.RunCandidate,
	minter RunIdentityMinter,
	calls *[]factoryCall,
	capSess *capturingSession,
) *rootModel {
	scanResult := &runscan.ScanResult{Candidates: candidates}
	return newRootModel(context.Background(), capSess, Options{
		Theme:           tuicommon.DefaultTheme(),
		ScanResult:      scanResult,
		MintRunIdentity: minter,
		SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
			*calls = append(*calls, factoryCall{runFolder: runFolder, isNewRun: isNewRun})
			return capSess
		},
	})
}

// TestRunSelect_NewRun_WithMinter_PopulatesCanonicalRunID verifies that when
// MintRunIdentity is provided and the user picks "new run" on the run-select screen,
// the minted run ID is written to selections.runID in canonical format.
//
// In RED: updateRunSelect does not call the minter, so selections.runID stays "".
// In GREEN: the minter is called and selections.runID == fixedMintedRunID.
func TestRunSelect_NewRun_WithMinter_PopulatesCanonicalRunID(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// "Start a new run" is the first item on the run-select list. Enter selects it.
	sendKey(m, tea.KeyEnter)

	if m.selections.runID == "" {
		t.Error("selections.runID is empty after 'new run' with minter; want minted run ID")
	}
	if m.selections.runID != fixedMintedRunID {
		t.Errorf("selections.runID = %q, want minted value %q", m.selections.runID, fixedMintedRunID)
	}
	if !domain.IsValidRunID(m.selections.runID) {
		t.Errorf("selections.runID %q does not satisfy domain.IsValidRunID", m.selections.runID)
	}
}

// TestRunSelect_NewRun_WithMinter_PopulatesMintedRunFolder verifies that when
// MintRunIdentity is provided and the user picks "new run", the minted run folder
// is written to selections.runFolder.
//
// In RED: minter is not called; selections.runFolder stays "".
// In GREEN: selections.runFolder == fixedMintedRunFolder.
func TestRunSelect_NewRun_WithMinter_PopulatesMintedRunFolder(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	if m.selections.runFolder == "" {
		t.Error("selections.runFolder is empty after 'new run' with minter; want minted run folder")
	}
	if m.selections.runFolder != fixedMintedRunFolder {
		t.Errorf("selections.runFolder = %q, want minted value %q", m.selections.runFolder, fixedMintedRunFolder)
	}
}

// TestRunSelect_NewRun_WithMinter_SessionFactoryCalledWithMintedFolder verifies that
// when MintRunIdentity is provided and the user picks "new run", the session factory is
// called with the minted run folder — not with an empty string.
//
// In RED: factory is called with "" (current code passes "" for new runs).
// In GREEN: factory is called with fixedMintedRunFolder.
func TestRunSelect_NewRun_WithMinter_SessionFactoryCalledWithMintedFolder(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	// The session factory must have been called at least once for the "new run" selection.
	if len(calls) == 0 {
		t.Fatal("session factory was not called after 'new run' selection; want at least one call")
	}
	// Find the new-run factory call and verify its run folder argument.
	var found bool
	for _, c := range calls {
		if c.isNewRun {
			found = true
			if c.runFolder != fixedMintedRunFolder {
				t.Errorf("session factory (isNewRun=true) received runFolder = %q, want minted %q; "+
					"an empty folder must never reach the factory when a minter is provided",
					c.runFolder, fixedMintedRunFolder)
			}
		}
	}
	if !found {
		t.Error("no session factory call with isNewRun=true found; " +
			"factory must be called when 'new run' is selected on the run-select screen")
	}
}

// TestRunSelect_NewRun_NilMinter_PreservesEmptySelections verifies that when
// MintRunIdentity is nil (the legacy/test path), selecting "new run" on the
// run-select screen leaves selections.runID and selections.runFolder empty,
// preserving backward-compatible behaviour and not panicking.
//
// This test passes in both RED and GREEN: it guards against accidental regression
// where a nil-minter check is removed and the code panics on nil dereference.
func TestRunSelect_NewRun_NilMinter_PreservesEmptySelections(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	// newModelWithScan does not set MintRunIdentity; field is nil.
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	if m.selections.runID != "" {
		t.Errorf("selections.runID = %q, want empty string (nil minter must preserve legacy empty-selection behaviour)",
			m.selections.runID)
	}
	if m.selections.runFolder != "" {
		t.Errorf("selections.runFolder = %q, want empty string (nil minter must preserve legacy empty-selection behaviour)",
			m.selections.runFolder)
	}
	if !m.selections.isNewRun {
		t.Error("selections.isNewRun = false after 'new run' selection; want true")
	}
}

// TestRunSelect_Resume_WithMinter_MinterNotCalled verifies that when the user selects
// an existing candidate on the run-select screen (the resume path), the MintRunIdentity
// function is never invoked — minting is exclusive to the "new run" branch.
func TestRunSelect_Resume_WithMinter_MinterNotCalled(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	mintCalled := false
	countingMinter := RunIdentityMinter(func() (string, string) {
		mintCalled = true
		return fixedMintedRunID, fixedMintedRunFolder
	})
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, countingMinter, &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Navigate down once to move past "Start a new run" to the first candidate, then select.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	if mintCalled {
		t.Error("minter was called when a candidate was selected (resume path); " +
			"minting must only occur in the 'new run' branch")
	}
	if m.selections.runID != "20260701T120000Z-a3f9" {
		t.Errorf("selections.runID = %q, want candidate run ID %q (resume path must use candidate identity)",
			m.selections.runID, "20260701T120000Z-a3f9")
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
