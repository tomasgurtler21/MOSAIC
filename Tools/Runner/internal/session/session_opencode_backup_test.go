package session_test

// Tests for the OpenCode backup-and-transform cleanup: BackupState.Cleanup
// (last-out restore and backup-dir deletion) must run on every terminal
// outcome (FR-21), and cleanup failures must be non-fatal (FR-22).

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

	"mosaic-run/internal/snapshot/lockprotocol"
)

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunCompleted verifies
// that BackupState.Cleanup (last-out check) runs when the run completes
// successfully: originals are restored and the backup directory is deleted.
//
// RED signal: at dispatch time, the ORIGINAL agent file (in the original agents
// dir, not a snapshot copy) must have mode=primary. This only holds when
// backup-and-transform applies transforms IN-PLACE. With copy-and-invoke (the
// current implementation), the original is never touched so it stays mode=subagent.
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunCompleted(t *testing.T) {
	const runID = "oc-cleanup-complete-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	var originalContentAtDispatch []byte
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
	cb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				originalContentAtDispatch, _ = os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:  cb,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// The ORIGINAL file must have been in mode=primary at dispatch time:
	// backup-and-transform applies transforms IN-PLACE, so the original agents
	// dir is modified (not a snapshot copy). With copy-and-invoke, the original
	// stays mode=subagent and this assertion fails RED.
	if !strings.Contains(string(originalContentAtDispatch), "primary") {
		t.Errorf("original agent-a.md at dispatch time: want mode=primary "+
			"(backup-and-transform applied in-place), got %q; "+
			"backup-and-transform must modify original agent files, not snapshot copies",
			string(originalContentAtDispatch))
	}

	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunStopped verifies
// that BackupState.Cleanup runs when the context is cancelled mid-run
// (RunStopped). Originals are restored and the backup directory is deleted.
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunStopped(t *testing.T) {
	const runID = "oc-cleanup-stop-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	var originalContentAtDispatch []byte
	f := harness.NewMockAdapter()
	ctx, cancel := context.WithCancel(context.Background())

	cb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				originalContentAtDispatch, _ = os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
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
		Harness:  cb,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(ctx, baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunStopped)

	// The ORIGINAL file must have been in mode=primary at dispatch time
	// (backup-and-transform in-place). RED: copy-and-invoke leaves original mode=subagent.
	if !strings.Contains(string(originalContentAtDispatch), "primary") {
		t.Errorf("original agent-a.md at dispatch time: want mode=primary "+
			"(backup-and-transform applied in-place), got %q; "+
			"backup-and-transform must modify original agent files, not snapshot copies",
			string(originalContentAtDispatch))
	}

	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunDeviationUnresolved
// verifies that BackupState.Cleanup runs when the run ends with
// RunDeviationUnresolved. Originals are restored and the backup dir is deleted.
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunDeviationUnresolved(t *testing.T) {
	const runID = "oc-cleanup-deviation-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	var originalContentAtDispatch []byte
	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "needs more work",
	}})
	cb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				originalContentAtDispatch, _ = os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:  cb,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// No Routing consultant: deviation becomes RunDeviationUnresolved.
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunDeviationUnresolved)

	// The ORIGINAL file must have been in mode=primary at dispatch time.
	// RED: copy-and-invoke leaves original mode=subagent.
	if !strings.Contains(string(originalContentAtDispatch), "primary") {
		t.Errorf("original agent-a.md at dispatch time: want mode=primary "+
			"(backup-and-transform applied in-place), got %q; "+
			"backup-and-transform must modify original agent files, not snapshot copies",
			string(originalContentAtDispatch))
	}

	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunFailed verifies that
// BackupState.Cleanup runs when the run ends with RunFailed (store.Apply error).
// Originals are restored and the backup directory is deleted.
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunFailed(t *testing.T) {
	const runID = "oc-cleanup-failed-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	var originalContentAtDispatch []byte
	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	cb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				originalContentAtDispatch, _ = os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
			}
		},
	}

	failStore := &memStore{
		applyErrOnFirst: true,
		applyFirstErr:   errors.New("test: forced apply failure"),
	}

	ses := session.New(session.Deps{
		Harness:  cb,
		Store:    failStore,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, _ := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	// RunFailed returns a non-nil error; verify status directly.
	if got.Status != domain.RunFailed {
		t.Errorf("want RunFailed, got %q (message: %q)", got.Status, got.Message)
	}

	// The ORIGINAL file must have been in mode=primary at dispatch time.
	// RED: copy-and-invoke leaves original mode=subagent.
	if !strings.Contains(string(originalContentAtDispatch), "primary") {
		t.Errorf("original agent-a.md at dispatch time: want mode=primary "+
			"(backup-and-transform applied in-place), got %q; "+
			"backup-and-transform must modify original agent files, not snapshot copies",
			string(originalContentAtDispatch))
	}

	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunRefused verifies
// that BackupState.Cleanup runs when the run is refused AFTER
// SetupBackupAndTransform has succeeded (defer is registered at step 5b).
//
// The refusal is triggered at step 7.5a (mode=unset). The deferred cleanup
// must run even though the run never entered the dispatch loop.
// This also covers AC10.11 (defer runs for post-setup refusals).
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunRefused(t *testing.T) {
	const runID = "oc-cleanup-refused-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	ses := session.New(session.Deps{
		Harness:  harness.NewMockAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// mode=unset triggers refusal at step 7.5a (after backup setup at step 5b).
	cfg := baseOpenCodeHarnessConfig(orchPath, runID)
	cfg.RunSettings.Mode = domain.ExecutionModeUnset

	got, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error for RunRefused, got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused, got %q (message: %q)", got.Status, got.Message)
	}

	// The backup was set up at step 5b, then the run was refused at step 7.5a.
	// The deferred Cleanup must have run, restoring originals and deleting backup.
	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunStoppedByConsultant
// verifies that BackupState.Cleanup runs when the routing consultant issues a
// stop instruction. Originals are restored and the backup directory is deleted.
func TestSession_Start_OpenCodeHarness_BackupCleanupRunsOnRunStoppedByConsultant(t *testing.T) {
	const runID = "oc-cleanup-consultant-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "test task", 0)
	consultant.queueStop("cleanup test stop")

	var originalContentAtDispatch []byte
	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	cb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				originalContentAtDispatch, _ = os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:  cb,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	cfg := baseOpenCodeHarnessConfig(orchPath, runID)
	cfg.RunSettings.Mode = domain.ExecutionModeOrchestrated

	got, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant, got %q (message: %q)", got.Status, got.Message)
	}

	// The ORIGINAL file must have been in mode=primary at dispatch time.
	// RED: copy-and-invoke leaves original mode=subagent.
	if !strings.Contains(string(originalContentAtDispatch), "primary") {
		t.Errorf("original agent-a.md at dispatch time: want mode=primary "+
			"(backup-and-transform applied in-place), got %q; "+
			"backup-and-transform must modify original agent files, not snapshot copies",
			string(originalContentAtDispatch))
	}

	assertBackupCleanedUp(t, agentsDir, backupDir)
}

// TestSession_Start_OpenCodeHarness_BackupCleanupFailureIsNonFatal verifies
// that a restore error during BackupState.Cleanup is logged as
// EventSnapshotCleanupFailed and does not change the run outcome (FR-22).
//
// The injectable RestoreFunc seam on BackupState (snapshot package) is used to
// force a restore failure without relying on filesystem tricks (read-only files,
// locked handles) that are unreliable on Windows. The BackupStateHook in
// session.Deps receives the BackupState immediately after SetupBackupAndTransform
// returns, before Cleanup is deferred, so the hook can set RestoreFunc to inject
// the error.
//
// RED signal: Before the implementation routes opencode through backup-and-transform,
// SetupBackupAndTransform is never called, BackupStateHook is never invoked, no
// error is injected, and EventSnapshotCleanupFailed is never logged.
func TestSession_Start_OpenCodeHarness_BackupCleanupFailureIsNonFatal(t *testing.T) {
	const runID = "oc-cleanup-fail-01"
	_, orchPath, _ := writeOpenCodeHarnessDir(t)

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

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
		BackupStateHook: func(bs *lockprotocol.BackupState) {
			// Inject a RestoreFunc that always returns an error. This forces
			// BackupState.Cleanup to fail (FR-22: failure must be non-fatal).
			bs.RestoreFunc = func(agentsDir, backupDir string) error {
				return errors.New("injected restore failure for FR-22 test")
			}
		},
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))

	// FR-22: restore failure must not surface as a returned error.
	if err != nil {
		t.Fatalf("want nil error (cleanup failure must be non-fatal), got %v", err)
	}
	// FR-22: restore failure must not change the run outcome.
	if got.Status != domain.RunCompleted {
		t.Errorf("want RunCompleted (cleanup restore failure must not change run outcome), "+
			"got %q (message: %q)", got.Status, got.Message)
	}
	// FR-22: restore failure must be recorded as a debug log event.
	if !logger.eventLogged(domain.EventSnapshotCleanupFailed) {
		t.Error("want EventSnapshotCleanupFailed debug event logged when BackupState.Cleanup " +
			"restore fails, but the event was not found in the debug log; " +
			"the implementation must log cleanup failures rather than silently ignore or surface them")
	}
}
