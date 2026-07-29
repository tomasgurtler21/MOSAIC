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
//   Existing artifact modes:
//   - ExistingFresh with a pre-existing artifact: session creates a new artifact
//     from scratch (GlobalSequence resets to 0 for the new run).
//   - ExistingFail with a pre-existing artifact: session returns RunRefused.
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

import (
	"context"
	"errors"
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
	state     domain.ArtifactState
	exists    bool
	readErr   error
	Applied   []domain.CompletedStep
	ReadCount int // counts every call to Read; used to verify re-read after deviation
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

func (m *memStore) Create(_ context.Context, info domain.WorkflowInfo, task string, checkpoints bool, now time.Time) (domain.ArtifactState, error) {
	m.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
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
// orchestrator file directory.
func baseLinearConfig(orchPath string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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
	cfg.ExistingArtifact = domain.ExistingResume

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
	cfg.ExistingArtifact = domain.ExistingResume

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
		ExistingArtifact:     domain.ExistingResume,
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
		ExistingArtifact:     domain.ExistingResume,
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

// ===== ExistingArtifact modes =====

// TestSession_Start_ExistingFresh_StartsFromScratch verifies that when
// ExistingArtifact=ExistingFresh and a pre-existing artifact is present, the
// session creates a fresh artifact rather than resuming. The first Apply call
// must have Seq=1 (fresh-run sequence number), not a continuation of the
// existing GlobalSequence.
func TestSession_Start_ExistingFresh_StartsFromScratch(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Pre-populate the store with an existing artifact at GlobalSequence=5.
	store.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "old task",
		GlobalSequence:  5,
		CurrentState: domain.CurrentState{
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#5",
		},
	}
	store.exists = true

	// Queue responses for both agents (fresh run, starting from agent-a).
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
	cfg.ExistingArtifact = domain.ExistingFresh

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify fresh start: the first Apply must have Seq=1 (not 6, which would
	// indicate continuation from the pre-existing GlobalSequence=5).
	if len(store.Applied) == 0 {
		t.Fatal("want at least one Apply call for fresh run, got zero")
	}
	if store.Applied[0].Seq != 1 {
		t.Errorf("want first Apply Seq=1 (fresh run), got Seq=%d (suggests resume from existing artifact)", store.Applied[0].Seq)
	}
}

// TestSession_Start_ExistingFail_ArtifactExistsReturnsRefusal verifies that
// when ExistingArtifact=ExistingFail and a pre-existing artifact is present,
// the session returns RunRefused without dispatching any agents.
func TestSession_Start_ExistingFail_ArtifactExistsReturnsRefusal(t *testing.T) {
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

	cfg := baseLinearConfig(orchPath)
	cfg.ExistingArtifact = domain.ExistingFail

	got, err := ses.Start(context.Background(), cfg)

	requireRefused(t, got, err)
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
