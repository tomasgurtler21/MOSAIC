package claudecode

import (
	"encoding/json"

	"mosaic-agent-test/internal/domain"
)

// These are the only types in the module that speak this harness's native
// language, and no package outside internal/harness/ may name them.

// PreToolUsePayload is the pre-invocation hook payload. ToolUseID is the
// harness's own dispatch-scoped identifier for this tool call; it is the
// same value the harness will send on the PostToolUse event that corresponds
// to this dispatch, and is the correlation token this adapter uses on the
// pre- and post-invocation phases. The completion event (SubagentStop) carries
// no tool_use_id; see CompletionPayload and correlation.go for the completion
// correlation mechanism.
type PreToolUsePayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolUseID     string          `json:"tool_use_id,omitempty"`
}

// PostToolUsePayload is the post-invocation hook payload. ToolUseID echoes the
// same dispatch-scoped identifier that was on the originating PreToolUse event,
// carrying the correlation token through to the post-invocation point.
type PostToolUsePayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	ToolUseID     string          `json:"tool_use_id,omitempty"`
}

// CompletionPayload is this harness's completion signal (its SubagentStop
// hook), fired when a dispatched collaborator finishes. It carries the
// collaborator's real reply directly (LastAssistantMessage), which is what
// makes reply recovery possible on a harness whose post-invocation point
// fires at launch. It carries NO dispatch identifier. The field is absent
// from the payload entirely — not merely empty — on every firing observed
// against harness version 2.1.240, under both the default and a relocated
// configuration home. AgentID is the only identifier on this event that
// distinguishes one dispatch from another, and it is what this adapter
// correlates on. See correlation.go for the mechanism and the captured
// evidence it rests on.
type CompletionPayload struct {
	HookEventName        string `json:"hook_event_name"`
	SessionID            string `json:"session_id"`
	AgentID              string `json:"agent_id"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// AgentStartPayload is the harness's in-session agent-start signal (its
// SubagentStart hook). It fires after the dispatch is issued and before the
// completion event for the same collaborator, carries the agent identifier
// the completion event will later carry, and requires no disk read.
//
// It carries no dispatch identifier either, which is why the association it
// establishes rests on dispatch ordering rather than on a shared field: the
// dispatch is recorded at the pre-invocation point and claimed here.
type AgentStartPayload struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	AgentID       string `json:"agent_id"`
	AgentType     string `json:"agent_type"`
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
