package tui

// folding_test.go verifies Fold: a scripted event sequence folded into the
// Model produces the expected state at each step — the suite's total, which
// test is running and which repetition of how many, the count of
// invocations observed for the running test, and each finished test's
// verdict, duration and cost.
//
// The load-bearing cases are the untidy ones (AC16.2): a burst of events
// arriving faster than renders, and a suite that ends while a test's events
// are still being folded. Neither may drop a verdict.

import (
	"math"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

func newFoldModel() Model {
	return NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
}

// ---------------------------------------------------------------------------
// Step-by-step folding of a small, ordinary sequence
// ---------------------------------------------------------------------------

func TestFold_SuiteStarted_SetsTotalTests(t *testing.T) {
	m := safeFold(t, newFoldModel(), domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		SuiteID:    "suite-under-test",
		TotalTests: 3,
	})
	if got := m.TotalTests(); got != 3 {
		t.Errorf("TotalTests() = %d, want 3", got)
	}
}

func TestFold_TestStarted_SetsRunning(t *testing.T) {
	run := domain.RunKey{RunID: "20260807T120000Z-0001", TestID: "test-a", RunNumber: 2}
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 1})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "test-a",
		Repetition:  2,
		Repetitions: 5,
		Run:         run,
	})

	running := m.Running()
	if len(running) != 1 {
		t.Fatalf("Running() has %d entries, want 1 after ProgressTestStarted", len(running))
	}
	rp := running[0]
	if rp.Key.TestID != "test-a" || rp.Key.RunNumber != 2 || rp.Repetitions != 5 {
		t.Errorf("Running()[0] = {TestID:%q RunNumber:%d Repetitions:%d}, want {TestID:%q RunNumber:2 Repetitions:5}",
			rp.Key.TestID, rp.Key.RunNumber, rp.Repetitions, "test-a")
	}
}

func TestFold_Invocation_IncrementsObservedCount(t *testing.T) {
	run := domain.RunKey{RunID: "20260807T120000Z-0001", TestID: "test-a", RunNumber: 1}
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 1})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: run})

	for i := 1; i <= 4; i++ {
		m = safeFold(t, m, domain.ProgressEvent{
			Kind:     domain.ProgressInvocation,
			Seq:      i,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
			Outcome:  domain.OutcomePassthrough,
			Run:      run,
		})
		if got := m.ObservedInvocations(run); got != i {
			t.Errorf("after invocation %d: ObservedInvocations(run) = %d, want %d", i, got, i)
		}
	}
}

func TestFold_TestFinished_AppendsToFinished(t *testing.T) {
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:             domain.ProgressTestFinished,
		TestID:           "test-a",
		Repetition:       1,
		Repetitions:      1,
		Verdict:          domain.VerdictFail,
		Duration:         2500 * time.Millisecond,
		Cost:             domain.CostReport{TotalUSD: 0.42, Attribution: domain.AttributionAttributed},
		FailedAssertions: []string{"final_state.last_status"},
	})

	finished := m.Finished()
	if len(finished) != 1 {
		t.Fatalf("Finished() has %d entries, want 1", len(finished))
	}
	got := finished[0]
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Finished()[0].Verdict = %q, want %q", got.Verdict, domain.VerdictFail)
	}
	if got.Duration != 2500*time.Millisecond {
		t.Errorf("Finished()[0].Duration = %v, want %v", got.Duration, 2500*time.Millisecond)
	}
	if got.Cost.TotalUSD != 0.42 {
		t.Errorf("Finished()[0].Cost.TotalUSD = %v, want 0.42", got.Cost.TotalUSD)
	}
}

// TestFold_SuiteFinished_SetsCountsAndCost verifies that the live counts and
// total cost the ProgressSuiteFinished event itself carries are reflected
// immediately by Fold. The richer report.Result (per-test assertions,
// reasons and conditions the detail screen needs) arrives separately via
// SuiteFinishedMsg once Suite.Run returns — see
// TestNavigation_SuiteFinishedMsg_PopulatesResult.
func TestFold_SuiteFinished_SetsCountsAndCost(t *testing.T) {
	seq := scriptedSuite{
		suiteID: "suite-under-test",
		tests: []scriptedTest{
			{testID: "test-a", verdict: domain.VerdictPass, cost: domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed}},
			{testID: "test-b", verdict: domain.VerdictFail, cost: domain.CostReport{TotalUSD: 0.20, Attribution: domain.AttributionAttributed}, failed: []string{"assertion"}},
		},
	}.events()

	m := foldAll(t, newFoldModel(), seq)

	counts := m.Counts()
	if counts[domain.VerdictPass] != 1 || counts[domain.VerdictFail] != 1 {
		t.Errorf("Counts() = %+v, want 1 pass and 1 fail", counts)
	}
	const wantCost = 0.30
	if got := m.TotalCost().TotalUSD; math.Abs(got-wantCost) > 1e-9 {
		t.Errorf("TotalCost().TotalUSD = %v, want %v", got, wantCost)
	}
}

// ---------------------------------------------------------------------------
// Multi-repetition folding: successive repetitions of the same test
// ---------------------------------------------------------------------------

// TestFold_MultiRepetition_TransitionsToNextRepetition folds a full
// start/finish pair for repetition 1 of a test, then a ProgressTestStarted
// for repetition 2 of the *same* TestID, and asserts Running() reflects
// repetition 2 rather than a stale repetition 1. AC16.2 names "which
// repetition of how many" as folded state; this is the transition from one
// repetition to the next, not a single isolated reading.
func TestFold_MultiRepetition_TransitionsToNextRepetition(t *testing.T) {
	runA1 := domain.RunKey{RunID: "20260807T120000Z-0001", TestID: "test-a", RunNumber: 1}
	runA2 := domain.RunKey{RunID: "20260807T120000Z-0002", TestID: "test-a", RunNumber: 2}
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 5})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "test-a",
		Repetition:  1,
		Repetitions: 5,
		Run:         runA1,
	})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestFinished,
		TestID:      "test-a",
		Repetition:  1,
		Repetitions: 5,
		Verdict:     domain.VerdictPass,
		Run:         runA1,
	})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      "test-a",
		Repetition:  2,
		Repetitions: 5,
		Run:         runA2,
	})

	running := m.Running()
	if len(running) != 1 {
		t.Fatalf("Running() has %d entries, want 1 after the repetition-2 ProgressTestStarted", len(running))
	}
	rp := running[0]
	if rp.Key.TestID != "test-a" || rp.Key.RunNumber != 2 || rp.Repetitions != 5 {
		t.Errorf("Running()[0] = {TestID:%q RunNumber:%d Repetitions:%d}, want {TestID:%q RunNumber:2 Repetitions:5} — repetition 2 must replace the stale repetition 1 reading",
			rp.Key.TestID, rp.Key.RunNumber, rp.Repetitions, "test-a")
	}
}

// ---------------------------------------------------------------------------
// ObservedInvocations is scoped to the currently-running test
// ---------------------------------------------------------------------------

// TestFold_ObservedInvocations_ScopedToCurrentTest folds two tests in
// sequence, each contributing a different non-zero count of
// ProgressInvocation events, and asserts that ObservedInvocations() after
// the second test starts reports only the second test's count — not the
// first test's count, and not the sum of both.
func TestFold_ObservedInvocations_ScopedToCurrentTest(t *testing.T) {
	runA := domain.RunKey{RunID: "20260807T120000Z-0001", TestID: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260807T120000Z-0002", TestID: "test-b", RunNumber: 1}
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})

	// test-a: 3 invocations, then finishes.
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA})
	for i := 1; i <= 3; i++ {
		m = safeFold(t, m, domain.ProgressEvent{
			Kind:     domain.ProgressInvocation,
			Seq:      i,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
			Outcome:  domain.OutcomePassthrough,
			Run:      runA,
		})
	}
	if got := m.ObservedInvocations(runA); got != 3 {
		t.Fatalf("after test-a's 3 invocations: ObservedInvocations(runA) = %d, want 3", got)
	}
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:       domain.ProgressTestFinished,
		TestID:     "test-a",
		Repetition: 1, Repetitions: 1,
		Verdict: domain.VerdictPass,
		Run:     runA,
	})

	// test-b: a different non-zero count of invocations (5). Starting test-b
	// must not reset test-a's count, and test-b's count must not include
	// test-a's 3 invocations.
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB})
	for i := 1; i <= 5; i++ {
		m = safeFold(t, m, domain.ProgressEvent{
			Kind:     domain.ProgressInvocation,
			Seq:      i,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
			Outcome:  domain.OutcomePassthrough,
			Run:      runB,
		})
	}

	if got := m.ObservedInvocations(runB); got != 5 {
		t.Errorf("ObservedInvocations(runB) after test-b's invocations = %d, want 5 (test-b's own count, not carried over from test-a's 3)", got)
	}
}

// ---------------------------------------------------------------------------
// Untidy case: a burst of invocation events arriving faster than renders
// ---------------------------------------------------------------------------

// TestFold_BurstOfInvocations_NoneDropped folds a large burst of
// ProgressInvocation events with nothing in between, simulating events
// arriving faster than the interface can render. Folding is synchronous and
// in-order here (there is no renderer to race), so every one of the burst
// must be reflected in ObservedInvocations — the model itself must never be
// the place an invocation event goes missing.
func TestFold_BurstOfInvocations_NoneDropped(t *testing.T) {
	const burst = 500
	run := domain.RunKey{RunID: "20260807T120000Z-0001", TestID: "test-a", RunNumber: 1}

	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 1})
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: run})

	var events []domain.ProgressEvent
	for i := 1; i <= burst; i++ {
		events = append(events, domain.ProgressEvent{
			Kind:     domain.ProgressInvocation,
			Seq:      i,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
			Outcome:  domain.OutcomePassthrough,
			Run:      run,
		})
	}
	m = foldAll(t, m, events)

	if got := m.ObservedInvocations(run); got != burst {
		t.Errorf("ObservedInvocations(run) after a %d-event burst = %d, want %d", burst, got, burst)
	}
}

// ---------------------------------------------------------------------------
// Untidy case: many tests finishing in immediate succession before the
// suite-finished event — no verdict may be dropped even under volume.
// ---------------------------------------------------------------------------

// TestFold_ManyTestsFinishingBeforeSuiteEnds_NoVerdictDropped folds a large
// number of tests' start/finish events immediately followed by
// ProgressSuiteFinished, so every finished verdict is still being folded in
// quick succession right up to the moment the suite reports itself done.
// AC16.2 requires that this ordering never loses a verdict.
func TestFold_ManyTestsFinishingBeforeSuiteEnds_NoVerdictDropped(t *testing.T) {
	const n = 200

	var tests []scriptedTest
	for i := 0; i < n; i++ {
		verdict := domain.VerdictPass
		if i%3 == 0 {
			verdict = domain.VerdictFail
		}
		tests = append(tests, scriptedTest{testID: fmtID(i), verdict: verdict})
	}
	seq := scriptedSuite{suiteID: "suite-under-test", tests: tests}.events()

	m := foldAll(t, newFoldModel(), seq)

	finished := m.Finished()
	if len(finished) != n {
		t.Fatalf("Finished() has %d entries, want %d — a verdict was dropped", len(finished), n)
	}

	total := 0
	for _, c := range m.Counts() {
		total += c
	}
	if total != n {
		t.Errorf("Counts() totals %d verdicts, want %d — a verdict was dropped", total, n)
	}
}

// ---------------------------------------------------------------------------
// Untidy case: the suite ends while the most recent test has only started
// ---------------------------------------------------------------------------

// TestFold_SuiteFinishedMidTest_UnmatchedStartDoesNotDropPriorVerdicts folds
// a ProgressSuiteFinished that arrives while the most recently started test
// has no matching ProgressTestFinished — the cancelled-mid-test race AC16.2
// and T16.2 call out. Folding must not panic, and no already-finished test's
// verdict may be lost because a later test never finished.
func TestFold_SuiteFinishedMidTest_UnmatchedStartDoesNotDropPriorVerdicts(t *testing.T) {
	m := newFoldModel()
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2})

	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:       domain.ProgressTestFinished,
		TestID:     "test-a",
		Repetition: 1, Repetitions: 1,
		Verdict: domain.VerdictPass,
	})

	// test-b starts but never finishes — the suite ends mid-test.
	m = safeFold(t, m, domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1})
	m = safeFold(t, m, domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	})

	finished := m.Finished()
	if len(finished) != 1 {
		t.Fatalf("Finished() has %d entries, want 1 — test-a's verdict must survive test-b's unmatched start", len(finished))
	}
	if finished[0].Key.TestID != "test-a" || finished[0].Verdict != domain.VerdictPass {
		t.Errorf("Finished()[0] = %+v, want test-a with verdict %q", finished[0], domain.VerdictPass)
	}
}
