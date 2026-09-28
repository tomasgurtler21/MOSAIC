package dispatchlog

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// JSONL format — response entries
// ============================================================

func TestLogger_LogResponse_WritesValidJSON(t *testing.T) {
	// LogResponse must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogResponse")
	}
	unmarshalLine(t, lines[0])
}

func TestLogger_LogResponse_TypeFieldIsResponse(t *testing.T) {
	// The "type" field in a response entry must be "response".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "response" {
		t.Errorf("response entry type = %q, want \"response\"", got)
	}
}

func TestLogger_LogResponse_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in a response entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("response entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogResponse_TopLevelAgentInstanceID(t *testing.T) {
	// The response entry must carry a top-level "agent_instance_id" field for
	// easy consumer correlation without parsing the nested response object.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.AgentInstanceID = "researcher#5"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["agent_instance_id"]; got != "researcher#5" {
		t.Errorf("response entry top-level agent_instance_id = %v, want \"researcher#5\"", got)
	}
}

func TestLogger_LogResponse_ContainsNestedResponseObject(t *testing.T) {
	// The response entry must contain a nested "response" object.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if _, ok := m["response"]; !ok {
		t.Errorf("response entry must have a nested \"response\" field; got keys: %v", mapKeys(m))
	}
}

func TestLogger_LogResponse_NestedStatusCode_IsPreserved(t *testing.T) {
	// The status_code in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusCode = domain.StatusBLOCKED
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["status_code"]; got != string(domain.StatusBLOCKED) {
		t.Errorf("response.status_code = %v, want %q", got, domain.StatusBLOCKED)
	}
}

func TestLogger_LogResponse_NestedStatusMessage_IsPreserved(t *testing.T) {
	// The status_message in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusMessage = "something happened with details"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if got := respObj["status_message"]; got != resp.StatusMessage {
		t.Errorf("response.status_message = %v, want %q", got, resp.StatusMessage)
	}
}

func TestLogger_LogResponse_LargeResultData_NotTruncated(t *testing.T) {
	// Large result_data must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("Result line " + strconv.Itoa(i) + ": some data that must not be truncated.\n")
	}
	largeResult := sb.String()

	resp := sampleResponse()
	resp.ResultData = largeResult
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "Result line 499") {
		t.Errorf("large result_data appears truncated; 'Result line 499' not found in log")
	}
}

func TestLogger_LogResponse_NestedRunID_IsPreserved(t *testing.T) {
	// The run_id in the nested response object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.RunID = validRunID
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["run_id"]; got != validRunID {
		t.Errorf("response.run_id = %v, want %q", got, validRunID)
	}
}

func TestLogger_LogResponse_NestedAgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the nested response object must be preserved verbatim,
	// independently of the top-level copy verified by TestLogger_LogResponse_TopLevelAgentInstanceID.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.AgentInstanceID = "nested-check-agent#3"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["agent_instance_id"]; got != "nested-check-agent#3" {
		t.Errorf("response (nested).agent_instance_id = %v, want \"nested-check-agent#3\"", got)
	}
}

func TestLogger_LogResponse_NestedErrorFields_WhenBlocked(t *testing.T) {
	// When the response has StatusCode BLOCKED with non-empty error_code and
	// error_reason, both fields must appear in the nested response object.
	// This is the only scenario in which the omitempty fields are serialised.
	workDir := t.TempDir()
	logger := New(workDir)

	resp := sampleResponse()
	resp.StatusCode = domain.StatusBLOCKED
	resp.ErrorCode = domain.ErrorTOOL_UNAVAILABLE
	resp.ErrorReason = "tool unavailable"
	logger.SetRunID(validRunID)
	logger.LogResponse(resp)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	respObj, _ := m["response"].(map[string]interface{})
	if respObj == nil {
		t.Fatal("response entry missing nested response object")
	}
	if got := respObj["error_code"]; got != string(domain.ErrorTOOL_UNAVAILABLE) {
		t.Errorf("response.error_code = %v, want %q", got, domain.ErrorTOOL_UNAVAILABLE)
	}
	if got := respObj["error_reason"]; got != "tool unavailable" {
		t.Errorf("response.error_reason = %v, want \"tool unavailable\"", got)
	}
}

func TestLogger_LogResponse_EntryIsOnSingleLine(t *testing.T) {
	// Each LogResponse entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogResponse must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}
