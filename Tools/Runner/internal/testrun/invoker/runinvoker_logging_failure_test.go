// Tests for the debug events SubprocessRunInvoker.Invoke emits on failure paths.
package invoker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
	"mosaic-run/internal/domain"
)

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
	_, err := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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
