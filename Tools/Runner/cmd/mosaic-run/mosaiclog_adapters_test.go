package main

// mosaiclog_adapters_test.go covers the safety net as applied by buildAdapter:
// which harnesses get it, and that the decorated OpenCode adapter keeps every
// optional capability the composition root discovers by type assertion.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

const mlMissingBinary = "mosaic-test-no-such-binary-xyz"

// mlInvokeFailingHarness builds the adapter for harnessStr over runFolder with
// an executable that cannot launch, pre-creates the invocation folder, runs one
// Invoke (which fails at launch) and returns the folder's event file path.
func mlInvokeFailingHarness(t *testing.T, runFolder, harnessStr string) string {
	t.Helper()
	ws := filepath.Dir(runFolder)
	events := filepath.Join(ws, "OrchestrationLogs", mlRunID, mlAgentInstance, "03_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(events), 0o755); err != nil {
		t.Fatal(err)
	}
	h := buildAdapter(runFolder, harnessStr, mlMissingBinary, "blanket", time.Minute)
	agentFile := filepath.Join(ws, "Research.md")
	if err := os.WriteFile(agentFile, []byte("# Agent: Research\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := h.Invoke(context.Background(),
		domain.AgentReference{Identifier: "Research", DefinitionPath: agentFile, InvocationKind: domain.InvocationOrdinary},
		domain.ProtocolRequest{AgentInstanceID: mlAgentInstance, RunID: mlRunID, TaskDescription: "t"})

	if err == nil {
		t.Fatal("Invoke succeeded, want a launch failure for a missing executable")
	}
	return events
}

func TestBuildAdapter_OpenCodeWithRunFolderAppliesTheSafetyNet(t *testing.T) {
	runFolder, _ := mlRunFolder(t)

	events := mlInvokeFailingHarness(t, runFolder, "opencode")

	evs := mlReadEvents(t, events)
	if len(evs) != 1 || evs[0]["event"] != "invocation_end" {
		t.Fatalf("events = %v, want the safety net's one invocation_end", evs)
	}
}

func TestBuildAdapter_OtherHarnessesGetNoSafetyNet(t *testing.T) {
	for _, harnessStr := range []string{"claude-code", "ghcp-cli"} {
		t.Run(harnessStr, func(t *testing.T) {
			runFolder, _ := mlRunFolder(t)

			events := mlInvokeFailingHarness(t, runFolder, harnessStr)

			if evs := mlReadEvents(t, events); len(evs) != 0 {
				t.Fatalf("events = %v, want none: the safety net applies to OpenCode only", evs)
			}
		})
	}
}

func TestBuildAdapter_OpenCodeWithoutRunFolderGetsNoSafetyNet(t *testing.T) {
	for name, folder := range map[string]string{"empty": "", "not a run folder": filepath.Join(t.TempDir(), "scratch")} {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			events := filepath.Join(ws, "OrchestrationLogs", mlRunID, mlAgentInstance, "03_events.jsonl")
			if err := os.MkdirAll(filepath.Dir(events), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(ws)
			h := buildAdapter(folder, "opencode", mlMissingBinary, "", time.Minute)

			_, err := h.Invoke(context.Background(), domain.AgentReference{Identifier: "Research"},
				domain.ProtocolRequest{AgentInstanceID: mlAgentInstance, RunID: mlRunID})

			if err == nil {
				t.Fatal("Invoke succeeded, want a launch failure")
			}
			if evs := mlReadEvents(t, events); len(evs) != 0 {
				t.Fatalf("events = %v, want none without a resolvable run folder", evs)
			}
		})
	}
}

func TestBuildAdapter_DecoratedOpenCodeKeepsOptionalCapabilities(t *testing.T) {
	runFolder, _ := mlRunFolder(t)

	h := buildAdapter(runFolder, "opencode", mlMissingBinary, "", time.Minute)

	raw, ok := h.(domain.RawInvoker)
	if !ok || raw == nil {
		t.Fatal("decorated OpenCode adapter lost domain.RawInvoker: routing consultation would silently degrade")
	}
	rev, ok := h.(domain.ExecutableRevealer)
	if !ok {
		t.Fatal("decorated OpenCode adapter lost domain.ExecutableRevealer")
	}
	if rev.ExecutablePath() != mlMissingBinary {
		t.Errorf("ExecutablePath = %q, want %q", rev.ExecutablePath(), mlMissingBinary)
	}
	rep, ok := h.(runIDReporter)
	if !ok || rep.RunID() != mlRunID {
		t.Fatalf("runIDReporter = %v (ok=%v), want run id %s", rep, ok, mlRunID)
	}
}

func TestBuildAdapter_DecoratedOpenCodeInvokeRawNeverWritesInvocationEnd(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	events := filepath.Join(ws, "OrchestrationLogs", mlRunID, mlAgentInstance, "03_events.jsonl")
	if err := os.MkdirAll(filepath.Dir(events), 0o755); err != nil {
		t.Fatal(err)
	}
	h := buildAdapter(runFolder, "opencode", mlMissingBinary, "", time.Minute)
	raw := h.(domain.RawInvoker)

	_, err := raw.InvokeRaw(context.Background(),
		domain.AgentReference{Identifier: mlAgentInstance, InvocationKind: domain.InvocationOrchestrator}, []byte("{}"))

	if err == nil {
		t.Fatal("InvokeRaw succeeded, want a launch failure")
	}
	if evs := mlReadEvents(t, events); len(evs) != 0 {
		t.Fatalf("events = %v, want none: raw calls are not invocations", evs)
	}
}
