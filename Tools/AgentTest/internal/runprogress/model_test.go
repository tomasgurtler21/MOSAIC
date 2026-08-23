package runprogress_test

// model_test.go verifies the pure progress-model core.
//
// Tests are organised into four behavioural groups:
//
//  1. Concurrent-run representation: several runs in flight simultaneously
//     are each represented in Running(); a finished run leaves the set
//     without clearing the others.
//
//  2. Per-run invocation isolation: one run's ProgressInvocation event
//     increments only that run's observed count; no other run's count
//     changes, and no count is reset by another run's start.
//
//  3. Running/finished/remaining tally: the Tally reflects the correct
//     counts at each step of a sequence, including after an out-of-order
//     completion where a finish arrives before its matching start.
//
//  4. Display/report non-contradiction invariant: after the suite-finished
//     event arrives, Tally.Finished equals the sum of all verdict counts the
//     event carried, and Counts() carries those verdict figures unchanged.
//
// All tests in this file are RED-phase: the stub Model.Fold returns the
// receiver unchanged, so Running() returns nil, Tally() returns {0,0,0}, and
// Counts() returns nil. Every assertion below will fail until the real
// implementation is provided.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runprogress"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeRunKey(runID, testID string, runNumber int) domain.RunKey {
	return domain.RunKey{RunID: runID, TestID: testID, RunNumber: runNumber}
}

func startedEv(run domain.RunKey, repetitions int) domain.ProgressEvent {
	return domain.ProgressEvent{
		Kind:        domain.ProgressTestStarted,
		TestID:      run.TestID,
		Repetition:  run.RunNumber,
		Repetitions: repetitions,
		Run:         run,
	}
}

func finishedEv(run domain.RunKey, repetitions int, verdict domain.Verdict) domain.ProgressEvent {
	return domain.ProgressEvent{
		Kind:        domain.ProgressTestFinished,
		TestID:      run.TestID,
		Repetition:  run.RunNumber,
		Repetitions: repetitions,
		Verdict:     verdict,
		Run:         run,
	}
}

func invocationEv(run domain.RunKey, seq int) domain.ProgressEvent {
	return domain.ProgressEvent{
		Kind:     domain.ProgressInvocation,
		Seq:      seq,
		Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"},
		Outcome:  domain.OutcomePassthrough,
		Run:      run,
	}
}

func foldAll(m runprogress.Model, events []domain.ProgressEvent) runprogress.Model {
	for _, ev := range events {
		m = m.Fold(ev)
	}
	return m
}

// runningByID returns a map of RunID → RunProgress for easier lookup in
// tests that need to inspect a specific run's state.
func runningByID(m runprogress.Model) map[string]runprogress.RunProgress {
	result := make(map[string]runprogress.RunProgress)
	for _, rp := range m.Running() {
		result[rp.Key.RunID] = rp
	}
	return result
}

// ---------------------------------------------------------------------------
// Group 1: Concurrent-run representation
// ---------------------------------------------------------------------------

// TestModel_ConcurrentRuns_AllRepresentedInRunning verifies that starting
// several runs before any of them finish causes all of them to appear in
// Running(). A second run starting must not evict the first.
func TestModel_ConcurrentRuns_AllRepresentedInRunning(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)
	runC := makeRunKey("20260807T120000Z-0003", "test-c", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(startedEv(runB, 1))
	m = m.Fold(startedEv(runC, 1))

	running := m.Running()
	if len(running) != 3 {
		t.Fatalf("Running() has %d entries, want 3 — starting run B and C must not evict run A from the in-flight set", len(running))
	}

	byID := runningByID(m)
	for _, want := range []domain.RunKey{runA, runB, runC} {
		if _, ok := byID[want.RunID]; !ok {
			t.Errorf("Running() does not include run %q — all three must be represented simultaneously", want.RunID)
		}
	}
}

// TestModel_FinishedRun_LeavesRunningSetWithoutClearingOthers verifies that
// when one of several concurrent runs finishes, it leaves the in-flight set
// while the remaining runs stay in it.
func TestModel_FinishedRun_LeavesRunningSetWithoutClearingOthers(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)
	runC := makeRunKey("20260807T120000Z-0003", "test-c", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(startedEv(runB, 1))
	m = m.Fold(startedEv(runC, 1))

	// Run B finishes; A and C must remain in-flight.
	m = m.Fold(finishedEv(runB, 1, domain.VerdictPass))

	running := m.Running()
	if len(running) != 2 {
		t.Fatalf("after run B finishes, Running() has %d entries, want 2 (A and C still in-flight)", len(running))
	}

	byID := runningByID(m)
	if _, ok := byID[runA.RunID]; !ok {
		t.Errorf("run A must remain in Running() after run B finishes")
	}
	if _, ok := byID[runC.RunID]; !ok {
		t.Errorf("run C must remain in Running() after run B finishes")
	}
	if _, ok := byID[runB.RunID]; ok {
		t.Errorf("run B must leave Running() after it finishes, but it is still present")
	}
}

// TestModel_Running_OrderedByStartedAtThenRunID verifies that Running()
// returns runs ordered by StartedAt, with RunID as a tiebreaker, so the
// ordering is total and stable rather than map-iteration order.
func TestModel_Running_OrderedByStartedAtThenRunID(t *testing.T) {
	// All three runs start in a known order; the returned slice must reflect
	// that order (earliest first).
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)
	runC := makeRunKey("20260807T120000Z-0003", "test-c", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(startedEv(runB, 1))
	m = m.Fold(startedEv(runC, 1))

	running := m.Running()
	if len(running) != 3 {
		t.Fatalf("Running() has %d entries, want 3", len(running))
	}

	// StartedAt for runA precedes runB, which precedes runC, so the slice
	// must be [A, B, C].
	wantOrder := []string{runA.RunID, runB.RunID, runC.RunID}
	for i, rp := range running {
		if rp.Key.RunID != wantOrder[i] {
			t.Errorf("Running()[%d].Key.RunID = %q, want %q — the slice must be ordered by StartedAt then RunID", i, rp.Key.RunID, wantOrder[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Group 2: Per-run invocation isolation
// ---------------------------------------------------------------------------

// TestModel_InvocationEvent_DoesNotCollideBetweenRuns verifies that a
// ProgressInvocation event keyed to run A increments only run A's
// ObservedInvocations, leaving run B's count unchanged.
func TestModel_InvocationEvent_DoesNotCollideBetweenRuns(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(startedEv(runB, 1))

	// Run A receives 3 invocations; run B receives 2, interleaved.
	m = m.Fold(invocationEv(runA, 1))
	m = m.Fold(invocationEv(runB, 1))
	m = m.Fold(invocationEv(runA, 2))
	m = m.Fold(invocationEv(runA, 3))
	m = m.Fold(invocationEv(runB, 2))

	byID := runningByID(m)
	rpA, okA := byID[runA.RunID]
	rpB, okB := byID[runB.RunID]

	if !okA {
		t.Fatalf("run A is not in Running() — cannot inspect its invocation count")
	}
	if !okB {
		t.Fatalf("run B is not in Running() — cannot inspect its invocation count")
	}

	if rpA.ObservedInvocations != 3 {
		t.Errorf("run A ObservedInvocations = %d, want 3 — run B's events must not increment run A's count", rpA.ObservedInvocations)
	}
	if rpB.ObservedInvocations != 2 {
		t.Errorf("run B ObservedInvocations = %d, want 2 — run A's events must not increment run B's count", rpB.ObservedInvocations)
	}
}

// TestModel_NewRunStart_DoesNotResetOtherRunsInvocationCount verifies that
// starting a new run does not reset any other in-flight run's observed
// invocation count.
func TestModel_NewRunStart_DoesNotResetOtherRunsInvocationCount(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(invocationEv(runA, 1))
	m = m.Fold(invocationEv(runA, 2))

	// Starting run B must not reset run A's accumulated count.
	m = m.Fold(startedEv(runB, 1))

	byID := runningByID(m)
	rpA, ok := byID[runA.RunID]
	if !ok {
		t.Fatalf("run A is not in Running() after run B starts")
	}
	if rpA.ObservedInvocations != 2 {
		t.Errorf("run A ObservedInvocations = %d after run B starts, want 2 — starting a new run must not reset another run's count", rpA.ObservedInvocations)
	}
}

// TestModel_InvocationEvent_KeyedByRunID_NotByTestID verifies that two runs
// of the same test (different repetitions, hence different run identities)
// accumulate invocation counts independently.
func TestModel_InvocationEvent_KeyedByRunID_NotByTestID(t *testing.T) {
	// Same TestID, different RunID — as in two concurrent repetitions.
	runA := makeRunKey("20260807T120000Z-0001", "test-x", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-x", 2)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 2})
	m = m.Fold(startedEv(runA, 2))
	m = m.Fold(startedEv(runB, 2))
	m = m.Fold(invocationEv(runA, 1))
	m = m.Fold(invocationEv(runA, 2))
	m = m.Fold(invocationEv(runB, 1))

	byID := runningByID(m)
	rpA, okA := byID[runA.RunID]
	rpB, okB := byID[runB.RunID]

	if !okA || !okB {
		t.Fatalf("both runs of test-x must be in Running(); found A=%v B=%v", okA, okB)
	}

	if rpA.ObservedInvocations != 2 {
		t.Errorf("run A (repetition 1 of test-x) ObservedInvocations = %d, want 2", rpA.ObservedInvocations)
	}
	if rpB.ObservedInvocations != 1 {
		t.Errorf("run B (repetition 2 of test-x) ObservedInvocations = %d, want 1", rpB.ObservedInvocations)
	}
}

// ---------------------------------------------------------------------------
// Group 3: Running/finished/remaining tally
// ---------------------------------------------------------------------------

// TestModel_Tally_InitiallyAllRemainingAfterSuiteStarted verifies that
// before any run starts, Tally shows all declared runs as remaining.
func TestModel_Tally_InitiallyAllRemainingAfterSuiteStarted(t *testing.T) {
	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 4, TotalRuns: 4})

	tally := m.Tally()
	if tally.Running != 0 || tally.Finished != 0 || tally.Remaining != 4 {
		t.Errorf("after suite-started: Tally = %+v, want {Running:0 Finished:0 Remaining:4}", tally)
	}
}

// TestModel_Tally_ReflectsRunningFinishedRemainingAtEachStep verifies the
// tally at several points through a sequence of starts and finishes.
func TestModel_Tally_ReflectsRunningFinishedRemainingAtEachStep(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)
	runC := makeRunKey("20260807T120000Z-0003", "test-c", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3})
	m = m.Fold(startedEv(runA, 1))
	m = m.Fold(startedEv(runB, 1))

	tally := m.Tally()
	if tally.Running != 2 || tally.Finished != 0 || tally.Remaining != 1 {
		t.Errorf("after 2 of 3 runs start: Tally = %+v, want {Running:2 Finished:0 Remaining:1}", tally)
	}

	m = m.Fold(finishedEv(runA, 1, domain.VerdictPass))

	tally = m.Tally()
	if tally.Running != 1 || tally.Finished != 1 || tally.Remaining != 1 {
		t.Errorf("after run A finishes: Tally = %+v, want {Running:1 Finished:1 Remaining:1}", tally)
	}

	m = m.Fold(startedEv(runC, 1))
	m = m.Fold(finishedEv(runB, 1, domain.VerdictFail))
	m = m.Fold(finishedEv(runC, 1, domain.VerdictPass))

	tally = m.Tally()
	if tally.Running != 0 || tally.Finished != 3 || tally.Remaining != 0 {
		t.Errorf("after all runs finish: Tally = %+v, want {Running:0 Finished:3 Remaining:0}", tally)
	}
}

// TestModel_Tally_RemainingDecrementsAsRunsStartAndFinish verifies that
// Remaining starts at TotalRuns, decrements when a run starts, and reaches
// zero when all runs have finished. It also verifies Remaining is floored
// at zero and never negative.
func TestModel_Tally_RemainingDecrementsAsRunsStartAndFinish(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})

	// Before any run starts, all 2 should be remaining.
	tally := m.Tally()
	if tally.Remaining != 2 {
		t.Errorf("after suite-started with TotalRuns=2: Tally.Remaining = %d, want 2", tally.Remaining)
	}

	// After one run starts, remaining drops to 1 (that run is now in-flight).
	m = m.Fold(startedEv(runA, 1))
	tally = m.Tally()
	if tally.Remaining != 1 {
		t.Errorf("after 1 of 2 runs starts: Tally.Remaining = %d, want 1", tally.Remaining)
	}

	// After the second run starts, remaining drops to 0.
	m = m.Fold(startedEv(runB, 1))
	tally = m.Tally()
	if tally.Remaining != 0 {
		t.Errorf("after 2 of 2 runs start: Tally.Remaining = %d, want 0", tally.Remaining)
	}

	// After both runs finish, remaining stays at 0 (never goes negative).
	m = m.Fold(finishedEv(runA, 1, domain.VerdictPass))
	m = m.Fold(finishedEv(runB, 1, domain.VerdictPass))
	tally = m.Tally()
	if tally.Remaining < 0 {
		t.Errorf("Tally.Remaining = %d after all runs finish, must not be negative — it is floored at zero", tally.Remaining)
	}
	if tally.Remaining != 0 {
		t.Errorf("Tally.Remaining = %d after all runs finish, want 0", tally.Remaining)
	}
}

// TestModel_Tally_OutOfOrderCompletion_FinishedIncrements verifies that a
// ProgressTestFinished event for a run that was never started still increments
// Tally.Finished. Lifecycle events are never dropped, so they must always be
// counted, regardless of ordering.
func TestModel_Tally_OutOfOrderCompletion_FinishedIncrements(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})
	m = m.Fold(startedEv(runA, 1))

	// Run B's finish arrives before its start — out-of-order completion.
	m = m.Fold(finishedEv(runB, 1, domain.VerdictPass))

	tally := m.Tally()
	if tally.Finished != 1 {
		t.Errorf("after out-of-order finish for run B: Tally.Finished = %d, want 1 — lifecycle events must always be counted", tally.Finished)
	}
	if tally.Running != 1 {
		t.Errorf("after out-of-order finish for run B: Tally.Running = %d, want 1 — run A must remain in-flight", tally.Running)
	}
}

// TestModel_Tally_OutOfOrderCompletion_RunningSetUnchanged verifies that an
// out-of-order finish (finish arrives before start) does not corrupt the
// in-flight set: the run that was already started remains in it.
func TestModel_Tally_OutOfOrderCompletion_RunningSetUnchanged(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 2, TotalRuns: 2})
	m = m.Fold(startedEv(runA, 1))

	// Run B's finish arrives before its start.
	m = m.Fold(finishedEv(runB, 1, domain.VerdictPass))

	// The in-flight set must be unchanged: only run A should appear.
	running := m.Running()
	if len(running) != 1 {
		t.Errorf("after out-of-order finish for run B: Running() has %d entries, want 1 (only run A)", len(running))
		return
	}
	if running[0].Key.RunID != runA.RunID {
		t.Errorf("after out-of-order finish for run B: Running()[0].RunID = %q, want %q (run A)", running[0].Key.RunID, runA.RunID)
	}
}

// ---------------------------------------------------------------------------
// Group 4: Display/report non-contradiction invariant
// ---------------------------------------------------------------------------

// TestModel_Counts_CarriedFromSuiteFinishedEventUnchanged verifies that
// Counts() returns exactly what the ProgressSuiteFinished event carried —
// the report's own authoritative figures, not anything recomputed from the
// event stream. A model that recomputed counts from lifecycle events would
// become a second source of truth, which the design forbids.
func TestModel_Counts_CarriedFromSuiteFinishedEventUnchanged(t *testing.T) {
	reportCounts := map[domain.Verdict]int{
		domain.VerdictPass:    5,
		domain.VerdictFail:    2,
		domain.VerdictTimeout: 1,
	}

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 8, TotalRuns: 8})
	m = m.Fold(domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		Counts:    reportCounts,
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	})

	modelCounts := m.Counts()
	for verdict, want := range reportCounts {
		if got := modelCounts[verdict]; got != want {
			t.Errorf("Counts()[%q] = %d, want %d — the model must carry the report's counts unchanged, not recompute them", verdict, got, want)
		}
	}
}

// TestModel_TallyFinished_MatchesReportTotal verifies that after the
// suite-finished event, Tally.Finished equals the sum of all verdict counts
// in Counts(). Both count the same runs from the same lifecycle events;
// disagreement means one is wrong.
func TestModel_TallyFinished_MatchesReportTotal(t *testing.T) {
	runA := makeRunKey("20260807T120000Z-0001", "test-a", 1)
	runB := makeRunKey("20260807T120000Z-0002", "test-b", 1)
	runC := makeRunKey("20260807T120000Z-0003", "test-c", 1)

	counts := map[domain.Verdict]int{
		domain.VerdictPass: 2,
		domain.VerdictFail: 1,
	}

	events := []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3},
		startedEv(runA, 1),
		startedEv(runB, 1),
		startedEv(runC, 1),
		finishedEv(runA, 1, domain.VerdictPass),
		finishedEv(runB, 1, domain.VerdictFail),
		finishedEv(runC, 1, domain.VerdictPass),
		{Kind: domain.ProgressSuiteFinished, Counts: counts, TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed}},
	}

	m := foldAll(runprogress.Model{}, events)

	tally := m.Tally()

	reportTotal := 0
	for _, n := range counts {
		reportTotal += n
	}

	if tally.Finished != reportTotal {
		t.Errorf("Tally.Finished = %d, report total = %d — the display tally must not contradict the report's authoritative count of finished runs", tally.Finished, reportTotal)
	}
}

// TestModel_TotalCost_CarriedFromSuiteFinishedEventUnchanged verifies that
// TotalCost() carries the suite-finished event's cost unchanged, not a sum
// recomputed from per-run cost events. The report builder computes the
// authoritative total; this model carries it through the stream.
func TestModel_TotalCost_CarriedFromSuiteFinishedEventUnchanged(t *testing.T) {
	wantCost := domain.CostReport{TotalUSD: 1.23, Attribution: domain.AttributionAttributed}

	var m runprogress.Model
	m = m.Fold(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, TotalTests: 1, TotalRuns: 1})
	m = m.Fold(domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: wantCost,
	})

	got := m.TotalCost()
	if got.TotalUSD != wantCost.TotalUSD {
		t.Errorf("TotalCost().TotalUSD = %v, want %v — the model must carry the report's total unchanged", got.TotalUSD, wantCost.TotalUSD)
	}
	if got.Attribution != wantCost.Attribution {
		t.Errorf("TotalCost().Attribution = %q, want %q", got.Attribution, wantCost.Attribution)
	}
}
