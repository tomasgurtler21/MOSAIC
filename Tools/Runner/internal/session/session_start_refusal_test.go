package session_test

// Tests for run-start refusal conditions: missing orchestrator file, unknown
// workflow, non-canonical artifact, admission failure, missing agent definition,
// checkpoint configuration, version mismatch, and refusal ordering.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Run-start refusal sequence =====

// TestSession_Start_MissingOrchestratorFile_ReturnsRefusal verifies that
// when the orchestrator file path does not exist, session.Start returns a
// RunRefused outcome before attempting any other run-start step.
func TestSession_Start_MissingOrchestratorFile_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: filepath.Join(dir, "nonexistent.md"),
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_WorkflowNotFound_ReturnsRefusal verifies that requesting
// a workflow ID that does not exist in the orchestrator file returns RunRefused.
func TestSession_Start_WorkflowNotFound_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "no-such-workflow",
		Task:                 "task",
		IsNewRun:             true,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_NonCanonicalArtifact_ReturnsRefusal verifies that when
// an artifact file exists at the location but is not in the canonical format
// (FR-7a), the session returns RunRefused.
func TestSession_Start_NonCanonicalArtifact_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Store returns a RefusalError on Read (simulating a non-canonical file).
	refusalStore := &memStore{
		readErr: &domain.RefusalError{
			Component: "artifact",
			Resource:  "Orchestration.md",
			Reason:    "missing type: orchestration-artifact frontmatter",
		},
	}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     refusalStore,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := baseLinearConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_AdmissionFailure_ReturnsRefusal verifies that a workflow
// pattern that compat refuses (e.g. parallel dispatch notation) produces
// RunRefused before any agent is invoked.
func TestSession_Start_AdmissionFailure_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()

	// Write an orchestrator file with a workflow that compat will refuse:
	// agent-with-mode notation "agent-a(mode)" is refused by FR-18a.6.
	const refusedContent = `<Workflow type="core" name="refused" version="1.0">
## Refused Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a(mode) | FALSE | - | out.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "refused-orch.md")
	if err := os.WriteFile(orchPath, []byte(refusedContent), 0600); err != nil {
		t.Fatalf("write refused-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a(mode)")

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "refused",
		Task:                 "task",
		IsNewRun:             true,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_AgentNotFound_ReturnsRefusal verifies that when an agent
// identifier in the routing table has no matching .md file in the orchestrator
// file's directory, the session returns RunRefused.
func TestSession_Start_AgentNotFound_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	// Deliberately do NOT write agent-a.md or agent-b.md.

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := baseLinearConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CheckpointsEnabledNoProvider_ReturnsRefusal verifies that
// requesting checkpoints when no checkpoint provider is available returns
// RunRefused (FR-9).
func TestSession_Start_CheckpointsEnabledNoProvider_ReturnsRefusal(t *testing.T) {
	ses, _, _, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // request checkpoints -- but the session has no provider

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// ===== FR-7b: workflow version mismatch =====

// TestSession_Start_VersionMismatchArtifact_ReturnsRefusal verifies that when
// an existing artifact's workflow_version differs from the selected workflow's
// version and AllowVersionDrift is false, the session refuses to start.
//
// This protects against resuming a run whose artifact was produced by a
// different (incompatible) version of the workflow definition.
func TestSession_Start_VersionMismatchArtifact_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Pre-populate the store with an artifact that has a different workflow
	// version than the linear workflow's "1.0".
	store := &memStore{
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			Workflow:        "linear",
			WorkflowVersion: "2.0", // mismatch: workflow is "1.0"
			Task:            "test task",
			GlobalSequence:  1,
		},
		exists: true,
	}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.AllowVersionDrift = false // default; explicit for clarity

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// ===== Run-start refusal ordering =====

// TestSession_Start_RefusalOrder_ArtifactBeforeAgentResolution verifies that
// the run-start sequence checks the artifact step before the agent-resolution
// step. When both conditions are simultaneously true (non-canonical artifact
// AND missing agent definition files), the refusal must come from the artifact
// step (which is earlier in the sequence) rather than from agent resolution.
//
// This test enforces the ordering constraint on the run-start sequence: a
// passing implementation that checks steps in the wrong order would allow one
// individual-condition test to pass while failing this ordering test.
func TestSession_Start_RefusalOrder_ArtifactBeforeAgentResolution(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	// Deliberately do NOT write agent-a.md or agent-b.md (agent resolution
	// condition — a later step in the sequence).

	const artifactSentinel = "sentinel-reason-from-artifact-step"

	// Store returns a RefusalError on Read (artifact condition — an earlier step).
	refusalStore := &memStore{
		readErr: &domain.RefusalError{
			Component: "artifact",
			Resource:  "Orchestration.md",
			Reason:    artifactSentinel,
		},
	}
	ses := session.New(session.Deps{
		Harness:   harness.NewMockAdapter(),
		Store:     refusalStore,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := baseLinearConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	// The message must come from the artifact step (earlier in sequence).
	// If it contained an agent-resolution message instead, the ordering would
	// be wrong.
	if !strings.Contains(msg, artifactSentinel) {
		t.Errorf("want refusal message from artifact step (earlier in sequence)\ngot: %q\nwant message to contain: %q", msg, artifactSentinel)
	}
}
