package tui

// cross_suite_results_test.go covers the cross-suite results-screen behaviour
// introduced by Stage 5 of the multi-suite run feature.
//
// After a queue of several suites completes, the results screen must:
//   - List the tests of every suite that ran (not just the last one).
//   - Group those rows by suite, identifying each group's suite.
//   - Derive each row from the test's aggregate verdict, passed/counted counts
//     and pass rates, matching the command-line text report's wording.
//   - Keep the cursor and the detail screen's test resolution addressing the
//     same test under the grouped presentation.
//   - Remain well formed when only one suite ran or when no results exist.
//
// These are RED-phase tests: they compile against the current implementation
// and fail at runtime because the implementation does not yet satisfy the
// above requirements. They become green once the implementation tasks complete.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// perCallRunner: returns a different report.Result for each Run invocation.
// ---------------------------------------------------------------------------

// perCallRunner satisfies SuiteRunner and returns results[0] on the first
// Run call, results[1] on the second, and so on. This lets a two-suite queue
// test produce distinct results from each suite without coupling test setup
// to a single fixed report.
type perCallRunner struct {
	mu      sync.Mutex
	results []report.Result
	idx     int
}

var _ SuiteRunner = (*perCallRunner)(nil)

func newPerCallRunner(results ...report.Result) *perCallRunner {
	return &perCallRunner{results: results}
}

func (r *perCallRunner) Run(
	_ context.Context,
	_ preflight.Plan,
	_ domain.ProgressSink,
	_ domain.RetentionPolicy,
) (report.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idx >= len(r.results) {
		return report.Result{}, nil
	}
	res := r.results[r.idx]
	r.idx++
	return res, nil
}

// ---------------------------------------------------------------------------
// driveTwoSuitesToCompletion: end-to-end helper.
// ---------------------------------------------------------------------------

// driveTwoSuitesToCompletion navigates a fresh Model from ScreenModeSelect
// through the multi-suite selection flow (Space-toggle two suites, Enter to
// confirm), the five-screen settings flow, and both suite runs, returning
// the Model once it reaches ScreenResults.
func driveTwoSuitesToCompletion(t *testing.T, suiteNames []string, runner SuiteRunner) Model {
	t.Helper()
	o := Options{
		Preflight: fixturePreflight(fixturePlan("suite-under-test")),
		Suite:     runner,
		Suites:    suiteNames,
		Harness:   "fake",
	}
	m := NewModel(o)
	m = advanceToRunFlow(t, m)

	// Toggle the first suite (cursor starts at 0), move to the second, toggle
	// it, then confirm the selection.
	m, _ = safeUpdate(t, m, keyMsg(" "))              // toggle suite 0
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))     // move to suite 1
	m, _ = safeUpdate(t, m, keyMsg(" "))              // toggle suite 1
	m, _ = safeUpdate(t, m, keyMsg("\r"))              // confirm

	if m.Screen() != ScreenRetention {
		t.Fatalf("driveTwoSuitesToCompletion: expected ScreenRetention after confirming 2 suites, got %q", m.Screen())
	}

	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("driveTwoSuitesToCompletion: no Cmd returned to start the first suite")
	}

	// First suite completes.
	msg1 := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg1)

	if m.Screen() == ScreenResults {
		// The queue stopped after the first suite (error or single-suite run).
		return m
	}
	if cmd2 == nil {
		t.Fatalf("driveTwoSuitesToCompletion: no Cmd returned to start the second suite")
	}

	// Second suite completes.
	msg2 := runCmd(t, cmd2)
	m, _ = safeUpdate(t, m, msg2)

	return m
}

// ---------------------------------------------------------------------------
// buildTestReportWithAggregate: fixture builder.
// ---------------------------------------------------------------------------

// buildTestReportWithAggregate builds a report.TestReport with the given name
// and aggregate, plus agg.Counted run entries whose last entry has
// lastRunVerdict. This lets a single test cover the case where the last
// repetition's verdict differs from the aggregate verdict.
func buildTestReportWithAggregate(name string, agg domain.AggregateResult, lastRunVerdict domain.Verdict) report.TestReport {
	runs := make([]report.RunReport, agg.Counted)
	for i := range runs {
		v := domain.VerdictFail
		if i%2 != 0 {
			v = domain.VerdictPass
		}
		runs[i] = report.RunReport{
			Key:     domain.RunKey{TestName: name, RunNumber: i + 1},
			Verdict: v,
		}
	}
	if len(runs) > 0 {
		runs[len(runs)-1].Verdict = lastRunVerdict
	}
	return report.TestReport{
		TestName:  name,
		Aggregate: agg,
		Runs:      runs,
	}
}

// testReportNames extracts the TestName of each TestReport, for readable
// failure messages.
func testReportNames(tests []report.TestReport) []string {
	names := make([]string, len(tests))
	for i, tr := range tests {
		names[i] = tr.TestName
	}
	return names
}

// ---------------------------------------------------------------------------
// T5.1 — All tests appear on the results screen after a multi-suite run.
// ---------------------------------------------------------------------------

// TestCrossSuite_AllTestsAppearAfterTwoSuites verifies that after a two-suite
// queue completes, resultTests returns tests from every suite that ran, not
// just the last suite's tests.
func TestCrossSuite_AllTestsAppearAfterTwoSuites(t *testing.T) {
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0, RequiredPassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "alpha-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0, RequiredPassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0, RequiredPassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites completed, got %q", m.Screen())
	}

	tests := m.resultTests()
	var foundAlpha1, foundAlpha2, foundBeta1 bool
	for _, tr := range tests {
		switch tr.TestName {
		case "alpha-test-1":
			foundAlpha1 = true
		case "alpha-test-2":
			foundAlpha2 = true
		case "beta-test-1":
			foundBeta1 = true
		}
	}
	if !foundAlpha1 || !foundAlpha2 {
		t.Errorf("resultTests() missing tests from suite-alpha; got %d test(s): %v",
			len(tests), testReportNames(tests))
	}
	if !foundBeta1 {
		t.Errorf("resultTests() missing tests from suite-beta; got %d test(s): %v",
			len(tests), testReportNames(tests))
	}
}

// TestCrossSuite_EarlierSuiteNotDisplacedByLater verifies that when a later
// suite completes, it does not erase the earlier suite's tests from the
// results list.
func TestCrossSuite_EarlierSuiteNotDisplacedByLater(t *testing.T) {
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "alpha-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictFail, Counted: 1},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-2", RunNumber: 1}, Verdict: domain.VerdictFail}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	tests := m.resultTests()
	// Must see tests from BOTH suites: 2 from alpha, 1 from beta = 3 total.
	const wantAtLeast = 3
	if len(tests) < wantAtLeast {
		t.Errorf("resultTests() returned %d test(s), want at least %d "+
			"(earlier suite's tests must not be displaced by a later suite); tests: %v",
			len(tests), wantAtLeast, testReportNames(tests))
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Results screen groups rows by suite and identifies each group.
// ---------------------------------------------------------------------------

// TestCrossSuite_ViewGroupedBySuite verifies that after a two-suite run the
// results view renders the suite identifier for each group, so the user can
// see which suite each test belongs to.
func TestCrossSuite_ViewGroupedBySuite(t *testing.T) {
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)
	if !strings.Contains(view, "suite-alpha") {
		t.Errorf("results view does not contain the suite-alpha group label;\n"+
			"the results screen must identify each suite's group\nview:\n%s", view)
	}
	if !strings.Contains(view, "suite-beta") {
		t.Errorf("results view does not contain the suite-beta group label;\n"+
			"the results screen must identify each suite's group\nview:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Per-test row content: aggregate verdict and pass-rate statistics.
// ---------------------------------------------------------------------------

// TestCrossSuite_RowUsesAggregateVerdict verifies that each test row on the
// results screen reports the test's aggregate verdict, not the verdict of the
// last individual repetition.
func TestCrossSuite_RowUsesAggregateVerdict(t *testing.T) {
	// Aggregate says "fail"; last run says "pass". The row must show "fail".
	agg := domain.AggregateResult{
		Verdict:          domain.VerdictFail,
		Passed:           3,
		Counted:          5,
		PassRate:         0.6,
		RequiredPassRate: 0.8,
	}
	tr := buildTestReportWithAggregate("aggregate-test", agg, domain.VerdictPass)
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{tr}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)
	// The row must contain the aggregate verdict ("fail") adjacent to the test
	// name, in the format the command-line text report uses.
	wantFragment := fmt.Sprintf("aggregate-test: %s", domain.VerdictFail)
	if !strings.Contains(view, wantFragment) {
		t.Errorf("results view does not contain %q;\n"+
			"the row must use the aggregate verdict, not the last repetition's verdict\nview:\n%s",
			wantFragment, view)
	}
}

// TestCrossSuite_RowLastRepPassAggregateFailShowsNotPassed verifies the
// specific case: a test whose final repetition passed but whose aggregate did
// not must appear on the results screen as not having passed.
func TestCrossSuite_RowLastRepPassAggregateFailShowsNotPassed(t *testing.T) {
	// 5 runs: passed=3, counted=5; pass rate=60%, required=80% → aggregate fails.
	// Last run: VerdictPass (the trap — the current implementation would show "pass").
	agg := domain.AggregateResult{
		Verdict:          domain.VerdictFail,
		Passed:           3,
		Counted:          5,
		PassRate:         0.6,
		RequiredPassRate: 0.8,
	}
	tr := buildTestReportWithAggregate("borderline-test", agg, domain.VerdictPass)
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{tr}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)
	// The view must contain the aggregate verdict ("fail") for this test, not
	// the last-repetition verdict ("pass").
	wantFail := fmt.Sprintf("borderline-test: %s", domain.VerdictFail)
	if !strings.Contains(view, wantFail) {
		t.Errorf("results view does not show %q for a test whose last repetition passed "+
			"but whose aggregate did not;\n"+
			"the row must reflect the aggregate verdict, never a single repetition's verdict\nview:\n%s",
			wantFail, view)
	}
}

// TestCrossSuite_RowShowsPassedOutOfCounted verifies that the row on the
// results screen shows the passed/counted repetition counts and both pass
// rates, matching the wording the command-line text report uses.
func TestCrossSuite_RowShowsPassedOutOfCounted(t *testing.T) {
	agg := domain.AggregateResult{
		Verdict:          domain.VerdictFail,
		Passed:           3,
		Counted:          5,
		PassRate:         0.6,
		RequiredPassRate: 0.8,
	}
	tr := buildTestReportWithAggregate("rate-test", agg, domain.VerdictFail)
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{tr}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)
	// The row must show "3/5 passed", "60%" (achieved) and "80%" (required).
	// This matches the command-line text report's wording from internal/report/text.go.
	if !strings.Contains(view, "3/5 passed") {
		t.Errorf("results view does not contain \"3/5 passed\";\n"+
			"the row must show passed-out-of-counted matching the command-line report\nview:\n%s", view)
	}
	if !strings.Contains(view, "60%") {
		t.Errorf("results view does not contain the achieved pass rate \"60%%\";\n"+
			"the row must show the achieved pass rate\nview:\n%s", view)
	}
	if !strings.Contains(view, "80%") {
		t.Errorf("results view does not contain the required pass rate \"80%%\";\n"+
			"the row must show the required pass rate\nview:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T5.4 — Cursor and drill-in across suite groups.
// ---------------------------------------------------------------------------

// TestCrossSuite_DrillIn_FirstSuiteTest verifies that pressing Enter with the
// cursor on the first test in the results list (from the first suite) opens
// that test's detail screen.
func TestCrossSuite_DrillIn_FirstSuiteTest(t *testing.T) {
	// Suite alpha has two tests; suite beta has two tests.
	// After the run:
	//   Without impl: resultTests = [beta-test-1, beta-test-2] (only last suite)
	//   With impl:    resultTests = [alpha-test-1, alpha-test-2, beta-test-1, beta-test-2]
	// Cursor at 0, Enter → detail must show alpha-test-1 (with impl).
	// Without impl, cursor 0 opens beta-test-1 → assertion fails correctly.
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "alpha-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "beta-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	// Cursor starts at 0. Press Enter to drill into the first test.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail after Enter on results, got %q", m.Screen())
	}

	view := safeView(t, m)
	if !strings.Contains(view, "alpha-test-1") {
		t.Errorf("detail screen does not show \"alpha-test-1\" when cursor is on the first test "+
			"in the multi-suite results list;\n"+
			"drill-in from the first suite's first test must open that test's detail\nview:\n%s", view)
	}
}

// TestCrossSuite_DrillIn_LaterSuiteTest verifies that pressing Enter with the
// cursor on a test from a later suite opens that suite's test's detail, not
// a test from an earlier suite.
func TestCrossSuite_DrillIn_LaterSuiteTest(t *testing.T) {
	// Suite alpha has 2 tests; suite beta has 2 tests.
	// With impl: resultTests indices are 0=alpha1, 1=alpha2, 2=beta1, 3=beta2.
	// Moving Down twice from 0 reaches index 2 (beta-test-1).
	// Without impl: resultTests = [beta1, beta2], Down×2 is clamped to 1 (beta-test-2).
	// The assertion (detail contains "beta-test-1") therefore fails without impl.
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "alpha-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "beta-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	// Move cursor down twice: with the correct flat ordering this lands on
	// beta-test-1 (index 2 out of 4). Without implementation (only beta's 2
	// tests), Down is clamped at index 1 (beta-test-2).
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))

	m, _ = safeUpdate(t, m, keyMsg("\r")) // drill in
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail after Enter, got %q", m.Screen())
	}

	view := safeView(t, m)
	if !strings.Contains(view, "beta-test-1") {
		t.Errorf("detail screen does not show \"beta-test-1\" when cursor was advanced to "+
			"the first test of the second suite;\n"+
			"drill-in must reach the correct test from any suite group\nview:\n%s", view)
	}
}

// TestCrossSuite_DrillIn_ReturnRestoresCursor verifies that pressing Esc on
// the detail screen returns to the results screen with the cursor still on the
// same test that was selected before drilling in.
func TestCrossSuite_DrillIn_ReturnRestoresCursor(t *testing.T) {
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
		{
			TestName:  "alpha-test-2",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-2", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	// Move cursor down once: with implementation this lands on alpha-test-2
	// (index 1). Record the cursor position.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	cursorBefore := m.resultsCursor

	// Drill in.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenDetail {
		t.Fatalf("expected ScreenDetail, got %q", m.Screen())
	}

	// Verify the detail is for the expected test (alpha-test-2 at index 1).
	detailView := safeView(t, m)
	if !strings.Contains(detailView, "alpha-test-2") {
		t.Errorf("detail view does not show \"alpha-test-2\" when cursor was at index 1;\n"+
			"drill-in must reach the correct test; detail view:\n%s", detailView)
	}

	// Return to results.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after Esc, got %q", m.Screen())
	}

	if m.resultsCursor != cursorBefore {
		t.Errorf("cursor after returning from detail = %d, want %d "+
			"(returning from the detail screen must restore the selection)",
			m.resultsCursor, cursorBefore)
	}
}

// ---------------------------------------------------------------------------
// T5.5 — Single-suite and no-results edge cases.
// ---------------------------------------------------------------------------

// TestCrossSuite_SingleSuite_RendersOneSuiteGroup verifies that a single-suite
// run renders the suite's identifier on the results screen, keeping the
// grouped-results layout consistent for single and multi-suite runs alike.
func TestCrossSuite_SingleSuite_RendersOneSuiteGroup(t *testing.T) {
	result := report.Build("suite-only", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "only-test",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "only-test", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite-only.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)
	// The results view must identify the suite, even when only one suite ran.
	if !strings.Contains(view, "suite-only") {
		t.Errorf("single-suite results view does not contain the suite identifier \"suite-only\";\n"+
			"the grouped-results layout must identify each suite group\nview:\n%s", view)
	}
	if !strings.Contains(view, "only-test") {
		t.Errorf("single-suite results view does not contain the test name \"only-test\";\nview:\n%s", view)
	}
}

// TestCrossSuite_NoResults_RendersWithoutPanic verifies that the results screen
// renders without panic or error when no results are available — for example
// before any suite has finished.
func TestCrossSuite_NoResults_RendersWithoutPanic(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))
	// Place the model on ScreenResults without completing any run.
	m.screen = ScreenResults

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("viewResults panicked with no results available: %v", r)
		}
	}()
	view := safeView(t, m)
	if view == "" {
		t.Errorf("results view is empty when no results are available; "+
			"the screen must render a well-formed (non-empty) view even with no results")
	}
}

// ---------------------------------------------------------------------------
// T5.6 — Per-suite report files are still written as the queue proceeds.
// ---------------------------------------------------------------------------

// TestCrossSuite_EachSuiteReportFileWritten verifies that WriteFile is called
// once for each suite that completes during a multi-suite run, so each suite
// gets its own report file rather than only the last one being recorded.
func TestCrossSuite_EachSuiteReportFileWritten(t *testing.T) {
	resultA := report.Build("suite-alpha", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "alpha-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "alpha-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")
	resultB := report.Build("suite-beta", time.Now(), time.Now(), []report.TestReport{
		{
			TestName:  "beta-test-1",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: "beta-test-1", RunNumber: 1}, Verdict: domain.VerdictPass}},
		},
	}, "")

	capture := &tuiCaptureWriteFile{}
	runner := newPerCallRunner(resultA, resultB)

	o := Options{
		Preflight:  fixturePreflight(fixturePlan("suite-under-test")),
		Suite:      runner,
		Suites:     []string{"suite-alpha.yaml", "suite-beta.yaml"},
		Harness:    "fake",
		ReportPath: "report.json",
		WriteFile:  capture.write,
	}
	m := NewModel(o)
	m = advanceToRunFlow(t, m)

	m, _ = safeUpdate(t, m, keyMsg(" "))             // toggle suite 0
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))    // move to suite 1
	m, _ = safeUpdate(t, m, keyMsg(" "))             // toggle suite 1
	m, _ = safeUpdate(t, m, keyMsg("\r"))             // confirm

	if m.Screen() != ScreenRetention {
		t.Fatalf("expected ScreenRetention, got %q", m.Screen())
	}

	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("no Cmd to start first suite")
	}

	msg1 := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg1)
	if cmd2 == nil {
		t.Fatalf("no Cmd to start second suite (WriteFile may have already been called once)")
	}

	msg2 := runCmd(t, cmd2)
	m, _ = safeUpdate(t, m, msg2)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites, got %q", m.Screen())
	}

	writes := capture.callCount()
	const wantWrites = 2
	if writes != wantWrites {
		t.Errorf("WriteFile called %d time(s), want %d "+
			"(each suite in the queue must produce its own report file)",
			writes, wantWrites)
	}
}
