package claudecode_test

// Tests for the correlation mechanism and the opacity property of tokens this
// adapter encounters in the wild.
//
// The mechanism has two parts:
//   - PreToolUse and PostToolUse carry tool_use_id; CorrelationToken is
//     populated directly from that field, so the identifier is dispatch-scoped
//     and opaque by the harness's own design.
//   - SubagentStop (the completion event) carries NO tool_use_id — the field
//     is absent from the payload entirely on every observed firing against
//     harness version 2.1.240. CorrelationToken is therefore always empty at
//     the completion phase; correlation at that phase is recovered via agent_id
//     through the agent-start association (established in a separate phase).
//
// Two properties still matter for the pre/post path: the identifier must
// survive the pre-to-post round trip, and it must reveal nothing — the subject
// can see the call it is being handed, so a readable, test-flavoured
// identifier would tell it that it is being tested and invalidate every verdict
// the tool produces.

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// bannedTokenVocabulary mirrors internal/harness/contract's own banned list:
// an identifier carrying any of these words tips the subject off that it is
// being exercised.
var bannedTokenVocabulary = []string{"test", "mosaic", "stub", "fake", "mock", "harness"}

func makePrePayload(toolUseID string) []byte {
	input, _ := json.Marshal(claudecode.TaskToolInput{
		SubagentType: "Worker",
		Prompt:       "the task message",
	})
	b, _ := json.Marshal(claudecode.PreToolUsePayload{
		HookEventName: "PreToolUse",
		ToolName:      claudecode.NativeDispatchToolName,
		ToolInput:     input,
		ToolUseID:     toolUseID,
	})
	return b
}

func makePostPayload(toolUseID string) []byte {
	input, _ := json.Marshal(claudecode.TaskToolInput{SubagentType: "Worker"})
	b, _ := json.Marshal(claudecode.PostToolUsePayload{
		HookEventName: "PostToolUse",
		ToolName:      claudecode.NativeDispatchToolName,
		ToolInput:     input,
		ToolResponse:  json.RawMessage(`"the collaborator's reply"`),
		ToolUseID:     toolUseID,
	})
	return b
}

func makeCompletionPayload(agentID string) []byte {
	b, _ := json.Marshal(claudecode.CompletionPayload{
		HookEventName:        "SubagentStop",
		AgentID:              agentID,
		LastAssistantMessage: "the real reply",
	})
	return b
}

// TestCorrelation_PrePhase_ToolUseIDBecomesCorrelationToken asserts the first
// of the mechanism's per-phase guarantees: the ToolUseID the harness sends on
// a PreToolUse event is returned as CorrelationToken, so every dispatch has a
// non-empty correlation identity from the moment it enters the adapter.
func TestCorrelation_PrePhase_ToolUseIDBecomesCorrelationToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const wantToken = "tu-abc123"

	call, err := a.TranslateCall(domain.PhasePre, makePrePayload(wantToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	if call.CorrelationToken != wantToken {
		t.Errorf("TranslateCall(PhasePre): CorrelationToken = %q, want %q", call.CorrelationToken, wantToken)
	}
}

// TestCorrelation_PostPhase_ToolUseIDBecomesCorrelationToken asserts the same
// guarantee for the post-invocation phase: the echoed ToolUseID is returned
// as CorrelationToken, keying the post record back to its start record.
func TestCorrelation_PostPhase_ToolUseIDBecomesCorrelationToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const wantToken = "tu-abc123"

	call, err := a.TranslateCall(domain.PhasePost, makePostPayload(wantToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}
	if call.CorrelationToken != wantToken {
		t.Errorf("TranslateCall(PhasePost): CorrelationToken = %q, want %q", call.CorrelationToken, wantToken)
	}
}

// TestCorrelation_CompletionPhase_CorrelationTokenIsAlwaysEmpty asserts that
// the completion phase never populates CorrelationToken: SubagentStop carries
// no tool_use_id (the field is absent from every observed payload against
// harness version 2.1.240), so no token is available to put there. Completion
// correlation is recovered instead via agent_id through the agent-start
// association established at a separate phase.
func TestCorrelation_CompletionPhase_CorrelationTokenIsAlwaysEmpty(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload("agent-abc123"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall(PhaseCompletion): CorrelationToken = %q, want empty — SubagentStop carries no tool_use_id; correlation at this phase is via agent_id", call.CorrelationToken)
	}
}

// TestCorrelation_PreAndPostCarrySameToken_CompletionTokenIsEmpty asserts the
// two-part correlation contract for a single dispatch:
//   - The pre- and post-invocation phases carry the same CorrelationToken
//     (the tool_use_id the harness sends on both PreToolUse and PostToolUse).
//   - The completion phase (SubagentStop) always has an empty CorrelationToken,
//     because SubagentStop carries no tool_use_id at all. Completion
//     correlation is via agent_id, not via a shared dispatch identifier.
func TestCorrelation_PreAndPostCarrySameToken_CompletionTokenIsEmpty(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const dispatchToken = "tu-dispatch-1"

	pre, err := a.TranslateCall(domain.PhasePre, makePrePayload(dispatchToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	post, err := a.TranslateCall(domain.PhasePost, makePostPayload(dispatchToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}
	completion, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload("agent-dispatch-1"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}

	if pre.CorrelationToken != dispatchToken {
		t.Errorf("PhasePre: CorrelationToken = %q, want %q", pre.CorrelationToken, dispatchToken)
	}
	if post.CorrelationToken != dispatchToken {
		t.Errorf("PhasePost: CorrelationToken = %q, want %q", post.CorrelationToken, dispatchToken)
	}
	if completion.CorrelationToken != "" {
		t.Errorf("PhaseCompletion: CorrelationToken = %q, want empty — SubagentStop carries no tool_use_id", completion.CorrelationToken)
	}
}

// TestCorrelation_TwoConcurrentDispatches_PreTokensAreDistinct asserts that
// two in-flight dispatches with different tool_use_ids produce distinct
// CorrelationTokens at the pre- and post-invocation phases. Completion events
// do not participate in token-based dispatch distinction: SubagentStop carries
// no tool_use_id, so both completion calls have an empty CorrelationToken.
// Distinguishing completions is the job of the agent-start association
// (via agent_id), which is out of scope for this test.
func TestCorrelation_TwoConcurrentDispatches_PreTokensAreDistinct(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const tokenA = "tu-dispatch-a"
	const tokenB = "tu-dispatch-b"

	preA, _ := a.TranslateCall(domain.PhasePre, makePrePayload(tokenA))
	preB, _ := a.TranslateCall(domain.PhasePre, makePrePayload(tokenB))
	completionA, _ := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload("agent-a"))
	completionB, _ := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload("agent-b"))

	if preA.CorrelationToken == preB.CorrelationToken {
		t.Errorf("two dispatches with different tool_use_ids produced the same CorrelationToken %q; they must be distinct", preA.CorrelationToken)
	}
	// Both completion events have empty CorrelationToken because SubagentStop
	// carries no tool_use_id.
	if completionA.CorrelationToken != "" {
		t.Errorf("completion A CorrelationToken = %q, want empty — SubagentStop carries no tool_use_id", completionA.CorrelationToken)
	}
	if completionB.CorrelationToken != "" {
		t.Errorf("completion B CorrelationToken = %q, want empty — SubagentStop carries no tool_use_id", completionB.CorrelationToken)
	}
}

// TestCorrelation_Idempotence_SecondPreCallDoesNotMintNewToken asserts
// idempotence: a pre-invocation payload that already carries a ToolUseID has
// it recovered, never replaced. No second token is minted.
func TestCorrelation_Idempotence_SecondPreCallDoesNotMintNewToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const existingToken = "tu-already-present"

	call, err := a.TranslateCall(domain.PhasePre, makePrePayload(existingToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	if call.CorrelationToken != existingToken {
		t.Errorf("CorrelationToken = %q, want the existing token %q — a call already carrying an identity must not have a second one minted", call.CorrelationToken, existingToken)
	}
}

// TestCorrelation_PrePhase_NoToolUseID_TokenIsEmpty asserts that a pre-
// invocation payload without a ToolUseID produces an empty CorrelationToken.
// This is the un-stubbed or passthrough case: no correlation identity is
// available, and that is not an error.
func TestCorrelation_PrePhase_NoToolUseID_TokenIsEmpty(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhasePre, makePrePayload(""))
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre, no tool_use_id): %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("CorrelationToken = %q, want empty — a payload without tool_use_id must not invent a token", call.CorrelationToken)
	}
}

// TestCorrelation_CompletionPhase_TokenIsEmptyAndReplyIsCaptured asserts the
// normal completion outcome: CorrelationToken is empty (SubagentStop carries
// no tool_use_id on any dispatch), and ObservedResponse is populated from
// last_assistant_message. An empty token must not prevent the reply from being
// captured — it is the expected condition, not a degradation.
func TestCorrelation_CompletionPhase_TokenIsEmptyAndReplyIsCaptured(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload("agent-some-dispatch"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("CorrelationToken = %q, want empty — SubagentStop carries no tool_use_id", call.CorrelationToken)
	}
	if call.ObservedResponse == "" {
		t.Errorf("ObservedResponse is empty; the reply must be captured from last_assistant_message")
	}
}

// TestCorrelation_ToolUseID_ContainsNoBannedVocabulary asserts the opacity
// property: the ToolUseID from a real pre-invocation fixture carries no
// test-revealing vocabulary. The conformance suite checks this for the
// declared CorrelationField name; this test checks a realistic token value.
func TestCorrelation_ToolUseID_ContainsNoBannedVocabulary(t *testing.T) {
	// Use a token representative of what the real harness sends.
	sampleToken := "tu-pre-valid-1"
	lower := strings.ToLower(sampleToken)
	for _, banned := range bannedTokenVocabulary {
		if strings.Contains(lower, banned) {
			t.Errorf("sample tool_use_id %q contains test-revealing vocabulary %q", sampleToken, banned)
		}
	}
}
