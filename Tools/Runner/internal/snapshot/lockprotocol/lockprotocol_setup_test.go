package lockprotocol

// Tests for SetupBackupAndTransform and BackupState.Cleanup: the public API
// surface consumed by session.go.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/transform"
)

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
	bs, err := SetupBackupAndTransform(agentsDir, "run-001", []transform.TransformRule{}, nil)

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
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
	backupDir := filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir foreign backupDir: %v", err)
	}
	// Write a file with an unrecognized name/extension to mark the directory
	// as foreign from the protocol's perspective.
	foreignFile := filepath.Join(backupDir, "user-data.txt")
	if err := os.WriteFile(foreignFile, []byte("not a protocol artifact\n"), 0o644); err != nil {
		t.Fatalf("setup: write foreign file: %v", err)
	}
	rules := transform.TransformationsFor("opencode")

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
	rules := transform.TransformationsFor("opencode")

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
