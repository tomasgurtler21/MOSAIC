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
	"strconv"
	"strings"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
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

// ============================================================
// NopDebugLogger (domain.NopDebugLogger)
// ============================================================

func TestNopDebugLogger_ImplementsDebugLogger(t *testing.T) {
	// NopDebugLogger must satisfy the domain.DebugLogger interface.
	// This test fails at compile time if the type does not implement the interface.
	var _ domain.DebugLogger = domain.NopDebugLogger{}
}

func TestNopDebugLogger_ZeroValueIsUsable(t *testing.T) {
	// The zero value of NopDebugLogger must be usable without explicit initialisation.
	var nop domain.NopDebugLogger
	nop.Log("test.event", "message")
}

func TestNopDebugLogger_LogDoesNotPanic_NoFields(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "message")
}

func TestNopDebugLogger_LogDoesNotPanic_WithFields(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "message", domain.F("agent", "researcher#1"), domain.F("kind", "ordinary"))
}

func TestNopDebugLogger_LogDoesNotPanic_EmptyArgs(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("", "")
}

func TestNopDebugLogger_LogDoesNotPanic_MultiLineMessage(t *testing.T) {
	nop := domain.NopDebugLogger{}
	nop.Log("test.event", "line1\nline2\nline3\n")
}

func TestNopDebugLogger_CopyIsSafe(t *testing.T) {
	// NopDebugLogger may be copied freely and shared; each copy must be independently usable.
	nop1 := domain.NopDebugLogger{}
	nop2 := nop1 // value copy
	nop1.Log("test.event", "from copy 1")
	nop2.Log("test.event", "from copy 2")
}

func TestNopDebugLogger_ConcurrentLogIsSafe(t *testing.T) {
	// NopDebugLogger must be safe for concurrent use.
	nop := domain.NopDebugLogger{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nop.Log("test.event", "concurrent call")
		}()
	}
	wg.Wait()
}

// ============================================================
// DebugField and F helper (domain)
// ============================================================

func TestDebugField_F_SetsKey(t *testing.T) {
	f := domain.F("mykey", "myvalue")
	if f.Key != "mykey" {
		t.Errorf("F() Key = %q, want %q", f.Key, "mykey")
	}
}

func TestDebugField_F_SetsValue(t *testing.T) {
	f := domain.F("mykey", "myvalue")
	if f.Value != "myvalue" {
		t.Errorf("F() Value = %q, want %q", f.Value, "myvalue")
	}
}

func TestDebugField_F_EmptyKeyAndValue(t *testing.T) {
	f := domain.F("", "")
	if f.Key != "" || f.Value != "" {
		t.Errorf("F(\"\", \"\") = {Key:%q, Value:%q}, want empty", f.Key, f.Value)
	}
}

// ============================================================
// Logger implements domain.DebugLogger
// ============================================================

func TestLogger_ImplementsDebugLogger(t *testing.T) {
	// *Logger must satisfy the domain.DebugLogger interface.
	// This fails at compile time if the method set is wrong.
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	var _ domain.DebugLogger = logger
}

// ============================================================
// File creation and naming (T4.2)
// ============================================================

func TestLogger_FileCreatedUnderRunnerLogs_WithRunID(t *testing.T) {
	// When SetRunID is called before the first Log, the log file must be
	// at RunnerLogs/{run_id}/{run_id}.log under the supplied working directory.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "starting run")
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+".log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected log file at %q, but stat failed: %v", wantPath, err)
	}
}

func TestLogger_PathReturnsAbsoluteFilePath_WithRunID(t *testing.T) {
	// Path() must return the absolute path to the log file after the first write.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "starting run")
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+".log")
	if gotPath := logger.Path(); gotPath != wantPath {
		t.Errorf("Path() = %q, want %q", gotPath, wantPath)
	}
}

func TestLogger_PathReturnsEmpty_BeforeFirstLog(t *testing.T) {
	// Path() must return "" before any Log call (lazy file creation contract).
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() before first Log = %q, want empty string", p)
	}
}

func TestLogger_NoFileCreated_BeforeFirstLog(t *testing.T) {
	// RunnerLogs/ must not be created until the first Log call.
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("RunnerLogs/ must not exist before first Log; stat(%q) err = %v", logDir, err)
	}
}

func TestLogger_FallbackFileName_StartsWithStartup_WhenNoRunID(t *testing.T) {
	// When no valid SetRunID is called before the first Log, the file name must
	// start with "startup-" so it cannot collide with a valid run_id name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "early startup log")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created in RunnerLogs/ after first Log call")
	}

	name := entries[0].Name()
	if !strings.HasPrefix(name, "startup-") {
		t.Errorf("fallback filename = %q, want prefix \"startup-\"", name)
	}
}

func TestLogger_FallbackFileName_IsNotAValidRunID(t *testing.T) {
	// The fallback filename base (without .log) must not satisfy domain.IsValidRunID,
	// keeping the two naming schemes distinguishable.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "startup entry")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}

	baseName := strings.TrimSuffix(entries[0].Name(), ".log")
	if domain.IsValidRunID(baseName) {
		t.Errorf("fallback filename base %q must not satisfy IsValidRunID, but it does", baseName)
	}
}

func TestLogger_LogsFolderCreatedDirectlyUnderWorkDir(t *testing.T) {
	// RunnerLogs/ must be a direct child of the supplied working directory.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "hello")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	fi, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("RunnerLogs/ not created at %q: %v", logDir, err)
	}
	if !fi.IsDir() {
		t.Errorf("%q must be a directory", logDir)
	}
}

func TestLogger_SetRunID_CalledAfterFirstLog_MovesFileIntoRunFolder(t *testing.T) {
	// When SetRunID is called after the first Log has already been written
	// under the fallback name, a replay must move the entries into the run
	// folder and Path() must report the new location.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "early entry without run_id")
	pathBefore := logger.Path()
	if pathBefore == "" {
		t.Fatal("Path() empty after first Log; expected a file to be created")
	}

	logger.SetRunID(validRunID)
	pathAfter := logger.Path()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+".log")
	if pathAfter != wantPath {
		t.Errorf("Path() after replay = %q, want %q", pathAfter, wantPath)
	}
	if pathAfter == pathBefore {
		t.Errorf("Path() must change after a successful replay, stayed at %q", pathBefore)
	}

	logger.Close()
}

func TestLogger_SetRunID_CalledAfterFirstLog_ReplaysEntriesWithoutCorrelationEntry(t *testing.T) {
	// A replay must carry the original entries into the run-folder file and
	// must never write a correlation entry (FR-9 removes that mechanism for
	// this logger; the replay itself is the correlation).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "early entry")
	logger.SetRunID(validRunID)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "early entry") {
		t.Errorf("replayed file must contain the original entry\ncontent:\n%s", content)
	}
	if strings.Contains(content, domain.EventRunnerRunID) {
		t.Errorf("replayed file must not contain a correlation entry (%q)\ncontent:\n%s",
			domain.EventRunnerRunID, content)
	}
}

func TestLogger_SetRunID_EmptyRunID_IsIgnored(t *testing.T) {
	// An empty run_id must be silently ignored; the next Log must use the fallback name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID("")
	logger.Log(domain.EventRunnerStart, "message after empty SetRunID")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}
	if !strings.HasPrefix(entries[0].Name(), "startup-") {
		t.Errorf("after empty SetRunID, file name = %q, want startup- prefix", entries[0].Name())
	}
}

func TestLogger_SetRunID_InvalidRunID_IsIgnored(t *testing.T) {
	// An invalid run_id must be silently ignored; the fallback name is used.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID("not-valid")
	logger.Log(domain.EventRunnerStart, "message after invalid SetRunID")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}
	if !strings.HasPrefix(entries[0].Name(), "startup-") {
		t.Errorf("after invalid SetRunID, file name = %q, want startup- prefix", entries[0].Name())
	}
}

func TestLogger_SetRunID_OnlyFirstEffectiveCallNames_TheFile(t *testing.T) {
	// Only the first effective (valid) SetRunID call names the file.
	// A second valid call before the first Log must be ignored.
	workDir := t.TempDir()
	logger := New(workDir)

	firstID := validRunID
	logger.SetRunID(firstID)
	logger.SetRunID("20260101T000000Z-ffff") // second call — must be ignored
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, firstID, firstID+".log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected file named after first run_id %q, stat failed: %v", wantPath, err)
	}
}

// ============================================================
// Buffer-and-replay (T1.1)
// ============================================================

func TestLogger_Replay_MovesBufferedEntriesVerbatim_InOriginalOrder(t *testing.T) {
	// Entries logged before identity is known must be replayed into the run
	// folder in original order, byte-identical to how they were originally
	// rendered (same formatting, same timestamps) — never re-rendered.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "first entry")
	logger.Log(domain.EventSessionDispatchStart, "second entry")
	logger.Log(domain.EventRunnerStart, "third entry")

	outOfRunPath := logger.Path()
	if outOfRunPath == "" {
		t.Fatal("Path() empty after pre-identity logs")
	}
	bufferedContent, err := os.ReadFile(outOfRunPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", outOfRunPath, err)
	}

	logger.SetRunID(validRunID)
	logger.Close()

	replayedContent := readLogFile(t, logger)
	if replayedContent != string(bufferedContent) {
		t.Errorf("replayed content does not match the original buffered bytes verbatim\noriginal:\n%s\nreplayed:\n%s",
			bufferedContent, replayedContent)
	}
}

func TestLogger_Replay_RemovesOutOfRunFile_AfterSuccess(t *testing.T) {
	// A successful replay must leave exactly one file on disk: the
	// out-of-run file must no longer exist once entries are in the run folder.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "pre-identity entry")
	outOfRunPath := logger.Path()

	logger.SetRunID(validRunID)
	logger.Close()

	if _, err := os.Stat(outOfRunPath); !os.IsNotExist(err) {
		t.Errorf("out-of-run file %q must be removed after a successful replay; stat err = %v", outOfRunPath, err)
	}
}

func TestLogger_SetRunID_BeforeFirstLog_NeverCreatesOutOfRunFile(t *testing.T) {
	// The common case: identity is known before the first entry, so no
	// out-of-run file is ever created and no replay is needed — only a
	// loose file directly under RunnerLogs/ would indicate one was.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("no loose file must exist directly under %q when identity is known upfront; found %q",
				logDir, e.Name())
		}
	}
}

func TestLogger_Replay_FailsWhenRunFolderBlocked_LeavesOutOfRunFileIntact(t *testing.T) {
	// When the run folder cannot be created, the out-of-run file and its
	// entries must be left exactly as they were, and Path() must keep
	// reporting the out-of-run location.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "entry before blocked replay")
	outOfRunPath := logger.Path()
	originalContent, err := os.ReadFile(outOfRunPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", outOfRunPath, err)
	}

	blockRunFolder(t, workDir, validRunID)
	logger.SetRunID(validRunID)

	if gotPath := logger.Path(); gotPath != outOfRunPath {
		t.Errorf("Path() after failed replay = %q, want unchanged %q", gotPath, outOfRunPath)
	}
	currentContent, err := os.ReadFile(outOfRunPath)
	if err != nil {
		t.Fatalf("out-of-run file must survive a failed replay: os.ReadFile(%q): %v", outOfRunPath, err)
	}
	if string(currentContent) != string(originalContent) {
		t.Errorf("out-of-run file content changed after a failed replay\nbefore:\n%s\nafter:\n%s",
			originalContent, currentContent)
	}

	logger.Close()
}

func TestLogger_Replay_Fails_SubsequentEntriesKeepAppendingToOutOfRunFile(t *testing.T) {
	// After a failed replay, every later entry must continue to append to
	// the same out-of-run file for the rest of the process (FR-10a) — never
	// split, never lost, never attempted against the unavailable run folder.
	// This must be distinguishable from the old (pre-replay) correlation-entry
	// behavior: a genuine replay attempt never writes a correlation entry and
	// never leaves a partial file behind at the blocked run-folder path, so
	// this test must fail against code that has not implemented the replay
	// attempt at all (old code just appends a correlation entry to the
	// existing file and never touches the run-folder path).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "entry before blocked replay")
	outOfRunPath := logger.Path()

	blockerPath := blockRunFolder(t, workDir, validRunID)
	logger.SetRunID(validRunID)

	logger.Log(domain.EventSessionDispatchStart, "entry after failed replay")
	logger.Close()

	if gotPath := logger.Path(); gotPath != outOfRunPath {
		t.Errorf("Path() after failed replay + more logging = %q, want unchanged %q", gotPath, outOfRunPath)
	}
	content := readLogFile(t, logger)
	if !strings.Contains(content, "entry before blocked replay") {
		t.Error("out-of-run file must retain entries logged before the failed replay")
	}
	if !strings.Contains(content, "entry after failed replay") {
		t.Error("entries logged after a failed replay must append to the same out-of-run file")
	}
	if strings.Contains(content, domain.EventRunnerRunID) {
		t.Errorf("a failed replay attempt must never write a correlation entry (%q); the replay branch, not the old correlation branch, must be what runs\ncontent:\n%s",
			domain.EventRunnerRunID, content)
	}

	info, err := os.Stat(blockerPath)
	if err != nil {
		t.Fatalf("blocker file %q must remain in place after a failed replay: Stat: %v", blockerPath, err)
	}
	if info.IsDir() {
		t.Errorf("run-folder path %q must never be created (still blocked by a regular file) after a failed replay", blockerPath)
	}
}

func TestLogger_Replay_DoesNotPanic_WhenRunFolderBlocked(t *testing.T) {
	// A blocked run folder must degrade silently: no panic, no error
	// surfaced, and logging must continue to work afterward.
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	logger.Log(domain.EventRunnerStart, "pre-identity")
	blockRunFolder(t, workDir, validRunID)

	logger.SetRunID(validRunID) // must not panic
	logger.Log(domain.EventSessionDispatchStart, "post-attempt")
}

func TestLogger_Replay_FailsMidWrite_LeavesOutOfRunFileIntact(t *testing.T) {
	// A replay can also fail after the run folder is successfully created, if
	// writing the buffered entries into the run-folder file itself fails. This
	// must degrade exactly like an os.MkdirAll failure: the out-of-run file
	// and its entries are left untouched, no partially-written file is left
	// at the run-folder path, and Path() keeps reporting the out-of-run
	// location. Simulated by pre-creating the run folder successfully (so
	// MkdirAll no-ops) but placing a directory at the exact path the replay
	// would open for writing, so the open/write itself fails deterministically
	// on both Windows and POSIX.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.Log(domain.EventRunnerStart, "entry before mid-write failure")
	outOfRunPath := logger.Path()
	originalContent, err := os.ReadFile(outOfRunPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", outOfRunPath, err)
	}

	runDir := filepath.Join(workDir, LogsFolderName, validRunID)
	runFilePath := filepath.Join(runDir, validRunID+".log")
	if err := os.MkdirAll(runFilePath, 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", runFilePath, err)
	}

	logger.SetRunID(validRunID)

	if gotPath := logger.Path(); gotPath != outOfRunPath {
		t.Errorf("Path() after mid-write replay failure = %q, want unchanged %q", gotPath, outOfRunPath)
	}
	currentContent, err := os.ReadFile(outOfRunPath)
	if err != nil {
		t.Fatalf("out-of-run file must survive a mid-write replay failure: os.ReadFile(%q): %v", outOfRunPath, err)
	}
	if string(currentContent) != string(originalContent) {
		t.Errorf("out-of-run file content changed after a mid-write replay failure\nbefore:\n%s\nafter:\n%s",
			originalContent, currentContent)
	}
	if info, err := os.Stat(runFilePath); err != nil || !info.IsDir() {
		t.Errorf("run-folder file path %q must remain untouched (still a directory) after a mid-write replay failure", runFilePath)
	}

	logger.Log(domain.EventSessionDispatchStart, "entry after mid-write failure")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "entry after mid-write failure") {
		t.Error("entries logged after a mid-write replay failure must append to the same out-of-run file")
	}
}

func TestLogger_NeverResolvesRunID_AllEntriesRemainInOutOfRunFile(t *testing.T) {
	// A process that never resolves a valid run_id leaves its out-of-run
	// file, with every entry, on disk — unchanged from current behavior.
	workDir := t.TempDir()
	logger := New(workDir)

	messages := []string{"entry one", "entry two", "entry three"}
	for _, m := range messages {
		logger.Log(domain.EventRunnerStart, m)
	}
	logger.Close()

	content := readLogFile(t, logger)
	for _, m := range messages {
		if !strings.Contains(content, m) {
			t.Errorf("out-of-run file missing entry %q\ncontent:\n%s", m, content)
		}
	}
	if base := filepath.Base(logger.Path()); !strings.HasPrefix(base, "startup-") {
		t.Errorf("Path() base = %q, want startup- prefix (never replayed)", base)
	}
}

// ============================================================
// Entry writing (T4.3)
// ============================================================

func TestLogger_SingleLineEntry_HasTimestampBracket(t *testing.T) {
	// Each entry must begin with a '[' opening the timestamp bracket.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	content := readLogFile(t, logger)
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "[") {
		t.Errorf("entry must start with '[', got: %q", firstLine)
	}
}

func TestLogger_SingleLineEntry_HasUTCTimestamp(t *testing.T) {
	// The timestamp must be UTC (ends with 'Z' inside the bracket).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "Z]") {
		t.Errorf("entry must contain UTC timestamp ending 'Z]'\ncontent: %s", content)
	}
}

func TestLogger_SingleLineEntry_ContainsEventName(t *testing.T) {
	// The entry must contain the event name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, domain.EventHarnessInvokeStart) {
		t.Errorf("entry must contain event name %q\ncontent: %s", domain.EventHarnessInvokeStart, content)
	}
}

func TestLogger_SingleLineEntry_ContainsField(t *testing.T) {
	// A single-line entry must render each field as key=value.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("agent", "researcher#1"),
		domain.F("kind", "ordinary"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "agent=researcher#1") {
		t.Errorf("entry must contain field 'agent=researcher#1'\ncontent: %s", content)
	}
	if !strings.Contains(content, "kind=ordinary") {
		t.Errorf("entry must contain field 'kind=ordinary'\ncontent: %s", content)
	}
}

func TestLogger_SingleLineEntry_ContainsPipeSeparatorAndMessage(t *testing.T) {
	// The message must follow a '|' separator.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "hello world")
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, "| hello world") {
		t.Errorf("entry must contain '| hello world'\ncontent: %s", content)
	}
}

func TestLogger_MultiLineEntry_UsesBeginEndDelimiters(t *testing.T) {
	// A multi-line message must use begin/end block delimiters so the payload
	// is unambiguously attributable to its entry.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, "line1\nline2\nline3")
	logger.Close()

	content := readLogFile(t, logger)

	beginMark := "--- begin " + domain.EventHarnessStdout + " ---"
	endMark := "--- end " + domain.EventHarnessStdout + " ---"

	if !strings.Contains(content, beginMark) {
		t.Errorf("multi-line entry must contain %q\ncontent:\n%s", beginMark, content)
	}
	if !strings.Contains(content, endMark) {
		t.Errorf("multi-line entry must contain %q\ncontent:\n%s", endMark, content)
	}
}

func TestLogger_MultiLineEntry_PayloadWrittenVerbatim(t *testing.T) {
	// The multi-line payload must appear verbatim inside the begin/end block.
	workDir := t.TempDir()
	logger := New(workDir)

	payload := "first line\nsecond line\n{\"type\":\"result\"}\n"
	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, payload)
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, payload) {
		t.Errorf("payload must appear verbatim in the log file\ncontent:\n%s", content)
	}
}

func TestLogger_MultiLineEntry_LargePayloadNotTruncated(t *testing.T) {
	// Multi-line payloads of any size must be written in full — no truncation.
	workDir := t.TempDir()
	logger := New(workDir)

	var sb strings.Builder
	const lineCount = 200
	for i := 0; i < lineCount; i++ {
		sb.WriteString("payload line " + strconv.Itoa(i) + " — should appear verbatim in the debug log\n")
	}
	largePayload := sb.String()

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, largePayload, domain.F("bytes", strconv.Itoa(len(largePayload))))
	logger.Close()

	content := readLogFile(t, logger)
	if !strings.Contains(content, largePayload) {
		t.Errorf("large payload (len=%d) must appear verbatim; log appears truncated", len(largePayload))
	}
}

func TestLogger_MultipleEntries_AllPresentInFile(t *testing.T) {
	// Sequential Log calls must all produce entries in the file.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	messages := []string{"first entry", "second entry", "third entry"}
	for _, msg := range messages {
		logger.Log(domain.EventRunnerStart, msg)
	}
	logger.Close()

	content := readLogFile(t, logger)
	for _, want := range messages {
		if !strings.Contains(content, want) {
			t.Errorf("log must contain %q\ncontent:\n%s", want, content)
		}
	}
}

func TestLogger_FieldsInMultiLineEntry_AppearsInHeader(t *testing.T) {
	// Fields must appear on the header line of a multi-line entry (before the begin block).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessStdout, "line1\nline2", domain.F("bytes", "10"))
	logger.Close()

	content := readLogFile(t, logger)
	lines := strings.Split(content, "\n")

	// Find the header line (starts with '[' and contains the event name).
	found := false
	for _, line := range lines {
		if strings.Contains(line, domain.EventHarnessStdout) && strings.HasPrefix(line, "[") {
			if strings.Contains(line, "bytes=10") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("field 'bytes=10' must appear on the multi-line entry header line\ncontent:\n%s", content)
	}
}

func TestLogger_FieldValue_WithSpace_IsQuoted(t *testing.T) {
	// A field value that contains a space must be rendered with %q quoting so
	// it cannot be mistaken for the boundary between two fields or the message
	// separator. Unquoted values with spaces would make the wire format ambiguous.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("path", "C:/a b/file.md"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	// The value must appear with surrounding double-quote characters (Go %q output).
	if !strings.Contains(content, `path="C:/a b/file.md"`) {
		t.Errorf("field value with space must be quoted; want path=\"C:/a b/file.md\" in log\ncontent:\n%s", content)
	}
}

func TestLogger_FieldValue_WithPipe_IsQuoted(t *testing.T) {
	// A field value that contains '|' must be rendered with %q quoting so it
	// cannot be mistaken for the '|' separator that divides the header from the
	// message body. An unquoted '|' in a field value would corrupt the wire format.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.Log(domain.EventHarnessInvokeStart, "dispatching",
		domain.F("tag", "foo|bar"),
	)
	logger.Close()

	content := readLogFile(t, logger)
	// The value must appear with surrounding double-quote characters (Go %q output).
	if !strings.Contains(content, `tag="foo|bar"`) {
		t.Errorf("field value with '|' must be quoted; want tag=\"foo|bar\" in log\ncontent:\n%s", content)
	}
}

func TestLogger_FlushPerEntry_EntryPresentBeforeClose(t *testing.T) {
	// Each entry must be flushed to the operating system immediately after Log
	// returns, before Close is called. This is the crash-survival guarantee: if
	// the process is killed by os.Exit (bypassing Close), the entry must already
	// be on disk.
	//
	// The file is read via an independent os.ReadFile — not through the Logger —
	// so we confirm durable flush rather than an in-memory buffer that Close would flush.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	const marker = "flush-survival-marker"
	logger.Log(domain.EventRunnerStart, marker)

	// Intentionally do NOT call Close before reading.
	p := logger.Path()
	if p == "" {
		t.Fatal("Path() is empty after Log; log file was not created")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) before Close: %v", p, err)
	}
	if !strings.Contains(string(data), marker) {
		t.Errorf("entry must be flushed to disk before Close is called; marker %q not found\ncontent:\n%s",
			marker, string(data))
	}

	logger.Close() // cleanup
}

// ============================================================
// Graceful degradation (T4.4)
// ============================================================

func TestLogger_GracefulDegradation_LogDoesNotPanic_WhenFolderBlocked(t *testing.T) {
	// When RunnerLogs/ cannot be created (blocked by a regular file),
	// Log must return without panicking.
	workDir := t.TempDir()
	blockRunnerLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.Log(domain.EventRunnerStart, "should not panic")
}

func TestLogger_GracefulDegradation_SubsequentCalls_NeverPanic(t *testing.T) {
	// After a logging failure, all subsequent Log calls must also not panic.
	workDir := t.TempDir()
	blockRunnerLogs(t, workDir)

	logger := New(workDir)
	defer logger.Close()

	logger.Log(domain.EventRunnerStart, "trigger failure")
	for i := 0; i < 5; i++ {
		logger.Log(domain.EventSessionDispatchStart, "call "+strconv.Itoa(i)+" after failure")
	}
}

func TestLogger_GracefulDegradation_PathIsEmpty_WhenFolderBlocked(t *testing.T) {
	// Path() must return "" when the logger is disabled (no file was created).
	workDir := t.TempDir()
	blockRunnerLogs(t, workDir)

	logger := New(workDir)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() on disabled logger = %q, want empty string", p)
	}
}

func TestLogger_GracefulDegradation_CloseIsIdempotent_OnDisabledLogger(t *testing.T) {
	// Close must be safe to call multiple times on a disabled logger.
	workDir := t.TempDir()
	blockRunnerLogs(t, workDir)

	logger := New(workDir)
	logger.Log(domain.EventRunnerStart, "trigger failure")
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_GracefulDegradation_CloseIsIdempotent_OnNeverUsedLogger(t *testing.T) {
	// Close must be safe to call multiple times on a logger that was never used.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.Close()
	logger.Close() // must not panic
}

func TestLogger_GracefulDegradation_CloseIsIdempotent_OnNormalLogger(t *testing.T) {
	// Close is idempotent even when the logger wrote entries normally.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "entry")
	logger.Close()
	logger.Close() // second close must not panic
}

// ============================================================
// No Orchestration-* folder dependency (T4.5)
// ============================================================

func TestLogger_WorksWithNoOrchestrationFolder_Present(t *testing.T) {
	// The logger must work when no Orchestration-{run_id} folder exists —
	// it must not require any run folder or Orchestration.md.
	workDir := t.TempDir()

	// Confirm no Orchestration-* folder exists as a precondition.
	entries, _ := os.ReadDir(workDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "Orchestration-") {
			t.Fatalf("test precondition: found unexpected %q in workDir", e.Name())
		}
	}

	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "logged before any Orchestration folder")
	logger.Close()

	if p := logger.Path(); p == "" {
		t.Error("Path() is empty; log file must be created even without an Orchestration folder")
	}
}

func TestLogger_LogFile_IsInsideRunnerLogs_NotOrchestrationFolder(t *testing.T) {
	// The log file must be somewhere under RunnerLogs/ (its run subfolder,
	// under the new layout), never inside any Orchestration-{run_id} folder.
	workDir := t.TempDir()

	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "hello")
	logger.Close()

	logPath := logger.Path()
	if logPath == "" {
		t.Fatal("log file not created")
	}

	// The log path must not contain any "Orchestration-" segment.
	if strings.Contains(logPath, "Orchestration-") {
		t.Errorf("log file is inside an Orchestration-* folder: %q", logPath)
	}
	// The log path must be somewhere under RunnerLogs/.
	logsRoot := filepath.Join(workDir, LogsFolderName)
	rel, err := filepath.Rel(logsRoot, logPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("log file %q must be located under %q", logPath, logsRoot)
	}
}

func TestLogger_CreatesNothingOutsideRunnerLogs_InWorkDir(t *testing.T) {
	// After logging, no new items must exist outside RunnerLogs/ in the working
	// directory. The logger must create exactly RunnerLogs/ and nothing else.
	workDir := t.TempDir()

	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "entry one")
	logger.Log(domain.EventSessionDispatchStart, "entry two")
	logger.Close()

	entries, err := os.ReadDir(workDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", workDir, err)
	}
	for _, e := range entries {
		if e.Name() != LogsFolderName {
			t.Errorf("unexpected item %q in working directory; only %q should be created by the logger",
				e.Name(), LogsFolderName)
		}
	}
}

func TestLogger_NeverCreatesInsideOrchestrationFolder(t *testing.T) {
	// Even when an Orchestration-{run_id} folder already exists in workDir,
	// the logger must never write anything inside it.
	workDir := t.TempDir()

	orchFolder := filepath.Join(workDir, "Orchestration-"+validRunID)
	if err := os.MkdirAll(orchFolder, 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", orchFolder, err)
	}

	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "message")
	logger.Close()

	orchEntries, err := os.ReadDir(orchFolder)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", orchFolder, err)
	}
	if len(orchEntries) != 0 {
		t.Errorf("logger must not create files inside %q; found: %v", orchFolder, orchEntries)
	}
}

func TestLogger_LogFile_ExistsAfterLoggingWithoutOrchestrationMd(t *testing.T) {
	// The logger must create its file even when no Orchestration.md exists anywhere.
	workDir := t.TempDir()

	logger := New(workDir)
	logger.SetRunID(validRunID)
	logger.Log(domain.EventRunnerStart, "no orchestration.md needed")
	logger.Close()

	logPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+".log")
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file must exist at %q without Orchestration.md: %v", logPath, err)
	}
}

// ============================================================
// Concurrency (T4.6)
// ============================================================

func TestLogger_ConcurrentWrites_AllEntriesPresent(t *testing.T) {
	// Entries written from multiple goroutines must all appear in the log file.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 10
	const entriesEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				msg := "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
				logger.Log(domain.EventSessionDispatchStart, msg)
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)

	// Every message must appear exactly once.
	for g := 0; g < goroutines; g++ {
		for i := 0; i < entriesEach; i++ {
			want := "goroutine-" + strconv.Itoa(g) + "-entry-" + strconv.Itoa(i)
			if !strings.Contains(content, want) {
				t.Errorf("concurrent log: missing entry %q", want)
			}
		}
	}
}

func TestLogger_ConcurrentWrites_EntriesNotInterleaved(t *testing.T) {
	// Multi-line entries written concurrently must not be interleaved mid-entry.
	// Every begin-block must be followed by its matching end-block before any
	// other entry's header appears.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 5
	const entriesEach = 15

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				payload := "goroutine=" + strconv.Itoa(g) + "\nentry=" + strconv.Itoa(i) + "\npayload-end"
				logger.Log(domain.EventHarnessStdout, payload, domain.F("bytes", strconv.Itoa(len(payload))))
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)
	assertNoInterleavedBlocks(t, content)
}

func TestLogger_ConcurrentWrites_EntryCountMatchesExpected(t *testing.T) {
	// The total number of written entries must equal goroutines × entriesEach.
	// This guards against dropped writes under concurrency.
	workDir := t.TempDir()
	logger := New(workDir)
	logger.SetRunID(validRunID)

	const goroutines = 8
	const entriesEach = 10
	const total = goroutines * entriesEach

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				logger.Log(domain.EventSessionStepDone, "concurrent single-line entry")
			}
		}()
	}
	wg.Wait()
	logger.Close()

	content := readLogFile(t, logger)
	// Count occurrences of the event name in the file — one per entry.
	count := strings.Count(content, domain.EventSessionStepDone)
	if count != total {
		t.Errorf("concurrent writes: got %d entries, want %d", count, total)
	}
}

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
