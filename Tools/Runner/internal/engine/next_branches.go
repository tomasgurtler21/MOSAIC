package engine

import (
	"mosaic-run/internal/domain"
)

// orchestratedDecision is the decision of ExecutionModeOrchestrated: the
// engine never routes, it always hands the decision to the consultant.
func orchestratedDecision(in NextInput) domain.EngineDecision {
	state := in.State
	if state.CurrentState.LastAgent == "" {
		// First decision of a new run: no row has been dispatched yet.
		return domain.EngineDecision{Consult: &domain.ConsultDecision{
			Trigger:       domain.ConsultTriggerOrchestratedMode,
			CurrentRow:    -1,
			ArtifactState: state,
		}}
	}
	// Subsequent call: derive position from state. Ignore currentPosition
	// errors -- the orchestrator reads the artifact directly and does not depend
	// on the engine's row resolution.
	pos, _ := currentPosition(in.Workflow, state)
	currentRowIdx := pos.rowIdx
	return domain.EngineDecision{Consult: &domain.ConsultDecision{
		Trigger:       domain.ConsultTriggerOrchestratedMode,
		CurrentRow:    currentRowIdx,
		CurrentPhase:  state.CurrentState.Phase,
		CurrentStage:  state.CurrentState.Stage,
		ArtifactState: state,
	}}
}

// nonSuccessDecision decides after a non-SUCCESS response at currentRow:
// the auto-review findings route-back, the review-loop-limit deviation, or
// the generic non-SUCCESS deviation.
func nonSuccessDecision(
	in NextInput,
	pos position,
	currentRow domain.RoutingRow,
	status domain.StatusCode,
	resp domain.ProtocolResponse,
) domain.EngineDecision {
	state := in.State
	currentRowIdx := pos.rowIdx
	if dec, ok := mechanicalRetry(in, pos, status); ok {
		return dec
	}
	isFindingsLoop := in.Mode == domain.ExecutionModeAutoReview &&
		status == domain.StatusCOMPLETED_NEEDS_ACTION &&
		isUnambiguousHint(currentRow.OnFindings)

	// COMPLETED_NEEDS_ACTION with an unambiguous On Findings hint -> loop-back dispatch.
	// This auto-route fires only in auto-review mode; in auto mode it deviates.
	if isFindingsLoop && !reviewLoopLimitReached(state, pos) {
		if dec, ok := findingsRouteBack(in, pos, currentRow); ok {
			return dec
		}
	}

	kind := domain.DeviationNonSuccess
	if isFindingsLoop && reviewLoopLimitReached(state, pos) {
		// Review loop limit reached in auto-review mode: deviate instead of routing.
		kind = domain.DeviationReviewLoopLimit
	}
	return domain.EngineDecision{Deviation: &domain.DeviationDecision{
		Info: domain.DeviationInfo{
			Kind:          kind,
			Response:      resp,
			CurrentRow:    currentRowIdx,
			CurrentPhase:  currentRow.Phase,
			CurrentStage:  pos.stage,
			ArtifactState: state,
		},
	}}
}

// findingsRouteBack builds the auto-review loop-back dispatch to the agent named
// by the row's On Findings hint. ok is false when no row above currentRow holds
// that agent, in which case the caller deviates.
func findingsRouteBack(
	in NextInput,
	pos position,
	currentRow domain.RoutingRow,
) (dec domain.EngineDecision, ok bool) {
	targetRowIdx := findNearestPrecedingRowForAgent(in.Workflow, pos.rowIdx, currentRow.OnFindings.Value)
	if targetRowIdx < 0 {
		return domain.EngineDecision{}, false
	}
	var stageNum domain.StageNumber
	var stageStr string
	if currentRow.PhaseParsed.IsStaged {
		stageNum = pos.stageNum
		stageStr = pos.stage
	}
	step, err := buildDispatchStep(in.Workflow, in.Stages, targetRowIdx, stageNum, stageStr,
		in.Agents, in.Seq, in.Now, in.RefreshedStages)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}, true
	}
	// Inject the reviewing agent's output artifacts into the dispatched
	// step's InputArtifacts. Entries already present in the table row's
	// resolved list are not duplicated.
	step.Request.InputArtifacts = injectReviewArtifacts(in.State.RunID,
		step.Request.InputArtifacts, in.LastOutputArtifacts)
	step.Request.InputArtifacts = injectCreatorArtifacts(in.State.RunID,
		step.Request.InputArtifacts, step.Request.OutputArtifacts, in.ArtifactRegistry)
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{
		Steps: []domain.DispatchStep{step},
	}}, true
}

// injectCreatorArtifacts adds the creator agent's own previously-produced
// output artifacts from the registry to inputs. The comparison is against the
// step's resolved output paths (not the raw row output patterns, which may
// contain unresolved template tokens), so injection fires correctly for rows
// with templated output artifact paths.
func injectCreatorArtifacts(
	runID string,
	inputs, resolvedOutputs []string,
	registry []domain.ArtifactRegistryEntry,
) []string {
	if len(registry) == 0 {
		return inputs
	}
	outputSet := make(map[string]bool, len(resolvedOutputs))
	for _, oa := range resolvedOutputs {
		outputSet[domain.StripRunPrefix(runID, oa)] = true
	}
	var creatorArts []string
	for _, entry := range registry {
		if outputSet[domain.StripRunPrefix(runID, entry.Artifact)] {
			creatorArts = append(creatorArts, entry.Artifact)
		}
	}
	if len(creatorArts) == 0 {
		return inputs
	}
	return injectReviewArtifacts(runID, inputs, creatorArts)
}
