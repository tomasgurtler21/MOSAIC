package suite_test

// Tests for spawn-failure retry behaviour under concurrency (TDD RED phase).
//
// Behaviour specified:
//  - A repetition whose first attempt ends with DispositionSpawnFailed is
//    retried exactly once, exactly as a state-integrity fault is.
//  - The retry runs in the same scheduling slot as the original attempt and
//    gets its own, distinct run identity (and therefore its own sandbox).
//  - When two repetitions are retrying at the same time, each occupies its
//    own slot and each retry gets a distinct identity from the other — no
//    identity collisions, no shared state.
//  - The MaxConcurrentRuns bound is still respected: the peak number of
//    simultaneously in-flight attempts never exceeds the bound, even when
//    those attempts include retries.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/suite"
)

// spawnFailedEvidence returns evidence that evaluate.Evaluate turns into
// VerdictFail with ReasonInfrastructure and DispositionSpawnFailed, matching
// what a real runner produces when the harness process exits non-zero.
func spawnFailedEvidence() domain.RunEvidence {
	return domain.RunEvidence{
		SubjectResult: domain.SubjectResult{
			Disposition: domain.DispositionSpawnFailed,
			ExitCode:    1,
			Stderr:      "harness: exec: exit status 1",
		},
	}
}

// deterministicRunID produces a run identity that encodes all three
// distinguishing fields so tests can verify no two attempts share an identity.
func deterministicRunID(testID string, runNumber, attempt int) string {
	return fmt.Sprintf("run-%s-rep%d-att%d", testID, runNumber, attempt)
}

// ---------------------------------------------------------------------------
// T16.7 — spawn-failed is retried exactly once with a distinct identity
// ---------------------------------------------------------------------------

// TestSuiteRun_SpawnFailed_IsRetried_ExactlyOnce verifies that a repetition
// whose first attempt ends with DispositionSpawnFailed is retried exactly
// once: the runner is called twice for that repetition and the second call
// carries a distinct run identity from the first.
func TestSuiteRun_SpawnFailed_IsRetried_ExactlyOnce(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		// Attempt 0: spawn-failed — triggers the retry.
		scriptedOutcome{evidence: spawnFailedEvidence()},
		// Attempt 1 (retry): passes cleanly.
		scriptedOutcome{evidence: passingEvidence()},
	)

	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 1,
		RunID:             deterministicRunID,
	})

	result, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 1, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times for test-a, want 2 (original attempt + one retry)", len(calls))
	}

	// The retry must have a distinct run identity so it gets its own sandbox.
	if calls[0].RunID == calls[1].RunID {
		t.Errorf("original attempt and retry share RunID %q — the retry must receive a distinct identity to avoid writing into the same sandbox", calls[0].RunID)
	}

	// The aggregate must reflect one excluded (spawn-failed) and one counted
	// (successful retry).
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if agg.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the spawn-failed attempt must be excluded from the denominator", agg.Excluded)
	}
	if agg.Counted != 1 {
		t.Errorf("Counted = %d, want 1 — the successful retry is the one that counts", agg.Counted)
	}
	if agg.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — the retry passed and determines the final verdict", agg.Verdict)
	}
}

// TestSuiteRun_SpawnFailed_NotRetriedMoreThanOnce verifies that the retry
// count is fixed at one: if the retry also fails with spawn-failed, no
// further attempt is made. The aggregate has two excluded runs and reports an
// infrastructure failure.
func TestSuiteRun_SpawnFailed_NotRetriedMoreThanOnce(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		// Attempt 0: spawn-failed.
		scriptedOutcome{evidence: spawnFailedEvidence()},
		// Attempt 1 (retry): spawn-failed again.
		scriptedOutcome{evidence: spawnFailedEvidence()},
		// Attempt 2: would be a third attempt — must never be called.
		scriptedOutcome{evidence: passingEvidence()},
	)

	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 1,
		RunID:             deterministicRunID,
	})

	_, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 1, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times for test-a, want exactly 2 — a spawn-failure is retried at most once, not until it passes", len(calls))
	}
}

// ---------------------------------------------------------------------------
// T16.7 — two repetitions retrying at once, distinct identities
// ---------------------------------------------------------------------------

// TestSuiteRun_TwoRepetitionsRetrying_EachGetsDistinctIdentity verifies that
// when two repetitions are retrying concurrently at bound=2, each of the four
// attempts (two originals + two retries) receives a distinct run identity.
// Distinct identities are the proxy for distinct sandboxes: each run identity
// maps to its own workspace directory.
func TestSuiteRun_TwoRepetitionsRetrying_EachGetsDistinctIdentity(t *testing.T) {
	runner := newScriptedRunner()
	// Two repetitions of the same test, each spawn-failing once then passing.
	runner.scriptFor("test-a",
		scriptedOutcome{evidence: spawnFailedEvidence()}, // rep 1, attempt 0
		scriptedOutcome{evidence: passingEvidence()},     // rep 1, attempt 1 (retry)
		scriptedOutcome{evidence: spawnFailedEvidence()}, // rep 2, attempt 0
		scriptedOutcome{evidence: passingEvidence()},     // rep 2, attempt 1 (retry)
	)

	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 2,
		RunID:             deterministicRunID,
	})

	result, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 2, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	calls := runner.callsFor("test-a")
	if len(calls) != 4 {
		t.Fatalf("runner was called %d times for test-a, want 4 (2 repetitions × 2 attempts each)", len(calls))
	}

	// All four run IDs must be distinct — no collisions.
	seen := make(map[string]bool)
	for _, c := range calls {
		if seen[c.RunID] {
			t.Errorf("duplicate RunID %q — each attempt including retries must receive a distinct identity to avoid writing into the same sandbox", c.RunID)
		}
		seen[c.RunID] = true
	}

	// Both repetitions must each produce one excluded and one counted.
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if agg.Excluded != 2 {
		t.Errorf("Excluded = %d, want 2 — each of the two repetitions excludes its spawn-failed first attempt", agg.Excluded)
	}
	if agg.Counted != 2 {
		t.Errorf("Counted = %d, want 2 — each repetition's successful retry is counted", agg.Counted)
	}
}

// ---------------------------------------------------------------------------
// T16.7 — the MaxConcurrentRuns bound is respected even when retries are live
// ---------------------------------------------------------------------------

// peakCountingRunner wraps a scriptedRunner and tracks the peak number of
// concurrent in-flight Run calls. Each Run call holds its slot for the
// entire duration of the underlying call.
type peakCountingRunner struct {
	inner   *scriptedRunner
	mu      sync.Mutex
	current int64
	peak    int64
}

func (r *peakCountingRunner) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	cur := atomic.AddInt64(&r.current, 1)
	defer atomic.AddInt64(&r.current, -1)

	r.mu.Lock()
	if cur > r.peak {
		r.peak = cur
	}
	r.mu.Unlock()

	return r.inner.Run(ctx, key, t, eval)
}

func (r *peakCountingRunner) peakInFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(r.peak)
}

var _ suite.TestRunner = (*peakCountingRunner)(nil)

// TestSuiteRun_ConcurrentRetries_BoundRespected verifies that when three
// repetitions are each retrying their spawn-failed first attempt, the peak
// number of simultaneously in-flight Run calls never exceeds the configured
// bound of 2. A retry runs inside its repetition's own slot and therefore
// counts as in-flight for that slot, not as an additional concurrent unit.
func TestSuiteRun_ConcurrentRetries_BoundRespected(t *testing.T) {
	const bound = 2

	base := newScriptedRunner()
	// Three repetitions, each spawn-failing once then passing.
	base.scriptFor("test-a",
		scriptedOutcome{evidence: spawnFailedEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
		scriptedOutcome{evidence: spawnFailedEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
		scriptedOutcome{evidence: spawnFailedEvidence()},
		scriptedOutcome{evidence: passingEvidence()},
	)
	counting := &peakCountingRunner{inner: base}

	s := suite.New(suite.Options{
		Runner:            counting,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: bound,
		RunID:             deterministicRunID,
	})

	_, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 3, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	if peak := counting.peakInFlight(); peak > bound {
		t.Errorf("peak in-flight Run calls = %d, want <= %d — the MaxConcurrentRuns bound must be respected even when retries are in flight", peak, bound)
	}
}
