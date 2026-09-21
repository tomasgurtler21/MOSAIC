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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// ---------------------------------------------------------------------------
// Cross-process lock release (test helper pattern)
// ---------------------------------------------------------------------------

// TestHelperHoldLock is a subprocess helper used by TestCrossProcess_LockReleasedOnExit.
// It acquires a lock on the path passed as the first argument, writes "ready" to
// stdout, then blocks until stdin closes (signaling the parent to proceed).
//
// This function runs in a subprocess and MUST NOT run in normal test execution.
func TestHelperHoldLock(t *testing.T) {
	if os.Getenv("FILELOCK_TEST_HELPER") != "1" {
		t.Skip("subprocess helper; only runs when FILELOCK_TEST_HELPER=1")
	}

	args := os.Args
	// Find the path argument after the -helper-path flag.
	path := ""
	for i, a := range args {
		if strings.HasPrefix(a, "-test.run") {
			continue
		}
		if i > 0 && args[i-1] == "-helper-path" {
			path = a
			break
		}
	}

	// Try the environment variable before falling back to positional args.
	// The parent test passes the lock path via FILELOCK_LOCK_PATH, so this is
	// the primary path-resolution mechanism.
	if path == "" {
		path = os.Getenv("FILELOCK_LOCK_PATH")
	}

	// Last resort: last non-flag positional arg, skipping args[0] (binary path).
	if path == "" {
		for _, a := range args[1:] {
			if !strings.HasPrefix(a, "-") {
				path = a
			}
		}
	}
	if path == "" {
		t.Fatal("TestHelperHoldLock: no lock path provided")
	}

	h, err := filelock.Lock(path)
	if err != nil {
		t.Fatalf("TestHelperHoldLock: Lock(%q): %v", path, err)
	}
	defer h.Unlock()

	// Signal the parent: print "ready\n" then block on stdin.
	os.Stdout.WriteString("ready\n")

	// Block until stdin closes (parent closes the pipe when it's done probing).
	buf := make([]byte, 1)
	os.Stdin.Read(buf) //nolint:errcheck
}

// TestCrossProcess_LockReleasedOnExit verifies that an OS-level lock is
// released automatically when the locking process exits.
//
// Strategy (re-exec helper pattern):
//  1. Spawn this binary as a subprocess with FILELOCK_TEST_HELPER=1 and
//     FILELOCK_LOCK_PATH=<path>. The helper acquires the lock and prints "ready".
//  2. Parent reads "ready", then probes with TryLock -- must return false.
//  3. Parent kills the subprocess.
//  4. Parent retries TryLock until success (with a timeout).
func TestCrossProcess_LockReleasedOnExit(t *testing.T) {
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("cross-process test not supported on this platform")
	}

	lockFile := lockPath(t, "cross-process.lock")

	// Spawn subprocess.
	cmd := exec.Command(os.Args[0],
		"-test.run=TestHelperHoldLock",
		"-test.v=false",
	)
	cmd.Env = append(os.Environ(),
		"FILELOCK_TEST_HELPER=1",
		"FILELOCK_LOCK_PATH="+lockFile,
	)
	stdinPipe, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		t.Fatalf("StdinPipe: %v", pipeErr)
	}
	stdoutPipe, outErr := cmd.StdoutPipe()
	if outErr != nil {
		t.Fatalf("StdoutPipe: %v", outErr)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	// Wait for "ready" signal.
	readyBuf := make([]byte, 6) // "ready\n"
	if _, err := stdoutPipe.Read(readyBuf); err != nil {
		cmd.Process.Kill()
		t.Fatalf("reading ready signal: %v", err)
	}

	// Probe: TryLock must return false (subprocess holds the lock).
	h, ok, err := filelock.TryLock(lockFile)
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("TryLock while subprocess holds lock: unexpected error: %v", err)
	}
	if ok {
		if h != nil {
			h.Unlock()
		}
		cmd.Process.Kill()
		t.Fatal("TryLock while subprocess holds lock: expected false, got true")
	}

	// Kill the subprocess; its lock should be released by the OS.
	stdinPipe.Close()
	cmd.Process.Kill()
	cmd.Wait()

	// Retry TryLock until success, with a bounded number of attempts.
	const maxAttempts = 20
	acquired := false
	var acquiredHandle *filelock.Handle
	for i := 0; i < maxAttempts; i++ {
		acquiredHandle, ok, err = filelock.TryLock(lockFile)
		if err != nil {
			t.Fatalf("TryLock after subprocess exit: attempt %d: unexpected error: %v", i+1, err)
		}
		if ok {
			acquired = true
			break
		}
	}
	if acquiredHandle != nil {
		acquiredHandle.Unlock()
	}
	if !acquired {
		t.Fatalf("TryLock after subprocess exit: lock was not released after %d attempts", maxAttempts)
	}
}
