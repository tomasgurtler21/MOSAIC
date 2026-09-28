package dispatchlog

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// ============================================================
// JSONL format — request entries
// ============================================================

func TestLogger_LogRequest_WritesValidJSON(t *testing.T) {
	// LogRequest must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogRequest")
	}
	unmarshalLine(t, lines[0]) // fails the test if JSON is invalid
}

func TestLogger_LogRequest_TypeFieldIsRequest(t *testing.T) {
	// The "type" field in a request entry must be "request".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "request" {
		t.Errorf("request entry type = %q, want \"request\"", got)
	}
}

func TestLogger_LogRequest_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in a request entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("request entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogRequest_TimestampIsUTC(t *testing.T) {
	// The "timestamp" field must end with 'Z' (UTC).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, _ := m["timestamp"].(string)
	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("request entry timestamp %q must end with 'Z'", ts)
	}
}

func TestLogger_LogRequest_ContainsNestedRequestObject(t *testing.T) {
	// The request entry must contain a nested "request" object.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if _, ok := m["request"]; !ok {
		t.Errorf("request entry must have a nested \"request\" field; got keys: %v", mapKeys(m))
	}
}

func TestLogger_LogRequest_AgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the request payload must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.AgentInstanceID = "specific-agent#42"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["agent_instance_id"]; got != "specific-agent#42" {
		t.Errorf("request.agent_instance_id = %v, want \"specific-agent#42\"", got)
	}
}

func TestLogger_LogRequest_TaskDescription_IsPreserved(t *testing.T) {
	// The task_description field must be preserved verbatim (no truncation).
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.TaskDescription = "do something very specific and important"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if got := reqObj["task_description"]; got != req.TaskDescription {
		t.Errorf("request.task_description = %q, want %q", got, req.TaskDescription)
	}
}

func TestLogger_LogRequest_RunID_IsPreserved(t *testing.T) {
	// The run_id in the nested request object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.RunID = validRunID
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["run_id"]; got != validRunID {
		t.Errorf("request.run_id = %v, want %q", got, validRunID)
	}
}

func TestLogger_LogRequest_Constraints_IsPreserved(t *testing.T) {
	// The constraints field in the nested request object must be preserved verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.Constraints = "no side effects"
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})
	if reqObj == nil {
		t.Fatal("request entry missing nested request object")
	}
	if got := reqObj["constraints"]; got != "no side effects" {
		t.Errorf("request.constraints = %v, want \"no side effects\"", got)
	}
}

func TestLogger_LogRequest_LargeTaskDescription_NotTruncated(t *testing.T) {
	// Large task descriptions must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("This is line " + strconv.Itoa(i) + " of a very long task description.\n")
	}
	largeDesc := sb.String()

	req := sampleRequest()
	req.TaskDescription = largeDesc
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	// Read the raw file to confirm the large payload is present.
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "line 499") {
		t.Errorf("large task description appears truncated; 'line 499' not found in log")
	}
}

func TestLogger_LogRequest_AllSliceFields_Serialised(t *testing.T) {
	// InputArtifacts, OutputArtifacts, InputFiles, OutputFiles must all appear in the entry.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.InputArtifacts = []string{"Orchestration-xyz/in.md"}
	req.OutputArtifacts = []string{"Orchestration-xyz/out.md"}
	req.InputFiles = []string{"src/a.go"}
	req.OutputFiles = []string{"src/b.go"}
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})

	checkStringSlice(t, reqObj, "input_artifacts", "Orchestration-xyz/in.md")
	checkStringSlice(t, reqObj, "output_artifacts", "Orchestration-xyz/out.md")
	checkStringSlice(t, reqObj, "input_files", "src/a.go")
	checkStringSlice(t, reqObj, "output_files", "src/b.go")
}

func TestLogger_LogRequest_BoolFields_Serialised(t *testing.T) {
	// include_result_summary and human_in_the_loop must appear in the request entry.
	workDir := t.TempDir()
	logger := New(workDir)

	req := sampleRequest()
	req.IncludeResultSummary = true
	req.HumanInTheLoop = true
	logger.SetRunID(validRunID)
	logger.LogRequest(req)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	reqObj, _ := m["request"].(map[string]interface{})

	if got := reqObj["include_result_summary"]; got != true {
		t.Errorf("request.include_result_summary = %v, want true", got)
	}
	if got := reqObj["human_in_the_loop"]; got != true {
		t.Errorf("request.human_in_the_loop = %v, want true", got)
	}
}

func TestLogger_LogRequest_EntryIsOnSingleLine(t *testing.T) {
	// Each LogRequest entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The file must end with exactly one newline after the JSON object.
	// No internal newlines must be present within the JSON entry.
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogRequest must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}
