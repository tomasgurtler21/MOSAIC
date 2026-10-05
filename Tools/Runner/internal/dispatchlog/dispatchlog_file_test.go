package dispatchlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ============================================================
// Logger implements domain.DispatchLogger
// ============================================================

func TestLogger_ImplementsDispatchLogger(t *testing.T) {
	// *Logger must satisfy the domain.DispatchLogger interface.
	// This is a compile-time-only check: the nil pointer assignment fails
	// to compile if the method set is wrong, but performs no method calls
	// so the stub does not panic in RED phase.
	var _ domain.DispatchLogger = (*Logger)(nil)
}

// ============================================================
// File creation and naming
// ============================================================

func TestLogger_FileCreatedUnderRunnerLogs_WithRunID(t *testing.T) {
	// When SetRunID is called before the first log call, the log file must be
	// at RunnerLogs/{run_id}/{run_id}-dispatch.log under the supplied working
	// directory.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+"-dispatch.log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected log file at %q, but stat failed: %v", wantPath, err)
	}
}

func TestLogger_PathReturnsAbsoluteFilePath_WithRunID(t *testing.T) {
	// Path() must return the absolute path to the log file after the first write.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, validRunID, validRunID+"-dispatch.log")
	if gotPath := logger.Path(); gotPath != wantPath {
		t.Errorf("Path() = %q, want %q", gotPath, wantPath)
	}
}

func TestLogger_PathReturnsEmpty_BeforeFirstLog(t *testing.T) {
	// Path() must return "" before any log call (lazy file creation contract).
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	if p := logger.Path(); p != "" {
		t.Errorf("Path() before first log call = %q, want empty string", p)
	}
}

func TestLogger_NoFileCreated_BeforeFirstLog(t *testing.T) {
	// RunnerLogs/ must not be created until the first log call.
	workDir := t.TempDir()
	logger := New(workDir)
	defer logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("RunnerLogs/ must not exist before first log call; stat(%q) err = %v", logDir, err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogRequest(t *testing.T) {
	// LogRequest must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogRequest: %v", err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogResponse(t *testing.T) {
	// LogResponse must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogResponse(sampleResponse())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogResponse: %v", err)
	}
}

func TestLogger_LazyCreation_TriggeredByLogError(t *testing.T) {
	// LogError must trigger lazy file creation.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogError("agent#1", "something went wrong")
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("RunnerLogs/ must be created after LogError: %v", err)
	}
}

func TestLogger_FallbackFileName_StartsWithStartup_WhenNoRunID(t *testing.T) {
	// When no valid SetRunID is called before the first log call, the file name must
	// start with "startup-" so it cannot collide with a valid run_id name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created in RunnerLogs/ after first log call")
	}

	name := entries[0].Name()
	if !strings.HasPrefix(name, "startup-") {
		t.Errorf("fallback filename = %q, want prefix \"startup-\"", name)
	}
}

func TestLogger_FallbackFileName_HasDispatchSuffix_WhenNoRunID(t *testing.T) {
	// The fallback filename must carry the "-dispatch" suffix so it is
	// distinguishable from debuglog's own out-of-run fallback filename, which
	// uses the plain "startup-{timestamp}.log" form.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created in RunnerLogs/ after first log call")
	}

	name := entries[0].Name()
	if !strings.HasSuffix(name, "-dispatch.log") {
		t.Errorf("fallback filename = %q, want suffix \"-dispatch.log\"", name)
	}
}

func TestLogger_FallbackFileName_IsNotAValidRunID(t *testing.T) {
	// The fallback filename base (without the -dispatch.log suffix) must not
	// satisfy domain.IsValidRunID, keeping the two naming schemes distinguishable.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.Close()

	logDir := filepath.Join(workDir, LogsFolderName)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", logDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file created")
	}

	baseName := strings.TrimSuffix(entries[0].Name(), "-dispatch.log")
	if domain.IsValidRunID(baseName) {
		t.Errorf("fallback filename base %q must not satisfy IsValidRunID, but it does", baseName)
	}
}

func TestLogger_SetRunID_CalledAfterFirstLog_DoesNotRenameFile(t *testing.T) {
	// When SetRunID is called after the first log call has already written,
	// the file name must remain unchanged (fallback name).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	pathBefore := logger.Path()
	if pathBefore == "" {
		t.Fatal("Path() empty after first log call; expected a file to be created")
	}

	logger.SetRunID(validRunID)
	pathAfter := logger.Path()

	if pathBefore != pathAfter {
		t.Errorf("Path() changed after SetRunID: before=%q, after=%q", pathBefore, pathAfter)
	}
}

func TestLogger_SetRunID_CalledAfterFirstLog_WritesCorrelationEntry(t *testing.T) {
	// When SetRunID is called after the first entry, a correlation entry
	// containing the run_id must be appended so the file can be tied to its run.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
	logger.SetRunID(validRunID)
	logger.Close()

	lines := readLogLines(t, logger)

	foundCorrelation := false
	for _, line := range lines {
		m := unmarshalLine(t, line)
		if m["type"] == "correlation" {
			if rid, ok := m["run_id"].(string); ok && rid == validRunID {
				foundCorrelation = true
			}
		}
	}
	if !foundCorrelation {
		t.Errorf("log file must contain a correlation entry with run_id %q\nlines: %v", validRunID, lines)
	}
}

func TestLogger_SetRunID_EmptyRunID_IsIgnored(t *testing.T) {
	// An empty run_id must be silently ignored; the next log call must use the fallback name.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID("")
	logger.LogRequest(sampleRequest())
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
	logger.LogRequest(sampleRequest())
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
	// A second valid call before the first log must be ignored.
	workDir := t.TempDir()
	logger := New(workDir)

	firstID := validRunID
	logger.SetRunID(firstID)
	logger.SetRunID("20260101T000000Z-ffff") // second call — must be ignored
	logger.LogRequest(sampleRequest())
	logger.Close()

	wantPath := filepath.Join(workDir, LogsFolderName, firstID, firstID+"-dispatch.log")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected file named after first run_id %q, stat failed: %v", wantPath, err)
	}
}

func TestLogger_RunnerLogsFolderCreatedDirectlyUnderWorkDir(t *testing.T) {
	// RunnerLogs/ must be a direct child of the supplied working directory
	// (the fallback-name case: no run subfolder is involved).
	workDir := t.TempDir()
	logger := New(workDir)

	logger.LogRequest(sampleRequest())
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

func TestLogger_NoDispatchLogsFolderEverCreated(t *testing.T) {
	// The retired DispatchLogs/ folder name must never be created by any code
	// path in this package, whether or not a run_id is known at first write.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	if _, err := os.Stat(filepath.Join(workDir, "DispatchLogs")); !os.IsNotExist(err) {
		t.Errorf("DispatchLogs/ must never be created; stat err = %v", err)
	}
}

func TestLogger_RunSubfolderNestsInsideRunnerLogs(t *testing.T) {
	// The run_id subfolder must nest one level inside RunnerLogs/, not sit
	// directly at the RunnerLogs/ root.
	workDir := t.TempDir()
	logger := New(workDir)

	logger.SetRunID(validRunID)
	logger.LogRequest(sampleRequest())
	logger.Close()

	runDir := filepath.Join(workDir, LogsFolderName, validRunID)
	fi, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("run subfolder not created at %q: %v", runDir, err)
	}
	if !fi.IsDir() {
		t.Errorf("%q must be a directory", runDir)
	}
}
