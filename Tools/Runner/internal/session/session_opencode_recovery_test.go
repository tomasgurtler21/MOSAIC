package session_test

// Tests for the OpenCode backup-and-transform recovery check: RecoveryCheck
// runs before SetupBackupAndTransform (step 4b ordering), handles corrupt
// manifests, and is a no-op when no backup directory exists.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_Start_OpenCodeHarness_RecoveryCheck_RunsBeforeStrategySelection
// verifies that RecoveryCheck runs before SetupBackupAndTransform (step 4b
// ordering: ContractsDesign.md Plan Override for T10.5(a)). An orphaned backup
// directory (stale from a previous crashed run) is cleaned up by RecoveryCheck,
// and EventSnapshotRecovery is logged.
//
// If RecoveryCheck did NOT run before SetupBackupAndTransform, the joiner path
// would be taken for the stale backup dir. The joiner would find .setup-complete
// and acquire a lock, but EventSnapshotRecovery would NOT be logged. The RED
// assertion checks for the log event.
func TestSession_Start_OpenCodeHarness_RecoveryCheck_RunsBeforeStrategySelection(t *testing.T) {
	const runID = "oc-recovery-ordering-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	// Pre-create an orphaned backup dir with a valid manifest and .setup-complete
	// (simulating a previous run that completed transforms but crashed before
	// releasing its lock). RecoveryCheck must detect and restore this at startup.
	writeStaleOpenCodeBackupDir(t, agentsDir, backupDir)

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

	logger := &sessionRecordingLogger{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// RecoveryCheck must have logged EventSnapshotRecovery when it cleaned up
	// the stale backup and restored originals. If it did not run (wrong ordering),
	// the event would be absent because the session took the joiner path instead.
	if !logger.eventLogged(domain.EventSnapshotRecovery) {
		t.Error("want EventSnapshotRecovery logged (RecoveryCheck ran and restored stale backup), " +
			"but the event was not found; RecoveryCheck must run before SetupBackupAndTransform " +
			"(ContractsDesign.md step 4b override)")
	}
}

// TestSession_Start_OpenCodeHarness_RecoveryCheck_CorruptManifestRefusesRun
// verifies that a corrupt recovery manifest causes RecoveryCheck to refuse the
// run (AC10.8). This also covers T10.4(d): if the run is refused before
// SetupBackupAndTransform (step 5b), no BackupState is created, so cleanup is
// a no-op and the session must not crash.
func TestSession_Start_OpenCodeHarness_RecoveryCheck_CorruptManifestRefusesRun(t *testing.T) {
	const runID = "oc-recovery-corrupt-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	// Pre-create a backup dir with an unparseable manifest.
	writeCorruptOpenCodeBackupDir(t, agentsDir, backupDir)

	ses := session.New(session.Deps{
		Harness:  harness.NewMockAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	// RecoveryCheck returns a RefusalError on corrupt manifest -> RunRefused.
	if err != nil {
		t.Fatalf("want nil error for RunRefused (refusals encode in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused (RecoveryCheck: corrupt manifest), got %q (message: %q)",
			got.Status, got.Message)
	}
	// The session must not crash: no BackupState was created (step 5b never ran).
	// The corrupt backup dir is left intact for manual inspection.
}

// TestSession_Start_OpenCodeHarness_RecoveryCheck_NoopWhenNoBackupExists
// verifies that RecoveryCheck is a no-op when no backup directory exists
// (normal startup). The run proceeds normally (AC10.7 no-op sub-case).
func TestSession_Start_OpenCodeHarness_RecoveryCheck_NoopWhenNoBackupExists(t *testing.T) {
	const runID = "oc-recovery-noop-01"
	_, orchPath, backupDir := writeOpenCodeHarnessDir(t)

	// Verify the backup dir does NOT exist before the run.
	if _, statErr := os.Stat(backupDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("pre-condition: backup dir %q must not exist before the run", backupDir)
	}

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

	// When no backup dir exists, RecoveryCheck is a no-op and the run proceeds.
	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)
}
