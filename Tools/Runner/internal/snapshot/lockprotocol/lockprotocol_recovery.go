package lockprotocol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
)

// validateRunID returns a non-nil error if runID is empty or contains path
// separator characters. A runID with path separators could allow the run-lock
// file to escape its intended directory (path traversal risk).
func validateRunID(agentsDir, runID string) *domain.RefusalError {
	if runID == "" {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    "invalid run ID: empty string",
		}
	}
	if strings.ContainsRune(runID, '/') {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    fmt.Sprintf("invalid run ID: contains path separator: %q", runID),
		}
	}
	if runtime.GOOS == "windows" && strings.ContainsRune(runID, '\\') {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  agentsDir,
			Reason:    fmt.Sprintf("invalid run ID: contains path separator: %q", runID),
		}
	}
	return nil
}

// recoveryHandlePartialBackup deletes all backup directory contents except
// .restoring (which is still held by restoringHandle), then releases the
// handle and removes the backup directory. Used when the manifest is missing
// (crashed before manifest.WriteManifest; no transforms were applied).
func recoveryHandlePartialBackup(backupDir string, restoringHandle *filelock.Handle) {
	if entries, rdErr := os.ReadDir(backupDir); rdErr == nil {
		for _, e := range entries {
			if e.Name() != backup.RestoringFileName {
				os.Remove(filepath.Join(backupDir, e.Name())) //nolint:errcheck
			}
		}
	}
	restoringHandle.Unlock() //nolint:errcheck
	os.RemoveAll(backupDir)  //nolint:errcheck
}

// recoveryRestoreAndCleanup restores originals from backup and removes the
// backup directory. restoringHandle is released inside this function after
// the restore succeeds and before RemoveAll.
//
// Returns nil on success or when a concurrent recoverer already cleaned up.
// Returns the restore error if restore fails and the backup dir still exists.
func recoveryRestoreAndCleanup(agentsDir, backupDir string, restoringHandle *filelock.Handle, logger domain.DebugLogger) error {
	if restoreErr := backup.RestoreFromBackup(agentsDir, backupDir); restoreErr != nil {
		restoringHandle.Unlock() //nolint:errcheck
		// A concurrent recoverer may have completed cleanup while we were inside
		// backup.RestoreFromBackup: its RemoveAll deleted the backup files, causing our
		// copy to fail. If the backup directory is now gone, the other recoverer
		// succeeded and this is a no-op for us.
		if _, statErr := os.Stat(backupDir); os.IsNotExist(statErr) {
			return nil
		}
		return restoreErr
	}

	if logger != nil {
		logger.Log(domain.EventSnapshotRecovery,
			fmt.Sprintf("snapshot recovered from backup at startup: %s", backupDir),
			domain.F("agents_dir", agentsDir))
	}

	// Release .restoring before RemoveAll (Windows-safe: close the handle so
	// .restoring itself can be deleted by RemoveAll).
	restoringHandle.Unlock() //nolint:errcheck
	os.RemoveAll(backupDir)  //nolint:errcheck
	return nil
}

// RecoveryCheck checks for orphaned backup-and-transform state from a previous
// crashed run and restores originals if safe.
//
// Before any destructive operation, verifies the backup directory is owned
// by the lock protocol. Foreign directories are skipped.
//
// Probes all .lock-* files; if any is held, skips recovery (another run is
// active). If none are held, acquires .restoring, re-probes under .restoring,
// and restores if still safe.
//
// Idempotent: no-op when nothing needs recovery.
// No-op for path-based harnesses (no backup directory exists).
// Returns *domain.RefusalError for a corrupt manifest (backup left for manual
// inspection).
func RecoveryCheck(agentsDir string, logger domain.DebugLogger, opts ...Option) error {
	_ = applyOptions(opts) // consume options (reserved for future timeout use)

	backupDir := filepath.Join(filepath.Dir(agentsDir), backup.BackupDirName)

	// No backup directory: normal startup, nothing to recover.
	if _, err := os.Stat(backupDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot stat backup directory: %v", err),
		}
	}

	// Verify ownership: foreign directories are silently skipped.
	owned, checkErr := isBackupDirOwned(backupDir)
	if checkErr != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot read backup directory: %v", checkErr),
		}
	}
	if !owned {
		return nil
	}

	// Initial probe: if any run lock is held, another run is active; skip recovery.
	anyHeld, probeErr := probeRunLocks(backupDir)
	if probeErr != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot probe run locks: %v", probeErr),
		}
	}
	if anyHeld {
		return nil
	}

	// M1 fix: call test hook between initial probe and .restoring acquisition.
	if afterRecoveryInitialProbeForTest != nil {
		afterRecoveryInitialProbeForTest()
	}

	// Acquire .restoring to serialize all recovery/exit operations.
	restoringPath := filepath.Join(backupDir, backup.RestoringFileName)
	restoringHandle, lockErr := filelock.Lock(restoringPath)
	if lockErr != nil {
		if errors.Is(lockErr, os.ErrNotExist) {
			return nil
		}
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  restoringPath,
			Reason:    fmt.Sprintf("cannot acquire .restoring: %v", lockErr),
		}
	}

	// Re-check backup dir: a concurrent recovery may have cleaned it up.
	if _, statErr := os.Stat(backupDir); os.IsNotExist(statErr) {
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	// Re-probe under .restoring: a joiner may have acquired its run lock
	// between the initial probe and our acquisition of .restoring.
	anyHeldReprobe, reprErr := probeRunLocks(backupDir)
	if reprErr != nil {
		restoringHandle.Unlock() //nolint:errcheck
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("re-probe run locks: %v", reprErr),
		}
	}
	if anyHeldReprobe {
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	// Determine backup state from manifest.
	_, manifestErr := manifest.ReadManifest(backupDir)
	if manifestErr != nil {
		var me *manifest.ManifestError
		if errors.As(manifestErr, &me) {
			switch me.Kind {
			case manifest.ManifestMissing:
				// Partial backup: crashed before manifest.WriteManifest; no transforms applied.
				recoveryHandlePartialBackup(backupDir, restoringHandle)
				return nil

			case manifest.ManifestCorrupt:
				restoringHandle.Unlock() //nolint:errcheck
				manifestPath := filepath.Join(backupDir, manifest.ManifestFileName)
				return &domain.RefusalError{
					Component: "snapshot",
					Resource:  manifestPath,
					Reason:    fmt.Sprintf("corrupt manifest: %v", manifestErr),
				}
			}
		}
		restoringHandle.Unlock() //nolint:errcheck
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  backupDir,
			Reason:    fmt.Sprintf("cannot read manifest: %v", manifestErr),
		}
	}

	// Full backup: restore originals and tear down.
	return recoveryRestoreAndCleanup(agentsDir, backupDir, restoringHandle, logger)
}
