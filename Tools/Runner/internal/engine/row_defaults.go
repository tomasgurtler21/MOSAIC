package engine

import (
	"errors"
	"fmt"

	"mosaic-run/internal/domain"
)

// Sentinel causes of ResolveRowDefaults stage argument errors.
var (
	ErrRowDefaultsStageRequired       = errors.New("staged row requires a stage")
	ErrRowDefaultsStageNotInSet       = errors.New("stage is not in the stage set")
	ErrRowDefaultsStageOnNonStagedRow = errors.New("non-staged row cannot take a stage")
)

// ResolveRowDefaults resolves the default dispatch payload of row at stage,
// exactly as an engine-routed dispatch does: inputs and outputs through
// ResolveArtifacts, HITL = row.HITL || the plan stage's HITL for a staged row.
// stage is 0 for a non-staged row. Paths are bare (no run prefix).
func ResolveRowDefaults(row domain.RoutingRow, stage domain.StageNumber, stages, refreshedStages *domain.StageSet) (domain.DispatchDefaults, error) {
	stageStr := ""
	if row.PhaseParsed.IsStaged {
		if stage <= 0 {
			return domain.DispatchDefaults{}, ErrRowDefaultsStageRequired
		}
		inSet := false
		if stages != nil {
			_, inSet = stages.Entry(stage)
		}
		if !inSet {
			return domain.DispatchDefaults{}, ErrRowDefaultsStageNotInSet
		}
		stageStr = domain.FormatStageValue(row.PhaseParsed.Group, stage)
	} else if stage != 0 {
		return domain.DispatchDefaults{}, ErrRowDefaultsStageOnNonStagedRow
	}
	return resolveDefaults(row, stage, stageStr, stages, refreshedStages)
}

// resolveDefaults is the one routine behind both the engine's dispatch step and
// ResolveRowDefaults. It tolerates a missing stage set the way the engine
// always has.
func resolveDefaults(row domain.RoutingRow, stageNum domain.StageNumber, stageStr string, stages, refreshedStages *domain.StageSet) (domain.DispatchDefaults, error) {
	hitl := row.HITL
	if row.PhaseParsed.IsStaged && stages != nil && stageNum > 0 {
		if entry, ok := stages.Entry(stageNum); ok {
			hitl = hitl || entry.HITL
		}
	}
	inputs, err := ResolveArtifacts(row.InputArtifacts, stageNum, stageStr, stages, refreshedStages, true)
	if err != nil {
		return domain.DispatchDefaults{}, fmt.Errorf("row for agent %q: input artifacts: %w", row.Agent, err)
	}
	outputs, err := ResolveArtifacts(row.OutputArtifacts, stageNum, stageStr, stages, refreshedStages, false)
	if err != nil {
		return domain.DispatchDefaults{}, fmt.Errorf("row for agent %q: output artifacts: %w", row.Agent, err)
	}
	return domain.DispatchDefaults{InputArtifacts: inputs, OutputArtifacts: outputs, HITL: hitl}, nil
}
