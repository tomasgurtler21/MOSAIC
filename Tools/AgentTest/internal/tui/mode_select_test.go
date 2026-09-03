package tui

// mode_select_test.go covers the Stage 5 additions to the TUI:
// mode-selection and process-reports flows (T5.1–T5.5).
//
// T5.1 — mode-select navigation
// T5.2 — process-select navigation
// T5.3 — store flow (input → operation → result → back)
// T5.4 — summary flow (input → operation → result → back)
// T5.5 — existing run flow unchanged (mode-select → run tests → harness/suite)

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
)

// ---------------------------------------------------------------------------
// helpers: store / summary fakes
// ---------------------------------------------------------------------------

// fakeStoreFunc is a StoreFunc that always returns a pre-canned result.
func fakeStoreFunc(result resultstore.StoreResult, err error) StoreFunc {
	return func(req resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error) {
		return result, err
	}
}

// fakeSummaryFunc is a SummaryFunc that always returns a pre-canned result.
func fakeSummaryFunc(result resultsummary.SummaryResult, err error) SummaryFunc {
	return func(req resultsummary.SummaryRequest) (resultsummary.SummaryResult, error) {
		return result, err
	}
}

// newProcessOptions returns Options wired for the process-reports flow with
// both Store and Summary stubbed out.
func newProcessOptions() Options {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Store = fakeStoreFunc(resultstore.StoreResult{Stored: 3}, nil)
	o.Summary = fakeSummaryFunc(resultsummary.SummaryResult{FilesWritten: []string{"TestResults/v1/user-summary.md"}}, nil)
	o.TestResultsRoot = "/TestResults"
	return o
}

// advanceToProcessSelect navigates from ScreenModeSelect to ScreenProcessSelect
// by selecting "Process Test Reports" (cursor down, then Enter).
func advanceToProcessSelect(t *testing.T, m Model) Model {
	t.Helper()
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("advanceToProcessSelect: expected ScreenModeSelect, got %q", m.Screen())
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor to "Process Test Reports"
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // Enter: select
	if m.Screen() != ScreenProcessSelect {
		t.Fatalf("advanceToProcessSelect: Screen() = %q, want %q", m.Screen(), ScreenProcessSelect)
	}
	return m
}

// ---------------------------------------------------------------------------
// T5.1 — mode-select navigation
// ---------------------------------------------------------------------------

// TestModeSelect_InitialScreen verifies that NewModel starts on ScreenModeSelect.
func TestModeSelect_InitialScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("NewModel Screen() = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestModeSelect_RunTests_EntersRunFlow verifies that pressing Enter on the
// "Run Tests" item (default cursor position) transitions to the run flow.
// When no harness catalog is wired, that means ScreenSuiteSelect.
func TestModeSelect_RunTests_EntersRunFlow(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter on "Run Tests" (cursor 0)
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after 'Run Tests' selection = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestModeSelect_RunTests_WithHarnesses_EntersHarnessSelect verifies that
// when harnesses are offered, "Run Tests" leads to ScreenHarnessSelect.
func TestModeSelect_RunTests_WithHarnesses_EntersHarnessSelect(t *testing.T) {
	o := newHarnessFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	m := NewModel(o)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter on "Run Tests" (cursor 0)
	if m.Screen() != ScreenHarnessSelect {
		t.Errorf("Screen() after 'Run Tests' with harness catalog = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}
}

// TestModeSelect_ProcessTestReports_EntersProcessSelect verifies that pressing
// Down then Enter on "Process Test Reports" transitions to ScreenProcessSelect.
func TestModeSelect_ProcessTestReports_EntersProcessSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	// assertion already in advanceToProcessSelect; screen check here is belt-and-suspenders
	if m.Screen() != ScreenProcessSelect {
		t.Errorf("Screen() = %q, want %q", m.Screen(), ScreenProcessSelect)
	}
}

// TestModeSelect_CursorWraps verifies that the mode-select cursor stays within
// bounds: moving Down past the last item and Up past the first item has no effect.
func TestModeSelect_CursorWraps(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	// Already at cursor 0 ("Run Tests"). Move Up: must stay at 0.
	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("unexpected screen after Up: %q", m.Screen())
	}
	// Move Down twice to reach end; second Down must clamp.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor → 1
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor → stays at 1
	// Select "Process Test Reports" (cursor 1) — if cursor had gone to 2 we'd panic.
	m2, _ := safeUpdate(t, m, keyMsg("\r"))
	if m2.Screen() != ScreenProcessSelect {
		t.Errorf("Screen() after double-Down + Enter = %q, want %q (cursor must clamp at last item)", m2.Screen(), ScreenProcessSelect)
	}
}

// TestModeSelect_Quit_CtrlC verifies that Ctrl+C from ScreenModeSelect quits
// (issues the tea.Quit command).
func TestModeSelect_Quit_CtrlC(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	_, cmd := safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("Ctrl+C from ScreenModeSelect returned nil cmd, want tea.Quit")
	}
	msg := runCmd(t, cmd)
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+C cmd returned %T, want tea.QuitMsg", msg)
	}
}

// TestModeSelect_Quit_Q verifies that pressing "q" from ScreenModeSelect quits
// (issues the tea.Quit command), mirroring the existing Ctrl+C test.
func TestModeSelect_Quit_Q(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	_, cmd := safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("'q' from ScreenModeSelect returned nil cmd, want tea.Quit")
	}
	msg := runCmd(t, cmd)
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("'q' cmd returned %T, want tea.QuitMsg", msg)
	}
}

// TestModeSelect_View_ContainsModeItems verifies that the mode-select view
// contains the expected item labels.
func TestModeSelect_View_ContainsModeItems(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	view := safeView(t, m)
	for _, label := range []string{"Run Tests", "Process Test Reports"} {
		if !strings.Contains(view, label) {
			t.Errorf("mode-select view does not contain %q:\n%s", label, view)
		}
	}
}

// TestModeSelect_ProcessReports_Unavailable_WhenNoFuncs verifies that when
// neither Store nor Summary is wired, "Process Test Reports" shows an
// "(unavailable)" annotation.
func TestModeSelect_ProcessReports_Unavailable_WhenNoFuncs(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	// No Store or Summary wired.
	m := NewModel(o)
	view := safeView(t, m)
	if !strings.Contains(view, "unavailable") {
		t.Errorf("mode-select view does not show 'unavailable' when neither Store nor Summary is wired:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — process-select navigation
// ---------------------------------------------------------------------------

// TestProcessSelect_StoreReports_EntersStoreInput verifies that Enter on
// "Store Reports" (cursor 0) transitions to ScreenStoreInput.
func TestProcessSelect_StoreReports_EntersStoreInput(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter on "Store Reports" (cursor 0)
	if m.Screen() != ScreenStoreInput {
		t.Errorf("Screen() after 'Store Reports' selection = %q, want %q", m.Screen(), ScreenStoreInput)
	}
}

// TestProcessSelect_GenerateSummary_EntersSummaryInput verifies that pressing
// Down then Enter on "Generate Summary" transitions to ScreenSummaryInput.
func TestProcessSelect_GenerateSummary_EntersSummaryInput(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor to "Generate Summary"
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // Enter
	if m.Screen() != ScreenSummaryInput {
		t.Errorf("Screen() after 'Generate Summary' selection = %q, want %q", m.Screen(), ScreenSummaryInput)
	}
}

// TestProcessSelect_Esc_ReturnToModeSelect verifies that Esc from
// ScreenProcessSelect returns to ScreenModeSelect.
func TestProcessSelect_Esc_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Esc from process-select = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestProcessSelect_Backspace_ReturnToModeSelect verifies that Backspace from
// ScreenProcessSelect returns to ScreenModeSelect, mirroring the existing Esc test.
func TestProcessSelect_Backspace_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyBackspace))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Backspace from process-select = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestProcessSelect_View_ContainsItems verifies that the process-select view
// shows both "Store Reports" and "Generate Summary".
func TestProcessSelect_View_ContainsItems(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	view := safeView(t, m)
	for _, label := range []string{"Store Reports", "Generate Summary"} {
		if !strings.Contains(view, label) {
			t.Errorf("process-select view does not contain %q:\n%s", label, view)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 — store flow
// ---------------------------------------------------------------------------

// advanceToStoreResult navigates through mode-select → process-select →
// store-input, types a path, confirms it, runs the returned cmd to deliver
// StoreFinishedMsg, then drives that msg into the model, returning the model
// on ScreenStoreResult.
func advanceToStoreResult(t *testing.T, m Model, path string) Model {
	t.Helper()
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // "Store Reports" → ScreenStoreInput

	// Type the path.
	for _, r := range path {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Confirm.
	var storeCmd tea.Cmd
	m, storeCmd = safeUpdate(t, m, keyMsg("\r"))
	// Model transitions to ScreenStoreResult immediately (before the async msg).
	if m.Screen() != ScreenStoreResult {
		t.Fatalf("Screen() after store path confirm = %q, want %q", m.Screen(), ScreenStoreResult)
	}
	// Run the goroutine cmd to get StoreFinishedMsg.
	if storeCmd != nil {
		msg := runCmd(t, storeCmd)
		m, _ = safeUpdate(t, m, msg)
	}
	return m
}

// TestStoreFlow_InputScreen_EntersOnStoreSelect verifies that entering
// "Store Reports" in process-select leads to ScreenStoreInput.
func TestStoreFlow_InputScreen_EntersOnStoreSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // "Store Reports"
	if m.Screen() != ScreenStoreInput {
		t.Errorf("Screen() = %q, want %q", m.Screen(), ScreenStoreInput)
	}
}

// TestStoreFlow_ConfirmPath_TransitionsToStoreResult verifies that typing a
// path and pressing Enter transitions to ScreenStoreResult.
func TestStoreFlow_ConfirmPath_TransitionsToStoreResult(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToStoreResult(t, m, "/tmp/reports")
	if m.Screen() != ScreenStoreResult {
		t.Errorf("Screen() = %q, want %q", m.Screen(), ScreenStoreResult)
	}
}

// TestStoreFlow_Result_ShowsSummaryLine verifies that the store-result screen
// renders the StoreResult's summary line.
func TestStoreFlow_Result_ShowsSummaryLine(t *testing.T) {
	m := NewModel(newProcessOptions()) // StoreFunc returns Stored: 3
	m = advanceToStoreResult(t, m, "/tmp/reports")
	view := safeView(t, m)
	if !strings.Contains(view, "Stored 3") {
		t.Errorf("store-result view = %q; want it to contain store summary (e.g. 'Stored 3')", view)
	}
}

// TestStoreFlow_Result_Enter_ReturnToModeSelect verifies that pressing Enter
// on the store-result screen returns to ScreenModeSelect.
func TestStoreFlow_Result_Enter_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToStoreResult(t, m, "/tmp/reports")
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Enter on store-result = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestStoreFlow_Result_Esc_ReturnToModeSelect verifies that pressing Esc
// on the store-result screen also returns to ScreenModeSelect.
func TestStoreFlow_Result_Esc_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToStoreResult(t, m, "/tmp/reports")
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Esc on store-result = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestStoreFlow_StoreInput_Esc_ReturnToProcessSelect verifies that Esc from
// ScreenStoreInput returns to ScreenProcessSelect (not ScreenModeSelect).
func TestStoreFlow_StoreInput_Esc_ReturnToProcessSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // "Store Reports" → ScreenStoreInput
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenProcessSelect {
		t.Errorf("Screen() after Esc from store-input = %q, want %q", m.Screen(), ScreenProcessSelect)
	}
}

// TestStoreFlow_Error_ShowsErrorMessage verifies that when StoreFunc returns
// an error, the store-result screen shows an error message.
func TestStoreFlow_Error_ShowsErrorMessage(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Store = fakeStoreFunc(resultstore.StoreResult{}, fmt.Errorf("disk full"))
	o.TestResultsRoot = "/TestResults"
	m := NewModel(o)
	m = advanceToStoreResult(t, m, "/tmp/reports")
	view := safeView(t, m)
	if !strings.Contains(view, "disk full") {
		t.Errorf("store-result view with error does not mention 'disk full':\n%s", view)
	}
}

// TestStoreFlow_JsonPath_SendsAsFile verifies that a path ending in .json is
// treated as a file (not a directory). We test this indirectly by confirming
// the flow completes without panic and reaches ScreenStoreResult.
func TestStoreFlow_JsonPath_CompletesFlow(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToStoreResult(t, m, "report.json")
	if m.Screen() != ScreenStoreResult {
		t.Errorf("Screen() = %q, want %q (json path must complete store flow)", m.Screen(), ScreenStoreResult)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — summary flow
// ---------------------------------------------------------------------------

// advanceToSummaryResult navigates through mode-select → process-select →
// summary-input, optionally types a version filter, confirms, runs the cmd
// to deliver SummaryFinishedMsg, and returns the model on ScreenSummaryResult.
func advanceToSummaryResult(t *testing.T, m Model, versionFilter string) Model {
	t.Helper()
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor to "Generate Summary"
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // → ScreenSummaryInput

	// Type the version filter if non-empty.
	for _, r := range versionFilter {
		m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Confirm.
	var summaryCmd tea.Cmd
	m, summaryCmd = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenSummaryResult {
		t.Fatalf("Screen() after summary confirm = %q, want %q", m.Screen(), ScreenSummaryResult)
	}
	// Run the goroutine cmd to get SummaryFinishedMsg.
	if summaryCmd != nil {
		msg := runCmd(t, summaryCmd)
		m, _ = safeUpdate(t, m, msg)
	}
	return m
}

// TestSummaryFlow_InputScreen_EntersOnSummarySelect verifies that selecting
// "Generate Summary" from process-select leads to ScreenSummaryInput.
func TestSummaryFlow_InputScreen_EntersOnSummarySelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenSummaryInput {
		t.Errorf("Screen() = %q, want %q", m.Screen(), ScreenSummaryInput)
	}
}

// TestSummaryFlow_ConfirmFilter_TransitionsToSummaryResult verifies that
// confirming the summary input (empty or non-empty filter) transitions to
// ScreenSummaryResult.
func TestSummaryFlow_ConfirmFilter_TransitionsToSummaryResult(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToSummaryResult(t, m, "") // empty filter = all versions
	if m.Screen() != ScreenSummaryResult {
		t.Errorf("Screen() = %q, want %q", m.Screen(), ScreenSummaryResult)
	}
}

// TestSummaryFlow_Result_ShowsFilesWritten verifies that the summary-result
// screen renders the paths of written summary files.
func TestSummaryFlow_Result_ShowsFilesWritten(t *testing.T) {
	m := NewModel(newProcessOptions()) // SummaryFunc returns FilesWritten: ["TestResults/v1/user-summary.md"]
	m = advanceToSummaryResult(t, m, "")
	view := safeView(t, m)
	if !strings.Contains(view, "user-summary.md") {
		t.Errorf("summary-result view does not contain 'user-summary.md':\n%s", view)
	}
}

// TestSummaryFlow_Result_Enter_ReturnToModeSelect verifies that pressing Enter
// on the summary-result screen returns to ScreenModeSelect.
func TestSummaryFlow_Result_Enter_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToSummaryResult(t, m, "")
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Enter on summary-result = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestSummaryFlow_Result_Esc_ReturnToModeSelect verifies that pressing Esc
// on the summary-result screen also returns to ScreenModeSelect.
func TestSummaryFlow_Result_Esc_ReturnToModeSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToSummaryResult(t, m, "")
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after Esc on summary-result = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestSummaryFlow_SummaryInput_Esc_ReturnToProcessSelect verifies that Esc
// from ScreenSummaryInput returns to ScreenProcessSelect.
func TestSummaryFlow_SummaryInput_Esc_ReturnToProcessSelect(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor to "Generate Summary"
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // → ScreenSummaryInput
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenProcessSelect {
		t.Errorf("Screen() after Esc from summary-input = %q, want %q", m.Screen(), ScreenProcessSelect)
	}
}

// TestSummaryFlow_WithVersionFilter_CompletesFlow verifies that typing a
// version filter does not break the flow.
func TestSummaryFlow_WithVersionFilter_CompletesFlow(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToSummaryResult(t, m, "v1.2.3")
	if m.Screen() != ScreenSummaryResult {
		t.Errorf("Screen() = %q, want %q (version filter must complete summary flow)", m.Screen(), ScreenSummaryResult)
	}
}

// ---------------------------------------------------------------------------
// T5.5 — existing run flow unchanged
// ---------------------------------------------------------------------------

// TestRunFlow_FromModeSelect_SuiteSelectFollows verifies the complete path
// mode-select → "Run Tests" → suite-select → settings → progress.
func TestRunFlow_FromModeSelect_SuiteSelectFollows(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenModeSelect)
	}
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter: "Run Tests" → ScreenSuiteSelect
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after 'Run Tests' = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	// Proceed through settings to ScreenProgress — run flow must be unaffected.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // suite-select → ScreenRetention
	if m.Screen() != ScreenRetention {
		t.Fatalf("Screen() after suite confirm = %q, want %q", m.Screen(), ScreenRetention)
	}
	m, cmd := advanceThroughSettingsToProgress(t, m)
	if m.Screen() != ScreenProgress {
		t.Errorf("Screen() after settings = %q, want %q (existing run flow must be unchanged)", m.Screen(), ScreenProgress)
	}
	_ = cmd
}

// TestRunFlow_ProcessReportsNeverEnteredAfterRunTests verifies that completing
// a "Run Tests" flow does not automatically lead to ScreenProcessSelect or any
// process-reports screen.
func TestRunFlow_ProcessReportsNeverEnteredAfterRunTests(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	// Navigate to ScreenProgress.
	m = advanceToRunFlow(t, m)
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
	// Run the suite to completion.
	if cmd != nil {
		msg := runCmd(t, cmd)
		if msg != nil {
			m, _ = safeUpdate(t, m, msg)
		}
	}
	processScreens := []Screen{ScreenProcessSelect, ScreenStoreInput, ScreenStoreResult, ScreenSummaryInput, ScreenSummaryResult}
	for _, s := range processScreens {
		if m.Screen() == s {
			t.Errorf("after run completion, Screen() = %q; process-reports screens must never be entered automatically after a run", s)
		}
	}
}

// TestRunFlow_FullFlow_ModeSelectToResults verifies the end-to-end navigation
// from ScreenModeSelect to ScreenResults via the run flow, ensuring all
// existing screens are reachable and none are skipped.
func TestRunFlow_FullFlow_ModeSelectToResults(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// Mode select → suite select.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after mode-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Run through to ScreenResults.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Errorf("Screen() at end of full run flow = %q, want %q (run flow must reach results unchanged)", m.Screen(), ScreenResults)
	}
}

// ---------------------------------------------------------------------------
// MAJOR finding: per-file path detail in store-result (AC5.4 "paths")
// ---------------------------------------------------------------------------

// capturingStoreFunc returns a StoreFunc that records the most recent request
// it received, and a pointer through which the caller can inspect it after the
// call. Used to verify the TUI builds the correct StoreFromPathsRequest.
func capturingStoreFunc(result resultstore.StoreResult, err error) (StoreFunc, *resultstore.StoreFromPathsRequest) {
	var captured resultstore.StoreFromPathsRequest
	fn := func(req resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error) {
		captured = req
		return result, err
	}
	return fn, &captured
}

// TestStoreFlow_Result_ShowsPerFileDetail verifies that the store-result screen
// renders per-file TargetPath for stored reports and SkipReason for skipped
// reports from StoreResult.Reports — satisfying AC5.4's "counts and paths"
// requirement with structured data.
func TestStoreFlow_Result_ShowsPerFileDetail(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	// Use short paths so the view does not truncate them.
	targetPath := "TestResults/v1/stored.json"
	o.Store = fakeStoreFunc(resultstore.StoreResult{
		Stored: 1,
		Reports: []resultstore.StoredReport{
			{
				SourcePath: "src.json",
				TargetPath: targetPath,
				Skipped:    false,
			},
			{
				SourcePath: "skip.json",
				Skipped:    true,
				SkipReason: resultstore.SkipUnknownVersion,
				Message:    "unknown version",
			},
		},
	}, nil)
	o.TestResultsRoot = "/TestResults"
	m := NewModel(o)
	m = advanceToStoreResult(t, m, "/tmp/reports")
	view := safeView(t, m)
	if !strings.Contains(view, targetPath) {
		t.Errorf("store-result view does not contain TargetPath %q:\n%s", targetPath, view)
	}
	if !strings.Contains(view, string(resultstore.SkipUnknownVersion)) {
		t.Errorf("store-result view does not contain SkipReason %q:\n%s", string(resultstore.SkipUnknownVersion), view)
	}
}

// ---------------------------------------------------------------------------
// Minor finding: FilesUpdated display in summary-result
// ---------------------------------------------------------------------------

// TestSummaryFlow_Result_ShowsFilesUpdated verifies that when SummaryResult has
// FilesUpdated entries (files refreshed rather than newly created), those paths
// appear in the summary-result view.
func TestSummaryFlow_Result_ShowsFilesUpdated(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	updatedPath := "TestResults/v2/user-summary.md"
	o.Summary = fakeSummaryFunc(resultsummary.SummaryResult{
		FilesUpdated: []string{updatedPath},
	}, nil)
	o.TestResultsRoot = "/TestResults"
	m := NewModel(o)
	m = advanceToSummaryResult(t, m, "")
	view := safeView(t, m)
	if !strings.Contains(view, updatedPath) {
		t.Errorf("summary-result view does not show FilesUpdated path %q:\n%s", updatedPath, view)
	}
}

// ---------------------------------------------------------------------------
// Minor finding: StoreFromPathsRequest routing for .json path
// ---------------------------------------------------------------------------

// TestStoreFlow_JsonPath_SendsAsFile verifies that the TUI routes a .json
// path into StoreFromPathsRequest.Files (not Dir). A TUI bug that places a
// .json path in Dir would cause a mutual-exclusion error from StoreFromPaths.
func TestStoreFlow_JsonPath_SendsAsFile(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	storeFn, capturedReq := capturingStoreFunc(resultstore.StoreResult{Stored: 1}, nil)
	o.Store = storeFn
	o.TestResultsRoot = "/TestResults"
	m := NewModel(o)
	m = advanceToStoreResult(t, m, "report.json")
	if capturedReq.Dir != "" {
		t.Errorf("TUI routed .json path into StoreFromPathsRequest.Dir = %q; want it in Files", capturedReq.Dir)
	}
	if len(capturedReq.Files) != 1 || capturedReq.Files[0] != "report.json" {
		t.Errorf("StoreFromPathsRequest.Files = %v; want [\"report.json\"]", capturedReq.Files)
	}
}

// ---------------------------------------------------------------------------
// Minor finding: input screen view content
// ---------------------------------------------------------------------------

// TestStoreInput_View_ContainsPathPrompt verifies that the store-input screen's
// view contains a prompt directing the user to enter a file or directory path.
func TestStoreInput_View_ContainsPathPrompt(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // "Store Reports" → ScreenStoreInput
	if m.Screen() != ScreenStoreInput {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenStoreInput)
	}
	view := safeView(t, m)
	if !strings.Contains(view, "file path") {
		t.Errorf("store-input view does not contain 'file path' path prompt:\n%s", view)
	}
}

// TestSummaryInput_View_ContainsVersionFilterTerminology verifies that the
// summary-input screen uses the agreed --for-version terminology (FR-23),
// matching the CLI flag defined by the Stage 3 implementation.
func TestSummaryInput_View_ContainsVersionFilterTerminology(t *testing.T) {
	m := NewModel(newProcessOptions())
	m = advanceToProcessSelect(t, m)
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // cursor to "Generate Summary"
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // → ScreenSummaryInput
	if m.Screen() != ScreenSummaryInput {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSummaryInput)
	}
	view := safeView(t, m)
	if !strings.Contains(view, "--for-version") {
		t.Errorf("summary-input view does not contain '--for-version' terminology:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Minor finding: partial-nil wiring (one of Store/Summary nil, other non-nil)
// ---------------------------------------------------------------------------

// TestModeSelect_PartialWiring_StoreNil_StillOffersProcessReports verifies that
// when only Summary is wired (Store is nil), "Process Test Reports" is NOT
// marked unavailable. The unavailable annotation is only shown when both funcs
// are nil.
func TestModeSelect_PartialWiring_StoreNil_StillOffersProcessReports(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Summary = fakeSummaryFunc(resultsummary.SummaryResult{}, nil)
	// Store intentionally nil — only Summary is wired.
	m := NewModel(o)
	view := safeView(t, m)
	if strings.Contains(view, "unavailable") {
		t.Errorf("mode-select marks 'Process Test Reports' unavailable when Summary is wired (Store nil);\n"+
			"want it available since at least one func is wired:\n%s", view)
	}
}

// TestModeSelect_PartialWiring_SummaryNil_StillOffersProcessReports verifies
// that when only Store is wired (Summary is nil), "Process Test Reports" is
// NOT marked unavailable.
func TestModeSelect_PartialWiring_SummaryNil_StillOffersProcessReports(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Store = fakeStoreFunc(resultstore.StoreResult{}, nil)
	// Summary intentionally nil — only Store is wired.
	m := NewModel(o)
	view := safeView(t, m)
	if strings.Contains(view, "unavailable") {
		t.Errorf("mode-select marks 'Process Test Reports' unavailable when Store is wired (Summary nil);\n"+
			"want it available since at least one func is wired:\n%s", view)
	}
}
