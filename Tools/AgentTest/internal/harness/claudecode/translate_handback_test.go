package claudecode_test

// Tests for translating the subagent hand-back at the completion phase: the
// PostToolUse firing of the SubagentHandback tool a subagent calls to deliver
// its report. The reply is tool_input.message (verbatim), the correlation key
// is agent_id, and only the PostToolUse of that exact tool is a completion.
// SubagentStop translation stays as it was.

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// handbackPayload builds a hand-back hook payload from the captured key set.
// Fields in drop are removed; fields in override replace the default.
func handbackPayload(t *testing.T, event string, override map[string]any, drop ...string) []byte {
	t.Helper()
	p := map[string]any{
		"agent_id":        "a24d6c04de2a3dec4",
		"agent_type":      "general-purpose",
		"cwd":             "/fixture/capture",
		"hook_event_name": event,
		"permission_mode": "auto",
		"prompt_id":       "prompt-1",
		"session_id":      "sess-1",
		"tool_input":      map[string]any{"message": `{"status_code":"SUCCESS"}`},
		"tool_name":       "SubagentHandback",
		"tool_use_id":     "toolu_handback_1",
		"transcript_path": "/fixture/capture/sess-1.jsonl",
	}
	if event == "PostToolUse" {
		p["tool_response"] = map[string]any{"success": true, "message": "Report delivered to your caller."}
		p["duration_ms"] = 3
	}
	for k, v := range override {
		p[k] = v
	}
	for _, k := range drop {
		delete(p, k)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal hand-back payload: %v", err)
	}
	return b
}

func TestTranslateCall_Completion_Handback_TakesReplyFromToolInputMessageAndAgentIDFromPayload(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	message := `{"status_code":"SUCCESS","note":"  keep   exact spacing "}`
	native := handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"message": message}})

	call, err := a.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, hand-back): %v", err)
	}

	if call.Phase != domain.PhaseCompletion {
		t.Errorf("Phase = %q, want %q", call.Phase, domain.PhaseCompletion)
	}
	if call.ObservedResponse != message {
		t.Errorf("ObservedResponse = %q, want the hand-back message verbatim %q", call.ObservedResponse, message)
	}
	if call.AgentID != "a24d6c04de2a3dec4" {
		t.Errorf("AgentID = %q, want the payload agent_id", call.AgentID)
	}
	if call.CorrelationToken != "" {
		t.Errorf("CorrelationToken = %q, want empty: the hand-back tool_use_id is not a correlation key", call.CorrelationToken)
	}
	if !reflect.DeepEqual(call.Capabilities, a.Capabilities()) {
		t.Errorf("Capabilities = %+v, want the adapter's own %+v", call.Capabilities, a.Capabilities())
	}
}

func TestTranslateCall_Completion_Handback_AcknowledgementIsNeverTheReply(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, handbackPayload(t, "PostToolUse", nil))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}
	if strings.Contains(call.ObservedResponse, "Report delivered") {
		t.Errorf("ObservedResponse = %q, the tool_response acknowledgement must never be taken as the reply", call.ObservedResponse)
	}
}

func TestTranslateCall_Completion_Handback_EmptyMessageIsADeliveryNotAnError(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	native := handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"message": ""}})

	call, err := a.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		t.Fatalf("TranslateCall(empty message): %v, want a delivery with an empty reply", err)
	}
	if call.ObservedResponse != "" {
		t.Errorf("ObservedResponse = %q, want empty", call.ObservedResponse)
	}
	if call.AgentID == "" {
		t.Errorf("AgentID is empty; an empty message must still be attributed to its agent")
	}
}

func TestTranslateCall_Completion_Handback_NonJSONMessageIsADeliveryNotAnError(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	message := "Done. All tasks complete, no JSON here."
	native := handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"message": message}})

	call, err := a.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		t.Fatalf("TranslateCall(non-JSON message): %v", err)
	}
	if call.ObservedResponse != message {
		t.Errorf("ObservedResponse = %q, want %q", call.ObservedResponse, message)
	}
}

func TestTranslateCall_Completion_Handback_RejectedInputs(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	tests := []struct {
		name    string
		native  []byte
		wantErr error
	}{
		{"not JSON at all", []byte(`{not json`), claudecode.ErrPayloadMalformed},
		{"envelope field of the wrong JSON type", handbackPayload(t, "PostToolUse", map[string]any{"agent_id": 42}), claudecode.ErrPayloadMalformed},
		{"PreToolUse of the hand-back is never a completion", handbackPayload(t, "PreToolUse", nil), claudecode.ErrPayloadUnrecognised},
		{"unknown hook event name", handbackPayload(t, "Stop", nil), claudecode.ErrPayloadUnrecognised},
		{"PostToolUse of another tool is never a silent completion", handbackPayload(t, "PostToolUse", map[string]any{"tool_name": "Agent"}), claudecode.ErrPayloadUnrecognised},
		{"PostToolUse with a near-miss tool name", handbackPayload(t, "PostToolUse", map[string]any{"tool_name": "subagenthandback"}), claudecode.ErrPayloadUnrecognised},
		{"PostToolUse with no tool name", handbackPayload(t, "PostToolUse", nil, "tool_name"), claudecode.ErrPayloadUnrecognised},
		{"empty agent_id", handbackPayload(t, "PostToolUse", map[string]any{"agent_id": ""}), claudecode.ErrIdentityUndetermined},
		{"absent agent_id", handbackPayload(t, "PostToolUse", nil, "agent_id"), claudecode.ErrIdentityUndetermined},
		{"absent tool_input", handbackPayload(t, "PostToolUse", nil, "tool_input"), claudecode.ErrPayloadMalformed},
		{"tool_input not an object", handbackPayload(t, "PostToolUse", map[string]any{"tool_input": "just text"}), claudecode.ErrPayloadMalformed},
		{"tool_input without message", handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"other": "x"}}), claudecode.ErrPayloadMalformed},
		{"message not a string", handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"message": 7}}), claudecode.ErrPayloadMalformed},
		{"message null", handbackPayload(t, "PostToolUse", map[string]any{"tool_input": map[string]any{"message": nil}}), claudecode.ErrPayloadMalformed},
		{
			// Identity is checked before tool_input, so this is the identity error.
			"empty agent_id and missing message: identity wins",
			handbackPayload(t, "PostToolUse", map[string]any{"agent_id": "", "tool_input": map[string]any{}}),
			claudecode.ErrIdentityUndetermined,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("TranslateCall panicked instead of returning an error: %v", r)
				}
			}()
			call, err := a.TranslateCall(domain.PhaseCompletion, tc.native)
			if err == nil {
				t.Fatalf("TranslateCall: expected an error, got call=%+v", call)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("TranslateCall error = %v, want it to wrap %v", err, tc.wantErr)
			}
		})
	}
}

func TestTranslateCall_Completion_SubagentStop_UnchangedByHandbackSupport(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	native, err := json.Marshal(map[string]any{
		"hook_event_name":        "SubagentStop",
		"agent_id":               "a24d6c04de2a3dec4",
		"last_assistant_message": "Thanks, all done.",
		"stop_hook_active":       false,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	call, err := a.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		t.Fatalf("TranslateCall(SubagentStop): %v", err)
	}
	if call.ObservedResponse != "Thanks, all done." || call.AgentID != "a24d6c04de2a3dec4" || call.CorrelationToken != "" {
		t.Errorf("SubagentStop call = %+v, want reply from last_assistant_message and agent_id, empty token", call)
	}
}

func TestTranslateCall_Completion_SubagentStop_EmptyAgentIDStillAccepted(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	native := []byte(`{"hook_event_name":"SubagentStop","agent_id":"","last_assistant_message":"x"}`)

	call, err := a.TranslateCall(domain.PhaseCompletion, native)
	if err != nil {
		t.Fatalf("TranslateCall(SubagentStop, empty agent_id): %v, want it accepted exactly as before", err)
	}
	if call.AgentID != "" || call.ObservedResponse != "x" {
		t.Errorf("call = %+v", call)
	}
}

func TestTranslateOutcome_Completion_Handback_AlwaysNeutral(t *testing.T) {
	a := claudecode.New(claudecode.Options{})
	call, err := a.TranslateCall(domain.PhaseCompletion, handbackPayload(t, "PostToolUse", nil))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	native, err := a.TranslateOutcome(domain.InterceptionOutcome{Kind: domain.OutcomeHalt, HaltReason: domain.HaltUnmatched, Message: "no"}, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}
	reply := decodeHookReply(t, native)
	if reply.HookSpecificOutput != nil && reply.HookSpecificOutput.PermissionDecision == "deny" {
		t.Errorf("hand-back completion reply denied (%+v); it fires after delivery and must never deny", reply.HookSpecificOutput)
	}
}
