package lockprotocol

// Tests for the joiner path of the lock protocol: manifest-wait, setup-complete-wait,
// post-lock re-verification, and the restoring re-poll loop.

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T8.1(a): joiner detects existing backup and manifest, acquires own lock
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ExistingManifestAndSetupComplete_AcquiresOwnLock verifies
// that when a full backup state exists (manifest + .setup-complete), setupAsJoiner
// acquires its own per-run lock without re-creating the backup directory or
// re-running transforms.
func TestSetupAsJoiner_ExistingManifestAndSetupComplete_AcquiresOwnLock(t *testing.T) {
	// Arrange: full backup state with a creator lock held to simulate a concurrent run.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	creator := mustNewState(t, agentsDir, backupDir, "run-creator", nil)
	t.Cleanup(func() { cleanupState(creator) })

	// Capture agent file content before the joiner runs to detect any mutation.
	workerBefore, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md before join: %v", err)
	}

	// Act
	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	if err := joiner.setupAsJoiner(); err != nil {
		t.Fatalf("setupAsJoiner: %v", err)
	}
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner's run lock is held (TryLock from external handle fails).
	h, ok, tryErr := filelock.TryLock(backup.RunLockPath(backupDir, "run-joiner"))
	if tryErr != nil {
		t.Fatalf("TryLock on joiner run lock: %v", tryErr)
	}
	if ok {
		if h != nil {
			h.Unlock() //nolint:errcheck
		}
		t.Error("joiner run lock is not held after setupAsJoiner; expected lock to be acquired")
	}

	// Assert: agent file content is unchanged (joiner did not re-transform or re-restore).
	workerAfter, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("read worker.md after join: %v", err)
	}
	if string(workerAfter) != string(workerBefore) {
		t.Error("joiner must not alter agent file content; content changed after setupAsJoiner")
	}

	// Assert: backup directory still exists (joiner did not tear it down).
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("backup directory should still exist after joiner setup; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// T8.1(b): joiner waits for .setup-complete when only manifest is present
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ManifestPresentNoSetupComplete_WaitsForSetupComplete
// verifies that setupAsJoiner blocks until .setup-complete appears when the
// manifest exists but .setup-complete has not yet been written by the creator.
func TestSetupAsJoiner_ManifestPresentNoSetupComplete_WaitsForSetupComplete(t *testing.T) {
	// Arrange: backup dir with manifest, agents transformed -- no .setup-complete.
	base := t.TempDir()
	agentsDir, backupDir := newTestBackupStateWithManifestOnly(t, base)
	rules := transform.TransformationsFor("opencode")
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: backup.TransformInPlace: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")

	// Write .setup-complete after a delay (simulating creator finishing transforms).
	go func() {
		time.Sleep(25 * time.Millisecond)
		if writeErr := backup.WriteSetupComplete(backupDir); writeErr != nil {
			t.Logf("background backup.WriteSetupComplete: %v", writeErr)
		}
	}()

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner succeeded after waiting for .setup-complete.
	if err != nil {
		t.Errorf("setupAsJoiner: expected nil (waited for .setup-complete); got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(c): post-lock re-verify: .restoring held after lock acquired
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_PostLockReverify_RestoringHeld_ReleasesLockAndStartsFresh
// verifies the post-lock re-verification path: after the joiner acquires its
// own run lock, if .restoring is held (a concurrent last-out teardown is in
// progress), the joiner releases and deletes its lock file, enters the
// .restoring re-poll loop, and returns errStartFresh when the backup directory
// disappears.
func TestSetupAsJoiner_PostLockReverify_RestoringHeld_ReleasesLockAndStartsFresh(t *testing.T) {
	// Arrange: full backup state.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})

	joiner.afterJoinerLockAcquired = func() {
		// Simulate last-out teardown: hold .restoring while joiner runs post-lock verify.
		go func() {
			h, lockErr := filelock.Lock(restoringPath)
			if lockErr != nil {
				return
			}
			close(restoringHeld)
			time.Sleep(60 * time.Millisecond)
			os.RemoveAll(backupDir) // backup dir disappears while .restoring is held
			h.Unlock()              //nolint:errcheck
		}()
		<-restoringHeld // block until .restoring is actually held, then return
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner entered the .restoring re-poll loop and returned errStartFresh
	// once the backup directory was removed.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh after .restoring was held during post-lock re-verify"+
			" and backup dir disappeared; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(d): manifest-wait: backup-dir-gone returns errStartFresh
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ManifestWait_BackupDirDisappears_StartsFresh verifies that
// when the backup directory is deleted while the joiner is waiting for the
// manifest, setupAsJoiner returns errStartFresh instead of timing out.
func TestSetupAsJoiner_ManifestWait_BackupDirDisappears_StartsFresh(t *testing.T) {
	// Arrange: backup dir without manifest.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	var once sync.Once
	joiner.onManifestPollTick = func() {
		once.Do(func() { os.RemoveAll(backupDir) })
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: backup-dir-gone detected during manifest wait -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when backup dir disappears during manifest wait; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(d): manifest-wait timeout
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ManifestWait_Timeout_ReturnsPollTimeout verifies that when
// the manifest never appears and the backup directory remains, setupAsJoiner
// returns errPollTimeout after the deadline is exceeded.
func TestSetupAsJoiner_ManifestWait_Timeout_ReturnsPollTimeout(t *testing.T) {
	// Arrange: backup dir with no manifest (manifest never written).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollInterval = 5 * time.Millisecond
	joiner.pollTimeout = 20 * time.Millisecond // short timeout

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: poll deadline exceeded.
	if !errors.Is(err, errPollTimeout) {
		t.Errorf("expected errPollTimeout when manifest never appears; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(e): .restoring re-poll: releases, backup intact -> joiner succeeds
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RestoringPoll_ReleasedWithBackupIntact_JoinerSucceeds
// verifies the re-poll happy path: the joiner encounters .restoring held
// (indicating a concurrent exit), polls until .restoring is released, then
// re-verifies the backup directory is intact and successfully acquires its
// own lock.
func TestSetupAsJoiner_RestoringPoll_ReleasedWithBackupIntact_JoinerSucceeds(t *testing.T) {
	// Arrange: full backup state; hold .restoring before joiner starts.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	restoringHandle, lockErr := filelock.Lock(restoringPath)
	if lockErr != nil {
		t.Fatalf("Lock .restoring: %v", lockErr)
	}

	// Release .restoring after a delay; backup dir remains intact (non-last-out exit).
	go func() {
		time.Sleep(35 * time.Millisecond)
		restoringHandle.Unlock() //nolint:errcheck
	}()

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 500 * time.Millisecond

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner polled .restoring, waited for release, re-verified backup, succeeded.
	if err != nil {
		t.Errorf("setupAsJoiner: expected nil after .restoring released with backup intact; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(g): setup-complete-wait detects backup-dir-gone -> start fresh
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_SetupCompleteWait_TeardownDetected_StartsFresh verifies
// the teardown-interleaving behavior during setup-complete-wait: when the
// backup directory disappears (last-out run tore it down after all runs exited),
// the joiner detects this on a poll tick and returns errStartFresh instead of
// timing out waiting for .setup-complete.
func TestSetupAsJoiner_SetupCompleteWait_TeardownDetected_StartsFresh(t *testing.T) {
	// Arrange: backup dir with manifest and transformed agents, no .setup-complete.
	base := t.TempDir()
	agentsDir, backupDir := newTestBackupStateWithManifestOnly(t, base)
	rules := transform.TransformationsFor("opencode")
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: backup.TransformInPlace: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	var once sync.Once
	joiner.onSetupCompletePollTick = func() {
		once.Do(func() { os.RemoveAll(backupDir) })
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: backup-dir-gone detected during setup-complete wait -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when backup dir disappears during setup-complete wait; got %v", err)
	}
}
