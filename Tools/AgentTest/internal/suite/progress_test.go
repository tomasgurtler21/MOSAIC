package suite_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

// TestSuiteRun_EmitsCompleteOrderedStream asserts every lifecycle point
// emits, in order: suite started, test started, one invocation event per
// observed collaborator call, test finished, suite finished (AC14.4).
func TestSuiteRun_EmitsCompleteOrderedStream(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Records = []domain.LogRecord{
		{Kind: domain.RecordStart, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}, Outcome: domain.OutcomeRewritePrompt},
		{Kind: domain.RecordStart, Seq: 2, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "writer"}, Outcome: domain.OutcomeRewritePrompt},
	}
	runner.scriptFor("test-a", scriptedOutcome{evidence: ev})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	events := sink.all()
	if len(events) == 0 {
		t.Fatalf("no progress events were emitted")
	}

	if events[0].Kind != domain.ProgressSuiteStarted {
		t.Errorf("first event kind = %v, want ProgressSuiteStarted", events[0].Kind)
	}
	if events[0].TotalTests != 1 {
		t.Errorf("ProgressSuiteStarted.TotalTests = %d, want 1", events[0].TotalTests)
	}

	last := events[len(events)-1]
	if last.Kind != domain.ProgressSuiteFinished {
		t.Errorf("last event kind = %v, want ProgressSuiteFinished", last.Kind)
	}
	// ProgressSuiteFinished must carry the same verdict counts and total
	// cost the returned Result reports — not just be the last event in the
	// stream by position (Major #3).
	if last.Kind == domain.ProgressSuiteFinished {
		if len(last.Counts) != len(result.Counts) {
			t.Errorf("ProgressSuiteFinished.Counts = %v, want %v (result.Counts)", last.Counts, result.Counts)
		}
		for verdict, wantCount := range result.Counts {
			if last.Counts[verdict] != wantCount {
				t.Errorf("ProgressSuiteFinished.Counts[%v] = %d, want %d (result.Counts[%v])", verdict, last.Counts[verdict], wantCount, verdict)
			}
		}
		if !reflect.DeepEqual(last.TotalCost, result.TotalCost) {
			t.Errorf("ProgressSuiteFinished.TotalCost = %+v, want %+v (result.TotalCost)", last.TotalCost, result.TotalCost)
		}
	}

	var kinds []domain.ProgressKind
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	assertContainsInOrder(t, kinds, []domain.ProgressKind{
		domain.ProgressSuiteStarted,
		domain.ProgressTestStarted,
		domain.ProgressInvocation,
		domain.ProgressInvocation,
		domain.ProgressTestFinished,
		domain.ProgressSuiteFinished,
	})

	// The two invocation events carry the observed sequence numbers and
	// identities, in the order the collaborators were actually observed.
	var invocations []domain.ProgressEvent
	for _, e := range events {
		if e.Kind == domain.ProgressInvocation {
			invocations = append(invocations, e)
		}
	}
	if len(invocations) != 2 {
		t.Fatalf("got %d invocation events, want 2", len(invocations))
	}
	if invocations[0].Seq != 1 || invocations[1].Seq != 2 {
		t.Errorf("invocation Seq values = [%d, %d], want [1, 2]", invocations[0].Seq, invocations[1].Seq)
	}
	if invocations[0].Identity.AgentIdentity != "researcher" {
		t.Errorf("first invocation identity = %+v, want AgentIdentity researcher", invocations[0].Identity)
	}
}

// TestSuiteRun_TestFinishedCarriesVerdictAndCost asserts ProgressTestFinished
// carries the verdict, duration and cost of the attempt it closes.
func TestSuiteRun_TestFinishedCarriesVerdictAndCost(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Cost = domain.CostReport{TotalUSD: 0.5, Attribution: domain.AttributionAttributed}
	ev.Duration = 3 * time.Second
	runner.scriptFor("test-a", scriptedOutcome{evidence: ev})
	sink := &recordingSink{}
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	_, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	var finished *domain.ProgressEvent
	for _, e := range sink.all() {
		if e.Kind == domain.ProgressTestFinished {
			ev := e
			finished = &ev
			break
		}
	}
	if finished == nil {
		t.Fatalf("no ProgressTestFinished event was emitted")
	}
	if finished.Verdict != domain.VerdictPass {
		t.Errorf("ProgressTestFinished.Verdict = %v, want PASS", finished.Verdict)
	}
	if finished.Cost.TotalUSD != 0.5 {
		t.Errorf("ProgressTestFinished.Cost.TotalUSD = %v, want 0.5", finished.Cost.TotalUSD)
	}
}

// TestSuiteRun_SlowSinkDoesNotStallSuite proves a progress sink that never
// returns from Emit cannot stall the suite (AC14.5).
func TestSuiteRun_SlowSinkDoesNotStallSuite(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	sink := newBlockingSink()
	s := newSuite(runner, newFakeClock(), sink)
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	done := make(chan struct{})
	var runErr error

	// Act
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				runErr = fmt.Errorf("Suite.Run panicked: %v", r)
			}
		}()
		_, runErr = s.Run(context.Background(), plan)
	}()

	// Assert
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Suite.Run did not return within 3s while its progress sink blocked on Emit: a slow or blocking sink must not stall the suite")
	}
	if runErr != nil {
		t.Errorf("Suite.Run returned an error while its progress sink blocked on Emit: %v", runErr)
	}
	if sink.emitCount() == 0 {
		t.Errorf("progress sink was never invoked: this test proves nothing about blocking behaviour without at least one attempted emission")
	}
}

// TestSuiteRun_FailingSinkDoesNotFailRun proves a progress sink that panics
// on Emit cannot fail the run: a display problem is not a test result
// (AC14.5).
func TestSuiteRun_FailingSinkDoesNotFailRun(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	runner.scriptFor("test-a", scriptedOutcome{evidence: passingEvidence()})
	s := newSuite(runner, newFakeClock(), failingSink{})
	plan := buildPlan(resolvedTest("test-a", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error because its progress sink failed: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1: a failing sink must not change the verdict", len(result.Tests))
	}
	if result.Tests[0].Aggregate.Verdict != domain.VerdictPass {
		t.Errorf("verdict = %v, want PASS: a failing progress sink must not change any verdict", result.Tests[0].Aggregate.Verdict)
	}
}

// assertContainsInOrder asserts want appears as a (not necessarily
// contiguous) subsequence of got, in the same relative order.
func assertContainsInOrder(t *testing.T, got, want []domain.ProgressKind) {
	t.Helper()
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("event kinds %v do not contain %v in order", got, want)
	}
}
