package tui

// session_rollup_test.go covers the session-level roll-up display on the
// results screen (Stage 6).
//
// After a multi-suite queue reaches the results screen the TUI must show a
// session-level roll-up summarising:
//   - The overall session outcome (passed / tests failed / infrastructure
//     failure), rendered as three distinguishable states.
//   - The combined verdict counts and total cost across every suite that ran.
//   - An aborted-queue statement and the names of any selected suites that
//     never started, when the queue stopped before completion.
//
// These are RED-phase tests: they compile against the current implementation
// and fail at runtime because the session roll-up rendering does not yet exist.
// They become green once the implementation tasks complete.
//
// Test strategy: each test drives a Model through the normal multi-suite
// execution flow to reach ScreenResults and asserts on the rendered view.
// The session is built internally when the queue reaches the results screen;
// controlling session figures means supplying deterministic suite results.
// All assertions target View() output so that they compile before the
// implementation-phase fields exist.

import (
	"context"
	"errors"
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
// sessionScriptedRunner: per-call (result, error) pairs
// ---------------------------------------------------------------------------

// sessionScriptedRunner satisfies SuiteRunner and returns a different
// (result, error) pair for each Run call. This lets tests control what each
// suite in a multi-suite queue returns without coupling to a single fixed
// result or a single fixed error.
type sessionScriptedRunner struct {
	mu      sync.Mutex
	entries []sessionScriptedEntry
	idx     int
}

type sessionScriptedEntry struct {
	result report.Result
	err    error
}

var _ SuiteRunner = (*sessionScriptedRunner)(nil)

func newSessionScriptedRunner(entries ...sessionScriptedEntry) *sessionScriptedRunner {
	return &sessionScriptedRunner{entries: entries}
}

func (r *sessionScriptedRunner) Run(
	_ context.Context,
	_ preflight.Plan,
	_ domain.ProgressSink,
	_ domain.RetentionPolicy,
) (report.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idx >= len(r.entries) {
		return report.Result{}, nil
	}
	e := r.entries[r.idx]
	r.idx++
	return e.result, e.err
}

// ---------------------------------------------------------------------------
// Fixture builders
// ---------------------------------------------------------------------------

// rollupPassingResult builds a report.Result where every test passed.
func rollupPassingResult(suiteID string, testCount int) report.Result {
	tests := make([]report.TestReport, testCount)
	for i := range tests {
		tests[i] = report.TestReport{
			TestName:  suiteID + "-test",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: suiteID + "-test", RunNumber: 1}, Verdict: domain.VerdictPass}},
		}
	}
	return report.Build(suiteID, time.Now(), time.Now(), tests, "")
}

// rollupFailingResult builds a report.Result with the given numbers of
// passing and failing tests.
func rollupFailingResult(suiteID string, passingCount, failingCount int) report.Result {
	var tests []report.TestReport
	for i := 0; i < passingCount; i++ {
		tests = append(tests, report.TestReport{
			TestName:  suiteID + "-pass-test",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictPass, Counted: 1, Passed: 1, PassRate: 1.0},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: suiteID + "-pass-test", RunNumber: 1}, Verdict: domain.VerdictPass}},
		})
	}
	for i := 0; i < failingCount; i++ {
		tests = append(tests, report.TestReport{
			TestName:  suiteID + "-fail-test",
			Aggregate: domain.AggregateResult{Verdict: domain.VerdictFail, Counted: 1},
			Runs:      []report.RunReport{{Key: domain.RunKey{TestName: suiteID + "-fail-test", RunNumber: 1}, Verdict: domain.VerdictFail}},
		})
	}
	return report.Build(suiteID, time.Now(), time.Now(), tests, "")
}

// rollupInfraFailureResult builds a report.Result that carries an
// infrastructure failure (InfrastructureFailures > 0) and no verdict counts.
// Constructed directly so InfrastructureFailures is set independently of the
// AggregateResult field layout.
func rollupInfraFailureResult(suiteID string) report.Result {
	return report.Result{
		SuiteID:                suiteID,
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  []report.TestReport{},
		Counts:                 map[domain.Verdict]int{},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}
}

// rollupInfraAndFailingResult builds a report.Result that carries both a
// FAIL verdict count AND an infrastructure failure. This is the scenario that
// must be classified as SessionInfrastructureFailure, not SessionTestsFailed.
func rollupInfraAndFailingResult(suiteID string) report.Result {
	return report.Result{
		SuiteID:    suiteID,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
		Tests:      []report.TestReport{},
		Counts: map[domain.Verdict]int{
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}
}

// driveThreeSuitesToResults navigates a fresh Model from ScreenModeSelect,
// toggles three suites, advances through the settings flow, runs the first
// two through runner, and returns the model. If the second suite errors the
// queue stops and the third suite never runs. The caller must supply at least
// 3 suite names.
func driveThreeSuitesToResults(t *testing.T, suiteNames []string, runner SuiteRunner) Model {
	t.Helper()
	o := Options{
		Preflight: fixturePreflight(fixturePlan("suite-under-test")),
		Suite:     runner,
		Suites:    suiteNames,
		Harness:   "fake",
	}
	m := NewModel(o)
	m = advanceToRunFlow(t, m)

	// Toggle the three suites; cursor starts at index 0.
	m, _ = safeUpdate(t, m, keyMsg(" "))          // toggle suite[0]
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // move to suite[1]
	m, _ = safeUpdate(t, m, keyMsg(" "))          // toggle suite[1]
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown)) // move to suite[2]
	m, _ = safeUpdate(t, m, keyMsg(" "))          // toggle suite[2]
	m, _ = safeUpdate(t, m, keyMsg("\r"))          // confirm

	if m.Screen() != ScreenRetention {
		t.Fatalf("driveThreeSuitesToResults: expected ScreenRetention after confirming 3 suites, got %q", m.Screen())
	}

	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("driveThreeSuitesToResults: no Cmd returned to start first suite")
	}

	// First suite.
	msg1 := runCmd(t, cmd)
	m, cmd2 := safeUpdate(t, m, msg1)
	if m.Screen() == ScreenResults {
		return m // first suite aborted the queue
	}
	if cmd2 == nil {
		t.Fatalf("driveThreeSuitesToResults: no Cmd returned to start second suite")
	}

	// Second suite (may error, stopping the queue before suite[2]).
	msg2 := runCmd(t, cmd2)
	m, _ = safeUpdate(t, m, msg2)
	return m
}

// driveThreeSuitesWithWriteFailure selects three suites and drives the first
// to completion; the WriteFile always returns an error, which should stop the
// queue after the first suite. The second and third suites never run.
func driveThreeSuitesWithWriteFailure(t *testing.T, suiteNames []string, firstResult report.Result) Model {
	t.Helper()
	runner := newPerCallRunner(firstResult)
	o := Options{
		Preflight:  fixturePreflight(fixturePlan("suite-under-test")),
		Suite:      runner,
		Suites:     suiteNames,
		Harness:    "fake",
		ReportPath: "report.json",
		WriteFile:  func(_ string, _ []byte) error { return errors.New("disk full") },
	}
	m := NewModel(o)
	m = advanceToRunFlow(t, m)

	m, _ = safeUpdate(t, m, keyMsg(" "))
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg(" "))
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg(" "))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Fatalf("driveThreeSuitesWithWriteFailure: expected ScreenRetention, got %q", m.Screen())
	}

	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("driveThreeSuitesWithWriteFailure: no Cmd returned to start first suite")
	}

	msg := runCmd(t, cmd)
	m, _ = safeUpdate(t, m, msg)
	return m
}

// ---------------------------------------------------------------------------
// T6.1 — Roll-up figures come from the session model
// ---------------------------------------------------------------------------

// TestSessionRollup_RollupPresentAfterMultiSuiteRun verifies that the results
// view contains a session-level section after a multi-suite queue completes.
// Without implementation the results screen shows only per-suite test rows and
// no session section; the assertion fails correctly (RED phase).
func TestSessionRollup_RollupPresentAfterMultiSuiteRun(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 2)
	resultB := rollupFailingResult("suite-b", 1, 1)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites completed, got %q", m.Screen())
	}

	view := safeView(t, m)

	// A session-level section must appear in the view. "session" as a
	// case-insensitive substring is the marker; the current results view has no
	// such section, so this assertion fails without implementation (RED phase).
	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view after a two-suite queue does not contain a session-level "+
			"roll-up section;\nthe results screen must display a run-level roll-up "+
			"after the queue completes;\nview:\n%s", view)
	}
}

// TestSessionRollup_ShowsOverallOutcome verifies that the session roll-up
// displays the overall session outcome classification. The outcome must be
// present in the view in a form that distinguishes the session state. Without
// implementation no roll-up is rendered, so the outcome is not shown → RED.
func TestSessionRollup_ShowsOverallOutcome(t *testing.T) {
	// All tests passing → outcome is SessionPassed.
	resultA := rollupPassingResult("suite-a", 2)
	resultB := rollupPassingResult("suite-b", 1)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)

	// The view must contain a session roll-up. Its presence ("session") is
	// necessary before checking the outcome classification.
	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view does not contain a session roll-up (no 'session' keyword);\n"+
			"the roll-up must show the overall session outcome;\nview:\n%s", view)
	}

	// The outcome classification must also appear, not just the section heading.
	// All suites pass → the outcome is SessionPassed; the view must contain a
	// recognisable indicator such as "passed". Without implementation the
	// outcome is never rendered → this assertion fails correctly (RED).
	if !strings.Contains(strings.ToLower(view), "passed") {
		t.Errorf("results view does not contain an outcome indicator for SessionPassed;\n"+
			"the session roll-up must convey the outcome classification, not just the "+
			"section heading;\nexpected a word such as \"passed\" to appear;\nview:\n%s", view)
	}
}

// TestSessionRollup_CombinedCountsDisplayed verifies that the session roll-up
// shows verdict counts that reflect the combined figures from all executed
// suites, not just the last one. With suite-a having 3 passing tests and
// suite-b having 2 passing tests, the combined pass count is 5.
//
// The test uses a distinctive total (5 passing, 2 failing across both suites)
// and asserts both counts appear in the view. Without implementation no roll-up
// is rendered, so neither count appears in a session context → RED.
func TestSessionRollup_CombinedCountsDisplayed(t *testing.T) {
	// Suite A: 3 passing; Suite B: 2 passing + 2 failing.
	// Combined: 5 passing, 2 failing.
	resultA := rollupPassingResult("suite-a", 3)
	resultB := rollupFailingResult("suite-b", 2, 2)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)

	// The roll-up must be present (session section exists).
	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view does not contain a session roll-up;\n"+
			"combined verdict counts require a session section to appear;\nview:\n%s", view)
	}

	// The combined pass count (5) and fail count (2) must appear in the view.
	// Suite A contributes 3 passing; Suite B contributes 2 passing + 2 failing.
	// These specific values are only correct if the roll-up aggregates both
	// suites from the session model, not just the last suite's figures.
	// Without implementation these counts are never rendered → assertions fail (RED).
	if !strings.Contains(view, "5") {
		t.Errorf("results view does not contain the combined pass count (5);\n"+
			"suite-a has 3 passing and suite-b has 2 passing, totalling 5;\n"+
			"the session roll-up must display the combined figure;\nview:\n%s", view)
	}
	if !strings.Contains(view, "2") {
		t.Errorf("results view does not contain the combined fail count (2);\n"+
			"suite-b has 2 failing tests; the session roll-up must display this figure;\n"+
			"view:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Three session outcomes presented distinguishably
// ---------------------------------------------------------------------------

// TestSessionRollup_PassedOutcome_ViewContainsSessionSection verifies that the
// results screen contains a session section for a fully passing queue. Without
// implementation no section exists → assertion fails (RED).
func TestSessionRollup_PassedOutcome_ViewContainsSessionSection(t *testing.T) {
	resultA := rollupPassingResult("suite-alpha", 2)
	resultB := rollupPassingResult("suite-beta", 1)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-alpha.yaml", "suite-beta.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)

	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view for a fully passing session does not contain a session "+
			"roll-up section;\nthe SessionPassed outcome must be presented on the results "+
			"screen;\nview:\n%s", view)
	}
}

// TestSessionRollup_TestsFailedAndInfraFailure_AreDistinguishable verifies
// that a session where tests failed (SessionTestsFailed) and a session where
// an infrastructure failure occurred (SessionInfrastructureFailure) are
// presented differently on the results screen. Both scenarios use the same
// raw FAIL count, so the views are identical without a session roll-up. With
// implementation the roll-up conveys different outcomes → views differ.
//
// This assertion is the core RED-phase check: without a roll-up, the two views
// are identical; with implementation they must differ.
func TestSessionRollup_TestsFailedAndInfraFailure_AreDistinguishable(t *testing.T) {
	// Scenario A: 1 failing test, 0 infra failures → SessionTestsFailed.
	resultTestsFailed := report.Result{
		SuiteID:                "suite-under-test",
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  []report.TestReport{},
		Counts:                 map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}
	// Scenario B: 1 failing test AND 1 infra failure → SessionInfrastructureFailure.
	resultInfraFail := report.Result{
		SuiteID:                "suite-under-test",
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  []report.TestReport{},
		Counts:                 map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}

	runnerA := newPerCallRunner(resultTestsFailed)
	mA := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerA)

	runnerB := newPerCallRunner(resultInfraFail)
	mB := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerB)

	if mA.Screen() != ScreenResults || mB.Screen() != ScreenResults {
		t.Fatalf("not all scenarios landed on ScreenResults: a=%q b=%q",
			mA.Screen(), mB.Screen())
	}

	viewA := strings.ToLower(safeView(t, mA))
	viewB := strings.ToLower(safeView(t, mB))

	// Both scenarios have the same per-suite test rows (no tests, one FAIL
	// in the counts map). Without a session roll-up the views are identical,
	// which is the expected RED state. With implementation the roll-up shows
	// "tests_failed" vs "infrastructure_failure", making them distinct.
	if viewA == viewB {
		t.Errorf("results view for SessionTestsFailed is identical to results view for " +
			"SessionInfrastructureFailure;\nthe two outcomes must be presented distinguishably;\n" +
			"without a session roll-up both views look the same — this is the expected RED state")
	}
}

// TestSessionRollup_ThreeOutcomes_NoTwoIdentical verifies that all three
// session outcomes (passed, tests_failed, infrastructure_failure) produce
// mutually distinct views. The critical pair is tests_failed vs
// infrastructure_failure: both have the same per-suite test rows, so they look
// the same without a session roll-up. With implementation the roll-up conveys
// different outcomes → views differ.
func TestSessionRollup_ThreeOutcomes_NoTwoIdentical(t *testing.T) {
	const suiteID = "suite-under-test"

	// Scenario A: a passing test → SessionPassed.
	// Uses rollupPassingResult so the view has a visible PASS test row.
	// This naturally differs from B and C (which have a FAIL row) without any
	// roll-up — a pre-existing distinction that this test does not rely on for
	// its primary claim, but which keeps the sanity check below meaningful.
	resultsPassed := rollupPassingResult(suiteID, 1)

	// Scenario B: a failing test, no infra failure → SessionTestsFailed.
	resultsFailed := rollupFailingResult(suiteID, 0, 1) // 0 passing, 1 failing

	// Scenario C: the same failing test AND an infra failure →
	// SessionInfrastructureFailure. Constructed directly so InfrastructureFailures
	// is set while keeping the same Tests slice as B.
	resultsInfra := report.Result{
		SuiteID:                suiteID,
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  resultsFailed.Tests, // identical test rows to B
		Counts:                 map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1, // the only difference from B
	}

	runnerPassed := newPerCallRunner(resultsPassed)
	mPassed := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerPassed)

	runnerFailed := newPerCallRunner(resultsFailed)
	mFailed := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerFailed)

	runnerInfra := newPerCallRunner(resultsInfra)
	mInfra := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerInfra)

	if mPassed.Screen() != ScreenResults || mFailed.Screen() != ScreenResults || mInfra.Screen() != ScreenResults {
		t.Fatalf("not all scenarios landed on ScreenResults: passed=%q failed=%q infra=%q",
			mPassed.Screen(), mFailed.Screen(), mInfra.Screen())
	}

	viewPassed := strings.ToLower(safeView(t, mPassed))
	viewFailed := strings.ToLower(safeView(t, mFailed))
	viewInfra := strings.ToLower(safeView(t, mInfra))

	// Critical pair: tests_failed vs infrastructure_failure have identical test
	// rows (both show one FAIL entry). Without a session roll-up the views are
	// the same. With implementation the roll-up conveys distinct outcomes.
	// This assertion correctly fails without implementation (RED phase).
	if viewFailed == viewInfra {
		t.Errorf("results view for SessionTestsFailed is identical to SessionInfrastructureFailure;\n"+
			"all three session outcomes must be distinguishable;\n"+
			"without a session roll-up tests_failed and infrastructure_failure look the same "+
			"(correct RED state)")
	}
	// Sanity check: scenario A (passing test rows) and scenario B (failing test
	// rows) must naturally differ even without a roll-up, confirming the runner
	// and fixture setup are correct.
	if viewPassed == viewFailed {
		t.Fatalf("results view for the passing scenario and the tests-failed scenario "+
			"are identical;\nthese two scenarios have different per-test rows (PASS vs FAIL) "+
			"and must produce different views even without a session roll-up;\n"+
			"this indicates a broken test fixture or runner setup, not a missing feature")
	}
}

// ---------------------------------------------------------------------------
// T6.3 — Infrastructure failure takes precedence over tests-failed
// ---------------------------------------------------------------------------

// TestSessionRollup_InfraFailure_PrecedesTestsFailed verifies that a session
// carrying both a failing verdict AND an infrastructure failure is presented
// as infrastructure failure (unmeasurable), not as tests-failed. The
// distinction matters: a regression and a measurement failure are different
// problems that require different responses.
func TestSessionRollup_InfraFailure_PrecedesTestsFailed(t *testing.T) {
	// Both scenarios use the same SuiteID and the same empty test list so the
	// views are identical without a session roll-up. The only difference between
	// A and B is InfrastructureFailures, which is not reflected in the per-test
	// rows. Without a roll-up the views are the same → assertion fails (RED).
	const suiteID = "suite-under-test"

	// Scenario A (baseline): 1 failing test, 0 infra → SessionTestsFailed.
	resultOnlyFail := report.Result{
		SuiteID:                suiteID,
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  []report.TestReport{},
		Counts:                 map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}
	// Scenario B (infra+fail): same SuiteID, same FAIL count, AND infra failure →
	// SessionInfrastructureFailure must take precedence.
	resultInfraAndFail := report.Result{
		SuiteID:                suiteID,
		StartedAt:              time.Now(),
		FinishedAt:             time.Now(),
		Tests:                  []report.TestReport{},
		Counts:                 map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}

	runnerOnlyFail := newPerCallRunner(resultOnlyFail)
	mOnlyFail := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerOnlyFail)

	runnerInfraAndFail := newPerCallRunner(resultInfraAndFail)
	mInfraAndFail := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runnerInfraAndFail)

	if mOnlyFail.Screen() != ScreenResults || mInfraAndFail.Screen() != ScreenResults {
		t.Fatalf("not all scenarios on ScreenResults: only_fail=%q infra_and_fail=%q",
			mOnlyFail.Screen(), mInfraAndFail.Screen())
	}

	viewOnlyFail := strings.ToLower(safeView(t, mOnlyFail))
	viewInfraAndFail := strings.ToLower(safeView(t, mInfraAndFail))

	// Both scenarios have the same per-suite test rows. Without a roll-up the
	// views are identical. With implementation the infra+fail case must show
	// a different (infrastructure-failure) outcome → views differ.
	if viewOnlyFail == viewInfraAndFail {
		t.Errorf("results view for a session with only failing tests is identical to " +
			"a session with failing tests AND an infrastructure failure;\n" +
			"the infrastructure failure outcome must take precedence and be presented " +
			"distinguishably from a pure regression;\n" +
			"without a session roll-up both views look the same (correct RED state)")
	}
}

// ---------------------------------------------------------------------------
// T6.4 — Aborted-queue presentation
// ---------------------------------------------------------------------------

// TestSessionRollup_SuiteError_AbortStatementPresent verifies that when a
// suite run error aborts the queue mid-way, the results screen says the queue
// aborted. Without implementation no such statement is rendered → RED.
func TestSessionRollup_SuiteError_AbortStatementPresent(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	runner := newSessionScriptedRunner(
		sessionScriptedEntry{result: resultA, err: nil},
		sessionScriptedEntry{result: report.Result{}, err: errors.New("suite-b run failed")},
	)

	m := driveThreeSuitesToResults(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after suite-b error, got %q", m.Screen())
	}

	view := safeView(t, m)

	// The results screen must state the queue aborted. Without implementation
	// no such statement is present → assertion fails correctly (RED).
	if !strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view after a suite error does not contain an abort statement;\n"+
			"when the queue stops early the results screen must say so;\nview:\n%s", view)
	}
}

// TestSessionRollup_SuiteError_NamesUnrunSuites verifies that when a suite
// error aborts the queue, the results screen names the suites that never ran.
// suite-c.yaml was selected but never started; its name must appear in the view.
func TestSessionRollup_SuiteError_NamesUnrunSuites(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	runner := newSessionScriptedRunner(
		sessionScriptedEntry{result: resultA, err: nil},
		sessionScriptedEntry{result: report.Result{}, err: errors.New("suite-b run failed")},
	)

	m := driveThreeSuitesToResults(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after suite-b error, got %q", m.Screen())
	}

	view := safeView(t, m)

	// suite-c.yaml was selected but never started. Its name must appear in the
	// view. Without implementation it is not listed → assertion fails (RED).
	if !strings.Contains(view, "suite-c.yaml") {
		t.Errorf("results view does not name \"suite-c.yaml\" as an unrun suite;\n"+
			"when the queue aborts the results screen must name every selected suite "+
			"that never started;\nview:\n%s", view)
	}
}

// TestSessionRollup_SuiteError_RollupCoversOnlyExecutedSuites verifies that
// after an abort the session roll-up still appears (covering only the suites
// that actually executed) and the aborted-queue statement is present alongside
// it. The two elements must coexist on the results screen.
func TestSessionRollup_SuiteError_RollupCoversOnlyExecutedSuites(t *testing.T) {
	// suite-a runs and passes; suite-b errors; suite-c never runs.
	// The session roll-up must reflect suite-a's results only.
	resultA := rollupPassingResult("suite-a", 2)
	runner := newSessionScriptedRunner(
		sessionScriptedEntry{result: resultA, err: nil},
		sessionScriptedEntry{result: report.Result{}, err: errors.New("suite-b failed")},
	)

	m := driveThreeSuitesToResults(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	view := safeView(t, m)

	// Both the roll-up (session section) and the abort statement must appear.
	// Without implementation neither is present → both assertions fail (RED).
	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view after an aborted queue does not contain a session "+
			"roll-up section;\nthe roll-up must cover the suites that executed;\n"+
			"view:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view after an aborted queue does not contain an abort "+
			"statement;\nthe roll-up and the abort statement must coexist;\nview:\n%s", view)
	}
}

// TestSessionRollup_WriteFailure_AbortStatementPresent verifies that a
// report-write failure mid-queue triggers the aborted-queue statement on the
// results screen. Without implementation no abort statement is shown → RED.
func TestSessionRollup_WriteFailure_AbortStatementPresent(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	m := driveThreeSuitesWithWriteFailure(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		resultA)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after write failure, got %q", m.Screen())
	}

	view := safeView(t, m)

	// The queue stopped because the write failed. The screen must say "aborted".
	// Without implementation no such statement is shown → assertion fails (RED).
	if !strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view after a report-write failure mid-queue does not contain "+
			"an abort statement;\na write failure that stops the queue must trigger the "+
			"same aborted-queue statement as a run failure;\nview:\n%s", view)
	}
}

// TestSessionRollup_WriteFailure_NamesUnrunSuites verifies that when a
// report-write failure aborts the queue, the results screen names the suites
// that never ran (suite-b.yaml and suite-c.yaml in this scenario).
func TestSessionRollup_WriteFailure_NamesUnrunSuites(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	m := driveThreeSuitesWithWriteFailure(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		resultA)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after write failure, got %q", m.Screen())
	}

	view := safeView(t, m)

	// Both unrun suites must be named. Without implementation neither is listed
	// → assertions fail (RED).
	if !strings.Contains(view, "suite-b.yaml") {
		t.Errorf("results view after a write-failure abort does not name \"suite-b.yaml\" "+
			"as an unrun suite;\nview:\n%s", view)
	}
	if !strings.Contains(view, "suite-c.yaml") {
		t.Errorf("results view after a write-failure abort does not name \"suite-c.yaml\" "+
			"as an unrun suite;\nview:\n%s", view)
	}
}

// TestSessionRollup_WriteFailure_ExistingDetailPaneStillShown verifies that
// when the queue aborts due to a report-write failure, the existing
// failure-detail pane (carrying the write-failure diagnostic) is still shown.
// The aborted-queue statement and the session roll-up are additive; they must
// not displace the detail pane.
func TestSessionRollup_WriteFailure_ExistingDetailPaneStillShown(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	m := driveThreeSuitesWithWriteFailure(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		resultA)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after write failure, got %q", m.Screen())
	}

	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false after a report-write failure mid-queue;\n"+
			"the existing failure-detail pane must remain live on ScreenResults even when "+
			"the aborted-queue statement is also present;\n"+
			"the abort statement is about the queue; the pane carries the write-failure "+
			"diagnostic")
	}
}

// ---------------------------------------------------------------------------
// T6.5 — Complete queue not presented as aborted
// ---------------------------------------------------------------------------

// TestSessionRollup_CompleteQueue_NoAbortStatement verifies that when every
// selected suite ran to completion, the results screen does NOT say the queue
// aborted. An abort statement on a complete run would be a false alarm.
func TestSessionRollup_CompleteQueue_NoAbortStatement(t *testing.T) {
	resultA := rollupPassingResult("suite-a", 1)
	resultB := rollupPassingResult("suite-b", 1)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites completed, got %q", m.Screen())
	}

	view := safeView(t, m)

	if strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view after a complete two-suite queue contains \"aborted\";\n"+
			"a queue that ran every selected suite must not be presented as aborted;\n"+
			"view:\n%s", view)
	}
}

// TestSessionRollup_CompleteQueue_NoUnrunSuiteNames verifies that when every
// selected suite ran, the results screen does NOT list any unrun suite names.
// Listing non-existent unrun suites would mislead the user.
func TestSessionRollup_CompleteQueue_NoUnrunSuiteNames(t *testing.T) {
	const unrunSentinel = "suite-never-ran.yaml"
	resultA := rollupPassingResult("suite-a", 1)
	resultB := rollupPassingResult("suite-b", 1)

	runner := newPerCallRunner(resultA, resultB)
	// The sentinel name is not in the suite list, so it must never appear.
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after both suites completed, got %q", m.Screen())
	}

	view := safeView(t, m)

	// Neither suite-a nor suite-b is "unrun" — the names already appear in
	// the test-list rows. We cannot assert their absence. Instead, assert that
	// "aborted" is absent (already done above) and that no "never ran" or
	// "unrun" language appears next to suite names.
	if strings.Contains(view, unrunSentinel) {
		t.Errorf("results view unexpectedly contains %q, which was never in the "+
			"suite list;\ncheck test setup", unrunSentinel)
	}
}

// TestSessionRollup_SingleSuiteRun_NotAborted verifies that a single-suite
// run (backward compatibility path) is not presented as aborted. A single-suite
// run trivially completes and the results screen must never show an abort
// statement for it.
func TestSessionRollup_SingleSuiteRun_NotAborted(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after single-suite run, got %q", m.Screen())
	}

	view := safeView(t, m)

	if strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view after a single-suite run contains \"aborted\";\n"+
			"a run that completed normally must never be presented as aborted;\n"+
			"view:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T6.6 — Roll-up and abort statement untruncated at standard width
// ---------------------------------------------------------------------------

// TestSessionRollup_StandardWidth_RollupPresent verifies that the session
// roll-up is present at the standard terminal width (80 chars). The roll-up
// text must be fully visible alongside the grouped test list without being
// truncated or dropped.
func TestSessionRollup_StandardWidth_RollupPresent(t *testing.T) {
	const standardWidth = 80
	const standardHeight = 24

	resultA := rollupPassingResult("suite-a", 2)
	resultB := rollupFailingResult("suite-b", 1, 1)

	runner := newPerCallRunner(resultA, resultB)
	m := driveTwoSuitesToCompletion(t, []string{"suite-a.yaml", "suite-b.yaml"}, runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults, got %q", m.Screen())
	}

	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: standardWidth, Height: standardHeight})
	view := safeView(t, m)

	// The roll-up must be present at 80-char width. Without implementation
	// no roll-up is rendered → assertion fails (RED).
	if !strings.Contains(strings.ToLower(view), "session") {
		t.Errorf("results view at standard terminal width (%d cols) does not contain "+
			"a session roll-up;\nthe roll-up must be present and not truncated at the "+
			"standard terminal width;\nview:\n%s", standardWidth, view)
	}
}

// TestSessionRollup_StandardWidth_AbortStatementPresent verifies that the
// aborted-queue statement is present and not truncated at the standard
// terminal width (80 chars) when the queue stopped before completion.
func TestSessionRollup_StandardWidth_AbortStatementPresent(t *testing.T) {
	const standardWidth = 80
	const standardHeight = 24

	resultA := rollupPassingResult("suite-a", 1)
	runner := newSessionScriptedRunner(
		sessionScriptedEntry{result: resultA, err: nil},
		sessionScriptedEntry{result: report.Result{}, err: errors.New("suite-b failed")},
	)

	m := driveThreeSuitesToResults(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		runner)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after suite-b error, got %q", m.Screen())
	}

	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: standardWidth, Height: standardHeight})
	view := safeView(t, m)

	if !strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view at standard width (%d cols) does not contain the "+
			"aborted-queue statement;\nthe abort statement must be untruncated at "+
			"standard terminal width;\nview:\n%s", standardWidth, view)
	}
}

// TestSessionRollup_StandardWidth_FailurePaneStillShown verifies that the
// existing failure-detail pane is still shown at the standard terminal width
// when the session roll-up and the aborted-queue statement are also present.
// All three elements must coexist on the results screen at 80 chars.
func TestSessionRollup_StandardWidth_FailurePaneStillShown(t *testing.T) {
	const standardWidth = 80
	const standardHeight = 24

	// Use a write failure so the detail pane is populated and the queue stops
	// (suite-b and suite-c never run).
	resultA := rollupPassingResult("suite-a", 1)
	m := driveThreeSuitesWithWriteFailure(t,
		[]string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"},
		resultA)

	if m.Screen() != ScreenResults {
		t.Fatalf("expected ScreenResults after write failure, got %q", m.Screen())
	}

	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: standardWidth, Height: standardHeight})

	// The failure-detail pane must still be live at standard width.
	_, _, shown := m.FailureDetail()
	if !shown {
		t.Errorf("FailureDetail() shown = false at standard width (%d cols);\n"+
			"the failure-detail pane must remain visible at standard width alongside "+
			"the session roll-up and the aborted-queue statement", standardWidth)
	}

	view := safeView(t, m)

	// The abort statement must be present at standard width.
	if !strings.Contains(strings.ToLower(view), "aborted") {
		t.Errorf("results view at standard width does not contain the abort statement;\n"+
			"roll-up, abort statement, and failure-detail pane must coexist at %d cols;\n"+
			"view:\n%s", standardWidth, view)
	}

	// The write-failure diagnostic code must still be visible in the pane.
	if !strings.Contains(view, "report-unwritable") {
		t.Errorf("results view at standard width does not contain the write-failure "+
			"diagnostic (\"report-unwritable\") from the detail pane;\n"+
			"the pane must remain visible alongside the session roll-up and abort "+
			"statement at %d cols;\nview:\n%s", standardWidth, view)
	}
}
