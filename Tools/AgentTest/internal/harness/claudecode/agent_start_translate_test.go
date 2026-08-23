package claudecode_test

// Tests for translating this harness's agent-start signal (SubagentStart hook)
// into the normalized model.
//
// The agent-start event is the in-session signal that a dispatched collaborator
// has started. It carries the agent identifier the completion event will later
// carry, and nothing else this adapter uses for correlation. It carries:
//   - AgentID, populated from agent_id
//   - Phase set to PhaseAgentStart
//   - CorrelationToken left empty (SubagentStart carries no tool_use_id)
//   - ObservedResponse left empty (there is no reply yet)
//
// domain.PhaseAgentStart does not exist until the implementation lands, which
// is what makes these tests fail to compile in TDD RED state. The AgentStartPayload
// type already exists in native.go; TranslateCall(PhaseAgentStart, ...) is
// what the implementation must add.

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// nativeAgentStart builds a minimal SubagentStart payload for the given agent
// identifier, in the claudecode adapter's own wire shape.
func nativeAgentStart(t *testing.T, agentID string) []byte {
	t.Helper()
	payload := claudecode.AgentStartPayload{
		HookEventName: "SubagentStart",
		SessionID:     "session-abc",
		AgentID:       agentID,
		AgentType:     "subagent",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("nativeAgentStart: marshal failed: %v", err)
	}
	return b
}

// --- Translation: SubagentStart → PhaseAgentStart ---------------------------

func TestTranslateCall_AgentStart_PopulatesAgentID(t *testing.T) {
	a := newTestAdapter()
	agentID := "agent-caef3b21"

	// domain.PhaseAgentStart does not exist yet — compile failure is expected
	// until the implementation lands.
	call, err := a.TranslateCall(domain.PhaseAgentStart, nativeAgentStart(t, agentID))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
	}

	if call.AgentID != agentID {
		t.Errorf("TranslateCall(PhaseAgentStart): AgentID = %q, want %q", call.AgentID, agentID)
	}
}

func TestTranslateCall_AgentStart_PhaseIsAgentStart(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhaseAgentStart, nativeAgentStart(t, "agent-xyz"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
	}

	if call.Phase != domain.PhaseAgentStart {
		t.Errorf("TranslateCall(PhaseAgentStart): Phase = %q, want %q", call.Phase, domain.PhaseAgentStart)
	}
}

func TestTranslateCall_AgentStart_CorrelationTokenIsEmpty(t *testing.T) {
	// SubagentStart carries no tool_use_id — the correlation token is always
	// empty at this phase, just as it is on SubagentStop.
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhaseAgentStart, nativeAgentStart(t, "agent-xyz"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
	}

	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall(PhaseAgentStart): CorrelationToken = %q, want empty — SubagentStart carries no tool_use_id", call.CorrelationToken)
	}
}

func TestTranslateCall_AgentStart_ObservedResponseIsEmpty(t *testing.T) {
	// There is no collaborator reply at the agent-start point; ObservedResponse
	// must be empty.
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhaseAgentStart, nativeAgentStart(t, "agent-xyz"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
	}

	if call.ObservedResponse != "" {
		t.Errorf("TranslateCall(PhaseAgentStart): ObservedResponse = %q, want empty — no reply exists at the agent-start point", call.ObservedResponse)
	}
}

func TestTranslateCall_AgentStart_CarriesCapabilityFlags(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhaseAgentStart, nativeAgentStart(t, "agent-xyz"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseAgentStart): %v", err)
	}

	caps := a.Capabilities()
	if call.Capabilities.CorrelationField != caps.CorrelationField {
		t.Errorf("TranslateCall(PhaseAgentStart): Capabilities.CorrelationField = %q, want %q", call.Capabilities.CorrelationField, caps.CorrelationField)
	}
	if call.Capabilities.CompletionCorrelationField != caps.CompletionCorrelationField {
		t.Errorf("TranslateCall(PhaseAgentStart): Capabilities.CompletionCorrelationField = %q, want %q", call.Capabilities.CompletionCorrelationField, caps.CompletionCorrelationField)
	}
}

// --- Completion correlation field carries no banned vocabulary ---------------

func TestCompletionCorrelationField_ContainsNoBannedVocabulary(t *testing.T) {
	// The completion correlation field name appears in diagnostic output and
	// capability declarations. It must not contain vocabulary that reveals to
	// the subject that it is being exercised.
	bannedWords := []string{"test", "mosaic", "stub", "fake", "mock", "harness"}
	a := newTestAdapter()
	field := strings.ToLower(a.Capabilities().CompletionCorrelationField)
	for _, banned := range bannedWords {
		if strings.Contains(field, banned) {
			t.Errorf("CompletionCorrelationField %q contains test-revealing vocabulary %q; the adapter must use an innocuous native field name",
				a.Capabilities().CompletionCorrelationField, banned)
		}
	}
}

// --- Error handling: SubagentStart payloads ---------------------------------

func TestTranslateCall_AgentStart_MalformedPayload_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall(PhaseAgentStart, malformed): panicked: %v", r)
		}
	}()

	_, err := a.TranslateCall(domain.PhaseAgentStart, []byte(`not json`))
	if err == nil {
		t.Fatal("TranslateCall(PhaseAgentStart, malformed): expected an error, got nil")
	}
}

func TestTranslateCall_AgentStart_WrongEventName_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	payload := claudecode.AgentStartPayload{
		HookEventName: "SubagentStop", // wrong event name for an agent-start translation
		SessionID:     "session-abc",
		AgentID:       "agent-xyz",
		AgentType:     "subagent",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, err = a.TranslateCall(domain.PhaseAgentStart, b)
	if err == nil {
		t.Fatal("TranslateCall(PhaseAgentStart, wrong event): expected an error for wrong hook_event_name, got nil")
	}
	if !strings.Contains(err.Error(), "hook_event_name") {
		t.Errorf("TranslateCall(PhaseAgentStart, wrong event): error = %v, want it to name hook_event_name", err)
	}
}

func TestTranslateCall_AgentStart_EmptyAgentID_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	payload := claudecode.AgentStartPayload{
		HookEventName: "SubagentStart",
		SessionID:     "session-abc",
		AgentID:       "", // empty — not determinable
		AgentType:     "subagent",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, err = a.TranslateCall(domain.PhaseAgentStart, b)
	if err == nil {
		t.Fatal("TranslateCall(PhaseAgentStart, empty agent_id): expected an error, got nil")
	}
}

// --- AgentStartEntry: registration produces no rewriting hook ---------------

func TestAgentStartEntry_DoesNotRewriteInput(t *testing.T) {
	// The agent-start hook is purely observational: it never rewrites the
	// intercepted call's input and therefore must not contribute to the
	// single-rewriter composition invariant Compose enforces.
	bridge := claudecode.Bridge{}
	entry := claudecode.AgentStartEntry(bridge)

	if entry.RewritesInput {
		t.Errorf("AgentStartEntry: Contribution.RewritesInput = true; the agent-start hook must never rewrite the intercepted call's input — it is observation only")
	}
}

func TestAgentStartEntry_MatchesEveryAgentStartEvent(t *testing.T) {
	// The agent-start entry must register a hook for the SubagentStart event
	// and contain at least one matcher targeting that event.
	bridge := claudecode.Bridge{}
	entry := claudecode.AgentStartEntry(bridge)

	matchers, ok := entry.Settings.Hooks["SubagentStart"]
	if !ok {
		t.Fatalf("AgentStartEntry: Hooks[\"SubagentStart\"] is absent; the agent-start registration must target the SubagentStart event")
	}
	if len(matchers) == 0 {
		t.Errorf("AgentStartEntry: Hooks[\"SubagentStart\"] is empty; at least one matcher is required")
	}
}
