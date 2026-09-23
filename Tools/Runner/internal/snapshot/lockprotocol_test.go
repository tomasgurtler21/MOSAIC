package snapshot

// Tests for the lock protocol lifecycle and last-out exit behavior.
//
// This file is an internal package test (package snapshot) to allow direct
// access to unexported types (LockProtocolState, newLockProtocolState,
// lastOutCheck) and test hooks (afterRestoringAcquired, afterManifestDeleted).
// External tests via BackupState.Cleanup are in the Stage 9 test file.
//
// All tests use t.TempDir() for filesystem isolation.
//
// T7.1 -- Per-run lock lifecycle:
//   (a) newLockProtocolState acquires .lock-{runID} inside the backup directory.
//   (b) The acquired run lock is exclusive: TryLock from a second handle returns false.
//   (c) Unlock releases the lock; the file can then be deleted (Windows-safe pattern).
//   (d) After unlock (simulating OS crash-release), TryLock on the same path succeeds.
//
// T7.2 -- Last-out check with backup directory teardown:
//   (a) Single run exits: restore runs (agent file restored to original) followed
//       by backup directory teardown.
//   (b) Non-last-out exit: .restoring is released WITHOUT deleting it; restore is
//       NOT triggered; .restoring file remains on disk.
//   (c) Orphaned lock files (acquirable) are deleted during last-out check.
//   (d) .restoring is acquired via blocking Lock BEFORE own lock is released.
//   (e) Windows-safe delete ordering: manifest is deleted while .restoring is held.
//   (f) After successful last-out teardown, the backup directory no longer exists.
//   (g) Two simultaneous exits: both serialize on .restoring; exactly one restores.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupFullBackupState creates a ready-to-use backup-and-transform state under
// base: creates agentsDir with a transformable worker.md, creates backupDir,
// copies agents to backup, writes manifest, writes recovery marker, transforms
// in place, and writes setup-complete. Returns agentsDir and backupDir.
func setupFullBackupState(t *testing.T, base string) (agentsDir, backupDir string) {
	t.Helper()

	agentsDir = filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir agentsDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(agentsDir, "worker.md"),
		[]byte("---\nmode: subagent\n---\n\nBody.\n"),
		0o644,
	); err != nil {
		t.Fatalf("setup: write worker.md: %v", err)
	}

	backupDir = filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}

	rules := TransformationsFor("opencode")

	if err := CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: CopyAgentsToBackup: %v", err)
	}
	if err := WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("setup: WriteManifest: %v", err)
	}
	if err := WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: WriteRecoveryMarker: %v", err)
	}
	if err := TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: TransformInPlace: %v", err)
	}
	if err := WriteSetupComplete(backupDir); err != nil {
		t.Fatalf("setup: WriteSetupComplete: %v", err)
	}

	return agentsDir, backupDir
}

// mustNewState creates a LockProtocolState, failing the test if creation fails.
// restoreFunc may be nil to use the default RestoreFromBackup.
func mustNewState(t *testing.T, agentsDir, backupDir, runID string, restoreFunc func(string, string) error) *LockProtocolState {
	t.Helper()
	state, err := newLockProtocolState(agentsDir, backupDir, runID, restoreFunc, nil)
	if err != nil {
		t.Fatalf("newLockProtocolState(%q): %v", runID, err)
	}
	return state
}

// cleanupState releases the run lock held by state, if any. Used in t.Cleanup
// to ensure test temp dirs can be removed on Windows (locked files block deletion).
func cleanupState(state *LockProtocolState) {
	if state == nil {
		return
	}
	if state.runLock != nil {
		state.runLock.Unlock() //nolint:errcheck
	}
}

// spyLogger is a minimal domain.DebugLogger spy used in tests that need to
// assert that specific log events were emitted. Safe for concurrent use.
type spyLogger struct {
	mu     sync.Mutex
	events []string
}

func (s *spyLogger) Log(event string, _ string, _ ...domain.DebugField) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *spyLogger) hasEvent(event string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e == event {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T7.1(a): newLockProtocolState acquires the per-run lock file
// ---------------------------------------------------------------------------

// TestLockProtocol_NewState_AcquiresRunLockFileInBackupDir verifies that
// newLockProtocolState creates and acquires the .lock-{runID} file inside the
// backup directory.
func TestLockProtocol_NewState_AcquiresRunLockFileInBackupDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := setupFullBackupState(t, base)
	runID := "run-001"
	lockFilePath := RunLockPath(backupDir, runID)

	// Act
	state, err := newLockProtocolState(agentsDir, backupDir, runID, nil, nil)
	if err != nil {
		t.Fatalf("newLockProtocolState: %v", err)
	}
	t.Cleanup(func() { cleanupState(state) })

	// Assert: .lock-run-001 exists inside the backup directory.
	if _, statErr := os.Stat(lockFilePath); statErr != nil {
		t.Errorf(".lock-{runID} file not created in backupDir: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// T7.1(b): acquired run lock is exclusive
// ---------------------------------------------------------------------------

// TestLockProtocol_AcquiredRunLock_IsExclusive verifies that while a
// LockProtocolState holds .lock-{runID}, a second TryLock attempt on the same
// path (via a separate handle) returns false (contention).
func TestLockProtocol_AcquiredRunLock_IsExclusive(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := setupFullBackupState(t, base)
	runID := "run-001"
	state := mustNewState(t, agentsDir, backupDir, runID, nil)
	t.Cleanup(func() { cleanupState(state) })

	// Act: try to acquire the same lock via a separate handle.
	h2, ok, err := filelock.TryLock(RunLockPath(backupDir, runID))
	if err != nil {
		t.Fatalf("TryLock (second handle): unexpected error: %v", err)
	}

	// Assert: contention; lock is held by the state.
	if ok {
		if h2 != nil {
			h2.Unlock() //nolint:errcheck
		}
		t.Error("TryLock on held run lock: expected ok=false, got true")
	}
}

// ---------------------------------------------------------------------------
// T7.1(c): Unlock releases the lock; file can be deleted
// ---------------------------------------------------------------------------

// TestLockProtocol_RunLockPath_UnlockAllowsFileDeletion verifies the
// Unlock-before-delete pattern using RunLockPath: after acquiring a run lock
// and releasing it, the lock file can be deleted with os.Remove.
// On Windows, attempting to delete a locked file fails; this test confirms
// the protocol's delete ordering is correct.
func TestLockProtocol_RunLockPath_UnlockAllowsFileDeletion(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	runID := "run-001"
	lockPath := RunLockPath(backupDir, runID)

	h, err := filelock.Lock(lockPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// Act: unlock, then delete.
	if err := h.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Errorf("os.Remove after Unlock: %v", err)
	}

	// Assert: file is gone.
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Errorf("lock file should be absent after Remove; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// T7.1(d): after unlock (simulated crash), lock becomes acquirable
// ---------------------------------------------------------------------------

// TestLockProtocol_RunLockPath_AfterUnlock_LockBecomesAcquirable verifies that
// after calling Unlock on a run lock handle (simulating the OS releasing the
// lock on process exit or crash), a subsequent TryLock on the same path
// returns ok=true.
func TestLockProtocol_RunLockPath_AfterUnlock_LockBecomesAcquirable(t *testing.T) {
	// Arrange
	backupDir := t.TempDir()
	runID := "run-crashed"
	lockPath := RunLockPath(backupDir, runID)

	// Acquire and immediately release (simulating a crash where the OS releases).
	h, err := filelock.Lock(lockPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := h.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// Act: another handle should now be able to acquire the lock.
	h2, ok, err := filelock.TryLock(lockPath)
	if err != nil {
		t.Fatalf("TryLock after Unlock: %v", err)
	}

	// Assert
	if !ok {
		t.Error("TryLock after Unlock: expected ok=true, got false")
	}
	if h2 == nil {
		t.Error("TryLock after Unlock: returned nil handle with ok=true")
	}
	if h2 != nil {
		h2.Unlock() //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// T7.2(a): single run -- restore runs, backup dir removed
// ---------------------------------------------------------------------------

// TestLockProtocol_LastOutCheck_SingleRun_RestoresAgentContent verifies that
// when the sole active run calls lastOutCheck, RestoreFromBackup is invoked
// and the agent file content is restored to its pre-transform state.
func TestLockProtocol_LastOutCheck_SingleRun_RestoresAgentContent(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := setupFullBackupState(t, base)
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
	agentsDir, backupDir := setupFullBackupState(t, base)

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
	agentsDir, backupDir := setupFullBackupState(t, base)

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
	restoringPath := filepath.Join(backupDir, RestoringFileName)
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
	agentsDir, backupDir := setupFullBackupState(t, base)

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
	agentsDir, backupDir := setupFullBackupState(t, base)

	// Simulate a crashed run: acquire lock then release it (file remains, no lock held).
	orphanedLockPath := RunLockPath(backupDir, "run-crashed")
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
	agentsDir, backupDir := setupFullBackupState(t, base)
	runID := "run-001"
	restoringPath := filepath.Join(backupDir, RestoringFileName)
	runLockPath := RunLockPath(backupDir, runID)

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
	agentsDir, backupDir := setupFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, RestoringFileName)
	manifestPath := filepath.Join(backupDir, ManifestFileName)

	state := mustNewState(t, agentsDir, backupDir, "run-001", nil)

	var (
		hookCalled          bool
		restoringWasHeld    bool
		manifestWasAbsent   bool
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
	agentsDir, backupDir := setupFullBackupState(t, base)

	var restoreCount int32
	countingRestore := func(aDir, bDir string) error {
		atomic.AddInt32(&restoreCount, 1)
		return RestoreFromBackup(aDir, bDir)
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
// Constructor error path: missing backup directory
// ---------------------------------------------------------------------------

// TestLockProtocol_NewState_MissingBackupDir_ReturnsError verifies that
// newLockProtocolState returns a non-nil error when backupDir does not exist.
// The function cannot acquire the run lock file inside a directory that does
// not exist, so it must surface the failure to the caller.
func TestLockProtocol_NewState_MissingBackupDir_ReturnsError(t *testing.T) {
	// Arrange: agentsDir exists, backupDir's parent path does not.
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agentsDir: %v", err)
	}
	nonExistentBackupDir := filepath.Join(base, "does-not-exist", BackupDirName)

	// Act
	state, err := newLockProtocolState(agentsDir, nonExistentBackupDir, "run-001", nil, nil)
	if state != nil {
		t.Cleanup(func() { cleanupState(state) })
	}

	// Assert: a non-nil error is returned when backupDir does not exist.
	if err == nil {
		t.Error("newLockProtocolState with non-existent backupDir: expected non-nil error, got nil")
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
	agentsDir, backupDir := setupFullBackupState(t, base)
	setupCompletePath := filepath.Join(backupDir, SetupCompleteFileName)
	restoringPath := filepath.Join(backupDir, RestoringFileName)

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
	agentsDir, backupDir := setupFullBackupState(t, base)

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

// ===========================================================================
// Stage 8: Creator and Join
//
// T8.1 -- Concurrent-join protocol
// T8.2 -- Race scenario interleaving via injectable hooks
// T8.3 -- Creator failure handling
//
// These tests are TDD RED phase: they require setupAsCreator and setupAsJoiner
// to be implemented (Stage 8 I8.1/I8.2) and will fail until then.
//
// New LockProtocolState fields required for Stage 8 (added as stubs):
//   pollInterval, pollTimeout time.Duration
//   afterCreatorLockAcquired, afterCreatorAgentsCopied func()
//   afterJoinerLockAcquired func()
//   onManifestPollTick, onSetupCompletePollTick, onRestoringPollTick func()
//
// New package-level sentinels required for Stage 8 (added as stubs):
//   errStartFresh, errPollTimeout, errRetriesExceeded
// ===========================================================================

// ---------------------------------------------------------------------------
// Stage 8 helpers
// ---------------------------------------------------------------------------

// newUnlockedStateForTest initializes a LockProtocolState without acquiring
// any run lock. Used in setupAsCreator and setupAsJoiner tests where lock
// acquisition is part of the code path being tested.
//
// pollInterval is set to 5ms and pollTimeout to 500ms. Callers may override
// these before calling setupAsCreator/setupAsJoiner.
func newUnlockedStateForTest(agentsDir, backupDir, runID string) *LockProtocolState {
	return &LockProtocolState{
		agentsDir:    agentsDir,
		backupDir:    backupDir,
		runID:        runID,
		restoreFunc:  RestoreFromBackup,
		pollInterval: 5 * time.Millisecond,
		pollTimeout:  500 * time.Millisecond,
	}
}

// setupBackupStateWithManifestOnly creates agentsDir with worker.md,
// creates backupDir, copies agents to backup, and writes the manifest.
// Does NOT run TransformInPlace or write .setup-complete.
// Used for joiner tests exercising the setup-complete-wait path.
func setupBackupStateWithManifestOnly(t *testing.T, base string) (agentsDir, backupDir string) {
	t.Helper()
	agentsDir = filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir agentsDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(agentsDir, "worker.md"),
		[]byte("---\nmode: subagent\n---\n\nBody.\n"),
		0o644,
	); err != nil {
		t.Fatalf("setup: write worker.md: %v", err)
	}
	backupDir = filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	rules := TransformationsFor("opencode")
	if err := CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: CopyAgentsToBackup: %v", err)
	}
	if err := WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("setup: WriteManifest: %v", err)
	}
	return agentsDir, backupDir
}

// newAgentsDir creates agentsDir with a single transformable worker.md.
// No backup directory is created. Used as the starting state for creator tests.
func newAgentsDir(t *testing.T, base string) string {
	t.Helper()
	agentsDir := filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir agentsDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(agentsDir, "worker.md"),
		[]byte("---\nmode: subagent\n---\n\nBody.\n"),
		0o644,
	); err != nil {
		t.Fatalf("setup: write worker.md: %v", err)
	}
	return agentsDir
}

// agentFileIsOriginal returns true when worker.md in agentsDir still
// contains the original content ("mode: subagent"), meaning it has NOT
// been transformed in place.
func agentFileIsOriginal(t *testing.T, agentsDir string) bool {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(agentsDir, "worker.md"))
	if err != nil {
		t.Fatalf("agentFileIsOriginal: ReadFile: %v", err)
	}
	return strings.Contains(string(content), "mode: subagent")
}

// testRunDispatchLoop is a test-only helper that simulates the bounded retry
// dispatch loop for T8.3(d). It mirrors the production SetupBackupAndTransform
// dispatch logic: call CreateBackupDir, dispatch to creator or joiner, retry
// on errStartFresh up to maxRetries times.
//
// In this helper, each creator attempt is forced to fail with errStartFresh by
// deleting the backup directory BEFORE lock acquisition, guaranteeing the loop
// exhausts retries and returns errRetriesExceeded.
func testRunDispatchLoop(t *testing.T, agentsDir, runID string, rules []TransformRule, maxRetries int) error {
	t.Helper()
	for attempt := 0; attempt <= maxRetries; attempt++ {
		backupDir, alreadyExisted, err := CreateBackupDir(agentsDir)
		if err != nil {
			return err
		}
		if alreadyExisted {
			return fmt.Errorf("testRunDispatchLoop: unexpected alreadyExisted on attempt %d", attempt)
		}

		// Delete backup dir before state creation to guarantee errStartFresh: lock
		// acquisition inside setupAsCreator will fail because the parent dir is gone.
		if removeErr := os.RemoveAll(backupDir); removeErr != nil {
			t.Fatalf("testRunDispatchLoop: RemoveAll: %v", removeErr)
		}

		state := &LockProtocolState{
			agentsDir:    agentsDir,
			backupDir:    backupDir,
			runID:        runID,
			restoreFunc:  RestoreFromBackup,
			pollInterval: 5 * time.Millisecond,
			pollTimeout:  50 * time.Millisecond,
		}
		t.Cleanup(func() { cleanupState(state) })

		loopErr := state.setupAsCreator(rules)
		if loopErr == nil {
			return nil
		}
		if !errors.Is(loopErr, errStartFresh) {
			return loopErr
		}
		// errStartFresh: continue to next attempt.
	}
	return errRetriesExceeded
}

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
	agentsDir, backupDir := setupFullBackupState(t, base)
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
	h, ok, tryErr := filelock.TryLock(RunLockPath(backupDir, "run-joiner"))
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
	agentsDir, backupDir := setupBackupStateWithManifestOnly(t, base)
	rules := TransformationsFor("opencode")
	if err := TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: TransformInPlace: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")

	// Write .setup-complete after a delay (simulating creator finishing transforms).
	go func() {
		time.Sleep(25 * time.Millisecond)
		if writeErr := WriteSetupComplete(backupDir); writeErr != nil {
			t.Logf("background WriteSetupComplete: %v", writeErr)
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
	agentsDir, backupDir := setupFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, RestoringFileName)

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
// T8.1(d): manifest-wait detects backup-dir-gone, .restoring-held, and timeout
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_ManifestWait_BackupDirDisappears_StartsFresh verifies that
// when the backup directory is deleted while the joiner is waiting for the
// manifest, setupAsJoiner returns errStartFresh instead of timing out.
func TestSetupAsJoiner_ManifestWait_BackupDirDisappears_StartsFresh(t *testing.T) {
	// Arrange: backup dir without manifest.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
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

// TestSetupAsJoiner_ManifestWait_RestoringBecomesHeld_EntersRestoringPoll verifies
// the teardown-interleaving behavior: when .restoring becomes held during the
// manifest-wait loop (indicating last-out teardown), the joiner enters the
// .restoring re-poll loop. Once .restoring is released and the backup dir is gone,
// the joiner returns errStartFresh.
func TestSetupAsJoiner_ManifestWait_RestoringBecomesHeld_EntersRestoringPoll(t *testing.T) {
	// Arrange: backup dir exists, no manifest (joiner is in manifest-wait).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	restoringPath := filepath.Join(backupDir, RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})
	var once sync.Once
	joiner.onManifestPollTick = func() {
		once.Do(func() {
			go func() {
				h, lockErr := filelock.Lock(restoringPath)
				if lockErr != nil {
					return
				}
				close(restoringHeld)
				time.Sleep(60 * time.Millisecond)
				os.RemoveAll(backupDir) // teardown: backup dir gone while .restoring held
				h.Unlock()              //nolint:errcheck
			}()
			<-restoringHeld
		})
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: .restoring re-poll detected backup dir gone -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh after .restoring held during manifest wait and backup dir disappeared; got %v", err)
	}
}

// TestSetupAsJoiner_ManifestWait_Timeout_ReturnsPollTimeout verifies that when
// the manifest never appears and the backup directory remains, setupAsJoiner
// returns errPollTimeout after the deadline is exceeded.
func TestSetupAsJoiner_ManifestWait_Timeout_ReturnsPollTimeout(t *testing.T) {
	// Arrange: backup dir with no manifest (manifest never written).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
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
	agentsDir, backupDir := setupFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, RestoringFileName)

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
// T8.1(f): joiner verifies own lock file still exists after acquisition
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_OwnLockFileDeletedAfterAcquisition_StartsFresh verifies
// the m4 robustness check: after acquiring its own run lock, the joiner
// verifies the lock file still exists on disk. If another process deleted
// the file between creation and locking, the joiner detects this and returns
// errStartFresh.
//
// This scenario cannot occur on Windows (locked files cannot be deleted), so
// the test is skipped on that platform.
func TestSetupAsJoiner_OwnLockFileDeletedAfterAcquisition_StartsFresh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot unlink a locked file on Windows; this scenario does not occur on this platform")
	}

	// Arrange: full backup state.
	base := t.TempDir()
	agentsDir, backupDir := setupFullBackupState(t, base)
	joinerLockPath := RunLockPath(backupDir, "run-joiner")

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.afterJoinerLockAcquired = func() {
		// Delete the joiner's lock file while the lock is held (Unix only).
		// The OS retains the inode (lock is still held via the open FD), but
		// os.Stat on the path will return not-found.
		os.Remove(joinerLockPath) //nolint:errcheck
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: joiner detected its lock file was deleted and returned errStartFresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when joiner's lock file was deleted after acquisition; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.1(g): manifest deleted mid-wait by last-out run (teardown interleaving)
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_SetupCompleteWait_TeardownDetected_StartsFresh verifies
// the teardown-interleaving behavior during setup-complete-wait: when the
// backup directory disappears (last-out run tore it down after all runs exited),
// the joiner detects this on a poll tick and returns errStartFresh instead of
// timing out waiting for .setup-complete.
func TestSetupAsJoiner_SetupCompleteWait_TeardownDetected_StartsFresh(t *testing.T) {
	// Arrange: backup dir with manifest and transformed agents, no .setup-complete.
	base := t.TempDir()
	agentsDir, backupDir := setupBackupStateWithManifestOnly(t, base)
	rules := TransformationsFor("opencode")
	if err := TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: TransformInPlace: %v", err)
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

// ---------------------------------------------------------------------------
// T8.2(a): Race (b) -- joiner acquires lock, then last-out holds .restoring
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceB_RestoringHeldAfterLockAcquisition_StartsFresh
// verifies Race (b) interleaving: the joiner acquires its own run lock, then
// a concurrent last-out run acquires .restoring and begins teardown. The
// joiner's post-lock re-verification detects .restoring as held, releases its
// own lock, enters the .restoring re-poll loop, and returns errStartFresh when
// the backup directory disappears.
func TestSetupAsJoiner_RaceB_RestoringHeldAfterLockAcquisition_StartsFresh(t *testing.T) {
	// Arrange: full backup state.
	base := t.TempDir()
	agentsDir, backupDir := setupFullBackupState(t, base)
	restoringPath := filepath.Join(backupDir, RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})

	joiner.afterJoinerLockAcquired = func() {
		// Last-out run acquires .restoring and tears down while joiner is about
		// to perform post-lock re-verification.
		go func() {
			h, lockErr := filelock.Lock(restoringPath)
			if lockErr != nil {
				return
			}
			close(restoringHeld)
			time.Sleep(60 * time.Millisecond)
			os.RemoveAll(backupDir)
			h.Unlock() //nolint:errcheck
		}()
		<-restoringHeld
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert
	if !errors.Is(err, errStartFresh) {
		t.Errorf("Race (b): expected errStartFresh after last-out teardown during post-lock re-verify; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(b): Race (a) partial backup -- no manifest, joiner times out
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceA_PartialBackupNoManifest_TimesOut verifies that when
// a creator crashes after creating the backup directory but before writing the
// manifest, a concurrent joiner's manifest-wait poll eventually times out with
// errPollTimeout. (Recovery of the partial backup is handled by RecoveryCheck
// at startup, tested in Stage 9. This test verifies the joiner side.)
func TestSetupAsJoiner_RaceA_PartialBackupNoManifest_TimesOut(t *testing.T) {
	// Arrange: backup dir exists (creator mkdir'd) but no manifest (creator crashed).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollInterval = 5 * time.Millisecond
	joiner.pollTimeout = 20 * time.Millisecond

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: manifest never appeared; joiner timed out rather than blocking forever.
	if !errors.Is(err, errPollTimeout) {
		t.Errorf("Race (a) partial backup: expected errPollTimeout; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(c): Race (c) -- joiner waits for creator to finish transforms
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_RaceC_WaitsForCreatorToFinishTransforms verifies Race (c)
// interleaving: a joiner that arrives while the creator has written the manifest
// but not yet .setup-complete (transforms still in progress) blocks until the
// creator writes .setup-complete, then proceeds.
func TestSetupAsJoiner_RaceC_WaitsForCreatorToFinishTransforms(t *testing.T) {
	// Arrange: backup dir with manifest, transforms complete, no .setup-complete.
	base := t.TempDir()
	agentsDir, backupDir := setupBackupStateWithManifestOnly(t, base)
	rules := TransformationsFor("opencode")
	if err := TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: TransformInPlace: %v", err)
	}

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 500 * time.Millisecond

	// Simulate creator writing .setup-complete after finishing transforms.
	go func() {
		time.Sleep(30 * time.Millisecond)
		if writeErr := WriteSetupComplete(backupDir); writeErr != nil {
			t.Logf("background WriteSetupComplete: %v", writeErr)
		}
	}()

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert
	if err != nil {
		t.Errorf("Race (c): expected nil after waiting for creator to write .setup-complete; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// T8.2(d): Race (a) live concurrent start -- exactly one caller wins os.Mkdir
// ---------------------------------------------------------------------------

// TestCreateBackupDir_ConcurrentCallers_ExactlyOneWinsCreation verifies that
// when multiple runs call CreateBackupDir concurrently, exactly one receives
// alreadyExisted=false (the winner that created the directory) and all others
// receive alreadyExisted=true (joiners). This is the concurrency invariant
// that drives the creator/joiner dispatch.
func TestCreateBackupDir_ConcurrentCallers_ExactlyOneWinsCreation(t *testing.T) {
	const numCallers = 6

	// Arrange: fresh agents dir, no backup dir yet.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)

	type result struct {
		alreadyExisted bool
		err            error
	}
	results := make([]result, numCallers)
	var wg sync.WaitGroup
	start := make(chan struct{})

	wg.Add(numCallers)
	for i := range numCallers {
		go func(idx int) {
			defer wg.Done()
			<-start
			_, existed, err := CreateBackupDir(agentsDir)
			results[idx] = result{alreadyExisted: existed, err: err}
		}(i)
	}

	// Act: release all goroutines simultaneously.
	close(start)
	wg.Wait()

	// Assert: all calls succeeded.
	for i, r := range results {
		if r.err != nil {
			t.Errorf("caller %d: CreateBackupDir error: %v", i, r.err)
		}
	}

	// Assert: exactly one got alreadyExisted=false (the winner).
	var winners int
	for _, r := range results {
		if !r.alreadyExisted {
			winners++
		}
	}
	if winners != 1 {
		t.Errorf("expected exactly 1 caller to win os.Mkdir (alreadyExisted=false), got %d", winners)
	}
}

// ---------------------------------------------------------------------------
// T8.3(a): creator lock creation fails because backup dir absent
// ---------------------------------------------------------------------------

// TestSetupAsCreator_BackupDirAbsent_ReturnsErrStartFresh verifies that when
// the backup directory does not exist at the time setupAsCreator attempts lock
// acquisition, setupAsCreator returns errStartFresh (signaling the dispatch
// loop to retry from CreateBackupDir). The creator must NOT apply any
// transforms without a valid lock.
func TestSetupAsCreator_BackupDirAbsent_ReturnsErrStartFresh(t *testing.T) {
	// Arrange: agents dir exists; backup dir does not.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	nonExistentBackupDir := filepath.Join(base, BackupDirName) // never created

	state := newUnlockedStateForTest(agentsDir, nonExistentBackupDir, "run-creator")

	// Act
	err := state.setupAsCreator(TransformationsFor("opencode"))

	// Assert
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when backup dir is absent; got %v", err)
	}

	// Assert: agent file was NOT transformed.
	if !agentFileIsOriginal(t, agentsDir) {
		t.Error("creator must not transform agents when lock acquisition fails (backup dir absent)")
	}
}

// ---------------------------------------------------------------------------
// T8.3(b): CopyAgentsToBackup fails because backup dir vanishes after lock
// ---------------------------------------------------------------------------

// TestSetupAsCreator_BackupDirVanishesAfterLockAcquired_DoesNotTransformAgents
// verifies that when the backup directory vanishes after the creator acquires
// its run lock (e.g., a concurrent recovery deleted the directory), the creator
// does not apply in-place transforms and returns an error (errStartFresh or
// a non-retryable error depending on the failure type).
func TestSetupAsCreator_BackupDirVanishesAfterLockAcquired_DoesNotTransformAgents(t *testing.T) {
	// Arrange: backup dir exists; hook deletes it after lock acquisition.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	var hookCalled bool
	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")
	state.afterCreatorLockAcquired = func() {
		hookCalled = true
		// Unlock is not called here; we cannot remove the lock file while locked
		// on Windows. We only delete the directory contents to make CopyAgentsToBackup
		// fail. On Unix, we can remove the entire directory.
		if runtime.GOOS != "windows" {
			os.RemoveAll(backupDir) //nolint:errcheck
		} else {
			// On Windows, leave the dir but remove everything inside so CopyAgentsToBackup fails.
			entries, _ := os.ReadDir(backupDir)
			for _, e := range entries {
				os.Remove(filepath.Join(backupDir, e.Name())) //nolint:errcheck
			}
			os.Remove(backupDir) //nolint:errcheck -- may fail on Windows if lock file held; that's OK
		}
	}

	// Act
	err := state.setupAsCreator(TransformationsFor("opencode"))
	t.Cleanup(func() { cleanupState(state) })

	// Assert: hook was called (setupAsCreator reached the lock-acquired phase).
	// This assertion fails in RED phase (stub never calls hooks), ensuring the
	// test provides a meaningful RED signal.
	if !hookCalled {
		t.Error("afterCreatorLockAcquired hook was not called; " +
			"setupAsCreator must acquire run lock and invoke the hook before copying agents")
	}

	// Assert: setupAsCreator returned an error (not nil).
	if err == nil {
		t.Error("setupAsCreator: expected error when backup dir vanishes after lock acquired; got nil")
	}

	// Assert: agent file was NOT transformed (creator must not transform without full setup).
	if !agentFileIsOriginal(t, agentsDir) {
		t.Error("creator must not transform agents when backup dir vanishes after lock acquisition")
	}
}

// ---------------------------------------------------------------------------
// T8.3(c): creator never transforms unless lock is held AND manifest succeeded
// ---------------------------------------------------------------------------

// TestSetupAsCreator_ManifestWriteFails_DoesNotTransformAgents verifies that
// when WriteManifest fails (because the backup directory vanishes between agent
// copy and manifest write), the creator does not apply in-place transforms.
// The invariant: TransformInPlace is only called after WriteManifest succeeds.
func TestSetupAsCreator_ManifestWriteFails_DoesNotTransformAgents(t *testing.T) {
	// Arrange: backup dir exists; hook deletes backup dir after agents are copied.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	var hookCalled bool
	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")
	state.afterCreatorAgentsCopied = func() {
		hookCalled = true
		// Delete backup dir contents (but not the dir itself on Windows due to lock file).
		// This makes WriteManifest fail with a filesystem error.
		entries, _ := os.ReadDir(backupDir)
		for _, e := range entries {
			os.Remove(filepath.Join(backupDir, e.Name())) //nolint:errcheck
		}
		if runtime.GOOS != "windows" {
			os.Remove(backupDir) //nolint:errcheck
		}
	}

	// Act
	err := state.setupAsCreator(TransformationsFor("opencode"))
	t.Cleanup(func() { cleanupState(state) })

	// Assert: hook was called (setupAsCreator reached the agents-copied phase).
	// This assertion fails in RED phase (stub never calls hooks), ensuring the
	// test provides a meaningful RED signal.
	if !hookCalled {
		t.Error("afterCreatorAgentsCopied hook was not called; " +
			"setupAsCreator must copy agents to backup and invoke the hook before writing manifest")
	}

	// Assert: error returned (manifest write failed).
	if err == nil {
		t.Error("setupAsCreator: expected error when manifest write fails; got nil")
	}

	// Assert: agent file was NOT transformed (transform requires manifest success).
	if !agentFileIsOriginal(t, agentsDir) {
		t.Error("creator must not transform agents when manifest write fails")
	}
}

// ---------------------------------------------------------------------------
// AC8.4 gap: setup-complete-wait + .restoring-held -> joiner enters restoring-poll
// ---------------------------------------------------------------------------

// TestSetupAsJoiner_SetupCompleteWait_RestoringBecomesHeld_StartsFresh verifies
// the teardown-interleaving behavior during setup-complete-wait: when .restoring
// becomes held during the setup-complete-wait loop (indicating a last-out
// teardown is in progress), the joiner exits the setup-complete-wait loop,
// enters the .restoring re-poll loop, and returns errStartFresh when the backup
// directory disappears.
//
// This test covers the fourth combination required by AC8.4:
// setup-complete-wait + .restoring-held. The other three combinations are
// covered by existing tests.
func TestSetupAsJoiner_SetupCompleteWait_RestoringBecomesHeld_StartsFresh(t *testing.T) {
	// Arrange: backup dir with manifest and transformed agents, no .setup-complete.
	// Joiner is in setup-complete-wait loop.
	base := t.TempDir()
	agentsDir, backupDir := setupBackupStateWithManifestOnly(t, base)
	rules := TransformationsFor("opencode")
	if err := TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: TransformInPlace: %v", err)
	}
	restoringPath := filepath.Join(backupDir, RestoringFileName)

	joiner := newUnlockedStateForTest(agentsDir, backupDir, "run-joiner")
	joiner.pollTimeout = 600 * time.Millisecond

	restoringHeld := make(chan struct{})
	var once sync.Once
	joiner.onSetupCompletePollTick = func() {
		once.Do(func() {
			go func() {
				h, lockErr := filelock.Lock(restoringPath)
				if lockErr != nil {
					return
				}
				close(restoringHeld)
				time.Sleep(60 * time.Millisecond)
				os.RemoveAll(backupDir) // teardown: backup dir gone while .restoring held
				h.Unlock()             //nolint:errcheck
			}()
			<-restoringHeld
		})
	}

	// Act
	err := joiner.setupAsJoiner()
	t.Cleanup(func() { cleanupState(joiner) })

	// Assert: .restoring re-poll detected backup dir gone -> start fresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh after .restoring held during setup-complete wait"+
			" and backup dir disappeared; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Minor: creator .restoring-held check on entry
// ---------------------------------------------------------------------------

// TestSetupAsCreator_RestoringHeldOnEntry_YieldsWithoutTransform verifies that
// setupAsCreator detects .restoring as held after acquiring its run lock (step 2
// of the creator protocol) and returns errStartFresh without applying any
// in-place transforms. The invariant: a creator never transforms when .restoring
// is held (a concurrent recovery is in progress).
func TestSetupAsCreator_RestoringHeldOnEntry_YieldsWithoutTransform(t *testing.T) {
	// Arrange: backup dir exists; .restoring is held before setupAsCreator is called.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	restoringPath := filepath.Join(backupDir, RestoringFileName)

	// Hold .restoring before the creator starts, simulating a concurrent recovery.
	restoringHandle, lockErr := filelock.Lock(restoringPath)
	if lockErr != nil {
		t.Fatalf("Lock .restoring: %v", lockErr)
	}
	t.Cleanup(func() { restoringHandle.Unlock() }) //nolint:errcheck

	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")

	// Act
	err := state.setupAsCreator(TransformationsFor("opencode"))
	t.Cleanup(func() { cleanupState(state) })

	// Assert: creator detected .restoring held and yielded via errStartFresh.
	if !errors.Is(err, errStartFresh) {
		t.Errorf("expected errStartFresh when .restoring is held on creator entry; got %v", err)
	}

	// Assert: agent file was NOT transformed.
	if !agentFileIsOriginal(t, agentsDir) {
		t.Error("creator must not transform agents when .restoring is held on entry")
	}
}

// ---------------------------------------------------------------------------
// Minor: EventSnapshotBackupCreated emitted on creator happy path
// ---------------------------------------------------------------------------

// TestSetupAsCreator_HappyPath_EmitsBackupCreatedEvent verifies that
// setupAsCreator emits the EventSnapshotBackupCreated debug event via the
// injected logger when the creator path completes successfully (lock acquired,
// backup copied, manifest written, transforms applied, setup-complete written).
func TestSetupAsCreator_HappyPath_EmitsBackupCreatedEvent(t *testing.T) {
	// Arrange: agents dir with a transformable worker.md; backup dir pre-created
	// (mirrors the dispatch loop: CreateBackupDir runs before setupAsCreator).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	spy := &spyLogger{}
	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")
	state.logger = spy

	// Act
	err := state.setupAsCreator(TransformationsFor("opencode"))
	t.Cleanup(func() { cleanupState(state) })

	// Assert: creator succeeded.
	if err != nil {
		t.Fatalf("setupAsCreator: expected nil on happy path; got %v", err)
	}

	// Assert: EventSnapshotBackupCreated was emitted.
	if !spy.hasEvent(domain.EventSnapshotBackupCreated) {
		t.Errorf("expected DebugLogger to receive event %q on successful creator path; no such event logged",
			domain.EventSnapshotBackupCreated)
	}
}

// ---------------------------------------------------------------------------
// T8.3(d): bounded retry -- dispatch loop terminates after max retries
// ---------------------------------------------------------------------------

// TestDispatchLoop_MaxRetriesExceeded_ReturnsRetriesExceeded verifies that the
// bounded retry dispatch loop terminates after maxRetries+1 failed attempts
// (each returning errStartFresh) and returns errRetriesExceeded. This prevents
// an infinite loop when the backup directory consistently vanishes.
//
// testRunDispatchLoop is used as a test-side proxy for the production dispatch
// loop in SetupBackupAndTransform (Stage 9). The errRetriesExceeded sentinel
// must be used by both to be interchangeable.
func TestDispatchLoop_MaxRetriesExceeded_ReturnsRetriesExceeded(t *testing.T) {
	// Arrange: agents dir exists; dispatch loop will create and delete backup dir
	// on every attempt, ensuring setupAsCreator returns errStartFresh each time.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	maxRetries := 2 // 1 initial attempt + 2 retries = 3 total

	// Act
	err := testRunDispatchLoop(t, agentsDir, "run-retry", TransformationsFor("opencode"), maxRetries)

	// Assert: loop terminated with errRetriesExceeded after exhausting all attempts.
	if !errors.Is(err, errRetriesExceeded) {
		t.Errorf("expected errRetriesExceeded after %d failed attempts; got %v", maxRetries+1, err)
	}
}

// ===========================================================================
// Recovery and Public API
//
// Covers startup recovery with .restoring re-probe (M1 fix) and the public
// API surface consumed by session.go.
//
// These tests are TDD RED phase: they require RecoveryCheck,
// SetupBackupAndTransform, and BackupState.Cleanup to be implemented and
// will fail until then.
// ===========================================================================

// ---------------------------------------------------------------------------
// Recovery helpers
// ---------------------------------------------------------------------------

// setupPartialBackup creates agentsDir with worker.md (untransformed) and a
// backupDir with no manifest and no lock files. This represents a crash that
// occurred after CreateBackupDir but before WriteManifest -- the "partial
// backup" state defined in the design.
func setupPartialBackup(t *testing.T, base string) (agentsDir, backupDir string) {
	t.Helper()
	agentsDir = filepath.Join(base, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir agentsDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(agentsDir, "worker.md"),
		[]byte("---\nmode: subagent\n---\n\nBody.\n"),
		0o644,
	); err != nil {
		t.Fatalf("setup: write worker.md: %v", err)
	}
	backupDir = filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	return agentsDir, backupDir
}

// writeCorruptManifest writes a syntactically invalid JSON file to backupDir
// as ManifestFileName. Used to test that a corrupt manifest causes a RefusalError
// and leaves the backup directory intact.
func writeCorruptManifest(t *testing.T, backupDir string) {
	t.Helper()
	manifestPath := filepath.Join(backupDir, ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("not-valid-json{{{"), 0o644); err != nil {
		t.Fatalf("writeCorruptManifest: %v", err)
	}
}

// backupDirPath returns the path of the backup directory that is a sibling of
// agentsDir. Mirrors the production BackupDirName placement.
func backupDirPath(agentsDir string) string {
	return filepath.Join(filepath.Dir(agentsDir), BackupDirName)
}

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
	agentsDir, backupDir := setupFullBackupState(t, base)
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
	agentsDir, backupDir := setupFullBackupState(t, base)

	// Create orphaned lock files: acquire then release (OS released on "crash").
	for _, id := range []string{"run-crashed-a", "run-crashed-b"} {
		path := RunLockPath(backupDir, id)
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
	agentsDir, backupDir := setupFullBackupState(t, base)

	activeLock, err := filelock.Lock(RunLockPath(backupDir, "run-active"))
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
	agentsDir, backupDir := setupPartialBackup(t, base)

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
	agentsDir, backupDir := setupPartialBackup(t, base)

	creatorLock, err := filelock.Lock(RunLockPath(backupDir, "run-creator"))
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
	agentsDir, backupDir := setupPartialBackup(t, base)
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
	manifestPath := filepath.Join(backupDir, ManifestFileName)
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
	agentsDir, backupDir := setupFullBackupState(t, base)

	// Hook: a joiner acquires its run lock after the initial probe and before
	// .restoring is locked. This simulates the window the M1 fix closes.
	var joinerHandle *filelock.Handle
	afterRecoveryInitialProbeForTest = func() {
		h, err := filelock.Lock(RunLockPath(backupDir, "run-joiner-slip"))
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
	agentsDir, _ := setupFullBackupState(t, base)

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

// ---------------------------------------------------------------------------
// Public API: SetupBackupAndTransform with nil rules returns (nil, nil)
// ---------------------------------------------------------------------------

// TestSetupBackupAndTransform_NilRules_ReturnsNilBackupState verifies that
// when rules is nil (FR-3: path-based harness with no transforms), the
// function returns (nil, nil) and the caller skips the backup entirely.
func TestSetupBackupAndTransform_NilRules_ReturnsNilBackupState(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)

	// Act
	bs, err := SetupBackupAndTransform(agentsDir, "run-001", nil, nil)

	// Assert: no error.
	if err != nil {
		t.Errorf("SetupBackupAndTransform with nil rules: expected nil error; got %v", err)
	}
	// Assert: nil BackupState signals "skip backup".
	if bs != nil {
		t.Errorf("SetupBackupAndTransform with nil rules: expected nil BackupState; got %+v", bs)
	}
}

// TestSetupBackupAndTransform_EmptyRules_ReturnsNilBackupState verifies that
// an empty (non-nil) rules slice also triggers the FR-3 skip path, returning
// (nil, nil).
func TestSetupBackupAndTransform_EmptyRules_ReturnsNilBackupState(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)

	// Act
	bs, err := SetupBackupAndTransform(agentsDir, "run-001", []TransformRule{}, nil)

	// Assert: no error.
	if err != nil {
		t.Errorf("SetupBackupAndTransform with empty rules: expected nil error; got %v", err)
	}
	// Assert: nil BackupState.
	if bs != nil {
		t.Errorf("SetupBackupAndTransform with empty rules: expected nil BackupState; got %+v", bs)
	}
}

// ---------------------------------------------------------------------------
// Public API: SetupBackupAndTransform with rules returns *BackupState
// ---------------------------------------------------------------------------

// TestSetupBackupAndTransform_WithRules_ReturnsNonNilBackupState verifies the
// happy path: with transformation rules provided, SetupBackupAndTransform
// creates a backup, applies transforms, and returns a non-nil *BackupState.
func TestSetupBackupAndTransform_WithRules_ReturnsNonNilBackupState(t *testing.T) {
	// Arrange: agents dir with a transformable file.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	// Act
	bs, err := SetupBackupAndTransform(agentsDir, "run-001", rules, nil)
	if bs != nil {
		t.Cleanup(func() { bs.Cleanup() }) //nolint:errcheck
	}

	// Assert: no error.
	if err != nil {
		t.Fatalf("SetupBackupAndTransform: expected nil error; got %v", err)
	}

	// Assert: BackupState is not nil.
	if bs == nil {
		t.Fatal("SetupBackupAndTransform with rules: expected non-nil *BackupState; got nil")
	}
}

// ---------------------------------------------------------------------------
// Public API: BackupState.Cleanup runs the last-out check
// ---------------------------------------------------------------------------

// TestBackupState_Cleanup_SingleRun_RestoresAgentAndRemovesBackupDir verifies
// that Cleanup performs the last-out check: restores agent files to original
// content and removes the backup directory when this is the only active run.
func TestBackupState_Cleanup_SingleRun_RestoresAgentAndRemovesBackupDir(t *testing.T) {
	// Arrange: set up via SetupBackupAndTransform.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	bs, err := SetupBackupAndTransform(agentsDir, "run-001", rules, nil)
	if err != nil {
		t.Fatalf("SetupBackupAndTransform: %v", err)
	}
	if bs == nil {
		t.Fatal("SetupBackupAndTransform returned nil BackupState for non-empty rules")
	}

	workerPath := filepath.Join(agentsDir, "worker.md")

	// Sanity: file should be transformed after setup.
	content, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md before Cleanup: %v", err)
	}
	if strings.Contains(string(content), "mode: subagent") {
		t.Fatal("setup: worker.md not transformed; test is not meaningful")
	}

	// Act
	cleanupErr := bs.Cleanup()

	// Assert: no error.
	if cleanupErr != nil {
		t.Fatalf("BackupState.Cleanup: unexpected error: %v", cleanupErr)
	}

	// Assert: agent file restored to original content.
	restored, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatalf("read worker.md after Cleanup: %v", err)
	}
	if !strings.Contains(string(restored), "mode: subagent") {
		t.Errorf("worker.md not restored after Cleanup; got:\n%s", restored)
	}

	// Assert: backup directory removed.
	if _, statErr := os.Stat(backupDirPath(agentsDir)); !os.IsNotExist(statErr) {
		t.Errorf("backup directory should be removed after last-out Cleanup; got: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Public API: BackupState.RestoreFunc is injectable
// ---------------------------------------------------------------------------

// TestBackupState_RestoreFunc_Injectable verifies that setting RestoreFunc on
// a BackupState causes Cleanup to call the injected function instead of the
// default RestoreFromBackup. This seam allows session tests to force a restore
// failure without filesystem tricks.
func TestBackupState_RestoreFunc_Injectable(t *testing.T) {
	// Arrange: set up via SetupBackupAndTransform.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	bs, err := SetupBackupAndTransform(agentsDir, "run-001", rules, nil)
	if err != nil {
		t.Fatalf("SetupBackupAndTransform: %v", err)
	}
	if bs == nil {
		t.Fatal("SetupBackupAndTransform returned nil BackupState for non-empty rules")
	}
	t.Cleanup(func() { bs.Cleanup() }) //nolint:errcheck -- ensure temp dir cleanup

	// Inject a counting restoreFunc that always returns an error.
	var restoreCalled int32
	bs.RestoreFunc = func(aDir, bDir string) error {
		atomic.AddInt32(&restoreCalled, 1)
		return fmt.Errorf("injected restore failure")
	}

	// Act
	cleanupErr := bs.Cleanup()

	// Assert: the injected function was called exactly once.
	if got := atomic.LoadInt32(&restoreCalled); got != 1 {
		t.Errorf("expected injected RestoreFunc to be called exactly once; got %d", got)
	}

	// Assert: Cleanup surfaced the injected error (non-nil return).
	if cleanupErr == nil {
		t.Error("Cleanup must surface the injected restore error (FR-22: non-nil return)")
	}
}

// ---------------------------------------------------------------------------
// Public API: logger and injectable timeout options are accepted
// ---------------------------------------------------------------------------

// TestSetupBackupAndTransform_LoggerAndOptionsAccepted verifies that
// SetupBackupAndTransform accepts a logger and option values (WithPollInterval,
// WithPollTimeout) without error. The log event EventSnapshotBackupCreated must
// be emitted on success.
func TestSetupBackupAndTransform_LoggerAndOptionsAccepted(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	spy := &spyLogger{}
	rules := TransformationsFor("opencode")

	// Act
	bs, err := SetupBackupAndTransform(
		agentsDir,
		"run-logger-test",
		rules,
		spy,
		WithPollInterval(5*time.Millisecond),
		WithPollTimeout(500*time.Millisecond),
		WithMaxRetries(1),
	)
	if bs != nil {
		t.Cleanup(func() { bs.Cleanup() }) //nolint:errcheck
	}

	// Assert: no error.
	if err != nil {
		t.Fatalf("SetupBackupAndTransform with logger+options: unexpected error: %v", err)
	}

	// Assert: BackupState is non-nil.
	if bs == nil {
		t.Fatal("SetupBackupAndTransform with logger+options: expected non-nil BackupState")
	}

	// Assert: backup-created event was logged.
	if !spy.hasEvent(domain.EventSnapshotBackupCreated) {
		t.Errorf("expected EventSnapshotBackupCreated to be logged on success; events: %v",
			spy.events)
	}
}

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
	agentsDir, _ := setupFullBackupState(t, base)
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

// ---------------------------------------------------------------------------
// Public API: SetupBackupAndTransform rejects invalid run IDs
// ---------------------------------------------------------------------------

// TestSetupBackupAndTransform_EmptyRunID_ReturnsRefusalError verifies that an
// empty runID is rejected before any filesystem operation is attempted.
// The contract requires a *domain.RefusalError with Component "snapshot" and a
// Reason that contains "invalid run ID".
func TestSetupBackupAndTransform_EmptyRunID_ReturnsRefusalError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	// Act: runID is an empty string.
	bs, err := SetupBackupAndTransform(agentsDir, "", rules, nil)

	// Assert: error is returned, no BackupState.
	if err == nil {
		if bs != nil {
			bs.Cleanup() //nolint:errcheck
		}
		t.Fatal("SetupBackupAndTransform with empty runID: expected non-nil error; got nil")
	}
	if bs != nil {
		bs.Cleanup() //nolint:errcheck
		t.Errorf("SetupBackupAndTransform with empty runID: expected nil BackupState; got %+v", bs)
	}

	// Assert: error is a *domain.RefusalError with Component "snapshot".
	var refusal *domain.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("SetupBackupAndTransform with empty runID: expected *domain.RefusalError; got %T: %v", err, err)
	}
	if refusal.Component != "snapshot" {
		t.Errorf("RefusalError.Component: want %q, got %q", "snapshot", refusal.Component)
	}
	if !strings.Contains(refusal.Reason, "invalid run ID") {
		t.Errorf("RefusalError.Reason: want string containing %q; got %q", "invalid run ID", refusal.Reason)
	}
}

// TestSetupBackupAndTransform_RunIDWithPathSeparator_ReturnsRefusalError
// verifies that a runID containing a path separator character is rejected.
// A runID with path separators could allow the run-lock file to escape its
// intended directory (path traversal risk). The contract requires a
// *domain.RefusalError with Component "snapshot" and Reason containing
// "invalid run ID".
func TestSetupBackupAndTransform_RunIDWithPathSeparator_ReturnsRefusalError(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	// Collect the separator characters to test. "/" is always invalid.
	// On Windows, "\" is also a path separator.
	separators := []struct {
		name  string
		runID string
	}{
		{"forward-slash", "run/001"},
	}
	if runtime.GOOS == "windows" {
		separators = append(separators, struct {
			name  string
			runID string
		}{"back-slash", `run\001`})
	}

	for _, tc := range separators {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Act
			bs, err := SetupBackupAndTransform(agentsDir, tc.runID, rules, nil)

			// Assert: error is returned, no BackupState.
			if err == nil {
				if bs != nil {
					bs.Cleanup() //nolint:errcheck
				}
				t.Fatalf("SetupBackupAndTransform with runID %q: expected non-nil error; got nil", tc.runID)
			}
			if bs != nil {
				bs.Cleanup() //nolint:errcheck
				t.Errorf("SetupBackupAndTransform with runID %q: expected nil BackupState; got %+v", tc.runID, bs)
			}

			// Assert: error is a *domain.RefusalError with Component "snapshot".
			var refusal *domain.RefusalError
			if !errors.As(err, &refusal) {
				t.Fatalf("SetupBackupAndTransform with runID %q: expected *domain.RefusalError; got %T: %v", tc.runID, err, err)
			}
			if refusal.Component != "snapshot" {
				t.Errorf("RefusalError.Component: want %q, got %q", "snapshot", refusal.Component)
			}
			if !strings.Contains(refusal.Reason, "invalid run ID") {
				t.Errorf("RefusalError.Reason: want string containing %q; got %q", "invalid run ID", refusal.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Public API: SetupBackupAndTransform refuses a foreign backup directory
// ---------------------------------------------------------------------------

// TestSetupBackupAndTransform_ForeignBackupDirectory_ReturnsRefusalError
// verifies the ownership guard on the joiner path: when a pre-existing
// .agents-backup directory contains files that are not recognized protocol
// artifacts, SetupBackupAndTransform refuses immediately with a
// *domain.RefusalError{Component: "snapshot", Reason: "foreign backup directory: ..."}.
//
// This prevents the protocol from silently treating a user-created directory
// as its own backup state and potentially deleting or overwriting its contents.
func TestSetupBackupAndTransform_ForeignBackupDirectory_ReturnsRefusalError(t *testing.T) {
	// Arrange: create agents dir and a sibling .agents-backup that looks foreign.
	// A foreign directory contains files that are NOT recognized protocol
	// artifacts (i.e., something other than .lock-* files, .restoring,
	// .setup-complete, recovery-manifest.json, .recovery-manifest.json.tmp,
	// or .md files).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir foreign backupDir: %v", err)
	}
	// Write a file with an unrecognized name/extension to mark the directory
	// as foreign from the protocol's perspective.
	foreignFile := filepath.Join(backupDir, "user-data.txt")
	if err := os.WriteFile(foreignFile, []byte("not a protocol artifact\n"), 0o644); err != nil {
		t.Fatalf("setup: write foreign file: %v", err)
	}
	rules := TransformationsFor("opencode")

	// Act: SetupBackupAndTransform sees the pre-existing backup dir and enters
	// the joiner path, which performs the ownership check.
	bs, err := SetupBackupAndTransform(agentsDir, "run-foreign-check", rules, nil)

	// Assert: error is returned, no BackupState.
	if err == nil {
		if bs != nil {
			bs.Cleanup() //nolint:errcheck
		}
		t.Fatal("SetupBackupAndTransform with foreign backup dir: expected non-nil error; got nil")
	}
	if bs != nil {
		bs.Cleanup() //nolint:errcheck
		t.Errorf("SetupBackupAndTransform with foreign backup dir: expected nil BackupState; got %+v", bs)
	}

	// Assert: error is a *domain.RefusalError with Component "snapshot".
	var refusal *domain.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("SetupBackupAndTransform with foreign backup dir: expected *domain.RefusalError; got %T: %v", err, err)
	}
	if refusal.Component != "snapshot" {
		t.Errorf("RefusalError.Component: want %q, got %q", "snapshot", refusal.Component)
	}
	if !strings.Contains(refusal.Reason, "foreign backup directory") {
		t.Errorf("RefusalError.Reason: want string containing %q; got %q", "foreign backup directory", refusal.Reason)
	}

	// Assert: foreign backup directory was NOT modified or deleted.
	if _, statErr := os.Stat(backupDir); statErr != nil {
		t.Errorf("foreign backup directory must not be deleted on refusal; got: %v", statErr)
	}
	if _, statErr := os.Stat(foreignFile); statErr != nil {
		t.Errorf("foreign file inside backup dir must remain after refusal; got: %v", statErr)
	}
}

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
	agentsDir, backupDir := setupPartialBackup(t, base)

	// Hook: a joiner acquires its run lock after the initial probe and before
	// .restoring is locked. Without the ordering guarantee, RecoveryCheck
	// might delete the partial backup while the joiner is in flight.
	var joinerHandle *filelock.Handle
	afterRecoveryInitialProbeForTest = func() {
		h, err := filelock.Lock(RunLockPath(backupDir, "run-joiner-partial"))
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
	backupDir := filepath.Join(base, BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	tmpManifestPath := filepath.Join(backupDir, ManifestTempFileName)
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
			ManifestTempFileName, gotErr)
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
// Public API: BackupState.Cleanup is idempotent
// ---------------------------------------------------------------------------

// TestBackupState_Cleanup_CalledTwice_IsIdempotent verifies the idempotency
// guarantee documented on BackupState.Cleanup: calling Cleanup a second time
// on the same *BackupState must not panic, must not invoke the restore
// function a second time, and must return nil.
func TestBackupState_Cleanup_CalledTwice_IsIdempotent(t *testing.T) {
	// Arrange: set up via SetupBackupAndTransform with an injected RestoreFunc
	// so that restore calls can be counted.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	rules := TransformationsFor("opencode")

	bs, err := SetupBackupAndTransform(agentsDir, "run-idempotent", rules, nil)
	if err != nil {
		t.Fatalf("SetupBackupAndTransform: %v", err)
	}
	if bs == nil {
		t.Fatal("SetupBackupAndTransform returned nil BackupState for non-empty rules")
	}

	// Inject a counting restore function to detect unwanted double-restore.
	var restoreCalled int32
	bs.RestoreFunc = func(aDir, bDir string) error {
		atomic.AddInt32(&restoreCalled, 1)
		return nil
	}

	// Act: first Cleanup -- performs the last-out check and restore.
	firstErr := bs.Cleanup()
	if firstErr != nil {
		t.Fatalf("first Cleanup: unexpected error: %v", firstErr)
	}

	// Act: second Cleanup -- must be safe to call again (idempotent).
	secondErr := bs.Cleanup()

	// Assert: second call returns nil.
	if secondErr != nil {
		t.Errorf("second Cleanup: expected nil (idempotent); got: %v", secondErr)
	}

	// Assert: RestoreFunc called exactly once across both Cleanup calls.
	if got := atomic.LoadInt32(&restoreCalled); got != 1 {
		t.Errorf("RestoreFunc must be called exactly once across two Cleanup calls; got %d", got)
	}
}
