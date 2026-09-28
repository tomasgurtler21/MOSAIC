package filelock_test

// Tests for the filelock package (exclusive OS-level file locking).
//
// Coverage:
//
//   Happy path:
//   - Lock on a new (non-existent) file creates the file and returns a non-nil handle.
//   - TryLock on an unlocked file returns (handle, true, nil).
//   - Unlock releases the lock without error.
//
//   In-process contention:
//   - Lock then TryLock on the same path via a separate handle returns (nil, false, nil).
//   - After Unlock, a subsequent TryLock on the same path succeeds (returns true).
//   - TryLock via a separate handle while the first handle holds the lock returns false.
//
//   Release and re-acquire:
//   - Lock, Unlock, then Lock again on the same path succeeds.
//   - Lock, Unlock, then TryLock on the same path succeeds.
//
//   Idempotency:
//   - Unlock on an already-released handle is a no-op (returns nil).
//
//   Error cases:
//   - Lock on a path whose parent directory does not exist returns a non-nil error.
//   - TryLock on a path whose parent directory does not exist returns a non-nil error.
//   - TryLock on a file whose parent directory has been deleted after the path was
//     formed returns a non-nil error (no panic).
//
//   Unlock-before-delete pattern:
//   - Lock, Unlock, delete the file, then Lock on the same path creates a new file
//     and succeeds (validates the Unlock-before-delete pattern).
//
//   Cross-process validation (test helper):
//   - A subprocess holds a lock; TryLock from the parent returns false.
//   - After the subprocess exits, TryLock from the parent returns true
//     (validates OS automatic lock release on process exit/crash).

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// lockPath returns a path to a lock file inside t's temp directory.
func lockPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// mustLock acquires an exclusive lock on path, failing the test on error.
func mustLock(t *testing.T, path string) *filelock.Handle {
	t.Helper()
	h, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("Lock(%q): unexpected error: %v", path, err)
	}
	if h == nil {
		t.Fatalf("Lock(%q): returned nil handle without error", path)
	}
	return h
}

// mustUnlock releases a lock, failing the test on error.
func mustUnlock(t *testing.T, h *filelock.Handle, desc string) {
	t.Helper()
	if err := h.Unlock(); err != nil {
		t.Fatalf("Unlock (%s): unexpected error: %v", desc, err)
	}
}
