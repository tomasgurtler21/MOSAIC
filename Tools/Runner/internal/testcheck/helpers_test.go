// Package testcheck_test provides black-box tests for the testcheck package.
// Tests cover dispatch log parsing, expected-outcome sidecar loading, and the
// two-layer exit-code + dispatch-sequence checker.
//
// All filesystem access uses t.TempDir() so tests never create files in the
// repository root.
package testcheck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testcheck"
)

// ============================================================
// JSONL line helpers
//
// These helpers produce minimal JSONL lines that match the entry shapes
// written by internal/dispatchlog. Only the fields the parser needs are
// included; extra fields are ignored by the parser and omitted here for
// readability.
// ============================================================

// requestLine returns a JSONL request entry for the given agent and task.
func requestLine(agentID, taskDesc string) string {
	agentJSON, _ := json.Marshal(agentID)
	taskJSON, _ := json.Marshal(taskDesc)
	return `{"type":"request","timestamp":"2026-01-01T00:00:00Z","request":{"agent_instance_id":` +
		string(agentJSON) + `,"task_description":` + string(taskJSON) +
		`,"input_artifacts":null,"output_artifacts":null,"include_result_summary":false,"human_in_the_loop":false}}`
}

// responseLine returns a JSONL response entry for the given agent and status.
func responseLine(agentID, statusCode string) string {
	agentJSON, _ := json.Marshal(agentID)
	statusJSON, _ := json.Marshal(statusCode)
	return `{"type":"response","timestamp":"2026-01-01T00:00:00Z","agent_instance_id":` +
		string(agentJSON) + `,"response":{"agent_instance_id":` + string(agentJSON) +
		`,"status_code":` + string(statusJSON) + `,"status_message":"done"}}`
}

// errorLine returns a JSONL error entry for the given agent and error text.
func errorLine(agentID, errText string) string {
	agentJSON, _ := json.Marshal(agentID)
	errJSON, _ := json.Marshal(errText)
	return `{"type":"error","timestamp":"2026-01-01T00:00:00Z","agent_instance_id":` +
		string(agentJSON) + `,"error":` + string(errJSON) + `}`
}

// versionLine returns a JSONL version entry.
func versionLine() string {
	return `{"type":"version","timestamp":"2026-01-01T00:00:00Z","tool_version":"1.0.0"}`
}

// correlationLine returns a JSONL correlation entry.
func correlationLine(runID string) string {
	idJSON, _ := json.Marshal(runID)
	return `{"type":"correlation","timestamp":"2026-01-01T00:00:00Z","run_id":` + string(idJSON) + `}`
}

// ============================================================
// File writing helpers
// ============================================================

// writeDispatchLog writes the given JSONL lines (one per element) to a file
// in the temp directory and returns the file path.
func writeDispatchLog(t *testing.T, dir string, lines []string) string {
	t.Helper()
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	path := filepath.Join(dir, "dispatch.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeDispatchLog: WriteFile: %v", err)
	}
	return path
}

// writeSidecar serializes the given ExpectedOutcome to JSON and writes it to a
// file in the temp directory, returning the file path.
func writeSidecar(t *testing.T, dir string, outcome testcheck.ExpectedOutcome) string {
	t.Helper()
	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("writeSidecar: json.Marshal: %v", err)
	}
	path := filepath.Join(dir, "outcome.expected.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writeSidecar: WriteFile: %v", err)
	}
	return path
}

// emptyDispatchLog writes an empty dispatch log file and returns its path.
func emptyDispatchLog(t *testing.T, dir string) string {
	t.Helper()
	return writeDispatchLog(t, dir, nil)
}
