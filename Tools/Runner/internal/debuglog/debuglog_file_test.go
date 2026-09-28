package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

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
