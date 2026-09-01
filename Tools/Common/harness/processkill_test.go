package harness

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestKillProcessTree_ValidProcess verifies that killProcessTree does not panic
// or error when given a valid process. We start a real subprocess, call
// killProcessTree, and verify the process is gone.
func TestKillProcessTree_ValidProcess(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >nul")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start subprocess: %v", err)
	}

	proc := cmd.Process
	killProcessTree(proc)

	// Wait for the process to exit (with a generous deadline).
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// Process exited — success.
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s after killProcessTree")
	}
}

// TestKillProcessTree_WindowsProcessTree verifies on Windows that killing a
// parent via killProcessTree also terminates its child process (the grandchild
// scenario that motivated this fix).
func TestKillProcessTree_WindowsProcessTree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	// Spawn a cmd.exe parent that spawns a ping child.
	// The parent is the process we will kill; the child should also die.
	parent := exec.Command("cmd", "/c", "ping -n 60 127.0.0.1 >nul")
	if err := parent.Start(); err != nil {
		t.Fatalf("failed to start parent process: %v", err)
	}
	parentPID := parent.Process.Pid

	// Give the child a moment to be created.
	time.Sleep(300 * time.Millisecond)

	// Kill the parent's entire process tree.
	killProcessTree(parent.Process)

	done := make(chan error, 1)
	go func() { done <- parent.Wait() }()

	select {
	case <-done:
		// Parent exited.
	case <-time.After(5 * time.Second):
		t.Fatalf("parent process (PID %d) did not exit within 5s", parentPID)
	}

	// Verify no child ping processes remain. A surviving child would appear
	// in tasklist; we check that wmic finds no processes whose ParentProcessId
	// matches the now-dead parent PID. On a real tree-kill this list should
	// be empty (or the PID recycled, which is acceptable here).
	check := exec.Command("wmic", "process", "where",
		"ParentProcessId="+strconv.Itoa(parentPID), "get", "ProcessId")
	out, _ := check.Output()
	// The output will contain only the header line "ProcessId" if no children
	// exist. A non-empty numeric line would indicate a surviving child.
	lines := 0
	for _, b := range out {
		if b == '\n' {
			lines++
		}
	}
	// More than 2 lines (header + blank + newline) means children survived.
	if lines > 2 {
		t.Errorf("child processes survived after killProcessTree; wmic output:\n%s", out)
	}
}

// TestKillProcessTree_AlreadyExited verifies that calling killProcessTree on an
// already-exited process does not panic.
func TestKillProcessTree_AlreadyExited(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit 0")
	} else {
		cmd = exec.Command("true")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start subprocess: %v", err)
	}
	_ = cmd.Wait() // let it exit naturally

	// Should not panic even though the process has already exited.
	proc := &os.Process{Pid: cmd.Process.Pid}
	killProcessTree(proc) // must not panic
}
