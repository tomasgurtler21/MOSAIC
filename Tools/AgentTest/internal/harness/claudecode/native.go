package claudecode

import (
	"encoding/json"

	"mosaic-agent-test/internal/domain"
)

// These are the only types in the module that speak this harness's native
// language, and no package outside internal/harness/ may name them.

// PreToolUsePayload is the pre-invocation hook payload.
type PreToolUsePayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// PostToolUsePayload is the post-invocation hook payload. ToolInput is echoed
// back as the call actually ran, which is what makes the planted correlation
// token recoverable here.
type PostToolUsePayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
}

// CompletionPayload is this harness's completion signal (its SubagentStop
// hook), fired when a dispatched collaborator finishes. It carries the
// collaborator's real reply directly (LastAssistantMessage), which is what
// makes reply recovery possible on a harness whose post-invocation point
// fires at launch rather than at completion. The correlation token planted
// at the pre-invocation point does not travel on this payload: it is
// recovered from the transcript file AgentTranscriptPath names (see
// translateCompletion).
type CompletionPayload struct {
	HookEventName        string `json:"hook_event_name"`
	SessionID            string `json:"session_id"`
	AgentID              string `json:"agent_id"`
	AgentTranscriptPath  string `json:"agent_transcript_path"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// TaskToolInput is the dispatch tool's input. SubagentType is where the
// composite collaborator identity's agent half comes from; it is empty for a
// plain tool call, which is exactly the future mode the composite identity
// exists to accommodate.
type TaskToolInput struct {
	SubagentType string `json:"subagent_type,omitempty"`
	Description  string `json:"description,omitempty"`
	Prompt       string `json:"prompt"`
}

// HookReply is the outbound structured reply.
type HookReply struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput carries the permission decision and, when rewriting, the
// replacement tool input.
type HookSpecificOutput struct {
	HookEventName string `json:"hookEventName"`
	// PermissionDecision is one of allow, deny, ask, defer. Only allow and
	// deny are ever emitted by this adapter.
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	// UpdatedInput replaces the tool's arguments before it runs. It is an
	// input-rewrite mechanism and not a response-substitution mechanism,
	// which is the whole reason this adapter declares direct substitution
	// unsupported.
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
}

// NeutralReply is the reply that changes nothing. It is what every contained
// failure emits, and what the post-invocation phase always emits.
//
// phase is accepted for symmetry with the rest of this package's
// translation surface even though the neutral shape does not currently vary
// by phase: hookSpecificOutput is simply absent, which both the pre- and
// post-invocation interception points read as "no decision, proceed
// unchanged".
func NeutralReply(phase domain.InterceptionPhase) []byte {
	b, err := json.Marshal(HookReply{})
	if err != nil {
		// HookReply{} has no field that can fail to marshal; this path is
		// unreachable in practice. A hard-coded valid reply is still a safer
		// degradation than propagating an error from a function whose whole
		// purpose is "always return something valid".
		return []byte(`{}`)
	}
	return b
}
