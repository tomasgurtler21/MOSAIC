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
) domain.EngineDecision {

	// No prior invocations: initial dispatch.
	if state.CurrentState.LastAgent == "" {
		return initialDispatch(workflow, stages, agents, seq, now)
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
	currentRowIdx := findCurrentRowIndex(workflow, stages, state)
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
			agents, seq, now, refreshedStages)
	}
	return handleExecutionSuccess(workflow, stages, currentRowIdx, state, agents, seq, now)
}

// ResumePoint determines where to continue from a parsed artifact.
// Pure function -- no I/O, no side effects.
//
// If the artifact has no execution log entries, returns the first row.
// If the last logged invocation completed cleanly (its agent matches
// current_state.last_agent), returns the row after it.
// If a mismatch is detected between the execution log's last entry and
// current_state, the last step was interrupted in-flight and must be
// re-dispatched (ResumeInfo.RerunLast = true, FR-33).
func ResumePoint(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
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

	lastEntry := state.ExecutionLog[len(state.ExecutionLog)-1]

	// Interruption detection: the log's last entry doesn't match CurrentState.
	interrupted := lastEntry.Agent != state.CurrentState.LastAgent

	if interrupted {
		// The last log entry was dispatched but not recorded in CurrentState.
		// Re-run the interrupted row.
		rowIdx, err := findRowForLogEntry(workflow, stages, lastEntry)
		if err != nil {
			return domain.ResumeInfo{}, fmt.Errorf("resume: %w", err)
		}
		row := workflow.Table.Rows[rowIdx]
		stageNum := parseStageNumber(lastEntry.Stage)
		groupIdx := -1
		if row.PhaseParsed.IsStaged {
			groupIdx = findGroupIndexInWorkflow(workflow, rowIdx)
		}
		return domain.ResumeInfo{
			RowIndex:    rowIdx,
			Phase:       row.Phase,
			Stage:       lastEntry.Stage,
			StageNumber: stageNum,
			GroupIndex:  groupIdx,
			Seq:         state.GlobalSequence,
			RerunLast:   true,
		}, nil
	}

	// Clean completion: advance to the next row.
	currentRowIdx, err := findRowForLogEntry(workflow, stages, lastEntry)
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
	currentStageNum := parseStageNumber(lastEntry.Stage)
	nextRowIdx, nextStageNum, nextStageStr, nextGroupIdxInGroups, complete :=
		computeNextFromExecution(workflow, stages, currentRowIdx, currentStageNum)

	if complete || nextRowIdx < 0 {
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

	nextRow := workflow.Table.Rows[nextRowIdx]
	nextGroupIdx := -1
	if nextRow.PhaseParsed.IsStaged {
		// Map the approach-ordered group index to aw.Groups index.
		nextGroupIdx = findGroupIndexInWorkflow(workflow, nextRowIdx)
		_ = nextGroupIdxInGroups // approach-ordered index not needed for ResumeInfo
	}

	return domain.ResumeInfo{
		RowIndex:    nextRowIdx,
		Phase:       nextRow.Phase,
		Stage:       nextStageStr,
		StageNumber: nextStageNum,
		GroupIndex:  nextGroupIdx,
		Seq:         state.GlobalSequence,
		RerunLast:   false,
	}, nil
}

// ---- Routing helpers ----

// initialDispatch returns the first dispatch when no prior invocations exist.
func initialDispatch(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	agents map[string]domain.AgentReference,
	seq int,
	now time.Time,
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
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: "no stage set available for staged workflow"}}
	}
	stage1 := stages.Entries[0]
	ordGroups := orderedGroupsForApproach(workflow, stage1.Approach)
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
				Reason: "entering EXECUTION phase but no stage set is available",
			}}
		}
		stage1 := stages.Entries[0]
		ordGroups := orderedGroupsForApproach(workflow, stage1.Approach)
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
	nextRowIdx, nextStageNum, nextStageStr, _, complete :=
		computeNextFromExecution(workflow, stages, currentRowIdx, currentStageNum)

	if complete {
		return domain.EngineDecision{Complete: &domain.CompleteDecision{
			FinalState: state.CurrentState,
		}}
	}

	step, err := buildDispatchStep(workflow, stages, nextRowIdx, nextStageNum, nextStageStr,
		agents, seq, now, nil)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}
	}
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}
}

// computeNextFromExecution returns the next row after a successful EXECUTION dispatch.
// Returns (rowIdx, stageNum, stageStr, groupIdxInOrdGroups, complete).
// When complete=true, there are no more rows to dispatch.
// When rowIdx < 0 and complete=false, there was an internal error.
func computeNextFromExecution(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	currentRowIdx int,
	currentStageNum domain.StageNumber,
) (rowIdx int, stageNum domain.StageNumber, stageStr string, groupIdx int, complete bool) {

	approach := getApproachForStage(stages, currentStageNum)
	ordGroups := orderedGroupsForApproach(workflow, approach)

	currentGroupIdx := -1
	for gi, g := range ordGroups {
		if currentRowIdx >= g.StartRow && currentRowIdx < g.EndRow {
			currentGroupIdx = gi
			break
		}
	}
	if currentGroupIdx < 0 {
		return -1, 0, "", -1, false
	}

	group := ordGroups[currentGroupIdx]

	// Not last row in current group: advance within the group.
	if currentRowIdx < group.EndRow-1 {
		next := currentRowIdx + 1
		ss := fmt.Sprintf("Stage-%d", currentStageNum)
		return next, currentStageNum, ss, currentGroupIdx, false
	}

	// Last row in current group: try next group within the same stage.
	if currentGroupIdx+1 < len(ordGroups) {
		nextGroup := ordGroups[currentGroupIdx+1]
		ss := fmt.Sprintf("Stage-%d", currentStageNum)
		return nextGroup.StartRow, currentStageNum, ss, currentGroupIdx + 1, false
	}

	// Last group in the current stage: try the next stage.
	if stages != nil {
		nextStageEntry, ok := stages.Entry(currentStageNum + 1)
		if ok {
			nextStageNum := nextStageEntry.Number
			nextApproach := nextStageEntry.Approach
			nextOrdGroups := orderedGroupsForApproach(workflow, nextApproach)
			if len(nextOrdGroups) > 0 {
				ss := fmt.Sprintf("Stage-%d", nextStageNum)
				return nextOrdGroups[0].StartRow, nextStageNum, ss, 0, false
			}
		}
	}

	// No more stages: dispatch the first post-EXECUTION row if any.
	if workflow.PostExecutionStartRow < workflow.PostExecutionEndRow {
		return workflow.PostExecutionStartRow, 0, "", -1, false
	}

	// Nothing left: run is complete.
	return -1, 0, "", -1, true
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
		Phase:         row.Phase,
		Stage:         stageStr,
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
// derived from the artifact state. Returns -1 if the row cannot be identified.
func findCurrentRowIndex(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
) int {
	agentName := extractAgentName(state.CurrentState.LastAgent)
	phase := state.CurrentState.Phase

	// Non-EXECUTION row: find by agent name + phase (unique per phase in supported workflows).
	if !isExecutionPhase(phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == phase {
				return row.Index
			}
		}
		return -1
	}

	// EXECUTION row: collect all matching rows.
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	if len(matches) == 0 {
		return -1
	}
	if len(matches) == 1 {
		// Unique agent in EXECUTION — no disambiguation needed.
		return matches[0]
	}

	// Multiple EXECUTION rows for this agent (e.g. build-review appears in both
	// the test group and the implementation group). Use seq-based position to
	// determine which row was last dispatched. The computation accounts for
	// per-stage approach variation so mixed-approach workflows are handled correctly.
	stageNum := parseStageNumber(state.CurrentState.Stage)
	if stageNum == 0 {
		return -1
	}
	return findExecutionRowBySeq(workflow, stages, stageNum, state.GlobalSequence)
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
	rowIdx := findExecutionRowBySeq(workflow, stages, stageNum, entry.Seq)
	if rowIdx < 0 {
		return -1, fmt.Errorf("cannot map seq=%d in stage %q to a routing row", entry.Seq, entry.Stage)
	}
	return rowIdx, nil
}

// findExecutionRowBySeq maps a global sequence number inside a given stage
// to a routing table row index, using the approach-ordered group structure.
//
// The computation assumes contiguous seq numbering (no gaps from deviations
// within the same stage). Each stage contributes one seq per active row.
//
// Previous stages may have used different approaches (and therefore different
// active row counts). The offset is computed by summing the actual row counts
// across all previous stages rather than assuming a uniform rows-per-stage value.
func findExecutionRowBySeq(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	stageNum domain.StageNumber,
	seq int,
) int {
	approach := getApproachForStage(stages, stageNum)
	ordGroups := orderedGroupsForApproach(workflow, approach)

	preExecCount := workflow.PreExecutionEndRow - workflow.PreExecutionStartRow

	// Sum the actual row counts contributed by all stages before stageNum.
	// Each stage may use a different approach, so row counts may differ.
	previousRowsTotal := 0
	if stages != nil {
		for _, entry := range stages.Entries {
			if entry.Number >= stageNum {
				break
			}
			prevGroups := orderedGroupsForApproach(workflow, entry.Approach)
			previousRowsTotal += countActiveRows(prevGroups)
		}
	}

	rowsThisStage := countActiveRows(ordGroups)
	if rowsThisStage == 0 {
		return -1
	}

	// 1-indexed position within the current stage.
	posInStage := seq - preExecCount - previousRowsTotal
	if posInStage <= 0 {
		return -1
	}

	pos := posInStage
	for _, g := range ordGroups {
		size := g.EndRow - g.StartRow
		if pos <= size {
			return g.StartRow + (pos - 1)
		}
		pos -= size
	}
	return -1
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

// orderedGroupsForApproach returns the execution groups in the order they should
// run for the given approach value.
//
// For single-group workflows (TwoGroup=false) the approach has no effect on
// ordering; the single group is returned as-is.
func orderedGroupsForApproach(workflow domain.AdmittedWorkflow, approach domain.Approach) []domain.ExecutionGroup {
	if !workflow.TwoGroup {
		return workflow.Groups
	}

	var testGroup, implGroup domain.ExecutionGroup
	for _, g := range workflow.Groups {
		switch g.Kind {
		case domain.GroupTest:
			testGroup = g
		case domain.GroupImplementation:
			implGroup = g
		}
	}

	switch approach {
	case domain.ApproachTDD:
		return []domain.ExecutionGroup{testGroup, implGroup}
	case domain.ApproachImplementationFirst:
		return []domain.ExecutionGroup{implGroup, testGroup}
	case domain.ApproachImplementationOnly:
		return []domain.ExecutionGroup{implGroup}
	case domain.ApproachTestsOnly:
		return []domain.ExecutionGroup{testGroup}
	default:
		return workflow.Groups
	}
}

// getApproachForStage returns the approach for the given stage number,
// falling back to ApproachTDD when the stage set is nil or the stage is not found.
func getApproachForStage(stages *domain.StageSet, stageNum domain.StageNumber) domain.Approach {
	if stages == nil {
		return domain.ApproachTDD
	}
	if entry, ok := stages.Entry(stageNum); ok {
		return entry.Approach
	}
	return domain.ApproachTDD
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

// parseStageNumber extracts the integer stage number from a stage string like "Stage-1".
// Returns 0 when the string is empty or cannot be parsed.
func parseStageNumber(stageStr string) domain.StageNumber {
	if stageStr == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(stageStr, "Stage-%d", &n); err != nil {
		return 0
	}
	return domain.StageNumber(n)
}

// isExecutionPhase returns true when the phase string represents a staged EXECUTION phase.
func isExecutionPhase(phase string) bool {
	return strings.HasPrefix(phase, "EXECUTION.")
}

// firstRowPhase returns the phase of row 0, or "" if the table is empty.
func firstRowPhase(workflow domain.AdmittedWorkflow) string {
	if len(workflow.Table.Rows) == 0 {
		return ""
	}
	return workflow.Table.Rows[0].Phase
}
