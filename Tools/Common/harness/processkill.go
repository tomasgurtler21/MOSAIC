package harness

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

// killProcessTree terminates the process and, on Windows, its entire
// descendant tree. On non-Windows platforms it falls back to p.Kill(),
// preserving existing behavior exactly.
//
// Errors are intentionally swallowed: the process may have already exited by
// the time the kill is attempted, matching the existing _ = p.Kill() pattern.
func killProcessTree(p *os.Process) {
	if runtime.GOOS == "windows" {
		// /F forces termination; /T kills the entire process tree rooted at
		// the given PID, including grandchildren (e.g. node.js spawned by a
		// .cmd shim wrapper).
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
		return
	}
	_ = p.Kill()
}
