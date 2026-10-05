package session_test

// Tests for run resume and mid-invocation interruption recovery.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Resume =====

// TestSession_Start_ResumesFromExistingArtifact verifies that when the
// artifact store already contains state reflecting a partially-completed run
// (agent-a done, agent-b remaining), the session continues from agent-b
// without re-dispatching agent-a.
func TestSession_Start_ResumesFromExistingArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with agent-a completed (seq=1, SUCCESS).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{
				Seq:    1,
				Agent:  "agent-a#1",
				Phase:  "PLANNING",
				Status: domain.StatusSUCCESS,
			},
		},
	}
	store.exists = true

	// Only agent-b should be dispatched (agent-a already completed).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 1 {
		t.Errorf("want 1 harness invocation (agent-b only), got %d", len(invs))
	}
	if len(invs) > 0 && invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed invocation to be agent-b, got %q", invs[0].Agent.Identifier)
	}
}

// TestSession_Start_MidInvocationInterruption_RerunsLastStep verifies FR-33:
// when the execution log's last entry does not match current_state (a sign of
// an in-flight interruption), the session re-dispatches the same row rather
// than advancing to the next one.
func TestSession_Start_MidInvocationInterruption_RerunsLastStep(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Simulate interruption: current_state says agent-b (advanced), but the
	// last execution log entry is agent-a (the actual last completed step).
	// This mismatch triggers RerunLast=true in engine.ResumePoint.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#2", // current_state advanced prematurely
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{
				Seq:    1,
				Agent:  "agent-a#1", // last logged step is agent-a, not agent-b
				Phase:  "PLANNING",
				Status: domain.StatusSUCCESS,
			},
		},
	}
	store.exists = true

	// Expect agent-a to be re-run (RerunLast) then agent-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least 1 invocation, got 0")
	}
	// First invocation should be agent-a (the re-run).
	if invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation to re-run agent-a (RerunLast=true), got %q", invs[0].Agent.Identifier)
	}
}
