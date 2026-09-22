// Whitebox tests for SubprocessRunInvoker.Invoke, buildRunArgs, and the
// new-folder-only discovery helpers. These tests live in package testrun
// (not testrun_test) to access unexported symbols: buildRunArgs,
// discoverNewestOrchestrationDir, snapshotOrchestrationDirs, and
// SubprocessRunInvoker.discoverBinaryFn.
//
// Test coverage:
//
//   - SubprocessRunInvoker.Invoke: workDir forwarding, ChildStderr on all four
//     invocation paths, exit code on all paths, bounded stderr in error text on
//     failure paths, and an end-to-end case proving pre-existing Orchestration-*
//     folders are ignored.
//
//   - buildRunArgs: flag combinations with and without GHCPPermissionMode.
//
//   - snapshotOrchestrationDirs: snapshot contents, non-existent workspace,
//     mixed file/dir entries, and entries not starting with "Orchestration-".
//
//   - discoverNewestOrchestrationDir: pre-existing ignored, no-new-folder error
//     carries listing, post-invoke ReadDir failure returns typed discoveryError,
//     multiple new folders selects newest by mtime.
//
//   - SubprocessRunInvoker.Invoke logging: fake DebugLogger injection verifies
//     correct event names, fields, and sequences on all invocation paths.
//     Nil-logger defaulting to NopDebugLogger is verified for panic safety.
package testrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers shared across T1.1 tests
// ---------------------------------------------------------------------------

// fakeCommandRunner is an injectable CommandRunner that records the arguments
// it was called with and returns configured outputs.
type fakeCommandRunner struct {
	capturedWorkDir string
	capturedPath    string
	capturedArgs    []string
	stdout          []byte
	stderr          []byte
	exitCode        int
	err             error
}

func (f *fakeCommandRunner) run(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
	f.capturedWorkDir = workDir
	f.capturedPath = path
	f.capturedArgs = args
	return f.stdout, f.stderr, f.exitCode, f.err
}

// newInvokerWithFakeRunner creates a SubprocessRunInvoker with the given
// fakeCommandRunner wired in, WorkingDir set, and a fixed ExecutablePath so
// the binary-discovery step is bypassed.
func newInvokerWithFakeRunner(workDir string, runner *fakeCommandRunner) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     workDir,
			Invoke:         runner.run,
		},
	}
}

// makeOrchestrationDir creates an Orchestration-* subdirectory inside dir and
// returns its full path.
func makeOrchestrationDir(t *testing.T, dir string, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Mkdir(p, 0755); err != nil {
		t.Fatalf("makeOrchestrationDir: %v", err)
	}
	return p
}

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- workDir forwarding
// =============================================================================

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

	_, err := invoker.Invoke(context.Background(), RunInvocation{
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

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- ChildStderr on all paths
// =============================================================================

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

	result, err := invoker.Invoke(context.Background(), RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
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

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- exit code on all paths
// =============================================================================

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

	result, err := invoker.Invoke(context.Background(), RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke on binary discovery failure")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 when binary discovery fails (no subprocess ran)", result.ExitCode)
	}
}

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- bounded stderr in error text
// =============================================================================

// TestInvoke_ErrorText_IncludesBoundedStderr_OnDiscoveryFailure verifies that
// the error returned when discovery fails includes a "Child stderr" section with
// the subprocess stderr output.
func TestInvoke_ErrorText_IncludesBoundedStderr_OnDiscoveryFailure(t *testing.T) {
	ws := t.TempDir()
	// No Orchestration-* dirs to trigger discovery failure.

	stderrContent := "testrun child stderr: could not write run folder\n"
	runner := &fakeCommandRunner{
		stderr:   []byte(stderrContent),
		exitCode: 1,
	}
	invoker := newInvokerWithFakeRunner(ws, runner)

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when discovery fails")
	}

	errText := invokeErr.Error()
	if !strings.Contains(errText, "testrun: run folder discovery failed:") {
		t.Errorf("error text does not contain canonical discovery-failure prefix\nerror text: %q\nwant it to contain: %q",
			errText, "testrun: run folder discovery failed:")
	}
	if !strings.Contains(errText, "Child stderr") {
		t.Errorf("error text does not contain bounded stderr section\nerror text: %q\nwant it to contain: %q",
			errText, "Child stderr")
	}
	if !strings.Contains(errText, "could not write run folder") {
		t.Errorf("error text does not contain stderr content\nerror text: %q\nwant it to contain: %q",
			errText, "could not write run folder")
	}
}

// TestInvoke_ErrorText_IncludesBoundedStderr_OnStartFailure verifies that the
// error returned when the subprocess fails to start includes the "Child stderr"
// section when stderr is non-empty.
func TestInvoke_ErrorText_IncludesBoundedStderr_OnStartFailure(t *testing.T) {
	runner := &fakeCommandRunner{
		stderr: []byte("permission denied: /fake/mosaic-run\n"),
		err:    errors.New("fork/exec: permission denied"),
	}
	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     t.TempDir(),
			Invoke:         runner.run,
		},
	}

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on start failure")
	}

	errText := invokeErr.Error()
	if !strings.Contains(errText, "Child stderr") {
		t.Errorf("error text does not contain bounded stderr section\nerror text: %q", errText)
	}

	// The error must carry the canonical start-failure prefix so callers can
	// distinguish a subprocess that failed to start from a discovery failure.
	const wantPrefix = "testrun: subprocess invocation failed:"
	if !strings.Contains(errText, wantPrefix) {
		t.Errorf("error text = %q, want it to contain %q\n(prefix distinguishes start-failure from other invoke errors)",
			errText, wantPrefix)
	}
}

// TestInvoke_ErrorText_OmitsStderrSection_WhenStderrEmpty verifies that when
// start failure occurs and stderr is empty, the error text does NOT include
// the "Child stderr" section.
func TestInvoke_ErrorText_OmitsStderrSection_WhenStderrEmpty(t *testing.T) {
	runner := &fakeCommandRunner{
		stderr: nil, // no stderr from subprocess
		err:    errors.New("exec: executable file not found in $PATH"),
	}
	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/missing/binary",
			WorkingDir:     t.TempDir(),
			Invoke:         runner.run,
		},
	}

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on start failure")
	}

	errText := invokeErr.Error()
	if strings.Contains(errText, "Child stderr") {
		t.Errorf("error text contains 'Child stderr' section when stderr is empty; want no stderr section\nerror: %q", errText)
	}
}

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- end-to-end pre-existing folder ignored
// =============================================================================

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

	result, err := invoker.Invoke(context.Background(), RunInvocation{
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

// =============================================================================
// T1.2: buildRunArgs flag combinations
// =============================================================================

// =============================================================================
// T3.1: buildRunArgs -- executable-path forwarding
// =============================================================================

// TestBuildRunArgs_ExecutablePath_IncludedWhenSet verifies that when
// RunInvocation.ExecutablePath is non-empty, buildRunArgs appends
// "--executable-path" followed by the path as consecutive elements in the
// returned args slice.
func TestBuildRunArgs_ExecutablePath_IncludedWhenSet(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: "/usr/local/bin/claude",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--executable-path" && i+1 < len(args) && args[i+1] == "/usr/local/bin/claude" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args missing --executable-path /usr/local/bin/claude; full args: %v", args)
	}
}

// TestBuildRunArgs_ExecutablePath_OmittedWhenEmpty verifies that when
// RunInvocation.ExecutablePath is empty, --executable-path does not appear in
// the returned args slice (backwards-compatible zero value).
func TestBuildRunArgs_ExecutablePath_OmittedWhenEmpty(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: "", // empty: must not produce the flag
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if a == "--executable-path" {
			t.Errorf("args contain --executable-path when ExecutablePath is empty; full args: %v", args)
		}
	}
}

// TestBuildRunArgs_ExecutablePath_SpacesPreservedAsSingleElement verifies that
// a path containing spaces (e.g. C:\Program Files\npm\claude.cmd) is emitted as
// a single argv element, not split on whitespace. This is the critical property
// that prevents shell word-splitting from corrupting the path when the child
// process is invoked.
func TestBuildRunArgs_ExecutablePath_SpacesPreservedAsSingleElement(t *testing.T) {
	pathWithSpaces := `C:\Program Files\npm\claude.cmd`
	inv := RunInvocation{
		WorkflowID:     "smoke-single",
		Mode:           "auto",
		Harness:        "claude-code",
		FixturePath:    "/catalog/Fixtures/smoke-single",
		Task:           "Test: smoke-single / auto / claude-code",
		ExecutablePath: pathWithSpaces,
	}
	args := buildRunArgs(inv)

	// Find --executable-path and verify the immediately following element is
	// the full path value (not split on spaces).
	for i, a := range args {
		if a == "--executable-path" {
			if i+1 >= len(args) {
				t.Fatalf("--executable-path is the last element; no value follows; full args: %v", args)
			}
			if args[i+1] != pathWithSpaces {
				t.Errorf("args[%d] (value after --executable-path) = %q, want %q\n(spaces in ExecutablePath must be preserved as a single argv element)",
					i+1, args[i+1], pathWithSpaces)
			}
			return
		}
	}
	t.Errorf("--executable-path flag not found in args %v", args)
}

// TestBuildRunArgs_ExecutablePath_EmittedBeforeGHCPPermissionMode verifies the
// ordering contract: when both ExecutablePath and GHCPPermissionMode are set,
// --executable-path appears before --ghcp-permission-mode in the args slice.
func TestBuildRunArgs_ExecutablePath_EmittedBeforeGHCPPermissionMode(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-ghcp",
		Mode:               "auto",
		Harness:            "ghcp-cli",
		FixturePath:        "/catalog/Fixtures/wf-ghcp",
		Task:               "Test: wf-ghcp / auto / ghcp-cli",
		ExecutablePath:     "/usr/local/bin/copilot",
		GHCPPermissionMode: "blanket",
	}
	args := buildRunArgs(inv)

	execPathIdx := -1
	ghcpIdx := -1
	for i, a := range args {
		if a == "--executable-path" && execPathIdx == -1 {
			execPathIdx = i
		}
		if a == "--ghcp-permission-mode" && ghcpIdx == -1 {
			ghcpIdx = i
		}
	}

	if execPathIdx == -1 {
		t.Fatalf("--executable-path not found in args %v", args)
	}
	if ghcpIdx == -1 {
		t.Fatalf("--ghcp-permission-mode not found in args %v", args)
	}
	if execPathIdx >= ghcpIdx {
		t.Errorf("--executable-path (index %d) must appear before --ghcp-permission-mode (index %d); full args: %v",
			execPathIdx, ghcpIdx, args)
	}
}

// TestBuildRunArgs_AllRequiredFlagsPresent verifies that buildRunArgs always
// includes the required flags: run, --workflow, --mode, --harness, --new-run,
// --input, --task.
func TestBuildRunArgs_AllRequiredFlagsPresent(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
	}
	args := buildRunArgs(inv)

	wantPairs := []struct{ flag, value string }{
		{"--workflow", "smoke-single"},
		{"--mode", "auto"},
		{"--harness", "claude-code"},
		{"--input", "/catalog/Fixtures/smoke-single"},
		{"--task", "Test: smoke-single / auto / claude-code"},
	}
	for _, pair := range wantPairs {
		foundFlag := false
		for i, a := range args {
			if a == pair.flag && i+1 < len(args) && args[i+1] == pair.value {
				foundFlag = true
				break
			}
		}
		if !foundFlag {
			t.Errorf("args missing %s %s; full args: %v", pair.flag, pair.value, args)
		}
	}

	// First element must be the "run" subcommand.
	if len(args) == 0 || args[0] != "run" {
		t.Errorf("args[0] = %q, want \"run\"", func() string {
			if len(args) > 0 {
				return args[0]
			}
			return "(empty)"
		}())
	}

	// --new-run must be present.
	foundNewRun := false
	for _, a := range args {
		if a == "--new-run" {
			foundNewRun = true
			break
		}
	}
	if !foundNewRun {
		t.Errorf("args missing --new-run flag; full args: %v", args)
	}
}

// TestBuildRunArgs_GHCPPermissionMode_IncludedWhenSet verifies that when
// GHCPPermissionMode is non-empty, the --ghcp-permission-mode flag and value
// are appended.
func TestBuildRunArgs_GHCPPermissionMode_IncludedWhenSet(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "ghcp-cli",
		FixturePath:        "/fixtures/wf-a",
		GHCPPermissionMode: "blanket",
		Task:               "Test: wf-a / auto / ghcp-cli",
	}
	args := buildRunArgs(inv)

	foundFlag := false
	for i, a := range args {
		if a == "--ghcp-permission-mode" && i+1 < len(args) && args[i+1] == "blanket" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Errorf("args missing --ghcp-permission-mode blanket; full args: %v", args)
	}
}

// TestBuildRunArgs_GHCPPermissionMode_OmittedWhenEmpty verifies that when
// GHCPPermissionMode is empty, --ghcp-permission-mode is not present in args.
func TestBuildRunArgs_GHCPPermissionMode_OmittedWhenEmpty(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		GHCPPermissionMode: "", // empty: must not produce the flag
		Task:               "Test: wf-a / auto / auto",
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if a == "--ghcp-permission-mode" {
			t.Errorf("args contain --ghcp-permission-mode when GHCPPermissionMode is empty; full args: %v", args)
		}
	}
}

// TestBuildRunArgs_AllFlagsAndValuesPresent verifies that all expected flag-value
// pairs are present when GHCPPermissionMode is set. Order among flags after the
// leading "run" subcommand is not checked -- the design does not mandate positional
// ordering.
func TestBuildRunArgs_AllFlagsAndValuesPresent(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "orchestrated-linear",
		Mode:               "auto-review",
		Harness:            "ghcp-cli",
		FixturePath:        "/catalog/Fixtures/orchestrated-linear",
		GHCPPermissionMode: "allowlist",
		Task:               "Test: orchestrated-linear / auto-review / ghcp-cli",
	}
	args := buildRunArgs(inv)

	// Verify all required flag-value pairs are present (order may vary after
	// the positional "run").
	checks := []struct {
		flag  string
		value string
	}{
		{"--workflow", "orchestrated-linear"},
		{"--mode", "auto-review"},
		{"--harness", "ghcp-cli"},
		{"--input", "/catalog/Fixtures/orchestrated-linear"},
		{"--ghcp-permission-mode", "allowlist"},
		{"--task", "Test: orchestrated-linear / auto-review / ghcp-cli"},
	}
	for _, c := range checks {
		found := false
		for i, a := range args {
			if a == c.flag && i+1 < len(args) && args[i+1] == c.value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("buildRunArgs: missing %s %s in args %v", c.flag, c.value, args)
		}
	}
}

// TestBuildRunArgs_Task_SpacesPreservedAsSingleElement verifies that a Task
// value containing spaces is emitted as a single argv element, not split. This
// confirms the argv contract: --task and its value are adjacent elements in the
// returned slice with no shell quoting or word-splitting involved.
func TestBuildRunArgs_Task_SpacesPreservedAsSingleElement(t *testing.T) {
	taskValue := "Test: my workflow / auto / claude-code"
	inv := RunInvocation{
		WorkflowID:  "my workflow",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/fixtures/my-workflow",
		Task:        taskValue,
	}
	args := buildRunArgs(inv)

	// Find --task and verify the immediately following element is the full value.
	for i, a := range args {
		if a == "--task" {
			if i+1 >= len(args) {
				t.Fatalf("--task flag is the last element; no value follows; full args: %v", args)
			}
			if args[i+1] != taskValue {
				t.Errorf("args[%d] (value after --task) = %q, want %q\n(spaces in Task must be preserved as a single argv element, not split)",
					i+1, args[i+1], taskValue)
			}
			return
		}
	}
	t.Errorf("--task flag not found in args %v", args)
}

// =============================================================================
// T1.3: snapshotOrchestrationDirs
// =============================================================================

// TestSnapshotOrchestrationDirs_ReturnsOrchestrationDirs verifies that
// snapshotOrchestrationDirs returns only Orchestration-* directories from the
// workspace (not files, not other directories).
func TestSnapshotOrchestrationDirs_ReturnsOrchestrationDirs(t *testing.T) {
	ws := t.TempDir()

	// Create directories and files.
	orchDir1 := "Orchestration-abc123"
	orchDir2 := "Orchestration-def456"
	otherDir := "SomeOtherDir"
	orchFile := "Orchestration-not-a-dir.txt"

	for _, name := range []string{orchDir1, orchDir2, otherDir} {
		if err := os.Mkdir(filepath.Join(ws, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, orchFile), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if _, ok := snapshot[orchDir1]; !ok {
		t.Errorf("snapshot missing %q", orchDir1)
	}
	if _, ok := snapshot[orchDir2]; !ok {
		t.Errorf("snapshot missing %q", orchDir2)
	}
	if _, ok := snapshot[otherDir]; ok {
		t.Errorf("snapshot includes non-Orchestration dir %q", otherDir)
	}
	if _, ok := snapshot[orchFile]; ok {
		t.Errorf("snapshot includes file (not a directory) %q", orchFile)
	}
}

// TestSnapshotOrchestrationDirs_EmptyWorkspace_ReturnsEmptyMap verifies that
// when the workspace contains no Orchestration-* dirs, an empty (non-nil) map
// is returned.
func TestSnapshotOrchestrationDirs_EmptyWorkspace_ReturnsEmptyMap(t *testing.T) {
	ws := t.TempDir()
	// Create a non-Orchestration dir to ensure the workspace is not empty.
	if err := os.Mkdir(filepath.Join(ws, "SomeOtherDir"), 0755); err != nil {
		t.Fatal(err)
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if snapshot == nil {
		t.Error("snapshotOrchestrationDirs returned nil, want non-nil empty map")
	}
	if len(snapshot) != 0 {
		t.Errorf("snapshot len = %d, want 0; snapshot = %v", len(snapshot), snapshot)
	}
}

// TestSnapshotOrchestrationDirs_NonExistentWorkspace_ReturnsEmptyMap verifies
// that when the workspace does not exist, snapshotOrchestrationDirs returns a
// non-nil empty map (no error is returned; the pre-invoke snapshot failure is
// silent and non-fatal).
func TestSnapshotOrchestrationDirs_NonExistentWorkspace_ReturnsEmptyMap(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")

	snapshot := snapshotOrchestrationDirs(nonExistent)

	if snapshot == nil {
		t.Error("snapshotOrchestrationDirs returned nil for non-existent workspace, want non-nil empty map")
	}
	if len(snapshot) != 0 {
		t.Errorf("snapshot len = %d, want 0 for non-existent workspace", len(snapshot))
	}
}

// TestSnapshotOrchestrationDirs_OnlyOrchestrationPrefixDirectories verifies
// that directories named "Orchestration-" exactly (zero suffix) and directories
// named differently are treated correctly.
func TestSnapshotOrchestrationDirs_OnlyOrchestrationPrefixDirectories(t *testing.T) {
	ws := t.TempDir()

	// Has the prefix and is a directory.
	orchWithSuffix := "Orchestration-run-abc"
	// Exact prefix only (no suffix after the dash).
	orchExact := "Orchestration-"
	// Does NOT have the prefix.
	notOrch := "NotOrchestration-abc"

	for _, name := range []string{orchWithSuffix, orchExact, notOrch} {
		if err := os.Mkdir(filepath.Join(ws, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := snapshotOrchestrationDirs(ws)

	if _, ok := snapshot[orchWithSuffix]; !ok {
		t.Errorf("snapshot missing %q", orchWithSuffix)
	}
	if _, ok := snapshot[orchExact]; !ok {
		t.Errorf("snapshot missing %q (dir named exactly 'Orchestration-' is valid)", orchExact)
	}
	if _, ok := snapshot[notOrch]; ok {
		t.Errorf("snapshot includes %q which does not start with 'Orchestration-'", notOrch)
	}
}

// =============================================================================
// T1.3: discoverNewestOrchestrationDir -- pre-existing dirs ignored
// =============================================================================

// TestDiscoverNewestOrchestrationDir_IgnoresPreExistingDirs verifies that
// directories in the preExisting set are excluded from consideration even when
// they have a newer mtime than any new directory.
func TestDiscoverNewestOrchestrationDir_IgnoresPreExistingDirs(t *testing.T) {
	ws := t.TempDir()

	// Create "new" folder first so it will have an older mtime.
	newPath := filepath.Join(ws, "Orchestration-new-run")
	if err := os.Mkdir(newPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create pre-existing folder.
	preExistingName := "Orchestration-old-run"
	prePath := filepath.Join(ws, preExistingName)
	if err := os.Mkdir(prePath, 0755); err != nil {
		t.Fatal(err)
	}

	// Give the pre-existing folder a future mtime to ensure a naive
	// "newest by mtime" approach (without filtering) would select it.
	futureTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(prePath, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}

	// Build the preExisting snapshot containing the old-run folder.
	preExisting := map[string]struct{}{
		preExistingName: {},
	}

	got, err := discoverNewestOrchestrationDir(ws, preExisting)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	if got != newPath {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q\n(pre-existing dir %q must be ignored even though it has a newer mtime)",
			got, newPath, preExistingName)
	}
}

// =============================================================================
// T1.3: discoverNewestOrchestrationDir -- no new folder includes listing
// =============================================================================

// TestDiscoverNewestOrchestrationDir_NoNewFolder_ErrorHasListing verifies that
// when no new Orchestration-* directories appear (all are pre-existing), the
// returned error is a *discoveryError with Kind == discoveryNoNewFolder and
// Listing populated with the workspace directory names.
func TestDiscoverNewestOrchestrationDir_NoNewFolder_ErrorHasListing(t *testing.T) {
	ws := t.TempDir()

	// Create an Orchestration dir that will be marked as pre-existing.
	existingName := "Orchestration-already-exists"
	if err := os.Mkdir(filepath.Join(ws, existingName), 0755); err != nil {
		t.Fatal(err)
	}
	// Also create a non-matching file to verify listing completeness.
	if err := os.WriteFile(filepath.Join(ws, "some-file.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// All Orchestration-* dirs are pre-existing.
	preExisting := map[string]struct{}{existingName: {}}

	_, err := discoverNewestOrchestrationDir(ws, preExisting)
	if err == nil {
		t.Fatal("expected error when all Orchestration-* dirs are pre-existing, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}

	// Workspace field must record the path that was scanned.
	if de.Workspace != ws {
		t.Errorf("discoveryError.Workspace = %q, want %q", de.Workspace, ws)
	}

	// Cause must be non-nil: the no-new-folder path sets a descriptive error.
	if de.Cause == nil {
		t.Errorf("discoveryError.Cause is nil on no-new-folder path, want descriptive error")
	}

	if len(de.Listing) == 0 {
		t.Errorf("discoveryError.Listing is empty, want workspace entries listed")
	}

	// The listing must contain the pre-existing dir name.
	foundExisting := false
	for _, name := range de.Listing {
		if name == existingName {
			foundExisting = true
			break
		}
	}
	if !foundExisting {
		t.Errorf("discoveryError.Listing = %v, does not contain %q", de.Listing, existingName)
	}

	// The listing must also contain the non-Orchestration file. Listing must
	// include ALL workspace entries, not only Orchestration-* dirs -- the
	// testrun.discovery.fail message body is strings.Join(e.Listing, "\n") and
	// a partial listing misleads the operator.
	foundFile := false
	for _, name := range de.Listing {
		if name == "some-file.txt" {
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Errorf("discoveryError.Listing = %v, does not contain %q (Listing must include all workspace entries, not only Orchestration-* dirs)",
			de.Listing, "some-file.txt")
	}
}

// TestDiscoverNewestOrchestrationDir_NoNewFolder_EmptyWorkspace_ErrorHasEmptyListing
// verifies that when the workspace exists but is empty, the error is a
// *discoveryError with Kind == discoveryNoNewFolder and an empty Listing.
func TestDiscoverNewestOrchestrationDir_NoNewFolder_EmptyWorkspace_ErrorHasEmptyListing(t *testing.T) {
	ws := t.TempDir() // empty workspace

	_, err := discoverNewestOrchestrationDir(ws, nil)
	if err == nil {
		t.Fatal("expected error for empty workspace, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}
}

// =============================================================================
// T1.3: discoverNewestOrchestrationDir -- post-invoke ReadDir failure
// =============================================================================

// TestDiscoverNewestOrchestrationDir_ReadDirFailure_ReturnsInfraError verifies
// that when the workspace directory itself cannot be read (e.g., removed after
// the subprocess ran), the returned error is a *discoveryError with Kind ==
// discoveryReadDirFailed. This is distinct from "no new folder" so callers can
// emit the appropriate diagnostic.
func TestDiscoverNewestOrchestrationDir_ReadDirFailure_ReturnsInfraError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "workspace-was-removed")

	_, err := discoverNewestOrchestrationDir(nonExistent, nil)
	if err == nil {
		t.Fatal("expected error for non-existent workspace, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v\n(post-invoke ReadDir failure must return a typed *discoveryError)", err, err)
	}

	if de.Kind != discoveryReadDirFailed {
		t.Errorf("discoveryError.Kind = %v, want discoveryReadDirFailed\n(ReadDir failure must be distinct from no-new-folder)", de.Kind)
	}

	// Workspace field must record the path that was scanned.
	if de.Workspace != nonExistent {
		t.Errorf("discoveryError.Workspace = %q, want %q", de.Workspace, nonExistent)
	}

	if de.Cause == nil {
		t.Errorf("discoveryError.Cause is nil, want the underlying ReadDir error")
	}
}

// TestDiscoverNewestOrchestrationDir_LargeWorkspace_ListingTruncatedAt200
// verifies that when the workspace contains more than maxListingEntries (200)
// entries, discoveryError.Listing is capped at 200 entries and Truncated
// carries the overflow count.
func TestDiscoverNewestOrchestrationDir_LargeWorkspace_ListingTruncatedAt200(t *testing.T) {
	ws := t.TempDir()

	// Create 201 files (no Orchestration-* dirs) so that discovery returns a
	// discoveryNoNewFolder error containing all 201 workspace entries in its
	// listing -- which must then be capped at 200 with Truncated == 1.
	for i := 0; i < 201; i++ {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(ws, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := discoverNewestOrchestrationDir(ws, nil)
	if err == nil {
		t.Fatal("expected error for workspace with no Orchestration-* dirs, got nil")
	}

	var de *discoveryError
	if !errors.As(err, &de) {
		t.Fatalf("error is not *discoveryError: %T: %v", err, err)
	}

	if de.Kind != discoveryNoNewFolder {
		t.Errorf("discoveryError.Kind = %v, want discoveryNoNewFolder", de.Kind)
	}

	if len(de.Listing) != 200 {
		t.Errorf("discoveryError.Listing len = %d, want 200 (must be capped at maxListingEntries)",
			len(de.Listing))
	}

	if de.Truncated != 1 {
		t.Errorf("discoveryError.Truncated = %d, want 1 (201 entries - 200 cap = 1 overflow)",
			de.Truncated)
	}
}

// =============================================================================
// T1.3: discoverNewestOrchestrationDir -- multiple new folders, newest selected
// =============================================================================

// TestDiscoverNewestOrchestrationDir_MultipleNewFolders_NewestSelected verifies
// that when multiple new Orchestration-* directories appear, the one with the
// most recent mtime is returned.
func TestDiscoverNewestOrchestrationDir_MultipleNewFolders_NewestSelected(t *testing.T) {
	ws := t.TempDir()

	// Create three new Orchestration-* dirs with distinct, controlled mtimes.
	older := filepath.Join(ws, "Orchestration-older")
	middle := filepath.Join(ws, "Orchestration-middle")
	newest := filepath.Join(ws, "Orchestration-newest")

	for _, p := range []string{older, middle, newest} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	if err := os.Chtimes(older, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(middle, now.Add(-1*time.Minute), now.Add(-1*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newest, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	if got != newest {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q (must select newest by mtime)", got, newest)
	}
}

// TestDiscoverNewestOrchestrationDir_EqualMtimes_LexicographicTieBreak verifies
// that when two new Orchestration-* directories have identical mtimes, the one
// that appears first in lexicographic (ReadDir) order wins.
func TestDiscoverNewestOrchestrationDir_EqualMtimes_LexicographicTieBreak(t *testing.T) {
	ws := t.TempDir()

	// "aaa" sorts before "zzz" in ReadDir lexicographic order.
	pathAAA := filepath.Join(ws, "Orchestration-aaa")
	pathZZZ := filepath.Join(ws, "Orchestration-zzz")

	for _, p := range []string{pathAAA, pathZZZ} {
		if err := os.Mkdir(p, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Set both directories to the exact same mtime.
	sameTime := time.Now().Add(-30 * time.Second)
	for _, p := range []string{pathAAA, pathZZZ} {
		if err := os.Chtimes(p, sameTime, sameTime); err != nil {
			t.Fatal(err)
		}
	}

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}

	// With equal mtimes the first in ReadDir order (Orchestration-aaa) must win.
	if got != pathAAA {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q\n(equal-mtime tie-break must select first in lexicographic/ReadDir order)",
			got, pathAAA)
	}
}

// TestDiscoverNewestOrchestrationDir_SingleNewFolder_Returned verifies the
// basic case: exactly one Orchestration-* dir exists and is returned.
func TestDiscoverNewestOrchestrationDir_SingleNewFolder_Returned(t *testing.T) {
	ws := t.TempDir()
	want := makeOrchestrationDir(t, ws, "Orchestration-only-one")

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error: %v", err)
	}
	if got != want {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q", got, want)
	}
}

// TestDiscoverNewestOrchestrationDir_PreExisting_NilTreatedAsEmpty verifies
// that passing nil as the preExisting set is treated as an empty set (no
// directories are excluded by default).
func TestDiscoverNewestOrchestrationDir_PreExisting_NilTreatedAsEmpty(t *testing.T) {
	ws := t.TempDir()
	want := makeOrchestrationDir(t, ws, "Orchestration-run-1")

	got, err := discoverNewestOrchestrationDir(ws, nil)
	if err != nil {
		t.Fatalf("discoverNewestOrchestrationDir returned error with nil preExisting: %v", err)
	}
	if got != want {
		t.Errorf("discoverNewestOrchestrationDir = %q, want %q", got, want)
	}
}

// =============================================================================
// errWithStderr: direct unit tests for the helper algorithm
// =============================================================================

// TestErrWithStderr_NilStderr_ReturnBaseErrUnchanged verifies that when stderr
// is nil, errWithStderr returns the base error unchanged (no stderr section
// appended).
func TestErrWithStderr_NilStderr_ReturnBaseErrUnchanged(t *testing.T) {
	baseErr := errors.New("base error text")
	got := errWithStderr(baseErr, nil)
	if got != baseErr {
		t.Errorf("errWithStderr(baseErr, nil) = %v, want identical baseErr (no stderr section for nil input)",
			got)
	}
}

// TestErrWithStderr_EmptyStderr_ReturnBaseErrUnchanged verifies that when stderr
// is an empty (zero-length) byte slice, errWithStderr returns the base error
// unchanged.
func TestErrWithStderr_EmptyStderr_ReturnBaseErrUnchanged(t *testing.T) {
	baseErr := errors.New("base error text")
	got := errWithStderr(baseErr, []byte{})
	if got != baseErr {
		t.Errorf("errWithStderr(baseErr, []byte{}) = %v, want identical baseErr (no stderr section for empty input)",
			got)
	}
}

// TestErrWithStderr_UnderLimit_AppendsFullContentTrailingNewlineStripped verifies
// that when stderr is under MaxStderrErrorBytes, the full content is appended and
// a trailing newline is stripped. The N count in the header reflects the
// post-strip length.
func TestErrWithStderr_UnderLimit_AppendsFullContentTrailingNewlineStripped(t *testing.T) {
	baseErr := errors.New("subprocess failed")
	// 18 bytes: "line1\nline2\nline3\n" -- under the 1024-byte limit.
	// After trailing-newline strip: "line1\nline2\nline3" (17 bytes).
	stderr := []byte("line1\nline2\nline3\n")
	got := errWithStderr(baseErr, stderr)

	errText := got.Error()

	if !strings.Contains(errText, "subprocess failed") {
		t.Errorf("error text %q does not contain base error message", errText)
	}
	if !strings.Contains(errText, "Child stderr (last 17 bytes):") {
		t.Errorf("error text %q does not contain expected header 'Child stderr (last 17 bytes):'", errText)
	}
	if !strings.Contains(errText, "line1\nline2\nline3") {
		t.Errorf("error text %q does not contain expected stderr content 'line1\\nline2\\nline3'", errText)
	}
	// The trailing newline must be stripped -- the error text must not end with "\n".
	if strings.HasSuffix(errText, "\n") {
		t.Errorf("error text ends with newline, want trailing newline stripped: %q", errText)
	}
}

// TestErrWithStderr_OverLimit_MultipleLines_PartialFirstLineDropped verifies
// the over-limit multi-line case: the 1024-byte tail begins mid-line, so the
// partial first line is dropped and the excerpt starts at the next complete line.
func TestErrWithStderr_OverLimit_MultipleLines_PartialFirstLineDropped(t *testing.T) {
	baseErr := errors.New("discovery failed")
	// Construct stderr that is slightly over MaxStderrErrorBytes (1024).
	// Layout: "partial\n" (8 bytes) + 1020 'B's = 1028 bytes total.
	// The 1024-byte tail starts at offset 4, capturing "ial\n" + 1020 'B's.
	// First newline in the tail is at index 3 ("ial\n"). Candidate is tail[4:]
	// = 1020 'B's. No trailing newline, so N = 1020.
	prefix := "partial\n"
	body := strings.Repeat("B", 1020)
	stderr := []byte(prefix + body)

	got := errWithStderr(baseErr, stderr)
	errText := got.Error()

	// The excerpt must contain the complete body of B's.
	if !strings.Contains(errText, body) {
		t.Errorf("error text does not contain the complete B-line body (partial first line must be dropped, leaving complete second line)")
	}
	// The partial fragment "ial" from the cut line must not appear as leading content.
	// (It may appear inside the body but should not precede the line-boundary cut.)
	// Verify by checking the header byte count matches the body length.
	wantHeader := fmt.Sprintf("Child stderr (last %d bytes):", len(body))
	if !strings.Contains(errText, wantHeader) {
		t.Errorf("error text %q does not contain expected header %q", errText, wantHeader)
	}
}

// TestErrWithStderr_OverLimit_SingleLongLine_FullTailUsed verifies that when
// stderr exceeds MaxStderrErrorBytes and contains no newlines (a single long
// line), the full 1024-byte tail is used without any line-boundary cut.
func TestErrWithStderr_OverLimit_SingleLongLine_FullTailUsed(t *testing.T) {
	baseErr := errors.New("subprocess start failed")
	// 2000 'X' bytes: no newlines anywhere.
	stderr := []byte(strings.Repeat("X", 2000))

	got := errWithStderr(baseErr, stderr)
	errText := got.Error()

	wantHeader := fmt.Sprintf("Child stderr (last %d bytes):", MaxStderrErrorBytes)
	if !strings.Contains(errText, wantHeader) {
		t.Errorf("error text %q does not contain expected header %q\n(single long line must use full %d-byte tail)",
			errText, wantHeader, MaxStderrErrorBytes)
	}
	// The tail must be the last MaxStderrErrorBytes bytes of the input.
	wantTail := string(stderr[len(stderr)-MaxStderrErrorBytes:])
	if !strings.Contains(errText, wantTail) {
		t.Errorf("error text does not contain the expected %d-byte tail content", MaxStderrErrorBytes)
	}
}

// TestErrWithStderr_AllWhitespace_SectionEmittedWithNZero verifies that
// all-whitespace stderr (e.g., "\n") is treated as non-empty. A stderr section
// is emitted with N=0 (the single newline is stripped, leaving zero bytes of
// content).
func TestErrWithStderr_AllWhitespace_SectionEmittedWithNZero(t *testing.T) {
	baseErr := errors.New("subprocess failed")
	// Single newline: 1 byte, under limit. After strip: 0 bytes. N = 0.
	stderr := []byte("\n")

	got := errWithStderr(baseErr, stderr)
	errText := got.Error()

	// The base error must appear.
	if !strings.Contains(errText, "subprocess failed") {
		t.Errorf("error text %q does not contain base error message", errText)
	}
	// A stderr section must be present (all-whitespace stderr is NOT suppressed).
	if !strings.Contains(errText, "Child stderr (last 0 bytes):") {
		t.Errorf("error text %q does not contain 'Child stderr (last 0 bytes):'\n(all-whitespace stderr is non-empty; section must be emitted with N=0)",
			errText)
	}
}

// TestErrWithStderr_OverLimit_EmptyCandidateAfterLineCut_FallsBackToFullTail
// verifies algorithm step 3d: when the line-boundary cut leaves a candidate that
// is empty after its trailing-newline strip, the algorithm falls back to using the
// full tail (step 3e) rather than emitting an empty excerpt.
//
// Stderr is arranged so that in the 1024-byte tail:
//   - indices 0-1021: 1022 bytes of 'A' (no newlines)
//   - index 1022: '\n'  (only non-trailing newline; inside search range tail[:1023])
//   - index 1023: '\n'  (trailing newline; excluded from search by step 3b)
//
// Step 3c finds '\n' at index 1022; candidate = tail[1023:] = "\n".
// Step 3d strips the trailing '\n': candidate becomes "" (empty) -> fallback.
// Step 3e uses the full 1024-byte tail; step 5 strips its trailing '\n'.
// N = 1023 (confirming fallback, not a partial-line drop).
func TestErrWithStderr_OverLimit_EmptyCandidateAfterLineCut_FallsBackToFullTail(t *testing.T) {
	baseErr := errors.New("subprocess failed")
	// Layout: 6 prefix bytes + 1022 'A' bytes + "\n\n" = 1030 bytes total.
	// The 1024-byte tail is: 1022 'A's + "\n\n" (indices 0-1021 = 'A', 1022 = '\n', 1023 = '\n').
	prefix := "XXXXXX"                    // 6 bytes (pushes total over 1024)
	content := strings.Repeat("A", 1022) // 1022 bytes, no newlines
	stderr := []byte(prefix + content + "\n\n")

	got := errWithStderr(baseErr, stderr)
	errText := got.Error()

	// Fallback to full tail (1024 bytes), trailing '\n' stripped in step 5 -> N = 1023.
	// A line-boundary-cut implementation without the fallback would produce N = 0
	// (only the "\n" candidate, then stripped to ""), which would not match 1023.
	wantHeader := "Child stderr (last 1023 bytes):"
	if !strings.Contains(errText, wantHeader) {
		t.Errorf("error text %q does not contain expected header %q\n(empty-candidate after line-boundary cut must fall back to full tail; N must be 1023, not 0 or 1)",
			errText, wantHeader)
	}
}

// =============================================================================
// T1.1: SubprocessRunInvoker.Invoke -- Path 5 (post-invoke ReadDir failure)
// =============================================================================

// TestInvoke_ChildStderr_PopulatedOnPath5_ReadDirFailure verifies that when the
// subprocess runs but the workspace directory is removed before the post-invoke
// ReadDir (Path 5: discoveryReadDirFailed), InvokeResult.ChildStderr is still
// populated with the subprocess stderr.
//
// The workspace is a subdirectory created within the test temp dir. The fake
// CommandRunner removes it as a side effect. Using a subdirectory (not the
// t.TempDir() root itself) ensures os.RemoveAll succeeds on all platforms, so
// discoverNewestOrchestrationDir genuinely encounters a ReadDir failure (Path 5)
// rather than an empty-directory result (Path 4).
func TestInvoke_ChildStderr_PopulatedOnPath5_ReadDirFailure(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	if err := os.Mkdir(ws, 0755); err != nil {
		t.Fatal(err)
	}

	wantStderr := []byte("subprocess stderr before workspace was removed\n")

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		// Remove ws (captured from outer scope) to simulate the workspace being
		// deleted while the subprocess runs. We use the captured ws rather than
		// the workDir parameter because the current stub passes "" as workDir
		// (WorkingDir forwarding is not yet implemented). The workspace removal
		// ensures the post-invoke discoverNewestOrchestrationDir ReadDir fails.
		if err := os.RemoveAll(ws); err != nil {
			return nil, nil, 0, fmt.Errorf("RemoveAll in fake runner: %w", err)
		}
		return nil, wantStderr, 1, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
		},
	}

	result, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})

	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when post-invoke ReadDir fails (Path 5), got nil")
	}
	if string(result.ChildStderr) != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q\n(ChildStderr must be populated on Path 5 post-invoke ReadDir failure; design requires InvokeResult to carry subprocess stderr on all error paths)",
			result.ChildStderr, wantStderr)
	}
}

// TestInvoke_ErrorText_IncludesBoundedStderr_OnPath5_ReadDirFailure verifies
// that the error returned when the post-invoke ReadDir fails (Path 5) includes a
// "Child stderr" section with the subprocess stderr output. This is the same
// bounded-stderr-in-error-text requirement as Path 4 (no-new-folder), since the
// child process DID run and produced stderr on both paths.
//
// See TestInvoke_ChildStderr_PopulatedOnPath5_ReadDirFailure for why the workspace
// is a subdirectory (not the t.TempDir() root) to ensure a genuine ReadDir failure.
func TestInvoke_ErrorText_IncludesBoundedStderr_OnPath5_ReadDirFailure(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	if err := os.Mkdir(ws, 0755); err != nil {
		t.Fatal(err)
	}

	stderrContent := "testrun child stderr: workspace removed mid-run\n"

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		// Remove ws (captured from outer scope) -- see TestInvoke_ChildStderr_
		// PopulatedOnPath5_ReadDirFailure for the rationale of using the captured
		// variable rather than the workDir parameter.
		if err := os.RemoveAll(ws); err != nil {
			return nil, nil, 0, fmt.Errorf("RemoveAll in fake runner: %w", err)
		}
		return nil, []byte(stderrContent), 1, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
		},
	}

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when post-invoke ReadDir fails (Path 5)")
	}

	errText := invokeErr.Error()
	if !strings.Contains(errText, "Child stderr") {
		t.Errorf("error text does not contain bounded stderr section\nerror text: %q\nwant it to contain: %q\n(Path 5 must include bounded stderr in error text, same as Path 4)",
			errText, "Child stderr")
	}
	if !strings.Contains(errText, "workspace removed mid-run") {
		t.Errorf("error text does not contain stderr content\nerror text: %q\nwant it to contain: %q",
			errText, "workspace removed mid-run")
	}
}

// =============================================================================
// T2.1: SubprocessRunInvoker.Invoke -- logging behavior (fake DebugLogger)
// =============================================================================

// fakeDebugLogger records Log calls so tests can assert on the events, messages,
// and fields emitted by SubprocessRunInvoker.Invoke.
type fakeDebugLogger struct {
	entries []fakeLogEntry
}

type fakeLogEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

// Log implements domain.DebugLogger. It records every call.
func (f *fakeDebugLogger) Log(event string, message string, fields ...domain.DebugField) {
	f.entries = append(f.entries, fakeLogEntry{
		event:   event,
		message: message,
		fields:  append([]domain.DebugField(nil), fields...),
	})
}

// hasEvent reports whether the fake received a Log call with the given event name.
func (f *fakeDebugLogger) hasEvent(event string) bool {
	for _, e := range f.entries {
		if e.event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for key in the first entry matching event.
// Returns ("", false) when no matching entry or no matching field is found.
func (f *fakeDebugLogger) fieldValue(event string, key string) (string, bool) {
	for _, e := range f.entries {
		if e.event != event {
			continue
		}
		for _, fld := range e.fields {
			if fld.Key == key {
				return fld.Value, true
			}
		}
	}
	return "", false
}

// messageFor returns the message of the first entry matching event.
// Returns ("", false) when no matching entry is found.
func (f *fakeDebugLogger) messageFor(event string) (string, bool) {
	for _, e := range f.entries {
		if e.event == event {
			return e.message, true
		}
	}
	return "", false
}

// newInvokerWithLogger creates a SubprocessRunInvoker with the given
// DebugLogger and fakeCommandRunner wired in.
func newInvokerWithLogger(workDir string, runner *fakeCommandRunner, logger domain.DebugLogger) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     workDir,
			Invoke:         runner.run,
			DebugLogger:    logger,
		},
	}
}

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

	_, err := invoker.Invoke(context.Background(), RunInvocation{
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

	_, err := invoker.Invoke(context.Background(), RunInvocation{
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

	_, err := invoker.Invoke(context.Background(), RunInvocation{
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

// TestInvokeLogging_NonZeroExit_EmitsInvokeError verifies that when the
// subprocess exits with a non-zero code but discovery succeeds (Path 2),
// testrun.invoke.error is emitted with the exit_code field set.
// Invoke returns nil error on this path (non-zero exit with successful
// discovery is not an infrastructure error).
func TestInvokeLogging_NonZeroExit_EmitsInvokeError(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}
	wantExitCode := 42

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		if err := os.Mkdir(filepath.Join(ws, "Orchestration-run-1"), 0755); err != nil {
			return nil, nil, 0, err
		}
		return nil, []byte("some stderr output\n"), wantExitCode, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    logger,
		},
	}

	// Path 2: non-zero exit with successful discovery returns (result, nil).
	_, err := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke returned unexpected error on non-zero exit / success-discovery path: %v", err)
	}

	// invoke.start must be emitted on Path 2 (subprocess ran; binary was known).
	if !logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("non-zero exit path: %q event not emitted (want: logged before subprocess start on all paths where binary is known)",
			domain.EventTestrunInvokeStart)
	}

	// invoke.done must be emitted on Path 2 (subprocess ran to completion).
	if !logger.hasEvent(domain.EventTestrunInvokeDone) {
		t.Errorf("non-zero exit path: %q event not emitted (want: logged after subprocess completes on Path 2)",
			domain.EventTestrunInvokeDone)
	}

	// invoke.error must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeError) {
		t.Errorf("non-zero exit path: %q event not emitted (want: logged for diagnostic traceability)",
			domain.EventTestrunInvokeError)
	}

	// exit_code field must reflect the actual exit code.
	exitCode, ok := logger.fieldValue(domain.EventTestrunInvokeError, "exit_code")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunInvokeError, "exit_code")
	} else if exitCode != strconv.Itoa(wantExitCode) {
		t.Errorf("%q event field exit_code = %q, want %q",
			domain.EventTestrunInvokeError, exitCode, strconv.Itoa(wantExitCode))
	}

	// The invoke.error event message must carry the child stderr on Path 2.
	msg, _ := logger.messageFor(domain.EventTestrunInvokeError)
	if !strings.Contains(msg, "some stderr output") {
		t.Errorf("%q event message does not contain child stderr content\nmessage: %q\n(Path 2 invoke.error message must be the full child stderr)",
			domain.EventTestrunInvokeError, msg)
	}
}

// TestInvokeLogging_StartFailure_EmitsInvokeError_WithErrorField verifies
// Path 3 (subprocess fails to start): testrun.invoke.start is emitted (logged
// before the exec attempt), then testrun.invoke.error is emitted with the
// error field carrying the OS error text. testrun.invoke.done is NOT emitted.
func TestInvokeLogging_StartFailure_EmitsInvokeError_WithErrorField(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}
	osErr := errors.New("exec: permission denied: /fake/mosaic-run")

	runner := &fakeCommandRunner{err: osErr}
	invoker := newInvokerWithLogger(ws, runner, logger)

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on start failure, got nil")
	}

	// invoke.start must be emitted (logged before exec attempt, binary known).
	if !logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("start-failure path: %q event not emitted\n(invoke.start is logged before exec attempt; binary path IS known even when exec fails)",
			domain.EventTestrunInvokeStart)
	}

	// invoke.done must NOT be emitted (subprocess did not complete).
	if logger.hasEvent(domain.EventTestrunInvokeDone) {
		t.Errorf("start-failure path: %q event must NOT be emitted\n(subprocess did not start; done event requires subprocess completion)",
			domain.EventTestrunInvokeDone)
	}

	// invoke.error must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeError) {
		t.Errorf("start-failure path: %q event not emitted (want: OS/infrastructure error logged)",
			domain.EventTestrunInvokeError)
	}

	// error field must carry the raw OS error text (single-line per contract).
	errField, ok := logger.fieldValue(domain.EventTestrunInvokeError, "error")
	if !ok {
		t.Errorf("%q event missing %q field on start-failure path\n(error field required on Paths 3 and 6 for structured consumers)",
			domain.EventTestrunInvokeError, "error")
	} else if errField == "" {
		t.Errorf("%q event field error is empty, want the OS error text",
			domain.EventTestrunInvokeError)
	} else if strings.Contains(errField, "\n") {
		// The error field must be single-line. If the raw error text contains
		// newlines they must be replaced with " | " before storing in the field.
		t.Errorf("%q event field error contains embedded newline; want single-line value\n"+
			"(embedded newlines must be replaced with \" | \" per the event field contract)\nerror field: %q",
			domain.EventTestrunInvokeError, errField)
	}

	// exit_code field must be "0" (subprocess never started).
	exitCode, ok := logger.fieldValue(domain.EventTestrunInvokeError, "exit_code")
	if !ok {
		t.Errorf("%q event missing %q field on start-failure path", domain.EventTestrunInvokeError, "exit_code")
	} else if exitCode != "0" {
		t.Errorf("%q event field exit_code = %q, want %q (subprocess never ran; exit code must be 0)",
			domain.EventTestrunInvokeError, exitCode, "0")
	}
}

// TestInvokeLogging_DiscoveryFailure_EmitsDiscoveryFail verifies Path 4
// (subprocess ran but no new folder found): all four events are emitted in
// order -- invoke.start, invoke.done, invoke.error (stderr non-empty), and
// testrun.discovery.fail. The discovery.fail event carries the workspace
// field and a message containing the directory listing (AC2.3: the log must
// contain the workspace directory listing on discovery failure).
func TestInvokeLogging_DiscoveryFailure_EmitsDiscoveryFail(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}
	// No Orchestration-* dirs -- discovery will fail (Path 4).
	// Add a non-matching file so the workspace directory listing is non-empty
	// and the message assertion below is meaningful (non-empty listing).
	if err := os.WriteFile(filepath.Join(ws, "some-file.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeCommandRunner{
		stderr:   []byte("child stderr on discovery failure\n"),
		exitCode: 1,
	}
	invoker := newInvokerWithLogger(ws, runner, logger)

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on discovery failure, got nil")
	}

	// invoke.start must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("discovery-failure path: %q event not emitted", domain.EventTestrunInvokeStart)
	}

	// invoke.done must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeDone) {
		t.Errorf("discovery-failure path: %q event not emitted", domain.EventTestrunInvokeDone)
	}

	// discovery.fail must be emitted.
	if !logger.hasEvent(domain.EventTestrunDiscoveryFail) {
		t.Errorf("discovery-failure path: %q event not emitted\n(want: workspace listing logged so operator can see what IS in the directory)",
			domain.EventTestrunDiscoveryFail)
	}

	// discovery.fail must carry the workspace field.
	workspace, ok := logger.fieldValue(domain.EventTestrunDiscoveryFail, "workspace")
	if !ok {
		t.Errorf("%q event missing %q field", domain.EventTestrunDiscoveryFail, "workspace")
	} else if workspace != ws {
		t.Errorf("%q event field workspace = %q, want %q",
			domain.EventTestrunDiscoveryFail, workspace, ws)
	}

	// discovery.fail message must be the workspace directory listing
	// (strings.Join(discoveryError.Listing, "\n") per the design contract).
	// AC2.3: the log must contain the workspace directory listing on discovery
	// failure. Assert non-empty and that it contains the known workspace entry.
	msg, hasMsgFail := logger.messageFor(domain.EventTestrunDiscoveryFail)
	if !hasMsgFail {
		t.Errorf("%q event not found (already asserted above -- this branch is defensive)",
			domain.EventTestrunDiscoveryFail)
	} else if strings.TrimSpace(msg) == "" {
		t.Errorf("%q event message is empty; want workspace directory listing\n"+
			"(AC2.3: the log must contain the workspace directory listing on discovery failure;\n"+
			"message must be strings.Join(discoveryError.Listing, \"\\n\"))",
			domain.EventTestrunDiscoveryFail)
	} else if !strings.Contains(msg, "some-file.txt") {
		t.Errorf("%q event message = %q; want it to contain workspace entry %q\n"+
			"(message must include all workspace entries so the operator can see what IS in the directory)",
			domain.EventTestrunDiscoveryFail, msg, "some-file.txt")
	}
}

// TestInvokeLogging_DiscoveryFailure_ExitZeroEmptyStderr_NoInvokeError verifies
// that on Path 4 (subprocess ran but no new folder found), when stderr is empty
// AND exit code is 0, testrun.invoke.error is NOT emitted. Per the design
// contract, invoke.error is only emitted when "stderr is non-empty OR
// exit_code != 0". An implementation that always emits invoke.error on Path 4
// would pass TestInvokeLogging_DiscoveryFailure_EmitsDiscoveryFail (which uses
// non-empty stderr and non-zero exit) but fail here.
func TestInvokeLogging_DiscoveryFailure_ExitZeroEmptyStderr_NoInvokeError(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}
	// No Orchestration-* dirs -- discovery will fail (Path 4).
	// exit code 0 and empty stderr: invoke.error must NOT be emitted.

	runner := &fakeCommandRunner{
		stderr:   nil,
		exitCode: 0,
	}
	invoker := newInvokerWithLogger(ws, runner, logger)

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on discovery failure (no Orchestration-* dirs), got nil")
	}

	// invoke.error must NOT be emitted when stderr is empty and exit code is 0.
	if logger.hasEvent(domain.EventTestrunInvokeError) {
		t.Errorf("discovery-failure path with exit 0 and empty stderr: %q event must NOT be emitted\n"+
			"(invoke.error is only emitted when stderr is non-empty OR exit_code != 0;\n"+
			"a clean-exit discovery failure is not a subprocess invocation error)",
			domain.EventTestrunInvokeError)
	}
}

// TestInvokeLogging_DiscoveryFailure_InvokeError_StderrMessage verifies that
// when discovery fails and stderr is non-empty, testrun.invoke.error is
// emitted with a message containing the child stderr.
func TestInvokeLogging_DiscoveryFailure_InvokeError_StderrMessage(t *testing.T) {
	ws := t.TempDir()
	logger := &fakeDebugLogger{}
	stderrContent := "child: process failed to produce run folder\n"

	runner := &fakeCommandRunner{
		stderr:   []byte(stderrContent),
		exitCode: 2,
	}
	invoker := newInvokerWithLogger(ws, runner, logger)

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on discovery failure")
	}

	msg, ok := logger.messageFor(domain.EventTestrunInvokeError)
	if !ok {
		t.Errorf("discovery-failure path with non-empty stderr: %q event not emitted",
			domain.EventTestrunInvokeError)
	} else if !strings.Contains(msg, "process failed to produce run folder") {
		t.Errorf("%q event message does not contain child stderr content\nmessage: %q\n(invoke.error message must be the full child stderr on discovery-failure path)",
			domain.EventTestrunInvokeError, msg)
	}
}

// TestInvokeLogging_BinaryDiscoveryFailure_EmitsInvokeErrorOnly verifies
// Path 6 (discoverBinary fails): only testrun.invoke.error is emitted.
// testrun.invoke.start is NOT emitted (binary path unknown).
// The error field carries the raw cause text.
func TestInvokeLogging_BinaryDiscoveryFailure_EmitsInvokeErrorOnly(t *testing.T) {
	logger := &fakeDebugLogger{}
	discoveryErr := errors.New("os.Executable failed: test injection")

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			WorkingDir:  t.TempDir(),
			DebugLogger: logger,
		},
		discoverBinaryFn: func() (string, error) {
			return "", discoveryErr
		},
	}

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error on binary discovery failure, got nil")
	}

	// invoke.start must NOT be emitted (binary path is unknown).
	if logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("binary-discovery-failure path: %q event must NOT be emitted\n(binary path is unknown on Path 6; start event requires the binary field)",
			domain.EventTestrunInvokeStart)
	}

	// invoke.error must be emitted.
	if !logger.hasEvent(domain.EventTestrunInvokeError) {
		t.Errorf("binary-discovery-failure path: %q event not emitted",
			domain.EventTestrunInvokeError)
	}

	// error field must carry the raw discovery error text.
	errField, ok := logger.fieldValue(domain.EventTestrunInvokeError, "error")
	if !ok {
		t.Errorf("%q event missing %q field on binary-discovery-failure path",
			domain.EventTestrunInvokeError, "error")
	} else if !strings.Contains(errField, discoveryErr.Error()) {
		t.Errorf("%q event field error = %q, want it to contain the raw discovery error %q",
			domain.EventTestrunInvokeError, errField, discoveryErr.Error())
	} else if strings.Contains(errField, "\n") {
		// The error field must be single-line. If the raw error text contains
		// newlines they must be replaced with " | " before storing in the field.
		t.Errorf("%q event field error contains embedded newline; want single-line value\n"+
			"(embedded newlines must be replaced with \" | \" per the event field contract)\nerror field: %q",
			domain.EventTestrunInvokeError, errField)
	}

	// exit_code must be "0" (no subprocess ran).
	exitCode, ok := logger.fieldValue(domain.EventTestrunInvokeError, "exit_code")
	if !ok {
		t.Errorf("%q event missing %q field on binary-discovery-failure path",
			domain.EventTestrunInvokeError, "exit_code")
	} else if exitCode != "0" {
		t.Errorf("%q event field exit_code = %q, want %q (no subprocess ran on Path 6)",
			domain.EventTestrunInvokeError, exitCode, "0")
	}
}

// TestInvokeLogging_Path5_ReadDirFailure_EmitsDiscoveryFailWithErrorMessage
// verifies Path 5 (post-invoke ReadDir failure): testrun.invoke.start,
// testrun.invoke.done, and testrun.discovery.fail are all emitted. The
// discovery.fail message must contain the ReadDir error text (de.Cause.Error()),
// NOT strings.Join(de.Listing, "\n") -- the Path 4 format. On Path 5, de.Listing
// is empty because ReadDir itself failed; an implementation that accidentally
// applies the Path 4 message format would emit an empty message here. The test
// asserts message non-emptiness to detect that regression.
//
// See TestInvoke_ChildStderr_PopulatedOnPath5_ReadDirFailure for the rationale
// of using a subdirectory workspace (not the t.TempDir() root itself).
func TestInvokeLogging_Path5_ReadDirFailure_EmitsDiscoveryFailWithErrorMessage(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "workspace")
	if err := os.Mkdir(ws, 0755); err != nil {
		t.Fatal(err)
	}
	logger := &fakeDebugLogger{}

	fakeRunner := func(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
		// Remove ws to simulate the workspace being deleted while the subprocess
		// runs. Uses the captured ws (not workDir parameter) because the current
		// stub passes "" as workDir. Removal causes the post-invoke ReadDir to
		// fail (Path 5: discoveryReadDirFailed).
		if err := os.RemoveAll(ws); err != nil {
			return nil, nil, 0, fmt.Errorf("RemoveAll in fake runner: %w", err)
		}
		return nil, []byte("path5 subprocess stderr\n"), 1, nil
	}

	invoker := &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     ws,
			Invoke:         fakeRunner,
			DebugLogger:    logger,
		},
	}

	_, invokeErr := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when post-invoke ReadDir fails (Path 5), got nil")
	}

	// invoke.start must be emitted (logged before subprocess start, binary known).
	if !logger.hasEvent(domain.EventTestrunInvokeStart) {
		t.Errorf("Path 5: %q event not emitted (want: logged before subprocess start)",
			domain.EventTestrunInvokeStart)
	}

	// invoke.done must be emitted (subprocess ran to completion before ReadDir failed).
	if !logger.hasEvent(domain.EventTestrunInvokeDone) {
		t.Errorf("Path 5: %q event not emitted (want: logged after subprocess completes; ReadDir failure is post-invoke)",
			domain.EventTestrunInvokeDone)
	}

	// discovery.fail must be emitted.
	if !logger.hasEvent(domain.EventTestrunDiscoveryFail) {
		t.Errorf("Path 5: %q event not emitted (want: logged when post-invoke ReadDir fails)",
			domain.EventTestrunDiscoveryFail)
	}

	// discovery.fail must carry the workspace field.
	workspace, ok := logger.fieldValue(domain.EventTestrunDiscoveryFail, "workspace")
	if !ok {
		t.Errorf("%q event missing %q field on Path 5",
			domain.EventTestrunDiscoveryFail, "workspace")
	} else if workspace != ws {
		t.Errorf("%q event field workspace = %q, want %q",
			domain.EventTestrunDiscoveryFail, workspace, ws)
	}

	// The discovery.fail message must be the ReadDir error text (de.Cause.Error()),
	// not strings.Join(de.Listing, "\n"). On Path 5, de.Listing is empty because
	// ReadDir failed before producing any listing. An implementation that incorrectly
	// uses the Path 4 message format would join an empty slice, producing an empty
	// (or whitespace-only) string. Assert the message is non-empty to catch that bug.
	msg, hasFail := logger.messageFor(domain.EventTestrunDiscoveryFail)
	if !hasFail {
		t.Errorf("%q event not found (already asserted above -- this branch is defensive)",
			domain.EventTestrunDiscoveryFail)
	} else if strings.TrimSpace(msg) == "" {
		t.Errorf("%q event message = %q; want non-empty ReadDir error text (de.Cause.Error())\n"+
			"An empty message indicates the implementation used strings.Join(de.Listing, \"\\n\") -- "+
			"the Path 4 format -- which produces empty string when Listing is empty (Path 5).",
			domain.EventTestrunDiscoveryFail, msg)
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
	_, err := invoker.Invoke(context.Background(), RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})
	if err != nil {
		t.Fatalf("Invoke with nil DebugLogger returned unexpected error: %v\n(nil logger must not affect invocation behavior, only suppress logging)", err)
	}
}

// =============================================================================
// buildRunArgs: --pre-consult flag emission
// =============================================================================

// TestBuildRunArgs_PreConsultTrue_EmitsFlag verifies that when
// RunInvocation.PreConsult is true, buildRunArgs emits --pre-consult=true in
// the argument list.
func TestBuildRunArgs_PreConsultTrue_EmitsFlag(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
		PreConsult:  true,
	}
	args := buildRunArgs(inv)

	wantFlag := "--pre-consult=true"
	found := false
	for _, a := range args {
		if a == wantFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("buildRunArgs with PreConsult=true: %q not found in args %v\n(--pre-consult flag must always be emitted with the boolean value from RunInvocation.PreConsult)",
			wantFlag, args)
	}

	// Also ensure the false variant is absent so both variants are unambiguous.
	for _, a := range args {
		if a == "--pre-consult=false" {
			t.Errorf("buildRunArgs with PreConsult=true: --pre-consult=false unexpectedly present in args %v", args)
		}
	}
}

// TestBuildRunArgs_PreConsultFalse_EmitsFlag verifies that when
// RunInvocation.PreConsult is false, buildRunArgs emits --pre-consult=false in
// the argument list.
func TestBuildRunArgs_PreConsultFalse_EmitsFlag(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/catalog/Fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
		PreConsult:  false,
	}
	args := buildRunArgs(inv)

	wantFlag := "--pre-consult=false"
	found := false
	for _, a := range args {
		if a == wantFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("buildRunArgs with PreConsult=false: %q not found in args %v\n(--pre-consult flag must always be emitted with the boolean value from RunInvocation.PreConsult)",
			wantFlag, args)
	}

	// Also ensure the true variant is absent.
	for _, a := range args {
		if a == "--pre-consult=true" {
			t.Errorf("buildRunArgs with PreConsult=false: --pre-consult=true unexpectedly present in args %v", args)
		}
	}
}

// TestBuildRunArgs_PreConsult_UsesEqualsFormat verifies that the --pre-consult
// flag uses the = separator format (--pre-consult=true or --pre-consult=false)
// rather than a space-separated value (--pre-consult true). This is the format
// expected by the subprocess's scanBoolFlagDefault parser.
func TestBuildRunArgs_PreConsult_UsesEqualsFormat(t *testing.T) {
	for _, preConsult := range []bool{true, false} {
		inv := RunInvocation{
			WorkflowID:  "wf-a",
			Mode:        "auto",
			Harness:     "auto",
			FixturePath: "/fixtures/wf-a",
			Task:        "Test: wf-a / auto / auto",
			PreConsult:  preConsult,
		}
		args := buildRunArgs(inv)

		// "--pre-consult" must not appear as a standalone arg (space-separated format).
		for _, a := range args {
			if a == "--pre-consult" {
				t.Errorf("PreConsult=%v: --pre-consult appears as standalone arg (without = separator); want --pre-consult=true or --pre-consult=false; full args: %v",
					preConsult, args)
			}
		}

		// The = format must be present.
		wantFlag := "--pre-consult=" + strconv.FormatBool(preConsult)
		found := false
		for _, a := range args {
			if a == wantFlag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PreConsult=%v: %q not found in args %v", preConsult, wantFlag, args)
		}
	}
}

// =============================================================================
// buildRunArgs: --infrastructure flag (nil/empty/populated semantics, = form)
// =============================================================================

// containsArg reports whether args contains the exact token s.
func containsArg(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// TestBuildRunArgs_Infrastructure_Nil_OmitsFlag verifies that when
// RunInvocation.InfrastructureKeys is nil, buildRunArgs does NOT include any
// --infrastructure token in the args slice (backwards-compat: callers that do
// not set the field behave as before).
//
// TDD RED: fails until I3.2 updates buildRunArgs to emit --infrastructure.
func TestBuildRunArgs_Infrastructure_Nil_OmitsFlag(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: nil, // nil: must omit flag entirely
	}
	args := buildRunArgs(inv)

	for _, a := range args {
		if strings.HasPrefix(a, "--infrastructure") {
			t.Errorf("InfrastructureKeys=nil: --infrastructure must be omitted; got arg %q in %v",
				a, args)
		}
	}
}

// TestBuildRunArgs_Infrastructure_NonNilEmpty_EmitsSingleTokenEmptyValue verifies
// that when InfrastructureKeys is non-nil but empty ([]string{}), buildRunArgs
// appends the single token "--infrastructure=" (with empty value after the =).
// This single-token form is required because pflag parses --infrastructure=
// as Changed()==true with value "", distinguishing it from "flag not passed".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_NonNilEmpty_EmitsSingleTokenEmptyValue(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{}, // non-nil empty: emit --infrastructure=
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--infrastructure=") {
		t.Errorf("InfrastructureKeys=[]string{}: want --infrastructure= (single token with empty value) in args %v", args)
	}
}

// TestBuildRunArgs_Infrastructure_SingleKey_EmitsSingleTokenWithValue verifies
// that a single InfrastructureKey is emitted as "--infrastructure=key" (= form).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_SingleKey_EmitsSingleTokenWithValue(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-review"},
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--infrastructure=mosaictest-review") {
		t.Errorf("InfrastructureKeys=[\"mosaictest-review\"]: want --infrastructure=mosaictest-review in args %v", args)
	}
}

// TestBuildRunArgs_Infrastructure_MultipleKeys_EmitsCommaJoined verifies that
// multiple InfrastructureKeys are joined with commas in the single token:
// "--infrastructure=k1,k2" (not two separate tokens).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_MultipleKeys_EmitsCommaJoined(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-checkpoint", "mosaictest-review"},
	}
	args := buildRunArgs(inv)

	want := "--infrastructure=mosaictest-checkpoint,mosaictest-review"
	if !containsArg(args, want) {
		t.Errorf("InfrastructureKeys=[checkpoint, review]: want %q in args %v", want, args)
	}
}

// TestBuildRunArgs_Infrastructure_WorkflowWithoutAgents_EmitsEmptyToken covers
// the case where a workflow has no infrastructure_agents in its frontmatter.
// Stage 2 guarantees CatalogEntry.InfrastructureAgents is []string{} (non-nil
// empty) for such workflows, so Orchestrator.Run sets InfrastructureKeys to
// that same non-nil empty slice, and buildRunArgs must emit --infrastructure=.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Infrastructure_WorkflowWithoutAgents_EmitsEmptyToken(t *testing.T) {
	// InfrastructureKeys is explicitly set to non-nil empty to simulate the
	// output of Orchestrator.Run for a workflow without infrastructure_agents.
	inv := RunInvocation{
		WorkflowID:         "simple-workflow",
		Mode:               "auto",
		Harness:            "claude-code",
		FixturePath:        "/fixtures/simple-workflow",
		Task:               "Test: simple-workflow / auto / claude-code",
		InfrastructureKeys: []string{},
	}
	args := buildRunArgs(inv)

	// The subprocess must receive --infrastructure= so pflag sets
	// Changed("infrastructure")==true with value "", activating the empty filter
	// (no agents active). Without this token the subprocess uses all declared agents.
	if !containsArg(args, "--infrastructure=") {
		t.Errorf("workflow without infrastructure_agents (InfrastructureKeys=[]string{}): "+
			"want --infrastructure= in args %v\n"+
			"(absence would leave all deployed agents active for this subprocess)", args)
	}
}

// =============================================================================
// buildRunArgs: --checkpoints and --commits flags
// =============================================================================

// TestBuildRunArgs_Checkpoints_Enabled_EmitsEnabled verifies that when
// RunInvocation.Checkpoints is "enabled", buildRunArgs emits
// "--checkpoints" followed by "enabled" as two consecutive tokens.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_Enabled_EmitsEnabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "enabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "enabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=enabled: want --checkpoints enabled in args %v", args)
	}
}

// TestBuildRunArgs_Checkpoints_Disabled_EmitsDisabled verifies that when
// RunInvocation.Checkpoints is "disabled", buildRunArgs emits
// "--checkpoints" followed by "disabled".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_Disabled_EmitsDisabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "disabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=disabled: want --checkpoints disabled in args %v", args)
	}
}

// TestBuildRunArgs_Checkpoints_EmptyString_EmitsDisabled verifies that when
// RunInvocation.Checkpoints is "" (empty string), buildRunArgs emits
// "--checkpoints disabled" (empty string maps to "disabled" as defense-in-depth).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Checkpoints_EmptyString_EmitsDisabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Checkpoints: "", // empty: must default to "disabled"
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--checkpoints" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Checkpoints=\"\" (empty): want --checkpoints disabled in args %v\n"+
			"(empty string must map to \"disabled\" to avoid the subprocess rejecting an empty value)",
			args)
	}
}

// TestBuildRunArgs_Commits_Enabled_EmitsEnabled verifies that when
// RunInvocation.Commits is "enabled", buildRunArgs emits "--commits" followed
// by "enabled" as two consecutive tokens.
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_Enabled_EmitsEnabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "enabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "enabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=enabled: want --commits enabled in args %v", args)
	}
}

// TestBuildRunArgs_Commits_Disabled_EmitsDisabled verifies that when
// RunInvocation.Commits is "disabled", buildRunArgs emits "--commits" followed
// by "disabled".
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_Disabled_EmitsDisabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "disabled",
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=disabled: want --commits disabled in args %v", args)
	}
}

// TestBuildRunArgs_Commits_EmptyString_EmitsDisabled verifies that when
// RunInvocation.Commits is "" (empty string), buildRunArgs emits
// "--commits disabled" (empty string maps to "disabled" as defense-in-depth).
//
// TDD RED: fails until I3.2 updates buildRunArgs.
func TestBuildRunArgs_Commits_EmptyString_EmitsDisabled(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "wf-a",
		Mode:        "auto",
		Harness:     "auto",
		FixturePath: "/fixtures/wf-a",
		Task:        "Test: wf-a / auto / auto",
		Commits:     "", // empty: must default to "disabled"
	}
	args := buildRunArgs(inv)

	found := false
	for i, a := range args {
		if a == "--commits" && i+1 < len(args) && args[i+1] == "disabled" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commits=\"\" (empty): want --commits disabled in args %v\n"+
			"(empty string must map to \"disabled\" to avoid the subprocess rejecting an empty value)",
			args)
	}
}

// =============================================================================
// T5.2: buildRunArgs -- --dev-test-mode emission
// =============================================================================

// TestBuildRunArgs_DevTestMode_AlwaysPresent verifies that buildRunArgs always
// emits --dev-test-mode in its output regardless of other RunInvocation fields.
// This is the subprocess-side signal that --infrastructure is accepted, and
// must be present in every invocation since buildRunArgs is only called by the
// test framework's SubprocessRunInvoker.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_AlwaysPresent(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		Harness:     "claude-code",
		FixturePath: "/fixtures/smoke-single",
		Task:        "Test: smoke-single / auto / claude-code",
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode in args %v\n"+
			"(--dev-test-mode must always be emitted; it is the subprocess-side signal "+
			"that --infrastructure is accepted)", args)
	}
}

// TestBuildRunArgs_DevTestMode_PresentWithNilInfrastructureKeys verifies that
// --dev-test-mode is emitted even when InfrastructureKeys is nil (the workflow
// declares no infrastructure agents). The flag is always emitted regardless of
// whether --infrastructure is also emitted.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_PresentWithNilInfrastructureKeys(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: nil,
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode when InfrastructureKeys=nil; "+
			"full args: %v", args)
	}
}

// TestBuildRunArgs_DevTestMode_PresentAlongsideInfrastructureKeys verifies that
// --dev-test-mode is emitted together with --infrastructure=k1 when
// InfrastructureKeys is populated. Both flags must appear in the output so the
// subprocess accepts and applies the infrastructure filter.
//
// TDD RED: fails until I5.3 adds --dev-test-mode to buildRunArgs.
func TestBuildRunArgs_DevTestMode_PresentAlongsideInfrastructureKeys(t *testing.T) {
	inv := RunInvocation{
		WorkflowID:         "wf-a",
		Mode:               "auto",
		Harness:            "auto",
		FixturePath:        "/fixtures/wf-a",
		Task:               "Test: wf-a / auto / auto",
		InfrastructureKeys: []string{"mosaictest-review"},
	}
	args := buildRunArgs(inv)

	if !containsArg(args, "--dev-test-mode") {
		t.Errorf("buildRunArgs: missing --dev-test-mode alongside --infrastructure; "+
			"full args: %v", args)
	}
	if !containsArg(args, "--infrastructure=mosaictest-review") {
		t.Errorf("buildRunArgs: missing --infrastructure=mosaictest-review in args %v", args)
	}
}
