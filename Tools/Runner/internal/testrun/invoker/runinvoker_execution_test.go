// Tests for SubprocessRunInvoker.Invoke subprocess execution: workDir forwarding, ChildStderr and exit code mapping.
package invoker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/testrun"
)

// TestInvoke_WorkDirForwardedToCommandRunner verifies that SubprocessRunInvoker
// passes WorkingDir as the workDir argument to the CommandRunner.
func TestInvoke_WorkDirForwardedToCommandRunner(t *testing.T) {
	ws := t.TempDir()

	var capturedWorkDir string
	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		capturedWorkDir = workDir
		// Create the new Orchestration-* dir as a side effect so discovery succeeds.
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
		},
	}

	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}

	if capturedWorkDir != ws {
		t.Errorf("CommandRunner called with workDir = %q, want %q (WorkingDir must be forwarded to subprocess)",
			capturedWorkDir, ws)
	}
}

// TestInvoke_ChildStderr_PopulatedOnSuccessPath verifies that when the
// subprocess exits successfully and discovery succeeds, InvokeResult.ChildStderr
// contains the stderr output from the CommandRunner.
func TestInvoke_ChildStderr_PopulatedOnSuccessPath(t *testing.T) {
	ws := t.TempDir()

	wantStderr := []byte("subprocess warning: output was noisy\n")
	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		// Create the new Orchestration-* dir as a side effect so discovery succeeds.
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, wantStderr, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
		},
	}

	result, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}

	if string(result.ChildStderr) != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q (ChildStderr must be populated on success path)",
			result.ChildStderr, wantStderr)
	}
}

// TestInvoke_ChildStderr_PopulatedOnDiscoveryFailurePath verifies that when
// the subprocess runs but run-folder discovery fails, InvokeResult.ChildStderr
// is populated with the subprocess stderr (not empty).
func TestInvoke_ChildStderr_PopulatedOnDiscoveryFailurePath(t *testing.T) {
	ws := t.TempDir()
	// No Orchestration-* dirs -- discovery will fail.

	wantStderr := []byte("subprocess stderr during failed run\n")
	runner := &fakeCommandRunner{
		stderr:   wantStderr,
		exitCode: 1,
	}
	invoker := newInvokerWithFakeRunner(ws, runner)

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})

	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when discovery fails, got nil")
	}
	if string(result.ChildStderr) != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q (ChildStderr must be populated on discovery-failure path)",
			result.ChildStderr, wantStderr)
	}
}

// TestInvoke_ChildStderr_PopulatedOnStartFailurePath verifies that when the
// CommandRunner returns a start error (binary missing, OS error), the returned
// InvokeResult.ChildStderr reflects any stderr captured before the failure.
func TestInvoke_ChildStderr_PopulatedOnStartFailurePath(t *testing.T) {
	ws := t.TempDir()
	wantStderr := []byte("sh: mosaic-run: No such file or directory\n")
	runner := &fakeCommandRunner{
		stderr: wantStderr,
		err:    errors.New("exec: binary not found"),
	}
	invoker := newInvokerWithFakeRunner(ws, runner)

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})

	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke on start failure, got nil")
	}
	if string(result.ChildStderr) != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q (ChildStderr must be populated on start-failure path)",
			result.ChildStderr, wantStderr)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 on start-failure path (subprocess never started, design specifies ExitCode: 0)",
			result.ExitCode)
	}
}

// TestInvoke_ChildStderr_EmptyOnBinaryDiscoveryFailurePath verifies that when
// binary discovery itself fails (before the subprocess runs), ChildStderr is
// empty and ExitCode is 0.
func TestInvoke_ChildStderr_EmptyOnBinaryDiscoveryFailurePath(t *testing.T) {
	runner := &fakeCommandRunner{}
	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			WorkingDir: t.TempDir(),
			Invoke:     runner.run,
			// ExecutablePath is empty so discoverBinary() is called.
		},
		discoverBinaryFn: func() (string, error) {
			return "", errors.New("os.Executable failed: test injection")
		},
	}

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})

	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke on binary discovery failure, got nil")
	}
	if len(result.ChildStderr) != 0 {
		t.Errorf("ChildStderr = %q, want empty (no subprocess ran when binary discovery fails)",
			result.ChildStderr)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (no subprocess ran when binary discovery fails)", result.ExitCode)
	}

	// The error must carry the canonical prefix so callers can distinguish
	// binary-discovery failure from subprocess start failure.
	const wantPrefix = "testrun: cannot discover mosaic-run binary:"
	if !strings.Contains(invokeErr.Error(), wantPrefix) {
		t.Errorf("error text = %q, want it to contain %q\n(caller uses this prefix to identify binary-discovery-failure path)",
			invokeErr.Error(), wantPrefix)
	}
}

// TestInvoke_ExitCode_PopulatedOnSuccessPath verifies that InvokeResult.ExitCode
// carries the subprocess exit code when both invocation and discovery succeed.
func TestInvoke_ExitCode_PopulatedOnSuccessPath(t *testing.T) {
	ws := t.TempDir()

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		// Create the new Orchestration-* dir as a side effect so discovery succeeds.
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 5, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
		},
	}

	result, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}
	if result.ExitCode != 5 {
		t.Errorf("ExitCode = %d, want 5", result.ExitCode)
	}
}

// TestInvoke_ExitCode_PopulatedOnDiscoveryFailurePath verifies that when
// discovery fails, InvokeResult.ExitCode still carries the subprocess exit code.
func TestInvoke_ExitCode_PopulatedOnDiscoveryFailurePath(t *testing.T) {
	ws := t.TempDir()
	// No Orchestration-* dirs to trigger discovery failure.

	runner := &fakeCommandRunner{exitCode: 2}
	invoker := newInvokerWithFakeRunner(ws, runner)

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when discovery fails")
	}
	if result.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2 (must be populated from subprocess even when discovery fails)", result.ExitCode)
	}
}

// TestInvoke_ExitCode_IsZero_OnBinaryDiscoveryFailurePath verifies that when
// binary discovery fails, ExitCode is 0 (no subprocess ran).
func TestInvoke_ExitCode_IsZero_OnBinaryDiscoveryFailurePath(t *testing.T) {
	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{WorkingDir: t.TempDir()},
		discoverBinaryFn: func() (string, error) {
			return "", errors.New("os.Executable: test injection")
		},
	}

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke on binary discovery failure")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 when binary discovery fails (no subprocess ran)", result.ExitCode)
	}
}

// TestInvoke_EndToEnd_PreExistingFolderIgnored verifies that when a subprocess
// creates a new Orchestration-* folder, Invoke returns that new folder and
// ignores any pre-existing Orchestration-* folders in the workspace.
//
// The test sets up a pre-existing folder with an artificially future mtime so
// that a naive "newest by mtime" picker (without pre-existing filtering) would
// wrongly select it over the newly created folder.
func TestInvoke_EndToEnd_PreExistingFolderIgnored(t *testing.T) {
	ws := t.TempDir()

	// Create the pre-existing folder first.
	prePath := filepath.Join(ws, "Orchestration-pre-existing")
	if err := os.Mkdir(prePath, 0755); err != nil {
		t.Fatal(err)
	}

	// The new folder will be created by the fake CommandRunner side effect.
	newFolderName := "Orchestration-new-run"
	newFolderPath := filepath.Join(ws, newFolderName)

	// Give the pre-existing folder a future mtime so that an implementation
	// that ignores the pre-existing snapshot would incorrectly select it over
	// the newly created folder (which has an older mtime).
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(prePath, futureTime, futureTime); err != nil {
		t.Fatalf("setting future mtime on pre-existing folder: %v", err)
	}

	// Fake CommandRunner creates the new run folder as a side effect and then
	// returns success.
	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(newFolderPath, 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, nil, 0, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
		},
	}

	result, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error: %v", err)
	}

	// The result RunFolder must point to the newly created folder, not to the
	// pre-existing one (even though the pre-existing one has a newer mtime).
	if result.RunFolder != newFolderPath {
		t.Errorf("RunFolder = %q, want %q\n(pre-existing Orchestration-* folder must be excluded from discovery;\nthe snapshot taken before invoke should filter it out)",
			result.RunFolder, newFolderPath)
	}
}
