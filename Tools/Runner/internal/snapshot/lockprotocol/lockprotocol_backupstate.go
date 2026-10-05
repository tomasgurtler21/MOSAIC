package lockprotocol

import (
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
	"mosaic-run/internal/snapshot/transform"
)

// BackupState is the handle returned by SetupBackupAndTransform.
// Its Cleanup method must be called on every terminal run outcome.
type BackupState struct {
	// RestoreFunc is an injectable function for testing. When non-nil, replaces
	// backup.RestoreFromBackup during Cleanup. Nil in production.
	// Read at Cleanup time; can be set any time before Cleanup is called.
	RestoreFunc func(agentsDir, backupDir string) error

	// unexported fields
	protocol  *LockProtocolState
	agentsDir string
	logger    domain.DebugLogger
}

// Cleanup runs the last-out check: releases the per-run lock, probes for
// remaining active runs, and restores originals if this is the last run out.
//
// Must be called on every terminal outcome. Idempotent.
// Restore failure is returned but is non-fatal (FR-22).
func (bs *BackupState) Cleanup() error {
	if bs.protocol == nil {
		// Already cleaned up: idempotent no-op.
		return nil
	}

	// Use injected RestoreFunc if provided; override the protocol's restore function.
	if bs.RestoreFunc != nil {
		bs.protocol.restoreFunc = bs.RestoreFunc
	}

	err := bs.protocol.lastOutCheck()

	// Mark as cleaned up so subsequent calls are no-ops.
	bs.protocol = nil

	return err
}

// afterRecoveryInitialProbeForTest is called inside RecoveryCheck between the
// initial .lock-* probe phase and the .restoring acquisition. Nil in
// production. Tests may set it to inject a concurrent lock acquisition,
// simulating a joiner that slips in between the two probe phases.
//
// The implementation must call this hook (if non-nil) after the initial probe
// completes and before filelock.Lock(.restoring) is called.
var afterRecoveryInitialProbeForTest func()

// isProtocolArtifact reports whether a file name inside the backup directory
// is a recognized protocol artifact. Only recognized files are permitted;
// subdirectories and any other file cause the ownership check to fail.
//
// Recognized artifacts: .lock-* files, .restoring, .setup-complete,
// recovery-manifest.json, .recovery-manifest.json.tmp, and .md files
// (agent backups written by backup.CopyAgentsToBackup).
func isProtocolArtifact(name string) bool {
	switch name {
	case backup.RestoringFileName, backup.SetupCompleteFileName, manifest.ManifestFileName, manifest.ManifestTempFileName:
		return true
	}
	if strings.HasPrefix(name, backup.RunLockPrefix) {
		return true
	}
	if transform.IsMDFile(name) {
		return true
	}
	return false
}

// isBackupDirOwned reports whether all entries in backupDir are recognized
// protocol artifacts. Returns (false, nil) for a foreign directory or an
// empty-but-foreign layout. Returns (false, err) on filesystem errors
// (excluding os.ErrNotExist, which is treated as owned=false, err=nil so
// the caller can distinguish the two cases with Stat if needed).
func isBackupDirOwned(backupDir string) (bool, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return false, nil // subdirectories are not permitted
		}
		if !isProtocolArtifact(entry.Name()) {
			return false, nil
		}
	}
	return true, nil
}

// probeRunLocks scans backupDir for .lock-* files and probes each via
// filelock.TryLock. Returns true if any lock is currently held by another
// process/handle (live run). Acquirable locks (orphaned) are released but
// not deleted here; deletion happens via os.RemoveAll during teardown.
//
// A filesystem error when probing a single lock is treated conservatively:
// the lock is assumed held (returns true).
//
// If the backup directory does not exist, returns (false, nil) -- no locks.
// If ReadDir fails for any other reason (e.g., Windows ERROR_SHARING_VIOLATION
// when a concurrent RemoveAll has the directory open), returns (true, nil) as
// a conservative fallback: the caller treats this as "something is active" and
// skips recovery, which is always safe.
func probeRunLocks(backupDir string) (anyHeld bool, err error) {
	entries, readErr := os.ReadDir(backupDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return false, nil // directory gone: no locks
		}
		// Transient error (e.g., Windows sharing violation during concurrent
		// RemoveAll). Treat conservatively: skip recovery rather than risking
		// concurrent restore. The caller will re-check via Stat if needed.
		return true, nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), backup.RunLockPrefix) {
			continue
		}
		lockPath := filepath.Join(backupDir, entry.Name())
		h, acquired, tryErr := filelock.TryLock(lockPath)
		if tryErr != nil {
			// Filesystem error: treat conservatively as held.
			anyHeld = true
			continue
		}
		if acquired {
			h.Unlock() //nolint:errcheck -- orphaned; no active run owns it
		} else {
			anyHeld = true
		}
	}
	return anyHeld, nil
}
