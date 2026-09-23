package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
)

// Package-internal sentinel errors. Never returned to callers outside the
// snapshot package; SetupBackupAndTransform wraps them into *domain.RefusalError
// before returning. Tests within the package may match them directly.
var (
	// errStartFresh signals that the backup directory vanished unexpectedly and
	// the caller (dispatch loop) should retry from CreateBackupDir.
	errStartFresh = errors.New("snapshot: start fresh")

	// errPollTimeout signals that a bounded polling loop exceeded its deadline.
	errPollTimeout = errors.New("snapshot: poll timeout exceeded")

	// errRetriesExceeded signals that the dispatch loop exhausted all retries.
	errRetriesExceeded = errors.New("snapshot: retries exceeded")
)

// LockProtocolState holds the state for one run's participation in the
// backup-and-transform lock protocol. Created internally by
// SetupBackupAndTransform; consumed via BackupState.
type LockProtocolState struct {
	agentsDir   string
	backupDir   string
	runID       string
	runLock     *filelock.Handle
	restoreFunc func(agentsDir, backupDir string) error
	logger      domain.DebugLogger

	// Test hooks: called at specific internal points. Nil in production.
	// afterRestoringAcquired fires after .restoring is locked via blocking
	// Lock, before own .lock-{runID} is released.
	afterRestoringAcquired func()
	// afterManifestDeleted fires after the manifest is deleted inside the
	// last-out path, while .restoring is still held.
	afterManifestDeleted func()
	// afterSetupCompleteDeleted fires after .setup-complete is deleted inside
	// the last-out path, while .restoring is still held. Used to verify that
	// .setup-complete is absent before .restoring is released (Windows-safe
	// delete ordering).
	afterSetupCompleteDeleted func()

	// removeAllFunc is called instead of os.RemoveAll to remove the backup
	// directory in lastOutCheck. Nil defaults to os.RemoveAll. Tests may
	// substitute a function that returns an error to verify that a RemoveAll
	// failure is logged non-fatally and does not change the exit code.
	removeAllFunc func(string) error

	// Stage 8: Timing configuration. Zero values are normalized to defaults
	// (100ms interval, 30s timeout) by setupAsCreator/setupAsJoiner.
	// Tests set short values for fast execution.
	pollInterval time.Duration
	pollTimeout  time.Duration

	// Stage 8 test hooks: called at specific internal points in the creator
	// and joiner paths. All are nil in production.

	// afterCreatorLockAcquired fires after setupAsCreator acquires the run lock,
	// before CopyAgentsToBackup. Used to inject failures after lock acquisition.
	afterCreatorLockAcquired func()

	// afterCreatorAgentsCopied fires after CopyAgentsToBackup completes, before
	// WriteManifest. Used to inject failures between copy and manifest write.
	afterCreatorAgentsCopied func()

	// afterJoinerLockAcquired fires after setupAsJoiner acquires the run lock,
	// before post-lock re-verification. Used to inject .restoring-held scenarios.
	afterJoinerLockAcquired func()

	// onManifestPollTick fires on each iteration of the manifest-wait polling
	// loop in setupAsJoiner. Used to trigger backup-dir-gone or .restoring-held
	// conditions during the poll.
	onManifestPollTick func()

	// onSetupCompletePollTick fires on each iteration of the .setup-complete-wait
	// polling loop in setupAsJoiner. Used to trigger teardown conditions.
	onSetupCompletePollTick func()

	// onRestoringPollTick fires on each iteration of the .restoring re-poll loop
	// in setupAsJoiner. Used to control the timing of re-poll resolution.
	onRestoringPollTick func()
}

// newLockProtocolState creates a new LockProtocolState and acquires the
// per-run lock file .lock-{runID} inside backupDir using filelock.Lock.
// Returns error if the lock cannot be acquired (e.g. parent directory missing).
//
// restoreFunc may be nil; a nil restoreFunc defaults to RestoreFromBackup.
// logger may be nil.
func newLockProtocolState(
	agentsDir, backupDir, runID string,
	restoreFunc func(string, string) error,
	logger domain.DebugLogger,
) (*LockProtocolState, error) {
	lockPath := RunLockPath(backupDir, runID)
	handle, err := filelock.Lock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot: acquire run lock %q: %w", lockPath, err)
	}
	if logger != nil {
		logger.Log(domain.EventSnapshotLockAcquired,
			fmt.Sprintf("run lock acquired: %s", lockPath),
			domain.F("run_id", runID))
	}

	if restoreFunc == nil {
		restoreFunc = RestoreFromBackup
	}

	return &LockProtocolState{
		agentsDir:   agentsDir,
		backupDir:   backupDir,
		runID:       runID,
		runLock:     handle,
		restoreFunc: restoreFunc,
		logger:      logger,
	}, nil
}

// lastOutCheck performs the exit protocol for one run:
//
//  1. Acquire .restoring via blocking filelock.Lock (serializes all exits).
//  2. Release own .lock-{runID} and delete it.
//  3. Probe remaining .lock-* files via filelock.TryLock:
//     orphaned (acquirable) = release and delete; held = another run is active.
//  4. If any lock is held: release .restoring WITHOUT deleting it; done.
//  5. If no locks remain (last out):
//     a. Call restoreFunc (defaults to RestoreFromBackup).
//     b. Delete manifest while .restoring is still held.
//     c. Delete .setup-complete.
//     d. Release .restoring handle.
//     e. Call os.RemoveAll on backup directory (failure is non-fatal).
//
// Returns error only for critical restore failures; RemoveAll failure is
// logged non-fatally and does not change the return value.
func (s *LockProtocolState) lastOutCheck() error {
	// Step 1: Acquire .restoring via blocking Lock (serializes all exits).
	restoringPath := filepath.Join(s.backupDir, RestoringFileName)
	restoringHandle, err := filelock.Lock(restoringPath)
	if err != nil {
		return fmt.Errorf("snapshot: acquire .restoring: %w", err)
	}

	// Fire hook: .restoring is now held, own lock is still held.
	if s.afterRestoringAcquired != nil {
		s.afterRestoringAcquired()
	}

	// Step 2: Release own .lock-{runID} and delete it.
	ownLockPath := RunLockPath(s.backupDir, s.runID)
	if s.runLock != nil {
		s.runLock.Unlock() //nolint:errcheck
		s.runLock = nil
	}
	os.Remove(ownLockPath) //nolint:errcheck
	if s.logger != nil {
		s.logger.Log(domain.EventSnapshotLockReleased,
			fmt.Sprintf("run lock released: %s", ownLockPath),
			domain.F("run_id", s.runID))
	}

	// Step 3: Probe remaining .lock-* files.
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		// If the backup dir vanished, nothing to do. Release .restoring.
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	otherRunActive := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, RunLockPrefix) {
			continue
		}

		lockPath := filepath.Join(s.backupDir, name)
		h, acquired, tryErr := filelock.TryLock(lockPath)
		if tryErr != nil {
			// Filesystem error probing this lock; treat conservatively as held.
			otherRunActive = true
			continue
		}
		if acquired {
			// Orphaned: acquired means no one holds it. Release and delete.
			h.Unlock() //nolint:errcheck
			os.Remove(lockPath) //nolint:errcheck
		} else {
			// Another run is actively holding this lock.
			otherRunActive = true
		}
	}

	// Step 4: If any lock is held, release .restoring WITHOUT deleting it; done.
	if otherRunActive {
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	// Step 5: Last out -- restore originals and tear down backup directory.

	// Step 5a: Call restoreFunc.
	if restoreErr := s.restoreFunc(s.agentsDir, s.backupDir); restoreErr != nil {
		restoringHandle.Unlock() //nolint:errcheck
		return restoreErr
	}
	if s.logger != nil {
		s.logger.Log(domain.EventSnapshotRestored,
			fmt.Sprintf("snapshot restored from backup: %s", s.backupDir),
			domain.F("run_id", s.runID))
	}

	// Step 5b: Delete manifest while .restoring is still held.
	manifestPath := filepath.Join(s.backupDir, ManifestFileName)
	os.Remove(manifestPath) //nolint:errcheck
	if s.afterManifestDeleted != nil {
		s.afterManifestDeleted()
	}

	// Step 5c: Delete .setup-complete while .restoring is still held.
	setupCompletePath := filepath.Join(s.backupDir, SetupCompleteFileName)
	os.Remove(setupCompletePath) //nolint:errcheck
	if s.afterSetupCompleteDeleted != nil {
		s.afterSetupCompleteDeleted()
	}

	// Step 5d: Release .restoring handle (do NOT delete the file; RemoveAll handles it).
	restoringHandle.Unlock() //nolint:errcheck

	// Step 5e: Remove the backup directory. Failure is non-fatal: log and continue.
	removeAll := s.removeAllFunc
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	if removeErr := removeAll(s.backupDir); removeErr != nil {
		if s.logger != nil {
			s.logger.Log(domain.EventSnapshotCleanupFailed,
				fmt.Sprintf("backup directory removal failed (non-fatal): %v", removeErr))
		}
	}

	return nil
}

// verifyBackupContents checks that every top-level .md file present in
// agentsDir also exists in backupDir. Returns errStartFresh if any file is
// absent (indicating the backup copy was partially or fully deleted).
//
// Called only from the test-hook path in setupAsCreator; never called in
// production (hooks are nil). Relies on isMDFile from backup.go.
func verifyBackupContents(agentsDir, backupDir string) error {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errStartFresh
		}
		return fmt.Errorf("snapshot: verify backup contents: cannot read agents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !isMDFile(e.Name()) {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(backupDir, e.Name())); os.IsNotExist(statErr) {
			return errStartFresh
		}
	}
	return nil
}

// setupAsCreator executes the first-run (creator) path. Single-pass (no
// internal retry). Steps: acquire run lock, check .restoring not held,
// copy agents to backup, write manifest, write recovery marker, transform
// in place, write setup-complete.
//
// Returns errStartFresh when the backup dir vanished (dispatch loop retries).
// Returns error for non-retryable failures.
func (s *LockProtocolState) setupAsCreator(rules []TransformRule) error {
	lockPath := RunLockPath(s.backupDir, s.runID)

	// Step 1: Acquire run lock. If the backup dir is absent (race: a concurrent
	// recovery deleted it between CreateBackupDir and here), return errStartFresh
	// so the dispatch loop retries from CreateBackupDir.
	handle, err := filelock.Lock(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errStartFresh
		}
		return fmt.Errorf("snapshot: creator acquire run lock: %w", err)
	}
	s.runLock = handle

	// Step 2: Check that .restoring is not held. A held .restoring means a
	// concurrent recovery is in progress; yield so it can finish cleanly.
	restoringPath := filepath.Join(s.backupDir, RestoringFileName)
	rh, acquired, tryErr := filelock.TryLock(restoringPath)
	if tryErr != nil || !acquired {
		if rh != nil {
			rh.Unlock() //nolint:errcheck
		}
		s.runLock.Unlock() //nolint:errcheck
		os.Remove(lockPath) //nolint:errcheck
		s.runLock = nil
		return errStartFresh
	}
	rh.Unlock() //nolint:errcheck -- not held; release the probe

	// Hook: afterCreatorLockAcquired fires after lock is held and .restoring
	// check passes, before any backup data operations.
	//
	// On Windows, LockFileEx prevents deleting the locked file, so we temporarily
	// release the run lock to allow the test seam to simulate backup-dir deletion.
	// After the hook, we attempt to re-acquire; failure means the dir was torn
	// down. In production this branch is never taken (hook is always nil).
	if s.afterCreatorLockAcquired != nil {
		s.runLock.Unlock() //nolint:errcheck
		s.runLock = nil
		s.afterCreatorLockAcquired()
		newHandle, retryErr := filelock.Lock(lockPath)
		if retryErr != nil {
			if errors.Is(retryErr, os.ErrNotExist) {
				return errStartFresh
			}
			return fmt.Errorf("snapshot: creator re-acquire run lock: %w", retryErr)
		}
		s.runLock = newHandle
	} else {
		// Production path: verify lock file still exists. On Unix a concurrent
		// recovery may have removed the backup directory via os.RemoveAll,
		// unlinking the lock file while the FD remains valid.
		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			return errStartFresh
		}
	}

	// Step 3: Copy agent files to backup. If the backup dir vanished (concurrent
	// recovery ran while we held the lock), return errStartFresh.
	if copyErr := CopyAgentsToBackup(s.agentsDir, s.backupDir); copyErr != nil {
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			return errStartFresh
		}
		return copyErr
	}

	// Hook: afterCreatorAgentsCopied fires after backup copy, before manifest write.
	//
	// Same unlock-relock pattern as afterCreatorLockAcquired to allow test seams
	// to simulate backup-dir deletion on Windows. After re-acquiring, verify that
	// all agent backup copies are still present; a missing file means the hook
	// simulated a partial or full teardown between copy and manifest write.
	// In production this branch is never taken (hook is always nil).
	if s.afterCreatorAgentsCopied != nil {
		s.runLock.Unlock() //nolint:errcheck
		s.runLock = nil
		s.afterCreatorAgentsCopied()
		newHandle, retryErr := filelock.Lock(lockPath)
		if retryErr != nil {
			if errors.Is(retryErr, os.ErrNotExist) {
				return errStartFresh
			}
			return fmt.Errorf("snapshot: creator re-acquire run lock after agents copied: %w", retryErr)
		}
		s.runLock = newHandle
		// Verify backup copy is intact: hook may have deleted agent files.
		if verifyErr := verifyBackupContents(s.agentsDir, s.backupDir); verifyErr != nil {
			return verifyErr
		}
	} else {
		// Production path: verify backup dir and lock file still exist before
		// writing the manifest. A concurrent recovery may have deleted them after
		// CopyAgentsToBackup completed.
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			return errStartFresh
		}
		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			return errStartFresh
		}
	}

	// Step 4: Write manifest. Invariant: TransformInPlace is only called AFTER
	// WriteManifest succeeds. If this fails the creator returns without transforming.
	if manifestErr := WriteManifest(s.backupDir, s.agentsDir, rules); manifestErr != nil {
		return manifestErr
	}

	// Step 5: Write recovery marker into agents dir so that RecoveryCheck can
	// recognise an in-progress or interrupted backup-and-transform run.
	if markerErr := WriteRecoveryMarker(s.agentsDir, s.backupDir); markerErr != nil {
		return markerErr
	}

	// Step 6: Apply in-place transforms.
	if transformErr := TransformInPlace(s.agentsDir, rules); transformErr != nil {
		return transformErr
	}

	// Step 7: Signal to joiners that all transforms are complete.
	if scErr := WriteSetupComplete(s.backupDir); scErr != nil {
		return scErr
	}

	// Emit debug event on successful creator completion.
	if s.logger != nil {
		s.logger.Log(domain.EventSnapshotBackupCreated,
			fmt.Sprintf("backup created: %s", s.backupDir),
			domain.F("run_id", s.runID))
	}

	return nil
}

// setupAsJoiner executes the concurrent-join path. Single-pass (no internal
// retry). Steps: wait for manifest (bounded poll), wait for .setup-complete,
// acquire own run lock, post-lock re-verification.
//
// Uses a single overall deadline (pollTimeout) shared across all poll loops.
//
// Returns errPollTimeout if any poll exceeds pollTimeout.
// Returns errStartFresh when the backup dir vanished.
// Returns error for non-retryable failures.
func (s *LockProtocolState) setupAsJoiner() error {
	// Normalize poll settings.
	pollInterval := s.pollInterval
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	pollTimeout := s.pollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30 * time.Second
	}

	deadline := time.Now().Add(pollTimeout)
	restoringPath := filepath.Join(s.backupDir, RestoringFileName)
	manifestPath := filepath.Join(s.backupDir, ManifestFileName)
	setupCompletePath := filepath.Join(s.backupDir, SetupCompleteFileName)

	// waitForRestoring polls until .restoring is acquirable or backup dir is gone.
	// Returns nil when .restoring was acquired+released with backup dir AND manifest
	// both intact (per design: "backup dir still exists with manifest").
	// Returns errStartFresh when backup dir or manifest is gone.
	// Returns errPollTimeout when deadline is exceeded.
	waitForRestoring := func() error {
		for {
			if s.onRestoringPollTick != nil {
				s.onRestoringPollTick()
			}
			// Check if backup dir has been torn down.
			if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
				return errStartFresh
			}
			// Try to acquire .restoring without blocking.
			h, acquired, tryErr := filelock.TryLock(restoringPath)
			if tryErr == nil && acquired {
				h.Unlock() //nolint:errcheck
				// Re-check backup dir and manifest after acquiring .restoring
				// momentarily. If either is gone the backup has been torn down.
				if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
					return errStartFresh
				}
				if _, statErr := os.Stat(manifestPath); os.IsNotExist(statErr) {
					return errStartFresh
				}
				return nil // backup dir and manifest intact -> proceed
			}
			if tryErr != nil {
				// Filesystem error: check whether backup dir is gone.
				if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
					return errStartFresh
				}
			}
			if time.Now().After(deadline) {
				return errPollTimeout
			}
			time.Sleep(pollInterval)
		}
	}

	// skipToLock is set to true when a .restoring re-poll during a wait phase
	// returns successfully (backup dir intact), signalling that we should skip
	// any remaining wait loops and proceed directly to lock acquisition.
	skipToLock := false

	// Phase 1: Wait for manifest.
	if !skipToLock {
		for {
			if s.onManifestPollTick != nil {
				s.onManifestPollTick()
			}
			// Teardown detection: backup dir gone.
			if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
				return errStartFresh
			}
			// Manifest found: creator has written it.
			if _, statErr := os.Stat(manifestPath); statErr == nil {
				break
			}
			// Teardown interleaving: .restoring held means an exit is in progress.
			rh, acquired, tryErr := filelock.TryLock(restoringPath)
			if tryErr == nil && acquired {
				rh.Unlock() //nolint:errcheck -- not held; probe released
			} else {
				if rh != nil {
					rh.Unlock() //nolint:errcheck
				}
				if err := waitForRestoring(); err != nil {
					return err
				}
				// Backup dir intact after re-poll -> skip remaining wait phases.
				skipToLock = true
				break
			}
			if time.Now().After(deadline) {
				return errPollTimeout
			}
			time.Sleep(pollInterval)
		}
	}

	// Phase 2: Wait for .setup-complete.
	if !skipToLock {
		for {
			if s.onSetupCompletePollTick != nil {
				s.onSetupCompletePollTick()
			}
			// Teardown detection: backup dir gone.
			if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
				return errStartFresh
			}
			// .setup-complete found: creator has finished all transforms.
			if _, statErr := os.Stat(setupCompletePath); statErr == nil {
				break
			}
			// Teardown interleaving: .restoring held.
			rh, acquired, tryErr := filelock.TryLock(restoringPath)
			if tryErr == nil && acquired {
				rh.Unlock() //nolint:errcheck
			} else {
				if rh != nil {
					rh.Unlock() //nolint:errcheck
				}
				if err := waitForRestoring(); err != nil {
					return err
				}
				skipToLock = true
				break
			}
			if time.Now().After(deadline) {
				return errPollTimeout
			}
			time.Sleep(pollInterval)
		}
	}

	// Phase 3: Acquire own run lock. This is a loop because post-lock
	// re-verification may detect a concurrent exit (.restoring held), release
	// the lock, enter the re-poll, and then re-acquire the lock.
	lockPath := RunLockPath(s.backupDir, s.runID)
	for {
		handle, err := filelock.Lock(lockPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errStartFresh
			}
			return fmt.Errorf("snapshot: joiner acquire run lock: %w", err)
		}
		s.runLock = handle

		// Hook: afterJoinerLockAcquired fires after lock acquisition,
		// before post-lock re-verification. Used to inject .restoring-held races.
		if s.afterJoinerLockAcquired != nil {
			s.afterJoinerLockAcquired()
		}

		// Post-lock re-verification step A: check .restoring.
		// A held .restoring means a concurrent last-out teardown is in progress.
		// Release the run lock and enter the re-poll loop; re-acquire afterwards.
		rh, acquired, tryErr := filelock.TryLock(restoringPath)
		if tryErr == nil && acquired {
			rh.Unlock() //nolint:errcheck -- not held; proceed
		} else {
			if rh != nil {
				rh.Unlock() //nolint:errcheck
			}
			s.runLock.Unlock() //nolint:errcheck
			os.Remove(lockPath) //nolint:errcheck
			s.runLock = nil
			if pollErr := waitForRestoring(); pollErr != nil {
				return pollErr
			}
			continue // re-acquire lock after re-poll succeeds
		}

		// Post-lock re-verification step B: backup dir still exists.
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			s.runLock.Unlock() //nolint:errcheck
			os.Remove(lockPath) //nolint:errcheck
			s.runLock = nil
			return errStartFresh
		}

		// Post-lock re-verification step C: own lock file still exists on disk
		// (m4 robustness -- on platforms where a locked file can be unlinked,
		// another process could delete .lock-{runID} between creation and locking).
		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			s.runLock.Unlock() //nolint:errcheck
			s.runLock = nil
			return errStartFresh
		}

		return nil
	}
}
