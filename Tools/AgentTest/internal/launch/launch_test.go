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
