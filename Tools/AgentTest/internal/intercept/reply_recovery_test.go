package intercept_test

// Tests for interception reply recovery (Stage 4).
//
// Coverage:
//   - Post point on a reply-recovery harness: with SupportsReplyRecovery true,
//     the post event must be a bare passthrough — no end record, no
//     ResolvePending, no ClearInFlight. The pending dispatch must survive
//     unchanged so the completion event can find it. With the capability false
//     the post-point behavior is preserved exactly.
//   - Completion event identity rule: the end record carries the call's own
//     identity when it is non-zero; falls back to the correlated pending
//     dispatch's identity when the call identity is zero; stays zero when
//     neither is available.
//   - Concurrent, out-of-order completions: several dispatches in flight
//     simultaneously complete in an order other than the one they were
//     dispatched in. Each completion must be attributed to its own pending
//     dispatch and to no other. These tests are the guard against a
//     single-pending-dispatch shortcut; they fail if one is ever introduced.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// postCallWithCapabilities builds a PhasePost call carrying the supplied
// HarnessCapabilities. The existing postCall helper in intercept_test.go does
// not expose capabilities, so a separate helper is needed for the reply-recovery
// cases where SupportsReplyRecovery matters.
func postCallWithCapabilities(id domain.CollaboratorIdentity, token, observedResponse string, caps domain.HarnessCapabilities) domain.InterceptedCall {
	return domain.InterceptedCall{
		Phase:            domain.PhasePost,
		Identity:         id,
		CorrelationToken: token,
		ObservedResponse: observedResponse,
		Capabilities:     caps,
	}
}

// collaboratorA, collaboratorB, collaboratorC are three distinct identities for
// the concurrent-dispatch tests. They are separate from researcherIdentity to
// make each test's expected attribution unambiguous.
func collaboratorA() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "collaborator-a"}
}

func collaboratorB() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "collaborator-b"}
}

func collaboratorC() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "collaborator-c"}
}

// stateWithThreePending returns a run state representing three simultaneous
// in-flight dispatches for three distinct identities.
func stateWithThreePending(
	tokenA string, idA domain.CollaboratorIdentity, expectedA json.RawMessage,
	tokenB string, idB domain.CollaboratorIdentity, expectedB json.RawMessage,
	tokenC string, idC domain.CollaboratorIdentity, expectedC json.RawMessage,
) domain.RunState {
	s := baseState()
	s.SequenceCounter = 3
	s.CollaboratorCounters[idA.Key()] = 1
	s.CollaboratorCounters[idB.Key()] = 1
	s.CollaboratorCounters[idC.Key()] = 1
	s.PendingStubs[tokenA] = domain.PendingStub{Seq: 1, Identity: idA, Expected: expectedA}
	s.PendingStubs[tokenB] = domain.PendingStub{Seq: 2, Identity: idB, Expected: expectedB}
	s.PendingStubs[tokenC] = domain.PendingStub{Seq: 3, Identity: idC, Expected: expectedC}
	s.InFlight[tokenA] = domain.InFlight{Seq: 1, Identity: idA, StartedAt: time.Now()}
	s.InFlight[tokenB] = domain.InFlight{Seq: 2, Identity: idB, StartedAt: time.Now()}
	s.InFlight[tokenC] = domain.InFlight{Seq: 3, Identity: idC, StartedAt: time.Now()}
	return s
}

// --- Post point on a reply-recovery harness ---

// TestDecide_PostInvocation_WithReplyRecovery_IsBarePassthrough confirms that
// a post event on a harness that declares SupportsReplyRecovery=true resolves
// to OutcomePassthrough. This is the first property of a bare passthrough.
func TestDecide_PostInvocation_WithReplyRecovery_IsBarePassthrough(t *testing.T) {
	id := researcherIdentity()
	token := "corr-recovery-passthrough"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, "launch-ack", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_PostInvocation_WithReplyRecovery_EmitsNoEndRecord confirms that
// a post event on a reply-recovery harness produces no end record. Exactly one
// end record must be produced per dispatch, and it must come from the completion
// event, which carries the real reply.
func TestDecide_PostInvocation_WithReplyRecovery_EmitsNoEndRecord(t *testing.T) {
	id := researcherIdentity()
	token := "corr-recovery-no-end-record"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, "launch-ack", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	rec := endRecordFor(decision.Records, token)
	if rec != nil {
		t.Errorf("post event on a reply-recovery harness must not emit an end record (want nil), got %+v", rec)
	}
}

// TestDecide_PostInvocation_WithReplyRecovery_DoesNotResolvePending confirms
// that a post event on a reply-recovery harness does not mark the pending
// dispatch as resolved. The pending dispatch must still be in state so the
// completion event can find it by correlation token.
func TestDecide_PostInvocation_WithReplyRecovery_DoesNotResolvePending(t *testing.T) {
	id := researcherIdentity()
	token := "corr-recovery-no-resolve"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, "launch-ack", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	for _, resolved := range decision.Delta.ResolvePending {
		if resolved == token {
			t.Errorf("Delta.ResolvePending = %v, must not contain %q on a reply-recovery harness post event", decision.Delta.ResolvePending, token)
		}
	}
}

// TestDecide_PostInvocation_WithReplyRecovery_DoesNotClearInFlight confirms
// that a post event on a reply-recovery harness does not clear the in-flight
// entry. The in-flight entry must survive so the completion event can close it.
func TestDecide_PostInvocation_WithReplyRecovery_DoesNotClearInFlight(t *testing.T) {
	id := researcherIdentity()
	token := "corr-recovery-no-clear-inflight"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, "launch-ack", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	for _, cleared := range decision.Delta.ClearInFlight {
		if cleared == token {
			t.Errorf("Delta.ClearInFlight = %v, must not contain %q on a reply-recovery harness post event", decision.Delta.ClearInFlight, token)
		}
	}
}

// TestDecide_PostInvocation_WithReplyRecovery_DeltaIsEffectivelyZero confirms
// that a post event on a reply-recovery harness produces a zero-valued delta:
// no state mutation of any kind. The pending dispatch and in-flight entry are
// the completion event's to close, not the post event's.
func TestDecide_PostInvocation_WithReplyRecovery_DeltaIsEffectivelyZero(t *testing.T) {
	id := researcherIdentity()
	token := "corr-recovery-zero-delta"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, "launch-ack", domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if len(decision.Delta.ClearInFlight) != 0 {
		t.Errorf("Delta.ClearInFlight = %v, want empty on a reply-recovery post event", decision.Delta.ClearInFlight)
	}
	if len(decision.Delta.ResolvePending) != 0 {
		t.Errorf("Delta.ResolvePending = %v, want empty on a reply-recovery post event", decision.Delta.ResolvePending)
	}
	if decision.Delta.SequenceIncrement != 0 {
		t.Errorf("Delta.SequenceIncrement = %d, want 0 on a reply-recovery post event", decision.Delta.SequenceIncrement)
	}
}

// TestDecide_PostInvocation_WithoutReplyRecovery_BehaviorUnchanged is the
// mirror of the above: with SupportsReplyRecovery=false the post point must
// behave exactly as it always has — echo comparison, ResolvePending,
// ClearInFlight, and one end record. This pins the capability=false side so
// the gate's condition cannot silently flip.
func TestDecide_PostInvocation_WithoutReplyRecovery_BehaviorUnchanged(t *testing.T) {
	id := researcherIdentity()
	token := "corr-no-recovery-unchanged"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCallWithCapabilities(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{SupportsReplyRecovery: false}),
		State:    postState(token, id, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	// Must still emit an end record.
	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("post event with SupportsReplyRecovery=false must still emit an end record for %q", token)
	}

	// Must still compare echo fidelity.
	if rec.Echo == nil {
		t.Errorf("end record must carry an echo comparison result when SupportsReplyRecovery=false, got nil")
	}

	// Must still resolve the pending dispatch.
	found := false
	for _, r := range decision.Delta.ResolvePending {
		if r == token {
			found = true
		}
	}
	if !found {
		t.Errorf("Delta.ResolvePending = %v, must contain %q when SupportsReplyRecovery=false", decision.Delta.ResolvePending, token)
	}

	// Must still clear the in-flight entry.
	found = false
	for _, c := range decision.Delta.ClearInFlight {
		if c == token {
			found = true
		}
	}
	if !found {
		t.Errorf("Delta.ClearInFlight = %v, must contain %q when SupportsReplyRecovery=false", decision.Delta.ClearInFlight, token)
	}
}

// --- Completion event identity rule ---

// TestDecide_Completion_CallIdentityPresent_EndRecordUsesCallIdentity covers
// the first branch of the identity rule: when the completion call carries a
// non-zero identity, the end record must use that identity.
func TestDecide_Completion_CallIdentityPresent_EndRecordUsesCallIdentity(t *testing.T) {
	id := researcherIdentity()
	token := "corr-completion-identity-present"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{SupportsReplyRecovery: true}),
		State:    statePendingOne(id, token, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("expected an end record for correlation token %q, got %+v", token, decision.Records)
	}
	if rec.Identity != id {
		t.Errorf("end record Identity = %+v, want %+v (call's own identity)", rec.Identity, id)
	}
}

// TestDecide_Completion_ZeroCallIdentity_FallsBackToPendingIdentity covers
// the second branch of the identity rule: when the completion call arrives with
// a zero identity — the harness did not carry the dispatch identity on the
// completion event — the end record must fall back to the identity from the
// correlated pending dispatch. Without this fallback, the end record is
// identityless and downstream attribution breaks.
func TestDecide_Completion_ZeroCallIdentity_FallsBackToPendingIdentity(t *testing.T) {
	id := researcherIdentity()
	token := "corr-completion-zero-identity"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	// Zero identity on the call — the harness did not carry it.
	zeroIdentityCall := domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		Identity:         domain.CollaboratorIdentity{},
		CorrelationToken: token,
		ObservedResponse: `{"status_code":"SUCCESS"}`,
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}

	in := intercept.Input{
		Call:     zeroIdentityCall,
		State:    statePendingOne(id, token, expected),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("expected an end record for correlation token %q, got %+v", token, decision.Records)
	}
	if rec.Identity == (domain.CollaboratorIdentity{}) {
		t.Errorf("end record Identity is zero, want the pending dispatch's identity %+v — the fallback to the correlated pending dispatch's identity is required when the call's own identity is zero", id)
	}
	if rec.Identity != id {
		t.Errorf("end record Identity = %+v, want %+v (pending dispatch's identity)", rec.Identity, id)
	}
}

// TestDecide_Completion_ZeroCallIdentity_NoPendingDispatch_IdentityRemainsZero
// covers the third branch of the identity rule: when the call identity is zero
// and there is no correlated pending dispatch, the end record's identity stays
// zero. The fallback must reach only to the correlated dispatch, never to a
// default or a guess.
func TestDecide_Completion_ZeroCallIdentity_NoPendingDispatch_IdentityRemainsZero(t *testing.T) {
	token := "corr-no-pending-zero"

	zeroIdentityCall := domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		Identity:         domain.CollaboratorIdentity{},
		CorrelationToken: token,
		ObservedResponse: "some reply",
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}

	in := intercept.Input{
		Call:     zeroIdentityCall,
		State:    baseState(), // no pending stubs
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: unexpected error: %v", err)
	}

	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("expected an end record for correlation token %q, got %+v", token, decision.Records)
	}
	if rec.Identity != (domain.CollaboratorIdentity{}) {
		t.Errorf("end record Identity = %+v, want zero — must never invent an identity when there is no correlated pending dispatch", rec.Identity)
	}
}

// --- Concurrent, out-of-order completions ---

// TestDecide_Completion_ConcurrentDispatches_EachAttributedToItsOwnPending is
// the guard against a single-pending-dispatch shortcut. Three dispatches are in
// flight simultaneously; their completion events arrive in reverse dispatch
// order, each with a zero call identity. Each end record must carry the identity
// of the dispatch the correlation token resolves to — not the first, last, or
// only pending dispatch, and not any other dispatch's identity.
//
// This test fails if any implementation uses a shortcut such as "return the
// only pending dispatch" or "return the first pending dispatch found in the map".
func TestDecide_Completion_ConcurrentDispatches_EachAttributedToItsOwnPending(t *testing.T) {
	tokenA := "corr-concurrent-a"
	tokenB := "corr-concurrent-b"
	tokenC := "corr-concurrent-c"
	idA := collaboratorA()
	idB := collaboratorB()
	idC := collaboratorC()
	respA := json.RawMessage(`{"agent":"a"}`)
	respB := json.RawMessage(`{"agent":"b"}`)
	respC := json.RawMessage(`{"agent":"c"}`)

	// All three dispatches in flight at the same time.
	fullState := stateWithThreePending(
		tokenA, idA, respA,
		tokenB, idB, respB,
		tokenC, idC, respC,
	)
	caps := domain.HarnessCapabilities{SupportsReplyRecovery: true}

	// Completion arrives for C first (reverse dispatch order). Call identity is
	// zero — the harness did not carry it on the completion event.
	decC, err := intercept.Decide(intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			Identity:         domain.CollaboratorIdentity{},
			CorrelationToken: tokenC,
			ObservedResponse: `{"agent":"c"}`,
			Capabilities:     caps,
		},
		State:    fullState,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("Decide for C: unexpected error: %v", err)
	}

	// Completion arrives for B second.
	decB, err := intercept.Decide(intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			Identity:         domain.CollaboratorIdentity{},
			CorrelationToken: tokenB,
			ObservedResponse: `{"agent":"b"}`,
			Capabilities:     caps,
		},
		State:    fullState,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("Decide for B: unexpected error: %v", err)
	}

	// Completion arrives for A last.
	decA, err := intercept.Decide(intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			Identity:         domain.CollaboratorIdentity{},
			CorrelationToken: tokenA,
			ObservedResponse: `{"agent":"a"}`,
			Capabilities:     caps,
		},
		State:    fullState,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("Decide for A: unexpected error: %v", err)
	}

	// C's end record must carry C's identity, not A's or B's.
	recC := endRecordFor(decC.Records, tokenC)
	if recC == nil {
		t.Fatalf("expected an end record for tokenC, got %+v", decC.Records)
	}
	if recC.Identity != idC {
		t.Errorf("end record for C: Identity = %+v, want %+v (C's own identity, resolved from C's pending dispatch)", recC.Identity, idC)
	}

	// B's end record must carry B's identity, not A's or C's.
	recB := endRecordFor(decB.Records, tokenB)
	if recB == nil {
		t.Fatalf("expected an end record for tokenB, got %+v", decB.Records)
	}
	if recB.Identity != idB {
		t.Errorf("end record for B: Identity = %+v, want %+v (B's own identity, resolved from B's pending dispatch)", recB.Identity, idB)
	}

	// A's end record must carry A's identity, not B's or C's.
	recA := endRecordFor(decA.Records, tokenA)
	if recA == nil {
		t.Fatalf("expected an end record for tokenA, got %+v", decA.Records)
	}
	if recA.Identity != idA {
		t.Errorf("end record for A: Identity = %+v, want %+v (A's own identity, resolved from A's pending dispatch)", recA.Identity, idA)
	}
}

// TestDecide_Completion_ConcurrentDispatches_EachResolvesOnlyItsOwnToken
// confirms that each completion marks only its own correlation token as
// resolved, not any of the other in-flight dispatches. This is the bookkeeping
// counterpart of the identity attribution test above.
func TestDecide_Completion_ConcurrentDispatches_EachResolvesOnlyItsOwnToken(t *testing.T) {
	tokenA := "corr-resolve-a"
	tokenB := "corr-resolve-b"
	tokenC := "corr-resolve-c"
	idA := collaboratorA()
	idB := collaboratorB()
	idC := collaboratorC()

	fullState := stateWithThreePending(
		tokenA, idA, json.RawMessage(`{"agent":"a"}`),
		tokenB, idB, json.RawMessage(`{"agent":"b"}`),
		tokenC, idC, json.RawMessage(`{"agent":"c"}`),
	)
	caps := domain.HarnessCapabilities{SupportsReplyRecovery: true}

	// Issue only B's completion — A and C must remain untouched.
	decB, err := intercept.Decide(intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			Identity:         domain.CollaboratorIdentity{},
			CorrelationToken: tokenB,
			ObservedResponse: `{"agent":"b"}`,
			Capabilities:     caps,
		},
		State:    fullState,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("Decide for B: unexpected error: %v", err)
	}

	// B's token must be in ResolvePending.
	foundB := false
	for _, r := range decB.Delta.ResolvePending {
		if r == tokenB {
			foundB = true
		}
		// A's and C's tokens must NOT be resolved.
		if r == tokenA || r == tokenC {
			t.Errorf("Delta.ResolvePending = %v, must not contain %q — only B's dispatch was completed", decB.Delta.ResolvePending, r)
		}
	}
	if !foundB {
		t.Errorf("Delta.ResolvePending = %v, must contain %q (B's own token)", decB.Delta.ResolvePending, tokenB)
	}

	// Only B's token must be in ClearInFlight.
	for _, c := range decB.Delta.ClearInFlight {
		if c == tokenA || c == tokenC {
			t.Errorf("Delta.ClearInFlight = %v, must not contain %q — only B's dispatch was completed", decB.Delta.ClearInFlight, c)
		}
	}
}
