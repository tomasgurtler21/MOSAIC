package engine_test

// Tests for engine.Next and engine.ResumePoint.
//
// Coverage:
//
//   "next" keyword routing (handleNonExecutionSuccess):
//   - On Success = "next" → dispatches next row by index (not by agent-name lookup).
//   - "next" past the last row → CompleteDecision (same as "COMPLETE").
//   - "Next" and "NEXT" → same as "next" (case-insensitive).
//   - "next" into a staged EXECUTION row (HasStagedPhase true) with stage set → full stage/group resolution.
//   - "next" into a staged EXECUTION row with nil stage set → StopDecision (consistent with named-agent behavior).
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
	"fmt"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
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

// newTestStageSet constructs a StageSet from a list of (number, hitl, approach) tuples.
func newTestStageSet(entries []domain.StageEntry) *domain.StageSet {
	ss := &domain.StageSet{Entries: entries}
	return ss
}

// singleStage returns a StageSet with one stage having the given approach.
func singleStageSet(approach domain.Approach) *domain.StageSet {
	return newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: approach},
	})
}

// twoStageSet returns a two-stage StageSet, both with the given approach.
func twoStageSet(approach domain.Approach) *domain.StageSet {
	return newTestStageSet([]domain.StageEntry{
		{Number: 1, HITL: false, Approach: approach},
		{Number: 2, HITL: false, Approach: approach},
	})
}

// newTestAgents returns a minimal agent reference map for the given identifiers.
func newTestAgents(ids ...string) map[string]domain.AgentReference {
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

