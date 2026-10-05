package lockprotocol

// Edge-case tests for RecoveryCheck: partial-backup races and protocol-artifact
// recognition (ManifestTempFileName).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
)

// ---------------------------------------------------------------------------
// Recovery: partial-backup deletion serialized under .restoring (AC9.9)
// ---------------------------------------------------------------------------

// TestRecoveryCheck_PartialBackupJoinerSlipsIn_SkipsDeletion verifies the
// ordering guarantee for partial-backup cleanup: when a joiner acquires its
// run lock between the initial probe and .restoring acquisition (the same race
// window closed by the M1 fix for full backups), RecoveryCheck detects the
// held lock on re-probe under .restoring and skips the partial backup
// deletion, leaving the backup directory intact.
//
// This test uses the afterRecoveryInitialProbeForTest hook to inject the race
// condition deterministically, proving that partial-backup deletion runs under
// .restoring and is therefore serialized with concurrent joiners.
func TestRecoveryCheck_PartialBackupJoinerSlipsIn_SkipsDeletion(t *testing.T) {
	// Arrange: partial backup state (backup dir, no manifest, no lock files).
	base := t.TempDir()
	agentsDir, backupDir := newTestPartialBackup(t, base)

	// Hook: a joiner acquires its run lock after the initial probe and before
	// .restoring is locked. Without the ordering guarantee, RecoveryCheck
	// might delete the partial backup while the joiner is in flight.
	var joinerHandle *filelock.Handle
	afterRecoveryInitialProbeForTest = func() {
		h, err := filelock.Lock(backup.RunLockPath(backupDir, "run-joiner-partial"))
		if err != nil {
			t.Errorf("hook: Lock joiner-partial: %v", err)
			return
		}
		joinerHandle = h
	}
	t.Cleanup(func() {
		afterRecoveryInitialProbeForTest = nil
		if joinerHandle != nil {
			joinerHandle.Unlock() //nolint:errcheck
		}
	})

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error (skipping is safe, not a refusal).
	if gotErr != nil {
		t.Errorf("RecoveryCheck (partial backup, joiner slipped in): unexpected error: %v", gotErr)
	}

	// Assert: backup directory still exists (deletion skipped because re-probe
	// under .restoring detected the joiner's held lock).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("backup directory must remain when joiner is detected during re-probe under .restoring; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Recovery: ManifestTempFileName recognized as a protocol artifact
// ---------------------------------------------------------------------------

// TestRecoveryCheck_BackupDirWithOnlyManifestTempFile_NotRefusedAsForeign
// verifies that a backup directory containing only the manifest temp file
// (.recovery-manifest.json.tmp) is not treated as a foreign directory.
//
// This file is written by WriteManifest's atomic write before the rename to
// ManifestFileName. A crash between the write and the rename leaves only the
// temp file in the backup directory. RecoveryCheck must recognize this file as
// a protocol artifact, treat the directory as a partial backup (no completed
// manifest), delete it, and return nil -- not refuse with "foreign backup
// directory".
func TestRecoveryCheck_BackupDirWithOnlyManifestTempFile_NotRefusedAsForeign(t *testing.T) {
	// Arrange: backup dir containing only the manifest temp file.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	tmpManifestPath := filepath.Join(backupDir, manifest.ManifestTempFileName)
	if err := os.WriteFile(tmpManifestPath, []byte(`{"partial":"manifest"}`), 0o644); err != nil {
		t.Fatalf("setup: write manifest temp file: %v", err)
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no "foreign backup directory" refusal. A directory holding only
	// a recognized protocol artifact must not be classified as foreign.
	var refusal *domain.RefusalError
	if errors.As(gotErr, &refusal) && strings.Contains(refusal.Reason, "foreign") {
		t.Errorf("RecoveryCheck must not refuse a backup dir containing only %q as foreign; got: %v",
			manifest.ManifestTempFileName, gotErr)
	}

	// Assert: no error (partial backup with no held locks -- delete and return nil).
	if gotErr != nil {
		t.Errorf("RecoveryCheck (only manifest temp file): unexpected error: %v", gotErr)
	}

	// Assert: partial backup directory removed (treated as a partial backup).
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("backup directory containing only manifest temp file should be removed; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Public API: logger and injectable timeout options are accepted by RecoveryCheck
// ---------------------------------------------------------------------------

// TestRecoveryCheck_LoggerAndOptionsAccepted verifies that RecoveryCheck
// accepts a logger and option values without error. When no backup directory
// exists, it is a no-op regardless of options.
func TestRecoveryCheck_LoggerAndOptionsAccepted(t *testing.T) {
	// Arrange: no backup directory.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	spy := &spyLogger{}

	// Act: options accepted even on no-op path.
	gotErr := RecoveryCheck(
		agentsDir,
		spy,
		WithPollInterval(5*time.Millisecond),
		WithPollTimeout(500*time.Millisecond),
	)

	// Assert: no error.
	if gotErr != nil {
		t.Errorf("RecoveryCheck with no backup dir and options: expected nil error; got %v", gotErr)
	}
}

// ---------------------------------------------------------------------------
// Public API: RecoveryCheck emits the recovery event on successful restore
// ---------------------------------------------------------------------------

// TestRecoveryCheck_SuccessfulRestore_EmitsRecoveryEvent verifies that when
// RecoveryCheck performs a successful restore (backup exists, no held locks),
// it logs EventSnapshotRecovery via the injected logger.
func TestRecoveryCheck_SuccessfulRestore_EmitsRecoveryEvent(t *testing.T) {
	// Arrange: full backup state, no lock files.
	base := t.TempDir()
	agentsDir, _ := newTestFullBackupState(t, base)
	spy := &spyLogger{}

	// Act
	gotErr := RecoveryCheck(agentsDir, spy)

	// Assert: no error.
	if gotErr != nil {
		t.Fatalf("RecoveryCheck: unexpected error: %v", gotErr)
	}

	// Assert: recovery event was logged.
	if !spy.hasEvent(domain.EventSnapshotRecovery) {
		t.Errorf("expected EventSnapshotRecovery to be logged after successful restore; events: %v",
			spy.events)
	}
}
