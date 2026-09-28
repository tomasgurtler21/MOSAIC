package session_test

// Tests for the CLI-harness copy-and-invoke snapshot integration: agent
// definition paths resolve to the run-scoped snapshot directory, and the
// snapshot is deleted on every terminal outcome (FR-21).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_Start_CLIHarness_AgentDefinitionPathsInSnapshot verifies that
// after step 5a creates the snapshot, every harness invocation receives an
// AgentReference whose DefinitionPath is inside the snapshot directory, not
// the original agents directory.
func TestSession_Start_CLIHarness_AgentDefinitionPathsInSnapshot(t *testing.T) {
	const runID = "testsnap-paths-01"
	_, orchPath, _ := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got none")
	}
	for _, inv := range invs {
		if !containsSnapshotPathSegment(inv.Agent.DefinitionPath, runID) {
			t.Errorf("agent %q: DefinitionPath %q does not contain snapshot path segment %q; "+
				"agents must be re-resolved from the snapshot directory after step 5a",
				inv.Agent.Identifier, inv.Agent.DefinitionPath, "agents-runner-"+runID)
		}
	}
}

// TestSession_Start_CLIHarness_SnapshotDeletedOnRunCompleted verifies that
// when a CLI-harness run completes (RunCompleted), the run-scoped snapshot
// directory created at step 5a is deleted.
func TestSession_Start_CLIHarness_SnapshotDeletedOnRunCompleted(t *testing.T) {
	const runID = "testsnap-cleanup-complete-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Agents must have been dispatched from inside the snapshot directory,
	// confirming the snapshot was actually created during the run.
	for _, inv := range f.Invocations() {
		if !containsSnapshotPathSegment(inv.Agent.DefinitionPath, runID) {
			t.Errorf("agent %q: DefinitionPath %q does not contain snapshot path segment "+
				"(snapshot was not created or step 5a did not run)",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
	}

	// After RunCompleted, the snapshot directory must be deleted.
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted after RunCompleted, stat returned: %v",
			snapshotDir, statErr)
	}
}

// TestSession_Start_CLIHarness_SnapshotDeletedOnRunStopped verifies that
// when a CLI-harness run is gracefully stopped (RunStopped), the snapshot
// directory is also deleted (cleanup applies to RunStopped as well as
// RunCompleted).
func TestSession_Start_CLIHarness_SnapshotDeletedOnRunStopped(t *testing.T) {
	const runID = "testsnap-cleanup-stop-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after agent-a finishes to trigger a graceful stop.
	// Only agent-a is queued; the session stops after agent-a's dispatch.
	cbHarness := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				cancel()
			}
		},
	}
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  cbHarness,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(ctx, baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunStopped)

	// The dispatched invocation must have used the snapshot directory,
	// confirming the snapshot was created before dispatching began.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one invocation before graceful stop, got none")
	}
	if !containsSnapshotPathSegment(invs[0].Agent.DefinitionPath, runID) {
		t.Errorf("agent %q: DefinitionPath %q does not contain snapshot path segment "+
			"(step 5a did not run or re-resolved agents from original dir)",
			invs[0].Agent.Identifier, invs[0].Agent.DefinitionPath)
	}

	// Snapshot directory must also be deleted on RunStopped.
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted after RunStopped, stat returned: %v",
			snapshotDir, statErr)
	}
}

// TestSession_Start_CLIHarness_SnapshotRecreatedOnPreExisting verifies
// that when the snapshot directory already exists at the expected path
// (simulating a stale artifact from a prior run), NewSnapshot removes it
// and recreates it fresh from source files. The run proceeds normally with
// RunCompleted (not refused).
func TestSession_Start_CLIHarness_SnapshotRecreatedOnPreExisting(t *testing.T) {
	const runID = "testsnap-collision-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	// Pre-create the snapshot directory to simulate a stale snapshot from a
	// prior run. NewSnapshot should remove and recreate it.
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("pre-create snapshot dir: %v", err)
	}

	// Write a marker file to the pre-existing snapshot directory so we can
	// verify it was actually removed and recreated.
	markerFile := filepath.Join(snapshotDir, "marker.txt")
	if err := os.WriteFile(markerFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	f := harness.NewMockAdapter()
	// Script both agents to return SUCCESS so the workflow completes.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "snapshot recreated successfully",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "workflow complete",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// The snapshot directory should be deleted (terminal outcome cleanup).
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q should be deleted after terminal outcome (RunCompleted)", snapshotDir)
	}

	// The marker file should NOT exist either (confirming both the original
	// snapshot was removed and the new one was cleaned up).
	if _, statErr := os.Stat(markerFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("marker file %q should not exist after cleanup", markerFile)
	}
}

// TestSession_Start_CLIHarness_CleanupFailureIsNonFatal verifies that when
// cleanup of the snapshot directory fails, Start still returns RunCompleted
// with a nil error, and the failure is recorded as an EventSnapshotCleanupFailed
// debug log entry instead of being surfaced as a returned error or a changed
// run outcome.
//
// To force cleanup to fail the test holds the snapshot directory open while
// session.Start runs:
//   - On Windows: an open file handle inside the directory prevents os.RemoveAll.
//   - On Linux/Mac: making the parent directory read-only (0o555) prevents
//     os.Remove from unlinking the snapshot directory.
//
// Both mechanisms are applied so the test is reliably cross-platform.
func TestSession_Start_CLIHarness_CleanupFailureIsNonFatal(t *testing.T) {
	const runID = "testsnap-cleanup-nonfatal-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)
	parentDir := filepath.Dir(snapshotDir)

	logger := &sessionRecordingLogger{}
	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// openedFile holds an open handle to a file inside the snapshot directory.
	// It is set inside onInvoke (after step 5a creates the snapshot) and kept
	// open while session.Start's deferred cleanup runs so that os.RemoveAll
	// fails on Windows. The handle is closed after ses.Start returns so that
	// t.TempDir can remove the directory in test cleanup.
	var openedFile *os.File

	// t.Cleanup ensures permissions and the file handle are restored even if
	// the test fails before the explicit restore below.
	t.Cleanup(func() {
		if openedFile != nil {
			openedFile.Close()
		}
		// Restore the parent directory so t.TempDir cleanup can remove it.
		os.Chmod(parentDir, 0o755) //nolint:errcheck
	})

	cbHarness := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			// Only arm the failure mechanism once, after the first dispatch.
			// By the time onInvoke fires, step 5a has already created the
			// snapshot directory, so the directory and its contents exist.
			if agentID != "agent-a" {
				return
			}
			// Windows: open a file inside the snapshot dir to lock it.
			if fh, err := os.Open(filepath.Join(snapshotDir, "agent-a.md")); err == nil {
				openedFile = fh
			}
			// Linux/Mac: make the parent directory read-only so os.Remove
			// cannot unlink the snapshot directory from it.
			os.Chmod(parentDir, 0o555) //nolint:errcheck
		},
	}

	ses := session.New(session.Deps{
		Harness:  cbHarness,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))

	// Restore access before any further assertions so that the test directory
	// can be cleaned up by t.TempDir regardless of outcome.
	if openedFile != nil {
		openedFile.Close()
		openedFile = nil
	}
	os.Chmod(parentDir, 0o755) //nolint:errcheck

	// Cleanup failure must not surface as a returned error.
	if err != nil {
		t.Fatalf("want nil error (cleanup failure must be non-fatal), got %v", err)
	}
	// Cleanup failure must not change the run outcome.
	if got.Status != domain.RunCompleted {
		t.Errorf("want RunCompleted (cleanup failure must not change run outcome), got %q (message: %q)",
			got.Status, got.Message)
	}
	// Cleanup failure must be recorded as a debug log event.
	if !logger.eventLogged(domain.EventSnapshotCleanupFailed) {
		t.Error("want EventSnapshotCleanupFailed debug event logged when snapshot cleanup fails, " +
			"but the event was not found in the debug log; " +
			"the implementation must log cleanup failures rather than silently ignore or surface them")
	}
}

// TestSession_Start_CLIHarness_OrchestratorRefPointsIntoSnapshot verifies that
// step 5a re-resolves the orchestrator reference from the snapshot directory
// and re-binds consultants with the snapshot-resolved reference. After Start
// returns, any consultant that implements domain.RunContextBinder must have
// received an orchRef whose DefinitionPath is inside the snapshot directory
// (not the original agents directory), with InvocationKind ==
// InvocationOrchestrator.
//
// This test wires an orchRefCaptureConsultant as the Routing dependency. The
// consultant implements RunContextBinder so it captures every orchRef the
// session hands to it. The last captured orchRef -- after step 5a runs the
// second bindRunContext call -- must contain the snapshot path segment.
func TestSession_Start_CLIHarness_OrchestratorRefPointsIntoSnapshot(t *testing.T) {
	const runID = "testsnap-orchref-01"
	_, orchPath, _ := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// capture records every BindRunContext call the session makes so we can
	// inspect the orchRef that step 5a supplies after re-resolving from the
	// snapshot directory.
	capture := &orchRefCaptureConsultant{}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  capture,
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Step 5a must call bindRunContext a second time (after re-resolving from
	// the snapshot dir). The first call happens before step 5a with the
	// original orchRef; the second call inside step 5a supplies the snapshot
	// orchRef. We expect at least 2 bindings.
	if capture.bindCallCount() < 2 {
		t.Fatalf("want BindRunContext called at least twice (once before step 5a, "+
			"once after re-resolution), got %d call(s); step 5a may have omitted "+
			"the re-bind of consultants with the snapshot orchestrator reference",
			capture.bindCallCount())
	}

	// The last-bound orchRef must point into the snapshot directory.
	orchRef := capture.lastBoundOrchRef()
	if !containsSnapshotPathSegment(orchRef.DefinitionPath, runID) {
		t.Errorf("orchRef.DefinitionPath %q does not contain snapshot path segment %q; "+
			"step 5a must re-resolve the orchestrator from the snapshot directory and "+
			"re-bind consultants so that consultation uses the snapshot copy of the "+
			"orchestrator script rather than the original",
			orchRef.DefinitionPath, "agents-runner-"+runID)
	}
	// The re-resolved orchRef must retain InvocationOrchestrator kind.
	if orchRef.InvocationKind != domain.InvocationOrchestrator {
		t.Errorf("orchRef.InvocationKind: want %q, got %q; "+
			"ResolveOrchestrator must set InvocationKind to InvocationOrchestrator",
			domain.InvocationOrchestrator, orchRef.InvocationKind)
	}
}

// TestSession_Start_CLIHarness_SnapshotDeletedOnDeviationUnresolved verifies
// that when Start returns RunDeviationUnresolved (a terminal outcome under
// FR-21), the run-scoped copy-and-invoke snapshot directory is deleted.
//
// All six RunStatus values are terminal (FR-21). The deviation is produced by
// having agent-a return PARTIALLY_DONE in the linear workflow (which has no
// On Findings column), with no routing consultant wired.
func TestSession_Start_CLIHarness_SnapshotDeletedOnDeviationUnresolved(t *testing.T) {
	const runID = "testsnap-nonterminal-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	// agent-a returns PARTIALLY_DONE; the linear workflow has On Findings "-",
	// so the engine cannot route automatically. Without a routing consultant,
	// the session returns RunDeviationUnresolved.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "partially done, needs more work",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// No Routing consultant wired: deviation terminates with RunDeviationUnresolved.
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunDeviationUnresolved)

	// Step 5b must have run and agents must have been dispatched from the
	// snapshot directory. This confirms the snapshot was actually created.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got none")
	}
	if !containsSnapshotPathSegment(invs[0].Agent.DefinitionPath, runID) {
		t.Errorf("agent %q: DefinitionPath %q does not contain snapshot path segment %q; "+
			"step 5b must run before the first dispatch so that agent files are "+
			"resolved from the snapshot directory",
			invs[0].Agent.Identifier, invs[0].Agent.DefinitionPath, "agents-runner-"+runID)
	}

	// The snapshot directory must be deleted after RunDeviationUnresolved
	// (FR-21: all six terminal outcomes trigger copy-and-invoke cleanup).
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted when Start returns "+
			"RunDeviationUnresolved (FR-21: all six terminal outcomes trigger cleanup); "+
			"stat returned: %v", snapshotDir, statErr)
	}
}

// TestSession_Start_NonCLIHarness_SnapshotStepSkipped verifies that when
// HarnessID is empty (not a known CLI harness), the snapshot step is skipped:
// agents are resolved from the original agents directory and no snapshot path
// segment appears in any DefinitionPath.
func TestSession_Start_NonCLIHarness_SnapshotStepSkipped(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	// HarnessID is "" (default from newLinearSession) — not a CLI harness.
	cfg := baseLinearConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Agents must NOT have been dispatched from a snapshot directory.
	for _, inv := range f.Invocations() {
		if strings.Contains(filepath.ToSlash(inv.Agent.DefinitionPath), "agents-runner-") {
			t.Errorf("agent %q: DefinitionPath %q contains snapshot path segment but no "+
				"snapshot should be created when HarnessID is not a CLI harness",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
	}
}
