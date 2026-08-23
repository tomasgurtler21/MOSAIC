package e2e

// Stage 17 — speedup demonstration: the bounded concurrent scheduler
// produces a measurable wall-clock improvement for the same suite with the
// same verdicts and fully isolated per-run evidence.
//
// Three properties are verified:
//
//  1. Wall-clock improvement (T17.2). The speedup-comparison suite runs
//     measurably faster at a bound above 1 than at bound=1. Real wall clock
//     is measured externally around each RunScenario call; no duration the
//     report carries is read, because the report's clock is the injected
//     scripted clock and has no relationship to real time.
//
//  2. Correctness under concurrency (T17.3). Both runs produce identical
//     aggregate verdicts and identical report ordering. The speedup must not
//     be bought with any behaviour change.
//
//  3. Evidence isolation across concurrent runs (T17.4). Every run's run
//     identity is non-empty and unique, the test identity on each run report
//     matches the test that produced it, and every run earns the expected
//     passing verdict — none of which hold if runs are writing into each
//     other's evidence.
//
// Fixture design. The speedup-comparison suite declares one test with six
// repetitions. The test harness injects a per-run delay via SleepBeforeExit:
// the stand-in sleeps that long before processing any turn, so the run's real
// cost is dominated by the sleep rather than by scheduling overhead. At
// MaxConcurrentRuns=1 the total wall clock is approximately 6×sleep; at
// MaxConcurrentRuns=6 it is approximately 1×sleep. The improvement is
// structural rather than lucky.
//
// Margin. The wall-clock assertion is directional with a generous margin: the
// parallel run must complete in less than half the time of the sequential run.
// This margin absorbs reasonable scheduling overhead and machine load while
// still requiring a clear, measurable improvement. No specific ratio is
// required to hold across different machines or provider accounts.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

const (
	speedupDir   = "testdata/e2e/speedup-comparison"
	speedupSuite = "speedup-comparison.suite.yaml"

	// speedupSleep is the per-run delay injected via SleepBeforeExit. Each run
	// sleeps this long before processing any turn, making its cost dominate
	// scheduling overhead by construction.
	speedupSleep = "400ms"

	// speedupReps is the number of repetitions the suite fixture declares.
	// At bound=1 total wall clock is at least speedupReps×speedupSleep.
	// At bound=speedupReps all repetitions run simultaneously and total wall
	// clock is approximately speedupSleep.
	speedupReps = 6

	// speedupBound is the MaxConcurrentRuns used for the parallel run.
	// Set equal to the repetition count so all repetitions execute
	// simultaneously and the theoretical speedup is maximised.
	speedupBound = speedupReps
)

// speedupScenario returns a Scenario for the speedup-comparison suite at the
// given MaxConcurrentRuns. The subject script is shared across all
// repetitions: each repetition's run dispatches one worker collaborator,
// whose response is substituted directly by the scripted adapter, and then
// finishes.
func speedupScenario(bound int) Scenario {
	return Scenario{
		Dir:       speedupDir,
		SuitePath: speedupSuite,
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		SleepBeforeExit:   speedupSleep,
		MaxConcurrentRuns: bound,
	}
}

// TestSpeedup_ParallelFasterThanSequential_WallClock runs the
// speedup-comparison suite at MaxConcurrentRuns=1 (sequential) and then at
// MaxConcurrentRuns=speedupBound (parallel). It measures real wall clock
// externally around each RunScenario call and asserts a directional
// improvement with a generous margin.
//
// The assertion: the parallel run must complete in less than half the time of
// the sequential run. At bound=1 the sequential run must execute all six
// repetitions one after the other, each paying the per-run sleep, so its wall
// clock is at least 6×400ms = 2.4s. At bound=6 all repetitions run
// simultaneously, so the parallel wall clock is approximately 400ms. Even
// with substantial scheduling overhead the parallel run remains well under
// half the sequential time.
//
// No duration from the report is read. Report durations are derived from the
// injected scripted clock, which is not real time, so they carry no
// information about real wall clock.
func TestSpeedup_ParallelFasterThanSequential_WallClock(t *testing.T) {
	seqStart := time.Now()
	outSeq := RunScenario(t, speedupScenario(1))
	seqDuration := time.Since(seqStart)

	parStart := time.Now()
	outPar := RunScenario(t, speedupScenario(speedupBound))
	parDuration := time.Since(parStart)

	// Both runs must complete without error before the timing assertion is
	// checked, so a scheduler bug surfaces as a verdict failure rather than
	// masking behind a timing comparison.
	if outSeq.ExitCode != cli.ExitSuccess {
		t.Fatalf("sequential run: exit code = %d, want %d (ExitSuccess)\nstdout: %s\nstderr: %s",
			outSeq.ExitCode, cli.ExitSuccess, outSeq.Stdout, outSeq.Stderr)
	}
	if outPar.ExitCode != cli.ExitSuccess {
		t.Fatalf("parallel run (bound=%d): exit code = %d, want %d (ExitSuccess)\nstdout: %s\nstderr: %s",
			speedupBound, outPar.ExitCode, cli.ExitSuccess, outPar.Stdout, outPar.Stderr)
	}

	t.Logf("wall clock: sequential=%v parallel(bound=%d)=%v", seqDuration, speedupBound, parDuration)

	// Directional improvement: parallel must be measurably faster.
	//
	// The margin is generous (parallel < sequential/2), not a tight ratio.
	// With 6 repetitions each sleeping 400ms the sequential run takes at least
	// 2.4s and the parallel run takes at most 400ms + scheduling overhead.
	// Even if scheduling overhead doubles the parallel time to 800ms, the
	// assertion holds: 800ms < 2400ms/2 = 1200ms.
	if parDuration*2 >= seqDuration {
		t.Errorf("wall-clock improvement not demonstrated: sequential=%v parallel(bound=%d)=%v; "+
			"want parallel*2 < sequential — a parallel run of %d repetitions each sleeping %s "+
			"should be measurably faster than sequential execution of the same suite",
			seqDuration, speedupBound, parDuration, speedupReps, speedupSleep)
	}
}

// TestSpeedup_VerdictsAndOrderingIdenticalAtAnyBound runs the
// speedup-comparison suite at bound=1 and at bound=speedupBound and asserts
// that both produce identical aggregate verdicts and identical report ordering.
// The speedup must not be bought with any behaviour change.
func TestSpeedup_VerdictsAndOrderingIdenticalAtAnyBound(t *testing.T) {
	outSeq := RunScenario(t, speedupScenario(1))
	outPar := RunScenario(t, speedupScenario(speedupBound))

	if outSeq.ExitCode != cli.ExitSuccess {
		t.Errorf("sequential run: exit code = %d, want %d (ExitSuccess)\nstdout: %s",
			outSeq.ExitCode, cli.ExitSuccess, outSeq.Stdout)
	}
	if outPar.ExitCode != cli.ExitSuccess {
		t.Errorf("parallel run (bound=%d): exit code = %d, want %d (ExitSuccess)\nstdout: %s",
			speedupBound, outPar.ExitCode, cli.ExitSuccess, outPar.Stdout)
	}

	// Suite-level verdict counts must be identical.
	for _, v := range []string{string(domain.VerdictPass), string(domain.VerdictFail)} {
		seqCount := outSeq.Result.Counts[v]
		parCount := outPar.Result.Counts[v]
		if seqCount != parCount {
			t.Errorf("suite Counts[%s]: sequential=%d parallel(bound=%d)=%d; "+
				"the concurrent scheduler must produce the same overall verdicts as the sequential one\n"+
				"stdout seq: %s\nstdout par: %s",
				v, seqCount, speedupBound, parCount, outSeq.Stdout, outPar.Stdout)
		}
	}

	// Test report count must be identical.
	if len(outSeq.Result.Tests) != len(outPar.Result.Tests) {
		t.Fatalf("test report count: sequential=%d parallel(bound=%d)=%d; must be identical",
			len(outSeq.Result.Tests), speedupBound, len(outPar.Result.Tests))
	}

	// Report ordering and per-test verdicts must be identical.
	for i := range outSeq.Result.Tests {
		seqTest := outSeq.Result.Tests[i]
		parTest := outPar.Result.Tests[i]

		if seqTest.TestID != parTest.TestID {
			t.Errorf("Tests[%d]: TestID sequential=%q parallel=%q; "+
				"report ordering must follow plan order at any bound",
				i, seqTest.TestID, parTest.TestID)
		}
		if seqTest.Aggregate.Verdict != parTest.Aggregate.Verdict {
			t.Errorf("Tests[%d] (%s) aggregate verdict: sequential=%q parallel(bound=%d)=%q; "+
				"must be identical at any bound",
				i, seqTest.TestID, seqTest.Aggregate.Verdict, speedupBound, parTest.Aggregate.Verdict)
		}
		if seqTest.Aggregate.Counted != parTest.Aggregate.Counted {
			t.Errorf("Tests[%d] (%s) aggregate counted: sequential=%d parallel(bound=%d)=%d; "+
				"the concurrent scheduler must not drop or duplicate repetitions",
				i, seqTest.TestID, seqTest.Aggregate.Counted, speedupBound, parTest.Aggregate.Counted)
		}
		if len(seqTest.Runs) != len(parTest.Runs) {
			t.Errorf("Tests[%d] (%s) run report count: sequential=%d parallel(bound=%d)=%d; "+
				"all repetitions must appear in the result at any bound",
				i, seqTest.TestID, len(seqTest.Runs), speedupBound, len(parTest.Runs))
		}
	}
}

// TestSpeedup_EvidenceNotCorruptedOrMixedAcrossRuns executes the
// speedup-comparison suite at bound=speedupBound and verifies that no run's
// evidence is corrupted or mixed with another's. Specifically:
//
//   - Every run must have a non-empty run ID.
//   - All run IDs must be unique across the suite; a duplicate means two runs
//     wrote into each other's evidence or the identity generator produced a
//     collision.
//   - Every run's TestID must match the test that produced it; a mismatch
//     means evidence was routed to the wrong test.
//   - Every run must earn the expected passing verdict; a wrong verdict means
//     one run's assertions were contaminated by another run's interception log.
//
// The total run report count must equal the suite's declared repetition count:
// the concurrent scheduler must not silently drop any repetition's result.
func TestSpeedup_EvidenceNotCorruptedOrMixedAcrossRuns(t *testing.T) {
	out := RunScenario(t, speedupScenario(speedupBound))

	if out.ExitCode != cli.ExitSuccess {
		t.Fatalf("parallel run (bound=%d): exit code = %d, want %d (ExitSuccess)\nstdout: %s\nstderr: %s",
			speedupBound, out.ExitCode, cli.ExitSuccess, out.Stdout, out.Stderr)
	}

	seen := map[string]string{} // run ID → "testID[i]" for duplicate reporting
	totalRunReports := 0

	for _, tr := range out.Result.Tests {
		for i, run := range tr.Runs {
			totalRunReports++

			// Each run must have a non-empty run identity.
			if run.Run.RunID == "" {
				t.Errorf("test %q run[%d]: RunID is empty; every run must have a non-empty run identity",
					tr.TestID, i)
				continue
			}

			// Each run ID must be unique; a duplicate would mean two runs
			// shared a sandbox or state file.
			if prior, dup := seen[run.Run.RunID]; dup {
				t.Errorf("test %q run[%d]: RunID %q duplicates the one from %s — "+
					"concurrent runs must receive distinct identities",
					tr.TestID, i, run.Run.RunID, prior)
			} else {
				seen[run.Run.RunID] = tr.TestID
			}

			// Each run's embedded TestID must match the test that produced it;
			// a mismatch indicates evidence was routed to the wrong test entry.
			if run.Run.TestID != tr.TestID {
				t.Errorf("test %q run[%d] (RunID %q): embedded TestID = %q, want %q — "+
					"run evidence is attributed to the wrong test",
					tr.TestID, i, run.Run.RunID, run.Run.TestID, tr.TestID)
			}

			// Each run must earn a passing verdict; a wrong verdict indicates
			// that one run's invocation log was contaminated by another run's
			// interceptions.
			if run.Verdict != string(domain.VerdictPass) {
				t.Errorf("test %q run[%d] (RunID %q): verdict = %q, want %q — "+
					"a non-passing verdict in an otherwise-clean suite suggests evidence corruption",
					tr.TestID, i, run.Run.RunID, run.Verdict, domain.VerdictPass)
			}
		}
	}

	// The scheduler must deliver every repetition's result; no run may be
	// silently dropped.
	if totalRunReports != speedupReps {
		t.Errorf("total run reports across suite: got %d, want %d (%d repetitions × 1 test); "+
			"the concurrent scheduler must not drop any repetition's result",
			totalRunReports, speedupReps, speedupReps)
	}
}
