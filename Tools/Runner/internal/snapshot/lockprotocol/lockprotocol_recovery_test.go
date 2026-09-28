package lockprotocol

// Tests for RecoveryCheck: the startup crash-recovery path that restores
// agent originals when a prior run left a backup directory.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
)

// ---------------------------------------------------------------------------
// No backup directory: recovery is a no-op
// ---------------------------------------------------------------------------

// TestRecoveryCheck_NoBackupDirectory_ReturnsNilAndIsNoop verifies that when
// no backup directory exists (normal startup, no prior crash), RecoveryCheck
// returns nil and does not modify the agents directory.
func TestRecoveryCheck_NoBackupDirectory_ReturnsNilAndIsNoop(t *testing.T) {
	// Arrange: agents dir exists, no backup dir sibling.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	workerPath := filepath.Join(agentsDir, "worker.md")

	originalContent, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md before RecoveryCheck: %v", err)
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Errorf("RecoveryCheck with no backup dir: expected nil error; got %v", gotErr)
	}

	// Assert: agents directory is unchanged.
	after, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md after RecoveryCheck: %v", err)
	}
	if string(after) != string(originalContent) {
		t.Error("RecoveryCheck must not modify agents when no backup exists")
	}
}

// ---------------------------------------------------------------------------
// Full backup, no lock files: recovery runs and restores originals
// ---------------------------------------------------------------------------

// TestRecoveryCheck_BackupExistsNoLockFiles_RecoveryRunsAndRestores verifies
// the normal crash-recovery path: a backup exists (transforms applied, no
// active runs), RecoveryCheck restores original content and removes the backup
// directory.
func TestRecoveryCheck_BackupExistsNoLockFiles_RecoveryRunsAndRestores(t *testing.T) {
	// Arrange: full backup state (transforms applied, no lock files).
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	workerPath := filepath.Join(agentsDir, "worker.md")

	// Sanity: content is currently transformed.
	transformed, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md before RecoveryCheck: %v", err)
	}
	if strings.Contains(string(transformed), "mode: subagent") {
		t.Fatal("setup: worker.md not transformed; test is not meaningful")
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Fatalf("RecoveryCheck: unexpected error: %v", gotErr)
	}

	// Assert: original content restored.
	restored, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md after RecoveryCheck: %v", err)
	}
	if !strings.Contains(string(restored), "mode: subagent") {
		t.Errorf("worker.md not restored to original content after RecoveryCheck; got:\n%s", restored)
	}

	// Assert: backup directory removed.
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should be removed after successful recovery; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Full backup, all orphaned lock files: delete orphans then recover
// ---------------------------------------------------------------------------

// TestRecoveryCheck_BackupWithOrphanedLocks_DeletesOrphansAndRecovers verifies
// that when the backup directory contains lock files for crashed runs (files
// exist but are not locked), RecoveryCheck deletes the orphaned lock files,
// performs recovery, and removes the backup directory.
func TestRecoveryCheck_BackupWithOrphanedLocks_DeletesOrphansAndRecovers(t *testing.T) {
	// Arrange: full backup state with two orphaned lock files.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	// Create orphaned lock files: acquire then release (OS released on "crash").
	for _, id := range []string{"run-crashed-a", "run-crashed-b"} {
		path := backup.RunLockPath(backupDir, id)
		h, err := filelock.Lock(path)
		if err != nil {
			t.Fatalf("Lock orphan %s: %v", id, err)
		}
		h.Unlock() //nolint:errcheck
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Fatalf("RecoveryCheck with orphaned locks: unexpected error: %v", gotErr)
	}

	// Assert: backup directory removed (last-out teardown completed).
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should be removed after recovery with orphaned locks; got: %v", statErr)
	}

	// Assert: original content restored.
	restored, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md after RecoveryCheck: %v", err)
	}
	if !strings.Contains(string(restored), "mode: subagent") {
		t.Errorf("worker.md not restored after recovery with orphaned locks; got:\n%s", restored)
	}
}

// ---------------------------------------------------------------------------
// Full backup, one held lock: skip recovery (another run is active)
// ---------------------------------------------------------------------------

// TestRecoveryCheck_BackupWithHeldLock_SkipsRecovery verifies that when at
// least one .lock-* file is held (another run is active), RecoveryCheck skips
// recovery and leaves the backup directory intact.
func TestRecoveryCheck_BackupWithHeldLock_SkipsRecovery(t *testing.T) {
	// Arrange: full backup state; one run holds its lock.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	activeLock, err := filelock.Lock(backup.RunLockPath(backupDir, "run-active"))
	if err != nil {
		t.Fatalf("Lock active run: %v", err)
	}
	t.Cleanup(func() { activeLock.Unlock() }) //nolint:errcheck

	// Capture agent content before RecoveryCheck.
	beforeContent, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md before RecoveryCheck: %v", err)
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Errorf("RecoveryCheck with held lock: unexpected error: %v", gotErr)
	}

	// Assert: backup directory still exists (recovery skipped).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("backup directory must remain when a held lock is detected; got: %v", statErr)
	}

	// Assert: agent content unchanged (no restore happened).
	afterContent, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md after RecoveryCheck: %v", err)
	}
	if string(afterContent) != string(beforeContent) {
		t.Error("agent content must not change when RecoveryCheck skips recovery due to held lock")
	}
}

// ---------------------------------------------------------------------------
// Partial backup, no manifest, no locks: delete partial backup and return nil
// ---------------------------------------------------------------------------

// TestRecoveryCheck_PartialBackupNoManifestNoLocks_DeletesPartialBackupDir
// verifies that when the backup directory exists but has no manifest (crash
// during backup creation, before WriteManifest), RecoveryCheck deletes the
// partial backup directory and returns nil. Agent files are untouched because
// no transforms were applied before the manifest was written.
func TestRecoveryCheck_PartialBackupNoManifestNoLocks_DeletesPartialBackupDir(t *testing.T) {
	// Arrange: partial backup state (backup dir, no manifest, agents untransformed).
	base := t.TempDir()
	agentsDir, backupDir := newTestPartialBackup(t, base)

	originalContent, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md before RecoveryCheck: %v", err)
	}

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Fatalf("RecoveryCheck (partial backup, no manifest): unexpected error: %v", gotErr)
	}

	// Assert: partial backup directory removed.
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("partial backup directory should be removed; got: %v", statErr)
	}

	// Assert: agent file unchanged.
	afterContent, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md after RecoveryCheck: %v", err)
	}
	if string(afterContent) != string(originalContent) {
		t.Error("RecoveryCheck must not alter agents when clearing a partial backup")
	}
}

// ---------------------------------------------------------------------------
// Partial backup with a held lock: creator is still copying, skip recovery
// ---------------------------------------------------------------------------

// TestRecoveryCheck_PartialBackupWithHeldLock_SkipsRecovery verifies that
// when a backup directory without a manifest has a held lock file (a creator
// is still in the middle of the backup creation protocol), RecoveryCheck
// skips recovery and leaves the directory intact.
func TestRecoveryCheck_PartialBackupWithHeldLock_SkipsRecovery(t *testing.T) {
	// Arrange: partial backup state; creator holds its lock (still copying).
	base := t.TempDir()
	agentsDir, backupDir := newTestPartialBackup(t, base)

	creatorLock, err := filelock.Lock(backup.RunLockPath(backupDir, "run-creator"))
	if err != nil {
		t.Fatalf("Lock creator: %v", err)
	}
	t.Cleanup(func() { creatorLock.Unlock() }) //nolint:errcheck

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: no error.
	if gotErr != nil {
		t.Errorf("RecoveryCheck (partial backup, held lock): unexpected error: %v", gotErr)
	}

	// Assert: backup directory still exists (recovery skipped).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("partial backup directory must remain when a held lock is present; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Corrupt manifest: refuse the run and leave backup directory untouched
// ---------------------------------------------------------------------------

// TestRecoveryCheck_CorruptManifest_RefusesRunAndLeavesBackupUntouched verifies
// that when the manifest in the backup directory exists but cannot be parsed,
// RecoveryCheck returns a *domain.RefusalError and does not delete or modify
// the backup directory. The backup is left for manual inspection.
func TestRecoveryCheck_CorruptManifest_RefusesRunAndLeavesBackupUntouched(t *testing.T) {
	// Arrange: backup dir with corrupt manifest, no lock files held.
	base := t.TempDir()
	agentsDir, backupDir := newTestPartialBackup(t, base)
	writeCorruptManifest(t, backupDir)

	// Act
	gotErr := RecoveryCheck(agentsDir, nil)

	// Assert: error is returned.
	if gotErr == nil {
		t.Fatal("RecoveryCheck with corrupt manifest: expected non-nil error; got nil")
	}

	// Assert: error is a *domain.RefusalError.
	var refusal *domain.RefusalError
	if !errors.As(gotErr, &refusal) {
		t.Errorf("RecoveryCheck with corrupt manifest: expected *domain.RefusalError; got %T: %v", gotErr, gotErr)
	}

	// Assert: backup directory left intact (for manual inspection).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("backup directory must remain after corrupt manifest refusal; got: %v", statErr)
	}

	// Assert: manifest file still present (backup untouched).
	manifestPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Errorf("manifest file must remain after corrupt manifest refusal; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Joiner slips in after initial probe: re-probe detects held lock, skip
// ---------------------------------------------------------------------------

// TestRecoveryCheck_JoinerSlipsInAfterInitialProbe_SkipsRecovery verifies the
// M1 fix: after the initial .lock-* probe finds no held locks, a concurrent
// joiner acquires its lock before .restoring is obtained. The re-probe under
// .restoring detects the newly held lock and RecoveryCheck releases .restoring
// and skips recovery, leaving the backup directory intact.
//
// This test uses the afterRecoveryInitialProbeForTest hook to inject the race
// condition deterministically.
func TestRecoveryCheck_JoinerSlipsInAfterInitialProbe_SkipsRecovery(t *testing.T) {
	// Arrange: full backup state with no lock files initially.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	// Hook: a joiner acquires its run lock after the initial probe and before
	// .restoring is locked. This simulates the window the M1 fix closes.
	var joinerHandle *filelock.Handle
	afterRecoveryInitialProbeForTest = func() {
		h, err := filelock.Lock(backup.RunLockPath(backupDir, "run-joiner-slip"))
		if err != nil {
			t.Errorf("hook: Lock joiner-slip: %v", err)
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

	// Assert: no error (skip is safe, not a refusal).
	if gotErr != nil {
		t.Errorf("RecoveryCheck (joiner slipped in): unexpected error: %v", gotErr)
	}

	// Assert: backup directory still exists (recovery skipped because joiner
	// was detected during re-probe under .restoring).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("backup directory must remain when joiner is detected during re-probe; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Two simultaneous recoverers: serialize on .restoring, exactly one recovers
// ---------------------------------------------------------------------------

// TestRecoveryCheck_TwoSimultaneousRecoverers_ExactlyOneRecovers verifies that
// two goroutines calling RecoveryCheck concurrently serialize correctly on
// .restoring: the first acquires .restoring and recovers; the second blocks,
// then finds no backup directory and no-ops. After both complete:
//   - Both returned nil.
//   - The backup directory no longer exists.
//   - The agent file is restored to its original content.
func TestRecoveryCheck_TwoSimultaneousRecoverers_ExactlyOneRecovers(t *testing.T) {
	// Arrange: full backup state, no lock files.
	base := t.TempDir()
	agentsDir, _ := newTestFullBackupState(t, base)

	type result struct {
		err error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	startCh := make(chan struct{})

	wg.Add(2)
	for i := range 2 {
		go func(idx int) {
			defer wg.Done()
			<-startCh
			results[idx].err = RecoveryCheck(agentsDir, nil)
		}(i)
	}

	close(startCh)
	wg.Wait()

	// Assert: both returned nil.
	for i, r := range results {
		if r.err != nil {
			t.Errorf("recoverer %d: RecoveryCheck returned unexpected error: %v", i, r.err)
		}
	}

	// Assert: backup directory is gone after both runs complete.
	if _, statErr := os.Stat(backupDirPath(agentsDir)); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should be removed after both recoverers complete; got: %v", statErr)
	}

	// Assert: agent file restored to original content.
	content, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md after two-recoverer test: %v", err)
	}
	if !strings.Contains(string(content), "mode: subagent") {
		t.Errorf("worker.md not restored after two-recoverer test; got:\n%s", content)
	}
}
