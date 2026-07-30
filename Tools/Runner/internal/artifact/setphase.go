package artifact

import (
	"context"
	"fmt"
	"os"
	"time"

	"mosaic-run/internal/domain"
)

// SetPhase updates only current_state.phase (and bumps last_updated and
// global_sequence) without appending an execution log entry or modifying
// the artifact registry. The write is atomic (write-temp-then-rename).
//
// This is the designated path for writing the COMPLETED phase marker after
// the session finishes. Using Apply for this purpose would require a
// synthetic CompletedStep with meaningless values, producing a spurious
// execution log row.
//
// The implementation re-reads the current artifact from disk so that the
// caller may pass any ArtifactState (including a zero value) without risk
// of overwriting data that may have been written since the caller's last
// reference.
func (f *fileStore) SetPhase(_ context.Context, _ domain.ArtifactState, phase string, now time.Time) (domain.ArtifactState, error) {
	// Re-read current state from disk to ensure we have the latest version.
	data, err := readFile(f.path)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetPhase: read: %w", err)
	}

	current, err := Parse(data)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetPhase: parse: %w", err)
	}

	// Apply the phase update.
	current.CurrentState.Phase = phase
	current.GlobalSequence++
	current.LastUpdated = now.UTC()

	// Render and write atomically.
	rendered, err := Render(current)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetPhase: render: %w", err)
	}
	if err := atomicWrite(f.path, rendered); err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetPhase: write: %w", err)
	}

	return current, nil
}

// readFile reads the bytes at path, returning the raw data.
// This is a thin helper so that SetPhase can re-read the artifact without
// going through the full fileStore.Read path (which adds the file path to
// RefusalError; we set it ourselves in SetPhase's callers if needed).
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
