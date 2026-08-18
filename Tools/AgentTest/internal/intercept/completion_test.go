package intercept_test

// Tests for Decide's handling of domain.PhaseCompletion (T12.2/T12.3): the
// harness's own completion signal, recovered from a later event than the
// post-invocation point on a harness where that point fires at launch (see
// Stage-12/Plan.md's "The defect"). ContractsDesign.md's PhaseCompletion
// contract states this explicitly: "intercept.Decide handles PhaseCompletion
// by recovering the pending stub by correlation token and producing the
// RecordEnd record carrying the EchoOutcome... exactly what decidePost does
// today, now driven by an event that carries a real reply."
//
// Coverage:
//   - AC12.1/AC12.2: a faithful reply on PhaseCompletion produces a passing
//     echo-fidelity result — the previously-impossible case.
//   - AC12.3: echo fidelity stays unconditional; a mismatch is still evidence,
//     recorded but never a halt.
//   - AC12.5: PhaseCompletion never halts or denies, even when the run state
//     would halt a pre-invocation call (the early-exit threshold reached).
//   - AC12.7: an un-stubbed or partially-stubbed dispatch — no pending stub
//     for the recovered token — produces no echo outcome and still passes
//     through cleanly.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

func completionCall(id domain.CollaboratorIdentity, token, observedResponse string, capabilities domain.HarnessCapabilities) domain.InterceptedCall {
	return domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		Identity:         id,
		CorrelationToken: token,
		ObservedResponse: observedResponse,
		Capabilities:     capabilities,
	}
}

func statePendingOne(id domain.CollaboratorIdentity, token string, expected json.RawMessage) domain.RunState {
	s := baseState()
	s.SequenceCounter = 1
	s.CollaboratorCounters[id.Key()] = 1
	s.PendingStubs[token] = domain.PendingStub{Seq: 1, Identity: id, Expected: expected}
	s.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}
	return s
}

// TestDecide_Completion_FaithfulReply_ProducesPassingEchoOutcome is AC12.2:
// the previously-impossible case. A collaborator reply recovered from the
// completion event, matching the pending stub's expected response, must
// produce a matching EchoOutcome — something evaluating against a launch
// acknowledgement could never do.
func TestDecide_Completion_FaithfulReply_ProducesPassingEchoOutcome(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, "corr-1", `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    statePendingOne(id, "corr-1", expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	if len(decision.Records) != 1 {
		t.Fatalf("expected exactly one record, got %d: %+v", len(decision.Records), decision.Records)
	}
	rec := decision.Records[0]
	if rec.Kind != domain.RecordEnd {
		t.Errorf("record Kind = %q, want %q", rec.Kind, domain.RecordEnd)
	}
	if rec.Echo == nil {
		t.Fatal("expected the end record to carry an echo comparison outcome")
	}
	if !rec.Echo.Match {
		t.Errorf("Echo.Match = false, want true for a faithful recovered reply (diff: %s)", rec.Echo.Diff)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q — the completion event never halts or denies", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_Completion_MismatchedReply_RecordsMismatchButStillPassesThrough
// is AC12.3: echo fidelity stays unconditional and un-invertible. A mismatch
// on the completion event is evidence for the verdict engine, not a reason
// for this core to intervene.
func TestDecide_Completion_MismatchedReply_RecordsMismatchButStillPassesThrough(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, "corr-1", `{"status_code":"FAILURE"}`, domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    statePendingOne(id, "corr-1", expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	if len(decision.Records) != 1 || decision.Records[0].Echo == nil {
		t.Fatalf("expected exactly one end record carrying an echo outcome, got %+v", decision.Records)
	}
	if decision.Records[0].Echo.Match {
		t.Error("Echo.Match = true, want false for a mismatched recovered reply")
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q — a mismatch is recorded, never halted on", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_Completion_NoPendingStub_NoEchoOutcomeButStillPassesThrough is
// AC12.7: an un-stubbed dispatch (or one whose token could not be recovered)
// has no pending stub to compare against. That is documented, expected
// behaviour while stub-collaborator generation is deferred — not a failure.
func TestDecide_Completion_NoPendingStub_NoEchoOutcomeButStillPassesThrough(t *testing.T) {
	id := researcherIdentity()

	in := intercept.Input{
		Call:     completionCall(id, "", "an ordinary un-stubbed reply", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    baseState(),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
	if len(decision.Records) != 1 {
		t.Fatalf("expected exactly one record, got %d: %+v", len(decision.Records), decision.Records)
	}
	if decision.Records[0].Echo != nil {
		t.Errorf("expected no echo outcome for an un-stubbed dispatch, got %+v", decision.Records[0].Echo)
	}
}

// TestDecide_Completion_NeverHaltsEvenWhenEarlyExitThresholdReached is
// AC12.5: the completion event fires after the collaborator has already run,
// so it must never halt or deny — even under exactly the run-state condition
// (the early-exit threshold reached) that would halt a pre-invocation call.
// Routing PhaseCompletion through the pre-invocation decision path is
// precisely the misrouting this stage's fix must not do.
func TestDecide_Completion_NeverHaltsEvenWhenEarlyExitThresholdReached(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := statePendingOne(id, "corr-1", expected)
	state.EarlyExitThreshold = 1 // already reached: SequenceCounter (1) >= threshold (1)

	in := intercept.Input{
		Call:     completionCall(id, "corr-1", `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}
	if decision.Outcome.Kind == domain.OutcomeHalt {
		t.Errorf("Outcome.Kind = %q, want anything but a halt — the completion event fires after the collaborator has already run and must never halt or deny", decision.Outcome.Kind)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_Completion_ClearsInFlightBookkeeping mirrors decidePost's own
// bookkeeping obligation: the completion event closes out the in-flight
// entry it corresponds to, the same way the post-invocation point always
// has.
func TestDecide_Completion_ClearsInFlightBookkeeping(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, "corr-1", `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    statePendingOne(id, "corr-1", expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	found := false
	for _, cleared := range decision.Delta.ClearInFlight {
		if cleared == "corr-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("Delta.ClearInFlight = %v, want it to include %q", decision.Delta.ClearInFlight, "corr-1")
	}
}
