package tui

// multi_suite_test.go covers Multi-Suite Selection.
//
// Sequential execution: given N selected suites, all suites execute in
// selection order, each completing fully before the next starts. An error in
// one suite stops the queue.
//
// Multiselect TUI integration: Space toggles items, Enter confirms the
// selection, and the suite-select view shows checkbox indicators.
//
// Results representation: after a multi-suite queue completes, the results
// screen must represent every suite that ran. The test added for this
// requirement (TestMultiSuite_Results_AllSuitesRepresented) is a RED-phase
// test — it fails with the current single-result implementation and becomes
// green once the cross-suite results feature is implemented.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// T5.1 — Sequential multi-suite execution
// ---------------------------------------------------------------------------

// TestMultiSuite_TwoSuites_RunSequentially verifies that when two suites are
// selected, the second suite starts only after the first has finished, and the
// model moves to ScreenResults only after both have completed.
func TestMultiSuite_TwoSuites_RunSequentially(t *testing.T) {
	runner := newFakeSuiteRunner().withEvents(
		scriptedSuite{
			suiteID: "suite-a",
			tests: []scriptedTest{
				{testID: "test-1", verdict: domain.VerdictPass},
			},
		}.events()...,
	)
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	m := NewModel(opts)

	// Navigate to suite-select.
	m = advanceToRunFlow(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("after advanceToRunFlow: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Toggle suite-a (cursor starts at 0).
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Move down to suite-b.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	// Toggle suite-b.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Confirm selection.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Fatalf("after confirming 2 suites: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
	if len(m.selectedSuites) != 2 {
		t.Fatalf("selectedSuites has %d items, want 2", len(m.selectedSuites))
	}

	// Advance through all settings screens to start the first suite.
	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("no Cmd returned after starting first suite")
	}
	if m.Screen() != ScreenProgress {
		t.Fatalf("after starting first suite: Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}

	// Deliver the first suite's result.
	msg := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg)

	// After the first suite finishes, the second should be starting.
	if m.Screen() != ScreenProgress {
		t.Errorf("after first suite finished: Screen() = %q, want %q (second suite should be starting)", m.Screen(), ScreenProgress)
	}
	if cmd2 == nil {
		t.Errorf("after first suite finished: expected non-nil Cmd to start second suite, got nil")
	}

	// Deliver the second suite's result.
	if cmd2 != nil {
		msg2 := runCmd(t, cmd2)
		m, _ = safeUpdate(t, m, msg2)
		if m.Screen() != ScreenResults {
			t.Errorf("after second suite finished: Screen() = %q, want %q", m.Screen(), ScreenResults)
		}
	}
}

// TestMultiSuite_SingleSuite_BackwardCompat verifies that pressing Enter
// without toggling any suite (single-press-Enter, no Space) still confirms
// the cursor item and runs it as a single suite, preserving the original behavior.
func TestMultiSuite_SingleSuite_BackwardCompat(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)
	if m.Screen() != ScreenResults {
		t.Errorf("single-suite backward compat: Screen() = %q, want %q", m.Screen(), ScreenResults)
	}
}

// TestMultiSuite_ErrorInFirstSuite_StopsQueue verifies that an error in the
// first suite moves the model to ScreenResults immediately, without starting
// the second suite. Subsequent suites must not run when an earlier one fails.
func TestMultiSuite_ErrorInFirstSuite_StopsQueue(t *testing.T) {
	runner := newFakeSuiteRunner()
	runner.err = fmt.Errorf("suite run failed")
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, runner)
	m := NewModel(opts)

	m = advanceToRunFlow(t, m)

	// Toggle both suites.
	m, _ = safeUpdate(t, m, keyMsg(" ")) // toggle suite-a
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg(" ")) // toggle suite-b
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirm

	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("no Cmd returned after starting first suite")
	}

	// Deliver the first suite's (failed) result.
	msg := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg)

	// After first suite errors, move directly to ScreenResults.
	if m.Screen() != ScreenResults {
		t.Errorf("after failed first suite: Screen() = %q, want %q", m.Screen(), ScreenResults)
	}
	// No Cmd to start a second suite.
	if cmd2 != nil {
		t.Errorf("after failed first suite: expected nil Cmd (no second suite), got non-nil")
	}
}

// TestMultiSuite_SelectedSuitesOrder verifies that selectedSuites holds the
// suites in declaration order (the order they appear in opts.Suites), which
// determines the execution order for multi-suite runs.
func TestMultiSuite_SelectedSuitesOrder(t *testing.T) {
	opts := newFixtureOptions([]string{"alpha.yaml", "beta.yaml", "gamma.yaml"}, newFakeSuiteRunner())
	m := NewModel(opts)
	m = advanceToRunFlow(t, m)

	// Toggle alpha (cursor 0).
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Move to gamma (skip beta).
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	// Toggle gamma.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Confirm.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Fatalf("Screen() = %q, want ScreenRetention", m.Screen())
	}
	// Declaration order: alpha first, gamma second (beta was not toggled).
	if len(m.selectedSuites) != 2 {
		t.Fatalf("selectedSuites has %d items, want 2", len(m.selectedSuites))
	}
	if m.selectedSuites[0] != "alpha.yaml" {
		t.Errorf("selectedSuites[0] = %q, want %q", m.selectedSuites[0], "alpha.yaml")
	}
	if m.selectedSuites[1] != "gamma.yaml" {
		t.Errorf("selectedSuites[1] = %q, want %q", m.selectedSuites[1], "gamma.yaml")
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Multiselect TUI integration
// ---------------------------------------------------------------------------

// TestMultiSuiteSelect_SpaceToggles_EnterConfirms verifies that Space toggles
// suites on the suite-select screen and Enter confirms the selection, populating
// selectedSuites with the toggled items in declaration order.
func TestMultiSuiteSelect_SpaceToggles_EnterConfirms(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"}, newFakeSuiteRunner())
	m := NewModel(opts)
	m = advanceToRunFlow(t, m)

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("after advanceToRunFlow: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Toggle suite-a (cursor 0).
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Move to suite-b.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	// Toggle suite-b.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Confirm.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Fatalf("after Enter with 2 suites toggled: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
	if len(m.selectedSuites) != 2 {
		t.Fatalf("selectedSuites has %d items, want 2", len(m.selectedSuites))
	}
	if m.selectedSuites[0] != "suite-a.yaml" {
		t.Errorf("selectedSuites[0] = %q, want %q", m.selectedSuites[0], "suite-a.yaml")
	}
	if m.selectedSuites[1] != "suite-b.yaml" {
		t.Errorf("selectedSuites[1] = %q, want %q", m.selectedSuites[1], "suite-b.yaml")
	}
}

// TestMultiSuiteSelect_View_ShowsCheckboxes verifies that the suite-select
// screen view contains checkbox-style indicators ("[ ]" or "[x]"), confirming
// that the multiselect widget is in use rather than the old cursor-only list.
func TestMultiSuiteSelect_View_ShowsCheckboxes(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	m := NewModel(opts)
	m = advanceToRunFlow(t, m)

	view := safeView(t, m)
	if !strings.Contains(view, "[ ]") && !strings.Contains(view, "[x]") {
		t.Errorf("suite-select view does not contain checkbox indicators ([ ] or [x]);\n"+
			"multiselect must show checkbox-style indicators\nview:\n%s", view)
	}
}

// TestMultiSuiteSelect_Help_ContainsSpaceToggle verifies that the suite-select
// help bar advertises the Space key for toggling, making the affordance
// discoverable to the user.
func TestMultiSuiteSelect_Help_ContainsSpaceToggle(t *testing.T) {
	entries := suiteSelectHelp()
	spaceKey := tuicommon.GlobalKeys.Space.Help().Key
	for _, e := range entries {
		if e.Key == spaceKey {
			return // found
		}
	}
	t.Errorf("suiteSelectHelp() does not contain a Space key entry (Key=%q);\n"+
		"multiselect must advertise the toggle key in the help bar", spaceKey)
}

// TestMultiSuiteSelect_EnterWithNoToggle_SelectsCursorItem verifies backward
// compatibility: pressing Enter without toggling any suite selects the item
// under the cursor, so single-press-Enter navigation continues to work.
func TestMultiSuiteSelect_EnterWithNoToggle_SelectsCursorItem(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	m := NewModel(opts)
	m = advanceToRunFlow(t, m)

	// Move cursor to suite-b without toggling anything.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	// Press Enter without Space.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Fatalf("Enter without toggle: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
	if len(m.selectedSuites) != 1 {
		t.Fatalf("selectedSuites has %d items, want 1 (cursor item)", len(m.selectedSuites))
	}
	if m.selectedSuites[0] != "suite-b.yaml" {
		t.Errorf("selectedSuites[0] = %q, want %q (cursor was on suite-b)", m.selectedSuites[0], "suite-b.yaml")
	}
}

// ---------------------------------------------------------------------------
// Results representation after a multi-suite run (T5.7 update)
// ---------------------------------------------------------------------------

// TestMultiSuite_Results_AllSuitesRepresented verifies that after a two-suite
// queue completes, resultTests returns tests from every suite that ran.
//
// The current implementation stores only the last suite's result in m.result,
// so resultTests returns only that suite's tests. This test is the RED-phase
// specification for the behaviour that must change: resultTests must draw from
// all retained suite results, not only the most recent one.
func TestMultiSuite_Results_AllSuitesRepresented(t *testing.T) {
	resultA := report.Build("suite-a", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "suite-a-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "suite-a-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-b", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "suite-b-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "suite-b-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites completed, got %q", m.Screen())
	}

	tests := m.resultTests()
	var foundA, foundB bool
	for _, tr := range tests {
		if tr.TestName == "suite-a-test-1" {
			foundA = true
		}
		if tr.TestName == "suite-b-test-1" {
			foundB = true
		}
	}
	if !foundA {
		t.Errorf("resultTests() does not include suite-a-test-1 from suite-a after a two-suite run;\n"+
			"the results screen must list tests from every suite that ran, not only the last one;\n"+
			"got %d test(s): %v", len(tests), testReportNames(tests))
	}
	if !foundB {
		t.Errorf("resultTests() does not include suite-b-test-1 from suite-b after a two-suite run;\n"+
			"got %d test(s): %v", len(tests), testReportNames(tests))
	}
}
