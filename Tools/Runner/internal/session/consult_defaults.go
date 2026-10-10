package session

import (
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// consultPayload is the artifact and HITL payload of a consultation-routed
// dispatch, before run-scoping.
type consultPayload struct {
	Inputs  []string
	Outputs []string
	HITL    bool
}

// consultOutputs carries the outputs written by the latest consultation-routed
// step to the dispatch loop, so the next engine decision sees them. set is
// false when no consultation-routed step has been applied since the loop last
// took them.
type consultOutputs struct {
	set   bool
	paths []string
}

// resolveConsultPayload builds the payload of a consultation-routed dispatch.
// An instruction field left unset takes the row default resolved by the same
// routine the engine uses for that row and stage; an explicit field passes
// through verbatim. effectiveStage is the recorded stage value, empty for a
// non-staged row.
func resolveConsultPayload(
	dispInstr *domain.DispatchInstruction,
	row domain.RoutingRow,
	effectiveStage string,
	stages, refreshedStages *domain.StageSet,
) (consultPayload, error) {
	var stageNum domain.StageNumber
	if row.PhaseParsed.IsStaged && effectiveStage != "" {
		if _, n, ok := domain.ParseStageValue(effectiveStage); ok {
			stageNum = n
		}
	}
	defaults, err := engine.ResolveRowDefaults(row, stageNum, stages, refreshedStages)
	if err != nil {
		return consultPayload{}, err
	}
	payload := consultPayload{Inputs: defaults.InputArtifacts, Outputs: defaults.OutputArtifacts, HITL: defaults.HITL}
	if dispInstr.InputArtifacts != nil {
		payload.Inputs = *dispInstr.InputArtifacts
	}
	if dispInstr.OutputArtifacts != nil {
		payload.Outputs = *dispInstr.OutputArtifacts
	}
	if dispInstr.HITLOverride != nil {
		payload.HITL = *dispInstr.HITLOverride
	}
	return payload, nil
}
