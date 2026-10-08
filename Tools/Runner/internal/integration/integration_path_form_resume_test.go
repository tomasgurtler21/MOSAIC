package integration_test

// Support and regression coverage: resume into a review route-back from an artifact whose registry rows are in
// the recorded (unprefixed) form: every dispatched input list stays run-scoped
// and de-duplicated, and nothing written after the resume carries the prefix.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

const reviewNativeArtifact = `---
type: orchestration-artifact
run_id: 20260727T170000Z-a3f9
workflow: cna-review
workflow_version: "1.0"
task: "review resume task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 1
runner_mode: auto-review
runner_pre_consultation: disabled
runner_manual_resolution: disabled
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "test-writer-tdd#1"
  error_code: null
---

<ExecutionLog type="core">

| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | test-writer-tdd#1 | PLANNING | - | 1 | SUCCESS | 2026-01-01T00:00:00Z | designed | - | - |

</ExecutionLog>

<Artifacts type="core">

| Artifact | Created In | Created By |
| --- | --- | --- |
| design.md | PLANNING | test-writer-tdd#1 |

</Artifacts>

<WorkflowNotes type="core">

| Seq | Note |
| --- | --- |

</WorkflowNotes>
`

const reviewResumeWorkflow = `<Workflow type="core" name="cna-review" version="1.0">
## CNA Review Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | test-writer-tdd | FALSE | build-review | - | - | design.md |
| PLANNING | build-review | FALSE | impl-tdd | test-writer-tdd | design.md | review.md |
| PLANNING | impl-tdd | FALSE | COMPLETE | - | design.md | result.md |
</Workflow>
`

func newReviewResumeRun(t *testing.T) *writtenOutputsRun {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	runFolder := scopedRunFolder(t, dir)
	orchPath := writeOrchFile(t, dir, "orchestrator.md", reviewResumeWorkflow)
	for _, a := range []string{"test-writer-tdd", "build-review", "impl-tdd"} {
		writeAgentFile(t, dir, a)
	}
	f := harness.NewMockAdapter()
	f.SetWriteRoot(dir)
	store := artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md"))
	sess := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Approvals: artifact.NewApprovalReader(),
		Outputs:   artifact.NewOutputWriteDetector(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return &writtenOutputsRun{
		sess: sess, adapter: f, store: store, runFolder: runFolder,
		cfg: domain.RunConfig{
			RunID:                integrationRunID,
			OrchestratorFilePath: orchPath,
			WorkflowID:           "cna-review",
			Task:                 "review resume task",
			IsNewRun:             false,
			RunFolder:            runFolder,
			RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview, ReviewLoopLimit: 3},
		},
	}
}

func TestIntegration_PathForm_ResumeIntoReviewRouteBack_DispatchesRunScopedOnceAndKeepsRecordedForm(t *testing.T) {
	r := newReviewResumeRun(t)
	if err := os.WriteFile(filepath.Join(r.runFolder, "Orchestration.md"), []byte(reviewNativeArtifact), 0o600); err != nil {
		t.Fatalf("write native-form artifact: %v", err)
	}
	writeRunFileAt(t, r.runFolder, "design.md", approvedDoc("true", "v1"))
	findings := harness.ScriptedEntry{
		Response: &domain.ProtocolResponse{StatusCode: domain.StatusCOMPLETED_NEEDS_ACTION, StatusMessage: "fix it"},
		Writes:   []harness.ScriptedWrite{{Path: runPath("review.md"), Content: approvedDoc("true", "findings")}},
	}
	r.adapter.Queue("build-review", findings, okResponse("review ok"))
	r.adapter.Queue("test-writer-tdd", okResponse("fixed"))
	r.adapter.Queue("impl-tdd", okResponse("implemented"))

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := r.adapter.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want build-review, test-writer-tdd, build-review, impl-tdd, got %d invocations", len(invs))
	}
	if in := invs[0].Request.InputArtifacts; len(in) != 1 || in[0] != runPath("design.md") {
		t.Errorf("reviewer inputs from the unprefixed registry: want exactly [%s], got %v", runPath("design.md"), in)
	}
	if in := invs[1].Request.InputArtifacts; len(in) != 1 || in[0] != runPath("review.md") {
		t.Errorf("route-back inputs: want exactly [%s], got %v", runPath("review.md"), in)
	}
	if text := readArtifactText(t, r.runFolder); strings.Contains(text, runPrefixText()) {
		t.Errorf("artifact written after the resumed route-back still contains %q: %s", runPrefixText(), text)
	}
}

// Support coverage: HITL output detection on a run resumed from the recorded
// form. The false stamp on agent-b's written output re-dispatches it, and the
// registry written afterwards keeps both outputs in the recorded form.
func TestIntegration_PathForm_ResumeThenHITLRedispatch_DetectsWrittenOutputAndKeepsRecordedForm(t *testing.T) {
	r := newWrittenOutputsRun(t)
	if err := os.WriteFile(filepath.Join(r.runFolder, "Orchestration.md"), []byte(nativeFormArtifact), 0o600); err != nil {
		t.Fatalf("write native-form artifact: %v", err)
	}
	writeRunFileAt(t, r.runFolder, "design.md", approvedDoc("true", "v1"))
	draft := okResponse("draft")
	draft.Writes = []harness.ScriptedWrite{{Path: runPath("result.md"), Content: approvedDoc("false", "draft")}}
	final := okResponse("final")
	final.Writes = []harness.ScriptedWrite{{Path: runPath("result.md"), Content: approvedDoc("true", "final")}}
	r.adapter.Queue("agent-b", draft, final)
	r.cfg.IsNewRun = false

	got, err := r.sess.Start(context.Background(), r.cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	if n := len(r.adapter.Invocations()); n != 2 {
		t.Errorf("want agent-b dispatched twice (false stamp on its written output), got %d", n)
	}
	state, readErr := r.store.Read(context.Background())
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	forms := map[string]string{}
	for _, e := range state.ArtifactRegistry {
		forms[e.Artifact] = e.CreatedBy
	}
	if len(forms) != 2 || forms["design.md"] != "agent-a#1" || forms["result.md"] != "agent-b#3" {
		t.Errorf("registry: want design.md by agent-a#1 and result.md by agent-b#3 in the recorded form, got %v", state.ArtifactRegistry)
	}
}
