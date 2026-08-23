package diaglog_test

// Tests for the file-backed diagnostic sink (T6.1, T6.3, T6.4).
//
// T6.1: Subject stdout and stderr events are written to the file the run was
// given, attributed to that run's run ID, with nothing written to the process's
// own standard streams.
//
// T6.3: Two runs' diagnostic output lands in two distinct files and never
// interleaves.
//
// T6.4: The diagnostic file's lifetime respects the sandbox's retention scope
// (its path is under the run's control directory) and does not prevent sandbox
// teardown (no open handle survives Close).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/diaglog"
)

// ---------------------------------------------------------------------------
// T6.1 addition — write-failure degradation: Close reports any write error
// ---------------------------------------------------------------------------

// TestSink_Close_ReturnsErrorAfterWriteFailure asserts that when a write
// failure occurs during Log, Close returns a non-nil error reporting the
// degradation. The design contract states: "a write failure after Open
// succeeded is recorded and reported by Close, because losing a diagnostic
// line must not fail a run whose subject behaved correctly."
//
// Write-failure injection: after Open the file is made unwritable via
// os.Chmod(0). On Unix this revokes the write permission immediately, so
// a subsequent Log call (which writes through the open handle) will fail.
// On Windows, os.Chmod maps to the read-only attribute, which may not
// prevent writes through an already-open handle; in that case setup fails
// and the test is skipped with a message rather than producing a false pass.
func TestSink_Close_ReturnsErrorAfterWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Revoke all permissions on the file to make future writes fail.
	if chmodErr := os.Chmod(path, 0000); chmodErr != nil {
		t.Skipf("setup: cannot change file permissions (%v); write-failure injection not supported on this platform — skip", chmodErr)
	}
	// Restore permissions so t.TempDir cleanup can remove the file.
	t.Cleanup(func() { _ = os.Chmod(path, 0666) })

	// Log must not panic. If the write attempt fails, the error is
	// buffered and surfaced by Close per the contract.
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "payload that cannot be written"})

	closeErr := sink.Close()
	if closeErr == nil {
		t.Errorf("Close returned nil; want a non-nil error because a write failure occurred during Log — the design contract requires Close to surface write failures")
	}
}

// ---------------------------------------------------------------------------
// T6.1 — file-backed sink: events are written to the file, attributed to runID
// ---------------------------------------------------------------------------

// TestSink_Log_WritesEventNameToFile asserts that Log persists the event name
// into the diagnostic file so the file can be read back to identify what
// events arrived.
func TestSink_Log_WritesEventNameToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "output from subject"})

	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: file not created by Open/Log: %v — Log must write to the file", readErr)
	}
	if !strings.Contains(string(data), "spawn.stdout") {
		t.Errorf("log file does not contain the event name %q; got:\n%s", "spawn.stdout", data)
	}
}

// TestSink_Log_WritesEventMessageToFile asserts that Log persists the event
// message — the subject's captured output — into the diagnostic file.
func TestSink_Log_WritesEventMessageToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const payload = "subject output that must appear verbatim in the diagnostic file"
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: payload})

	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: file not created by Open/Log: %v", readErr)
	}
	if !strings.Contains(string(data), payload) {
		t.Errorf("log file does not contain the event message %q; got:\n%s", payload, data)
	}
}

// TestSink_Log_AttributesRunID asserts that the run ID supplied to Open
// appears in the diagnostic file so the file can be correlated back to the
// run that produced it when logs are inspected out of context.
func TestSink_Log_AttributesRunID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")
	const runID = "20260101T120000Z-cafe"

	sink, err := diaglog.Open(path, runID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "content"})
	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if !strings.Contains(string(data), runID) {
		t.Errorf("log file does not contain the run ID %q; the file must be attributed to the run: content:\n%s", runID, data)
	}
}

// TestSink_Log_WritesBothSubjectStreams asserts that both the stdout and
// stderr spawn events are recorded when both are logged. The file must contain
// both — one missing is a loss of diagnostic information.
func TestSink_Log_WritesBothSubjectStreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "stdout payload"})
	sink.Log(commonharness.Event{Name: "spawn.stderr", Message: "stderr payload"})
	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if !strings.Contains(string(data), "stdout payload") {
		t.Errorf("log file is missing the spawn.stdout payload; content:\n%s", data)
	}
	if !strings.Contains(string(data), "stderr payload") {
		t.Errorf("log file is missing the spawn.stderr payload; content:\n%s", data)
	}
}

// TestSink_Open_CreatesParentDirectory asserts that Open creates the parent
// directory on demand, so the caller does not need a separate mkdir before
// calling Open.
func TestSink_Open_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newsubdir")
	path := filepath.Join(dir, "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open in non-existent parent dir %q: %v — Open must create the parent directory", dir, err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "content"})
	_ = sink.Close()

	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("parent directory %q was not created by Open: %v", dir, statErr)
	}
}

// TestDiscard_DropsAllEvents asserts that Discard silently discards every
// event and never creates a file, panics, or blocks.
func TestDiscard_DropsAllEvents(t *testing.T) {
	// Discard.Log must not panic, block, or otherwise fail. This test simply
	// asserts the call completes; no file is created and nothing is asserted
	// about output.
	diaglog.Discard.Log(commonharness.Event{Name: "spawn.stdout", Message: "should be dropped"})
	diaglog.Discard.Log(commonharness.Event{Name: "spawn.stderr", Message: "should also be dropped"})
}

// ---------------------------------------------------------------------------
// T6.3 — per-run isolation: two runs write to distinct files, never the same
// ---------------------------------------------------------------------------

// TestSink_TwoSinks_WriteToDistinctFiles asserts that when two sinks are
// opened at different paths, each run's events appear only in its own file
// and not in the other's.
func TestSink_TwoSinks_WriteToDistinctFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "run1.log")
	path2 := filepath.Join(dir, "run2.log")

	sink1, err1 := diaglog.Open(path1, "20260101T120000Z-0001")
	if err1 != nil {
		t.Fatalf("Open path1: %v", err1)
	}
	sink2, err2 := diaglog.Open(path2, "20260101T120000Z-0002")
	if err2 != nil {
		t.Fatalf("Open path2: %v", err2)
	}

	const msg1 = "exclusive-output-from-run-one"
	const msg2 = "exclusive-output-from-run-two"

	sink1.Log(commonharness.Event{Name: "spawn.stdout", Message: msg1})
	sink2.Log(commonharness.Event{Name: "spawn.stdout", Message: msg2})

	if closeErr := sink1.Close(); closeErr != nil {
		t.Fatalf("Close sink1: %v", closeErr)
	}
	if closeErr := sink2.Close(); closeErr != nil {
		t.Fatalf("Close sink2: %v", closeErr)
	}

	data1, readErr1 := os.ReadFile(path1)
	if readErr1 != nil {
		t.Fatalf("ReadFile path1: %v — sink1 must write to its own file", readErr1)
	}
	data2, readErr2 := os.ReadFile(path2)
	if readErr2 != nil {
		t.Fatalf("ReadFile path2: %v — sink2 must write to its own file", readErr2)
	}

	// Each file must contain only its own run's output.
	if !strings.Contains(string(data1), msg1) {
		t.Errorf("path1 does not contain run 1's output %q; content:\n%s", msg1, data1)
	}
	if strings.Contains(string(data1), msg2) {
		t.Errorf("path1 contains run 2's output %q — output interleaved across files; content:\n%s", msg2, data1)
	}
	if !strings.Contains(string(data2), msg2) {
		t.Errorf("path2 does not contain run 2's output %q; content:\n%s", msg2, data2)
	}
	if strings.Contains(string(data2), msg1) {
		t.Errorf("path2 contains run 1's output %q — output interleaved across files; content:\n%s", msg1, data2)
	}
}

// TestSink_TwoSinks_FilePathsAreDistinct asserts that sinks opened at
// different paths are fully independent: no shared state connects them.
func TestSink_TwoSinks_FilePathsAreDistinct(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "run1.log")
	path2 := filepath.Join(dir, "run2.log")

	if path1 == path2 {
		t.Fatal("test setup error: path1 and path2 are the same")
	}

	sink1, err1 := diaglog.Open(path1, "20260101T120000Z-0001")
	if err1 != nil {
		t.Fatalf("Open path1: %v", err1)
	}
	sink2, err2 := diaglog.Open(path2, "20260101T120000Z-0002")
	if err2 != nil {
		t.Fatalf("Open path2: %v", err2)
	}
	// Log ten events to each sink alternately, to surface any shared-state
	// bug that would mix events at any ordering.
	for i := range 10 {
		sink1.Log(commonharness.Event{Name: "spawn.stdout", Message: strings.Repeat("a", i+1)})
		sink2.Log(commonharness.Event{Name: "spawn.stdout", Message: strings.Repeat("b", i+1)})
	}

	if err := sink1.Close(); err != nil {
		t.Fatalf("Close sink1: %v", err)
	}
	if err := sink2.Close(); err != nil {
		t.Fatalf("Close sink2: %v", err)
	}

	data1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("ReadFile path1: %v", err)
	}
	data2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("ReadFile path2: %v", err)
	}

	// path1 must contain only 'a' payloads; path2 only 'b' payloads.
	if strings.Contains(string(data1), strings.Repeat("b", 10)) {
		t.Errorf("path1 contains a payload that belongs in path2; the two sinks must be independent")
	}
	if strings.Contains(string(data2), strings.Repeat("a", 10)) {
		t.Errorf("path2 contains a payload that belongs in path1; the two sinks must be independent")
	}
}

// ---------------------------------------------------------------------------
// T6.4 — file lifetime: Close releases the handle; sandbox teardown succeeds
// ---------------------------------------------------------------------------

// TestSink_Close_ReleasesFileHandle asserts that after Close returns, the
// diagnostic file is not held open by any handle. On Windows, an open handle
// blocks directory removal, which would prevent sandbox teardown.
func TestSink_Close_ReleasesFileHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "diagnostic payload"})
	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	// The file must exist (Log wrote to it before Close).
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("diagnostic file not created: %v — Log must write to the file before Close releases the handle", statErr)
	}

	// After Close the file must be removable. On Windows an open handle
	// blocks os.Remove; this assertion fails if Close left one open.
	if removeErr := os.Remove(path); removeErr != nil {
		t.Errorf("os.Remove after Close: %v — Close must release the file handle so sandbox teardown can delete the file", removeErr)
	}
}

// TestSink_Close_AllowsDirectoryRemoval asserts that after Close the
// directory containing the diagnostic log can be removed, which is the actual
// operation sandbox teardown performs.
func TestSink_Close_AllowsDirectoryRemoval(t *testing.T) {
	controlDir := filepath.Join(t.TempDir(), "ctl")
	if err := os.MkdirAll(controlDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(controlDir, "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stderr", Message: "stderr payload"})
	if closeErr := sink.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}

	// Sandbox teardown deletes the whole control directory tree.
	// Close must release the handle so RemoveAll succeeds immediately.
	if removeErr := os.RemoveAll(controlDir); removeErr != nil {
		t.Errorf("os.RemoveAll after Close: %v — Close must release the file handle so the entire sandbox can be torn down", removeErr)
	}
}

// TestSink_Close_ReturnsNilOnSuccess asserts that Close returns nil when all
// log entries were written successfully, so a caller does not receive a
// spurious error when no degradation occurred.
func TestSink_Close_ReturnsNilOnSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")

	sink, err := diaglog.Open(path, "20260101T120000Z-0001")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sink.Log(commonharness.Event{Name: "spawn.stdout", Message: "content"})

	if closeErr := sink.Close(); closeErr != nil {
		t.Errorf("Close returned %v; want nil when all writes succeeded", closeErr)
	}
}
