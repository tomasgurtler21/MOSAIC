package tui

// testresults_test.go covers the test-flow results screen: per-harness pass
// counts, mismatch detail rendering, deploy-failure messaging, and the
// Done()/Back() key contract. Tests run in package tui (internal) to access
// unexported rootModel fields used to size the screen under test.

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/testcheck"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
)

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
	s := devtest.NewTestResultsScreen(m.width, m.height, style, summary)
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
	s := devtest.NewTestResultsScreen(m.width, m.height, style, summary)
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
	s := devtest.NewTestResultsScreen(m.width, m.height, style, summary)
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
	s := devtest.NewTestResultsScreen(m.width, m.height, style, summary)
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
	s := devtest.NewTestResultsScreen(m.width, m.height, style, &testrun.TestSummary{})
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Back() {
		t.Error("results screen Back() must always return false")
	}
}
