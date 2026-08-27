package tui

// concurrent_run_equivalence_test.go extends the cross-frontend equivalence
// test with concurrent-run sequences, so both frontends continue to be driven
// from one scripted event sequence.
//
// Both the TUI and the CLI import runprogress.Model and delegate their
// multi-run display fold to it. Structural equivalence holds by construction:
// the same Fold produces the same Tally, which both frontends render from the
// same types. These tests verify that the shared core, driven from a
// concurrent-run sequence, produces a coherent Tally that cli.FormatTally
// can render — and that the TUI's own Fold (which will delegate to the same
// core) also produces a coherent result when driven from the same sequence.
//
// Import note: internal/cli and internal/runprogress are imported here only
// because this is a _test.go file, which tools/importcheck exempts from the
// frontend import-boundary gate; internal/tui's production code (app.go)
// imports no sibling frontend.
//
// All tests relying on runprogress.Model are RED-phase: the stub Fold returns
// the receiver unchanged, so Tally() returns {0,0,0}. Tests relying only on
// existing TUI behaviour (Finished()) may pass with the current implementation
// and are included to pin that behaviour under the concurrent-run shape.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runprogress"
)

// concurrentRunSeq builds an event sequence where two runs are in flight
// simultaneously, so both frontends must handle overlapping start/finish
// events without one run's events corrupting the other's.
func concurrentRunSeq() []domain.ProgressEvent {
	runA := domain.RunKey{RunID: "20260807T120000Z-0001", TestName: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260807T120000Z-0002", TestName: "test-b", RunNumber: 1}

	return []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, SuiteID: "concurrent-suite", TotalTests: 2, TotalRuns: 2},
		{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA},
		{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB},
		{Kind: domain.ProgressInvocation, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"}, Outcome: domain.OutcomePassthrough, Run: runA},
		{Kind: domain.ProgressInvocation, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"}, Outcome: domain.OutcomePassthrough, Run: runB},
		{Kind: domain.ProgressTestFinished, TestID: "test-a", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictPass, Run: runA},
		{Kind: domain.ProgressTestFinished, TestID: "test-b", Repetition: 1, Repetitions: 1, Verdict: domain.VerdictFail, Run: runB},
		{
			Kind:      domain.ProgressSuiteFinished,
			Counts:    map[domain.Verdict]int{domain.VerdictPass: 1, domain.VerdictFail: 1},
			TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		},
	}
}

// TestEquivalence_ConcurrentRunSequence_SharedCoreProducesTally verifies
// that driving runprogress.Model from a concurrent-run sequence produces a
// coherent Tally reflecting both runs as finished. Both frontends import and
// delegate to this core, so if the core is correct, both frontends are
// correct by construction.
//
// RED-phase: the stub Tally() returns {0,0,0}, so Tally.Finished = 0 ≠ 2.
func TestEquivalence_ConcurrentRunSequence_SharedCoreProducesTally(t *testing.T) {
	seq := concurrentRunSeq()

	var m runprogress.Model
	for _, ev := range seq {
		m = m.Fold(ev)
	}

	tally := m.Tally()
	if tally.Finished != 2 {
		t.Errorf("after two concurrent runs finish, Tally.Finished = %d, want 2 — both runs' lifecycle events must be counted", tally.Finished)
	}
	if tally.Running != 0 {
		t.Errorf("after both runs finish, Tally.Running = %d, want 0", tally.Running)
	}
	if tally.Remaining != 0 {
		t.Errorf("after both runs finish, Tally.Remaining = %d, want 0", tally.Remaining)
	}
}

// TestEquivalence_ConcurrentRunSequence_CLIRendersTheTally verifies that
// cli.FormatTally renders the shared core's Tally as a non-empty line.
// Since both frontends call the same FormatTally, this pins the CLI
// rendering of the concurrent-run tally.
//
// RED-phase: FormatTally stub returns "", and Tally() returns {0,0,0}.
func TestEquivalence_ConcurrentRunSequence_CLIRendersTheTally(t *testing.T) {
	seq := concurrentRunSeq()

	var m runprogress.Model
	for _, ev := range seq {
		m = m.Fold(ev)
	}

	tally := m.Tally()
	line := cli.FormatTally(tally)

	if line == "" {
		t.Error("cli.FormatTally of the concurrent-run tally returned an empty string; both frontends must be able to render it")
	}
}

// TestEquivalence_ConcurrentRunSequence_CLIFormatsAllRunAttribLines verifies
// that FormatEvent produces a run-attribution prefix for every per-run event
// in the concurrent sequence. Both runs' events must be individually
// readable when they appear interleaved.
func TestEquivalence_ConcurrentRunSequence_CLIFormatsAllRunAttribLines(t *testing.T) {
	seq := concurrentRunSeq()

	var lines []string
	for _, ev := range seq {
		if line := cli.FormatEvent(ev); line != "" {
			lines = append(lines, line)
		}
	}

	const prefixA = "[20260807T120000Z-0001]"
	const prefixB = "[20260807T120000Z-0002]"

	var countA, countB int
	for _, line := range lines {
		if strings.HasPrefix(line, prefixA) {
			countA++
		}
		if strings.HasPrefix(line, prefixB) {
			countB++
		}
	}

	// Each run contributes at least: started, invocation, finished = 3 lines.
	if countA < 3 {
		t.Errorf("run A produced %d attributed lines, want at least 3 (started, invocation, finished)", countA)
	}
	if countB < 3 {
		t.Errorf("run B produced %d attributed lines, want at least 3 (started, invocation, finished)", countB)
	}
}

// TestEquivalence_ConcurrentRunSequence_TUIFoldRecordsAllFinishedRuns verifies
// that the TUI's existing Fold accumulates both finished runs correctly when
// driven from the concurrent-run sequence. This pins the behaviour the new
// multi-run model must preserve: Finished() must include both runs.
//
// This test exercises the EXISTING Fold code and is expected to pass with
// the current implementation. It is included to pin the behaviour so the
// rework in the implementation stage does not inadvertently break it.
func TestEquivalence_ConcurrentRunSequence_TUIFoldRecordsAllFinishedRuns(t *testing.T) {
	seq := concurrentRunSeq()
	m := foldAll(t, newFoldModel(), seq)

	finished := m.Finished()
	if len(finished) != 2 {
		t.Errorf("after concurrent-run sequence, Finished() has %d entries, want 2 — both runs must be recorded", len(finished))
	}

	verdicts := make(map[domain.Verdict]int)
	for _, r := range finished {
		verdicts[r.Verdict]++
	}
	if verdicts[domain.VerdictPass] != 1 || verdicts[domain.VerdictFail] != 1 {
		t.Errorf("after concurrent-run sequence, verdicts = %v, want 1 PASS and 1 FAIL", verdicts)
	}
}

// TestEquivalence_ConcurrentRunSequence_SharedCoreRunningSetConsistentWithCLI
// verifies that the shared core's Running() set is consistent with what the
// CLI renders: when two runs start concurrently, the running set holds both,
// and after both finish the set is empty.
//
// RED-phase: Running() stub returns nil (empty), so len == 0 ≠ 2.
func TestEquivalence_ConcurrentRunSequence_SharedCoreRunningSetConsistentWithCLI(t *testing.T) {
	runA := domain.RunKey{RunID: "20260807T120000Z-0001", TestName: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260807T120000Z-0002", TestName: "test-b", RunNumber: 1}

	midSeq := []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, SuiteID: "concurrent-suite", TotalTests: 2, TotalRuns: 2},
		{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA},
		{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB},
	}

	var m runprogress.Model
	for _, ev := range midSeq {
		m = m.Fold(ev)
	}

	running := m.Running()
	if len(running) != 2 {
		t.Errorf("after both runs start, Running() has %d entries, want 2 — both runs must be in-flight simultaneously", len(running))
	}

	// Verify both runs are attributed correctly, matching the CLI's attribution prefix format.
	byID := make(map[string]bool)
	for _, rp := range running {
		byID[rp.Key.RunID] = true
	}
	if !byID[runA.RunID] {
		t.Errorf("Running() does not include run A (%s)", runA.RunID)
	}
	if !byID[runB.RunID] {
		t.Errorf("Running() does not include run B (%s)", runB.RunID)
	}
}
