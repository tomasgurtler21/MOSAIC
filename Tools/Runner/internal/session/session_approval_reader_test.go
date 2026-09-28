package session_test

// Tests for the ApprovalCapability check at run start and for filesystem-based
// approval reading: approved artifacts pass without redispatch, unapproved
// artifacts trigger the standard redispatch-then-escalate path, and an incapable
// approval reader refuses HITL workflows at run start without refusing non-HITL ones.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Filesystem approval reader =====

// TestSession_HITL_FilesystemApprovalReader_Approved_PassesWithoutRedispatch
// verifies that when the real file-based approval reader is wired and the
// dispatched output artifact carries human_approved: true in its YAML
// frontmatter, the human-review row passes verification without any re-dispatch.
//
// This test uses artifact.NewApprovalReader() — the same reader the CLI already
// wires and that the interactive frontend must wire after I7.3. It writes a
// real file on disk and asserts that the session reads it correctly.
func TestSession_HITL_FilesystemApprovalReader_Approved_PassesWithoutRedispatch(t *testing.T) {
	// Write an approved artifact file at an absolute path in a temp directory.
	// The consultant will override the table row output artifacts to point here,
	// giving the approval reader a deterministic, absolute path to check.
	tmpDir := t.TempDir()
	approvedPath := filepath.Join(tmpDir, "plan.md")
	approvedContent := "---\nhuman_approved: true\n---\n# Plan\n"
	if err := os.WriteFile(approvedPath, []byte(approvedContent), 0600); err != nil {
		t.Fatalf("write approved artifact: %v", err)
	}

	consultant := &scriptedRoutingConsultant{}
	approvedPaths := []string{approvedPath}
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 0, &approvedPaths)
	consultant.queueDispatch("agent-b", "continue", 1)
	consultant.queueStop("done")

	// Use the real filesystem-based approval reader.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, artifact.NewApprovalReader())

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

	// An approved artifact must not trigger a redispatch.
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls != 1 {
		t.Errorf("want agent-a dispatched exactly once (human_approved: true -> no redispatch), got %d invocations", agentACalls)
	}
}

// TestSession_HITL_FilesystemApprovalReader_Unapproved_RedispatchesThenEscalates
// verifies that when the real file-based approval reader is wired and the
// dispatched output artifact carries human_approved: false, the human-review
// row is redispatched once, still fails approval, and then escalates to a
// deviation — the same re-dispatch-then-escalate path as the non-interactive
// frontend.
func TestSession_HITL_FilesystemApprovalReader_Unapproved_RedispatchesThenEscalates(t *testing.T) {
	tmpDir := t.TempDir()
	unapprovedPath := filepath.Join(tmpDir, "plan.md")
	unapprovedContent := "---\nhuman_approved: false\n---\n# Plan\n"
	if err := os.WriteFile(unapprovedPath, []byte(unapprovedContent), 0600); err != nil {
		t.Fatalf("write unapproved artifact: %v", err)
	}

	consultant := &scriptedRoutingConsultant{}
	unapprovedPaths := []string{unapprovedPath}
	// First dispatch: agent-a with the unapproved artifact.
	consultant.queueDispatchWithOutputs("agent-a", "do the work", 0, &unapprovedPaths)
	// After HITL escalation the consultant is invoked to resolve the deviation.
	consultant.queueStop("HITL escalation: unapproved after redispatch")

	ses, f, _, orchPath := newHITLLinearSession(t, consultant, artifact.NewApprovalReader())

	// Queue two responses for agent-a: original dispatch + redispatch.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done (redispatch)",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// An unapproved artifact must trigger a redispatch: agent-a must be
	// dispatched at least twice (original + redispatch before escalation).
	agentACalls := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentACalls++
		}
	}
	if agentACalls < 2 {
		t.Errorf("want agent-a dispatched at least twice (human_approved: false -> redispatch), got %d invocations", agentACalls)
	}
}

// ===== Incapable approval reader =====

// TestSession_RunStart_IncapableApprovalReader_HITLWorkflow_Refuses verifies
// that when the session is configured with no approval reader (Deps.Approvals
// nil, normalised to the session internal unreadable stand-in) and the
// selected workflow declares at least one human-review row, Start refuses the
// run at run start before any artifact is created.
//
// This test is in the RED phase: without the ApprovalCapability run-start
// check (domain.ApprovalCapability.ApprovalsReadable() returning false), the
// session proceeds, HITL rows fail their approval checks one by one after
// spending a redispatch each, and the run terminates with a non-refusal status
// (RunDeviationUnresolved or RunStoppedByConsultant) rather than RunRefused.
func TestSession_RunStart_IncapableApprovalReader_HITLWorkflow_Refuses(t *testing.T) {
	dir := t.TempDir()
	// hitl-linear-orch.md has HITL=true for both agent-a and agent-b.
	orchPath := copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}

	// Deps.Approvals deliberately nil: session.New normalises it to
	// unreadableApprovalReader{}, which must implement
	// domain.ApprovalCapability.ApprovalsReadable() = false. The session must
	// detect this at run start and refuse before dispatching any agent.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// Approvals: nil — deliberately omitted
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error (refusal must be encoded in RunOutcome, not returned as error), got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused when approval reader is incapable and workflow declares human-review rows, "+
			"got %q (message: %q)", got.Status, got.Message)
	}

	// The refusal must happen before any artifact is created: Store.Create
	// and Store.Apply must not have been called.
	if store.CreatedRunID != "" {
		t.Errorf("want no artifact created before run-start refusal, but Store.Create was called with runID=%q",
			store.CreatedRunID)
	}
	if len(store.Applied) != 0 {
		t.Errorf("want 0 Store.Apply calls before run-start refusal, got %d", len(store.Applied))
	}
}

// TestSession_RunStart_IncapableApprovalReader_NoHITLWorkflow_Proceeds verifies
// the negative case of the ApprovalCapability check: when the approval reader
// is incapable but the selected workflow declares no human-review rows, the run
// is not refused at run start. An incapable reader only blocks workflows that
// would actually require approval reads.
func TestSession_RunStart_IncapableApprovalReader_NoHITLWorkflow_Proceeds(t *testing.T) {
	dir := t.TempDir()
	// linear-orch.md has HITL=false for both agents — no approval reads needed.
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}

	// Approvals deliberately nil -> unreadableApprovalReader{}.
	// The run must NOT be refused because the workflow has no HITL rows.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		// Approvals: nil
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Errorf("want run NOT refused when workflow has no human-review rows, "+
			"even with an incapable approval reader; got RunRefused (message: %q)", got.Message)
	}
}
