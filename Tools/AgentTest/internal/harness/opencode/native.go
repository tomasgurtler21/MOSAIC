package opencode

// Native hook payload types and the neutral reply. See ContractsDesign.md's
// "ToolBeforePayload / ToolAfterPayload" and "HookReply" sections for the
// full contract. These are the only types in this package that speak
// OpenCode's native language; no package outside internal/harness/ may name
// them.

import (
	"encoding/json"

	"mosaic-agent-test/internal/domain"
)

// ToolBeforePayload is the pre-invocation payload the generated plugin
// writes to the interceptor's standard input. HookEventName is the plugin's
// own discriminator, present so a payload can be validated as being for the
// phase it claims rather than inferred from context.
type ToolBeforePayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"sessionID"`
	CallID        string          `json:"callID"`
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args"`
}

// ToolAfterPayload is the post-invocation payload. Args is echoed as the
// call actually ran, which is what makes the planted correlation token
// recoverable here.
type ToolAfterPayload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"sessionID"`
	CallID        string          `json:"callID"`
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args"`

	// Output is the real tool's result as the after-hook received it. Its
	// runtime shape varies across at least three observed forms: bare XML text
	// (raw bytes starting with '<'), a JSON-encoded string whose value may be
	// an XML envelope, and a JSON content array of the form
	// {"content":[{"type":"text","text":"…"}]}. An {"output":"…"} object is
	// also handled. The field is carried raw and interpreted defensively by
	// extractOutput rather than decoded into one assumed shape.
	Output json.RawMessage `json:"output"`
}

// TaskToolArgs is the dispatch tool's argument object. SubagentType is where
// the composite collaborator identity's agent half comes from; it is empty
// for a plain tool call, which is exactly the future mode the composite
// identity exists to accommodate.
type TaskToolArgs struct {
	SubagentType string `json:"subagent_type,omitempty"`
	Description  string `json:"description,omitempty"`
	Prompt       string `json:"prompt"`
}

// HookReply is the outbound reply the interceptor writes to standard output
// and the generated plugin reads. This shape is defined by this adapter, not
// by the harness: the plugin is generated here, so the wire between the two
// halves is ours to specify.
type HookReply struct {
	// Decision is "allow" or "deny". Empty means no decision — proceed
	// unchanged.
	Decision string `json:"decision,omitempty"`

	// UpdatedArgs replaces the call's arguments before it runs. It is an
	// argument-rewrite mechanism and not a result-substitution mechanism,
	// which is the whole reason this adapter declares direct substitution
	// unsupported.
	UpdatedArgs json.RawMessage `json:"updatedArgs,omitempty"`

	// Reason is surfaced to the subject when the call is denied. The plugin
	// throws it, which is this harness's only way to block a call.
	Reason string `json:"reason,omitempty"`
}

// NeutralReply is the reply that changes nothing. It is what every contained
// failure emits and what the post-invocation phase always emits.
//
// phase is accepted for symmetry with the rest of this package's
// translation surface even though the neutral shape does not currently vary
// by phase: an empty Decision is read by both the pre- and post-invocation
// hooks as "no decision, proceed unchanged".
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
