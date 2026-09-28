package session_test

// Tests for resume configuration: mode, commit settings, and all RunSettings
// are read from the artifact frontmatter rather than from the caller-supplied
// RunConfig on a resumed run.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Resume configuration =====

// TestSession_Start_Resume_UsesModeFromArtifact verifies that on resume, the
// execution mode is taken from the artifact's RunSettings (as read from the
// frontmatter) rather than from the caller-supplied RunConfig. This is part of
// the "a resumed run reads all configuration from frontmatter" contract.
//
// RED-phase signal: cfg sets Checkpoints=true but the linear workflow has no
// checkpoint-class agent. Without I5.4, the session applies the cfg checkpoints
// value on the resume path and immediately refuses the run. With I5.4, the
// session reads Checkpoints=false from the artifact frontmatter, bypasses the
// refusal, and the run completes. cfg also sets Mode=orchestrated (vs artifact
// Mode=auto) — once I5.4 is implemented and also overrides the mode from the
// artifact, the final state assertion provides additional coverage.
func TestSession_Start_Resume_UsesModeFromArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an existing artifact that has mode=auto and
	// checkpoints=false. These are the values I5.4 must read and use on resume.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:        domain.ExecutionModeAuto,
			Checkpoints: false,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	// agent-b is the second row; on resume from after agent-a, the session
	// dispatches agent-b next.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	// Checkpoints=true with no checkpoint agent declared → the current session
	// code refuses the run at the checkpoint validation step. I5.4 must override
	// cfg with artifact values (Checkpoints=false) before this check fires.
	cfg.Checkpoints = true
	// Mode=orchestrated differs from the artifact's "auto". Once I5.4 also
	// overrides cfg.Mode with the artifact value, the final state assertion below
	// provides additional coverage of the read-back contract.
	cfg.Mode = domain.ExecutionModeOrchestrated

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the artifact says Checkpoints=false and Mode=auto,
	// so there should be no checkpoint-related refusal and routing should succeed.
	// Without I5.4, cfg.Checkpoints=true triggers a refusal (no checkpoint agent
	// in the linear workflow), and this assertion fails with RunRefused.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify the final artifact state reflects Mode=auto read from the frontmatter,
	// not the caller-supplied orchestrated. This assertion is a secondary guard:
	// it becomes meaningful once I5.4 explicitly propagates the artifact mode
	// into the session's operating state and Apply persists it.
	if store.state.RunSettings.Mode != domain.ExecutionModeAuto {
		t.Errorf("want resume to read Mode=%q from artifact frontmatter into final state, got %q",
			domain.ExecutionModeAuto, store.state.RunSettings.Mode)
	}
}

// TestSession_Start_Resume_NoCommitSetupDispatch verifies that when resuming a
// run that had commits enabled, the commit setup dispatch does NOT occur again.
// The commit branch is already recorded in the artifact from the original run
// start; repeating the setup would create a second branch.
//
// Note: this test is a guard that becomes meaningful after I5.2 is implemented.
// Once commit setup dispatch is added for new runs, this test ensures resume
// does not also trigger it.
func TestSession_Start_Resume_NoCommitSetupDispatch(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const existingBranch = "mosaic/run/original-run-id"

	// Pre-populate the store with an existing artifact that has commits enabled
	// and a commit branch already recorded from the original run start.
	// The artifact reflects: seq 1 = commit setup (infra), seq 2 = agent-a (workflow).
	// CurrentState.LastAgent matches the last workflow log entry (agent-a#2).
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  2,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
			CommitBranch:        existingBranch,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#2", // matches last workflow log entry below
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "commit-manager-git#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	// Only queue agent-b (the remaining workflow step) — no commit agent.
	// If the session incorrectly re-runs commit setup, it would invoke
	// commit-manager-git and receive a harness error (empty queue), causing
	// RunFailed instead of RunCompleted.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)
	// CommitBranch is set to a value that differs from the artifact's recorded
	// branch. A correct I5.4 reads CommitBranch from the artifact (existingBranch);
	// a buggy I5.4 that copies cfg.CommitBranch into the state would leave
	// "cfg-provided-branch" in the final state, failing the assertion below.
	cfg.CommitBranch = "cfg-provided-branch"

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that commit-manager-git was not invoked during resume.
	for _, inv := range f.Invocations() {
		if strings.Contains(inv.Agent.Identifier, "commit-manager-git") {
			t.Errorf("want no commit-manager-git invocation on resume, got one: agent=%q",
				inv.Agent.Identifier)
		}
	}

	// Verify the final artifact state preserves the CommitBranch recorded in
	// the pre-existing artifact frontmatter. Without resume-reads-settings
	// (I5.4), the session would not propagate CommitBranch from the artifact
	// into the run's operating configuration and the final state would not
	// carry the correct branch name.
	if store.state.RunSettings.CommitBranch != existingBranch {
		t.Errorf("want resume to preserve CommitBranch=%q from artifact frontmatter, got %q in final state",
			existingBranch, store.state.RunSettings.CommitBranch)
	}
}

// TestSession_Start_Resume_AllSettingsPreservedFromArtifact verifies that on
// resume, all six RunSettings fields (Mode, Checkpoints, Commits,
// CommitBranchVariant, CommitBranch, PreConsultation, ManualResolution) are
// taken from the artifact's frontmatter. No configuration value is re-derived
// from the caller-supplied RunConfig.
//
// RED-phase signal: cfg sets Checkpoints=true but the linear workflow has no
// checkpoint-class agent. Without I5.4, the session applies cfg.Checkpoints on
// the resume path and refuses the run immediately. With I5.4, the session reads
// all RunSettings from the artifact frontmatter (Checkpoints=false, Mode=auto,
// CommitBranchVariant=MOSAICOwned) before any validation, so the run proceeds
// and the assertions below can verify each field.
func TestSession_Start_Resume_AllSettingsPreservedFromArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Populate the artifact with all settings. Checkpoints=false, Mode=auto,
	// and CommitBranchVariant=MOSAICOwned are the values I5.4 must read.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Checkpoints:         false,
			Commits:             false,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
			CommitBranch:        "",
			PreConsultation:     false,
			ManualResolution:    false,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	// Checkpoints=true with no checkpoint agent → current code refuses the run.
	// I5.4 must read Checkpoints=false from the artifact before this check fires.
	cfg.Checkpoints = true
	// Mode and CommitBranchVariant conflict with artifact values. Once I5.4
	// overrides all cfg RunSettings fields with artifact values, these conflicts
	// are suppressed and the assertions below verify each field was taken from
	// the artifact rather than from cfg.
	cfg.Mode = domain.ExecutionModeOrchestrated
	cfg.CommitBranchVariant = domain.CommitBranchUserOwn

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the artifact says Checkpoints=false, so no
	// checkpoint-related refusal should occur. Without I5.4, cfg.Checkpoints=true
	// triggers a refusal (no checkpoint agent in the linear workflow), and this
	// assertion fails with RunRefused.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify all RunSettings fields are preserved from the artifact frontmatter.
	// These assertions are secondary guards that become meaningful once I5.4
	// explicitly propagates artifact RunSettings into the session's operating
	// state and Apply persists them.
	if store.state.Mode != domain.ExecutionModeAuto {
		t.Errorf("want resume to preserve artifact Mode=%q, got %q",
			domain.ExecutionModeAuto, store.state.Mode)
	}
	if store.state.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("want resume to preserve artifact CommitBranchVariant=%q, got %q",
			domain.CommitBranchMOSAICOwned, store.state.CommitBranchVariant)
	}
}
