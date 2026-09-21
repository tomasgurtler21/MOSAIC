package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// Option type -- injectable configuration for RecoveryCheck and
// SetupBackupAndTransform.
// ---------------------------------------------------------------------------

// Option configures the lock protocol behavior.
type Option func(*options)

// options holds the resolved configuration after applying all Option values.
type options struct {
	pollInterval time.Duration
	pollTimeout  time.Duration
	maxRetries   int
}

// WithPollInterval sets the interval for bounded polling loops. Default: 100ms.
// Values <= 0 are normalized to the default.
func WithPollInterval(d time.Duration) Option {
	return func(o *options) { o.pollInterval = d }
}

// WithPollTimeout sets the maximum duration for bounded polling loops.
// Default: 30s. Values <= 0 are normalized to the default.
func WithPollTimeout(d time.Duration) Option {
	return func(o *options) { o.pollTimeout = d }
}

// WithMaxRetries sets the maximum number of "start fresh" retries. Default: 3
// (meaning up to 4 total attempts). Values <= 0 are normalized to 0.
func WithMaxRetries(n int) Option {
	return func(o *options) { o.maxRetries = n }
}

// defaultOptions returns an options struct populated with default values.
func defaultOptions() options {
	return options{
		pollInterval: 100 * time.Millisecond,
		pollTimeout:  30 * time.Second,
		maxRetries:   3,
	}
}

// applyOptions applies each Option to a base options struct and returns the
// result with zero values normalized to defaults.
func applyOptions(opts []Option) options {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	if o.pollInterval <= 0 {
		o.pollInterval = 100 * time.Millisecond
	}
	if o.pollTimeout <= 0 {
		o.pollTimeout = 30 * time.Second
	}
	if o.maxRetries < 0 {
		o.maxRetries = 0
	}
	return o
}

// ---------------------------------------------------------------------------
// BackupState -- handle returned by SetupBackupAndTransform.
// ---------------------------------------------------------------------------

// BackupState is the handle returned by SetupBackupAndTransform.
// Its Cleanup method must be called on every terminal run outcome.
type BackupState struct {
	// RestoreFunc is an injectable function for testing. When non-nil, replaces
	// RestoreFromBackup during Cleanup. Nil in production.
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

// ---------------------------------------------------------------------------
// afterRecoveryInitialProbeForTest -- test hook for race scenario in
// RecoveryCheck. Declared here (non-test file) so the implementation can
// reference it. Set only in tests; nil in production.
// ---------------------------------------------------------------------------

// afterRecoveryInitialProbeForTest is called inside RecoveryCheck between the
// initial .lock-* probe phase and the .restoring acquisition. Nil in
// production. Tests may set it to inject a concurrent lock acquisition,
// simulating a joiner that slips in between the two probe phases.
//
// The implementation must call this hook (if non-nil) after the initial probe
// completes and before filelock.Lock(.restoring) is called.
var afterRecoveryInitialProbeForTest func()

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// isProtocolArtifact reports whether a file name inside the backup directory
// is a recognized protocol artifact. Only recognized files are permitted;
// subdirectories and any other file cause the ownership check to fail.
//
// Recognized artifacts: .lock-* files, .restoring, .setup-complete,
// recovery-manifest.json, .recovery-manifest.json.tmp, and .md files
// (agent backups written by CopyAgentsToBackup).
func isProtocolArtifact(name string) bool {
	switch name {
	case RestoringFileName, SetupCompleteFileName, ManifestFileName, ManifestTempFileName:
		return true
	}
	if strings.HasPrefix(name, RunLockPrefix) {
		return true
	}
	if isMDFile(name) {
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
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), RunLockPrefix) {
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

// ---------------------------------------------------------------------------
// RecoveryCheck -- startup recovery entry point.
// ---------------------------------------------------------------------------

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

	backupDir := filepath.Join(filepath.Dir(agentsDir), BackupDirName)

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
		// Foreign directory: skip, leave intact.
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
	// In production this is always nil. Tests use it to inject a joiner race.
	if afterRecoveryInitialProbeForTest != nil {
		afterRecoveryInitialProbeForTest()
	}

	// Acquire .restoring to serialize all recovery/exit operations.
	restoringPath := filepath.Join(backupDir, RestoringFileName)
	restoringHandle, lockErr := filelock.Lock(restoringPath)
	if lockErr != nil {
		if errors.Is(lockErr, os.ErrNotExist) {
			// Backup directory removed by a concurrent recovery: already recovered.
			return nil
		}
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  restoringPath,
			Reason:    fmt.Sprintf("cannot acquire .restoring: %v", lockErr),
		}
	}

	// Re-check backup dir: a concurrent recovery may have cleaned it up while we
	// were waiting for .restoring.
	if _, statErr := os.Stat(backupDir); os.IsNotExist(statErr) {
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	// Re-probe under .restoring (M1 fix): a joiner may have acquired its run
	// lock between the initial probe and our acquisition of .restoring.
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
		// Joiner slipped in: skip recovery.
		restoringHandle.Unlock() //nolint:errcheck
		return nil
	}

	// Determine backup state from manifest.
	_, manifestErr := ReadManifest(backupDir)
	if manifestErr != nil {
		var me *ManifestError
		if errors.As(manifestErr, &me) {
			switch me.Kind {
			case ManifestMissing:
				// Partial backup: crashed before WriteManifest. No transforms were
				// applied. Delete all contents while .restoring is held (serialized
				// under .restoring per AC9.9), then release and remove the now-empty
				// directory. On Windows, the open .restoring handle prevents RemoveAll
				// from deleting the file while the handle is held; delete every other
				// entry first, then unlock, then RemoveAll cleans up .restoring and
				// the empty directory.
				if entries, rdErr := os.ReadDir(backupDir); rdErr == nil {
					for _, e := range entries {
						if e.Name() != RestoringFileName {
							os.Remove(filepath.Join(backupDir, e.Name())) //nolint:errcheck
						}
					}
				}
				restoringHandle.Unlock() //nolint:errcheck
				os.RemoveAll(backupDir) //nolint:errcheck
				return nil

			case ManifestCorrupt:
				// Corrupt manifest: leave backup for manual inspection.
				restoringHandle.Unlock() //nolint:errcheck
				manifestPath := filepath.Join(backupDir, ManifestFileName)
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
	if restoreErr := RestoreFromBackup(agentsDir, backupDir); restoreErr != nil {
		restoringHandle.Unlock() //nolint:errcheck
		// A concurrent recoverer may have completed cleanup while we were inside
		// RestoreFromBackup: its RemoveAll deleted the backup files, causing our
		// copy to fail with access-denied or file-not-found on Windows. If the
		// backup directory is now gone, the other recoverer succeeded and this is
		// a no-op for us.
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
	// .restoring itself can be deleted by RemoveAll; filelock.Unlock closes the
	// underlying file handle). After this point, a concurrent recoverer may
	// acquire .restoring, but it will find the backup dir in the process of
	// being removed and will either observe it gone or handle the partial state.
	restoringHandle.Unlock() //nolint:errcheck
	os.RemoveAll(backupDir) //nolint:errcheck

	return nil
}

// ---------------------------------------------------------------------------
// SetupBackupAndTransform -- backup creation / join entry point.
// ---------------------------------------------------------------------------

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
	rules []TransformRule,
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
		backupDir, alreadyExisted, mkErr := CreateBackupDir(agentsDir)
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
			restoreFunc:  RestoreFromBackup,
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
			// Success: return the BackupState handle.
			return &BackupState{
				protocol:  state,
				agentsDir: agentsDir,
				logger:    logger,
			}, nil
		}

		// errStartFresh: the backup directory vanished unexpectedly (concurrent
		// recovery or teardown). Clean up any partially acquired lock and retry.
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
