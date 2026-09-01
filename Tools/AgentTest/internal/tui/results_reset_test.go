package tui

// results_reset_test.go verifies the back-key behavior on the results screen
// and the whole-state reset it triggers. Pressing the back key on the results
// screen must move to the mode-select screen and restore the application to
// a freshly-constructed state so a user can start a different run without
// restarting the program.
//
// Tests are in the internal package tui so they can read unexported Model
// fields directly, matching the established convention in this test suite.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
)

// ---------------------------------------------------------------------------
// Back key on results navigates to mode-select; select key still drills in
// ---------------------------------------------------------------------------

// TestResultsReset_BackKeyMovesToModeSelect verifies that pressing the back
// key (esc) on the results screen navigates to the mode-select screen.
func TestResultsReset_BackKeyMovesToModeSelect(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-a", verdict: domain.VerdictPass},
			},
		}.events()...,
	)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want %q", m.Screen(), ScreenResults)
	}

	// Act: press back (esc) on the results screen.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))

	// Assert: the application is now on mode-select.
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after back on results = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// TestResultsReset_SelectKeyStillDrillsIntoDetail verifies that the select
// (enter) key on the results screen still opens the selected test's detail
// after the back binding is added — the two handlers must not interfere.
func TestResultsReset_SelectKeyStillDrillsIntoDetail(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-a", verdict: domain.VerdictPass},
				{testID: "test-b", verdict: domain.VerdictFail, failed: []string{"assertion"}},
			},
		}.events()...,
	)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want %q", m.Screen(), ScreenResults)
	}

	// Act: cursor down then enter (select, not back).
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	// Assert: detail screen — enter drills in rather than exiting.
	if m.Screen() != ScreenDetail {
		t.Errorf("Screen() after down+enter on results = %q, want %q", m.Screen(), ScreenDetail)
	}
}

// ---------------------------------------------------------------------------
// Chained back: detail -> results -> mode-select
// ---------------------------------------------------------------------------

// TestResultsReset_DetailBackThenResultsBackReachesModeSelect verifies the
// full chain: back from detail returns to results (existing), then back from
// results now returns to mode-select (new).
func TestResultsReset_DetailBackThenResultsBackReachesModeSelect(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-a", verdict: domain.VerdictPass},
				{testID: "test-b", verdict: domain.VerdictFail, failed: []string{"assertion"}},
			},
		}.events()...,
	)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	// Drill into detail.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if m.Screen() != ScreenDetail {
		t.Fatalf("Screen() = %q, want ScreenDetail after drilling in", m.Screen())
	}

	// Back from detail must return to results (existing, unchanged behavior).
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenResults {
		t.Errorf("Screen() after back from detail = %q, want %q", m.Screen(), ScreenResults)
	}

	// Back from results must now reach mode-select (new behavior).
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Errorf("Screen() after back from results = %q, want %q", m.Screen(), ScreenModeSelect)
	}
}

// ---------------------------------------------------------------------------
// Run state matches a freshly constructed model after reset
// ---------------------------------------------------------------------------

// TestResultsReset_RunStateMatchesFreshModel verifies that pressing back on
// the results screen clears every piece of run state. The post-reset model is
// compared field-by-field against a freshly constructed one (same Options).
// The only fields allowed to differ are sinkBox, width and height, which must
// survive the reset (tested separately).
func TestResultsReset_RunStateMatchesFreshModel(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-a", verdict: domain.VerdictFail, failed: []string{"an assertion"}},
			},
		}.events()...,
	)
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	m := NewModel(opts)
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want ScreenResults", m.Screen())
	}

	// Verify there is run state to clear before the reset.
	if len(m.suiteResults) == 0 && m.result == nil && len(m.finished) == 0 {
		t.Log("warning: no run state detected before pressing back; the reset assertion may be vacuous")
	}

	// Act: press back on results.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect; reset requires back to move the screen first", m.Screen())
	}

	// Compare run-state fields against a freshly constructed model.
	fresh := NewModel(opts)

	if len(m.suiteResults) != 0 {
		t.Errorf("suiteResults len = %d, want 0 after reset", len(m.suiteResults))
	}
	if m.session != nil {
		t.Errorf("session is non-nil after reset, want nil")
	}
	if m.result != nil {
		t.Errorf("result is non-nil after reset, want nil")
	}
	if m.resultErr != nil {
		t.Errorf("resultErr = %v after reset, want nil", m.resultErr)
	}
	if len(m.finished) != 0 {
		t.Errorf("finished len = %d, want 0 after reset", len(m.finished))
	}
	if m.TotalTests() != fresh.TotalTests() {
		t.Errorf("TotalTests() = %d after reset, want %d", m.TotalTests(), fresh.TotalTests())
	}
	if len(m.Running()) != 0 {
		t.Errorf("Running() len = %d after reset, want 0", len(m.Running()))
	}
	if m.resultsCursor != fresh.resultsCursor {
		t.Errorf("resultsCursor = %d after reset, want %d", m.resultsCursor, fresh.resultsCursor)
	}
	if m.detailScrollOffset != fresh.detailScrollOffset {
		t.Errorf("detailScrollOffset = %d after reset, want %d", m.detailScrollOffset, fresh.detailScrollOffset)
	}
	if m.statusMsg != "" {
		t.Errorf("statusMsg = %q after reset, want empty", m.statusMsg)
	}
	if m.statusError {
		t.Errorf("statusError = true after reset, want false")
	}
	if m.showFailureDetail {
		t.Errorf("showFailureDetail = true after reset, want false")
	}
	if m.failureTitle != "" {
		t.Errorf("failureTitle = %q after reset, want empty", m.failureTitle)
	}
	if m.failureBody != "" {
		t.Errorf("failureBody = %q after reset, want empty", m.failureBody)
	}
	if len(m.selectedSuites) != 0 {
		t.Errorf("selectedSuites len = %d after reset, want 0", len(m.selectedSuites))
	}
	if len(m.preflightedPlans) != 0 {
		t.Errorf("preflightedPlans len = %d after reset, want 0", len(m.preflightedPlans))
	}
	if m.selectedSuiteIdx != fresh.selectedSuiteIdx {
		t.Errorf("selectedSuiteIdx = %d after reset, want %d", m.selectedSuiteIdx, fresh.selectedSuiteIdx)
	}
	if m.queueRunsTotal != fresh.queueRunsTotal {
		t.Errorf("queueRunsTotal = %d after reset, want %d", m.queueRunsTotal, fresh.queueRunsTotal)
	}
	if m.completedSuiteRuns != fresh.completedSuiteRuns {
		t.Errorf("completedSuiteRuns = %d after reset, want %d", m.completedSuiteRuns, fresh.completedSuiteRuns)
	}
	if m.reportPath != opts.ReportPath {
		t.Errorf("reportPath = %q after reset, want %q (initial)", m.reportPath, opts.ReportPath)
	}
}

// ---------------------------------------------------------------------------
// Run-configuration settings are reset to freshly-constructed values
// ---------------------------------------------------------------------------

// TestResultsReset_RunConfigSettingsAreReset verifies that every
// run-configuration setting — harness, subject model, stub model, retention,
// repetitions, catalog folder and maximum concurrent runs — returns to its
// freshly-constructed value after the back-key reset. The test changes every
// setting from its initial value before running, so any setting the reset
// omits produces a clear failure.
func TestResultsReset_RunConfigSettingsAreReset(t *testing.T) {
	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.Retention = domain.RetainNever
	opts.Harness = "initial-harness"
	opts.CatalogFolder = "/initial/catalog"
	opts.ReportPath = "/initial/report.json"
	m := NewModel(opts)

	// Change every run-configuration setting from its initial value to
	// simulate a user who customised the run before starting.
	rep := 5
	m.repetitions = &rep
	maxRuns := 3
	m.maxConcurrentRuns = &maxRuns
	m.retention = domain.RetainAlways
	m.selectedHarness = "changed-harness"
	m.selectedSubjectModel = "subject-model-override"
	m.selectedStubModel = "stub-model-override"
	m.catalogFolder = "/changed/catalog"
	m.reportPath = "/changed/report.json"

	// Run a suite to reach the results screen.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want ScreenResults", m.Screen())
	}

	// Act: press back on results.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect after back", m.Screen())
	}

	// Assert: all run-configuration settings match the freshly-constructed values.
	fresh := NewModel(opts)

	if m.retention != fresh.retention {
		t.Errorf("retention = %q after reset, want %q", m.retention, fresh.retention)
	}
	if m.repetitions != nil {
		t.Errorf("repetitions = %d after reset, want nil", *m.repetitions)
	}
	if m.maxConcurrentRuns != nil {
		t.Errorf("maxConcurrentRuns = %d after reset, want nil", *m.maxConcurrentRuns)
	}
	if m.selectedHarness != fresh.selectedHarness {
		t.Errorf("selectedHarness = %q after reset, want %q", m.selectedHarness, fresh.selectedHarness)
	}
	if m.selectedSubjectModel != fresh.selectedSubjectModel {
		t.Errorf("selectedSubjectModel = %q after reset, want %q", m.selectedSubjectModel, fresh.selectedSubjectModel)
	}
	if m.selectedStubModel != fresh.selectedStubModel {
		t.Errorf("selectedStubModel = %q after reset, want %q", m.selectedStubModel, fresh.selectedStubModel)
	}
	if m.catalogFolder != fresh.catalogFolder {
		t.Errorf("catalogFolder = %q after reset, want %q", m.catalogFolder, fresh.catalogFolder)
	}
	if m.reportPath != fresh.reportPath {
		t.Errorf("reportPath = %q after reset, want %q", m.reportPath, fresh.reportPath)
	}
}

// ---------------------------------------------------------------------------
// Second run after the reset behaves as a first run
// ---------------------------------------------------------------------------

// TestResultsReset_SecondRunShowsNoResultsFromPreviousRun drives two complete
// runs separated by the back-key reset and verifies that the second run's
// results screen shows no test from the first run. The two runs use runners
// with distinctly named tests so any carryover of suiteResults is detectable.
func TestResultsReset_SecondRunShowsNoResultsFromPreviousRun(t *testing.T) {
	// First run: a test named only in this run.
	firstRunner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "first-suite",
			tests: []scriptedTest{
				{testID: "first-only-test", verdict: domain.VerdictFail, failed: []string{"failed"}},
			},
		}.events()...,
	)
	opts := newFixtureOptions([]string{"suite-a.yaml"}, firstRunner)
	m := NewModel(opts)
	m = runSuiteToCompletion(t, m, firstRunner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after first run = %q, want ScreenResults", m.Screen())
	}

	// Reset: back on results.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect after back; second-run test cannot proceed", m.Screen())
	}

	// Second run: a different runner with a different test name.
	secondRunner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "second-suite",
			tests: []scriptedTest{
				{testID: "second-only-test", verdict: domain.VerdictPass},
			},
		}.events()...,
	)
	m.opts.Suite = secondRunner

	// Navigate through mode-select and run the second suite.
	m = runSuiteToCompletion(t, m, secondRunner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after second run = %q, want ScreenResults", m.Screen())
	}

	// Assert: the first run's test does not appear in the results.
	for _, tr := range m.resultTests() {
		if tr.TestName == "first-only-test" {
			t.Errorf("results after second run contain first-only-test from the previous run; "+
				"reset did not clear suiteResults")
		}
	}
}

// TestResultsReset_ReportPathResetsToInitialValue verifies that after the
// back-key reset, the report path is back at the Options-initial value so a
// second run resolves its path from the newly selected suite rather than
// inheriting the previous run's mutated path.
func TestResultsReset_ReportPathResetsToInitialValue(t *testing.T) {
	const initialPath = "/reports/initial.json"

	runner := newFakeSuiteRunner()
	opts := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	opts.ReportPath = initialPath
	m := NewModel(opts)

	// Run to results.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want ScreenResults", m.Screen())
	}

	// Simulate handleSuiteFinished having mutated the report path (as it
	// does when ReportPathFor produces a timestamped filename).
	m.reportPath = "/reports/suite-a.yaml-20260828T120000Z-abcd.json"

	// Act: press back.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect after back", m.Screen())
	}

	// Assert: report path is restored to the Options-initial value.
	if m.reportPath != initialPath {
		t.Errorf("reportPath after reset = %q, want %q (initial)", m.reportPath, initialPath)
	}
}

// ---------------------------------------------------------------------------
// Terminal dimensions and the progress sink survive the reset
// ---------------------------------------------------------------------------

// TestResultsReset_DimensionsSurviveReset verifies that the terminal
// dimensions established by a resize event are preserved through the reset.
// The reset rebuilds the model from Options but must carry width and height
// across rather than reverting to DefaultWidth/DefaultHeight.
func TestResultsReset_DimensionsSurviveReset(t *testing.T) {
	const wantWidth = 200
	const wantHeight = 60

	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// Apply a resize to establish non-default dimensions before the run.
	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: wantWidth, Height: wantHeight})
	if m.width != wantWidth || m.height != wantHeight {
		t.Fatalf("resize did not take effect before the run: width=%d height=%d", m.width, m.height)
	}

	// Run to results.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want ScreenResults", m.Screen())
	}

	// Act: press back.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect after back", m.Screen())
	}

	// Assert: dimensions are unchanged after reset.
	if m.width != wantWidth {
		t.Errorf("width = %d after reset, want %d (terminal dimensions must survive the reset)", m.width, wantWidth)
	}
	if m.height != wantHeight {
		t.Errorf("height = %d after reset, want %d (terminal dimensions must survive the reset)", m.height, wantHeight)
	}
}

// TestResultsReset_ProgressSinkSurvivesReset verifies that the shared
// progress-sink holder (sinkBox) carries the same pointer through the reset.
// The running program installs its sink into the box after NewModel returns;
// if the reset creates a new box, subsequent progress events can no longer
// reach the model.
func TestResultsReset_ProgressSinkSurvivesReset(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	// Capture the sinkBox pointer before the run.
	originalSinkBox := m.sinkBox
	if originalSinkBox == nil {
		t.Fatal("NewModel did not install a sinkBox")
	}

	// Run to results.
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() after completion = %q, want ScreenResults", m.Screen())
	}

	// Act: press back.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenModeSelect {
		t.Fatalf("Screen() = %q, want ScreenModeSelect after back", m.Screen())
	}

	// Assert: the sinkBox pointer is unchanged. A new pointer would sever the
	// connection between the running program's installed sink and the model.
	if m.sinkBox != originalSinkBox {
		t.Errorf("sinkBox pointer changed from %p to %p after reset; "+
			"the program-installed progress sink can no longer reach the model",
			originalSinkBox, m.sinkBox)
	}

	// Verify the sink is still functional: emitting an event through it must
	// not panic even in a headless test (the discard sink absorbs the event).
	ev := domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, SuiteID: "after-reset-suite"}
	m.sinkBox.get().Emit(ev)
}

// ---------------------------------------------------------------------------
// Help bar and key handling agree on the back entry
// ---------------------------------------------------------------------------

// TestResultsReset_HelpBarAdvertisesBackKey verifies that the results screen's
// help bar includes a back (esc) entry. EntryScreenHelp already supplies it;
// this test pins the requirement so that removing it from resultsHelp is caught.
func TestResultsReset_HelpBarAdvertisesBackKey(t *testing.T) {
	entries := resultsHelp()

	var found bool
	for _, e := range entries {
		if strings.EqualFold(e.Key, "esc") || strings.EqualFold(e.Desc, "back") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("resultsHelp() does not include a back/esc entry; "+
			"every key the results screen acts on must be advertised in its help bar")
	}
}

// TestResultsReset_HelpBarAndKeyHandlerAgreeOnBack verifies that the results
// screen's help bar and its key handler are consistent: the help bar
// advertises esc/back, and pressing esc must change the screen. If the help
// bar advertises a key that the handler ignores, the user is misled.
func TestResultsReset_HelpBarAndKeyHandlerAgreeOnBack(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-a", verdict: domain.VerdictPass},
			},
		}.events()...,
	)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Fatalf("Screen() = %q, want ScreenResults before testing back agreement", m.Screen())
	}

	// Check what the help bar says.
	entries := resultsHelp()
	var helpAdvertisesBack bool
	for _, e := range entries {
		if strings.EqualFold(e.Key, "esc") || strings.EqualFold(e.Desc, "back") {
			helpAdvertisesBack = true
			break
		}
	}

	// Check what the key handler does.
	after, _ := safeUpdate(t, m, keyType(tea.KeyEsc))
	handlerActedOnBack := after.Screen() != ScreenResults

	// The two must agree: if the help bar advertises back then pressing esc
	// must actually change the screen, and vice versa.
	if helpAdvertisesBack && !handlerActedOnBack {
		t.Errorf("results screen help bar advertises the back key (esc) but the handler "+
			"does not act on it: Screen() remains %q after pressing esc; "+
			"the help bar and key handler are out of agreement", after.Screen())
	}
	if !helpAdvertisesBack && handlerActedOnBack {
		t.Errorf("results screen key handler acts on esc but the help bar does not "+
			"advertise it; the help bar and key handler are out of agreement")
	}
}
