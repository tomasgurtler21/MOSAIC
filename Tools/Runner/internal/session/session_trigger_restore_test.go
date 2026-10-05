package session_test

// Tests for restore-class agent exclusion, no-cascades rule, and
// .agent.md extension support in infrastructure trigger evaluation.

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

// newRestoreGitSession builds a session backed by restore-git-orch.md,
// which declares checkpoint-restore-git with Class=restore, INVOCATION_INTERVAL:1
// (halt policy), plus a checkpoint-manager-git placeholder (Class=checkpoint,
// INVOCATION_INTERVAL:999, on_failure=continue) to satisfy the checkpoint
// precondition when cfg.Checkpoints=true. The trigger evaluation contract
// excludes all restore-class agents from automatic dispatch regardless of
// declared triggers or agent name.
func newRestoreGitSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "restore-git-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-restore-git")
	writeAgentFile(t, dir, "checkpoint-manager-git")
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

// newClassRestoreSession builds a session backed by class-restore-orch.md,
// which declares checkpoint-restore-s3 (Class=restore, INVOCATION_INTERVAL:1,
// halt) plus a checkpoint-manager-git placeholder (Class=checkpoint,
// INVOCATION_INTERVAL:999, on_failure=continue). The restore agent has a name
// different from "checkpoint-restore-git" to confirm that class-based exclusion
// applies generically to any restore-class agent, not only to the specific
// "checkpoint-restore-git" agent by name.
func newClassRestoreSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "class-restore-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-restore-s3")
	writeAgentFile(t, dir, "checkpoint-manager-git")
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

// ===== Restore-class agent exclusion tests =====
//
// Coverage:
//   - A restore-class agent is never dispatched by automatic trigger evaluation,
//     regardless of the triggers its declaration names.
//   - The exclusion keys on Class == "restore", not on the agent's name: covered by
//     both checkpoint-restore-git and a differently-named checkpoint-restore-s3.

// TestSession_Start_TriggerEval_CheckpointRestoreGit_NeverDispatchedAutomatically
// verifies that checkpoint-restore-git is never dispatched by automatic trigger
// evaluation, even when it is declared with matching triggers (INVOCATION_INTERVAL:1).
// The exclusion is class-based: in the restore-git-orch.md fixture,
// checkpoint-restore-git carries Class="restore", and evaluateTriggers excludes
// all restore-class agents regardless of name.
//
// This test is non-vacuous. Trigger evaluation is implemented, and the fixture's
// INVOCATION_INTERVAL:1 trigger matches after every workflow step, so removing the
// restore-class guard in evaluateTriggers dispatches this agent and fails the test.
// The fixture also declares a checkpoint-class agent (INVOCATION_INTERVAL:999, which
// never fires) purely so that cfg.Checkpoints=true satisfies the run-start
// checkpoint-provider precondition; without it the run would refuse to start and the
// assertion below would pass for the wrong reason.
func TestSession_Start_TriggerEval_CheckpointRestoreGit_NeverDispatchedAutomatically(t *testing.T) {
	ses, f, _, orchPath := newRestoreGitSession(t)

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
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-restore-git" {
			t.Errorf("checkpoint-restore-git dispatched by automatic trigger evaluation: restore-class agents must only be dispatched manually, never automatically")
		}
	}
}

// TestSession_Start_TriggerEval_RestoreClass_Generic_NeverDispatchedAutomatically
// verifies that the restore-class exclusion in evaluateTriggers is class-based
// and applies to any restore-class agent, not only to the agent named
// "checkpoint-restore-git". A restore agent with a different name
// ("checkpoint-restore-s3") declared with INVOCATION_INTERVAL:1 must also be
// excluded from automatic trigger evaluation.
//
// This test is non-vacuous, and it is the one that pins the exclusion to the class
// rather than the name: reverting evaluateTriggers to a name check against
// "checkpoint-restore-git" leaves the sibling test above passing while this one
// fails, because checkpoint-restore-s3's INVOCATION_INTERVAL:1 trigger would then
// be evaluated and dispatched.
func TestSession_Start_TriggerEval_RestoreClass_Generic_NeverDispatchedAutomatically(t *testing.T) {
	ses, f, _, orchPath := newClassRestoreSession(t)

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
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-restore-s3" {
			t.Errorf("checkpoint-restore-s3 dispatched by automatic trigger evaluation: restore-class agents must only be dispatched manually, never automatically")
		}
	}
}

// ===== No-cascades rule test =====
//
// Infrastructure agent completions do not re-evaluate triggers; infra dispatches
// are counted exactly (not exponentially).

// TestSession_Start_TriggerEval_NoCascades_InfraCompletionDoesNotRetrigger verifies
// that infrastructure agent completions do not cause further trigger evaluations.
// Only workflow step completions (IsInfrastructure=false) trigger evaluation; infra
// completions (IsInfrastructure=true) are skipped entirely.
//
// With INVOCATION_INTERVAL:1 and 2 workflow steps, the expected dispatch sequence is:
//   workflow-step-1 -> infra -> workflow-step-2 -> infra
// Exactly 2 infra dispatches, one per workflow step. If cascades occurred (infra
// completion re-evaluating triggers), additional infra dispatches would appear.
func TestSession_Start_TriggerEval_NoCascades_InfraCompletionDoesNotRetrigger(t *testing.T) {
	ses, f, _, orchPath := newIntervalAgentSession(t)

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

	infraCount := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			infraCount++
		}
	}
	// Exactly 2 infra dispatches: one after each workflow step.
	if infraCount != 2 {
		t.Errorf("want exactly 2 checkpoint-manager-git dispatches (one per workflow step, no cascades from infra completions), got %d", infraCount)
	}
}

// ===== .agent.md extension test =====

// TestSession_Start_TriggerEval_InfraAgentDotAgentMdExtension_DispatchesCorrectly
// verifies that evaluateTriggers resolves infra agent definition files named
// with the .agent.md compound extension (e.g. checkpoint-manager-git.agent.md)
// just as it would a .md file. This regression-locks the extension-agnostic
// behavior introduced by the switch to agentresolve.ResolveOne.
func TestSession_Start_TriggerEval_InfraAgentDotAgentMdExtension_DispatchesCorrectly(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	// Use .agent.md extension for the infra agent to verify extension-agnostic resolution.
	agentFilePath := filepath.Join(dir, "checkpoint-manager-git.agent.md")
	if err := os.WriteFile(agentFilePath, []byte("# Agent: checkpoint-manager-git\n"), 0600); err != nil {
		t.Fatalf("write checkpoint-manager-git.agent.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
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

	invs := f.Invocations()
	dispatched := false
	for _, inv := range invs {
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			dispatched = true
			if !strings.HasSuffix(inv.Agent.DefinitionPath, "checkpoint-manager-git.agent.md") {
				t.Errorf("want DefinitionPath ending in .agent.md, got %q", inv.Agent.DefinitionPath)
			}
		}
	}
	if !dispatched {
		t.Error("want checkpoint-manager-git dispatched (infra trigger fired), but it was not")
	}
}
