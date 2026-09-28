// Tests for ResolveAndAnnounce output and logging.
package resolve_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun/resolve"
)

// TestResolveAndAnnounce_CLIMode_WritesFormattedLinePerHarness verifies that
// in CLI mode (non-nil cliOutput), ResolveAndAnnounce writes one formatted
// line per resolved harness, in the format "  <harnessID>: <path>\n".
func TestResolveAndAnnounce_CLIMode_WritesFormattedLinePerHarness(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	want := "  claude-code: " + absPath + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("writer output = %q, want it to contain %q", got, want)
	}
}

// TestResolveAndAnnounce_CLIMode_LogsResolvePathEventPerHarness verifies that
// each resolved harness produces a log entry with EventTestrunResolvePath.
func TestResolveAndAnnounce_CLIMode_LogsResolvePathEventPerHarness(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.events) == 0 {
		t.Fatal("expected at least one log call, got none")
	}
	if logger.events[0] != domain.EventTestrunResolvePath {
		t.Errorf("first log event = %q, want %q", logger.events[0], domain.EventTestrunResolvePath)
	}
}

// TestResolveAndAnnounce_CLIMode_LogFieldsContainHarnessAndPath verifies that
// the log entry fields include the harness ID and resolved absolute path.
func TestResolveAndAnnounce_CLIMode_LogFieldsContainHarnessAndPath(t *testing.T) {
	const (
		harnessID = "claude-code"
		absPath   = "/usr/local/bin/claude"
	)
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{harnessID},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.fields) == 0 {
		t.Fatal("expected log fields, got none")
	}
	if !logger.hasEventField(0, "harness", harnessID) {
		t.Errorf("log fields missing harness=%q; fields[0] = %v", harnessID, logger.fields[0])
	}
	if !logger.hasEventField(0, "path", absPath) {
		t.Errorf("log fields missing path=%q; fields[0] = %v", absPath, logger.fields[0])
	}
}

// TestResolveAndAnnounce_CLIMode_MultipleHarnesses_WritesAllLines verifies
// that all resolved harnesses are written to the writer.
func TestResolveAndAnnounce_CLIMode_MultipleHarnesses_WritesAllLines(t *testing.T) {
	paths := map[string]string{
		"claude":  "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
	}
	lookPath := perBinaryLookPath(paths)
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code", "opencode"},
		lookPath,
		&out,
		nil, // nil logger: no logging, no panic
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "claude-code:") {
		t.Errorf("writer output missing \"claude-code:\"; got: %q", got)
	}
	if !strings.Contains(got, "opencode:") {
		t.Errorf("writer output missing \"opencode:\"; got: %q", got)
	}
}

// TestResolveAndAnnounce_CLIMode_SkipsFakeHarness_NoOutputLine verifies that
// the "fake" harness produces no writer output (it is silently skipped).
func TestResolveAndAnnounce_CLIMode_SkipsFakeHarness_NoOutputLine(t *testing.T) {
	lookPath := fixedLookPath("/usr/local/bin/claude", nil)
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{"fake"},
		lookPath,
		&out,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("writer output for fake-only harnesses should be empty, got: %q", out.String())
	}
}

// TestResolveAndAnnounce_CLIMode_MultipleHarnesses_LogsEachHarness verifies
// that one log entry is produced per real harness.
func TestResolveAndAnnounce_CLIMode_MultipleHarnesses_LogsEachHarness(t *testing.T) {
	paths := map[string]string{
		"claude":  "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
	}
	lookPath := perBinaryLookPath(paths)
	logger := &recordingDebugLogger{}

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code", "opencode"},
		lookPath,
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.events) != 2 {
		t.Errorf("expected 2 log calls for 2 harnesses, got %d: %v", len(logger.events), logger.events)
	}
	for i, event := range logger.events {
		if event != domain.EventTestrunResolvePath {
			t.Errorf("logger.events[%d] = %q, want %q", i, event, domain.EventTestrunResolvePath)
		}
	}
}

// TestResolveAndAnnounce_TUIMode_NilWriter_LogsWithoutPanic verifies that
// when cliOutput is nil (TUI mode), ResolveAndAnnounce logs paths via the
// debug logger without panicking.
func TestResolveAndAnnounce_TUIMode_NilWriter_LogsWithoutPanic(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil, // TUI mode: no stdout output
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.events) == 0 {
		t.Error("TUI mode: expected logger to receive calls, got none")
	}
}

// TestResolveAndAnnounce_NilLogger_NoPanic verifies that a nil logger is
// tolerated: logging is skipped without panicking.
func TestResolveAndAnnounce_NilLogger_NoPanic(t *testing.T) {
	lookPath := fixedLookPath("/usr/local/bin/claude", nil)
	var out bytes.Buffer

	// Must not panic.
	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		nil, // nil interface value: logging must be skipped
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestResolveAndAnnounce_Failure_ReturnsError verifies that a resolution
// failure is propagated as a non-nil error.
func TestResolveAndAnnounce_Failure_ReturnsError(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err == nil {
		t.Fatal("expected error on resolution failure, got nil")
	}
}

// TestResolveAndAnnounce_Failure_DoesNotLog verifies that on resolution
// failure, no log entries are emitted (the error is returned before logging).
func TestResolveAndAnnounce_Failure_DoesNotLog(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	logger := &recordingDebugLogger{}

	_, _ = resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil,
		logger,
	)
	if len(logger.events) != 0 {
		t.Errorf("expected no log calls on failure, got %d event(s): %v", len(logger.events), logger.events)
	}
}

// TestResolveAndAnnounce_Failure_DoesNotWriteToWriter verifies that on
// resolution failure, nothing is written to the cli output writer.
func TestResolveAndAnnounce_Failure_DoesNotWriteToWriter(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	var out bytes.Buffer

	_, _ = resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		nil,
	)
	if out.Len() != 0 {
		t.Errorf("expected no writer output on failure, got %q", out.String())
	}
}

// TestResolveAndAnnounce_Success_ReturnsResolvedMap verifies that on success
// the returned map contains the harness-to-path entries.
func TestResolveAndAnnounce_Success_ReturnsResolvedMap(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	result, err := resolve.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := result["claude-code"]; !ok {
		t.Error("result map missing key claude-code")
	} else if got != absPath {
		t.Errorf("result[claude-code] = %q, want %q", got, absPath)
	}
}
