package engine_test

// Tests for engine.Next and engine.ResumePoint.
//
// Coverage:
//
//   Initial dispatch (no prior invocations):
//   - No log entries and empty CurrentState → dispatches first pre-execution row.
//   - No log entries, workflow starts with EXECUTION rows → dispatches first EXECUTION row of stage 1.
//
//   Linear pre/post-execution advance (non-EXECUTION rows):
//   - SUCCESS + unambiguous On Success hint → dispatches the named agent.
//   - SUCCESS + On Success = "COMPLETE" → returns CompleteDecision.
//   - SUCCESS + absent On Success → returns DeviationDecision.
//   - SUCCESS + free-form On Success (ambiguous) → returns DeviationDecision.
//
//   Staged EXECUTION — approach-driven group ordering (AC7.2):
//   - TDD approach → test group dispatched first, then implementation group.
//   - Implementation-First approach → implementation group first, then test group.
//   - Implementation-Only approach → only implementation group rows dispatched.
//   - Tests-Only approach → only test group rows dispatched.
//
//   Staged EXECUTION — stage-to-stage progression:
//   - After last row in test group (TDD) → dispatches first row of implementation group.
//   - After last row of last group in stage N → dispatches first row of stage N+1 (test group first for TDD).
//   - After last row of last stage → dispatches first post-EXECUTION row.
//   - After last row of last stage, no post-EXECUTION rows → returns CompleteDecision.
//   - Tests-Only approach, last stage: after final test-group row → dispatches first post-EXECUTION row.
//   - Implementation-First approach, last stage: after final test-group row → dispatches first post-EXECUTION row.
//
//   On Success ignored inside staged EXECUTION (AC7.3):
//   - EXECUTION row with On Success set to a specific agent → engine ignores it and uses group/stage logic.
//
//   HITL computation (AC7.4):
//   - Row HITL=true, Stage HITL=false → effective HITL=true.
//   - Row HITL=false, Stage HITL=true, inside EXECUTION → effective HITL=true.
//   - Row HITL=false, Stage HITL=true, outside EXECUTION → effective HITL=false.
//   - Row HITL=false, Stage HITL=false → effective HITL=false.
//
//   Agent instance ID assignment (AC7.5):
//   - seq=0 → dispatched AgentInstanceID is "{agent}#1".
//   - seq=5 → dispatched AgentInstanceID is "{agent}#6".
//   - IDs are monotonically increasing: each Next call increments seq.
//
//   Templated artifact path resolution (AC7.6):
//   - {StageNumber} in input artifacts → replaced with current stage number.
//   - {StageNumber} in output artifacts → replaced with current stage number.
//   - Stage-* in input artifacts → expanded to one path per stage in StageSet.
//   - Stage-* in output artifacts → passed through unexpanded.
//   - Unresolvable placeholder → StopDecision with descriptive reason.
//
//   Non-EXECUTION Stage-* resolution using refreshedStages (AC7.9):
//   - plan-review row with Stage-*/Plan.md input + refreshedStages(3 stages) → 3 expanded paths.
//   - Nil refreshedStages on non-EXECUTION row → uses run-start StageSet for expansion.
//
//   On Findings hint-based auto-routing for COMPLETED_NEEDS_ACTION (AC7.10):
//   - CNA + unambiguous On Findings → DispatchDecision (loop-back to named agent).
//   - CNA + On Findings column absent → DeviationDecision.
//   - CNA + On Findings value is "" (dash/empty) → DeviationDecision.
//   - CNA + free-form On Findings (e.g. "agent (or other based on issue)") → DeviationDecision.
//   - CNA inside EXECUTION + unambiguous On Findings → DispatchDecision (loop-back).
//   - Non-CNA non-SUCCESS (e.g. PARTIALLY_DONE) → DeviationDecision regardless of On Findings.
//
//   Routing hint interpretation for non-SUCCESS responses:
//   - BLOCKED response + unambiguous On Findings → DeviationDecision (On Findings only for CNA).
//
//   Resume point derivation (AC7.7):
//   - No execution log entries → RowIndex=0, RerunLast=false.
//   - Last log entry agent matches CurrentState → RowIndex = next row, RerunLast=false.
//   - Mismatch between last log entry and CurrentState → RerunLast=true, RowIndex = last log row.
//   - Resume from mid-stage position → GroupIndex and StageNumber derived correctly.
//   - Resume with no interruption at end of pre-execution rows → RowIndex = first EXECUTION row.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

// ---- Workflow content fixtures (reused from compat tests, inline for isolation) ----

const quickFixContent = `<!-- workflow-version: 3.0 -->
## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

const brownfieldTDDContent = `<!-- workflow-version: 3.4 -->
## Brownfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

const brownfieldBuildVerifiedContent = `<!-- workflow-version: 2.0 -->
## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |
`

const implOnlyContent = `<!-- workflow-version: 3.1 -->
## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

const greenfieldTDDContent = `<!-- workflow-version: 3.3 -->
## Greenfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | ✅ | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | ❌ | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

// onSuccessDivergentContent is a minimal EXECUTION-only workflow used to test
// that On Success is ignored inside the staged EXECUTION phase. test-writer-tdd
// declares On Success="implementation-tdd" (skipping to the implementation group),
// but with TDD approach, group-order logic requires tests-review-tdd next.
// A correct engine ignores On Success and dispatches tests-review-tdd; an incorrect
// engine would dispatch implementation-tdd. This makes the two behaviours distinguishable.
const onSuccessDivergentContent = `<!-- workflow-version: 1.0 -->
## On-Success-Divergent Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | implementation-tdd | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
`

// ---- Helper functions ----

var fixedNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func mustParseAndAdmit(t *testing.T, content, id, version string) domain.AdmittedWorkflow {
	t.Helper()
	info := domain.WorkflowInfo{
		ID:      domain.WorkflowID(id),
		Version: domain.WorkflowVersion(version),
	}
	table, err := workflow.Parse([]byte(content), info)
	if err != nil {
		t.Fatalf("workflow.Parse(%s): %v", id, err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("compat.Admit(%s): %v", id, err)
	}
	return aw
}

// makeStageSet constructs a StageSet from a list of (number, hitl, approach) tuples.
func makeStageSet(entries []domain.StageEntry) *domain.StageSet {
	ss := &domain.StageSet{Entries: entries}
	return ss
}

// singleStage returns a StageSet with one stage having the given approach.
func singleStageSet(approach domain.Approach) *domain.StageSet {
	return makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: approach},
	})
}

// twoStageSet returns a two-stage StageSet, both with the given approach.
func twoStageSet(approach domain.Approach) *domain.StageSet {
	return makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: approach},
		{Number: 2, HITL: false, Approach: approach},
	})
}

// makeAgents returns a minimal agent reference map for the given identifiers.
func makeAgents(ids ...string) map[string]domain.AgentReference {
	m := make(map[string]domain.AgentReference, len(ids))
	for _, id := range ids {
		m[id] = domain.AgentReference{
			Identifier:     id,
			DefinitionPath: "/agents/" + id + ".md",
		}
	}
	return m
}

// emptyState returns an ArtifactState with no log entries and an empty CurrentState.
func emptyState() domain.ArtifactState {
	return domain.ArtifactState{}
}

// stateAfter returns an ArtifactState reflecting that agentID (in format "name#seq")
// just completed with the given status in the given phase/stage.
func stateAfter(phase, stage, agentID string, status domain.StatusCode, seq int) domain.ArtifactState {
	return domain.ArtifactState{
		GlobalSequence: seq,
		CurrentState: domain.CurrentState{
			Phase:      phase,
			Stage:      stage,
			LastStatus: status,
			LastAgent:  agentID,
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{
				Seq:    seq,
				Agent:  agentID,
				Phase:  phase,
				Stage:  stage,
				Status: status,
			},
		},
	}
}

// successResponse returns a ProtocolResponse with SUCCESS status.
func successResponse(agentID string) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{
		AgentInstanceID: agentID,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "completed successfully",
	}
}

// cnaResponse returns a ProtocolResponse with COMPLETED_NEEDS_ACTION status.
func cnaResponse(agentID string) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{
		AgentInstanceID: agentID,
		StatusCode:      domain.StatusCOMPLETED_NEEDS_ACTION,
		StatusMessage:   "completed with findings",
	}
}

// nonSuccessResponse returns a ProtocolResponse with the given non-SUCCESS status.
func nonSuccessResponse(agentID string, status domain.StatusCode) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{
		AgentInstanceID: agentID,
		StatusCode:      status,
		StatusMessage:   "encountered an issue",
	}
}

// requireDispatch asserts that the decision is a DispatchDecision with exactly one step.
func requireDispatch(t *testing.T, dec domain.EngineDecision) domain.DispatchStep {
	t.Helper()
	if dec.Dispatch == nil {
		var which string
		switch {
		case dec.Complete != nil:
			which = "CompleteDecision"
		case dec.Deviation != nil:
			which = "DeviationDecision"
		case dec.Stop != nil:
			which = fmt.Sprintf("StopDecision(%q)", dec.Stop.Reason)
		default:
			which = "nil decision (all fields nil)"
		}
		t.Fatalf("want DispatchDecision, got %s", which)
	}
	if len(dec.Dispatch.Steps) != 1 {
		t.Fatalf("want exactly 1 dispatch step, got %d", len(dec.Dispatch.Steps))
	}
	return dec.Dispatch.Steps[0]
}

// requireComplete asserts that the decision is a CompleteDecision.
func requireComplete(t *testing.T, dec domain.EngineDecision) domain.CompleteDecision {
	t.Helper()
	if dec.Complete == nil {
		t.Fatalf("want CompleteDecision, got something else (Dispatch=%v Deviation=%v Stop=%v)",
			dec.Dispatch != nil, dec.Deviation != nil, dec.Stop != nil)
	}
	return *dec.Complete
}

// requireDeviation asserts that the decision is a DeviationDecision.
func requireDeviation(t *testing.T, dec domain.EngineDecision) domain.DeviationDecision {
	t.Helper()
	if dec.Deviation == nil {
		t.Fatalf("want DeviationDecision, got something else (Dispatch=%v Complete=%v Stop=%v)",
			dec.Dispatch != nil, dec.Complete != nil, dec.Stop != nil)
	}
	return *dec.Deviation
}

// requireStop asserts that the decision is a StopDecision.
func requireStop(t *testing.T, dec domain.EngineDecision) domain.StopDecision {
	t.Helper()
	if dec.Stop == nil {
		t.Fatalf("want StopDecision, got something else (Dispatch=%v Complete=%v Deviation=%v)",
			dec.Dispatch != nil, dec.Complete != nil, dec.Deviation != nil)
	}
	return *dec.Stop
}

// agentName strips the "#seq" suffix from an agent instance ID.
func agentName(instanceID string) string {
	if i := strings.LastIndex(instanceID, "#"); i >= 0 {
		return instanceID[:i]
	}
	return instanceID
}

// ===== Initial dispatch (no prior invocations) =====

// TestNext_FirstCall_DispatchesFirstPreExecutionRow verifies that when the
// artifact has no execution history, Next dispatches the very first row of
// the workflow (a pre-execution row when the workflow starts before EXECUTION).
func TestNext_FirstCall_DispatchesFirstPreExecutionRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	// First row (index 0) is planner-tdd-soft in PLANNING phase.
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0 (first row), got %d", step.RowIndex)
	}
	if agentName(step.Request.AgentInstanceID) != "planner-tdd-soft" {
		t.Errorf("want agent planner-tdd-soft, got %s", step.Request.AgentInstanceID)
	}
}

// TestNext_FirstCall_StagedOnly_DispatchesFirstExecutionRow verifies that a
// workflow with no pre-execution rows dispatches the first EXECUTION row of
// stage 1 on the initial call.
func TestNext_FirstCall_StagedOnly_DispatchesFirstExecutionRow(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	// implementation-only has no pre-execution rows; row 0 is EXECUTION.[StageNumber].
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0 (first EXECUTION row), got %d", step.RowIndex)
	}
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("want agent implementation-tdd, got %s", step.Request.AgentInstanceID)
	}
	// Stage context must be Stage-1 for the first stage.
	if step.Stage != "Stage-1" {
		t.Errorf("want Stage=Stage-1, got %q", step.Stage)
	}
}

// ===== Linear pre/post-execution advance =====

// TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent verifies that a SUCCESS
// response from a pre-execution row routes to the agent named in On Success.
func TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft, OnSuccess="plan-review"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, stages, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	// On Success of row 0 = "plan-review" → dispatch row 1.
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("want agent plan-review (from On Success hint), got %s", step.Request.AgentInstanceID)
	}
	if step.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review), got %d", step.RowIndex)
	}
}

// TestNext_PostExecution_CompleteTarget_ReturnsComplete verifies that a SUCCESS
// response from the last post-execution row whose On Success is "COMPLETE"
// causes the engine to return a CompleteDecision.
func TestNext_PostExecution_CompleteTarget_ReturnsComplete(t *testing.T) {
	// quick-fix row 3: test-runner, OnSuccess="COMPLETE"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("REVIEW", "", "test-runner#4", domain.StatusSUCCESS, 4)

	dec := engine.Next(aw, stages, state, successResponse("test-runner#4"), agents, 4, fixedNow, nil)

	requireComplete(t, dec)
}

// TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation verifies that a SUCCESS
// response from a pre-execution row with no On Success hint triggers a Deviation.
func TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation(t *testing.T) {
	// Build a minimal workflow where the first pre-execution row has no On Success.
	// brownfield-tdd row 0 (codebase-research) has OnSuccess="requirements-refinement",
	// so we construct a minimal table with a column-absent On Success.
	const noHintContent = `<!-- workflow-version: 1.0 -->
## No Hint Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | ❌ | - | out.md |
| PLANNING | agent-b | ❌ | out.md | final.md |
`
	info := domain.WorkflowInfo{ID: "no-hint", Version: "1.0"}
	table, err := workflow.Parse([]byte(noHintContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Admit will fail for no Stage-*/Plan.md but this workflow has no EXECUTION rows,
	// so HasStagedPhase=false and compat allows it.
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := makeAgents("agent-a", "agent-b")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, nil, state, successResponse("agent-a#1"), agents, 1, fixedNow, nil)

	requireDeviation(t, dec)
}

// ===== Agent instance ID assignment =====

// TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch verifies that the
// dispatched AgentInstanceID uses seq+1, not seq (FR-11: increment before invocation).
func TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	// seq=5: the next dispatch should be #6.
	dec := engine.Next(aw, stages, emptyState(), nil, agents, 5, fixedNow, nil)

	step := requireDispatch(t, dec)
	wantID := "planner-tdd-soft#6"
	if step.Request.AgentInstanceID != wantID {
		t.Errorf("want AgentInstanceID=%q (seq incremented before dispatch), got %q",
			wantID, step.Request.AgentInstanceID)
	}
}

// TestNext_AgentInstanceID_ZeroSeqProducesOne verifies seq=0 → ID uses #1.
func TestNext_AgentInstanceID_ZeroSeqProducesOne(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	wantID := "planner-tdd-soft#1"
	if step.Request.AgentInstanceID != wantID {
		t.Errorf("want AgentInstanceID=%q, got %q", wantID, step.Request.AgentInstanceID)
	}
}

// ===== HITL computation =====

// TestNext_HITL_RowTrue_EffectiveTrue verifies that row-level HITL=true
// produces an effective HITL=true regardless of stage HITL.
func TestNext_HITL_RowTrue_EffectiveTrue(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft has HITL=✅ (true)
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true), got false")
	}
}

// TestNext_HITL_BothFalse_EffectiveFalse verifies that when both row HITL and
// stage HITL are false, effective HITL is false.
func TestNext_HITL_BothFalse_EffectiveFalse(t *testing.T) {
	// quick-fix row 1: plan-review has HITL=❌ (false), stage HITL=false
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesNoHITL := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, stagesNoHITL, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	if step.EffectiveHITL {
		t.Error("want EffectiveHITL=false (row HITL=false, stage HITL=false), got true")
	}
}

// TestNext_HITL_StageTrue_InsideExecution_EffectiveTrue verifies that when stage
// HITL=true and the dispatch is inside the EXECUTION phase, effective HITL=true
// even when the row itself has HITL=false.
func TestNext_HITL_StageTrue_InsideExecution_EffectiveTrue(t *testing.T) {
	// quick-fix row 2: implementation-tdd has HITL=false, but stage HITL=true
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesHITL := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	// State: plan-review (row 1) just succeeded; next is EXECUTION row.
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(aw, stagesHITL, state, successResponse("plan-review#2"), agents, 2, fixedNow, nil)

	step := requireDispatch(t, dec)
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (stage HITL=true inside EXECUTION), got false")
	}
}

// TestNext_HITL_StageTrue_OutsideExecution_EffectiveFalse verifies that stage
// HITL is NOT consulted for pre-execution or post-execution rows. When the row
// has HITL=false, effective HITL must be false even if stage HITL=true.
func TestNext_HITL_StageTrue_OutsideExecution_EffectiveFalse(t *testing.T) {
	// quick-fix row 1: plan-review is a PLANNING row (pre-execution), HITL=false.
	// Stage HITL=true should be ignored for this row.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesHITL := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, stagesHITL, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	if step.EffectiveHITL {
		t.Error("want EffectiveHITL=false (stage HITL not applied outside EXECUTION), got true")
	}
}

// ===== Staged EXECUTION — approach-driven group ordering =====

// TestNext_Approach_TDD_DispatchesTestGroupFirst verifies that TDD approach
// dispatches the test group (test-writer-tdd) before the implementation group.
func TestNext_Approach_TDD_DispatchesTestGroupFirst(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6, last pre-execution row),
	// with TDD approach, the first EXECUTION dispatch should be test-writer-tdd (row 7).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(aw, stages, state, successResponse("contracts-review#7"), agents, 7, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("TDD approach: want first EXECUTION dispatch to test-writer-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Approach_ImplementationFirst_DispatchesImplGroupFirst verifies that
// Implementation-First approach dispatches the implementation group first.
func TestNext_Approach_ImplementationFirst_DispatchesImplGroupFirst(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6), with Implementation-First
	// approach, the first EXECUTION dispatch should be implementation-tdd (row 9),
	// not test-writer-tdd.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachImplementationFirst)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(aw, stages, state, successResponse("contracts-review#7"), agents, 7, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("Implementation-First approach: want first EXECUTION dispatch to implementation-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Approach_ImplementationOnly_SkipsTestGroup verifies that
// Implementation-Only approach never dispatches test group agents.
// After contracts-review, the first dispatch must be implementation-tdd.
func TestNext_Approach_ImplementationOnly_SkipsTestGroup(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(aw, stages, state, successResponse("contracts-review#7"), agents, 7, fixedNow, nil)

	step := requireDispatch(t, dec)
	name := agentName(step.Request.AgentInstanceID)
	if name == "test-writer-tdd" || name == "tests-review-tdd" {
		t.Errorf("Implementation-Only approach: must not dispatch test agents, got %s", name)
	}
	if name != "implementation-tdd" {
		t.Errorf("Implementation-Only approach: want implementation-tdd, got %s", name)
	}
}

// TestNext_Approach_TestsOnly_SkipsImplGroup verifies that Tests-Only approach
// never dispatches implementation group agents.
// After contracts-review, the first dispatch must be test-writer-tdd.
func TestNext_Approach_TestsOnly_SkipsImplGroup(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTestsOnly)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(aw, stages, state, successResponse("contracts-review#7"), agents, 7, fixedNow, nil)

	step := requireDispatch(t, dec)
	name := agentName(step.Request.AgentInstanceID)
	if name == "implementation-tdd" || name == "implementation-review" {
		t.Errorf("Tests-Only approach: must not dispatch implementation agents, got %s", name)
	}
	if name != "test-writer-tdd" {
		t.Errorf("Tests-Only approach: want test-writer-tdd, got %s", name)
	}
}

// ===== Staged EXECUTION — stage progression =====

// TestNext_Staged_TDD_AfterTestGroup_DispatchesImplGroup verifies that after
// the last row of the test group, the next dispatch is the first row of the
// implementation group (TDD approach).
func TestNext_Staged_TDD_AfterTestGroup_DispatchesImplGroup(t *testing.T) {
	// brownfield-tdd, TDD approach:
	// Test group: rows 7 (test-writer-tdd) and 8 (tests-review-tdd).
	// After tests-review-tdd succeeds (row 8), next must be implementation-tdd (row 9).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(aw, stages, state, successResponse("tests-review-tdd#9"), agents, 9, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("after last test-group row, want implementation-tdd (impl group), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Staged_TDD_AfterImplGroupLastStage_AdvancesToNextStage verifies that
// after the last row of the implementation group in stage N (with more stages
// remaining), the engine starts stage N+1 with the test group (TDD approach).
func TestNext_Staged_TDD_AfterImplGroupLastStage_AdvancesToNextStage(t *testing.T) {
	// brownfield-tdd, TDD approach, two stages:
	// After implementation-review (row 10) in Stage-1, advance to Stage-2 test group.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := twoStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(aw, stages, state, successResponse("implementation-review#11"), agents, 11, fixedNow, nil)

	step := requireDispatch(t, dec)
	// Should start Stage-2, test group first (TDD) → test-writer-tdd.
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("after Stage-1 last impl-group row, want test-writer-tdd in Stage-2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Stage-2" {
		t.Errorf("want Stage=Stage-2, got %q", step.Stage)
	}
}

// TestNext_Staged_AfterLastStage_DispatchesPostExecutionRow verifies that after
// the last row of the last stage's last group, the engine dispatches the first
// post-execution row.
func TestNext_Staged_AfterLastStage_DispatchesPostExecutionRow(t *testing.T) {
	// brownfield-tdd, TDD approach, one stage:
	// After implementation-review (row 10, last impl-group row, only stage),
	// engine must dispatch test-runner (row 11, the post-execution REVIEW row).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(aw, stages, state, successResponse("implementation-review#11"), agents, 11, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("after last stage last group, want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row has no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// TestNext_Staged_AfterLastStage_NoPostExecution_ReturnsComplete verifies that
// when a workflow has no post-execution rows, the engine returns CompleteDecision
// after the last row of the last stage.
func TestNext_Staged_AfterLastStage_NoPostExecution_ReturnsComplete(t *testing.T) {
	// brownfield-tdd-build-verified has no post-execution rows (table ends at row 12).
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// implementation-review is row 12, the last EXECUTION row (and last row overall).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#13", domain.StatusSUCCESS, 13)

	dec := engine.Next(aw, stages, state, successResponse("implementation-review#13"), agents, 13, fixedNow, nil)

	requireComplete(t, dec)
}

// ===== On Success ignored inside EXECUTION (FR-12b) =====

// TestNext_Staged_OnSuccessIgnoredInsideExecution verifies that even when an
// EXECUTION row declares an On Success hint, the engine ignores it and routes
// by group/stage logic, not by the hint.
//
// Uses onSuccessDivergentContent where test-writer-tdd declares
// On Success="implementation-tdd" — which skips ahead to the implementation group.
// With TDD approach, group-order logic requires tests-review-tdd next.
// An engine that incorrectly consults On Success would dispatch implementation-tdd,
// making a correct vs incorrect implementation distinguishable.
func TestNext_Staged_OnSuccessIgnoredInsideExecution(t *testing.T) {
	aw := mustParseAndAdmit(t, onSuccessDivergentContent, "on-success-divergent", "1.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review")
	// test-writer-tdd is row 0; after it succeeds in Stage-1 (seq=1).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, stages, state, successResponse("test-writer-tdd#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	// Group-order logic (On Success ignored): within the test group, next is tests-review-tdd.
	// If On Success were consulted, implementation-tdd would be dispatched (wrong).
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("On Success must be ignored inside EXECUTION: want tests-review-tdd (group-order next), got %s (suggests On Success was consulted)",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Stage-1" {
		t.Errorf("want Stage-1, got %q", step.Stage)
	}
}

// ===== Templated artifact path resolution =====

// TestNext_Paths_StageNumber_SubstitutedInInput verifies that {StageNumber} in
// input artifact paths is replaced with the current stage number string.
func TestNext_Paths_StageNumber_SubstitutedInInput(t *testing.T) {
	// quick-fix row 2: implementation-tdd has input "Stage-{StageNumber}/Plan.md".
	// When dispatched in Stage-1, it must resolve to "Stage-1/Plan.md".
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(aw, stages, state, successResponse("plan-review#2"), agents, 2, fixedNow, nil)

	step := requireDispatch(t, dec)
	// Should have resolved Stage-{StageNumber}/Plan.md → Stage-1/Plan.md.
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/Plan.md" {
			found = true
		}
		if strings.Contains(a, "{StageNumber}") {
			t.Errorf("unresolved {StageNumber} found in input artifact: %s", a)
		}
	}
	if !found {
		t.Errorf("want Stage-1/Plan.md in InputArtifacts, got %v", step.Request.InputArtifacts)
	}
}

// TestNext_Paths_StageNumber_SubstitutedInOutput verifies {StageNumber} is
// replaced in output artifact paths too.
func TestNext_Paths_StageNumber_SubstitutedInOutput(t *testing.T) {
	// quick-fix row 2: implementation-tdd output is "Stage-{StageNumber}/PlanProgress.md".
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(aw, stages, state, successResponse("plan-review#2"), agents, 2, fixedNow, nil)

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			found = true
		}
		if strings.Contains(a, "{StageNumber}") {
			t.Errorf("unresolved {StageNumber} found in output artifact: %s", a)
		}
	}
	if !found {
		t.Errorf("want Stage-1/PlanProgress.md in OutputArtifacts, got %v", step.Request.OutputArtifacts)
	}
}

// TestNext_Paths_StageStar_InputExpanded verifies that Stage-* in input artifacts
// is expanded to one path per stage in the stage set.
func TestNext_Paths_StageStar_InputExpanded(t *testing.T) {
	// quick-fix row 1 (plan-review) has input "Stage-*/Plan.md, Stage-*/PlanProgress.md".
	// With 3 stages, Stage-*/Plan.md expands to Stage-1/Plan.md, Stage-2/Plan.md, Stage-3/Plan.md.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	threeStages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: domain.ApproachImplementationOnly},
		{Number: 2, HITL: false, Approach: domain.ApproachImplementationOnly},
		{Number: 3, HITL: false, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, threeStages, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)

	// Stage-*/Plan.md must have expanded to 3 entries.
	wantExpanded := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	for _, want := range wantExpanded {
		found := false
		for _, a := range step.Request.InputArtifacts {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("want %q in InputArtifacts after Stage-* expansion; got %v", want, step.Request.InputArtifacts)
		}
	}
	// The raw Stage-* pattern must not appear unexpanded.
	for _, a := range step.Request.InputArtifacts {
		if strings.Contains(a, "*") {
			t.Errorf("Stage-* still present unexpanded in InputArtifacts: %s", a)
		}
	}
}

// TestNext_Paths_StageStar_OutputPassedThrough verifies that Stage-* in output
// artifacts is dispatched as-is (not expanded).
func TestNext_Paths_StageStar_OutputPassedThrough(t *testing.T) {
	// quick-fix row 0 (planner-tdd-soft) output includes "Stage-*/Plan.md" and
	// "Stage-*/PlanProgress.md". These must pass through to the subagent unchanged.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Stage-*/Plan.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("want Stage-*/Plan.md unexpanded in OutputArtifacts, got %v", step.Request.OutputArtifacts)
	}
}

// ===== Non-EXECUTION Stage-* resolution using refreshedStages (AC7.9) =====

// TestNext_Paths_NonExecution_StageStar_UsesRefreshedStages verifies that
// when refreshedStages is provided for a non-EXECUTION row, Stage-* input
// wildcards are expanded against the refreshed set.
func TestNext_Paths_NonExecution_StageStar_UsesRefreshedStages(t *testing.T) {
	// quick-fix row 1: plan-review has "Stage-*/Plan.md" input.
	// Run-start stages has 1 stage; refreshedStages has 3 stages.
	// The expansion must use the refreshed set (3 entries), not the run-start set.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	runStartStages := singleStageSet(domain.ApproachImplementationOnly)
	refreshed := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: domain.ApproachImplementationOnly},
		{Number: 2, HITL: false, Approach: domain.ApproachImplementationOnly},
		{Number: 3, HITL: false, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, runStartStages, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, refreshed)

	step := requireDispatch(t, dec)
	// With refreshedStages(3), Stage-*/Plan.md must expand to 3 paths.
	count := 0
	for _, a := range step.Request.InputArtifacts {
		if strings.HasSuffix(a, "/Plan.md") && strings.HasPrefix(a, "Stage-") && !strings.Contains(a, "*") {
			count++
		}
	}
	if count != 3 {
		t.Errorf("want 3 expanded Stage-N/Plan.md paths (from refreshedStages), got %d; InputArtifacts=%v",
			count, step.Request.InputArtifacts)
	}
}

// TestNext_Paths_NonExecution_StageStar_NilRefreshed_UsesRunStartStages verifies
// that when refreshedStages is nil, Stage-* expansion uses the run-start stage set.
func TestNext_Paths_NonExecution_StageStar_NilRefreshed_UsesRunStartStages(t *testing.T) {
	// quick-fix row 1: plan-review, Stage-*/Plan.md with run-start stages = 2.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	runStartStages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: domain.ApproachImplementationOnly},
		{Number: 2, HITL: false, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, runStartStages, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	count := 0
	for _, a := range step.Request.InputArtifacts {
		if strings.HasSuffix(a, "/Plan.md") && strings.HasPrefix(a, "Stage-") && !strings.Contains(a, "*") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("want 2 expanded Stage-N/Plan.md paths (from run-start stages), got %d; InputArtifacts=%v",
			count, step.Request.InputArtifacts)
	}
}

// ===== On Findings hint-based auto-routing (AC7.10) =====

// TestNext_OnFindings_UnambiguousHint_CNA_ReturnsDispatch verifies that a
// COMPLETED_NEEDS_ACTION response from a row with an unambiguous On Findings
// hint causes the engine to return a DispatchDecision targeting that agent
// (loop-back within the current group), not a Deviation.
func TestNext_OnFindings_UnambiguousHint_CNA_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 8: build-review (in test group), OnFindings="test-writer-tdd".
	// When build-review returns CNA, engine must dispatch test-writer-tdd (loop-back).
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review is at row 8 (0-indexed) in the test group of brownfield-tdd-build-verified.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(aw, stages, state, cnaResponse("build-review#9"), agents, 9, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("CNA with unambiguous OnFindings=test-writer-tdd: want loop-back Dispatch to test-writer-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch
// verifies that the On Findings auto-routing still works inside EXECUTION even
// though On Success is ignored there (FR-12b applies to On Success, not On Findings).
func TestNext_OnFindings_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 11: second build-review (in impl group), OnFindings="implementation-tdd".
	// When it returns CNA, engine must dispatch implementation-tdd (loop-back), not Deviation.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// row 11 is build-review in the impl group; OnFindings="implementation-tdd".
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusCOMPLETED_NEEDS_ACTION, 12)

	dec := engine.Next(aw, stages, state, cnaResponse("build-review#12"), agents, 12, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("CNA+OnFindings inside EXECUTION: want loop-back to implementation-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation verifies that when the
// On Findings column is absent from the routing table, a CNA response yields
// a DeviationDecision.
func TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation(t *testing.T) {
	// Construct a minimal workflow without an On Findings column at all.
	const noFindingsContent = `<!-- workflow-version: 1.0 -->
## No Findings Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | ❌ | COMPLETE | - | out.md |
`
	info := domain.WorkflowInfo{ID: "no-findings", Version: "1.0"}
	table, err := workflow.Parse([]byte(noFindingsContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := makeAgents("agent-a")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusCOMPLETED_NEEDS_ACTION, 1)

	dec := engine.Next(aw, nil, state, cnaResponse("agent-a#1"), agents, 1, fixedNow, nil)

	requireDeviation(t, dec)
}

// TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings is present but has no value (dash/empty) yields
// a DeviationDecision.
func TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 7: test-writer-tdd has OnFindings="-" (no hint value).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusCOMPLETED_NEEDS_ACTION, 8)

	dec := engine.Next(aw, stages, state, cnaResponse("test-writer-tdd#8"), agents, 8, fixedNow, nil)

	requireDeviation(t, dec)
}

// TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings contains free-form qualification yields a
// DeviationDecision (ambiguous hint cannot be auto-routed).
func TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 10: implementation-review has OnFindings=
	// "implementation-tdd (or other based on issue)" — free-form, not unambiguous.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// implementation-review is row 10 (0-indexed) in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(aw, stages, state, cnaResponse("implementation-review#11"), agents, 11, fixedNow, nil)

	requireDeviation(t, dec)
}

// TestNext_OnFindings_NonCNA_NonSuccess_ReturnsDeviation verifies that for
// non-CNA non-SUCCESS statuses, the engine always returns Deviation regardless
// of whether On Findings has a hint. On Findings auto-routing is only for CNA.
func TestNext_OnFindings_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 8: tests-review-tdd has OnFindings="test-writer-tdd".
	// If it returns PARTIALLY_DONE (not CNA), the engine must return Deviation,
	// even though On Findings names an agent.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusPARTIALLY_DONE, 9)

	dec := engine.Next(aw, stages, state, nonSuccessResponse("tests-review-tdd#9", domain.StatusPARTIALLY_DONE), agents, 9, fixedNow, nil)

	requireDeviation(t, dec)
}

// TestNext_Deviation_Kind_IsNonSuccess verifies the DeviationKind in the
// deviation info is set to DeviationNonSuccess for a non-SUCCESS response.
func TestNext_Deviation_Kind_IsNonSuccess(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)

	dec := engine.Next(aw, stages, state, nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED), agents, 9, fixedNow, nil)

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationNonSuccess {
		t.Errorf("want DeviationKind=%q, got %q", domain.DeviationNonSuccess, dev.Info.Kind)
	}
}

// TestNext_Deviation_IncludesCurrentRowAndPhase verifies that DeviationInfo
// carries the current row index and phase for the resolver's context.
func TestNext_Deviation_IncludesCurrentRowAndPhase(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// tests-review-tdd is row 8 in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)
	resp := nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED)

	dec := engine.Next(aw, stages, state, resp, agents, 9, fixedNow, nil)

	dev := requireDeviation(t, dec)
	if dev.Info.CurrentRow != 8 {
		t.Errorf("want CurrentRow=8 (tests-review-tdd), got %d", dev.Info.CurrentRow)
	}
	if dev.Info.CurrentPhase != "EXECUTION.[StageNumber]" {
		t.Errorf("want CurrentPhase=EXECUTION.[StageNumber], got %q", dev.Info.CurrentPhase)
	}
	if dev.Info.CurrentStage != "Stage-1" {
		t.Errorf("want CurrentStage=Stage-1, got %q", dev.Info.CurrentStage)
	}
}

// ===== Implementation-First approach: test group runs after implementation group =====

// TestNext_Approach_ImplementationFirst_AfterImplGroup_DispatchesTestGroup verifies
// that with Implementation-First approach, after the last row of the implementation
// group, the engine dispatches the first row of the test group.
func TestNext_Approach_ImplementationFirst_AfterImplGroup_DispatchesTestGroup(t *testing.T) {
	// brownfield-tdd, Implementation-First: impl group = rows 9-10, test group = rows 7-8.
	// After implementation-review (row 10) in Stage-1, the next dispatch must be
	// test-writer-tdd (row 7, first test group row).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachImplementationFirst)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(aw, stages, state, successResponse("implementation-review#11"), agents, 11, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("Implementation-First: after impl group, want test-writer-tdd (first test-group row), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Approach_TestsOnly_AfterTestGroup_AdvancesToNextStage verifies that
// with Tests-Only approach, after the last row of the test group, the engine
// advances to the next stage (not the implementation group, which is skipped).
func TestNext_Approach_TestsOnly_AfterTestGroup_AdvancesToNextStage(t *testing.T) {
	// brownfield-tdd, Tests-Only, 2 stages:
	// After tests-review-tdd (row 8, last test-group row) in Stage-1,
	// advance to Stage-2 test-writer-tdd (no implementation group).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := twoStageSet(domain.ApproachTestsOnly)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(aw, stages, state, successResponse("tests-review-tdd#9"), agents, 9, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("Tests-Only after last test-group row: want test-writer-tdd in Stage-2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Stage-2" {
		t.Errorf("Tests-Only: want Stage-2, got %q", step.Stage)
	}
}

// ===== Build-verified workflow: duplicate agent (build-review appears twice) =====

// TestNext_BuildVerified_TDD_TestGroupSequence verifies the complete test-group
// sequence in brownfield-tdd-build-verified with TDD approach:
// test-writer-tdd → build-review → tests-review-tdd.
func TestNext_BuildVerified_TDD_TestGroupSequence(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)

	tests := []struct {
		name       string
		state      domain.ArtifactState
		response   *domain.ProtocolResponse
		seq        int
		wantAgent  string
	}{
		{
			name:      "after contracts-review, dispatches test-writer-tdd",
			state:     stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7),
			response:  successResponse("contracts-review#7"),
			seq:       7,
			wantAgent: "test-writer-tdd",
		},
		{
			name:      "after test-writer-tdd, dispatches build-review (test build)",
			state:     stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8),
			response:  successResponse("test-writer-tdd#8"),
			seq:       8,
			wantAgent: "build-review",
		},
		{
			name:      "after build-review (test build), dispatches tests-review-tdd",
			state:     stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusSUCCESS, 9),
			response:  successResponse("build-review#9"),
			seq:       9,
			wantAgent: "tests-review-tdd",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := engine.Next(aw, stages, tc.state, tc.response, agents, tc.seq, fixedNow, nil)
			step := requireDispatch(t, dec)
			if agentName(step.Request.AgentInstanceID) != tc.wantAgent {
				t.Errorf("want %s, got %s", tc.wantAgent, step.Request.AgentInstanceID)
			}
		})
	}
}

// TestNext_BuildVerified_TDD_ImplGroupSequence verifies the complete impl-group
// sequence in brownfield-tdd-build-verified with TDD approach:
// implementation-tdd → build-review → implementation-review.
func TestNext_BuildVerified_TDD_ImplGroupSequence(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)

	tests := []struct {
		name      string
		state     domain.ArtifactState
		response  *domain.ProtocolResponse
		seq       int
		wantAgent string
	}{
		{
			name:      "after tests-review-tdd (last test-group row), dispatches implementation-tdd",
			state:     stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#10", domain.StatusSUCCESS, 10),
			response:  successResponse("tests-review-tdd#10"),
			seq:       10,
			wantAgent: "implementation-tdd",
		},
		{
			name:      "after implementation-tdd, dispatches build-review (impl build)",
			state:     stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#11", domain.StatusSUCCESS, 11),
			response:  successResponse("implementation-tdd#11"),
			seq:       11,
			wantAgent: "build-review",
		},
		{
			name:      "after build-review (impl build), dispatches implementation-review",
			state:     stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusSUCCESS, 12),
			response:  successResponse("build-review#12"),
			seq:       12,
			wantAgent: "implementation-review",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := engine.Next(aw, stages, tc.state, tc.response, agents, tc.seq, fixedNow, nil)
			step := requireDispatch(t, dec)
			if agentName(step.Request.AgentInstanceID) != tc.wantAgent {
				t.Errorf("want %s, got %s", tc.wantAgent, step.Request.AgentInstanceID)
			}
		})
	}
}

// ===== DispatchStep fields =====

// TestNext_DispatchStep_PhaseAndStageSet verifies that the dispatched step
// carries the correct Phase and Stage values for recording in the artifact.
func TestNext_DispatchStep_PhaseAndStageSet(t *testing.T) {
	// quick-fix: dispatching implementation-tdd (row 2, EXECUTION.[StageNumber]) in Stage-1.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(aw, stages, state, successResponse("plan-review#2"), agents, 2, fixedNow, nil)

	step := requireDispatch(t, dec)
	if step.Phase != "EXECUTION.[StageNumber]" {
		t.Errorf("want Phase=EXECUTION.[StageNumber], got %q", step.Phase)
	}
	if step.Stage != "Stage-1" {
		t.Errorf("want Stage=Stage-1, got %q", step.Stage)
	}
}

// TestNext_DispatchStep_RowIndex verifies that RowIndex matches the routing table.
func TestNext_DispatchStep_RowIndex(t *testing.T) {
	// quick-fix: row 0 = planner-tdd-soft.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0, got %d", step.RowIndex)
	}
}

// TestNext_DispatchStep_AgentIdentifier verifies that DispatchStep.Agent.Identifier
// matches the routing table's agent field.
func TestNext_DispatchStep_AgentIdentifier(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	if step.Agent.Identifier != "planner-tdd-soft" {
		t.Errorf("want Agent.Identifier=planner-tdd-soft, got %q", step.Agent.Identifier)
	}
}

// ===== ResumePoint =====

// TestResumePoint_NoLogEntries_ReturnsFirstRow verifies that with no execution
// log entries, ResumePoint returns RowIndex=0 and RerunLast=false.
func TestResumePoint_NoLogEntries_ReturnsFirstRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)

	info, err := engine.ResumePoint(aw, stages, emptyState())
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 0 {
		t.Errorf("want RowIndex=0, got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false for fresh start, got true")
	}
	if info.Seq != 0 {
		t.Errorf("want Seq=0 (no prior invocations), got %d", info.Seq)
	}
}

// TestResumePoint_LastEntrySuccess_AdvancesToNextRow verifies that when the
// last log entry matches CurrentState (clean completion), the resume point is
// the row after the last completed one.
func TestResumePoint_LastEntrySuccess_AdvancesToNextRow(t *testing.T) {
	// After planner-tdd-soft (row 0) completed with SUCCESS.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (row after planner-tdd-soft), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
	if info.Seq != 1 {
		t.Errorf("want Seq=1 (GlobalSequence from artifact), got %d", info.Seq)
	}
}

// TestResumePoint_Interruption_RerunsLastRow verifies that when the last log
// entry does not match CurrentState (detected mismatch), RerunLast=true and
// RowIndex points to the interrupted row (not the next one).
func TestResumePoint_Interruption_RerunsLastRow(t *testing.T) {
	// Simulate an interruption: the log says plan-review#2 ran, but the runner
	// was interrupted before CurrentState was updated to reflect it. CurrentState
	// still shows planner-tdd-soft from the prior step.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)

	// CurrentState reflects planner-tdd-soft (row 0), but the log has a second
	// entry for plan-review (row 1) — meaning plan-review started but the artifact
	// was not updated to reflect its completion.
	state := domain.ArtifactState{
		GlobalSequence: 1,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "plan-review#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (interruption detected), got false")
	}
	// RowIndex should point to the interrupted row (plan-review = row 1).
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the interrupted row), got %d", info.RowIndex)
	}
}

// TestResumePoint_StagedWorkflow_DerivesGroupAndStage verifies that ResumePoint
// correctly derives GroupIndex and StageNumber for a staged workflow position.
func TestResumePoint_StagedWorkflow_DerivesGroupAndStage(t *testing.T) {
	// After test-writer-tdd (row 7) in Stage-1 of brownfield-tdd.
	// GroupIndex should be 0 (test group), StageNumber=1.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8)

	info, err := engine.ResumePoint(aw, stages, state)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	// Next row should be tests-review-tdd (row 8).
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd), got %d", info.RowIndex)
	}
	// GroupIndex=0 (test group, first group in admitted workflow Groups slice).
	if info.GroupIndex != 0 {
		t.Errorf("want GroupIndex=0 (test group), got %d", info.GroupIndex)
	}
	if info.StageNumber != 1 {
		t.Errorf("want StageNumber=1, got %d", info.StageNumber)
	}
	if info.Stage != "Stage-1" {
		t.Errorf("want Stage=Stage-1, got %q", info.Stage)
	}
}

// TestResumePoint_StagedWorkflow_NonExecutionRow_GroupIndexNegativeOne verifies
// that a resume point outside the staged phase has GroupIndex=-1.
func TestResumePoint_StagedWorkflow_NonExecutionRow_GroupIndexNegativeOne(t *testing.T) {
	// After codebase-research (row 0) in brownfield-tdd (RESEARCH phase, pre-execution).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)
	state := stateAfter("RESEARCH", "", "codebase-research#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.GroupIndex != -1 {
		t.Errorf("want GroupIndex=-1 (not in staged phase), got %d", info.GroupIndex)
	}
	if info.StageNumber != 0 {
		t.Errorf("want StageNumber=0 (not in staged phase), got %d", info.StageNumber)
	}
}

// TestResumePoint_MidStageInterruption_DerivesCorrectRow verifies FR-33:
// when the runner was interrupted while executing an EXECUTION row, ResumePoint
// returns RerunLast=true so the session re-dispatches the interrupted row.
func TestResumePoint_MidStageInterruption_DerivesCorrectRow(t *testing.T) {
	// In brownfield-tdd (TDD approach), Stage-1:
	// Log says tests-review-tdd ran (row 8), but CurrentState only shows test-writer-tdd.
	// This means tests-review-tdd was dispatched but the artifact update was lost.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTDD)

	state := domain.ArtifactState{
		GlobalSequence: 8,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "test-writer-tdd#8",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 9, Agent: "tests-review-tdd#9", Phase: "EXECUTION.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (mid-stage interruption), got false")
	}
	// tests-review-tdd is row 8 (0-indexed) in brownfield-tdd.
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd, the interrupted row), got %d", info.RowIndex)
	}
}

// ===== DispatchStep carries artifact paths exactly =====

// TestNext_Artifacts_PreExecutionRow_NoTemplateExpansion verifies that
// pre-execution artifact paths with no template variables are passed through
// unchanged.
func TestNext_Artifacts_PreExecutionRow_NoTemplateExpansion(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft output = "Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md"
	// Plan.md has no template — must appear unchanged.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(aw, stages, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	found := false
	for _, a := range step.Request.OutputArtifacts {
		if a == "Plan.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("want Plan.md in OutputArtifacts unchanged, got %v", step.Request.OutputArtifacts)
	}
}

// TestNext_Artifacts_InputArtifactsMatchRow verifies that InputArtifacts in the
// ProtocolRequest are populated from the routing row (after template resolution).
func TestNext_Artifacts_InputArtifactsMatchRow(t *testing.T) {
	// quick-fix row 1 (plan-review): inputs are Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md.
	// After Stage-* expansion with 1 stage: Plan.md, Stage-1/Plan.md, Stage-1/PlanProgress.md.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet(domain.ApproachImplementationOnly)
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, stages, state, successResponse("planner-tdd-soft#1"), agents, 1, fixedNow, nil)

	step := requireDispatch(t, dec)
	if len(step.Request.InputArtifacts) == 0 {
		t.Error("want non-empty InputArtifacts for plan-review row, got empty")
	}
	// "Plan.md" (no template) must appear.
	foundPlanMd := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Plan.md" {
			foundPlanMd = true
		}
	}
	if !foundPlanMd {
		t.Errorf("want Plan.md in InputArtifacts, got %v", step.Request.InputArtifacts)
	}
}

// ===== Unresolvable template path → StopDecision (AC7.6) =====

// TestNext_Paths_UnresolvableStageNumber_ReturnsStop verifies that when an artifact
// path contains {StageNumber} in a context where the stage number cannot be resolved
// (e.g. a pre-execution PLANNING row has no stage context), the engine returns a
// StopDecision with a non-empty reason rather than silently ignoring or panicking.
func TestNext_Paths_UnresolvableStageNumber_ReturnsStop(t *testing.T) {
	// A PLANNING row with {StageNumber} in its input — {StageNumber} is only valid
	// inside EXECUTION phase. There is no current stage context when dispatching this
	// pre-execution row, so the engine cannot substitute the placeholder.
	const unresolvableTemplateContent = `<!-- workflow-version: 1.0 -->
## Unresolvable Template Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | COMPLETE | - | Stage-{StageNumber}/Plan.md | out.md |
`
	info := domain.WorkflowInfo{ID: "unresolvable-template", Version: "1.0"}
	table, err := workflow.Parse([]byte(unresolvableTemplateContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := makeAgents("agent-a")

	dec := engine.Next(aw, nil, emptyState(), nil, agents, 0, fixedNow, nil)

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason for unresolvable {StageNumber} placeholder")
	}
}

// ===== Free-form On Success hint → DeviationDecision (AC7.1, FR-27) =====

// TestNext_PreExecution_FreeFormOnSuccess_ReturnsDeviation verifies that when a
// pre-execution row's On Success column contains qualifying text (free-form value),
// a SUCCESS response yields a DeviationDecision rather than dispatching the named agent.
// This is distinct from the absent-column case: the column is present, but the value
// is not an unambiguous agent identifier.
func TestNext_PreExecution_FreeFormOnSuccess_ReturnsDeviation(t *testing.T) {
	// On Success = "agent-b (or other based on issue)" — presence of qualifying text
	// makes this a free-form / ambiguous hint that cannot be auto-routed.
	const freeFormOnSuccessContent = `<!-- workflow-version: 1.0 -->
## Free-Form On Success Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b (or other based on issue) | - | - | out.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | out.md | final.md |
`
	info := domain.WorkflowInfo{ID: "free-form-on-success", Version: "1.0"}
	table, err := workflow.Parse([]byte(freeFormOnSuccessContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	agents := makeAgents("agent-a", "agent-b")
	state := stateAfter("PLANNING", "", "agent-a#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(aw, nil, state, successResponse("agent-a#1"), agents, 1, fixedNow, nil)

	requireDeviation(t, dec)
}

// ===== Greenfield-TDD workflow fixture (T7.1 requirement) =====

// TestNext_GreenfieldTDD_ArchitecturePhaseRouting verifies that the greenfield-tdd
// workflow's ARCHITECTURE phase routes correctly: after system-design-review succeeds,
// the engine dispatches planner-tdd-soft as named in the On Success column.
// This exercises a routing path (ARCHITECTURE phase) absent in the other four workflow
// fixtures and confirms the engine handles the greenfield row structure correctly.
func TestNext_GreenfieldTDD_ArchitecturePhaseRouting(t *testing.T) {
	aw := mustParseAndAdmit(t, greenfieldTDDContent, "greenfield-tdd", "3.3")
	stages := singleStageSet(domain.ApproachTDD)
	agents := makeAgents(
		"requirements-refinement", "requirements-review",
		"system-designer", "system-design-review",
		"planner-tdd-soft", "plan-review",
		"contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// system-design-review is row 3 (0-indexed) in greenfield-tdd; On Success = "planner-tdd-soft".
	state := stateAfter("ARCHITECTURE", "", "system-design-review#4", domain.StatusSUCCESS, 4)

	dec := engine.Next(aw, stages, state, successResponse("system-design-review#4"), agents, 4, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "planner-tdd-soft" {
		t.Errorf("greenfield-tdd: after system-design-review (ARCHITECTURE phase), want planner-tdd-soft, got %s",
			step.Request.AgentInstanceID)
	}
	// planner-tdd-soft is row 4 in greenfield-tdd.
	if step.RowIndex != 4 {
		t.Errorf("want RowIndex=4 (planner-tdd-soft), got %d", step.RowIndex)
	}
}

// ===== Tests-Only approach: final stage boundary (AC7.2) =====

// TestNext_Approach_TestsOnly_LastStage_AdvancesToPostExecution verifies that with
// Tests-Only approach, after the final test-group row in the last stage the engine
// advances to the first post-EXECUTION row (not to an implementation group that
// Tests-Only does not run). Completes the AC7.2 coverage for Tests-Only approach.
func TestNext_Approach_TestsOnly_LastStage_AdvancesToPostExecution(t *testing.T) {
	// brownfield-tdd, Tests-Only approach, single stage:
	// Test group = [test-writer-tdd (row 7), tests-review-tdd (row 8)].
	// After tests-review-tdd succeeds in Stage-1 (the only stage), there are no
	// more stages and no implementation group to run. Engine must dispatch
	// test-runner (row 11, the post-execution REVIEW row).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachTestsOnly)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(aw, stages, state, successResponse("tests-review-tdd#9"), agents, 9, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("Tests-Only last stage: after final test-group row, want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row carries no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// ===== Implementation-First approach: final stage boundary (AC7.2) =====

// TestNext_Approach_ImplementationFirst_LastStageTestGroupComplete_AdvancesToPostExecution
// verifies that with Implementation-First approach, after the test group (which runs
// second) finishes in the last stage, the engine advances to the first post-EXECUTION
// row. Completes the AC7.2 coverage for Implementation-First approach.
func TestNext_Approach_ImplementationFirst_LastStageTestGroupComplete_AdvancesToPostExecution(t *testing.T) {
	// brownfield-tdd, Implementation-First approach, single stage:
	// Group order: impl group (rows 9-10) runs first, then test group (rows 7-8).
	// After tests-review-tdd (row 8, last row of the test group which runs second)
	// in Stage-1 (the only stage), engine must advance to test-runner (post-execution).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet(domain.ApproachImplementationFirst)
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(aw, stages, state, successResponse("tests-review-tdd#9"), agents, 9, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-runner" {
		t.Errorf("Implementation-First last-stage test-group completion: want post-execution test-runner, got %s",
			step.Request.AgentInstanceID)
	}
	// Post-execution row carries no Stage context.
	if step.Stage != "" {
		t.Errorf("want Stage=\"\" for post-execution row, got %q", step.Stage)
	}
}

// ===== HITL truth table: both row and stage HITL true (AC7.4) =====

// TestNext_HITL_RowTrue_StageTrue_EffectiveTrue verifies the final case in the HITL
// truth table: when both row HITL and stage HITL are true (and the dispatch is inside
// EXECUTION), effective HITL is true. Completes the four-combination truth table for AC7.4.
func TestNext_HITL_RowTrue_StageTrue_EffectiveTrue(t *testing.T) {
	// Use a minimal EXECUTION-only workflow with HITL=true on the first row.
	// Stage HITL=true as well → effective HITL must be true (OR of both).
	const bothHITLTrueContent = `<!-- workflow-version: 1.0 -->
## Both HITL True Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ✅ | agent-b | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/out.md |
| EXECUTION.[StageNumber] | agent-b | ❌ | COMPLETE | - | Stage-{StageNumber}/out.md | Stage-{StageNumber}/final.md |
`
	info := domain.WorkflowInfo{ID: "both-hitl-true", Version: "1.0"}
	table, err := workflow.Parse([]byte(bothHITLTrueContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	stagesHITL := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: true, Approach: domain.ApproachImplementationOnly},
	})
	agents := makeAgents("agent-a", "agent-b")

	// Initial dispatch: no prior log, dispatch agent-a (row 0, HITL=true) in Stage-1 (HITL=true).
	dec := engine.Next(aw, stagesHITL, emptyState(), nil, agents, 0, fixedNow, nil)

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-a" {
		t.Fatalf("want agent-a as first dispatch, got %s", step.Request.AgentInstanceID)
	}
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true AND stage HITL=true), got false")
	}
}
