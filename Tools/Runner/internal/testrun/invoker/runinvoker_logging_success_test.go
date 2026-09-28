// Tests for the debug events SubprocessRunInvoker.Invoke emits on the success path and with a nil logger.
package invoker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/domain"
)

// TestInvokeLogging_SuccessPath_EmitsStartAndDone verifies that on the success
// path (subprocess exits 0, discovery succeeds), Invoke emits
// testrun.invoke.start and testrun.invoke.done events with the expected fields.
// testrun.invoke.error is NOT emitted on the success/exit-0 path.
func TestInvokeLogging_SuccessPath_EmitsStartAndDone(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    logger,
		},
	}

	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error on success path: %v", err)
	}

	// invoke.start must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("success path: %q event not emitted (want: logged before subprocess start)",
			domain.EventTestrunInvokeStart)
	}

	// invoke.done must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeDone) {
		t.Errorf("success path: %q event not emitted (want: logged after subprocess completes)",
			domain.EventTestrunInvokeDone)
	}

	// invoke.error must NOT be emitted on a clean success path (exit 0).
	if logger.hasEvent(domain.EventTestrunInvokeError) {
		t.Errorf("success path with exit 0: %q event must NOT be emitted (no diagnostic output on clean success)",
			domain.EventTestrunInvokeError)
	}
}

// TestInvokeLogging_SuccessPath_StartEvent_HasBinaryArgsWorkdir verifies that
// the testrun.invoke.start event carries the binary path, argument list, and
// working directory as structured fields.
func TestInvokeLogging_SuccessPath_StartEvent_HasBinaryArgsWorkdir(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    logger,
		},
	}

	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}

	// binary field must contain the executable path.
	binary, ok := logger.fieldValue(domain.EventTestrunInvokeStart, "binary")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunInvokeStart, "binary")
	} else if binary != "/fake/mosaic-run" {
		t.Errorf("%q event field binary = %q, want %q",
			domain.EventTestrunInvokeStart, binary, "/fake/mosaic-run")
	}

	// workdir field must contain the working directory.
	workdir, ok := logger.fieldValue(domain.EventTestrunInvokeStart, "workdir")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunInvokeStart, "workdir")
	} else if workdir != ws {
		t.Errorf("%q event field workdir = %q, want %q",
			domain.EventTestrunInvokeStart, workdir, ws)
	}

	// args field must be present and non-empty.
	args, ok := logger.fieldValue(domain.EventTestrunInvokeStart, "args")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunInvokeStart, "args")
	} else if args == "" {
		t.Errorf("%q event field args is empty; want arguments from buildRunArgs",
			domain.EventTestrunInvokeStart)
	}
}

// TestInvokeLogging_SuccessPath_DoneEvent_HasExitCode verifies that the
// testrun.invoke.done event carries the subprocess exit code as a decimal
// string in the exit_code field.
func TestInvokeLogging_SuccessPath_DoneEvent_HasExitCode(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    logger,
		},
	}

	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}

	exitCode, ok := logger.fieldValue(domain.EventTestrunInvokeDone, "exit_code")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunInvokeDone, "exit_code")
	} else if exitCode != "0" {
		t.Errorf("%q event field exit_code = %q, want %q (exit 0 must be %q, not empty)",
			domain.EventTestrunInvokeDone, exitCode, "0", "0")
	}
}

// TestInvokeLogging_NilLogger_NoPanic verifies that when DebugLogger is nil
// in RunInvokerOptions, Invoke completes without panic. The nil logger must
// be silently replaced by NopDebugLogger (double-guard: constructor and
// logger() accessor). This test documents the nil-safety contract.
func TestInvokeLogging_NilLogger_NoPanic(t *testing.T) {
	ws := t.TempDir()

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    nil, // explicitly nil; must not panic
		},
	}

	// This must not panic even though DebugLogger is nil.
	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke with nil DebugLogger returned unexpected error: %v\n(nil logger must not affect invocation behavior, only suppress logging)", err)
	}
}
