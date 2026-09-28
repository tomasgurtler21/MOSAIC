// Package debuglog (in-package tests) covers the file-backed Logger and the
// domain.NopDebugLogger. Tests are in-package so that unexported failure-injection
// seams are accessible when the implementation is added.
//
// Every test that touches the filesystem resolves its working directory from
// t.TempDir() so that no test can create RunnerLogs/ in the repository root.
package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// Helpers
// ============================================================

// readLogFile reads the content of the logger's current log file.
// The caller must have called logger.Close() before readLogFile so that any
// buffered output is fully flushed.
func readLogFile(t *testing.T, logger *Logger) string {
	t.Helper()
	p := logger.Path()
	if p == "" {
		t.Fatal("readLogFile: logger.Path() is empty; no log file was created")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("readLogFile: ReadFile(%q): %v", p, err)
	}
	return string(data)
}

// blockRunnerLogs places a regular file where RunnerLogs/ would be created,
// causing os.MkdirAll to fail on both Windows and POSIX. Returns the path of
// the blocker file.
func blockRunnerLogs(t *testing.T, workDir string) string {
	t.Helper()
	blocker := filepath.Join(workDir, LogsFolderName)
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatalf("blockRunnerLogs: WriteFile(%q): %v", blocker, err)
	}
	return blocker
}

// blockRunFolder places a regular file where RunnerLogs/{runID}/ would be
// created, causing a replay's os.MkdirAll to fail deterministically. Returns
// the path of the blocker file.
func blockRunFolder(t *testing.T, workDir, runID string) string {
	t.Helper()
	logDir := filepath.Join(workDir, LogsFolderName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("blockRunFolder: MkdirAll(%q): %v", logDir, err)
	}
	blocker := filepath.Join(logDir, runID)
	if err := os.WriteFile(blocker, []byte("blocker"), 0644); err != nil {
		t.Fatalf("blockRunFolder: WriteFile(%q): %v", blocker, err)
	}
	return blocker
}

// validRunID is a canonical run_id used across tests.
const validRunID = "20260805T143029Z-9bc0"

// assertNoInterleavedBlocks scans the log content and fails if any multi-line
// entry's begin-block is interrupted by another entry's header line.
func assertNoInterleavedBlocks(t *testing.T, content string) {
	t.Helper()
	lines := strings.Split(content, "\n")
	inBlock := false
	blockEvent := ""
	for lineNum, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- begin "):
			if inBlock {
				t.Errorf("line %d: nested begin block %q while still inside block for %q",
					lineNum+1, line, blockEvent)
			}
			inBlock = true
			// Extract event name: "--- begin {event} ---"
			trimmed := strings.TrimPrefix(line, "--- begin ")
			blockEvent = strings.TrimSuffix(trimmed, " ---")

		case strings.HasPrefix(line, "--- end "):
			if !inBlock {
				t.Errorf("line %d: end block %q without preceding begin", lineNum+1, line)
			}
			inBlock = false
			blockEvent = ""

		default:
			// A line inside a block that starts with '[' and contains 'Z]' looks
			// like a new entry header, which would indicate interleaving.
			if inBlock && strings.HasPrefix(line, "[") && strings.Contains(line, "Z]") {
				t.Errorf("line %d: entry header found inside block for %q: %q",
					lineNum+1, blockEvent, line)
			}
		}
	}
	if inBlock {
		t.Errorf("log ended with unclosed begin block for event %q", blockEvent)
	}
}
