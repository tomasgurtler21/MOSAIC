package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== T9.4: HITL — non-compliant produces one redispatch then escalates =====

// TestIntegration_HITL_NonCompliant_OneRedispatchThenDeviation verifies that
// when an agent dispatched with HITL=true returns SUCCESS but its output
// artifact is not human-approved, the session re-dispatches the agent exactly
// once. A second non-compliant result escalates to a deviation; the run never
// advances past the unverified step and agent-b is never dispatched.
func TestIntegration_HITL_NonCompliant_OneRedispatchThenDeviation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "hitl-linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	// agent-a is queued twice: initial dispatch and one HITL redispatch.
	// Both return SUCCESS, but the approval reader always reports ApprovalFalse.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done — awaiting human review",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "redispatched — still awaiting review",
	}})

	// ApprovalReader always reports ApprovalFalse: the human gate is never closed.
	approvals := intFixedApprovalReader{approval: domain.ApprovalFalse}

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithApprovals(f, artifactPath, approvals)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "hitl task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := sess.Start(context.Background(), cfg)

	// After the single allowed redispatch the escalation produces a deviation.
	// With no routing consultant wired, the run ends with RunDeviationUnresolved.
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved after HITL redispatch exhausted, got %q (msg: %q)",
			got.Status, got.Message)
	}

	// agent-a must be dispatched exactly twice: once initially, once on redispatch.
	// agent-b must never be dispatched: the run must not advance past the
	// unverified step.
	invs := f.Invocations()
	agentACount, agentBCount := 0, 0
	for _, inv := range invs {
		switch inv.Agent.Identifier {
		case "agent-a":
			agentACount++
		case "agent-b":
			agentBCount++
		}
	}
	if agentACount != 2 {
		t.Errorf("want agent-a dispatched exactly twice (initial + one redispatch), got %d",
			agentACount)
	}
	if agentBCount != 0 {
		t.Errorf("want agent-b never dispatched (run must not advance past unverified step), got %d",
			agentBCount)
	}
}

// ===== T9.5: Stop and orchestrator failure — artifact left resumable =====

// TestIntegration_ConsultantStop_ArtifactResumable verifies that when the
// routing consultant returns a stop instruction, the run ends with
// RunStoppedByConsultant and the orchestration artifact remains on disk in a
// state from which it can be resumed. A stop is never silent data loss.
func TestIntegration_ConsultantStop_ArtifactResumable(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})

	// Consultant dispatches agent-a then stops after it completes.
	consultant := &intScriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "Proceed.", 0)
	consultant.queueStop("workflow paused by operator")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "stop task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// The stop reason must surface in the run outcome.
	if !strings.Contains(got.StopReason, "paused") {
		t.Errorf("want StopReason to contain the consultant's reason; got %q", got.StopReason)
	}

	// The artifact must exist on disk: a stop is never silent data loss.
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Errorf("want artifact file to exist after consultant stop (resumable state), got %v",
			statErr)
	}

	// Neither consultation is recorded: the log holds agent-a alone at Seq 1.
	requireOnlyWorkflowRows(t, artifactPath, "agent-a#1")
}

// requireOnlyWorkflowRows reads the artifact at path and fails unless its
// Execution Log holds exactly the named agent instances, in order, at Seq
// 1..n, with global_sequence n: consultations leave no row and use no slot.
func requireOnlyWorkflowRows(t *testing.T, path string, wantAgents ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("parse artifact: %v", err)
	}
	if len(state.ExecutionLog) != len(wantAgents) {
		t.Fatalf("want execution log %v, got %v", wantAgents, state.ExecutionLog)
	}
	for i, want := range wantAgents {
		if e := state.ExecutionLog[i]; e.Agent != want || e.Seq != i+1 {
			t.Errorf("execution log[%d]: want %s at Seq %d, got %q at Seq %d", i, want, i+1, e.Agent, e.Seq)
		}
	}
	if state.GlobalSequence != len(wantAgents) {
		t.Errorf("want global_sequence %d, got %d", len(wantAgents), state.GlobalSequence)
	}
}

// TestIntegration_ConsultantFailure_ArtifactResumable verifies that when the
// routing consultant returns a transport error (simulating an orchestrator
// failure), the run ends with RunStoppedByConsultant and the artifact remains
// on disk, parseable and ready to resume.
func TestIntegration_ConsultantFailure_ArtifactResumable(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})

	// Consultant dispatches agent-a, then fails with a transport error on the
	// next call — simulating an orchestrator crash.
	consultant := &intScriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "Proceed.", 0)
	consultant.queueError(domain.ConsultFailTransport)

	artifactPath := filepath.Join(dir, "Orchestration.md")
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		RunID: integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "failure task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	// The artifact must exist and be parseable: the run can be resumed after
	// the orchestrator failure is resolved.
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("want artifact to exist after orchestrator failure (resumable state), got %v",
			readErr)
	}
	if _, parseErr := artifact.Parse(data); parseErr != nil {
		t.Errorf("want artifact parseable after orchestrator failure, got %v", parseErr)
	}

	// Neither the successful nor the failed consultation is recorded.
	requireOnlyWorkflowRows(t, artifactPath, "agent-a#1")
}

// ===== Workflow Notes appended by the script orchestrator =====

// noteWritingConsultant wraps a scripted consultant and, on the given call
// number, appends a Workflow Note row directly to the artifact file the way a
// script orchestrator does during its deliberation. Seq is the artifact's
// global_sequence at that moment.
type noteWritingConsultant struct {
	inner        *intScriptedRoutingConsultant
	artifactPath string
	onCall       int
	seq          int
	note         string
	calls        int
	t            *testing.T
}

func (c *noteWritingConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.calls++
	if c.calls == c.onCall {
		data, err := os.ReadFile(c.artifactPath)
		if err != nil {
			c.t.Fatalf("read artifact during consultation: %v", err)
		}
		row := "| " + strconv.Itoa(c.seq) + " | " + c.note + " |\n"
		updated := strings.Replace(string(data), "</WorkflowNotes>", row+"</WorkflowNotes>", 1)
		if err := os.WriteFile(c.artifactPath, []byte(updated), 0o600); err != nil {
			c.t.Fatalf("write artifact during consultation: %v", err)
		}
	}
	return c.inner.ConsultRouting(ctx, req)
}

// TestIntegration_ConsultantAppendedWorkflowNote_SurvivesLaterStateWrites
// verifies, against the real file store, that a Workflow Note the script
// orchestrator appends to Orchestration.md during a routing consultation is
// still in the artifact after the Runner's later writes.
func TestIntegration_ConsultantAppendedWorkflowNote_SurvivesLaterStateWrites(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyFile(t, dir, "orchestrator.md",
		filepath.Join(sessionTestdataDir, "linear-orch.md"))
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "planning done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})

	inner := &intScriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "Proceed.", 0)
	inner.queueDispatch("agent-b", "Proceed.", 1)
	inner.queueStop("done")

	artifactPath := filepath.Join(dir, "Orchestration.md")
	// The second consultation runs after agent-a is recorded (global_sequence 1).
	consultant := &noteWritingConsultant{
		inner: inner, artifactPath: artifactPath, onCall: 2, seq: 1,
		note: "orchestrator observed the planning output", t: t,
	}
	sess := newSessionWithRouting(f, artifactPath, consultant)

	cfg := domain.RunConfig{
		RunID:                integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "notes task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := sess.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)

	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	state, parseErr := artifact.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse artifact: %v", parseErr)
	}
	if len(state.WorkflowNotes) != 1 ||
		state.WorkflowNotes[0].Seq != 1 ||
		state.WorkflowNotes[0].Note != "orchestrator observed the planning output" {
		t.Errorf("want the consultant's Workflow Note preserved after later writes, got %+v", state.WorkflowNotes)
	}
	requireOnlyWorkflowRows(t, artifactPath, "agent-a#1", "agent-b#2")
}
