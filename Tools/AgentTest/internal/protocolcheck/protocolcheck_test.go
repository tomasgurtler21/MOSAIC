package protocolcheck_test

// Tests for CheckInvocation and CheckResponse: validating Communication
// Protocol messages and counting violations by class.
//
// Coverage follows T7.2: a well-formed invocation and response produce no
// violations; each violation class is detected and counted; and the same
// validation applies to a collaborator's message and to the subject's own
// final message (AC7.5), since both are plain strings to this package.
//
// ViolationUnrequestedResultData is decided from the ResponseContext
// CheckResponse is passed alongside the raw text, not from the text alone:
// a response cannot self-report whether it was asked for a summary. The
// three ResponseContext outcomes — requested, not requested, and unknown —
// are each covered below, along with ResponseContextFor deriving that
// context from a recovered domain.TaskMessage.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/protocolcheck"
)

const protocolV1 protocolcheck.Version = "1.10"

const wellFormedInvocation = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "task_description": "Research the topic and write findings.",
  "input_artifacts": ["Requirements.md"],
  "output_artifacts": ["Research.md"],
  "constraints": "",
  "include_result_summary": false,
  "human_in_the_loop": false
}`

const invocationMissingTaskDescription = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "input_artifacts": [],
  "output_artifacts": ["Research.md"],
  "include_result_summary": false,
  "human_in_the_loop": false
}`

const wellFormedResponseSuccess = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "SUCCESS",
  "status_message": "Research completed and Research.md was written."
}`

const wellFormedResponseBlocked = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "BLOCKED",
  "status_message": "Cannot proceed.",
  "error_code": "E101",
  "error_reason": "Requirements.md does not exist."
}`

const responseInventedStatusCode = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "DONE_ISH",
  "status_message": "Finished, probably."
}`

const responseErrorFieldsWithoutBlocked = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "SUCCESS",
  "status_message": "Done.",
  "error_code": "E101",
  "error_reason": "Should not be here on a SUCCESS response."
}`

const responseMissingErrorFieldsOnBlocked = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "BLOCKED",
  "status_message": "Cannot proceed."
}`

const responseInventedErrorCode = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "BLOCKED",
  "status_message": "Cannot proceed.",
  "error_code": "E999",
  "error_reason": "Unknown error code."
}`

const responseMissingRequiredField = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_message": "Done, but status_code is missing."
}`

const responseWrappedInProse = `Sure, here is my result:

{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "SUCCESS",
  "status_message": "Done."
}

Let me know if you need anything else.`

const responseUnparseable = `I finished the task successfully and everything looks great.`

const responseWithResultData = `{
  "agent_instance_id": "researcher#1",
  "run_id": "run-1",
  "status_code": "SUCCESS",
  "status_message": "Done.",
  "result_data": "3 findings recorded."
}`

// --- Well-formed messages produce no violations ---

func TestCheckInvocation_WellFormed_ProducesNoViolations(t *testing.T) {
	result := protocolcheck.CheckInvocation(wellFormedInvocation, protocolV1)

	if !result.Parsed {
		t.Fatal("Parsed = false, want true for a well-formed invocation")
	}
	if len(result.Violations) != 0 {
		t.Errorf("Violations = %+v, want none", result.Violations)
	}
}

func TestCheckResponse_WellFormedSuccess_ProducesNoViolations(t *testing.T) {
	result := protocolcheck.CheckResponse(wellFormedResponseSuccess, protocolV1, protocolcheck.UnknownRequest)

	if !result.Parsed {
		t.Fatal("Parsed = false, want true for a well-formed response")
	}
	if len(result.Violations) != 0 {
		t.Errorf("Violations = %+v, want none", result.Violations)
	}
}

func TestCheckResponse_WellFormedBlockedWithErrorFields_ProducesNoViolations(t *testing.T) {
	result := protocolcheck.CheckResponse(wellFormedResponseBlocked, protocolV1, protocolcheck.UnknownRequest)

	if !result.Parsed {
		t.Fatal("Parsed = false, want true for a well-formed BLOCKED response")
	}
	if len(result.Violations) != 0 {
		t.Errorf("Violations = %+v, want none", result.Violations)
	}
}

// --- Each violation class is detected and counted ---

func TestCheckInvocation_MissingTaskDescription_DetectsMissingRequiredField(t *testing.T) {
	result := protocolcheck.CheckInvocation(invocationMissingTaskDescription, protocolV1)

	if !hasClass(result, protocolcheck.ViolationMissingRequiredField) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationMissingRequiredField, result.Violations)
	}
}

func TestCheckResponse_MissingStatusCode_DetectsMissingRequiredField(t *testing.T) {
	result := protocolcheck.CheckResponse(responseMissingRequiredField, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationMissingRequiredField) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationMissingRequiredField, result.Violations)
	}
}

func TestCheckResponse_InventedStatusCode_IsDetected(t *testing.T) {
	result := protocolcheck.CheckResponse(responseInventedStatusCode, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationInventedStatusCode) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationInventedStatusCode, result.Violations)
	}
}

func TestCheckResponse_ErrorFieldsPresentWithoutBlockedStatus_IsDetected(t *testing.T) {
	result := protocolcheck.CheckResponse(responseErrorFieldsWithoutBlocked, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationErrorFieldsWithoutBlocked) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationErrorFieldsWithoutBlocked, result.Violations)
	}
}

func TestCheckResponse_ErrorFieldsAbsentOnBlockedStatus_IsDetected(t *testing.T) {
	result := protocolcheck.CheckResponse(responseMissingErrorFieldsOnBlocked, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationMissingErrorFieldsOnBlocked) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationMissingErrorFieldsOnBlocked, result.Violations)
	}
}

func TestCheckResponse_InventedErrorCode_IsDetected(t *testing.T) {
	result := protocolcheck.CheckResponse(responseInventedErrorCode, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationInventedErrorCode) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationInventedErrorCode, result.Violations)
	}
}

func TestCheckResponse_WrappedInProse_IsDetectedAsNotBareMessage(t *testing.T) {
	// A response wrapped in prose rather than being the message is a
	// distinct violation from being unparseable outright: the JSON is
	// present and valid, it is merely not the whole reply.
	result := protocolcheck.CheckResponse(responseWrappedInProse, protocolV1, protocolcheck.UnknownRequest)

	if !hasClass(result, protocolcheck.ViolationNotBareMessage) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationNotBareMessage, result.Violations)
	}
}

func TestCheckResponse_Unparseable_ReportsNotParsedWithUnparseableViolation(t *testing.T) {
	result := protocolcheck.CheckResponse(responseUnparseable, protocolV1, protocolcheck.UnknownRequest)

	if result.Parsed {
		t.Error("Parsed = true, want false for text with no recoverable protocol JSON")
	}
	if !hasClass(result, protocolcheck.ViolationUnparseable) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationUnparseable, result.Violations)
	}
}

// --- Counting ---

func TestResult_CountByClass_TalliesEachViolationOnce(t *testing.T) {
	result := protocolcheck.CheckResponse(responseErrorFieldsWithoutBlocked, protocolV1, protocolcheck.UnknownRequest)

	counts := result.CountByClass()

	if got := counts[protocolcheck.ViolationErrorFieldsWithoutBlocked]; got != 1 {
		t.Errorf("CountByClass()[%q] = %d, want 1", protocolcheck.ViolationErrorFieldsWithoutBlocked, got)
	}
}

func TestResult_CountByClass_OnNoViolations_ReturnsNoEntries(t *testing.T) {
	result := protocolcheck.CheckResponse(wellFormedResponseSuccess, protocolV1, protocolcheck.UnknownRequest)

	counts := result.CountByClass()

	if len(counts) != 0 {
		t.Errorf("CountByClass() = %+v, want an empty map for a clean response", counts)
	}
}

// --- AC7.5: applicable to the subject's own final message ---

// TestCheckResponse_AppliesEquallyToTheSubjectsOwnFinalMessage documents
// AC7.5: CheckResponse takes a plain string, a version and a
// ResponseContext, with no notion of "collaborator" versus "subject" — so
// the subagent layer's rule that the subject's own final message must
// itself be protocol-valid is implementable by calling this exact function
// against that message.
func TestCheckResponse_AppliesEquallyToTheSubjectsOwnFinalMessage(t *testing.T) {
	subjectsOwnFinalMessage := wellFormedResponseSuccess

	result := protocolcheck.CheckResponse(subjectsOwnFinalMessage, protocolV1, protocolcheck.UnknownRequest)

	if !result.Parsed {
		t.Fatal("Parsed = false, want true — the subject's own final message validates through the identical path as a collaborator's")
	}
	if len(result.Violations) != 0 {
		t.Errorf("Violations = %+v, want none", result.Violations)
	}
}

// --- ViolationUnrequestedResultData: the three ResponseContext outcomes ---

// TestCheckResponse_ResultDataRequested_ProducesNoViolation covers the
// "requested" outcome: RequestKnown true, IncludeResultSummary true —
// result_data is legitimate.
func TestCheckResponse_ResultDataRequested_ProducesNoViolation(t *testing.T) {
	requested := protocolcheck.ResponseContext{RequestKnown: true, IncludeResultSummary: true}

	result := protocolcheck.CheckResponse(responseWithResultData, protocolV1, requested)

	if hasClass(result, protocolcheck.ViolationUnrequestedResultData) {
		t.Errorf("expected no %q violation when the originating invocation set include_result_summary, got %+v", protocolcheck.ViolationUnrequestedResultData, result.Violations)
	}
	if len(result.Unevaluated) != 0 {
		t.Errorf("Unevaluated = %+v, want none — the class is decidable when RequestKnown is true", result.Unevaluated)
	}
}

// TestCheckResponse_ResultDataNotRequested_DetectsUnrequestedResultData
// covers the "not requested" outcome: RequestKnown true,
// IncludeResultSummary false — result_data should not have been sent.
func TestCheckResponse_ResultDataNotRequested_DetectsUnrequestedResultData(t *testing.T) {
	notRequested := protocolcheck.ResponseContext{RequestKnown: true, IncludeResultSummary: false}

	result := protocolcheck.CheckResponse(responseWithResultData, protocolV1, notRequested)

	if !hasClass(result, protocolcheck.ViolationUnrequestedResultData) {
		t.Errorf("expected %q among violations, got %+v", protocolcheck.ViolationUnrequestedResultData, result.Violations)
	}
	if len(result.Unevaluated) != 0 {
		t.Errorf("Unevaluated = %+v, want none — the class is decidable when RequestKnown is true", result.Unevaluated)
	}
}

// TestCheckResponse_UnknownRequest_ReportsResultDataClassAsUnevaluated
// covers the "unknown" outcome: RequestKnown false. The class must never be
// counted as a violation from an unknown request, and must never be
// silently dropped either — it appears in Unevaluated.
func TestCheckResponse_UnknownRequest_ReportsResultDataClassAsUnevaluated(t *testing.T) {
	result := protocolcheck.CheckResponse(responseWithResultData, protocolV1, protocolcheck.UnknownRequest)

	if hasClass(result, protocolcheck.ViolationUnrequestedResultData) {
		t.Errorf("expected no %q violation when the originating request is unknown, got %+v", protocolcheck.ViolationUnrequestedResultData, result.Violations)
	}
	unevaluated := false
	for _, class := range result.Unevaluated {
		if class == protocolcheck.ViolationUnrequestedResultData {
			unevaluated = true
		}
	}
	if !unevaluated {
		t.Errorf("Unevaluated = %+v, want it to contain %q — an unknown request must never silently decline to report the gap", result.Unevaluated, protocolcheck.ViolationUnrequestedResultData)
	}
}

// TestResult_CountByClass_NeverCountsAnUnevaluatedClass guards the
// invariant that a class named in Unevaluated can never also inflate
// CountByClass — an unknown request must not be able to fail a
// ClassProtocolViolations assertion.
func TestResult_CountByClass_NeverCountsAnUnevaluatedClass(t *testing.T) {
	result := protocolcheck.CheckResponse(responseWithResultData, protocolV1, protocolcheck.UnknownRequest)

	counts := result.CountByClass()

	if got := counts[protocolcheck.ViolationUnrequestedResultData]; got != 0 {
		t.Errorf("CountByClass()[%q] = %d, want 0 for an unevaluated class", protocolcheck.ViolationUnrequestedResultData, got)
	}
}

// --- ResponseContextFor: deriving context from a recovered invocation ---

func TestResponseContextFor_ParsedInvocation_YieldsKnownRequest(t *testing.T) {
	inv := domain.TaskMessage{
		IncludeResultSummary: true,
		Extraction:           domain.ExtractionParsed,
	}

	got := protocolcheck.ResponseContextFor(inv)

	if !got.RequestKnown {
		t.Fatal("RequestKnown = false, want true for a cleanly parsed invocation")
	}
	if !got.IncludeResultSummary {
		t.Error("IncludeResultSummary = false, want true — it must mirror the invocation's own flag")
	}
}

func TestResponseContextFor_DegradedExtraction_YieldsUnknownRequest(t *testing.T) {
	// A degraded extraction means the fields were never reliably read off the
	// wire, so IncludeResultSummary would be an artefact of the zero value,
	// not a fact.
	inv := domain.TaskMessage{
		IncludeResultSummary: true,
		Extraction:           domain.ExtractionDegraded,
	}

	got := protocolcheck.ResponseContextFor(inv)

	if got != protocolcheck.UnknownRequest {
		t.Errorf("ResponseContextFor(degraded) = %+v, want UnknownRequest", got)
	}
}

func TestResponseContextFor_FailedExtraction_YieldsUnknownRequest(t *testing.T) {
	inv := domain.TaskMessage{Extraction: domain.ExtractionFailed}

	got := protocolcheck.ResponseContextFor(inv)

	if got != protocolcheck.UnknownRequest {
		t.Errorf("ResponseContextFor(failed) = %+v, want UnknownRequest", got)
	}
}

func hasClass(result protocolcheck.Result, class protocolcheck.ViolationClass) bool {
	for _, v := range result.Violations {
		if v.Class == class {
			return true
		}
	}
	return false
}
