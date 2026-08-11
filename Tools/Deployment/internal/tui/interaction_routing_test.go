package tui

// interaction_routing_test.go verifies that the root model correctly routes each question
// kind to its dedicated screen overlay and that non-blocking events (Notice, Progress) update
// the status message without showing an overlay. Tests are in package tui (internal) so they
// can inspect unexported rootModel fields.

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Stub service for routing tests
// ---------------------------------------------------------------------------

type stubRoutingService struct{}

func (s *stubRoutingService) ListHarnesses() []domain.HarnessRef { return nil }
func (s *stubRoutingService) DeployNew(_ context.Context, _ app.DeployRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *stubRoutingService) Update(_ context.Context, _ app.UpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *stubRoutingService) Promote(_ context.Context, _ app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}
func (s *stubRoutingService) UpdateWorkflows(_ context.Context, _ app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *stubRoutingService) TransformHarness(_ context.Context, _ app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return app.TransformHarnessResult{}, nil
}

func (s *stubRoutingService) DeployUtilityInfrastructure(_ context.Context, _ app.UtilityInfraRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}

// newRoutingModel returns a rootModel in screenRunning state (simulating mid-flow) with a
// nil service, since routing tests only exercise the question overlay system.
func newRoutingModel() *rootModel {
	svc := &stubRoutingService{}
	m := newRootModel(context.Background(), svc, Options{Theme: DefaultTheme()})
	// Advance to screenRunning to simulate the state where questions arrive.
	m.screen = screenRunning
	return m
}

// ---------------------------------------------------------------------------
// Question routing helpers
// ---------------------------------------------------------------------------

// buildSelectOneMsg constructs a questionMsg for a SelectOne question with the given ID.
func buildSelectOneMsg(id domain.QuestionID, options []domain.Option) questionMsg {
	return questionMsg{
		kind: questionSelectOne,
		choiceQ: domain.ChoiceQuestion{
			Question: domain.Question{
				ID:    id,
				Title: "Test question",
			},
			Options: options,
		},
		reply: make(chan answerMsg, 1),
	}
}

// buildTextMsg constructs a questionMsg for an AskText question.
func buildTextMsg(id domain.QuestionID) questionMsg {
	return questionMsg{
		kind: questionAskText,
		textQ: domain.TextQuestion{
			Question: domain.Question{
				ID:    id,
				Title: "Enter a value",
			},
		},
		reply: make(chan answerMsg, 1),
	}
}

// buildReviewMsg constructs a questionMsg for a Review call.
func buildReviewMsg(p domain.Plan) questionMsg {
	return questionMsg{
		kind:  questionReview,
		plan:  p,
		reply: make(chan answerMsg, 1),
	}
}

// buildConfirmMsg constructs a questionMsg for a Confirm call.
func buildConfirmMsg(id domain.QuestionID) questionMsg {
	return questionMsg{
		kind: questionConfirm,
		confirmQ: domain.Question{
			ID:    id,
			Title: "Confirm?",
		},
		reply: make(chan answerMsg, 1),
	}
}

// ---------------------------------------------------------------------------
// QTierModel routing (I22.1, I22.2)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QTierModel_ShowsModelSelectScreen verifies that when the app layer
// sends a QTierModel question, the root model switches to screenQuestion and activates the
// ModelSelectScreen overlay (not the generic inlineSelectOne).
func TestInteractionRouting_QTierModel_ShowsModelSelectScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTierModel, []domain.Option{{ID: "gpt-4o", Label: "GPT-4o"}})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QTierModel; want screenQuestion", m.screen)
	}
	if m.modelOverlay == nil {
		t.Error("modelOverlay is nil after QTierModel; want a ModelSelectScreen")
	}
	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QTierModel; must use modelOverlay instead")
	}
}

// TestInteractionRouting_QTierModel_ViewDelegatesToModelOverlay verifies that the root
// model's View() returns the ModelSelectScreen's content when active.
func TestInteractionRouting_QTierModel_ViewDelegatesToModelOverlay(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTierModel, []domain.Option{{ID: "gpt-4o", Label: "GPT-4o"}})
	m.Update(qMsg)

	view := m.View()

	if !strings.Contains(view, "GPT-4o") {
		t.Errorf("View() after QTierModel does not show model option label 'GPT-4o':\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// QAgentModel routing (I22.1, I22.3)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QAgentModel_ShowsModelSelectScreen verifies that QAgentModel also
// routes to the ModelSelectScreen overlay.
func TestInteractionRouting_QAgentModel_ShowsModelSelectScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QAgentModel, []domain.Option{{ID: "claude-3-opus", Label: "claude-3-opus"}})

	m.Update(qMsg)

	if m.modelOverlay == nil {
		t.Error("modelOverlay is nil after QAgentModel; want a ModelSelectScreen")
	}
}

// ---------------------------------------------------------------------------
// QLocalModification routing (I22.1, I22.6)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QLocalModification_ShowsConflictScreen verifies that a
// QLocalModification SelectOne question routes to the ConflictScreen overlay.
func TestInteractionRouting_QLocalModification_ShowsConflictScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QLocalModification, []domain.Option{
		{ID: string(domain.DecisionOverwrite), Label: "Overwrite"},
		{ID: string(domain.DecisionSkip), Label: "Skip"},
	})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QLocalModification; want screenQuestion", m.screen)
	}
	if m.conflictOverlay == nil {
		t.Error("conflictOverlay is nil after QLocalModification; want a ConflictScreen")
	}
	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QLocalModification; must use conflictOverlay")
	}
}

// ---------------------------------------------------------------------------
// AskText routing (I22.1, I22.5)
// ---------------------------------------------------------------------------

// TestInteractionRouting_AskText_ShowsTextPromptScreen verifies that AskText questions
// (QCustomTool, QCustomModel) route to the TextPromptScreen overlay, replacing the prior
// behaviour of immediately returning SkippedOne.
func TestInteractionRouting_AskText_ShowsTextPromptScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildTextMsg(domain.QCustomTool)

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after AskText; want screenQuestion", m.screen)
	}
	if m.textOverlay == nil {
		t.Error("textOverlay is nil after AskText; want a TextPromptScreen")
	}
}

// ---------------------------------------------------------------------------
// Review routing (I22.1, I22.7)
// ---------------------------------------------------------------------------

// TestInteractionRouting_Review_ShowsReviewScreen verifies that a Review() call routes to
// the ReviewScreen overlay (not the inlineConfirm fallback used in earlier stages).
func TestInteractionRouting_Review_ShowsReviewScreen(t *testing.T) {
	m := newRoutingModel()
	plan := domain.Plan{
		Mode:          domain.ModeDeployNew,
		WorkspacePath: "/ws",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, Action: domain.ActionCreate},
		},
	}
	qMsg := buildReviewMsg(plan)

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after Review; want screenQuestion", m.screen)
	}
	if m.reviewOverlay == nil {
		t.Error("reviewOverlay is nil after Review; want a ReviewScreen")
	}
	if m.confirmOverlay != nil {
		t.Error("confirmOverlay is non-nil after Review; must use reviewOverlay, not confirmOverlay")
	}
}

// ---------------------------------------------------------------------------
// Confirm routing (non-specialised questions)
// ---------------------------------------------------------------------------

// TestInteractionRouting_Confirm_ShowsConfirmOverlay verifies that Confirm questions (other
// than Review) route to the inlineConfirm overlay.
func TestInteractionRouting_Confirm_ShowsConfirmOverlay(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildConfirmMsg(domain.QExternalOptIn)

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after Confirm; want screenQuestion", m.screen)
	}
	if m.confirmOverlay == nil {
		t.Error("confirmOverlay is nil after Confirm; want an inlineConfirm")
	}
}

// ---------------------------------------------------------------------------
// Generic SelectOne fallback (non-specialised IDs)
// ---------------------------------------------------------------------------

// TestInteractionRouting_GenericSelectOne_UsesInlineOverlay verifies that SelectOne questions
// with unspecialised IDs (e.g., QHarness, QWorkflows) fall through to the generic
// inlineSelectOne overlay.
func TestInteractionRouting_GenericSelectOne_UsesInlineOverlay(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QHarness, []domain.Option{{ID: "claude-code", Label: "Claude Code"}})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after generic SelectOne; want screenQuestion", m.screen)
	}
	if m.selectOverlay == nil {
		t.Error("selectOverlay is nil for a generic SelectOne; want inlineSelectOne")
	}
	if m.modelOverlay != nil {
		t.Error("modelOverlay is non-nil for QHarness; must use selectOverlay for non-model questions")
	}
}

// ---------------------------------------------------------------------------
// Non-blocking events (I22.9)
// ---------------------------------------------------------------------------

// TestInteractionRouting_Notice_UpdatesStatusMsg verifies that a Notice message updates the
// status message without showing an overlay or changing the screen.
func TestInteractionRouting_Notice_UpdatesStatusMsg(t *testing.T) {
	m := newRoutingModel()
	initial := m.screen
	m.Update(questionMsg{
		kind: questionNotice,
		notice: domain.Notice{
			Level:   domain.NoticeInfo,
			Title:   "Transform",
			Message: "Transforming test-runner…",
		},
	})

	if m.screen != initial {
		t.Errorf("screen changed from %v to %v after Notice; must not change", initial, m.screen)
	}
	if !strings.Contains(m.statusMsg, "Transform") {
		t.Errorf("statusMsg = %q after Notice; want it to contain 'Transform'", m.statusMsg)
	}
	if m.selectOverlay != nil || m.confirmOverlay != nil || m.textOverlay != nil {
		t.Error("an overlay was activated by a Notice message; Notice must never show an overlay")
	}
}

// TestInteractionRouting_Progress_UpdatesStatusMsg verifies that a Progress event updates
// the status message and includes phase, current, and total counts.
func TestInteractionRouting_Progress_UpdatesStatusMsg(t *testing.T) {
	m := newRoutingModel()
	m.Update(questionMsg{
		kind: questionProgress,
		progress: domain.ProgressEvent{
			Phase:   "transforming",
			Current: 3,
			Total:   10,
			Subject: "test-runner",
		},
	})

	if !strings.Contains(m.statusMsg, "transforming") {
		t.Errorf("statusMsg = %q; want it to contain 'transforming'", m.statusMsg)
	}
	if !strings.Contains(m.statusMsg, "3") || !strings.Contains(m.statusMsg, "10") {
		t.Errorf("statusMsg = %q; want it to show the progress counts (3/10)", m.statusMsg)
	}
}

// TestInteractionRouting_Progress_NoTotal_ShowsPhaseAndSubject verifies that when Total is
// zero, the progress status still shows the phase and subject without a divide-by-zero panic.
func TestInteractionRouting_Progress_NoTotal_ShowsPhaseAndSubject(t *testing.T) {
	m := newRoutingModel()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Progress with Total=0 caused a panic: %v", r)
		}
	}()

	m.Update(questionMsg{
		kind: questionProgress,
		progress: domain.ProgressEvent{
			Phase:   "planning",
			Total:   0,
			Subject: "collecting artifacts",
		},
	})

	if !strings.Contains(m.statusMsg, "planning") {
		t.Errorf("statusMsg = %q; want it to contain 'planning'", m.statusMsg)
	}
}

// ---------------------------------------------------------------------------
// Answer routing: overlay produces answer, reply channel receives it
// ---------------------------------------------------------------------------

// TestInteractionRouting_ModelSelect_AnswerRoutedToReplyChannel verifies the end-to-end
// dispatch: when the ModelSelectScreen receives an Enter key, the answer is sent to the
// question's reply channel so the app goroutine can unblock.
func TestInteractionRouting_ModelSelect_AnswerRoutedToReplyChannel(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTierModel, []domain.Option{{ID: "gpt-4o", Label: "GPT-4o"}})
	m.Update(qMsg) // installs the ModelSelectScreen

	// Press Enter to select the first option.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The overlay should now be done and the screen should return to running.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after answering model question; want screenRunning", m.screen)
	}
	if m.modelOverlay != nil {
		t.Error("modelOverlay is non-nil after answering; want nil (overlay was dismissed)")
	}

	// The reply channel should have received the answer.
	select {
	case ans := <-qMsg.reply:
		if ans.choiceAns.Status != domain.Answered {
			t.Errorf("reply channel received Status=%q, want Answered", ans.choiceAns.Status)
		}
		if ans.choiceAns.OptionID != "gpt-4o" {
			t.Errorf("reply channel received OptionID=%q, want %q", ans.choiceAns.OptionID, "gpt-4o")
		}
	default:
		t.Error("reply channel has no message; the answer was not sent after Enter")
	}
}

// TestInteractionRouting_TextPrompt_AnswerRoutedToReplyChannel verifies that the
// TextPromptScreen answer is sent to the reply channel when the user confirms text input.
func TestInteractionRouting_TextPrompt_AnswerRoutedToReplyChannel(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildTextMsg(domain.QCustomTool)
	m.Update(qMsg) // installs the TextPromptScreen

	// Type a tool name and confirm.
	for _, ch := range "my-mcp-server" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after text answer; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.textAns.Status != domain.Answered {
			t.Errorf("reply channel Status = %q, want Answered", ans.textAns.Status)
		}
		if ans.textAns.Text != "my-mcp-server" {
			t.Errorf("reply channel Text = %q, want %q", ans.textAns.Text, "my-mcp-server")
		}
	default:
		t.Error("reply channel has no message; text answer was not sent")
	}
}

// TestInteractionRouting_ReviewScreen_ConfirmRoutedToReplyChannel verifies that pressing 'y'
// on the ReviewScreen sends a ConfirmAnswer with Confirm=true to the reply channel.
func TestInteractionRouting_ReviewScreen_ConfirmRoutedToReplyChannel(t *testing.T) {
	m := newRoutingModel()
	plan := domain.Plan{
		Mode:          domain.ModeDeployNew,
		WorkspacePath: "/ws",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
	}
	qMsg := buildReviewMsg(plan)
	m.Update(qMsg)

	// Confirm the plan with 'y'.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after review confirm; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.confirmAns.Status != domain.Answered {
			t.Errorf("reply channel Status = %q, want Answered", ans.confirmAns.Status)
		}
		if !ans.confirmAns.Confirm {
			t.Error("reply channel Confirm = false after 'y'; want true")
		}
	default:
		t.Error("reply channel has no message after review confirm")
	}
}

// ---------------------------------------------------------------------------
// ConflictScreen Esc abort routing
// ---------------------------------------------------------------------------

// TestInteractionRouting_ConflictScreen_Esc_SendsCancelledToReplyChannel verifies the
// end-to-end abort path: when the ConflictScreen overlay receives Esc, the root model routes
// a Cancelled answer to the reply channel so the app goroutine can abort the run. The root
// model must treat this as a run abort, not as backward navigation to a prior screen.
func TestInteractionRouting_ConflictScreen_Esc_SendsCancelledToReplyChannel(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QLocalModification, []domain.Option{
		{ID: string(domain.DecisionOverwrite), Label: "Overwrite with new version"},
		{ID: string(domain.DecisionSkip), Label: "Skip (keep local version)"},
		{ID: string(domain.DecisionBackupThenOverwrite), Label: "Back up then overwrite"},
	})
	m.Update(qMsg) // installs the ConflictScreen overlay

	if m.conflictOverlay == nil {
		t.Fatal("precondition: conflictOverlay is nil after QLocalModification; cannot test Esc abort path")
	}

	// Press Esc to abort the run.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// The overlay must be dismissed after producing an answer.
	if m.conflictOverlay != nil {
		t.Error("conflictOverlay is non-nil after Esc; overlay must be dismissed once it produces an answer")
	}

	// The screen must return to running, not navigate backward to a prior screen.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after ConflictScreen Esc; want screenRunning — abort is signalled via the reply channel, not by screen navigation", m.screen)
	}

	// The reply channel must receive Cancelled so the app goroutine can abort.
	select {
	case ans := <-qMsg.reply:
		if ans.choiceAns.Status != domain.Cancelled {
			t.Errorf("reply channel Status = %q after ConflictScreen Esc; want Cancelled", ans.choiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after ConflictScreen Esc; Cancelled must be routed to the reply channel so the app goroutine can abort")
	}
}

// ---------------------------------------------------------------------------
// Run done: SummaryScreen shown
// ---------------------------------------------------------------------------

// TestInteractionRouting_RunDone_ShowsSummaryScreen verifies that when the service call
// completes successfully, the root model creates a SummaryScreen and transitions to screenDone.
func TestInteractionRouting_RunDone_ShowsSummaryScreen(t *testing.T) {
	m := newRoutingModel()
	summary := domain.RunSummary{
		Outcome:       domain.OutcomeSuccess,
		WorkspacePath: "/ws",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
	}

	m.Update(runDoneMsg{summary: summary})

	if m.screen != screenDone {
		t.Errorf("screen = %v after runDoneMsg; want screenDone", m.screen)
	}
	if m.summaryScreen == nil {
		t.Error("summaryScreen is nil after runDoneMsg; want a SummaryScreen")
	}
}

// TestInteractionRouting_RunDone_ViewDelegatesToSummaryScreen verifies that on screenDone
// with a SummaryScreen, View() returns content from the SummaryScreen.
func TestInteractionRouting_RunDone_ViewDelegatesToSummaryScreen(t *testing.T) {
	m := newRoutingModel()
	summary := domain.RunSummary{
		Outcome:       domain.OutcomeSuccess,
		WorkspacePath: "/workspace",
		Harness:       domain.HarnessRef{ID: "claude-code", DisplayName: "Claude Code"},
	}
	m.Update(runDoneMsg{summary: summary})

	view := m.View()

	if !strings.Contains(view, "Summary") || !strings.Contains(view, "Claude Code") {
		t.Errorf("View() on screenDone does not delegate to SummaryScreen:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Resize propagation to active overlays
// ---------------------------------------------------------------------------

// TestInteractionRouting_Resize_PropagatestoModelOverlay verifies that a WindowSizeMsg
// reaches the active ModelSelectScreen overlay without panicking.
func TestInteractionRouting_Resize_PropagatestoModelOverlay(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectOneMsg(domain.QTierModel, []domain.Option{{ID: "gpt-4o", Label: "gpt-4o"}}))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active modelOverlay: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.modelOverlay == nil {
		t.Error("modelOverlay is nil after resize; must survive the resize")
	}
}

// ---------------------------------------------------------------------------
// Overlay type verification: concrete fields are the right screen types
// ---------------------------------------------------------------------------

// TestInteractionRouting_ModelOverlay_IsNonNilAfterQTierModel verifies that the modelOverlay
// field is set after a QTierModel question. The field is typed as *screens.ModelSelectScreen,
// so a non-nil value is sufficient to confirm routing. The compiler enforces the type.
func TestInteractionRouting_ModelOverlay_IsNonNilAfterQTierModel(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectOneMsg(domain.QTierModel, []domain.Option{{ID: "gpt-4o", Label: "gpt-4o"}}))

	if m.modelOverlay == nil {
		t.Error("modelOverlay (*screens.ModelSelectScreen) is nil after QTierModel; want non-nil")
	}
	// Confirm it renders something useful.
	view := m.modelOverlay.View()
	if view == "" {
		t.Error("ModelSelectScreen.View() returned empty string after creation")
	}
}

// ---------------------------------------------------------------------------
// QPromoteCustomTool routing (T4.3)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// QTransformTargetHarness routing (I6.3/I6.4)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QTransformTargetHarness_ShowsSelectionOverlay verifies that when the
// app layer sends a QTransformTargetHarness question, the root model switches to screenQuestion
// and activates a selection overlay. Before I6.3/I6.4 wire this question ID to a specific
// overlay, the message is unhandled and this test fails correctly.
func TestInteractionRouting_QTransformTargetHarness_ShowsSelectionOverlay(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTransformTargetHarness, []domain.Option{
		{ID: "opencode", Label: "OpenCode"},
		{ID: "cursor", Label: "Cursor"},
	})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QTransformTargetHarness; want screenQuestion; "+
			"I6.3/I6.4 must wire QTransformTargetHarness to an overlay so the user can select a target harness", m.screen)
	}
	// QTransformTargetHarness is a harness-selection choice and should route to the generic
	// select overlay (the same mechanism used by other SelectOne harness-type questions).
	if m.selectOverlay == nil {
		t.Error("selectOverlay is nil after QTransformTargetHarness; want a selection overlay; " +
			"I6.3/I6.4 must handle QTransformTargetHarness and route it to a selection overlay")
	}
	if m.modelOverlay != nil {
		t.Error("modelOverlay is non-nil after QTransformTargetHarness; harness selection must not use the model overlay")
	}
}

// TestInteractionRouting_QTransformTargetHarness_ViewContainsHarnessOptions verifies that the
// overlay shown for QTransformTargetHarness includes the harness option labels in its View().
func TestInteractionRouting_QTransformTargetHarness_ViewContainsHarnessOptions(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTransformTargetHarness, []domain.Option{
		{ID: "opencode", Label: "OpenCode"},
		{ID: "cursor", Label: "Cursor"},
	})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Fatalf("precondition: screen = %v after QTransformTargetHarness; want screenQuestion; "+
			"I6.3/I6.4 must wire the overlay before this view test is meaningful", m.screen)
	}

	view := m.View()
	if !strings.Contains(view, "OpenCode") {
		t.Errorf("View() after QTransformTargetHarness does not contain 'OpenCode':\n%s\n"+
			"the target-harness overlay must display the harness options to the user", view)
	}
}

// ---------------------------------------------------------------------------
// QTransformTargetModel routing (I6.3/I6.4)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QTransformTargetModel_ShowsModelSelectScreen verifies that when the
// app layer sends a QTransformTargetModel question, the root model switches to screenQuestion
// and activates the ModelSelectScreen overlay. This mirrors QTierModel and QAgentModel routing.
// Before I6.3/I6.4 wire this question ID, the message is unhandled and this test fails.
func TestInteractionRouting_QTransformTargetModel_ShowsModelSelectScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTransformTargetModel, []domain.Option{
		{ID: "gpt-4o", Label: "GPT-4o"},
		{ID: "claude-3-sonnet", Label: "Claude 3 Sonnet"},
	})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QTransformTargetModel; want screenQuestion; "+
			"I6.3/I6.4 must wire QTransformTargetModel to the ModelSelectScreen overlay", m.screen)
	}
	if m.modelOverlay == nil {
		t.Error("modelOverlay is nil after QTransformTargetModel; want a ModelSelectScreen; " +
			"QTransformTargetModel is a model-selection question and must reuse the ModelSelectScreen " +
			"overlay exactly as QTierModel and QAgentModel do")
	}
	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QTransformTargetModel; must use modelOverlay instead")
	}
}

// TestInteractionRouting_QTransformTargetModel_ViewContainsModelOptions verifies that the
// ModelSelectScreen overlay for QTransformTargetModel displays the provided model options.
func TestInteractionRouting_QTransformTargetModel_ViewContainsModelOptions(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectOneMsg(domain.QTransformTargetModel, []domain.Option{
		{ID: "gpt-4o", Label: "GPT-4o"},
	})

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Fatalf("precondition: screen = %v after QTransformTargetModel; want screenQuestion; "+
			"I6.3/I6.4 must wire the ModelSelectScreen overlay first", m.screen)
	}

	view := m.View()
	if !strings.Contains(view, "GPT-4o") {
		t.Errorf("View() after QTransformTargetModel does not contain 'GPT-4o':\n%s\n"+
			"the model overlay must display the available target-model options", view)
	}
}

// ---------------------------------------------------------------------------
// QPromoteCustomTool routing (T4.3)
// ---------------------------------------------------------------------------

// TestInteractionRouting_QPromoteCustomTool_ShowsTextPromptScreen verifies that the
// QPromoteCustomTool text question is routed to the existing TextPromptScreen overlay.
// The TUI routes text questions by question kind (AskText), not by question id, so a
// new text question id reaches the existing overlay with no routing change or new screen.
// This confirms that the reverse custom-tool dialog reuses the text-prompt overlay as
// required by the design.
func TestInteractionRouting_QPromoteCustomTool_ShowsTextPromptScreen(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildTextMsg(domain.QPromoteCustomTool)

	m.Update(qMsg)

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QPromoteCustomTool; want screenQuestion", m.screen)
	}
	if m.textOverlay == nil {
		t.Error("textOverlay is nil after QPromoteCustomTool; want a TextPromptScreen — routing is by question kind (text), not id")
	}
	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QPromoteCustomTool; must use textOverlay for text questions")
	}
	if m.modelOverlay != nil {
		t.Error("modelOverlay is non-nil after QPromoteCustomTool; must use textOverlay, not modelOverlay")
	}
}
