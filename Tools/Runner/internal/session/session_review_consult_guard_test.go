package session_test

// Guard-condition tests and EXECUTION PHASE_END prospective semantics for
// review-class routing consultation (split from session_review_consult_test.go).
//
// Covers:
//   - EXECUTION PHASE_END fires only after the last stage, not after each stage.
//   - Non-review infrastructure classes do NOT trigger consultation.
//   - Review agent failure does NOT trigger consultation.
//   - Nil Routing silently skips consultation.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_ReviewConsult_ExecutionPhaseEnd_FiresOnlyAfterLastStage verifies
// the FR-3 prospective semantics for EXECUTION PHASE_END: the look-ahead spans
// the entire EXECUTION phase across all stages, so PHASE_END fires only after
// the last step of the *last* stage -- not after the last step of each
// individual stage.
//
// Fixture: phase-end-execution-staged-orch.md
//   Stage-1: agent-a -> agent-b
//   Stage-2: agent-a -> agent-b
//   InfraAgent: review-agent-a, PHASE_END, continue
//
// Under prospective EXECUTION PHASE_END semantics (GREEN):
//   - agent-b completes Stage-1 (row 1): NOT the last EXECUTION step -- no PHASE_END
//   - agent-b completes Stage-2 (row 3): last step of last stage = last EXECUTION step
//     PHASE_END fires; review-agent-a is dispatched; consultant stops the run.
//   Total dispatches: 5 (agent-a/S1, agent-b/S1, agent-a/S2, agent-b/S2, review-agent-a)
//
// Under retrospective semantics (RED -- current code):
//   - PHASE_END fires when phase changes from EXECUTION.Stage-1 to EXECUTION.Stage-2,
//     i.e., after agent-b/Stage-1 completes. review-agent-a is dispatched too early.
//   - The consultant stops the run after Stage-1, before Stage-2 is dispatched.
//   Total dispatches: 3 (agent-a/S1, agent-b/S1, review-agent-a)
//
// RED failure: total invocations == 3, not 5; review-agent-a is at index 2, not 4;
//   Stage-2 agents (invs[2]=agent-a, invs[3]=agent-b) are never dispatched.
func TestSession_ReviewConsult_ExecutionPhaseEnd_FiresOnlyAfterLastStage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consultant stops the run when called. In GREEN this is called after Stage-2's
	// last step. In RED this is called (incorrectly) after Stage-1's last step.
	consultant.queueStop("execution phase-end review seen, stopping")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "phase-end-execution-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "review-agent-a")

	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	// Stage-1, step 1: not the last EXECUTION step -- PHASE_END must not fire.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 agent-a done",
	}})
	// Stage-1, step 2 (last Stage-1 step): NOT the last EXECUTION step.
	// PHASE_END must NOT fire here; the look-ahead still sees Stage-2 steps ahead.
	// In RED: PHASE_END fires here (phase changes to EXECUTION.Stage-2 next).
	// In GREEN: no PHASE_END; Stage-2 proceeds.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 agent-b done",
	}})
	// Stage-2, step 1: not the last EXECUTION step -- PHASE_END must not fire.
	// Only consumed in GREEN.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 agent-a done",
	}})
	// Stage-2, step 2: last step of last stage = last EXECUTION step.
	// PHASE_END fires after this completes (GREEN). Only consumed in GREEN.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 agent-b done",
	}})
	// review-agent-a dispatched by PHASE_END after Stage-2's last step.
	// Only consumed in GREEN.
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "execution-phase-end-review",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir
	}

	got, err := ses.Start(context.Background(), cfg)

	// In RED: run stops after Stage-1 (consultant called too early).
	// This assertion still passes in RED (consultant does stop the run) -- the
	// failing assertions below distinguish RED from GREEN.
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	invs := f.Invocations()

	// GREEN: 5 dispatches (a/S1, b/S1, a/S2, b/S2, review-agent-a).
	// RED:   3 dispatches (a/S1, b/S1, review-agent-a) -- PHASE_END fired too early.
	if len(invs) != 5 {
		t.Fatalf("want 5 invocations (agent-a/S1, agent-b/S1, agent-a/S2, agent-b/S2, review-agent-a), got %d: EXECUTION PHASE_END must fire only after the last stage, not after Stage-1", len(invs))
	}

	// Stage-2 must have been dispatched before review-agent-a.
	// In RED, Stage-2 is never reached; invs[2] would be review-agent-a.
	if invs[2].Agent.Identifier != "agent-a" {
		t.Errorf("want invocation[2] = agent-a (Stage-2 first step), got %q: PHASE_END must not fire before Stage-2 is dispatched", invs[2].Agent.Identifier)
	}
	if invs[3].Agent.Identifier != "agent-b" {
		t.Errorf("want invocation[3] = agent-b (Stage-2 last step), got %q: PHASE_END must not fire before Stage-2 last step", invs[3].Agent.Identifier)
	}

	// review-agent-a must be the fifth dispatch (index 4), after both stages.
	// In RED it is the third dispatch (index 2), after only Stage-1.
	if invs[4].Agent.Identifier != "review-agent-a" {
		t.Errorf("want invocation[4] = review-agent-a (EXECUTION PHASE_END fires after last stage), got %q", invs[4].Agent.Identifier)
	}

	// Consultant must be called exactly once (after Stage-2's last step in GREEN).
	if consultant.CallCount != 1 {
		t.Fatalf("want 1 consultation call (after EXECUTION PHASE_END at end of Stage-2), got %d", consultant.CallCount)
	}
}

// ---- Guard-condition tests: consultation does NOT occur ----

// TestSession_ReviewConsult_NonReviewClass_NoConsultation verifies scenario (d):
// non-review infrastructure classes (checkpoint) do NOT trigger a routing
// consultation even when Routing is wired.
//
// Uses review-consult-checkpoint-orch.md: checkpoint-class agent with
// INVOCATION_INTERVAL:1 and on_failure=continue -- no review-class agents.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_NonReviewClass_NoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No instructions queued. Any unexpected ConsultRouting call exhausts the
	// queue and returns a transport error, making the unwanted call visible as
	// RunStoppedByConsultant rather than a silent no-op.

	dir := t.TempDir()
	// review-consult-checkpoint-orch.md: checkpoint-class agent (INVOCATION_INTERVAL:1,
	// continue) -- no review-class agents declared.
	orchPath := copyOrchestratorFile(t, dir, "review-consult-checkpoint-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount != 0 {
		t.Errorf("want no consultation calls for checkpoint-class infrastructure agents, got %d",
			consultant.CallCount)
	}
}

// TestSession_ReviewConsult_ReviewFailure_NoConsultation verifies scenario (e):
// a review agent failure (non-SUCCESS StatusCode) does NOT trigger a routing
// consultation; the existing on_failure=continue policy applies and the run
// proceeds normally.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_ReviewFailure_NoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No instructions queued. Unexpected calls surface as errors.

	ses, f, _, orchPath := newReviewConsultSession(t, consultant)

	// Both review agents return non-SUCCESS (BLOCKED). on_failure=continue, so
	// the run proceeds and completes normally.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-a blocked",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-b blocked",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-a blocked again",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "review-b blocked again",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount != 0 {
		t.Errorf("want no consultation calls when review agents return non-SUCCESS, got %d",
			consultant.CallCount)
	}
}

// TestSession_ReviewConsult_NilRouting_NoConsultation verifies scenario (f):
// when session.Deps.Routing is nil, the consultation is silently skipped and
// the run proceeds as if no review consultation mechanism exists.
//
// Call-site coverage: this test exercises call site 1 only (no consultation
// dispatch occurs, so the consultRoute dispatch-completion tail -- call site 2
// -- is never reached). The nil-Routing guard at call site 2 is structurally
// identical to call site 1: both check `s.deps.Routing != nil` using the same
// shared session field before invoking consultRoute. A single test is therefore
// sufficient to validate the guard for both sites.
//
// Guard-condition test: passes in both RED and GREEN.
func TestSession_ReviewConsult_NilRouting_NoConsultation(t *testing.T) {
	// newReviewClassSession creates a session with Routing: nil.
	// No post-review consultation expected; run proceeds normally.
	ses, f, _, orchPath := newReviewClassSession(t)

	// Full linear run with reviews: 2 workflow steps x 2 review agents = 4 reviews.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := baseLinearConfig(orchPath)
	got, err := ses.Start(context.Background(), cfg)

	// Run completes normally: Routing=nil means no consultation.
	requireRunStatus(t, got, err, domain.RunCompleted)
}
