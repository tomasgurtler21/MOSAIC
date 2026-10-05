package filelock_test

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"mosaic-run/internal/filelock"
)

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
