package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

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
