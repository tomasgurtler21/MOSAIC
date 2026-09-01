package tui

// display_tally_invariant_test.go verifies the invariant that the display
// tally is never substituted for the report's authoritative counts, and never
// contradicts them when both are available.
//
// The display tally (runprogress.Tally) is derived from lifecycle events and
// is display-only. The report's counts (ProgressSuiteFinished.Counts) are
// computed by the verdict engine and are authoritative. Neither frontend may
// recompute counts from the event stream, and the tally must not contradict
// the report's figures.
//
// Tests in this file exercise the shared runprogress.Model (which both
// frontends will import) and the existing tui.Model to verify:
//
//  1. The shared core carries the report's counts verbatim from the suite-
//     finished event — it does not recompute them.
//  2. After the suite finishes, Tally.Finished equals the sum of all report
//     counts — the two independent sources of "how many runs finished" agree.
//  3. The TUI's existing Fold carries the event's counts unchanged, not a
//     recomputed value (this test is expected to pass with the current
//     implementation and pins that behaviour through the rework).
//
// Import note: internal/runprogress is imported here only because this is a
// _test.go file, which tools/importcheck exempts from the frontend gate.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runprogress"
)

// TestDisplayTallyInvariant_SharedCore_TallyFinishedMatchesReportTotal
// verifies that after the suite-finished event, the shared core's
// Tally.Finished equals the sum of the verdict counts the event carried.
// The two independent figures count the same runs and must agree.
//
// RED-phase: Tally() stub returns {0,0,0} and Counts() stub returns nil, so
// the assertions fail until the real implementation is provided.
func TestDisplayTallyInvariant_SharedCore_TallyFinishedMatchesReportTotal(t *testing.T) {
	runA := domain.RunKey{RunID: "20260807T120000Z-0001", TestName: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260807T120000Z-0002", TestName: "test-b", RunNumber: 1}
	runC := domain.RunKey{RunID: "20260807T120000Z-0003", TestName: "test-c", RunNumber: 1}

	reportCounts := map[domain.Verdict]int{
		domain.VerdictPass: 2,
		domain.VerdictFail: 1,
	}

	events := []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, TotalTests: 3, TotalRuns: 3},
		{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA},
		{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB},
		{Kind: domain.ProgressTestStarted, TestID: "test-c", Repetition: 1, Repetitions: 1, Run: runC},
		{Kind: domain.ProgressTestFinished, TestID: "test-a", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictPass, Run: runA},
		{Kind: domain.ProgressTestFinished, TestID: "test-b", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictFail, Run: runB},
		{Kind: domain.ProgressTestFinished, TestID: "test-c", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictPass, Run: runC},
		{Kind: domain.ProgressSuiteFinished, Counts: reportCounts, TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed}},
	}

	var m runprogress.Model
	for _, ev := range events {
		m = m.Fold(ev)
	}

	tally := m.Tally()

	// The authoritative total from the report.
	reportTotal := 0
	for _, n := range reportCounts {
		reportTotal += n
	}

	if tally.Finished != reportTotal {
		t.Errorf("Tally.Finished = %d, report total = %d — the display tally must not contradict the report's authoritative count of finished runs", tally.Finished, reportTotal)
	}

	// The model must also carry the per-verdict breakdown verbatim.
	modelCounts := m.Counts()
	for verdict, want := range reportCounts {
		if got := modelCounts[verdict]; got != want {
			t.Errorf("Counts()[%q] = %d, want %d — the model must carry the report's counts unchanged, not recompute them from lifecycle events", verdict, got, want)
		}
	}
}

// TestDisplayTallyInvariant_SharedCore_CountsNotRecomputedFromStream verifies
// that the shared core's Counts() carries what the suite-finished event
// supplied, not a value recomputed from counting ProgressTestFinished events.
// A model that recomputed counts would become a second source of truth, which
// the design forbids.
//
// Concretely: fold only the suite-finished event (no individual finished
// events), and the counts from that event must still appear in Counts().
//
// RED-phase: Counts() stub returns nil.
func TestDisplayTallyInvariant_SharedCore_CountsNotRecomputedFromStream(t *testing.T) {
	reportCounts := map[domain.Verdict]int{
		domain.VerdictPass:    5,
		domain.VerdictFail:    2,
		domain.VerdictTimeout: 1,
	}

	// Skip individual test lifecycle events — only the suite events.
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
			t.Errorf("Counts()[%q] = %d, want %d after suite-finished — the model must carry the report's figures regardless of whether individual test lifecycle events were folded", verdict, got, want)
		}
	}
}

// TestDisplayTallyInvariant_TUIFold_CarriesReportCountsUnchanged drives the
// TUI's existing Fold with a scripted suite sequence and verifies that
// m.Counts() after ProgressSuiteFinished exactly matches what the event
// itself carried. The TUI must not recompute counts from the event stream.
//
// This test is expected to pass with the current TUI implementation and is
// included to pin that behaviour through the multi-run rework: the
// implementation stage must not break this invariant.
func TestDisplayTallyInvariant_TUIFold_CarriesReportCountsUnchanged(t *testing.T) {
	seq := scriptedSuite{
		suiteID: "suite-under-test",
		tests: []scriptedTest{
			{testID: "test-a", verdict: domain.VerdictPass},
			{testID: "test-b", verdict: domain.VerdictPass},
			{testID: "test-c", verdict: domain.VerdictFail},
		},
	}.events()

	m := foldAll(t, newFoldModel(), seq)

	// The final event carries the authoritative counts.
	final := seq[len(seq)-1]
	if final.Kind != domain.ProgressSuiteFinished {
		t.Fatalf("test setup error: last event in sequence is %q, want ProgressSuiteFinished", final.Kind)
	}

	// The TUI must surface exactly what the event carried, not a recomputed value.
	tuiCounts := m.Counts()
	for verdict, want := range final.Counts {
		if got := tuiCounts[verdict]; got != want {
			t.Errorf("TUI Counts()[%q] = %d, want %d (from the suite-finished event) — the TUI must carry the event's counts unchanged", verdict, got, want)
		}
	}
}

// TestDisplayTallyInvariant_TUIFold_ConcurrentRunsStillCarryReportCounts
// drives the TUI's Fold from the concurrent-run sequence and verifies that
// m.Counts() still reflects what the suite-finished event carried, not a
// value recomputed by counting the interleaved ProgressTestFinished events.
//
// This guards against a regression where the multi-run rework accidentally
// introduces per-verdict counting inside the fold.
//
// This test is expected to pass with the current TUI implementation.
func TestDisplayTallyInvariant_TUIFold_ConcurrentRunsStillCarryReportCounts(t *testing.T) {
	seq := concurrentRunSeq()
	m := foldAll(t, newFoldModel(), seq)

	// Locate the suite-finished event in the sequence.
	var suiteFinished domain.ProgressEvent
	for _, ev := range seq {
		if ev.Kind == domain.ProgressSuiteFinished {
			suiteFinished = ev
		}
	}
	if suiteFinished.Kind != domain.ProgressSuiteFinished {
		t.Fatal("test setup error: concurrentRunSeq does not include a suite-finished event")
	}

	tuiCounts := m.Counts()
	for verdict, want := range suiteFinished.Counts {
		if got := tuiCounts[verdict]; got != want {
			t.Errorf("after concurrent-run sequence: TUI Counts()[%q] = %d, want %d — must match the suite-finished event, not be recomputed", verdict, got, want)
		}
	}
}
