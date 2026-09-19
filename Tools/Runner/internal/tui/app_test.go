package tui

// app_test.go covers the test-mode TUI flow: DevMode gating, screen
// registration, navigation, progress-screen state transitions, and results
// rendering. Tests run in package tui (internal) to access unexported
// screenID constants and rootModel fields.

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newDevModel returns a rootModel with DevMode enabled, a fake catalog loader,
// and a no-op test runner factory. The session stub is the same type used in
// navigation_test.go (stubNavSession).
func newDevModel() *rootModel {
	sess := &stubNavSession{}
	return newRootModel(context.Background(), sess, Options{
		Theme:   tuicommon.DefaultTheme(),
		DevMode: true,
		TestCatalogLoader: func(_ string) (testrun.CatalogPort, error) {
			return &fakeCatalog{}, nil
		},
		TestRunnerFactory: func(_ context.Context, _ testrun.TestConfig, _ testrun.ProgressReporter) (*testrun.TestSummary, error) {
			return &testrun.TestSummary{AllPass: true, TotalPass: 1}, nil
		},
	})
}

// newNonDevModel returns a rootModel with DevMode disabled.
func newNonDevModel() *rootModel {
	sess := &stubNavSession{}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// sendString sends a rune-based key message to the model and discards the
// returned cmd. Returns the updated model for optional inspection.
func sendString(m *rootModel, s string) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

func sendEnter(m *rootModel) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

func sendEsc(m *rootModel) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

// newTestProgressScreen builds a TestProgressScreen sized to match the model.
func newTestProgressScreenFor(m *rootModel) *screens.TestProgressScreen {
	style := stylesFromTheme(m.theme)
	return screens.NewTestProgressScreen(m.width, m.height, style)
}

// ---------------------------------------------------------------------------
// fakeCatalog implements testrun.CatalogPort with empty responses.
// ---------------------------------------------------------------------------

type fakeCatalog struct{}

func (f *fakeCatalog) Workflows() []testcatalog.CatalogEntry     { return nil }
func (f *fakeCatalog) SmokeSet() []testcatalog.CatalogEntry      { return nil }
func (f *fakeCatalog) FullSuite() []testcatalog.CatalogEntry     { return nil }
func (f *fakeCatalog) WorkflowByID(_ string) ([]testcatalog.CatalogEntry, error) {
	return nil, nil
}
func (f *fakeCatalog) WorkflowModes(_ string) ([]string, error) { return nil, nil }
func (f *fakeCatalog) WorkflowIDs() []string                    { return nil }
func (f *fakeCatalog) SidecarPath(_, _ string) string           { return "" }

// ---------------------------------------------------------------------------
// AC9.2: Test flow invisible without --dev
// ---------------------------------------------------------------------------

// TestDevMode_Disabled_HarnessScreenHasNoRunTestsOption verifies that when
// DevMode is false, the "Run Tests" entry is absent from the harness screen.
func TestDevMode_Disabled_HarnessScreenHasNoRunTestsOption(t *testing.T) {
	m := newNonDevModel()
	if m.screen != screenSetupHarness {
		t.Fatalf("expected initial screen screenSetupHarness (%d), got %d", screenSetupHarness, m.screen)
	}
	view := m.harnessScreen.View()
	if strings.Contains(view, "Run Tests") {
		t.Error("harness screen must not contain 'Run Tests' when DevMode is false")
	}
}

// TestDevMode_Disabled_DevModeFieldIsFalse verifies that rootModel.devMode is
// false when Options.DevMode is false.
func TestDevMode_Disabled_DevModeFieldIsFalse(t *testing.T) {
	m := newNonDevModel()
	if m.devMode {
		t.Error("rootModel.devMode must be false when Options.DevMode is false")
	}
}

// ---------------------------------------------------------------------------
// AC9.1: "Run Tests" option visible with --dev
// ---------------------------------------------------------------------------

// TestDevMode_Enabled_HarnessScreenShowsRunTestsOption verifies that when
// DevMode is true, the harness screen lists "Run Tests".
func TestDevMode_Enabled_HarnessScreenShowsRunTestsOption(t *testing.T) {
	m := newDevModel()
	if m.screen != screenSetupHarness {
		t.Fatalf("expected initial screen screenSetupHarness (%d), got %d", screenSetupHarness, m.screen)
	}
	view := m.harnessScreen.View()
	if !strings.Contains(view, "Run Tests") {
		t.Error("harness screen must contain 'Run Tests' when DevMode is true")
	}
}

// TestDevMode_Enabled_DevModeFieldIsTrue verifies that rootModel.devMode is
// true when Options.DevMode is true.
func TestDevMode_Enabled_DevModeFieldIsTrue(t *testing.T) {
	m := newDevModel()
	if !m.devMode {
		t.Error("rootModel.devMode must be true when Options.DevMode is true")
	}
}

// TestDevMode_Enabled_SelectRunTests_TransitionsToTestCatalog verifies that
// pressing Enter on the "Run Tests" entry (first item in dev-mode harness
// screen) transitions the model to screenTestCatalog.
func TestDevMode_Enabled_SelectRunTests_TransitionsToTestCatalog(t *testing.T) {
	m := newDevModel()
	// "Run Tests" is the first item; Enter on it selects it.
	m = sendEnter(m)
	if m.screen != screenTestCatalog {
		t.Errorf("expected screenTestCatalog (%d) after selecting Run Tests, got %d", screenTestCatalog, m.screen)
	}
	if m.testCatalogScreen == nil {
		t.Error("testCatalogScreen must be non-nil after entering test flow")
	}
}

// TestDevMode_Enabled_BackFromTestCatalog_ReturnsToHarnessScreen verifies that
// pressing Esc on the test catalog screen returns to the harness screen.
func TestDevMode_Enabled_BackFromTestCatalog_ReturnsToHarnessScreen(t *testing.T) {
	m := newDevModel()
	m = sendEnter(m) // enter test flow -> screenTestCatalog
	if m.screen != screenTestCatalog {
		t.Fatalf("expected screenTestCatalog, got %d", m.screen)
	}
	m = sendEsc(m) // Esc -> back to harness screen
	if m.screen != screenSetupHarness {
		t.Errorf("expected screenSetupHarness after Esc on catalog screen, got %d", m.screen)
	}
}

// ---------------------------------------------------------------------------
// AC9.3: Progress screen handles state transitions
// ---------------------------------------------------------------------------

// TestTestProgress_DeployStartMsg_ShowsRunning verifies that testDeployStartMsg
// causes the progress screen to display RUNNING for the deploy step.
func TestTestProgress_DeployStartMsg_ShowsRunning(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testDeployStartMsg{})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "RUNNING") {
		t.Error("progress screen must show RUNNING after testDeployStartMsg")
	}
	if !strings.Contains(view, "Deploy") {
		t.Error("progress screen must show 'Deploy' label after testDeployStartMsg")
	}
}

// TestTestProgress_DeployDoneMsg_NoError_ShowsPass verifies that a successful
// testDeployDoneMsg transitions the deploy row to PASS.
func TestTestProgress_DeployDoneMsg_NoError_ShowsPass(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testDeployStartMsg{})
	m.Update(testDeployDoneMsg{Err: nil})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "PASS") {
		t.Errorf("progress screen must show PASS after successful testDeployDoneMsg; got:\n%s", view)
	}
}

// TestTestProgress_DeployDoneMsg_WithError_ShowsFail verifies that a
// testDeployDoneMsg with an error transitions the deploy row to FAIL.
func TestTestProgress_DeployDoneMsg_WithError_ShowsFail(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testDeployStartMsg{})
	m.Update(testDeployDoneMsg{Err: context.DeadlineExceeded})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "FAIL") {
		t.Errorf("progress screen must show FAIL after testDeployDoneMsg with error; got:\n%s", view)
	}
}

// TestTestProgress_RunStartMsg_AddsRunningRow verifies that testRunStartMsg
// adds a row for the test with RUNNING state.
func TestTestProgress_RunStartMsg_AddsRunningRow(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testRunStartMsg{Harness: "claude-code", Workflow: "smoke-single", Mode: "auto"})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "smoke-single") {
		t.Error("progress screen must show workflow name after testRunStartMsg")
	}
	if !strings.Contains(view, "RUNNING") {
		t.Error("progress screen must show RUNNING state after testRunStartMsg")
	}
}

// TestTestProgress_RunDoneMsg_Pass_UpdatesRowToPass verifies that a passing
// testRunDoneMsg updates the matching row to PASS.
func TestTestProgress_RunDoneMsg_Pass_UpdatesRowToPass(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testRunStartMsg{Harness: "claude-code", Workflow: "smoke-single", Mode: "auto"})
	m.Update(testRunDoneMsg{Result: testrun.TestRunResult{
		WorkflowID: "smoke-single",
		Mode:       "auto",
		Harness:    "claude-code",
		Pass:       true,
	}})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "PASS") {
		t.Errorf("progress screen must show PASS after passing testRunDoneMsg; got:\n%s", view)
	}
}

// TestTestProgress_AllDoneMsg_SetsAllDoneAndBuildsResultsScreen verifies that
// testAllDoneMsg marks the progress screen as all-done and builds the results screen.
func TestTestProgress_AllDoneMsg_SetsAllDoneAndBuildsResultsScreen(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	summary := &testrun.TestSummary{AllPass: true, TotalPass: 2}
	m.Update(testAllDoneMsg{Summary: summary})

	if !m.testProgressScreen.AllDone() {
		t.Error("testProgressScreen must be marked all-done after testAllDoneMsg")
	}
	if m.testResultsScreen == nil {
		t.Error("testResultsScreen must be built after testAllDoneMsg")
	}
}

// ---------------------------------------------------------------------------
// AC9.4: Results summary shows per-harness counts and mismatch details
// ---------------------------------------------------------------------------

// TestTestResults_AllPass_ShowsPassCounts verifies that the results screen
// shows correct pass counts when all tests pass.
func TestTestResults_AllPass_ShowsPassCounts(t *testing.T) {
	m := newDevModel()
	style := stylesFromTheme(m.theme)
	summary := &testrun.TestSummary{
		AllPass:   true,
		TotalPass: 3,
		TotalFail: 0,
	}
	s := screens.NewTestResultsScreen(m.width, m.height, style, summary)
	view := s.View()
	if !strings.Contains(view, "3 pass") {
		t.Errorf("results screen must show '3 pass'; got:\n%s", view)
	}
}

// TestTestResults_Failure_ShowsMismatchDetail verifies that when a test fails
// due to a mismatch, the results screen shows the mismatch message.
func TestTestResults_Failure_ShowsMismatchDetail(t *testing.T) {
	m := newDevModel()
	style := stylesFromTheme(m.theme)
	summary := &testrun.TestSummary{
		AllPass:   false,
		TotalFail: 1,
		HarnessResults: []testrun.HarnessResults{
			{
				Harness:   "claude-code",
				FailCount: 1,
				Results: []testrun.TestRunResult{
					{
						WorkflowID: "smoke-single",
						Mode:       "auto",
						Harness:    "claude-code",
						Pass:       false,
						Mismatch: &testcheck.Mismatch{
							Kind:    testcheck.MismatchExitCode,
							Message: "exit code: expected 0, got 1",
						},
					},
				},
			},
		},
	}
	s := screens.NewTestResultsScreen(m.width, m.height, style, summary)
	view := s.View()
	if !strings.Contains(view, "smoke-single") {
		t.Errorf("results screen must show the failing workflow ID; got:\n%s", view)
	}
	if !strings.Contains(view, "exit code") {
		t.Errorf("results screen must show mismatch message detail; got:\n%s", view)
	}
}

// TestTestResults_DeployError_ShowsDeployFailedMessage verifies that when
// deployment failed, the results screen shows a deploy-failed message.
func TestTestResults_DeployError_ShowsDeployFailedMessage(t *testing.T) {
	m := newDevModel()
	style := stylesFromTheme(m.theme)
	summary := &testrun.TestSummary{
		AllPass:     false,
		DeployError: context.DeadlineExceeded,
	}
	s := screens.NewTestResultsScreen(m.width, m.height, style, summary)
	view := s.View()
	if !strings.Contains(view, "Deploy failed") {
		t.Errorf("results screen must show 'Deploy failed' when summary.DeployError is set; got:\n%s", view)
	}
}

// TestTestResults_DoneOnEnter verifies that pressing Enter sets Done() on the
// results screen.
func TestTestResults_DoneOnEnter(t *testing.T) {
	m := newDevModel()
	style := stylesFromTheme(m.theme)
	summary := &testrun.TestSummary{AllPass: true}
	s := screens.NewTestResultsScreen(m.width, m.height, style, summary)
	if s.Done() {
		t.Fatal("results screen must not be Done() before any key press")
	}
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Error("results screen must be Done() after Enter")
	}
}

// TestTestResults_BackIsAlwaysFalse verifies that the results screen never
// reports Back() == true (there is no back navigation from results).
func TestTestResults_BackIsAlwaysFalse(t *testing.T) {
	m := newDevModel()
	style := stylesFromTheme(m.theme)
	s := screens.NewTestResultsScreen(m.width, m.height, style, &testrun.TestSummary{})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Back() {
		t.Error("results screen Back() must always return false")
	}
}

// =============================================================================
// T4.5: TUI running-phase display -- testResolvedPathsMsg dispatch
// =============================================================================

// TestResolvedPathsMsg_Dispatch_SetsResolvedPathsOnProgressScreen verifies
// that when the rootModel receives a testResolvedPathsMsg, it calls
// SetResolvedPaths on the testProgressScreen so that the progress screen
// renders the "Resolved binaries:" section. This tests the message-dispatch
// path: factory -> testResolvedPathsMsg -> rootModel.Update -> SetResolvedPaths.
func TestResolvedPathsMsg_Dispatch_SetsResolvedPathsOnProgressScreen(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	paths := map[string]string{
		"claude-code": "/usr/local/bin/claude",
	}
	order := []string{"claude-code"}

	m.Update(testResolvedPathsMsg{Paths: paths, HarnessOrder: order})

	// The progress screen must now render the resolved paths section.
	view := m.testProgressScreen.View()
	if !strings.Contains(view, "claude-code") {
		t.Errorf("testProgressScreen.View() after testResolvedPathsMsg: does not contain \"claude-code\"\ngot:\n%s", view)
	}
	if !strings.Contains(view, "/usr/local/bin/claude") {
		t.Errorf("testProgressScreen.View() after testResolvedPathsMsg: does not contain path %q\ngot:\n%s",
			"/usr/local/bin/claude", view)
	}
}

// TestResolvedPathsMsg_Dispatch_ShowsResolvedBinariesSection verifies that the
// "Resolved binaries:" heading appears in the progress screen after the message
// is dispatched.
func TestResolvedPathsMsg_Dispatch_ShowsResolvedBinariesSection(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	m.Update(testResolvedPathsMsg{
		Paths:        map[string]string{"opencode": "/usr/local/bin/opencode"},
		HarnessOrder: []string{"opencode"},
	})

	view := m.testProgressScreen.View()
	if !strings.Contains(view, "Resolved binaries:") {
		t.Errorf("testProgressScreen.View() after testResolvedPathsMsg: does not contain \"Resolved binaries:\"\ngot:\n%s", view)
	}
}

// TestResolvedPathsMsg_Dispatch_WithNilProgressScreen_NoPanic verifies that
// receiving testResolvedPathsMsg when testProgressScreen is nil does not panic.
// This guards against a nil-pointer dereference in the edge case where the
// message arrives before the progress screen is initialised (should not happen
// in production, but must not crash if it does).
//
// NOTE (TDD RED): This test passes trivially in RED because there is no dispatch
// handler for testResolvedPathsMsg yet -- no handler means no nil-pointer
// dereference and no panic. Once the handler is added in I4.5, this test
// transitions from a trivial pass to a genuine nil-pointer guard.
func TestResolvedPathsMsg_Dispatch_WithNilProgressScreen_NoPanic(t *testing.T) {
	m := newDevModel()
	// testProgressScreen is nil (not yet initialised).
	m.testProgressScreen = nil

	// Must not panic.
	m.Update(testResolvedPathsMsg{
		Paths:        map[string]string{"claude-code": "/usr/local/bin/claude"},
		HarnessOrder: []string{"claude-code"},
	})
}

// TestResolvedPathsMsg_Dispatch_BeforeDeployStart_OrderGuarantee verifies that
// the resolved-paths section appears BEFORE any deploy/test rows in the progress
// screen. The factory calls resolution first, then notifies, then constructs
// the deployer, so testResolvedPathsMsg always arrives before testDeployStartMsg.
// This test simulates the guaranteed ordering by dispatching both messages in
// the correct order and asserting the section precedes deploy output.
func TestResolvedPathsMsg_Dispatch_BeforeDeployStart_OrderGuarantee(t *testing.T) {
	m := newDevModel()
	m.testProgressScreen = newTestProgressScreenFor(m)
	m.screen = screenTestProgress

	// 1. Resolved paths arrive first (guaranteed by factory ordering).
	m.Update(testResolvedPathsMsg{
		Paths:        map[string]string{"claude-code": "/usr/local/bin/claude"},
		HarnessOrder: []string{"claude-code"},
	})

	// 2. Deploy starts after resolution notification.
	m.Update(testDeployStartMsg{})

	view := m.testProgressScreen.View()

	resolvedIdx := strings.Index(view, "Resolved binaries:")
	deployIdx := strings.Index(view, "Deploy")

	if resolvedIdx == -1 {
		t.Error("progress screen must contain \"Resolved binaries:\" section")
	}
	if deployIdx == -1 {
		t.Error("progress screen must contain \"Deploy\" row after testDeployStartMsg")
	}
	// Resolved binaries section must appear before the deploy row.
	if resolvedIdx != -1 && deployIdx != -1 && resolvedIdx > deployIdx {
		t.Errorf("\"Resolved binaries:\" (idx %d) appears after \"Deploy\" (idx %d); resolved paths must be displayed before deploy starts",
			resolvedIdx, deployIdx)
	}
}
