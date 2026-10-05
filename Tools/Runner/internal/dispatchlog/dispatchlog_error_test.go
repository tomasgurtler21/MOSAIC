package dispatchlog

import (
	"os"
	"strings"
	"testing"
)

// ============================================================
// JSONL format — error entries
// ============================================================

func TestLogger_LogError_WritesValidJSON(t *testing.T) {
	// LogError must write a line that parses as valid JSON.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("no JSONL lines written after LogError")
	}
	unmarshalLine(t, lines[0])
}

func TestLogger_LogError_TypeFieldIsError(t *testing.T) {
	// The "type" field in an error entry must be "error".
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["type"]; got != "error" {
		t.Errorf("error entry type = %q, want \"error\"", got)
	}
}

func TestLogger_LogError_HasTimestamp(t *testing.T) {
	// The "timestamp" field must be present and non-empty in an error entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "something failed")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	ts, ok := m["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("error entry missing non-empty \"timestamp\" field; got %v", m["timestamp"])
	}
}

func TestLogger_LogError_AgentInstanceID_IsPreserved(t *testing.T) {
	// The agent_instance_id in the error entry must match the argument passed to LogError.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("specific-agent#7", "harness timed out")
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["agent_instance_id"]; got != "specific-agent#7" {
		t.Errorf("error entry agent_instance_id = %v, want \"specific-agent#7\"", got)
	}
}

func TestLogger_LogError_ErrorText_IsPreserved(t *testing.T) {
	// The "error" field in the error entry must match the errText argument verbatim.
	workDir := t.TempDir()
	logger := New(workDir)

	errText := "exit status 1: harness process terminated unexpectedly"
	logger.SetRunID(validRunID)
	logger.LogError("agent#1", errText)
	logger.Close()

	lines := readLogLines(t, logger)
	m := unmarshalLine(t, lines[0])
	if got := m["error"]; got != errText {
		t.Errorf("error entry error = %v, want %q", got, errText)
	}
}

func TestLogger_LogError_EntryIsOnSingleLine(t *testing.T) {
	// Each LogError entry must be a single JSON line (JSONL contract).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "some error text")
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogError must produce exactly 1 line, got %d lines:\n%s", len(rawLines), string(data))
	}
}

func TestLogger_LogError_EntryIsOnSingleLine_WithNewlinesInErrText(t *testing.T) {
	// When errText contains embedded newlines, json.Marshal must escape them so
	// the entry remains a single JSONL line. An implementation that used string
	// concatenation instead of proper JSON marshaling would produce multiple lines.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "line one\nline two\nline three")
	logger.Close()

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(rawLines) != 1 {
		t.Errorf("LogError with embedded newlines in errText must produce exactly 1 JSONL line, got %d lines:\n%s", len(rawLines), string(data))
	}
}
