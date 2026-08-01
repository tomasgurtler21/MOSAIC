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

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-common/interaction"
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

func (m *memStore) Create(_ context.Context, info domain.WorkflowInfo, task string, checkpoints bool, now time.Time, runID string) (domain.ArtifactState, error) {
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
		Checkpoints:     checkpoints,
	}
	m.exists = true
	return m.state, nil
}

func (m *memStore) Apply(_ context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	m.Applied = append(m.Applied, step)
	state.GlobalSequence = step.Seq
	state.CurrentState = domain.CurrentState{
		Phase:      step.Phase,
		Stage:      step.Stage,
		LastStatus: step.Status,
		LastAgent:  step.AgentInstance,
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

// ---- scripted DeviationResolver ----

type scriptedResolver struct {
	instruction domain.RejoinInstruction
	err         error
	Called      bool
}

func (r *scriptedResolver) Resolve(_ context.Context, _ domain.DeviationInfo) (domain.RejoinInstruction, error) {
	r.Called = true
	return r.instruction, r.err
}

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
	resolver := &scriptedResolver{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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
		OnDeviation:          domain.DeviationDelegate,
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
	const refusedContent = `[[SECTION:Workflow:refused]]
<!-- workflow-version: 1.0 -->
## Refused Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a(mode) | ❌ | - | out.md |
[[/SECTION:Workflow:refused]]
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
	const loopbackContent = `[[SECTION:Workflow:loopback]]
<!-- workflow-version: 1.0 -->
## Loopback Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | agent-a | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
[[/SECTION:Workflow:loopback]]
`
	orchPath := filepath.Join(dir, "loopback-orch.md")
	if err := os.WriteFile(orchPath, []byte(loopbackContent), 0600); err != nil {
		t.Fatalf("write loopback-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	resolver := &scriptedResolver{}
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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
		OnDeviation:          domain.DeviationDelegate,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Deviation resolver must NOT have been called (On Findings routing is
	// handled entirely by the engine returning a Dispatch decision).
	if resolver.Called {
		t.Error("want deviation resolver NOT called for On Findings loop-back, but it was called")
	}

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
| 1 | Stage One | First | - | ❌ |
| 2 | Stage Two | Second | 1 | ❌ |
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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

// TestSession_Start_Deviation_ResolvesAndResumes verifies the deviation path:
// engine returns Deviation → deviation resolver invoked → artifact re-read →
// execution resumes at the rejoin row.
func TestSession_Start_Deviation_ResolvesAndResumes(t *testing.T) {
	dir := t.TempDir()

	// Workflow where agent-a has absent On Findings → any non-SUCCESS triggers
	// a deviation (the engine can't route it automatically).
	const deviationWorkflow = `[[SECTION:Workflow:deviate]]
<!-- workflow-version: 1.0 -->
## Deviation Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | plan.md | result.md |
[[/SECTION:Workflow:deviate]]
`
	orchPath := filepath.Join(dir, "deviation-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	// Resolver directs rejoin at row 0 (re-run agent-a).
	resolver := &scriptedResolver{
		instruction: domain.RejoinInstruction{
			Rejoin: &domain.RejoinAtRow{RowIndex: 0},
		},
	}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// First agent-a call → PARTIALLY_DONE (deviation, no On Findings column).
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusPARTIALLY_DONE,
		StatusMessage:   "only partially done",
	}})
	// Second agent-a call (after rejoin) → SUCCESS.
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
		WorkflowID:           "deviate",
		Task:                 "task",
		IsNewRun:             true,
		OnDeviation:          domain.DeviationDelegate,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	if !resolver.Called {
		t.Error("want deviation resolver called, but it was not")
	}

	// After the deviation resolver runs, the session must re-read the artifact
	// from the store (FR-23: the orchestrator may update the artifact out-of-band
	// during deviation resolution). Minimum 2 reads: once at run start, once
	// after deviation resolution.
	if store.ReadCount < 2 {
		t.Errorf("want at least 2 store.Read calls (run-start + post-deviation re-read), got %d", store.ReadCount)
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
| 1 | Stage One | The only stage | - | ❌ |
`), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}

	f := harness.NewFakeAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
	}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
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
		Deviation: &scriptedResolver{},
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
	const deviationWorkflow = `[[SECTION:Workflow:deviate-stop]]
<!-- workflow-version: 1.0 -->
## Deviation Stop Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | plan.md | result.md |
[[/SECTION:Workflow:deviate-stop]]
`
	orchPath := filepath.Join(dir, "deviation-stop-orch.md")
	if err := os.WriteFile(orchPath, []byte(deviationWorkflow), 0600); err != nil {
		t.Fatalf("write deviation-stop-orch.md: %v", err)
	}
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	store := &memStore{}
	// Resolver returns StopRun (cannot recover from the deviation).
	resolver := &scriptedResolver{
		instruction: domain.RejoinInstruction{
			Stop: &domain.StopRun{Reason: "no recovery possible"},
		},
	}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
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
		OnDeviation:          domain.DeviationDelegate,
	}

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error for deviation-unresolved outcome, got %v", err)
	}
	if got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want RunDeviationUnresolved when resolver returns StopRun, got %q (message: %q)", got.Status, got.Message)
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
		Deviation: &scriptedResolver{},
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
	const resolveWorkflow = `[[SECTION:Workflow:resolve-test]]
<!-- workflow-version: 1.0 -->
## Resolve Test Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | Plan.md | Progress.md |
| PLANNING | agent-b | ❌ | Progress.md | Result.md |
[[/SECTION:Workflow:resolve-test]]
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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

	const resolveWorkflow = `[[SECTION:Workflow:resolve-out]]
<!-- workflow-version: 1.0 -->
## Resolve Output Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | - | Progress.md |
| PLANNING | agent-b | ❌ | Progress.md | Result.md |
[[/SECTION:Workflow:resolve-out]]
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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
	resolveWorkflow := `[[SECTION:Workflow:already-scoped]]
<!-- workflow-version: 1.0 -->
## Already Scoped Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | ` + scopedPrefix + `Plan.md | ` + scopedPrefix + `Progress.md |
[[/SECTION:Workflow:already-scoped]]
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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

	const resolveWorkflow = `[[SECTION:Workflow:no-runid]]
<!-- workflow-version: 1.0 -->
## No RunID Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | Plan.md | Progress.md |
[[/SECTION:Workflow:no-runid]]
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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
	resolver := &scriptedResolver{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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
	resolver := &scriptedResolver{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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
	resolver := &scriptedResolver{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
| 1 | Stage One | First stage | - | ❌ |
| 2 | Stage Two | Second stage | 1 | ❌ |
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
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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
| 1 | Stage One | The only stage | - | ❌ |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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
| 1 | Stage One | The only stage | - | ❌ |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(singleStagePlan), 0600); err != nil {
		t.Fatalf("write Plan.md: %v", err)
	}
	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     &memStore{},
		Deviation: &scriptedResolver{},
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
		OnDeviation:          domain.DeviationDelegate,
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
	resolver := &scriptedResolver{} // will record whether Resolve was called

	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Deviation: resolver,
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

	// The deviation resolver must never be invoked for infrastructure agent failures.
	// Infrastructure on_failure policies are applied without deviation resolution.
	if resolver.Called {
		t.Error("deviation resolver invoked for infrastructure agent failure: infra failures must follow on_failure policy only, never enter deviation resolution")
	}
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
		Deviation: &scriptedResolver{},
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
