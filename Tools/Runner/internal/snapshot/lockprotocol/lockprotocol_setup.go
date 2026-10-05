package lockprotocol

import (
	"errors"
	"fmt"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

// SetupBackupAndTransform creates or joins the backup-and-transform state.
//
// If rules is nil or empty, returns (nil, nil) -- caller skips backup.
// Validates runID (must be non-empty, no path separators).
//
// On success, returns a *BackupState whose Cleanup method must be called
// on run exit (via defer).
// On error, returns nil BackupState and a non-nil error.
func SetupBackupAndTransform(
	agentsDir string,
	runID string,
	rules []transform.TransformRule,
	logger domain.DebugLogger,
	opts ...Option,
) (*BackupState, error) {
	// FR-3: nil or empty rules means skip the backup entirely.
	if len(rules) == 0 {
		return nil, nil
	}

	// Validate runID before any filesystem operations.
	if refErr := validateRunID(agentsDir, runID); refErr != nil {
		return nil, refErr
	}

	o := applyOptions(opts)

	for attempt := 0; attempt <= o.maxRetries; attempt++ {
		backupDir, alreadyExisted, mkErr := backup.NewBackupDir(agentsDir)
		if mkErr != nil {
			return nil, &domain.RefusalError{
				Component: "snapshot",
				Resource:  agentsDir,
				Reason:    fmt.Sprintf("cannot create backup directory: %v", mkErr),
			}
		}

		// Build a LockProtocolState without acquiring the run lock. The lock is
		// acquired by setupAsCreator (step 1) or setupAsJoiner (phase 3).
		state := &LockProtocolState{
			agentsDir:    agentsDir,
			backupDir:    backupDir,
			runID:        runID,
			restoreFunc:  backup.RestoreFromBackup,
			pollInterval: o.pollInterval,
			pollTimeout:  o.pollTimeout,
			logger:       logger,
		}

		var loopErr error
		if !alreadyExisted {
			// Creator path: we won the mkdir race.
			loopErr = state.setupAsCreator(rules)
		} else {
			// Joiner path: backup dir already exists. Verify ownership before
			// proceeding -- foreign directories are refused immediately.
			owned, checkErr := isBackupDirOwned(backupDir)
			if checkErr != nil {
				return nil, &domain.RefusalError{
					Component: "snapshot",
					Resource:  backupDir,
					Reason:    fmt.Sprintf("cannot read backup directory: %v", checkErr),
				}
			}
			if !owned {
				return nil, &domain.RefusalError{
					Component: "snapshot",
					Resource:  backupDir,
					Reason:    fmt.Sprintf("foreign backup directory: %s", backupDir),
				}
			}
			loopErr = state.setupAsJoiner()
		}

		if loopErr == nil {
			return &BackupState{
				protocol:  state,
				agentsDir: agentsDir,
				logger:    logger,
			}, nil
		}

		// errStartFresh: the backup directory vanished unexpectedly. Clean up any
		// partially acquired lock and retry.
		if errors.Is(loopErr, errStartFresh) {
			if state.runLock != nil {
				state.runLock.Unlock() //nolint:errcheck
				state.runLock = nil
			}
			continue
		}

		// Poll timeout: wrap in RefusalError.
		if errors.Is(loopErr, errPollTimeout) {
			return nil, &domain.RefusalError{
				Component: "snapshot",
				Resource:  backupDir,
				Reason:    "poll timeout exceeded",
			}
		}

		// Already a RefusalError: return as-is.
		var refErr *domain.RefusalError
		if errors.As(loopErr, &refErr) {
			return nil, loopErr
		}

		// Unknown non-retryable error: wrap.
		return nil, &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("setup failed: %v", loopErr),
		}
	}

	// Dispatch loop exhausted all retries.
	return nil, &domain.RefusalError{
		Component: "snapshot",
		Resource:  agentsDir,
		Reason:    "retries exceeded",
	}
}
