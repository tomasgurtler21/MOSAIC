package lockprotocol

// Tests for the last-out check (lastOutCheck): restore, teardown, ordering.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
)

// ---------------------------------------------------------------------------
// T7.2(a): single run -- restore runs, backup dir removed
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_SingleRun_RestoresAgentContent verifies that
// when the sole active run calls lastOutCheck, RestoreFromBackup is invoked
// and the agent file content is restored to its pre-transform state.
func TestLockProtocol_LastOutCheck_SingleRun_RestoresAgentContent(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	workerPath := filepath.Join(agentsDir, "worker.md")

	// Sanity: file is currently transformed.
	transformedContent, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md before restore: %v", err)
	}
	if strings.Contains(string(transformedContent), "mode: subagent") {
		t.Fatal("setup: worker.md not transformed; test is not meaningful")
	}

	state := mustNewState(t, agentsDir, backupDir, "run-001", nil)
	// No t.Cleanup needed: lastOutCheck releases and deletes the run lock.

	// Act
	if err := state.lastOutCheck(); err != nil {
		t.Fatalf("lastOutCheck: %v", err)
	}

	// Assert: worker.md has original content.
	restored, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md after restore: %v", err)
	}
	if !strings.Contains(string(restored), "mode: subagent") {
		t.Errorf("worker.md not restored to original content; got:\n%s", restored)
	}
	if strings.Contains(string(restored), "mode: primary") {
		t.Errorf("worker.md still has transformed content after restore; got:\n%s", restored)
	}
}

// ---------------------------------------------------------------------------
// T7.2(f): after successful last-out teardown, backup directory is removed
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_AfterSuccessfulTeardown_BackupDirRemoved verifies
// that after the last-out lastOutCheck completes, the backup directory no
// longer exists on disk.
func TestLockProtocol_LastOutCheck_AfterSuccessfulTeardown_BackupDirRemoved(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	state := mustNewState(t, agentsDir, backupDir, "run-001", nil)

	// Act
	if err := state.lastOutCheck(); err != nil {
		t.Fatalf("lastOutCheck: %v", err)
	}

	// Assert: backup directory is gone.
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should not exist after last-out teardown; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// T7.2(b): non-last-out -- .restoring released without being deleted
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_NonLastOut_RestoringFileRemainsOnDisk verifies
// that when a non-last-out run calls lastOutCheck, it releases .restoring
// without deleting the file. The .restoring file must remain on disk because
// another run is still active and may be waiting on it.
func TestLockProtocol_LastOutCheck_NonLastOut_RestoringFileRemainsOnDisk(t *testing.T) {
	// Arrange: two active runs; run-002 keeps its lock held.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	state1 := mustNewState(t, agentsDir, backupDir, "run-001", nil)
	state2, err := newLockProtocolState(agentsDir, backupDir, "run-002", nil, nil)
	if err != nil {
		t.Fatalf("newLockProtocolState (run-002): %v", err)
	}
	// state2 keeps its lock held throughout the test; release it at the end
	// so the temp dir can be cleaned up on Windows.
	t.Cleanup(func() { cleanupState(state2) })

	// Act: run-001 exits (non-last-out because run-002 still holds its lock).
	if err := state1.lastOutCheck(); err != nil {
		t.Fatalf("lastOutCheck (run-001): %v", err)
	}

	// Assert: .restoring file exists (created by the protocol, NOT deleted by
	// non-last-out exit).
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)
	if _, statErr := os.Stat(restoringPath); os.IsNotExist(statErr) {
		t.Error(".restoring must remain on disk after non-last-out exit; file is absent")
	}
}

// TestLockProtocol_LastOutCheck_NonLastOut_RestoreNotTriggered verifies that
// when a non-last-out run calls lastOutCheck, RestoreFromBackup is NOT called
// (another run is still active and holds the backup state).
func TestLockProtocol_LastOutCheck_NonLastOut_RestoreNotTriggered(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	var restoreCalled bool
	noRestore := func(aDir, bDir string) error {
		restoreCalled = true
		return nil
	}

	state1 := mustNewState(t, agentsDir, backupDir, "run-001", noRestore)
	state2, err := newLockProtocolState(agentsDir, backupDir, "run-002", nil, nil)
	if err != nil {
		t.Fatalf("newLockProtocolState (run-002): %v", err)
	}
	t.Cleanup(func() { cleanupState(state2) })

	// Act
	if err := state1.lastOutCheck(); err != nil {
		t.Fatalf("lastOutCheck (run-001): %v", err)
	}

	// Assert: restore was not called.
	if restoreCalled {
		t.Error("non-last-out exit must NOT call restore; restoreFunc was called")
	}
}

// ---------------------------------------------------------------------------
// T7.2(c): orphaned lock files are deleted during last-out check
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_OrphanedLocksDeletedDuringLastOut verifies
// that during the last-out check, lock files that are acquirable (i.e. orphaned
// -- the owning process crashed or exited without cleanup) are deleted so that
// the backup directory can be fully removed.
func TestLockProtocol_LastOutCheck_OrphanedLocksDeletedDuringLastOut(t *testing.T) {
	// Arrange: set up a full backup state with two lock files.
	// run-crashed: acquired and immediately released (orphaned).
	// run-active: this run will call lastOutCheck and is the last out.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	// Simulate a crashed run: acquire lock then release it (file remains, no lock held).
	orphanedLockPath := backup.RunLockPath(backupDir, "run-crashed")
	h, err := filelock.Lock(orphanedLockPath)
	if err != nil {
		t.Fatalf("Lock (orphaned): %v", err)
	}
	if err := h.Unlock(); err != nil {
		t.Fatalf("Unlock (orphaned): %v", err)
	}
	// Verify the orphaned lock file exists before lastOutCheck.
	if _, statErr := os.Stat(orphanedLockPath); statErr != nil {
		t.Fatalf("orphaned lock file should exist before lastOutCheck: %v", statErr)
	}

	// The active run that will call lastOutCheck.
	state := mustNewState(t, agentsDir, backupDir, "run-active", nil)

	// Act
	if err := state.lastOutCheck(); err != nil {
		t.Fatalf("lastOutCheck: %v", err)
	}

	// Assert: orphaned lock file was deleted during last-out cleanup.
	// The backup directory itself should also be gone (last-out teardown).
	if _, statErr := os.Stat(orphanedLockPath); !os.IsNotExist(statErr) {
		t.Error("orphaned lock file should be deleted during last-out check; still exists")
	}
}

// ---------------------------------------------------------------------------
// T7.2(d): .restoring acquired via blocking Lock before own lock released
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_RestoringAcquiredBeforeOwnLockRelease verifies
// that inside lastOutCheck, .restoring is acquired via a blocking Lock call
// BEFORE the run's own .lock-{runID} is released. The afterRestoringAcquired
// test hook is called at this point; the test asserts that at hook time:
//   - .restoring is held (TryLock from another handle returns false), and
//   - own run lock is still held (TryLock from another handle returns false).
func TestLockProtocol_LastOutCheck_RestoringAcquiredBeforeOwnLockRelease(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	runID := "run-001"
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)
	runLockPath := backup.RunLockPath(backupDir, runID)

	state := mustNewState(t, agentsDir, backupDir, runID, nil)

	var (
		hookCalled          bool
		restoringWasHeld    bool
		ownLockWasStillHeld bool
	)

	state.afterRestoringAcquired = func() {
		hookCalled = true

		// .restoring must be locked by lastOutCheck at this point.
		hRestoring, restoringOk, err := filelock.TryLock(restoringPath)
		if err == nil && restoringOk && hRestoring != nil {
			// TryLock succeeded: .restoring was NOT held -- unexpected.
			hRestoring.Unlock() //nolint:errcheck
			restoringWasHeld = false
		} else {
			restoringWasHeld = true
		}

		// Own run lock must still be held at this point (acquired before .restoring).
		hOwn, ownOk, err := filelock.TryLock(runLockPath)
		if err == nil && ownOk && hOwn != nil {
			// TryLock succeeded: own lock was already released -- unexpected.
			hOwn.Unlock() //nolint:errcheck
			ownLockWasStillHeld = false
		} else {
			ownLockWasStillHeld = true
		}
	}

	// Act
	_ = state.lastOutCheck() // may return error from stub

	// Assert
	if !hookCalled {
		t.Error("afterRestoringAcquired hook was never called; implementation must invoke it")
		return
	}
	if !restoringWasHeld {
		t.Error(".restoring was not held when afterRestoringAcquired hook fired; " +
			"implementation must acquire .restoring via blocking Lock before calling the hook")
	}
	if !ownLockWasStillHeld {
		t.Error("own run lock was already released when afterRestoringAcquired hook fired; " +
			"implementation must acquire .restoring BEFORE releasing own lock")
	}
}

// ---------------------------------------------------------------------------
// T7.2(e): manifest deleted while .restoring is held (Windows-safe ordering)
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_ManifestDeletedWhileRestoringHeld verifies the
// Windows-safe delete ordering inside the last-out path: the manifest is deleted
// while .restoring is still held. The afterManifestDeleted hook fires after the
// manifest is deleted; the test asserts:
//   - .restoring is still held at hook time (TryLock returns false), and
//   - the manifest file is absent at hook time.
func TestLockProtocol_LastOutCheck_ManifestDeletedWhileRestoringHeld(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)
	manifestPath := filepath.Join(backupDir, manifest.ManifestFileName)

	state := mustNewState(t, agentsDir, backupDir, "run-001", nil)

	var (
		hookCalled        bool
		restoringWasHeld  bool
		manifestWasAbsent bool
	)

	state.afterManifestDeleted = func() {
		hookCalled = true

		// .restoring must still be locked.
		hRestoring, restoringOk, err := filelock.TryLock(restoringPath)
		if err == nil && restoringOk && hRestoring != nil {
			hRestoring.Unlock() //nolint:errcheck
			restoringWasHeld = false
		} else {
			restoringWasHeld = true
		}

		// Manifest must already be gone.
		_, statErr := os.Stat(manifestPath)
		manifestWasAbsent = os.IsNotExist(statErr)
	}

	// Act
	_ = state.lastOutCheck()

	// Assert
	if !hookCalled {
		t.Error("afterManifestDeleted hook was never called; " +
			"implementation must invoke it after deleting the manifest")
		return
	}
	if !restoringWasHeld {
		t.Error(".restoring was not held when afterManifestDeleted hook fired; " +
			"manifest must be deleted while .restoring is still held (Windows-safe ordering)")
	}
	if !manifestWasAbsent {
		t.Error("manifest was still present when afterManifestDeleted hook fired; " +
			"implementation must delete the manifest before calling the hook")
	}
}

// ---------------------------------------------------------------------------
// T7.2(g): two simultaneous exits -- exactly one performs restore
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_TwoSimultaneousExits_ExactlyOneRestores verifies
// that when two runs call lastOutCheck concurrently, both serialize on .restoring
// (blocking Lock) and exactly one triggers restore + teardown. After both goroutines
// complete:
//   - The injected restoreFunc was called exactly once.
//   - The backup directory no longer exists.
func TestLockProtocol_LastOutCheck_TwoSimultaneousExits_ExactlyOneRestores(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	var restoreCount int32
	countingRestore := func(aDir, bDir string) error {
		atomic.AddInt32(&restoreCount, 1)
		return backup.RestoreFromBackup(aDir, bDir)
	}

	state1 := mustNewState(t, agentsDir, backupDir, "run-001", countingRestore)
	state2 := mustNewState(t, agentsDir, backupDir, "run-002", countingRestore)

	var wg sync.WaitGroup
	startCh := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-startCh
		// Errors from lastOutCheck in this goroutine are non-fatal to the test
		// (the aggregate assertion below catches the actual invariant).
		if err := state1.lastOutCheck(); err != nil {
			t.Logf("state1.lastOutCheck: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		<-startCh
		if err := state2.lastOutCheck(); err != nil {
			t.Logf("state2.lastOutCheck: %v", err)
		}
	}()

	close(startCh) // release both goroutines simultaneously
	wg.Wait()

	// Assert: exactly one restore.
	if got := atomic.LoadInt32(&restoreCount); got != 1 {
		t.Errorf("expected exactly 1 restore call, got %d", got)
	}

	// Assert: backup directory is gone.
	if _, statErr := os.Stat(backupDir); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should be removed after all runs exit; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Windows-safe ordering: .setup-complete deleted before .restoring released
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_SetupCompleteDeletedBeforeRestoringReleased
// verifies that .setup-complete is deleted while .restoring is still held,
// ensuring the Windows-safe delete ordering is respected. The
// afterSetupCompleteDeleted hook fires after .setup-complete is deleted; at
// hook time:
//   - .setup-complete must be absent on disk, and
//   - .restoring must still be held (TryLock from another handle returns false).
func TestLockProtocol_LastOutCheck_SetupCompleteDeletedBeforeRestoringReleased(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	setupCompletePath := filepath.Join(backupDir, backup.SetupCompleteFileName)
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	state := mustNewState(t, agentsDir, backupDir, "run-001", nil)

	var (
		hookCalled          bool
		setupCompleteAbsent bool
		restoringStillHeld  bool
	)

	state.afterSetupCompleteDeleted = func() {
		hookCalled = true

		// .setup-complete must be gone when this hook fires.
		_, statErr := os.Stat(setupCompletePath)
		setupCompleteAbsent = os.IsNotExist(statErr)

		// .restoring must still be locked (not yet released).
		h, ok, err := filelock.TryLock(restoringPath)
		if err == nil && ok && h != nil {
			// TryLock succeeded: .restoring was not held -- unexpected.
			h.Unlock() //nolint:errcheck
			restoringStillHeld = false
		} else {
			restoringStillHeld = true
		}
	}

	// Act
	_ = state.lastOutCheck()

	// Assert
	if !hookCalled {
		t.Error("afterSetupCompleteDeleted hook was never called; " +
			"implementation must invoke it after deleting .setup-complete")
		return
	}
	if !setupCompleteAbsent {
		t.Error(".setup-complete was still present when afterSetupCompleteDeleted hook fired; " +
			"implementation must delete .setup-complete before calling the hook")
	}
	if !restoringStillHeld {
		t.Error(".restoring was not held when afterSetupCompleteDeleted hook fired; " +
			".setup-complete must be deleted while .restoring is still held (Windows-safe ordering)")
	}
}

// ---------------------------------------------------------------------------
// RemoveAll failure is non-fatal: logged, does not change exit code
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_RemoveAllFailure_LoggedNonFatal verifies that
// when the backup directory removal fails during last-out teardown, the error
// is logged via the injected DebugLogger and lastOutCheck returns nil.
// The restore has already succeeded at this point; a failed removal leaves a
// stale empty directory that the next startup recovery will clean up.
func TestLockProtocol_LastOutCheck_RemoveAllFailure_LoggedNonFatal(t *testing.T) {
	// Arrange: sole active run with a spy logger and a removeAllFunc that fails.
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)

	spy := &spyLogger{}
	state, err := newLockProtocolState(agentsDir, backupDir, "run-001", nil, spy)
	if err != nil {
		t.Fatalf("newLockProtocolState: %v", err)
	}
	// Override the removal function so that teardown fails.
	state.removeAllFunc = func(_ string) error {
		return errors.New("simulated RemoveAll failure")
	}

	// Act
	gotErr := state.lastOutCheck()

	// Assert: non-fatal -- lastOutCheck must return nil despite RemoveAll failure.
	if gotErr != nil {
		t.Errorf("lastOutCheck must return nil when RemoveAll fails (non-fatal); got: %v", gotErr)
	}
	// Assert: DebugLogger received the snapshot cleanup-failed event.
	if !spy.hasEvent(domain.EventSnapshotCleanupFailed) {
		t.Errorf("expected DebugLogger to receive event %q on RemoveAll failure; no such event logged",
			domain.EventSnapshotCleanupFailed)
	}
}
