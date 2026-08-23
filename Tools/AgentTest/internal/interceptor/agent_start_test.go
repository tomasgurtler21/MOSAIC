package interceptor_test

// Tests for the interceptor shell's handling of PhaseAgentStart:
//
//   - The shell always exits 0, always writes a decodable reply, and always
//     produces OutcomePassthrough — the identical containment obligations the
//     post and completion phases carry already apply here.
//   - Malformed or unrecognised agent-start payloads stay fully contained:
//     exit 0, a decodable reply, and no reach into the run state.
//   - On a well-formed payload the shell commits the agent-start delta
//     (DequeueUnclaimed + BindAgent) to the run state so the completion phase
//     can resolve through it.
//
// domain.PhaseAgentStart does not exist until the implementation lands, so
// every test in this file fails to compile in TDD RED state.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// encodeAgentStart builds a fake-adapter agent-start wire payload. The fake
// adapter's wire shape carries a phase, an agentID, and no token: the
// agent-start event is not a tool-use hook and does not carry a correlation
// token.
func encodeAgentStart(t *testing.T, agentID string) []byte {
	t.Helper()
	// The fake adapter will need to accept PhaseAgentStart payloads.  For now
	// the wire shape is close enough to the existing wireCall that we add an
	// AgentID field and leave Token empty.
	type wireAgentStart struct {
		Phase   string `json:"phase"`
		AgentID string `json:"agent_id"`
	}
	b, err := json.Marshal(wireAgentStart{Phase: string(domain.PhaseAgentStart), AgentID: agentID})
	if err != nil {
		t.Fatalf("encodeAgentStart: marshal failed: %v", err)
	}
	return b
}

// --- T4.5 / AC4.6: PhaseAgentStart always exits 0 and always replies --------

func TestRun_AgentStart_ExitsZero(t *testing.T) {
	state := baseState()
	state.UnclaimedDispatches = []string{"corr-token-1"}
	state.AgentDispatch = map[string]string{}

	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}, domain.HarnessCapabilities{SupportsReplyRecovery: true}, nil)

	native := encodeAgentStart(t, "agent-abc")
	_, exitCode, _ := run(t, h.Config, domain.PhaseAgentStart, native)

	if exitCode != 0 {
		t.Fatalf("Run(PhaseAgentStart) returned exit code %d, want 0 — agent-start must always exit 0", exitCode)
	}
}

func TestRun_AgentStart_AlwaysWritesDecodableReply(t *testing.T) {
	state := baseState()
	state.UnclaimedDispatches = []string{"corr-token-1"}
	state.AgentDispatch = map[string]string{}

	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}, domain.HarnessCapabilities{SupportsReplyRecovery: true}, nil)

	native := encodeAgentStart(t, "agent-abc")
	reply, exitCode, _ := run(t, h.Config, domain.PhaseAgentStart, native)

	if exitCode != 0 {
		t.Fatalf("Run(PhaseAgentStart) exit code %d", exitCode)
	}
	if len(reply) == 0 {
		t.Fatal("Run(PhaseAgentStart): empty reply; the shell must always write a reply")
	}
	got := decodeReply(t, reply)
	if got.Kind != "passthrough" {
		t.Errorf("Run(PhaseAgentStart): reply Kind = %q, want %q — agent-start is observation only", got.Kind, "passthrough")
	}
}

func TestRun_AgentStart_ReplyIsPassthrough(t *testing.T) {
	state := baseState()
	state.UnclaimedDispatches = []string{"corr-token-1"}
	state.AgentDispatch = map[string]string{}

	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}, domain.HarnessCapabilities{SupportsReplyRecovery: true}, nil)

	native := encodeAgentStart(t, "agent-abc")
	reply, _, _ := run(t, h.Config, domain.PhaseAgentStart, native)
	got := decodeReply(t, reply)

	if got.Kind == "halt" {
		t.Errorf("Run(PhaseAgentStart): reply is halt; agent-start must never halt — it is an observation-only phase")
	}
	if got.Kind == "substitute" {
		t.Errorf("Run(PhaseAgentStart): reply is substitute; agent-start must never substitute — it is an observation-only phase")
	}
	if got.Kind != "passthrough" {
		t.Errorf("Run(PhaseAgentStart): reply Kind = %q, want %q", got.Kind, "passthrough")
	}
}

// --- T4.5 / AC4.6: failure containment for PhaseAgentStart -----------------

func TestRun_AgentStart_MalformedPayload_IsContained(t *testing.T) {
	original := baseState()
	original.UnclaimedDispatches = []string{"corr-token-1"}
	original.AgentDispatch = map[string]string{}

	h := newHarness(t, original, domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}, domain.HarnessCapabilities{SupportsReplyRecovery: true}, nil)
	malformed := []byte(`not json at all`)

	reply, exitCode, diag := run(t, h.Config, domain.PhaseAgentStart, malformed)
	assertContained(t, exitCode, reply, diag)

	// Malformed payload must not reach run state: no agent binding must be
	// written, and the unclaimed queue must remain unchanged.
	committed := readState(t, h.Store)
	if len(committed.AgentDispatch) != 0 {
		t.Errorf("Run(PhaseAgentStart, malformed): AgentDispatch = %v, want empty — malformed payload must not create an agent binding", committed.AgentDispatch)
	}
	if len(committed.UnclaimedDispatches) != len(original.UnclaimedDispatches) {
		t.Errorf("Run(PhaseAgentStart, malformed): UnclaimedDispatches = %v, want %v — malformed payload must not dequeue from the unclaimed queue",
			committed.UnclaimedDispatches, original.UnclaimedDispatches)
	}
}

// --- T4.2 / AC4.5: state is committed on a well-formed agent-start ----------

func TestRun_AgentStart_CommitsAgentBindingToState(t *testing.T) {
	agentID := "agent-xyz789"
	token := "corr-token-1"

	state := baseState()
	state.UnclaimedDispatches = []string{token}
	state.AgentDispatch = map[string]string{}

	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough}, domain.HarnessCapabilities{SupportsReplyRecovery: true}, nil)

	native := encodeAgentStart(t, agentID)
	_, exitCode, _ := run(t, h.Config, domain.PhaseAgentStart, native)

	if exitCode != 0 {
		t.Fatalf("Run(PhaseAgentStart) exit code %d", exitCode)
	}

	committed := readState(t, h.Store)

	// The unclaimed queue must be emptied: the token was claimed by this agent-start.
	if len(committed.UnclaimedDispatches) != 0 {
		t.Errorf("Run(PhaseAgentStart): UnclaimedDispatches = %v after agent-start, want empty", committed.UnclaimedDispatches)
	}

	// The agent binding must be present so the completion phase can resolve it.
	bound, ok := committed.AgentDispatch[agentID]
	if !ok {
		t.Errorf("Run(PhaseAgentStart): AgentDispatch does not contain %q after agent-start", agentID)
	} else if bound != token {
		t.Errorf("Run(PhaseAgentStart): AgentDispatch[%q] = %q, want %q", agentID, bound, token)
	}
}
