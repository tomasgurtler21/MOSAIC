// Package engine is the pure, side-effect-free decision core of the runner.
// It answers "what runs next" given the admitted workflow, stage set, and
// current artifact state.
//
// Purity constraint: Next and ResumePoint import no I/O packages and never
// call time.Now(), os.*, net.*, os/exec.*, or math/rand.*. All time-varying
// inputs arrive as parameters. The time package is imported only for the
// time.Time parameter type; no system-clock reads occur.
package engine

import (
	"fmt"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// Next determines what should happen next given the current state.
// Pure function -- no I/O, no side effects.
//
// Parameters:
//   - workflow: the admitted workflow with resolved execution groups.
//   - stages: the stage set (nil for non-staged workflows).
//   - state: the current artifact state (parsed Orchestration.md).
//   - lastResponse: the full protocol response from the most recent invocation
//     (nil on first call or after resume). When nil, Next derives routing
//     from state.CurrentState alone.
//   - agents: resolved agent references keyed by agent identifier.
//   - seq: current global sequence number. Next increments it by one and
//     uses the result as the dispatch sequence number.
//   - now: current timestamp, passed in for deterministic test output.
//   - refreshedStages: optional refreshed stage set for Stage-* input
//     resolution on non-EXECUTION rows (nil when not applicable).
//   - stageSource: optional and at most one may be supplied. It describes
//     where the stage table was looked for, and is used only to build the
//     stop message when a staged row is reached with stages == nil. The
//     zero value (or omission) yields the generic message. Passing more
//     than one is a programming error and the first is used.
//
// Returns an EngineDecision: Dispatch, Complete, Deviation, or Stop.
func Next(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
	lastResponse *domain.ProtocolResponse,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
	refreshedStages *domain.StageSet,
	stageSource ...domain.StageSource,
) domain.EngineDecision {

	var src domain.StageSource
	if len(stageSource) > 0 {
		src = stageSource[0]
	}

	// No prior invocations: initial dispatch.
	if state.CurrentState.LastAgent == "" {
		return initialDispatch(workflow, stages, agents, seq, now, src)
	}

	// Determine the response status and the response for deviation assembly.
	var status domain.StatusCode
	var resp domain.ProtocolResponse
	if lastResponse != nil {
		status = lastResponse.StatusCode
		resp = *lastResponse
	} else {
		status = state.CurrentState.LastStatus
	}

	// Locate the row that was last completed.
	// Check for a routing error (e.g. unresolvable approach) before the generic
	// "could not determine current row" check — the approach error is more specific.
	currentRowIdx, rowFindErr := findCurrentRowIndex(workflow, stages, state)
	if rowFindErr != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: rowFindErr.Error()}}
	}
	if currentRowIdx < 0 {
		return domain.EngineDecision{Stop: &domain.StopDecision{
			Reason: fmt.Sprintf("could not determine current row from artifact state (LastAgent=%q)",
				state.CurrentState.LastAgent),
		}}
	}
	currentRow := workflow.Table.Rows[currentRowIdx]

	// Handle non-SUCCESS responses.
	if status != domain.StatusSUCCESS {
		// COMPLETED_NEEDS_ACTION with an unambiguous On Findings hint → loop-back dispatch.
		if status == domain.StatusCOMPLETED_NEEDS_ACTION && isUnambiguousHint(currentRow.OnFindings) {
			targetAgent := currentRow.OnFindings.Value
			targetRowIdx := findFirstRowForAgent(workflow, targetAgent)
			if targetRowIdx >= 0 {
				var stageNum domain.StageNumber
				var stageStr string
				if currentRow.PhaseParsed.IsStaged {
					stageNum = parseStageNumber(state.CurrentState.Stage)
					stageStr = state.CurrentState.Stage
				}
				step, err := buildDispatchStep(workflow, stages, targetRowIdx, stageNum, stageStr,
					agents, seq, now, refreshedStages)
				if err != nil {
					return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
				}
				return domain.EngineDecision{Dispatch: &domain.DispatchDecision{
					Steps: []domain.DispatchStep{step},
				}}
			}
		}

		// All other non-SUCCESS → Deviation.
		return domain.EngineDecision{Deviation: &domain.DeviationDecision{
			Info: domain.DeviationInfo{
				Kind:          domain.DeviationNonSuccess,
				Response:      resp,
				CurrentRow:    currentRowIdx,
				CurrentPhase:  currentRow.Phase,
				CurrentStage:  state.CurrentState.Stage,
				ArtifactState: state,
			},
		}}
	}

	// SUCCESS: route based on row type.
	if !currentRow.PhaseParsed.IsStaged {
		return handleNonExecutionSuccess(workflow, stages, currentRowIdx, currentRow, state,
			agents, seq, now, refreshedStages, src)
	}
	return handleExecutionSuccess(workflow, stages, currentRowIdx, state, agents, seq, now)
}

// ResumePoint determines where to continue from a parsed artifact.
// Pure function -- no I/O, no side effects.
//
// Position is resolved from the last *workflow* entry in the execution log:
// trailing infrastructure entries are skipped, in sequence order, using the
// recognition rule (agent absent from workflow.Table, present in infra).
// infra may be nil, meaning no infrastructure agents are declared.
//
// If the artifact has no execution log entries, or none that is a workflow
// entry, returns the first row.
// If the last *workflow* logged invocation completed cleanly (its agent
// matches current_state.last_agent), returns the row after it. Trailing
// infrastructure entries appearing after it are not an interruption.
// If a mismatch is detected between the last workflow execution log entry
// and current_state, the last workflow step was interrupted in-flight and
// must be re-dispatched (ResumeInfo.RerunLast = true, FR-33).
//
// Returns *domain.PositionUnresolvedError when the position cannot be
// determined.
func ResumePoint(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
	infra domain.InfraAgentSet,
) (domain.ResumeInfo, error) {
	if len(state.ExecutionLog) == 0 {
		// Fresh start: resume from the first row.
		return domain.ResumeInfo{
			RowIndex:    0,
			Phase:       firstRowPhase(workflow),
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         0,
			RerunLast:   false,
		}, nil
	}

	lastWorkflowEntry, found, findErr := lastWorkflowLogEntry(workflow, state.ExecutionLog, infra)
	if findErr != nil {
		return domain.ResumeInfo{}, fmt.Errorf("resume: %w", findErr)
	}
	if !found {
		// The log holds no workflow entry at all (only infrastructure activity
		// on record so far): resume from the first row, as if fresh.
		return domain.ResumeInfo{
			RowIndex:    0,
			Phase:       firstRowPhase(workflow),
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         0,
			RerunLast:   false,
		}, nil
	}

	// Interruption detection: the last WORKFLOW entry doesn't match
	// CurrentState. Trailing infrastructure entries appearing after it are
	// not an interruption.
	interrupted := lastWorkflowEntry.Agent != state.CurrentState.LastAgent

	if interrupted {
		// The last workflow log entry was dispatched but not recorded in
		// CurrentState. Re-run the interrupted row.
		rowIdx, err := findRowForLogEntry(workflow, stages, lastWorkflowEntry)
		if err != nil {
			return domain.ResumeInfo{}, fmt.Errorf("resume: %w", err)
		}
		row := workflow.Table.Rows[rowIdx]
		stageNum := parseStageNumber(lastWorkflowEntry.Stage)
		groupIdx := -1
		if row.PhaseParsed.IsStaged {
			groupIdx = findGroupIndexInWorkflow(workflow, rowIdx)
		}
		return domain.ResumeInfo{
			RowIndex:    rowIdx,
			Phase:       row.Phase,
			Stage:       lastWorkflowEntry.Stage,
			StageNumber: stageNum,
			GroupIndex:  groupIdx,
			Seq:         state.GlobalSequence,
			RerunLast:   true,
		}, nil
	}

	// Clean completion: advance to the next row after the last workflow step.
	currentRowIdx, err := findRowForLogEntry(workflow, stages, lastWorkflowEntry)
	if err != nil {
		return domain.ResumeInfo{}, fmt.Errorf("resume: %w", err)
	}

	currentRow := workflow.Table.Rows[currentRowIdx]

	if !currentRow.PhaseParsed.IsStaged {
		// Non-EXECUTION row: next is the row immediately after in the table.
		nextRowIdx := currentRowIdx + 1
		if nextRowIdx >= len(workflow.Table.Rows) {
			return domain.ResumeInfo{
				RowIndex:    nextRowIdx,
				Phase:       "",
				Stage:       "",
				StageNumber: 0,
				GroupIndex:  -1,
				Seq:         state.GlobalSequence,
				RerunLast:   false,
			}, nil
		}
		nextRow := workflow.Table.Rows[nextRowIdx]
		return domain.ResumeInfo{
			RowIndex:    nextRowIdx,
			Phase:       nextRow.Phase,
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         state.GlobalSequence,
			RerunLast:   false,
		}, nil
	}

	// EXECUTION row: apply group/stage logic.
	currentStageNum := parseStageNumber(lastWorkflowEntry.Stage)
	adv, advErr := computeNextFromExecution(workflow, stages, currentRowIdx, currentStageNum)
	if advErr != nil {
		return domain.ResumeInfo{}, fmt.Errorf("resume: %w", advErr)
	}

	if adv.Complete || adv.RowIndex < 0 {
		return domain.ResumeInfo{
			RowIndex:    len(workflow.Table.Rows),
			Phase:       "",
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         state.GlobalSequence,
			RerunLast:   false,
		}, nil
	}

	nextRow := workflow.Table.Rows[adv.RowIndex]
	nextGroupIdx := -1
	if nextRow.PhaseParsed.IsStaged {
		nextGroupIdx = findGroupIndexInWorkflow(workflow, adv.RowIndex)
	}

	return domain.ResumeInfo{
		RowIndex:    adv.RowIndex,
		Phase:       nextRow.Phase,
		Stage:       adv.StageString,
		StageNumber: adv.StageNumber,
		GroupIndex:  nextGroupIdx,
		Seq:         state.GlobalSequence,
		RerunLast:   false,
	}, nil
}

// lastWorkflowLogEntry returns the most recent execution log entry produced
// by a workflow participant, skipping any trailing infrastructure entries
// (recognised by derivation: absent from the routing table, present in infra).
//
// found is false when the log holds no workflow entry at all -- only
// infrastructure activity is on record so far.
//
// Returns *domain.PositionUnresolvedError when a trailing entry is neither a
// workflow participant nor a recognised infrastructure agent: the position
// cannot be determined in that case. infra may be nil, meaning no
// infrastructure agents are declared.
func lastWorkflowLogEntry(
	workflow domain.AdmittedWorkflow,
	log []domain.ExecutionLogEntry,
	infra domain.InfraAgentSet,
) (entry domain.ExecutionLogEntry, found bool, err error) {
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		agentName := extractAgentName(e.Agent)
		if isWorkflowParticipant(workflow, agentName) {
			return e, true, nil
		}
		if infra != nil && infra.IsInfrastructureAgent(e.Agent) {
			continue // trailing infrastructure entry: keep looking
		}
		return domain.ExecutionLogEntry{}, false, &domain.PositionUnresolvedError{
			AgentInstance: e.Agent,
			Phase:         e.Phase,
			Stage:         e.Stage,
			Cause:         domain.CauseAgentNotInWorkflow,
		}
	}
	return domain.ExecutionLogEntry{}, false, nil
}

// isWorkflowParticipant reports whether agentName names a routing table
// participant for this workflow, in any row.
func isWorkflowParticipant(workflow domain.AdmittedWorkflow, agentName string) bool {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName {
			return true
		}
	}
	return false
}

// noStageSetReason builds the stop reason for a staged row reached with no
// stage set available. base is the generic message for the call site
// (distinct wording for initial dispatch vs. entering EXECUTION from a
// prior row). When stageSource is the zero value ("not stated"), base is
// returned unchanged. Otherwise the message names the path the stage table
// was looked for, and distinguishes "was supposed to be seeded here and is
// missing" from "this run never had one" via Seeded.
func noStageSetReason(base string, stageSource domain.StageSource) string {
	if stageSource == (domain.StageSource{}) {
		return base
	}
	if stageSource.Seeded {
		return fmt.Sprintf("%s: a stage table was seeded at %s but is missing", base, stageSource.Path)
	}
	return fmt.Sprintf("%s: no stage table was found at %s", base, stageSource.Path)
}

// ---- Routing helpers ----

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

	targetAgent := hint.Value

	// Check whether the target agent lives in an EXECUTION row.
	// If so, we are entering the EXECUTION phase → apply approach-driven ordering
	// to determine the actual first row to dispatch (ignoring the specific agent
	// named in On Success, which reflects the default TDD ordering).
	targetInExecution := false
	for _, row := range workflow.Table.Rows {
		if row.Agent == targetAgent && row.PhaseParsed.IsStaged {
			targetInExecution = true
			break
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
	targetRowIdx := findFirstNonExecutionRowForAgent(workflow, targetAgent)
	if targetRowIdx < 0 {
		// Fall back to any row for this agent.
		targetRowIdx = findFirstRowForAgent(workflow, targetAgent)
	}
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
		return executionAdvance{RowIndex: -1}, nil
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

// ---- Dispatch step construction ----

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
	inputArts, err := resolveArtifacts(row.InputArtifacts, stageNum, stageStr, stages, refreshedStages, true)
	if err != nil {
		return domain.DispatchStep{}, err
	}
	outputArts, err := resolveArtifacts(row.OutputArtifacts, stageNum, stageStr, stages, refreshedStages, false)
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

// resolveArtifacts expands template variables in artifact paths.
// For input artifacts: {StageNumber} is substituted and Stage-* is expanded per stage.
// For output artifacts: {StageNumber} is substituted and Stage-* is passed through unexpanded.
func resolveArtifacts(
	arts []string,
	stageNum domain.StageNumber,
	stageStr string,
	stages *domain.StageSet,
	refreshedStages *domain.StageSet,
	isInput bool,
) ([]string, error) {

	isExecution := stageStr != "" && stageNum > 0

	// Effective stage set for Stage-* expansion in input artifacts.
	effectiveStages := stages
	if !isExecution && refreshedStages != nil {
		effectiveStages = refreshedStages
	}

	var result []string
	for _, art := range arts {
		// Substitute {StageNumber}.
		if strings.Contains(art, "{StageNumber}") {
			if !isExecution {
				return nil, fmt.Errorf(
					"unresolvable {StageNumber} in artifact path %q: no stage context at this row", art)
			}
			art = strings.ReplaceAll(art, "{StageNumber}", fmt.Sprintf("%d", stageNum))
		}

		// Handle Stage-* wildcard.
		if strings.Contains(art, "Stage-*") {
			if !isInput {
				// Output artifacts: pass through unexpanded.
				result = append(result, art)
				continue
			}
			// Input artifacts: expand to one path per stage.
			if effectiveStages == nil {
				return nil, fmt.Errorf(
					"Stage-* in artifact %q requires a stage set but none is available", art)
			}
			for _, entry := range effectiveStages.Entries {
				expanded := strings.ReplaceAll(art, "Stage-*", fmt.Sprintf("Stage-%d", entry.Number))
				result = append(result, expanded)
			}
			continue
		}

		result = append(result, art)
	}
	return result, nil
}

// ---- Row location helpers ----

// findCurrentRowIndex returns the routing table row index that was last dispatched,
// derived from the artifact state.
//
// It returns (-1, nil) when the row simply cannot be identified from the state
// (no matching agent+phase row, unparseable stage number) — an ambiguity, not a
// contract violation.
//
// It returns (-1, err) when the row would have been identified by sequence
// arithmetic but that arithmetic could not run (e.g. unresolvable approach).
// The error is propagated verbatim, never collapsed into -1.
func findCurrentRowIndex(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
) (int, error) {
	agentName := extractAgentName(state.CurrentState.LastAgent)
	phase := state.CurrentState.Phase

	// An agent that is not a routing table participant at all is not an
	// ambiguity to fall back on -- it is an unresolvable position, and the
	// stop must name this as the cause (AC3.7). After the Apply fix,
	// CurrentState.LastAgent always names a workflow participant in a
	// correctly-recorded artifact, so reaching this branch means the
	// recorded agent genuinely is not one.
	if !isWorkflowParticipant(workflow, agentName) {
		return -1, &domain.PositionUnresolvedError{
			AgentInstance: state.CurrentState.LastAgent,
			Phase:         phase,
			Stage:         state.CurrentState.Stage,
			Cause:         domain.CauseAgentNotInWorkflow,
		}
	}

	// Non-EXECUTION row: find by agent name + phase (unique per phase in supported workflows).
	if !isExecutionPhase(phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == phase {
				return row.Index, nil
			}
		}
		return -1, nil
	}

	// EXECUTION row: collect all matching rows.
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	if len(matches) == 0 {
		return -1, nil
	}
	if len(matches) == 1 {
		// Unique agent in EXECUTION — no disambiguation needed.
		return matches[0], nil
	}

	// Multiple EXECUTION rows for this agent (e.g. build-review appears in both
	// the test group and the implementation group). Use seq-based position to
	// determine which row was last dispatched. The computation accounts for
	// per-stage approach variation so mixed-approach workflows are handled correctly.
	stageNum := parseStageNumber(state.CurrentState.Stage)
	if stageNum == 0 {
		return -1, nil
	}
	rowIdx, err := findExecutionRowBySeq(workflow, stages, stageNum, state.GlobalSequence, matches)
	if err != nil {
		return -1, err // propagate, do not collapse to ambiguous -1
	}
	return rowIdx, nil
}

// findRowForLogEntry returns the row index corresponding to an execution log entry.
// Uses agent+phase for unique agents, falling back to seq-based position for
// agents that appear multiple times in EXECUTION rows.
func findRowForLogEntry(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	entry domain.ExecutionLogEntry,
) (int, error) {
	agentName := extractAgentName(entry.Agent)
	if !isExecutionPhase(entry.Phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == entry.Phase {
				return row.Index, nil
			}
		}
		return -1, fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	}

	// EXECUTION: try unique match first.
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return -1, fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	}

	// Multiple matches: use seq-based position.
	stageNum := parseStageNumber(entry.Stage)
	if stageNum == 0 {
		return -1, fmt.Errorf("cannot parse stage number from %q", entry.Stage)
	}
	rowIdx, seqErr := findExecutionRowBySeq(workflow, stages, stageNum, entry.Seq, matches)
	if seqErr != nil {
		return -1, seqErr // propagate approach error verbatim
	}
	if rowIdx < 0 {
		return -1, fmt.Errorf("cannot map seq=%d in stage %q to a routing row", entry.Seq, entry.Stage)
	}
	return rowIdx, nil
}

// findExecutionRowBySeq maps a global sequence number inside a given stage
// to a routing table row index among the candidate rows in matches, using the
// approach-ordered group structure.
//
// The naive computation (seq minus everything dispatched before this stage)
// assumes one seq increment per active row. An interleaved infrastructure
// invocation within the stage also consumes a seq number, which only ever
// inflates this position, never deflates it. matches (the routing rows that
// share the target agent) is therefore searched for the candidate with the
// largest position not exceeding the computed position -- the correct
// candidate whether or not an infrastructure step landed in between.
//
// Previous stages may have used different approaches (and therefore different
// active row counts). The offset is computed by summing the actual row counts
// across all previous stages rather than assuming a uniform rows-per-stage value.
//
// Returns an error when any stage at or before stageNum has an unresolvable
// approach, since the row-count arithmetic depends on every prior stage's
// ordered sequence.
func findExecutionRowBySeq(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	stageNum domain.StageNumber,
	seq int,
	matches []int,
) (int, error) {
	ordGroups, err := orderedGroupsForStage(workflow, stages, stageNum)
	if err != nil {
		return -1, err
	}

	preExecCount := workflow.PreExecutionEndRow - workflow.PreExecutionStartRow

	// Sum the actual row counts contributed by all stages before stageNum.
	// Each stage may use a different approach, so row counts may differ.
	previousRowsTotal := 0
	if stages != nil {
		for _, entry := range stages.Entries {
			if entry.Number >= stageNum {
				break
			}
			prevGroups, prevErr := orderedGroupsForStage(workflow, stages, entry.Number)
			if prevErr != nil {
				return -1, prevErr
			}
			previousRowsTotal += countActiveRows(prevGroups)
		}
	}

	rowsThisStage := countActiveRows(ordGroups)
	if rowsThisStage == 0 {
		return -1, nil
	}

	// 1-indexed position within the current stage.
	posInStage := seq - preExecCount - previousRowsTotal
	if posInStage <= 0 {
		return -1, nil
	}

	matchSet := make(map[int]bool, len(matches))
	for _, m := range matches {
		matchSet[m] = true
	}

	// Walk the ordered groups assigning each row its 1-indexed position, and
	// track the candidate (from matches) with the largest position not
	// exceeding posInStage. Fall back to the earliest candidate when
	// posInStage falls before every candidate's true position.
	bestRow, bestPos := -1, -1
	firstRow, firstPos := -1, -1
	pos := 0
	for _, g := range ordGroups {
		for r := g.StartRow; r < g.EndRow; r++ {
			pos++
			if !matchSet[r] {
				continue
			}
			if firstRow < 0 || pos < firstPos {
				firstRow, firstPos = r, pos
			}
			if pos <= posInStage && pos > bestPos {
				bestRow, bestPos = r, pos
			}
		}
	}
	if bestRow >= 0 {
		return bestRow, nil
	}
	return firstRow, nil
}

// findFirstRowForAgent returns the index of the first routing table row
// whose Agent matches the given identifier.
func findFirstRowForAgent(workflow domain.AdmittedWorkflow, agentName string) int {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName {
			return row.Index
		}
	}
	return -1
}

// findFirstNonExecutionRowForAgent returns the index of the first non-EXECUTION
// row whose Agent matches the given identifier.
func findFirstNonExecutionRowForAgent(workflow domain.AdmittedWorkflow, agentName string) int {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && !row.PhaseParsed.IsStaged {
			return row.Index
		}
	}
	return -1
}

// findGroupIndexInWorkflow returns the index into aw.Groups that contains rowIdx,
// or -1 when the row is not in any execution group.
func findGroupIndexInWorkflow(workflow domain.AdmittedWorkflow, rowIdx int) int {
	for i, g := range workflow.Groups {
		if rowIdx >= g.StartRow && rowIdx < g.EndRow {
			return i
		}
	}
	return -1
}

// ---- Approach and group helpers ----

// orderedGroupsForStage returns the execution groups in the order they run for
// the given stage, resolved from the workflow's approach table.
//
// When the workflow declares no groups, the approach is ignored entirely and the
// workflow's single implicit group is returned without error.
//
// Returns *domain.UnresolvableApproachError when the stage's Approach value has
// no matching table row. It never falls back to the declared order, to a default
// approach, or to running every group.
func orderedGroupsForStage(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	stageNum domain.StageNumber,
) ([]domain.ExecutionGroup, error) {
	// No groups declared: ignore the approach entirely.
	if !workflow.GroupsDeclared {
		return workflow.Groups, nil
	}

	// Grouped workflow: look up the approach for this stage.
	if stages == nil {
		return nil, fmt.Errorf("stage %d: staged workflow requires a stage set but none is available", stageNum)
	}
	entry, ok := stages.Entry(stageNum)
	if !ok {
		return nil, fmt.Errorf("stage %d has no entry in stage set", stageNum)
	}

	// Look up the approach in the workflow's approach table.
	groupNames, found := workflow.ApproachTable.Sequence(entry.Approach)
	if !found {
		return nil, &domain.UnresolvableApproachError{
			WorkflowID: workflow.Table.Info.ID,
			Stage:      stageNum,
			Approach:   entry.Approach,
			Declared:   workflow.ApproachTable.Approaches(),
		}
	}

	// Map group names to execution groups in sequence order.
	result := make([]domain.ExecutionGroup, 0, len(groupNames))
	for _, name := range groupNames {
		g, ok := workflow.GroupByName(name)
		if !ok {
			// This should be unreachable for an admitted workflow (guaranteed by A4),
			// but surface an error rather than silently skipping.
			return nil, fmt.Errorf("approach %q references group %q, which no EXECUTION row declares", entry.Approach, name)
		}
		result = append(result, g)
	}
	return result, nil
}

// countActiveRows returns the total number of rows across all groups in the slice.
func countActiveRows(groups []domain.ExecutionGroup) int {
	total := 0
	for _, g := range groups {
		total += g.EndRow - g.StartRow
	}
	return total
}

// ---- Hint disambiguation ----

// isUnambiguousHint returns true when the hint column is present, the value is
// non-empty, and the value contains no spaces or parentheses (making it a
// plain agent identifier or the keyword "COMPLETE").
func isUnambiguousHint(hint domain.OptionalHint) bool {
	if !hint.ColumnPresent || hint.Value == "" {
		return false
	}
	return !strings.ContainsAny(hint.Value, " ()")
}

// ---- String parsing helpers ----

// extractAgentName strips the "#seq" suffix from an agent instance ID.
// For example, "plan-review#3" becomes "plan-review".
func extractAgentName(instanceID string) string {
	if i := strings.LastIndex(instanceID, "#"); i >= 0 {
		return instanceID[:i]
	}
	return instanceID
}

// parseStageNumber extracts the integer stage number from a recorded stage
// value, accepting both the target form ("Test.1", "1") and the legacy form
// ("Stage-1") via domain.ParseStageValue. Returns 0 when the string is empty
// or cannot be parsed.
func parseStageNumber(stageStr string) domain.StageNumber {
	_, n, ok := domain.ParseStageValue(stageStr)
	if !ok {
		return 0
	}
	return n
}

// isExecutionPhase returns true when the phase value -- in either the target
// bare form ("EXECUTION") or a legacy qualified form
// ("EXECUTION.Test.[StageNumber]") -- represents a staged EXECUTION phase.
func isExecutionPhase(phase string) bool {
	return domain.RecordedPhaseName(phase) == "EXECUTION"
}

// firstRowPhase returns the phase of row 0, or "" if the table is empty.
func firstRowPhase(workflow domain.AdmittedWorkflow) string {
	if len(workflow.Table.Rows) == 0 {
		return ""
	}
	return workflow.Table.Rows[0].Phase
}
