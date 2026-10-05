package dispatchlog

import (
	"testing"
)

// ============================================================
// SetToolVersion -- version entry
// ============================================================

func TestLogger_SetToolVersion_WritesVersionEntryAsFirstLine(t *testing.T) {
	// When SetToolVersion is called before the first log call, the very first
	// JSONL line must be a version entry with type "version" and the correct
	// tool_version value.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, ok := first["type"].(string); !ok || got != "version" {
		t.Errorf("first line type = %q, want %q; entry: %s", got, "version", lines[0])
	}
	if got, ok := first["tool_version"].(string); !ok || got != "1.0.0" {
		t.Errorf("first line tool_version = %q, want %q; entry: %s", got, "1.0.0", lines[0])
	}
}

func TestLogger_SetToolVersion_VersionEntryHasTimestamp(t *testing.T) {
	// The version entry must include a non-empty timestamp field in RFC 3339 format.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	// The first line must be the version entry; if it is not, the timestamp
	// check below is against the wrong entry and the test would be invalid.
	if typ, _ := first["type"].(string); typ != "version" {
		t.Fatalf("first line type = %q, want %q (cannot check timestamp on wrong entry type)", typ, "version")
	}
	ts, ok := first["timestamp"].(string)
	if !ok || ts == "" {
		t.Errorf("version entry must have a non-empty timestamp field; entry: %s", lines[0])
	}
}

func TestLogger_SetToolVersion_VersionEntryNotRepeated(t *testing.T) {
	// The version entry must appear exactly once regardless of how many log
	// calls follow.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "some error")
	logger.Close()

	lines := readLogLines(t, logger)
	count := 0
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("version entry must appear exactly once; found %d occurrences\nlines: %v", count, lines)
	}
}

func TestLogger_SetToolVersion_VersionEntryAbsentWhenNotCalled(t *testing.T) {
	// When SetToolVersion is never called, no version entry must appear in the log.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			t.Errorf("version entry must not appear when SetToolVersion was not called; line: %s", line)
		}
	}
}

func TestLogger_SetToolVersion_EmptyVersionNoEntry(t *testing.T) {
	// When SetToolVersion is called with an empty string, no version entry
	// must be written -- graceful degradation.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "version" {
			t.Errorf("version entry must not appear when SetToolVersion called with empty string; line: %s", line)
		}
	}
}

func TestLogger_SetToolVersion_VersionEntryBeforeLogRequest(t *testing.T) {
	// The version entry must appear as the first line, before any request entry.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (version + request); got %d: %v", len(lines), lines)
	}
	first := unmarshalLine(t, lines[0])
	if first["type"] != "version" {
		t.Errorf("first line type = %q, want %q", first["type"], "version")
	}
	second := unmarshalLine(t, lines[1])
	if second["type"] != "request" {
		t.Errorf("second line type = %q, want %q (first request should follow version entry)", second["type"], "request")
	}
}

func TestLogger_SetToolVersion_VersionEntryFirstWhenSetRunIDCalledAfterLog(t *testing.T) {
	// When SetRunID is called after the first log (which appends a correlation
	// entry), the version entry must still be the first line in the file.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.LogRequest(sampleRequest())
	logger.SetRunID(validRunID)
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, ok := first["type"].(string); !ok || got != "version" {
		t.Errorf("first line type = %q, want %q (version entry must be first even when SetRunID called post-log)", got, "version")
	}
}

func TestLogger_SetToolVersion_OnlyFirstNonEmptyValueTakesEffect(t *testing.T) {
	// If SetToolVersion is called multiple times, only the first non-empty value
	// must appear in the version entry.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetToolVersion("1.0.0")
	logger.SetToolVersion("9.9.9") // second call must be ignored
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	first := unmarshalLine(t, lines[0])
	if got, _ := first["tool_version"].(string); got != "1.0.0" {
		t.Errorf("tool_version = %q, want %q (second SetToolVersion call must be ignored)", got, "1.0.0")
	}
}
