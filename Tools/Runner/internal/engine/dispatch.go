package engine

import (
	"fmt"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// initialDispatch returns the first dispatch when no prior invocations exist.
func initialDispatch(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
	stageSource domain.StageSource,
) domain.EngineDecision {
	if workflow.PreExecutionEndRow > workflow.PreExecutionStartRow {
		// Pre-execution rows exist: dispatch the first one.
		step, err := buildDispatchStep(workflow, stages, workflow.PreExecutionStartRow, 0, "", agents, seq, now, nil)
		if err != nil {
			return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
		}
		return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
	}

	if !workflow.HasStagedPhase {
		// Non-staged workflow with no pre-execution rows (unusual but handle it).
		if len(workflow.Table.Rows) == 0 {
			return domain.EngineDecision{Stop: &domain.StopDecision{Reason: "workflow has no rows"}}
		}
		step, err := buildDispatchStep(workflow, stages, 0, 0, "", agents, seq, now, nil)
		if err != nil {
			return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
		}
		return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
	}

	// Staged workflow with no pre-execution rows: dispatch first EXECUTION row of stage 1.
	if stages == nil || len(stages.Entries) == 0 {
		return domain.EngineDecision{Stop: &domain.StopDecision{
			Reason: noStageSetReason("no stage set available for staged workflow", stageSource),
		}}
	}
	stage1 := stages.Entries[0]
	ordGroups, ordErr := orderedGroupsForStage(workflow, stages, stage1.Number)
	if ordErr != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: ordErr.Error()}}
	}
	if len(ordGroups) == 0 {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: "no active execution groups for stage 1"}}
	}
	firstRow := ordGroups[0].StartRow
	stageStr := fmt.Sprintf("Stage-%d", stage1.Number)
	step, err := buildDispatchStep(workflow, stages, firstRow, stage1.Number, stageStr, agents, seq, now, nil)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
	}
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
}

// handleNonExecutionSuccess routes after a non-EXECUTION row returns SUCCESS.
// Outside EXECUTION, the On Success column determines the next step.
//
// A named target agent is resolved relative to the row that just ran, so the
// designated row is dispatched even when the agent fills several rows: the
// nearest non-EXECUTION row after the current row, else the nearest
// non-EXECUTION row before it, else the same search over any row. The `next`
// keyword advances by position, and entry into EXECUTION is approach-driven.
func handleNonExecutionSuccess(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	currentRowIdx int,
	currentRow domain.RoutingRow,
	state domain.ArtifactState,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
	refreshedStages *domain.StageSet,
	stageSource domain.StageSource,
) domain.EngineDecision {

	hint := currentRow.OnSuccess
	if !isUnambiguousHint(hint) {
		return domain.EngineDecision{Deviation: &domain.DeviationDecision{
			Info: domain.DeviationInfo{
				Kind:          domain.DeviationAmbiguousRoute,
				CurrentRow:    currentRowIdx,
				CurrentPhase:  currentRow.Phase,
				CurrentStage:  state.CurrentState.Stage,
				ArtifactState: state,
			},
		}}
	}

	if strings.EqualFold(hint.Value, "COMPLETE") {
		return domain.EngineDecision{Complete: &domain.CompleteDecision{
			FinalState: state.CurrentState,
		}}
	}

	// "next" is a position-based reserved keyword: advance to currentRowIdx+1
	// instead of doing agent-name lookup. This fixes the latent same-agent
	// infinite-loop bug where consecutive rows with the same agent name would
	// route back to the same row indefinitely.
	targetAgent := ""
	targetInExecution := false

	if strings.EqualFold(hint.Value, "next") {
		nextIdx := currentRowIdx + 1
		if nextIdx >= len(workflow.Table.Rows) {
			// Past the last row: same as "COMPLETE".
			return domain.EngineDecision{Complete: &domain.CompleteDecision{
				FinalState: state.CurrentState,
			}}
		}
		if workflow.Table.Rows[nextIdx].PhaseParsed.IsStaged && workflow.HasStagedPhase {
			// Next row is a staged EXECUTION row: enter the EXECUTION code path.
			targetInExecution = true
		} else {
			// Next row is a non-EXECUTION row (or staged without HasStagedPhase):
			// dispatch by index. Pass refreshedStages matching the existing
			// non-EXECUTION path.
			step, err := buildDispatchStep(workflow, stages, nextIdx, 0, "", agents, seq, now, refreshedStages)
			if err != nil {
				return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
			}
			return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
		}
	} else {
		targetAgent = hint.Value

		// Check whether the target agent lives in an EXECUTION row.
		// If so, we are entering the EXECUTION phase → apply approach-driven ordering
		// to determine the actual first row to dispatch (ignoring the specific agent
		// named in On Success, which reflects the default TDD ordering).
		for _, row := range workflow.Table.Rows {
			if row.Agent == targetAgent && row.PhaseParsed.IsStaged {
				targetInExecution = true
				break
			}
		}
	}

	if targetInExecution && workflow.HasStagedPhase {
		if stages == nil || len(stages.Entries) == 0 {
			return domain.EngineDecision{Stop: &domain.StopDecision{
				Reason: noStageSetReason("entering EXECUTION phase but no stage set is available", stageSource),
			}}
		}
		stage1 := stages.Entries[0]
		ordGroups, ordErr := orderedGroupsForStage(workflow, stages, stage1.Number)
		if ordErr != nil {
			return domain.EngineDecision{Stop: &domain.StopDecision{Reason: ordErr.Error()}}
		}
		if len(ordGroups) == 0 {
			return domain.EngineDecision{Stop: &domain.StopDecision{
				Reason: "no active execution groups for stage 1",
			}}
		}
		firstRow := ordGroups[0].StartRow
		stageStr := fmt.Sprintf("Stage-%d", stage1.Number)
		step, err := buildDispatchStep(workflow, stages, firstRow, stage1.Number, stageStr,
			agents, seq, now, nil)
		if err != nil {
			return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
		}
		return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
	}

	// Target is a non-EXECUTION row: find it by agent name.
	targetRowIdx := findScopedRowForAgent(workflow, currentRowIdx, targetAgent)
	if targetRowIdx < 0 {
		return domain.EngineDecision{Deviation: &domain.DeviationDecision{
			Info: domain.DeviationInfo{
				Kind:          domain.DeviationAmbiguousRoute,
				CurrentRow:    currentRowIdx,
				CurrentPhase:  currentRow.Phase,
				CurrentStage:  state.CurrentState.Stage,
				ArtifactState: state,
			},
		}}
	}

	step, err := buildDispatchStep(workflow, stages, targetRowIdx, 0, "", agents, seq, now, refreshedStages)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
	}
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
}

// handleExecutionSuccess routes after an EXECUTION row returns SUCCESS using
// group/stage logic. On Success is intentionally ignored inside EXECUTION.
func handleExecutionSuccess(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	currentRowIdx int,
	state domain.ArtifactState,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
) domain.EngineDecision {

	currentStageNum := parseStageNumber(state.CurrentState.Stage)
	adv, advErr := computeNextFromExecution(workflow, stages, currentRowIdx, currentStageNum)
	if advErr != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: advErr.Error()}}
	}

	if adv.Complete {
		return domain.EngineDecision{Complete: &domain.CompleteDecision{
			FinalState: state.CurrentState,
		}}
	}

	step, err := buildDispatchStep(workflow, stages, adv.RowIndex, adv.StageNumber, adv.StageString,
		agents, seq, now, nil)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
	}
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
}

// executionAdvance is the outcome of advancing past a completed EXECUTION row.
type executionAdvance struct {
	RowIndex    int                // next row to dispatch; undefined when Complete is true
	StageNumber domain.StageNumber
	StageString string             // "Stage-N"
	Complete    bool               // no further rows to dispatch
}

// computeNextFromExecution returns the next row after a successful EXECUTION
// dispatch, or an error when group ordering cannot be resolved for the current
// or the next stage.
func computeNextFromExecution(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	currentRowIdx int,
	currentStageNum domain.StageNumber,
) (executionAdvance, error) {

	ordGroups, err := orderedGroupsForStage(workflow, stages, currentStageNum)
	if err != nil {
		return executionAdvance{}, err
	}

	currentGroupIdx := -1
	for gi, g := range ordGroups {
		if currentRowIdx >= g.StartRow && currentRowIdx < g.EndRow {
			currentGroupIdx = gi
			break
		}
	}
	if currentGroupIdx < 0 {
		return executionAdvance{}, &domain.RowNotInGroupError{
			WorkflowID: workflow.Table.Info.ID,
			RowIndex:   currentRowIdx,
			Stage:      currentStageNum,
			Groups:     ordGroups,
		}
	}

	group := ordGroups[currentGroupIdx]

	// Not last row in current group: advance within the group.
	if currentRowIdx < group.EndRow-1 {
		next := currentRowIdx + 1
		ss := fmt.Sprintf("Stage-%d", currentStageNum)
		return executionAdvance{RowIndex: next, StageNumber: currentStageNum, StageString: ss}, nil
	}

	// Last row in current group: try next group within the same stage.
	if currentGroupIdx+1 < len(ordGroups) {
		nextGroup := ordGroups[currentGroupIdx+1]
		ss := fmt.Sprintf("Stage-%d", currentStageNum)
		return executionAdvance{RowIndex: nextGroup.StartRow, StageNumber: currentStageNum, StageString: ss}, nil
	}

	// Last group in the current stage: try the next stage.
	if stages != nil {
		nextStageEntry, ok := stages.Entry(currentStageNum + 1)
		if ok {
			nextOrdGroups, nextErr := orderedGroupsForStage(workflow, stages, nextStageEntry.Number)
			if nextErr != nil {
				return executionAdvance{}, nextErr
			}
			if len(nextOrdGroups) > 0 {
				ss := fmt.Sprintf("Stage-%d", nextStageEntry.Number)
				return executionAdvance{RowIndex: nextOrdGroups[0].StartRow, StageNumber: nextStageEntry.Number, StageString: ss}, nil
			}
		}
	}

	// No more stages: dispatch the first post-EXECUTION row if any.
	if workflow.PostExecutionStartRow < workflow.PostExecutionEndRow {
		return executionAdvance{RowIndex: workflow.PostExecutionStartRow}, nil
	}

	// Nothing left: run is complete.
	return executionAdvance{Complete: true}, nil
}

// buildDispatchStep assembles a DispatchStep for the given routing table row.
// stageNum=0 and stageStr="" indicate a non-EXECUTION row.
func buildDispatchStep(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	rowIdx int,
	stageNum domain.StageNumber,
	stageStr string,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
	refreshedStages *domain.StageSet,
) (domain.DispatchStep, error) {
	_ = now // timestamp is available for future use; not inspected by current tests
	row := workflow.Table.Rows[rowIdx]
	isExecution := row.PhaseParsed.IsStaged

	// Compute effective HITL.
	rowHITL := row.HITL
	stageHITL := false
	if isExecution && stages != nil && stageNum > 0 {
		if entry, ok := stages.Entry(stageNum); ok {
			stageHITL = entry.HITL
		}
	}
	effectiveHITL := rowHITL || stageHITL

	// Resolve artifact paths.
	inputArts, err := ResolveArtifacts(row.InputArtifacts, stageNum, stageStr, stages, refreshedStages, true)
	if err != nil {
		return domain.DispatchStep{}, err
	}
	outputArts, err := ResolveArtifacts(row.OutputArtifacts, stageNum, stageStr, stages, refreshedStages, false)
	if err != nil {
		return domain.DispatchStep{}, err
	}

	agent := agents[row.Agent]
	instanceID := fmt.Sprintf("%s#%d", row.Agent, seq+1)

	// The recorded Phase/Stage are the canonical form the artifact stores:
	// the bare phase name (never the routing table's qualified string) and
	// the group-qualified stage value. row.Phase / stageStr, the qualified
	// routing forms, remain available above for artifact-path resolution
	// only and never reach the recorded fields.
	recordedStage := ""
	if isExecution {
		recordedStage = domain.FormatStageValue(row.PhaseParsed.Group, stageNum)
	}

	return domain.DispatchStep{
		RowIndex: rowIdx,
		Agent:    agent,
		Request: domain.ProtocolRequest{
			AgentInstanceID: instanceID,
			InputArtifacts:  inputArts,
			OutputArtifacts: outputArts,
			HumanInTheLoop:  effectiveHITL,
		},
		EffectiveHITL: effectiveHITL,
		Phase:         row.PhaseParsed.Name,
		Stage:         recordedStage,
	}, nil
}
