package intercept_test

// Tests for agent-identity-based completion correlation: the mechanism that
// recovers the dispatch a completion event belongs to when the event carries
// no dispatch identifier.
//
// The implementation adds:
//   - domain.PhaseAgentStart — a fourth interception phase
//   - domain.RunState.UnclaimedDispatches — FIFO queue of recorded dispatches
//   - domain.RunState.AgentDispatch — agent-identifier to token mapping
//   - domain.StateDelta.EnqueueUnclaimed / DequeueUnclaimed / BindAgent / ReleaseAgents
//   - ResolveToken steps 2 (AgentDispatch lookup) and 3 (sole-outstanding fallback)
//   - Decide routing for PhaseAgentStart
//
// All of those compile-fail until the implementation lands, which is the
// correct TDD RED state.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// --- helpers for agent-correlation tests ------------------------------------

func baseStateWithAgentSupport() domain.RunState {
	s := baseState()
	// The following fields do not exist yet; they will be added by the
	// implementation. Referencing them here is what makes these tests fail
	// to compile until the implementation lands.
	s.UnclaimedDispatches = []string{}
	s.AgentDispatch = map[string]string{}
	return s
}

func stateWithOneInFlight(token string, seq int) domain.RunState {
	s := baseStateWithAgentSupport()
	s.SequenceCounter = seq
	s.InFlight = map[string]domain.InFlight{
		token: {Seq: seq, Identity: researcherIdentity(), StartedAt: time.Now()},
	}
	return s
}

func stateWithAgentBinding(agentID, token string, seq int) domain.RunState {
	s := stateWithOneInFlight(token, seq)
	s.AgentDispatch = map[string]string{agentID: token}
	return s
}

func stateWithPendingAndAgentBinding(agentID, token string, seq int, response json.RawMessage) domain.RunState {
	s := baseStateWithAgentSupport()
	s.SequenceCounter = seq
	s.InFlight = map[string]domain.InFlight{
		token: {Seq: seq, Identity: researcherIdentity(), StartedAt: time.Now()},
	}
	s.PendingStubs = map[string]domain.PendingStub{
		token: {Seq: seq, Identity: researcherIdentity(), Expected: response},
	}
	s.AgentDispatch = map[string]string{agentID: token}
	return s
}

// --- T4.1 / AC4.2: completion resolves to dispatch via agent binding --------

func TestResolveToken_AgentDispatch_ReturnsTokenFromAgent(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"

	state := stateWithAgentBinding(agentID, token, 1)

	call := domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		AgentID:          agentID,
		CorrelationToken: "",
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}

	got, src := intercept.ResolveToken(state, call)

	if got != token {
		t.Errorf("ResolveToken: token = %q, want %q", got, token)
	}
	if src != intercept.TokenFromAgent {
		t.Errorf("ResolveToken: source = %q, want %q", src, intercept.TokenFromAgent)
	}
}

func TestDecide_Completion_WithAgentBinding_ProducesEndRecordWithSeqIdentityAndEcho(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"
	response := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			AgentID:          agentID,
			CorrelationToken: "",
			ObservedResponse: string(response),
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    stateWithPendingAndAgentBinding(agentID, token, 1, response),
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if len(decision.Records) == 0 {
		t.Fatal("Decide: no records emitted, want at least one end record")
	}

	// Find the end record.
	var endRec *domain.LogRecord
	for i := range decision.Records {
		if decision.Records[i].Kind == domain.RecordEnd {
			endRec = &decision.Records[i]
			break
		}
	}
	if endRec == nil {
		t.Fatal("Decide: no RecordEnd in decision records")
	}

	if endRec.Seq != 1 {
		t.Errorf("Decide: end record Seq = %d, want 1 (from the dispatch's sequence number)", endRec.Seq)
	}
	if endRec.Identity != researcherIdentity() {
		t.Errorf("Decide: end record Identity = %+v, want %+v", endRec.Identity, researcherIdentity())
	}
	if endRec.Echo == nil {
		t.Error("Decide: end record Echo is nil; an evaluated echo outcome is required when a pending stub was found")
	}
}

// --- T4.2 / AC4.5: association lifecycle -----------------------------------

func TestStateDelta_EnqueueUnclaimed_AppearsInRunState(t *testing.T) {
	token := "corr-token-1"
	state := baseStateWithAgentSupport()

	// The delta produced by the pre-invocation phase must enqueue the token
	// into UnclaimedDispatches so the agent-start phase can pop it.
	delta := domain.StateDelta{
		EnqueueUnclaimed: []string{token},
	}
	next := state.Apply(delta)

	if len(next.UnclaimedDispatches) != 1 || next.UnclaimedDispatches[0] != token {
		t.Errorf("Apply(EnqueueUnclaimed): UnclaimedDispatches = %v, want [%q]", next.UnclaimedDispatches, token)
	}
}

func TestStateDelta_DequeueUnclaimed_RemovesFromHead(t *testing.T) {
	token1, token2 := "corr-token-1", "corr-token-2"
	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{token1, token2}

	// Dequeue one entry from the head; token1 is oldest and must be removed.
	delta := domain.StateDelta{
		DequeueUnclaimed: 1,
	}
	next := state.Apply(delta)

	if len(next.UnclaimedDispatches) != 1 || next.UnclaimedDispatches[0] != token2 {
		t.Errorf("Apply(DequeueUnclaimed 1): UnclaimedDispatches = %v, want [%q]", next.UnclaimedDispatches, token2)
	}
}

func TestStateDelta_BindAgent_AddsMappingToAgentDispatch(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"
	state := baseStateWithAgentSupport()

	delta := domain.StateDelta{
		BindAgent: map[string]string{agentID: token},
	}
	next := state.Apply(delta)

	got, ok := next.AgentDispatch[agentID]
	if !ok {
		t.Fatalf("Apply(BindAgent): AgentDispatch does not contain key %q", agentID)
	}
	if got != token {
		t.Errorf("Apply(BindAgent): AgentDispatch[%q] = %q, want %q", agentID, got, token)
	}
}

func TestStateDelta_ReleaseAgents_RemovesBindingOnCompletion(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"
	state := baseStateWithAgentSupport()
	state.AgentDispatch = map[string]string{agentID: token}

	delta := domain.StateDelta{
		ReleaseAgents: []string{agentID},
	}
	next := state.Apply(delta)

	if _, ok := next.AgentDispatch[agentID]; ok {
		t.Errorf("Apply(ReleaseAgents): AgentDispatch still contains %q after release", agentID)
	}
}

func TestDecide_Completion_ReleasesAgentBindingOnCompletion(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			AgentID:          agentID,
			CorrelationToken: "",
			ObservedResponse: `{"status_code":"SUCCESS"}`,
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    stateWithAgentBinding(agentID, token, 1),
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	// After completion, the agent binding must be released so it does not
	// linger in the run state and cause a false match for a later dispatch.
	found := false
	for _, r := range decision.Delta.ReleaseAgents {
		if r == agentID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Decide(completion): delta does not release agent binding for %q; binding must be removed on completion", agentID)
	}
}

// --- T4.2 / AC4.5: PhaseAgentStart claims from queue and binds --------------

func TestDecide_PhaseAgentStart_ClaimsHeadOfUnclaimedAndBindsAgent(t *testing.T) {
	agentID := "agent-abc123"
	token := "corr-token-1"

	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{token}

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          agentID,
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart): %v", err)
	}

	// Must dequeue exactly one entry from the head of UnclaimedDispatches.
	if decision.Delta.DequeueUnclaimed != 1 {
		t.Errorf("Decide(PhaseAgentStart): Delta.DequeueUnclaimed = %d, want 1", decision.Delta.DequeueUnclaimed)
	}

	// Must bind the agent identifier to the claimed token.
	if decision.Delta.BindAgent == nil {
		t.Fatalf("Decide(PhaseAgentStart): Delta.BindAgent is nil; agent binding must be established")
	}
	bound, ok := decision.Delta.BindAgent[agentID]
	if !ok {
		t.Errorf("Decide(PhaseAgentStart): Delta.BindAgent does not contain key %q", agentID)
	} else if bound != token {
		t.Errorf("Decide(PhaseAgentStart): Delta.BindAgent[%q] = %q, want %q", agentID, bound, token)
	}
}

// --- T4.5 / AC4.6: PhaseAgentStart is always passthrough -------------------

func TestDecide_PhaseAgentStart_OutcomeIsAlwaysPassthrough(t *testing.T) {
	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{"corr-token-1"}

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          "agent-xyz",
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart): %v", err)
	}

	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Decide(PhaseAgentStart): Outcome.Kind = %q, want %q — agent-start is observation only and must never substitute, rewrite or halt",
			decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

func TestDecide_PhaseAgentStart_EmitsNoLogRecords(t *testing.T) {
	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{"corr-token-1"}

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          "agent-xyz",
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart): %v", err)
	}

	if len(decision.Records) != 0 {
		t.Errorf("Decide(PhaseAgentStart): emitted %d records, want 0 — agent-start is observation only and must emit no log records",
			len(decision.Records))
	}
}

func TestDecide_PhaseAgentStart_NeverTerminatesSubject(t *testing.T) {
	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{"corr-token-1"}
	state.EarlyExitThreshold = 1 // threshold already reached, but agent-start must still not terminate
	state.SequenceCounter = 1

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          "agent-xyz",
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart): %v", err)
	}

	if decision.TerminateSubject {
		t.Error("Decide(PhaseAgentStart): TerminateSubject = true; agent-start must never terminate the subject, only completion and post phases may")
	}
}

// --- T4.3 / AC4.3: multi-dispatch ordering ---------------------------------

func TestResolveToken_SoleOutstanding_WhenNoAgentBinding(t *testing.T) {
	// When exactly one dispatch is in flight and has no agent binding yet,
	// the completion event should resolve to it unambiguously via the
	// sole-outstanding fallback.
	token := "corr-token-sole"

	state := baseStateWithAgentSupport()
	state.SequenceCounter = 1
	state.InFlight = map[string]domain.InFlight{
		token: {Seq: 1, Identity: researcherIdentity(), StartedAt: time.Now()},
	}
	// No AgentDispatch entry — the degradation path applies.

	call := domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		AgentID:          "agent-unknown",
		CorrelationToken: "",
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}

	got, src := intercept.ResolveToken(state, call)

	if got != token {
		t.Errorf("ResolveToken(sole-outstanding): token = %q, want %q", got, token)
	}
	if src != intercept.TokenFromSoleOutstanding {
		t.Errorf("ResolveToken(sole-outstanding): source = %q, want %q", src, intercept.TokenFromSoleOutstanding)
	}
}

func TestResolveToken_MultipleOutstanding_NoBinding_Unresolved(t *testing.T) {
	// With two dispatches in flight and no agent binding, resolution must not
	// guess — it must return TokenUnresolved.
	token1 := "corr-token-1"
	token2 := "corr-token-2"

	state := baseStateWithAgentSupport()
	state.SequenceCounter = 2
	state.InFlight = map[string]domain.InFlight{
		token1: {Seq: 1, Identity: researcherIdentity(), StartedAt: time.Now()},
		token2: {Seq: 2, Identity: researcherIdentity(), StartedAt: time.Now()},
	}
	// No AgentDispatch entry.

	call := domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		AgentID:          "agent-unknown",
		CorrelationToken: "",
		Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
	}

	got, src := intercept.ResolveToken(state, call)

	if got != "" {
		t.Errorf("ResolveToken(multiple outstanding, no binding): token = %q, want empty", got)
	}
	if src != intercept.TokenUnresolved {
		t.Errorf("ResolveToken(multiple outstanding, no binding): source = %q, want %q", src, intercept.TokenUnresolved)
	}
}

func TestDecide_Completion_MultipleDispatches_EachResolvesToOwnDispatch(t *testing.T) {
	// Three dispatches in flight, each bound to a distinct agent identifier.
	// Completing them in a different order from their dispatch order must still
	// resolve each to its own dispatch's sequence number and pending stub.
	tokens := []string{"corr-token-1", "corr-token-2", "corr-token-3"}
	agents := []string{"agent-a", "agent-b", "agent-c"}
	response := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := baseStateWithAgentSupport()
	state.SequenceCounter = 3
	state.InFlight = map[string]domain.InFlight{}
	state.PendingStubs = map[string]domain.PendingStub{}
	state.AgentDispatch = map[string]string{}
	for i, tok := range tokens {
		seq := i + 1
		state.InFlight[tok] = domain.InFlight{Seq: seq, Identity: researcherIdentity(), StartedAt: time.Now()}
		state.PendingStubs[tok] = domain.PendingStub{Seq: seq, Identity: researcherIdentity(), Expected: response}
		state.AgentDispatch[agents[i]] = tok
	}

	// Complete agent-b (dispatch 2) first — out of order.
	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			AgentID:          "agent-b",
			CorrelationToken: "",
			ObservedResponse: string(response),
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(multi-dispatch, agent-b): %v", err)
	}

	// Find the end record.
	var endRec *domain.LogRecord
	for i := range decision.Records {
		if decision.Records[i].Kind == domain.RecordEnd {
			endRec = &decision.Records[i]
			break
		}
	}
	if endRec == nil {
		t.Fatal("Decide(multi-dispatch, agent-b): no RecordEnd")
	}

	// agent-b is bound to corr-token-2, which has Seq=2.
	if endRec.Seq != 2 {
		t.Errorf("Decide(multi-dispatch, agent-b): end record Seq = %d, want 2 (the dispatch agent-b was bound to)", endRec.Seq)
	}

	// No uncorrelated-completion event must be emitted.
	for _, r := range decision.Records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
			t.Errorf("Decide(multi-dispatch, agent-b): emitted RunEventUncorrelatedCompletion but resolution should have succeeded via agent binding")
		}
	}
}

// --- T4.4 / AC4.4: uncorrelated completion produces named condition ---------

func TestDecide_Completion_UnresolvableWhileDispatchOutstanding_EmitsUncorrelatedEvent(t *testing.T) {
	// A completion event whose agent identifier maps to nothing, while at
	// least one dispatch is outstanding, must produce RunEventUncorrelatedCompletion
	// rather than a well-formed empty end record.
	token := "corr-token-1"

	state := baseStateWithAgentSupport()
	state.SequenceCounter = 1
	state.InFlight = map[string]domain.InFlight{
		token: {Seq: 1, Identity: researcherIdentity(), StartedAt: time.Now()},
	}
	// AgentDispatch is empty — the completion cannot be correlated.

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			AgentID:          "agent-unknown",
			CorrelationToken: "",
			ObservedResponse: `{"status_code":"SUCCESS"}`,
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(unresolvable completion): %v", err)
	}

	found := false
	for _, r := range decision.Records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
			found = true
			if r.Detail == "" {
				t.Error("Decide(unresolvable completion): RunEventUncorrelatedCompletion record has empty Detail; it must name how many dispatches were outstanding")
			}
			break
		}
	}
	if !found {
		t.Error("Decide(unresolvable completion): no RunEventUncorrelatedCompletion record emitted when completion arrived and a dispatch was outstanding but no binding existed")
	}
}

func TestDecide_Completion_UnresolvableWhenNothingOutstanding_NoUncorrelatedEvent(t *testing.T) {
	// A completion arriving when nothing is outstanding is a legitimate un-stubbed
	// dispatch. It must NOT produce RunEventUncorrelatedCompletion.
	state := baseStateWithAgentSupport()
	// No in-flight, no pending stubs, no agent dispatch.

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseCompletion,
			AgentID:          "agent-unknown",
			CorrelationToken: "",
			ObservedResponse: `{}`,
			Capabilities:     domain.HarnessCapabilities{SupportsReplyRecovery: true},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(nothing outstanding): %v", err)
	}

	for _, r := range decision.Records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
			t.Errorf("Decide(nothing outstanding): emitted RunEventUncorrelatedCompletion, but no dispatch was outstanding so this is a legitimate un-stubbed completion, not a correlation failure")
		}
	}
}

// --- empty-queue degradation path for PhaseAgentStart ----------------------

func TestDecide_PhaseAgentStart_EmptyQueue_IsNeutralPassthrough(t *testing.T) {
	// When the unclaimed queue is empty at the time of the agent-start event
	// (e.g. the event fires before the pre-invocation record is written, or the
	// queue was already exhausted), the phase must:
	//   - return OutcomePassthrough (never halt, substitute or rewrite);
	//   - not panic or return an error;
	//   - set DequeueUnclaimed == 0 (nothing to dequeue from an empty queue).
	state := baseStateWithAgentSupport()
	// UnclaimedDispatches is empty — already the zero value, made explicit.
	state.UnclaimedDispatches = []string{}

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          "agent-late",
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart, empty queue): %v", err)
	}

	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Decide(PhaseAgentStart, empty queue): Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
	if decision.Delta.DequeueUnclaimed != 0 {
		t.Errorf("Decide(PhaseAgentStart, empty queue): Delta.DequeueUnclaimed = %d, want 0 — nothing to dequeue", decision.Delta.DequeueUnclaimed)
	}
	if decision.TerminateSubject {
		t.Error("Decide(PhaseAgentStart, empty queue): TerminateSubject = true; agent-start must never terminate the subject")
	}
}

// --- T4.6 / AC4.7: nothing observable by the subject -----------------------

func TestPhaseAgentStart_NoBannedVocabularyInOutcomeName(t *testing.T) {
	// The phase name itself must not reveal that the subject is being exercised:
	// the phase name appears in interceptor command arguments, and a subject
	// reading its own process tree could observe it.
	bannedWords := []string{"test", "mosaic", "stub", "fake", "mock", "harness", "intercept"}
	phaseName := string(domain.PhaseAgentStart)
	lower := strings.ToLower(phaseName)
	for _, banned := range bannedWords {
		if strings.Contains(lower, banned) {
			t.Errorf("PhaseAgentStart value %q contains test-revealing vocabulary %q; the subject must not be able to detect it is under test", phaseName, banned)
		}
	}
}

func TestDecide_PhaseAgentStart_EmitsNoSideEffects(t *testing.T) {
	// The agent-start phase must write nothing into the subject's directory or
	// any location the subject can read.
	state := baseStateWithAgentSupport()
	state.UnclaimedDispatches = []string{"corr-token-1"}

	in := intercept.Input{
		Call: domain.InterceptedCall{
			Phase:            domain.PhaseAgentStart,
			AgentID:          "agent-xyz",
			CorrelationToken: "",
			Capabilities:     domain.HarnessCapabilities{},
		},
		State:    state,
		Registry: domain.StubRegistry{},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide(PhaseAgentStart): %v", err)
	}

	if len(decision.SideEffects) != 0 {
		t.Errorf("Decide(PhaseAgentStart): %d side effects emitted, want 0 — agent-start must write nothing the subject can observe",
			len(decision.SideEffects))
	}
}
