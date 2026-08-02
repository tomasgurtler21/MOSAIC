package tui

// navigation_test.go verifies the Model navigation state machine: initial
// screen, window resize handling, ctrl+c cancellation, and screen transitions
// between source, run list, run detail, pricing, and no-data screens.
//
// These tests are in package tui (internal) because screenName and Model
// fields are unexported. Tests drive the model through the bubbletea Update
// cycle without a real terminal.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
	"mosaic-log-analyzer/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newNavModel builds a Model with a nil Service pointer. Analysis outcomes are
// injected directly as analysisDoneMsg values to avoid real I/O.
func newNavModel() Model {
	interact, questions := NewInteraction()
	return Model{
		opts:      Options{Service: nil, InitialPath: ""},
		theme:     tuicommon.DefaultTheme(),
		interact:  interact,
		questions: questions,
		cancel:    func() {},
		screen:    screenLoading,
		width:     80,
		height:    24,
	}
}

func asModel(tm tea.Model) Model {
	return tm.(Model)
}

func pressKey(m Model, key string) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func pressKeyType(m Model, kt tea.KeyType) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: kt})
}

// makeReport builds a minimal Report with the given named run IDs.
func makeReport(runIDs ...string) domain.Report {
	runs := make([]domain.RunReport, len(runIDs))
	for i, id := range runIDs {
		runs[i] = domain.RunReport{
			Run:  domain.NamedRun(id),
			Totals: domain.Totals{
				Tokens: domain.TokenUsage{Input: domain.Tokens(100)},
				Money: domain.CategoryMoney{
					Input:    domain.KnownMoney(domain.Money(1_000_000_000)), // $1.00
					Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
					Complete: true,
				},
			},
			Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
		}
	}
	return domain.Report{
		Runs:     runs,
		AllRuns:  domain.Totals{},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}
}

// makePendingText constructs a Pending for an AskText question.
func makePendingText(id interaction.QuestionID, reply chan answerMsg) Pending {
	return Pending{
		id:    id,
		kind:  pendingAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{ID: id, Title: string(id)}},
		reply: reply,
	}
}

// makePendingSelect constructs a Pending for a SelectOne question.
func makePendingSelect(id interaction.QuestionID, optionIDs []string, reply chan answerMsg) Pending {
	opts := make([]interaction.Option, len(optionIDs))
	for i, oid := range optionIDs {
		opts[i] = interaction.Option{ID: oid, Label: oid}
	}
	return Pending{
		id:   id,
		kind: pendingSelectOne,
		choiceQ: interaction.ChoiceQuestion{
			Question: interaction.Question{ID: id, Title: string(id)},
			Options:  opts,
		},
		reply: reply,
	}
}

// ---------------------------------------------------------------------------
// Tests: initial state
// ---------------------------------------------------------------------------

func TestModel_InitialScreen(t *testing.T) {
	m := newNavModel()
	if m.screen != screenLoading {
		t.Errorf("initial screen = %q, want %q", m.screen, screenLoading)
	}
}

// ---------------------------------------------------------------------------
// Tests: window resize
// ---------------------------------------------------------------------------

func TestModel_WindowResize(t *testing.T) {
	m := newNavModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := asModel(updated)
	if m2.width != 120 {
		t.Errorf("width after resize = %d, want 120", m2.width)
	}
	if m2.height != 40 {
		t.Errorf("height after resize = %d, want 40", m2.height)
	}
}

// ---------------------------------------------------------------------------
// Tests: ctrl+c cancels from loading screen
// ---------------------------------------------------------------------------

func TestModel_CtrlC_ReturnsCmdFromLoadingScreen(t *testing.T) {
	m := newNavModel()
	_, cmd := pressKeyType(m, tea.KeyCtrlC)
	if cmd == nil {
		t.Fatal("ctrl+c should return a non-nil quit command")
	}
}

// ---------------------------------------------------------------------------
// Tests: analysis done → correct screen transitions
// ---------------------------------------------------------------------------

func TestModel_AnalysisDone_Report_GoesToRunsScreen(t *testing.T) {
	m := newNavModel()
	report := makeReport("20260101T120000Z-abcd")
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeReport, Report: report},
	})
	m2 := asModel(updated)
	if m2.screen != screenRuns {
		t.Errorf("screen = %q, want %q", m2.screen, screenRuns)
	}
	if m2.runsScreen == nil {
		t.Error("runsScreen should not be nil after OutcomeReport")
	}
}

func TestModel_AnalysisDone_NoData_GoesToNoDataScreen(t *testing.T) {
	m := newNavModel()
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test/logs"}
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeNoData, Source: src},
	})
	m2 := asModel(updated)
	if m2.screen != screenNoData {
		t.Errorf("screen = %q, want %q", m2.screen, screenNoData)
	}
	if m2.noDataScreen == nil {
		t.Error("noDataScreen should not be nil after OutcomeNoData")
	}
	if m2.exitCode != 2 {
		t.Errorf("exitCode = %d, want 2", m2.exitCode)
	}
}

func TestModel_AnalysisDone_SourceNotFound_GoesToErrorScreen(t *testing.T) {
	m := newNavModel()
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeSourceNotFound},
	})
	m2 := asModel(updated)
	if m2.screen != screenError {
		t.Errorf("screen = %q, want %q", m2.screen, screenError)
	}
	if m2.exitCode != 3 {
		t.Errorf("exitCode = %d, want 3", m2.exitCode)
	}
}

func TestModel_AnalysisDone_SourceUnusable_GoesToErrorScreen(t *testing.T) {
	m := newNavModel()
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{
			Outcome: app.OutcomeSourceUnusable,
			Source:  domain.Source{Reason: "not a logs tree"},
		},
	})
	m2 := asModel(updated)
	if m2.screen != screenError {
		t.Errorf("screen = %q, want %q", m2.screen, screenError)
	}
	if m2.exitCode != 4 {
		t.Errorf("exitCode = %d, want 4", m2.exitCode)
	}
}

// ---------------------------------------------------------------------------
// Tests: runs screen navigation
// ---------------------------------------------------------------------------

func TestModel_RunsScreen_Enter_GoesToRunDetail(t *testing.T) {
	m := newNavModel()
	report := makeReport("20260101T120000Z-abcd")
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeReport, Report: report},
	})
	m2 := asModel(updated)

	updated2, _ := pressKeyType(m2, tea.KeyEnter)
	m3 := asModel(updated2)
	if m3.screen != screenRunDetail {
		t.Errorf("after Enter on runs screen: screen = %q, want %q", m3.screen, screenRunDetail)
	}
	if m3.runDetailScreen == nil {
		t.Error("runDetailScreen should not be nil after navigating to it")
	}
}

func TestModel_RunDetail_Esc_ReturnsToRuns(t *testing.T) {
	m := newNavModel()
	report := makeReport("20260101T120000Z-abcd")
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeReport, Report: report},
	})
	m2 := asModel(updated)
	updated2, _ := pressKeyType(m2, tea.KeyEnter)
	m3 := asModel(updated2)

	updated3, _ := pressKey(m3, "esc")
	m4 := asModel(updated3)
	if m4.screen != screenRuns {
		t.Errorf("after Esc on run detail: screen = %q, want %q", m4.screen, screenRuns)
	}
}

func TestModel_RunsScreen_P_GoesToPricingWhenUnpricedModels(t *testing.T) {
	m := newNavModel()
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")

	report := makeReport("20260101T120000Z-abcd")
	report.UnpricedModels = []domain.ModelID{"claude-test-model"}

	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.screen = screenRuns

	updated, _ := pressKey(m, "p")
	m2 := asModel(updated)

	if m2.screen != screenPricing {
		t.Errorf("after 'p' with unpriced models: screen = %q, want %q", m2.screen, screenPricing)
	}
	if m2.pricingScreen == nil {
		t.Error("pricingScreen should not be nil after 'p' with unpriced models")
	}
}

func TestModel_RunsScreen_P_NoOpWhenNoUnpricedModels(t *testing.T) {
	m := newNavModel()
	report := makeReport("20260101T120000Z-abcd")
	// No UnpricedModels — pressing 'p' must be a no-op.

	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.screen = screenRuns

	updated, _ := pressKey(m, "p")
	m2 := asModel(updated)

	if m2.screen != screenRuns {
		t.Errorf("after 'p' with no unpriced models: screen = %q, want %q", m2.screen, screenRuns)
	}
	if m2.pricingScreen != nil {
		t.Error("pricingScreen should remain nil when no unpriced models are present")
	}
}

// ---------------------------------------------------------------------------
// Tests: pending question → source screen
// ---------------------------------------------------------------------------

func TestModel_Pending_LogSourcePath_GoesToSourceScreen(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionLogSourcePath, reply)

	updated, _ := m.Update(p)
	m2 := asModel(updated)
	// Close interaction to unblock any waitForPending goroutine in tests.
	m2.interact.Close()

	if m2.screen != screenSource {
		t.Errorf("screen = %q, want %q", m2.screen, screenSource)
	}
	if m2.sourceScreen == nil {
		t.Error("sourceScreen should not be nil after QuestionLogSourcePath pending")
	}
}

func TestModel_Pending_PricingRate_GoesToQuestionScreen(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionPricingRate, reply)

	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	if m2.screen != screenQuestion {
		t.Errorf("screen = %q, want %q", m2.screen, screenQuestion)
	}
	if m2.textOverlay == nil {
		t.Error("textOverlay should not be nil after QuestionPricingRate pending")
	}
}

func TestModel_Pending_PricingAction_GoesToQuestionScreen(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingSelect(app.QuestionPricingAction, []string{
		app.PricingActionEnter,
		app.PricingActionShowPath,
		app.PricingActionSkip,
	}, reply)

	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	if m2.screen != screenQuestion {
		t.Errorf("screen = %q, want %q", m2.screen, screenQuestion)
	}
	if m2.selectOverlay == nil {
		t.Error("selectOverlay should not be nil after QuestionPricingAction pending")
	}
}

// ---------------------------------------------------------------------------
// Tests: no-data screen navigation
// ---------------------------------------------------------------------------

func TestModel_NoData_EscReturnsQuitCmd(t *testing.T) {
	m := newNavModel()
	style := stylesFromTheme(m.theme)
	m.noDataScreen = screens.NewNoDataScreen(
		domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty"},
		80, 24, style,
	)
	m.screen = screenNoData

	_, cmd := pressKey(m, "esc")
	if cmd == nil {
		t.Error("Esc on no-data screen should return a non-nil quit command")
	}
}

func TestModel_NoData_S_GoesToLoadingAndRestarts(t *testing.T) {
	m := newNavModel()
	style := stylesFromTheme(m.theme)
	m.noDataScreen = screens.NewNoDataScreen(
		domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty"},
		80, 24, style,
	)
	m.screen = screenNoData

	updated, cmd := pressKey(m, "s")
	m2 := asModel(updated)
	if m2.screen != screenLoading {
		t.Errorf("after 's' on no-data: screen = %q, want %q", m2.screen, screenLoading)
	}
	if cmd == nil {
		t.Error("after 's' on no-data: expected a non-nil restart command")
	}
}

// ---------------------------------------------------------------------------
// Tests: updateSource answer-plumbing round trips
// ---------------------------------------------------------------------------

// TestModel_UpdateSource_AnsweredOnSubmit verifies that typing a path and
// pressing Enter delivers an Answered reply on the pending question's channel.
func TestModel_UpdateSource_AnsweredOnSubmit(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionLogSourcePath, reply)

	// Route to source screen.
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close() // prevent goroutine leak from ignored waitForPending cmd

	if m2.screen != screenSource {
		t.Fatalf("expected screenSource after pending, got %q", m2.screen)
	}

	// Type a path, then press Enter to submit.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/test/logs")})
	m3 := asModel(updated2)
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := asModel(updated3)

	if m4.screen != screenLoading {
		t.Errorf("after submit: screen = %q, want %q", m4.screen, screenLoading)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered {
			t.Errorf("answer status = %v, want Answered", ans.textAns.Status)
		}
		if ans.textAns.Text != "/test/logs" {
			t.Errorf("answer text = %q, want %q", ans.textAns.Text, "/test/logs")
		}
	default:
		t.Error("no answer received on reply channel after Enter on source screen")
	}
}

// TestModel_UpdateSource_CancelledOnEsc verifies that pressing Esc on the
// source screen delivers a Cancelled reply on the pending question's channel.
func TestModel_UpdateSource_CancelledOnEsc(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionLogSourcePath, reply)

	// Route to source screen.
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	// Press Esc to cancel.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(updated2)

	if m3.screen != screenLoading {
		t.Errorf("after Esc on source screen: screen = %q, want %q", m3.screen, screenLoading)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Cancelled {
			t.Errorf("answer status = %v, want Cancelled", ans.textAns.Status)
		}
	default:
		t.Error("no answer received on reply channel after Esc on source screen")
	}
}

// ---------------------------------------------------------------------------
// Tests: updateQuestion overlay answer-plumbing round trips
// ---------------------------------------------------------------------------

// TestModel_UpdateQuestion_SelectOverlay_AnsweredOnEnter verifies that pressing
// Enter on a selectOverlay delivers an Answered reply with the selected option.
func TestModel_UpdateQuestion_SelectOverlay_AnsweredOnEnter(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	opts := []string{app.PricingActionEnter, app.PricingActionShowPath, app.PricingActionSkip}
	p := makePendingSelect(app.QuestionPricingAction, opts, reply)

	// Route to question screen with a selectOverlay.
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	if m2.screen != screenQuestion || m2.selectOverlay == nil {
		t.Fatalf("expected screenQuestion with selectOverlay, got screen=%q selectOverlay=%v",
			m2.screen, m2.selectOverlay)
	}

	// Cursor starts at position 0 (PricingActionEnter). Press Enter to select.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := asModel(updated2)

	if m3.screen != screenLoading {
		t.Errorf("after Enter on selectOverlay: screen = %q, want %q", m3.screen, screenLoading)
	}
	if m3.selectOverlay != nil {
		t.Error("selectOverlay should be nil after answering")
	}
	select {
	case ans := <-reply:
		if ans.choiceAns.Status != interaction.Answered {
			t.Errorf("answer status = %v, want Answered", ans.choiceAns.Status)
		}
		if ans.choiceAns.OptionID != app.PricingActionEnter {
			t.Errorf("OptionID = %q, want %q", ans.choiceAns.OptionID, app.PricingActionEnter)
		}
	default:
		t.Error("no answer received on reply channel after Enter on selectOverlay")
	}
}

// TestModel_UpdateQuestion_SelectOverlay_CancelledOnEsc verifies that pressing
// Esc on a selectOverlay delivers a Cancelled reply.
func TestModel_UpdateQuestion_SelectOverlay_CancelledOnEsc(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	opts := []string{app.PricingActionEnter, app.PricingActionShowPath, app.PricingActionSkip}
	p := makePendingSelect(app.QuestionPricingAction, opts, reply)

	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	// Press Esc to cancel.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(updated2)

	if m3.screen != screenLoading {
		t.Errorf("after Esc on selectOverlay: screen = %q, want %q", m3.screen, screenLoading)
	}
	select {
	case ans := <-reply:
		if ans.choiceAns.Status != interaction.Cancelled {
			t.Errorf("answer status = %v, want Cancelled", ans.choiceAns.Status)
		}
	default:
		t.Error("no answer received on reply channel after Esc on selectOverlay")
	}
}

// TestModel_UpdateQuestion_TextOverlay_AnsweredOnSubmit verifies that typing
// text and pressing Enter on a textOverlay delivers an Answered reply with the
// entered text.
func TestModel_UpdateQuestion_TextOverlay_AnsweredOnSubmit(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionPricingRate, reply)

	// Route to question screen with a textOverlay.
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	if m2.screen != screenQuestion || m2.textOverlay == nil {
		t.Fatalf("expected screenQuestion with textOverlay, got screen=%q textOverlay=%v",
			m2.screen, m2.textOverlay)
	}

	// Type a rate value, then press Enter to submit.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3.00")})
	m3 := asModel(updated2)
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := asModel(updated3)

	if m4.screen != screenLoading {
		t.Errorf("after Enter on textOverlay: screen = %q, want %q", m4.screen, screenLoading)
	}
	if m4.textOverlay != nil {
		t.Error("textOverlay should be nil after answering")
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered {
			t.Errorf("answer status = %v, want Answered", ans.textAns.Status)
		}
		if ans.textAns.Text != "3.00" {
			t.Errorf("answer text = %q, want %q", ans.textAns.Text, "3.00")
		}
	default:
		t.Error("no answer received on reply channel after Enter on textOverlay")
	}
}

// TestModel_UpdateQuestion_TextOverlay_CancelledOnEsc verifies that pressing
// Esc on a textOverlay delivers a Cancelled reply.
func TestModel_UpdateQuestion_TextOverlay_CancelledOnEsc(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionPricingRate, reply)

	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	// Press Esc to cancel.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(updated2)

	if m3.screen != screenLoading {
		t.Errorf("after Esc on textOverlay: screen = %q, want %q", m3.screen, screenLoading)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Cancelled {
			t.Errorf("answer status = %v, want Cancelled", ans.textAns.Status)
		}
	default:
		t.Error("no answer received on reply channel after Esc on textOverlay")
	}
}
