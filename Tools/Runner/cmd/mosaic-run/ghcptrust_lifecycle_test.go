package main

// ghcptrust_lifecycle_test.go exercises the preflight composed inside the
// run-lifecycle decorator, as the composition root builds it.

import (
	"context"
	"testing"

	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/ghcptrust"
)

// composeTrust nests lifecycle (outermost) -> preflight -> inner.
func composeTrust(runFolder string, inner *mlSession, checker trustChecker, in domain.Interaction, interactive bool) func(context.Context) (domain.RunOutcome, error) {
	pre := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, interactive))
	sess := newRunLifecycleSession(pre, mlLogConfig(runFolder, "ghcp-cli"))
	return func(ctx context.Context) (domain.RunOutcome, error) {
		return sess.Start(ctx, domain.RunConfig{})
	}
}

func TestTrustComposition_TUIAbort_YieldsOneAbortedPair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}}
	inner := &mlSession{}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	out, err := composeTrust(runFolder, inner, checker, in, true)(context.Background())

	if err != nil || out.Status != domain.RunRefused {
		t.Fatalf("got (%+v, %v), want RunRefused and a nil error", out, err)
	}
	if inner.calls != 0 {
		t.Errorf("wrapped session started %d time(s) after an abort", inner.calls)
	}
	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	if events[1]["outcome"] != "aborted" {
		t.Errorf("run_end.outcome = %v, want aborted", events[1]["outcome"])
	}
}

func TestTrustComposition_OrderIsRunStartThenCheckThenStart(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	var atCheck, atStart []string
	checker := &MockTrustChecker{
		Result: trustResult(ghcptrust.StatusUntrusted),
		OnCheck: func(string) {
			atCheck = mlEventNames(mlReadEvents(t, mlRunLogPath(ws)))
		},
	}
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		atStart = mlEventNames(mlReadEvents(t, mlRunLogPath(ws)))
		return domain.RunOutcome{Status: domain.RunCompleted}, nil
	}}

	_, err := composeTrust(runFolder, inner, checker, &MockInteraction{}, false)(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(atCheck) != 1 || atCheck[0] != "run_start" {
		t.Errorf("events when the trust check ran = %v, want only run_start", atCheck)
	}
	if len(atStart) != 1 || atStart[0] != "run_start" {
		t.Errorf("events when the wrapped Start ran = %v, want only run_start", atStart)
	}
	if len(checker.Checked) != 1 || inner.calls != 1 {
		t.Errorf("checks=%d starts=%d, want 1 each", len(checker.Checked), inner.calls)
	}
	mlAssertPairs(t, mlReadEvents(t, mlRunLogPath(ws)), 1)
}

func TestTrustComposition_TUIProceed_RunsSessionAndWritesCompletedPair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: true}}
	inner := &mlSession{}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	out, err := composeTrust(runFolder, inner, checker, in, true)(context.Background())

	if err != nil || out.Status != domain.RunCompleted || inner.calls != 1 {
		t.Fatalf("got (%+v, %v) after %d call(s), want the wrapped run", out, err, inner.calls)
	}
	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	if events[1]["outcome"] != "completed" {
		t.Errorf("run_end.outcome = %v, want completed", events[1]["outcome"])
	}
}

func TestTrustComposition_ResumeAfterAbort_RechecksAndWritesSecondPair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Cancelled}}
	inner := &mlSession{}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}
	start := composeTrust(runFolder, inner, checker, in, true)

	_, _ = start(context.Background())
	in.ConfirmReply = interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: true}
	out, err := start(context.Background())

	if err != nil || out.Status != domain.RunCompleted {
		t.Fatalf("second start got (%+v, %v), want proceed", out, err)
	}
	if len(checker.Checked) != 2 || len(in.Questions) != 2 || inner.calls != 1 {
		t.Errorf("checks=%d questions=%d starts=%d, want 2, 2, 1", len(checker.Checked), len(in.Questions), inner.calls)
	}
	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 2)
	if events[1]["outcome"] != "aborted" || events[3]["outcome"] != "completed" {
		t.Errorf("outcomes = %v, %v, want aborted then completed", events[1]["outcome"], events[3]["outcome"])
	}
}
