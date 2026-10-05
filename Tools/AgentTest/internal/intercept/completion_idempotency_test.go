package intercept_test

// Tests for completion idempotency: exactly one completion is recorded per
// agent identifier. A later completion carrying the identifier of an agent
// that already completed is a bare passthrough.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

func agentCompletion(agentID, observed string) domain.InterceptedCall {
	return domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		AgentID:          agentID,
		ObservedResponse: observed,
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}
}

// twoAgentsInFlight returns a state with two dispatches outstanding, each
// bound to its own agent, both with pending stubs expecting the given reply.
func twoAgentsInFlight(expected json.RawMessage) domain.RunState {
	s := baseState()
	s.SequenceCounter = 2
	for i, p := range []struct{ agent, token string }{{"agent-a", "tok-a"}, {"agent-b", "tok-b"}} {
		s.InFlight[p.token] = domain.InFlight{Seq: i + 1, Identity: researcherIdentity(), StartedAt: time.Now()}
		s.PendingStubs[p.token] = domain.PendingStub{Seq: i + 1, Identity: researcherIdentity(), Expected: expected}
	}
	s.AgentDispatch = map[string]string{"agent-a": "tok-a", "agent-b": "tok-b"}
	return s
}

func mustDecide(t *testing.T, in intercept.Input) intercept.Decision {
	t.Helper()
	d, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	return d
}

func assertBarePassthrough(t *testing.T, d intercept.Decision) {
	t.Helper()
	if d.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want passthrough", d.Outcome.Kind)
	}
	if len(d.Records) != 0 {
		t.Errorf("Records = %+v, want none", d.Records)
	}
	if len(d.SideEffects) != 0 {
		t.Errorf("SideEffects = %+v, want none", d.SideEffects)
	}
	if d.TerminateSubject {
		t.Error("TerminateSubject = true, want false")
	}
	if !isZeroDelta(d.Delta) {
		t.Errorf("Delta = %+v, want zero delta", d.Delta)
	}
}

func isZeroDelta(d domain.StateDelta) bool {
	return d.SequenceIncrement == 0 && len(d.CollaboratorIncrements) == 0 &&
		len(d.AddPending) == 0 && len(d.ResolvePending) == 0 &&
		len(d.MarkInFlight) == 0 && len(d.ClearInFlight) == 0 &&
		!d.SetEarlyExitTriggered && len(d.EnqueueUnclaimed) == 0 &&
		d.DequeueUnclaimed == 0 && len(d.BindAgent) == 0 &&
		len(d.ReleaseAgents) == 0 && len(d.CompleteAgents) == 0
}

func endRecords(d intercept.Decision) int {
	n := 0
	for _, r := range d.Records {
		if r.Kind == domain.RecordEnd {
			n++
		}
	}
	return n
}

func runEvents(d intercept.Decision, event domain.RunEventKind) int {
	n := 0
	for _, r := range d.Records {
		if r.Kind == domain.RecordRun && r.Event == event {
			n++
		}
	}
	return n
}

func TestDecide_Completion_FirstCorrelatedCompletion_MarksAgentCompleted(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	in := intercept.Input{
		Call:  agentCompletion("agent-a", string(reply)),
		State: twoAgentsInFlight(reply),
		Now:   time.Now(),
	}

	d := mustDecide(t, in)

	if got := d.Delta.CompleteAgents; len(got) != 1 || got[0] != "agent-a" {
		t.Errorf("Delta.CompleteAgents = %v, want [agent-a]", got)
	}
	if endRecords(d) != 1 {
		t.Fatalf("end records = %d, want 1", endRecords(d))
	}
	if d.Records[0].Echo == nil || !d.Records[0].Echo.Match {
		t.Errorf("first completion must still carry a matching echo, got %+v", d.Records[0].Echo)
	}
}

func TestDecide_Completion_FirstUncorrelatedCompletionWithAgentID_MarksAgentCompleted(t *testing.T) {
	// A helper that was never dispatched resolves to no token; it is still
	// the agent's one completion.
	in := intercept.Input{
		Call:  agentCompletion("helper-x", "done"),
		State: baseState(),
		Now:   time.Now(),
	}

	d := mustDecide(t, in)

	if got := d.Delta.CompleteAgents; len(got) != 1 || got[0] != "helper-x" {
		t.Errorf("Delta.CompleteAgents = %v, want [helper-x]", got)
	}
}

func TestDecide_Completion_EmptyAgentID_IsNeverMarked(t *testing.T) {
	id := researcherIdentity()
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	in := intercept.Input{
		Call:     completionCall(id, "tok-1", string(reply), domain.HarnessCapabilities{}),
		State:    statePendingOne(id, "tok-1", reply),
		Registry: registryWithOneStub(id, string(reply), nil),
		Now:      time.Now(),
	}

	d := mustDecide(t, in)

	if len(d.Delta.CompleteAgents) != 0 {
		t.Errorf("Delta.CompleteAgents = %v, want none for empty AgentID", d.Delta.CompleteAgents)
	}
}

func TestDecide_Completion_EmptyAgentID_RepeatedCompletionsAreNotGuarded(t *testing.T) {
	// Adapters without agent identity must behave exactly as before.
	in := intercept.Input{
		Call:  domain.InterceptedCall{Phase: domain.PhaseCompletion, ObservedResponse: "x"},
		State: baseState(),
		Now:   time.Now(),
	}

	first := mustDecide(t, in)
	in.State = in.State.Apply(first.Delta)
	second := mustDecide(t, in)

	if endRecords(first) != 1 || endRecords(second) != 1 {
		t.Errorf("end records first=%d second=%d, want 1 each", endRecords(first), endRecords(second))
	}
}

func TestDecide_Completion_SecondCompletionForSameAgent_IsBarePassthrough(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	state := twoAgentsInFlight(reply)
	first := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", string(reply)), State: state, Now: time.Now()})
	state = state.Apply(first.Delta)

	second := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", "later text"), State: state, Now: time.Now()})

	assertBarePassthrough(t, second)
}

func TestDecide_Completion_SecondCompletion_WithOtherDispatchesInFlight_EmitsNoUncorrelatedEvent(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	state := twoAgentsInFlight(reply)
	first := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", string(reply)), State: state, Now: time.Now()})
	state = state.Apply(first.Delta)
	if len(state.InFlight) == 0 {
		t.Fatal("precondition: agent-b's dispatch must still be in flight")
	}

	second := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", "x"), State: state, Now: time.Now()})

	if n := runEvents(second, domain.RunEventUncorrelatedCompletion); n != 0 {
		t.Errorf("uncorrelated-completion events = %d, want 0", n)
	}
	assertBarePassthrough(t, second)
}

func TestDecide_Completion_SecondCompletion_DoesNotDisturbOtherAgentsCompletion(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	state := twoAgentsInFlight(reply)
	first := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", string(reply)), State: state, Now: time.Now()})
	state = state.Apply(first.Delta)
	dup := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", "x"), State: state, Now: time.Now()})
	state = state.Apply(dup.Delta)

	other := mustDecide(t, intercept.Input{Call: agentCompletion("agent-b", string(reply)), State: state, Now: time.Now()})

	if endRecords(other) != 1 {
		t.Fatalf("agent-b end records = %d, want 1", endRecords(other))
	}
	if other.Records[0].CorrelationToken != "tok-b" {
		t.Errorf("agent-b end record token = %q, want tok-b", other.Records[0].CorrelationToken)
	}
}

func TestDecide_Completion_CompletedAgent_NeverTriggersCutoff(t *testing.T) {
	// Threshold already reached by the counter, cutoff not yet fired: a
	// guarded duplicate must not fire it.
	state := baseState()
	state.SequenceCounter = 3
	state.EarlyExitThreshold = 1
	state.CompletedAgents = map[string]bool{"agent-a": true}

	d := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", "x"), State: state, Now: time.Now()})

	assertBarePassthrough(t, d)
}

func TestDecide_Completion_FirstCompletionAtNthDispatch_StillTerminatesOnce(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	state := twoAgentsInFlight(reply)
	state.EarlyExitThreshold = 1

	first := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", string(reply)), State: state, Now: time.Now()})
	if !first.TerminateSubject || runEvents(first, domain.RunEventEarlyExitTriggered) != 1 {
		t.Fatalf("first completion at threshold: TerminateSubject=%v, early-exit records=%d; want true and 1",
			first.TerminateSubject, runEvents(first, domain.RunEventEarlyExitTriggered))
	}
	state = state.Apply(first.Delta)

	second := mustDecide(t, intercept.Input{Call: agentCompletion("agent-a", "x"), State: state, Now: time.Now()})

	assertBarePassthrough(t, second)
}

func TestDecide_Completion_CompletedAgent_IgnoredEvenWhenCallCarriesToken(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)
	state := twoAgentsInFlight(reply)
	state.CompletedAgents = map[string]bool{"agent-a": true}
	call := agentCompletion("agent-a", "x")
	call.CorrelationToken = "tok-a"

	d := mustDecide(t, intercept.Input{Call: call, State: state, Now: time.Now()})

	assertBarePassthrough(t, d)
}

func TestDecide_Completion_GenuinelyUncorrelatedFirstCompletion_StillReportsUncorrelated(t *testing.T) {
	reply := json.RawMessage(`{"status_code":"SUCCESS"}`)

	d := mustDecide(t, intercept.Input{
		Call:  agentCompletion("agent-unknown", "x"),
		State: twoAgentsInFlight(reply),
		Now:   time.Now(),
	})

	if n := runEvents(d, domain.RunEventUncorrelatedCompletion); n == 0 {
		t.Error("uncorrelated-completion events = 0, want at least 1 for a genuinely uncorrelated first completion")
	}
}
