package session_test

// Self-referential and auto-mode HITL+Stage-* glob approval tests
// (split from hitl_glob_test.go).
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

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

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
// The real write detector is wired; stage files are written during planner's
// dispatch and gated once the stage set is derived from Plan.md.
func TestSession_HITL_GlobApproval_SelfRef_Orchestrated_AllApproved_NoRedispatch(t *testing.T) {
	// tmpDir starts empty: Plan.md is absent at session startup so the session's
	// step-8a2 plan read does not pre-populate the stage set. Plan.md and stage
	// files are written to disk when planner is invoked (via fileWritingAdapter),
	// simulating the agent creating them as output.
	tmpDir := chdirWorkspace(t)

	globPath := "Stage-*/Plan.md"
	globPaths := []string{globPath}

	inner := harness.NewMockAdapter()
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
		Outputs:   artifact.NewOutputWriteDetector(),
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
// The real write detector is wired; stage files are written during planner's
// dispatch and gated once the stage set is derived from Plan.md.
func TestSession_HITL_GlobApproval_SelfRef_Downstream_AllApproved_PlanReviewDispatched(t *testing.T) {
	// tmpDir starts empty: Plan.md is absent at session startup.
	tmpDir := chdirWorkspace(t)

	globPath := "Stage-*/Plan.md"
	plannerOutputPaths := []string{globPath}
	reviewInputPaths := []string{globPath}

	inner := harness.NewMockAdapter()
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
		Outputs:   artifact.NewOutputWriteDetector(),
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

// ---- auto-mode tests (hitlCheckLoop) ----

// hitlGlobSelfRefAutoContent builds an inline single-row workflow definition
// where planner has HITL=TRUE and Stage-*/Plan.md as output, using the provided
// absolute runFolder path. No preceding row establishes a stage set.
func hitlGlobSelfRefAutoContent() string {
	return (`<Workflow type="core" name="hitl-glob-selfref-auto" version="1.0">
## HITL Glob Self-Referential Auto Workflow (inline)

Used by auto-mode self-referential HITL+Stage-* tests to verify that the
hitlCheckLoop derives the stage set before performing approval reads when the
dispatched row is both the Stage-* producer and the HITL subject.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | TRUE | COMPLETE | - | - | Stage-*/Plan.md |
</Workflow>
`)
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
// The real write detector is wired; stage files are written during planner's
// dispatch and gated once the stage set is derived from Plan.md.
func TestSession_HITL_GlobApproval_SelfRef_Auto_AllApproved_NoRedispatch(t *testing.T) {
	// tmpDir starts empty: no Plan.md at session startup.
	tmpDir := chdirWorkspace(t)

	// Write the inline single-row orchestrator with absolute Stage-* output path.
	orchDir := t.TempDir()
	orchContent := hitlGlobSelfRefAutoContent()
	orchPath := filepath.Join(orchDir, "orchestrator.md")
	if err := os.WriteFile(orchPath, []byte(orchContent), 0600); err != nil {
		t.Fatalf("write inline orchestrator: %v", err)
	}
	writeAgentFile(t, orchDir, "planner")

	inner := harness.NewMockAdapter()
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
		Outputs:   artifact.NewOutputWriteDetector(),
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
func hitlGlobAutoOrchestratorContent() string {
	return (`<Workflow type="core" name="hitl-glob-auto" version="1.0">
## HITL Glob Auto Workflow (inline)

Used by auto-mode HITL+glob tests to verify the hitlCheckLoop path.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | agent-a | - | - | Stage-*/Plan.md |
| PLANNING | agent-a | TRUE | COMPLETE | - | - | Stage-*/Plan.md |
</Workflow>
`)
}

// TestSession_HITL_GlobApproval_Auto_AllApproved_NoRedispatch verifies that
// the auto-mode HITL check loop (hitlCheckLoop) also expands Stage-* output
// artifact patterns before reading approvals. When all expanded per-stage files
// are approved the agent must be dispatched exactly once.
//
// The stage files are written during agent-a's dispatch and the real write
// detector is wired, so only detected written outputs are gated.
func TestSession_HITL_GlobApproval_Auto_AllApproved_NoRedispatch(t *testing.T) {
	tmpDir := chdirWorkspace(t)

	// Pre-create Plan.md for stage-set re-derivation (2 stages).
	writePlanMD(t, tmpDir)

	// Write the orchestrator file inline with absolute Stage-* output paths.
	orchDir := t.TempDir()
	orchContent := hitlGlobAutoOrchestratorContent()
	orchPath := filepath.Join(orchDir, "orchestrator.md")
	if err := os.WriteFile(orchPath, []byte(orchContent), 0600); err != nil {
		t.Fatalf("write inline orchestrator: %v", err)
	}
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "agent-a")

	f := harness.NewMockAdapter()
	store := &memStore{}
	// Both approved per-stage files are written during agent-a's dispatch so the
	// real write detector attributes them to it.
	fa := &fileWritingAdapter{
		inner:   f,
		agentID: "agent-a",
		setup: func() {
			writeStageArtifact(t, tmpDir, 1, approvedArtifactContent)
			writeStageArtifact(t, tmpDir, 2, approvedArtifactContent)
		},
	}
	ses := session.New(session.Deps{
		Harness:   fa,
		Store:     store,
		Approvals: artifact.NewApprovalReader(),
		Outputs:   artifact.NewOutputWriteDetector(),
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
