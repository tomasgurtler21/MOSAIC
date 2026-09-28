package filelock_test

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// Error cases: missing parent directory
// ---------------------------------------------------------------------------

// TestLock_MissingParentDirectory verifies that Lock returns an error when the
// parent directory does not exist.
func TestLock_MissingParentDirectory(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "no-such-dir", "file.lock")

	h, err := filelock.Lock(nonExistent)
	if err == nil {
		if h != nil {
			h.Unlock()
		}
		t.Fatal("Lock with missing parent directory: expected error, got nil")
	}
}

// TestTryLock_MissingParentDirectory verifies that TryLock returns a non-nil
// error (not a panic) when the parent directory does not exist.
func TestTryLock_MissingParentDirectory(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "no-such-dir", "file.lock")

	h, ok, err := filelock.TryLock(nonExistent)
	if err == nil {
		if h != nil {
			h.Unlock()
		}
		t.Fatalf("TryLock with missing parent directory: expected error, got nil (ok=%v)", ok)
	}
}

// TestTryLock_ParentDirectoryDeletedAfterPathFormed verifies that TryLock
// returns an error and does not panic when the parent directory is deleted
// after the path string was formed.
func TestTryLock_ParentDirectoryDeletedAfterPathFormed(t *testing.T) {
	// Create a directory, then remove it to simulate the race where a caller
	// constructs the path before the directory vanishes.
	dir := t.TempDir()
	path := filepath.Join(dir, "file.lock")

	// Remove the directory to simulate the deletion.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	// TryLock must return an error, not panic.
	h, ok, err := filelock.TryLock(path)
	if err == nil {
		if h != nil {
			h.Unlock()
		}
		t.Fatalf("TryLock on deleted parent dir: expected error, got nil (ok=%v)", ok)
	}
	// Ensure no handle leaks.
	if h != nil {
		h.Unlock()
		t.Error("TryLock on deleted parent dir: returned non-nil handle with error")
	}
	_ = ok
}

// ---------------------------------------------------------------------------
// Unlock-before-delete pattern
// ---------------------------------------------------------------------------

// TestLock_UnlockDeleteRecreate verifies the Unlock-before-delete pattern:
// Lock, Unlock, delete the file, Lock again creates a fresh file and succeeds.
// This validates the protocol used by the lock cleanup logic.
func TestLock_UnlockDeleteRecreate(t *testing.T) {
	path := lockPath(t, "unlock-delete-recreate.lock")

	h1 := mustLock(t, path)
	mustUnlock(t, h1, "initial acquire")

	// Delete the lock file (only safe after Unlock on Windows).
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove lock file: %v", err)
	}

	// Lock again: must create a new file and succeed.
	h2, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("Lock after delete: unexpected error: %v", err)
	}
	if h2 == nil {
		t.Fatal("Lock after delete: returned nil handle")
	}
	defer h2.Unlock()

	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("Lock after delete did not create a new file: %v", statErr)
	}
}
