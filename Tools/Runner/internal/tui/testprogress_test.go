package tui

// testprogress_test.go covers the test-flow progress screen's state
// transitions -- deploy start/done, per-run start/done rows, all-done
// summary construction -- and the resolved-binaries dispatch path
// (testResolvedPathsMsg). Tests run in package tui (internal) to access
// unexported screenID constants and rootModel fields.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

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
