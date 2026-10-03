package engine_test

// On Findings route-back and non-EXECUTION On Success targets are resolved by
// position when an agent fills several routing-table rows:
//
//   - On Findings goes to the nearest row strictly above the row that ran whose
//     agent is the target. Group and stage boundaries are ignored; a row below
//     is never chosen. No such row is the no-target deviation.
//   - A non-EXECUTION On Success naming a duplicated agent goes to the nearest
//     matching non-EXECUTION row below the current row, or the nearest above
//     when none is below.

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// ===== On Findings: nearest preceding row =====

// twoGroupFindingsContent has the same fixer in the Test group (row 1) and in
// the Implementation group (row 3). Each group ends with its own gate, whose
// On Findings names the fixer.
const twoGroupFindingsContent = `## Two Group Findings Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | fixer | FALSE | - | - | Script/fix-tests.md | Stage-{StageNumber}/FixTests.md |
| EXECUTION.Test.[StageNumber] | gate-tests | FALSE | - | fixer | Script/gate-tests.md | Stage-{StageNumber}/GateTests.md |
| EXECUTION.Implementation.[StageNumber] | fixer | FALSE | - | - | Script/fix-impl.md | Stage-{StageNumber}/FixImpl.md |
| EXECUTION.Implementation.[StageNumber] | gate-impl | FALSE | - | fixer | Script/gate-impl.md | Stage-{StageNumber}/GateImpl.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

const (
	tgRowFixTests = 1
	tgRowGateTest = 2
	tgRowFixImpl  = 3
	tgRowGateImpl = 4
)

func twoGroupAgents() map[string]domain.AgentReference {
	return newTestAgents("fixer", "gate-tests", "gate-impl")
}

// twoGroupLogAtImplGate is the run up to the Implementation-group gate
// returning findings.
func twoGroupLogAtImplGate() *runLog {
	log := &runLog{}
	return log.
		workflowStep("fixer", "Test.1", domain.StatusSUCCESS, tgRowFixTests).
		workflowStep("gate-tests", "Test.1", domain.StatusSUCCESS, tgRowGateTest).
		workflowStep("fixer", "Implementation.1", domain.StatusSUCCESS, tgRowFixImpl).
		workflowStep("gate-impl", "Implementation.1", domain.StatusCOMPLETED_NEEDS_ACTION, tgRowGateImpl)
}

func TestNext_OnFindings_TargetInTwoGroups_RoutesToNearestPrecedingRow(t *testing.T) {
	aw := mustParseAndAdmit(t, twoGroupFindingsContent, "two-group-findings", "1.0")

	step := requireNextStep(t, aw, singleStageSet("TDD"), twoGroupAgents(), twoGroupLogAtImplGate())

	if got := agentName(step.Request.AgentInstanceID); got != "fixer" {
		t.Errorf("findings must route to fixer, got %s", got)
	}
	requireStepAt(t, step, tgRowFixImpl-1, "Implementation.1")
}

func TestNext_OnFindings_TargetInTwoGroups_TestGateStillRoutesToTestGroupRow(t *testing.T) {
	aw := mustParseAndAdmit(t, twoGroupFindingsContent, "two-group-findings", "1.0")
	log := &runLog{}
	log.workflowStep("fixer", "Test.1", domain.StatusSUCCESS, tgRowFixTests).
		workflowStep("gate-tests", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, tgRowGateTest)

	step := requireNextStep(t, aw, singleStageSet("TDD"), twoGroupAgents(), log)

	requireStepAt(t, step, tgRowFixTests-1, "Test.1")
}

func TestNext_OnFindings_NearestPrecedingRoute_InjectsReviewOutputs(t *testing.T) {
	aw := mustParseAndAdmit(t, twoGroupFindingsContent, "two-group-findings", "1.0")
	log := twoGroupLogAtImplGate()
	reviewOutput := "Stage-1/GateImpl.md"

	dec := engine.Next(engine.NextInput{
		Workflow:            aw,
		Stages:              singleStageSet("TDD"),
		State:               log.state(),
		LastResponse:        cnaResponse(log.last().Agent),
		LastOutputArtifacts: []string{reviewOutput},
		Agents:              twoGroupAgents(),
		Seq:                 log.last().Seq,
		Now:                 fixedNow,
		Mode:                domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	requireStepAt(t, step, tgRowFixImpl-1, "Implementation.1")
	found := false
	for _, a := range step.Request.InputArtifacts {
		if a == reviewOutput {
			found = true
		}
	}
	if !found {
		t.Errorf("want review output %q among inputs, got %v", reviewOutput, step.Request.InputArtifacts)
	}
}

func TestNext_OnFindings_NearestPrecedingRoute_ReviewLoopLimitStillApplies(t *testing.T) {
	aw := mustParseAndAdmit(t, twoGroupFindingsContent, "two-group-findings", "1.0")
	log := twoGroupLogAtImplGate()
	state := log.state()
	state.ReviewLoopLimit = 1

	dec := engineNextAuto(aw, singleStageSet("TDD"), state, cnaResponse(log.last().Agent))

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationReviewLoopLimit {
		t.Errorf("want review loop limit deviation, got %q", dev.Info.Kind)
	}
}

func TestNext_OnFindings_SameAgentWriterGateReview_RoutesToWriterDirectlyAbove(t *testing.T) {
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	log := &runLog{}
	log.workflowStep("loop-agent", "Test.1", domain.StatusSUCCESS, loopRowWriter).
		workflowStep("loop-agent", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate)

	step := requireNextStep(t, aw, singleStageSet("TDD"), newTestAgents("loop-agent"), log)

	requireStepAt(t, step, loopRowWriter-1, "Test.1")
}

// targetBelowContent has the On Findings target only below the reviewer.
const targetBelowContent = `## Target Below Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | checker | FALSE | - | fixer | Plan.md | check.md |
| DESIGN | fixer | FALSE | COMPLETE | - | Plan.md | Plan.md |
`

// targetAbsentContent names an On Findings target that has no row at all.
const targetAbsentContent = `## Target Absent Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | checker | FALSE | COMPLETE | fixer | Plan.md | check.md |
| DESIGN | other | FALSE | COMPLETE | - | Plan.md | Plan.md |
`

func nextAfterCheckerFindings(t *testing.T, content string) domain.EngineDecision {
	t.Helper()
	aw := mustParseAndAdmit(t, content, "no-preceding-target", "1.0")
	state := stateAfter("PLANNING", "", "checker#1", domain.StatusCOMPLETED_NEEDS_ACTION, 1)
	return engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       singleStageSet("TDD"),
		State:        state,
		LastResponse: cnaResponse("checker#1"),
		Agents:       newTestAgents("checker", "fixer", "other"),
		Seq:          1,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
}

func TestNext_OnFindings_TargetOnlyBelow_ReturnsDeviation(t *testing.T) {
	dec := nextAfterCheckerFindings(t, targetBelowContent)

	requireDeviation(t, dec)
}

func TestNext_OnFindings_TargetAbsent_ReturnsDeviation(t *testing.T) {
	dec := nextAfterCheckerFindings(t, targetAbsentContent)

	requireDeviation(t, dec)
}

func TestNext_OnFindings_TargetAboveAndBelow_NeverChoosesRowBelow(t *testing.T) {
	const content = `## Target Above And Below Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | fixer | FALSE | checker | - | Plan.md | Plan.md |
| PLANNING | checker | FALSE | - | fixer | Plan.md | check.md |
| DESIGN | fixer | FALSE | COMPLETE | - | Plan.md | Plan.md |
`
	aw := mustParseAndAdmit(t, content, "above-and-below", "1.0")
	state := stateAfter("PLANNING", "", "checker#2", domain.StatusCOMPLETED_NEEDS_ACTION, 2)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       singleStageSet("TDD"),
		State:        state,
		LastResponse: cnaResponse("checker#2"),
		Agents:       newTestAgents("checker", "fixer"),
		Seq:          2,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	step := requireDispatch(t, dec)
	if step.RowIndex != 0 {
		t.Errorf("want the fixer row above (index 0), got index %d", step.RowIndex)
	}
}

// ===== Non-EXECUTION On Success with a duplicated agent =====

// nextAfterSuccess calls engine.Next for a SUCCESS of agentID in phase.
func nextAfterSuccess(t *testing.T, content, phase, agentID string, seq int, agents ...string) domain.DispatchStep {
	t.Helper()
	aw := mustParseAndAdmit(t, content, "on-success-lookup", "1.0")
	state := stateAfter(phase, "", agentID, domain.StatusSUCCESS, seq)
	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       singleStageSet("TDD"),
		State:        state,
		LastResponse: successResponse(agentID),
		Agents:       newTestAgents(agents...),
		Seq:          seq,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
	return requireDispatch(t, dec)
}

func TestNext_NonExecutionOnSuccess_DuplicatedAgent_DispatchesNearestRowBelow(t *testing.T) {
	const content = `## Duplicate Below Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | checker | FALSE | - | - | Plan.md | a.md |
| PLANNING | drafter | FALSE | checker | - | Plan.md | b.md |
| REVIEW | checker | FALSE | COMPLETE | - | Plan.md | c.md |
`

	step := nextAfterSuccess(t, content, "PLANNING", "drafter#1", 1, "checker", "drafter")

	if step.RowIndex != 2 {
		t.Errorf("want the checker row below the drafter (index 2), got index %d", step.RowIndex)
	}
}

func TestNext_NonExecutionOnSuccess_DuplicatedAgentOnlyAbove_DispatchesNearestRowAbove(t *testing.T) {
	const content = `## Duplicate Above Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | checker | FALSE | - | - | Plan.md | a.md |
| PLANNING | checker | FALSE | - | - | Plan.md | b.md |
| REVIEW | drafter | FALSE | checker | - | Plan.md | c.md |
`

	step := nextAfterSuccess(t, content, "REVIEW", "drafter#1", 1, "checker", "drafter")

	if step.RowIndex != 1 {
		t.Errorf("want the nearest checker row above (index 1), got index %d", step.RowIndex)
	}
}

func TestNext_NonExecutionOnSuccess_UniqueAgents_DispatchesNamedRow(t *testing.T) {
	step := nextAfterSuccess(t, quickFixContent, "PLANNING", "planner-tdd-soft#1", 1,
		"planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner")

	if got := agentName(step.Request.AgentInstanceID); got != "plan-review" || step.RowIndex != 1 {
		t.Errorf("want plan-review at index 1, got %s at index %d", got, step.RowIndex)
	}
}

// ===== Full sequence of the staged findings loop =====

// stagedLoopStep is one step of the loop run and what must be dispatched next.
type stagedLoopStep struct {
	name      string
	stage     string
	status    domain.StatusCode
	row       int
	wantIndex int
	wantStage string
}

// runStagedLoop replays the steps against the staged findings loop workflow,
// checking the next dispatch after each. The last step has no further dispatch
// when wantComplete is set.
func runStagedLoop(t *testing.T, stages *domain.StageSet, steps []stagedLoopStep, wantComplete bool) {
	t.Helper()
	aw := mustParseAndAdmit(t, stagedFindingsLoopContent, "staged-findings-loop", "1.0")
	agents := newTestAgents("loop-agent")
	log := &runLog{}
	for i, s := range steps {
		log.workflowStep("loop-agent", s.stage, s.status, s.row)
		snapshot := &runLog{entries: append([]domain.ExecutionLogEntry(nil), log.entries...)}
		last := i == len(steps)-1

		t.Run(s.name, func(t *testing.T) {
			dec := nextAfterLog(aw, stages, agents, snapshot)
			if last && wantComplete {
				requireComplete(t, dec)
				return
			}
			requireStepAt(t, requireDispatch(t, dec), s.wantIndex, s.wantStage)
		})
	}
}

func TestNext_StagedFindingsLoop_SingleStage_RunsAllSixStepsInOrder(t *testing.T) {
	steps := []stagedLoopStep{
		{"writer", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"gate needs action", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate, loopRowWriter - 1, "Test.1"},
		{"writer again", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"gate succeeds", "Test.1", domain.StatusSUCCESS, loopRowGate, loopRowReview - 1, "Test.1"},
		{"review", "Test.1", domain.StatusSUCCESS, loopRowReview, loopRowImpl - 1, "Implementation.1"},
		{"impl", "Implementation.1", domain.StatusSUCCESS, loopRowImpl, 0, ""},
	}

	runStagedLoop(t, singleStageSet("TDD"), steps, true)
}

func TestNext_StagedFindingsLoop_RouteBackInStageOne_StageTwoRunsEveryRow(t *testing.T) {
	steps := []stagedLoopStep{
		{"stage 1 writer", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"stage 1 gate needs action", "Test.1", domain.StatusCOMPLETED_NEEDS_ACTION, loopRowGate, loopRowWriter - 1, "Test.1"},
		{"stage 1 writer again", "Test.1", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.1"},
		{"stage 1 gate succeeds", "Test.1", domain.StatusSUCCESS, loopRowGate, loopRowReview - 1, "Test.1"},
		{"stage 1 review", "Test.1", domain.StatusSUCCESS, loopRowReview, loopRowImpl - 1, "Implementation.1"},
		{"stage 1 impl", "Implementation.1", domain.StatusSUCCESS, loopRowImpl, loopRowWriter - 1, "Test.2"},
		{"stage 2 writer", "Test.2", domain.StatusSUCCESS, loopRowWriter, loopRowGate - 1, "Test.2"},
		{"stage 2 gate", "Test.2", domain.StatusSUCCESS, loopRowGate, loopRowReview - 1, "Test.2"},
		{"stage 2 review", "Test.2", domain.StatusSUCCESS, loopRowReview, loopRowImpl - 1, "Implementation.2"},
		{"stage 2 impl", "Implementation.2", domain.StatusSUCCESS, loopRowImpl, 0, ""},
	}

	runStagedLoop(t, twoStageSet("TDD"), steps, true)
}
