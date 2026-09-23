//go:build windows

// Windows file locking implementation using LockFileEx / UnlockFileEx via
// syscall.NewLazyDLL. This avoids promoting golang.org/x/sys/windows from
// an indirect dependency to a direct one.
//
// Lock semantics:
//   - Byte range 0..0 (length 1) is locked; locking beyond EOF is valid for
//     advisory locks on Windows.
//   - File is opened with O_CREATE|O_RDWR and default share flags so that
//     other handles (from this or other processes) can open and stat the file.
//   - Two handles to the same file from the same process DO conflict:
//     LockFileEx is per-handle, not per-process.
//   - Callers MUST Unlock before deleting the lock file on Windows.

package filelock

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	modKernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modKernel32.NewProc("LockFileEx")
	procUnlockFileEx = modKernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   uint32 = 0x00000002
	lockfileFailImmediately uint32 = 0x00000001

	// errLockViolation is ERROR_LOCK_VIOLATION (33) returned by LockFileEx
	// when the byte range is already locked and LOCKFILE_FAIL_IMMEDIATELY is set.
	errLockViolation syscall.Errno = 33
)

// lockImpl holds the Windows-specific lock state.
type lockImpl struct {
	file *os.File
}

// unlock releases the Windows file lock via UnlockFileEx, then closes the file.
func (l *lockImpl) unlock() error {
	ol := new(syscall.Overlapped) // offset 0 by default; unlocks the same region
	r1, _, winErr := procUnlockFileEx.Call(
		l.file.Fd(),
		0, // reserved, must be zero
		1, // nNumberOfBytesToUnlockLow
		0, // nNumberOfBytesToUnlockHigh
		uintptr(unsafe.Pointer(ol)),
	)
	runtime.KeepAlive(l.file)
	closeErr := l.file.Close()
	if r1 == 0 {
		return fmt.Errorf("filelock: UnlockFileEx: %w", winErr)
	}
	return closeErr
}

// callLockFileEx invokes LockFileEx with the given flags on byte range [0, 1).
// Returns a non-nil error on failure; the error is syscall.Errno for known cases.
func callLockFileEx(file *os.File, flags uint32) error {
	ol := new(syscall.Overlapped) // zero Offset/OffsetHigh = lock at byte 0
	r1, _, winErr := procLockFileEx.Call(
		file.Fd(),
		uintptr(flags),
		0, // reserved, must be zero
		1, // nNumberOfBytesToLockLow
		0, // nNumberOfBytesToLockHigh
		uintptr(unsafe.Pointer(ol)),
	)
	runtime.KeepAlive(file)
	if r1 == 0 {
		return winErr
	}
	return nil
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
	if err := callLockFileEx(f, lockfileExclusiveLock); err != nil {
		f.Close()
		return nil, fmt.Errorf("filelock: LockFileEx %q: %w", path, err)
	}
	return &Handle{impl: &lockImpl{file: f}}, nil
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
	lockErr := callLockFileEx(f, lockfileExclusiveLock|lockfileFailImmediately)
	if lockErr == nil {
		return &Handle{impl: &lockImpl{file: f}}, true, nil
	}
	f.Close()
	// ERROR_LOCK_VIOLATION means the region is already locked: report contention.
	if errno, ok := lockErr.(syscall.Errno); ok && errno == errLockViolation {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("filelock: TryLockFileEx %q: %w", path, lockErr)
}
