package logging_test

// Tests for log directory auto-creation and write-failure tolerance (T15.5).
//
// The logger must:
//   - Create the MosaicDeploy/logs directory if it does not exist before the first write.
//   - Never propagate a sink write error to the caller or panic on any Logger method.
//   - Accumulate every write failure in Degraded() so the run summary can report them.
//
// Shared helpers (makeLogRoot, sampleRunContext, sampleEvent) are defined in dual_sink_test.go.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// ---------------------------------------------------------------------------
// T15.5: Log directory creation
// ---------------------------------------------------------------------------

// TestLogger_CreatesLogDirectoryIfAbsent verifies that when MosaicDeploy/logs/ does not yet
// exist, the logger creates it before writing so no manual setup is required.
func TestLogger_CreatesLogDirectoryIfAbsent(t *testing.T) {
	root := makeLogRoot(t)
	logDir := filepath.Join(root, "MosaicDeploy", "logs")

	// Confirm the directory does not exist yet.
	if _, err := os.Stat(logDir); err == nil {
		t.Fatalf("precondition violated: log directory %q already exists before test", logDir)
	}

	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-mkdir-test"))
	log.EndRun(domain.OutcomeSuccess)

	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("log directory %q was not created after StartRun: %v", logDir, err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "latest.log")); err != nil {
		t.Errorf("latest.log was not created under %q: %v", logDir, err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "history.log")); err != nil {
		t.Errorf("history.log was not created under %q: %v", logDir, err)
	}
}

// TestLogger_IntermediateDirectoriesCreated verifies that the full MosaicDeploy/logs path is
// created even when neither MosaicDeploy nor logs exist yet.
func TestLogger_IntermediateDirectoriesCreated(t *testing.T) {
	root := makeLogRoot(t)
	// Confirm no MosaicDeploy directory exists.
	mosaicDeployDir := filepath.Join(root, "MosaicDeploy")
	if _, err := os.Stat(mosaicDeployDir); err == nil {
		t.Fatalf("precondition violated: %q already exists", mosaicDeployDir)
	}

	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-deep-mkdir"))
	log.EndRun(domain.OutcomeSuccess)

	logDir := filepath.Join(root, "MosaicDeploy", "logs")
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("intermediate directories not created; log directory %q missing: %v", logDir, err)
	}
}

// ---------------------------------------------------------------------------
// T15.5: Write-failure tolerance (AC15.4)
// ---------------------------------------------------------------------------

// blockLogDir places a regular file at the path that would be the log directory, causing any
// subsequent attempt to create the directory to fail.
func blockLogDir(t *testing.T, root string) {
	t.Helper()
	logParent := filepath.Join(root, "MosaicDeploy")
	if err := os.MkdirAll(logParent, 0o755); err != nil {
		t.Fatalf("blockLogDir: create parent: %v", err)
	}
	logDirPath := filepath.Join(logParent, "logs")
	if err := os.WriteFile(logDirPath, []byte("I am a file, not a directory"), 0o644); err != nil {
		t.Fatalf("blockLogDir: write blocking file: %v", err)
	}
}

// TestLogger_WriteFailureAccumulatesInDegraded verifies that when the log directory cannot be
// created (because a file occupies that path), the write failure appears in Degraded() rather
// than being returned or panicked.
func TestLogger_WriteFailureAccumulatesInDegraded(t *testing.T) {
	root := makeLogRoot(t)
	blockLogDir(t, root)

	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-unwritable"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) == 0 {
		t.Error("Degraded() is empty after write failure; want at least one accumulated error")
	}
}

// TestLogger_WriteFailureDoesNotPanicOrReturn verifies that all Logger methods can be called
// without panicking when the underlying sinks are broken (AC15.4).
func TestLogger_WriteFailureDoesNotPanicOrReturn(t *testing.T) {
	root := makeLogRoot(t)
	blockLogDir(t, root)

	log := logging.New(root, config.ToolConfig{})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Logger method panicked with unavailable sinks: %v", r)
		}
	}()

	log.StartRun(sampleRunContext("run-no-panic"))
	log.Event(sampleEvent("some-artifact"))
	log.Action(domain.ActionRecord{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "surviving-agent"},
		TargetPath: "/workspace/agents/surviving-agent.md",
		Taken:      domain.TakenCreated,
	})
	log.EndRun(domain.OutcomeCompletedWithGaps)

	// The deployment completes; the failure is visible via Degraded, not via a panic or return.
	degraded := log.Degraded()
	if len(degraded) == 0 {
		t.Error("Degraded() is empty after write failure; expected accumulated errors")
	}
}

// TestLogger_DegradedIsEmptyOnSuccessfulRun verifies that Degraded() returns an empty slice
// when both sinks write successfully.
func TestLogger_DegradedIsEmptyOnSuccessfulRun(t *testing.T) {
	root := makeLogRoot(t)
	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-healthy"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) != 0 {
		t.Errorf("Degraded() has %d error(s) on a successful run; want 0", len(degraded))
	}
}

// TestLogger_DegradedAccumulatesAcrossMultipleMethods verifies that write failures triggered
// by multiple method calls (StartRun, Event, EndRun) are all accumulated rather than only
// the first failure being recorded. The test calls StartRun, two Event calls, and EndRun, so
// at least two distinct failures are expected — one per write attempt to either sink.
func TestLogger_DegradedAccumulatesAcrossMultipleMethods(t *testing.T) {
	root := makeLogRoot(t)
	blockLogDir(t, root)

	log := logging.New(root, config.ToolConfig{})
	log.StartRun(sampleRunContext("run-multi-method-fail"))
	log.Event(sampleEvent("artifact-a"))
	log.Event(sampleEvent("artifact-b"))
	log.EndRun(domain.OutcomeSuccess)

	degraded := log.Degraded()
	if len(degraded) < 2 {
		t.Errorf("Degraded() has %d error(s) after StartRun + 2 Event + EndRun with broken sinks; want at least 2 accumulated errors", len(degraded))
	}
}
