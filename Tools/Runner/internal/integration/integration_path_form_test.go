package integration_test

// End-to-end tests for the recorded path form with the real file store: the
// Execution Log Inputs column and the Artifacts registry carry no
// Orchestration-{run_id}/ prefix, while every path sent to a harness stays
// run-scoped, HITL output detection still sees the written outputs, and a run
// resumed from an artifact in the unprefixed form dispatches correctly.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// nativeFormArtifact is an artifact as the native template writes it: blank
// lines around the region tags, unpadded tables and unprefixed paths. agent-a
// has completed and registered design.md.
const nativeFormArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: written-outputs
workflow_version: "1.0"
task: "written outputs task"
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

| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | agent-a#1 | PLANNING | - | 1 | SUCCESS | 2026-01-01T00:00:00Z | designed | - | - |

</ExecutionLog>

<Artifacts type="core">

| Artifact | Created In | Created By |
| --- | --- | --- |
| design.md | PLANNING | agent-a#1 |

</Artifacts>

<WorkflowNotes type="core">

| Seq | Note |
| --- | --- |

</WorkflowNotes>
`

func readArtifactText(t *testing.T, runFolder string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runFolder, "Orchestration.md"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	return string(data)
}

func runPrefixText() string { return domain.RunScopedFolder(integrationRunID) + "/" }

func TestIntegration_PathForm_RecordedInputsAndRegistryCarryNoRunPrefix(t *testing.T) {
	r := newWrittenOutputsRun(t)
	first := okResponse("designed")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "v1")}}
	r.adapter.Queue("agent-a", first)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	state, readErr := r.store.Read(context.Background())
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	if len(state.ArtifactRegistry) != 1 || state.ArtifactRegistry[0].Artifact != "design.md" {
		t.Errorf("registry: want the single unprefixed design.md, got %v", state.ArtifactRegistry)
	}
	var inputs []string
	for _, e := range state.ExecutionLog {
		inputs = append(inputs, e.Inputs)
	}
	if len(inputs) != 2 || inputs[1] != "design.md" {
		t.Errorf("Inputs column: want agent-b's input recorded as design.md, got %q", inputs)
	}
	if text := readArtifactText(t, r.runFolder); strings.Contains(text, runPrefixText()) {
		t.Errorf("artifact still contains %q:\n%s", runPrefixText(), text)
	}
}

func TestIntegration_PathForm_DispatchedPathsStayRunScopedOnce(t *testing.T) {
	r := newWrittenOutputsRun(t)
	first := okResponse("designed")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "v1")}}
	r.adapter.Queue("agent-a", first)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := r.adapter.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want 2 invocations, got %d", len(invs))
	}
	if in := invs[1].Request.InputArtifacts; len(in) != 1 || in[0] != runPath("design.md") {
		t.Errorf("agent-b inputs: want exactly [%s], got %v", runPath("design.md"), in)
	}
	if out := invs[0].Request.OutputArtifacts; len(out) != 1 || out[0] != runPath("design.md") {
		t.Errorf("agent-a outputs: want exactly [%s], got %v", runPath("design.md"), out)
	}
}

func TestIntegration_PathForm_HITLRedispatchStillDetectsWrittenOutput(t *testing.T) {
	r := newWrittenOutputsRun(t)
	first := okResponse("draft")
	first.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("false", "draft")}}
	second := okResponse("final")
	second.Writes = []harness.ScriptedWrite{{Path: runPath("design.md"), Content: approvedDoc("true", "final")}}
	r.adapter.Queue("agent-a", first, second)
	r.adapter.Queue("agent-b", okResponse("reviewed"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	state, readErr := r.store.Read(context.Background())
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	if n := len(r.adapter.Invocations()); n != 3 {
		t.Errorf("want 3 invocations (the false stamp on the written output re-dispatches agent-a), got %d", n)
	}
	if len(state.ArtifactRegistry) != 1 || state.ArtifactRegistry[0].Artifact != "design.md" ||
		state.ArtifactRegistry[0].CreatedBy != "agent-a#2" {
		t.Errorf("registry: want one unprefixed design.md by agent-a#2, got %v", state.ArtifactRegistry)
	}
}

func TestIntegration_PathForm_ResumeFromUnprefixedArtifactDispatchesRunScopedInputs(t *testing.T) {
	r := newWrittenOutputsRun(t)
	artifactPath := filepath.Join(r.runFolder, "Orchestration.md")
	if err := os.WriteFile(artifactPath, []byte(nativeFormArtifact), 0o600); err != nil {
		t.Fatalf("write native-form artifact: %v", err)
	}
	writeRunFileAt(t, r.runFolder, "design.md", approvedDoc("true", "v1"))
	r.adapter.Queue("agent-b", okResponse("reviewed"))
	r.cfg.IsNewRun = false

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := r.adapter.Invocations()
	if len(invs) != 1 || invs[0].Agent.Identifier != "agent-b" {
		t.Fatalf("want a single agent-b invocation after resume, got %d", len(invs))
	}
	if in := invs[0].Request.InputArtifacts; len(in) != 1 || in[0] != runPath("design.md") {
		t.Errorf("agent-b inputs: want exactly [%s], got %v", runPath("design.md"), in)
	}
	text := readArtifactText(t, r.runFolder)
	if strings.Contains(text, runPrefixText()) {
		t.Errorf("artifact written after resume still contains %q:\n%s", runPrefixText(), text)
	}
	if !strings.Contains(text, "| design.md |") {
		t.Errorf("registry row design.md must survive the resume:\n%s", text)
	}
}
