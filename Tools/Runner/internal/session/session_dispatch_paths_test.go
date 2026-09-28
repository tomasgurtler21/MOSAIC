package session_test

// Tests for run-scoped dispatch path resolution: RunID propagation from
// artifact state and run-scoped artifact path prefixing.

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

// ===== Run-scoped dispatch: RunID propagation and path resolution =====

// TestSession_Start_Dispatch_PopulatesRunID_FromArtifactState verifies that
// when the artifact store returns an ArtifactState with a non-empty RunID, the
// constructed ProtocolRequest sent to the harness carries that same RunID value.
//
// The session derives RunID from the artifact state (not from RunConfig) so that
// resumed runs carry the RunID that was minted at creation time.
func TestSession_Start_Dispatch_PopulatesRunID_FromArtifactState(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store state with a known RunID.
	// The session should read this from the artifact and carry it into the request.
	store.state = domain.ArtifactState{
		RunID:           "20260727T170000Z-a3f9",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
	}
	// We still call Create (IsNewRun=true) but override the returned state.
	// To achieve this without changing flow, we use a custom store wrapper that
	// injects the RunID into the created state. The simplest approach: set
	// RunID on the memStore so Create propagates it.
	store.state.RunID = "20260727T170000Z-a3f9"

	// Use a new session with RunID set in RunConfig. The session passes config.RunID
	// to Store.Create; Store.Create returns state.RunID in the created ArtifactState.
	// The dispatch loop must then populate ProtocolRequest.RunID from state.RunID.
	cfg := baseLinearConfig(orchPath)
	cfg.RunID = "20260727T170000Z-a3f9"

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

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	// Every dispatched request must carry the run_id from the artifact state.
	for i, inv := range invs {
		if inv.Request.RunID != "20260727T170000Z-a3f9" {
			t.Errorf("invocation[%d] ProtocolRequest.RunID: want %q, got %q",
				i, "20260727T170000Z-a3f9", inv.Request.RunID)
		}
	}
}

// TestSession_Start_Dispatch_ResolvesInputArtifacts_ToRunScopedForm verifies
// that input_artifacts paths in the ProtocolRequest are resolved to run-scoped
// form by prepending the run-scoped folder name (e.g. "Plan.md" becomes
// "Orchestration-{run_id}/Plan.md").
func TestSession_Start_Dispatch_ResolvesInputArtifacts_ToRunScopedForm(t *testing.T) {
	dir := t.TempDir()

	// Workflow with explicit input artifacts to verify path resolution.
	const resolveWorkflow = `<Workflow type="core" name="resolve-test" version="1.0">
## Resolve Test Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | Plan.md | Progress.md |
| PLANNING | agent-b | FALSE | Progress.md | Result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "resolve-orch.md")
	if err := os.WriteFile(orchPath, []byte(resolveWorkflow), 0600); err != nil {
		t.Fatalf("write resolve-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	const runID = "20260727T170000Z-a3f9"
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "resolve-test",
		Task:                 "task",
		RunID:                runID,
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	// agent-a's input artifact "Plan.md" must be resolved to
	// "Orchestration-20260727T170000Z-a3f9/Plan.md".
	firstReq := invs[0].Request
	expectedInput := "Orchestration-" + runID + "/Plan.md"
	if !containsInput(firstReq.InputArtifacts, expectedInput) {
		t.Errorf("first request InputArtifacts: want %q (run-scoped), got %v",
			expectedInput, firstReq.InputArtifacts)
	}
	// The unscoped path must not appear.
	if containsInput(firstReq.InputArtifacts, "Plan.md") {
		t.Errorf("first request InputArtifacts: must not contain unscoped %q when run_id is set, got %v",
			"Plan.md", firstReq.InputArtifacts)
	}
}

// TestSession_Start_Dispatch_ResolvesOutputArtifacts_ToRunScopedForm verifies
// that output_artifacts paths in the ProtocolRequest are also resolved to
// run-scoped form (the same resolution applies to both input and output paths).
func TestSession_Start_Dispatch_ResolvesOutputArtifacts_ToRunScopedForm(t *testing.T) {
	dir := t.TempDir()

	const resolveWorkflow = `<Workflow type="core" name="resolve-out" version="1.0">
## Resolve Output Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | - | Progress.md |
| PLANNING | agent-b | FALSE | Progress.md | Result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "resolve-out-orch.md")
	if err := os.WriteFile(orchPath, []byte(resolveWorkflow), 0600); err != nil {
		t.Fatalf("write resolve-out-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	const runID = "20260101T120000Z-beef"
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "resolve-out",
		Task:                 "task",
		RunID:                runID,
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	// agent-a's output artifact "Progress.md" must be resolved to
	// "Orchestration-{runID}/Progress.md".
	firstReq := invs[0].Request
	expectedOutput := "Orchestration-" + runID + "/Progress.md"
	if !containsInput(firstReq.OutputArtifacts, expectedOutput) {
		t.Errorf("first request OutputArtifacts: want %q (run-scoped), got %v",
			expectedOutput, firstReq.OutputArtifacts)
	}
	// The unscoped path must not appear.
	if containsInput(firstReq.OutputArtifacts, "Progress.md") {
		t.Errorf("first request OutputArtifacts: must not contain unscoped %q when run_id is set, got %v",
			"Progress.md", firstReq.OutputArtifacts)
	}
}

// TestSession_Start_Dispatch_DoesNotDoublePrefixAlreadyScopedPaths verifies
// that artifact paths which already contain the run-scoped folder prefix are
// NOT double-prefixed. A path such as "Orchestration-{run_id}/Plan.md" must
// remain unchanged; it must not become
// "Orchestration-{run_id}/Orchestration-{run_id}/Plan.md".
//
// RED phase note: this test passes vacuously during the RED phase because no
// path resolution exists yet — paths pass through unchanged, so there is nothing
// to double-prefix. It will provide correct regression protection once the
// implementation adds path prefixing (a double-prefix bug would be caught).
func TestSession_Start_Dispatch_DoesNotDoublePrefixAlreadyScopedPaths(t *testing.T) {
	dir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	scopedPrefix := "Orchestration-" + runID + "/"

	// Workflow where input/output are already run-scoped.
	resolveWorkflow := `<Workflow type="core" name="already-scoped" version="1.0">
## Already Scoped Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | ` + scopedPrefix + `Plan.md | ` + scopedPrefix + `Progress.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "already-scoped-orch.md")
	if err := os.WriteFile(orchPath, []byte(resolveWorkflow), 0600); err != nil {
		t.Fatalf("write already-scoped-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// agent-a returns SUCCESS → COMPLETE.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "already-scoped",
		Task:                 "task",
		RunID:                runID,
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	req := invs[0].Request

	// Input and output paths must remain exactly as specified — no double prefix.
	expectedInput := scopedPrefix + "Plan.md"
	expectedOutput := scopedPrefix + "Progress.md"
	doublePrefix := scopedPrefix + scopedPrefix

	for _, inp := range req.InputArtifacts {
		if strings.HasPrefix(inp, doublePrefix) {
			t.Errorf("InputArtifacts: double-prefixed path detected: %q", inp)
		}
		if inp == expectedInput {
			// Correct — path is present once with the right prefix.
			continue
		}
		// Any other value is unexpected.
		t.Errorf("InputArtifacts: unexpected path %q (want %q, no double prefix)", inp, expectedInput)
	}

	for _, out := range req.OutputArtifacts {
		if strings.HasPrefix(out, doublePrefix) {
			t.Errorf("OutputArtifacts: double-prefixed path detected: %q", out)
		}
		if out == expectedOutput {
			// Correct — path is present once with the right prefix.
			continue
		}
		// Any other value is unexpected.
		t.Errorf("OutputArtifacts: unexpected path %q (want %q, no double prefix)", out, expectedOutput)
	}
}

// TestSession_Start_Dispatch_PopulatesRunID_FromArtifactState_ResumedRun verifies
// that on the resume path (IsNewRun=false), every ProtocolRequest sent to the
// harness carries the RunID from the artifact state returned by Store.Read.
//
// cfg.RunID is intentionally left empty so that the only possible source for a
// non-empty ProtocolRequest.RunID is state.RunID (loaded from the artifact via
// Store.Read). This closes the resume-path gap and eliminates the
// RunID-source ambiguity present in the new-run test (both config.RunID and
// state.RunID were the same value there, making the source indistinguishable).
func TestSession_Start_Dispatch_PopulatesRunID_FromArtifactState_ResumedRun(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	const artifactRunID = "20260727T170000Z-a3f9"

	// Pre-populate the store with a run whose artifact already carries a RunID.
	// Both agents are still pending (no execution log, GlobalSequence=0) so the
	// session will dispatch both agent-a and agent-b.
	store.state = domain.ArtifactState{
		RunID:           artifactRunID,
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}
	store.exists = true

	// cfg.RunID is empty — it is not the source of ProtocolRequest.RunID on the
	// resume path. The session must derive RunID from the loaded artifact state.
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.RunID = "" // deliberately empty to distinguish from state.RunID
	cfg.RunFolder = filepath.Join(filepath.Dir(orchPath), domain.RunScopedFolder(artifactRunID))

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

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	// Every dispatched request must carry the RunID from the artifact state,
	// not from cfg.RunID (which is empty).
	for i, inv := range invs {
		if inv.Request.RunID != artifactRunID {
			t.Errorf("invocation[%d] ProtocolRequest.RunID: want %q (from artifact state), got %q",
				i, artifactRunID, inv.Request.RunID)
		}
	}
}

// TestSession_Start_Create_PassesConfigRunID_ToStore verifies that when
// IsNewRun=true, the session passes RunConfig.RunID to Store.Create as the
// runID argument. This satisfies the contract: session.go calls Store.Create
// with the minted run_id (supplied via RunConfig.RunID by the CLI/TUI layer).
func TestSession_Start_Create_PassesConfigRunID_ToStore(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	const wantRunID = "20260727T170000Z-a3f9"
	cfg := baseLinearConfig(orchPath)
	cfg.RunID = wantRunID
	cfg.IsNewRun = true

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

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The runID argument recorded by memStore.Create must match cfg.RunID.
	if store.CreatedRunID != wantRunID {
		t.Errorf("Store.Create runID argument: want %q (from cfg.RunID), got %q",
			wantRunID, store.CreatedRunID)
	}
}
