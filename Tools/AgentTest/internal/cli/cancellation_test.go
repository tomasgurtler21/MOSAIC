package cli_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// TestExecute_PropagatesContextToSuiteRunner asserts the context Execute
// receives — the one the composition root cancels on an interrupt — is the
// same one (or a derivative of it) handed to the suite runner. Without this,
// an interrupt has no channel through which to cancel a run.
func TestExecute_PropagatesContextToSuiteRunner(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	ctx := context.WithValue(context.Background(), ctxProbeKey{}, "probe-value")
	cli.Execute(ctx, []string{"run", "suite.yaml"}, opts)

	if !runner.wasCalled() {
		t.Fatal("suite runner was not invoked")
	}
	runner.mu.Lock()
	seen := runner.gotCtx
	runner.mu.Unlock()
	if seen == nil {
		t.Fatal("suite runner observed a nil context")
	}
	if got, _ := seen.Value(ctxProbeKey{}).(string); got != "probe-value" {
		t.Errorf("suite runner's context lost the caller's value; got %q", got)
	}
}

type ctxProbeKey struct{}

// TestExecute_InterruptCancelsThroughTheSuite_NotProcessKill asserts AC15.7:
// cancelling the context Execute was given propagates to the in-flight
// suite run, and Execute returns rather than hanging. A hard process kill
// would skip the teardown that removes the run's workspace; routing
// cancellation through the suite's own context is what keeps that teardown
// running.
func TestExecute_InterruptCancelsThroughTheSuite_NotProcessKill(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{
		result: passingResult("orchestrator-routing"),
		block:  make(chan struct{}), // never closed: only cancellation ends Run
	}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan int, 1)
	go func() {
		done <- cli.Execute(ctx, []string{"run", "suite.yaml"}, opts)
	}()

	// Give Execute a moment to reach the suite call before cancelling, so
	// the cancellation genuinely interrupts an in-flight run rather than
	// racing its own startup.
	deadline := time.After(2 * time.Second)
	for !runner.wasCalled() {
		select {
		case <-deadline:
			t.Fatal("suite runner was never invoked; Execute must call it before this test can exercise cancellation")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return after its context was cancelled")
	}

	if !runner.sawCancellation() {
		t.Error("suite runner never observed the cancelled context; cancellation did not propagate through the suite")
	}
}

// TestExecute_ReturnsPromptly_ForANormalInvocation is a smoke test against
// the more general claim that no code path in this package blocks on user
// input: a normal, non-blocking suite run must complete and return well
// within a generous bound, not merely "eventually".
func TestExecute_ReturnsPromptly_ForANormalInvocation(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	done := make(chan int, 1)
	go func() {
		done <- cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)
	}()

	select {
	case got := <-done:
		if got != cli.ExitSuccess {
			t.Errorf("exit code = %d, want ExitSuccess (%d)", got, cli.ExitSuccess)
		}
		if !runner.wasCalled() {
			t.Error("suite runner was not invoked for a normal run")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return for a normal invocation with nothing to wait on")
	}
}
