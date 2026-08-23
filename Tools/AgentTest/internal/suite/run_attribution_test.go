package suite_test

// Tests that every per-run progress event carries the identity of the run
// that produced it, and that suite-level events carry no run identity.
//
// These tests reference domain.ProgressEvent.Run (a field added additively
// by the implementation phase). Until that field exists these tests fail to
// compile — the expected RED state.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestSuiteRun_TestStartedCarriesRunIdentity verifies that a ProgressTestStarted
// event carries the identity of the run it opens. A test-started event with
// an empty Run.RunID is unattributable when several runs are in flight.
func TestSuiteRun_TestStartedCarriesRunIdentity(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	var found bool
	for _, ev := range sink.all() {
		if ev.Kind != domain.ProgressTestStarted {
			continue
		}
		found = true
		if ev.Run.RunID == "" {
			t.Errorf("ProgressTestStarted.Run.RunID is empty; per-run events must carry a non-empty run identity so they are attributable under concurrent runs")
		}
		if ev.Run.TestID != "test-a" {
			t.Errorf("ProgressTestStarted.Run.TestID = %q, want %q; the Run field must identify which test this event belongs to", ev.Run.TestID, "test-a")
		}
		if ev.Run.RunNumber < 1 {
			t.Errorf("ProgressTestStarted.Run.RunNumber = %d, want >= 1; run numbers are 1-based", ev.Run.RunNumber)
		}
	}
	if !found {
		t.Fatal("no ProgressTestStarted event was emitted")
	}
}

// TestSuiteRun_InvocationCarriesRunIdentity verifies that a ProgressInvocation
// event carries the identity of the run whose attempt produced the invocation.
// Without Run, an invocation event from one run is indistinguishable from one
// produced by another run executing concurrently.
func TestSuiteRun_InvocationCarriesRunIdentity(t *testing.T) {
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Records = []domain.LogRecord{
		{Kind: domain.RecordStart, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}, Outcome: domain.OutcomeRewritePrompt},
	}
	runner.scriptFor("test-a", scriptedOutcome{evidence: ev})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	var found bool
	for _, ev := range sink.all() {
		if ev.Kind != domain.ProgressInvocation {
			continue
		}
		found = true
		if ev.Run.RunID == "" {
			t.Errorf("ProgressInvocation.Run.RunID is empty; per-run events must carry a non-empty run identity so they are attributable under concurrent runs")
		}
		if ev.Run.TestID != "test-a" {
			t.Errorf("ProgressInvocation.Run.TestID = %q, want %q", ev.Run.TestID, "test-a")
		}
	}
	if !found {
		t.Fatal("no ProgressInvocation event was emitted; this test needs at least one invocation to verify run attribution")
	}
}

// TestSuiteRun_TestFinishedCarriesRunIdentity verifies that a ProgressTestFinished
// event carries the identity of the attempt the repetition settled on.
func TestSuiteRun_TestFinishedCarriesRunIdentity(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	var found bool
	for _, ev := range sink.all() {
		if ev.Kind != domain.ProgressTestFinished {
			continue
		}
		found = true
		if ev.Run.RunID == "" {
			t.Errorf("ProgressTestFinished.Run.RunID is empty; per-run events must carry a non-empty run identity")
		}
		if ev.Run.TestID != "test-a" {
			t.Errorf("ProgressTestFinished.Run.TestID = %q, want %q", ev.Run.TestID, "test-a")
		}
	}
	if !found {
		t.Fatal("no ProgressTestFinished event was emitted")
	}
}

// TestSuiteRun_PerRunEventsCarrySameRunIdentityAcrossTheirLifecycle verifies
// that the test-started, invocation, and test-finished events for one
// repetition all carry the same Run.RunID. A run whose events carry
// inconsistent identities cannot be correlated after the fact.
func TestSuiteRun_PerRunEventsCarrySameRunIdentityAcrossTheirLifecycle(t *testing.T) {
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Records = []domain.LogRecord{
		{Kind: domain.RecordStart, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}, Outcome: domain.OutcomeRewritePrompt},
	}
	runner.scriptFor("test-a", scriptedOutcome{evidence: ev})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	// Collect the run identities from each per-run event kind.
	runIDs := map[domain.ProgressKind]string{}
	for _, ev := range sink.all() {
		switch ev.Kind {
		case domain.ProgressTestStarted, domain.ProgressInvocation, domain.ProgressTestFinished:
			runIDs[ev.Kind] = ev.Run.RunID
		}
	}

	if len(runIDs) == 0 {
		t.Fatal("no per-run events were emitted")
	}

	// All per-run events for this single-run suite must carry the same RunID.
	var canonical string
	for _, id := range runIDs {
		if canonical == "" {
			canonical = id
		}
	}
	for kind, id := range runIDs {
		if id != canonical {
			t.Errorf("%s.Run.RunID = %q, want %q; all per-run events for one repetition must carry the same run identity", kind, id, canonical)
		}
	}
}

// TestSuiteRun_SuiteStartedHasNoRunIdentity verifies that a ProgressSuiteStarted
// event carries a zero-valued Run field. Suite-level events belong to the
// suite, not to any single run, so attributing them to one run is wrong.
func TestSuiteRun_SuiteStartedHasNoRunIdentity(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	var found bool
	for _, ev := range sink.all() {
		if ev.Kind != domain.ProgressSuiteStarted {
			continue
		}
		found = true
		if ev.Run != (domain.RunKey{}) {
			t.Errorf("ProgressSuiteStarted.Run = %+v, want zero value; suite-level events belong to no single run and must carry no run identity", ev.Run)
		}
	}
	if !found {
		t.Fatal("no ProgressSuiteStarted event was emitted")
	}
}

// TestSuiteRun_SuiteFinishedHasNoRunIdentity verifies that a ProgressSuiteFinished
// event carries a zero-valued Run field. Suite-level events belong to the
// suite, not to any single run.
func TestSuiteRun_SuiteFinishedHasNoRunIdentity(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	var found bool
	for _, ev := range sink.all() {
		if ev.Kind != domain.ProgressSuiteFinished {
			continue
		}
		found = true
		if ev.Run != (domain.RunKey{}) {
			t.Errorf("ProgressSuiteFinished.Run = %+v, want zero value; suite-level events belong to no single run and must carry no run identity", ev.Run)
		}
	}
	if !found {
		t.Fatal("no ProgressSuiteFinished event was emitted")
	}
}

// TestSuiteRun_MultipleTestsCarryDistinctRunIdentities verifies that when a
// suite runs more than one test, each test's per-run events carry a distinct
// run identity. Sharing one run ID across tests makes events unattributable.
func TestSuiteRun_MultipleTestsCarryDistinctRunIdentities(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	runner.scriptFor("test-b", scriptedOutcome{evidence: passingEvidence()})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(
		resolvedTest("test-a", 1, 1.0),
		resolvedTest("test-b", 1, 1.0),
	)

	_, err := runSuite(t, s, context.Background(), plan)
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	// Collect run IDs by test ID from test-started events.
	runIDByTest := map[string]string{}
	for _, ev := range sink.all() {
		if ev.Kind == domain.ProgressTestStarted {
			runIDByTest[ev.Run.TestID] = ev.Run.RunID
		}
	}

	if len(runIDByTest) < 2 {
		t.Fatalf("collected run IDs for %d test(s), want 2; need at least two test-started events to assert distinctness", len(runIDByTest))
	}

	idA, idB := runIDByTest["test-a"], runIDByTest["test-b"]
	if idA == "" || idB == "" {
		t.Fatalf("missing run ID: test-a=%q test-b=%q; both tests must emit a test-started event with a non-empty run identity", idA, idB)
	}
	if idA == idB {
		t.Errorf("test-a and test-b share run identity %q; each run must carry a distinct identity", idA)
	}
}
