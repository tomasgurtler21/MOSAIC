package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

func TestRunLifecycle_WritesOnePairAroundStart(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	var namesDuringStart []string
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		namesDuringStart = mlEventNames(mlReadEvents(t, mlRunLogPath(ws)))
		return domain.RunOutcome{Status: domain.RunCompleted}, nil
	}}

	_, err := newRunLifecycleSession(inner, mlLogConfig(runFolder, "opencode")).Start(context.Background(), domain.RunConfig{})

	if err != nil {
		t.Fatal(err)
	}
	if len(namesDuringStart) != 1 || namesDuringStart[0] != "run_start" {
		t.Fatalf("events while the wrapped Start ran = %v, want only run_start", namesDuringStart)
	}
	mlAssertPairs(t, mlReadEvents(t, mlRunLogPath(ws)), 1)
}

func TestRunLifecycle_EnvelopeAndFields(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	sess := newRunLifecycleSession(&mlSession{}, mlLogConfig(runFolder, "ghcp-cli"))

	_, _ = sess.Start(context.Background(), domain.RunConfig{})

	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	start, end := events[0], events[1]
	for _, ev := range events {
		if ev["schema_version"] != "1.1.0" || ev["harness"] != "ghcp-cli" || ev["run_id"] != mlRunID ||
			ev["timestamp"] != "2026-10-03T18:50:44.123Z" {
			t.Errorf("envelope wrong: %v", ev)
		}
		for _, k := range []string{"session_id", "model", "adapter_version"} {
			if _, ok := ev[k]; ok {
				t.Errorf("%s must be omitted: %v", k, ev)
			}
		}
	}
	if start["cwd"] != ws {
		t.Errorf("run_start.cwd = %v, want the run folder's parent %q", start["cwd"], ws)
	}
	if !mlValidOutcome(end["outcome"]) {
		t.Errorf("run_end.outcome = %v, want a vocabulary value", end["outcome"])
	}
}

func TestRunLifecycle_OutcomePerExitClass(t *testing.T) {
	boom := errors.New("infrastructure failure")
	tests := []struct {
		name   string
		status domain.RunStatus
		err    error
		want   string // "" = any vocabulary value
	}{
		{"completed", domain.RunCompleted, nil, "completed"},
		{"stopped", domain.RunStopped, nil, "stopped"},
		{"stopped by consultant", domain.RunStoppedByConsultant, nil, ""},
		{"deviation unresolved", domain.RunDeviationUnresolved, nil, ""},
		{"failure", domain.RunFailed, nil, ""},
		{"start failure", domain.RunStartFailed, nil, ""},
		{"refusal", domain.RunRefused, nil, "aborted"},
		{"error return", "", boom, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runFolder, ws := mlRunFolder(t)
			want := domain.RunOutcome{Status: tt.status, Message: "m"}
			inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
				return want, tt.err
			}}

			got, err := newRunLifecycleSession(inner, mlLogConfig(runFolder, "claude-code")).Start(context.Background(), domain.RunConfig{})

			if got.Status != want.Status || got.Message != want.Message || err != tt.err {
				t.Fatalf("returned (%+v, %v), want the wrapped session's (%+v, %v) unchanged", got, err, want, tt.err)
			}
			events := mlReadEvents(t, mlRunLogPath(ws))
			mlAssertPairs(t, events, 1)
			outcome := events[1]["outcome"]
			if !mlValidOutcome(outcome) || (tt.want != "" && outcome != tt.want) {
				t.Fatalf("run_end.outcome = %v, want %q", outcome, tt.want)
			}
		})
	}
}

func TestRunLifecycle_CancellationYieldsInterrupted(t *testing.T) {
	tests := []struct {
		name string
		ret  func(ctx context.Context) (domain.RunOutcome, error)
	}{
		{"returns ctx error", func(ctx context.Context) (domain.RunOutcome, error) { return domain.RunOutcome{}, ctx.Err() }},
		{"returns a stop outcome after cancel", func(context.Context) (domain.RunOutcome, error) {
			return domain.RunOutcome{Status: domain.RunStopped}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runFolder, ws := mlRunFolder(t)
			ctx, cancel := context.WithCancel(context.Background())
			inner := &mlSession{start: func(c context.Context, _ domain.RunConfig) (domain.RunOutcome, error) {
				cancel()
				return tt.ret(c)
			}}

			_, _ = newRunLifecycleSession(inner, mlLogConfig(runFolder, "opencode")).Start(ctx, domain.RunConfig{})

			events := mlReadEvents(t, mlRunLogPath(ws))
			mlAssertPairs(t, events, 1)
			if events[1]["outcome"] != "interrupted" {
				t.Fatalf("outcome = %v, want interrupted", events[1]["outcome"])
			}
		})
	}
}

func TestRunLifecycle_ResumeAppendsSecondPair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	sess := newRunLifecycleSession(&mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		return domain.RunOutcome{Status: domain.RunStopped}, nil
	}}, mlLogConfig(runFolder, "opencode"))

	_, _ = sess.Start(context.Background(), domain.RunConfig{IsNewRun: true})
	_, _ = sess.Start(context.Background(), domain.RunConfig{IsNewRun: false})

	mlAssertPairs(t, mlReadEvents(t, mlRunLogPath(ws)), 2)
}

func TestRunLifecycle_SeparateSessionsOnSameRunEachWritePair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)

	for i := 0; i < 2; i++ {
		sess := newRunLifecycleSession(&mlSession{}, mlLogConfig(runFolder, "claude-code"))
		_, _ = sess.Start(context.Background(), domain.RunConfig{})
	}

	mlAssertPairs(t, mlReadEvents(t, mlRunLogPath(ws)), 2)
}

func TestRunLifecycle_WrappedSessionThatNeverDispatchesStillYieldsPair(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		return domain.RunOutcome{Status: domain.RunRefused, Message: "trust folder refused"}, nil
	}}

	_, _ = newRunLifecycleSession(inner, mlLogConfig(runFolder, "ghcp-cli")).Start(context.Background(), domain.RunConfig{})

	events := mlReadEvents(t, mlRunLogPath(ws))
	mlAssertPairs(t, events, 1)
	if events[1]["outcome"] != "aborted" {
		t.Fatalf("outcome = %v, want aborted", events[1]["outcome"])
	}
}

func TestRunLifecycle_PassThroughWhenNothingCanBeLogged(t *testing.T) {
	tests := []struct {
		name      string
		folder    func(ws string) string
		harnessID string
	}{
		{"fake harness", func(ws string) string { return filepath.Join(ws, "Orchestration-"+mlRunID) }, "fake"},
		{"unknown harness", func(ws string) string { return filepath.Join(ws, "Orchestration-"+mlRunID) }, "something-else"},
		{"not a run folder", func(ws string) string { return filepath.Join(ws, "scratch") }, "opencode"},
		{"non-canonical run id", func(ws string) string { return filepath.Join(ws, "Orchestration-abc") }, "opencode"},
		{"empty run folder", func(string) string { return "" }, "opencode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := t.TempDir()
			t.Chdir(ws)
			want := domain.RunOutcome{Status: domain.RunStopped, Message: "x"}
			inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) { return want, nil }}

			got, err := newRunLifecycleSession(inner, mlLogConfig(tt.folder(ws), tt.harnessID)).Start(context.Background(), domain.RunConfig{})

			if err != nil || got.Status != want.Status || inner.calls != 1 {
				t.Fatalf("got (%+v, %v), calls=%d; want the wrapped result and exactly one inner Start", got, err, inner.calls)
			}
			if _, statErr := os.Stat(filepath.Join(ws, "OrchestrationLogs")); statErr == nil {
				t.Fatal("OrchestrationLogs was created, want a pure pass-through")
			}
		})
	}
}

func TestRunLifecycle_UnwritableLogLocationDoesNotChangeTheRun(t *testing.T) {
	boom := errors.New("infrastructure failure")
	tests := []struct {
		name   string
		status domain.RunStatus
		err    error
	}{
		{"completed", domain.RunCompleted, nil},
		{"stopped", domain.RunStopped, nil},
		{"refused", domain.RunRefused, nil},
		{"error", "", boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runFolder, ws := mlRunFolder(t)
			if err := os.WriteFile(filepath.Join(ws, "OrchestrationLogs"), []byte("blocker"), 0o600); err != nil {
				t.Fatal(err)
			}
			want := domain.RunOutcome{Status: tt.status, Message: "m"}
			inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) { return want, tt.err }}

			got, err := newRunLifecycleSession(inner, mlLogConfig(runFolder, "opencode")).Start(context.Background(), domain.RunConfig{})

			if got.Status != want.Status || got.Message != want.Message || err != tt.err || inner.calls != 1 {
				t.Fatalf("got (%+v, %v), calls=%d; want (%+v, %v) and one Start", got, err, inner.calls, want, tt.err)
			}
		})
	}
}
