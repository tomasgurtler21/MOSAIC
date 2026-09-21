// Package filelock provides cross-platform exclusive OS-level file locking.
//
// Lock operations use flock(2) on Unix (Linux, macOS, FreeBSD, NetBSD, OpenBSD)
// and LockFileEx on Windows. Locks are automatically released by the OS when
// the holding process exits or crashes.
//
// Caller contract:
//   - On Windows a locked file cannot be deleted. Callers MUST call Unlock
//     before deleting the lock file.
//   - Use TryLock for non-blocking probes; use Lock only where blocking is correct.
package filelock

import "sync"

// Handle represents an acquired file lock. The lock is exclusive and
// OS-level: released automatically by the OS if the process exits or crashes.
type Handle struct {
	mu       sync.Mutex
	released bool
	// platform-specific state is in the platform files.
	impl *lockImpl
}

// Unlock releases the lock and closes the underlying file handle.
// Idempotent: calling Unlock on an already-released handle is a no-op.
// Thread-safe.
func (h *Handle) Unlock() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return nil
	}
	h.released = true
	return h.impl.unlock()
}
