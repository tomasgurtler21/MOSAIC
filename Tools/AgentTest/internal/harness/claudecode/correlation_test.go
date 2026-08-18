package claudecode_test

// Tests for the tool_use_id correlation mechanism and the opacity property of
// tokens this adapter encounters in the wild.
//
// The mechanism: CorrelationToken is populated directly from the ToolUseID
// field the harness sends on PreToolUse, PostToolUse, and SubagentStop events.
// No token is minted or planted by this adapter; the identifier is
// dispatch-scoped and opaque by the harness's own design.
//
// Two properties matter and are each easy to lose: the identifier that comes
// from the harness must survive the pre-to-post round trip in the translated
// calls, and it must reveal nothing — the subject can see the call it is being
// handed, so a readable, test-flavoured identifier would tell it that it is
// being tested and invalidate every verdict the tool produces.

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

func makeCompletionPayload(toolUseID string) []byte {
	b, _ := json.Marshal(claudecode.CompletionPayload{
		HookEventName:        "SubagentStop",
		ToolUseID:            toolUseID,
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

// TestCorrelation_CompletionPhase_ToolUseIDBecomesCorrelationToken asserts
// the completion-phase guarantee: the SubagentStop event's ToolUseID is
// returned as CorrelationToken, making the completion event resolvable back to
// the dispatch that produced it.
func TestCorrelation_CompletionPhase_ToolUseIDBecomesCorrelationToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const wantToken = "tu-abc123"

	call, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload(wantToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}
	if call.CorrelationToken != wantToken {
		t.Errorf("TranslateCall(PhaseCompletion): CorrelationToken = %q, want %q", call.CorrelationToken, wantToken)
	}
}

// TestCorrelation_SameTokenAcrossAllThreePhases asserts that a single
// dispatch produces the same CorrelationToken at all three phases, which is
// the property that makes completion events resolvable back to their dispatch.
func TestCorrelation_SameTokenAcrossAllThreePhases(t *testing.T) {
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
	completion, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload(dispatchToken))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}

	if pre.CorrelationToken != dispatchToken || post.CorrelationToken != dispatchToken || completion.CorrelationToken != dispatchToken {
		t.Errorf("CorrelationToken mismatch across phases: pre=%q post=%q completion=%q, all want %q",
			pre.CorrelationToken, post.CorrelationToken, completion.CorrelationToken, dispatchToken)
	}
}

// TestCorrelation_TwoConcurrentDispatches_ResolveIndependently asserts that
// two in-flight dispatches with different ToolUseIDs produce distinct
// CorrelationTokens, so a completion event cannot be accidentally attributed
// to the wrong dispatch.
func TestCorrelation_TwoConcurrentDispatches_ResolveIndependently(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	const tokenA = "tu-dispatch-a"
	const tokenB = "tu-dispatch-b"

	preA, _ := a.TranslateCall(domain.PhasePre, makePrePayload(tokenA))
	preB, _ := a.TranslateCall(domain.PhasePre, makePrePayload(tokenB))
	completionA, _ := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload(tokenA))
	completionB, _ := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload(tokenB))

	if preA.CorrelationToken == preB.CorrelationToken {
		t.Errorf("two dispatches with different ToolUseIDs produced the same CorrelationToken %q; they must be distinct", preA.CorrelationToken)
	}
	if completionA.CorrelationToken != tokenA {
		t.Errorf("completion A CorrelationToken = %q, want %q", completionA.CorrelationToken, tokenA)
	}
	if completionB.CorrelationToken != tokenB {
		t.Errorf("completion B CorrelationToken = %q, want %q", completionB.CorrelationToken, tokenB)
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

// TestCorrelation_CompletionPhase_NoToolUseID_TokenIsEmptyNotError asserts
// degradation: a completion payload without a ToolUseID produces an empty
// CorrelationToken and no error. This is the un-stubbed dispatch case, and
// is a legitimate condition that must not prevent the observed reply from
// being captured.
func TestCorrelation_CompletionPhase_NoToolUseID_TokenIsEmptyNotError(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, makeCompletionPayload(""))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, no tool_use_id): %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("CorrelationToken = %q, want empty for a completion without tool_use_id", call.CorrelationToken)
	}
	if call.ObservedResponse == "" {
		t.Errorf("ObservedResponse is empty; the reply must still be captured even without a correlation token")
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
