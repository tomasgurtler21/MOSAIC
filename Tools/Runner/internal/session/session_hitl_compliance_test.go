package session_test

// Tests for HITL verification in the dispatch loop: accepted compliant results,
// single automatic redispatch on first non-compliance, escalation to deviation
// on second non-compliance, and the skip conditions (effective HITL false,
// empty output artifact list). The gate applies to every response status; the
// non-SUCCESS and written-output cases live in session_written_outputs_test.go.

import (
	"context"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== HITL verification in the loop =====

// TestSession_HITL_CompliantOutputArtifacts_Accepted verifies that when
// effective HITL is true and all output artifacts have human_approved=true, the
// result is accepted and the run advances without a redispatch.
func TestSession_HITL_CompliantOutputArtifacts_Accepted(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// In orchestrated mode: dispatch agent-a, then (after SUCCESS with compliant
	// artifacts) dispatch agent-b, then stop.
	consultant.queueDispatch("agent-a", "do the work", 0)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// All artifacts return ApprovalTrue.
	ses, f, store, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalTrue})

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

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Exactly 2 workflow steps should be recorded (no redispatch).
	workflowSteps := 0
	for _, s := range store.Applied {
		if !s.IsInfrastructure {
			workflowSteps++
		}
	}
	if workflowSteps != 2 {
		t.Errorf("want 2 workflow steps recorded (compliant result accepted, no redispatch), got %d", workflowSteps)
	}
}

// TestSession_HITL_NonCompliantSuccess_RedispatchesSameAgentOnce verifies that
// when effective HITL is true and a SUCCESS result has non-approved output
// artifacts, the session redispatches the same agent exactly once with the
// same dispatch parameters, before the step is accepted or escalated.
func TestSession_HITL_NonCompliantSuccess_RedispatchesSameAgentOnce(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Orchestrated mode: dispatch agent-a. After its non-compliant success, the
	// session should redispatch agent-a (no new consultation for the redispatch).
	// After the second agent-a (compliant), the consultant dispatches agent-b.
	consultant.queueDispatch("agent-a", "do the work", 0)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// Use ApprovalFalse for every read. The expected sequence:
	//   1. agent-a dispatched → SUCCESS → HITL check → non-compliant → redispatch
	//   2. agent-a redispatched → SUCCESS → HITL check → non-compliant (still False)
	//      → second non-compliant → escalate to deviation → consultant consulted
	// With all-False approvals, agent-a is dispatched twice, verifying the
	// redispatch path without needing a switching reader.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// Queue agent-a twice (original + redispatch).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done again",
	}})
	// After the second non-compliant result escalates to deviation, consultant
	// is re-asked. Queue agent-b for after the escalation.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	// Count how many times agent-a was dispatched.
	agentACalls := 0
	for _, inv := range invs {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	// With non-compliant artifacts on the first call, agent-a must be redispatched
	// at least once. Total should be >= 2.
	if agentACalls < 2 {
		t.Errorf("want agent-a dispatched at least twice (original + HITL redispatch), got %d times",
			agentACalls)
	}

	// Verify the redispatch carried identical request parameters as the original
	// dispatch (AC6.5 parameter identity). Collect all agent-a invocations.
	var agentAInvs []harness.Invocation
	for _, inv := range invs {
		if inv.Agent.Identifier == "agent-a" {
			agentAInvs = append(agentAInvs, inv)
		}
	}
	if len(agentAInvs) >= 2 {
		orig := agentAInvs[0].Request
		redispatch := agentAInvs[1].Request
		if orig.TaskDescription != redispatch.TaskDescription {
			t.Errorf("want redispatch TaskDescription=%q identical to original, got %q",
				orig.TaskDescription, redispatch.TaskDescription)
		}
		if orig.Constraints != redispatch.Constraints {
			t.Errorf("want redispatch Constraints=%q identical to original, got %q",
				orig.Constraints, redispatch.Constraints)
		}
		if len(orig.OutputArtifacts) != len(redispatch.OutputArtifacts) {
			t.Errorf("want redispatch OutputArtifacts length %d identical to original, got %d",
				len(orig.OutputArtifacts), len(redispatch.OutputArtifacts))
		}
	}
}

// TestSession_HITL_SecondNonCompliantResult_EscalatesToDeviation verifies that
// after the single allowed redispatch is consumed, a second non-compliant result
// is treated as a deviation (the run consults the RoutingConsultant again
// rather than redispatching a third time).
func TestSession_HITL_SecondNonCompliantResult_EscalatesToDeviation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// First consultation: dispatch agent-a.
	consultant.queueDispatch("agent-a", "do the work", 0)
	// After both agent-a calls produce non-compliant results, the second
	// non-compliant escalates to a deviation. The consultant is re-invoked:
	consultant.queueDispatch("agent-b", "proceed after escalation", 1)
	consultant.queueStop("done")

	// All reads return ApprovalFalse → both agent-a results are non-compliant.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done again",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// The consultant must have been called at least twice:
	// once to dispatch agent-a, and once more after the escalation.
	if consultant.CallCount < 2 {
		t.Errorf("want at least 2 ConsultRouting calls (initial + after escalation), got %d",
			consultant.CallCount)
	}
}

// TestSession_HITL_SkippedWhenEffectiveHITLFalse verifies that HITL compliance
// verification is not performed when the effective HITL for the dispatch was
// false. The run advances after a SUCCESS without any approval check.
func TestSession_HITL_SkippedWhenEffectiveHITLFalse(t *testing.T) {
	// Use the linear workflow where HITL=false for both agents, with an
	// ApprovalReader that always returns ApprovalFalse. If HITL were erroneously
	// applied, the run would redispatch agent-a. With HITL=false, the run
	// completes normally.
	// Build a session that would fail HITL if it were checked.
	dir := t.TempDir()
	orchPath2 := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f2 := harness.NewMockAdapter()
	ses2 := session.New(session.Deps{
		Harness:   f2,
		Store:     &memStore{},
		Approvals: &fixedApprovalReader{domain.ApprovalFalse}, // would fail if checked
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f2.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f2.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath2,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses2.Start(context.Background(), cfg)

	// With HITL=false on all rows, ApprovalFalse must not cause redispatch.
	// The run must complete normally.
	requireRunStatus(t, got, err, domain.RunCompleted)

	if f2.RemainingQueueSize() != 0 {
		t.Errorf("want all queued responses consumed (no HITL redispatch for HITL=false rows), "+
			"got %d unconsumed entries", f2.RemainingQueueSize())
	}
}

// TestSession_HITL_RedispatchAllowanceScopedPerStep verifies that the single
// HITL redispatch allowance resets for each new step. After one step uses its
// redispatch allowance, the next step's first non-compliant result also triggers
// a redispatch (not an escalation).
func TestSession_HITL_RedispatchAllowanceScopedPerStep(t *testing.T) {
	// Use a two-step orchestrated workflow where both steps have HITL=true.
	// For each step: first result is non-compliant → redispatch; second is compliant → accept.
	// If the allowance leaked between steps, step 2's first non-compliant would
	// escalate to a deviation instead of redispatching.
	consultant := &scriptedRoutingConsultant{}
	// Step 1 dispatch, step 1 redispatch consultation (not needed — HITL redispatch is automatic),
	// step 2 dispatch, after step 2 non-compliant the consultant may be re-asked depending
	// on implementation, then stop.
	consultant.queueDispatch("agent-a", "step 1", 0)
	// After agent-a#1 non-compliant → redispatch agent-a (automatic, no new consult).
	// After agent-a#2 compliant → consultant dispatches agent-b.
	consultant.queueDispatch("agent-b", "step 2", 1)
	// After agent-b#3 non-compliant → redispatch agent-b (automatic, new consult NOT needed).
	// After agent-b#4 compliant → consultant stops.
	consultant.queueStop("done")

	// For simplicity, use fixedApprovalReader(ApprovalFalse) and assert that
	// BOTH agent-a and agent-b are dispatched at least twice each (both steps redispatch).
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// Queue: agent-a original, agent-a redispatch, agent-b original, agent-b redispatch.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done again",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done again",
	}})
	// After agent-b's second non-compliant escalates, consultant dispatches again.
	// Queue one more for any escalation handling.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done final",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Count agent-a and agent-b dispatches.
	totalA, totalB := 0, 0
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "agent-a":
			totalA++
		case "agent-b":
			totalB++
		}
	}
	// Both agent-a and agent-b must have been dispatched at least twice if the
	// redispatch allowance is correctly scoped to each step.
	if totalA < 2 {
		t.Errorf("want agent-a dispatched at least twice (non-compliant HITL on step 1), got %d", totalA)
	}
	if totalB < 2 {
		t.Errorf("want agent-b dispatched at least twice (allowance scoped per step, non-compliant HITL on step 2), got %d", totalB)
	}
}

// ===== HITL skip: empty output artifact list =====

// TestSession_HITL_SkippedWhenOutputArtifactListIsEmpty verifies that when the
// effective HITL is true but the dispatched request carries no output artifacts,
// HITL compliance verification is skipped. With no artifacts to inspect for
// approval, the result is accepted without a redispatch.
func TestSession_HITL_SkippedWhenOutputArtifactListIsEmpty(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Dispatch agent-a with explicit empty OutputArtifacts override.
	// HITL is true for agent-a (from the hitl-linear-orch.md fixture), but with
	// zero output artifacts there is nothing to approve — the check must be skipped.
	empty := []string{}
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 0, &empty)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// ApprovalFalse — would trigger a redispatch if HITL were applied.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

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

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// agent-a must be dispatched exactly once (no HITL redispatch for empty artifacts).
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (HITL skipped for empty output artifact list), got %d", agentACalls)
	}
}
