package session_test

// Tests for ConsultRoute fallback artifact template resolution (D3b) and
// unrecorded consultation cause persistence (D4): when a consultant-routed
// dispatch falls back to a row's declared OutputArtifacts containing template
// tokens, those tokens must be resolved before dispatch; and when a harness
// failure triggers a consultation, a CompletedStep capturing the failure must
// be persisted so the execution log has a complete record of every attempt.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// consultStagedArtsOrchName is the workflow name in consult-staged-artifacts-orch.md.
const consultStagedArtsOrchName = "consult-staged-arts"

// stage1DoneAutoStateArts returns a Stage-1-done ArtifactState for the
// consult-staged-artifacts-orch.md fixture, which has only one agent (agent-a)
// per stage. Stage-1 has a single row (agent-a), so only one execution log
// entry is present at GlobalSequence=1 with LastAgent="agent-a#1".
func stage1DoneAutoStateArts() domain.ArtifactState {
	return domain.ArtifactState{
		Workflow:        consultStagedArtsOrchName,
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "EXECUTION", Stage: "1", Status: domain.StatusSUCCESS},
		},
	}
}

// TestSession_ConsultRoute_FallbackArtifacts_ResolvesTemplateTokens verifies
// that when a consultant-routed dispatch falls back to a row's declared
// OutputArtifacts (because the RoutingInstruction does not supply its own),
// and those declared paths contain the {StageNumber} template token, the
// dispatched ProtocolRequest and the CompletedStep applied to the store both
// carry fully resolved paths with no literal {StageNumber} token.
//
// The test uses consult-staged-artifacts-orch.md, which declares
// "Stage-{StageNumber}/Output.md" in the Output column. After plan expansion
// with Stage-1 and Stage-2, row 1 is EXECUTION.Stage-2/agent-a with
// OutputArtifacts = ["Stage-{StageNumber}/Output.md"].
//
// Setup: Stage-1 pre-seeded as complete (Stage = "1"). The engine auto-dispatches
// Stage-2/agent-a (row 1). The harness fails, triggering consultRoute with
// deviation.CurrentStage = "2". The consultant re-routes to row 1 without
// supplying OutputArtifacts, so consultRoute falls back to row.OutputArtifacts.
//
// With the current code, the unresolved "Stage-{StageNumber}/Output.md" is
// passed directly to the request and CompletedStep. After the D3b fix,
// engine.ResolveArtifacts is called, yielding "Stage-2/Output.md".
func TestSession_ConsultRoute_FallbackArtifacts_ResolvesTemplateTokens(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Row 0 is EXECUTION.[StageNumber]/agent-a; the instruction names stage 2.
	// No OutputArtifacts in the instruction: consultRoute falls back to row.OutputArtifacts.
	consultant.queueStagedDispatch("agent-a", "re-route Stage-2/agent-a", 0, 2)
	consultant.queueStop("Stage-2/agent-a completed")

	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, "consult-staged-artifacts-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeConsultStagedPlan(t, dir)

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-seed: Stage-1 complete, Stage-2 pending.
	store.state = stage1DoneAutoStateArts()
	store.exists = true

	// Engine auto-dispatches Stage-2/agent-a (row 1). Harness fails on every
	// attempt until the E501 budget is used up, which triggers consultRoute with
	// deviation.CurrentStage = "2".
	for i := 0; i < engine.E501AttemptLimit; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure on Stage-2/agent-a")})
	}
	// Consultant re-routes to row 1 (agent-a). Harness succeeds.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "Stage-2/agent-a done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           consultStagedArtsOrchName,
		Task:                 "test task",
		IsNewRun:             false,
		RunID:               testRunID,
		RunFolder:            dir,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	_, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	const unresolved = "{StageNumber}"

	// Check the dispatched ProtocolRequest: the consultant-routed invocation
	// (index 1, after the failed auto-routed attempt at index 0) must not
	// contain the literal template token in its OutputArtifacts.
	invocations := f.Invocations()
	consultIdx := engine.E501AttemptLimit
	if len(invocations) <= consultIdx {
		t.Fatalf("want at least %d harness invocations (failed auto attempts + consultant re-route), got %d",
			consultIdx+1, len(invocations))
	}
	// The invocation after the failed auto attempts is the consultant-routed dispatch.
	consultReq := invocations[consultIdx].Request

	// Guard: the consultant-routed request must carry the row's OutputArtifacts.
	// If this slice is empty, consultRoute either did not fall back to row.OutputArtifacts
	// or the orchfile parser did not populate the row's OutputArtifacts column. Both are
	// bugs this test must catch. With the current code this assertion fails (RED) because
	// the request arrives with no output artifacts.
	if len(consultReq.OutputArtifacts) == 0 {
		t.Fatal("want non-empty OutputArtifacts in consultant-routed ProtocolRequest " +
			"(row declares Stage-{StageNumber}/Output.md), got empty; " +
			"consultRoute must populate the request from row.OutputArtifacts when the " +
			"RoutingInstruction does not supply its own artifacts")
	}

	for _, art := range consultReq.OutputArtifacts {
		if strings.Contains(art, unresolved) {
			t.Errorf("consultant-routed ProtocolRequest.OutputArtifacts contains unresolved "+
				"template token %q in path %q; consultRoute must call engine.ResolveArtifacts "+
				"on the fallback row.OutputArtifacts before dispatching",
				unresolved, art)
		}
	}

	// Check the CompletedStep applied to the store: the workflow step's
	// OutputArtifacts (stored from currentOutputArts) must also be resolved.
	for _, s := range store.Applied {
		if s.IsInfrastructure || s.HITLRejected {
			continue
		}
		for _, art := range s.WrittenArtifacts {
			if strings.Contains(art, unresolved) {
				t.Errorf("CompletedStep.WrittenArtifacts[%q] contains unresolved "+
					"template token %q; the CompletedStep must carry resolved artifact paths",
					art, unresolved)
			}
		}
	}
}

// TestSession_ConsultRoute_HarnessFailure_PersistsFailedAttemptRecord verifies
// that when a harness invocation failure triggers a consultation (deviation
// path), a workflow CompletedStep with Status=BLOCKED and ErrorCode=E501 whose
// Summary carries the failure message is persisted to the store. The execution
// log therefore always has a record of every dispatch attempt, including those
// that fail before a response is returned.
func TestSession_ConsultRoute_HarnessFailure_PersistsFailedAttemptRecord(t *testing.T) {
	const errMsg = "simulated invocation failure to verify D4 cause capture"

	consultant := &scriptedRoutingConsultant{}
	// Row 0 is PLANNING/agent-a in linear-orch.md.
	// The consultant re-routes to row 0 after the harness failure.
	consultant.queueDispatch("agent-a", "retry planning after harness failure", 0)
	consultant.queueStop("planning complete")

	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// The harness fails on every dispatch until the engine's E501 budget is used
	// up; only then is the consultation triggered with a deviation.
	for i := 0; i < engine.E501AttemptLimit; i++ {
		f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New(errMsg)})
	}
	// Next invocation (consultant re-routes to row 0 again): SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning recovered",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	_, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error (harness error is a handled deviation), got %v", err)
	}

	// store.Applied must contain a workflow CompletedStep (neither
	// infrastructure nor HITL rejected) with Status BLOCKED and ErrorCode E501
	// whose Summary contains the error message (for traceability).
	var failedAttemptStep *domain.CompletedStep
	for i := range store.Applied {
		s := &store.Applied[i]
		if !s.IsInfrastructure && !s.HITLRejected &&
			s.Status == domain.StatusBLOCKED && s.ErrorCode == domain.ErrorTOOL_UNAVAILABLE {
			cp := *s
			failedAttemptStep = &cp
			break
		}
	}
	if failedAttemptStep == nil {
		t.Error("want a workflow CompletedStep in store.Applied with Status=BLOCKED and " +
			"ErrorCode=E501 capturing the harness failure, got none; " +
			"a record of the failed dispatch attempt must be persisted so the " +
			"execution log has a complete history of every dispatch attempt")
	} else {
		if !strings.Contains(failedAttemptStep.Summary, errMsg) {
			t.Errorf("failed-attempt CompletedStep.Summary = %q; want it to contain "+
				"the harness error message %q for traceability",
				failedAttemptStep.Summary, errMsg)
		}
	}
}
