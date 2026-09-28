package artifact

import (
	"context"
	"fmt"
	"time"

	"mosaic-run/internal/domain"
)

// AdoptRunnerSettings records runner_mode, runner_pre_consultation and
// runner_manual_resolution once, on the first Runner resume of a
// native-created artifact. See domain.ArtifactStore.AdoptRunnerSettings.
func (f *fileStore) AdoptRunnerSettings(_ context.Context, mode domain.ExecutionMode, preConsultation, manualResolution bool, now time.Time) (domain.ArtifactState, error) {
	refuse := func(reason string) error {
		return &domain.RefusalError{Component: "artifact", Resource: f.path, Reason: reason}
	}
	if mode == domain.ExecutionModeUnset {
		return domain.ArtifactState{}, refuse("cannot record runner settings without a runner_mode")
	}

	data, err := readFile(f.path)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("AdoptRunnerSettings: read: %w", err)
	}
	current, err := Parse(data)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("AdoptRunnerSettings: parse: %w", err)
	}
	if current.Mode != domain.ExecutionModeUnset {
		return domain.ArtifactState{}, refuse(fmt.Sprintf("runner_mode is already recorded as %q; runner settings are set once", current.Mode))
	}

	current.Mode = mode
	current.PreConsultation = preConsultation
	current.ManualResolution = manualResolution
	current.LastUpdated = now.UTC()

	rendered, err := Render(current)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("AdoptRunnerSettings: render: %w", err)
	}
	if err := atomicWrite(f.path, rendered); err != nil {
		return domain.ArtifactState{}, fmt.Errorf("AdoptRunnerSettings: write: %w", err)
	}
	return current, nil
}
