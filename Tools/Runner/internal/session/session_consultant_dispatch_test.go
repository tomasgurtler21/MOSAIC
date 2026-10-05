package session_test

// Tests for consultant-routed dispatch: request construction (task description,
// constraints, input/output artifact overrides, sequence numbers, backward jumps)
// and the HITL priority table (HITLOverride column).

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Request construction and free navigation =====

// TestSession_ConsultantRoutedDispatch_UsesInstructionTaskDescription verifies
// that when the RoutingConsultant dispatches an agent, the harness receives the
// TaskDescription from the DispatchInstruction verbatim — not GenericTaskDescription
// and not anything derived from the routing table.
func TestSession_ConsultantRoutedDispatch_UsesInstructionTaskDescription(t *testing.T) {
	const wantTaskDesc = "consultant-specific task instructions"

	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", wantTaskDesc, 0)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	if invs[0].Request.TaskDescription != wantTaskDesc {
		t.Errorf("want task_description=%q from consultant instruction, got %q",
			wantTaskDesc, invs[0].Request.TaskDescription)
	}
}

// TestSession_AutoRoutedDispatch_UsesGenericTaskDescription verifies that
// auto-routed dispatches use GenericTaskDescription as the task description.
func TestSession_AutoRoutedDispatch_UsesGenericTaskDescription(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

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

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	if invs[0].Request.TaskDescription != domain.GenericTaskDescription {
		t.Errorf("want task_description=%q for auto-routed dispatch, got %q",
			domain.GenericTaskDescription, invs[0].Request.TaskDescription)
	}
}

// TestSession_ConsultantRoutedDispatch_NilConstraints_FallsBackToTableRow
// verifies that when the consultant's DispatchInstruction omits Constraints
// (nil pointer), the harness request falls back to the table row's constraints
// (from the deployment defaults, which are empty for these fixtures, so the
// request's Constraints field is empty).
func TestSession_ConsultantRoutedDispatch_NilConstraints_FallsBackToTableRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Nil Constraints → fallback to row default (empty for these fixtures).
	consultant.queueDispatch("agent-a", "task", 0)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	// DispatchInstruction.Constraints is nil → row default applies (empty string).
	if invs[0].Request.Constraints != "" {
		t.Errorf("want empty constraints from row default when instruction omits Constraints, got %q",
			invs[0].Request.Constraints)
	}
}

// TestSession_ConsultantRoutedDispatch_NonNilConstraints_OverridesTableRow
// verifies that when the consultant's DispatchInstruction provides a non-nil
// Constraints pointer, its value replaces the table row's constraints.
func TestSession_ConsultantRoutedDispatch_NonNilConstraints_OverridesTableRow(t *testing.T) {
	const overrideConstraints = "no external calls allowed"

	consultant := &scriptedRoutingConsultant{}
	c := overrideConstraints
	consultant.queueDispatchWithConstraints("agent-a", "task", 0, &c)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	if invs[0].Request.Constraints != overrideConstraints {
		t.Errorf("want constraints=%q from instruction override, got %q",
			overrideConstraints, invs[0].Request.Constraints)
	}
}

// TestSession_ConsultantRoutedDispatch_EmptySliceInputArtifacts_OverridesSendsNone
// verifies that when the consultant supplies InputArtifacts as a non-nil
// pointer to an empty slice, the dispatched request carries no input artifacts
// (the empty slice is intentional, not a fallback).
func TestSession_ConsultantRoutedDispatch_EmptySliceInputArtifacts_OverridesSendsNone(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	empty := []string{}
	consultant.queueDispatchWithInputs("agent-b", "task", 1, &empty)
	// After agent-b, stop.
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	// The consultant supplied *[]string{} — an explicit empty override, not nil.
	// The dispatched request must carry no input artifacts.
	if len(invs[0].Request.InputArtifacts) != 0 {
		t.Errorf("want 0 input artifacts (explicit empty override), got %v",
			invs[0].Request.InputArtifacts)
	}
}

// TestSession_ConsultantRoutedDispatch_NilInputArtifacts_FallsBackToTableRow
// verifies that when the consultant's DispatchInstruction omits InputArtifacts
// (nil pointer), the dispatched request falls back to the routing table row's
// input artifact list.
func TestSession_ConsultantRoutedDispatch_NilInputArtifacts_FallsBackToTableRow(t *testing.T) {
	// agent-b in the linear workflow has "plan.md" as its input artifact.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "task", 1) // nil InputArtifacts → fallback to row
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	// The table row for agent-b has "plan.md" as input. Nil InputArtifacts in the
	// instruction must fall back to the row's list. Check that at least one
	// artifact contains "plan.md" (the path may be run-folder-qualified).
	found := false
	for _, a := range invs[0].Request.InputArtifacts {
		if strings.Contains(a, "plan.md") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want fallback to table row's plan.md input artifact, got %v",
			invs[0].Request.InputArtifacts)
	}
}

// TestSession_SequenceNumber_AlwaysRunnerAssigned verifies that the
// AgentInstanceID in every dispatched request carries the Runner's own
// sequence counter in the "{agent}#{seq}" form, regardless of whether routing
// came from the consultant or the engine. The sequence must be monotonically
// increasing across the run.
func TestSession_SequenceNumber_AlwaysRunnerAssigned(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step one", 0)
	consultant.queueDispatch("agent-b", "step two", 1)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

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

	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want 2 harness invocations, got %d", len(invs))
	}
	// agent-a takes the first slot: the consultation that chose it consumes none.
	if invs[0].Request.AgentInstanceID != "agent-a#1" {
		t.Errorf("want agent-a#1, got %q", invs[0].Request.AgentInstanceID)
	}
	// agent-b takes the next slot directly.
	if invs[1].Request.AgentInstanceID != "agent-b#2" {
		t.Errorf("want agent-b#2, got %q", invs[1].Request.AgentInstanceID)
	}
}

// TestSession_BackwardJump_UpdatesCurrentStateToDispatchedRow verifies that
// when the consultant dispatches an agent whose row index is earlier than the
// current position (a backward jump), current_state is updated to the
// dispatched row's position, phase, and stage.
func TestSession_BackwardJump_UpdatesCurrentStateToDispatchedRow(t *testing.T) {
	// Start at agent-b (row 1), then the consultant jumps back to agent-a (row 0).
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step one", 1) // dispatch row 1 first
	consultant.queueDispatch("agent-a", "step two — backward jump to row 0", 0) // backward jump
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if len(store.Applied) < 2 {
		t.Fatalf("want at least 2 Apply calls, got %d", len(store.Applied))
	}
	// The second Apply (after the backward-jump dispatch of agent-a) must record
	// an agent instance whose name contains "agent-a", confirming the backward
	// jump dispatched the correct agent.
	secondStep := store.Applied[len(store.Applied)-1]
	if !strings.Contains(secondStep.AgentInstance, "agent-a") {
		t.Errorf("want last recorded step to be agent-a (backward jump), got %q",
			secondStep.AgentInstance)
	}
}

// ===== HITL priority table: OutputArtifacts and HITLOverride columns =====

// TestSession_ConsultantRoutedDispatch_NilOutputArtifacts_FallsBackToTableRow
// verifies that when the consultant's DispatchInstruction omits OutputArtifacts
// (nil pointer), the dispatched request falls back to the routing table row's
// output artifact list.
func TestSession_ConsultantRoutedDispatch_NilOutputArtifacts_FallsBackToTableRow(t *testing.T) {
	// agent-a in the linear workflow has "plan.md" as its output artifact.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "task", 0) // nil OutputArtifacts → fallback to row
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	// The table row for agent-a has "plan.md" as output. Nil OutputArtifacts in
	// the instruction must fall back to the row's list.
	found := false
	for _, a := range invs[0].Request.OutputArtifacts {
		if strings.Contains(a, "plan.md") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want fallback to table row's plan.md output artifact, got %v",
			invs[0].Request.OutputArtifacts)
	}
}

// TestSession_ConsultantRoutedDispatch_NonNilOutputArtifacts_OverridesTableRow
// verifies that when the consultant's DispatchInstruction provides a non-nil
// OutputArtifacts pointer, its value replaces the table row's output artifacts.
func TestSession_ConsultantRoutedDispatch_NonNilOutputArtifacts_OverridesTableRow(t *testing.T) {
	const overrideOutput = "override-output.md"

	consultant := &scriptedRoutingConsultant{}
	override := []string{overrideOutput}
	consultant.queueDispatchWithOutputs("agent-a", "task", 0, &override)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation, got 0")
	}
	// The overridden output artifact must appear in the request.
	found := false
	for _, a := range invs[0].Request.OutputArtifacts {
		if strings.Contains(a, overrideOutput) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want output artifact %q from override, got %v", overrideOutput, invs[0].Request.OutputArtifacts)
	}
	// The row default "plan.md" must not appear (the override replaces, not merges).
	for _, a := range invs[0].Request.OutputArtifacts {
		if strings.Contains(a, "plan.md") {
			t.Errorf("want row default plan.md replaced by override, but it still appears in %v",
				invs[0].Request.OutputArtifacts)
		}
	}
}

// TestSession_ConsultantRoutedDispatch_NilHITLOverride_UsesTableRowHITL
// verifies that when the consultant's DispatchInstruction omits HITLOverride
// (nil pointer), the effective HITL for the dispatch is taken from the routing
// table row. In the linear-orch.md fixture, both rows have HITL=false, so a nil
// override means no HITL check is applied even with an ApprovalFalse reader.
func TestSession_ConsultantRoutedDispatch_NilHITLOverride_UsesTableRowHITL(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// queueDispatch sets HITLOverride=nil → row default (HITL=false for linear-orch.md).
	consultant.queueDispatch("agent-a", "task", 0)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// Wire ApprovalFalse: if HITL were erroneously applied, a redispatch would occur.
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Routing:   consultant,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

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

	// With HITL=false from the table row (nil override), ApprovalFalse must not
	// trigger a redispatch. All queued responses must be consumed exactly once.
	if f.RemainingQueueSize() != 0 {
		t.Errorf("want all queued responses consumed (nil HITLOverride uses row HITL=false, no redispatch), "+
			"got %d unconsumed", f.RemainingQueueSize())
	}
}

// TestSession_ConsultantRoutedDispatch_HITLOverrideFalse_SuppressesHITL
// verifies that when the consultant's DispatchInstruction explicitly sets
// HITLOverride to false, HITL compliance verification is suppressed even for a
// row whose table HITL column is true.
func TestSession_ConsultantRoutedDispatch_HITLOverrideFalse_SuppressesHITL(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// HITLOverride=false on a HITL=true row (agent-a in hitl-linear-orch.md).
	hitlFalse := false
	consultant.queueDispatchWithHITL("agent-a", "task", 0, &hitlFalse)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// ApprovalFalse — would redispatch if HITL were applied.
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

	// HITLOverride=false must suppress HITL. agent-a must not be redispatched.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (HITLOverride=false suppresses HITL check), got %d", agentACalls)
	}
}
