package filelock_test

import (
	"testing"

	"mosaic-run/internal/filelock"
)

// ---------------------------------------------------------------------------
// In-process contention
// ---------------------------------------------------------------------------

// TestTryLock_ContentionReturnsFalse verifies that a second TryLock on a
// path already locked (via a separate handle) returns false.
// This tests the mutual exclusion contract via two open file descriptions.
func TestTryLock_ContentionReturnsFalse(t *testing.T) {
	path := lockPath(t, "contention.lock")

	// Acquire the lock with the first handle.
	h1 := mustLock(t, path)
	defer h1.Unlock()

	// A second TryLock with a separate handle must report contention.
	h2, ok, err := filelock.TryLock(path)
	if err != nil {
		t.Fatalf("TryLock (second handle): unexpected error: %v", err)
	}
	if ok {
		h2.Unlock()
		t.Fatal("TryLock (second handle): expected ok=false (contention), got true")
	}
	if h2 != nil {
		h2.Unlock()
		t.Fatal("TryLock (second handle): expected nil handle on contention, got non-nil")
	}
}

// TestLockThenTryLock_SamePathDifferentHandle verifies the full in-process
// contention scenario: Lock, TryLock returns false, Unlock, TryLock succeeds.
func TestLockThenTryLock_SamePathDifferentHandle(t *testing.T) {
	path := lockPath(t, "lock-then-try.lock")

	// Step 1: Acquire the blocking lock.
	h1 := mustLock(t, path)

	// Step 2: TryLock with a different handle must fail.
	h2, ok, err := filelock.TryLock(path)
	if err != nil {
		h1.Unlock()
		t.Fatalf("TryLock while locked: unexpected error: %v", err)
	}
	if ok {
		h2.Unlock()
		h1.Unlock()
		t.Fatal("TryLock while locked: expected ok=false, got true")
	}

	// Step 3: Unlock the first handle.
	mustUnlock(t, h1, "first handle")

	// Step 4: TryLock must now succeed.
	h3, ok2, err2 := filelock.TryLock(path)
	if err2 != nil {
		t.Fatalf("TryLock after Unlock: unexpected error: %v", err2)
	}
	if !ok2 {
		t.Fatal("TryLock after Unlock: expected ok=true, got false")
	}
	if h3 == nil {
		t.Fatal("TryLock after Unlock: returned nil handle with ok=true")
	}
	mustUnlock(t, h3, "third handle")
}
