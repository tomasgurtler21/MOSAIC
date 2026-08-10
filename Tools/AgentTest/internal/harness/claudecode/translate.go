package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"

	"mosaic-agent-test/internal/domain"
)

var (
	ErrPayloadUnrecognised  = errors.New("claudecode: unrecognised interception payload")
	ErrPayloadMalformed     = errors.New("claudecode: malformed interception payload")
	ErrIdentityUndetermined = errors.New("claudecode: collaborator identity not determinable from payload")
	// ErrSubstitutionUnsupported is returned by TranslateOutcome for
	// domain.OutcomeSubstitute, which this harness cannot achieve: its
	// interception point can rewrite input but cannot fabricate a
	// successful result. Emitting a plausible-looking wrong reply here is
	// the failure mode the capability-honesty test exists to prevent, so
	// the adapter fails loudly instead.
	ErrSubstitutionUnsupported = errors.New("claudecode: this harness cannot substitute a result directly")
	// ErrUnrecognisedDispatchToolName is returned when a native payload
	// reports a dispatch-tool name this adapter does not know how to
	// normalize. Passing an unrecognised name through verbatim is exactly
	// how a vendor's next rename would become another invisible defect, so
	// it is surfaced as a handleable error instead.
	ErrUnrecognisedDispatchToolName = errors.New("claudecode: unrecognised native dispatch-tool name")
)

// normalizeDispatchToolName maps this harness's native dispatch-tool name to
// the normalized, harness-neutral vocabulary authored tests use. An
// unrecognised native name is rejected rather than passed through silently.
func normalizeDispatchToolName(native string) (string, error) {
	if native == NativeDispatchToolName {
		return domain.DispatchToolName, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnrecognisedDispatchToolName, native)
}

// TranslateCall implements domain.HarnessAdapter. An unrecognised event, a
// payload missing the fields identity is derived from, and a payload that
// is not valid JSON at all are each returned as a handleable error — never
// a panic and never a zero-valued call mistaken for success.
func (a *Adapter) TranslateCall(phase domain.InterceptionPhase, native []byte) (domain.InterceptedCall, error) {
	switch phase {
	case domain.PhasePre:
		return a.translatePre(native)
	case domain.PhasePost:
		return a.translatePost(native)
	case domain.PhaseCompletion:
		return a.translateCompletion(native)
	default:
		return domain.InterceptedCall{}, fmt.Errorf("%w: interception phase %q", ErrPayloadUnrecognised, phase)
	}
}

func (a *Adapter) translatePre(native []byte) (domain.InterceptedCall, error) {
	var payload PreToolUsePayload
	if err := json.Unmarshal(native, &payload); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("%w: %v", ErrPayloadMalformed, err)
	}
	if payload.HookEventName != "PreToolUse" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: hook_event_name %q", ErrPayloadUnrecognised, payload.HookEventName)
	}
	if payload.ToolName == "" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: tool_name is empty", ErrIdentityUndetermined)
	}

	normalizedToolName, err := normalizeDispatchToolName(payload.ToolName)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	input, err := decodeTaskToolInput(payload.ToolInput)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, _ := RecoverToken(input)

	return domain.InterceptedCall{
		Phase:            domain.PhasePre,
		Identity:         domain.CollaboratorIdentity{ToolName: normalizedToolName, AgentIdentity: input.SubagentType},
		Message:          parseTaskMessage(input.Prompt),
		CorrelationToken: token,
		RawPayload:       json.RawMessage(native),
		Capabilities:     a.Capabilities(),
	}, nil
}

func (a *Adapter) translatePost(native []byte) (domain.InterceptedCall, error) {
	var payload PostToolUsePayload
	if err := json.Unmarshal(native, &payload); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("%w: %v", ErrPayloadMalformed, err)
	}
	if payload.HookEventName != "PostToolUse" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: hook_event_name %q", ErrPayloadUnrecognised, payload.HookEventName)
	}
	if payload.ToolName == "" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: tool_name is empty", ErrIdentityUndetermined)
	}

	normalizedToolName, err := normalizeDispatchToolName(payload.ToolName)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	input, err := decodeTaskToolInput(payload.ToolInput)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, _ := RecoverToken(input)

	return domain.InterceptedCall{
		Phase:            domain.PhasePost,
		Identity:         domain.CollaboratorIdentity{ToolName: normalizedToolName, AgentIdentity: input.SubagentType},
		Message:          parseTaskMessage(input.Prompt),
		CorrelationToken: token,
		RawPayload:       json.RawMessage(native),
		Capabilities:     a.Capabilities(),
		ObservedResponse: extractText(payload.ToolResponse),
	}, nil
}

// translateCompletion translates the harness's SubagentStop completion
// signal into a call carrying the recovered collaborator reply and, when
// recoverable, the correlation token planted at the pre-invocation point.
//
// An absent AgentTranscriptPath or a transcript carrying no planted token
// are both legitimate outcomes for an un-stubbed or partially-stubbed
// dispatch (AC12.7): CorrelationToken comes back empty, never an error.
// Only a malformed payload or a wrong hook_event_name is an error, mirroring
// translatePre/translatePost's own validation.
func (a *Adapter) translateCompletion(native []byte) (domain.InterceptedCall, error) {
	var payload CompletionPayload
	if err := json.Unmarshal(native, &payload); err != nil {
		return domain.InterceptedCall{}, fmt.Errorf("%w: %v", ErrPayloadMalformed, err)
	}
	if payload.HookEventName != "SubagentStop" {
		return domain.InterceptedCall{}, fmt.Errorf("%w: hook_event_name %q", ErrPayloadUnrecognised, payload.HookEventName)
	}

	return domain.InterceptedCall{
		Phase:            domain.PhaseCompletion,
		CorrelationToken: a.recoverCompletionToken(payload.AgentTranscriptPath),
		RawPayload:       json.RawMessage(native),
		Capabilities:     a.Capabilities(),
		ObservedResponse: payload.LastAssistantMessage,
	}, nil
}

// recoverCompletionToken reads the collaborator's own transcript file
// (through a.opts.TranscriptReader, or the real file reader when nil) and
// scans its entries for the correlation token planted at the pre-invocation
// point, using the same marker PlantToken/RecoverToken use. An empty
// transcriptPath, an unreadable transcript, or a transcript carrying no
// planted token all resolve to an empty token — none of them is an error
// here, since none of them is anything but a legitimate un-stubbed or
// partially-stubbed dispatch.
func (a *Adapter) recoverCompletionToken(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}

	reader := a.opts.TranscriptReader
	if reader == nil {
		reader = readTranscriptFile
	}

	entries, err := reader(transcriptPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if token, ok := RecoverToken(TaskToolInput{Prompt: entry}); ok {
			return token
		}
	}
	return ""
}

// decodeTaskToolInput decodes tool_input into TaskToolInput. An absent
// tool_input decodes to the zero value rather than an error: a hook event
// this adapter does not recognise is caught earlier, by hook_event_name.
func decodeTaskToolInput(raw json.RawMessage) (TaskToolInput, error) {
	var input TaskToolInput
	if len(raw) == 0 {
		return input, nil
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return TaskToolInput{}, fmt.Errorf("%w: tool_input: %v", ErrPayloadMalformed, err)
	}
	return input, nil
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

// extractText recovers a plain string from a native JSON value that may be
// either a JSON string (the common case) or some other JSON value, in which
// case its raw text is preserved rather than dropped.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// TranslateOutcome implements domain.HarnessAdapter. call.Phase decides the
// reply's shape first: once the real call has already executed
// (PhasePost), the reply always passes through and never denies, regardless
// of what outcome the caller supplies.
func (a *Adapter) TranslateOutcome(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	if call.Phase == domain.PhasePost || call.Phase == domain.PhaseCompletion {
		// The real call has already run by either of these phases, so the
		// reply always passes through and never denies, regardless of what
		// outcome the caller supplies (e.g. intercept.Decide misrouting the
		// call through its pre-invocation decision path).
		return NeutralReply(call.Phase), nil
	}

	switch outcome.Kind {
	case domain.OutcomeRewritePrompt:
		return a.translateRewritePrompt(outcome, call)
	case domain.OutcomeHalt:
		return translateHalt(outcome)
	case domain.OutcomePassthrough:
		return NeutralReply(domain.PhasePre), nil
	case domain.OutcomeSubstitute:
		// This harness's interception point can rewrite input but cannot
		// fabricate a successful result. Emitting a reply that merely looks
		// like a substitution here would be exactly the unfaithful stub the
		// capability-honesty test exists to catch.
		return nil, ErrSubstitutionUnsupported
	default:
		return nil, fmt.Errorf("%w: outcome kind %q", ErrPayloadUnrecognised, outcome.Kind)
	}
}

// translateRewritePrompt builds the allow-with-rewritten-input reply. The
// original call's subagent type and description are recovered from the
// call's own raw payload rather than invented, so the harness's identity
// for the call is preserved across the rewrite.
func (a *Adapter) translateRewritePrompt(outcome domain.InterceptionOutcome, call domain.InterceptedCall) ([]byte, error) {
	var payload PreToolUsePayload
	if err := json.Unmarshal(call.RawPayload, &payload); err != nil {
		return nil, fmt.Errorf("%w: recovering original tool input: %v", ErrPayloadMalformed, err)
	}
	orig, err := decodeTaskToolInput(payload.ToolInput)
	if err != nil {
		return nil, err
	}

	updated := TaskToolInput{
		SubagentType: orig.SubagentType,
		Description:  orig.Description,
		Prompt:       outcome.RewrittenPrompt,
	}
	if outcome.CorrelationToken != "" {
		updated = PlantToken(updated, outcome.CorrelationToken)
	}

	updatedJSON, err := json.Marshal(updated)
	if err != nil {
		return nil, fmt.Errorf("claudecode: marshaling updated tool input: %w", err)
	}

	reply := HookReply{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
			UpdatedInput:       updatedJSON,
		},
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
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
	}
	return json.Marshal(reply)
}
