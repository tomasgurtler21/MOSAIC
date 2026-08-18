package tui

// navigation_test.go verifies the Model navigation state machine: initial
// screen, window resize handling, ctrl+c cancellation, and screen transitions
// between source, run list, run detail, pricing, and no-data screens.
//
// These tests are in package tui (internal) because screenName and Model
// fields are unexported. Tests drive the model through the bubbletea Update
// cycle without a real terminal.

import (
	"strings"
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
	// Session must be non-nil: the new implementation guards on session presence.
	m.session = new(app.Session)
	m.result = &app.Result{Outcome: app.OutcomeReport, Session: new(app.Session)}

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

// TestModel_RunsScreen_P_AlwaysOpensPricingScreenWhenAllModelsArePriced verifies
// that pressing 'p' on the Runs screen opens the pricing screen even when every
// model in the report already has a price. The old conditional guard
// (len(UnpricedModels) > 0) encoded a bug that this test pins as fixed.
func TestModel_RunsScreen_P_AlwaysOpensPricingScreenWhenAllModelsArePriced(t *testing.T) {
	m := newNavModel()
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")
	// Session must be non-nil: the implementation guards on session presence.
	m.session = new(app.Session)
	m.result = &app.Result{Outcome: app.OutcomeReport, Session: new(app.Session)}

	// Report with NO unpriced models — all models are priced.
	report := makeReport("20260101T120000Z-abcd")
	// UnpricedModels is deliberately left empty (zero value): all models are priced.

	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.screen = screenRuns

	updated, _ := pressKey(m, "p")
	m2 := asModel(updated)

	if m2.screen != screenPricing {
		t.Errorf("after 'p' with all models priced: screen = %q, want %q — "+
			"'p' must always open the pricing screen", m2.screen, screenPricing)
	}
	if m2.pricingScreen == nil {
		t.Error("pricingScreen should not be nil after 'p' even when all models are priced")
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

// TestModel_Pending_PricingAction_BackgroundPricingScreenReflectsModelPricingStates
// verifies that when a QuestionPricingAction pending arrives with an active
// session and a non-empty report, the pricingScreen background is populated
// from app.ModelPricingStates and its View renders the report's models. This
// pins the I5.6 requirement: the reworked PricingScreen-as-background must
// reflect the report's actual model list, not the old unpriced-only list.
func TestModel_Pending_PricingAction_BackgroundPricingScreenReflectsModelPricingStates(t *testing.T) {
	// Arrange: model with an active session and a report containing a known model.
	m := newNavModel()
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")
	sess := new(app.Session)
	m.session = sess
	m.result = &app.Result{Outcome: app.OutcomeReport, Session: sess}

	// Build a report whose orchestrator carries "background-model".
	// ReportModels walks Runs[].Orchestrator.Models, so setting it here ensures
	// ModelPricingStates includes it in the background screen's model list.
	m.report = domain.Report{
		Runs: []domain.RunReport{{
			Run: domain.NamedRun("20260101T120000Z-abcd"),
			Totals: domain.Totals{
				Money: domain.CategoryMoney{Total: domain.NoMoneyData()},
			},
			Orchestrator: domain.ActorReport{
				Actor:  domain.Orchestrator(),
				Models: []domain.ModelID{"background-model"},
			},
		}},
		Currency: domain.Currency,
		Quality:  domain.NewQualitySummary(nil),
	}

	reply := make(chan answerMsg, 1)
	p := makePendingSelect(app.QuestionPricingAction, []string{
		app.PricingActionEnter,
		app.PricingActionShowPath,
		app.PricingActionSkip,
	}, reply)

	// Act.
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()

	// Assert: pricingScreen must be non-nil (background constructed for the overlay).
	if m2.pricingScreen == nil {
		t.Fatal("pricingScreen must be non-nil after QuestionPricingAction when result and session are set")
	}
	// Assert: the background must reflect the report's models via ModelPricingStates.
	// If constructed correctly, the model "background-model" appears in the view.
	view := m2.pricingScreen.View(m2.width, m2.height)
	if !strings.Contains(view, "background-model") {
		t.Errorf("pricingScreen background does not reflect report models after QuestionPricingAction;\n"+
			"expected 'background-model' (from ModelPricingStates) but got:\n%s", view)
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

// ---------------------------------------------------------------------------
// Tests: source-path switch from Runs list
// ---------------------------------------------------------------------------

// setUpRunsScreen returns a Model positioned on the Runs screen with the
// given report. Callers are responsible for setting opts.Service when needed.
func setUpRunsScreen(report domain.Report) Model {
	m := newNavModel()
	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.screen = screenRuns
	return m
}

// TestModel_RunsScreen_S_GoesToSourceScreen verifies that pressing 's' on the
// Runs screen transitions the model to the source-path entry screen.
func TestModel_RunsScreen_S_GoesToSourceScreen(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd", "20260101T130000Z-bcde")
	m := setUpRunsScreen(report)

	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)

	if m2.screen != screenSource {
		t.Errorf("after 's' on runs: screen = %q, want %q", m2.screen, screenSource)
	}
	if m2.sourceScreen == nil {
		t.Error("sourceScreen should not be nil after 's' on runs screen")
	}
}

// TestModel_RunsScreen_S_DoesNotForwardKeyToRunsScreen verifies that pressing
// 's' on the Runs screen does not forward the key to RunsScreen: the screen
// is now screenSource, not screenRunDetail, and RunsScreen.Done() is false.
func TestModel_RunsScreen_S_DoesNotForwardKeyToRunsScreen(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd", "20260101T130000Z-bcde")
	m := setUpRunsScreen(report)

	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)

	// The model must not have navigated into a run detail (key not forwarded as
	// if it were Enter or a movement key that reached RunsScreen).
	if m2.screen == screenRunDetail {
		t.Error("after 's' on runs: must not navigate to run detail (key was forwarded)")
	}
	if m2.runsScreen != nil && m2.runsScreen.Done() {
		t.Error("after 's' on runs: RunsScreen.Done() must be false — key must not be forwarded")
	}
}

// TestModel_RunsInitiatedSource_Confirm_GoesToLoadingWithExplicitPath verifies
// that confirming a path on the Runs-initiated source screen sets the explicit
// analysis path and transitions to the loading screen.
func TestModel_RunsInitiatedSource_Confirm_GoesToLoadingWithExplicitPath(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd")
	m := setUpRunsScreen(report)

	// Open source screen from Runs.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	if m2.screen != screenSource {
		t.Fatalf("expected screenSource after 's' on runs, got %q", m2.screen)
	}

	// Type a path.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/new/logs")})
	m3 := asModel(updated2)

	// Confirm with Enter.
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := asModel(updated3)

	if m4.screen != screenLoading {
		t.Errorf("after confirming path: screen = %q, want %q", m4.screen, screenLoading)
	}
	if m4.analysisPath != "/new/logs" {
		t.Errorf("after confirming path: analysisPath = %q, want %q", m4.analysisPath, "/new/logs")
	}
}

// TestModel_RunsInitiatedSource_Confirm_ReturnsAnalysisCommand verifies that
// confirming a path returns a non-nil analysis command so the new analysis
// actually starts.
func TestModel_RunsInitiatedSource_Confirm_ReturnsAnalysisCommand(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd")
	m := setUpRunsScreen(report)
	// Service must be non-nil so buildAnalysisCmd does not dereference nil.
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")

	// Open source screen, type a path, and confirm.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/new/logs")})
	m3 := asModel(updated2)
	_, cmd := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Error("after confirming path from Runs-initiated source: expected a non-nil analysis command")
	}
}

// TestModel_RunsInitiatedSource_Confirm_NewAnalysisDoneUpdatesReport verifies
// the end-to-end flow: after the Runs-initiated source path is confirmed and
// an analysis completes with a new report, the Runs screen shows the new data.
func TestModel_RunsInitiatedSource_Confirm_NewAnalysisDoneUpdatesReport(t *testing.T) {
	report1 := makeReport("20260101T120000Z-abcd")
	m := setUpRunsScreen(report1)
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")

	// Open source screen, type a path, and confirm.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/new/logs")})
	m3 := asModel(updated2)
	updated3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := asModel(updated3)
	if m4.screen != screenLoading {
		t.Fatalf("expected screenLoading after confirm, got %q", m4.screen)
	}

	// Inject the analysis result with a brand-new report.
	report2 := makeReport("20260201T120000Z-xxxx")
	updated4, _ := m4.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeReport, Report: report2},
	})
	m5 := asModel(updated4)

	if m5.screen != screenRuns {
		t.Errorf("after new analysis done: screen = %q, want %q", m5.screen, screenRuns)
	}
	if m5.runsScreen == nil {
		t.Fatal("runsScreen should not be nil after new analysis")
	}
	// The new report must have replaced the old one.
	if len(m5.report.Runs) == 0 {
		t.Fatal("new report should have at least one run")
	}
	if m5.report.Runs[0].Run.ID != "20260201T120000Z-xxxx" {
		t.Errorf("report not updated: got run id %q, want %q",
			m5.report.Runs[0].Run.ID, "20260201T120000Z-xxxx")
	}
	// The old run must not be present.
	if m5.report.Runs[0].Run.ID == "20260101T120000Z-abcd" {
		t.Error("report still shows the old run id — report was not replaced")
	}
}

// TestModel_RunsInitiatedSource_Esc_ReturnsToRunsScreen verifies that pressing
// Esc on the Runs-initiated source screen returns the model to the Runs screen,
// not to the loading screen.
func TestModel_RunsInitiatedSource_Esc_ReturnsToRunsScreen(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd")
	m := setUpRunsScreen(report)

	// Open source screen from Runs.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	if m2.screen != screenSource {
		t.Fatalf("expected screenSource after 's', got %q", m2.screen)
	}

	// Press Esc to cancel.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(updated2)

	if m3.screen != screenRuns {
		t.Errorf("after Esc on Runs-initiated source: screen = %q, want %q", m3.screen, screenRuns)
	}
}

// TestModel_RunsInitiatedSource_Esc_PreviousReportIntact verifies that
// cancelling the Runs-initiated source screen leaves the existing report and
// its runs unchanged — the user's current view is fully preserved.
func TestModel_RunsInitiatedSource_Esc_PreviousReportIntact(t *testing.T) {
	const origRunID = "20260101T120000Z-abcd"
	report := makeReport(origRunID)
	m := setUpRunsScreen(report)

	// Open source screen from Runs, then cancel.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := asModel(updated2)

	if len(m3.report.Runs) == 0 {
		t.Fatal("report should still have runs after cancelling source screen")
	}
	if m3.report.Runs[0].Run.ID != origRunID {
		t.Errorf("report changed after cancel: got run id %q, want %q",
			m3.report.Runs[0].Run.ID, origRunID)
	}
}

// TestModel_RunsInitiatedSource_Esc_NoAnalysisRestarted verifies that
// cancelling the Runs-initiated source screen does not start a new analysis —
// no analysis command must be returned.
func TestModel_RunsInitiatedSource_Esc_NoAnalysisRestarted(t *testing.T) {
	report := makeReport("20260101T120000Z-abcd")
	m := setUpRunsScreen(report)

	// Open source screen from Runs, then cancel.
	updated, _ := pressKey(m, "s")
	m2 := asModel(updated)
	_, cmd := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Error("after Esc on Runs-initiated source: expected nil cmd (no analysis restart)")
	}
}

// TestModel_InteractionInitiatedSource_Confirm_NoPendingReplyAndNoDoubleRestart
// verifies that the Interaction-initiated source screen path (opened via a
// pending QuestionLogSourcePath, not via 's' on the Runs list) still delivers
// the answer to the pending question's channel and does NOT return an analysis
// command of its own — the analysis resumes via the Interaction port, not by
// calling buildAnalysisCmd a second time.
func TestModel_InteractionInitiatedSource_Confirm_NoPendingReplyAndNoDoubleRestart(t *testing.T) {
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionLogSourcePath, reply)

	// Route to source screen via Interaction pending question (not via 's').
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close() // prevent goroutine leak from waitForPending

	if m2.screen != screenSource {
		t.Fatalf("expected screenSource after pending question, got %q", m2.screen)
	}

	// Type a path and confirm.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/logs/root")})
	m3 := asModel(updated2)
	updated4, cmd := m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m5 := asModel(updated4)

	if m5.screen != screenLoading {
		t.Errorf("after confirm on Interaction-initiated source: screen = %q, want %q",
			m5.screen, screenLoading)
	}
	// The answer must reach the pending question channel.
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered {
			t.Errorf("answer status = %v, want Answered", ans.textAns.Status)
		}
		if ans.textAns.Text != "/logs/root" {
			t.Errorf("answer text = %q, want %q", ans.textAns.Text, "/logs/root")
		}
	default:
		t.Error("no answer on reply channel after Enter on Interaction-initiated source screen")
	}
	// No second analysis command must be started — the Interaction port drives the restart.
	if cmd != nil {
		t.Error("Interaction-initiated source confirm must not return an analysis command (double restart)")
	}
}

// TestModel_RunDetailScreen_S_DoesNotOpenSourceScreen verifies that pressing
// 's' while viewing a Run Detail screen has no effect: the model stays on the
// run detail and no source screen is opened. The source-switch shortcut is
// only wired in updateRuns, never in the shared Update prologue.
func TestModel_RunDetailScreen_S_DoesNotOpenSourceScreen(t *testing.T) {
	// Navigate to a run detail screen.
	m := newNavModel()
	report := makeReport("20260101T120000Z-abcd")
	updated, _ := m.Update(analysisDoneMsg{
		result: app.Result{Outcome: app.OutcomeReport, Report: report},
	})
	m2 := asModel(updated)
	updated2, _ := pressKeyType(m2, tea.KeyEnter)
	m3 := asModel(updated2)
	if m3.screen != screenRunDetail {
		t.Fatalf("expected screenRunDetail before pressing 's', got %q", m3.screen)
	}

	// Press 's' on the run detail screen.
	updated3, _ := pressKey(m3, "s")
	m4 := asModel(updated3)

	if m4.screen != screenRunDetail {
		t.Errorf("after 's' on run detail: screen = %q, want %q (should be unchanged)",
			m4.screen, screenRunDetail)
	}
	if m4.sourceScreen != nil {
		t.Error("after 's' on run detail: sourceScreen should remain nil")
	}
}

// ---------------------------------------------------------------------------
// Test helpers: pricing screen
// ---------------------------------------------------------------------------

// setUpPricingScreen returns a Model positioned on the Pricing screen, wired
// with a non-nil session and runsScreen so that navigation assertions are valid.
// The caller may override m.report after calling this helper.
func setUpPricingScreen(models []app.ModelPricingStatus, sess *app.Session) Model {
	m := newNavModel()
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")
	m.session = sess
	m.result = &app.Result{Outcome: app.OutcomeReport, Session: sess}

	report := makeReport("20260101T120000Z-abcd")
	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.pricingScreen = screens.NewPricingScreen(models, "/test/pricing.yaml", m.width, m.height, style)
	m.screen = screenPricing
	return m
}

// ---------------------------------------------------------------------------
// Tests: pricing screen edit round trip (T5.3)
// ---------------------------------------------------------------------------

// TestModel_PricingScreen_Enter_StartsPricingEditCmd verifies that pressing Enter
// on the pricing screen triggers a non-nil Cmd to start the pricing-editor flow.
// With the stub buildPricingEditCmd (returns nil), this test FAILS (RED).
func TestModel_PricingScreen_Enter_StartsPricingEditCmd(t *testing.T) {
	// Arrange
	sess := new(app.Session)
	models := []app.ModelPricingStatus{{Model: "model-a", Priced: false}}
	m := setUpPricingScreen(models, sess)

	// Act: press Enter to select the highlighted model.
	_, cmd := pressKeyType(m, tea.KeyEnter)

	// Assert: a non-nil Cmd must be returned to start the editor flow.
	if cmd == nil {
		t.Error("pressing Enter on the pricing screen must return a non-nil Cmd to start the editor flow")
	}
}

// TestModel_PricingEditDone_Saved_GoesToRunsWithRepricedReport verifies that
// when a pricingEditDoneMsg with Outcome == PricingEditSaved arrives, the model
// transitions to the Runs screen and applies the re-priced report.
// With the stub handlePricingEditDone (does not apply the new report), the
// report assertion FAILS (RED).
func TestModel_PricingEditDone_Saved_GoesToRunsWithRepricedReport(t *testing.T) {
	// Arrange
	origReport := makeReport("20260101T120000Z-orig")
	sess := new(app.Session)
	models := []app.ModelPricingStatus{{Model: "model-a"}}
	m := setUpPricingScreen(models, sess)
	m.report = origReport
	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(origReport, m.width, m.height, style)

	repricedReport := makeReport("20260201T120000Z-new") // different run ID

	// Act: inject a saved pricing-edit result.
	updated, _ := m.Update(pricingEditDoneMsg{
		result: app.PricingEditResult{
			Outcome:    app.PricingEditSaved,
			Model:      "model-a",
			Report:     repricedReport,
			ConfigPath: "/test/pricing.yaml",
		},
	})
	m2 := asModel(updated)

	// Assert: screen must return to Runs.
	if m2.screen != screenRuns {
		t.Errorf("after PricingEditSaved: screen = %q, want %q", m2.screen, screenRuns)
	}
	// Assert: the model must carry the re-priced report.
	if len(m2.report.Runs) == 0 {
		t.Fatal("report must be updated after PricingEditSaved; got empty Runs")
	}
	if m2.report.Runs[0].Run.ID != "20260201T120000Z-new" {
		t.Errorf("report not updated after PricingEditSaved: run id = %q, want %q",
			m2.report.Runs[0].Run.ID, "20260201T120000Z-new")
	}
}

// TestModel_PricingEditDone_Saved_SurfacesConfirmation verifies that when
// PricingEditSaved arrives, a save confirmation is surfaced in the pricing
// screen's status line, not silently dropped. The pricingScreen must render
// observable confirmation text ("written" or "saved") after the editor
// completes, so the I5.4 confirmation requirement cannot be satisfied by
// merely returning to the Runs screen.
//
// With the stub handlePricingEditDone (no SetStatus call), no confirmation
// word appears in pricingScreen.View() — this test FAILS (RED).
func TestModel_PricingEditDone_Saved_SurfacesConfirmation(t *testing.T) {
	// Arrange
	sess := new(app.Session)
	models := []app.ModelPricingStatus{{Model: "model-a", Priced: false}}
	m := setUpPricingScreen(models, sess)

	repricedReport := makeReport("20260201T120000Z-new")

	// Act: inject a saved pricing-edit result.
	updated, _ := m.Update(pricingEditDoneMsg{
		result: app.PricingEditResult{
			Outcome:    app.PricingEditSaved,
			Model:      "model-a",
			Report:     repricedReport,
			ConfigPath: "/test/pricing.yaml",
		},
	})
	m2 := asModel(updated)

	// Assert: pricingScreen must remain non-nil so the confirmation state is
	// accessible even after the model transitions to screenRuns.
	if m2.pricingScreen == nil {
		t.Fatal("pricingScreen must remain non-nil after PricingEditSaved so " +
			"the confirmation status can be retained")
	}
	// Assert: the pricing screen's view must carry a save confirmation
	// set via PricingScreen.SetStatus (or an equivalent mechanism). The exact
	// wording is not pinned; any of "written", "Written", "saved", or "Saved"
	// satisfies the binding rule that the write confirmation must be surfaced.
	view := m2.pricingScreen.View(m2.width, m2.height)
	hasConfirmation := strings.Contains(view, "written") ||
		strings.Contains(view, "Written") ||
		strings.Contains(view, "saved") ||
		strings.Contains(view, "Saved")
	if !hasConfirmation {
		t.Errorf("after PricingEditSaved: pricingScreen.View() does not carry a save "+
			"confirmation\n(expected one of: 'written', 'Written', 'saved', 'Saved');\ngot:\n%s", view)
	}
}

// TestModel_PricingEditDone_NoChange_ReturnsToRunsReportUnchanged verifies that
// when PricingEditNoChange arrives (user skipped all rate questions), the model
// returns to the Runs screen with the original report intact.
func TestModel_PricingEditDone_NoChange_ReturnsToRunsReportUnchanged(t *testing.T) {
	// Arrange
	const origRunID = "20260101T120000Z-orig"
	origReport := makeReport(origRunID)
	sess := new(app.Session)
	models := []app.ModelPricingStatus{{Model: "model-a"}}
	m := setUpPricingScreen(models, sess)
	m.report = origReport
	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(origReport, m.width, m.height, style)

	// Act: inject a no-change result (user skipped the rate questions).
	updated, _ := m.Update(pricingEditDoneMsg{
		result: app.PricingEditResult{
			Outcome: app.PricingEditNoChange,
			Model:   "model-a",
			Report:  origReport,
		},
	})
	m2 := asModel(updated)

	// Assert.
	if m2.screen != screenRuns {
		t.Errorf("after PricingEditNoChange: screen = %q, want %q", m2.screen, screenRuns)
	}
	if len(m2.report.Runs) == 0 {
		t.Fatal("report must still have runs after PricingEditNoChange")
	}
	if m2.report.Runs[0].Run.ID != origRunID {
		t.Errorf("report changed after PricingEditNoChange: run id = %q, want %q",
			m2.report.Runs[0].Run.ID, origRunID)
	}
}

// ---------------------------------------------------------------------------
// Tests: guard paths (T5.4)
// ---------------------------------------------------------------------------

// TestModel_PricingScreen_Esc_ReturnsToRunsUnchanged verifies that pressing Esc
// on the pricing screen returns to the Runs screen with the report intact.
func TestModel_PricingScreen_Esc_ReturnsToRunsUnchanged(t *testing.T) {
	// Arrange
	const origRunID = "20260101T120000Z-abcd"
	origReport := makeReport(origRunID)
	sess := new(app.Session)
	models := []app.ModelPricingStatus{{Model: "model-a", Priced: false}}
	m := setUpPricingScreen(models, sess)
	m.report = origReport
	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(origReport, m.width, m.height, style)

	// Act: press Esc to return without selecting.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := asModel(updated)

	// Assert: screen must return to Runs.
	if m2.screen != screenRuns {
		t.Errorf("after Esc on pricing screen: screen = %q, want %q", m2.screen, screenRuns)
	}
	// Assert: the report must be unchanged.
	if len(m2.report.Runs) == 0 {
		t.Fatal("report must still have runs after Esc on pricing screen")
	}
	if m2.report.Runs[0].Run.ID != origRunID {
		t.Errorf("report changed after Esc on pricing screen: run id = %q, want %q",
			m2.report.Runs[0].Run.ID, origRunID)
	}
}

// TestModel_RunsScreen_P_NoSession_StaysOnRunsScreen verifies that pressing 'p'
// on the Runs screen does nothing when there is no active session. This guards
// against navigating to a pricing screen with no session to supply model states
// or to drive the editor. The user must stay on the Runs screen.
//
// To force the old guard to pass (making this a meaningful RED test), the
// report has UnpricedModels set. The new session-based guard must override it.
func TestModel_RunsScreen_P_NoSession_StaysOnRunsScreen(t *testing.T) {
	// Arrange
	m := newNavModel()
	m.opts.Service = newServiceWithPricingPath("/test/pricing.yaml")
	// Deliberately NOT setting m.session or m.result — simulating "no analysis yet".

	// Report has unpriced models so the OLD guard would have triggered navigation;
	// the new session guard must block it instead.
	report := makeReport("20260101T120000Z-abcd")
	report.UnpricedModels = []domain.ModelID{"claude-test-model"}

	style := stylesFromTheme(m.theme)
	m.runsScreen = screens.NewRunsScreen(report, m.width, m.height, style)
	m.report = report
	m.screen = screenRuns

	// Act.
	updated, _ := pressKey(m, "p")
	m2 := asModel(updated)

	// Assert: with no session, the guard must block navigation to the pricing screen.
	if m2.screen != screenRuns {
		t.Errorf("after 'p' with no session: screen = %q, want %q — "+
			"session guard must block pricing navigation", m2.screen, screenRuns)
	}
	if m2.pricingScreen != nil {
		t.Error("pricingScreen should remain nil when there is no session")
	}
}
