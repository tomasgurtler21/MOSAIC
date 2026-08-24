package opencode

// TranslateCall and TranslateOutcome. See ContractsDesign.md's "AgentTest:
// Translation" section for the full contract.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

	// ErrUnrecognisedDispatchToolName is returned when a native payload
	// reports a dispatch-tool name this adapter does not know how to
	// normalize. Passing an unrecognised name through verbatim is exactly
	// how a vendor's next rename would become another invisible defect, so
	// it is surfaced as a handleable error instead.
	ErrUnrecognisedDispatchToolName = errors.New("opencode: unrecognised native dispatch-tool name")
)

// normalizeDispatchToolName maps this harness's native dispatch-tool name to
// the normalized, harness-neutral vocabulary authored tests use. An
// unrecognised native name is rejected rather than passed through silently.
func normalizeDispatchToolName(native string) (string, error) {
	if native == InterceptedToolName {
		return domain.DispatchToolName, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnrecognisedDispatchToolName, native)
}

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

	normalizedToolName, err := normalizeDispatchToolName(payload.Tool)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	args, err := decodeTaskToolArgs(payload.Args)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, ok := RecoverToken(args)
	if !ok {
		// First-time dispatch: no token in the args, so mint one. The seam
		// (opts.NewToken) allows tests to supply a deterministic token; nil
		// selects the package-level generator, whose opacity is a tested
		// property.
		mint := a.opts.NewToken
		if mint == nil {
			mint = NewToken
		}
		token = mint()
	}

	return domain.InterceptedCall{
		Phase:            domain.PhasePre,
		Identity:         domain.CollaboratorIdentity{ToolName: normalizedToolName, AgentIdentity: args.SubagentType},
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

	normalizedToolName, err := normalizeDispatchToolName(payload.Tool)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	args, err := decodeTaskToolArgs(payload.Args)
	if err != nil {
		return domain.InterceptedCall{}, err
	}

	token, _ := RecoverToken(args)

	return domain.InterceptedCall{
		Phase:            domain.PhasePost,
		Identity:         domain.CollaboratorIdentity{ToolName: normalizedToolName, AgentIdentity: args.SubagentType},
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
//
// When the entire raw string is not valid JSON, the function scans for
// embedded JSON objects by locating each '{' and walking forward to its
// matching '}' (tracking nested braces). Each candidate is attempted in
// order; the first one that unmarshals successfully and carries a non-empty
// agent_instance_id is returned as ExtractionRecovered. If no candidate
// satisfies the requirement, ExtractionDegraded is returned.
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
	// Fallback: scan for embedded JSON objects within prose.
	if recovered, ok := extractJSONObject(raw); ok {
		recovered.Raw = raw
		recovered.Extraction = domain.ExtractionRecovered
		return recovered
	}
	return domain.TaskMessage{Raw: raw, Extraction: domain.ExtractionDegraded}
}

// extractJSONObject scans s for all top-level '{...}' substrings (handling
// nested braces) and returns the first domain.TaskMessage that unmarshals
// successfully and carries a non-empty AgentInstanceID. The second return
// value is false when no such object is found.
func extractJSONObject(s string) (domain.TaskMessage, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		// Walk forward tracking brace depth to find the matching '}'.
		depth := 0
		end := -1
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			// No closing brace found from this '{'; no further candidates possible.
			break
		}
		candidate := s[i : end+1]
		var tm domain.TaskMessage
		if err := json.Unmarshal([]byte(candidate), &tm); err == nil && tm.AgentInstanceID != "" {
			return tm, true
		}
		// Advance past this object to try the next one.
		i = end
	}
	return domain.TaskMessage{}, false
}

// extractOutput recovers a plain string from the after-hook's Output field,
// whose runtime shape varies across at least three documented forms. Text is
// first extracted from whichever container shape is detected, then a single
// shared XML-stripping pass is applied: if the extracted text contains a
// <task_result>…</task_result> span, only the trimmed inner content is
// returned. This ensures the XML envelope is stripped regardless of which
// container shape delivered it.
//
// Container shapes tried in order:
//
//  1. Bare XML text: raw bytes starting with '<' are used directly as text
//  2. Bare JSON string (possibly wrapping an XML envelope)
//  3. {"output": "…"} object -> .output string
//  4. {"content":[{"type":"text","text":"…"}]} -> concatenated text parts
//  5. Fallback: raw bytes as string
func extractOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return stripXMLEnvelope(extractOutputText(raw))
}

// extractOutputText extracts plain text from a raw output value without
// applying XML envelope stripping. It tries container shapes in order and
// returns the first successful extraction.
func extractOutputText(raw json.RawMessage) string {
	// Bare XML / non-JSON text: raw bytes starting with '<'.
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("<")) {
		return string(raw)
	}

	// Bare JSON string (possibly an XML envelope or plain text).
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// {"output": "…"} object.
	var outputForm struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &outputForm); err == nil && outputForm.Output != "" {
		return outputForm.Output
	}

	// {"content":[{"type":"text","text":"…"}]} content array.
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

	return string(raw)
}

// stripXMLEnvelope extracts the content between the first <task_result> and
// the last </task_result> tag, trimming surrounding whitespace. When no
// matching tags are found, s is returned unchanged. Using first open and last
// close handles nested occurrences defensively, yielding the outermost span.
func stripXMLEnvelope(s string) string {
	const openTag = "<task_result>"
	const closeTag = "</task_result>"
	openIdx := strings.Index(s, openTag)
	closeIdx := strings.LastIndex(s, closeTag)
	if openIdx >= 0 && closeIdx > openIdx {
		return strings.TrimSpace(s[openIdx+len(openTag) : closeIdx])
	}
	return s
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
