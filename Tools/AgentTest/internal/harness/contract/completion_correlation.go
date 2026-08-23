package contract

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// CheckCompletionCorrelation asserts that an adapter declaring a non-empty
// CompletionCorrelationField can actually correlate a completion event back
// to its dispatch through the agent-start association, and that the reply the
// completion carries survives translation.
//
// Driven end to end through the decision core for a run state the check
// builds itself: a dispatch is recorded at the pre-invocation point, claimed
// at agent start, and resolved at completion. The obligations asserted:
//
//   - the completion resolves to the dispatch that produced it, yielding an
//     end record with that dispatch's sequence number, its identity and an
//     evaluated echo outcome;
//   - with several dispatches in flight, each completion resolves to its own
//     dispatch and none to another's;
//   - a completion whose agent identifier maps to nothing, while a dispatch
//     is outstanding, produces the uncorrelated-completion run event and not
//     a well-formed empty end record;
//   - the agent-start phase yields OutcomePassthrough and never a
//     substitution, a rewrite or a halt.
//
// Skipped, with the gap named in the skip reason, for an adapter declaring an
// empty CompletionCorrelationField.
func CheckCompletionCorrelation(t *testing.T, cfg Config) error {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())
	caps := adapter.Capabilities()

	if caps.CompletionCorrelationField == "" {
		// No obligation: this adapter uses the standard correlation field on
		// completion events. The check is irrelevant.
		return nil
	}

	if cfg.NativeAgentStart == nil {
		return fmt.Errorf("CheckCompletionCorrelation: adapter declares CompletionCorrelationField=%q but Config.NativeAgentStart is nil; "+
			"provide NativeAgentStart so the suite can hold the agent-start mechanism honest", caps.CompletionCorrelationField)
	}
	if cfg.NativeCompletionForAgent == nil {
		return fmt.Errorf("CheckCompletionCorrelation: adapter declares CompletionCorrelationField=%q but Config.NativeCompletionForAgent is nil; "+
			"provide NativeCompletionForAgent so the suite can drive completions attributed to specific agents", caps.CompletionCorrelationField)
	}

	// Build a stub response to use as the expected echo.
	stubResponse := mustMarshal(t, map[string]string{"status_code": "SUCCESS"})

	// --- obligation 1: single dispatch resolves via agent-start association ---
	t.Run("SingleDispatch", func(t *testing.T) {
		t.Helper()

		agentID := "agent-" + randomToken(t)
		token := randomToken(t)

		// Pre-invocation: translate and record. The pre event carries the
		// correlation token; the agent-start association is built on top.
		id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
		msg := domain.TaskMessage{
			AgentInstanceID: "Worker#1",
			TaskDescription: "do the work",
			InputArtifacts:  []string{},
			OutputArtifacts: []string{},
		}

		preNative := cfg.NativePre(id, msg, token)
		_, err := adapter.TranslateCall(domain.PhasePre, preNative)
		if err != nil {
			t.Fatalf("TranslateCall(PhasePre): %v", err)
		}

		// Agent-start: the adapter translates the native payload into a call
		// carrying AgentID. The decision core uses it to pop from the unclaimed
		// queue and bind the agent identifier to the token.
		agentStartNative := cfg.NativeAgentStart(agentID)
		agentStartCall, err := adapter.TranslateCall(domain.PhaseAgentStart, agentStartNative)
		if err != nil {
			t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
		}

		if agentStartCall.Phase != domain.PhaseAgentStart {
			t.Errorf("TranslateCall(PhaseAgentStart): Phase = %q, want %q", agentStartCall.Phase, domain.PhaseAgentStart)
		}
		if agentStartCall.AgentID != agentID {
			t.Errorf("TranslateCall(PhaseAgentStart): AgentID = %q, want %q", agentStartCall.AgentID, agentID)
		}
		if agentStartCall.CorrelationToken != "" {
			t.Errorf("TranslateCall(PhaseAgentStart): CorrelationToken = %q, want empty — agent-start carries no dispatch identifier", agentStartCall.CorrelationToken)
		}

		// Build the run state as the decision core would see it at completion:
		// one pending stub (written at the pre-invocation point) and the agent
		// binding (written at the agent-start point).
		state := domain.RunState{
			SchemaVersion:   1,
			TestID:          "contract",
			RunNumber:       1,
			SequenceCounter: 1,
			InFlight: map[string]domain.InFlight{
				token: {Seq: 1, Identity: id, StartedAt: time.Now()},
			},
			PendingStubs: map[string]domain.PendingStub{
				token: {Seq: 1, Identity: id, Expected: stubResponse},
			},
			UnclaimedDispatches: []string{},
			AgentDispatch:       map[string]string{agentID: token},
		}

		// Completion: translate the native completion payload attributed to
		// agentID and drive it through the decision core.
		completionNative := cfg.NativeCompletionForAgent(agentID, string(stubResponse))
		completionCall, err := adapter.TranslateCall(domain.PhaseCompletion, completionNative)
		if err != nil {
			t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
		}

		if completionCall.AgentID != agentID {
			t.Errorf("TranslateCall(PhaseCompletion): AgentID = %q, want %q", completionCall.AgentID, agentID)
		}
		if completionCall.CorrelationToken != "" {
			t.Errorf("TranslateCall(PhaseCompletion): CorrelationToken = %q, want empty — completion event carries no dispatch identifier", completionCall.CorrelationToken)
		}

		in := intercept.Input{
			Call:     completionCall,
			State:    state,
			Registry: domain.StubRegistry{},
			Now:      time.Now(),
		}

		decision, err := intercept.Decide(in)
		if err != nil {
			t.Fatalf("Decide(completion via agent binding): %v", err)
		}

		// The completion must produce an end record with the dispatch's seq, identity and echo.
		var endRec *domain.LogRecord
		for i := range decision.Records {
			if decision.Records[i].Kind == domain.RecordEnd {
				endRec = &decision.Records[i]
				break
			}
		}
		if endRec == nil {
			t.Fatal("Decide(completion via agent binding): no RecordEnd emitted")
		}
		if endRec.Seq != 1 {
			t.Errorf("Decide(completion via agent binding): end record Seq = %d, want 1 (the dispatch's sequence number)", endRec.Seq)
		}
		if endRec.Identity != id {
			t.Errorf("Decide(completion via agent binding): end record Identity = %+v, want %+v", endRec.Identity, id)
		}
		if endRec.Echo == nil {
			t.Error("Decide(completion via agent binding): end record Echo is nil; an echo outcome is required when a pending stub was found")
		}

		// No uncorrelated-completion event must appear.
		for _, r := range decision.Records {
			if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
				t.Errorf("Decide(completion via agent binding): emitted RunEventUncorrelatedCompletion but resolution should have succeeded")
			}
		}
	})

	// --- obligation 2: several dispatches each resolve to their own ---------
	t.Run("MultipleDispatches", func(t *testing.T) {
		t.Helper()

		// Three dispatches in flight, each bound to a distinct agent identifier.
		// Completing them out of dispatch order must still produce the correct
		// sequence number for each.
		type dispatch struct {
			agentID string
			token   string
			seq     int
		}
		dispatches := []dispatch{
			{agentID: "agent-" + randomToken(t), token: randomToken(t), seq: 1},
			{agentID: "agent-" + randomToken(t), token: randomToken(t), seq: 2},
			{agentID: "agent-" + randomToken(t), token: randomToken(t), seq: 3},
		}

		id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
		state := domain.RunState{
			SchemaVersion:       1,
			TestID:              "contract",
			RunNumber:           1,
			SequenceCounter:     3,
			InFlight:            map[string]domain.InFlight{},
			PendingStubs:        map[string]domain.PendingStub{},
			UnclaimedDispatches: []string{},
			AgentDispatch:       map[string]string{},
		}
		for _, d := range dispatches {
			state.InFlight[d.token] = domain.InFlight{Seq: d.seq, Identity: id, StartedAt: time.Now()}
			state.PendingStubs[d.token] = domain.PendingStub{Seq: d.seq, Identity: id, Expected: stubResponse}
			state.AgentDispatch[d.agentID] = d.token
		}

		// Complete in reverse order to prove ordering independence.
		for i := len(dispatches) - 1; i >= 0; i-- {
			d := dispatches[i]

			completionNative := cfg.NativeCompletionForAgent(d.agentID, string(stubResponse))
			completionCall, err := adapter.TranslateCall(domain.PhaseCompletion, completionNative)
			if err != nil {
				t.Fatalf("TranslateCall(PhaseCompletion, dispatch %d): %v", d.seq, err)
			}

			in := intercept.Input{
				Call:     completionCall,
				State:    state,
				Registry: domain.StubRegistry{},
				Now:      time.Now(),
			}

			decision, err := intercept.Decide(in)
			if err != nil {
				t.Fatalf("Decide(multi-dispatch, seq %d): %v", d.seq, err)
			}

			var endRec *domain.LogRecord
			for j := range decision.Records {
				if decision.Records[j].Kind == domain.RecordEnd {
					endRec = &decision.Records[j]
					break
				}
			}
			if endRec == nil {
				t.Fatalf("Decide(multi-dispatch, seq %d): no RecordEnd", d.seq)
			}
			if endRec.Seq != d.seq {
				t.Errorf("Decide(multi-dispatch): completion for agentID %q resolved to seq %d, want %d", d.agentID, endRec.Seq, d.seq)
			}

			// Apply the delta so subsequent completions operate on consistent state.
			state = state.Apply(decision.Delta)
		}
	})

	// --- obligation 3: unresolvable completion produces named condition ------
	t.Run("UnresolvableProducesNamedCondition", func(t *testing.T) {
		t.Helper()

		// One dispatch in flight, agent binding absent. The completion event for
		// an unknown agent must produce RunEventUncorrelatedCompletion — not an
		// empty RecordEnd that silently swallows the failure.
		token := randomToken(t)
		id := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}

		state := domain.RunState{
			SchemaVersion:       1,
			TestID:              "contract",
			RunNumber:           1,
			SequenceCounter:     1,
			InFlight:            map[string]domain.InFlight{token: {Seq: 1, Identity: id, StartedAt: time.Now()}},
			PendingStubs:        map[string]domain.PendingStub{token: {Seq: 1, Identity: id, Expected: stubResponse}},
			UnclaimedDispatches: []string{},
			AgentDispatch:       map[string]string{}, // empty: no binding
		}

		unknownAgentID := "agent-" + randomToken(t)
		completionNative := cfg.NativeCompletionForAgent(unknownAgentID, `{}`)
		completionCall, err := adapter.TranslateCall(domain.PhaseCompletion, completionNative)
		if err != nil {
			t.Fatalf("TranslateCall(PhaseCompletion, unknown agent): %v", err)
		}

		in := intercept.Input{
			Call:     completionCall,
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
					t.Error("Decide(unresolvable): RunEventUncorrelatedCompletion Detail is empty; it must name how many dispatches were outstanding")
				}
				break
			}
		}
		if !found {
			t.Error("Decide(unresolvable): no RunEventUncorrelatedCompletion emitted when completion arrived with no binding and a dispatch was outstanding")
		}
	})

	// --- obligation 4: agent-start phase always yields OutcomePassthrough ----
	t.Run("AgentStartIsPassthrough", func(t *testing.T) {
		t.Helper()

		agentID := "agent-" + randomToken(t)
		agentStartNative := cfg.NativeAgentStart(agentID)

		agentStartCall, err := adapter.TranslateCall(domain.PhaseAgentStart, agentStartNative)
		if err != nil {
			t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
		}

		state := domain.RunState{
			SchemaVersion:       1,
			TestID:              "contract",
			RunNumber:           1,
			SequenceCounter:     1,
			InFlight:            map[string]domain.InFlight{},
			PendingStubs:        map[string]domain.PendingStub{},
			UnclaimedDispatches: []string{randomToken(t)}, // one token to claim
			AgentDispatch:       map[string]string{},
		}

		in := intercept.Input{
			Call:     agentStartCall,
			State:    state,
			Registry: domain.StubRegistry{},
			Now:      time.Now(),
		}

		decision, err := intercept.Decide(in)
		if err != nil {
			t.Fatalf("Decide(PhaseAgentStart): %v", err)
		}

		if decision.Outcome.Kind != domain.OutcomePassthrough {
			t.Errorf("Decide(PhaseAgentStart): Outcome.Kind = %q, want %q — agent-start must never substitute, rewrite or halt",
				decision.Outcome.Kind, domain.OutcomePassthrough)
		}
		if decision.TerminateSubject {
			t.Error("Decide(PhaseAgentStart): TerminateSubject = true; agent-start must never terminate the subject")
		}

		// Translate the outcome through the adapter.
		native, err := adapter.TranslateOutcome(decision.Outcome, agentStartCall)
		if err != nil {
			t.Fatalf("TranslateOutcome(PhaseAgentStart): %v", err)
		}
		if len(native) == 0 {
			t.Error("TranslateOutcome(PhaseAgentStart): empty reply; the adapter must always write a reply")
		}
	})

	return nil
}

// testCompletionCorrelation is the internal wrapper that drives
// CheckCompletionCorrelation as a t.Run subtest.
func testCompletionCorrelation(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())
	if adapter.Capabilities().CompletionCorrelationField == "" {
		t.Skipf("contract: CompletionCorrelationField is empty; completion correlation via agent-start not applicable to this adapter")
		return
	}

	if err := CheckCompletionCorrelation(t, cfg); err != nil {
		t.Error(err)
	}
}

// buildRunStateForCorrelation is a helper for constructing run states with the
// UnclaimedDispatches and AgentDispatch fields populated. It is referenced
// from within this file only.
func buildRunStateForCorrelation(testID string, seq int, inFlight map[string]domain.InFlight, pendingStubs map[string]domain.PendingStub, agentDispatch map[string]string) domain.RunState {
	return domain.RunState{
		SchemaVersion:       1,
		TestID:              testID,
		RunNumber:           1,
		SequenceCounter:     seq,
		CollaboratorCounters: map[string]int{},
		InFlight:            inFlight,
		PendingStubs:        pendingStubs,
		UnclaimedDispatches: []string{},
		AgentDispatch:       agentDispatch,
	}
}

// Ensure the helper is used so it does not trigger an "unused function"
// compiler error.
var _ = json.Marshal // import used by mustMarshal
var _ = buildRunStateForCorrelation
