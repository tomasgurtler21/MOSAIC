package filelock_test

import (
	"os"
	"testing"

	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// Happy path: Lock
// ---------------------------------------------------------------------------

// TestLock_NewFile verifies that Lock creates a new file and returns a valid handle.
func TestLock_NewFile(t *testing.T) {
	path := lockPath(t, "new.lock")

	h, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("Lock on new file: unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("Lock on new file: returned nil handle")
	}
	defer h.Unlock()

	// The file should exist after Lock.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("Lock created no file: Stat error: %v", statErr)
	}
}

// TestLock_ExistingFile verifies that Lock succeeds on an already-existing (unlocked) file.
func TestLock_ExistingFile(t *testing.T) {
	path := lockPath(t, "existing.lock")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	h, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("Lock on existing file: unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("Lock on existing file: returned nil handle")
	}
	h.Unlock()
}

// ---------------------------------------------------------------------------
// Happy path: TryLock
// ---------------------------------------------------------------------------

// TestTryLock_UnlockedFile verifies that TryLock on an unlocked file succeeds.
func TestTryLock_UnlockedFile(t *testing.T) {
	path := lockPath(t, "trylocked.lock")

	h, ok, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock on unlocked file: unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryLock on unlocked file: expected ok=true, got false")
	}
	if h == nil {
		t.Fatal("TryLock on unlocked file: returned nil handle with ok=true")
	}
	defer h.Unlock()
}

// TestTryLock_NewFile verifies that TryLock creates the file when it does not exist.
func TestTryLock_NewFile(t *testing.T) {
	path := lockPath(t, "trylock-new.lock")

	h, ok, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock on new path: unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryLock on new path: expected ok=true, got false")
	}
	if h == nil {
		t.Fatal("TryLock on new path: returned nil handle")
	}
	defer h.Unlock()

	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("TryLock created no file: Stat error: %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// Release and re-acquire
// ---------------------------------------------------------------------------

// TestLock_ReleaseAndReAcquire verifies that Lock, Unlock, Lock again succeeds.
func TestLock_ReleaseAndReAcquire(t *testing.T) {
	path := lockPath(t, "reacquire.lock")

	h1 := mustLock(t, path)
	mustUnlock(t, h1, "first acquire")

	h2, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("Lock after Unlock: unexpected error: %v", err)
	}
	if h2 == nil {
		t.Fatal("Lock after Unlock: returned nil handle")
	}
	mustUnlock(t, h2, "second acquire")
}

// TestTryLock_ReleaseAndReAcquire verifies that Lock, Unlock, TryLock succeeds.
func TestTryLock_ReleaseAndReAcquire(t *testing.T) {
	path := lockPath(t, "trylock-reacquire.lock")

	h1 := mustLock(t, path)
	mustUnlock(t, h1, "first acquire")

	h2, ok, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock after Unlock: unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryLock after Unlock: expected ok=true, got false")
	}
	if h2 == nil {
		t.Fatal("TryLock after Unlock: returned nil handle with ok=true")
	}
	mustUnlock(t, h2, "second acquire")
}

// ---------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------

// TestUnlock_Idempotent verifies that calling Unlock twice does not error.
func TestUnlock_Idempotent(t *testing.T) {
	path := lockPath(t, "idempotent.lock")

	h := mustLock(t, path)
	mustUnlock(t, h, "first unlock")

	// Second Unlock must be a no-op.
	if err := h.Unlock(); err != nil {
		t.Errorf("second Unlock: expected nil error, got %v", err)
	}
}
