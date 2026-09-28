// Package dispatchlog (in-package tests) covers the file-backed Logger and the
// domain.NopDispatchLogger. Tests are in-package so that unexported failure-injection
// seams are accessible when the implementation is added.
//
// Every test that touches the filesystem resolves its working directory from
// t.TempDir() so that no test can create RunnerLogs/ in the repository root.
package dispatchlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// Helpers
// ============================================================

// readLogLines reads the dispatch log file and returns its non-empty lines.
// The caller must have called logger.Close() before readLogLines so that any
// in-flight writes are complete.
func readLogLines(t *testing.T, logger *Logger) []string {
	t.Helper()
	p := logger.Path()
	if p == "" {
		t.Fatal("readLogLines: logger.Path() is empty; no log file was created")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("readLogLines: ReadFile(%q): %v", p, err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// blockDispatchLogs places a regular file where RunnerLogs/ would be created,
// causing os.MkdirAll to fail on both Windows and POSIX (including for the
// nested RunnerLogs/{run_id}/ subfolder, since MkdirAll cannot create a
// directory under a path component that is already a regular file). Returns
// the path of the blocker file.
func blockDispatchLogs(t *testing.T, workDir string) string {
	t.Helper()
	blocker := filepath.Join(workDir, LogsFolderName)
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatalf("blockDispatchLogs: WriteFile(%q): %v", blocker, err)
	}
	return blocker
}

// validRunID is a canonical run_id used across tests.
const validRunID = "20260824T162217Z-b7c1"

// sampleRequest builds a ProtocolRequest with all fields populated for tests
// that verify field completeness in JSONL output.
func sampleRequest() domain.ProtocolRequest {
	return domain.ProtocolRequest{
		AgentInstanceID:      "test-agent#1",
		RunID:                validRunID,
		TaskDescription:      "do something useful",
		InputArtifacts:       []string{"Orchestration-xyz/input.md"},
		OutputArtifacts:      []string{"Orchestration-xyz/output.md"},
		InputFiles:           []string{"src/main.go"},
		OutputFiles:          []string{"src/result.go"},
		Constraints:          "no side effects",
		IncludeResultSummary: true,
		HumanInTheLoop:       false,
	}
}

// sampleResponse builds a ProtocolResponse with all fields populated for tests
// that verify field completeness in JSONL output.
func sampleResponse() domain.ProtocolResponse {
	return domain.ProtocolResponse{
		AgentInstanceID: "test-agent#1",
		RunID:           validRunID,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "completed successfully",
		ResultData:      "some result data here",
		ErrorCode:       domain.ErrorNone,
		ErrorReason:     "",
	}
}

// unmarshalLine parses one JSONL line into a generic map for field inspection.
func unmarshalLine(t *testing.T, line string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("unmarshalLine: JSON parse failed for line %q: %v", line, err)
	}
	return m
}

// ============================================================
// Helpers (not tests)
// ============================================================

// mapKeys returns the keys of a map for use in error messages.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// checkStringSlice asserts that the given field in m is a JSON array containing
// the expected string element.
func checkStringSlice(t *testing.T, m map[string]interface{}, field, wantElem string) {
	t.Helper()
	raw, ok := m[field]
	if !ok {
		t.Errorf("field %q missing from entry; available: %v", field, mapKeys(m))
		return
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Errorf("field %q is not an array; got %T", field, raw)
		return
	}
	for _, elem := range arr {
		if s, ok := elem.(string); ok && s == wantElem {
			return
		}
	}
	t.Errorf("field %q does not contain %q; got %v", field, wantElem, arr)
}
