package session_test

// Tests for HITL execution-log integrity: rejected dispatches are persisted as
// distinct log entries (HITLRejected=true, IsInfrastructure=true, nil
// OutputArtifacts), sequence numbers are strictly monotonic across all
// persisted dispatches, and resume correctly re-dispatches an interrupted
// rejected step rather than treating it as complete.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_HITL_RejectedDispatchPersisted_AutoMode verifies that the
// hitlCheckLoop (auto-mode dispatch path) persists a HITL-rejected dispatch as a
// distinct execution log entry with HITLRejected=true and IsInfrastructure=true
// before redispatching the same agent. The rejected entry must carry nil
// OutputArtifacts to avoid registering non-compliant artifact paths.
//
// Scenario: agent-a's first attempt is non-compliant (ApprovalFalse); after the
// rejected-step Apply, agent-a is redispatched and the redispatch is compliant
// (ApprovalTrue). The run then proceeds to agent-b and completes.
func TestSession_HITL_RejectedDispatchPersisted_AutoMode(t *testing.T) {
	// First approval call returns False (plan.md on agent-a's initial attempt);
	// subsequent calls return True so the redispatch is accepted and the run
	// completes without escalation.
	approvals := &switchingApprovalReader{
		firstApproval: domain.ApprovalFalse,
		restApproval:  domain.ApprovalTrue,
	}
	ses, f, store, orchPath := newAutoHITLSession(t, approvals, nil)

	// Queue: original agent-a (rejected), redispatched agent-a (accepted), agent-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v1",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v2 approved",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}
	ses.Start(context.Background(), cfg) //nolint:errcheck

	requireHITLRejectedSteps(t, store)
}

// TestSession_HITL_RejectedDispatchPersisted_OrchestratedMode verifies the same
// rejected-dispatch persistence behavior for the hitlLoop (orchestrated-mode
// consultRoute path). The rejected step must appear in store.Applied with
// HITLRejected=true before the session proceeds to the redispatch.
//
// Scenario: consultant dispatches agent-a; the first attempt is non-compliant so
// the hitlLoop triggers a redispatch; the redispatch is compliant; consultant then
// dispatches agent-b and the run completes.
func TestSession_HITL_RejectedDispatchPersisted_OrchestratedMode(t *testing.T) {
	approvals := &switchingApprovalReader{
		firstApproval: domain.ApprovalFalse,
		restApproval:  domain.ApprovalTrue,
	}
	consultant := &scriptedRoutingConsultant{}
	// Two dispatch instructions: agent-a (first, including its internal HITL
	// redispatch) and agent-b (second). The HITL redispatch of agent-a happens
	// inside hitlLoop without consuming a new consultation instruction.
	consultant.queueDispatch("agent-a", "draft the plan", 0)
	consultant.queueDispatch("agent-b", "execute the plan", 1)

	ses, f, store, orchPath := newHITLLinearSession(t, consultant, approvals)

	// Queue: original agent-a (rejected by HITL), redispatched agent-a (accepted),
	// agent-b (accepted).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v1",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v2 approved",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	requireHITLRejectedSteps(t, store)
}

// TestSession_HITL_SequenceMonotonicity_FullScenario verifies that sequence
// numbers are strictly monotonically increasing across a complete HITL
// redispatch-escalation-consultant-reroute cycle, and that no two dispatched
// agent instance IDs share the same "#N" numeric suffix within a single run.
//
// Scenario: orchestrated mode; agent-a's output (plan.md) is always non-compliant,
// triggering HITLRedispatch on the first check and HITLEscalate on the second. The
// escalation causes a recursive consultRoute call; the consultant re-routes to
// agent-b, whose output (result.md) is always compliant.
//
// Without persisting rejected-step Applies, the escalation's consultRoute derives
// its consultSeq from a stale state.GlobalSequence (never incremented for the
// missing rejected Applies), reusing the same "#1" slot that was already assigned
// to the original agent-a dispatch. This test catches that reuse.
func TestSession_HITL_SequenceMonotonicity_FullScenario(t *testing.T) {
	// plan.md (agent-a output) is never approved; result.md (agent-b output) is
	// always approved. This drives the full rejection+escalation path for agent-a
	// while allowing agent-b to complete cleanly.
	approvals := &perPathApprovalReader{
		specific: map[string]domain.HumanApproval{
			"plan.md":   domain.ApprovalFalse,
			"result.md": domain.ApprovalTrue,
		},
		fallback: domain.ApprovalFalse,
	}

	consultant := &scriptedRoutingConsultant{}
	// First instruction dispatches agent-a. After two non-compliant results,
	// HITLEscalate fires and the session calls consultRoute recursively. The
	// second instruction (agent-b) is consumed by that recursive call.
	consultant.queueDispatch("agent-a", "draft the plan", 0)
	consultant.queueDispatch("agent-b", "execute after escalation", 1)

	ses, f, store, orchPath := newHITLLinearSession(t, consultant, approvals)

	// agent-a is dispatched twice: original attempt and HITL redispatch.
	// agent-b is dispatched once by the recursive consultRoute after escalation.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v1",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v2",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// All Applied Seq values must be unique and strictly increasing.
	for i := 1; i < len(store.Applied); i++ {
		if store.Applied[i].Seq <= store.Applied[i-1].Seq {
			t.Errorf("store.Applied[%d].Seq=%d is not greater than Applied[%d].Seq=%d; "+
				"sequence numbers must be strictly increasing across all persisted dispatches",
				i, store.Applied[i].Seq, i-1, store.Applied[i-1].Seq)
		}
	}

	// The full scenario must produce at least two HITLRejected entries: one for
	// the initial dispatch that triggered HITLRedispatch and one for the redispatch
	// that triggered HITLEscalate.
	rejectedCount := 0
	for _, s := range store.Applied {
		if s.HITLRejected {
			rejectedCount++
		}
	}
	if rejectedCount < 2 {
		t.Errorf("want at least 2 HITLRejected steps in store.Applied "+
			"(initial dispatch + redispatch, both non-compliant), got %d; "+
			"both rejected attempts must be persisted before escalating",
			rejectedCount)
	}

	// All dispatched agent instance IDs must carry unique "#N" numeric suffixes.
	seenSuffix := make(map[int]string)
	for _, inv := range f.Invocations() {
		instanceID := inv.Request.AgentInstanceID
		idx := strings.LastIndex(instanceID, "#")
		if idx < 0 {
			continue
		}
		var n int
		if _, scanErr := fmt.Sscanf(instanceID[idx+1:], "%d", &n); scanErr != nil {
			continue
		}
		if prev, already := seenSuffix[n]; already {
			t.Errorf("agent instance ID numeric suffix #%d is reused: first seen in %q, reused by %q; "+
				"every dispatch within a run must carry a unique sequence-derived suffix",
				n, prev, instanceID)
		}
		seenSuffix[n] = instanceID
	}
}

// TestSession_HITL_Resume_AfterRejectedDispatch verifies two complementary
// properties of execution-log integrity after a HITL rejection:
//
//  1. (RED phase assertion) After a HITL rejection cycle, the number of persisted
//     Apply calls must account for the rejected dispatch as a distinct entry.
//     Without the fix, only the accepted Apply is recorded, understating the run's
//     progress and leaving the rejected attempt invisible to the execution log.
//
//  2. (Behavioral assertion) When a run is interrupted after a rejected-step Apply
//     but before the redispatch or escalation Apply -- so the last workflow log
//     entry is the rejected row and current_state.LastAgent is empty (because
//     IsInfrastructure=true left current_state unchanged) -- resuming the run must
//     re-dispatch the unresolved step, not skip it as already complete.
func TestSession_HITL_Resume_AfterRejectedDispatch(t *testing.T) {
	// --- Part 1: Apply count integrity after one rejection and one acceptance ---

	approvals := &switchingApprovalReader{
		firstApproval: domain.ApprovalFalse,
		restApproval:  domain.ApprovalTrue,
	}
	ses, f, store, orchPath := newAutoHITLSession(t, approvals, nil)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v1",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan v2 approved",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	// With one HITL rejection and one acceptance for agent-a, plus one acceptance
	// for agent-b, the execution log must hold 3 distinct Apply rows:
	//   [1] rejected agent-a (HITLRejected=true)
	//   [2] accepted agent-a
	//   [3] accepted agent-b
	// Without the fix only 2 rows are recorded (the rejected Apply is skipped).
	const wantApplied = 3
	if len(store.Applied) != wantApplied {
		t.Errorf("want %d Applied steps (rejected agent-a + accepted agent-a + agent-b), "+
			"got %d; the HITL-rejected dispatch must be persisted as a distinct execution log entry",
			wantApplied, len(store.Applied))
	}

	// --- Part 2: Resume correctly re-dispatches the unresolved step ---
	//
	// Pre-populate a store with the state that should exist when a run is
	// interrupted after the rejected-step Apply but before the redispatch Apply.
	resumeStore := &memStore{
		state: domain.ArtifactState{
			Workflow:        "linear",
			WorkflowVersion: "1.0",
			Task:            "test task",
			GlobalSequence:  1,
			RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
			CurrentState: domain.CurrentState{
				// IsInfrastructure=true on the rejected Apply means current_state
				// was not updated; LastAgent remains empty (no prior accepted step).
				LastAgent:  "",
				LastStatus: domain.StatusSUCCESS,
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{
					Seq:    1,
					Agent:  "agent-a#1",
					Phase:  "PLANNING",
					Status: domain.StatusSUCCESS,
				},
			},
		},
		exists: true,
	}

	resumeDir := t.TempDir()
	resumeOrchPath := copyOrchestratorFile(t, resumeDir, "hitl-linear-orch.md")
	writeAgentFile(t, resumeDir, "agent-a")
	writeAgentFile(t, resumeDir, "agent-b")

	f2 := harness.NewMockAdapter()
	ses2 := session.New(session.Deps{
		Harness:   f2,
		Store:     resumeStore,
		Approvals: &fixedApprovalReader{domain.ApprovalTrue},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Only agent-a should be dispatched first (re-run of the interrupted step);
	// agent-b follows after agent-a completes.
	f2.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done with approval",
	}})
	f2.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses2.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: resumeOrchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false, // resume: artifact pre-exists with rejected-step log entry
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	})
	if err != nil {
		t.Fatalf("want nil error on resume, got %v", err)
	}
	if got.Status != domain.RunCompleted {
		t.Errorf("want RunCompleted on resume, got %q (message: %q)", got.Status, got.Message)
	}

	// The first resume invocation must be agent-a (the re-run of the interrupted
	// rejected-step). The engine detects that the last workflow log entry
	// (agent-a#1) does not match current_state.LastAgent (""), sets RerunLast=true,
	// and re-dispatches agent-a rather than advancing to agent-b.
	invs2 := f2.Invocations()
	if len(invs2) == 0 {
		t.Fatal("want at least 1 harness invocation on resume, got 0")
	}
	if invs2[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first resume invocation to re-dispatch agent-a (interrupted rejected step), "+
			"got %q; the engine must treat the rejected-step log entry as an interrupted step "+
			"requiring re-dispatch, not as a completed step to advance past",
			invs2[0].Agent.Identifier)
	}
}
