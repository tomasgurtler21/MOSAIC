package artifact_test

// Shared test helpers, fixture builders, and constants used across the artifact
// test suite. Functions and constants here are referenced by multiple test files.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// fixturePath returns the absolute path to a named fixture file.
func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", "artifact", name)
}

// asRefusalError asserts that err is (or wraps) a *domain.RefusalError.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// mustReadCanonical parses the canonical.md fixture and returns the ArtifactState.
func mustReadCanonical(t *testing.T) domain.ArtifactState {
	t.Helper()
	data, err := os.ReadFile(fixturePath("canonical.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse(canonical.md): unexpected error: %v", err)
	}
	return state
}

// newTestStep builds a CompletedStep for use in Apply tests.
func newTestStep(seq int, agent, phase, stage string, status domain.StatusCode, ts time.Time, artifacts []string) domain.CompletedStep {
	return domain.CompletedStep{
		Seq:             seq,
		AgentInstance:   agent,
		Phase:           phase,
		Stage:           stage,
		Status:          status,
		Timestamp:       ts,
		Summary:         "step summary",
		OutputArtifacts: artifacts,
	}
}

// mustCreateStore creates a temp artifact and returns the store and initial state.
func mustCreateStore(t *testing.T) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", domain.RunSettings{}, time.Now(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, state
}

// setPhaseFixture creates a temporary artifact via store.Create and returns
// the store and initial state for use in SetPhase tests.
func setPhaseFixture(t *testing.T) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", domain.RunSettings{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatalf("setPhaseFixture: Create: %v", err)
	}
	return store, state
}

// newTestFixtureWithRunID creates a temp artifact with the given runID and returns
// the store and the initial state for use in Create+runID tests.
func newTestFixtureWithRunID(t *testing.T, runID string) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", domain.RunSettings{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), runID)
	if err != nil {
		t.Fatalf("newTestFixtureWithRunID: Create: %v", err)
	}
	return store, state
}

// minimalArtifactBytes builds the minimal valid artifact bytes for use in
// inline parse tests. When runID is non-empty, it is placed after "type:" and
// before "workflow:" in the frontmatter. When runID is empty, the run_id line
// is omitted entirely (simulating a pre-v1.8 artifact).
func minimalArtifactBytes(runID string) []byte {
	runIDLine := ""
	if runID != "" {
		runIDLine = "run_id: " + runID + "\n"
	}
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		runIDLine +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		"checkpoints: disabled\n" +
		"current_state:\n" +
		"  phase: null\n" +
		"  stage: null\n" +
		"  last_status: null\n" +
		"  last_agent: null\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"</WorkflowNotes>\n")
}

// minimalArtifactWithExecutionRow builds minimal valid artifact bytes that
// contain one execution log row. The inputs parameter is the raw cell value
// to insert in the Inputs column; pass "" to render it as "-".
func minimalArtifactWithExecutionRow(inputs string) []byte {
	cell := inputs
	if cell == "" {
		cell = "-"
	}
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T01:00:00Z\n" +
		"global_sequence: 1\n" +
		"checkpoints: disabled\n" +
		"current_state:\n" +
		"  phase: PLANNING\n" +
		"  stage: null\n" +
		"  last_status: SUCCESS\n" +
		"  last_agent: \"planner#1\"\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary      | Inputs | Checkpoint |\n" +
		"| --- | --------- | -------- | ----- | ------- | -------------------- | ------------ | ------ | ---------- |\n" +
		"| 1   | planner#1 | PLANNING | -     | SUCCESS | 2026-01-01T01:00:00Z | Plan created | " + cell + " | - |\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"| Artifact | Created In | Created By |\n" +
		"| -------- | ---------- | ---------- |\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"| Seq | Note |\n" +
		"| --- | ---- |\n" +
		"</WorkflowNotes>\n")
}

// minimalArtifactWithSingleOverrideBytes builds minimal valid artifact bytes
// that contain one infrastructure_overrides entry for "checkpoint-manager-git"
// with a single STAGE_END trigger and no trigger_param.
func minimalArtifactWithSingleOverrideBytes() []byte {
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		"checkpoints: disabled\n" +
		"infrastructure_overrides:\n" +
		"  checkpoint-manager-git:\n" +
		"    triggers:\n" +
		"      - trigger: STAGE_END\n" +
		"current_state:\n" +
		"  phase: null\n" +
		"  stage: null\n" +
		"  last_status: null\n" +
		"  last_agent: null\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"</WorkflowNotes>\n")
}

// minimalArtifactWithMultiTriggerOverrideBytes builds minimal valid artifact
// bytes that contain one infrastructure_overrides entry for
// "orchestration-review" with two triggers: one with and one without a param.
func minimalArtifactWithMultiTriggerOverrideBytes() []byte {
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		"checkpoints: disabled\n" +
		"infrastructure_overrides:\n" +
		"  orchestration-review:\n" +
		"    triggers:\n" +
		"      - trigger: INVOCATION_INTERVAL\n" +
		"        trigger_param: 15\n" +
		"      - trigger: PHASE_END\n" +
		"current_state:\n" +
		"  phase: null\n" +
		"  stage: null\n" +
		"  last_status: null\n" +
		"  last_agent: null\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"</WorkflowNotes>\n")
}

// minimalArtifactWithConfigBytes builds a canonical artifact document with
// configLines inserted after global_sequence and before current_state. Callers
// must supply any checkpoints, commits, mode, or other frontmatter lines they
// need; this function does not inject defaults.
func minimalArtifactWithConfigBytes(configLines string) []byte {
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test task\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		configLines +
		"current_state:\n" +
		"  phase: null\n" +
		"  stage: null\n" +
		"  last_status: null\n" +
		"  last_agent: null\n" +
		"  error_code: null\n" +
		"---\n" +
		"\n" +
		"<ExecutionLog type=\"core\">\n" +
		"</ExecutionLog>\n" +
		"\n" +
		"<Artifacts type=\"core\">\n" +
		"</Artifacts>\n" +
		"\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"</WorkflowNotes>\n")
}

// standardConfigLines is the baseline set of config lines that represent a
// known-good (non-malformed) minimal configuration, used as the starting
// point for tests that override only the field under test.
const standardConfigLines = "checkpoints: disabled\ncommits: disabled\n"

// mustBeAbsoluteSubstring is the stable substring asserted in the error message
// from Create when given a non-absolute store path.
const mustBeAbsoluteSubstring = "must be absolute"
