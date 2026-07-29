package harness_test

// Tests for the harness package.
//
// Coverage (T8.1 / AC8.1):
//
//   FakeAdapter — scripted response delivery:
//   - Returns the scripted ProtocolResponse for a queued success entry.
//   - Returns the scripted error for a queued error entry.
//   - Returns an error when the queue for a given agent is exhausted.
//   - Consumes entries in FIFO order (first added, first returned).
//   - Multiple agents share the same adapter; each has its own FIFO queue.
//   - Records all invocations in call order (agent and request preserved).
//
//   FakeAdapter — malformed response:
//   - RawJSON with invalid JSON returns an error (not a panic).
//   - RawJSON with valid JSON returns the parsed ProtocolResponse.
//
//   FakeAdapter — context cancellation:
//   - A context cancelled before Invoke is called returns ctx.Err().
//   - A valid scripted entry is NOT consumed when the context is cancelled.
//
//   Protocol serialisation:
//   - MarshalRequest produces JSON with the protocol-specified field names
//     (snake_case: agent_instance_id, task_description, input_artifacts,
//     output_artifacts, include_result_summary, human_in_the_loop).
//   - MarshalRequest omits optional fields when they hold zero values
//     (input_files, output_files, constraints use omitempty).
//   - MarshalRequest includes optional fields when they are non-zero.
//   - UnmarshalResponse reconstructs a ProtocolResponse from valid JSON.
//   - UnmarshalResponse returns an error on malformed JSON.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ---- helpers ----

func agentRef(id string) domain.AgentReference {
	return domain.AgentReference{
		Identifier:     id,
		DefinitionPath: "/agents/" + id + ".md",
	}
}

func minimalRequest(instanceID string) domain.ProtocolRequest {
	return domain.ProtocolRequest{
		AgentInstanceID: instanceID,
		TaskDescription: "do the thing",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
	}
}

func successEntry(agentID string) harness.ScriptedEntry {
	return harness.ScriptedEntry{
		Response: &domain.ProtocolResponse{
			AgentInstanceID: agentID + "#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "completed",
		},
	}
}

// ===== FakeAdapter: scripted response delivery =====

// TestFakeAdapter_ReturnsScriptedResponse verifies that Invoke returns the
// scripted ProtocolResponse for a queued success entry.
func TestFakeAdapter_ReturnsScriptedResponse(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", successEntry("agent-a"))

	resp, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
}

// TestFakeAdapter_ReturnsScriptedError verifies that Invoke returns the
// scripted error for a queued error entry.
func TestFakeAdapter_ReturnsScriptedError(t *testing.T) {
	f := harness.NewFakeAdapter()
	sentinel := errors.New("simulated harness crash")
	f.Queue("agent-a", harness.ScriptedEntry{Err: sentinel})

	_, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))

	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

// TestFakeAdapter_ExhaustedQueue_ReturnsError verifies that Invoke returns an
// error when the queue for an agent has been exhausted, rather than blocking
// or panicking.
func TestFakeAdapter_ExhaustedQueue_ReturnsError(t *testing.T) {
	f := harness.NewFakeAdapter()
	// No entries queued for "agent-a".

	_, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))

	if err == nil {
		t.Fatal("want error from exhausted queue, got nil")
	}
}

// TestFakeAdapter_FIFOOrder verifies that entries are consumed in the order
// they were added (first-in, first-out).
func TestFakeAdapter_FIFOOrder(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a",
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{StatusCode: domain.StatusSUCCESS, StatusMessage: "first"}},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{StatusCode: domain.StatusBLOCKED, StatusMessage: "second"}},
	)

	first, _ := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))
	second, _ := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#2"))

	if first.StatusMessage != "first" {
		t.Errorf("want first entry returned first, got %q", first.StatusMessage)
	}
	if second.StatusMessage != "second" {
		t.Errorf("want second entry returned second, got %q", second.StatusMessage)
	}
}

// TestFakeAdapter_MultipleAgents_IndependentQueues verifies that different
// agents each have their own independent FIFO queue.
func TestFakeAdapter_MultipleAgents_IndependentQueues(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{StatusMessage: "a-response"}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{StatusMessage: "b-response"}})

	respA, _ := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))
	respB, _ := f.Invoke(context.Background(), agentRef("agent-b"), minimalRequest("agent-b#1"))

	if respA.StatusMessage != "a-response" {
		t.Errorf("want a-response, got %q", respA.StatusMessage)
	}
	if respB.StatusMessage != "b-response" {
		t.Errorf("want b-response, got %q", respB.StatusMessage)
	}
}

// TestFakeAdapter_RecordsInvocationsInOrder verifies that all calls are
// recorded in call order with the agent and request preserved.
func TestFakeAdapter_RecordsInvocationsInOrder(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", successEntry("agent-a"))
	f.Queue("agent-b", successEntry("agent-b"))

	reqA := minimalRequest("agent-a#1")
	reqB := minimalRequest("agent-b#2")

	f.Invoke(context.Background(), agentRef("agent-a"), reqA) //nolint:errcheck
	f.Invoke(context.Background(), agentRef("agent-b"), reqB) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want 2 invocations recorded, got %d", len(invs))
	}
	if invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation for agent-a, got %q", invs[0].Agent.Identifier)
	}
	if invs[1].Agent.Identifier != "agent-b" {
		t.Errorf("want second invocation for agent-b, got %q", invs[1].Agent.Identifier)
	}
	if invs[0].Request.AgentInstanceID != "agent-a#1" {
		t.Errorf("want first request agent_instance_id=agent-a#1, got %q", invs[0].Request.AgentInstanceID)
	}
}

// ===== FakeAdapter: malformed response =====

// TestFakeAdapter_MalformedRawJSON_ReturnsError verifies that a RawJSON entry
// with invalid JSON returns an error from Invoke rather than causing a panic.
// This exercises the "surface malformed responses as handleable deviations"
// requirement (AC8.1).
func TestFakeAdapter_MalformedRawJSON_ReturnsError(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{RawJSON: []byte(`{not valid json`)})

	_, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))

	if err == nil {
		t.Fatal("want error from malformed JSON, got nil")
	}
	// The error should mention the agent so the caller can correlate it.
	if !contains(err.Error(), "agent-a") {
		t.Errorf("want error message to mention agent-a, got %q", err.Error())
	}
}

// TestFakeAdapter_ValidRawJSON_ReturnsParsedResponse verifies that a RawJSON
// entry with valid JSON returns the parsed ProtocolResponse.
func TestFakeAdapter_ValidRawJSON_ReturnsParsedResponse(t *testing.T) {
	f := harness.NewFakeAdapter()
	raw, _ := json.Marshal(domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "from raw json",
	})
	f.Queue("agent-a", harness.ScriptedEntry{RawJSON: raw})

	resp, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.StatusMessage != "from raw json" {
		t.Errorf("want StatusMessage=from raw json, got %q", resp.StatusMessage)
	}
}

// ===== FakeAdapter: context cancellation =====

// TestFakeAdapter_CancelledContext_ReturnsCancellationError verifies that
// Invoke returns ctx.Err() when the context is already cancelled on entry.
func TestFakeAdapter_CancelledContext_ReturnsCancellationError(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", successEntry("agent-a"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invoking

	_, err := f.Invoke(ctx, agentRef("agent-a"), minimalRequest("agent-a#1"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// TestFakeAdapter_CancelledContext_DoesNotConsumeEntry verifies that a
// cancelled context does not consume a scripted entry from the queue. The
// entry should remain available for the next (non-cancelled) call.
func TestFakeAdapter_CancelledContext_DoesNotConsumeEntry(t *testing.T) {
	f := harness.NewFakeAdapter()
	f.Queue("agent-a", successEntry("agent-a"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Invoke(ctx, agentRef("agent-a"), minimalRequest("agent-a#1")) //nolint:errcheck

	// The entry should still be in the queue; a fresh context should return it.
	resp, err := f.Invoke(context.Background(), agentRef("agent-a"), minimalRequest("agent-a#1"))
	if err != nil {
		t.Fatalf("want no error on second call with fresh context, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want SUCCESS on second call, got %q", resp.StatusCode)
	}
}

// ===== Protocol serialisation =====

// TestMarshalRequest_ProducesExpectedFieldNames verifies that MarshalRequest
// produces JSON with the Communication Protocol v1.7 snake_case field names.
func TestMarshalRequest_ProducesExpectedFieldNames(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID:      "writer#1",
		TaskDescription:      "write tests",
		InputArtifacts:       []string{"Plan.md"},
		OutputArtifacts:      []string{"PlanProgress.md"},
		IncludeResultSummary: true,
		HumanInTheLoop:       false,
	}

	data, err := harness.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal of output: %v", err)
	}

	expectedKeys := []string{
		"agent_instance_id",
		"task_description",
		"input_artifacts",
		"output_artifacts",
		"include_result_summary",
		"human_in_the_loop",
	}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("want key %q in marshalled JSON, not found; keys: %v", key, mapKeys(m))
		}
	}
}

// TestMarshalRequest_OmitsEmptyOptionalFields verifies that optional fields
// (input_files, output_files, constraints) are absent from the JSON when they
// hold zero values (omitempty).
func TestMarshalRequest_OmitsEmptyOptionalFields(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID: "writer#1",
		TaskDescription: "task",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
		// InputFiles, OutputFiles, Constraints intentionally left zero.
	}

	data, err := harness.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}

	var m map[string]any
	json.Unmarshal(data, &m) //nolint:errcheck

	for _, absent := range []string{"input_files", "output_files", "constraints"} {
		if _, ok := m[absent]; ok {
			t.Errorf("want %q absent from JSON when zero, but it is present", absent)
		}
	}
}

// TestMarshalRequest_IncludesOptionalFieldsWhenSet verifies that optional
// fields appear in the JSON when they have non-zero values.
func TestMarshalRequest_IncludesOptionalFieldsWhenSet(t *testing.T) {
	req := domain.ProtocolRequest{
		AgentInstanceID: "writer#1",
		TaskDescription: "task",
		InputArtifacts:  []string{},
		OutputArtifacts: []string{},
		InputFiles:      []string{"src/main.go"},
		Constraints:     "must finish in one pass",
	}

	data, err := harness.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}

	var m map[string]any
	json.Unmarshal(data, &m) //nolint:errcheck

	if _, ok := m["input_files"]; !ok {
		t.Error("want input_files in JSON when set, not found")
	}
	if _, ok := m["constraints"]; !ok {
		t.Error("want constraints in JSON when set, not found")
	}
}

// TestUnmarshalResponse_ParsesValidJSON verifies that UnmarshalResponse
// reconstructs a ProtocolResponse with all fields from valid JSON.
func TestUnmarshalResponse_ParsesValidJSON(t *testing.T) {
	raw := `{
		"agent_instance_id": "reviewer#3",
		"status_code": "COMPLETED_NEEDS_ACTION",
		"status_message": "found issues",
		"error_code": "",
		"error_reason": ""
	}`

	resp, err := harness.UnmarshalResponse([]byte(raw))

	if err != nil {
		t.Fatalf("UnmarshalResponse: %v", err)
	}
	if resp.AgentInstanceID != "reviewer#3" {
		t.Errorf("want AgentInstanceID=reviewer#3, got %q", resp.AgentInstanceID)
	}
	if resp.StatusCode != domain.StatusCOMPLETED_NEEDS_ACTION {
		t.Errorf("want StatusCode=COMPLETED_NEEDS_ACTION, got %q", resp.StatusCode)
	}
	if resp.StatusMessage != "found issues" {
		t.Errorf("want StatusMessage=found issues, got %q", resp.StatusMessage)
	}
}

// TestUnmarshalResponse_ReturnsErrorOnInvalidJSON verifies that
// UnmarshalResponse returns a non-nil error for malformed JSON.
func TestUnmarshalResponse_ReturnsErrorOnInvalidJSON(t *testing.T) {
	_, err := harness.UnmarshalResponse([]byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

// ---- small utilities ----

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsLoop(s, sub))
}

func containsLoop(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
