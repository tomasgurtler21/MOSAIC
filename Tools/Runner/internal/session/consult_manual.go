package session

import (
	"errors"
	"path/filepath"
	"strings"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// newConsultRequest builds the routing consultation request. Besides the
// orchestration artifact and the current stage set it carries what the manual
// resolver needs and must not read from files itself: the Artifacts registry
// (paths in recorded, unprefixed form) and the row-defaults resolver bound to
// the stage sets in force when the row defaults are asked for.
func newConsultRequest(
	config domain.RunConfig,
	deviation *domain.DeviationInfo,
	state domain.ArtifactState,
	stages, refreshedStages **domain.StageSet,
) domain.ConsultationRequest {
	return domain.ConsultationRequest{
		OrchestrationArtifact: filepath.Join(config.RunFolder, "Orchestration.md"),
		Context:               domain.ConsultContextRouting,
		Deviation:             deviation,
		Stages:                currentStageSet(*stages, *refreshedStages),
		ArtifactRegistry:      recordedRegistry(state),
		RowDefaults: func(row domain.RoutingRow, stage domain.StageNumber) (domain.DispatchDefaults, error) {
			return engine.ResolveRowDefaults(row, stage, *stages, *refreshedStages)
		},
	}
}

// recordedRegistry returns the state's Artifacts registry with the run-folder
// prefix removed from each path. Nil when the registry is empty.
func recordedRegistry(state domain.ArtifactState) []domain.ArtifactRegistryEntry {
	if len(state.ArtifactRegistry) == 0 {
		return nil
	}
	prefix := domain.RunScopedFolder(state.RunID) + "/"
	out := make([]domain.ArtifactRegistryEntry, len(state.ArtifactRegistry))
	for i, e := range state.ArtifactRegistry {
		e.Artifact = strings.TrimPrefix(e.Artifact, prefix)
		out[i] = e
	}
	return out
}

// consultFailureOutcome is the resumable stop for a failed consultation. When
// manual routing ended without a dispatch (user cancel, unavailable
// interaction, exceeded bound) the stop reason names that case.
func consultFailureOutcome(consultErr error) domain.RunOutcome {
	outcome := domain.RunOutcome{
		Status:  domain.RunStoppedByConsultant,
		Message: "consultation failed: " + consultErr.Error(),
		Cause:   consultErr,
	}
	var ce *domain.ConsultationError
	if errors.As(consultErr, &ce) {
		switch ce.Failure {
		case domain.ConsultFailUserAbandoned, domain.ConsultFailInteractionUnavailable, domain.ConsultFailManualBoundExceeded:
			outcome.StopReason = ce.Detail
		}
	}
	return outcome
}
