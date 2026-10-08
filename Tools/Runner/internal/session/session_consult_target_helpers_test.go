package session_test

// Shared helpers for tests that check the row and stage a consultation-routed
// step is dispatched and recorded at: scripted instructions that carry a
// stage, and a rig over the grouped staged fixture consult-row-stage-orch.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// queueStagedDispatch enqueues a dispatch instruction for a staged row: the
// plan stage is carried in DispatchInstruction.Stage.
func (s *scriptedRoutingConsultant) queueStagedDispatch(agent, taskDesc string, rowIndex int, stage domain.StageNumber) {
	s.queueDispatch(agent, taskDesc, rowIndex)
	s.stageLastDispatch(stage)
}

// stageLastDispatch sets the stage on the most recently queued dispatch
// instruction. It lets the other queueDispatchWith* variants target a staged row.
func (s *scriptedRoutingConsultant) stageLastDispatch(stage domain.StageNumber) {
	s.instructions[len(s.instructions)-1].Dispatch.Stage = stage
}

// Row indices (zero-based) of consult-row-stage-orch.md.
const (
	rsPlanner     = 0 // PLANNING, non-staged
	rsPlanReview  = 1 // PLANNING, non-staged review
	rsTestWriter  = 2 // EXECUTION.Test, staged
	rsImplementer = 3 // EXECUTION.Implementation, staged
	rsBuildReview = 4 // EXECUTION.Implementation, staged
	rsImplReview  = 5 // EXECUTION.Implementation, staged
	rsFinalReview = 6 // REVIEW, non-staged
)

const consultRowStageWorkflow = "consult-row-stage"

var consultRowStageAgents = []string{
	"planner", "plan-review", "test-writer", "implementer", "build-review", "impl-review", "final-review",
}

// rowStageRig is a resumed run over consult-row-stage-orch.md whose Plan.md
// holds stages 1 and 2 (Approach TDD) and stage 3 (Approach Implementation-Only).
type rowStageRig struct {
	ses   session.Session
	f     *harness.MockAdapter
	store *memStore
	cfg   domain.RunConfig
}

// writeConsultRowStagePlan writes the three-stage Plan.md of the fixture.
func writeConsultRowStagePlan(t *testing.T, dir string) {
	t.Helper()
	const planContent = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Stage One | First stage | - | FALSE | TDD |
| 2 | Stage Two | Second stage | 1 | FALSE | TDD |
| 3 | Stage Three | Third stage | 2 | FALSE | Implementation-Only |
`
	if err := os.WriteFile(filepath.Join(dir, "Plan.md"), []byte(planContent), 0600); err != nil {
		t.Fatalf("writeConsultRowStagePlan: %v", err)
	}
}

// rowStageOpts selects the fixture variant of a rig: the orchestrator file,
// extra agent definitions and the approval reader (nil when the fixture has no
// HITL rows).
type rowStageOpts struct {
	fixture     string
	extraAgents []string
	approvals   domain.ApprovalReader
}

// newRowStageRig builds the rig in the given mode and pre-seeds the store with
// seed (a resumed run).
func newRowStageRig(t *testing.T, consultant domain.RoutingConsultant, mode domain.ExecutionMode, seed domain.ArtifactState) *rowStageRig {
	t.Helper()
	return newRowStageRigWith(t, consultant, mode, seed, rowStageOpts{fixture: "consult-row-stage-orch.md"})
}

// requireStepAt asserts the recorded phase, 1-based row and stage of a step.
func requireStepAt(t *testing.T, step domain.CompletedStep, phase string, row domain.WorkflowRow, stage string) {
	t.Helper()
	if step.Phase != phase {
		t.Errorf("step %q: want Phase %q, got %q", step.AgentInstance, phase, step.Phase)
	}
	if step.WorkflowRow != row {
		t.Errorf("step %q: want WorkflowRow %d, got %d", step.AgentInstance, row, step.WorkflowRow)
	}
	if step.Stage != stage {
		t.Errorf("step %q: want Stage %q, got %q", step.AgentInstance, stage, step.Stage)
	}
}

// stepsByAgent returns every applied step (HITL-rejected attempts included)
// whose agent instance belongs to the agent, in order.
func stepsByAgent(store *memStore, agent string) []domain.CompletedStep {
	var out []domain.CompletedStep
	for _, s := range store.Applied {
		if strings.HasPrefix(s.AgentInstance, agent+"#") {
			out = append(out, s)
		}
	}
	return out
}

// newRowStageRigWith builds the rig over the fixture and options given.
func newRowStageRigWith(t *testing.T, consultant domain.RoutingConsultant, mode domain.ExecutionMode, seed domain.ArtifactState, opts rowStageOpts) *rowStageRig {
	t.Helper()
	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, opts.fixture)
	for _, agent := range append(append([]string{}, consultRowStageAgents...), opts.extraAgents...) {
		writeAgentFile(t, dir, agent)
	}
	writeConsultRowStagePlan(t, dir)

	f := harness.NewMockAdapter()
	store := &memStore{state: seed, exists: true}
	store.state.RunSettings = domain.RunSettings{Mode: mode}
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: opts.approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return &rowStageRig{ses: ses, f: f, store: store, cfg: domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           consultRowStageWorkflow,
		Task:                 "test task",
		IsNewRun:             false,
		RunID:                testRunID,
		RunFolder:            dir,
		RunSettings:          domain.RunSettings{Mode: mode},
	}}
}

// rowStageSeed builds the artifact state of a run whose last recorded workflow
// step is agent (instance number 2) at the given 1-based row, phase and stage.
func rowStageSeed(agent string, row domain.WorkflowRow, phase, stage string) domain.ArtifactState {
	return domain.ArtifactState{
		Workflow:        consultRowStageWorkflow,
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  2,
		CurrentState: domain.CurrentState{
			Phase:      phase,
			Stage:      stage,
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  agent + "#2",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 2, Agent: agent + "#2", Phase: phase, Stage: stage, WorkflowRow: row, Status: domain.StatusSUCCESS},
		},
	}
}

// workflowSteps returns the applied steps that are workflow steps (neither
// infrastructure nor HITL-rejected), in order.
func workflowSteps(store *memStore) []domain.CompletedStep {
	var out []domain.CompletedStep
	for _, s := range store.Applied {
		if !s.IsInfrastructure && !s.HITLRejected {
			out = append(out, s)
		}
	}
	return out
}

// queueSuccess queues one SUCCESS response for agent under the instance id.
func queueSuccess(f *harness.MockAdapter, agent, instance string) {
	f.Queue(agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: instance,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
}
