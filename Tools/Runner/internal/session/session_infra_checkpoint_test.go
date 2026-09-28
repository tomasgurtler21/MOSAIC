package session_test

// Tests for conditional checkpoint refusal and infrastructure_overrides
// validation at run start.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Conditional checkpoint refusal =====

// TestSession_Start_CheckpointsEnabled_WithCheckpointClassAgent_RunProceeds
// verifies that when checkpoints are enabled AND the orchestrator file
// declares a checkpoint-class infrastructure agent, the session does NOT
// refuse at the checkpoint check step and proceeds to dispatch workflow agents.
//
// This is the key conditional: the current unconditional refusal must become
// conditional on the presence of a declared checkpoint-class agent.
func TestSession_Start_CheckpointsEnabled_WithCheckpointClassAgent_RunProceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // enabled; checkpoint-manager-git is declared in the orch file

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_CheckpointsEnabled_WithoutCheckpointClassAgent_ReturnsRefusal
// verifies that when checkpoints are enabled but no checkpoint-class
// infrastructure agent is declared, the session refuses to start. This is the
// existing behavior preserved as a regression test: the conditional refusal
// must still refuse when the prerequisite is absent.
func TestSession_Start_CheckpointsEnabled_WithoutCheckpointClassAgent_ReturnsRefusal(t *testing.T) {
	// linear-orch.md has no InfrastructureAgents region -> no checkpoint-class agent.
	ses, _, _, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // enabled, but no checkpoint provider is declared

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CheckpointsDisabled_WithCheckpointClassAgent_Proceeds
// verifies that when checkpoints are disabled, the session proceeds normally
// regardless of whether a checkpoint-class agent is declared. Disabling
// checkpoints must not produce a refusal.
func TestSession_Start_CheckpointsDisabled_WithCheckpointClassAgent_Proceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = false // disabled; session must not refuse

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== infrastructure_overrides validation at run start =====

// newCommitAgentSession builds a session backed by the commit-agent-orch.md
// fixture, which declares a commit-class infrastructure agent
// (commit-manager-git) with a STAGE_END trigger. Agent files for agent-a
// and agent-b are written into the temp dir.
func newCommitAgentSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// TestSession_Start_InfrastructureOverride_UnknownAgentName_ReturnsRefusal
// verifies that when the artifact state contains an infrastructure_overrides
// entry naming an agent that is not declared in the orchestrator file's
// InfrastructureAgents region, the session refuses to start.
//
// This test uses a resume scenario (IsNewRun=false) so the override is
// loaded from the pre-existing artifact state rather than a newly created one.
//
// Agent responses are queued for both workflow agents so that, absent the
// override check, the session would reach dispatch and complete successfully.
// The zero-invocations assertion confirms that the refusal is a pre-dispatch
// refusal: if it fires only after the first dispatch (wrong place in the
// run-start sequence), the assertion catches it.
func TestSession_Start_InfrastructureOverride_UnknownAgentName_ReturnsRefusal(t *testing.T) {
	// linear-orch.md has no declared infrastructure agents.
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an artifact that overrides an agent name
	// that is not declared in the orchestrator file.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{AgentName: "unknown-infra-agent"},
		},
	}
	store.exists = true

	// Queue responses so the session would succeed if the override check were absent.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The refusal must happen before any dispatch.
	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (override validation must fire before dispatch), got %d invocation(s)", len(f.Invocations()))
	}
}

// TestSession_Start_InfrastructureOverride_ReplacementSemantics_OverrideReplacesNotMerges
// verifies that an infrastructure_overrides entry replaces (not merges with)
// the agent's declared trigger list for the duration of the run.
//
// Setup: the orchestrator declares checkpoint-manager-git with
// INVOCATION_INTERVAL:1 (fires after every workflow step). The artifact state
// overrides its triggers to INVOCATION_INTERVAL:9999 (effectively never fires
// in a short run).
//
// With correct replacement semantics: only INVOCATION_INTERVAL:9999 is in
// effect -- checkpoint-manager-git is NOT dispatched during the 2-step run.
//
// With incorrect merging semantics: both INVOCATION_INTERVAL:1 and
// INVOCATION_INTERVAL:9999 would be present; INVOCATION_INTERVAL:1 fires
// after the first workflow step, causing checkpoint-manager-git to be
// dispatched -- and making this test fail with a clear signal.
//
// RED-phase note: this test passes vacuously while trigger evaluation is not
// yet implemented, because no infrastructure agent is dispatched for any
// reason. It provides correct specification enforcement once trigger
// evaluation is added: an implementation with merging semantics would then
// dispatch checkpoint-manager-git and cause this test to fail.
func TestSession_Start_InfrastructureOverride_ReplacementSemantics_OverrideReplacesNotMerges(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	// Override checkpoint-manager-git's triggers from INVOCATION_INTERVAL:1 to
	// INVOCATION_INTERVAL:9999 so it effectively never fires in a short run.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{
				AgentName: "checkpoint-manager-git",
				Triggers: []domain.DeclaredInfraTrigger{
					{Trigger: "INVOCATION_INTERVAL", Param: "9999"},
				},
			},
		},
	}
	store.exists = true

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the override references a declared agent, so no
	// refusal for an unknown agent name.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-git must NOT have been dispatched.
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			t.Errorf("checkpoint-manager-git dispatched unexpectedly: INVOCATION_INTERVAL:1 should have been replaced by :9999 (replacement semantics), but appears to have been merged (merging semantics)")
		}
	}
}

// TestSession_Start_InfrastructureOverride_ClassRestrictedTrigger_ReturnsRefusal
// verifies that when an infrastructure_overrides entry specifies a trigger
// outside the allowed set for the agent's class, the session refuses to start.
//
// The design specifies that commit-class agents are restricted to STAGE_END
// triggers only. Supplying INVOCATION_INTERVAL in an override for a
// commit-class agent violates this restriction and must produce RunRefused.
//
// Agent responses are queued so that, absent the class-restriction check,
// the session would dispatch successfully. This ensures the RED-phase failure
// is "RunCompleted instead of RunRefused" (missing class-restriction validation),
// not a spurious deviation-resolver path from an empty harness queue.
func TestSession_Start_InfrastructureOverride_ClassRestrictedTrigger_ReturnsRefusal(t *testing.T) {
	// commit-agent-orch.md declares commit-manager-git (commit class, STAGE_END).
	ses, f, store, orchPath := newCommitAgentSession(t)

	// Override the commit-class agent with a trigger not in its allowed set.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{
				AgentName: "commit-manager-git",
				Triggers: []domain.DeclaredInfraTrigger{
					{Trigger: "INVOCATION_INTERVAL", Param: "5"},
				},
			},
		},
	}
	store.exists = true

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_InfrastructureOverride_EmptyOverrides_Proceeds verifies
// that an artifact state with a nil InfrastructureOverrides slice (the common
// case) causes no refusal and the session proceeds normally.
func TestSession_Start_InfrastructureOverride_EmptyOverrides_Proceeds(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate with no overrides.
	store.state = domain.ArtifactState{
		Workflow:                "linear",
		WorkflowVersion:         "1.0",
		Task:                    "test task",
		GlobalSequence:          0,
		RunSettings:             domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: nil, // no overrides -- the common case
	}
	store.exists = true

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}
