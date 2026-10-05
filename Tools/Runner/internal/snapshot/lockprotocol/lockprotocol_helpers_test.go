package lockprotocol

// Shared test helpers for lock-protocol tests. All files in this package's
// test suite have access to these helpers because they are in the same package.
//
// State builders follow the newTestFoo(t, ...) naming convention:
//   newTestFullBackupState    - full backup with transforms and .setup-complete
//   newTestBackupStateWithManifestOnly - manifest written, no .setup-complete
//   newTestPartialBackup      - backup dir only, no manifest (pre-WriteManifest crash)

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/snapshot/backup"
	"mosaic-run/internal/snapshot/manifest"
	"mosaic-run/internal/snapshot/transform"
)

// ---------------------------------------------------------------------------
// State builders
// ---------------------------------------------------------------------------

// newTestFullBackupState creates a ready-to-use backup-and-transform state under
// base: creates agentsDir with a transformable worker.md, creates backupDir,
// copies agents to backup, writes manifest, writes recovery marker, transforms
// in place, and writes setup-complete. Returns agentsDir and backupDir.
func newTestFullBackupState(t *testing.T, base string) (agentsDir, backupDir string) {
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

	backupDir = filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}

	rules := transform.TransformationsFor("opencode")

	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: backup.CopyAgentsToBackup: %v", err)
	}
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("setup: manifest.WriteManifest: %v", err)
	}
	if err := backup.WriteRecoveryMarker(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: backup.WriteRecoveryMarker: %v", err)
	}
	if err := backup.TransformInPlace(agentsDir, rules); err != nil {
		t.Fatalf("setup: backup.TransformInPlace: %v", err)
	}
	if err := backup.WriteSetupComplete(backupDir); err != nil {
		t.Fatalf("setup: backup.WriteSetupComplete: %v", err)
	}

	return agentsDir, backupDir
}

// newTestBackupStateWithManifestOnly creates agentsDir with worker.md,
// creates backupDir, copies agents to backup, and writes the manifest.
// Does NOT run TransformInPlace or write .setup-complete.
// Used for joiner tests exercising the setup-complete-wait path.
func newTestBackupStateWithManifestOnly(t *testing.T, base string) (agentsDir, backupDir string) {
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
	backupDir = filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	rules := transform.TransformationsFor("opencode")
	if err := backup.CopyAgentsToBackup(agentsDir, backupDir); err != nil {
		t.Fatalf("setup: backup.CopyAgentsToBackup: %v", err)
	}
	if err := manifest.WriteManifest(backupDir, agentsDir, rules); err != nil {
		t.Fatalf("setup: manifest.WriteManifest: %v", err)
	}
	return agentsDir, backupDir
}

// newTestPartialBackup creates agentsDir with worker.md (untransformed) and a
// backupDir with no manifest and no lock files. This represents a crash that
// occurred after NewBackupDir but before WriteManifest -- the "partial
// backup" state defined in the design.
func newTestPartialBackup(t *testing.T, base string) (agentsDir, backupDir string) {
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
	backupDir = filepath.Join(base, backup.BackupDirName)
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatalf("setup: mkdir backupDir: %v", err)
	}
	return agentsDir, backupDir
}

// ---------------------------------------------------------------------------
// Other shared helpers
// ---------------------------------------------------------------------------

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
		restoreFunc:  backup.RestoreFromBackup,
		pollInterval: 5 * time.Millisecond,
		pollTimeout:  500 * time.Millisecond,
	}
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
// dispatch logic: call NewBackupDir, dispatch to creator or joiner, retry
// on errStartFresh up to maxRetries times.
//
// In this helper, each creator attempt is forced to fail with errStartFresh by
// deleting the backup directory BEFORE lock acquisition, guaranteeing the loop
// exhausts retries and returns errRetriesExceeded.
func testRunDispatchLoop(t *testing.T, agentsDir, runID string, rules []transform.TransformRule, maxRetries int) error {
	t.Helper()
	for attempt := 0; attempt <= maxRetries; attempt++ {
		backupDir, alreadyExisted, err := backup.NewBackupDir(agentsDir)
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
			restoreFunc:  backup.RestoreFromBackup,
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

// writeCorruptManifest writes a syntactically invalid JSON file to backupDir
// as ManifestFileName. Used to test that a corrupt manifest causes a RefusalError
// and leaves the backup directory intact.
func writeCorruptManifest(t *testing.T, backupDir string) {
	t.Helper()
	manifestPath := filepath.Join(backupDir, manifest.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("not-valid-json{{{"), 0o644); err != nil {
		t.Fatalf("writeCorruptManifest: %v", err)
	}
}

// backupDirPath returns the path of the backup directory that is a sibling of
// agentsDir. Mirrors the production BackupDirName placement.
func backupDirPath(agentsDir string) string {
	return filepath.Join(filepath.Dir(agentsDir), backup.BackupDirName)
}

// ---------------------------------------------------------------------------
// spyLogger
// ---------------------------------------------------------------------------

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
