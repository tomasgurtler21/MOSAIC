package domain_test

// Tests for ProtocolRequest and ProtocolResponse JSON serialization with run_id.
//
// Coverage:
//
//   ProtocolRequest — run_id serialization (T7.1):
//   - RunID field is marshalled to "run_id" JSON key when set.
//   - "run_id" key is absent from JSON output when RunID is empty (omitempty).
//   - JSON containing "run_id" is unmarshalled into ProtocolRequest.RunID.
//   - JSON without "run_id" key unmarshals successfully with RunID="" (no error).
//
//   ProtocolResponse — run_id serialization (T7.1 / T7.3):
//   - RunID field is marshalled to "run_id" JSON key when set.
//   - "run_id" key is absent from JSON output when RunID is empty (omitempty).
//   - JSON containing "run_id" is extracted into ProtocolResponse.RunID (T7.3).
//   - JSON without "run_id" key unmarshals successfully with RunID="" (T7.3 — no error).

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ---- ProtocolRequest: marshal ----

// TestProtocolRequest_WithRunID_MarshalIncludesRunIDKey verifies that when
// ProtocolRequest.RunID is non-empty, the marshalled JSON output contains the
// "run_id" key with the correct value.
func TestProtocolRequest_WithRunID_MarshalIncludesRunIDKey(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID: "test-agent#1",
		RunID:           "20260727T170000Z-a3f9",
		TaskDescription: "some task",
		InputArtifacts:  []string{"Orchestration/Plan.md"},
		OutputArtifacts: []string{"Orchestration/Progress.md"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, `"run_id"`) {
		t.Errorf("marshalled JSON does not contain \"run_id\" key: %s", got)
	}
	if !strings.Contains(got, `"20260727T170000Z-a3f9"`) {
		t.Errorf("marshalled JSON does not contain the run_id value: %s", got)
	}
}

// TestProtocolRequest_EmptyRunID_MarshalOmitsRunIDKey verifies that when
// ProtocolRequest.RunID is empty, the marshalled JSON output does NOT contain
// the "run_id" key. This is the omitempty behaviour that lets pre-v1.8 consumers
// send clean JSON without the field.
func TestProtocolRequest_EmptyRunID_MarshalOmitsRunIDKey(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID: "test-agent#1",
		RunID:           "", // empty — must be omitted
		TaskDescription: "some task",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	got := string(data)
	if strings.Contains(got, `"run_id"`) {
		t.Errorf("marshalled JSON must not contain \"run_id\" when RunID is empty, but got: %s", got)
	}
}

// ---- ProtocolRequest: unmarshal ----

// TestProtocolRequest_UnmarshalWithRunID_PopulatesRunIDField verifies that a
// JSON object containing "run_id" is correctly unmarshalled into
// ProtocolRequest.RunID.
func TestProtocolRequest_UnmarshalWithRunID_PopulatesRunIDField(t *testing.T) {
	const raw = `{
		"agent_instance_id": "test-agent#1",
		"run_id": "20260727T170000Z-a3f9",
		"task_description": "some task",
		"input_artifacts": [],
		"output_artifacts": [],
		"include_result_summary": false,
		"human_in_the_loop": false
	}`

	var req domain.ProtocolRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if req.RunID != "20260727T170000Z-a3f9" {
		t.Errorf("RunID: want %q, got %q", "20260727T170000Z-a3f9", req.RunID)
	}
}

// TestProtocolRequest_UnmarshalWithoutRunID_RunIDIsEmpty verifies that a JSON
// object without "run_id" unmarshals successfully and leaves ProtocolRequest.RunID
// as the empty string (no error, no panics). This handles pre-v1.8 payloads.
func TestProtocolRequest_UnmarshalWithoutRunID_RunIDIsEmpty(t *testing.T) {
	const raw = `{
		"agent_instance_id": "test-agent#1",
		"task_description": "some task",
		"input_artifacts": [],
		"output_artifacts": [],
		"include_result_summary": false,
		"human_in_the_loop": false
	}`

	var req domain.ProtocolRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if req.RunID != "" {
		t.Errorf("RunID: want empty string when absent in JSON, got %q", req.RunID)
	}
}

// ---- ProtocolRequest: round-trip ----

// TestProtocolRequest_RunID_RoundTrip verifies that a ProtocolRequest with
// RunID survives a marshal-then-unmarshal round trip with the value intact.
func TestProtocolRequest_RunID_RoundTrip(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	req := domain.ProtocolRequest{
		AgentInstanceID: "test-agent#1",
		RunID:           runID,
		TaskDescription: "round-trip task",
		InputArtifacts:  []string{"Orchestration/Plan.md"},
		OutputArtifacts: []string{"Orchestration/Progress.md"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	var got domain.ProtocolRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if got.RunID != runID {
		t.Errorf("RunID after round-trip: want %q, got %q", runID, got.RunID)
	}
}

// ---- ProtocolResponse: marshal (T7.1) ----

// TestProtocolResponse_WithRunID_MarshalIncludesRunIDKey verifies that when
// ProtocolResponse.RunID is non-empty, the marshalled JSON output contains the
// "run_id" key with the correct value.
func TestProtocolResponse_WithRunID_MarshalIncludesRunIDKey(t *testing.T) {
	resp := domain.ProtocolResponse{
		AgentInstanceID: "test-agent#1",
		RunID:           "20260727T170000Z-a3f9",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	got := string(data)
	if !strings.Contains(got, `"run_id"`) {
		t.Errorf("marshalled ProtocolResponse JSON does not contain \"run_id\" key: %s", got)
	}
	if !strings.Contains(got, `"20260727T170000Z-a3f9"`) {
		t.Errorf("marshalled ProtocolResponse JSON does not contain the run_id value: %s", got)
	}
}

// TestProtocolResponse_EmptyRunID_MarshalOmitsRunIDKey verifies that when
// ProtocolResponse.RunID is empty, the marshalled JSON output does NOT contain
// the "run_id" key (omitempty).
func TestProtocolResponse_EmptyRunID_MarshalOmitsRunIDKey(t *testing.T) {
	resp := domain.ProtocolResponse{
		AgentInstanceID: "test-agent#1",
		RunID:           "", // empty — must be omitted
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	got := string(data)
	if strings.Contains(got, `"run_id"`) {
		t.Errorf("marshalled ProtocolResponse JSON must not contain \"run_id\" when RunID is empty, but got: %s", got)
	}
}

// ---- ProtocolResponse: unmarshal (T7.3) ----

// TestProtocolResponse_UnmarshalWithRunID_ExtractsRunID verifies that run_id
// is extracted from a subagent response JSON into ProtocolResponse.RunID.
// This covers the T7.3 requirement: the runner parses run_id from responses.
func TestProtocolResponse_UnmarshalWithRunID_ExtractsRunID(t *testing.T) {
	const raw = `{
		"agent_instance_id": "test-agent#1",
		"run_id": "20260727T170000Z-a3f9",
		"status_code": "SUCCESS",
		"status_message": "task completed"
	}`

	var resp domain.ProtocolResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if resp.RunID != "20260727T170000Z-a3f9" {
		t.Errorf("RunID: want %q, got %q", "20260727T170000Z-a3f9", resp.RunID)
	}
}

// TestProtocolResponse_UnmarshalWithoutRunID_ParsesSuccessfully verifies that
// a response JSON without "run_id" parses successfully, with RunID as the empty
// string and no error. This covers the T7.3 requirement: a response without
// run_id must not cause a parsing failure (FR-36: no validation in this stage).
func TestProtocolResponse_UnmarshalWithoutRunID_ParsesSuccessfully(t *testing.T) {
	const raw = `{
		"agent_instance_id": "test-agent#1",
		"status_code": "SUCCESS",
		"status_message": "task completed"
	}`

	var resp domain.ProtocolResponse
	err := json.Unmarshal([]byte(raw), &resp)

	if err != nil {
		t.Fatalf("json.Unmarshal: want no error for response without run_id, got: %v", err)
	}
	if resp.RunID != "" {
		t.Errorf("RunID: want empty string when absent in JSON, got %q", resp.RunID)
	}
}

// TestProtocolResponse_UnmarshalWithoutRunID_OtherFieldsIntact verifies that
// unmarshalling a response without "run_id" still correctly populates all other
// fields, confirming the missing field does not corrupt the parse.
func TestProtocolResponse_UnmarshalWithoutRunID_OtherFieldsIntact(t *testing.T) {
	const raw = `{
		"agent_instance_id": "agent-x#3",
		"status_code": "BLOCKED",
		"status_message": "cannot proceed",
		"error_code": "E101",
		"error_reason": "file not found"
	}`

	var resp domain.ProtocolResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if resp.AgentInstanceID != "agent-x#3" {
		t.Errorf("AgentInstanceID: want %q, got %q", "agent-x#3", resp.AgentInstanceID)
	}
	if resp.StatusCode != domain.StatusBLOCKED {
		t.Errorf("StatusCode: want %q, got %q", domain.StatusBLOCKED, resp.StatusCode)
	}
	if resp.ErrorCode != domain.ErrorINPUT_NOT_FOUND {
		t.Errorf("ErrorCode: want %q, got %q", domain.ErrorINPUT_NOT_FOUND, resp.ErrorCode)
	}
}

// TestProtocolResponse_RunID_RoundTrip verifies that a ProtocolResponse with
// RunID set survives a marshal-then-unmarshal round trip with the value intact.
func TestProtocolResponse_RunID_RoundTrip(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	resp := domain.ProtocolResponse{
		AgentInstanceID: "test-agent#1",
		RunID:           runID,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	var got domain.ProtocolResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}

	if got.RunID != runID {
		t.Errorf("RunID after round-trip: want %q, got %q", runID, got.RunID)
	}
}

// TestProtocolRequest_RunID_PositionedAfterAgentInstanceID verifies that the
// "run_id" key appears in the JSON output between "agent_instance_id" and
// "task_description". This validates the field ordering matches the v1.8
// protocol specification (run_id placed immediately after agent_instance_id).
func TestProtocolRequest_RunID_PositionedAfterAgentInstanceID(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID: "test-agent#1",
		RunID:           "20260727T170000Z-a3f9",
		TaskDescription: "some task",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}

	got := string(data)
	agentIdx := strings.Index(got, `"agent_instance_id"`)
	runIDIdx := strings.Index(got, `"run_id"`)
	taskIdx := strings.Index(got, `"task_description"`)

	if agentIdx < 0 {
		t.Fatal("marshalled JSON missing \"agent_instance_id\"")
	}
	if runIDIdx < 0 {
		t.Fatal("marshalled JSON missing \"run_id\"")
	}
	if taskIdx < 0 {
		t.Fatal("marshalled JSON missing \"task_description\"")
	}

	if !(agentIdx < runIDIdx && runIDIdx < taskIdx) {
		t.Errorf("run_id must appear after agent_instance_id and before task_description in JSON output.\nJSON: %s", got)
	}
}
