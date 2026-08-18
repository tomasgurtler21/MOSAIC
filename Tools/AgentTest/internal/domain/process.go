package domain

import (
	"os"
	"runtime"
	"syscall"
)

// ProcessAlive reports whether a process with the given PID is currently
// running. Both the lock-reclamation protocol and workspace reaping need
// this same liveness evidence, so it lives here rather than in either
// adapter, which may not import one another.
//
// On Windows, os.FindProcess itself performs a real liveness check (it
// opens a handle to the process and fails if none exists), so success there
// is sufficient. On Unix, os.FindProcess always succeeds regardless of
// whether the PID exists, so liveness there is checked by probing with
// signal 0, which the OS answers without actually delivering a signal.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer proc.Release()
	if runtime.GOOS == "windows" {
		// FindProcess already performed the real liveness check above.
		return true
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
