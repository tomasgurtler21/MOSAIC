//go:build linux || darwin || freebsd || netbsd || openbsd

// Unix file locking implementation using syscall.Flock.
//
// Build tag uses an explicit OS list rather than !windows to exclude
// platforms that lack syscall.Flock (plan9, solaris, aix, js).
//
// Lock semantics:
//   - flock(2) is per open file description; two os.OpenFile calls on the
//     same path produce independent file descriptions, so in-process
//     contention tests work correctly.
//   - LOCK_EX|LOCK_NB (TryLock) returns EWOULDBLOCK on contention.
//   - LOCK_EX (Lock) blocks until the lock is available; retries on EINTR.
//   - LOCK_UN (unlock) is called before Close so the kernel sees an explicit
//     release rather than relying on close-releases-all-flock behavior.

package filelock

import (
	"fmt"
	"os"
	"syscall"
)

// lockImpl holds the Unix-specific lock state.
type lockImpl struct {
	file *os.File
}

// unlock releases the flock and closes the file.
func (l *lockImpl) unlock() error {
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		l.file.Close()
		return fmt.Errorf("filelock: flock LOCK_UN: %w", err)
	}
	return l.file.Close()
}

// Lock acquires an exclusive lock on the file at path, creating the file if
// it does not exist. Blocks indefinitely until the lock is acquired.
//
// Returns an error if the parent directory does not exist or the file cannot
// be opened.
func Lock(path string) (*Handle, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf("filelock: open %q: %w", path, err)
	}
	// Blocking exclusive lock; retry on EINTR (signal interrupted the syscall).
	for {
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if lockErr == nil {
			return &Handle{impl: &lockImpl{file: f}}, nil
		}
		if lockErr == syscall.EINTR {
			continue
		}
		f.Close()
		return nil, fmt.Errorf("filelock: flock LOCK_EX %q: %w", path, lockErr)
	}
}

// TryLock attempts to acquire an exclusive lock on the file at path without
// blocking. Creates the file if it does not exist.
//
// Returns (*Handle, true, nil) if the lock was acquired.
// Returns (nil, false, nil) if the file is already locked (contention).
// Returns (nil, false, error) on filesystem errors.
func TryLock(path string) (*Handle, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, false, fmt.Errorf("filelock: open %q: %w", path, err)
	}
	lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if lockErr == nil {
		return &Handle{impl: &lockImpl{file: f}}, true, nil
	}
	f.Close()
	// EWOULDBLOCK means the lock is already held: report contention.
	if lockErr == syscall.EWOULDBLOCK {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("filelock: flock LOCK_EX|LOCK_NB %q: %w", path, lockErr)
}
