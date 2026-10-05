package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// NewFileStore creates a new file-based ArtifactStore that reads from and
// writes to the Orchestration.md file at the given path.
//
// Writes are atomic: the implementation uses write-to-temp-then-rename (FR-34).
func NewFileStore(path string) domain.ArtifactStore {
	return &fileStore{path: path}
}

// IsRunScopedArtifactPath reports whether path's parent directory is a
// run-scoped orchestration folder, i.e. named "Orchestration-{run_id}" where
// run_id satisfies domain.IsValidRunID.
//
// Reports false for a relative path, for a parent that is not an
// Orchestration-* folder, and for an Orchestration-* folder whose suffix is not
// a canonical run_id.
//
// This is a predicate only. Non-run-scoped absolute paths remain legal for
// Create (many existing tests build stores under t.TempDir()); the predicate
// exists so the condition can be logged without changing behaviour.
//
// Its sole production consumer is newLoggedArtifactStore in cmd/mosaic-run,
// which uses it to decide whether to emit EventArtifactPathNonRunScoped. The
// artifact package itself never logs and never takes a DebugLogger.
func IsRunScopedArtifactPath(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	parentDir := filepath.Base(filepath.Dir(path))
	runID, ok := domain.ParseRunFolder(parentDir)
	if !ok {
		return false
	}
	return domain.IsValidRunID(runID)
}

// fileStore is the file-backed implementation of domain.ArtifactStore.
type fileStore struct {
	path string
}

func (f *fileStore) Read(ctx context.Context) (domain.ArtifactState, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ArtifactState{}, os.ErrNotExist
		}
		return domain.ArtifactState{}, err
	}
	state, err := Parse(data)
	if err != nil {
		// Set the Resource to the file path in RefusalErrors.
		var re *domain.RefusalError
		if errors.As(err, &re) {
			re.Resource = f.path
		}
		return domain.ArtifactState{}, err
	}
	return state, nil
}

func (f *fileStore) Create(ctx context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	// Every artifact carries a valid run_id; nothing is written otherwise.
	if !domain.IsValidRunID(runID) {
		return domain.ArtifactState{}, &domain.RefusalError{
			Component: "artifact",
			Resource:  f.path,
			Reason:    "cannot create an artifact without a valid run_id",
			Cause:     &domain.RunIdentityError{Problem: runIDProblem(runID), RunID: runID},
		}
	}

	// Reject a non-absolute store path before any filesystem side effects.
	// A relative path would land in the process CWD, never in the intended
	// run-scoped folder. Absolute non-run-scoped paths are permitted (many
	// tests build stores under t.TempDir()).
	if !filepath.IsAbs(f.path) {
		return domain.ArtifactState{}, fmt.Errorf("artifact store path must be absolute, got %q", f.path)
	}

	// Fail if file already exists.
	if _, err := os.Stat(f.path); err == nil {
		return domain.ArtifactState{}, fmt.Errorf("artifact: file already exists at %s", f.path)
	}

	state := domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now.UTC(),
		LastUpdated:     now.UTC(),
		GlobalSequence:  0,
		RunSettings:     settings,
	}

	data, err := Render(state)
	if err != nil {
		return domain.ArtifactState{}, err
	}

	// Ensure the run-scoped folder exists before writing. The folder
	// (e.g. Orchestration-{run_id}/) is never created by the caller;
	// Create is responsible for initialising the entire run directory.
	if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
		return domain.ArtifactState{}, err
	}

	if err := os.WriteFile(f.path, data, 0644); err != nil {
		return domain.ArtifactState{}, err
	}

	return state, nil
}

func (f *fileStore) Apply(ctx context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	// Sequence numbers are strictly increasing and name their invocation.
	for _, e := range state.ExecutionLog {
		if step.Seq <= e.Seq {
			return domain.ArtifactState{}, fmt.Errorf("artifact: step seq %d is not above logged seq %d", step.Seq, e.Seq)
		}
	}
	if !strings.HasSuffix(step.AgentInstance, "#"+strconv.Itoa(step.Seq)) {
		return domain.ArtifactState{}, fmt.Errorf("artifact: agent instance %q does not end in #%d", step.AgentInstance, step.Seq)
	}

	// Build the new execution log entry.
	newEntry := domain.ExecutionLogEntry{
		Seq:         step.Seq,
		Agent:       step.AgentInstance,
		Phase:       step.Phase,
		Stage:       step.Stage,
		WorkflowRow: step.WorkflowRow,
		Status:      step.Status,
		Timestamp:   step.Timestamp,
		Summary:     TruncateSummary(step.Summary),
		Inputs:      step.Inputs,
		Checkpoint:  step.Checkpoint,
	}

	// Build the new state.
	newState := state
	newState.ExecutionLog = append(append([]domain.ExecutionLogEntry(nil), state.ExecutionLog...), newEntry)
	// global_sequence holds the last allocated number. A higher stored value
	// (an interrupted allocation) is kept.
	if step.Seq > newState.GlobalSequence {
		newState.GlobalSequence = step.Seq
	}
	newState.LastUpdated = step.Timestamp

	// current_state is updated only for workflow steps. An infrastructure
	// step leaves phase, stage, last_status, last_agent, and error_code
	// exactly as they were, on disk as well as in the returned state, so the
	// recorded workflow position continues to name the last workflow step.
	// The invocation is still fully recorded above and below: the execution
	// log entry, sequence bump, and artifact registry upsert all apply to an
	// infrastructure step unchanged.
	if !step.IsInfrastructure {
		newState.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.RoutedStatus(),
			LastAgent:  step.AgentInstance,
			ErrorCode:  step.RoutedErrorCode(),
		}
	}

	// Upsert artifact registry entries. A key and its run-folder-prefixed form
	// denote the same artifact.
	registry := append([]domain.ArtifactRegistryEntry(nil), state.ArtifactRegistry...)
	createdIn := step.Phase
	if step.Stage != "" {
		createdIn = step.Phase + "." + step.Stage
	}
	for _, art := range step.WrittenArtifacts {
		entry := domain.ArtifactRegistryEntry{
			Artifact:  art,
			CreatedIn: createdIn,
			CreatedBy: step.AgentInstance,
		}
		registry = upsertRegistry(state.RunID, registry, entry)
	}
	newState.ArtifactRegistry = registry

	// Render and write atomically.
	data, err := Render(newState)
	if err != nil {
		return domain.ArtifactState{}, err
	}
	if err := atomicWrite(f.path, data); err != nil {
		return domain.ArtifactState{}, err
	}

	return newState, nil
}

// registryKey removes the "Orchestration-{runID}/" prefix from an artifact path
// so that prefixed and unprefixed forms compare equal.
func registryKey(runID, artifact string) string {
	if runID == "" {
		return artifact
	}
	return strings.TrimPrefix(artifact, "Orchestration-"+runID+"/")
}

// upsertRegistry replaces the entry for entry.Artifact in place at its first
// occurrence, removes any later duplicates of the same key, or appends the
// entry when the key is new.
func upsertRegistry(runID string, registry []domain.ArtifactRegistryEntry, entry domain.ArtifactRegistryEntry) []domain.ArtifactRegistryEntry {
	out := make([]domain.ArtifactRegistryEntry, 0, len(registry)+1)
	replaced := false
	for _, e := range registry {
		if registryKey(runID, e.Artifact) != registryKey(runID, entry.Artifact) {
			out = append(out, e)
			continue
		}
		if !replaced {
			out = append(out, entry)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, entry)
	}
	return out
}

// atomicWrite writes data to path atomically using write-to-temp-then-rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-orchestration-*.md")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmpName, path)
}
