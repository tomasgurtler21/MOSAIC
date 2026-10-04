package main

import (
	"os"
	"path/filepath"
	"testing"

	commonharness "mosaic-common/harness"
)

// mlDeployEmptyOrchestrator creates the harness's orchestrator-script file (empty)
// under ws so CLI discovery succeeds and the session itself refuses the run
// before any invocation.
func mlDeployEmptyOrchestrator(t *testing.T, ws, harnessID string) {
	t.Helper()
	entry, ok := commonharness.LookupCLIHarness(harnessID)
	if !ok {
		t.Fatalf("unknown CLI harness %s", harnessID)
	}
	path := filepath.Join(ws, entry.AgentsDir, "orchestrator-script.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCLIWiring_RunLogLivesUnderTheWorkingDirectory drives the real CLI
// composition path in a temp working directory. The session refuses the run
// before any invocation (the orchestrator file is empty), so no harness is spawned.
func TestCLIWiring_RunLogLivesUnderTheWorkingDirectory(t *testing.T) {
	ws := t.TempDir()
	t.Chdir(ws)
	mlDeployEmptyOrchestrator(t, ws, "opencode")
	args := []string{
		"run", "--harness", "opencode", "--executable-path", filepath.Join(ws, "no-such-opencode"),
		"--new-run", "--mode", "orchestrated", "--workflow", "no-such-workflow", "--task", "t",
		"--review-loop-limit", "none", "--checkpoints", "disabled", "--commits", "disabled",
	}

	code := runCLIMode(args, args)

	if code == 0 {
		t.Fatalf("exit code = 0, want a refusal exit code")
	}
	matches, _ := filepath.Glob(filepath.Join(ws, "OrchestrationLogs", "*", "00_orchestrator_events.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("run logs under the working directory = %v, want exactly one", matches)
	}
	events := mlReadEvents(t, matches[0])
	mlAssertPairs(t, events, 1)
	runID := filepath.Base(filepath.Dir(matches[0]))
	if events[0]["run_id"] != runID || events[0]["cwd"] != ws {
		t.Errorf("run_start = %v, want run_id %s and cwd %q", events[0], runID, ws)
	}
	if events[1]["outcome"] != "aborted" {
		t.Errorf("outcome = %v, want aborted for a refusal", events[1]["outcome"])
	}
}
