package session_test

// Tests that every workflow dispatch is recorded with the 1-based number of
// the routing-table row it ran, and that infrastructure dispatches record no
// row. Covers engine-routed, HITL re-dispatch and orchestrator-directed paths.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// queueOK queues a SUCCESS response for agent with the given instance id.
func queueOK(f *harness.MockAdapter, agent, instance string) {
	f.Queue(agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: instance,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
}

// requireStepRows asserts, for each applied step whose agent instance starts
// with agentName, that its WorkflowRow equals want. At least one such step
// must exist.
func requireStepRows(t *testing.T, store *memStore, agentName string, want domain.WorkflowRow) {
	t.Helper()
	found := 0
	for _, s := range store.Applied {
		if len(s.AgentInstance) < len(agentName) || s.AgentInstance[:len(agentName)] != agentName {
			continue
		}
		found++
		if s.WorkflowRow != want {
			t.Errorf("step %q (HITLRejected=%v): want WorkflowRow %d, got %d",
				s.AgentInstance, s.HITLRejected, want, s.WorkflowRow)
		}
	}
	if found == 0 {
		t.Fatalf("no applied step for agent %q", agentName)
	}
}

func TestSession_EngineRoutedDispatch_RecordsOneBasedRow(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)
	queueOK(f, "agent-a", "agent-a#1")
	queueOK(f, "agent-b", "agent-b#2")

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
	requireStepRows(t, store, "agent-a", 1)
	requireStepRows(t, store, "agent-b", 2)
}

func TestSession_HITLRedispatch_RecordsRowOnRejectedAndAcceptedSteps(t *testing.T) {
	approvals := &switchingApprovalReader{
		firstApproval: domain.ApprovalFalse,
		restApproval:  domain.ApprovalTrue,
	}
	ses, f, store, orchPath := newAutoHITLSession(t, approvals, nil)
	queueOK(f, "agent-a", "agent-a#1")
	queueOK(f, "agent-a", "agent-a#2")
	queueOK(f, "agent-b", "agent-b#3")

	cfg := baseLinearConfig(orchPath)
	cfg.WorkflowID = "linear"
	ses.Start(context.Background(), cfg) //nolint:errcheck

	rejected := 0
	for _, s := range store.Applied {
		if s.HITLRejected {
			rejected++
		}
	}
	if rejected == 0 {
		t.Fatalf("scenario produced no HITL-rejected step")
	}
	requireStepRows(t, store, "agent-a", 1)
	requireStepRows(t, store, "agent-b", 2)
}

func TestSession_ConsultDispatch_RecordsRowOfDirectedRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Zero-based row index 1 is the second table row (agent-b): number 2.
	consultant.queueDispatch("agent-b", "run second row first", 1)
	consultant.queueDispatch("agent-a", "then first row", 0)
	consultant.queueStop("done")
	ses, f, store, orchPath := newOrchestratedSession(t, consultant)
	queueOK(f, "agent-b", "agent-b#1")
	queueOK(f, "agent-a", "agent-a#2")

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireStepRows(t, store, "agent-b", 2)
	requireStepRows(t, store, "agent-a", 1)
}

func TestSession_ConsultDispatchWithHITL_RecordsRowOnAllAttempts(t *testing.T) {
	approvals := &switchingApprovalReader{
		firstApproval: domain.ApprovalFalse,
		restApproval:  domain.ApprovalTrue,
	}
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "draft the plan", 0)
	consultant.queueDispatch("agent-b", "execute the plan", 1)
	ses, f, store, orchPath := newHITLLinearSession(t, consultant, approvals)
	queueOK(f, "agent-a", "agent-a#1")
	queueOK(f, "agent-a", "agent-a#2")
	queueOK(f, "agent-b", "agent-b#3")

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireStepRows(t, store, "agent-a", 1)
	requireStepRows(t, store, "agent-b", 2)
}

func TestSession_InfrastructureTriggerDispatch_RecordsNoRow(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)
	queueOK(f, "agent-a", "agent-a#1")
	queueOK(f, "checkpoint-manager-git", "checkpoint-manager-git#2")
	queueOK(f, "agent-b", "agent-b#3")
	queueOK(f, "checkpoint-manager-git", "checkpoint-manager-git#4")

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	requireStepRows(t, store, "checkpoint-manager-git", domain.NoWorkflowRow)
	requireStepRows(t, store, "agent-a", 1)
	requireStepRows(t, store, "agent-b", 2)
}

func TestSession_CommitSetupStep_RecordsNoRow(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:mosaic/run/test-run-id]",
	}})
	queueOK(f, "agent-a", "agent-a#2")
	queueOK(f, "agent-b", "agent-b#3")

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"
	ses.Start(context.Background(), cfg) //nolint:errcheck

	requireStepRows(t, store, "commit-manager-git", domain.NoWorkflowRow)
	requireStepRows(t, store, "agent-a", 1)
}
