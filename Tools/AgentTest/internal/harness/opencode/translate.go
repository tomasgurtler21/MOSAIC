package opencode

// TranslateCall and TranslateOutcome. See ContractsDesign.md's "AgentTest:
// Translation" section for the full contract.

import (
	"encoding/json"
	"errors"
	"fmt"

	"mosaic-agent-test/internal/domain"
)

var (
	ErrPayloadUnrecognised  = errors.New("opencode: unrecognised interception payload")
	ErrPayloadMalformed     = errors.New("opencode: malformed interception payload")
	ErrIdentityUndetermined = errors.New("opencode: collaborator identity not determinable from payload")

	// ErrSubstitutionUnsupported is returned by TranslateOutcome for
	// domain.OutcomeSubstitute. This harness's pre-invocation point can
	// rewrite arguments or block a call but cannot fabricate a successful
	// result, and its post-invocation point's mutations are not honoured.
	// Emitting a plausible-looking reply here is exactly the failure mode
	// the capability-honesty test exists to prevent.
	ErrSubstitutionUnsupported = errors.New("opencode: this harness cannot substitute a result directly")
)

// TranslateCall implements domain.HarnessAdapter. An unrecognised phase, a
// wrong-phase discriminator, a payload missing the fields identity is
// derived from, and a payload that is not valid JSON at all are each
// returned as a handleable error — never a panic and never a zero-valued
// call mistaken for success.
func (a *Adapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	switch phase {
	case domain.PhasePre:
		return a.translatePre(native)
	case domain.PhasePost:
		return a.translatePost(native)
	default:
		return domain.InterceptedCall{}, fmt.Errorf("%w: interception phase %q", ErrPayloadUnrecognised, phase)
	}
}

func (a *Adapter) translatePre(native []byte) (domain.InterceptedCall, error) {
	var payload ToolBeforePayload
	if err := json.Unmarshal(native, &payload); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("%w: %v", ErrPayloadMalformed, err)
	}
	if payload.HookEventName != "tool.execute.before" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: hook_event_name %q", ErrPayloadUnrecognised, payload.HookEventName)
	}
	if payload.Tool == "" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: tool is empty", ErrIdentityUndetermined)
	}

	args, err := decodeTaskToolArgs(payload.Args)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, _ := RecoverToken(args)

	return domain.InterceptedCall{
		Phase:            domain.PhasePre,
		Identity:         domain.CollaboratorIdentity{ToolName: payload.Tool, AgentIdentity: args.SubagentType},
		Message:          parseTaskMessage(args.Prompt),
		CorrelationToken: token,
		RawPayload:       json.RawMessage(native),
		Capabilities:     a.Capabilities(),
	}, nil
}

func (a *Adapter) translatePost(native []byte) (domain.InterceptedCall, error) {
	var payload ToolAfterPayload
	if err := json.Unmarshal(native, &payload); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("%w: %v", ErrPayloadMalformed, err)
	}
	if payload.HookEventName != "tool.execute.after" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: hook_event_name %q", ErrPayloadUnrecognised, payload.HookEventName)
	}
	if payload.Tool == "" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: tool is empty", ErrIdentityUndetermined)
	}

	args, err := decodeTaskToolArgs(payload.Args)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, _ := RecoverToken(args)

	return domain.InterceptedCall{
		Phase:            domain.PhasePost,
		Identity:         domain.CollaboratorIdentity{ToolName: payload.Tool, AgentIdentity: args.SubagentType},
		Message:          parseTaskMessage(args.Prompt),
		CorrelationToken: token,
		RawPayload:       json.RawMessage(native),
		Capabilities:     a.Capabilities(),
		ObservedResponse: extractOutput(payload.Output),
	}, nil
}

// decodeTaskToolArgs decodes args into TaskToolArgs. Absent args decode to
// the zero value rather than an error: a hook event this adapter does not
// recognise is caught earlier, by hook_event_name. A present-but-malformed
// value — for example a JSON string where an object is expected — is a
// handleable error.
func decodeTaskToolArgs(raw json.RawMessage) (TaskToolArgs, error) {
	var args TaskToolArgs
	if len(raw) == 0 {
		return args, nil
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return TaskToolArgs{}, fmt.Errorf("%w: args: %v", ErrPayloadMalformed, err)
	}
	return args, nil
}

// parseTaskMessage recovers the Communication Protocol invocation carried in
// a dispatch tool's prompt text. raw is preserved verbatim regardless of
// whether it parses, so protocol validation downstream can still inspect
// what was actually sent.
func parseTaskMessage(raw string) domain.TaskMessage {
	if raw == "" {
		return domain.TaskMessage{Extraction: domain.ExtractionDegraded}
	}
	var tm domain.TaskMessage
	if err := json.Unmarshal([]byte(raw), &tm); err == nil && tm.AgentInstanceID != "" {
		tm.Raw = raw
		tm.Extraction = domain.ExtractionParsed
		return tm
	}
	return domain.TaskMessage{Raw: raw, Extraction: domain.ExtractionDegraded}
}

// extractOutput recovers a plain string from the after-hook's Output, whose
// runtime shape is inconsistent between two undocumented forms. It tries, in
// order: a {"output": "…"} object; a {"content":[{"type":"text","text":"…"}]}
// object, concatenating its text parts; a bare JSON string; and finally the
// raw bytes as text, so nothing is silently dropped.
func extractOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var outputForm struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &outputForm); err == nil && outputForm.Output != "" {
		return outputForm.Output
	}

	var contentForm struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &contentForm); err == nil && len(contentForm.Content) > 0 {
		var text string
		for _, part := range contentForm.Content {
			if part.Type == "text" {
				text += part.Text
			}
		}
		if text != "" {
			return text
		}
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return string(raw)
}

// TranslateOutcome implements domain.HarnessAdapter. call.Phase decides the
// reply's shape first: once the real call has already executed (PhasePost),
// the reply always passes through and never denies, regardless of what
// outcome the caller supplies.
func (a *Adapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	if call.Phase == domain.PhasePost {
		return NeutralReply(domain.PhasePost), nil
	}

	switch outcome.Kind {
	case domain.OutcomeRewritePrompt:
		return a.translateRewritePrompt(outcome, call)
	case domain.OutcomeHalt:
		return translateHalt(outcome)
	case domain.OutcomePassthrough:
		return NeutralReply(domain.PhasePre), nil
	case domain.OutcomeSubstitute:
		// This harness's pre-invocation point can rewrite arguments or block
		// a call but cannot fabricate a successful result. Emitting a reply
		// that merely looks like a substitution here would be exactly the
		// unfaithful stub the capability-honesty test exists to catch.
		return nil, ErrSubstitutionUnsupported
	default:
		return nil, fmt.Errorf("%w: outcome kind %q", ErrPayloadUnrecognised, outcome.Kind)
	}
}

// translateRewritePrompt builds the allow-with-updated-args reply. The
// original call's subagent type and description are recovered from the
// call's own raw payload rather than invented, so the harness's identity for
// the call is preserved across the rewrite.
func (a *Adapter) translateRewritePrompt(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	var payload ToolBeforePayload
	if err := json.Unmarshal(call.RawPayload, &payload); err != nil {
		return nil, fmt.Errorf("%w: recovering original args: %v", ErrPayloadMalformed, err)
	}
	orig, err := decodeTaskToolArgs(payload.Args)
	if err != nil {
		return nil, err
	}

	updated := TaskToolArgs{
		SubagentType: orig.SubagentType,
		Description:  orig.Description,
		Prompt:       outcome.RewrittenPrompt,
	}
	if outcome.CorrelationToken != "" {
		updated = PlantToken(updated, outcome.CorrelationToken)
	}

	updatedJSON, err := json.Marshal(updated)
	if err != nil {
		return nil, fmt.Errorf("opencode: marshaling updated args: %w", err)
	}

	reply := HookReply{
		Decision:    "allow",
		UpdatedArgs: updatedJSON,
	}
	return json.Marshal(reply)
}

// translateHalt builds the deny-with-reason reply.
func translateHalt(outcome domain.InterceptionOutcome) ([]byte, error) {
	reason := outcome.Message
	if reason == "" {
		reason = "the call was not matched by any registered stub"
	}
	reply := HookReply{
		Decision: "deny",
		Reason:   reason,
	}
	return json.Marshal(reply)
}
