package lockprotocol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
)

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
	pollInterval := s.pollInterval
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	pollTimeout := s.pollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30 * time.Second
	}

	deadline := time.Now().Add(pollTimeout)
	restoringPath := filepath.Join(s.backupDir, backup.RestoringFileName)
	manifestPath := filepath.Join(s.backupDir, manifest.ManifestFileName)
	setupCompletePath := filepath.Join(s.backupDir, backup.SetupCompleteFileName)

	skipToLock, err := s.joinerPhase1WaitForManifest(deadline, pollInterval, restoringPath, manifestPath)
	if err != nil {
		return err
	}
	if !skipToLock {
		skipToLock, err = s.joinerPhase2WaitForSetupComplete(deadline, pollInterval, restoringPath, setupCompletePath)
		if err != nil {
			return err
		}
	}
	return s.joinerPhase3AcquireLock(deadline, pollInterval, restoringPath)
}

// joinerWaitForRestoring polls until .restoring is acquirable or backup dir is gone.
// Returns nil when .restoring was acquired+released with backup dir AND manifest both intact.
// Returns errStartFresh when backup dir or manifest is gone.
// Returns errPollTimeout when deadline is exceeded.
func (s *LockProtocolState) joinerWaitForRestoring(deadline time.Time, pollInterval time.Duration, restoringPath, manifestPath string) error {
	for {
		if s.onRestoringPollTick != nil {
			s.onRestoringPollTick()
		}
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			return errStartFresh
		}
		h, acquired, tryErr := filelock.TryLock(restoringPath)
		if tryErr == nil && acquired {
			h.Unlock() //nolint:errcheck
			if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
				return errStartFresh
			}
			if _, statErr := os.Stat(manifestPath); os.IsNotExist(statErr) {
				return errStartFresh
			}
			return nil
		}
		if tryErr != nil {
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

// joinerPhase1WaitForManifest polls until the manifest appears or a teardown is detected.
// Returns (true, nil) when .restoring re-poll signals backup intact (skipToLock).
// Returns (false, nil) when manifest found normally.
// Returns (false, err) on timeout or unrecoverable error.
func (s *LockProtocolState) joinerPhase1WaitForManifest(deadline time.Time, pollInterval time.Duration, restoringPath, manifestPath string) (bool, error) {
	for {
		if s.onManifestPollTick != nil {
			s.onManifestPollTick()
		}
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			return false, errStartFresh
		}
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			return false, nil
		}
		rh, acquired, tryErr := filelock.TryLock(restoringPath)
		if tryErr == nil && acquired {
			rh.Unlock() //nolint:errcheck -- not held; probe released
		} else {
			if rh != nil {
				rh.Unlock() //nolint:errcheck
			}
			if err := s.joinerWaitForRestoring(deadline, pollInterval, restoringPath, manifestPath); err != nil {
				return false, err
			}
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, errPollTimeout
		}
		time.Sleep(pollInterval)
	}
}

// joinerPhase2WaitForSetupComplete polls until .setup-complete appears or teardown is detected.
// Returns (true, nil) when .restoring re-poll signals backup intact (skipToLock).
// Returns (false, nil) when .setup-complete found normally.
// Returns (false, err) on timeout or unrecoverable error.
func (s *LockProtocolState) joinerPhase2WaitForSetupComplete(deadline time.Time, pollInterval time.Duration, restoringPath, setupCompletePath string) (bool, error) {
	manifestPath := filepath.Join(s.backupDir, manifest.ManifestFileName)
	for {
		if s.onSetupCompletePollTick != nil {
			s.onSetupCompletePollTick()
		}
		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			return false, errStartFresh
		}
		if _, statErr := os.Stat(setupCompletePath); statErr == nil {
			return false, nil
		}
		rh, acquired, tryErr := filelock.TryLock(restoringPath)
		if tryErr == nil && acquired {
			rh.Unlock() //nolint:errcheck
		} else {
			if rh != nil {
				rh.Unlock() //nolint:errcheck
			}
			if err := s.joinerWaitForRestoring(deadline, pollInterval, restoringPath, manifestPath); err != nil {
				return false, err
			}
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, errPollTimeout
		}
		time.Sleep(pollInterval)
	}
}

// joinerPhase3AcquireLock acquires the per-run lock with post-lock re-verification.
// This is a loop: if .restoring is held after acquiring the lock, the lock is
// released, a re-poll waits for .restoring to clear, and the loop retries.
func (s *LockProtocolState) joinerPhase3AcquireLock(deadline time.Time, pollInterval time.Duration, restoringPath string) error {
	manifestPath := filepath.Join(s.backupDir, manifest.ManifestFileName)
	lockPath := backup.RunLockPath(s.backupDir, s.runID)
	for {
		handle, err := filelock.Lock(lockPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errStartFresh
			}
			return fmt.Errorf("snapshot: joiner acquire run lock: %w", err)
		}
		s.runLock = handle

		if s.afterJoinerLockAcquired != nil {
			s.afterJoinerLockAcquired()
		}

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
			if pollErr := s.joinerWaitForRestoring(deadline, pollInterval, restoringPath, manifestPath); pollErr != nil {
				return pollErr
			}
			continue
		}

		if _, statErr := os.Stat(s.backupDir); os.IsNotExist(statErr) {
			s.runLock.Unlock() //nolint:errcheck
			os.Remove(lockPath) //nolint:errcheck
			s.runLock = nil
			return errStartFresh
		}

		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			s.runLock.Unlock() //nolint:errcheck
			s.runLock = nil
			return errStartFresh
		}

		return nil
	}
}
