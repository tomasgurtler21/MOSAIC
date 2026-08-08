package runner_test

// Supervision tests: three terminating conditions, three distinct
// dispositions, driven entirely through a stub launcher so no real process
// is ever needed. The dispositions must remain distinguishable all the way
// into the returned evidence, and the supervisor's own decision must win
// over whatever the launcher's decoded result reports when the two disagree.

import (
	"context"
	"os"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

func TestRun_EarlyExitSentinel_YieldsEarlyExitDisposition(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		// The supervisor observed the sentinel, cancelled the launch and
		// terminated the subject: a normal outcome of a single-decision
		// test, never a fault.
		return domain.SubjectResult{Disposition: domain.DispositionEarlyExit}, nil
	}
	req := newRequest("early-exit")

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectResult.Disposition != domain.DispositionEarlyExit {
		t.Errorf("Disposition = %q, want %q", evidence.SubjectResult.Disposition, domain.DispositionEarlyExit)
	}
}

func TestRun_DeclaredTimeoutElapses_YieldsTimedOutDisposition(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		<-ctx.Done()
		return domain.SubjectResult{Disposition: domain.DispositionTimedOut}, ctx.Err()
	}
	req := newRequest("timeout")
	timeout := 10 * time.Millisecond
	req.Settings.Timeout = &timeout

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	// A timeout is a disposition, not a startup fault: Run's error return is
	// documented as reserved for a fault that prevented the attempt from
	// starting at all, and a declared timeout elapsing is not that.
	if err != nil {
		t.Errorf("Run returned unexpected error for a timeout disposition: %v — a timeout is not a startup fault", err)
	}

	if evidence.SubjectResult.Disposition != domain.DispositionTimedOut {
		t.Errorf("Disposition = %q, want %q", evidence.SubjectResult.Disposition, domain.DispositionTimedOut)
	}
	if evidence.SubjectResult.Disposition == domain.DispositionEarlyExit {
		t.Error("a real timeout must never be reported as an early exit")
	}
}

func TestRun_LauncherReportsTurnLimit_YieldsTurnLimitDisposition(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		// The harness announced its own turn limit in its output envelope —
		// a distinct event from the runner's own declared TurnLimit, decoded
		// by the adapter's decoder and reported through the launcher.
		return domain.SubjectResult{Disposition: domain.DispositionTurnLimit}, nil
	}
	req := newRequest("turn-limit")

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectResult.Disposition != domain.DispositionTurnLimit {
		t.Errorf("Disposition = %q, want %q", evidence.SubjectResult.Disposition, domain.DispositionTurnLimit)
	}
}

func TestRun_SentinelCancelledRun_WinsOverLauncherDecodingItAsTimeout(t *testing.T) {
	// The precedence case: the supervisor cancelled the launch because of
	// the sentinel, but the launcher's decoded result nonetheless reports a
	// timeout (the context cancellation looks, from the decoder's vantage
	// point, exactly like one). The supervisor's decision must win, or a
	// deliberate early exit reads as a fault.
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		// Write the sentinel the plan declared, then wait for the
		// supervisor to notice it and cancel this context — simulating a
		// subject that observed the sentinel mid-turn. The decoded
		// disposition below is deliberately wrong (a timeout), so the only
		// way this test passes is if the supervisor's own decision
		// overrides it.
		if plan.EarlyExitSentinel != "" {
			_ = os.WriteFile(plan.EarlyExitSentinel, []byte("1"), 0o644)
		}
		<-ctx.Done()
		return domain.SubjectResult{Disposition: domain.DispositionTimedOut}, ctx.Err()
	}
	req := newRequest("sentinel-precedence")

	// A generous safety bound: if a future implementation fails to cancel
	// the launcher's context on the sentinel, this fails the test via
	// timeout instead of hanging the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	evidence, err := runner.Run(ctx, h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if evidence.SubjectResult.Disposition != domain.DispositionEarlyExit {
		t.Errorf("Disposition = %q, want %q — the supervisor's decision must win over the launcher's decoded timeout", evidence.SubjectResult.Disposition, domain.DispositionEarlyExit)
	}
}
