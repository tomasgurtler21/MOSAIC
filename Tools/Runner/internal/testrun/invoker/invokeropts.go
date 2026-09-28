package invoker

import (
	"context"
	"path/filepath"
	"time"

	"mosaic-run/internal/domain"
)

// CommandRunner is the injectable seam for subprocess execution.
// The workDir parameter sets the child process working directory (cmd.Dir).
type CommandRunner func(
	ctx context.Context,
	workDir string,
	path string,
	args []string,
) (stdout []byte, stderr []byte, exitCode int, err error)

// RunInvokerOptions configures the SubprocessRunInvoker.
type RunInvokerOptions struct {
	// ExecutablePath is the path to the mosaic-run binary.
	// When empty, auto-discovered via os.Executable().
	ExecutablePath string

	// WorkingDir is the workspace directory (cwd for the subprocess).
	WorkingDir string

	// Timeout per invocation. Zero means no timeout.
	// Note: on context cancellation or timeout, exec.CommandContext kills the
	// subprocess. The killed process exit code is platform-dependent (typically
	// -1 on Unix, 1 on Windows). Tests should not assert a specific exit code
	// value for the timeout/cancellation case.
	Timeout time.Duration

	// Invoke is the subprocess runner. When nil, uses execCommandRunnerRun.
	// Signature includes workDir for setting the child process cwd.
	Invoke CommandRunner

	// DebugLogger is the diagnostic logger for test-flow events.
	// When nil, defaults to domain.NopDebugLogger{} (no logging, no panic).
	DebugLogger domain.DebugLogger
}

// SubprocessRunInvoker implements RunInvoker by shelling out to the
// mosaic-run binary with run subcommand flags.
type SubprocessRunInvoker struct {
	opts RunInvokerOptions

	// discoverBinaryFn, when non-nil, replaces the call to discoverSelfBinary()
	// for determining the mosaic-run executable path. This field is unexported
	// and exists solely for testing the binary-discovery-failure path without
	// requiring os.Executable() to fail.
	//
	// When nil (the default, including all production construction paths),
	// discoverBinary() calls the real discoverSelfBinary() function.
	discoverBinaryFn func() (string, error)
}

// NewSubprocessRunInvoker creates a SubprocessRunInvoker with the given options.
func NewSubprocessRunInvoker(opts RunInvokerOptions) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{opts: opts}
}

// DispatchLogPath derives the dispatch log file path from a workspace and run
// ID. This encodes the convention from dispatchlog.go:
//
//	{workspace}/RunnerLogs/{run_id}/{run_id}-dispatch.log
//
// Exported so the orchestrator can also use it for diagnostic purposes.
func DispatchLogPath(workspace string, runID string) string {
	return filepath.Join(workspace, "RunnerLogs", runID, runID+"-dispatch.log")
}
