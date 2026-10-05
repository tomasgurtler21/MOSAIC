package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// scriptedSession implements session.Session with scripted responses.
// It records the RunConfig passed to Start so tests can assert flag-to-config mapping.
type scriptedSession struct {
	outcome domain.RunOutcome
	err     error
	called  bool
	config  domain.RunConfig
}

func (s *scriptedSession) Start(_ context.Context, config domain.RunConfig) (domain.RunOutcome, error) {
	s.called = true
	s.config = config
	return s.outcome, s.err
}

func runCLI(t *testing.T, args []string, sess *scriptedSession) (exitCode int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, nil, nil, sess, &out, &errOut)
	return code, out.String(), errOut.String()
}

// spyStore is a test double for domain.ArtifactStore.
// It panics on operations that CLI tests should not trigger (Read, Create, Apply),
// and records every SetPhase call for T5.3 assertions.
type spyStore struct {
	setCalls []spySetPhaseCall
}

type spySetPhaseCall struct {
	state domain.ArtifactState
	phase string
	now   time.Time
}

func (s *spyStore) Read(_ context.Context) (domain.ArtifactState, error) {
	panic("spyStore.Read: unexpected call in CLI tests")
}

func (s *spyStore) Create(_ context.Context, _ domain.WorkflowInfo, _ string, _ domain.RunSettings, _ time.Time, _ string) (domain.ArtifactState, error) {
	panic("spyStore.Create: unexpected call in CLI tests")
}

func (s *spyStore) Apply(_ context.Context, _ domain.ArtifactState, _ domain.CompletedStep) (domain.ArtifactState, error) {
	panic("spyStore.Apply: unexpected call in CLI tests")
}

func (s *spyStore) SetPhase(_ context.Context, state domain.ArtifactState, phase string, now time.Time) (domain.ArtifactState, error) {
	s.setCalls = append(s.setCalls, spySetPhaseCall{state, phase, now})
	state.CurrentState.Phase = phase
	return state, nil
}

func (s *spyStore) SetCommitBranch(_ context.Context, _ string, _ time.Time) (domain.ArtifactState, error) {
	panic("spyStore.SetCommitBranch: unexpected call in CLI tests")
}

func (s *spyStore) AdoptRunnerSettings(_ context.Context, _ domain.ExecutionMode, _, _ bool, _ time.Time) (domain.ArtifactState, error) {
	panic("spyStore.AdoptRunnerSettings: unexpected call in CLI tests")
}

// runCLIWithStore is like runCLI but injects a real store (for T5.3 tests).
func runCLIWithStore(t *testing.T, args []string, store domain.ArtifactStore, sess *scriptedSession) (exitCode int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(context.Background(), args, store, nil, sess, &out, &errOut)
	return code, out.String(), errOut.String()
}

// writeCompletedRunArtifact creates a completed run folder and its Orchestration.md
// inside rootDir. Returns the absolute path to the Orchestration.md file.
func writeCompletedRunArtifact(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderPath := filepath.Join(rootDir, "Orchestration-"+runID)
	if err := os.MkdirAll(folderPath, 0700); err != nil {
		t.Fatalf("writeCompletedRunArtifact: %v", err)
	}
	artifactContent := `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
checkpoints: disabled
current_state:
  phase: COMPLETED
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`
	artifactContent = strings.Replace(artifactContent, "run_id: 20260727T170000Z-a3f9", "run_id: "+runID, 1)
	artifactPath := filepath.Join(folderPath, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("writeCompletedRunArtifact: %v", err)
	}
	return artifactPath
}

const testRunID = "20260727T170000Z-a3f9"

// cliRunID1 and cliRunID2 are run IDs used in default-scan-path tests that need
// more than one resumable candidate in the working directory.
const (
	cliRunID1 = "20260201T000000Z-cccc"
	cliRunID2 = "20260202T000000Z-dddd"
)

// writeResumableRunArtifact creates a resumable (non-COMPLETED) run folder and
// its Orchestration.md inside rootDir. Returns the absolute path of the artifact.
func writeResumableRunArtifact(t *testing.T, rootDir, runID string) string {
	t.Helper()
	folderPath := filepath.Join(rootDir, "Orchestration-"+runID)
	if err := os.MkdirAll(folderPath, 0700); err != nil {
		t.Fatalf("writeResumableRunArtifact: %v", err)
	}
	artifactContent := `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: test-workflow
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
checkpoints: disabled
current_state:
  phase: EXECUTION
  stage: ""
  last_status: SUCCESS
  last_agent: "agent#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent   | Phase     | Stage | Status  | Timestamp            | Summary | Checkpoint |
| --- | ------- | --------- | ----- | ------- | -------------------- | ------- | ---------- |
| 1   | agent#1 | EXECUTION | -     | SUCCESS | 2026-01-01T00:00:00Z | done    | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`
	artifactContent = strings.Replace(artifactContent, "run_id: 20260727T170000Z-a3f9", "run_id: "+runID, 1)
	artifactPath := filepath.Join(folderPath, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(artifactContent), 0600); err != nil {
		t.Fatalf("writeResumableRunArtifact: %v", err)
	}
	return artifactPath
}

// baseHarnessArgs returns the minimum required flags for the run subcommand.
// --mode auto is included because mode is required; tests that exercise mode
// behaviour specifically should not use this helper.
func baseHarnessArgs() []string {
	return []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
	}
}

// newTestCLIHarnessWorkDir creates a temp working directory with the orchestrator
// file at the path expected by the given harness, changes the process working
// directory to it, and registers a cleanup to restore the original directory.
// Must be called from a test function (not a goroutine).
func newTestCLIHarnessWorkDir(t *testing.T, harnessID string) {
	t.Helper()
	entry, ok := commonharness.LookupCLIHarness(harnessID)
	if !ok {
		t.Skipf("newTestCLIHarnessWorkDir: %q is not a CLI harness", harnessID)
		return
	}
	workDir := t.TempDir()
	agentsDirFull := filepath.Join(workDir, entry.AgentsDir)
	if err := os.MkdirAll(agentsDirFull, 0o755); err != nil {
		t.Fatalf("newTestCLIHarnessWorkDir: mkdir %q: %v", agentsDirFull, err)
	}
	orchPath := filepath.Join(agentsDirFull, "orchestrator-script.md")
	if err := os.WriteFile(orchPath, []byte("# orchestrator\n"), 0o644); err != nil {
		t.Fatalf("newTestCLIHarnessWorkDir: write %q: %v", orchPath, err)
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("newTestCLIHarnessWorkDir: getwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("newTestCLIHarnessWorkDir: chdir %q: %v", workDir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

// newStage7BaseArgs returns the minimum required flags for a successful run
// command invocation, explicitly including --mode. Callers that test mode
// behaviour directly should not use this helper.
func newStage7BaseArgs() []string {
	return []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
	}
}

// writeRunArtifactWithRunIDLine creates a resumable run folder for folderRunID
// whose artifact carries exactly runIDLine as its run_id line ("" omits the
// key), so identity problems can be authored directly. Returns the folder path.
func writeRunArtifactWithRunIDLine(t *testing.T, rootDir, folderRunID, runIDLine string) string {
	t.Helper()
	path := writeResumableRunArtifact(t, rootDir, folderRunID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("writeRunArtifactWithRunIDLine: %v", err)
	}
	replacement := runIDLine
	if replacement != "" {
		replacement += "\n"
	}
	edited := strings.Replace(string(data), "run_id: "+folderRunID+"\n", replacement, 1)
	if edited == string(data) {
		t.Fatalf("writeRunArtifactWithRunIDLine: run_id line not found in fixture")
	}
	if err := os.WriteFile(path, []byte(edited), 0600); err != nil {
		t.Fatalf("writeRunArtifactWithRunIDLine: %v", err)
	}
	return filepath.Dir(path)
}
