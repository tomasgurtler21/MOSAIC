package workspace_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/workspace"
)

func testKey(t *testing.T, suffix string) domain.RunKey {
	t.Helper()
	return domain.RunKey{RunID: "run-" + suffix, TestID: "test-" + suffix, RunNumber: 1}
}

func TestCreate_MakesFreshIsolatedSandboxWithSeparateSubjectAndControlDirs(t *testing.T) {
	// Arrange
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())

	// Act
	sb, err := mgr.Create(testKey(t, "a"))

	// Assert
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if sb.SubjectDir == "" || sb.ControlDir == "" {
		t.Fatalf("Create returned a sandbox with an empty SubjectDir or ControlDir: %+v", sb)
	}
	if sb.SubjectDir == sb.ControlDir {
		t.Fatalf("SubjectDir and ControlDir must be distinct, got the same path %q", sb.SubjectDir)
	}
	if _, err := os.Stat(sb.SubjectDir); err != nil {
		t.Fatalf("SubjectDir was not created: %v", err)
	}
	if _, err := os.Stat(sb.ControlDir); err != nil {
		t.Fatalf("ControlDir was not created: %v", err)
	}
}

func TestCreate_RefusesASecondCreateForARunKeyThisManagerAlreadyCreated(t *testing.T) {
	// Arrange: the manager itself created this sandbox for this key, so it
	// has its own record that the key is taken — a distinct condition from
	// "the directory otherwise happens to be non-empty" for unrelated
	// reasons.
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	key := testKey(t, "b")
	if _, err := mgr.Create(key); err != nil {
		t.Fatalf("initial Create failed: %v", err)
	}

	// Act: a second Create for the same key this manager already created.
	_, err := mgr.Create(key)

	// Assert
	if err == nil {
		t.Fatal("expected a second Create for an already-created run key to fail, got nil error")
	}
	if !errors.Is(err, workspace.ErrSandboxExists) {
		t.Fatalf("expected ErrSandboxExists, got %v", err)
	}
}

func TestCreate_RefusesToAdoptExistingNonEmptyDirectory(t *testing.T) {
	// Arrange: the target directory is non-empty for reasons unrelated to
	// this manager ever having created a sandbox there — no manager record
	// of the key exists, only leftover content at the computed path. This is
	// distinct from ErrSandboxExists, which fires when the manager's own
	// record shows the key already taken.
	root := t.TempDir()
	mgr := workspace.NewManager(root, newFakeClock())
	key := testKey(t, "b-unrelated")
	sb, err := mgr.Locate(key)
	if err == nil {
		t.Fatalf("expected the key to be unknown to this manager before Create, got sandbox %+v", sb)
	}
	precomputed, err := mgr.Create(key)
	if err != nil {
		t.Fatalf("failed to determine the sandbox path via Create: %v", err)
	}
	if err := mgr.Teardown(precomputed); err != nil {
		t.Fatalf("failed to tear down the probing sandbox: %v", err)
	}
	freshMgr := workspace.NewManager(root, newFakeClock())
	if err := os.MkdirAll(precomputed.Root, 0o755); err != nil {
		t.Fatalf("failed to recreate the sandbox directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(precomputed.Root, "unrelated.txt"), []byte("leftover"), 0o644); err != nil {
		t.Fatalf("failed to plant unrelated content: %v", err)
	}

	// Act: a fresh manager (no record of this key) finds a non-empty
	// directory at the computed path.
	_, err = freshMgr.Create(key)

	// Assert
	if err == nil {
		t.Fatal("expected Create over an existing non-empty directory to fail, got nil error")
	}
	if !errors.Is(err, workspace.ErrDirectoryNotEmpty) {
		t.Fatalf("expected ErrDirectoryNotEmpty, got %v", err)
	}
}

func TestLocate_FindsAnExistingSandboxByRunIdentity(t *testing.T) {
	// Arrange
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	key := testKey(t, "c")
	created, err := mgr.Create(key)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Act
	found, err := mgr.Locate(key)

	// Assert
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if found.Root != created.Root {
		t.Fatalf("Locate returned a different sandbox root: got %q, want %q", found.Root, created.Root)
	}
}

func TestLocate_ReportsNotFoundForAnUnknownRunIdentity(t *testing.T) {
	// Arrange
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())

	// Act
	_, err := mgr.Locate(testKey(t, "never-created"))

	// Assert
	if !errors.Is(err, workspace.ErrSandboxNotFound) {
		t.Fatalf("expected ErrSandboxNotFound, got %v", err)
	}
}

func TestTeardown_RemovesTheWholeSandboxTree(t *testing.T) {
	// Arrange
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	sb, err := mgr.Create(testKey(t, "d"))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	seeded := filepath.Join(sb.SubjectDir, "seeded.txt")
	if err := os.WriteFile(seeded, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to seed a file: %v", err)
	}

	// Act
	err = mgr.Teardown(sb)

	// Assert
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}
	if _, statErr := os.Stat(sb.Root); !os.IsNotExist(statErr) {
		t.Fatalf("expected sandbox root %q to be gone after Teardown, stat error: %v", sb.Root, statErr)
	}
}

// writeLockInfo drops a lock file at s.LockPath() describing a holder, so
// Reap has the same evidence the lock protocol relies on for liveness and
// staleness.
func writeLockInfo(t *testing.T, s domain.Sandbox, info domain.LockInfo) {
	t.Helper()
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal lock info: %v", err)
	}
	if err := os.WriteFile(s.LockPath(), b, 0o644); err != nil {
		t.Fatalf("failed to write lock info: %v", err)
	}
}

func TestReap_RemovesWorkspacesOrphanedByACrashedRunAndLeavesLiveOnesAlone(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	root := t.TempDir()
	mgr := workspace.NewManager(root, clock)

	orphaned, err := mgr.Create(testKey(t, "dead"))
	if err != nil {
		t.Fatalf("Create(dead) failed: %v", err)
	}
	// A holder PID that (barring extraordinary coincidence) does not exist.
	writeLockInfo(t, orphaned, domain.LockInfo{PID: 999999991, Host: "gone", AcquiredAt: clock.Now(), ControlDir: orphaned.ControlDir})

	live, err := mgr.Create(testKey(t, "alive"))
	if err != nil {
		t.Fatalf("Create(alive) failed: %v", err)
	}
	writeLockInfo(t, live, domain.LockInfo{PID: os.Getpid(), Host: "here", AcquiredAt: clock.Now(), ControlDir: live.ControlDir})

	// Act
	reaped, err := mgr.Reap(workspace.ReapPolicy{OlderThan: 30 * time.Second})

	// Assert
	if err != nil {
		t.Fatalf("Reap returned unexpected error: %v", err)
	}
	if _, statErr := os.Stat(orphaned.Root); !os.IsNotExist(statErr) {
		t.Fatalf("expected orphaned sandbox %q to be reaped, stat error: %v", orphaned.Root, statErr)
	}
	if _, statErr := os.Stat(live.Root); statErr != nil {
		t.Fatalf("expected live sandbox %q to be left alone, stat error: %v", live.Root, statErr)
	}
	found := false
	for _, r := range reaped {
		if r.Root == orphaned.Root {
			found = true
		}
		if r.Root == live.Root {
			t.Fatalf("Reap reported the live sandbox %q as reaped", live.Root)
		}
	}
	if !found {
		t.Fatalf("expected Reap's report to include the orphaned sandbox %q, got %+v", orphaned.Root, reaped)
	}
}

func TestReap_DryRunReportsWithoutDeleting(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	root := t.TempDir()
	mgr := workspace.NewManager(root, clock)

	orphaned, err := mgr.Create(testKey(t, "dryrun-dead"))
	if err != nil {
		t.Fatalf("Create(dryrun-dead) failed: %v", err)
	}
	writeLockInfo(t, orphaned, domain.LockInfo{PID: 999999992, Host: "gone", AcquiredAt: clock.Now(), ControlDir: orphaned.ControlDir})

	// Act
	reaped, err := mgr.Reap(workspace.ReapPolicy{OlderThan: 30 * time.Second, DryRun: true})

	// Assert
	if err != nil {
		t.Fatalf("Reap returned unexpected error: %v", err)
	}
	if _, statErr := os.Stat(orphaned.Root); statErr != nil {
		t.Fatalf("DryRun must not delete anything, but sandbox %q is gone: %v", orphaned.Root, statErr)
	}
	found := false
	for _, r := range reaped {
		if r.Root == orphaned.Root {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected DryRun's report to include the orphaned sandbox %q it would have reaped, got %+v", orphaned.Root, reaped)
	}
}

func TestReap_ReclaimsAWorkspaceAgedPastOlderThanEvenWithALivePID(t *testing.T) {
	// Arrange: the holder's PID is genuinely alive, but the lock is older
	// than the staleness threshold — the age-based ("stale") reclaim path,
	// distinct from the dead-holder path covered above. Mirrors the lock
	// protocol's separate staleness-threshold reclaim.
	clock := newFakeClock()
	root := t.TempDir()
	mgr := workspace.NewManager(root, clock)

	stale, err := mgr.Create(testKey(t, "stale-live-pid"))
	if err != nil {
		t.Fatalf("Create(stale-live-pid) failed: %v", err)
	}
	acquiredAt := clock.Now()
	writeLockInfo(t, stale, domain.LockInfo{PID: os.Getpid(), Host: "here", AcquiredAt: acquiredAt, ControlDir: stale.ControlDir})
	clock.Advance(31 * time.Second)

	// Act
	reaped, err := mgr.Reap(workspace.ReapPolicy{OlderThan: 30 * time.Second})

	// Assert
	if err != nil {
		t.Fatalf("Reap returned unexpected error: %v", err)
	}
	if _, statErr := os.Stat(stale.Root); !os.IsNotExist(statErr) {
		t.Fatalf("expected the aged-past-OlderThan sandbox %q to be reaped despite a live PID, stat error: %v", stale.Root, statErr)
	}
	var got *workspace.ReapedSandbox
	for i := range reaped {
		if reaped[i].Root == stale.Root {
			got = &reaped[i]
		}
	}
	if got == nil {
		t.Fatalf("expected Reap's report to include %q, got %+v", stale.Root, reaped)
	}
	if got.Reason != "stale" {
		t.Fatalf("expected Reason %q for the age-based reclaim, got %q", "stale", got.Reason)
	}
}

func TestTeardown_OnAnAlreadyRemovedSandboxIsANoop(t *testing.T) {
	// Arrange: mirrors HarnessAdapter.Deprovision's explicit "must not fail
	// when a recorded path is already gone" — teardown must be safely
	// callable twice, since it runs on every exit path including a panic.
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	sb, err := mgr.Create(testKey(t, "double-teardown"))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := mgr.Teardown(sb); err != nil {
		t.Fatalf("first Teardown returned unexpected error: %v", err)
	}

	// Act: tear down the same, already-removed sandbox again.
	err = mgr.Teardown(sb)

	// Assert
	if err != nil {
		t.Fatalf("Teardown of an already-removed sandbox must be a no-op, got error: %v", err)
	}
}
