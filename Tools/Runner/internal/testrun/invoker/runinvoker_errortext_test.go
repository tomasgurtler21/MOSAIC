// Tests for bounded stderr in SubprocessRunInvoker.Invoke error text and the errWithStderr helper.
package invoker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testrun"
)

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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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

	result, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
		WorkflowID: "wf-x", Mode: "auto", Harness: "auto",
	})

	if invokeErr == nil {
		t.Fatal("expected non-nil error from Invoke when post-invoke ReadDir fails (Path 5), got nil")
	}
	if string(result.ChildStderr) != string(wantStderr) {
		t.Errorf("ChildStderr = %q, want %q\n(ChildStderr must be populated on Path 5 post-invoke ReadDir failure; design requires testrun.InvokeResult to carry subprocess stderr on all error paths)",
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

	_, invokeErr := invoker.Invoke(context.Background(), testrun.RunInvocation{
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
