// Package protocolcheck validates Communication Protocol messages —
// invocations and responses — against a targeted protocol version and
// counts violations by class.
//
// Pure — no I/O. Applicable both to collaborator messages and to the
// subject's own final message, since the subagent layer's validation rule
// depends on the latter.
package protocolcheck

import (
	"encoding/json"
	"errors"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// Version identifies the Communication Protocol version being validated
// against, so a protocol revision is a parameter rather than a rewrite.
type Version string

// validStatusCodes are the status codes the Communication Protocol defines.
// A status code outside this set is ViolationInventedStatusCode.
var validStatusCodes = map[string]bool{
	"SUCCESS":                true,
	"COMPLETED_NEEDS_ACTION": true,
	"PARTIALLY_DONE":         true,
	"NEEDS_CLARIFICATION":    true,
	"CAPABILITY_EXCEEDED":    true,
	"BLOCKED":                true,
}

// validErrorCodes are the error codes the Communication Protocol defines for
// BLOCKED responses. An error_code outside this set is
// ViolationInventedErrorCode.
var validErrorCodes = map[string]bool{
	"E101": true,
	"E401": true,
	"E501": true,
	"E502": true,
	"E503": true,
}

// requiredInvocationFields are the fields a task invocation must carry,
// non-empty, to avoid ViolationMissingRequiredField.
var requiredInvocationFields = []string{"agent_instance_id", "run_id", "task_description"}

// requiredResponseFields are the fields an agent response must carry,
// non-empty, to avoid ViolationMissingRequiredField.
var requiredResponseFields = []string{"agent_instance_id", "run_id", "status_code", "status_message"}

// CheckInvocation validates a task invocation message.
func CheckInvocation(raw string, v Version) Result {
	fields, bare, err := extractMessage(raw)
	if err != nil {
		return Result{Parsed: false, Violations: []Violation{{Class: ViolationUnparseable, Detail: err.Error()}}}
	}

	var violations []Violation
	if !bare {
		violations = append(violations, Violation{Class: ViolationNotBareMessage})
	}
	violations = append(violations, missingRequiredFieldViolations(fields, requiredInvocationFields)...)

	return Result{Parsed: true, Violations: violations}
}

// CheckResponse validates an agent response message.
//
// CheckResponse takes the originating invocation's context because one
// protocol rule — result_data may appear only when it was requested — is
// not decidable from the response text alone. A response cannot
// self-report whether it was asked for a summary.
func CheckResponse(raw string, v Version, req ResponseContext) Result {
	fields, bare, err := extractMessage(raw)
	if err != nil {
		return Result{Parsed: false, Violations: []Violation{{Class: ViolationUnparseable, Detail: err.Error()}}}
	}

	var violations []Violation
	var unevaluated []ViolationClass

	if !bare {
		violations = append(violations, Violation{Class: ViolationNotBareMessage})
	}
	violations = append(violations, missingRequiredFieldViolations(fields, requiredResponseFields)...)

	statusCode := stringField(fields, "status_code")
	if statusCode != "" && !validStatusCodes[statusCode] {
		violations = append(violations, Violation{Class: ViolationInventedStatusCode, Field: "status_code", Detail: statusCode})
	}

	isBlocked := statusCode == "BLOCKED"
	hasErrorCode := hasNonEmptyString(fields, "error_code")
	hasErrorReason := hasNonEmptyString(fields, "error_reason")

	if !isBlocked && (hasErrorCode || hasErrorReason) {
		violations = append(violations, Violation{Class: ViolationErrorFieldsWithoutBlocked})
	}
	if isBlocked && (!hasErrorCode || !hasErrorReason) {
		violations = append(violations, Violation{Class: ViolationMissingErrorFieldsOnBlocked})
	}
	if hasErrorCode {
		errorCode := stringField(fields, "error_code")
		if !validErrorCodes[errorCode] {
			violations = append(violations, Violation{Class: ViolationInventedErrorCode, Field: "error_code", Detail: errorCode})
		}
	}

	if _, present := fields["result_data"]; present {
		switch {
		case !req.RequestKnown:
			unevaluated = append(unevaluated, ViolationUnrequestedResultData)
		case !req.IncludeResultSummary:
			violations = append(violations, Violation{Class: ViolationUnrequestedResultData, Field: "result_data"})
		}
	}

	return Result{Parsed: true, Violations: violations, Unevaluated: unevaluated}
}

// ResponseContext carries the facts about the originating invocation that a
// response cannot state about itself.
type ResponseContext struct {
	// RequestKnown reports whether the originating invocation was recovered
	// at all. False means the request-dependent rules are not evaluated,
	// rather than evaluated against a false flag — an unknown request must
	// never manufacture a violation.
	RequestKnown bool
	// IncludeResultSummary is the flag the originating invocation carried.
	// Meaningful only when RequestKnown is true.
	IncludeResultSummary bool
}

// UnknownRequest is the context to pass when the originating invocation
// could not be recovered. Named rather than zero-valued so a caller's
// intent is visible at the call site.
var UnknownRequest = ResponseContext{}

// ResponseContextFor derives the context from a recovered invocation. An
// invocation whose Extraction is ExtractionDegraded or ExtractionFailed
// yields UnknownRequest: its fields were never read off the wire, so its
// IncludeResultSummary is an artefact of the zero value, not a fact.
func ResponseContextFor(inv domain.TaskMessage) ResponseContext {
	if inv.Extraction == domain.ExtractionDegraded || inv.Extraction == domain.ExtractionFailed {
		return UnknownRequest
	}
	return ResponseContext{RequestKnown: true, IncludeResultSummary: inv.IncludeResultSummary}
}

// Result is the outcome of validating one message.
type Result struct {
	Parsed     bool
	Violations []Violation
	// Unevaluated lists the classes this call could not decide, because the
	// context they need was absent. It is never nil-versus-empty ambiguous:
	// a class is either checked and counted, or named here. A rule that
	// silently declines to fire reads downstream as a clean run.
	Unevaluated []ViolationClass
}

// CountByClass tallies Violations by class. A class listed in Unevaluated
// is never counted here, since it was never decided.
func (r Result) CountByClass() map[ViolationClass]int {
	counts := map[ViolationClass]int{}
	for _, v := range r.Violations {
		counts[v.Class]++
	}
	return counts
}

// Violation is one detected protocol defect.
type Violation struct {
	Class  ViolationClass
	Field  string
	Detail string
}

// ViolationClass names one class of Communication Protocol violation.
type ViolationClass string

const (
	// A required field is missing or empty.
	ViolationMissingRequiredField ViolationClass = "missing_required_field"
	// status_code is not one of the defined codes.
	ViolationInventedStatusCode ViolationClass = "invented_status_code"
	// error_code / error_reason present on a non-BLOCKED response.
	ViolationErrorFieldsWithoutBlocked ViolationClass = "error_fields_without_blocked"
	// error_code / error_reason absent on a BLOCKED response.
	ViolationMissingErrorFieldsOnBlocked ViolationClass = "missing_error_fields_on_blocked"
	// error_code is not one of the defined codes.
	ViolationInventedErrorCode ViolationClass = "invented_error_code"
	// The message is wrapped in prose instead of being the whole reply.
	ViolationNotBareMessage ViolationClass = "not_bare_message"
	// result_data present although include_result_summary was not set.
	// Decidable only with a ResponseContext whose RequestKnown is true;
	// otherwise reported in Result.Unevaluated.
	ViolationUnrequestedResultData ViolationClass = "unrequested_result_data"
	// The message is not parseable as the protocol at all.
	ViolationUnparseable ViolationClass = "unparseable"
)

// extractMessage locates the first JSON object in raw and decodes it,
// reporting whether raw consists of exactly that JSON object (bare) or
// carries other text before or after it (wrapped in prose).
func extractMessage(raw string) (map[string]json.RawMessage, bool, error) {
	idx := strings.IndexByte(raw, '{')
	if idx < 0 {
		return nil, false, errors.New("protocolcheck: no JSON object found in message")
	}

	dec := json.NewDecoder(strings.NewReader(raw[idx:]))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil {
		return nil, false, err
	}

	consumed := int(dec.InputOffset())
	prefix := raw[:idx]
	suffix := raw[idx+consumed:]
	bare := strings.TrimSpace(prefix) == "" && strings.TrimSpace(suffix) == ""
	return fields, bare, nil
}

// missingRequiredFieldViolations returns one ViolationMissingRequiredField
// per name in required whose field is absent or empty.
func missingRequiredFieldViolations(fields map[string]json.RawMessage, required []string) []Violation {
	var violations []Violation
	for _, name := range required {
		if !hasNonEmptyString(fields, name) {
			violations = append(violations, Violation{Class: ViolationMissingRequiredField, Field: name})
		}
	}
	return violations
}

// hasNonEmptyString reports whether fields[key] decodes to a non-empty
// string.
func hasNonEmptyString(fields map[string]json.RawMessage, key string) bool {
	return stringField(fields, key) != ""
}

// stringField decodes fields[key] as a string, returning "" when the key is
// absent or not a JSON string.
func stringField(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
