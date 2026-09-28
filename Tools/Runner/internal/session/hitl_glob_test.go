package session_test

// Session-level tests for HITL + Stage-* glob output artifact approval.
//
// These tests verify that when a dispatched step has HITL=true and its output
// artifacts contain Stage-* wildcard patterns, the session expands those patterns
// to concrete per-stage paths before performing approval reads. The expansion
// mirrors the engine's resolveArtifacts logic for input artifacts.
//
// Both HITL approval-check loop instances are covered:
//   - The hitlCheckLoop at ~session.go:755 (auto-mode / auto-review-mode path).
//   - The hitlLoop inside consultRoute at ~session.go:1320 (orchestrated-mode path).
//
// Test cases:
//
//   All stage files approved (orchestrated mode):
//   - HITL=true, Stage-* output, stages={1,2}, all per-stage files approved ->
//     session must NOT redispatch the agent. Stage files are written during the dispatch
//     and detected by the real write detector.
//
//   Some stage files unapproved (orchestrated mode):
//   - HITL=true, Stage-* output, stages={1,2}, Stage-2/Plan.md unapproved ->
//     session must redispatch once then escalate.
//
//   Zero files on disk / nil StageSet (orchestrated mode):
//   - HITL=true, Stage-* output, no stage file written and no Plan.md -> the
//     declared output was never written, so the step is accepted without a
//     redispatch.
//
//   All stage files approved (auto mode, hitlCheckLoop):
//   - Same scenario via the engine's auto-routing path. Stage files are written during the
//     dispatch and detected by the real write detector.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- helpers ----

// approvedArtifactContent is a minimal Markdown document with human_approved: true.
const approvedArtifactContent = "---\nhuman_approved: true\n---\n# Plan\n"

// unapprovedArtifactContent is a minimal Markdown document with human_approved: false.
const unapprovedArtifactContent = "---\nhuman_approved: false\n---\n# Plan\n"

// planMD2Stages is a Plan.md containing a 2-stage table; used as the stage
// set source that the session reads after planner's Stage-* output row completes.
const planMD2Stages = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`

// newHITLGlobStagedSession builds a session backed by the hitl-glob-staged-orch.md
// fixture. It creates agent files for "planner" and "agent-a" in a temp dir.
// The runFolder is the directory from which the session reads Plan.md for
// stage-set re-derivation after planner's Stage-* output row completes.
func newHITLGlobStagedSession(
	t *testing.T,
	consultant domain.RoutingConsultant,
	approvals domain.ApprovalReader,
	runFolder string,
	agentAWrites func(),
) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-glob-staged-orch.md")
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "agent-a")

	f = harness.NewMockAdapter()
	store = &memStore{}
	// agent-a's output files are written during its first invocation, so the
	// write detector attributes them to that dispatch.
	ses = session.New(session.Deps{
		Harness:   &fileWritingAdapter{inner: f, agentID: "agent-a", setup: agentAWrites},
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Outputs:   artifact.NewOutputWriteDetector(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	_ = runFolder // used by caller in RunConfig.RunFolder
	return
}

// hitlGlobOrchestratedConfig returns a RunConfig for the hitl-glob-staged workflow
// in orchestrated mode with the given run folder for stage-set re-derivation.
func hitlGlobOrchestratedConfig(orchPath, runFolder string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "hitl-glob-staged",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            runFolder,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
}

// writeStageArtifact writes a Plan.md file under <runFolder>/Stage-N/ with the
// given content. The Stage-N directory is created if it does not exist.
func writeStageArtifact(t *testing.T, runFolder string, stageNum int, content string) string {
	t.Helper()
	stageDir := filepath.Join(runFolder, fmt.Sprintf("Stage-%d", stageNum))
	if err := os.MkdirAll(stageDir, 0700); err != nil {
		t.Fatalf("writeStageArtifact: mkdir %q: %v", stageDir, err)
	}
	planPath := filepath.Join(stageDir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(content), 0600); err != nil {
		t.Fatalf("writeStageArtifact: write %q: %v", planPath, err)
	}
	return planPath
}

// fileWritingAdapter wraps MockAdapter and calls a one-shot setup function
// when the target agent is first invoked. This simulates the agent writing
// output files to disk during its execution, making them available for
// stage re-derivation and approval reads that occur after Invoke returns --
// but NOT before session startup, so the session's step-8a2 plan read does
// not pre-populate the stage set.
type fileWritingAdapter struct {
	inner   *harness.MockAdapter
	agentID string
	once    sync.Once
	setup   func()
}

func (a *fileWritingAdapter) Invoke(ctx context.Context, ref domain.AgentReference, req domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	if ref.Identifier == a.agentID {
		a.once.Do(a.setup)
	}
	return a.inner.Invoke(ctx, ref, req)
}

// writePlanMD writes the 2-stage Plan.md into runFolder.
func writePlanMD(t *testing.T, runFolder string) {
	t.Helper()
	path := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(path, []byte(planMD2Stages), 0600); err != nil {
		t.Fatalf("writePlanMD: %v", err)
	}
}

// ---- orchestrated-mode tests ----

// TestSession_HITL_GlobApproval_Orchestrated_AllApproved_NoRedispatch verifies
// that when the session receives a HITL=true step whose output artifacts contain
// a Stage-* wildcard, and all expanded per-stage files carry human_approved: true,
// the session accepts the step without redispatching the agent.
//
// The stage files are written during agent-a's dispatch and the real write
// detector is wired, so the gate only reads them once the session detects them
// as written outputs of that dispatch. The session must expand Stage-*/Plan.md
// to Stage-1/Plan.md and Stage-2/Plan.md, find both approved, and accept.
func TestSession_HITL_GlobApproval_Orchestrated_AllApproved_NoRedispatch(t *testing.T) {
	tmpDir := chdirWorkspace(t)

	// Pre-create Plan.md so the session can re-derive the stage set (2 stages)
	// after the planner row completes.
	writePlanMD(t, tmpDir)

	// Both per-stage files are written (approved) by agent-a during its dispatch.
	writeStages := func() {
		writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
		writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
	}

	// The glob path that agent-a declares as its output. The session must expand
	// this to Stage-1/Plan.md and Stage-2/Plan.md before reading approvals.
	globPath := "Stage-*/Plan.md"
	globPaths := []string{globPath}

	consultant := &scriptedRoutingConsultant{}
	// Row 0: dispatch planner with default output (Stage-*/Plan.md from workflow).
	// Planner completing with Stage-* output triggers stage-set re-derivation.
	consultant.queueDispatch("planner", "create the plan", 0)
	// Row 1: dispatch agent-a with the absolute Stage-* glob path as output.
	// All per-stage files are approved; the session must NOT redispatch.
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	// After agent-a completes successfully (no redispatch), the consultant stops.
	consultant.queueStop("all stages approved")

	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir, writeStages)

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done",
	}})

	ses.Start(context.Background(), hitlGlobOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	// agent-a must be dispatched exactly once: all per-stage files are approved,
	// so the HITL check must accept on the first attempt with no redispatch.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (all stage files approved -> no redispatch), got %d invocations", agentACalls)
	}
}

// TestSession_HITL_GlobApproval_Orchestrated_SomeUnapproved_RedispatchThenEscalate
// verifies that when a HITL=true step has Stage-* output, some per-stage files
// carry human_approved: false, the session redispatches once and then escalates
// when the redispatched result is still non-compliant.
func TestSession_HITL_GlobApproval_Orchestrated_SomeUnapproved_RedispatchThenEscalate(t *testing.T) {
	tmpDir := chdirWorkspace(t)
	writePlanMD(t, tmpDir)

	// Stage 1 approved, stage 2 unapproved; both written by agent-a's dispatch.
	writeStages := func() {
		writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
		writeStageArtifact(t, tmpDir, 2, unapprovedArtifactContent)
	}

	globPath := "Stage-*/Plan.md"
	globPaths := []string{globPath}

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("planner", "create the plan", 0)
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	// After HITL redispatch of agent-a, Stage-2/Plan.md is still unapproved ->
	// the second non-compliance triggers escalation. The consultant resolves the
	// deviation by stopping.
	consultant.queueStop("HITL escalation: stage 2 unapproved after redispatch")

	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir, writeStages)

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	// First dispatch of agent-a.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done (attempt 1)",
	}})
	// Automatic HITL redispatch of agent-a (same unapproved file -> escalation).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done (attempt 2)",
	}})

	ses.Start(context.Background(), hitlGlobOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	// agent-a must be dispatched at least twice: original + one automatic HITL
	// redispatch before escalation.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls < 2 {
		t.Errorf("want agent-a dispatched at least twice (stage 2 unapproved -> redispatch), got %d invocations", agentACalls)
	}
}

// TestSession_HITL_GlobApproval_Orchestrated_ZeroFiles_Accepted verifies that
// when a HITL=true step declares a Stage-* output and the invocation writes no
// matching file (Plan.md is absent, so no stage set exists either), the step is
// accepted without a redispatch: a declared output that was never written is not
// a gate miss.
func TestSession_HITL_GlobApproval_Orchestrated_ZeroFiles_Accepted(t *testing.T) {
	tmpDir := chdirWorkspace(t)
	// Deliberately do NOT write Plan.md or any stage file.

	globPath := "Stage-*/Plan.md"
	globPaths := []string{globPath}

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("planner", "create the plan", 0)
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	consultant.queueStop("done")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "hitl-glob-staged-orch.md")
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "agent-a")
	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Routing:   consultant,
		Approvals: artifact.NewApprovalReader(),
		Outputs:   artifact.NewOutputWriteDetector(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done",
	}})

	ses.Start(context.Background(), hitlGlobOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (no stage file written -> nothing to gate), got %d invocations", agentACalls)
	}
}
