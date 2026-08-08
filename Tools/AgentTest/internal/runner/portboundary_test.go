package runner_test

// Port-boundary tests for spawning: the runner obtains a spawn plan from the
// harness adapter and hands it to the launcher port; it starts no process
// itself. Driven entirely through a stub adapter and a stub launcher.
//
// The negative half of AC13.9 — that this package references no harness by
// name and imports no concrete adapter — is not re-asserted here as a
// runtime check: tools/importcheck already enforces it structurally across
// every non-test file in this package (internal/runner is classified as a
// use case, and the harness-isolation rule forbids it from importing
// internal/harness/claudecode, internal/harness/opencode or
// internal/harness/fake). This file's own imports are the same evidence by
// construction — it imports no concrete adapter either.

import (
	"context"
	"reflect"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

func TestRun_PlanTheAdapterReturnedReachesTheLauncherUnmodified(t *testing.T) {
	h := newHarness(t)
	h.Adapter.plan = domain.SpawnPlan{
		Executable:        "stub-subject",
		Args:              []string{"--flag", "value"},
		WorkingDir:        "some/working/dir",
		Env:               []string{"KEY=VALUE"},
		Stdin:             []byte("scripted input"),
		Timeout:           2 * time.Minute,
		EarlyExitSentinel: "sentinel-path",
	}
	req := newRequest("plan-unmodified")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if !reflect.DeepEqual(h.Launcher.receivedPlan, h.Adapter.plan) {
		t.Errorf("launcher received plan %+v, want exactly the adapter's plan %+v", h.Launcher.receivedPlan, h.Adapter.plan)
	}
}

func TestRun_SequenceIsAdapterProvisionThenSpawnPlanThenLauncherLaunch(t *testing.T) {
	h := newHarness(t)
	req := newRequest("sequence")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	order := h.Rec.all()
	provisionIdx, planIdx, launchIdx := -1, -1, -1
	for i, name := range order {
		switch name {
		case "adapter.Provision":
			if provisionIdx == -1 {
				provisionIdx = i
			}
		case "adapter.SpawnPlan":
			if planIdx == -1 {
				planIdx = i
			}
		case "launcher.Launch":
			if launchIdx == -1 {
				launchIdx = i
			}
		}
	}

	if provisionIdx == -1 || planIdx == -1 || launchIdx == -1 {
		t.Fatalf("call order = %v, want it to include adapter.Provision, adapter.SpawnPlan and launcher.Launch", order)
	}
	if !(provisionIdx < planIdx && planIdx < launchIdx) {
		t.Errorf("call order = %v, want Provision before SpawnPlan before Launch", order)
	}
}

func TestRun_NeverInvokesTheLauncherBeforeTheAdapterProducesAPlan(t *testing.T) {
	h := newHarness(t)
	h.Adapter.planErr = errNotUsedByRunnerTests
	req := newRequest("plan-failure")

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Error("Run returned no error, want the adapter's SpawnPlan failure to be reported")
	}

	order := h.Rec.all()
	attemptedPlan := false
	for _, name := range order {
		if name == "adapter.SpawnPlan" {
			attemptedPlan = true
		}
		if name == "launcher.Launch" {
			t.Error("launcher.Launch was called despite the adapter failing to produce a plan")
		}
	}
	if !attemptedPlan {
		t.Fatalf("call order = %v, want it to include adapter.SpawnPlan — a run that never asked for a plan proves nothing about the launcher never being called", order)
	}
}
