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
//     session must NOT redispatch the agent. Currently fails (RED) because the
//     session reads the literal Stage-*/Plan.md path (which is always missing)
//     rather than the expanded per-stage paths.
//
//   Some stage files unapproved (orchestrated mode):
//   - HITL=true, Stage-* output, stages={1,2}, Stage-2/Plan.md unapproved ->
//     session must redispatch once then escalate.
//
//   Zero files on disk / nil StageSet (orchestrated mode):
//   - HITL=true, Stage-* output, stages=nil (Plan.md absent -> re-derivation fails)
//     -> session must treat zero expansion as non-compliant and trigger a redispatch.
//
//   All stage files approved (auto mode, hitlCheckLoop):
//   - Same scenario via the engine's auto-routing path. Currently fails (RED) because
//     the hitlCheckLoop also does not expand Stage-* before approval reads.

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
) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-glob-staged-orch.md")
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "agent-a")

	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
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

// fileWritingAdapter wraps FakeAdapter and calls a one-shot setup function
// when the target agent is first invoked. This simulates the agent writing
// output files to disk during its execution, making them available for
// stage re-derivation and approval reads that occur after Invoke returns —
// but NOT before session startup, so the session's step-8a2 plan read does
// not pre-populate the stage set.
type fileWritingAdapter struct {
	inner   *harness.FakeAdapter
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
// This test is in the RED phase: without Stage-* expansion in the hitlLoop inside
// consultRoute, the session reads the literal "Stage-*/Plan.md" path, which is
// always absent from disk, and incorrectly triggers a redispatch. The fix must
// expand Stage-*/Plan.md to Stage-1/Plan.md and Stage-2/Plan.md before reading
// approvals, so that both approved files are found and the step is accepted.
func TestSession_HITL_GlobApproval_Orchestrated_AllApproved_NoRedispatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create Plan.md so the session can re-derive the stage set (2 stages)
	// after the planner row completes.
	writePlanMD(t, tmpDir)

	// Pre-create per-stage files, both approved.
	writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
	writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)

	// The glob path that agent-a declares as its output. The session must expand
	// this to Stage-1/Plan.md and Stage-2/Plan.md before reading approvals.
	globPath := filepath.Join(tmpDir, "Stage-*/Plan.md")
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

	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir)

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
	tmpDir := t.TempDir()
	writePlanMD(t, tmpDir)

	// Stage 1 approved, stage 2 unapproved.
	writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
	writeStageArtifact(t, tmpDir, 2, unapprovedArtifactContent)

	globPath := filepath.Join(tmpDir, "Stage-*/Plan.md")
	globPaths := []string{globPath}

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("planner", "create the plan", 0)
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	// After HITL redispatch of agent-a, Stage-2/Plan.md is still unapproved ->
	// the second non-compliance triggers escalation. The consultant resolves the
	// deviation by stopping.
	consultant.queueStop("HITL escalation: stage 2 unapproved after redispatch")

	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir)

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

// TestSession_HITL_GlobApproval_Orchestrated_ZeroFiles_NonCompliant verifies
// that when a HITL=true step has Stage-* output artifacts but the stage set could
// not be derived (Plan.md absent -> stages is nil), the session treats the
// zero-expansion result as non-compliant and triggers a redispatch rather than
// silently accepting the step.
//
// This exercises the zero-match handling: when expandStageGlobs returns the glob
// path unchanged (because stages is nil), the HITL loop must synthesize an
// ApprovalFileMissing entry so DecideHITLCompliance sees non-compliance instead
// of hitting the len(Approvals)==0 -> HITLAccept short-circuit.
func TestSession_HITL_GlobApproval_Orchestrated_ZeroFiles_NonCompliant(t *testing.T) {
	tmpDir := t.TempDir()
	// Deliberately do NOT write Plan.md: stage re-derivation will fail, leaving
	// stages nil when agent-a's HITL check runs.

	globPath := filepath.Join(tmpDir, "Stage-*/Plan.md")
	globPaths := []string{globPath}

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("planner", "create the plan", 0)
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 1, &globPaths)
	// After HITL non-compliance (zero expansion, no stage files) -> redispatch ->
	// still non-compliant -> escalation. Consultant resolves by stopping.
	consultant.queueStop("HITL escalation: no stage files found")

	ses, f, _, orchPath := newHITLGlobStagedSession(t, consultant, artifact.NewApprovalReader(), tmpDir)

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done (attempt 1)",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "work done (attempt 2)",
	}})

	ses.Start(context.Background(), hitlGlobOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	// agent-a must be dispatched at least twice: the nil-stages case must produce
	// non-compliance (not silent acceptance), triggering at least one redispatch.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls < 2 {
		t.Errorf("want agent-a dispatched at least twice (nil stages -> zero-match non-compliance -> redispatch), got %d invocations", agentACalls)
	}
}

// ---- orchestrated-mode self-referential tests (single-row, no prior stage row) ----
//
// These tests verify the scenario where a single workflow row has both HITL=TRUE
// and Stage-* output artifacts, with no preceding row to establish a stage set.
// The session must derive the stage set from Plan.md (written by the agent as
// output) before the HITL check, not just rely on the post-loop re-derivation.
//
// Design note: Plan.md and stage files are written to disk during planner's
// Invoke call (via fileWritingAdapter) rather than before ses.Start. This
// prevents the session's step-8a2 plan read from pre-populating the stage set
// at startup, accurately modeling the real scenario where the agent creates
// Plan.md as part of its work and the file does not exist before the run begins.

// hitlGlobSelfRefOrchestratedConfig returns a RunConfig for the
// hitl-glob-selfref workflow in orchestrated mode.
func hitlGlobSelfRefOrchestratedConfig(orchPath, runFolder string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "hitl-glob-selfref",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            runFolder,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
}

// TestSession_HITL_GlobApproval_SelfRef_Orchestrated_AllApproved_NoRedispatch
// verifies that the orchestrated-mode HITL compliance loop (hitlLoop inside
// consultRoute) correctly handles the self-referential scenario: a single
// workflow row where the same agent has HITL=TRUE and produces Stage-* output,
// with no preceding row that establishes a stage set.
//
// The session must derive the stage set from Plan.md before the HITL check so
// that expandStageGlobs receives a non-nil stage set and produces expanded
// paths (Stage-1/Plan.md, Stage-2/Plan.md). When all expanded files carry
// human_approved: true the step must be accepted without redispatch.
//
// RED phase: without the pre-loop stage re-derivation, stages is nil at HITL
// check time, expandStageGlobs passes through the literal Stage-*/Plan.md path,
// the approval read fails (no such literal file on disk), the step is
// non-compliant, and planner is redispatched. The test fails because
// plannerCalls != 1.
func TestSession_HITL_GlobApproval_SelfRef_Orchestrated_AllApproved_NoRedispatch(t *testing.T) {
	// tmpDir starts empty: Plan.md is absent at session startup so the session's
	// step-8a2 plan read does not pre-populate the stage set. Plan.md and stage
	// files are written to disk when planner is invoked (via fileWritingAdapter),
	// simulating the agent creating them as output.
	tmpDir := t.TempDir()

	globPath := filepath.Join(tmpDir, "Stage-*/Plan.md")
	globPaths := []string{globPath}

	inner := harness.NewFakeAdapter()
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	// Second entry consumed only in RED (broken code) when HITL non-compliance
	// triggers an automatic redispatch before escalation.
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan rechecked (HITL redispatch, broken code only)",
	}})

	fa := &fileWritingAdapter{
		inner:   inner,
		agentID: "planner",
		setup: func() {
			// Write Plan.md and stage files on first planner invoke.
			// After this, Plan.md is available for pre-loop stage re-derivation,
			// and Stage-1/Plan.md, Stage-2/Plan.md are available for approval reads.
			writePlanMD(t, tmpDir)
			writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
			writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
		},
	}

	consultant := &scriptedRoutingConsultant{}
	// Row 0: dispatch planner with Stage-* output. No prior row establishes a
	// stage set; stages is nil at HITL check time in the current code.
	// All per-stage files are approved; the session must NOT redispatch.
	consultant.queueDispatchWithOutputs("planner", "create the plan", 0, &globPaths)
	// After planner completes with HITL accepted (no redispatch), the consultant stops.
	consultant.queueStop("planner HITL accepted, single-row workflow done")

	orchDir := t.TempDir()
	orchPath := copyOrchestratorFile(t, orchDir, "hitl-glob-selfref-orch.md")
	writeAgentFile(t, orchDir, "planner")

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   fa,
		Store:     store,
		Routing:   consultant,
		Approvals: artifact.NewApprovalReader(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	ses.Start(context.Background(), hitlGlobSelfRefOrchestratedConfig(orchPath, tmpDir)) //nolint:errcheck

	// planner must be dispatched exactly once: the session derives the stage set
	// from Plan.md (written by planner's first invoke) before the HITL check,
	// finds all stage files approved, and accepts without triggering a redispatch.
	plannerCalls := 0
	for _, inv := range inner.Invocations() {
		if inv.Agent.Identifier == "planner" {
			plannerCalls++
		}
	}
	if plannerCalls != 1 {
		t.Errorf("want planner dispatched exactly once (self-referential HITL, all stage files approved -> no redispatch), got %d invocations", plannerCalls)
	}
}

// TestSession_HITL_GlobApproval_SelfRef_Downstream_AllApproved_PlanReviewDispatched
// verifies that when a self-referential HITL+Stage-* row (planner, row 0) is
// followed by a downstream consumer that takes Stage-*/Plan.md as an input
// artifact (plan-review, row 1), the run completes via the intended path:
// planner dispatched once (HITL passes cleanly), then plan-review dispatched once.
//
// RED phase: without the pre-loop stage re-derivation, planner's HITL check
// fails (stages nil -> literal path read -> non-compliant), triggering an
// automatic redispatch. After the redispatch also fails, HITL escalates and
// the consultant's instruction to dispatch plan-review is consumed during the
// escalation rather than the intended post-success routing. The test fails
// because plannerCalls != 1, showing that the first row was not accepted
// cleanly and the downstream consumer was reached via the wrong path.
func TestSession_HITL_GlobApproval_SelfRef_Downstream_AllApproved_PlanReviewDispatched(t *testing.T) {
	// tmpDir starts empty: Plan.md is absent at session startup.
	tmpDir := t.TempDir()

	globPath := filepath.Join(tmpDir, "Stage-*/Plan.md")
	plannerOutputPaths := []string{globPath}
	reviewInputPaths := []string{globPath}

	inner := harness.NewFakeAdapter()
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	// Second planner entry consumed only in RED when HITL non-compliance
	// triggers an automatic redispatch.
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan rechecked (HITL redispatch, broken code only)",
	}})
	inner.Queue("plan-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "plan-review#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	fa := &fileWritingAdapter{
		inner:   inner,
		agentID: "planner",
		setup: func() {
			writePlanMD(t, tmpDir)
			writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
			writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
		},
	}

	consultant := &scriptedRoutingConsultant{}
	// Row 0: dispatch planner with Stage-* output (self-referential HITL).
	consultant.queueDispatchWithOutputs("planner", "create the plan", 0, &plannerOutputPaths)
	// Row 1: dispatch plan-review with Stage-* input (downstream consumer).
	// In GREEN: dispatched normally after planner's HITL passes on first attempt.
	// In RED: dispatched via HITL escalation (planner's HITL never passed cleanly).
	consultant.queueDispatchWithInputs("plan-review", "review the plan", 1, &reviewInputPaths)
	consultant.queueStop("downstream workflow complete")

	orchDir := t.TempDir()
	orchPath := copyOrchestratorFile(t, orchDir, "hitl-glob-selfref-downstream-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "plan-review")

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   fa,
		Store:     store,
		Routing:   consultant,
		Approvals: artifact.NewApprovalReader(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "hitl-glob-selfref-downstream",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            tmpDir,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	plannerCalls := 0
	reviewCalls := 0
	for _, inv := range inner.Invocations() {
		switch inv.Agent.Identifier {
		case "planner":
			plannerCalls++
		case "plan-review":
			reviewCalls++
		}
	}
	// planner must be dispatched exactly once: the pre-loop stage re-derivation
	// populates the stage set (from Plan.md written by planner's invoke) so the
	// HITL check accepts without a redispatch.
	if plannerCalls != 1 {
		t.Errorf("want planner dispatched exactly once (self-referential HITL passes -> no redispatch before plan-review), got %d invocations", plannerCalls)
	}
	// plan-review must be dispatched exactly once: it executes via the intended
	// post-success routing, not via HITL escalation.
	if reviewCalls != 1 {
		t.Errorf("want plan-review dispatched exactly once (downstream consumer after planner HITL passes), got %d invocations", reviewCalls)
	}
}

// ---- auto-mode test (hitlCheckLoop) ----

// hitlGlobSelfRefAutoContent builds an inline single-row workflow definition
// where planner has HITL=TRUE and Stage-*/Plan.md as output, using the provided
// absolute runFolder path. No preceding row establishes a stage set.
func hitlGlobSelfRefAutoContent(runFolder string) string {
	return fmt.Sprintf(`<Workflow type="core" name="hitl-glob-selfref-auto" version="1.0">
## HITL Glob Self-Referential Auto Workflow (inline)

Used by auto-mode self-referential HITL+Stage-* tests to verify that the
hitlCheckLoop derives the stage set before performing approval reads when the
dispatched row is both the Stage-* producer and the HITL subject.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | TRUE | COMPLETE | - | - | %s/Stage-*/Plan.md |
</Workflow>
`, runFolder)
}

// TestSession_HITL_GlobApproval_SelfRef_Auto_AllApproved_NoRedispatch verifies
// that the auto-mode HITL check loop (hitlCheckLoop) handles the self-referential
// scenario: a single workflow row where planner has HITL=TRUE and Stage-* output,
// with no preceding row that establishes a stage set.
//
// The session must derive the stage set from Plan.md before the hitlCheckLoop so
// that expandStageGlobs receives a non-nil stage set and produces expanded paths.
// When all expanded files carry human_approved: true the step must be accepted
// without redispatch.
//
// RED phase: without the pre-loop stage re-derivation, stages is nil at
// hitlCheckLoop time, expandStageGlobs passes through the literal path, the
// approval read fails, the step is non-compliant, and planner is redispatched.
// The test fails because plannerCalls != 1.
//
// Design note: tmpDir starts empty so the session's step-8a2 plan read at
// startup does not pre-populate the stage set. Plan.md and stage files are
// written during planner's first Invoke, simulating the agent creating them.
func TestSession_HITL_GlobApproval_SelfRef_Auto_AllApproved_NoRedispatch(t *testing.T) {
	// tmpDir starts empty: no Plan.md at session startup.
	tmpDir := t.TempDir()

	// Write the inline single-row orchestrator with absolute Stage-* output path.
	orchDir := t.TempDir()
	orchContent := hitlGlobSelfRefAutoContent(tmpDir)
	orchPath := filepath.Join(orchDir, "orchestrator.md")
	if err := os.WriteFile(orchPath, []byte(orchContent), 0600); err != nil {
		t.Fatalf("write inline orchestrator: %v", err)
	}
	writeAgentFile(t, orchDir, "planner")

	inner := harness.NewFakeAdapter()
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan created",
	}})
	// Second entry consumed only in RED when HITL non-compliance triggers an
	// automatic redispatch before escalation.
	inner.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan rechecked (HITL redispatch, broken code only)",
	}})

	fa := &fileWritingAdapter{
		inner:   inner,
		agentID: "planner",
		setup: func() {
			writePlanMD(t, tmpDir)
			writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
			writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
		},
	}

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   fa,
		Store:     store,
		Approvals: artifact.NewApprovalReader(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		// No Routing: auto-mode uses engine routing.
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "hitl-glob-selfref-auto",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            tmpDir,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// planner must be dispatched exactly once: the hitlCheckLoop must derive the
	// stage set from Plan.md (written by planner's first invoke) before performing
	// approval reads on Stage-* output artifacts, and accept without redispatch.
	plannerCalls := 0
	for _, inv := range inner.Invocations() {
		if inv.Agent.Identifier == "planner" {
			plannerCalls++
		}
	}
	if plannerCalls != 1 {
		t.Errorf("want planner dispatched exactly once (auto mode, self-referential HITL, all stage files approved -> no redispatch), got %d invocations", plannerCalls)
	}
}

// hitlGlobAutoOrchestratorContent builds an inline workflow definition where the
// planner and agent-a rows use the provided absolute runFolder path in their
// output artifact column. This lets the auto-mode test use real absolute file
// paths without requiring a static fixture with hard-coded paths.
func hitlGlobAutoOrchestratorContent(runFolder string) string {
	return fmt.Sprintf(`<Workflow type="core" name="hitl-glob-auto" version="1.0">
## HITL Glob Auto Workflow (inline)

Used by auto-mode HITL+glob tests to verify the hitlCheckLoop path.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | agent-a | - | - | %s/Stage-*/Plan.md |
| PLANNING | agent-a | TRUE | COMPLETE | - | - | %s/Stage-*/Plan.md |
</Workflow>
`, runFolder, runFolder)
}

// TestSession_HITL_GlobApproval_Auto_AllApproved_NoRedispatch verifies that
// the auto-mode HITL check loop (hitlCheckLoop) also expands Stage-* output
// artifact patterns before reading approvals. When all expanded per-stage files
// are approved the agent must be dispatched exactly once.
//
// This test is in the RED phase: the hitlCheckLoop at ~session.go:755 does not
// currently expand Stage-* before approval reads, so it reads the literal path
// (which always fails) and incorrectly redispatches.
func TestSession_HITL_GlobApproval_Auto_AllApproved_NoRedispatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create Plan.md for stage-set re-derivation (2 stages).
	writePlanMD(t, tmpDir)

	// Pre-create per-stage files, both approved.
	writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
	writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)

	// Write the orchestrator file inline with absolute Stage-* output paths.
	orchDir := t.TempDir()
	orchContent := hitlGlobAutoOrchestratorContent(tmpDir)
	orchPath := filepath.Join(orchDir, "orchestrator.md")
	if err := os.WriteFile(orchPath, []byte(orchContent), 0600); err != nil {
		t.Fatalf("write inline orchestrator: %v", err)
	}
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "agent-a")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Approvals: artifact.NewApprovalReader(),
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		// No Routing: auto-mode uses engine routing.
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "hitl-glob-auto",
		Task:                 "test task",
		IsNewRun:             true,
		RunFolder:            tmpDir,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// agent-a must be dispatched exactly once: all per-stage files are approved,
	// so the hitlCheckLoop must expand Stage-* and accept on the first attempt.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (auto mode, all stage files approved -> no redispatch), got %d invocations", agentACalls)
	}
}
