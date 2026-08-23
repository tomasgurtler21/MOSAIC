package e2e

// T14.9 — end-to-end correctness of a full suite at a bound above 1.
//
// A suite with two tests each declaring two repetitions is run at bound=1
// (strictly sequential) and then at bound=4 (fully concurrent for this suite
// size). Both runs must produce identical aggregate verdicts, identical per-test
// aggregate verdicts, and consistent test-report ordering.
//
// "Identical verdicts at any bound" is the correctness contract the concurrent
// scheduler must satisfy. This is not a performance test: the scripted adapter's
// direct-substitution path is synchronous, so no real concurrency is exercised
// in the pipeline — only the scheduler's slot management and result assembly are
// under test. The scripted stand-in uses one collaborator identity ("worker")
// for both tests so they share one subject-turn script.
//
// This test is a correctness guard. With the current sequential implementation
// it passes at both bounds. After the concurrent scheduler is introduced, it
// continues to pass — confirming the implementation does not introduce result
// non-determinism, dropped runs, or reordered test reports.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// boundedConcurrentScenario returns a Scenario for the bounded-concurrent
// suite at the given MaxConcurrentRuns. Both run-alpha and run-beta dispatch
// the same collaborator identity ("worker"), so they share a single
// subject-turn script. Their individual stub registries route the substituted
// response to test-specific reply text.
func boundedConcurrentScenario(bound int) Scenario {
	return Scenario{
		Dir:       "testdata/e2e/bounded-concurrent",
		SuitePath: "bounded-concurrent.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "worker"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		MaxConcurrentRuns: bound,
	}
}

// TestBoundedConcurrentSuite_ProducesSameVerdictsAtBound1AndBound4 runs the
// bounded-concurrent scenario at MaxConcurrentRuns=1 (sequential) and
// MaxConcurrentRuns=4 (concurrent for this four-run suite) and asserts that
// both exit with ExitSuccess and produce identical overall verdict counts.
func TestBoundedConcurrentSuite_ProducesSameVerdictsAtBound1AndBound4(t *testing.T) {
	out1 := RunScenario(t, boundedConcurrentScenario(1))
	out4 := RunScenario(t, boundedConcurrentScenario(4))

	if out1.ExitCode != cli.ExitSuccess {
		t.Errorf("bound=1: exit code = %d, want %d (ExitSuccess)\nstdout: %s",
			out1.ExitCode, cli.ExitSuccess, out1.Stdout)
	}
	if out4.ExitCode != cli.ExitSuccess {
		t.Errorf("bound=4: exit code = %d, want %d (ExitSuccess)\nstdout: %s",
			out4.ExitCode, cli.ExitSuccess, out4.Stdout)
	}

	for _, v := range []string{string(domain.VerdictPass), string(domain.VerdictFail)} {
		c1 := out1.Result.Counts[v]
		c4 := out4.Result.Counts[v]
		if c1 != c4 {
			t.Errorf("Counts[%s]: bound=1=%d, bound=4=%d; the concurrent scheduler must produce the same overall verdicts as the sequential one",
				v, c1, c4)
		}
	}
}

// TestBoundedConcurrentSuite_PerTestVerdictsIdenticalAtBound1AndBound4
// verifies that each test's aggregate verdict and run count are the same at
// bound=1 and bound=4. A concurrent scheduler that mis-routes a run to the
// wrong test or drops a repetition would produce a count mismatch.
func TestBoundedConcurrentSuite_PerTestVerdictsIdenticalAtBound1AndBound4(t *testing.T) {
	out1 := RunScenario(t, boundedConcurrentScenario(1))
	out4 := RunScenario(t, boundedConcurrentScenario(4))

	byID := func(out Outcome) map[string]wireTestReportDoc {
		m := make(map[string]wireTestReportDoc)
		for _, tr := range out.Result.Tests {
			m[tr.TestID] = tr
		}
		return m
	}

	map1 := byID(out1)
	map4 := byID(out4)

	for _, testID := range []string{"run-alpha", "run-beta"} {
		tr1, ok1 := map1[testID]
		tr4, ok4 := map4[testID]
		if !ok1 || !ok4 {
			t.Errorf("test %q: present at bound=1=%v, bound=4=%v; both runs must include all tests",
				testID, ok1, ok4)
			continue
		}
		if tr1.Aggregate.Verdict != tr4.Aggregate.Verdict {
			t.Errorf("test %q: aggregate verdict bound=1=%q, bound=4=%q; must be identical",
				testID, tr1.Aggregate.Verdict, tr4.Aggregate.Verdict)
		}
		if tr1.Aggregate.Counted != tr4.Aggregate.Counted {
			t.Errorf("test %q: aggregate counted bound=1=%d, bound=4=%d; must be identical",
				testID, tr1.Aggregate.Counted, tr4.Aggregate.Counted)
		}
		if len(tr1.Runs) != len(tr4.Runs) {
			t.Errorf("test %q: run count bound=1=%d, bound=4=%d; all repetitions must appear in the result at any bound",
				testID, len(tr1.Runs), len(tr4.Runs))
		}
	}
}

// TestBoundedConcurrentSuite_TestOrderPreservedAtBound4 verifies that the
// test report order in the result follows the plan order (run-alpha before
// run-beta) even when MaxConcurrentRuns=4 allows both tests to execute
// simultaneously. A concurrent scheduler that appends results on completion
// rather than reserving slots by position would produce a non-deterministic
// ordering.
func TestBoundedConcurrentSuite_TestOrderPreservedAtBound4(t *testing.T) {
	out := RunScenario(t, boundedConcurrentScenario(4))

	if out.ExitCode != cli.ExitSuccess {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitSuccess)\nstdout: %s",
			out.ExitCode, cli.ExitSuccess, out.Stdout)
	}
	if len(out.Result.Tests) != 2 {
		t.Fatalf("got %d test reports, want 2", len(out.Result.Tests))
	}

	// Plan order: run-alpha listed first in the suite file, run-beta second.
	if out.Result.Tests[0].TestID != "run-alpha" {
		t.Errorf("Tests[0].TestID = %q, want %q; test reports must follow plan order regardless of completion order",
			out.Result.Tests[0].TestID, "run-alpha")
	}
	if out.Result.Tests[1].TestID != "run-beta" {
		t.Errorf("Tests[1].TestID = %q, want %q; test reports must follow plan order regardless of completion order",
			out.Result.Tests[1].TestID, "run-beta")
	}

	// Each test declares 2 repetitions; both run reports must appear.
	for _, tr := range out.Result.Tests {
		if len(tr.Runs) != 2 {
			t.Errorf("test %q: got %d run reports, want 2 (one per repetition)", tr.TestID, len(tr.Runs))
		}
		for i, run := range tr.Runs {
			if run.Run.RunID == "" {
				t.Errorf("test %q run[%d]: RunID is empty; every run must have a non-empty run identity", tr.TestID, i)
			}
		}
	}
}
