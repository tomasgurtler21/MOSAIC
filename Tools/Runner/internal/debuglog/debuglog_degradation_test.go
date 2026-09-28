package debuglog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

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
