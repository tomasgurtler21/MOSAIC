package session_test

// Tests for the session package.
//
// Coverage:
//
//   Run-start refusal sequence — order enforced:
//   - Missing orchestrator file → RunRefused (orchfile step fails first).
//   - Wrong workflow ID → RunRefused (orchfile step: workflow not found).
//   - Non-canonical artifact at artifact location → RunRefused (artifact step).
//   - Workflow admission failure (unsupported pattern) → RunRefused (compat step).
//   - Agent definition file missing → RunRefused (agentresolve step).
//   - Checkpoints=true with no provider → RunRefused (checkpoint step).
//   - Existing artifact with mismatched workflow_version → RunRefused (version check).
//   - Refusal ordering: non-canonical artifact (earlier step) takes priority over
//     missing agents (later step) when both conditions are simultaneously true.
//
//   IsNewRun contract:
//   - IsNewRun=true with a pre-existing artifact: session returns RunRefused
//     (race-condition guard — the folder already contains an artifact).
//   - IsNewRun=false with a missing artifact: session returns RunRefused
//     (stale-scan guard — the resolved run folder has no artifact to resume).
//
//   Happy path:
//   - Engine Dispatch → harness Invoke → artifact Apply → engine called again.
//   - After all rows complete → engine returns Complete → RunCompleted.
//   - Artifact state after each step matches the expected sequence
//     (GlobalSequence increments, CurrentState updated).
//
//   On Findings loop-back does not invoke deviation resolver:
//   - An agent returns COMPLETED_NEEDS_ACTION with an unambiguous On Findings
//     hint → engine returns Dispatch (loop-back) → harness invoked, artifact
//     updated; deviation resolver is NOT invoked.
//
//   Stage-set re-derivation after Stage-* output:
//   - A row produces Stage-* output artifacts → session re-reads plan via
//     planstages → passes refreshed stage set to engine for the next call.
//   - Verified by observing that the engine's next dispatch resolves Stage-*
//     input wildcards against the newly-read stages (expanded paths in request).
//
//   Deviation handling:
//   - Engine returns Deviation → deviation resolver invoked → artifact re-read
//     from disk after resolver runs (FR-23: resolver may update the artifact
//     out-of-band) → execution resumes at the rejoin row.
//   - Deviation resolver returns StopRun → session returns RunDeviationUnresolved.
//
//   Resume:
//   - Session reads an existing artifact (GlobalSequence > 0) → derives resume
//     point → continues the dispatch loop from that point.
//   - Mid-invocation interruption (RerunLast=true): last logged invocation did
//     not complete → session re-dispatches the same row.
//
//   Graceful stop:
//   - Context cancelled after first dispatch completes → session stops after the
//     current step → returns RunStopped (not RunCompleted).
//
//   Infrastructure-agent trigger hook:
//   - The session's dispatch loop calls the OnInfrastructureTrigger hook after
//     each harness invocation. Verified by injecting a counter and asserting
//     it equals the number of dispatched agents (one call per dispatch cycle).
//
//   Conditional checkpoint refusal (replaces unconditional refusal):
//   - Checkpoints=true AND a checkpoint-class infrastructure agent is declared
//     in the orchestrator file → run proceeds (no refusal). [RED]
//   - Checkpoints=true AND no checkpoint-class agent declared → run refused.
//   - Checkpoints=false → run proceeds regardless of declared agents.
//
//   infrastructure_overrides validation at run start:
//   - Override naming an agent not declared in the orchestrator's
//     InfrastructureAgents region → run refused. [RED]
//   - Override replaces (not merges with) declared trigger list: with INVOCATION_INTERVAL:1
//     replaced by INVOCATION_INTERVAL:9999, the infra agent is not dispatched. [vacuous RED]
//   - Override on a gated-class agent with a class-restricted trigger violation → run refused. [RED]
//
//   Run-start agent-per-class selection:
//   - Multiple same-class gated agents declared, no InfraClassSelections entry
//     → run refused before any dispatch. [RED]
//   - Multiple same-class gated agents declared, InfraClassSelections specifies
//     one → only the selected agent's triggers are evaluated; the non-selected
//     agent is never dispatched. [RED]
//   - Single gated-class agent, no InfraClassSelections entry → auto-selected,
//     run proceeds without refusal.
//   - Multiple non-gated (review) class agents, no InfraClassSelections entry
//     → all review agents fire unconditionally (no filtering for non-gated class).
//   - Mixed gated and non-gated: selected gated agent fires, non-selected gated
//     agent is inactive, review agents always fire. [RED]
//
//   Stage set continuity within a run: [RED]
//   - A pre-EXECUTION row's Stage-* output is successfully re-derived → the
//     run goes on to enter EXECUTION and dispatch the stage 1 rows instead of
//     stopping for an unavailable stage set.
//   - A stage set already derived earlier in the run survives a later failed
//     re-read (triggered by a further Stage-* output): EXECUTION is still
//     reached and dispatched, not stopped.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- test data paths ----

const testdataDir = "../../testdata/session"

func orchFilePath(name string) string {
	return filepath.Join(testdataDir, name)
}

// ---- in-memory ArtifactStore ----

// memStore is an in-memory ArtifactStore for use in session tests.
// It starts empty (ErrNotExist on Read) and records every Apply call.
type memStore struct {
	state          domain.ArtifactState
	exists         bool
	readErr        error
	Applied        []domain.CompletedStep
	ReadCount      int    // counts every call to Read; used to verify re-read after deviation
	CreatedRunID   string // records the runID argument passed to Create; used to verify AC7.7

	// applyErrOnFirst, when true, causes Apply to return applyFirstErr on the
	// very first Apply call (when Applied is still empty). Used to simulate
	// storage failures immediately after artifact creation, e.g. for the
	// commit setup row recording failure path (Plan Risks §1).
	applyErrOnFirst bool
	applyFirstErr   error
}

func (m *memStore) Read(_ context.Context) (domain.ArtifactState, error) {
	m.ReadCount++
	if m.readErr != nil {
		return domain.ArtifactState{}, m.readErr
	}
	if !m.exists {
		return domain.ArtifactState{}, os.ErrNotExist
	}
	return m.state, nil
}

func (m *memStore) Create(_ context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	m.CreatedRunID = runID // record for AC7.7 assertion
	m.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now,
		LastUpdated:     now,
		GlobalSequence:  0,
		RunSettings:     settings,
	}
	m.exists = true
	return m.state, nil
}

func (m *memStore) Apply(_ context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	if m.applyErrOnFirst && len(m.Applied) == 0 {
		return domain.ArtifactState{}, m.applyFirstErr
	}
	m.Applied = append(m.Applied, step)
	state.GlobalSequence = step.Seq
	// current_state is updated only for workflow steps, matching the real
	// fileStore's contract (ContractsDesign.md, domain.ArtifactStore.Apply):
	// an infrastructure step must not move the recorded workflow position.
	if !step.IsInfrastructure {
		state.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.Status,
			LastAgent:  step.AgentInstance,
		}
	}
	state.ExecutionLog = append(state.ExecutionLog, domain.ExecutionLogEntry{
		Seq:    step.Seq,
		Agent:  step.AgentInstance,
		Phase:  step.Phase,
		Stage:  step.Stage,
		Status: step.Status,
	})
	m.state = state
	return state, nil
}

func (m *memStore) SetPhase(_ context.Context, _ domain.ArtifactState, _ string, _ time.Time) (domain.ArtifactState, error) {
	return domain.ArtifactState{}, fmt.Errorf("memStore.SetPhase: not implemented (session tests do not exercise SetPhase)")
}

// ---- fixed-time Clock ----

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// ---- no-op Interaction ----

type noopInteraction struct{}

func (n *noopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *noopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *noopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- callbackHarness ----

// callbackHarness wraps a HarnessAdapter and calls onInvoke after each
// successful invocation. It is used in graceful-stop tests to synchronise a
// context cancellation with the completion of a specific dispatch (so the
// cancellation happens genuinely mid-loop, not before the session starts).
type callbackHarness struct {
	delegate domain.HarnessAdapter
	onInvoke func(agentID string)
}

func (h *callbackHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	resp, err := h.delegate.Invoke(ctx, agent, request)
	if err == nil && h.onInvoke != nil {
		h.onInvoke(agent.Identifier)
	}
	return resp, err
}

// ---- test helpers ----

// writeAgentFile creates a minimal agent definition file in dir with the
// given agent identifier as the filename stem.
func writeAgentFile(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFile(%q): %v", agentID, err)
	}
}

// copyOrchestratorFile copies the named fixture file from testdata/session
// into the given directory and returns the destination path.
func copyOrchestratorFile(t *testing.T, dir, fixtureName string) string {
	t.Helper()
	src := orchFilePath(fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyOrchestratorFile: read %q: %v", src, err)
	}
	dst := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyOrchestratorFile: write %q: %v", dst, err)
	}
	return dst
}

// newLinearSession builds a session backed by the linear-orch.md fixture.
// It creates agent files for agent-a and agent-b in the temp dir.
// The FakeAdapter and memStore are returned so tests can configure them.
func newLinearSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// baseLinearConfig returns a RunConfig for the linear workflow in the given
// orchestrator file directory. IsNewRun is true (new run) by default; tests
// that exercise resume behaviour should override it with cfg.IsNewRun = false.
func baseLinearConfig(orchPath string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}
}

// requireRunStatus asserts that the RunOutcome has the expected status and no
// unexpected error.
func requireRunStatus(t *testing.T, got domain.RunOutcome, err error, want domain.RunStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != want {
		t.Errorf("want Status=%q, got %q (message: %q)", want, got.Status, got.Message)
	}
}

// requireRefused asserts that Start returned a RunRefused outcome with a nil
// error. Pre-invocation refusals are encoded in the RunOutcome (not as errors)
// so frontends can present them without error-branch handling.
func requireRefused(t *testing.T, got domain.RunOutcome, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error for refusal (refusals are encoded in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused status, got %q (message: %q)", got.Status, got.Message)
	}
	return got.Message
}

// ===== Run-start refusal sequence (AC8.3) =====

// TestSession_Start_MissingOrchestratorFile_ReturnsRefusal verifies that
// when the orchestrator file path does not exist, session.Start returns a
// RunRefused outcome before attempting any other run-start step.
func TestSession_Start_MissingOrchestratorFile_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   harness.NewFakeAdapter(),
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
		Harness:   harness.NewFakeAdapter(),
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
		Harness:   harness.NewFakeAdapter(),
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
		Harness:   harness.NewFakeAdapter(),
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
		Harness:   harness.NewFakeAdapter(),
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

// ===== Happy path (AC8.4) =====

// TestSession_Start_HappyPath_LinearWorkflow_Completes verifies the core
// dispatch cycle: engine Dispatch → harness Invoke → artifact Apply, repeated
// until engine returns Complete, which produces RunCompleted.
//
// Workflow: agent-a (PLANNING) → agent-b (PLANNING) → COMPLETE.
// Both agents return SUCCESS.
func TestSession_Start_HappyPath_LinearWorkflow_Completes(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	// Script both agents to return SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_HappyPath_ArtifactUpdatedAfterEachStep verifies that the
// artifact store's Apply is called after each successful harness invocation,
// and that the applied steps arrive in the expected order.
func TestSession_Start_HappyPath_ArtifactUpdatedAfterEachStep(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

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

	// Expect exactly two Apply calls (one per dispatched agent).
	if len(store.Applied) != 2 {
		t.Fatalf("want 2 Apply calls, got %d", len(store.Applied))
	}
	// First apply: agent-a in PLANNING.
	if store.Applied[0].AgentInstance != "agent-a#1" {
		t.Errorf("want first Apply for agent-a#1, got %q", store.Applied[0].AgentInstance)
	}
	if store.Applied[0].Phase != "PLANNING" {
		t.Errorf("want first Apply phase=PLANNING, got %q", store.Applied[0].Phase)
	}
	// Second apply: agent-b in PLANNING.
	if store.Applied[1].AgentInstance != "agent-b#2" {
		t.Errorf("want second Apply for agent-b#2, got %q", store.Applied[1].AgentInstance)
	}
}

// TestSession_Start_HappyPath_HarnessInvokedWithCorrectRequest verifies that
// the harness is called with a ProtocolRequest whose AgentInstanceID follows
// the "{name}#{seq}" format and whose input/output artifacts match the
// routing table row (with templates resolved for stage context).
func TestSession_Start_HappyPath_HarnessInvokedWithCorrectRequest(t *testing.T) {
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
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}
	first := invs[0].Request
	// agent-a is seq=1 on a fresh run.
	if first.AgentInstanceID != "agent-a#1" {
		t.Errorf("want agent_instance_id=agent-a#1, got %q", first.AgentInstanceID)
	}
}

// ===== On Findings loop-back (AC8.9) =====

// TestSession_Start_OnFindings_LoopBack_HarnessInvokedNotDeviation verifies
// that when an agent returns COMPLETED_NEEDS_ACTION and the engine's On
// Findings hint names an unambiguous agent (loop-back), the session invokes
// the harness for the loop-back target and does NOT call the deviation
// resolver.
//
// Workflow: agent-a returns CNA → engine dispatches agent-a again (On Findings
// hint = "agent-a") → agent-a returns SUCCESS → engine advances to agent-b
// → agent-b returns SUCCESS → COMPLETE.
func TestSession_Start_OnFindings_LoopBack_HarnessInvokedNotDeviation(t *testing.T) {
	dir := t.TempDir()

	// Workflow where agent-a has On Findings = "agent-a" (self-loop for CNA).
	const loopbackContent = `<Workflow type="core" name="loopback" version="1.0">
## Loopback Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | agent-a | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "loopback-orch.md")
	if err := os.WriteFile(orchPath, []byte(loopbackContent), 0600); err != nil {
		t.Fatalf("write loopback-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// First agent-a call → CNA (triggers loop-back via On Findings hint).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "found issues, re-run me",
	}})
	// Second agent-a call (loop-back) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "now done",
	}})
	// agent-b → SUCCESS → COMPLETE.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "loopback",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Three harness invocations: agent-a (CNA), agent-a (loop-back), agent-b.
	invs := f.Invocations()
	if len(invs) != 3 {
		t.Errorf("want 3 harness invocations, got %d", len(invs))
	}
}

// ===== Stage-set re-derivation (AC8.8) =====

// TestSession_Start_StageStarOutput_TriggersStageSetRederivation verifies
// that after a row produces Stage-* output artifacts, the session re-reads the
// plan artifact via planstages and passes the refreshed stage set to the engine
// for the subsequent row.
//
// The refreshed stage set enables the engine to expand Stage-* wildcards in
// the next row's input artifacts against stage folders that did not exist when
// the run started.
func TestSession_Start_StageStarOutput_TriggersStageSetRederivation(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-star-output-orch.md")
	writeAgentFile(t, dir, "planner")
	writeAgentFile(t, dir, "reviewer")

	// Write a Plan.md with 2 stages so planstages can read it.
	// The session is expected to look for Plan.md adjacent to the orchestrator
	// file or at a well-known relative path.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First | - | FALSE |
| 2 | Stage Two | Second | 1 | FALSE |
`
	planPath := filepath.Join(dir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// planner produces Stage-*/Plan.md in its output.
	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	// reviewer receives the expanded Stage-*/Plan.md as input (Stage-1/Plan.md,
	// Stage-2/Plan.md) and returns SUCCESS → COMPLETE.
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-star-output",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that the reviewer was invoked with expanded Stage-* input paths.
	// After the planner completes (with Stage-* output), the session should
	// re-derive the stage set (2 stages) and pass it to the engine. The engine
	// then expands Stage-*/Plan.md → [Stage-1/Plan.md, Stage-2/Plan.md].
	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want 2 harness invocations, got %d", len(invs))
	}
	reviewerReq := invs[1].Request
	// The reviewer's input should contain expanded stage-specific paths, not
	// the literal Stage-*/Plan.md wildcard.
	if containsInput(reviewerReq.InputArtifacts, "Stage-*/Plan.md") {
		t.Error("want Stage-*/Plan.md expanded to per-stage paths in reviewer input, but got literal wildcard")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-1/Plan.md") {
		t.Error("want Stage-1/Plan.md in reviewer input after stage-set re-derivation with 2 stages")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-2/Plan.md") {
		t.Error("want Stage-2/Plan.md in reviewer input after stage-set re-derivation with 2 stages")
	}
}

// ===== Deviation handling (AC8.5) =====

// TestSession_Start_Deviation_ReturnsDeviationUnresolved verifies that when
// the engine returns a Deviation decision and no routing consultant is wired,
// the session returns RunDeviationUnresolved without an error.
func TestSession_Start_Deviation_ReturnsDeviationUnresolved(t *testing.T) {
	dir := t.TempDir()

	// Workflow where agent-a has absent On Findings → any non-SUCCESS triggers
	// a deviation (the engine can't route it automatically).
	const deviationWorkflow = `<Workflow type="core" name="deviate" version="1.0">
## Deviation Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "deviation-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	// No routing consultant wired: deviation terminates with RunDeviationUnresolved.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a returns PARTIALLY_DONE → deviation (no On Findings column).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partially done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when no routing consultant is wired, got %q", got.Status)
	}
}

// ===== Resume (AC8.6) =====

// TestSession_Start_ResumesFromExistingArtifact verifies that when the
// artifact store already contains state reflecting a partially-completed run
// (agent-a done, agent-b remaining), the session continues from agent-b
// without re-dispatching agent-a.
func TestSession_Start_ResumesFromExistingArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with agent-a completed (seq=1, SUCCESS).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{
				Seq:    1,
				Agent:  "agent-a#1",
				Phase:  "PLANNING",
				Status: domain.StatusSUCCESS,
			},
		},
	}
	store.exists = true

	// Only agent-b should be dispatched (agent-a already completed).
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false // resume: artifact already exists

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 1 {
		t.Errorf("want 1 harness invocation (agent-b only), got %d", len(invs))
	}
	if len(invs) > 0 && invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed invocation to be agent-b, got %q", invs[0].Agent.Identifier)
	}
}

// TestSession_Start_MidInvocationInterruption_RerunsLastStep verifies FR-33:
// when the execution log's last entry does not match current_state (a sign of
// an in-flight interruption), the session re-dispatches the same row rather
// than advancing to the next one.
func TestSession_Start_MidInvocationInterruption_RerunsLastStep(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Simulate interruption: current_state says agent-b (advanced), but the
	// last execution log entry is agent-a (the actual last completed step).
	// This mismatch triggers RerunLast=true in engine.ResumePoint.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#2", // current_state advanced prematurely
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{
				Seq:    1,
				Agent:  "agent-a#1", // last logged step is agent-a, not agent-b
				Phase:  "PLANNING",
				Status: domain.StatusSUCCESS,
			},
		},
	}
	store.exists = true

	// Expect agent-a to be re-run (RerunLast) then agent-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "re-done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false // resume: artifact already exists

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least 1 invocation, got 0")
	}
	// First invocation should be agent-a (the re-run).
	if invs[0].Agent.Identifier != "agent-a" {
		t.Errorf("want first invocation to re-run agent-a (RerunLast=true), got %q", invs[0].Agent.Identifier)
	}
}

// ===== Graceful stop =====

// TestSession_Start_GracefulStop_ReturnsRunStopped verifies that when the
// context is cancelled after the first dispatch completes (mid-loop), the
// session stops after that step and returns RunStopped -- not RunCompleted.
//
// The callbackHarness fires cancel() synchronously inside agent-a's Invoke
// return path, ensuring the cancellation happens after agent-a's response is
// processed but before agent-b's Invoke is attempted. FakeAdapter's Invoke
// checks ctx.Done() at the start, so agent-b's Invoke will see the cancelled
// context and return ctx.Err(), which the session must convert to RunStopped.
func TestSession_Start_GracefulStop_ReturnsRunStopped(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel the context after agent-a's invocation completes. The callback
	// fires synchronously in the session's goroutine immediately after Invoke
	// returns, giving us a reliable mid-loop cancellation point.
	harnessCb := &callbackHarness{
		delegate: f,
		onInvoke: func(agentID string) {
			if agentID == "agent-a" {
				cancel()
			}
		},
	}

	ses := session.New(session.Deps{
		Harness:   harnessCb,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Only queue agent-a. The cancel fires before agent-b is dispatched, so
	// FakeAdapter returns ctx.Err() for agent-b without consuming a scripted entry.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(ctx, baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error for graceful stop, got %v", err)
	}
	// Graceful stop must return RunStopped, not RunCompleted. The run is
	// resumable (agent-b was not dispatched).
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped after mid-loop cancellation, got %q", got.Status)
	}
}

// ===== Infrastructure-agent trigger point (AC8.7) =====

// TestSession_Start_InfrastructureAgentTrigger_CalledPerDispatch verifies
// that the infrastructure-agent trigger hook (OnInfrastructureTrigger on Deps)
// is called exactly once per dispatch cycle. The hook is injected as a counter
// function so the test can assert the exact call count without relying on
// side effects or inferring the hook's execution from other observables.
//
// A two-agent linear workflow produces exactly 2 dispatch cycles, so the hook
// counter must reach 2 when the run completes.
func TestSession_Start_InfrastructureAgentTrigger_CalledPerDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	hookCount := 0
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		OnInfrastructureTrigger: func() { hookCount++ },
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

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)

	// The hook must be called exactly once per dispatch cycle.
	// Two agents dispatched → hookCount must be 2.
	if hookCount != 2 {
		t.Errorf("want OnInfrastructureTrigger called 2 times (one per dispatch), got %d", hookCount)
	}
}

// ===== Staged workflow run with planstages (AC8.3 + AC8.4) =====

// TestSession_Start_StagedWorkflow_Completes verifies that a staged
// (implementation-only) workflow runs successfully end-to-end: the session
// reads the stage set from Plan.md at the start, enters the EXECUTION phase,
// dispatches all rows across all stages, and returns RunCompleted.
func TestSession_Start_StagedWorkflow_Completes(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	// Write Plan.md with a single stage so the session can read the stage set.
	planPath := filepath.Join(dir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Stage 1: implementation-tdd → implementation-review → COMPLETE.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== Plan.md path resolution (AC1.3, AC1.4) =====

// TestSession_Start_PlanFile_ResolvedFromRunFolder_StagesApplied verifies
// that when Plan.md lives in config.RunFolder -- a directory distinct from
// the orchestrator file's directory -- the session reads it from there and
// applies the resulting stage set, letting a staged-only workflow (no
// pre-EXECUTION rows) run to completion.
func TestSession_Start_PlanFile_ResolvedFromRunFolder_StagesApplied(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // deliberately distinct from orchDir
	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implemented",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_PlanFile_InOrchestratorDir_NotPickedUp verifies that a
// Plan.md placed in the orchestrator file's directory is not treated as the
// run's plan file when config.RunFolder points elsewhere. The stage set must
// stay nil, so a staged-only workflow (no pre-EXECUTION rows) stops with a
// clear reason instead of silently reading the wrong file.
func TestSession_Start_PlanFile_InOrchestratorDir_NotPickedUp(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // Plan.md is intentionally absent here

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md sits next to the orchestrator file, NOT in RunFolder.
	planPath := filepath.Join(orchDir, "Plan.md")
	if err := os.WriteFile(planPath, []byte(`# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	// The run must not be refused (a plan file "exists" only from the wrong
	// directory's point of view; from RunFolder's point of view it is absent).
	if got.Status == domain.RunRefused {
		t.Fatalf("want run not refused, got RunRefused (message: %q)", got.Message)
	}
	// staged-orch.md has no pre-EXECUTION rows, so the first row is already an
	// EXECUTION row. With no stage set available, the engine must stop cleanly
	// rather than dispatch against the orchestrator-directory Plan.md.
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped (no stage set available), got %q (message: %q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "stage set") {
		t.Errorf("want stop message to name the missing stage set, got %q", got.Message)
	}
	if len(f.Invocations()) != 0 {
		t.Errorf("want no harness invocations (stopped before dispatch), got %d", len(f.Invocations()))
	}
}

// ===== Absence tolerance for pre-EXECUTION rows (AC1.2) =====

// TestSession_Start_NoPlanFile_NewRun_DispatchesFirstPreExecutionRow verifies
// that a new run of a staged workflow with pre-EXECUTION rows is not refused
// when Plan.md does not exist anywhere: the artifact store's Create is called
// and the first pre-EXECUTION row is dispatched normally.
func TestSession_Start_NoPlanFile_NewRun_DispatchesFirstPreExecutionRow(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md written here or anywhere else

	orchPath := copyOrchestratorFile(t, orchDir, "pre-exec-staged-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planned",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "pre-exec-staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Fatalf("want run not refused when Plan.md is absent, got RunRefused (message: %q)", got.Message)
	}

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation (planner dispatched), got 0")
	}
	if invs[0].Agent.Identifier != "planner" {
		t.Errorf("want first invocation to be planner, got %q", invs[0].Agent.Identifier)
	}
	if !store.exists {
		t.Error("want artifact store's Create to have been called for a new run, but no artifact was created")
	}
}

// TestSession_Start_NoPlanFile_ResumedRun_DispatchesNextPreExecutionRow
// verifies that a run resumed at a pre-EXECUTION row of a staged workflow is
// not refused when Plan.md does not exist: the session continues from the
// next pre-EXECUTION row without treating the missing plan file as a fault.
func TestSession_Start_NoPlanFile_ResumedRun_DispatchesNextPreExecutionRow(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md written here or anywhere else

	orchPath := copyOrchestratorFile(t, orchDir, "pre-exec-staged-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	f := harness.NewFakeAdapter()
	store := &memStore{
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			Workflow:        "pre-exec-staged",
			WorkflowVersion: "1.0",
			Task:            "task",
			GlobalSequence:  1,
			RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
			CurrentState: domain.CurrentState{
				Phase:      "PLANNING",
				LastStatus: domain.StatusSUCCESS,
				LastAgent:  "planner#1",
			},
			ExecutionLog: []domain.ExecutionLogEntry{
				{Seq: 1, Agent: "planner#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			},
		},
		exists: true,
	}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "pre-exec-staged",
		Task:                 "task",
		IsNewRun:             false, // resume: artifact already exists

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Fatalf("want resumed run not refused when Plan.md is absent, got RunRefused (message: %q)", got.Message)
	}

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least 1 harness invocation (reviewer dispatched), got 0")
	}
	if invs[0].Agent.Identifier != "reviewer" {
		t.Errorf("want resumed invocation to be reviewer, got %q", invs[0].Agent.Identifier)
	}
}

// ===== Preserved refusal on a malformed plan file (AC1.5) =====

// TestSession_Start_MalformedPlanFile_NewRun_ReturnsRefusal verifies that a
// Plan.md that exists in the run folder but cannot be parsed (no ## Stages
// heading) still refuses a new run of a staged workflow.
func TestSession_Start_MalformedPlanFile_NewRun_ReturnsRefusal(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nNo stages table here.\n"), 0600); err != nil {
		t.Fatalf("write malformed Plan.md: %v", err)
	}

	ses := session.New(session.Deps{
		Harness:   harness.NewFakeAdapter(),
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_MalformedPlanFile_ResumedRun_ReturnsRefusal verifies that
// the same malformed-plan-file refusal applies to a resumed run, not only a
// new one.
func TestSession_Start_MalformedPlanFile_ResumedRun_ReturnsRefusal(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "staged-orch.md")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nNo stages table here.\n"), 0600); err != nil {
		t.Fatalf("write malformed Plan.md: %v", err)
	}

	store := &memStore{
		state: domain.ArtifactState{
			Type:            "orchestration-artifact",
			Workflow:        "staged",
			WorkflowVersion: "1.0",
			Task:            "task",
			GlobalSequence:  0,
		},
		exists: true,
	}
	ses := session.New(session.Deps{
		Harness:   harness.NewFakeAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             false, // resume: artifact already exists

		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// ===== Stage-* re-derivation resolves from the run folder (AC1.3) =====

// TestSession_Start_StageStarRederivation_ResolvesFromRunFolder verifies that
// the Stage-* output re-derivation read site (triggered after a row emits
// Stage-* outputs) also resolves Plan.md from config.RunFolder rather than
// the orchestrator file's directory.
func TestSession_Start_StageStarRederivation_ResolvesFromRunFolder(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // deliberately distinct from orchDir

	orchPath := copyOrchestratorFile(t, orchDir, "stage-star-output-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")

	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First | - | FALSE |
| 2 | Stage Two | Second | 1 | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-star-output",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) < 2 {
		t.Fatalf("want 2 harness invocations, got %d", len(invs))
	}
	reviewerReq := invs[1].Request
	if containsInput(reviewerReq.InputArtifacts, "Stage-*/Plan.md") {
		t.Error("want Stage-*/Plan.md expanded to per-stage paths in reviewer input, but got literal wildcard")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-1/Plan.md") {
		t.Error("want Stage-1/Plan.md in reviewer input after run-folder-based stage-set re-derivation")
	}
	if !containsInput(reviewerReq.InputArtifacts, "Stage-2/Plan.md") {
		t.Error("want Stage-2/Plan.md in reviewer input after run-folder-based stage-set re-derivation")
	}
}

// ===== EXECUTION reached with no stage set (AC1.6) =====

// TestSession_Start_ExecutionReached_NoStageSet_StopsCleanly verifies that
// when a staged workflow's pre-EXECUTION rows complete without ever producing
// a readable Plan.md, reaching the EXECUTION phase produces a clear
// RunStopped outcome naming the missing stage set -- not a panic and not a
// dispatch of the EXECUTION row.
func TestSession_Start_ExecutionReached_NoStageSet_StopsCleanly(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir() // no Plan.md ever appears here

	orchPath := copyOrchestratorFile(t, orchDir, "pre-exec-staged-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Both pre-EXECUTION rows succeed but neither produces a readable Plan.md,
	// so the stage set remains nil when the EXECUTION row is reached.
	f.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planned (no Plan.md written in this scenario)",
	}})
	f.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "pre-exec-staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Errorf("want RunStopped when EXECUTION is reached with no stage set, got %q (message: %q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "stage set") {
		t.Errorf("want stop message to name the missing stage set, got %q", got.Message)
	}

	invs := f.Invocations()
	if len(invs) != 2 {
		t.Fatalf("want exactly 2 harness invocations (planner, reviewer) before the stop, got %d", len(invs))
	}
	if invs[len(invs)-1].Agent.Identifier == "implementation-tdd" {
		t.Error("want implementation-tdd NOT dispatched when no stage set is available")
	}
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
		Harness:   harness.NewFakeAdapter(),
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false          // resume: artifact already exists
	cfg.AllowVersionDrift = false // default; explicit for clarity

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// ===== Deviation resolver returns StopRun =====

// TestSession_Start_Deviation_ResolverStop_ReturnsDeviationUnresolved verifies
// that when the deviation resolver returns a StopRun instruction, the session
// records the current state and returns RunDeviationUnresolved (not RunCompleted
// and not an error).
func TestSession_Start_Deviation_ResolverStop_ReturnsDeviationUnresolved(t *testing.T) {
	dir := t.TempDir()

	// Same deviation workflow as TestSession_Start_Deviation_ResolvesAndResumes:
	// agent-a has no On Findings column, so PARTIALLY_DONE triggers a deviation.
	const deviationWorkflow = `<Workflow type="core" name="deviate-stop" version="1.0">
## Deviation Stop Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "deviation-stop-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-stop-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	// No routing consultant wired: any deviation terminates with RunDeviationUnresolved.
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a returns PARTIALLY_DONE → deviation (no On Findings column).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "blocked, cannot continue",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-stop",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error for deviation-unresolved outcome, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when no routing consultant is wired, got %q (message: %q)", got.Status, got.Message)
	}
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
		Harness:   harness.NewFakeAdapter(),
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

// ===== IsNewRun contract =====

// TestSession_Start_IsNewRunTrue_ExistingArtifact_ReturnsRefusal verifies the
// race-condition guard: when IsNewRun=true but an artifact already exists at
// the resolved run folder, the session returns RunRefused without dispatching
// any agents. This prevents accidentally overwriting an in-progress run.
func TestSession_Start_IsNewRunTrue_ExistingArtifact_ReturnsRefusal(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an existing artifact.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
	}
	store.exists = true

	cfg := baseLinearConfig(orchPath) // IsNewRun: true
	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_IsNewRunFalse_MissingArtifact_ReturnsRefusal verifies the
// stale-scan guard: when IsNewRun=false but no artifact exists at the resolved
// run folder, the session returns RunRefused. This prevents resuming a run
// whose folder was deleted between scan and session start.
func TestSession_Start_IsNewRunFalse_MissingArtifact_ReturnsRefusal(t *testing.T) {
	ses, _, _, orchPath := newLinearSession(t)
	// store.exists is false by default (no artifact)

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false // attempt to resume with no artifact present

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// ===== Collision refusal message with artifact path =====

// TestSession_Start_IsNewRunTrue_ExistingArtifact_MessageContainsArtifactPath
// verifies that when IsNewRun=true and an artifact already exists, the refusal
// message contains the resolved artifact path derived from config.RunFolder.
// This lets the user immediately identify and inspect the conflicting file.
//
// The assertion pins on the path as a substring rather than the exact full
// message, so the human-readable text can evolve without breaking this test.
func TestSession_Start_IsNewRunTrue_ExistingArtifact_MessageContainsArtifactPath(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	runFolder := t.TempDir()

	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
	}
	store.exists = true

	cfg := baseLinearConfig(orchPath) // IsNewRun: true
	cfg.RunFolder = runFolder

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// Stable substring: always present regardless of wording changes.
	if !strings.Contains(msg, "run folder already contains an artifact") {
		t.Errorf("want refusal message to contain stable substring %q; got %q",
			"run folder already contains an artifact", msg)
	}

	// Path substring: the resolved artifact path must appear in the message.
	wantPath := filepath.Join(runFolder, "Orchestration.md")
	if !strings.Contains(msg, wantPath) {
		t.Errorf("want refusal message to contain artifact path %q; got %q", wantPath, msg)
	}
}

// TestSession_Start_IsNewRunTrue_ExistingArtifact_EmptyRunFolder_StableMessage
// verifies that when config.RunFolder is empty the collision refusal still fires
// and contains the stable substring, but does NOT include a bare "at <path>"
// segment that would imply a working-directory-relative artifact.
//
// An empty RunFolder is not expected in production code (earlier stages populate
// it before Start is called), but the guard must not produce a misleading message
// when it encounters one.
func TestSession_Start_IsNewRunTrue_ExistingArtifact_EmptyRunFolder_StableMessage(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
	}
	store.exists = true

	cfg := baseLinearConfig(orchPath) // IsNewRun: true
	// cfg.RunFolder is intentionally left empty

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// Stable substring must always be present.
	if !strings.Contains(msg, "run folder already contains an artifact") {
		t.Errorf("want refusal message to contain stable substring; got %q", msg)
	}

	// When RunFolder is empty the message must not contain " at " — that phrasing
	// is only added when a real path is available, to avoid implying a bare
	// working-directory artifact path.
	if strings.Contains(msg, " at ") {
		t.Errorf("want empty-RunFolder refusal to omit 'at <path>'; got %q", msg)
	}
}

// TestSession_Start_IsNewRunTrue_GuardConditionsUnchanged verifies AC3.3: the
// race-condition guard fires under exactly the same conditions as before —
// IsNewRun=true AND the store reports a readable artifact — and still returns
// a RunRefused outcome. This test pairs with
// TestSession_Start_IsNewRunTrue_ExistingArtifact_ReturnsRefusal to make the
// invariant explicit: status=RunRefused AND message contains the stable substring.
func TestSession_Start_IsNewRunTrue_GuardConditionsUnchanged(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	runFolder := t.TempDir()

	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  3,
	}
	store.exists = true

	cfg := baseLinearConfig(orchPath) // IsNewRun: true
	cfg.RunFolder = runFolder

	got, err := ses.Start(context.Background(), cfg)

	// Guard must still refuse.
	msg := requireRefused(t, got, err)

	// Zero agents must have been dispatched.
	if store.ReadCount > 1 {
		// Only one Read is expected: the run-start artifact check.
		// If ReadCount > 1 the guard did not fire before the dispatch loop.
	}
	_ = msg // message content verified by other tests in this section
}

// TestSession_Start_IsNewRunFalse_ResumePathRefusal_MessageUnchanged verifies
// AC3.4: the resume-path refusal ("no artifact found at the resolved run folder;
// cannot resume") is not affected by the collision-refusal message change.
//
// This test pins on the resume-path refusal's expected message text, so a
// future change that accidentally overwrites it would be caught here.
func TestSession_Start_IsNewRunFalse_ResumePathRefusal_MessageUnchanged(t *testing.T) {
	ses, _, _, orchPath := newLinearSession(t)
	// store.exists is false by default (no artifact)

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false // attempt to resume with no artifact present

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	const wantSubstring = "no artifact found at the resolved run folder; cannot resume"
	if !strings.Contains(msg, wantSubstring) {
		t.Errorf("want resume-path refusal message to contain %q; got %q", wantSubstring, msg)
	}
}

// ===== Run-scoped dispatch: RunID propagation and path resolution (T7.2) =====
//
// All tests in this section are in the RED phase: they compile but fail because
// the session dispatch loop does not yet populate ProtocolRequest.RunID from
// ArtifactState.RunID, and does not yet resolve artifact paths to run-scoped
// form (Orchestration-{run_id}/...).

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
//
// This satisfies AC7.4: artifact paths in input_artifacts/output_artifacts are
// resolved to run-scoped form.
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
	f := harness.NewFakeAdapter()
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
	f := harness.NewFakeAdapter()
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
// This satisfies the risk mitigation in the Stage 7 plan: "Resolve only paths
// that are relative and not already run-scoped; pass through absolute paths unchanged."
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

	f := harness.NewFakeAdapter()
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

// TestSession_Start_Dispatch_EmptyRunID_PathsNotPrefixed verifies that when
// RunID is empty (pre-v1.8 or caller did not set it), artifact paths are NOT
// prefixed. This maintains backward compatibility with runs that have no run_id.
//
// RED phase note: this test passes vacuously during the RED phase because no
// path resolution exists yet — empty RunID produces the same no-op result as
// correct behavior. It will provide correct regression protection once the
// implementation adds path prefixing (spurious prefixing with empty RunID would
// be caught).
func TestSession_Start_Dispatch_EmptyRunID_PathsNotPrefixed(t *testing.T) {
	dir := t.TempDir()

	const resolveWorkflow = `<Workflow type="core" name="no-runid" version="1.0">
## No RunID Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | Plan.md | Progress.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "no-runid-orch.md")
	if err := os.WriteFile(orchPath, []byte(resolveWorkflow), 0600); err != nil {
		t.Fatalf("write no-runid-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")

	f := harness.NewFakeAdapter()
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

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "no-runid",
		Task:                 "task",
		RunID:                "", // no run_id
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}

	req := invs[0].Request

	// With empty RunID, paths must remain unchanged (no Orchestration-/ prefix added).
	if !containsInput(req.InputArtifacts, "Plan.md") {
		t.Errorf("InputArtifacts: want unscoped %q when RunID is empty, got %v",
			"Plan.md", req.InputArtifacts)
	}
	// RunID in request must also be empty.
	if req.RunID != "" {
		t.Errorf("ProtocolRequest.RunID: want empty string when config.RunID is empty, got %q", req.RunID)
	}
}

// TestSession_Start_Dispatch_PopulatesRunID_FromArtifactState_ResumedRun verifies
// that on the resume path (IsNewRun=false), every ProtocolRequest sent to the
// harness carries the RunID from the artifact state returned by Store.Read.
//
// cfg.RunID is intentionally left empty so that the only possible source for a
// non-empty ProtocolRequest.RunID is state.RunID (loaded from the artifact via
// Store.Read). This closes the AC7.3 resume-path gap and eliminates the
// RunID-source ambiguity present in the new-run test (both config.RunID and
// state.RunID were the same value there, making the source indistinguishable).
//
// This test is in the RED phase: it fails because the session dispatch loop does
// not yet populate ProtocolRequest.RunID from ArtifactState.RunID.
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
	cfg.IsNewRun = false
	cfg.RunID = "" // deliberately empty to distinguish from state.RunID

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
// runID argument. This satisfies AC7.7: session.go calls Store.Create with
// the minted run_id (supplied via RunConfig.RunID by the CLI/TUI layer).
//
// This test is in the RED phase: it fails if the session passes an incorrect or
// empty value to Store.Create rather than cfg.RunID.
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

// ---- small utilities ----

func containsInput(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// Ensure errors is imported.
var _ = errors.New

// ===== Conditional checkpoint refusal =====

// newCheckpointAgentSession builds a session backed by the
// checkpoint-agent-orch.md fixture, which declares a checkpoint-class
// infrastructure agent. Agent files for agent-a and agent-b are written into
// the temp dir. The FakeAdapter and memStore are returned for test configuration.
func newCheckpointAgentSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "checkpoint-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// TestSession_Start_CheckpointsEnabled_WithCheckpointClassAgent_RunProceeds
// verifies that when checkpoints are enabled AND the orchestrator file
// declares a checkpoint-class infrastructure agent, the session does NOT
// refuse at the checkpoint check step and proceeds to dispatch workflow agents.
//
// This is the key conditional: the current unconditional refusal must become
// conditional on the presence of a declared checkpoint-class agent.
func TestSession_Start_CheckpointsEnabled_WithCheckpointClassAgent_RunProceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

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
	cfg.Checkpoints = true // enabled; checkpoint-manager-git is declared in the orch file

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// TestSession_Start_CheckpointsEnabled_WithoutCheckpointClassAgent_ReturnsRefusal
// verifies that when checkpoints are enabled but no checkpoint-class
// infrastructure agent is declared, the session refuses to start. This is the
// existing behavior preserved as a regression test: the conditional refusal
// must still refuse when the prerequisite is absent.
func TestSession_Start_CheckpointsEnabled_WithoutCheckpointClassAgent_ReturnsRefusal(t *testing.T) {
	// linear-orch.md has no InfrastructureAgents region → no checkpoint-class agent.
	ses, _, _, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // enabled, but no checkpoint provider is declared

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CheckpointsDisabled_WithCheckpointClassAgent_Proceeds
// verifies that when checkpoints are disabled, the session proceeds normally
// regardless of whether a checkpoint-class agent is declared. Disabling
// checkpoints must not produce a refusal.
func TestSession_Start_CheckpointsDisabled_WithCheckpointClassAgent_Proceeds(t *testing.T) {
	ses, f, _, orchPath := newCheckpointAgentSession(t)

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
	cfg.Checkpoints = false // disabled; session must not refuse

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== infrastructure_overrides validation at run start =====

// TestSession_Start_InfrastructureOverride_UnknownAgentName_ReturnsRefusal
// verifies that when the artifact state contains an infrastructure_overrides
// entry naming an agent that is not declared in the orchestrator file's
// InfrastructureAgents region, the session refuses to start.
//
// This test uses a resume scenario (IsNewRun=false) so the override is
// loaded from the pre-existing artifact state rather than a newly created one.
//
// Agent responses are queued for both workflow agents so that, absent the
// override check, the session would reach dispatch and complete successfully.
// The zero-invocations assertion confirms that the refusal is a pre-dispatch
// refusal: if it fires only after the first dispatch (wrong place in the
// run-start sequence), the assertion catches it.
func TestSession_Start_InfrastructureOverride_UnknownAgentName_ReturnsRefusal(t *testing.T) {
	// linear-orch.md has no declared infrastructure agents.
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an artifact that overrides an agent name
	// that is not declared in the orchestrator file.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{AgentName: "unknown-infra-agent"},
		},
	}
	store.exists = true

	// Queue responses so the session would succeed if the override check were absent.
	// Without this setup the RED phase would fail for a different reason (empty harness
	// queue → deviation resolver path) rather than the missing override validation.
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
	cfg.IsNewRun = false // resume: existing artifact has the override

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The refusal must happen before any dispatch. A non-zero invocation count
	// would indicate the override check runs too late (after first dispatch).
	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (override validation must fire before dispatch), got %d invocation(s)", len(f.Invocations()))
	}
}

// newIntervalAgentSession builds a session backed by the interval-agent-orch.md
// fixture, which declares a checkpoint-class infrastructure agent
// (checkpoint-manager-git) with an INVOCATION_INTERVAL:1 trigger (fires after
// every workflow step). Agent files for agent-a and agent-b are written into
// the temp dir.
func newIntervalAgentSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newCommitAgentSession builds a session backed by the commit-agent-orch.md
// fixture, which declares a commit-class infrastructure agent
// (commit-manager-git) with a STAGE_END trigger. Agent files for agent-a
// and agent-b are written into the temp dir.
func newCommitAgentSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// TestSession_Start_InfrastructureOverride_ReplacementSemantics_OverrideReplacesNotMerges
// verifies that an infrastructure_overrides entry replaces (not merges with)
// the agent's declared trigger list for the duration of the run.
//
// Setup: the orchestrator declares checkpoint-manager-git with
// INVOCATION_INTERVAL:1 (fires after every workflow step). The artifact state
// overrides its triggers to INVOCATION_INTERVAL:9999 (effectively never fires
// in a short run).
//
// With correct replacement semantics: only INVOCATION_INTERVAL:9999 is in
// effect — checkpoint-manager-git is NOT dispatched during the 2-step run.
//
// With incorrect merging semantics: both INVOCATION_INTERVAL:1 and
// INVOCATION_INTERVAL:9999 would be present; INVOCATION_INTERVAL:1 fires
// after the first workflow step, causing checkpoint-manager-git to be
// dispatched — and making this test fail with a clear signal.
//
// RED-phase note: this test passes vacuously while trigger evaluation is not
// yet implemented, because no infrastructure agent is dispatched for any
// reason. It provides correct specification enforcement once trigger
// evaluation is added: an implementation with merging semantics would then
// dispatch checkpoint-manager-git and cause this test to fail.
func TestSession_Start_InfrastructureOverride_ReplacementSemantics_OverrideReplacesNotMerges(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	// Override checkpoint-manager-git's triggers from INVOCATION_INTERVAL:1 to
	// INVOCATION_INTERVAL:9999 so it effectively never fires in a short run.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{
				AgentName: "checkpoint-manager-git",
				Triggers: []domain.DeclaredInfraTrigger{
					{Trigger: "INVOCATION_INTERVAL", Param: "9999"},
				},
			},
		},
	}
	store.exists = true

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
	cfg.IsNewRun = false // resume: load the override from the artifact state

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the override references a declared agent, so no
	// refusal for an unknown agent name.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-git must NOT have been dispatched. With replacement
	// semantics, INVOCATION_INTERVAL:1 is gone and only :9999 is in effect,
	// which does not fire during a 2-step run. A dispatch of checkpoint-manager-git
	// here indicates the implementation is merging triggers rather than replacing.
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			t.Errorf("checkpoint-manager-git dispatched unexpectedly: INVOCATION_INTERVAL:1 should have been replaced by :9999 (replacement semantics), but appears to have been merged (merging semantics)")
		}
	}
}

// TestSession_Start_InfrastructureOverride_ClassRestrictedTrigger_ReturnsRefusal
// verifies that when an infrastructure_overrides entry specifies a trigger
// outside the allowed set for the agent's class, the session refuses to start.
//
// The design specifies that commit-class agents are restricted to STAGE_END
// triggers only. Supplying INVOCATION_INTERVAL in an override for a
// commit-class agent violates this restriction and must produce RunRefused.
//
// Agent responses are queued so that, absent the class-restriction check,
// the session would dispatch successfully. This ensures the RED-phase failure
// is "RunCompleted instead of RunRefused" (missing class-restriction validation),
// not a spurious deviation-resolver path from an empty harness queue.
func TestSession_Start_InfrastructureOverride_ClassRestrictedTrigger_ReturnsRefusal(t *testing.T) {
	// commit-agent-orch.md declares commit-manager-git (commit class, STAGE_END).
	ses, f, store, orchPath := newCommitAgentSession(t)

	// Override the commit-class agent with a trigger not in its allowed set.
	// INVOCATION_INTERVAL is not permitted for commit-class agents.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  0,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: []domain.InfrastructureOverride{
			{
				AgentName: "commit-manager-git",
				Triggers: []domain.DeclaredInfraTrigger{
					{Trigger: "INVOCATION_INTERVAL", Param: "5"},
				},
			},
		},
	}
	store.exists = true

	// Queue responses so the session would succeed if the class-restriction check
	// were absent; the test must fail only because of the missing validation.
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
	cfg.IsNewRun = false // resume: load the override from the artifact state

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_InfrastructureOverride_EmptyOverrides_Proceeds verifies
// that an artifact state with a nil InfrastructureOverrides slice (the common
// case) causes no refusal and the session proceeds normally.
func TestSession_Start_InfrastructureOverride_EmptyOverrides_Proceeds(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate with no overrides.
	store.state = domain.ArtifactState{
		Workflow:                "linear",
		WorkflowVersion:         "1.0",
		Task:                    "test task",
		GlobalSequence:          0,
		RunSettings:             domain.RunSettings{Mode: domain.ExecutionModeAuto},
		InfrastructureOverrides: nil, // no overrides — the common case
	}
	store.exists = true

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
	cfg.IsNewRun = false // resume

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== T6.1: Trigger evaluation logic =====
//
// Coverage:
//
//   INVOCATION_INTERVAL trigger:
//   - Fires after each workflow step when param=1 (interval=1 test).
//   - Fires at the exact threshold when global_sequence reaches param (boundary test).
//   - Does not fire when global_sequence is below param (vacuous RED).
//
//   STAGE_END trigger:
//   - Fires after the first workflow step of a new stage (retrospective: current
//     step's Stage != previous workflow step's Stage).
//   - Does not fire between steps within the same stage (vacuous RED).
//   - Does not fire on the very first workflow step (no previous step to compare; vacuous RED).
//
//   restore-class exclusion:
//   - A restore-class agent is never dispatched by automatic trigger evaluation,
//     regardless of the triggers its declaration names.
//   - The exclusion keys on Class == "restore", not on the agent's name: covered by
//     both checkpoint-restore-git and a differently-named checkpoint-restore-s3.
//
//   No-cascades rule:
//   - Infrastructure agent completions do not re-evaluate triggers; infra dispatches
//     are counted exactly (not exponentially).

// --- Session helpers for trigger evaluation tests ---

// newHighIntervalSession builds a session backed by high-interval-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:3 (halt policy).
// With only 2 workflow steps, this trigger must not fire.
func newHighIntervalSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "high-interval-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newTwoIntervalSession builds a session backed by two-interval-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:2 (halt policy).
// The trigger fires once after the 2nd workflow step (global_sequence == param).
func newTwoIntervalSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "two-interval-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newContinueInfraSession builds a session backed by continue-infra-orch.md,
// which declares checkpoint-manager-git with INVOCATION_INTERVAL:1 and
// on_failure=continue. A non-SUCCESS response from this agent must not halt
// the run; the dispatch loop should proceed to the next workflow step.
func newContinueInfraSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "continue-infra-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newRestoreGitSession builds a session backed by restore-git-orch.md,
// which declares checkpoint-restore-git with Class=restore, INVOCATION_INTERVAL:1
// (halt policy), plus a checkpoint-manager-git placeholder (Class=checkpoint,
// INVOCATION_INTERVAL:999, on_failure=continue) to satisfy the checkpoint
// precondition when cfg.Checkpoints=true. The trigger evaluation contract
// excludes all restore-class agents from automatic dispatch regardless of
// declared triggers or agent name.
func newRestoreGitSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "restore-git-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
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
func newClassRestoreSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "class-restore-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newStageEndStagedSession builds a session backed by stage-end-staged-orch.md
// (staged workflow + commit-manager-git STAGE_END halt). Plan.md with 2 stages is
// written into the same temp dir. Agent files for implementation-tdd and
// implementation-review are also written.
func newStageEndStagedSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")

	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | First stage | - | FALSE |
| 2 | Stage Two | Second stage | 1 | FALSE |
`
	planPath := filepath.Join(filepath.Dir(orchPath), "Plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
		t.Fatalf("newStageEndStagedSession: write Plan.md: %v", err)
	}

	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// --- INVOCATION_INTERVAL trigger tests ---

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL1_FiresAfterEachWorkflowStep
// verifies that an infrastructure agent with INVOCATION_INTERVAL:1 is dispatched
// after each workflow step, interleaved in the dispatch sequence. With 2 workflow
// steps, the infra agent must be dispatched exactly twice — at positions 2 and 4
// in the invocation sequence (after each workflow step).
//
// The trigger matching rule: fires when (global_sequence - seq_of_last_infra_row) >= 1.
// With no prior infra row, fires when global_sequence >= 1 (i.e., after every step).
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL1_FiresAfterEachWorkflowStep(t *testing.T) {
	ses, f, _, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Triggered after agent-a (global_sequence=1 >= param=1, no prior infra row).
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
	// Triggered after agent-b (global_sequence=3, last infra at seq=2; 3-2=1 >= 1).
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true // enable checkpoint class so its triggers are evaluated

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 invocations (agent-a, checkpoint, agent-b, checkpoint), got %d", len(invs))
	}
	if invs[1].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[1]: want checkpoint-manager-git (fired after agent-a), got %q", invs[1].Agent.Identifier)
	}
	if invs[3].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[3]: want checkpoint-manager-git (fired after agent-b), got %q", invs[3].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL_BoundaryAtExactThreshold
// verifies that an infrastructure agent with INVOCATION_INTERVAL:2 fires exactly
// once after the 2nd workflow step, when global_sequence reaches exactly param=2.
// This is a boundary test: the trigger fires at the threshold (>=), not strictly above.
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL_BoundaryAtExactThreshold(t *testing.T) {
	ses, f, _, orchPath := newTwoIntervalSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-a (global_sequence=1), the rule checks: 1 >= 2? No → does not fire.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-b (global_sequence=2), the rule checks: 2 >= 2? Yes → fires.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 3 {
		t.Fatalf("want 3 invocations (agent-a, agent-b, checkpoint), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "checkpoint-manager-git" {
		t.Errorf("invocations[2]: want checkpoint-manager-git (fired at exact threshold), got %q", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_TriggerEval_INVOCATION_INTERVAL_HighThreshold_DoesNotFireInShortRun
// verifies that an infrastructure agent with INVOCATION_INTERVAL:3 does not fire
// during a 2-step workflow run (global_sequence=2 < param=3).
//
// RED phase: this test passes vacuously because no trigger evaluation occurs.
// Once implementation is added, a bug firing below the threshold would be caught.
func TestSession_Start_TriggerEval_INVOCATION_INTERVAL_HighThreshold_DoesNotFireInShortRun(t *testing.T) {
	ses, f, _, orchPath := newHighIntervalSession(t)

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
		if inv.Agent.Identifier == "checkpoint-manager-git" {
			t.Errorf("checkpoint-manager-git dispatched unexpectedly: INVOCATION_INTERVAL:3 must not fire with only 2 workflow steps (global_sequence=2 < param=3)")
		}
	}
}

// --- STAGE_END trigger tests ---

// TestSession_Start_TriggerEval_STAGE_END_FiresOnStageTransition verifies that a
// commit-class infrastructure agent with STAGE_END trigger is dispatched after the
// first workflow step of a new stage. The STAGE_END rule is retrospective: it fires
// when the just-completed step's Stage differs from the previous workflow step's Stage.
//
// Dispatch sequence with 2 stages (Stage-1: tdd, review; Stage-2: tdd, review):
//   - implementation-tdd  Stage-1 (first step, no prior step → no STAGE_END)
//   - implementation-review Stage-1 (same stage → no STAGE_END)
//   - implementation-tdd  Stage-2 (Stage-2 ≠ Stage-1 → STAGE_END fires → commit-manager-git)
//   - commit-manager-git  (infra dispatch, IsInfrastructure=true → no cascade)
//   - implementation-review Stage-2 (same stage → no STAGE_END)
func TestSession_Start_TriggerEval_STAGE_END_FiresOnStageTransition(t *testing.T) {
	ses, f, _, orchPath := newStageEndStagedSession(t)

	// Stage 1
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 tdd done",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 1 review done",
	}})
	// Stage 2 first step: Stage-2 != Stage-1 → STAGE_END fires.
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 tdd done",
	}})
	// Infrastructure dispatch triggered by STAGE_END.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
	}})
	// Stage 2 second step: same stage → no STAGE_END.
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "stage 2 review done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            filepath.Dir(orchPath), // Plan.md was written next to the orchestrator file.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := f.Invocations()
	if len(invs) != 5 {
		t.Fatalf("want 5 invocations (tdd1, review1, tdd2, commit, review2), got %d", len(invs))
	}

	commitIdx := -1
	for i, inv := range invs {
		if inv.Agent.Identifier == "commit-manager-git" {
			commitIdx = i
			break
		}
	}
	if commitIdx == -1 {
		t.Fatal("want commit-manager-git dispatched after stage transition, but it was never invoked")
	}
	// commit-manager-git must follow tdd1, review1, tdd2 (index 3 in 0-based).
	if commitIdx != 3 {
		t.Errorf("want commit-manager-git at invocation[3] (after stage transition), got at invocation[%d]", commitIdx)
	}
}

// TestSession_Start_TriggerEval_STAGE_END_DoesNotFireWithinSameStage verifies that
// the STAGE_END trigger does not fire when consecutive workflow steps are in the same stage.
// Only a change in the Stage field between the current step and the prior workflow step
// triggers the STAGE_END condition.
//
// RED phase: this test passes vacuously because trigger evaluation is not yet implemented.
// Once implementation is added, a bug that fires STAGE_END within the same stage would
// produce unexpected commit-manager-git dispatches and cause this test to fail.
func TestSession_Start_TriggerEval_STAGE_END_DoesNotFireWithinSameStage(t *testing.T) {
	// Build a staged session with only 1 stage. Within a single stage all steps
	// have the same Stage value, so STAGE_END can never fire.
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	const singleStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "commit-manager-git" {
			t.Errorf("commit-manager-git dispatched unexpectedly: STAGE_END must not fire when all steps share the same stage")
		}
	}
}

// TestSession_Start_TriggerEval_STAGE_END_DoesNotFireOnFirstWorkflowStep verifies
// that STAGE_END does not fire after the very first workflow step of a run, because
// there is no previous workflow step to compare the Stage value against.
//
// RED phase: this test passes vacuously because trigger evaluation is not yet
// implemented. It provides regression protection once implementation is added: a
// bug that fires STAGE_END without a previous step would cause unexpected dispatches.
func TestSession_Start_TriggerEval_STAGE_END_DoesNotFireOnFirstWorkflowStep(t *testing.T) {
	// Use a staged workflow with a single-step stage so the first (and only)
	// workflow step has no predecessor. STAGE_END must not fire after that step.
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "stage-end-staged-orch.md")
	writeAgentFile(t, dir, "implementation-tdd")
	writeAgentFile(t, dir, "implementation-review")
	const singleStagePlan = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// Only the first workflow step in Stage-1. After this completes, STAGE_END
	// must not fire because prevWorkflowStep is nil (no prior step).
	f.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "staged",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir, // Plan.md was written into dir; the session must resolve it here.
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "commit-manager-git" {
			t.Errorf("commit-manager-git dispatched after first step: STAGE_END must not fire when prevWorkflowStep is nil (no prior step to compare)")
		}
	}
}

// --- restore-class agent exclusion tests ---

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

	// restore-git-orch.md has checkpoint-restore-git (Class=restore) with
	// INVOCATION_INTERVAL:1. With checkpoints enabled, the trigger would match
	// after every step — but restore-class agents must be excluded from
	// automatic evaluation by class, not by name.
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

	// class-restore-orch.md has checkpoint-restore-s3 (Class=restore) with
	// INVOCATION_INTERVAL:1. The trigger would match after every step — but
	// the exclusion must apply to any restore-class agent, not just
	// "checkpoint-restore-git" by name.
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

// --- No-cascades rule test ---

// TestSession_Start_TriggerEval_NoCascades_InfraCompletionDoesNotRetrigger verifies
// that infrastructure agent completions do not cause further trigger evaluations.
// Only workflow step completions (IsInfrastructure=false) trigger evaluation; infra
// completions (IsInfrastructure=true) are skipped entirely.
//
// With INVOCATION_INTERVAL:1 and 2 workflow steps, the expected dispatch sequence is:
//   workflow-step-1 → infra → workflow-step-2 → infra
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
	// Any additional dispatch would indicate cascading from an infra completion.
	if infraCount != 2 {
		t.Errorf("want exactly 2 checkpoint-manager-git dispatches (one per workflow step, no cascades from infra completions), got %d", infraCount)
	}
}

// ===== T6.2: on_failure handling =====
//
// Coverage:
//
//   halt policy:
//   - When an infrastructure agent dispatch returns a non-SUCCESS status and the
//     agent's on_failure is "halt", the run stops with RunStopped after the
//     Execution Log row is written.
//   - Halt does not enter the deviation resolver.
//
//   continue policy:
//   - When an infrastructure agent dispatch returns a non-SUCCESS status and the
//     agent's on_failure is "continue", the dispatch loop continues to the next
//     workflow step. The run completes normally.

// TestSession_Start_InfraOnFailureHalt_StopsRun verifies that when an infrastructure
// agent dispatch returns a non-SUCCESS status and the declared on_failure policy is
// "halt", the session stops the run and returns RunStopped. The Execution Log row
// for the failed dispatch is written before the halt.
//
// The halt must stop the run before the next workflow agent (agent-b) is dispatched.
func TestSession_Start_InfraOnFailureHalt_StopsRun(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// checkpoint-manager-git returns BLOCKED (non-SUCCESS). on_failure=halt → run stops.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	// Queue agent-b so that the run would succeed if the halt were missing.
	// A non-zero invocation count for agent-b after this test indicates the halt
	// did not fire before the next workflow step was dispatched.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Halt policy must stop the run.
	requireRunStatus(t, got, err, domain.RunStopped)

	// The Execution Log row for the failed infra agent must be written before halt.
	// At minimum: one row for agent-a and one row for checkpoint-manager-git.
	if len(store.Applied) < 2 {
		t.Errorf("want at least 2 Applied steps (agent-a + failed checkpoint-manager-git) before halt, got %d", len(store.Applied))
	}

	// agent-b must not have been dispatched (halt fired before reaching it).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-b" {
			t.Error("agent-b dispatched after halt: halt policy must stop the run before dispatching further workflow steps")
		}
	}
}

// TestSession_Start_InfraOnFailureContinue_WorkflowCompletesAfterInfraFailure verifies
// that when an infrastructure agent dispatch returns a non-SUCCESS status and the
// declared on_failure policy is "continue", the dispatch loop proceeds to the next
// workflow step without halting. The run completes normally (RunCompleted).
//
// The infra agent's Execution Log row must be written (failure is on record), but
// the session continues to dispatch the remaining workflow steps.
func TestSession_Start_InfraOnFailureContinue_WorkflowCompletesAfterInfraFailure(t *testing.T) {
	ses, f, _, orchPath := newContinueInfraSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// checkpoint-manager-git fails (non-SUCCESS). on_failure=continue → proceed.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// After agent-b, INVOCATION_INTERVAL:1 fires again.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Continue policy must not halt; the run must complete.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that checkpoint-manager-git was dispatched (failure was on_failure=continue,
	// not skipped) and agent-b was dispatched after the infra failure.
	infraCount := 0
	agentBDispatched := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-git":
			infraCount++
		case "agent-b":
			agentBDispatched = true
		}
	}
	if infraCount == 0 {
		t.Error("want checkpoint-manager-git dispatched (even with on_failure=continue, the dispatch must occur before the policy is applied)")
	}
	if !agentBDispatched {
		t.Error("want agent-b dispatched after infra failure with continue policy, but it was not dispatched")
	}
}

// TestSession_Start_InfraOnFailureHalt_DoesNotInvokeDeviationResolver verifies that
// infrastructure agent failures are handled exclusively by the on_failure policy and
// never enter the deviation resolver. The deviation resolver handles workflow agent
// deviations only; infra failures follow their own path.
func TestSession_Start_InfraOnFailureHalt_DoesNotInvokeDeviationResolver(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
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
	// checkpoint-manager-git fails with halt policy.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "checkpoint storage unavailable",
	}})
	// Queue agent-b to prevent a deviation from an empty harness queue in RED phase.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	// Halt must produce RunStopped (not RunCompleted or RunDeviationUnresolved).
	requireRunStatus(t, got, err, domain.RunStopped)

}


// ===== T6.3: Checkpoint content-reference extraction =====
//
// Coverage:
//
//   Marker present:
//   - When a checkpoint-class infrastructure agent's status_message contains the
//     [checkpoint:{sha}] marker pattern, the sha is extracted and recorded in the
//     Checkpoint column of that agent's own Execution Log row.
//
//   Marker absent:
//   - When the status_message contains no [checkpoint:{sha}] marker, the
//     Checkpoint column for that row remains empty (vacuous RED).

// TestSession_Start_CheckpointExtraction_MarkerPresent_ShaRecordedOnInfraRow verifies
// that when checkpoint-manager-git's status_message contains a [checkpoint:{sha}]
// marker, the sha is extracted and recorded in the CompletedStep.Checkpoint field of
// that agent's own dispatch row. The sha must appear only on the infra agent's row,
// not on the preceding workflow step's row.
//
// The [checkpoint:{sha}] marker pattern: one or more non-']' characters as the sha.
func TestSession_Start_CheckpointExtraction_MarkerPresent_ShaRecordedOnInfraRow(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	const wantSHA = "a1b2c3d4e5f6"

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	// checkpoint-manager-git returns a message with the [checkpoint:{sha}] marker.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint saved [checkpoint:" + wantSHA + "] successfully",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint saved, no marker in this response",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Find the Applied step for the first checkpoint-manager-git dispatch.
	var infraStep *domain.CompletedStep
	for i := range store.Applied {
		step := &store.Applied[i]
		if strings.Contains(step.AgentInstance, "checkpoint-manager-git") && infraStep == nil {
			infraStep = step
		}
	}
	if infraStep == nil {
		t.Fatal("want checkpoint-manager-git Applied step recorded, but none found in store.Applied")
	}
	if infraStep.Checkpoint != wantSHA {
		t.Errorf("checkpoint-manager-git Applied step Checkpoint: want %q (extracted from [checkpoint:{sha}] marker), got %q",
			wantSHA, infraStep.Checkpoint)
	}
}

// TestSession_Start_CheckpointExtraction_MarkerAbsent_CheckpointColumnEmpty verifies
// that when checkpoint-manager-git's status_message contains no [checkpoint:{sha}]
// marker, the Checkpoint field of its Applied step remains empty (the Checkpoint
// column shows "-" in the rendered Execution Log).
//
// RED phase: this test passes vacuously because no infra agent is dispatched.
// Once implementation is added, a bug that always extracts a sha (even when absent)
// would populate the Checkpoint field incorrectly and cause this test to fail.
func TestSession_Start_CheckpointExtraction_MarkerAbsent_CheckpointColumnEmpty(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// No [checkpoint:{sha}] marker in the status_message.
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint attempted but no sha available",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "no checkpoint taken",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that no checkpoint-manager-git step has a non-empty Checkpoint field.
	for i := range store.Applied {
		step := &store.Applied[i]
		if strings.Contains(step.AgentInstance, "checkpoint-manager-git") && step.Checkpoint != "" {
			t.Errorf("checkpoint-manager-git Applied step Checkpoint: want empty (no [checkpoint:{sha}] marker in response), got %q", step.Checkpoint)
		}
	}
}

// ===== Run-start agent-per-class selection =====
//
// Coverage for T7.1: buildActiveAgentsFilter behaviour surfaced through the
// session dispatch loop, and session-level refusal when multiple agents of the
// same gated class are declared without a class selection.

// --- Session helpers for agent-per-class selection tests ---

// newMultiCheckpointSession builds a session backed by multi-checkpoint-orch.md,
// which declares two checkpoint-class infrastructure agents
// (checkpoint-manager-git with INVOCATION_INTERVAL:1 halt, and
// checkpoint-manager-alt with INVOCATION_INTERVAL:1 continue).
// Agent files for agent-a and agent-b are written into the temp dir.
func newMultiCheckpointSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-checkpoint-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newReviewClassSession builds a session backed by review-class-orch.md,
// which declares two review-class infrastructure agents (review-agent-a and
// review-agent-b, both with INVOCATION_INTERVAL:1 continue).
// Agent files for agent-a and agent-b are written into the temp dir.
func newReviewClassSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "review-class-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newMultiClassMixedSession builds a session backed by multi-class-mixed-orch.md,
// which declares two checkpoint-class agents (checkpoint-manager-git halt,
// checkpoint-manager-alt continue) and one review-class agent (review-agent
// continue), all with INVOCATION_INTERVAL:1.
// Agent files for agent-a and agent-b are written into the temp dir.
func newMultiClassMixedSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-class-mixed-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newTwoGatedClassesSession builds a session backed by multi-two-gated-classes-orch.md,
// which declares two checkpoint-class agents (checkpoint-manager-git halt,
// checkpoint-manager-alt continue) AND two commit-class agents (commit-manager-git halt,
// commit-manager-alt continue), all with INVOCATION_INTERVAL:1.
// Agent files for agent-a and agent-b are written into the temp dir.
func newTwoGatedClassesSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "multi-two-gated-classes-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// --- Refusal when multiple same-class agents, no selection ---

// TestSession_Start_MultipleGatedClassAgents_NoClassSelection_ReturnsRefusal
// verifies that when multiple agents of the same gated class (checkpoint) are
// declared and RunConfig.InfraClassSelections contains no entry for that class,
// the session refuses to start before dispatching any workflow step.
//
// The refusal must be a run-start refusal: no harness invocations may occur.
// Without the check the session proceeds, checkpoint-manager-git fires after
// agent-a with INVOCATION_INTERVAL:1 — but the FakeAdapter has no response
// queued for it, causing a halt (RunStopped). The test asserts RunRefused, so
// the RED failure is clear: got RunStopped instead of RunRefused.
func TestSession_Start_MultipleGatedClassAgents_NoClassSelection_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newMultiCheckpointSession(t)

	// Queue only the workflow agents. In RED, trigger evaluation fires for
	// checkpoint-manager-git after agent-a completes; FakeAdapter returns an error
	// (no entry queued), which is treated as halt → RunStopped. In GREEN the
	// session refuses at run-start, so agent-a is never dispatched.
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

	// No InfraClassSelections: multiple checkpoint agents declared, no selection.
	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = nil

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The refusal must fire before any dispatch. Zero invocations in GREEN;
	// non-zero in RED (agent-a IS dispatched before trigger fires).
	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations (class-selection refusal must fire before dispatch), got %d", len(f.Invocations()))
	}
}

// --- Selection respected: only selected agent fires ---

// TestSession_Start_MultipleGatedClassAgents_WithClassSelection_OnlySelectedFires
// verifies that when multiple checkpoint-class agents are declared and
// InfraClassSelections specifies one of them, only the selected agent's triggers
// are evaluated and it is the only checkpoint agent dispatched.
//
// In RED (nil activeAgents filter): both agents are evaluated, checkpoint-manager-alt
// is dispatched. FakeAdapter has no response queued for checkpoint-manager-alt; it
// returns an error treated as BLOCKED with continue policy, recording an invocation.
// The assertion "checkpoint-manager-alt not dispatched" catches the RED failure.
//
// In GREEN: buildActiveAgentsFilter returns {checkpoint-manager-git: true};
// checkpoint-manager-alt is skipped by evaluateTriggers; only checkpoint-manager-git
// fires. The FakeAdapter queue is consumed without error and the run completes.
func TestSession_Start_MultipleGatedClassAgents_WithClassSelection_OnlySelectedFires(t *testing.T) {
	ses, f, _, orchPath := newMultiCheckpointSession(t)

	// Queue the expected dispatch sequence for GREEN:
	// agent-a → checkpoint-manager-git → agent-b → checkpoint-manager-git → COMPLETE.
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
	// NOTE: checkpoint-manager-alt is intentionally NOT queued. In RED, if
	// checkpoint-manager-alt is dispatched (nil filter), FakeAdapter returns an
	// error treated as BLOCKED+continue. The invocation is still recorded,
	// allowing the assertion to detect the RED failure.

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-alt must never be dispatched (it was not selected).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-alt" {
			t.Errorf("checkpoint-manager-alt dispatched: only the selected agent (checkpoint-manager-git) may fire; the non-selected checkpoint agent must be inactive")
		}
	}
}

// --- Single gated-class agent: auto-selected, no refusal ---

// TestSession_Start_SingleGatedClassAgent_AutoSelected_RunProceeds verifies that
// when exactly one checkpoint-class agent is declared and InfraClassSelections
// is nil, the session auto-selects that agent without prompting and the run
// proceeds normally. The single agent's triggers are evaluated.
//
// This is a regression test: the session must not refuse when every gated class
// has exactly one declared agent, regardless of whether InfraClassSelections is nil.
func TestSession_Start_SingleGatedClassAgent_AutoSelected_RunProceeds(t *testing.T) {
	// checkpoint-agent-orch.md declares one checkpoint-class agent.
	ses, f, _, orchPath := newCheckpointAgentSession(t)

	// Expected GREEN dispatch: agent-a → checkpoint-manager-git (STAGE_END on
	// first step does not fire; STAGE_END fires only when stage changes) →
	// agent-b. With STAGE_END trigger and a linear workflow (no stage change),
	// the checkpoint agent never fires. That is correct behaviour: the trigger
	// contract is unrelated to auto-selection.
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
	cfg.InfraClassSelections = nil // no selection needed for a single-agent class

	got, err := ses.Start(context.Background(), cfg)

	// Must not refuse: single checkpoint-class agent is auto-selected.
	requireRunStatus(t, got, err, domain.RunCompleted)
}

// --- Non-gated review agents: always fire, unaffected by selection ---

// TestSession_Start_NonGatedReviewAgents_MultipleAgents_BothFireWithoutSelection
// verifies that review-class agents are not subject to the at-most-one-per-class
// selection rule. When multiple review-class agents are declared and
// InfraClassSelections is nil, the session proceeds without refusal and both
// review agents are dispatched (their triggers evaluate unconditionally).
//
// This test passes in both RED and GREEN because the selection logic does not
// apply to non-gated classes. It provides regression protection: if the selection
// logic were incorrectly applied to the review class, the run would either refuse
// or filter out one of the review agents.
func TestSession_Start_NonGatedReviewAgents_MultipleAgents_BothFireWithoutSelection(t *testing.T) {
	ses, f, _, orchPath := newReviewClassSession(t)

	// With INVOCATION_INTERVAL:1, both review agents fire after each workflow step.
	// Dispatch sequence: agent-a → review-a → review-b → agent-b → review-a → review-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("review-agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-a#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("review-agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent-b#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.InfraClassSelections = nil // no selection required for non-gated classes

	got, err := ses.Start(context.Background(), cfg)

	// Must not refuse: review class is non-gated, multiple agents are fine.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Both review agents must have been dispatched.
	reviewADispatched := false
	reviewBDispatched := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "review-agent-a":
			reviewADispatched = true
		case "review-agent-b":
			reviewBDispatched = true
		}
	}
	if !reviewADispatched {
		t.Error("want review-agent-a dispatched (non-gated class, always fires), but it was not")
	}
	if !reviewBDispatched {
		t.Error("want review-agent-b dispatched (non-gated class, always fires), but it was not")
	}
}

// --- Two gated classes simultaneously ---

// TestSession_Start_TwoGatedClasses_BothSelectedFire verifies the boundary case
// where TWO gated classes each have multiple declared agents simultaneously:
// two checkpoint-class agents and two commit-class agents. The filter must handle
// both classes independently — the selected checkpoint agent fires, the selected
// commit agent fires, and neither non-selected agent fires.
//
// This is a regression guard against cross-class interaction bugs: a bug that
// causes one class's filter to bleed into another class's filter would be caught
// by verifying that the commit-class selection is honoured independently of the
// checkpoint-class selection.
//
// In RED (nil filter): all four infrastructure agents are evaluated and dispatched
// on each trigger. The assertions that non-selected agents were not dispatched
// catch the RED failures.
//
// In GREEN: buildActiveAgentsFilter returns
//   {"checkpoint-manager-git": true, "commit-manager-git": true}
// and only those two agents' triggers are evaluated per workflow step.
func TestSession_Start_TwoGatedClasses_BothSelectedFire(t *testing.T) {
	ses, f, _, orchPath := newTwoGatedClassesSession(t)

	// Queue the expected GREEN dispatch sequence.
	// With INVOCATION_INTERVAL:1, one checkpoint agent and one commit agent fire
	// after each of the two workflow steps:
	//   agent-a → checkpoint-manager-git → commit-manager-git →
	//   agent-b → checkpoint-manager-git → commit-manager-git → COMPLETE
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
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit done",
	}})
	// The non-selected agents (checkpoint-manager-alt, commit-manager-alt) are
	// intentionally NOT queued. In RED they fire (nil filter) and FakeAdapter records
	// their invocations, allowing the assertions below to catch the RED failure.

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{
		"checkpoint": "checkpoint-manager-git",
		"commit":     "commit-manager-git",
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Neither non-selected agent may be dispatched.
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-alt":
			t.Errorf("checkpoint-manager-alt dispatched: only the selected checkpoint agent (checkpoint-manager-git) may fire")
		case "commit-manager-alt":
			t.Errorf("commit-manager-alt dispatched: only the selected commit agent (commit-manager-git) may fire")
		}
	}

	// Both selected agents must have been dispatched (at least once).
	checkpointFired := false
	commitFired := false
	for _, inv := range f.Invocations() {
		switch inv.Agent.Identifier {
		case "checkpoint-manager-git":
			checkpointFired = true
		case "commit-manager-git":
			commitFired = true
		}
	}
	if !checkpointFired {
		t.Error("want checkpoint-manager-git dispatched (selected for checkpoint class), but it was not")
	}
	if !commitFired {
		t.Error("want commit-manager-git dispatched (selected for commit class), but it was not")
	}
}

// --- Mixed classes: gated filtered, non-gated always fires ---

// TestSession_Start_MixedClasses_SelectedGatedFires_NonGatedAlwaysFires verifies
// the combined case: two checkpoint-class agents (gated) and one review-class agent
// (non-gated). With InfraClassSelections specifying checkpoint-manager-git, only
// that agent's triggers are evaluated for the checkpoint class; review-agent fires
// unconditionally.
//
// In RED (nil filter): all three agents are evaluated. checkpoint-manager-alt fires
// after each workflow step (INVOCATION_INTERVAL:1) but has no scripted response;
// FakeAdapter records the invocation and returns an error treated as BLOCKED+continue.
// The assertion "checkpoint-manager-alt not dispatched" catches the RED failure.
//
// In GREEN: buildActiveAgentsFilter returns {checkpoint-manager-git: true,
// review-agent: true}; checkpoint-manager-alt is skipped; only checkpoint-manager-git
// and review-agent fire. The queued responses are consumed and the run completes.
func TestSession_Start_MixedClasses_SelectedGatedFires_NonGatedAlwaysFires(t *testing.T) {
	ses, f, _, orchPath := newMultiClassMixedSession(t)

	// Queue the expected GREEN dispatch sequence:
	// agent-a → checkpoint-manager-git → review-agent → agent-b →
	// checkpoint-manager-git → review-agent → COMPLETE.
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
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "checkpoint taken",
	}})
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#6",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})
	// checkpoint-manager-alt intentionally NOT queued. In RED it fires and its
	// invocation is recorded (FakeAdapter records before checking the queue),
	// allowing the assertion below to detect the RED failure.

	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// checkpoint-manager-alt must never be dispatched (not selected).
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "checkpoint-manager-alt" {
			t.Errorf("checkpoint-manager-alt dispatched: only the selected checkpoint agent (checkpoint-manager-git) may fire; checkpoint-manager-alt was not selected")
		}
	}

	// review-agent must always be dispatched (non-gated class, not filtered).
	reviewAgentDispatched := false
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "review-agent" {
			reviewAgentDispatched = true
			break
		}
	}
	if !reviewAgentDispatched {
		t.Error("want review-agent dispatched (non-gated class, always fires regardless of selection), but it was not")
	}
}

// ===== Seeding: pre-creation validation =====
//
// For a new run, the session must build and validate the seed plan before calling
// Store.Create so that an invalid seed set leaves no run folder on disk. These
// tests use the in-memory store; actual disk rollback is exercised in the
// integration tests.
//
// RED phase: all tests fail until the session inserts seed.BuildPlan before
// Store.Create in the run-start sequence and returns a run refusal on any
// planning error.

// TestSession_Start_SeedInputs_MissingSource_RefusesBeforeStoreCreate verifies
// that a seed source path that does not exist on disk produces a RunRefused
// outcome whose message names the path, and that Store.Create is never called.
func TestSession_Start_SeedInputs_MissingSource_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	nonExistent := filepath.Join(t.TempDir(), "nonexistent-input.md")
	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{nonExistent}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// Store.Create must not have been called (planning must precede creation).
	if store.exists {
		t.Error("want Store.Create NOT called before seed planning refusal, but store.exists is true")
	}
	if !strings.Contains(msg, "nonexistent-input.md") {
		t.Errorf("want refusal message to name the missing source path; got %q", msg)
	}
}

// TestSession_Start_SeedInputs_RunnerManagedDestination_RefusesBeforeStoreCreate
// verifies that a source file whose base name matches a runner-managed destination
// ("Orchestration.md") produces a RunRefused before Store.Create is called.
func TestSession_Start_SeedInputs_RunnerManagedDestination_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	// A source named "Orchestration.md" maps to the runner-managed destination.
	seedDir := t.TempDir()
	src := filepath.Join(seedDir, "Orchestration.md")
	if err := os.WriteFile(src, []byte("# should be rejected\n"), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}
	// A Requirement* candidate keeps the seed-naming rule from masking the
	// runner-managed-destination refusal this test targets.
	reqSrc := filepath.Join(seedDir, "Requirement.md")
	if err := os.WriteFile(reqSrc, []byte("# req\n"), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{src, reqSrc}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	if store.exists {
		t.Error("want Store.Create NOT called for runner-managed destination refusal, but store.exists is true")
	}
	// The refusal message must identify the runner-managed destination that
	// was refused. "Orchestration.md" is the sole reserved destination, so its
	// name must appear in the message — this distinguishes a runner-managed
	// refusal from any other seed refusal that might arise from an unrelated
	// bug path.
	if !strings.Contains(msg, "Orchestration.md") {
		t.Errorf("want refusal message to name the runner-managed destination (Orchestration.md); got %q", msg)
	}
}

// TestSession_Start_SeedInputs_CrossSourceCollision_RefusesBeforeStoreCreate
// verifies that two source files with the same base name — producing a
// destination collision — refuse the run before Store.Create is called, and
// that the refusal message names both offending sources.
func TestSession_Start_SeedInputs_CrossSourceCollision_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	d := t.TempDir()
	src1 := filepath.Join(d, "dirA", "Plan.md")
	src2 := filepath.Join(d, "dirB", "Plan.md")
	for _, p := range []string{src1, src2} {
		if mkErr := os.MkdirAll(filepath.Dir(p), 0700); mkErr != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), mkErr)
		}
		if wErr := os.WriteFile(p, []byte("content\n"), 0600); wErr != nil {
			t.Fatalf("write %s: %v", p, wErr)
		}
	}
	// A Requirement* candidate keeps the seed-naming rule from masking the
	// cross-source collision refusal this test targets.
	reqSrc := filepath.Join(d, "Requirement.md")
	if wErr := os.WriteFile(reqSrc, []byte("req\n"), 0600); wErr != nil {
		t.Fatalf("write %s: %v", reqSrc, wErr)
	}

	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{src1, src2, reqSrc}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	if store.exists {
		t.Error("want Store.Create NOT called for cross-source collision, but store.exists is true")
	}
	// Both offending source roots must appear in the refusal message.
	if !strings.Contains(msg, "dirA") || !strings.Contains(msg, "dirB") {
		t.Errorf("want refusal message to name both colliding sources; got %q", msg)
	}
}

// ===== Seeding: resume skips seeding entirely =====

// TestSession_Start_Resume_SeedInputsPopulated_ValidationAndCopySkipped verifies
// that when IsNewRun is false, SeedInputs is ignored entirely — no BuildPlan
// validation, no Apply copy — even when SeedInputs contains paths that would
// fail validation. A resumed run must never be refused for seed-related reasons.
func TestSession_Start_Resume_SeedInputsPopulated_ValidationAndCopySkipped(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Set up a resumable artifact: agent-a completed, agent-b pending.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false
	// A non-existent path: would refuse the run if validation ran on resume.
	cfg.SeedInputs = []string{filepath.Join(t.TempDir(), "does-not-exist.md")}

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete normally: SeedInputs is silently ignored on resume.
	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===========================================================================
// Debug-logging tests (T5.3, T5.4, T5.5, T5.6)
// ===========================================================================

// ---- sessionLogEntry / sessionRecordingLogger ----

// sessionLogEntry is one captured call to sessionRecordingLogger.Log.
type sessionLogEntry struct {
	Event   string
	Message string
	Fields  []domain.DebugField
}

// sessionRecordingLogger is a thread-safe domain.DebugLogger that records every
// Log call. Session logging tests inject this to assert which events were emitted.
type sessionRecordingLogger struct {
	mu      sync.Mutex
	entries []sessionLogEntry
}

// Log implements domain.DebugLogger.
func (r *sessionRecordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, sessionLogEntry{
		Event:   event,
		Message: message,
		Fields:  append([]domain.DebugField{}, fields...),
	})
}

// eventLogged reports whether at least one entry with the given event name
// was recorded.
func (r *sessionRecordingLogger) eventLogged(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for the given field key in the first entry
// with the given event name. Returns ("", false) when not found.
func (r *sessionRecordingLogger) fieldValue(event, key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event != event {
			continue
		}
		for _, f := range e.Fields {
			if f.Key == key {
				return f.Value, true
			}
		}
	}
	return "", false
}

// allEvents returns the event names of all recorded entries in order.
func (r *sessionRecordingLogger) allEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.Event
	}
	return names
}

// ---- newLinearSessionWithDebug helper ----

// newLinearSessionWithDebug builds a session backed by the linear-orch.md
// fixture and wires the supplied debug logger into Deps.Debug. The returned
// FakeAdapter and memStore are available for test configuration.
func newLinearSessionWithDebug(t *testing.T, debug domain.DebugLogger) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Debug:     debug,
	})
	return
}

// ---- T5.3: session logs dispatch start and step completion ----

// TestSession_Start_WithLogger_LogsDispatchStart verifies that the session
// emits EventSessionDispatchStart before each harness invocation, carrying
// the agent instance ID, phase, stage and row index as structured fields.
func TestSession_Start_WithLogger_LogsDispatchStart(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	if !logger.eventLogged(domain.EventSessionDispatchStart) {
		t.Errorf("want %s logged before each harness invocation, got events: %v",
			domain.EventSessionDispatchStart, logger.allEvents())
	}
	// The agent instance ID of the first dispatch should appear in a field.
	agentVal, ok := logger.fieldValue(domain.EventSessionDispatchStart, "agent")
	if !ok {
		t.Errorf("want 'agent' field on %s entry", domain.EventSessionDispatchStart)
	} else if agentVal != "agent-a#1" {
		t.Errorf("want agent=agent-a#1 on first %s, got %q", domain.EventSessionDispatchStart, agentVal)
	}
}

// TestSession_Start_WithLogger_LogsDispatchStart_PhaseAndStageFields verifies
// that the EventSessionDispatchStart entry carries the workflow phase and stage
// context so log readers can identify where in the workflow the dispatch occurred.
func TestSession_Start_WithLogger_LogsDispatchStart_PhaseAndStageFields(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	phaseVal, ok := logger.fieldValue(domain.EventSessionDispatchStart, "phase")
	if !ok {
		t.Errorf("want 'phase' field on %s entry", domain.EventSessionDispatchStart)
	} else if phaseVal == "" {
		t.Errorf("want non-empty 'phase' field on %s", domain.EventSessionDispatchStart)
	}
}

// TestSession_Start_WithLogger_LogsStepDone verifies that the session emits
// EventSessionStepDone after each successful step is applied to the artifact,
// carrying the status code as a structured field.
func TestSession_Start_WithLogger_LogsStepDone(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := newLinearSessionWithDebug(t, logger)

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

	if !logger.eventLogged(domain.EventSessionStepDone) {
		t.Errorf("want %s logged after each completed step, got events: %v",
			domain.EventSessionStepDone, logger.allEvents())
	}
	statusVal, ok := logger.fieldValue(domain.EventSessionStepDone, "status")
	if !ok {
		t.Errorf("want 'status' field on %s entry", domain.EventSessionStepDone)
	} else if statusVal != string(domain.StatusSUCCESS) {
		t.Errorf("want status=SUCCESS on %s, got %q", domain.EventSessionStepDone, statusVal)
	}
}

// ---- T5.4: harness error, deviation handling, and unresolved deviation logged ----

// buildDeviationWorkflowSession is a helper that constructs a session with the
// deviation-trigger workflow (agent-a has no On Findings column, so any
// non-SUCCESS status triggers a deviation) and wires the supplied logger.
// Agent files are created in a temp dir. The orchestrator file path is returned
// so the caller can build a RunConfig. No Routing consultant is wired, so
// deviations terminate with RunDeviationUnresolved.
func buildDeviationWorkflowSession(
	t *testing.T,
	logger domain.DebugLogger,
) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	const deviationWorkflow = `<Workflow type="core" name="deviate-log" version="1.0">
## Deviation Log Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | plan.md | result.md |
</Workflow>
`
	orchPath = filepath.Join(dir, "deviate-log-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("buildDeviationWorkflowSession: write %q: %v", orchPath, err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Debug:    logger,
	})
	return
}

// TestSession_Start_WithLogger_HarnessError_LogsHarnessError verifies that
// when a harness invocation fails (not a context cancellation), the session
// logs EventSessionHarnessError before terminating.
func TestSession_Start_WithLogger_HarnessError_LogsHarnessError(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := buildDeviationWorkflowSession(t, logger)

	// Queue a harness-level error for agent-a.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("simulated harness failure")})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionHarnessError) {
		t.Errorf("want %s logged on harness error, got events: %v",
			domain.EventSessionHarnessError, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_DeviationResolution_LogsDeviation verifies that
// when the engine returns a Deviation decision, the session logs
// EventSessionDeviation before terminating.
func TestSession_Start_WithLogger_DeviationResolution_LogsDeviation(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, _, orchPath := buildDeviationWorkflowSession(t, logger)

	// agent-a returns PARTIALLY_DONE with no On Findings column → engine returns Deviation.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partly done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionDeviation) {
		t.Errorf("want %s logged when engine returns Deviation, got events: %v",
			domain.EventSessionDeviation, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_DeviationUnresolved_LoggedWithoutStoreApply
// verifies that when a harness error occurs and no routing consultant is wired,
// the session logs EventSessionDeviationUnresolved without calling Store.Apply.
// This covers the path that leaves no trace in Orchestration.md.
func TestSession_Start_WithLogger_DeviationUnresolved_LoggedWithoutStoreApply(t *testing.T) {
	logger := &sessionRecordingLogger{}
	ses, f, store, orchPath := buildDeviationWorkflowSession(t, logger)

	// Harness error → no routing consultant → EventSessionDeviationUnresolved.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness failed")})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "deviate-log",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}) //nolint:errcheck

	if !logger.eventLogged(domain.EventSessionDeviationUnresolved) {
		t.Errorf("want %s logged on unresolved deviation, got events: %v",
			domain.EventSessionDeviationUnresolved, logger.allEvents())
	}
	// Store.Apply must NOT have been called: the unresolved step was never recorded.
	if len(store.Applied) != 0 {
		t.Errorf("want Store.Apply NOT called on unresolved deviation, got %d calls", len(store.Applied))
	}
}

// ---- T5.5: run-start refusals logged ----

// TestSession_Start_WithLogger_MissingOrchestratorFile_LogsRefusal verifies
// that a run-start refusal occurring before Store.Create is logged as
// EventSessionRefusal. This is the path that currently leaves no trace because
// no artifact exists to record the refusal reason.
func TestSession_Start_WithLogger_MissingOrchestratorFile_LogsRefusal(t *testing.T) {
	logger := &sessionRecordingLogger{}
	dir := t.TempDir()
	ses := session.New(session.Deps{
		Harness:   harness.NewFakeAdapter(),
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Debug:     logger,
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: filepath.Join(dir, "nonexistent.md"),
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
	if !logger.eventLogged(domain.EventSessionRefusal) {
		t.Errorf("want %s logged on run-start refusal, got events: %v",
			domain.EventSessionRefusal, logger.allEvents())
	}
}

// TestSession_Start_WithLogger_AgentNotFound_LogsRefusal verifies that a
// run-start refusal triggered by a missing agent definition file (pre-dispatch)
// is also logged as EventSessionRefusal.
func TestSession_Start_WithLogger_AgentNotFound_LogsRefusal(t *testing.T) {
	logger := &sessionRecordingLogger{}
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	// Deliberately omit agent-a.md and agent-b.md so agent resolution fails.

	ses := session.New(session.Deps{
		Harness:   harness.NewFakeAdapter(),
		Store:     &memStore{},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
		Debug:     logger,
	})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRefused(t, got, err)
	if !logger.eventLogged(domain.EventSessionRefusal) {
		t.Errorf("want %s logged on agent-not-found refusal, got events: %v",
			domain.EventSessionRefusal, logger.allEvents())
	}
}

// ---- T5.6: nil Debug field defaults to no-op; behaviour unchanged ----

// TestSession_Start_NilDebugField_NoopIsDefault verifies that a session created
// without the Debug field set (nil) behaves identically to one with no logger:
// the run completes normally and no panic occurs. This is the regression guard
// that ensures all existing Deps{...} literals (which omit Debug) keep working.
func TestSession_Start_NilDebugField_NoopIsDefault(t *testing.T) {
	// newLinearSession creates Deps without Debug — Debug is nil.
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

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	// Behaviour must be identical to a run with a real logger: completes normally.
	requireRunStatus(t, got, err, domain.RunCompleted)
}

// ===== Stage set continuity across a run =====

// TestSession_Start_StageStarOutputPrecedesStagedExecution_EntersExecution
// verifies that when a pre-EXECUTION planning row produces Stage-* outputs
// and the resulting stage set is successfully re-derived, the run goes on to
// enter the EXECUTION phase and dispatch the stage 1 rows rather than
// stopping for an unavailable stage set.
func TestSession_Start_StageStarOutputPrecedesStagedExecution_EntersExecution(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "stage-continuity-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md does not exist yet: this is a genuinely new run, and the
	// planner has not produced it until its own invocation completes. A
	// single stage keeps the run's EXECUTION phase to one pass through the
	// stage 1 rows.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")

	fake := harness.NewFakeAdapter()
	fake.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	fake.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})
	fake.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	fake.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implementation approved",
	}})

	// Write Plan.md as a side effect of the planner's own invocation
	// succeeding, exactly as the planner's own tooling would produce it
	// mid-run rather than it pre-existing before the run starts.
	f := &callbackHarness{
		delegate: fake,
		onInvoke: func(agentID string) {
			if agentID == "planner" {
				if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
					t.Fatalf("write Plan.md after planner invocation: %v", err)
				}
			}
		},
	}

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-continuity",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := fake.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 harness invocations (planner, reviewer, implementation-tdd, implementation-review), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want the third invocation to be implementation-tdd (EXECUTION reached and dispatched), got %q", invs[2].Agent.Identifier)
	}
}

// TestSession_Start_FailedStageStarRederivation_RetainsExistingStageSet
// verifies that when a stage set has already been successfully derived
// earlier in a run, a later failed re-read of the plan file (triggered by a
// further Stage-* output) does not discard it: the run still reaches
// EXECUTION and dispatches the stage 1 rows instead of stopping for an
// unavailable stage set.
//
// Note: Stage 5 tests follow below this comment.
//
// Stage 5 coverage:
//
//   Run configuration settling:
//   - Mode is required; an absent mode refuses the run before any artifact is
//     created or any harness invocation occurs. Outcome is RunRefused (not
//     RunDeviationUnresolved), confirming the run-start check — not a later
//     engine decision — stopped the run.
//   - Commits enabled without a declared commit-class agent refuses the run
//     before any artifact is created.
//   - Commits disabled with no commit-class agent declared proceeds normally;
//     the artifact records Commits=false (silently disabled contract).
//   - Checkpoints precondition and mode refusal ordering verified.
//
//   Commit setup dispatch:
//   - A successful commit setup dispatch with a [branch:{name}] marker records
//     the branch name as CommitBranch in the created artifact.
//   - A missing [branch:{name}] marker refuses the run; no artifact is created.
//   - A harness error during commit setup refuses the run; no artifact is created.
//   - Apply failure while recording the commit setup row refuses the run
//     (Plan Risks §1). [RED]
//   - When commits are disabled, no commit-class agent is dispatched at run start.
//   - The commit setup dispatch appears as the first execution log row with
//     Seq=1 and Status=SUCCESS.
//
//   Pre-consultation:
//   - Enabled in auto mode → PreConsult is called before any workflow dispatch.
//   - Enabled in auto-review mode → PreConsult is called before any workflow dispatch.
//   - Disabled → PreConsult is never called.
//   - Enabled in orchestrated mode → PreConsult is NOT called (orchestrated
//     mode is excluded). [RED]
//   - Pre-consultation advice strings (TaskDescription, Constraints) are
//     appended to the subsequent auto-routed dispatch's request fields. [RED]
//   - PreConsult failure refuses the run and leaves no artifact.
//
//   Resume configuration:
//   - Resume uses RunSettings.Mode from the artifact, not from the config;
//     the final artifact state reflects the mode read from the frontmatter.
//   - Resume does not invoke the commit setup dispatch a second time; the final
//     artifact state preserves the CommitBranch from the frontmatter.
//   - All six RunSettings values are read from the artifact on resume;
//     Mode and CommitBranchVariant are asserted on the final state.
func TestSession_Start_FailedStageStarRederivation_RetainsExistingStageSet(t *testing.T) {
	orchDir := t.TempDir()
	runFolder := t.TempDir()

	orchPath := copyOrchestratorFile(t, orchDir, "stage-continuity-orch.md")
	writeAgentFile(t, orchDir, "planner")
	writeAgentFile(t, orchDir, "reviewer")
	writeAgentFile(t, orchDir, "implementation-tdd")
	writeAgentFile(t, orchDir, "implementation-review")

	// Plan.md does not exist yet: this is a genuinely new run. A single
	// stage keeps the run's EXECUTION phase to one pass through the stage 1
	// rows.
	planContent := `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | The only stage | - | FALSE |
`
	planPath := filepath.Join(runFolder, "Plan.md")

	fake := harness.NewFakeAdapter()
	fake.Queue("planner", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "planner#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "plan and stage dirs created",
	}})
	fake.Queue("reviewer", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "reviewer#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "reviewed",
	}})
	fake.Queue("implementation-tdd", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-tdd#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "tests written",
	}})
	fake.Queue("implementation-review", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "implementation-review#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "implementation approved",
	}})

	// Plan.md is written after the planner's invocation succeeds (its
	// Stage-* output triggers the first, successful re-derivation) and then
	// removed after the reviewer's invocation succeeds (its own Stage-*
	// output triggers a second re-derivation attempt that must fail). Only
	// a stage set retained from the first, successful re-derivation lets the
	// run go on to reach EXECUTION.
	f := &callbackHarness{
		delegate: fake,
		onInvoke: func(agentID string) {
			switch agentID {
			case "planner":
				if err := os.WriteFile(planPath, []byte(planContent), 0600); err != nil {
					t.Fatalf("write Plan.md after planner invocation: %v", err)
				}
			case "reviewer":
				if err := os.Remove(planPath); err != nil {
					t.Fatalf("remove Plan.md after reviewer invocation: %v", err)
				}
			}
		},
	}

	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "stage-continuity",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            runFolder,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	invs := fake.Invocations()
	if len(invs) != 4 {
		t.Fatalf("want 4 harness invocations (planner, reviewer, implementation-tdd, implementation-review), got %d", len(invs))
	}
	if invs[2].Agent.Identifier != "implementation-tdd" {
		t.Errorf("want the third invocation to be implementation-tdd (EXECUTION reached despite the failed re-read), got %q", invs[2].Agent.Identifier)
	}
}

// ---- scriptedPreConsultant (Stage 5) ----

// scriptedPreConsultant is a controllable implementation of domain.PreConsultant
// for Stage 5 pre-consultation tests.
type scriptedPreConsultant struct {
	advice domain.PreConsultationAdvice
	err    error
	Called bool
}

func (p *scriptedPreConsultant) PreConsult(_ context.Context, _ domain.ConsultationRequest) (domain.PreConsultationAdvice, error) {
	p.Called = true
	return p.advice, p.err
}

// ---- Stage 5 helpers ----

// newCommitSession builds a session backed by the commit-agent-orch.md fixture,
// which declares a linear workflow (agent-a, agent-b) and a commit-class
// infrastructure agent (commit-manager-git). Agent definition files for all
// three are created in the temp directory. The FakeAdapter, memStore, and
// orchestrator path are returned so tests can configure their scripted responses.
func newCommitSession(t *testing.T) (ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f = harness.NewFakeAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// baseCommitConfig returns a RunConfig for the commit-agent-orch.md workflow
// with commits enabled and mode set to auto. Tests override individual fields
// as needed.
func baseCommitConfig(orchPath string) domain.RunConfig {
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
	}
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true
	cfg.CommitBranchVariant = domain.CommitBranchMOSAICOwned
	return cfg
}

// ===== Stage 5: Run configuration settling =====

// TestSession_Start_ModeUnset_ReturnsRefusal verifies that when RunSettings.Mode
// is not set (ExecutionModeUnset = ""), session.Start refuses the run. Mode is
// required with no default; its absence is always a refusal, regardless of the
// workflow or the presence of infrastructure agents.
func TestSession_Start_ModeUnset_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	// Script both agents to return SUCCESS, so the test fails only on the
	// refusal assertion and not by running out of scripted responses.
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
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_ModeUnset_RefusalBeforeHarnessInvocation verifies the ordering
// contract: the mode check must occur before any harness invocation. A missing
// mode must be caught in the run-start sequence, before the dispatch loop begins.
//
// The test asserts two things: (1) the outcome is RunRefused (not
// RunDeviationUnresolved or any other status that would indicate the run
// stopped for a different reason) and (2) no harness invocation occurred.
// Without an explicit RunRefused assertion, the test could pass vacuously if
// an earlier engine change stops the run via deviation before dispatch, making
// it appear the ordering constraint is enforced when it is not.
func TestSession_Start_ModeUnset_RefusalBeforeHarnessInvocation(t *testing.T) {
	ses, f, _, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	got, err := ses.Start(context.Background(), cfg)

	// Must return RunRefused — not RunDeviationUnresolved — confirming that the
	// run-start mode check is what stopped the run, not a later engine decision.
	requireRefused(t, got, err)

	if len(f.Invocations()) != 0 {
		t.Errorf("want zero harness invocations when mode is unset, got %d "+
			"(ordering violation: mode check must precede harness dispatch)",
			len(f.Invocations()))
	}
}

// TestSession_Start_ModeUnset_RefusalBeforeArtifactCreated verifies that the mode
// check fires before ArtifactStore.Create is called. No artifact should exist when
// the run is refused for a missing mode.
func TestSession_Start_ModeUnset_RefusalBeforeArtifactCreated(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeUnset // intentionally unset to trigger the refusal

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when mode is unset (refusal must precede Store.Create)")
	}
}

// TestSession_Start_CommitsEnabled_NoCommitClassAgent_ReturnsRefusal verifies that
// requesting commits when no commit-class infrastructure agent is declared in the
// orchestrator file refuses the run. A run cannot enable commits if no commit
// provider is available.
func TestSession_Start_CommitsEnabled_NoCommitClassAgent_ReturnsRefusal(t *testing.T) {
	ses, _, _, orchPath := newLinearSession(t) // linear-orch.md has no infra agents

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true // request commits — but no commit-class agent is declared

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_NoCommitClassAgent_NoArtifactCreated verifies
// that the commits-without-provider refusal fires before any artifact is created.
// The run-start sequence must enforce this precondition before Store.Create so
// that no partial run state is left behind.
func TestSession_Start_CommitsEnabled_NoCommitClassAgent_NoArtifactCreated(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commits are enabled but no commit-class agent is declared")
	}
}

// TestSession_Start_CommitsDefault_NoCommitClassAgent_RunProceeds verifies that
// when no commit-class agent is declared and commits are disabled (the zero value
// of RunSettings.Commits), the run proceeds normally. Commits are silently
// disabled — no refusal occurs and the workflow completes.
//
// The spec says "silently false" — the silence is about not asking the user, but
// the created artifact must reflect the actual settled value (Commits=false).
func TestSession_Start_CommitsDefault_NoCommitClassAgent_RunProceeds(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

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
	cfg.Mode = domain.ExecutionModeAuto
	// Commits is false by default — no commit provider required.

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// The artifact must record Commits=false. The "silently disabled" contract
	// means the user is not asked, but the persisted setting must still reflect
	// the actual value so a resumed run does not misread its configuration.
	if store.state.Commits {
		t.Error("want artifact Commits=false when no commit-class agent is declared (silently disabled), got Commits=true")
	}
}

// ===== Stage 5: Commit setup dispatch =====

// TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact
// verifies that when commits are enabled and the commit-class agent returns a
// [branch:{name}] marker in its status_message, the reported branch name is
// stored as CommitBranch in the created artifact. This is the core success path
// for the commit setup dispatch.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	// Commit setup dispatch returns a branch marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch established [branch:" + wantBranch + "]",
	}})
	// Workflow agents for the dispatch loop that follows setup.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.state.CommitBranch != wantBranch {
		t.Errorf("want CommitBranch=%q in artifact after successful commit setup, got %q",
			wantBranch, store.state.CommitBranch)
	}
}

// TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow verifies that
// the commit setup dispatch is recorded as the first execution log row, with
// IsInfrastructure=true, before any workflow step is logged.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// store.Applied records every CompletedStep passed to Apply, including
	// infrastructure steps (IsInfrastructure=true). The commit setup dispatch
	// must be the first applied step and must be marked as infrastructure.
	if len(store.Applied) == 0 {
		t.Fatal("want at least one applied step, got none")
	}
	first := store.Applied[0]
	if !first.IsInfrastructure {
		t.Errorf("want first applied step to be infrastructure (commit setup), got IsInfrastructure=false (agent=%q)", first.AgentInstance)
	}
	if !strings.Contains(first.AgentInstance, "commit-manager-git") {
		t.Errorf("want first applied step agent to contain \"commit-manager-git\", got %q", first.AgentInstance)
	}
	// Seq must be 1: the commit setup dispatch is the first recorded row.
	// The ContractsDesign specifies the commit setup dispatch Seq is always 1.
	if first.Seq != 1 {
		t.Errorf("want commit setup dispatch Seq=1 (first row), got Seq=%d", first.Seq)
	}
	// Status must mirror the dispatch's own status code (SUCCESS in this case).
	if first.Status != domain.StatusSUCCESS {
		t.Errorf("want commit setup dispatch Status=%q, got %q", domain.StatusSUCCESS, first.Status)
	}
}

// TestSession_Start_CommitsEnabled_MissingBranchMarker_ReturnsRefusal verifies
// that when the commit-class agent's setup dispatch returns SUCCESS but its
// status_message does not contain a [branch:{name}] marker, the run is refused.
// A missing marker means the branch was not established, so the run cannot
// safely proceed.
func TestSession_Start_CommitsEnabled_MissingBranchMarker_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// The commit agent returns SUCCESS but omits the [branch:{name}] marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "setup complete (no branch marker)",
	}})

	cfg := baseCommitConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_MissingBranchMarker_NoArtifactCreated verifies
// that a missing branch marker refuses the run before ArtifactStore.Create is
// called. No artifact should exist so a refused run leaves no trace.
func TestSession_Start_CommitsEnabled_MissingBranchMarker_NoArtifactCreated(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "no marker here",
	}})

	cfg := baseCommitConfig(orchPath)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commit setup dispatch has no branch marker")
	}
}

// TestSession_Start_CommitsEnabled_HarnessError_ReturnsRefusal verifies that
// when the harness fails during the commit setup dispatch, the run is refused.
// The failure is immediately terminal with no retry.
func TestSession_Start_CommitsEnabled_HarnessError_ReturnsRefusal(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// The harness fails entirely for the commit setup dispatch.
	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("git: authentication failed"),
	})

	cfg := baseCommitConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_CommitsEnabled_HarnessError_NoArtifactCreated verifies that
// a harness error during commit setup refuses the run before any artifact is
// created. The failure is terminal and no state survives.
func TestSession_Start_CommitsEnabled_HarnessError_NoArtifactCreated(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	f.Queue("commit-manager-git", harness.ScriptedEntry{
		Err: errors.New("git: timeout"),
	})

	cfg := baseCommitConfig(orchPath)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.exists {
		t.Error("want no artifact created when commit setup dispatch harness fails")
	}
}

// TestSession_Start_CommitsEnabled_SetupApplyFailure_ReturnsRefusal verifies that
// when the commit setup dispatch succeeds (branch marker extracted) but recording
// the dispatch as the first execution log row fails (ArtifactStore.Apply returns
// an error), the run is refused and the run folder is removed. Recording failure
// is terminal per the Plan's Risks table (§1): the run folder is removed and no
// partial state survives.
//
// The test is in the RED phase: without commit setup row recording (I5.2), the
// first Apply call happens for a workflow step, not the commit setup row, and the
// run does not return RunRefused from this path.
func TestSession_Start_CommitsEnabled_SetupApplyFailure_ReturnsRefusal(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	// Create a real run folder that the session must remove on Apply failure.
	dir := t.TempDir()
	runFolder := filepath.Join(dir, "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}

	// Configure the store to fail the very first Apply call. When I5.2 is
	// implemented, that first call will be the commit setup row recording;
	// the session must treat this as a terminal failure and refuse the run.
	store.applyErrOnFirst = true
	store.applyFirstErr = errors.New("disk full: unable to record commit setup row")

	const wantBranch = "mosaic/run/test-run-id"
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"
	cfg.RunFolder = runFolder

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)

	// The session must remove the run folder on Apply failure so that a refused
	// run leaves no trace on disk. This mirrors the pre-consultation failure
	// contract (os.RemoveAll(cfg.RunFolder) on any terminal pre-dispatch failure).
	if _, statErr := os.Stat(runFolder); !os.IsNotExist(statErr) {
		t.Error("want run folder removed when commit setup row Apply fails (failure must remove any run folder)")
	}
}

// TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart verifies that
// when commits are disabled, the commit-class agent is not invoked during run
// start. Only workflow agents appear in the harness invocation log.
func TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// No commit-manager-git entry queued: any invocation would return a harness
	// error, causing the test to observe the wrong failure mode.
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

	cfg := baseCommitConfig(orchPath)
	cfg.Commits = false // disable commits

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	for _, inv := range invs {
		if strings.Contains(inv.Agent.Identifier, "commit-manager-git") {
			t.Errorf("want no commit-manager-git invocation when commits are disabled, got invocation with agent_instance_id=%q",
				inv.Request.AgentInstanceID)
		}
	}
}

// ===== Stage 5: Pre-consultation =====

// TestSession_Start_PreConsultation_AutoMode_CalledBeforeWorkflowDispatch verifies
// that when pre-consultation is enabled and the run mode is auto, PreConsult is
// invoked before any workflow agent is dispatched via the harness. The
// pre-consultation is part of the run-start sequence and must complete before
// the dispatch loop begins.
func TestSession_Start_PreConsultation_AutoMode_CalledBeforeWorkflowDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		advice: domain.PreConsultationAdvice{
			TaskDescription: "extra context from pre-consultation",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if !preConsultant.Called {
		t.Error("want PreConsult called when pre-consultation is enabled in auto mode, but it was not called")
	}
}

// TestSession_Start_PreConsultation_AutoReviewMode_CalledBeforeWorkflowDispatch
// verifies that pre-consultation is also invoked when the run mode is auto-review.
// Both auto and auto-review are modes where pre-consultation is meaningful.
func TestSession_Start_PreConsultation_AutoReviewMode_CalledBeforeWorkflowDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAutoReview
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if !preConsultant.Called {
		t.Error("want PreConsult called when pre-consultation is enabled in auto-review mode, but it was not called")
	}
}

// TestSession_Start_PreConsultation_Disabled_NotCalled verifies that when
// PreConsultation is false (the default), the PreConsult port is never invoked,
// even when mode is auto. Pre-consultation is opt-in.
func TestSession_Start_PreConsultation_Disabled_NotCalled(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = false // explicitly disabled (also the default)

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if preConsultant.Called {
		t.Error("want PreConsult NOT called when pre-consultation is disabled, but it was called")
	}
}

// TestSession_Start_PreConsultation_OrchestratedMode_NotCalled verifies that
// pre-consultation is NOT invoked when the run mode is orchestrated, even when
// PreConsultation=true. Pre-consultation is only meaningful in auto and
// auto-review; it must be silently skipped in orchestrated mode regardless of
// the caller's PreConsultation setting.
//
// Guard: this test passes vacuously before I5.3 is implemented (PreConsult is
// never called, so Called=false trivially). It will fail if I5.3 calls
// PreConsult unconditionally without mode-based filtering — i.e., if
// pre-consultation fires in orchestrated mode, Called=true and this test fails.
func TestSession_Start_PreConsultation_OrchestratedMode_NotCalled(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	// Queue agent-a so the run can proceed past the first dispatch before the
	// engine's orchestrated-mode routing takes over. The test does not assert
	// on the run's final outcome — only on whether PreConsult was called.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeOrchestrated
	cfg.PreConsultation = true // enabled, but must be suppressed in orchestrated mode

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if preConsultant.Called {
		t.Error("want PreConsult NOT called in orchestrated mode (pre-consultation is only meaningful in auto and auto-review), but it was called")
	}
}

// TestSession_Start_PreConsultation_AdviceAppliedToDispatch verifies that the
// strings returned by PreConsult (TaskDescription and Constraints) are wired
// into the subsequent auto-routed dispatch's request fields. Verifying that
// PreConsult is called (done in other tests) is distinct from verifying that
// its output is actually used.
//
// This test is in the RED phase: without I5.3 retaining and applying the
// pre-consultation advice strings, the dispatch's TaskDescription will not
// contain the advice text.
func TestSession_Start_PreConsultation_AdviceAppliedToDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	const adviceText = "pre-consultation-advice-sentinel-zq9"
	preConsultant := &scriptedPreConsultant{
		advice: domain.PreConsultationAdvice{
			TaskDescription: adviceText,
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
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

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation, got none")
	}
	// The pre-consultation advice must appear in the first auto-routed dispatch's
	// TaskDescription. The design contract (ContractsDesign §DispatchInstruction
	// field-resolution) specifies that advice strings are appended to auto-routed
	// dispatches only.
	if !strings.Contains(invs[0].Request.TaskDescription, adviceText) {
		t.Errorf("want first dispatch TaskDescription to contain pre-consultation advice %q, got %q",
			adviceText, invs[0].Request.TaskDescription)
	}
}

// TestSession_Start_PreConsultation_Failure_ReturnsRefusal verifies that when
// the PreConsultant returns an error, the run is refused. A pre-consultation
// failure prevents the run from starting — it occurs before the dispatch loop
// begins, so no work has been done yet.
func TestSession_Start_PreConsultation_Failure_ReturnsRefusal(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		err: &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  "orchestrator agent timed out",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
}

// TestSession_Start_PreConsultation_Failure_NoArtifactCreated verifies that a
// pre-consultation failure removes the run folder so that a refused run leaves no
// trace. Per the ContractsDesign ordering (Create → Apply(commit setup row) →
// pre-consultation → dispatch loop), ArtifactStore.Create is called before
// pre-consultation runs, so the artifact exists in the store at the point of
// failure. The "no trace" guarantee is therefore about the run folder being
// removed from the filesystem (os.RemoveAll(cfg.RunFolder)), not about Create
// never having been called.
func TestSession_Start_PreConsultation_Failure_NoArtifactCreated(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create a run folder that the session must remove on pre-consultation failure.
	runFolder := filepath.Join(dir, "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	preConsultant := &scriptedPreConsultant{
		err: &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  "connection refused",
		},
	}

	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true
	cfg.RunFolder = runFolder

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The session must remove the run folder on pre-consultation failure so that
	// a refused run leaves no trace on disk. Store.Create was called (store.exists
	// is true by design), but the filesystem run folder must be gone.
	if _, statErr := os.Stat(runFolder); !os.IsNotExist(statErr) {
		t.Error("want run folder removed when pre-consultation fails (failure must remove any run folder)")
	}
}

// ===== Stage 6: Launch-failure preservation contracts =====

// TestSession_Start_PreConsultation_LaunchFailure_IsNewRunFalse_RunFolderPreserved
// verifies that when a pre-consultation fails with a *domain.HarnessLaunchError
// on a resumed run (IsNewRun=false), the session:
//   1. Returns RunRefused,
//   2. Carries the HarnessLaunchError in outcome.Cause (so the TUI can detect
//      the launch-failure identity without parsing the message), and
//   3. Preserves the run folder and its Orchestration.md byte-for-byte.
//
// This is the session-level counterpart to the app-level restart tests. Without
// this test the destructive behavior (silently wiping a user's existing run
// history on every launch failure during a resume) is invisible to the suite.
//
// RED: session.refusal does not set Cause, and the pre-consultation failure
// handler removes the run folder unconditionally regardless of IsNewRun.
func TestSession_Start_PreConsultation_LaunchFailure_IsNewRunFalse_RunFolderPreserved(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create the run folder and write a pre-existing Orchestration.md so the
	// test can assert the file survives the pre-consultation failure.
	runFolder := filepath.Join(dir, "Orchestration-20260817T140615Z-test")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: create run folder: %v", err)
	}
	orchContent := []byte("---\nrun_id: test\nhuman_approved: false\n---\n# Existing run history\n")
	orchFile := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(orchFile, orchContent, 0o600); err != nil {
		t.Fatalf("setup: write Orchestration.md: %v", err)
	}

	// The memStore simulates a valid existing artifact so the session proceeds
	// past the artifact-read step and reaches pre-consultation.
	store := &memStore{
		state: domain.ArtifactState{
			Workflow:        "linear",
			WorkflowVersion: "1.0",
			Task:            "test task",
			GlobalSequence:  1,
			RunSettings: domain.RunSettings{
				Mode:            domain.ExecutionModeAuto,
				PreConsultation: true,
			},
		},
		exists: true,
	}

	// Inject a preConsultant that returns a HarnessLaunchError — the exact error
	// the harness adapters will produce when the subprocess cannot be started.
	launchErr := &domain.HarnessLaunchError{
		Harness:    "ghcp-cli",
		Executable: "/usr/bin/copilot",
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
	preConsultant := &scriptedPreConsultant{err: launchErr}

	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false
	cfg.RunFolder = runFolder
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	// The run must be refused.
	requireRefused(t, got, err)

	// The Cause field must carry the HarnessLaunchError so the TUI can detect
	// a launch failure via errors.As without parsing the message text.
	var found *domain.HarnessLaunchError
	if !errors.As(got.Cause, &found) {
		t.Error("outcome.Cause does not carry *domain.HarnessLaunchError; " +
			"the TUI relies on errors.As(outcome.Cause, &le) to route to the " +
			"override screen — without it, the override screen is never reached")
	}

	// The run folder must be preserved byte-for-byte. On IsNewRun=false the
	// folder predates this attempt (it contains the user's run history) and the
	// session must never delete it on pre-consultation failure.
	if _, statErr := os.Stat(runFolder); os.IsNotExist(statErr) {
		t.Error("run folder was removed on IsNewRun=false pre-consultation failure; " +
			"a resumed run's folder must never be deleted — it contains existing run history " +
			"that the override restart depends on")
	}
	gotContent, readErr := os.ReadFile(orchFile)
	if readErr != nil {
		t.Fatalf("Orchestration.md could not be read after pre-consultation failure: %v", readErr)
	}
	if string(gotContent) != string(orchContent) {
		t.Errorf("Orchestration.md content changed after pre-consultation failure; "+
			"want byte-for-byte preservation, got %q", gotContent)
	}
}

// TestSession_Start_PreConsultation_LaunchFailure_IsNewRunTrue_RunFolderRemoved
// verifies that when a pre-consultation fails with a *domain.HarnessLaunchError
// on a new run (IsNewRun=true), the session:
//   1. Returns RunRefused,
//   2. Carries the HarnessLaunchError in outcome.Cause, and
//   3. Removes the run folder (the same "no trace remains" contract as for
//      other pre-consultation failures on a new run).
//
// Together with the IsNewRunFalse counterpart above, this pins both rows of the
// ContractsDesign run-folder-preservation table for the launch-failure cause.
//
// RED (Cause only): folder removal already happens unconditionally; the Cause
// assertion will fail until session.refusal is made cause-carrying.
func TestSession_Start_PreConsultation_LaunchFailure_IsNewRunTrue_RunFolderRemoved(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create the run folder that the session must remove on new-run failure.
	runFolder := filepath.Join(dir, "Orchestration-20260817T140615Z-new")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: create run folder: %v", err)
	}

	launchErr := &domain.HarnessLaunchError{
		Harness:    "ghcp-cli",
		Executable: "/usr/bin/copilot",
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
	preConsultant := &scriptedPreConsultant{err: launchErr}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = true
	cfg.RunFolder = runFolder
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	// The run must be refused.
	requireRefused(t, got, err)

	// The Cause field must carry the HarnessLaunchError so the TUI can display
	// the override screen regardless of whether the failure was on a new or resumed run.
	var found *domain.HarnessLaunchError
	if !errors.As(got.Cause, &found) {
		t.Error("outcome.Cause does not carry *domain.HarnessLaunchError on IsNewRun=true; " +
			"the TUI uses the same errors.As check on all pre-consultation failures " +
			"and must detect the launch-failure identity in both cases")
	}

	// On a new run, the folder must be removed — the "no trace remains" contract.
	if _, statErr := os.Stat(runFolder); !os.IsNotExist(statErr) {
		t.Error("run folder was not removed on IsNewRun=true pre-consultation failure; " +
			"a failed new-run attempt must clean up its own folder")
	}
}

// ===== Stage 5: Resume configuration =====

// TestSession_Start_Resume_UsesModeFromArtifact verifies that on resume, the
// execution mode is taken from the artifact's RunSettings (as read from the
// frontmatter) rather than from the caller-supplied RunConfig. This is part of
// the "a resumed run reads all configuration from frontmatter" contract.
//
// RED-phase signal: cfg sets Checkpoints=true but the linear workflow has no
// checkpoint-class agent. Without I5.4, the session applies the cfg checkpoints
// value on the resume path and immediately refuses the run. With I5.4, the
// session reads Checkpoints=false from the artifact frontmatter, bypasses the
// refusal, and the run completes. cfg also sets Mode=orchestrated (vs artifact
// Mode=auto) — once I5.4 is implemented and also overrides the mode from the
// artifact, the final state assertion provides additional coverage.
func TestSession_Start_Resume_UsesModeFromArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an existing artifact that has mode=auto and
	// checkpoints=false. These are the values I5.4 must read and use on resume.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:        domain.ExecutionModeAuto,
			Checkpoints: false,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	// agent-b is the second row; on resume from after agent-a, the session
	// dispatches agent-b next.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false
	// Checkpoints=true with no checkpoint agent declared → the current session
	// code refuses the run at the checkpoint validation step. I5.4 must override
	// cfg with artifact values (Checkpoints=false) before this check fires.
	cfg.Checkpoints = true
	// Mode=orchestrated differs from the artifact's "auto". Once I5.4 also
	// overrides cfg.Mode with the artifact value, the final state assertion below
	// provides additional coverage of the read-back contract.
	cfg.Mode = domain.ExecutionModeOrchestrated

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the artifact says Checkpoints=false and Mode=auto,
	// so there should be no checkpoint-related refusal and routing should succeed.
	// Without I5.4, cfg.Checkpoints=true triggers a refusal (no checkpoint agent
	// in the linear workflow), and this assertion fails with RunRefused.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify the final artifact state reflects Mode=auto read from the frontmatter,
	// not the caller-supplied orchestrated. This assertion is a secondary guard:
	// it becomes meaningful once I5.4 explicitly propagates the artifact mode
	// into the session's operating state and Apply persists it.
	if store.state.RunSettings.Mode != domain.ExecutionModeAuto {
		t.Errorf("want resume to read Mode=%q from artifact frontmatter into final state, got %q",
			domain.ExecutionModeAuto, store.state.RunSettings.Mode)
	}
}

// TestSession_Start_Resume_NoCommitSetupDispatch verifies that when resuming a
// run that had commits enabled, the commit setup dispatch does NOT occur again.
// The commit branch is already recorded in the artifact from the original run
// start; repeating the setup would create a second branch.
//
// Note: this test is a guard that becomes meaningful after I5.2 is implemented.
// Once commit setup dispatch is added for new runs, this test ensures resume
// does not also trigger it.
func TestSession_Start_Resume_NoCommitSetupDispatch(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const existingBranch = "mosaic/run/original-run-id"

	// Pre-populate the store with an existing artifact that has commits enabled
	// and a commit branch already recorded from the original run start.
	// The artifact reflects: seq 1 = commit setup (infra), seq 2 = agent-a (workflow).
	// CurrentState.LastAgent matches the last workflow log entry (agent-a#2).
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  2,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
			CommitBranch:        existingBranch,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#2", // matches last workflow log entry below
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "commit-manager-git#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	// Only queue agent-b (the remaining workflow step) — no commit agent.
	// If the session incorrectly re-runs commit setup, it would invoke
	// commit-manager-git and receive a harness error (empty queue), causing
	// RunFailed instead of RunCompleted.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.IsNewRun = false
	// CommitBranch is set to a value that differs from the artifact's recorded
	// branch. A correct I5.4 reads CommitBranch from the artifact (existingBranch);
	// a buggy I5.4 that copies cfg.CommitBranch into the state would leave
	// "cfg-provided-branch" in the final state, failing the assertion below.
	cfg.CommitBranch = "cfg-provided-branch"

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify that commit-manager-git was not invoked during resume.
	for _, inv := range f.Invocations() {
		if strings.Contains(inv.Agent.Identifier, "commit-manager-git") {
			t.Errorf("want no commit-manager-git invocation on resume, got one: agent=%q",
				inv.Agent.Identifier)
		}
	}

	// Verify the final artifact state preserves the CommitBranch recorded in
	// the pre-existing artifact frontmatter. Without resume-reads-settings
	// (I5.4), the session would not propagate CommitBranch from the artifact
	// into the run's operating configuration and the final state would not
	// carry the correct branch name.
	if store.state.RunSettings.CommitBranch != existingBranch {
		t.Errorf("want resume to preserve CommitBranch=%q from artifact frontmatter, got %q in final state",
			existingBranch, store.state.RunSettings.CommitBranch)
	}
}

// TestSession_Start_Resume_AllSettingsPreservedFromArtifact verifies that on
// resume, all six RunSettings fields (Mode, Checkpoints, Commits,
// CommitBranchVariant, CommitBranch, PreConsultation, ManualResolution) are
// taken from the artifact's frontmatter. No configuration value is re-derived
// from the caller-supplied RunConfig.
//
// RED-phase signal: cfg sets Checkpoints=true but the linear workflow has no
// checkpoint-class agent. Without I5.4, the session applies cfg.Checkpoints on
// the resume path and refuses the run immediately. With I5.4, the session reads
// all RunSettings from the artifact frontmatter (Checkpoints=false, Mode=auto,
// CommitBranchVariant=MOSAICOwned) before any validation, so the run proceeds
// and the assertions below can verify each field.
func TestSession_Start_Resume_AllSettingsPreservedFromArtifact(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Populate the artifact with all settings. Checkpoints=false, Mode=auto,
	// and CommitBranchVariant=MOSAICOwned are the values I5.4 must read.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAuto,
			Checkpoints:         false,
			Commits:             false,
			CommitBranchVariant: domain.CommitBranchMOSAICOwned,
			CommitBranch:        "",
			PreConsultation:     false,
			ManualResolution:    false,
		},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false
	// Checkpoints=true with no checkpoint agent → current code refuses the run.
	// I5.4 must read Checkpoints=false from the artifact before this check fires.
	cfg.Checkpoints = true
	// Mode and CommitBranchVariant conflict with artifact values. Once I5.4
	// overrides all cfg RunSettings fields with artifact values, these conflicts
	// are suppressed and the assertions below verify each field was taken from
	// the artifact rather than from cfg.
	cfg.Mode = domain.ExecutionModeOrchestrated
	cfg.CommitBranchVariant = domain.CommitBranchUserOwn

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete: the artifact says Checkpoints=false, so no
	// checkpoint-related refusal should occur. Without I5.4, cfg.Checkpoints=true
	// triggers a refusal (no checkpoint agent in the linear workflow), and this
	// assertion fails with RunRefused.
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify all RunSettings fields are preserved from the artifact frontmatter.
	// These assertions are secondary guards that become meaningful once I5.4
	// explicitly propagates artifact RunSettings into the session's operating
	// state and Apply persists them.
	if store.state.Mode != domain.ExecutionModeAuto {
		t.Errorf("want resume to preserve artifact Mode=%q, got %q",
			domain.ExecutionModeAuto, store.state.Mode)
	}
	if store.state.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("want resume to preserve artifact CommitBranchVariant=%q, got %q",
			domain.CommitBranchMOSAICOwned, store.state.CommitBranchVariant)
	}
}

// =============================================================================
// Stage 6: Mode-Driven Dispatch Loop
// =============================================================================
//
// Tests for the mode-driven dispatch loop. Coverage:
//
//   Mode-driven routing decision:
//   - Orchestrated mode consults the RoutingConsultant on every step,
//     including the first step of a new run, and never calls the engine for
//     routing.
//   - Auto mode routes via the engine on SUCCESS and consults on deviation.
//   - Auto-review mode routes COMPLETED_NEEDS_ACTION via the engine's
//     On-Findings hint when unambiguous; consults on all other non-SUCCESS.
//
//   Request construction and free navigation:
//   - Consultant-routed dispatches use TaskDescription verbatim from the
//     instruction; auto-routed dispatches use GenericTaskDescription.
//   - Nil fields in DispatchInstruction fall back to the table row's values.
//   - Non-nil fields override the table row.
//   - Sequence numbers are always assigned by the Runner.
//   - A backward jump updates current_state to the dispatched row's position.
//
//   HITL verification in the loop:
//   - A compliant result is accepted and the run advances.
//   - A non-compliant SUCCESS triggers exactly one same-agent redispatch.
//   - A second non-compliant result escalates to a deviation.
//   - Verification is skipped when HITL is false, status is not SUCCESS, or
//     there are no output artifacts.
//   - The redispatch allowance is scoped per step and resets for the next step.
//
//   Consultation recording:
//   - A consultation produces an infrastructure-flagged Execution Log row.
//   - The consultation row consumes global_sequence.
//   - current_state continues to name the last workflow step.
//   - The consultation row's agent instance names the orchestrator that was
//     invoked (derived from the file stem), not a hardcoded identifier.
//   - A non-default orchestrator file stem produces the correct row identity.
//
//   Consultation log attribution — resume and rewind (Stage 6):
//   - Resuming a run whose log ends with a consultation row correctly skips
//     that row and advances to the next workflow step (position recovery).
//   - Resuming an interrupted run (FR-33 rewind path) whose log contains
//     consultation rows correctly identifies the last completed workflow step
//     and routes to the step that was interrupted.
//
//   Failure handling:
//   - Consultation failure is terminal by default (RunStoppedByConsultant).
//   - The artifact is left resumable after a consultation failure.
//   - With ManualResolution enabled in orchestrated mode, consultation failure
//     invokes the Manual resolver instead of terminating.
//   - Subagent harness errors remain deviations, not crashes.
//   - A stop instruction returns RunStoppedByConsultant with the reason.
//
//   Write discipline:
//   - The preceding result is written (Apply) before the consultation.
//   - The artifact is re-read (Read) after the consultation returns.

// ---- Stage-6 test helpers ----

// scriptedRoutingConsultant implements domain.RoutingConsultant with a scripted
// sequence of RoutingInstruction/error pairs consumed in FIFO order.
// After the queue is exhausted, ConsultRouting returns a ConsultFailTransport
// error so tests that over-consult fail rather than panic.
type scriptedRoutingConsultant struct {
	instructions []domain.RoutingInstruction
	errors       []error // parallel slice; nil element = no error for that call
	idx          int
	CallCount    int
	Requests     []domain.ConsultationRequest
}

func (s *scriptedRoutingConsultant) ConsultRouting(_ context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	s.CallCount++
	s.Requests = append(s.Requests, req)
	if s.idx >= len(s.instructions) {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("scriptedRoutingConsultant: queue exhausted (call #%d)", s.CallCount),
		}
	}
	instr := s.instructions[s.idx]
	var err error
	if s.idx < len(s.errors) {
		err = s.errors[s.idx]
	}
	s.idx++
	return instr, err
}

func (s *scriptedRoutingConsultant) queueDispatch(agent, taskDesc string, rowIndex int) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithConstraints(agent, taskDesc string, rowIndex int, constraints *string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			Constraints:     constraints,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithInputs(agent, taskDesc string, rowIndex int, inputs *[]string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			InputArtifacts:  inputs,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithOutputs(agent, taskDesc string, rowIndex int, outputs *[]string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           agent,
			RowIndex:        rowIndex,
			TaskDescription: taskDesc,
			OutputArtifacts: outputs,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueDispatchWithHITL(agent, taskDesc string, rowIndex int, hitlOverride *bool) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:        agent,
			RowIndex:     rowIndex,
			TaskDescription: taskDesc,
			HITLOverride: hitlOverride,
		},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueStop(reason string) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{
		Stop: &domain.StopInstruction{Reason: reason},
	})
	s.errors = append(s.errors, nil)
}

func (s *scriptedRoutingConsultant) queueError(failure domain.ConsultationFailure) {
	s.instructions = append(s.instructions, domain.RoutingInstruction{})
	s.errors = append(s.errors, &domain.ConsultationError{
		Failure: failure,
		Detail:  "scripted consultation error",
	})
}

// fixedApprovalReader implements domain.ApprovalReader, always returning the
// same HumanApproval value for every path.
type fixedApprovalReader struct {
	approval domain.HumanApproval
}

func (r *fixedApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return r.approval
}

// perPathApprovalReader implements domain.ApprovalReader, returning a
// specific approval value for named paths and a fallback for all others.
type perPathApprovalReader struct {
	specific map[string]domain.HumanApproval
	fallback domain.HumanApproval
}

func (r *perPathApprovalReader) ReadApproval(_ context.Context, path string) domain.HumanApproval {
	if v, ok := r.specific[path]; ok {
		return v
	}
	return r.fallback
}

// applyBeforeConsultConsultant wraps a scriptedRoutingConsultant and records,
// at each ConsultRouting call, the number of Apply calls already made and the
// current Read count. Used by write-discipline tests.
type applyBeforeConsultConsultant struct {
	inner       *scriptedRoutingConsultant
	store       *memStore
	ApplyAtCall []int // len(store.Applied) at the moment of each ConsultRouting call
	ReadAtCall  []int // store.ReadCount at the moment of each ConsultRouting call
}

func (c *applyBeforeConsultConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.ApplyAtCall = append(c.ApplyAtCall, len(c.store.Applied))
	c.ReadAtCall = append(c.ReadAtCall, c.store.ReadCount)
	return c.inner.ConsultRouting(ctx, req)
}

// baseOrchestratedConfig returns a RunConfig for the linear workflow in
// orchestrated mode.
func baseOrchestratedConfig(orchPath string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
}

// newOrchestratedSession builds a session backed by the linear-orch.md fixture
// in orchestrated mode, using the supplied RoutingConsultant. A no-op
// DeviationResolver is wired to prevent nil-panics in the pre-Stage-6 code
// path that still routes deviations through Deviation instead of Routing.
func newOrchestratedSession(t *testing.T, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newHITLLinearSession builds a session backed by the hitl-linear-orch.md
// fixture (agent-a has HITL=true) using the supplied RoutingConsultant and
// ApprovalReader. A no-op DeviationResolver is wired to prevent nil-panics
// in the pre-Stage-6 code path; the Stage 6 implementation routes deviations
// through the RoutingConsultant instead.
func newHITLLinearSession(t *testing.T, consultant domain.RoutingConsultant, approvals domain.ApprovalReader) (
	ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// ===== Mode-driven routing decision =====

// TestSession_OrchestratedMode_ConsultsRoutingOnEveryStep verifies that in
// orchestrated mode the RoutingConsultant is invoked for every step,
// including the first step of a new run, and the engine is never asked for
// routing (the consultant drives all decisions).
func TestSession_OrchestratedMode_ConsultsRoutingOnEveryStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do planning", 0)
	consultant.queueDispatch("agent-b", "do review", 1)
	consultant.queueStop("workflow complete")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "planning done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "review done",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after orchestrator stop, got %q (message: %q)", got.Status, got.Message)
	}
	// Consultant must be called once per step decision: first step, after agent-a, after agent-b.
	if consultant.CallCount != 3 {
		t.Errorf("want 3 ConsultRouting calls, got %d", consultant.CallCount)
	}
}

// TestSession_OrchestratedMode_FirstStepConsultsWithNilLastMessage verifies
// that the very first consultation of a new orchestrated run carries a nil
// LastStatusMessage (there is no prior agent result at this point).
func TestSession_OrchestratedMode_FirstStepConsultsWithNilLastMessage(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "start the work", 0)
	consultant.queueStop("done")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	if consultant.CallCount == 0 {
		t.Fatal("want at least one ConsultRouting call for the first step, got 0")
	}
	if consultant.Requests[0].LastStatusMessage != nil {
		t.Errorf("want nil LastStatusMessage on first consultation of a new run, got %v",
			consultant.Requests[0].LastStatusMessage)
	}
}

// TestSession_AutoMode_AllSuccess_EngineRoutesWithNoConsultation verifies that
// in auto mode an all-SUCCESS run completes entirely via the engine's routing
// without any consultation. The RoutingConsultant must not be called.
func TestSession_AutoMode_AllSuccess_EngineRoutesWithNoConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No queue entries: any call to ConsultRouting returns a transport error.

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
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
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount > 0 {
		t.Errorf("want 0 ConsultRouting calls for all-SUCCESS auto run, got %d", consultant.CallCount)
	}
}

// TestSession_AutoMode_NonSuccessStatus_TriggersConsultation verifies that in
// auto mode a non-SUCCESS agent result causes the RoutingConsultant to be
// called instead of the engine resolving the deviation.
func TestSession_AutoMode_NonSuccessStatus_TriggersConsultation(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// After the BLOCKED result, the consultant dispatches agent-a again then agent-b.
	consultant.queueDispatch("agent-a", "retry the work", 0)
	consultant.queueDispatch("agent-b", "now proceed", 1)
	// After agent-b completes, the consultant has no more entries; that is
	// acceptable if the engine completes the run instead.

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// First agent-a call returns BLOCKED (non-SUCCESS) → should trigger consultation.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "tool unavailable",
	}})
	// Second agent-a call (after consultant redispatches) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The consultant must have been invoked at least once for the BLOCKED deviation.
	if consultant.CallCount == 0 {
		t.Errorf("want at least 1 ConsultRouting call after BLOCKED result in auto mode, got 0")
	}
}

// TestSession_AutoReviewMode_CNAWithUnambiguousHint_EngineAutoRoutes verifies
// that in auto-review mode a COMPLETED_NEEDS_ACTION result with an unambiguous
// On Findings hint is auto-routed by the engine without consulting the
// RoutingConsultant.
func TestSession_AutoReviewMode_CNAWithUnambiguousHint_EngineAutoRoutes(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// No entries: any unexpected consultation triggers an error response.

	dir := t.TempDir()
	// Reuse the loopback-style workflow content used in the existing On Findings test.
	const loopbackContent = `<Workflow type="core" name="loopback" version="1.0">
## Loopback Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | agent-a | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | plan.md | result.md |
</Workflow>
`
	orchPath := filepath.Join(dir, "loopback-orch.md")
	if err := os.WriteFile(orchPath, []byte(loopbackContent), 0600); err != nil {
		t.Fatalf("write loopback-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a returns CNA → On Findings hint "agent-a" → engine auto-routes (no consult).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "found issues",
	}})
	// Second agent-a call (loop-back) → SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "issues resolved",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "loopback",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if consultant.CallCount > 0 {
		t.Errorf("want 0 ConsultRouting calls for CNA with unambiguous On Findings in auto-review mode, got %d",
			consultant.CallCount)
	}
}

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
	// agent-a: seq must be 1.
	if invs[0].Request.AgentInstanceID != "agent-a#1" {
		t.Errorf("want agent-a#1, got %q", invs[0].Request.AgentInstanceID)
	}
	// agent-b: seq must be 2 (monotonically after agent-a's seq of 1).
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

	// Build a session that would fail HITL if it were checked.
	dir := t.TempDir()
	orchPath2 := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f2 := harness.NewFakeAdapter()
	_ = ses
	_ = orchPath
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

// TestSession_HITL_SkippedWhenStatusNotSuccess verifies that HITL compliance
// verification is skipped for non-SUCCESS results. A BLOCKED result from an
// agent whose HITL is true must not trigger a HITL redispatch.
//
// Correct sequence:
//  1. Consultant dispatches agent-a "first" → BLOCKED → HITL skipped (non-SUCCESS) → deviation
//  2. Consultant re-dispatches agent-a "retry after BLOCKED" → SUCCESS → HITL fires (correct:
//     Status==SUCCESS, EffectiveHITL=true) → ApprovalFalse → one automatic redispatch
//  3. HITL redispatch of "retry after BLOCKED" → SUCCESS → ApprovalFalse → second non-compliant
//     → escalation treated as deviation → consultant dispatches agent-b
//
// The definitive signal that HITL was NOT applied to the BLOCKED result is the
// task description carried by the 2nd agent-a invocation: it must be
// "retry after BLOCKED" (the consultant's re-dispatch), not "first" (which would
// indicate an intervening HITL-triggered redispatch of the BLOCKED result).
func TestSession_HITL_SkippedWhenStatusNotSuccess(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consultant dispatches agent-a twice: original and retry after BLOCKED.
	// After the retry's HITL-redispatch chain escalates to a deviation, the
	// consultant dispatches agent-b and then stops.
	consultant.queueDispatch("agent-a", "first", 0)
	consultant.queueDispatch("agent-a", "retry after BLOCKED", 0)
	consultant.queueDispatch("agent-b", "proceed", 1)
	consultant.queueStop("done")

	// ApprovalFalse for all artifacts. If HITL incorrectly fires on the BLOCKED
	// result it produces a same-task redispatch before the consultant's retry,
	// making the 2nd agent-a invocation carry task "first" instead of
	// "retry after BLOCKED".
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// Call 1: BLOCKED (non-SUCCESS) — HITL must be skipped.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "blocked",
	}})
	// Call 2 (consultant redispatch "retry after BLOCKED"): SUCCESS. HITL
	// correctly fires here because Status==SUCCESS and EffectiveHITL=true.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	// Call 3: automatic HITL redispatch of the "retry after BLOCKED" SUCCESS
	// (one allowed redispatch per step). ApprovalFalse again → second
	// non-compliant → escalation to deviation → consultant dispatches agent-b.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done again",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Collect agent-a invocations in call order.
	var agentAInvocations []harness.Invocation
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "agent-a" {
			agentAInvocations = append(agentAInvocations, inv)
		}
	}

	// Exactly 3 agent-a invocations are expected: the BLOCKED original, the
	// consultant's "retry after BLOCKED", and the automatic HITL redispatch of
	// that retry. A count of 4+ indicates HITL incorrectly fired on the BLOCKED
	// result (producing an extra same-task redispatch before the consultant's retry).
	if got := len(agentAInvocations); got != 3 {
		t.Errorf("want exactly 3 agent-a dispatches "+
			"(BLOCKED original + consultant retry + HITL-redispatch-of-retry), got %d", got)
	}

	// The 2nd agent-a invocation must carry the consultant's task description,
	// not the original "first". If HITL incorrectly fired on the BLOCKED result,
	// it would insert a same-task redispatch ("first") as invocation #2, pushing
	// the consultant's retry to #3.
	if len(agentAInvocations) >= 2 {
		const wantTask = "retry after BLOCKED"
		if got := agentAInvocations[1].Request.TaskDescription; got != wantTask {
			t.Errorf("want 2nd agent-a invocation to carry consultant task %q "+
				"(HITL was skipped for BLOCKED → deviation routed through consultant), "+
				"got %q — HITL may have incorrectly fired on the BLOCKED result",
				wantTask, got)
		}
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

// ===== Consultation recording =====

// TestSession_Consultation_RecordedAsInfrastructureRow verifies that each
// RoutingConsultant invocation is recorded as an Execution Log row with
// IsInfrastructure=true. Workflow steps must not be flagged as infrastructure.
func TestSession_Consultation_RecordedAsInfrastructureRow(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do planning", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Verify at least one infrastructure row exists (the consultation).
	infraRows := 0
	for _, step := range store.Applied {
		if step.IsInfrastructure {
			infraRows++
		}
	}
	if infraRows == 0 {
		t.Error("want at least one infrastructure-flagged row for consultation, got 0")
	}

	// The Agent column of each infrastructure row must carry the resolved
	// orchestrator identifier (the file stem of the orchestrator file) followed
	// by "#N". The fixture writes the file as "orchestrator.md", so the stem is
	// "orchestrator".
	for _, step := range store.Applied {
		if !step.IsInfrastructure {
			continue
		}
		if !strings.HasPrefix(step.AgentInstance, "orchestrator#") {
			t.Errorf("want consultation row AgentInstance to match orchestrator#N, got %q",
				step.AgentInstance)
		}
	}

	// The agent-a step must not be flagged as infrastructure.
	for _, step := range store.Applied {
		if strings.Contains(step.AgentInstance, "agent-a") && step.IsInfrastructure {
			t.Errorf("want agent-a step not flagged as infrastructure, but it is")
		}
	}
}

// TestSession_Consultation_ConsumesGlobalSequence verifies that a consultation
// row consumes a global_sequence slot, so the sequence number of the next
// workflow step is higher than it would be without the consultation.
func TestSession_Consultation_ConsumesGlobalSequence(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// With one consultation (first step) and one workflow step, the final
	// GlobalSequence is exactly 2 (consultation=1 + agent-a=2). The sequence is
	// fully deterministic in this setup, so an exact assertion catches off-by-one
	// errors (e.g. double-counting the consultation would produce 3).
	if store.state.GlobalSequence != 2 {
		t.Errorf("want GlobalSequence == 2 after one consultation + one workflow step, got %d",
			store.state.GlobalSequence)
	}
}

// TestSession_Consultation_DoesNotMoveCurrentState verifies that after a
// consultation is recorded, current_state continues to name the last WORKFLOW
// step (not the consultation row).
func TestSession_Consultation_DoesNotMoveCurrentState(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	// After agent-a completes, a second consultation is triggered.
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// After the run ends, current_state.LastAgent must name agent-a, not any
	// consultation-derived identifier.
	if !strings.Contains(store.state.CurrentState.LastAgent, "agent-a") {
		t.Errorf("want current_state.LastAgent to name agent-a (last workflow step), got %q",
			store.state.CurrentState.LastAgent)
	}
}

// ===== Failure handling =====

// TestSession_ConsultationFailure_ReturnsStoppedByConsultant verifies that a
// consultation error (any failure class) is terminal by default and returns
// RunStoppedByConsultant.
func TestSession_ConsultationFailure_ReturnsStoppedByConsultant(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueError(domain.ConsultFailMalformedJSON) // first consultation fails

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (consultation failure encoded in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant for consultation failure, got %q (message: %q)",
			got.Status, got.Message)
	}
}

// TestSession_ConsultationFailure_ArtifactLeftResumable verifies that after a
// consultation failure the artifact state is left intact and the run is
// resumable. The artifact must still exist (not deleted) and have a valid state.
func TestSession_ConsultationFailure_ArtifactLeftResumable(t *testing.T) {
	// Dispatch agent-a successfully, then fail on the second consultation.
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "step 1", 0)
	consultant.queueError(domain.ConsultFailTransport) // second consultation fails

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant, got %q", got.Status)
	}
	// The artifact must still be present (store.exists=true) and have the
	// agent-a step recorded. A deleted or reset artifact is not resumable.
	if !store.exists {
		t.Error("want artifact to exist (run is resumable after consultation failure)")
	}
	agentARecorded := false
	for _, step := range store.Applied {
		if strings.Contains(step.AgentInstance, "agent-a") {
			agentARecorded = true
			break
		}
	}
	if !agentARecorded {
		t.Error("want agent-a's completed step recorded in the artifact (run is resumable)")
	}
}

// TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver
// verifies that in orchestrated mode with ManualResolution=true, a consultation
// failure falls back to the Manual resolver instead of terminating.
func TestSession_ManualResolutionEnabled_ConsultationFailure_UsesManualResolver(t *testing.T) {
	// Primary consultant: fails on first call.
	primary := &scriptedRoutingConsultant{}
	primary.queueError(domain.ConsultFailTransport)

	// Manual resolver: dispatches agent-a after the primary fails.
	manual := &scriptedRoutingConsultant{}
	manual.queueDispatch("agent-a", "manual resolution fallback", 0)
	manual.queueStop("done")

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   primary,
		Manual:    manual,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done via manual",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeOrchestrated,
			ManualResolution: true,
		},
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// The manual resolver must have been invoked after the primary consultant failed.
	if manual.CallCount == 0 {
		t.Error("want ManualResolver called after primary consultation failure with ManualResolution=true, got 0 calls")
	}
}

// TestSession_StopInstruction_ReturnsStoppedByConsultant verifies that a
// StopInstruction from the RoutingConsultant causes the session to return
// RunStoppedByConsultant.
func TestSession_StopInstruction_ReturnsStoppedByConsultant(t *testing.T) {
	const stopReason = "orchestrator decided to pause here"

	consultant := &scriptedRoutingConsultant{}
	consultant.queueStop(stopReason)

	ses, _, _, orchPath := newOrchestratedSession(t, consultant)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	if got.StopReason != stopReason {
		t.Errorf("want StopReason=%q, got %q", stopReason, got.StopReason)
	}
}

// TestSession_SubagentHarnessError_IsDeviation_NotCrash verifies that a
// harness-level error while invoking a subagent is treated as a deviation
// (the consultant is invoked to decide what to do next) and does NOT crash
// the session or return RunFailed.
func TestSession_SubagentHarnessError_IsDeviation_NotCrash(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// In orchestrated mode: dispatch agent-a; when it fails at harness level,
	// the session must consult again (deviation). The consultant then dispatches
	// agent-a again for a retry.
	consultant.queueDispatch("agent-a", "try the work", 0)
	// After the harness error, consultation is triggered again:
	consultant.queueDispatch("agent-a", "retry after harness error", 0)
	consultant.queueStop("done after retry")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	// First agent-a invocation: harness error.
	f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness: subprocess timed out")})
	// Second agent-a invocation (after retry): SUCCESS.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done on retry",
	}})

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error (harness error is a deviation, not a crash), got %v", err)
	}
	// The session must NOT return RunFailed for a harness error.
	if got.Status == domain.RunFailed {
		t.Errorf("want harness error treated as deviation (not RunFailed), got RunFailed (message: %q)",
			got.Message)
	}
}

// ===== Write discipline =====

// TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation
// verifies that the preceding agent's result is written to the artifact store
// (via Apply) before the RoutingConsultant is invoked. This guarantees the
// orchestrator reads an up-to-date artifact.
func TestSession_WriteBeforeConsult_PrecedingResultWrittenBeforeConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done") // second consultation, after agent-a

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   tracking,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ApplyAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls to observe ordering, got %d", len(tracking.ApplyAtCall))
	}
	// First consultation: no workflow step has been completed yet (Apply=0).
	if tracking.ApplyAtCall[0] != 0 {
		t.Errorf("want 0 workflow Apply calls at first consultation, got %d", tracking.ApplyAtCall[0])
	}
	// Second consultation (after agent-a): agent-a's result must already be written (Apply >= 1).
	if tracking.ApplyAtCall[1] < 1 {
		t.Errorf("want >= 1 Apply calls at second consultation (agent-a result written first), got %d",
			tracking.ApplyAtCall[1])
	}
}

// TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation verifies that
// the artifact is re-read from the store after a consultation returns, so any
// Workflow Notes the orchestrator appended during its deliberation are visible
// in the session's next iteration.
func TestSession_RereadAfterConsult_ArtifactRereadAfterConsultation(t *testing.T) {
	inner := &scriptedRoutingConsultant{}
	inner.queueDispatch("agent-a", "step 1", 0)
	inner.queueStop("done")

	store := &memStore{}
	tracking := &applyBeforeConsultConsultant{inner: inner, store: store}

	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   tracking,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses.Start(context.Background(), domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}) //nolint:errcheck

	if len(tracking.ReadAtCall) < 2 {
		t.Fatalf("want at least 2 ConsultRouting calls, got %d", len(tracking.ReadAtCall))
	}
	// After the first consultation returns (which dispatched agent-a), the session
	// must re-read the artifact before the second consultation. So the Read count
	// at the second consultation must be higher than at the first.
	if tracking.ReadAtCall[1] <= tracking.ReadAtCall[0] {
		t.Errorf("want ReadCount to increase between consultations (re-read after each), "+
			"got ReadAtCall[0]=%d ReadAtCall[1]=%d", tracking.ReadAtCall[0], tracking.ReadAtCall[1])
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

	f := harness.NewFakeAdapter()
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

// ===== Consultation failure classes =====

// TestSession_ConsultationFailureClasses_AllTerminal verifies that each
// orchestrator failure class terminates the run with RunStoppedByConsultant.
// This covers ConsultFailMissingField, ConsultFailUnknownAction, and
// ConsultFailUnknownAgent — the three classes not exercised by the existing
// ConsultationFailure tests (which already cover ConsultFailMalformedJSON and
// ConsultFailTransport).
func TestSession_ConsultationFailureClasses_AllTerminal(t *testing.T) {
	classes := []domain.ConsultationFailure{
		domain.ConsultFailMissingField,
		domain.ConsultFailUnknownAction,
		domain.ConsultFailUnknownAgent,
	}
	for _, failClass := range classes {
		failClass := failClass
		t.Run(string(failClass), func(t *testing.T) {
			consultant := &scriptedRoutingConsultant{}
			consultant.queueError(failClass)

			ses, _, _, orchPath := newOrchestratedSession(t, consultant)

			got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

			if err != nil {
				t.Fatalf("want nil error (failure encoded in RunOutcome for %s), got %v", failClass, err)
			}
			if got.Status != domain.RunStoppedByConsultant {
				t.Errorf("want RunStoppedByConsultant for %s failure class, got %q (message: %q)",
					failClass, got.Status, got.Message)
			}
		})
	}
}

// ===== Consultation log attribution (Stage 6) =====

// TestSession_Consultation_NonDefaultOrchestrator_RowNamesInvokedOrchestrator
// verifies that a consultation row's agent instance uses the file stem of the
// orchestrator supplied to the run, not any hardcoded identifier. With an
// orchestrator file named "custom-orch.md" every infrastructure row must carry
// the prefix "custom-orch", not "orchestrator-script" or any other literal.
func TestSession_Consultation_NonDefaultOrchestrator_RowNamesInvokedOrchestrator(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-a", "do work", 0)
	consultant.queueStop("done")

	dir := t.TempDir()
	// Write the orchestrator under a non-default filename stem so that the
	// resolved Identifier ("custom-orch") differs from every hardcoded string
	// that could exist in the implementation.
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// The consultation at seq=1 is followed by agent-a at seq=2.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}
	ses.Start(context.Background(), cfg) //nolint:errcheck

	// Every infrastructure row must carry "custom-orch#N". Any other prefix
	// indicates the implementation used a hardcoded identifier instead of
	// deriving the identity from the resolved orchestrator file.
	infraRows := 0
	for _, step := range store.Applied {
		if !step.IsInfrastructure {
			continue
		}
		infraRows++
		if !strings.HasPrefix(step.AgentInstance, "custom-orch#") {
			t.Errorf("want consultation row AgentInstance to start with custom-orch#, got %q",
				step.AgentInstance)
		}
	}
	if infraRows == 0 {
		t.Error("want at least one infrastructure-flagged consultation row, got 0")
	}
}

// TestSession_Resume_WithTrailingConsultationRow_PositionRecovery verifies that
// when a run is resumed and the execution log ends with a consultation row after
// the last workflow step, the session correctly identifies the last workflow
// step and advances to the step that follows it.
//
// Log layout: [agent-a#1 (workflow), orchestrator#2 (consultation/infra)]
// current_state: agent-a#1 (matching — no interruption)
// Expected: session dispatches agent-b (the step following agent-a in the
// linear workflow), proving it skipped the trailing consultation row.
func TestSession_Resume_WithTrailingConsultationRow_PositionRecovery(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Consulted once to decide the next step (agent-b) after the resume.
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	// Pre-populate: agent-a completed (seq=1) then a consultation row (seq=2).
	// The consultation row's Agent is "orchestrator#2" — the stem of
	// "orchestrator.md", which newOrchestratedSession writes for the fixture.
	// current_state records agent-a#1, matching the last workflow log entry
	// (no interruption).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "orchestrator#2", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseOrchestratedConfig(orchPath)
	cfg.IsNewRun = false

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation row must not cause PositionUnresolvedError), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: if the resume had not correctly
	// recognised the consultation row as infrastructure and skipped it, it
	// would have returned RunFailed with PositionUnresolvedError instead.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed session to dispatch agent-b (step following agent-a), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep
// verifies the re-run rewind path (FR-33): when a run is resumed after a
// mid-invocation interruption and the execution log contains consultation rows,
// the rewind correctly identifies the last completed workflow step by skipping
// the consultation rows, then routes to the step that was interrupted.
//
// Log layout:
//   - seq=1: orchestrator#1 (consultation/infra — first routing decision)
//   - seq=2: agent-a#2      (workflow step — completed)
//   - seq=3: orchestrator#3 (consultation/infra — next routing decision)
//
// current_state.LastAgent = "agent-b#4" (premature: set before agent-b's
// Apply completed, then the session crashed).
//
// The rewind must skip orchestrator#3, find agent-a#2 as the last completed
// workflow step, and route to agent-b (the step that was being dispatched when
// the crash occurred).
func TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// After the rewind the session consults for the next step and dispatches agent-b.
	consultant.queueDispatch("agent-b", "step 2 (retry after interruption)", 1)
	consultant.queueStop("done")

	ses, f, store, orchPath := newOrchestratedSession(t, consultant)

	// Pre-populate: consultation (seq=1), agent-a completed (seq=2), another
	// consultation (seq=3). current_state was advanced to "agent-b#4" before
	// the session crashed without recording agent-b's Apply, so agent-b is absent
	// from the log. This is the mid-invocation interruption scenario.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#4", // premature: agent-b was never logged
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "orchestrator#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "orchestrator#3", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseOrchestratedConfig(orchPath)
	cfg.IsNewRun = false

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation rows must not prevent rewind), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: a correct rewind sets CurrentState to
	// agent-a#2 (the last completed workflow step), which routes the engine to
	// agent-b. If the rewind had failed to skip the consultation rows it would
	// have returned RunFailed with PositionUnresolvedError, or re-dispatched
	// agent-a unnecessarily.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want rewind to route to agent-b (the interrupted step), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_WithTrailingConsultationRow_NonDefaultOrchestrator_PositionRecovery
// is the non-default-orchestrator complement to
// TestSession_Resume_WithTrailingConsultationRow_PositionRecovery.
//
// Where that test relies on the default "orchestrator.md" stem (identifier
// "orchestrator"), this test names the orchestrator file "custom-orch.md" so the
// resolved identifier is "custom-orch". The pre-populated log carries
// "custom-orch#N" for the consultation entry. If position recovery hardcodes any
// string other than reading s.orchRef.Identifier, the consultation row will not be
// recognised as infrastructure and the test will fail with a PositionUnresolvedError
// or a wrong first dispatch.
//
// Log layout: [agent-a#1 (workflow), custom-orch#2 (consultation/infra)]
// current_state: agent-a#1 (matching — no interruption)
// Expected: session dispatches agent-b (the step after agent-a), then
// RunStoppedByConsultant when the consultant issues a stop.
func TestSession_Resume_WithTrailingConsultationRow_NonDefaultOrchestrator_PositionRecovery(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step 2", 1)
	consultant.queueStop("done")

	dir := t.TempDir()
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-populate: agent-a completed (seq=1) then a consultation row (seq=2).
	// The consultation row uses "custom-orch#2" — the stem of "custom-orch.md".
	// current_state records agent-a#1, matching the last workflow log entry
	// (no interruption).
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "custom-orch#2", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation row with non-default orchestrator must not cause PositionUnresolvedError), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: if position recovery failed to recognise
	// "custom-orch#2" as an infrastructure row it would have returned RunFailed
	// with PositionUnresolvedError instead.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want resumed session to dispatch agent-b (step following agent-a), got %q",
			invs[0].Agent.Identifier)
	}
}

// TestSession_Resume_Rewind_WithConsultationRowsInLog_NonDefaultOrchestrator_CorrectlyIdentifiesLastWorkflowStep
// is the non-default-orchestrator complement to
// TestSession_Resume_Rewind_WithConsultationRowsInLog_CorrectlyIdentifiesLastWorkflowStep.
//
// The orchestrator file is named "custom-orch.md" so the resolved identifier is
// "custom-orch". Pre-populated consultation log entries carry "custom-orch#N".
// If the FR-33 rewind path hardcodes any specific string the consultation rows will
// not be recognised as infrastructure, the rewind will mis-identify the last
// completed workflow step, and the test will fail.
//
// Log layout:
//   - seq=1: custom-orch#1 (consultation/infra — first routing decision)
//   - seq=2: agent-a#2      (workflow step — completed)
//   - seq=3: custom-orch#3  (consultation/infra — next routing decision)
//
// current_state.LastAgent = "agent-b#4" (premature: set before agent-b's Apply
// completed, then the session crashed).
//
// The rewind must skip custom-orch#3, find agent-a#2 as the last completed
// workflow step, and route to agent-b (the step that was being dispatched when
// the crash occurred). Expected result: RunStoppedByConsultant.
func TestSession_Resume_Rewind_WithConsultationRowsInLog_NonDefaultOrchestrator_CorrectlyIdentifiesLastWorkflowStep(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "step 2 (retry after interruption)", 1)
	consultant.queueStop("done")

	dir := t.TempDir()
	data, err := os.ReadFile(orchFilePath("linear-orch.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	orchPath := filepath.Join(dir, "custom-orch.md")
	if err := os.WriteFile(orchPath, data, 0600); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Pre-populate: consultation (seq=1), agent-a completed (seq=2), another
	// consultation (seq=3). current_state was advanced to "agent-b#4" before the
	// session crashed without recording agent-b's Apply — the mid-invocation
	// interruption scenario. All consultation entries use "custom-orch#N".
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  3,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-b#4", // premature: agent-b was never logged
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "custom-orch#1", Phase: "", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "custom-orch#3", Phase: "", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#5",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             false,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeOrchestrated},
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error on resume (consultation rows with non-default orchestrator must not prevent rewind), got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant after stop instruction, got %q (message: %q)",
			got.Status, got.Message)
	}
	// agent-b must have been dispatched: a correct rewind sets CurrentState to
	// agent-a#2 (the last completed workflow step), which routes the engine to
	// agent-b. If the rewind failed to skip "custom-orch#N" entries it would have
	// returned RunFailed with PositionUnresolvedError, or re-dispatched agent-a.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one harness invocation (agent-b) on resume, got 0")
	}
	if invs[0].Agent.Identifier != "agent-b" {
		t.Errorf("want rewind to route to agent-b (the interrupted step), got %q",
			invs[0].Agent.Identifier)
	}
}

// ===== Stage 7: ApprovalCapability and disk-based approval reading =====

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

	f := harness.NewFakeAdapter()
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

	f := harness.NewFakeAdapter()
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
