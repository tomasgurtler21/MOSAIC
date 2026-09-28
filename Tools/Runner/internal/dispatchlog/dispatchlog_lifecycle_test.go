package dispatchlog

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// ============================================================
// Multiple entries and ordering
// ============================================================

func TestLogger_MultipleEntries_AllPresentInFile(t *testing.T) {
	// Sequential log calls must all produce entries in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "late harness error")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) != 3 {
		t.Errorf("expected 3 JSONL lines, got %d\nlines: %v", len(lines), lines)
	}
}

func TestLogger_EntryTypes_InOrder(t *testing.T) {
	// When LogRequest, LogResponse, and LogError are called in sequence,
	// the resulting entries must appear in the same order in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "error")
	logger.Close()

	lines := readLogLines(t, logger)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	types := []string{"request", "response", "error"}
	for i, wantType := range types {
		m := unmarshalLine(t, lines[i])
		if got := m["type"]; got != wantType {
			t.Errorf("line %d type = %q, want %q", i, got, wantType)
		}
	}
}

// ============================================================
// Close and Path behavior
// ============================================================

func TestLogger_Path_ReturnsSamePathAfterClose(t *testing.T) {
	// Path() must return the same path before and after Close.
	// Close disables future writes but must not clear the path.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	pathBeforeClose := logger.Path()

	logger.Close()
	pathAfterClose := logger.Path()

	if pathBeforeClose != pathAfterClose {
		t.Errorf("Path() changed after Close: before=%q, after=%q", pathBeforeClose, pathAfterClose)
	}
}

func TestLogger_Close_PreventsSubsequentWrites(t *testing.T) {
	// After Close, LogRequest/LogResponse/LogError must produce no new entries.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	// These calls must be no-ops after Close.
	logger.LogRequest(sampleRequest())
	logger.LogResponse(sampleResponse())
	logger.LogError("agent#1", "after close")

	lines := readLogLines(t, logger)
	if len(lines) != 1 {
		t.Errorf("after Close, subsequent log calls must be no-ops; got %d lines, want 1", len(lines))
	}
}

func TestLogger_Close_IsIdempotent_OnNormalLogger(t *testing.T) {
	// Close is idempotent even when the logger wrote entries normally.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()
	logger.Close() // second close must not panic
}

func TestLogger_Close_IsIdempotent_OnNeverUsedLogger(t *testing.T) {
	// Close must be safe to call multiple times on a logger that was never used.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_FlushPerEntry_EntryPresentBeforeClose(t *testing.T) {
	// Each entry must be flushed to disk before Close is called. This ensures
	// entries survive an os.Exit that bypasses Close.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())

	// Intentionally do NOT call Close before reading.
	p := logger.Path()
	if p == "" {
		t.Fatal("Path() is empty after LogRequest; log file was not created")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) before Close: %v", p, err)
	}
	if !strings.Contains(string(data), "request") {
		t.Errorf("entry must be flushed to disk before Close is called; 'request' not found\ncontent:\n%s", string(data))
	}

	logger.Close() // cleanup
}

// ============================================================
// Fail-silent behavior
// ============================================================

func TestLogger_FailSilent_LogRequest_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogRequest must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogRequest(sampleRequest()) // must not panic
}

func TestLogger_FailSilent_LogResponse_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogResponse must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogResponse(sampleResponse()) // must not panic
}

func TestLogger_FailSilent_LogError_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created, LogError must return without panicking.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogError("agent#1", "error text") // must not panic
}

func TestLogger_FailSilent_PathIsEmpty_WhenFolderBlocked(t *testing.T) {
	// Path() must return "" when the logger is disabled (no file was created).
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest())
	logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() on disabled logger = %q, want empty string", p)
	}
}

func TestLogger_FailSilent_PermanentDisable_SubsequentCallsNeverPanic(t *testing.T) {
	// After a logging failure, all subsequent log calls must also not panic.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.LogRequest(sampleRequest()) // trigger failure
	for i := 0; i < 5; i++ {
		logger.LogRequest(sampleRequest())
		logger.LogResponse(sampleResponse())
		logger.LogError("agent#"+strconv.Itoa(i), "repeated error")
	}
}

func TestLogger_FailSilent_CloseIsIdempotent_OnDisabledLogger(t *testing.T) {
	// Close must be safe to call multiple times on a disabled logger.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest()) // trigger failure
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_FailSilent_SetRunID_OnDisabledLogger_NoPanic(t *testing.T) {
	// SetRunID on a disabled logger must not panic.
	workDir := t.TempDir()
	blockDispatchLogs(t, workDir)

	logger := New(workDir)
	logger.LogRequest(sampleRequest()) // trigger failure
	logger.SetRunID(validRunID)        // must not panic
	logger.Close()
}
