package testdeploy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/testdeploy"
)

// =============================================================================
// Exit-code-to-error mapping
// =============================================================================

// TestDeploy_ExitSuccess_ReturnsNilError asserts that exit 0 from the deploy
// tool produces a nil error — the happy path.
func TestDeploy_ExitSuccess_ReturnsNilError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, exitSuccess, nil
		},
	})

	err := minimalDeploy(d)

	if err != nil {
		t.Errorf("Deploy returned error on exit 0: %v; want nil error on success", err)
	}
}

// TestDeploy_ExitFailure_ReturnsErrDeployFailed asserts that exit 1 from the
// deploy tool maps to a sentinel error wrapping ErrDeployFailed.
func TestDeploy_ExitFailure_ReturnsErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed: workspace not found"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want an error wrapping ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want it to wrap ErrDeployFailed (exit 1 is a deploy failure)", err)
	}
}

// TestDeploy_ExitUsage_ReturnsErrDeployFailed asserts that exit 3 (flag/usage
// error from the deploy tool) also maps to ErrDeployFailed. From the caller's
// perspective a bad-argument error is still a deployment failure — it is not
// a distinct error class.
func TestDeploy_ExitUsage_ReturnsErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("flag --harness is required"), exitUsage, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 3, want an error wrapping ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want it to wrap ErrDeployFailed (exit 3 is a deploy failure)", err)
	}
}

// TestDeploy_DeployFailed_StderrInToolMessage asserts that the deploy tool's
// stderr text is preserved in DeployError.ToolMessage so that diagnostics can
// quote the tool's own message rather than paraphrasing it.
func TestDeploy_DeployFailed_StderrInToolMessage(t *testing.T) {
	const stderrText = "error: workspace /nonexistent does not exist"

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte(stderrText), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want ErrDeployFailed")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("Deploy error = %v, want a *DeployError carrying the tool's stderr", err)
	}
	if !strings.Contains(de.ToolMessage, stderrText) {
		t.Errorf("DeployError.ToolMessage = %q, want it to contain the stderr text %q", de.ToolMessage, stderrText)
	}
}

// TestDeploy_DeployFailed_ExitCodeInDeployError asserts that the actual exit
// code from the deploy tool is preserved in DeployError.ExitCode so that
// diagnostics can report the exact code.
func TestDeploy_DeployFailed_ExitCodeInDeployError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("something went wrong"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1, want ErrDeployFailed")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Fatalf("Deploy error = %v, want a *DeployError", err)
	}
	if de.ExitCode != exitFailure {
		t.Errorf("DeployError.ExitCode = %d, want %d", de.ExitCode, exitFailure)
	}
}

// =============================================================================
// ErrDeployFailed wrapping contract
// =============================================================================

// TestDeploy_DeployError_UnwrapsToErrDeployFailed asserts that errors.Is
// returns true for ErrDeployFailed on a *DeployError. This is the standard
// Go error sentinel pattern — callers should be able to branch on the class
// without knowing the concrete type.
func TestDeploy_DeployError_UnwrapsToErrDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("errors.Is(err, ErrDeployFailed) = false, want true; error = %v", err)
	}
}

// TestDeploy_DeployError_ErrorsAsDeployError asserts that errors.As unwraps
// a *DeployError from a deploy failure. Callers need this to read ToolMessage
// and ExitCode for diagnostic output.
func TestDeploy_DeployError_ErrorsAsDeployError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error on exit 1")
	}
	var de *testdeploy.DeployError
	if !errors.As(err, &de) {
		t.Errorf("errors.As(err, &DeployError) = false, want true; error = %v", err)
	}
}

// =============================================================================
// ErrToolUnavailable: binary not found or not executable
// =============================================================================

// TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable asserts that an
// invocation failure (the binary is absent, not executable, or killed before
// the exit code is known) maps to ErrToolUnavailable, not to ErrDeployFailed.
// Binary-not-found is an infrastructure problem, not a deploy failure.
func TestDeploy_ToolUnavailable_ReturnsErrToolUnavailable(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: executable file not found in $PATH")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !errors.Is(err, testdeploy.ErrToolUnavailable) {
		t.Errorf("Deploy error = %v, want it to wrap ErrToolUnavailable (not ErrDeployFailed)", err)
	}
}

// TestDeploy_ToolUnavailable_ErrorNamesExecutablePath asserts that the
// ErrToolUnavailable error names the executable path that was searched, so
// the operator knows what binary to install or what path to override.
func TestDeploy_ToolUnavailable_ErrorNamesExecutablePath(t *testing.T) {
	const binaryPath = "/usr/local/bin/mosaic-deploy"

	d := testdeploy.New(testdeploy.Options{
		ExecutablePath: binaryPath,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: no such file or directory")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked, want ErrToolUnavailable")
	}
	if !strings.Contains(err.Error(), binaryPath) {
		t.Errorf("error = %q, want it to name the executable path %q so the operator knows what was searched", err.Error(), binaryPath)
	}
}

// TestDeploy_ToolUnavailable_IsDistinctFromDeployFailed asserts that
// ErrToolUnavailable and ErrDeployFailed are distinct error classes — they
// must not satisfy each other's errors.Is check.
func TestDeploy_ToolUnavailable_IsDistinctFromDeployFailed(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("exec: no such file or directory")
		},
	})

	err := minimalDeploy(d)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool cannot be invoked")
	}
	if errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("errors.Is(err, ErrDeployFailed) = true for a binary-not-found error, want false; "+
			"ErrToolUnavailable and ErrDeployFailed must be distinct sentinel classes (err = %v)", err)
	}
}

// =============================================================================
// Timeout and caller cancellation
// =============================================================================

// TestDeploy_TimeoutExceeded_ReturnsErrTimedOut asserts that when the deploy
// tool exceeds the configured timeout, the call returns ErrTimedOut and does
// not hang. The timeout applied by the deployer bounds the delegate's runtime,
// so a stalled binary degrades a run rather than hanging it.
func TestDeploy_TimeoutExceeded_ReturnsErrTimedOut(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Timeout: 20 * time.Millisecond,
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			// Block until the context the deployer applied expires.
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	// The outer context is generous; the deployer's own timeout fires first.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := d.Deploy(ctx, "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if err == nil {
		t.Fatal("Deploy returned nil error when the tool timed out, want ErrTimedOut")
	}
	if !errors.Is(err, testdeploy.ErrTimedOut) {
		t.Errorf("Deploy error = %v, want it to wrap ErrTimedOut (the deployer's own timeout, not the caller's context)", err)
	}
}

// TestDeploy_CallerContextCancelled_ReturnsError asserts that if the caller's
// own context is cancelled, the call returns promptly with a non-nil error
// rather than continuing. The error must not wrap ErrTimedOut — a caller
// cancellation is distinct from the deployer's internal timeout.
func TestDeploy_CallerContextCancelled_ReturnsError(t *testing.T) {
	d := newDeployer(testdeploy.Options{
		Timeout: 10 * time.Second, // much longer than the test will run
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			<-ctx.Done()
			return nil, nil, 0, ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately before Deploy is called

	err := d.Deploy(ctx, "/catalog", "/mosaic", "/workspace", []string{"auto"}, []string{"smoke-single"}, nil)

	if err == nil {
		t.Fatal("Deploy returned nil error when the caller's context was cancelled, want a non-nil error")
	}
	if errors.Is(err, testdeploy.ErrTimedOut) {
		t.Errorf("Deploy error = %v, want it NOT to wrap ErrTimedOut for a caller cancellation "+
			"(ErrTimedOut is reserved for the deployer's own internal timeout)", err)
	}
}

// =============================================================================
// Multi-harness failure: stops on first error
// =============================================================================

// TestDeploy_MultipleHarnesses_FirstHarnessFailure_StopsEarly asserts that
// if the first harness invocation fails (non-zero exit), the deployer returns
// the error immediately without invoking the runner for subsequent harnesses.
func TestDeploy_MultipleHarnesses_FirstHarnessFailure_StopsEarly(t *testing.T) {
	invocations := 0

	d := newDeployer(testdeploy.Options{
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, []byte, int, error) {
			invocations++
			// First invocation fails; second should never be reached.
			return nil, []byte("deploy failed"), exitFailure, nil
		},
	})

	err := d.Deploy(context.Background(), "/catalog", "/mosaic", "/workspace",
		[]string{"auto", "claude-code"}, []string{"smoke-single"}, nil)

	if err == nil {
		t.Fatal("Deploy returned nil error after first harness failed, want ErrDeployFailed")
	}
	if !errors.Is(err, testdeploy.ErrDeployFailed) {
		t.Errorf("Deploy error = %v, want ErrDeployFailed", err)
	}
	if invocations != 1 {
		t.Errorf("CommandRunner invoked %d time(s), want exactly 1 "+
			"(deployer must stop on first harness failure, not continue to remaining harnesses)", invocations)
	}
}
