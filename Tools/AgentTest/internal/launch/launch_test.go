package launch_test

// Tests for the harness-neutral SubjectLauncher (T11.8). This package
// performs process control through mosaic-common/harness's plan-execution
// entry point and nothing else: the decoder that turns a finished
// invocation into a domain.SubjectResult is supplied at construction as a
// plain function value, so this package never names a harness and never
// imports a concrete adapter.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/launch"
)

func testPlan(t *testing.T) domain.SpawnPlan {
	t.Helper()
	return domain.SpawnPlan{
		Executable: helperExe(t),
		WorkingDir: t.TempDir(),
		Timeout:    5 * time.Second,
	}
}

// TestLaunch_ResultComesFromTheInjectedDecoder asserts that Launch's result
// is produced by whatever Decoder value the caller supplied — demonstrated
// with a scripted decoder no harness supplied — and not by any logic this
// package invented on its own.
func TestLaunch_ResultComesFromTheInjectedDecoder(t *testing.T) {
	setHelperEnv(t, "success")

	const marker = "distinguishing-scripted-decoder-marker"
	var decoderCalled bool
	dec := func(res commonharness.Response, err error) (domain.SubjectResult, error) {
		decoderCalled = true
		return domain.SubjectResult{ProtocolMessage: marker, Disposition: domain.DispositionCompleted}, nil
	}

	launcher := launch.New(dec)
	result, err := launcher.Launch(context.Background(), testPlan(t))
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if !decoderCalled {
		t.Fatalf("Launch: the injected decoder was never called")
	}
	if result.ProtocolMessage != marker {
		t.Errorf("Launch: ProtocolMessage = %q, want the scripted decoder's marker %q", result.ProtocolMessage, marker)
	}
}

// TestLaunch_CancelledRunStillYieldsAResult asserts that a subject which
// started and was then cancelled still produces a SubjectResult through the
// decoder, rather than only an error — cancellation is a normal outcome a
// supervisor can act on, not a fault.
func TestLaunch_CancelledRunStillYieldsAResult(t *testing.T) {
	setHelperEnv(t, "hang")

	dec := func(res commonharness.Response, err error) (domain.SubjectResult, error) {
		if errors.Is(err, commonharness.ErrCancelled) {
			return domain.SubjectResult{Disposition: domain.DispositionEarlyExit}, nil
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}
	launcher := launch.New(dec)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := launcher.Launch(ctx, testPlan(t))
	if err != nil {
		t.Fatalf("Launch: expected a result rather than an error for a cancelled-but-started subject, got error: %v", err)
	}
	if result.Disposition == "" {
		t.Errorf("Launch: Disposition is empty; a cancelled run must still carry a disposition from the decoder")
	}
}

// TestLaunch_StartFailureReturnsErrorAndSpawnFailedResult asserts that when
// the subject could not be started at all, Launch returns an error together
// with a result carrying domain.DispositionSpawnFailed — never a zero
// value, which would read downstream as a subject that completed silently.
func TestLaunch_StartFailureReturnsErrorAndSpawnFailedResult(t *testing.T) {
	dec := func(res commonharness.Response, err error) (domain.SubjectResult, error) {
		if err != nil {
			return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, nil
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}
	launcher := launch.New(dec)

	plan := testPlan(t)
	plan.Executable = "definitely-not-a-real-executable-xyz-nonexistent"

	result, err := launcher.Launch(context.Background(), plan)
	if err == nil {
		t.Fatalf("Launch: expected an error when the subject could not be started at all")
	}
	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("Launch: Disposition = %q, want %q — a start failure must never read as a zero-valued (silently completed) result", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// TestLaunch_ResolveExecutableError_YieldsSpawnFailedResult asserts that when
// the executable resolver returns an error, Launch returns
// domain.DispositionSpawnFailed together with a non-nil error — never a
// zero-valued result, which downstream would read as a subject that completed
// silently.
//
// This test drives the error-handling contract of the I2.4 code path: the
// launch path must call a resolver (seam: launch.WithResolveExecutable) and
// propagate any failure as DispositionSpawnFailed rather than ignoring it.
// Before I2.4 introduces the WithResolveExecutable option this test does not
// compile, giving a hard RED state that no implementation detail can mask.
func TestLaunch_ResolveExecutableError_YieldsSpawnFailedResult(t *testing.T) {
	resolveErr := errors.New("resolution-failure-injected-by-test")
	failingResolver := func(path string) (commonharness.Command, error) {
		return commonharness.Command{}, resolveErr
	}

	dec := func(res commonharness.Response, runErr error) (domain.SubjectResult, error) {
		// The resolver fails before Run is called; the decoder should never
		// receive a call. Return Completed so any mistaken invocation is
		// distinguishable from the expected SpawnFailed outcome.
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	launcher := launch.New(dec, launch.WithResolveExecutable(failingResolver))

	plan := domain.SpawnPlan{
		Executable: "any-path.cmd",
		WorkingDir: t.TempDir(),
		Timeout:    5 * time.Second,
	}

	result, err := launcher.Launch(context.Background(), plan)
	if err == nil {
		t.Fatalf("Launch: expected an error when executable resolution fails, got nil")
	}
	if !errors.Is(err, resolveErr) {
		t.Errorf("Launch: error = %v; want it to wrap the resolver's error %v", err, resolveErr)
	}
	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("Launch: Disposition = %q, want %q — a resolution failure must never yield a zero-valued result downstream", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// TestLaunch_WindowsCmd_CmdShimStartsViaResolveExecutable asserts that when
// the plan's executable is a Windows .cmd shim, Launch successfully starts it
// and produces a non-SpawnFailed result.
//
// Mechanism note: on current Go/Windows, exec.Command with an absolute .cmd
// path succeeds regardless of whether launch.go calls
// commonharness.ResolveExecutable or constructs Command{Path: shimPath}
// directly, because the Windows runtime handles both invocation paths through
// cmd.exe. This test therefore provides regression coverage for the .cmd shim
// startup outcome, not a TDD RED guard for the mechanism that I2.4 changes.
// The mechanism — that the launch path calls the shared-package resolver and
// does not reimplement resolution locally — is verified by code inspection and
// by TestLaunch_ResolveExecutableError_YieldsSpawnFailedResult, which drives
// the injected-resolver seam that I2.4 must introduce.
//
// This test skips on non-Windows because .cmd shims are a Windows-specific
// mechanism; the resolver is a no-op on other platforms. The analogous
// OS-level test for the shared harness spawner lives in
// Tools/Common/harness/spawn_test.go.
func TestLaunch_WindowsCmd_CmdShimStartsViaResolveExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("this test exercises .cmd shim resolution on the AgentTest launch path; it only applies on Windows where npm CLI shims are .cmd files; the shared-harness equivalent is in Tools/Common/harness/spawn_test.go")
	}

	// Configure the helper process so the shim's target (this test binary)
	// enters success mode and exits cleanly. Environment variables are
	// inherited by subprocesses, so the .cmd shim passes them through to the
	// test binary acting as the fake CLI.
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	// Create a .cmd shim that delegates to this test binary. This mirrors the
	// shape of a real npm-installed CLI shim on Windows: a batch script that
	// forwards all arguments to the underlying executable. The launch path must
	// resolve this through commonharness.ResolveExecutable so it uses the
	// same cmd.exe /c wrapping that the shared spawner uses.
	shimDir := t.TempDir()
	testBinary := os.Args[0]
	// Quote the binary path to handle spaces; %* forwards all plan args to the
	// helper subprocess so the shim behaves like a real npm CLI shim.
	shimContent := "@echo off\r\n\"" + testBinary + "\" %*\r\n"
	shimPath := filepath.Join(shimDir, "subject.cmd")
	if err := os.WriteFile(shimPath, []byte(shimContent), 0644); err != nil {
		t.Fatalf("WriteFile shim: %v", err)
	}

	dec := func(res commonharness.Response, runErr error) (domain.SubjectResult, error) {
		if runErr != nil {
			return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, nil
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}
	launcher := launch.New(dec)

	plan := domain.SpawnPlan{
		Executable: shimPath,
		WorkingDir: t.TempDir(),
		Timeout:    10 * time.Second,
	}

	result, err := launcher.Launch(context.Background(), plan)
	if err != nil {
		t.Fatalf("Launch: unexpected error when launching .cmd shim %q; the launch path must be able to start a .cmd shim as the AgentTest subject: %v", shimPath, err)
	}
	if result.Disposition == domain.DispositionSpawnFailed {
		t.Errorf("Launch: Disposition = %q; the .cmd shim must have started successfully so that the launch path exercises the same resolution the shared spawner uses", result.Disposition)
	}
}

// TestLaunch_PassesPlanArgsThroughUnchanged asserts that this package
// performs no argument construction of its own: whatever domain.SpawnPlan.Args
// declares reaches the subprocess verbatim.
func TestLaunch_PassesPlanArgsThroughUnchanged(t *testing.T) {
	argsFile := setHelperEnvCapturingArgs(t, "success")

	dec := func(res commonharness.Response, err error) (domain.SubjectResult, error) {
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}
	launcher := launch.New(dec)

	plan := testPlan(t)
	plan.Args = []string{"--one", "two", "--three"}

	if _, err := launcher.Launch(context.Background(), plan); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	got := readHelperArgs(t, argsFile)
	want := []string{"--one", "two", "--three"}
	if len(got) != len(want) {
		t.Fatalf("Launch: subprocess received args %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Launch: args[%d] = %q, want %q — this package must not construct or alter arguments", i, got[i], want[i])
		}
	}
}
