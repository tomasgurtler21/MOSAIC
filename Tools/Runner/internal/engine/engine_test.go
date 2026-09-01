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
//   On Findings hint-based auto-routing for COMPLETED_NEEDS_ACTION, mode-gated:
//   - auto-review + CNA + unambiguous On Findings → DispatchDecision (loop-back to named agent).
//   - auto + CNA + unambiguous On Findings → DeviationDecision (On-Findings route gated to auto-review).
//   - CNA + On Findings column absent → DeviationDecision in all modes.
//   - CNA + On Findings value is "" (dash/empty) → DeviationDecision in all modes.
//   - CNA + free-form On Findings (e.g. "agent (or other based on issue)") → DeviationDecision in all modes.
//   - auto-review + CNA inside EXECUTION + unambiguous On Findings → DispatchDecision (loop-back).
//   - auto + CNA inside EXECUTION + unambiguous On Findings → DeviationDecision.
//   - Non-CNA non-SUCCESS (e.g. PARTIALLY_DONE) → DeviationDecision regardless of On Findings.
//
//   Routing hint interpretation for non-SUCCESS responses:
//   - BLOCKED response + unambiguous On Findings → DeviationDecision (On Findings only for CNA).
//
//   Mode-gated routing (Stage 2):
//   - auto mode SUCCESS → routes to On Success target (same as auto-review).
//   - auto-review mode SUCCESS → routes to On Success target (same as auto).
//   - orchestrated mode, first call → ConsultDecision with ConsultTriggerOrchestratedMode.
//   - orchestrated mode, after SUCCESS → ConsultDecision (engine never routes).
//   - orchestrated mode, after CNA (even with unambiguous OnFindings) → ConsultDecision.
//   - auto + CNA + ambiguous OnFindings → DeviationDecision.
//   - auto-review + CNA + ambiguous OnFindings → DeviationDecision.
//   - auto + non-CNA non-SUCCESS → DeviationDecision.
//   - auto-review + non-CNA non-SUCCESS → DeviationDecision.
//
//   Review artifact injection (Stage 2, auto-review only):
//   - auto-review CNA auto-route-back: LastOutputArtifacts injected into dispatched InputArtifacts.
//   - Injected artifacts appear after table-row entries, not before.
//   - Paths already in table InputArtifacts are not duplicated by injection.
//   - SUCCESS-routed dispatches never carry injected artifacts.
//   - auto mode CNA deviates; no injection occurs on deviation decisions.
//
//   Resume point derivation (AC7.7):
//   - No execution log entries → RowIndex=0, RerunLast=false.
//   - Last log entry agent matches CurrentState → RowIndex = next row, RerunLast=false.
//   - Mismatch between last log entry and CurrentState → RerunLast=true, RowIndex = last log row.
//   - Resume from mid-stage position → GroupIndex and StageNumber derived correctly.
//   - Resume with no interruption at end of pre-execution rows → RowIndex = first EXECUTION row.
//
//   Canonical phase and stage recording (AC4.2-AC4.5):
//   - Grouped EXECUTION dispatch step records bare phase + group-qualified stage ("Test.1").
//   - Ungrouped staged dispatch step records bare phase + plain stage number ("1").
//   - Non-staged dispatch step records an empty stage.
//   - Target-format recorded state (bare phase, group-qualified stage) resolves position,
//     including disambiguating a repeated agent by group ("Test.1" vs "Implementation.1"),
//     at both the Next and ResumePoint levels.
//   - A legacy artifact (qualified phase, "Stage-N" stage) still resolves position, recovers
//     the stage number, and remains resumable at both the Next and ResumePoint levels.

import (
	"errors"
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

const quickFixContent = `## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

const brownfieldTDDContent = `## Brownfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

const brownfieldBuildVerifiedContent = `## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

const implOnlyContent = `## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

const greenfieldTDDContent = `## Greenfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | TRUE | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | FALSE | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// onSuccessDivergentContent is a minimal EXECUTION-only workflow used to test
// that On Success is ignored inside the staged EXECUTION phase. test-writer-tdd
// declares On Success="implementation-tdd" (skipping to the implementation group),
// but with TDD approach, group-order logic requires tests-review-tdd next.
// A correct engine ignores On Success and dispatches tests-review-tdd; an incorrect
// engine would dispatch implementation-tdd. This makes the two behaviours distinguishable.
const onSuccessDivergentContent = `## On-Success-Divergent Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | test-writer-tdd | FALSE | implementation-tdd | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
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
			InvocationKind: domain.InvocationOrdinary,
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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// implementation-only has no pre-execution rows; row 0 is EXECUTION.[StageNumber].
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0 (first EXECUTION row), got %d", step.RowIndex)
	}
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("want agent implementation-tdd, got %s", step.Request.AgentInstanceID)
	}
	// Stage context must be the plain stage number for the first stage
	// (implementation-only declares no group).
	if step.Stage != "1" {
		t.Errorf("want Stage=1, got %q", step.Stage)
	}
}

// ===== Stage-set-absent diagnostic (domain.StageSource) =====
//
// When a staged row is reached with stages == nil, Next's stop message must
// name what was expected and where it was looked for, distinguishing "a
// stage table was supposed to be seeded here and is missing" from "this run
// never had one". stageSource is optional and variadic; omitting it (or
// passing the zero value) must still yield a non-empty stop reason.

// TestNext_NoStageSet_Omitted_StillProducesNonEmptyStopReason verifies that
// calling Next without a stageSource argument at all (the pre-existing call
// shape used everywhere else in this file) continues to produce a stop with
// a non-empty reason for a staged workflow with no stage set.
func TestNext_NoStageSet_Omitted_StillProducesNonEmptyStopReason(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason when no stage set is available, got empty")
	}
}

// TestNext_NoStageSet_InitialDispatch_NeverSeeded_NamesPathLookedFor verifies
// that on the very first call (initial dispatch into a staged-only workflow),
// a stageSource whose Seeded field is false still names the path that was
// looked for in the stop reason.
func TestNext_NoStageSet_InitialDispatch_NeverSeeded_NamesPathLookedFor(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: false}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// TestNext_NoStageSet_InitialDispatch_SeededButMissing_NamesPathLookedFor
// verifies that a stageSource whose Seeded field is true also names the path
// that was looked for -- the seeded-but-missing case must still identify
// where the table should have been.
func TestNext_NoStageSet_InitialDispatch_SeededButMissing_NamesPathLookedFor(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: true}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// TestNext_NoStageSet_InitialDispatch_SeededVsNeverSeeded_DistinctMessages
// verifies that Seeded actually changes the rendered stop reason: the
// seeded-but-missing case and the never-seeded case, for the same Path, must
// be distinguishable from each other -- this is the entire point of carrying
// the Seeded field rather than just Path.
func TestNext_NoStageSet_InitialDispatch_SeededVsNeverSeeded_DistinctMessages(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")
	path := "/runs/example/Plan.md"

	neverSeeded := requireStop(t, engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     domain.StageSource{Path: path, Seeded: false},
		Mode:            domain.ExecutionModeAutoReview,
	}))
	seededMissing := requireStop(t, engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		StageSource:     domain.StageSource{Path: path, Seeded: true},
		Mode:            domain.ExecutionModeAutoReview,
	}))

	if neverSeeded.Reason == seededMissing.Reason {
		t.Errorf("want distinct stop reasons for Seeded=false vs Seeded=true (same Path); got identical reason %q for both",
			neverSeeded.Reason)
	}
}

// TestNext_NoStageSet_EnteringExecution_NeverSeeded_NamesPathLookedFor
// verifies that the second call site where a staged row can be reached with
// no stage set -- transitioning from a pre-EXECUTION row's SUCCESS into the
// staged EXECUTION phase -- also renders the stageSource path into the stop
// reason.
func TestNext_NoStageSet_EnteringExecution_NeverSeeded_NamesPathLookedFor(t *testing.T) {
	// quick-fix row 1: plan-review, OnSuccess="implementation-tdd" (an EXECUTION row).
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)
	src := domain.StageSource{Path: "/runs/example/Plan.md", Seeded: false}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		StageSource:     src,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, src.Path) {
		t.Errorf("want StopDecision.Reason to name the looked-for path %q; got %q", src.Path, stop.Reason)
	}
}

// ===== Linear pre/post-execution advance =====

// TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent verifies that a SUCCESS
// response from a pre-execution row routes to the agent named in On Success.
func TestNext_PreExecution_OnSuccess_AdvancesToNamedAgent(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft, OnSuccess="plan-review"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("REVIEW", "", "test-runner#4", domain.StatusSUCCESS, 4)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-runner#4"),
		Agents:          agents,
		Seq:             4,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireComplete(t, dec)
}

// TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation verifies that a SUCCESS
// response from a pre-execution row with no On Success hint triggers a Deviation.
func TestNext_PreExecution_AbsentOnSuccess_ReturnsDeviation(t *testing.T) {
	// Build a minimal workflow where the first pre-execution row has no On Success.
	// brownfield-tdd row 0 (codebase-research) has OnSuccess="requirements-refinement",
	// so we construct a minimal table with a column-absent On Success.
	const noHintContent = `## No Hint Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | agent-a | FALSE | - | out.md |
| PLANNING | agent-b | FALSE | out.md | final.md |
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

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    successResponse("agent-a#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// ===== Agent instance ID assignment =====

// TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch verifies that the
// dispatched AgentInstanceID uses seq+1, not seq (FR-11: increment before invocation).
func TestNext_AgentInstanceID_SeqIncrementedBeforeDispatch(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	// seq=5: the next dispatch should be #6.
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             5,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	// quick-fix row 0: planner-tdd-soft has HITL=TRUE (true)
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true), got false")
	}
}

// TestNext_HITL_BothFalse_EffectiveFalse verifies that when both row HITL and
// stage HITL are false, effective HITL is false.
func TestNext_HITL_BothFalse_EffectiveFalse(t *testing.T) {
	// quick-fix row 1: plan-review has HITL=FALSE (false), stage HITL=false
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stagesNoHITL := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesNoHITL,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	// State: plan-review (row 1) just succeeded; next is EXECUTION row.
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-First")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Tests-Only")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := twoStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Should start Stage-2, test group first (TDD) → test-writer-tdd.
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("after Stage-1 last impl-group row, want test-writer-tdd in Stage-2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Test.2" {
		t.Errorf("want Stage=Test.2, got %q", step.Stage)
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
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// implementation-review is row 12, the last EXECUTION row (and last row overall).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#13", domain.StatusSUCCESS, 13)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#13"),
		Agents:          agents,
		Seq:             13,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review")
	// test-writer-tdd is row 0; after it succeeds in Stage-1 (seq=1).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-writer-tdd#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Group-order logic (On Success ignored): within the test group, next is tests-review-tdd.
	// If On Success were consulted, implementation-tdd would be dispatched (wrong).
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("On Success must be ignored inside EXECUTION: want tests-review-tdd (group-order next), got %s (suggests On Success was consulted)",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "1" {
		t.Errorf("want 1, got %q", step.Stage)
	}
}

// ===== Templated artifact path resolution =====

// TestNext_Paths_StageNumber_SubstitutedInInput verifies that {StageNumber} in
// input artifact paths is replaced with the current stage number string.
func TestNext_Paths_StageNumber_SubstitutedInInput(t *testing.T) {
	// quick-fix row 2: implementation-tdd has input "Stage-{StageNumber}/Plan.md".
	// When dispatched in Stage-1, it must resolve to "Stage-1/Plan.md".
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
		{Number: 3, HITL: false, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          threeStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	runStartStages := singleStageSet("Implementation-Only")
	refreshed := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
		{Number: 3, HITL: false, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          runStartStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		RefreshedStages: refreshed,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
		{Number: 1, HITL: false, Approach: "Implementation-Only"},
		{Number: 2, HITL: false, Approach: "Implementation-Only"},
	})
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          runStartStages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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

// TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_ReturnsDispatch verifies
// that in auto-review mode a COMPLETED_NEEDS_ACTION response from a row with
// an unambiguous On Findings hint causes the engine to return a DispatchDecision
// targeting that agent (loop-back), not a Deviation.
func TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 8: build-review (in test group), OnFindings="test-writer-tdd".
	// When build-review returns CNA in auto-review mode, engine dispatches test-writer-tdd.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review is at row 8 (0-indexed) in the test group of brownfield-tdd-build-verified.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("auto-review+CNA+unambiguous OnFindings: want loop-back Dispatch to test-writer-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_Auto_UnambiguousHint_CNA_ReturnsDeviation verifies that
// in auto mode a COMPLETED_NEEDS_ACTION response from a row with an unambiguous
// On Findings hint returns a DeviationDecision, not a Dispatch. The On-Findings
// auto-route fires only in auto-review mode.
func TestNext_OnFindings_Auto_UnambiguousHint_CNA_ReturnsDeviation(t *testing.T) {
	// Same setup as the auto-review variant above; only the mode differs.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch
// verifies that in auto-review mode the On Findings auto-routing works inside
// EXECUTION even though On Success is ignored there.
func TestNext_OnFindings_AutoReview_UnambiguousHint_CNA_InsideExecution_ReturnsDispatch(t *testing.T) {
	// brownfield-tdd-build-verified row 11: second build-review (in impl group), OnFindings="implementation-tdd".
	// When it returns CNA in auto-review mode, engine dispatches implementation-tdd.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// row 11 is build-review in the impl group; OnFindings="implementation-tdd".
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusCOMPLETED_NEEDS_ACTION, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#12"),
		Agents:       agents,
		Seq:          12,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("auto-review+CNA+OnFindings inside EXECUTION: want loop-back to implementation-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_OnFindings_Auto_UnambiguousHint_CNA_InsideExecution_ReturnsDeviation
// verifies that in auto mode a CNA response with an unambiguous On Findings hint
// inside EXECUTION also returns Deviation, not Dispatch.
func TestNext_OnFindings_Auto_UnambiguousHint_CNA_InsideExecution_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#12", domain.StatusCOMPLETED_NEEDS_ACTION, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#12"),
		Agents:       agents,
		Seq:          12,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation verifies that when the
// On Findings column is absent from the routing table, a CNA response yields
// a DeviationDecision.
func TestNext_OnFindings_AbsentColumn_CNA_ReturnsDeviation(t *testing.T) {
	// Construct a minimal workflow without an On Findings column at all.
	const noFindingsContent = `## No Findings Workflow

| Phase | Subagent | HITL | On Success | Input | Output |
|-------|----------|:----:|------------|-------|--------|
| PLANNING | agent-a | FALSE | COMPLETE | - | out.md |
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

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    cnaResponse("agent-a#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings is present but has no value (dash/empty) yields
// a DeviationDecision.
func TestNext_OnFindings_EmptyValue_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 7: test-writer-tdd has OnFindings="-" (no hint value).
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusCOMPLETED_NEEDS_ACTION, 8)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    cnaResponse("test-writer-tdd#8"),
		Agents:          agents,
		Seq:             8,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation verifies that a CNA
// response where On Findings contains free-form qualification yields a
// DeviationDecision (ambiguous hint cannot be auto-routed).
func TestNext_OnFindings_FreeFormHint_CNA_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 10: implementation-review has OnFindings=
	// "implementation-tdd (or other based on issue)" — free-form, not unambiguous.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// implementation-review is row 10 (0-indexed) in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    cnaResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusPARTIALLY_DONE, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    nonSuccessResponse("tests-review-tdd#9", domain.StatusPARTIALLY_DONE),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_Deviation_Kind_IsNonSuccess verifies the DeviationKind in the
// deviation info is set to DeviationNonSuccess for a non-SUCCESS response.
func TestNext_Deviation_Kind_IsNonSuccess(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationNonSuccess {
		t.Errorf("want DeviationKind=%q, got %q", domain.DeviationNonSuccess, dev.Info.Kind)
	}
}

// TestNext_Deviation_IncludesCurrentRowAndPhase verifies that DeviationInfo
// carries the current row index and phase for the resolver's context.
func TestNext_Deviation_IncludesCurrentRowAndPhase(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// tests-review-tdd is row 8 in brownfield-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)
	resp := nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    resp,
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.CurrentRow != 8 {
		t.Errorf("want CurrentRow=8 (tests-review-tdd), got %d", dev.Info.CurrentRow)
	}
	if dev.Info.CurrentPhase != "EXECUTION.Test.[StageNumber]" {
		t.Errorf("want CurrentPhase=EXECUTION.Test.[StageNumber], got %q", dev.Info.CurrentPhase)
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
	stages := singleStageSet("Implementation-First")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := twoStageSet("Tests-Only")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "test-writer-tdd" {
		t.Errorf("Tests-Only after last test-group row: want test-writer-tdd in Stage-2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Test.2" {
		t.Errorf("Tests-Only: want Test.2, got %q", step.Stage)
	}
}

// ===== Build-verified workflow: duplicate agent (build-review appears twice) =====

// TestNext_BuildVerified_TDD_TestGroupSequence verifies the complete test-group
// sequence in brownfield-tdd-build-verified with TDD approach:
// test-writer-tdd → build-review → tests-review-tdd.
func TestNext_BuildVerified_TDD_TestGroupSequence(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
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
			dec := engine.Next(engine.NextInput{
				Workflow:        aw,
				Stages:          stages,
				State:           tc.state,
				LastResponse:    tc.response,
				Agents:          agents,
				Seq:             tc.seq,
				Now:             fixedNow,
				Mode:            domain.ExecutionModeAutoReview,
			})
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
	stages := singleStageSet("TDD")
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
			dec := engine.Next(engine.NextInput{
				Workflow:        aw,
				Stages:          stages,
				State:           tc.state,
				LastResponse:    tc.response,
				Agents:          agents,
				Seq:             tc.seq,
				Now:             fixedNow,
				Mode:            domain.ExecutionModeAutoReview,
			})
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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "plan-review#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// The bare phase name and the plain stage number are recorded, not the
	// routing table's qualified phase string or a "Stage-N" form.
	if step.Phase != "EXECUTION" {
		t.Errorf("want Phase=EXECUTION, got %q", step.Phase)
	}
	if step.Stage != "1" {
		t.Errorf("want Stage=1, got %q", step.Stage)
	}
}

// TestNext_DispatchStep_RowIndex verifies that RowIndex matches the routing table.
func TestNext_DispatchStep_RowIndex(t *testing.T) {
	// quick-fix: row 0 = planner-tdd-soft.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.RowIndex != 0 {
		t.Errorf("want RowIndex=0, got %d", step.RowIndex)
	}
}

// TestNext_DispatchStep_AgentIdentifier verifies that DispatchStep.Agent.Identifier
// matches the routing table's agent field.
func TestNext_DispatchStep_AgentIdentifier(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")

	info, err := engine.ResumePoint(aw, stages, emptyState(), nil)
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
	stages := singleStageSet("Implementation-Only")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state, nil)
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
	stages := singleStageSet("Implementation-Only")

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

	info, err := engine.ResumePoint(aw, stages, state, nil)
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
	stages := singleStageSet("TDD")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8)

	info, err := engine.ResumePoint(aw, stages, state, nil)
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
	stages := singleStageSet("TDD")
	state := stateAfter("RESEARCH", "", "codebase-research#1", domain.StatusSUCCESS, 1)

	info, err := engine.ResumePoint(aw, stages, state, nil)
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
	stages := singleStageSet("TDD")

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

	info, err := engine.ResumePoint(aw, stages, state, nil)
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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	const unresolvableTemplateContent = `## Unresolvable Template Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | COMPLETE | - | Stage-{StageNumber}/Plan.md | out.md |
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

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	const freeFormOnSuccessContent = `## Free-Form On Success Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b (or other based on issue) | - | - | out.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | out.md | final.md |
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

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          nil,
		State:           state,
		LastResponse:    successResponse("agent-a#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("TDD")
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

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("system-design-review#4"),
		Agents:          agents,
		Seq:             4,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Tests-Only")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	stages := singleStageSet("Implementation-First")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("tests-review-tdd#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

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
	const bothHITLTrueContent = `## Both HITL True Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | TRUE | agent-b | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/out.md |
| EXECUTION.[StageNumber] | agent-b | FALSE | COMPLETE | - | Stage-{StageNumber}/out.md | Stage-{StageNumber}/final.md |
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
		{Number: 1, HITL: true, Approach: "Implementation-Only"},
	})
	agents := makeAgents("agent-a", "agent-b")

	// Initial dispatch: no prior log, dispatch agent-a (row 0, HITL=true) in Stage-1 (HITL=true).
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stagesHITL,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-a" {
		t.Fatalf("want agent-a as first dispatch, got %s", step.Request.AgentInstanceID)
	}
	if !step.EffectiveHITL {
		t.Error("want EffectiveHITL=true (row HITL=true AND stage HITL=true), got false")
	}
}

// ===== T4.1: Table-driven group ordering (three or more groups) =====
//
// threeGroupContent is a minimal staged workflow with three named execution groups
// (Alpha, Beta, Gamma) and a four-row approach table. It is used to verify that
// the engine uses the approach table for ordering rather than the natural group
// declaration order or any hardcoded mapping.
//
// Group layout (zero-based row indices in the routing table, no pre-execution rows):
//   Row 0: EXECUTION.Alpha  agent-alpha
//   Row 1: EXECUTION.Beta   agent-beta
//   Row 2: EXECUTION.Gamma  agent-gamma
//
// Approach table:
//   AlphaFirst       → Alpha, Beta, Gamma
//   BetaFirst        → Beta, Alpha, Gamma
//   GammaOnly        → Gamma
//   AlphaBeta        → Alpha, Beta
const threeGroupContent = `## Three-Group Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Alpha.[StageNumber] | agent-alpha | FALSE | - | - | - | - |
| EXECUTION.Beta.[StageNumber] | agent-beta | FALSE | - | - | - | - |
| EXECUTION.Gamma.[StageNumber] | agent-gamma | FALSE | - | - | - | - |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| AlphaFirst | Alpha, Beta, Gamma |
| BetaFirst | Beta, Alpha, Gamma |
| GammaOnly | Gamma |
| AlphaBeta | Alpha, Beta |
`

// TestNext_ThreeGroups_BetaFirst_FirstDispatchIsBeta verifies that when the workflow
// has three named groups and the stage uses the "BetaFirst" approach, the engine
// dispatches agent-beta first (Beta group appears before Alpha in that row).
//
// This test is RED with the current implementation, which uses orderedGroupsForApproach
// and falls back to the natural group order (Alpha first) when TwoGroup=false.
// The new orderedGroupsForStage implementation must look up the approach table to
// honour the row's group sequence.
func TestNext_ThreeGroups_BetaFirst_FirstDispatchIsBeta(t *testing.T) {
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "BetaFirst"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-beta" {
		t.Errorf("BetaFirst approach: want first dispatch to agent-beta (Beta group first in approach row), got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Beta.1" {
		t.Errorf("want Beta.1, got %q", step.Stage)
	}
}

// TestNext_ThreeGroups_GammaOnly_SkipsAlphaAndBeta verifies that with the "GammaOnly"
// approach, only the Gamma group rows are dispatched. Alpha and Beta are omitted.
//
// This test is RED: the current implementation (TwoGroup=false) returns all groups
// and would dispatch agent-alpha first, not agent-gamma.
func TestNext_ThreeGroups_GammaOnly_SkipsAlphaAndBeta(t *testing.T) {
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "GammaOnly"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "agent-gamma" {
		t.Errorf("GammaOnly approach: want first (only) dispatch to agent-gamma, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_ThreeGroups_GammaOnly_AfterGamma_NextStageStartsWithGamma verifies
// that after agent-gamma completes stage 1 under the GammaOnly approach, stage 2
// begins with agent-gamma again (not agent-alpha). With the declared-order
// fallback the single implicit group covers all rows, so stage 2 would start at
// row 0 (agent-alpha); table-driven routing correctly starts at Gamma's row.
//
// RED: the current implementation (TwoGroup=false) uses a single implicit group
// covering all rows, so stage 2 starts with agent-alpha rather than agent-gamma.
func TestNext_ThreeGroups_GammaOnly_AfterGamma_NextStageStartsWithGamma(t *testing.T) {
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	// Two stages with GammaOnly: Gamma is the only dispatched group each stage.
	// After Gamma in stage 1 the engine must begin stage 2 with Gamma, not Alpha.
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "GammaOnly"},
		{Number: 2, HITL: false, Approach: "GammaOnly"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")
	// agent-gamma is the only dispatched row (seq=1) under GammaOnly.
	state := stateAfter("EXECUTION.Gamma.[StageNumber]", "Stage-1", "agent-gamma#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("agent-gamma#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.Stage != "Gamma.2" {
		t.Errorf("GammaOnly: after stage 1 Gamma, want Gamma.2, got %q", step.Stage)
	}
	if agentName(step.Request.AgentInstanceID) != "agent-gamma" {
		t.Errorf("GammaOnly stage 2: want agent-gamma (table-driven), got %s", step.Request.AgentInstanceID)
	}
}

// TestNext_ThreeGroups_AlphaBeta_SkipsGamma verifies that with the "AlphaBeta"
// approach, Gamma is skipped. After agent-alpha and agent-beta complete the stage,
// the engine should advance to the next stage (or complete), not dispatch agent-gamma.
//
// This test is RED: the current implementation (TwoGroup=false) returns all groups
// and would dispatch agent-gamma after agent-beta.
func TestNext_ThreeGroups_AlphaBeta_SkipsGamma(t *testing.T) {
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	// Two stages: stage 1 AlphaBeta, stage 2 AlphaBeta.
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "AlphaBeta"},
		{Number: 2, HITL: false, Approach: "AlphaBeta"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")
	// agent-beta is row 1 (last row in the Alpha+Beta sequence). After it completes
	// in stage 1, the engine should advance to stage 2 Alpha (row 0), not dispatch
	// Gamma (which is skipped by AlphaBeta).
	state := stateAfter("EXECUTION.Beta.[StageNumber]", "Stage-1", "agent-beta#2", domain.StatusSUCCESS, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("agent-beta#2"),
		Agents:          agents,
		Seq:             2,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// Should advance to stage 2, starting with Alpha (first group in AlphaBeta order).
	if agentName(step.Request.AgentInstanceID) != "agent-alpha" {
		t.Errorf("AlphaBeta: after agent-beta in stage 1, want agent-alpha in stage 2, got %s",
			step.Request.AgentInstanceID)
	}
	if step.Stage != "Alpha.2" {
		t.Errorf("AlphaBeta: want Alpha.2 after Alpha+Beta complete in stage 1, got %q", step.Stage)
	}
}

// TestNext_ThreeGroups_BetaFirst_FullSequence verifies that with "BetaFirst"
// approach, all three groups are dispatched in the table-driven order (Beta,
// Alpha, Gamma) — which diverges from the declared row order (Alpha, Beta, Gamma).
// This test fails without table-driven routing because the current fallback runs
// groups in declaration order (Alpha first), whereas the table says Beta first.
//
// RED: the current implementation (TwoGroup=false) uses declared order and
// dispatches agent-alpha first rather than agent-beta.
func TestNext_ThreeGroups_BetaFirst_FullSequence(t *testing.T) {
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "BetaFirst"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")

	// BetaFirst → Beta (row 1), Alpha (row 0), Gamma (row 2): the order diverges
	// from declared row order, so it cannot coincide with the declared-order fallback.
	wantSequence := []string{"agent-beta", "agent-alpha", "agent-gamma"}
	states := []domain.ArtifactState{
		emptyState(),
		stateAfter("EXECUTION.Beta.[StageNumber]", "Stage-1", "agent-beta#1", domain.StatusSUCCESS, 1),
		stateAfter("EXECUTION.Alpha.[StageNumber]", "Stage-1", "agent-alpha#2", domain.StatusSUCCESS, 2),
	}
	responses := []*domain.ProtocolResponse{
		nil,
		successResponse("agent-beta#1"),
		successResponse("agent-alpha#2"),
	}

	for i, want := range wantSequence {
		dec := engine.Next(engine.NextInput{
			Workflow:        aw,
			Stages:          stages,
			State:           states[i],
			LastResponse:    responses[i],
			Agents:          agents,
			Seq:             i,
			Now:             fixedNow,
			Mode:            domain.ExecutionModeAutoReview,
		})
		step := requireDispatch(t, dec)
		if agentName(step.Request.AgentInstanceID) != want {
			t.Errorf("BetaFirst step %d: want %s, got %s", i+1, want, step.Request.AgentInstanceID)
		}
	}
}

// TestNext_ThreeGroups_GroupInEveryApproachRow_RunsInEveryStage verifies that a
// group present in an approach row is dispatched in every stage that uses that
// approach. Both stages use BetaFirst (→ Beta, Alpha, Gamma), so after Gamma
// completes stage 1 the engine must begin stage 2 with Beta, not with Alpha.
// This fails without table-driven routing because the declared-order fallback
// (single implicit group, all rows) always starts the next stage at row 0 (Alpha).
//
// RED: the current implementation uses declared row order and dispatches
// agent-alpha at the start of stage 2 rather than agent-beta.
func TestNext_ThreeGroups_GroupInEveryApproachRow_RunsInEveryStage(t *testing.T) {
	// BetaFirst: Beta (row 1), Alpha (row 0), Gamma (row 2). Gamma is last.
	// After Gamma finishes stage 1 the engine must pick up BetaFirst again for
	// stage 2, dispatching Beta — not Alpha (row 0, which declared-order would use).
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "BetaFirst"},
		{Number: 2, HITL: false, Approach: "BetaFirst"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")

	// Stage 1 BetaFirst: Beta=seq1, Alpha=seq2, Gamma=seq3. After Gamma (#3) the
	// engine advances to stage 2 and must dispatch Beta first.
	state := stateAfter("EXECUTION.Gamma.[StageNumber]", "Stage-1", "agent-gamma#3", domain.StatusSUCCESS, 3)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("agent-gamma#3"),
		Agents:          agents,
		Seq:             3,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.Stage != "Beta.2" {
		t.Errorf("after Gamma in stage 1, want Beta.2, got %q", step.Stage)
	}
	if agentName(step.Request.AgentInstanceID) != "agent-beta" {
		t.Errorf("stage 2 BetaFirst: want agent-beta first (table-driven), got %s",
			step.Request.AgentInstanceID)
	}
}

// ===== T4.2: Unresolvable approach at the routing decision =====
//
// These tests verify that when a stage's Approach value has no matching row in
// the workflow's Execution Groups table, the engine surfaces a StopDecision (or
// error from ResumePoint) rather than falling back to a default order.
// All tests are RED with the current implementation, which falls back to ApproachTDD.

// TestNext_UnresolvableApproach_InitialDispatch_ReturnsStop verifies that when the
// very first EXECUTION dispatch has an approach not in the workflow's table, the
// engine returns a StopDecision naming the unresolvable value.
//
// RED: current implementation falls back to ApproachTDD and dispatches normally.
func TestNext_UnresolvableApproach_InitialDispatch_ReturnsStop(t *testing.T) {
	// brownfield-tdd has an approach table with TDD, Implementation-First,
	// Implementation-Only, Tests-Only. "UnknownApproach" is not in the table.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// Simulate being at contracts-review (row 6, last pre-execution row) → entering EXECUTION.
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("want non-empty StopDecision.Reason for unresolvable approach")
	}
}

// TestNext_UnresolvableApproach_StopReason_NamesApproachValue verifies that the
// StopDecision reason string contains the unresolvable approach token so the user
// can identify and fix the plan artifact.
//
// RED: current implementation produces no stop at all (falls back silently).
func TestNext_UnresolvableApproach_StopReason_NamesApproachValue(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach %q; got %q",
			"UnknownApproach", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_InterStageAdvance_ReturnsStop verifies that an
// unresolvable approach at stage N+1 produces a StopDecision when the engine tries
// to advance from the last row of stage N.
//
// RED: current implementation falls back to ApproachTDD for the missing approach.
func TestNext_UnresolvableApproach_InterStageAdvance_ReturnsStop(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	// Stage 1 uses TDD (valid), stage 2 uses "UnknownApproach" (not in table).
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "UnknownApproach"},
	})
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	// implementation-review (row 10) is the last row of the Implementation group in TDD stage 1.
	// After it completes, the engine should try to advance to stage 2 but find "UnknownApproach" → stop.
	state := stateAfter("EXECUTION.Implementation.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusSUCCESS, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-review#11"),
		Agents:          agents,
		Seq:             11,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach in stage 2; got %q", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_NoFallbackDispatch verifies that a StopDecision is
// returned (not a DispatchDecision) when the approach is unresolvable. This proves
// there is no silent fallback to a default ordering.
//
// RED: current implementation dispatches normally (silently falls back to TDD).
func TestNext_UnresolvableApproach_NoFallbackDispatch(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	// Must be Stop, not Dispatch.
	if dec.Dispatch != nil {
		t.Errorf("unresolvable approach must not produce a DispatchDecision (got agent %s); want StopDecision",
			agentName(dec.Dispatch.Steps[0].Request.AgentInstanceID))
	}
	if dec.Stop == nil {
		t.Error("unresolvable approach must produce a StopDecision")
	}
}

// TestResumePoint_UnresolvableApproach_ReturnsError verifies that ResumePoint
// returns a non-nil error (not a silent -1 row index) when the approach used in
// sequence-arithmetic disambiguation is unresolvable.
//
// The test uses brownfield-tdd-build-verified where build-review appears in both
// the Test and Implementation groups. The sequence-based disambiguation calls
// orderedGroupsForStage which must fail with an error for an unresolvable approach.
//
// RED: the current implementation collapses seq-arithmetic failures to -1, which
// produces a "could not determine current row" stop rather than the approach error.
func TestResumePoint_UnresolvableApproach_ReturnsError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	// "UnknownApproach" is not in brownfield-tdd-build-verified's Execution Groups table.
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})

	// build-review appears at rows 8 and 11. The sequence-based disambiguation
	// uses orderedGroupsForStage, which must fail for "UnknownApproach".
	// GlobalSequence=9 positions build-review as the second EXECUTION dispatch
	// (after test-writer-tdd=seq 8), which triggers seq-based lookup.
	state := domain.ArtifactState{
		GlobalSequence: 9,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#9",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 9, Agent: "build-review#9", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	_, err := engine.ResumePoint(aw, stages, state, nil)

	if err == nil {
		t.Fatal("ResumePoint must return an error when approach is unresolvable during seq disambiguation")
	}
}

// TestResumePoint_UnresolvableApproach_ErrorContainsApproachValue verifies that the
// error returned by ResumePoint wraps or chains an error containing the unresolvable
// approach value for user diagnostics.
//
// RED: the current implementation returns nil error (collapses to -1, then the
// caller sees a "could not determine current row" stop).
func TestResumePoint_UnresolvableApproach_ErrorContainsApproachValue(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})

	state := domain.ArtifactState{
		GlobalSequence: 9,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#9",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 9, Agent: "build-review#9", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	_, err := engine.ResumePoint(aw, stages, state, nil)
	if err == nil {
		t.Fatal("ResumePoint must return an error")
	}

	// The error chain must include *domain.UnresolvableApproachError.
	var uae *domain.UnresolvableApproachError
	if !errors.As(err, &uae) {
		t.Errorf("error must wrap *domain.UnresolvableApproachError; got %T: %v", err, err)
	}
	if uae != nil && !strings.Contains(uae.Error(), "UnknownApproach") {
		t.Errorf("UnresolvableApproachError must name the approach value; got %q", uae.Error())
	}
}

// TestNext_UnresolvableApproach_InitialDispatch_StopViaNext verifies that the
// unresolvable-approach failure surfaces through engine.Next as a StopDecision
// when the initial EXECUTION dispatch itself has an unresolvable approach
// (no pre-execution rows).
//
// RED: current engine falls back to TDD approach silently.
func TestNext_UnresolvableApproach_InitialDispatch_StagedOnly_ReturnsStop(t *testing.T) {
	// threeGroupContent has no pre-execution rows and its stage must have a
	// matching approach. Use "NoSuchApproach" which is absent from the table.
	aw := mustParseAndAdmit(t, threeGroupContent, "three-group", "1.0")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "NoSuchApproach"},
	})
	agents := makeAgents("agent-alpha", "agent-beta", "agent-gamma")

	// Initial dispatch (no prior log) → engine enters EXECUTION immediately.
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "NoSuchApproach") {
		t.Errorf("StopDecision.Reason must name %q; got %q", "NoSuchApproach", stop.Reason)
	}
}

// TestNext_UnresolvableApproach_WinsOverGenericRowStop verifies that when
// findCurrentRowIndex cannot determine the current row because sequence
// arithmetic fails due to an unresolvable approach, the resulting StopDecision
// carries the approach error message — not the generic "could not determine
// current row from artifact state" message. This locks in the ordering
// requirement from the design: the error check precedes the < 0 check in Next.
//
// In brownfield-tdd-build-verified, build-review appears in both the Test and
// Implementation groups. When build-review is the last agent in the execution
// log, the engine cannot determine the current row by agent+phase match alone
// (ambiguous) and falls back to sequence arithmetic via orderedGroupsForStage.
// With an unresolvable approach, that arithmetic fails: findCurrentRowIndex
// returns (-1, err) rather than (-1, nil). Without the ordered check, the engine
// would produce the generic stop; with it, the approach stop wins.
//
// RED: the current implementation collapses sequence-arithmetic failures to
// (-1, nil), so Next produces the generic "could not determine current row"
// stop rather than the approach stop.
func TestNext_UnresolvableApproach_WinsOverGenericRowStop(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "UnknownApproach"},
	})
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review appears in both the Test group (row 8) and the Implementation
	// group (row 11). With seq=9, the engine uses sequence arithmetic to
	// disambiguate — which calls orderedGroupsForStage and fails for
	// "UnknownApproach". The StopDecision must name the approach, not emit the
	// generic "could not determine current row" message.
	state := domain.ArtifactState{
		GlobalSequence: 9,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#9",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
			{Seq: 9, Agent: "build-review#9", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	// The stop reason must name the unresolvable approach value. The generic
	// "could not determine current row" message is the wrong outcome here.
	if !strings.Contains(stop.Reason, "UnknownApproach") {
		t.Errorf("StopDecision.Reason must name the unresolvable approach %q (not emit the generic row-not-found message); got %q",
			"UnknownApproach", stop.Reason)
	}
}

// ===== T4.3: No-groups-declared path =====
//
// These tests verify that when a workflow declares no groups (bare EXECUTION rows,
// no approach table), any Approach value in the StageSet is silently ignored and
// all EXECUTION rows run in declaration order.

// TestNext_NoGroups_UnknownApproachValue_DoesNotStop verifies that when the workflow
// has no groups (GroupsDeclared=false), an approach value not present in any table
// does NOT cause a StopDecision. The approach is simply ignored.
func TestNext_NoGroups_UnknownApproachValue_DoesNotStop(t *testing.T) {
	// implOnlyContent has no groups and no approach table.
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := makeStageSet([]domain.StageEntry{
		// "CompletelyUnknownToken" is not a known approach, but with no groups
		// declared the engine must ignore it rather than stopping.
		{Number: 1, HITL: false, Approach: "CompletelyUnknownToken"},
	})
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	// Fresh start: initial dispatch.
	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	// Must be a dispatch (rows run in order), not a Stop.
	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-tdd" {
		t.Errorf("no-groups workflow: want implementation-tdd (first EXECUTION row), got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_NoGroups_AllExecutionRowsDispatchedInOrder verifies that all EXECUTION
// rows in a bare (no-groups) workflow run in declaration order when the StageSet
// carries an approach value.
func TestNext_NoGroups_AllExecutionRowsDispatchedInOrder(t *testing.T) {
	// implOnlyContent: EXECUTION rows are implementation-tdd (row 0), implementation-review (row 1).
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "SomeOpaqueValue"},
	})
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	// After implementation-tdd (row 0, seq 1) completes, next must be implementation-review (row 1).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("implementation-tdd#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-review" {
		t.Errorf("no-groups: want implementation-review (second EXECUTION row), got %s",
			step.Request.AgentInstanceID)
	}
}

// ===== Position immune to infrastructure activity =====
//
// Coverage:
//
//   Resume after infrastructure activity (T3.2):
//   - A run whose on-disk artifact ends with an infrastructure log entry resumes
//     at the workflow row after the last WORKFLOW step, not the row after the
//     infrastructure entry, and is not misdiagnosed as interrupted.
//   - A genuine mid-flight interruption occurring after infrastructure activity
//     is still detected and the interrupted workflow row is re-dispatched.
//
//   Sequence-based disambiguation with interleaved infrastructure invocations (T3.3):
//   - A repeated agent (occupying more than one EXECUTION row) is still
//     resolved to the correct row when an infrastructure step's own sequence
//     bump has shifted the raw sequence arithmetic.
//   - A single-agent-per-row workflow, where identification never depends on
//     sequence arithmetic, continues to resolve correctly across an interleaved
//     infrastructure invocation.
//
//   Unresolved-position diagnostic (T3.4):
//   - When the recorded agent is neither a workflow participant nor a declared
//     infrastructure agent, the stop names the agent and states that it is not
//     a workflow participant.

func declaredCheckpointInfraAgent() []domain.DeclaredInfraAgent {
	return []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	}
}

// TestResumePoint_TrailingInfrastructureEntry_NotMisdiagnosedAsInterrupted verifies
// that when the execution log ends with an infrastructure entry and current_state
// correctly names the last WORKFLOW step (per the Apply fix), ResumePoint resumes
// at the row after that workflow step rather than treating the trailing
// infrastructure entry as an interruption.
func TestResumePoint_TrailingInfrastructureEntry_NotMisdiagnosedAsInterrupted(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	infra := domain.NewInfraAgentSet(declaredCheckpointInfraAgent())

	state := domain.ArtifactState{
		GlobalSequence: 2,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "checkpoint-manager-git#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, infra)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean stop after infrastructure activity, not an interruption), got true")
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the row after the last workflow step), got %d", info.RowIndex)
	}
}

// TestResumePoint_InterruptionAfterInfrastructureActivity_RerunsWorkflowRow verifies
// that a genuine mid-flight interruption occurring after infrastructure activity is
// still detected: the workflow row that was dispatched but never recorded in
// current_state is re-run, and the interleaved infrastructure entry does not
// obscure the interruption.
func TestResumePoint_InterruptionAfterInfrastructureActivity_RerunsWorkflowRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	infra := domain.NewInfraAgentSet(declaredCheckpointInfraAgent())

	// planner-tdd-soft (row 0) completed and is recorded in current_state.
	// checkpoint-manager-git then ran (does not move current_state). plan-review
	// (row 1) was then dispatched and completed, but the runner was interrupted
	// before current_state could be updated to reflect it.
	state := domain.ArtifactState{
		GlobalSequence: 3,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "planner-tdd-soft#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "planner-tdd-soft#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 2, Agent: "checkpoint-manager-git#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
			{Seq: 3, Agent: "plan-review#3", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, infra)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if !info.RerunLast {
		t.Error("want RerunLast=true (plan-review was dispatched but never recorded), got false")
	}
	if info.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (plan-review, the interrupted row), got %d", info.RowIndex)
	}
}

// TestNext_Interleaved_Infrastructure_SequenceDisambiguation_RepeatedAgent verifies
// AC3.6 for a workflow where one agent (build-review) occupies more than one
// EXECUTION row: an infrastructure step's sequence bump, interleaved between
// two workflow rows, must not throw off sequence-based row disambiguation.
func TestNext_Interleaved_Infrastructure_SequenceDisambiguation_RepeatedAgent(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.1")
	stages := singleStageSet("TDD")
	agents := makeAgents("test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review")

	// Stage-1, TDD approach, test group = [test-writer-tdd, build-review, tests-review-tdd].
	// 7 pre-execution rows consume seq 1-7. test-writer-tdd completes at seq 8.
	// checkpoint-manager-git then fires as an infrastructure step at seq 9 (not a
	// routing row). build-review then completes at seq 10 -- one ahead of where it
	// would land (seq 9) if only workflow rows consumed sequence numbers.
	state := domain.ArtifactState{
		GlobalSequence: 10,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#10",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#10"),
		Agents:          agents,
		Seq:             10,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	got := agentName(step.Request.AgentInstanceID)
	if got != "tests-review-tdd" {
		t.Errorf("want dispatch of tests-review-tdd (the row after build-review in the test group), got %q "+
			"-- sequence-based disambiguation was thrown off by the infrastructure step's sequence bump", got)
	}
}

// TestNext_Interleaved_Infrastructure_SequenceDisambiguation_SingleAgentPerRow verifies
// AC3.6 for a workflow where every agent occupies exactly one row: identification is
// by agent+phase, not sequence arithmetic, so an inflated global_sequence from an
// interleaved infrastructure step must not affect resolution.
func TestNext_Interleaved_Infrastructure_SequenceDisambiguation_SingleAgentPerRow(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	// planner-tdd-soft completes at seq 1. checkpoint-manager-git then fires as an
	// infrastructure step at seq 2 (not a routing row). plan-review then completes
	// at seq 3 -- one ahead of where it would land (seq 2) if only workflow rows
	// consumed sequence numbers.
	state := domain.ArtifactState{
		GlobalSequence: 3,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "plan-review#3",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("plan-review#3"),
		Agents:          agents,
		Seq:             3,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	got := agentName(step.Request.AgentInstanceID)
	if got != "implementation-tdd" {
		t.Errorf("want dispatch of implementation-tdd (On Success target of plan-review), got %q", got)
	}
}

// TestNext_UnresolvedPosition_AgentNotInWorkflow_StopNamesCause verifies AC3.7:
// when the recorded agent is not a workflow participant (and not recognisable as
// an infrastructure entry either), the stop names the agent and states that it is
// not a workflow participant, rather than a generic "could not determine" message.
func TestNext_UnresolvedPosition_AgentNotInWorkflow_StopNamesCause(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	state := domain.ArtifactState{
		GlobalSequence: 1,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			Stage:      "",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "mystery-agent#1",
		},
	}

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("mystery-agent#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	if !strings.Contains(stop.Reason, "mystery-agent") {
		t.Errorf("Stop reason must name the unresolvable agent, got %q", stop.Reason)
	}
	lower := strings.ToLower(stop.Reason)
	if !strings.Contains(lower, "not a") || !strings.Contains(lower, "participant") {
		t.Errorf("Stop reason must state that the agent is not a workflow participant (AC3.7), got %q", stop.Reason)
	}
}

// ===== Canonical phase and stage recording =====
//
// The runner and the LLM orchestrator write the same Orchestration.md: the
// bare phase name in the phase field, and the group-and-stage value in the
// stage field ("Test.1" for a grouped EXECUTION row, "1" for an ungrouped
// one, absent for a non-staged phase). The tests below cover what a
// dispatch step records (AC4.2/AC4.3), that position resolution works
// against that recorded form including disambiguation by group (AC4.4),
// and that a legacy on-disk artifact (qualified phase, "Stage-N" stage)
// remains resumable (AC4.5).

// TestNext_StagedExecution_GroupedRow_RecordsBarePhaseAndGroupQualifiedStage
// verifies that a dispatch into a grouped EXECUTION row (the common case)
// records the bare phase name and the group-qualified stage value, not the
// routing table's qualified phase string or a bare "Stage-N" form.
func TestNext_StagedExecution_GroupedRow_RecordsBarePhaseAndGroupQualifiedStage(t *testing.T) {
	// brownfield-tdd: after contracts-review (row 6), TDD approach dispatches
	// test-writer-tdd (row 7, group "Test") for stage 1.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("DESIGN", "", "contracts-review#7", domain.StatusSUCCESS, 7)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("contracts-review#7"),
		Agents:          agents,
		Seq:             7,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.Phase != "EXECUTION" {
		t.Errorf("DispatchStep.Phase: want bare %q, got %q (the routing table's qualified string must not reach the recorded field)",
			"EXECUTION", step.Phase)
	}
	if step.Stage != "Test.1" {
		t.Errorf("DispatchStep.Stage: want group-qualified %q, got %q", "Test.1", step.Stage)
	}
}

// TestNext_StagedExecution_UngroupedRow_RecordsBarePhaseAndPlainStageNumber
// verifies that a dispatch into an ungrouped staged row records the bare
// phase name and the plain stage number, with no group segment.
func TestNext_StagedExecution_UngroupedRow_RecordsBarePhaseAndPlainStageNumber(t *testing.T) {
	aw := mustParseAndAdmit(t, implOnlyContent, "implementation-only", "3.1")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("implementation-tdd", "implementation-review", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           emptyState(),
		LastResponse:    nil,
		Agents:          agents,
		Seq:             0,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.Phase != "EXECUTION" {
		t.Errorf("DispatchStep.Phase: want bare %q, got %q", "EXECUTION", step.Phase)
	}
	if step.Stage != "1" {
		t.Errorf("DispatchStep.Stage: want plain stage number %q (no group declared), got %q", "1", step.Stage)
	}
}

// TestNext_NonStagedRow_RecordsEmptyStage verifies that a dispatch into a
// non-staged row records an empty stage value.
func TestNext_NonStagedRow_RecordsEmptyStage(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("planner-tdd-soft#1"),
		Agents:          agents,
		Seq:             1,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.Phase != "PLANNING" {
		t.Errorf("DispatchStep.Phase: want %q, got %q", "PLANNING", step.Phase)
	}
	if step.Stage != "" {
		t.Errorf("DispatchStep.Stage: want empty (non-staged phase), got %q", step.Stage)
	}
}

// TestNext_TargetFormatState_RepeatedAgent_Test_ResolvesToTestGroupRow verifies
// that position resolution works against the target recorded format
// (bare phase, group-qualified stage) for an agent that occupies more than
// one EXECUTION row, and that the Test-group row is picked when the
// recorded stage names the Test group.
func TestNext_TargetFormatState_RepeatedAgent_Test_ResolvesToTestGroupRow(t *testing.T) {
	// brownfield-tdd-build-verified: build-review appears at row 8 (Test
	// group) and row 11 (Implementation group). Recorded state names the
	// Test-group row (agent instance "build-review#9", stage "Test.1");
	// the next dispatch must continue within the Test group
	// (tests-review-tdd, row 9), not jump to the Implementation group.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION", "Test.1", "build-review#9", domain.StatusSUCCESS, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#9"),
		Agents:          agents,
		Seq:             9,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("recorded stage %q must resolve build-review to the Test-group row (row 8); want next dispatch tests-review-tdd, got %s",
			"Test.1", step.Request.AgentInstanceID)
	}
}

// TestNext_TargetFormatState_RepeatedAgent_Implementation_ResolvesToImplementationGroupRow
// is the mirror of the above: the same repeated agent, but the recorded
// stage names the Implementation group, and resolution must pick the
// Implementation-group row instead -- proving the group segment is not
// discarded during position resolution.
func TestNext_TargetFormatState_RepeatedAgent_Implementation_ResolvesToImplementationGroupRow(t *testing.T) {
	// build-review at row 11 (Implementation group); recorded stage
	// "Implementation.1" must resolve there, continuing to
	// implementation-review (row 12), not back into the Test group.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION", "Implementation.1", "build-review#12", domain.StatusSUCCESS, 12)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("build-review#12"),
		Agents:          agents,
		Seq:             12,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "implementation-review" {
		t.Errorf("recorded stage %q must resolve build-review to the Implementation-group row (row 11); want next dispatch implementation-review, got %s",
			"Implementation.1", step.Request.AgentInstanceID)
	}
}

// TestResumePoint_TargetFormatState_RepeatedAgent_Test_ResolvesNextRow is the
// ResumePoint counterpart of the Next-level disambiguation tests above,
// exercising findRowForLogEntry against a target-format execution log entry.
func TestResumePoint_TargetFormatState_RepeatedAgent_Test_ResolvesNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 9,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "Test.1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#9",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 9, Agent: "build-review#9", Phase: "EXECUTION", Stage: "Test.1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 9 {
		t.Errorf("want RowIndex=9 (tests-review-tdd, next row in the Test group), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestResumePoint_TargetFormatState_RepeatedAgent_Implementation_ResolvesNextRow
// mirrors the above for the Implementation-group row of the same repeated agent.
func TestResumePoint_TargetFormatState_RepeatedAgent_Implementation_ResolvesNextRow(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 12,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION",
			Stage:      "Implementation.1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "build-review#12",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 12, Agent: "build-review#12", Phase: "EXECUTION", Stage: "Implementation.1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: unexpected error: %v", err)
	}
	if info.RowIndex != 12 {
		t.Errorf("want RowIndex=12 (implementation-review, next row in the Implementation group), got %d", info.RowIndex)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestResumePoint_LegacyArtifact_QualifiedPhaseAndStageN_StillResolvesAndResumes
// pins AC4.5: an artifact written by an earlier runner version, carrying the
// qualified phase and the "Stage-N" stage form on disk, must still resolve
// position correctly and be resumable after this stage's write-path change.
// Reading such an artifact is unaffected by what the runner now writes.
func TestResumePoint_LegacyArtifact_QualifiedPhaseAndStageN_StillResolvesAndResumes(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")

	state := domain.ArtifactState{
		GlobalSequence: 8,
		CurrentState: domain.CurrentState{
			Phase:      "EXECUTION.Test.[StageNumber]",
			Stage:      "Stage-1",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "test-writer-tdd#8",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 8, Agent: "test-writer-tdd#8", Phase: "EXECUTION.Test.[StageNumber]", Stage: "Stage-1", Status: domain.StatusSUCCESS},
		},
	}

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("ResumePoint: legacy artifact must still resolve, got error: %v", err)
	}
	if info.RowIndex != 8 {
		t.Errorf("want RowIndex=8 (tests-review-tdd, next row after legacy test-writer-tdd entry), got %d", info.RowIndex)
	}
	if info.StageNumber != 1 {
		t.Errorf("want StageNumber=1 recovered from legacy %q, got %d", "Stage-1", info.StageNumber)
	}
	if info.RerunLast {
		t.Error("want RerunLast=false (clean completion), got true")
	}
}

// TestNext_LegacyArtifact_QualifiedPhaseAndStageN_ResolvesAndDispatches is the
// Next-level counterpart: a legacy-form CurrentState must still let Next
// determine the current row and route the following dispatch, exactly as it
// did before this stage's write-path change.
func TestNext_LegacyArtifact_QualifiedPhaseAndStageN_ResolvesAndDispatches(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.Test.[StageNumber]", "Stage-1", "test-writer-tdd#8", domain.StatusSUCCESS, 8)

	dec := engine.Next(engine.NextInput{
		Workflow:        aw,
		Stages:          stages,
		State:           state,
		LastResponse:    successResponse("test-writer-tdd#8"),
		Agents:          agents,
		Seq:             8,
		Now:             fixedNow,
		Mode:            domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "tests-review-tdd" {
		t.Errorf("legacy artifact must still resolve position correctly: want next dispatch tests-review-tdd, got %s",
			step.Request.AgentInstanceID)
	}
}

// requireConsult asserts that the decision is a ConsultDecision.
func requireConsult(t *testing.T, dec domain.EngineDecision) domain.ConsultDecision {
	t.Helper()
	if dec.Consult == nil {
		var which string
		switch {
		case dec.Dispatch != nil:
			which = "DispatchDecision"
		case dec.Complete != nil:
			which = "CompleteDecision"
		case dec.Deviation != nil:
			which = fmt.Sprintf("DeviationDecision(kind=%q)", dec.Deviation.Info.Kind)
		case dec.Stop != nil:
			which = fmt.Sprintf("StopDecision(%q)", dec.Stop.Reason)
		default:
			which = "nil decision (all fields nil)"
		}
		t.Fatalf("want ConsultDecision, got %s", which)
	}
	return *dec.Consult
}

// ===== Mode-gated routing (Stage 2) =====

// TestNext_Mode_Auto_Success_RoutesNormally verifies that in auto mode a SUCCESS
// response routes to the On Success target exactly as before.
func TestNext_Mode_Auto_Success_RoutesNormally(t *testing.T) {
	// quick-fix row 0: planner-tdd-soft, OnSuccess="plan-review"
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("auto mode SUCCESS: want On Success route to plan-review, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Mode_AutoReview_Success_RoutesNormally verifies that in auto-review
// mode SUCCESS routing is identical to auto — the auto-review extension applies
// only to CNA with an unambiguous On Findings hint.
func TestNext_Mode_AutoReview_Success_RoutesNormally(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if agentName(step.Request.AgentInstanceID) != "plan-review" {
		t.Errorf("auto-review mode SUCCESS: want On Success route to plan-review, got %s",
			step.Request.AgentInstanceID)
	}
}

// TestNext_Mode_Orchestrated_FirstCall_ReturnsConsultDecision verifies that in
// orchestrated mode the engine returns a ConsultDecision on the very first call
// (before any agent has run), with trigger ConsultTriggerOrchestratedMode.
func TestNext_Mode_Orchestrated_FirstCall_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        emptyState(),
		LastResponse: nil,
		Agents:       agents,
		Seq:          0,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode first call: want trigger ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// First call: CurrentRow must be -1 (no row dispatched yet).
	if consult.CurrentRow != -1 {
		t.Errorf("orchestrated mode first call: want CurrentRow=-1 (no row yet), got %d",
			consult.CurrentRow)
	}
}

// TestNext_Mode_Orchestrated_AfterSuccess_ReturnsConsultDecision verifies that
// in orchestrated mode every call returns a ConsultDecision, not just the first.
// Even a SUCCESS response does not produce a routing decision from the engine.
func TestNext_Mode_Orchestrated_AfterSuccess_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("planner-tdd-soft#1"),
		Agents:       agents,
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode after SUCCESS: want trigger ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// After a PLANNING-phase SUCCESS the current phase is "PLANNING" and stage is "".
	if consult.CurrentPhase != "PLANNING" {
		t.Errorf("orchestrated mode after SUCCESS: want CurrentPhase=%q, got %q",
			"PLANNING", consult.CurrentPhase)
	}
	if consult.CurrentStage != "" {
		t.Errorf("orchestrated mode after SUCCESS: want CurrentStage=%q (empty for PLANNING phase), got %q",
			"", consult.CurrentStage)
	}
}

// TestNext_Mode_Orchestrated_AfterCNA_ReturnsConsultDecision verifies that
// in orchestrated mode even a CNA response does not produce a DeviationDecision
// or a DispatchDecision — it always produces ConsultDecision.
func TestNext_Mode_Orchestrated_AfterCNA_ReturnsConsultDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	// build-review has an unambiguous OnFindings hint. In auto-review this dispatches;
	// in orchestrated it must still consult.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("build-review#9"),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeOrchestrated,
	})

	consult := requireConsult(t, dec)
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("orchestrated mode after CNA: want ConsultTriggerOrchestratedMode, got %q",
			consult.Trigger)
	}
	// After a CNA in EXECUTION the current phase and stage come from the state.
	if consult.CurrentPhase != "EXECUTION.[StageNumber]" {
		t.Errorf("orchestrated mode after CNA: want CurrentPhase=%q, got %q",
			"EXECUTION.[StageNumber]", consult.CurrentPhase)
	}
	if consult.CurrentStage != "Stage-1" {
		t.Errorf("orchestrated mode after CNA: want CurrentStage=%q, got %q",
			"Stage-1", consult.CurrentStage)
	}
}

// TestNext_Mode_Auto_CNA_AmbiguousOnFindings_ReturnsDeviation verifies that in
// auto mode CNA with an ambiguous On Findings hint still yields Deviation (not
// Dispatch). Ambiguous OnFindings must deviate in every mode.
func TestNext_Mode_Auto_CNA_AmbiguousOnFindings_ReturnsDeviation(t *testing.T) {
	// brownfield-tdd row 10: implementation-review has
	// OnFindings="implementation-tdd (or other based on issue)" — ambiguous.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("implementation-review#11"),
		Agents:       agents,
		Seq:          11,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_AutoReview_CNA_AmbiguousOnFindings_ReturnsDeviation verifies
// that in auto-review mode CNA with an ambiguous On Findings hint also yields
// Deviation. The auto-review extension only fires on unambiguous hints.
func TestNext_Mode_AutoReview_CNA_AmbiguousOnFindings_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#11", domain.StatusCOMPLETED_NEEDS_ACTION, 11)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse("implementation-review#11"),
		Agents:       agents,
		Seq:          11,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_Auto_NonCNA_NonSuccess_ReturnsDeviation verifies that in auto
// mode non-CNA non-SUCCESS statuses always yield Deviation regardless of On Findings.
func TestNext_Mode_Auto_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	// tests-review-tdd has OnFindings="test-writer-tdd" — unambiguous — but the
	// status is PARTIALLY_DONE (not CNA), so it must deviate.
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusPARTIALLY_DONE, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: nonSuccessResponse("tests-review-tdd#9", domain.StatusPARTIALLY_DONE),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAuto,
	})

	requireDeviation(t, dec)
}

// TestNext_Mode_AutoReview_NonCNA_NonSuccess_ReturnsDeviation verifies that in
// auto-review mode non-CNA non-SUCCESS statuses also yield Deviation, not Dispatch.
func TestNext_Mode_AutoReview_NonCNA_NonSuccess_ReturnsDeviation(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#9", domain.StatusBLOCKED, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: nonSuccessResponse("tests-review-tdd#9", domain.StatusBLOCKED),
		Agents:       agents,
		Seq:          9,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	requireDeviation(t, dec)
}

// ===== Review artifact injection (Stage 2) =====
//
// On the auto-review auto-route-back (CNA + unambiguous On Findings), the
// reviewing agent's output artifacts (in.LastOutputArtifacts) are appended to
// the dispatched step's Request.InputArtifacts, after the table row's entries,
// without duplicating any path already present.
//
// No injection occurs on SUCCESS-routed dispatches or Deviation decisions.

// TestNext_ArtifactInjection_AutoReview_CNA_InjectsLastOutputArtifacts verifies
// that on the auto-review auto-route-back the reviewing agent's output artifacts
// are present in the dispatched step's InputArtifacts, after the table row's
// own entries.
func TestNext_ArtifactInjection_AutoReview_CNA_InjectsLastOutputArtifacts(t *testing.T) {
	// brownfield-tdd-build-verified row 8: build-review, OnFindings="test-writer-tdd".
	// build-review's own output artifact (LastOutputArtifacts) must be appended
	// to the dispatched test-writer-tdd step's InputArtifacts.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)
	// These are the output artifacts of the build-review step that just ran.
	reviewOutputs := []string{"Stage-1/build-review-tests.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// The dispatched step is test-writer-tdd. Its InputArtifacts must include
	// the build-review output artifact appended after the table row's own entries.
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/build-review-tests.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("auto-review CNA auto-route-back: want review output artifact %q in dispatched step InputArtifacts, got %v",
			"Stage-1/build-review-tests.md", step.Request.InputArtifacts)
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_NoDeduplication_WhenUnique verifies
// that when LastOutputArtifacts contains paths not already in the table row's
// InputArtifacts, all of them are appended.
func TestNext_ArtifactInjection_AutoReview_CNA_NoDeduplication_WhenUnique(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)
	// Two unique review outputs not in the table row's inputs.
	reviewOutputs := []string{"Stage-1/build-review-tests.md", "Stage-1/extra-finding.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range reviewOutputs {
		if !inputSet[expected] {
			t.Errorf("auto-review CNA: want review output %q in dispatched InputArtifacts, got %v",
				expected, step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_DeduplicatesExisting verifies that
// paths already present in the table row's InputArtifacts are not duplicated
// when LastOutputArtifacts contains them too, and that a new unique path from
// LastOutputArtifacts IS injected (proving injection occurred at all).
func TestNext_ArtifactInjection_AutoReview_CNA_DeduplicatesExisting(t *testing.T) {
	// We use the build-review row (row 8) dispatching back to test-writer-tdd.
	// test-writer-tdd's table InputArtifacts are:
	//   Stage-1/Plan.md, ContractsDesign.md, Stage-1/PlanProgress.md
	// LastOutputArtifacts contains two entries:
	//   - "Stage-1/Plan.md": already in the table row — must NOT appear twice.
	//   - "Stage-1/build-review-findings.md": NOT in the table row — must appear
	//     exactly once, proving injection actually ran.
	// This combination tests both dedup (existing path) and injection (unique path)
	// in a single call, so a missing-injection bug would cause the unique path to
	// be absent and the test would fail.
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)
	// Stage-1/Plan.md is already in test-writer-tdd's table InputArtifacts;
	// Stage-1/build-review-findings.md is not.
	reviewOutputs := []string{"Stage-1/Plan.md", "Stage-1/build-review-findings.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)

	// The duplicate path must appear exactly once (dedup contract).
	dupCount := 0
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/Plan.md" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Errorf("auto-review CNA dedup: want Stage-1/Plan.md exactly once in InputArtifacts, got %d occurrences in %v",
			dupCount, step.Request.InputArtifacts)
	}

	// The unique path must be present (injection contract — without this, a
	// completely missing injection implementation would still pass the dedup check).
	foundUnique := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/build-review-findings.md" {
			foundUnique = true
			break
		}
	}
	if !foundUnique {
		t.Errorf("auto-review CNA injection: want unique review output %q present in InputArtifacts (proves injection ran), got %v",
			"Stage-1/build-review-findings.md", step.Request.InputArtifacts)
	}
}

// TestNext_ArtifactInjection_AutoReview_Success_NoInjection verifies that a
// SUCCESS-routed dispatch never carries injected artifacts, even when
// LastOutputArtifacts is non-empty.
func TestNext_ArtifactInjection_AutoReview_Success_NoInjection(t *testing.T) {
	// planner-tdd-soft returns SUCCESS; routes to plan-review.
	// plan-review's table InputArtifacts do not include any "review-output.md".
	// Even with LastOutputArtifacts = ["review-output.md"], it must not appear.
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")
	state := stateAfter("PLANNING", "", "planner-tdd-soft#1", domain.StatusSUCCESS, 1)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        successResponse("planner-tdd-soft#1"),
		LastOutputArtifacts: []string{"review-output.md"},
		Agents:              agents,
		Seq:                 1,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	for _, a := range step.Request.InputArtifacts {
		if a == "review-output.md" {
			t.Errorf("SUCCESS-routed dispatch must not carry injected artifact %q, but found it in InputArtifacts %v",
				"review-output.md", step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_Auto_CNA_NoInjection verifies that in auto mode
// a CNA response does not inject artifacts — it deviates, and Deviation carries
// no injected InputArtifacts.
func TestNext_ArtifactInjection_Auto_CNA_NoInjection(t *testing.T) {
	// In auto mode CNA deviates, so injection is irrelevant. This test also
	// confirms that the deviation decision itself is returned (not a dispatch).
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: []string{"Stage-1/build-review-tests.md"},
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAuto,
	})

	// auto mode CNA deviates — injection never happens on a non-Dispatch decision.
	requireDeviation(t, dec)
}

// TestNext_ArtifactInjection_AutoReview_CNA_InjectsAfterTableEntries verifies
// that injected artifacts are appended AFTER the table row's existing entries,
// not prepended or interleaved.
func TestNext_ArtifactInjection_AutoReview_CNA_InjectsAfterTableEntries(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)
	injectedArtifact := "Stage-1/build-review-tests.md"
	reviewOutputs := []string{injectedArtifact}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	arts := step.Request.InputArtifacts

	// The injected artifact must appear after at least one table-provided artifact.
	injectedIdx := -1
	for i, a := range arts {
		if a == injectedArtifact {
			injectedIdx = i
			break
		}
	}
	if injectedIdx < 0 {
		t.Fatalf("injected artifact %q not found in InputArtifacts %v", injectedArtifact, arts)
	}
	if injectedIdx == 0 && len(arts) > 1 {
		// Injected is first and there are others — check if it's really a table entry
		// by seeing if a known table entry (e.g. Stage-1/Plan.md) comes after it.
		// For test-writer-tdd, table entries are: Stage-1/Plan.md, ContractsDesign.md, Stage-1/PlanProgress.md
		for _, tableEntry := range []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"} {
			for i, a := range arts {
				if a == tableEntry && i > injectedIdx {
					t.Errorf("injected artifact %q (index %d) appears before table entry %q (index %d); injected must come after table entries",
						injectedArtifact, injectedIdx, tableEntry, i)
				}
			}
		}
	}
	// Also verify: all known table entries appear before the injected artifact.
	for _, tableEntry := range []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"} {
		tableIdx := -1
		for i, a := range arts {
			if a == tableEntry {
				tableIdx = i
				break
			}
		}
		if tableIdx >= 0 && tableIdx > injectedIdx {
			t.Errorf("table entry %q (index %d) appears after injected artifact %q (index %d); injected must come last",
				tableEntry, tableIdx, injectedArtifact, injectedIdx)
		}
	}
}

// ===== ExecutionModeUnset edge case (Stage 2) =====

// TestNext_Mode_Unset_ReturnsDeviationAmbiguousRoute verifies that when the
// execution mode is the zero value (ExecutionModeUnset), Next returns a
// DeviationDecision with Kind DeviationAmbiguousRoute rather than silently
// picking a mode or panicking.
func TestNext_Mode_Unset_ReturnsDeviationAmbiguousRoute(t *testing.T) {
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        emptyState(),
		LastResponse: nil,
		Agents:       agents,
		Seq:          0,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeUnset,
	})

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationAmbiguousRoute {
		t.Errorf("ExecutionModeUnset: want DeviationKind=%q, got %q",
			domain.DeviationAmbiguousRoute, dev.Info.Kind)
	}
}

// ===== Nil / empty LastOutputArtifacts on auto-review CNA path (Stage 2) =====

// TestNext_ArtifactInjection_AutoReview_CNA_NilLastOutputArtifacts verifies that
// when LastOutputArtifacts is nil the dispatched step's InputArtifacts match the
// table row exactly — no injection, no panic.
func TestNext_ArtifactInjection_AutoReview_CNA_NilLastOutputArtifacts(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: nil,
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	// With nil LastOutputArtifacts the table row's InputArtifacts must appear
	// and no extra entries should be present beyond what the table supplies.
	tableEntries := []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"}
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range tableEntries {
		if !inputSet[expected] {
			t.Errorf("nil LastOutputArtifacts: table entry %q missing from InputArtifacts %v",
				expected, step.Request.InputArtifacts)
		}
	}
	// No artifact beyond the table entries should be present.
	tableSet := make(map[string]bool)
	for _, e := range tableEntries {
		tableSet[e] = true
	}
	for _, a := range step.Request.InputArtifacts {
		if !tableSet[a] {
			t.Errorf("nil LastOutputArtifacts: unexpected extra artifact %q in InputArtifacts %v (should match table row only)",
				a, step.Request.InputArtifacts)
		}
	}
}

// TestNext_ArtifactInjection_AutoReview_CNA_EmptyLastOutputArtifacts verifies that
// when LastOutputArtifacts is an empty slice the dispatched step's InputArtifacts
// match the table row exactly — same expectation as nil.
func TestNext_ArtifactInjection_AutoReview_CNA_EmptyLastOutputArtifacts(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	stages := singleStageSet("TDD")
	agents := makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "build-review#9", domain.StatusCOMPLETED_NEEDS_ACTION, 9)

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("build-review#9"),
		LastOutputArtifacts: []string{},
		Agents:              agents,
		Seq:                 9,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	tableEntries := []string{"Stage-1/Plan.md", "ContractsDesign.md", "Stage-1/PlanProgress.md"}
	inputSet := make(map[string]bool)
	for _, a := range step.Request.InputArtifacts {
		inputSet[a] = true
	}
	for _, expected := range tableEntries {
		if !inputSet[expected] {
			t.Errorf("empty LastOutputArtifacts: table entry %q missing from InputArtifacts %v",
				expected, step.Request.InputArtifacts)
		}
	}
	tableSet := make(map[string]bool)
	for _, e := range tableEntries {
		tableSet[e] = true
	}
	for _, a := range step.Request.InputArtifacts {
		if !tableSet[a] {
			t.Errorf("empty LastOutputArtifacts: unexpected extra artifact %q in InputArtifacts %v (should match table row only)",
				a, step.Request.InputArtifacts)
		}
	}
}

// ===== D1 fix: RowNotInGroupError when row not in any ordered group =====
//
// computeNextFromExecution used to return a -1 sentinel when the current row
// was not found in any group of the resolved stage, causing an out-of-bounds
// panic in the caller (buildDispatchStep). These tests verify that after the
// fix the engine returns a StopDecision with a diagnostic message instead.
//
// Scenario: Tests-Only approach limits orderedGroupsForStage to the Test group
// only ([7,9) in brownfield-tdd). When the recorded state places an
// Implementation group row (implementation-tdd, row 9) as the current row,
// that row does not appear in any ordered group, triggering the error.

// creatorInjectionContent is a minimal EXECUTION-only workflow used to exercise
// the FR-7/FR-8 creator-artifact injection path. test-writer-tdd's output
// artifact (Stage-{StageNumber}/PlanProgress.md) is intentionally absent from
// its input artifact list so that creator-artifact injection produces a
// genuinely new entry that would not be present without it.
const creatorInjectionContent = `## Creator Injection Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/impl.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

// brownfieldTDDAgents returns a full agent map for brownfield-tdd rows.
func brownfieldTDDAgents() map[string]domain.AgentReference {
	return makeAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
}

// TestNext_RowNotInGroup_ReturnsStopDecision verifies that when the current
// EXECUTION row is not found in any ordered group of the resolved stage,
// handleExecutionSuccess returns a StopDecision rather than panicking via
// an out-of-bounds slice access on the -1 sentinel row index.
//
// Setup: brownfield-tdd with Tests-Only approach (only the Test group is
// ordered for stage 1: rows [7,9)). State records implementation-tdd (row 9)
// as the last completed agent. Row 9 is outside [7,9), so no group contains
// it -- the "row not in group" condition.
func TestNext_RowNotInGroup_ReturnsStopDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	// Tests-Only: orderedGroupsForStage returns only the Test group [7,9).
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	// Must return StopDecision, not panic from buildDispatchStep(-1).
	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("StopDecision.Reason must be non-empty for diagnostics")
	}
}

// TestNext_RowNotInGroup_StopReasonContainsDiagnostics verifies that the
// StopDecision reason produced when the current row is not in any ordered
// group includes the row index, the stage number, and group information so
// the failure is diagnosable without reproducing the run.
func TestNext_RowNotInGroup_StopReasonContainsDiagnostics(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	// implementation-tdd is row 9; stage 1 with Tests-Only has only [7,9).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	// Reason must mention the row index (9), the stage (1), and at least one
	// group so the caller can diagnose the routing failure.
	for _, want := range []string{"9", "1"} {
		if !strings.Contains(stop.Reason, want) {
			t.Errorf("StopDecision.Reason %q does not contain expected diagnostic %q", stop.Reason, want)
		}
	}
}

// TestNext_RowNotInGroup_ErrorIsRowNotInGroupError verifies that the underlying
// error produced by computeNextFromExecution is a *domain.RowNotInGroupError
// whose fields identify the failing row and stage precisely.
//
// Because Next embeds the error message in StopDecision.Reason, we verify
// field-level diagnostics by constructing the expected error and comparing
// its Error() string against the stop reason (equivalent to asserting the
// structured fields without coupling to message format beyond key identifiers).
func TestNext_RowNotInGroup_ErrorIsRowNotInGroupError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	// The domain error message format from RowNotInGroupError.Error() includes
	// the stage number and the row index. Verify both appear in the reason.
	// This also confirms the error type (not a generic string) was used.
	if !strings.Contains(stop.Reason, "stage 1") {
		t.Errorf("StopDecision.Reason %q: expected stage number in RowNotInGroupError format", stop.Reason)
	}
	if !strings.Contains(stop.Reason, "row 9") {
		t.Errorf("StopDecision.Reason %q: expected row index in RowNotInGroupError format", stop.Reason)
	}
}

// ===== FR-1b fix: ResumePoint errors on RowNotInGroupError (non-complete workflow) =====

// TestResumePoint_RowNotInGroup_NonCompleteWorkflow_ReturnsError verifies that
// when computeNextFromExecution returns a *domain.RowNotInGroupError (because
// the current EXECUTION row is not in any ordered group), ResumePoint wraps
// it and returns an error rather than silently treating the situation as
// end-of-run (the pre-fix behavior via the adv.RowIndex < 0 branch).
//
// Before the fix: computeNextFromExecution returns {RowIndex:-1, nil} and
// ResumePoint falls through to the adv.RowIndex < 0 guard at line 308,
// returning a spurious end-of-run ResumeInfo with no error.
// After the fix: computeNextFromExecution returns *RowNotInGroupError and
// ResumePoint wraps it via the existing advErr != nil guard (lines 304-306).
func TestResumePoint_RowNotInGroup_NonCompleteWorkflow_ReturnsError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	// implementation-tdd (row 9) is not in the Test-Only ordered group [7,9).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	_, err := engine.ResumePoint(aw, stages, state, nil)
	if err == nil {
		t.Fatal("want error when EXECUTION row is not in any ordered group of a non-complete workflow, got nil")
	}
	// The error must wrap *domain.RowNotInGroupError so callers can inspect it.
	var rnigErr *domain.RowNotInGroupError
	if !errors.As(err, &rnigErr) {
		t.Errorf("want error wrapping *domain.RowNotInGroupError, got %T: %v", err, err)
	}
}

// TestResumePoint_RowNotInGroup_ErrorMessageContainsDiagnostics verifies that
// the error returned by ResumePoint when a row is not in any ordered group
// contains enough context to diagnose the failure: the row index and stage.
func TestResumePoint_RowNotInGroup_ErrorMessageContainsDiagnostics(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	_, err := engine.ResumePoint(aw, stages, state, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"9", "1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain expected diagnostic %q (row index or stage)", msg, want)
		}
	}
}

// TestResumePoint_CompleteWorkflow_EndOfRunBehaviorUnchanged verifies that
// when the workflow is genuinely complete (computeNextFromExecution returns
// adv.Complete=true), ResumePoint still returns the end-of-run info with no
// error. The D1/FR-1b fix must not disturb this path.
//
// Setup: onSuccessDivergentContent has no post-execution rows. After the last
// EXECUTION row (implementation-review, row 3) completes, computeNextFromExecution
// exhausts all groups, all stages, and all post-execution rows, returning
// {Complete: true}. ResumePoint should return (ResumeInfo{RowIndex=4}, nil).
func TestResumePoint_CompleteWorkflow_EndOfRunBehaviorUnchanged(t *testing.T) {
	aw := mustParseAndAdmit(t, onSuccessDivergentContent, "on-success-divergent", "1.0")
	// No groups declared; any approach works since groups are implicit.
	stages := singleStageSet("Implementation-Only")
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review")
	_ = agents // agents not needed for ResumePoint; declared for clarity
	// implementation-review is the last EXECUTION row (row 3); no post-execution rows.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#4", domain.StatusSUCCESS, 4)

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("want nil error for complete workflow, got: %v", err)
	}
	// RowIndex should be len(rows) = 4, signaling end-of-run.
	wantRowIndex := 4 // len(workflow.Table.Rows) for onSuccessDivergentContent
	if info.RowIndex != wantRowIndex {
		t.Errorf("complete workflow: want RowIndex=%d (end-of-run), got %d", wantRowIndex, info.RowIndex)
	}
	if info.RerunLast {
		t.Error("complete workflow: want RerunLast=false, got true")
	}
}

// ===== FR-7/FR-8: Creator-artifact injection on review loop-back =====
//
// On auto-review COMPLETED_NEEDS_ACTION auto-route-back, the engine must inject
// the target (creator) agent's own previously-produced output artifacts from
// NextInput.ArtifactRegistry into the dispatched step's InputArtifacts,
// alongside the existing review-artifact injection.
//
// The comparison is against step.Request.OutputArtifacts (the resolved, bare
// paths from buildDispatchStep), not against raw row.OutputArtifacts (which
// may contain unresolved template tokens).
//
// creatorInjectionContent: test-writer-tdd's output is Stage-{StageNumber}/PlanProgress.md
// (resolved: Stage-1/PlanProgress.md). Its input list does NOT include this
// artifact, so injection adds a genuinely new entry to the dispatched step.

// TestNext_CreatorArtifactInjection_RegistryPresent_InjectsOutputArtifact
// verifies that when ArtifactRegistry contains an entry matching the target
// row's resolved output artifact path, that path is injected into the
// dispatched step's InputArtifacts.
//
// Scenario: tests-review-tdd returns CNA with OnFindings=test-writer-tdd.
// ArtifactRegistry carries Stage-1/PlanProgress.md (test-writer-tdd's prior
// output, bare path as session would supply after StripRunPrefix). After
// the existing review-artifact injection, the creator output must also appear.
func TestNext_CreatorArtifactInjection_RegistryPresent_InjectsOutputArtifact(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	stages := singleStageSet("TDD")
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	// tests-review-tdd (row 1) completed with CNA; OnFindings=test-writer-tdd.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#5", domain.StatusCOMPLETED_NEEDS_ACTION, 5)

	// Registry entry: test-writer-tdd's prior output, bare path after StripRunPrefix.
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-1/PlanProgress.md", CreatedIn: "Test.1", CreatedBy: "test-writer-tdd#1"},
	}
	// Review agent's last output artifacts (injected by existing review logic).
	reviewOutputs := []string{"Stage-1/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#5"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 5,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    registry,
	})

	step := requireDispatch(t, dec)
	// The creator's prior output artifact must appear in the dispatched
	// test-writer-tdd step's InputArtifacts (it is not a table row entry,
	// so deduplication does not suppress it).
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("creator-artifact injection: want %q in dispatched InputArtifacts %v",
			"Stage-1/PlanProgress.md", step.Request.InputArtifacts)
	}
}

// TestNext_CreatorArtifactInjection_RegistryAbsent_NoInjection verifies that
// when ArtifactRegistry is nil (first invocation of the creator agent), no
// creator-artifact injection occurs -- the dispatched step's InputArtifacts
// contain only the table row entries and the review agent's LastOutputArtifacts.
func TestNext_CreatorArtifactInjection_RegistryAbsent_NoInjection(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	stages := singleStageSet("TDD")
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "tests-review-tdd#5", domain.StatusCOMPLETED_NEEDS_ACTION, 5)
	reviewOutputs := []string{"Stage-1/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#5"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 5,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    nil, // first invocation: no prior outputs in registry
	})

	step := requireDispatch(t, dec)
	// Stage-1/PlanProgress.md is NOT in the table input for test-writer-tdd
	// and is NOT in reviewOutputs. With no registry, it must not appear.
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-1/PlanProgress.md" {
			t.Errorf("creator-artifact injection: no registry supplied, but %q appeared in InputArtifacts %v (spurious injection)",
				"Stage-1/PlanProgress.md", step.Request.InputArtifacts)
		}
	}
}

// TestNext_CreatorArtifactInjection_TemplatedOutput_MatchesResolvedPath verifies
// that creator-artifact injection fires correctly when the target row's declared
// output artifact contains a {StageNumber} template token. The comparison must
// use the resolved path (Stage-1/PlanProgress.md) rather than the raw template
// (Stage-{StageNumber}/PlanProgress.md), so a registry entry with the bare
// resolved path triggers injection.
func TestNext_CreatorArtifactInjection_TemplatedOutput_MatchesResolvedPath(t *testing.T) {
	aw := mustParseAndAdmit(t, creatorInjectionContent, "creator-injection", "1.0")
	// Stage 2: resolved output would be Stage-2/PlanProgress.md.
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
	})
	agents := makeAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd")
	// State: tests-review-tdd completed for stage 2 (CNA).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-2", "tests-review-tdd#7", domain.StatusCOMPLETED_NEEDS_ACTION, 7)
	// Registry carries the bare resolved path for stage 2 (as session provides
	// after StripRunPrefix -- the resolved path, not the raw template).
	registry := []domain.ArtifactRegistryEntry{
		{Artifact: "Stage-2/PlanProgress.md", CreatedIn: "Test.2", CreatedBy: "test-writer-tdd#5"},
	}
	reviewOutputs := []string{"Stage-2/tests-review-tdd.md"}

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              stages,
		State:               state,
		LastResponse:        cnaResponse("tests-review-tdd#7"),
		LastOutputArtifacts: reviewOutputs,
		Agents:              agents,
		Seq:                 7,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
		ArtifactRegistry:    registry,
	})

	step := requireDispatch(t, dec)
	// Stage-2/PlanProgress.md must be injected (resolved path match).
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == "Stage-2/PlanProgress.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("templated-output creator injection: want %q in dispatched InputArtifacts %v",
			"Stage-2/PlanProgress.md", step.Request.InputArtifacts)
	}
}

// ===== ResolveArtifacts export: codify existing behavior (T1.4) =====
//
// These tests call engine.ResolveArtifacts (the newly exported name) to
// codify the existing template-expansion behavior so that the rename
// (I1.6) does not regress it. Tests are placed in the engine_test package
// (black-box) to verify the exported name is accessible from outside the
// engine package.

// TestResolveArtifacts_StageNumber_Substitution_InputArtifact verifies that
// {StageNumber} in an input artifact path is replaced with the current stage
// number when a stage context is present.
func TestResolveArtifacts_StageNumber_Substitution_InputArtifact(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/Plan.md", "ContractsDesign.md"}
	stages := singleStageSet("TDD")
	got, err := engine.ResolveArtifacts(arts, 2, "Stage-2", stages, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-2/Plan.md", "ContractsDesign.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageNumber_Substitution_OutputArtifact verifies that
// {StageNumber} in an output artifact path is also replaced with the current
// stage number (same substitution rule for both directions).
func TestResolveArtifacts_StageNumber_Substitution_OutputArtifact(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/PlanProgress.md"}
	stages := singleStageSet("TDD")
	got, err := engine.ResolveArtifacts(arts, 3, "Stage-3", stages, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-3/PlanProgress.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageWildcard_Input_ExpandedPerStage verifies that
// Stage-* in an input artifact path is expanded to one path per stage in
// the effective stage set.
func TestResolveArtifacts_StageWildcard_Input_ExpandedPerStage(t *testing.T) {
	arts := []string{"Stage-*/Plan.md"}
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
		{Number: 3, HITL: false, Approach: "TDD"},
	})
	// Non-EXECUTION context (stageNum=0, stageStr="") uses refreshedStages for expansion.
	got, err := engine.ResolveArtifacts(arts, 0, "", stages, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-1/Plan.md", "Stage-2/Plan.md", "Stage-3/Plan.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_StageWildcard_Output_PassThrough verifies that Stage-*
// in an output artifact path is passed through unexpanded (output artifacts
// use the wildcard as a literal declaration, not a glob expansion).
func TestResolveArtifacts_StageWildcard_Output_PassThrough(t *testing.T) {
	arts := []string{"Stage-*/PlanProgress.md"}
	stages := makeStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: "TDD"},
		{Number: 2, HITL: false, Approach: "TDD"},
	})
	got, err := engine.ResolveArtifacts(arts, 1, "Stage-1", stages, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"Stage-*/PlanProgress.md"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestResolveArtifacts_UnresolvablePlaceholder_ReturnsError verifies that
// {StageNumber} in an artifact path without a stage context (stageNum=0,
// stageStr="") returns an error rather than leaving the placeholder literal.
func TestResolveArtifacts_UnresolvablePlaceholder_ReturnsError(t *testing.T) {
	arts := []string{"Stage-{StageNumber}/Plan.md"}
	stages := singleStageSet("TDD")
	// No stage context: stageNum=0, stageStr="" (non-EXECUTION row).
	_, err := engine.ResolveArtifacts(arts, 0, "", stages, nil, true)
	if err == nil {
		t.Fatal("want error for {StageNumber} without stage context, got nil")
	}
	if !strings.Contains(err.Error(), "{StageNumber}") {
		t.Errorf("error %q: want mention of the unresolvable placeholder {StageNumber}", err.Error())
	}
}

// stringSlicesEqual returns true when a and b have the same length and equal
// elements in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
