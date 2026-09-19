// Package testdeploy invokes the mosaic-deploy binary to deploy the test
// catalog into a workspace. It follows the same CommandRunner seam pattern as
// Tools/AgentTest/internal/agentdeploy: an injectable function type replaces
// every subprocess call so the package is fully testable without a real binary.
//
// This package must not import mosaic-deploy. Both modules live in Tools/go.work
// and an import would compile — but would then drag the deployment tool's
// internal packages across the isolation boundary.
package testdeploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The deployment tool's stable exit codes, mirrored here as plain local
// constants. See Tools/Deployment/internal/cli/run.go for the authoritative
// source (ExitSuccess, ExitFailure, ExitUsage).
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 3
)

// Sentinel errors for every distinct failure class. Callers use errors.Is to
// detect the class and errors.As with *DeployError to read the detail.
var (
	// ErrToolUnavailable is returned when the deployment tool could not be
	// invoked at all — the binary is absent, not executable, or failed to start.
	ErrToolUnavailable = errors.New("testdeploy: deployment tool unavailable")

	// ErrDeployFailed is returned when the tool ran and exited non-zero.
	// Always unwrapped from a *DeployError that carries the tool's stderr and
	// exit code.
	ErrDeployFailed = errors.New("testdeploy: deploy failed")

	// ErrTimedOut is returned when the invocation exceeded Options.Timeout.
	ErrTimedOut = errors.New("testdeploy: timed out")
)

// CommandRunner is the injectable seam for subprocess execution.
// Matches the pattern in Tools/AgentTest/internal/agentdeploy.
type CommandRunner func(ctx context.Context, path string, args []string) (
	stdout []byte, stderr []byte, exitCode int, err error,
)

// Options configures the deploy invoker.
type Options struct {
	// ExecutablePath is the explicit path to the mosaic-deploy binary.
	// When empty, the deployer auto-discovers it as a sibling of the running
	// mosaic-run binary via os.Executable() + filepath.Dir().
	ExecutablePath string

	// Timeout bounds the deploy invocation. Zero means no timeout.
	Timeout time.Duration

	// Invoke is the subprocess runner. When nil, a default implementation
	// using exec.CommandContext is used.
	Invoke CommandRunner
}

// Deployer shells out to the mosaic-deploy binary to deploy the test catalog
// into a workspace.
type Deployer struct {
	opts Options
}

// New creates a Deployer with the given options.
func New(opts Options) *Deployer {
	return &Deployer{opts: opts}
}

// DeployError carries diagnostic detail for a failed deploy.
// It always wraps ErrDeployFailed.
type DeployError struct {
	// ToolMessage is the deploy tool's own text, taken from captured stderr.
	ToolMessage string
	// ExitCode is the deploy tool's process exit code.
	ExitCode int
}

func (e *DeployError) Error() string {
	return fmt.Sprintf("%s (exit %d): %s", ErrDeployFailed.Error(), e.ExitCode, e.ToolMessage)
}

// Unwrap returns ErrDeployFailed so callers can use errors.Is(err, ErrDeployFailed)
// to detect the class without knowing the concrete type.
func (e *DeployError) Unwrap() error { return ErrDeployFailed }

// Deploy deploys the test catalog into the workspace for the given harnesses.
// It invokes mosaic-deploy deploy once per harness with the flags:
// --mosaic-root (when non-empty), --catalog-folder, --workspace, --harness,
// --workflows (always emitted to prevent silent empty deployments), --auto-confirm.
//
// Exit 0 -> nil error.
// Non-zero exit -> *DeployError wrapping ErrDeployFailed.
// Invocation failure (binary missing, etc.) -> ErrToolUnavailable.
// Deployer timeout exceeded -> ErrTimedOut.
// Caller cancellation -> propagated as-is (not ErrTimedOut).
// Multi-harness: stops on the first harness error (fail-fast).
func (d *Deployer) Deploy(ctx context.Context, catalogFolder string,
	mosaicRoot string, workspace string, harnesses []string,
	workflows []string) error {

	invoke := d.opts.Invoke
	if invoke == nil {
		invoke = execCommandRunner
	}

	execPath := d.opts.ExecutablePath
	if execPath == "" {
		var err error
		execPath, err = discoverDeployBinary()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrToolUnavailable, err)
		}
	}

	for _, harness := range harnesses {
		if err := d.deployOne(ctx, invoke, execPath, catalogFolder, mosaicRoot, workspace, harness, workflows); err != nil {
			return err
		}
	}
	return nil
}

// deployOne performs a single mosaic-deploy invocation for one harness.
func (d *Deployer) deployOne(ctx context.Context, invoke CommandRunner, execPath string,
	catalogFolder, mosaicRoot, workspace, harness string, workflows []string) error {

	runCtx := ctx
	if d.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, d.opts.Timeout)
		defer cancel()
	}

	args := buildDeployArgs(catalogFolder, mosaicRoot, workspace, harness, workflows)
	_, stderr, exitCode, invokeErr := invoke(runCtx, execPath, args)

	if invokeErr != nil {
		// The deployer's own timeout fired: runCtx expired with DeadlineExceeded.
		// This is distinct from a caller cancellation (which produces Canceled).
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf(
				"%w: deployment tool %q did not respond within the configured timeout of %v",
				ErrTimedOut, execPath, d.opts.Timeout,
			)
		}
		// Caller's context was cancelled or timed out: propagate as-is, never
		// remap to ErrTimedOut or ErrToolUnavailable.
		if errors.Is(invokeErr, context.Canceled) || errors.Is(invokeErr, context.DeadlineExceeded) {
			return fmt.Errorf(
				"testdeploy: invocation of %q cancelled: %w",
				execPath, invokeErr,
			)
		}
		// Binary missing or not startable.
		return fmt.Errorf(
			"%w: deployment tool %q could not be invoked: %v",
			ErrToolUnavailable, execPath, invokeErr,
		)
	}

	if exitCode == exitSuccess {
		return nil
	}

	// Non-zero exit: the deploy subcommand emits no structured failure envelope.
	// ToolMessage comes from stderr.
	return &DeployError{
		ToolMessage: string(stderr),
		ExitCode:    exitCode,
	}
}

// buildDeployArgs constructs the argument list for the deployment tool's deploy
// subcommand. --workflows is always emitted (even when empty) to prevent a nil
// selection from resolving silently to an empty deployment.
func buildDeployArgs(catalogFolder, mosaicRoot, workspace, harness string, workflows []string) []string {
	args := []string{"deploy"}

	// --mosaic-root follows the empty-means-omitted convention: omit when empty,
	// letting the deploy tool resolve its own root.
	if mosaicRoot != "" {
		args = append(args, "--mosaic-root", mosaicRoot)
	}

	args = append(args, "--catalog-folder", catalogFolder)
	args = append(args, "--workspace", workspace)
	args = append(args, "--harness", harness)

	// --workflows is ALWAYS emitted, even when the list is empty.
	// An omitted --workflows flag causes the deploy tool to silently resolve to
	// an empty deployment that reports success while deploying only the orchestrator.
	// strings.Join handles nil and empty slices identically, producing "".
	args = append(args, "--workflows", strings.Join(workflows, ","))

	// --auto-confirm suppresses the plan-review gate, which fires on every deploy.
	args = append(args, "--auto-confirm")

	return args
}

// discoverDeployBinary locates mosaic-deploy as a sibling of the running
// mosaic-run binary via os.Executable() + filepath.Dir().
func discoverDeployBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	dir := filepath.Dir(self)
	name := "mosaic-deploy"
	if runtime.GOOS == "windows" {
		name = "mosaic-deploy.exe"
	}
	return filepath.Join(dir, name), nil
}

// execCommandRunner is the real process-execution implementation of
// CommandRunner. It captures both stdout and stderr.
func execCommandRunner(ctx context.Context, path string, args []string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitErr.ExitCode(), nil
	}
	// The binary could not be started or run — missing, not executable, or killed
	// by a signal. Return the error so the caller can map it to ErrToolUnavailable.
	return nil, stderrBuf.Bytes(), 0, runErr
}
