package lockprotocol

// Tests for LockProtocolState construction and per-run lock file lifecycle.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
)

// ---------------------------------------------------------------------------
// T7.1(a): newLockProtocolState acquires the per-run lock file
// ---------------------------------------------------------------------------

// TestLockProtocol_NewState_AcquiresRunLockFileInBackupDir verifies that
// newLockProtocolState creates and acquires the .lock-{runID} file inside the
// backup directory.
func TestLockProtocol_NewState_AcquiresRunLockFileInBackupDir(t *testing.T) {
	// Arrange
	base := t.TempDir()
	agentsDir, backupDir := newTestFullBackupState(t, base)
	runID := "run-001"
	lockFilePath := backup.RunLockPath(backupDir, runID)

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
	agentsDir, backupDir := newTestFullBackupState(t, base)
	runID := "run-001"
	state := mustNewState(t, agentsDir, backupDir, runID, nil)
	t.Cleanup(func() { cleanupState(state) })

	// Act: try to acquire the same lock via a separate handle.
	h2, ok, err := filelock.TryLock(backup.RunLockPath(backupDir, runID))
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
	lockPath := backup.RunLockPath(backupDir, runID)

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
	lockPath := backup.RunLockPath(backupDir, runID)

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
	nonExistentBackupDir := filepath.Join(base, "does-not-exist", backup.BackupDirName)

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
