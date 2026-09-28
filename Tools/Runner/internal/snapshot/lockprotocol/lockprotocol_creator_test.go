package lockprotocol

// Tests for the creator path of the lock protocol: lock acquisition, agent copying,
// manifest writing, in-place transforms, and the bounded retry dispatch loop.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// T8.3(a): creator lock creation fails because backup dir absent
// ---------------------------------------------------------------------------

// TestSetupAsCreator_BackupDirAbsent_ReturnsErrStartFresh verifies that when
// the backup directory does not exist at the time setupAsCreator attempts lock
// acquisition, setupAsCreator returns errStartFresh (signaling the dispatch
// loop to retry from NewBackupDir). The creator must NOT apply any
// transforms without a valid lock.
func TestSetupAsCreator_BackupDirAbsent_ReturnsErrStartFresh(t *testing.T) {
	// Arrange: agents dir exists; backup dir does not.
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	nonExistentBackupDir := filepath.Join(base, backup.BackupDirName) // never created

	state := newUnlockedStateForTest(agentsDir, nonExistentBackupDir, "run-creator")

	// Act
	err := state.setupAsCreator(transform.TransformationsFor("opencode"))

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
	backupDir := filepath.Join(base, backup.BackupDirName)
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
	err := state.setupAsCreator(transform.TransformationsFor("opencode"))
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
	backupDir := filepath.Join(base, backup.BackupDirName)
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
	err := state.setupAsCreator(transform.TransformationsFor("opencode"))
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
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)

	// Hold .restoring before the creator starts, simulating a concurrent recovery.
	restoringHandle, lockErr := filelock.Lock(restoringPath)
	if lockErr != nil {
		t.Fatalf("Lock .restoring: %v", lockErr)
	}
	t.Cleanup(func() { restoringHandle.Unlock() }) //nolint:errcheck

	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")

	// Act
	err := state.setupAsCreator(transform.TransformationsFor("opencode"))
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
	// (mirrors the dispatch loop: NewBackupDir runs before setupAsCreator).
	base := t.TempDir()
	agentsDir := newAgentsDir(t, base)
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	spy := &spyLogger{}
	state := newUnlockedStateForTest(agentsDir, backupDir, "run-creator")
	state.logger = spy

	// Act
	err := state.setupAsCreator(transform.TransformationsFor("opencode"))
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
	err := testRunDispatchLoop(t, agentsDir, "run-retry", transform.TransformationsFor("opencode"), maxRetries)

	// Assert: loop terminated with errRetriesExceeded after exhausting all attempts.
	if !errors.Is(err, errRetriesExceeded) {
		t.Errorf("expected errRetriesExceeded after %d failed attempts; got %v", maxRetries+1, err)
	}
}
