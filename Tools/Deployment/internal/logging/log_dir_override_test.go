package logging_test

// Tests for the log-destination override on the Logger (T7.1, T7.2, T7.4, T7.5).
//
// These tests reference logging.WithLogDir and the variadic logging.New signature,
// neither of which exist yet. They will not compile until the implementation adds
// them — that is the expected TDD RED state.
//
// Behaviours asserted:
//
//   T7.1 — When WithLogDir supplies a destination, both sinks (latest.log and
//           history.log) are written there. The default MosaicDeploy/logs location
//           is not created and not written to.
//
//   T7.2 — When no destination is supplied, both sinks are written to the default
//           location (<mosaicRoot>/MosaicDeploy/logs) with the same
//           truncate-at-StartRun and append-history behaviour as today. Backward
//           compatibility is absolute: an invocation that passes no new option must
//           behave byte-for-byte as it does today.
//
//   T7.4 — A supplied destination directory is created on demand when it does not
//           exist. When the directory cannot be created (a file occupies the path),
//           write failures accumulate in Degraded rather than panicking or returning.
//
//   T7.5 — Two loggers with distinct destinations do not share files. One logger's
//           truncation of its own latest.log leaves the other's intact.
//
// Shared helpers (makeLogRoot, sampleRunContext, sampleEvent, readSinkFile) are
// defined in dual_sink_test.go in this same package.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// ---------------------------------------------------------------------------
// T7.1: Supplied destination receives both sinks; default is untouched
// ---------------------------------------------------------------------------

// TestWithLogDir_BothSinksWrittenToSuppliedDir verifies that when WithLogDir
// supplies a destination directory, both latest.log and history.log are written
// there, and Paths() returns their locations under that directory.
func TestWithLogDir_BothSinksWrittenToSuppliedDir(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(overrideDir))
	log.StartRun(sampleRunContext("run-override-dir"))
	log.EndRun(domain.OutcomeSuccess)

	paths := log.Paths()
	wantLatest := filepath.Join(overrideDir, "latest.log")
	wantHistory := filepath.Join(overrideDir, "history.log")

	if paths.Latest != wantLatest {
		t.Errorf("Paths().Latest = %q; want %q (supplied destination)", paths.Latest, wantLatest)
	}
	if paths.History != wantHistory {
		t.Errorf("Paths().History = %q; want %q (supplied destination)", paths.History, wantHistory)
	}
	if _, err := os.Stat(wantLatest); err != nil {
		t.Errorf("latest.log not found under supplied dir %q: %v", overrideDir, err)
	}
	if _, err := os.Stat(wantHistory); err != nil {
		t.Errorf("history.log not found under supplied dir %q: %v", overrideDir, err)
	}
}

// TestWithLogDir_DefaultLocationUntouched verifies that when WithLogDir supplies a
// destination, the default MosaicDeploy/logs directory is not created or written to.
func TestWithLogDir_DefaultLocationUntouched(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()
	defaultLogDir := filepath.Join(root, "MosaicDeploy", "logs")

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(overrideDir))
	log.StartRun(sampleRunContext("run-default-untouched"))
	log.EndRun(domain.OutcomeSuccess)

	if _, err := os.Stat(defaultLogDir); err == nil {
		t.Errorf("default log directory %q was created even though WithLogDir was supplied; want it untouched",
			defaultLogDir)
	}
}

// TestWithLogDir_ContentAppearsInSuppliedDir verifies that log records written via
// StartRun appear in both sinks under the supplied destination.
func TestWithLogDir_ContentAppearsInSuppliedDir(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()
	runID := "run-content-check"

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(overrideDir))
	log.StartRun(sampleRunContext(runID))
	log.EndRun(domain.OutcomeSuccess)

	for _, name := range []string{"latest.log", "history.log"} {
		content := readSinkFile(t, filepath.Join(overrideDir, name))
		if !strings.Contains(content, runID) {
			t.Errorf("%s under supplied dir does not contain RunID %q", name, runID)
		}
	}
}

// TestWithLogDir_LatestTruncatedAtStartRun verifies that the truncate-at-StartRun
// behaviour is preserved when a destination is supplied: the second StartRun
// truncates latest.log in the override directory so only the second run's records
// remain.
func TestWithLogDir_LatestTruncatedAtStartRun(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()
	cfg := config.ToolConfig{}

	log1 := logging.New(root, cfg, logging.WithLogDir(overrideDir))
	rc1 := sampleRunContext("run-trunc-1st")
	log1.StartRun(rc1)
	log1.EndRun(domain.OutcomeSuccess)

	log2 := logging.New(root, cfg, logging.WithLogDir(overrideDir))
	rc2 := sampleRunContext("run-trunc-2nd")
	log2.StartRun(rc2)
	log2.EndRun(domain.OutcomeSuccess)

	content := readSinkFile(t, filepath.Join(overrideDir, "latest.log"))
	if strings.Contains(content, rc1.RunID) {
		t.Errorf("latest.log in override dir still contains first run %q after second run; want truncation at StartRun",
			rc1.RunID)
	}
	if !strings.Contains(content, rc2.RunID) {
		t.Errorf("latest.log in override dir missing second run %q", rc2.RunID)
	}
}

// TestWithLogDir_HistoryAppendsAcrossRuns verifies that the append-history behaviour
// is preserved when a destination is supplied: history.log in the override directory
// accumulates records from every run.
func TestWithLogDir_HistoryAppendsAcrossRuns(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()
	cfg := config.ToolConfig{}
	ids := []string{"run-hist-1", "run-hist-2"}

	for _, id := range ids {
		l := logging.New(root, cfg, logging.WithLogDir(overrideDir))
		l.StartRun(sampleRunContext(id))
		l.EndRun(domain.OutcomeSuccess)
	}

	content := readSinkFile(t, filepath.Join(overrideDir, "history.log"))
	for _, id := range ids {
		if !strings.Contains(content, id) {
			t.Errorf("history.log in override dir missing RunID %q; want all runs retained", id)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.2: No destination supplied → default behaviour is unchanged
// ---------------------------------------------------------------------------

// TestNoLogDir_DefaultPathsUsed verifies that when New is called without WithLogDir
// the two sink paths are the MosaicDeploy/logs locations documented as fixed by
// contract.
func TestNoLogDir_DefaultPathsUsed(t *testing.T) {
	root := makeLogRoot(t)

	log := logging.New(root, config.ToolConfig{})
	paths := log.Paths()

	wantLatest := filepath.Join(root, "MosaicDeploy", "logs", "latest.log")
	wantHistory := filepath.Join(root, "MosaicDeploy", "logs", "history.log")

	if paths.Latest != wantLatest {
		t.Errorf("Paths().Latest = %q; want default %q", paths.Latest, wantLatest)
	}
	if paths.History != wantHistory {
		t.Errorf("Paths().History = %q; want default %q", paths.History, wantHistory)
	}
}

// TestNoLogDir_LatestTruncatedAtStartRun verifies that the truncate-at-StartRun
// behaviour at the default location is intact when no override is supplied.
func TestNoLogDir_LatestTruncatedAtStartRun(t *testing.T) {
	root := makeLogRoot(t)
	cfg := config.ToolConfig{}

	log1 := logging.New(root, cfg)
	rc1 := sampleRunContext("run-default-1st")
	log1.StartRun(rc1)
	log1.EndRun(domain.OutcomeSuccess)

	log2 := logging.New(root, cfg)
	rc2 := sampleRunContext("run-default-2nd")
	log2.StartRun(rc2)
	log2.EndRun(domain.OutcomeSuccess)

	content := readSinkFile(t, log2.Paths().Latest)
	if strings.Contains(content, rc1.RunID) {
		t.Errorf("latest.log (default location) still contains first run %q after second run; want truncation", rc1.RunID)
	}
	if !strings.Contains(content, rc2.RunID) {
		t.Errorf("latest.log (default location) missing second run %q", rc2.RunID)
	}
}

// TestNoLogDir_HistoryAppendsAllRuns verifies that history.log at the default
// location accumulates runs when no override is supplied.
func TestNoLogDir_HistoryAppendsAllRuns(t *testing.T) {
	root := makeLogRoot(t)
	cfg := config.ToolConfig{}
	ids := []string{"run-default-hist-1", "run-default-hist-2"}

	for _, id := range ids {
		l := logging.New(root, cfg)
		l.StartRun(sampleRunContext(id))
		l.EndRun(domain.OutcomeSuccess)
	}

	last := logging.New(root, cfg)
	content := readSinkFile(t, last.Paths().History)
	for _, id := range ids {
		if !strings.Contains(content, id) {
			t.Errorf("history.log (default location) missing RunID %q; want all runs retained", id)
		}
	}
}

// TestEmptyStringWithLogDir_UsesDefault verifies that WithLogDir("") is equivalent
// to not supplying WithLogDir at all: the logger uses the default MosaicDeploy/logs
// location.
func TestEmptyStringWithLogDir_UsesDefault(t *testing.T) {
	root := makeLogRoot(t)

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(""))
	paths := log.Paths()

	wantLatest := filepath.Join(root, "MosaicDeploy", "logs", "latest.log")
	wantHistory := filepath.Join(root, "MosaicDeploy", "logs", "history.log")

	if paths.Latest != wantLatest {
		t.Errorf("WithLogDir(\"\") Paths().Latest = %q; want default %q", paths.Latest, wantLatest)
	}
	if paths.History != wantHistory {
		t.Errorf("WithLogDir(\"\") Paths().History = %q; want default %q", paths.History, wantHistory)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Directory created on demand; write failures accumulate as degradation
// ---------------------------------------------------------------------------

// TestWithLogDir_NonExistentDirCreatedOnDemand verifies that when the supplied
// destination directory does not yet exist, the logger creates it before the first
// write.
func TestWithLogDir_NonExistentDirCreatedOnDemand(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := filepath.Join(t.TempDir(), "new-subdir", "logs")

	if _, err := os.Stat(overrideDir); err == nil {
		t.Fatalf("precondition: override dir %q already exists", overrideDir)
	}

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(overrideDir))
	log.StartRun(sampleRunContext("run-create-dir"))
	log.EndRun(domain.OutcomeSuccess)

	if _, err := os.Stat(overrideDir); err != nil {
		t.Errorf("supplied destination dir %q was not created on demand: %v", overrideDir, err)
	}
	if _, err := os.Stat(filepath.Join(overrideDir, "latest.log")); err != nil {
		t.Errorf("latest.log not found after on-demand dir creation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(overrideDir, "history.log")); err != nil {
		t.Errorf("history.log not found after on-demand dir creation: %v", err)
	}
}

// TestWithLogDir_BlockedDir_DegradedNotPanicked verifies that when the supplied
// destination directory cannot be created (a regular file occupies the path), write
// failures accumulate in Degraded rather than panicking or being returned.
func TestWithLogDir_BlockedDir_DegradedNotPanicked(t *testing.T) {
	root := makeLogRoot(t)
	parent := t.TempDir()
	blockedDir := filepath.Join(parent, "blocked-logs")
	if err := os.WriteFile(blockedDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("setup: could not create blocking file: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Logger panicked on blocked supplied destination: %v", r)
		}
	}()

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(blockedDir))
	log.StartRun(sampleRunContext("run-blocked-dir"))
	log.Event(sampleEvent("artifact-x"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) == 0 {
		t.Error("Degraded() is empty after write failure against blocked supplied destination; want accumulated errors")
	}
}

// TestWithLogDir_BlockedDir_AllMethodsAccumulate verifies that every Logger method
// accumulates failures into Degraded when the supplied directory is unwritable, not
// just the first call.
func TestWithLogDir_BlockedDir_AllMethodsAccumulate(t *testing.T) {
	root := makeLogRoot(t)
	parent := t.TempDir()
	blockedDir := filepath.Join(parent, "blocked-all")
	if err := os.WriteFile(blockedDir, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(blockedDir))
	log.StartRun(sampleRunContext("run-blocked-all"))
	log.Event(sampleEvent("a"))
	log.Event(sampleEvent("b"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) < 2 {
		t.Errorf("Degraded() has %d error(s) after StartRun + 2 Events + EndRun on blocked supplied destination; want at least 2",
			len(degraded))
	}
}

// TestWithLogDir_DegradedEmptyOnSuccessfulWrite verifies that Degraded() is empty
// when a supplied destination directory is writable and all writes succeed.
func TestWithLogDir_DegradedEmptyOnSuccessfulWrite(t *testing.T) {
	root := makeLogRoot(t)
	overrideDir := t.TempDir()

	log := logging.New(root, config.ToolConfig{}, logging.WithLogDir(overrideDir))
	log.StartRun(sampleRunContext("run-clean-write"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) != 0 {
		t.Errorf("Degraded() has %d error(s) on a successful write to supplied destination; want 0",
			len(degraded))
	}
}

// ---------------------------------------------------------------------------
// T7.5: Two loggers with distinct destinations stay isolated
// ---------------------------------------------------------------------------

// TestDistinctLogDirs_LatestTruncationDoesNotCrossLoggers verifies that calling
// StartRun on a second logger (which truncates its own latest.log) leaves the first
// logger's latest.log intact. One logger's truncation must not reach another's file.
func TestDistinctLogDirs_LatestTruncationDoesNotCrossLoggers(t *testing.T) {
	root := makeLogRoot(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	runID1 := "run-isolated-1"
	runID2 := "run-isolated-2"

	log1 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir1))
	log1.StartRun(sampleRunContext(runID1))
	log1.EndRun(domain.OutcomeSuccess)

	// log2 truncates its own latest.log at StartRun; log1's latest.log must be unaffected.
	log2 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir2))
	log2.StartRun(sampleRunContext(runID2))
	log2.EndRun(domain.OutcomeSuccess)

	content1 := readSinkFile(t, filepath.Join(dir1, "latest.log"))
	if !strings.Contains(content1, runID1) {
		t.Errorf("first logger's latest.log lost RunID %q after second logger's StartRun; "+
			"truncation must be contained to the logger's own destination", runID1)
	}
}

// TestDistinctLogDirs_HistoryDoesNotCrossLoggers verifies that appending to a
// second logger's history.log does not affect the first logger's history.log.
func TestDistinctLogDirs_HistoryDoesNotCrossLoggers(t *testing.T) {
	root := makeLogRoot(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	runID1 := "run-hist-iso-1"
	runID2 := "run-hist-iso-2"

	log1 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir1))
	log1.StartRun(sampleRunContext(runID1))
	log1.EndRun(domain.OutcomeSuccess)

	log2 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir2))
	log2.StartRun(sampleRunContext(runID2))
	log2.EndRun(domain.OutcomeSuccess)

	// Log 1's history must contain only log1's entries.
	content1 := readSinkFile(t, filepath.Join(dir1, "history.log"))
	if strings.Contains(content1, runID2) {
		t.Errorf("first logger's history.log contains second logger's RunID %q; "+
			"sinks must be strictly per-destination", runID2)
	}

	// Log 2's history must contain only log2's entries.
	content2 := readSinkFile(t, filepath.Join(dir2, "history.log"))
	if strings.Contains(content2, runID1) {
		t.Errorf("second logger's history.log contains first logger's RunID %q; "+
			"sinks must be strictly per-destination", runID1)
	}
}

// TestDistinctLogDirs_PathsAreNonOverlapping verifies that two loggers constructed
// with distinct destinations report distinct, non-overlapping paths from Paths().
func TestDistinctLogDirs_PathsAreNonOverlapping(t *testing.T) {
	root := makeLogRoot(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	log1 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir1))
	log2 := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dir2))

	paths1 := log1.Paths()
	paths2 := log2.Paths()

	if paths1.Latest == paths2.Latest {
		t.Errorf("two loggers with distinct destinations report the same Latest path %q; paths must differ",
			paths1.Latest)
	}
	if paths1.History == paths2.History {
		t.Errorf("two loggers with distinct destinations report the same History path %q; paths must differ",
			paths1.History)
	}
}

// TestDistinctLogDirs_ThreeLoggers_AllIsolated verifies isolation across three
// loggers running against three distinct destinations. Each logger's latest.log
// must contain only its own run's content.
func TestDistinctLogDirs_ThreeLoggers_AllIsolated(t *testing.T) {
	root := makeLogRoot(t)
	ids := []string{"run-trio-1", "run-trio-2", "run-trio-3"}
	dirs := []string{t.TempDir(), t.TempDir(), t.TempDir()}

	for i, id := range ids {
		l := logging.New(root, config.ToolConfig{}, logging.WithLogDir(dirs[i]))
		l.StartRun(sampleRunContext(id))
		l.EndRun(domain.OutcomeSuccess)
	}

	for i, id := range ids {
		content := readSinkFile(t, filepath.Join(dirs[i], "latest.log"))
		if !strings.Contains(content, id) {
			t.Errorf("logger %d latest.log missing its own RunID %q", i+1, id)
		}
		for j, otherId := range ids {
			if j == i {
				continue
			}
			if strings.Contains(content, otherId) {
				t.Errorf("logger %d latest.log unexpectedly contains RunID %q from logger %d; "+
					"destinations must be strictly isolated", i+1, otherId, j+1)
			}
		}
	}
}
