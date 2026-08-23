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

// --- Loud correlation failure: T2.1, T2.2, T2.3 ---
//
// A completion event that arrives while a dispatch is outstanding but cannot
// be correlated to it must emit a named run event rather than silently
// producing an empty end record with no sequence number, no identity and no
// echo outcome. A completion event arriving when nothing is outstanding is a
// legitimate un-stubbed dispatch and must not raise any condition.

// TestDecide_Completion_CorrelationFails_WithOutstandingDispatch_EmitsUncorrelatedCompletionEvent
// is the core of the loud-failure contract: when a completion arrives with
// no token while a dispatch is outstanding, the decision must include a
// RecordRun event naming the failure. The current implementation silently
// drops the end record without any condition — this test must fail (RED) until
// decidePost is updated to check CorrelationExpected and emit the event.
func TestDecide_Completion_CorrelationFails_WithOutstandingDispatch_EmitsUncorrelatedCompletionEvent(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	// A dispatch is outstanding: token "corr-1" is pending and in-flight.
	state := statePendingOne(id, "corr-1", expected)

	// The completion call carries no token — correlation will fail.
	in := intercept.Input{
		Call:     completionCall(id, "", "some reply", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	// A RecordRun with RunEventUncorrelatedCompletion must be present to
	// name the failure. Without it the incomplete end record is invisible.
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventUncorrelatedCompletion {
			found = true
			if rec.Detail == "" {
				t.Error("RunEventUncorrelatedCompletion record must carry a non-empty Detail naming how many dispatches were outstanding")
			}
		}
	}
	if !found {
		t.Errorf("expected a RecordRun with RunEventUncorrelatedCompletion but none found in %+v", decision.Records)
	}
}

// TestDecide_Completion_UncorrelatedEnd_HasZeroSeqAndNoIdentity pins the
// observable shape of the end record when correlation failed: Seq must be
// zero (no concrete dispatch was resolved) and no identity fallback must
// be applied. The named run event is what reports the failure; the end
// record must not impersonate a well-formed correlated record.
func TestDecide_Completion_UncorrelatedEnd_HasZeroSeqAndNoIdentity(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := statePendingOne(id, "corr-1", expected)

	in := intercept.Input{
		Call:     completionCall(id, "", "some reply", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	// The end record must exist (completion always emits one) but must not
	// carry a concrete sequence number or identity when correlation failed.
	var endRec *domain.LogRecord
	for i := range decision.Records {
		if decision.Records[i].Kind == domain.RecordEnd {
			r := decision.Records[i]
			endRec = &r
		}
	}
	if endRec == nil {
		t.Fatal("expected a RecordEnd to always be present on the completion path, got none")
	}
	if endRec.Seq != 0 {
		t.Errorf("end record Seq = %d, want 0 when correlation failed (the SeqFromCounter fallback must leave Seq zero)", endRec.Seq)
	}
	if endRec.Echo != nil {
		t.Errorf("end record Echo = %+v, want nil when no pending stub was found — echo non-evaluation must be implicit here", endRec.Echo)
	}
}

// TestDecide_Completion_LegitimateUnstubbed_NoOutstandingDispatches_NoCondition
// is the T2.3 complement: when no dispatches are outstanding at all, the same
// empty-token scenario is a legitimate un-stubbed completion and must not
// raise RunEventUncorrelatedCompletion. The distinction between the two
// cases must key on whether a dispatch existed, not on the token being empty.
func TestDecide_Completion_LegitimateUnstubbed_NoOutstandingDispatches_NoCondition(t *testing.T) {
	id := researcherIdentity()

	// No pending stubs, no in-flight entries — nothing is outstanding.
	state := baseState()

	// Completion with no token: this is a legitimate un-stubbed completion.
	in := intercept.Input{
		Call:     completionCall(id, "", "some reply", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventUncorrelatedCompletion {
			t.Errorf("RunEventUncorrelatedCompletion must not fire for a legitimate un-stubbed completion (nothing was outstanding): %+v", rec)
		}
	}
}

// TestDecide_Completion_OutcomeIsAlwaysPassthrough_EvenOnCorrelationFailure
// pins that an uncorrelated completion still yields OutcomePassthrough. The
// collaborator has already run by this point; refusing it damages the subject.
func TestDecide_Completion_OutcomeIsAlwaysPassthrough_EvenOnCorrelationFailure(t *testing.T) {
	id := researcherIdentity()
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := statePendingOne(id, "corr-1", expected)

	in := intercept.Input{
		Call:     completionCall(id, "", "some reply", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q — the completion event must never halt or deny even on correlation failure", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// --- CorrelationExpected contract ---

// TestCorrelationExpected_WithPendingStubs_ReturnsTrue pins that a run state
// holding pending stubs reports correlation as expected: a completion event
// in this state that resolves to nothing is a genuine failure, not a benign
// un-stubbed dispatch.
func TestCorrelationExpected_WithPendingStubs_ReturnsTrue(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.PendingStubs["token-1"] = domain.PendingStub{Seq: 1, Identity: id, Expected: json.RawMessage(`{}`)}

	if !intercept.CorrelationExpected(state) {
		t.Error("CorrelationExpected must return true when the state holds at least one pending stub")
	}
}

// TestCorrelationExpected_WithInFlightEntries_ReturnsTrue pins that the
// predicate also fires on in-flight entries alone: a passthrough-policy
// dispatch creates an in-flight entry but no pending stub, and a completion
// that cannot be correlated to it is still a failure.
func TestCorrelationExpected_WithInFlightEntries_ReturnsTrue(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.InFlight["token-1"] = domain.InFlight{Seq: 1, Identity: id}

	if !intercept.CorrelationExpected(state) {
		t.Error("CorrelationExpected must return true when the state holds at least one in-flight entry")
	}
}

// TestCorrelationExpected_WithNothingOutstanding_ReturnsFalse pins the
// benign case: nothing is outstanding, so any completion arriving now is a
// legitimate un-stubbed dispatch and raises no condition.
func TestCorrelationExpected_WithNothingOutstanding_ReturnsFalse(t *testing.T) {
	state := baseState()

	if intercept.CorrelationExpected(state) {
		t.Error("CorrelationExpected must return false when nothing is outstanding — this is the legitimate un-stubbed case")
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
