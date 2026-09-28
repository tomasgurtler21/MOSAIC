package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Incompatible workflows refused per FR-18a (file store) =====

// TestIntegration_IncompatibleWorkflow_RefusedFR18a verifies that each of the
// FR-18a incompatibility conditions causes the session to return RunRefused
// before any harness invocation, with the full stack including the real
// file-based artifact store.
//
// The compat unit tests cover all seven FR-18a conditions individually.
// This integration test verifies that the refusal correctly propagates through
// the full session → compat → RunRefused path for three representative
// conditions: parallel dispatch, staged non-EXECUTION phase, and agent-with-mode
// notation.
func TestIntegration_IncompatibleWorkflow_RefusedFR18a(t *testing.T) {
	cases := []struct {
		name            string
		workflowID      string
		content         string
		wantMsgContains string // substring expected in the refusal message
	}{
		{
			name:       "parallel-dispatch",
			workflowID: "parallel-dispatch",
			// Condition 5: comma in OnSuccess signals parallel routing.
			content: `<Workflow type="core" name="parallel-dispatch" version="1.0">
## Parallel Dispatch Workflow

| Phase | Subagent | HITL | On Success       | Input | Output |
|-------|----------|:----:|------------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | agent-a, agent-b | Stage-{StageNumber}/Plan.md | out.md |
</Workflow>
`,
			wantMsgContains: "parallel",
		},
		{
			name:       "dynamic-stage-set",
			workflowID: "dynamic-stages",
			// Condition 4: an EXECUTION row produces Stage-*/Plan.md (dynamic stage
			// set — implies stages can be added during execution, which is not
			// supported). The output uses the literal wildcard "Stage-*/Plan.md"
			// rather than the template form "Stage-{StageNumber}/Plan.md" to
			// exercise the exact pattern compat checks for.
			content: `<Workflow type="core" name="dynamic-stages" version="1.0">
## Dynamic Stage Set Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | COMPLETE | Stage-{StageNumber}/Plan.md | Stage-*/Plan.md |
</Workflow>
`,
			wantMsgContains: "dynamic",
		},
		{
			name:       "agent-with-mode-notation",
			workflowID: "mode-notation",
			// Condition 6: parentheses in agent identifier.
			content: `<Workflow type="core" name="mode-notation" version="1.0">
## Mode Notation Workflow

| Phase | Subagent       | HITL | On Success | Input | Output |
|-------|----------------|:----:|------------|-------|--------|
| PLANNING | agent-a(mode) | FALSE | COMPLETE | - | out.md |
</Workflow>
`,
			wantMsgContains: "parentheses",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			orchPath := writeOrchFile(t, dir, "orchestrator.md", tc.content)

			// No artifact file written: these runs are refused before the artifact
			// is created (compat check at step 4 runs before artifact creation at
			// step 8 in the run-start sequence).
			artifactPath := filepath.Join(dir, "Orchestration.md")
			f := harness.NewMockAdapter()
			sess := newSession(f, artifactPath)

			cfg := domain.RunConfig{
				RunID: integrationRunID,
				OrchestratorFilePath: orchPath,
				WorkflowID:           domain.WorkflowID(tc.workflowID),
				Task:                 "task",
				IsNewRun:             true,
				RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
			}

			got, err := sess.Start(context.Background(), cfg)
			msg := requireRefused(t, got, err)

			if tc.wantMsgContains != "" && !strings.Contains(msg, tc.wantMsgContains) {
				t.Errorf("want refusal message to contain %q, got %q",
					tc.wantMsgContains, msg)
			}

			// No harness invocations should have been made before the refusal.
			if len(f.Invocations()) > 0 {
				t.Error("want no harness invocations for incompatible workflow refusal, but got some")
			}
		})
	}
}

// ===== Non-canonical existing file refused per FR-7a (file store) =====

// TestIntegration_NonCanonicalArtifact_Refused_FR7a verifies that when a file
// exists at the artifact location but is not in the canonical Orchestration.md
// format (FR-7a), the real file-based artifact store returns a RefusalError and
// the session returns RunRefused without invoking any agents.
//
// This differs from the session unit test (which injects a mock store that
// returns RefusalError directly) by exercising the real FileStore.Read and
// artifact.Parse path against actual non-canonical file bytes.
func TestIntegration_NonCanonicalArtifact_Refused_FR7a(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Write a file at the artifact path that is NOT in the canonical format:
	// no YAML frontmatter, no <SectionName type="..."> tags. The real FileStore calls
	// artifact.Parse which returns *domain.RefusalError for this content.
	runFolder := scopedRunFolder(t, dir)
	artifactPath := filepath.Join(runFolder, "Orchestration.md")
	const nonCanonicalContent = `# Orchestration

This file exists but is not in the canonical orchestration-artifact format.
It has no YAML frontmatter and no SECTION boundary tags.
`
	if err := os.WriteFile(artifactPath, []byte(nonCanonicalContent), 0600); err != nil {
		t.Fatalf("write non-canonical artifact: %v", err)
	}

	f := harness.NewMockAdapter()
	sess := newSession(f, artifactPath)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: non-canonical artifact already written
		RunFolder:            runFolder,
	}

	got, err := sess.Start(context.Background(), cfg)
	msg := requireRefused(t, got, err)

	// The refusal message must describe the non-canonical artifact condition.
	// The artifact.Parse function returns "artifact: refused ...: missing
	// frontmatter ..." which contains both "artifact" and "frontmatter".
	if !strings.Contains(msg, "artifact") && !strings.Contains(msg, "frontmatter") {
		t.Errorf("want refusal message to mention non-canonical artifact condition, got %q", msg)
	}

	// The harness must not have been invoked: refusal happens at the artifact
	// read step (step 3) before any agent is dispatched.
	if len(f.Invocations()) > 0 {
		t.Error("want no harness invocations when artifact is non-canonical, but got some")
	}
}

// ===== Run identity =====

// TestIntegration_Resume_InvalidRunIdentity_RefusedBeforeAnyInvocation verifies,
// through the real file store, that a resume whose artifact has an absent,
// empty, malformed or folder-mismatched run_id is refused before any agent is
// invoked, that the refusal names the problem, and that the artifact on disk
// is left exactly as it was (no replacement identity is minted or written).
func TestIntegration_Resume_InvalidRunIdentity_RefusedBeforeAnyInvocation(t *testing.T) {
	const otherRunID = "20260101T000000Z-abcd"
	tests := []struct {
		name      string
		runIDLine string // "" omits the key
		mentions  string
	}{
		{"run_id key absent", "", "run_id"},
		{"run_id empty", `run_id: ""`, "run_id"},
		{"run_id malformed", "run_id: not-a-run-id", "malformed"},
		{"run_id names a different folder", "run_id: " + otherRunID, otherRunID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			dir := t.TempDir()
			orchPath := copyFile(t, dir, "orchestrator.md",
				filepath.Join(sessionTestdataDir, "linear-orch.md"))
			writeAgentFile(t, dir, "agent-a")
			writeAgentFile(t, dir, "agent-b")
			runFolder := scopedRunFolder(t, dir)
			artifactPath := filepath.Join(runFolder, "Orchestration.md")
			line := ""
			if tc.runIDLine != "" {
				line = tc.runIDLine + "\n"
			}
			content := "---\ntype: orchestration-artifact\n" + line + `workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-a#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
| -------- | ---------- | ---------- |
</Artifacts>
`
			if err := os.WriteFile(artifactPath, []byte(content), 0600); err != nil {
				t.Fatalf("write artifact: %v", err)
			}
			f := harness.NewMockAdapter()
			sess := newSession(f, artifactPath)
			cfg := domain.RunConfig{
				RunID:                integrationRunID,
				OrchestratorFilePath: orchPath,
				WorkflowID:           "linear",
				Task:                 "test task",
				IsNewRun:             false,
				RunFolder:            runFolder,
			}

			// Act
			got, err := sess.Start(context.Background(), cfg)

			// Assert
			msg := requireRefused(t, got, err)
			if !strings.Contains(msg, tc.mentions) {
				t.Errorf("refusal must name the problem (want it to mention %q), got %q", tc.mentions, msg)
			}
			if n := len(f.Invocations()); n != 0 {
				t.Errorf("want no harness invocation before the refusal, got %d", n)
			}
			after, readErr := os.ReadFile(artifactPath)
			if readErr != nil {
				t.Fatalf("read artifact back: %v", readErr)
			}
			if string(after) != content {
				t.Error("artifact on disk changed during a refused resume; no identity may be minted or repaired")
			}
		})
	}
}
