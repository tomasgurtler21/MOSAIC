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

	"mosaic-run/internal/domain"
)

// Next is the routing decision. It is pure: no I/O, no clock read, no
// filesystem access, no randomness. Every mode-conditional input arrives in
// NextInput.
//
// Behaviour by in.Mode:
//
//	ExecutionModeOrchestrated — Next never produces a routing decision.
//	  It returns EngineDecision{Consult: &ConsultDecision{Trigger:
//	  ConsultTriggerOrchestratedMode, ...}} for every call, including the
//	  first call of a new run.
//
//	ExecutionModeAuto — On StatusSUCCESS, routes to the On Success target or
//	  the next EXECUTION row exactly as before. On ANY non-SUCCESS status,
//	  including StatusCOMPLETED_NEEDS_ACTION, returns a DeviationDecision.
//	  The On-Findings auto-route never fires.
//
//	ExecutionModeAutoReview — As ExecutionModeAuto, plus: on
//	  StatusCOMPLETED_NEEDS_ACTION where the row's OnFindings hint is
//	  unambiguous, returns a DispatchDecision targeting that agent's row,
//	  with in.LastOutputArtifacts injected into the dispatched step's
//	  Request.InputArtifacts (see review artifact injection below). When the
//	  reviewer's COMPLETED_NEEDS_ACTION count at the current phase and stage
//	  reaches state.ReviewLoopLimit (0 = no limit), the engine returns a
//	  DeviationReviewLoopLimit deviation instead of auto-routing.
//
// An unambiguous OnFindings hint is one where ColumnPresent is true, Value is
// non-empty, and Value contains no space and no parenthesis.
//
// Review artifact injection applies to the auto-review COMPLETED_NEEDS_ACTION
// auto-route-back and to nothing else. A SUCCESS-routed dispatch never carries
// injected artifacts. The dispatched step's InputArtifacts are the table row's
// entries in table order, followed by the entries of in.LastOutputArtifacts
// that are not already present, in their given order. Comparison is on the
// exact path string as dispatched.
//
// DispatchDecision.Steps continues to hold exactly one element.
func Next(in NextInput) domain.EngineDecision {
	// Unpack for readability.
	workflow := in.Workflow
	stages := in.Stages
	state := in.State
	lastResponse := in.LastResponse
	agents := in.Agents
	seq := in.Seq
	now := in.Now
	refreshedStages := in.RefreshedStages
	src := in.StageSource

	// ExecutionModeUnset is a programming error: surface it as an ambiguous-route
	// deviation rather than silently picking a mode or panicking.
	if in.Mode == domain.ExecutionModeUnset {
		return domain.EngineDecision{Deviation: &domain.DeviationDecision{
			Info: domain.DeviationInfo{
				Kind:          domain.DeviationAmbiguousRoute,
				ArtifactState: state,
			},
		}}
	}

	// ExecutionModeOrchestrated: the engine never produces a routing decision.
	// Return a ConsultDecision for every call, including the first.
	if in.Mode == domain.ExecutionModeOrchestrated {
		if state.CurrentState.LastAgent == "" {
			// First decision of a new run: no row has been dispatched yet.
			return domain.EngineDecision{Consult: &domain.ConsultDecision{
				Trigger:       domain.ConsultTriggerOrchestratedMode,
				CurrentRow:    -1,
				ArtifactState: state,
			}}
		}
		// Subsequent call: derive position from state. Ignore findCurrentRowIndex
		// errors — the orchestrator reads the artifact directly and does not depend
		// on the engine's row resolution.
		currentRowIdx, _ := findCurrentRowIndex(workflow, stages, state)
		return domain.EngineDecision{Consult: &domain.ConsultDecision{
			Trigger:       domain.ConsultTriggerOrchestratedMode,
			CurrentRow:    currentRowIdx,
			CurrentPhase:  state.CurrentState.Phase,
			CurrentStage:  state.CurrentState.Stage,
			ArtifactState: state,
		}}
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
		// This auto-route fires only in auto-review mode; in auto mode it deviates.
		if in.Mode == domain.ExecutionModeAutoReview &&
			status == domain.StatusCOMPLETED_NEEDS_ACTION &&
			isUnambiguousHint(currentRow.OnFindings) &&
			!reviewLoopLimitReached(state) {
			targetAgent := currentRow.OnFindings.Value
			targetRowIdx := findNearestPrecedingRowForAgent(workflow, currentRowIdx, targetAgent)
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
				// Inject the reviewing agent's output artifacts into the dispatched
				// step's InputArtifacts. Entries already present in the table row's
				// resolved list are not duplicated.
				step.Request.InputArtifacts = injectReviewArtifacts(
					step.Request.InputArtifacts, in.LastOutputArtifacts)
				// Inject the creator agent's own previously-produced output artifacts
				// from the registry. The comparison is against step.Request.OutputArtifacts
				// (resolved paths from buildDispatchStep), not against raw row.OutputArtifacts
				// (which may contain unresolved template tokens). This ensures injection
				// fires correctly for rows with templated output artifact paths.
				if len(in.ArtifactRegistry) > 0 {
					outputArtSet := make(map[string]bool, len(step.Request.OutputArtifacts))
					for _, oa := range step.Request.OutputArtifacts {
						outputArtSet[oa] = true
					}
					var creatorArts []string
					for _, entry := range in.ArtifactRegistry {
						if outputArtSet[entry.Artifact] {
							creatorArts = append(creatorArts, entry.Artifact)
						}
					}
					if len(creatorArts) > 0 {
						step.Request.InputArtifacts = injectReviewArtifacts(
							step.Request.InputArtifacts, creatorArts)
					}
				}
				return domain.EngineDecision{Dispatch: &domain.DispatchDecision{
					Steps: []domain.DispatchStep{step},
				}}
			}
		}

		// Review loop limit reached in auto-review mode: deviate instead of routing.
		if in.Mode == domain.ExecutionModeAutoReview &&
			status == domain.StatusCOMPLETED_NEEDS_ACTION &&
			isUnambiguousHint(currentRow.OnFindings) &&
			reviewLoopLimitReached(state) {
			return domain.EngineDecision{Deviation: &domain.DeviationDecision{
				Info: domain.DeviationInfo{
					Kind:          domain.DeviationReviewLoopLimit,
					Response:      resp,
					CurrentRow:    currentRowIdx,
					CurrentPhase:  currentRow.Phase,
					CurrentStage:  state.CurrentState.Stage,
					ArtifactState: state,
				},
			}}
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
