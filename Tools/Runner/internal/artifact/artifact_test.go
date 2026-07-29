package artifact_test

// Tests for the artifact package.
//
// Coverage:
//
//   Parse — happy path from canonical golden fixture:
//   - Frontmatter fields: type, workflow, workflow_version, task, started,
//     last_updated, global_sequence, checkpoints.
//   - CurrentState fields: phase, stage, last_status, last_agent, error_code.
//   - ExecutionLog: row count, first row's Seq/Agent/Phase/Stage/Status/Timestamp/Summary.
//   - Empty-value convention: "-" in Stage column → "" in ArtifactState (and vice versa).
//   - Staged Stage value ("Stage-1") preserved as-is.
//   - ArtifactRegistry: row count, first row's Artifact/CreatedIn/CreatedBy.
//   - WorkflowNotes: count, first note's Seq and text preserved verbatim.
//
//   Parse — refusal cases:
//   - Non-existent file → os.ErrNotExist (tested via Read, not Parse directly).
//   - Missing "type: orchestration-artifact" → *domain.RefusalError.
//   - Old template format (no [[SECTION:...]] tags) → *domain.RefusalError.
//   - Missing [[SECTION:ExecutionLog]] section → *domain.RefusalError.
//   - Missing [[SECTION:Artifacts]] section → *domain.RefusalError.
//   - Truncated file → *domain.RefusalError.
//   - RefusalError.Component must be "artifact".
//   - RefusalError.Resource must name the file path.
//
//   Create:
//   - WorkflowID appears in the returned ArtifactState.
//   - WorkflowVersion appears in the returned ArtifactState.
//   - Task appears in the returned ArtifactState.
//   - Checkpoints=true is recorded.
//   - Checkpoints=false is recorded.
//   - ExecutionLog is empty for a new artifact.
//   - ArtifactRegistry is empty for a new artifact.
//   - Creating a second artifact at the same path returns an error.
//   - The created file can be read back via Read with consistent state.
//
//   Apply:
//   - ExecutionLog grows by one row after Apply.
//   - ExecutionLog entry Seq matches the step's Seq.
//   - ExecutionLog entry Agent matches the step's AgentInstance.
//   - ExecutionLog entry Phase matches the step's Phase.
//   - Stage in execution log entry is "Stage-N" when step.Stage is "Stage-N".
//   - Stage in execution log entry is "" (rendered as "-") when step.Stage is "".
//   - ArtifactRegistry gains an entry for each output artifact in the step.
//   - ArtifactRegistry upserts (updates) an existing entry when the artifact path appears again.
//   - CurrentState.Phase is updated to the step's Phase.
//   - CurrentState.Stage is updated to the step's Stage.
//   - CurrentState.LastStatus is updated to the step's Status.
//   - CurrentState.LastAgent is updated to the step's AgentInstance.
//   - GlobalSequence in the returned state is incremented.
//   - LastUpdated is updated to the step's Timestamp.
//   - WorkflowNotes are preserved unchanged after Apply (FR-14a).
//
//   Render / round-trip:
//   - Parse → Render produces byte-identical output for the canonical fixture.
//
//   TruncateSummary:
//   - Messages of 100 characters or fewer are returned unchanged.
//   - Exactly 100 characters is not truncated.
//   - Messages longer than 100 characters become head-50 + " … " + tail-50.
//   - Pipe characters ("|") are stripped from the result.
//   - Newline characters are stripped from the result.
//   - The head/tail split is counted on the clean (stripped) string.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// ---- Parse: frontmatter happy path ----

func TestParse_CanonicalFile_Type(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Type != "orchestration-artifact" {
		t.Errorf("Type: want %q, got %q", "orchestration-artifact", state.Type)
	}
}

func TestParse_CanonicalFile_WorkflowID(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Workflow != domain.WorkflowID("quick-fix") {
		t.Errorf("Workflow: want %q, got %q", "quick-fix", state.Workflow)
	}
}

func TestParse_CanonicalFile_WorkflowVersion(t *testing.T) {
	state := mustReadCanonical(t)

	if state.WorkflowVersion != domain.WorkflowVersion("3.0") {
		t.Errorf("WorkflowVersion: want %q, got %q", "3.0", state.WorkflowVersion)
	}
}

func TestParse_CanonicalFile_Task(t *testing.T) {
	state := mustReadCanonical(t)

	if state.Task != "Fix the authentication timeout bug" {
		t.Errorf("Task: want %q, got %q", "Fix the authentication timeout bug", state.Task)
	}
}

func TestParse_CanonicalFile_Started(t *testing.T) {
	state := mustReadCanonical(t)

	want := time.Date(2026, 1, 29, 9, 0, 0, 0, time.UTC)
	if !state.Started.Equal(want) {
		t.Errorf("Started: want %v, got %v", want, state.Started)
	}
}

func TestParse_CanonicalFile_GlobalSequence(t *testing.T) {
	state := mustReadCanonical(t)

	if state.GlobalSequence != 2 {
		t.Errorf("GlobalSequence: want 2, got %d", state.GlobalSequence)
	}
}

func TestParse_CanonicalFile_CheckpointsTrue(t *testing.T) {
	state := mustReadCanonical(t)

	// canonical.md has checkpoints: enabled
	if !state.Checkpoints {
		t.Error("Checkpoints: want true (enabled), got false")
	}
}

func TestParse_CanonicalFile_LastUpdated(t *testing.T) {
	state := mustReadCanonical(t)

	want := time.Date(2026, 1, 29, 10, 0, 0, 0, time.UTC)
	if !state.LastUpdated.Equal(want) {
		t.Errorf("LastUpdated: want %v, got %v", want, state.LastUpdated)
	}
}

// ---- Parse: CurrentState happy path ----

func TestParse_CanonicalFile_CurrentState_Phase(t *testing.T) {
	state := mustReadCanonical(t)

	if state.CurrentState.Phase != "EXECUTION" {
		t.Errorf("CurrentState.Phase: want %q, got %q", "EXECUTION", state.CurrentState.Phase)
	}
}

func TestParse_CanonicalFile_CurrentState_Stage(t *testing.T) {
	state := mustReadCanonical(t)

	if state.CurrentState.Stage != "Stage-1" {
		t.Errorf("CurrentState.Stage: want %q, got %q", "Stage-1", state.CurrentState.Stage)
	}
}

func TestParse_CanonicalFile_CurrentState_LastStatus(t *testing.T) {
	state := mustReadCanonical(t)

	if state.CurrentState.LastStatus != domain.StatusSUCCESS {
		t.Errorf("CurrentState.LastStatus: want %q, got %q", domain.StatusSUCCESS, state.CurrentState.LastStatus)
	}
}

func TestParse_CanonicalFile_CurrentState_LastAgent(t *testing.T) {
	state := mustReadCanonical(t)

	if state.CurrentState.LastAgent != "implementation-tdd#2" {
		t.Errorf("CurrentState.LastAgent: want %q, got %q", "implementation-tdd#2", state.CurrentState.LastAgent)
	}
}

func TestParse_CanonicalFile_CurrentState_ErrorCode_EmptyWhenNull(t *testing.T) {
	state := mustReadCanonical(t)

	// error_code: null in YAML → "" (ErrorNone) in struct
	if state.CurrentState.ErrorCode != domain.ErrorNone {
		t.Errorf("CurrentState.ErrorCode: want %q (null), got %q", domain.ErrorNone, state.CurrentState.ErrorCode)
	}
}

// ---- Parse: ExecutionLog happy path ----

func TestParse_CanonicalFile_ExecutionLog_RowCount(t *testing.T) {
	state := mustReadCanonical(t)

	if len(state.ExecutionLog) != 2 {
		t.Errorf("ExecutionLog: want 2 rows, got %d", len(state.ExecutionLog))
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Seq(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Seq != 1 {
		t.Errorf("ExecutionLog[0].Seq: want 1, got %d", state.ExecutionLog[0].Seq)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Agent(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Agent != "planner-tdd-soft#1" {
		t.Errorf("ExecutionLog[0].Agent: want %q, got %q", "planner-tdd-soft#1", state.ExecutionLog[0].Agent)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Phase(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Phase != "PLANNING" {
		t.Errorf("ExecutionLog[0].Phase: want %q, got %q", "PLANNING", state.ExecutionLog[0].Phase)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_DashStage_ReturnsEmptyString(t *testing.T) {
	// Row 0 has Stage="-" in the table (non-EXECUTION phase, no stage).
	// The empty-value convention translates "-" → "" in ArtifactState.
	// No other component ever sees or produces "-".
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Stage != "" {
		t.Errorf("ExecutionLog[0].Stage: want %q for dash cell, got %q", "", state.ExecutionLog[0].Stage)
	}
}

func TestParse_CanonicalFile_ExecutionLog_SecondRow_StagedValue_Preserved(t *testing.T) {
	// Row 1 has Stage="Stage-1" (EXECUTION phase, staged).
	// The value must be preserved as-is in ArtifactState.
	state := mustReadCanonical(t)

	if state.ExecutionLog[1].Stage != "Stage-1" {
		t.Errorf("ExecutionLog[1].Stage: want %q, got %q", "Stage-1", state.ExecutionLog[1].Stage)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Status(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Status != domain.StatusSUCCESS {
		t.Errorf("ExecutionLog[0].Status: want %q, got %q", domain.StatusSUCCESS, state.ExecutionLog[0].Status)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Timestamp(t *testing.T) {
	state := mustReadCanonical(t)

	want := time.Date(2026, 1, 29, 9, 5, 0, 0, time.UTC)
	if !state.ExecutionLog[0].Timestamp.Equal(want) {
		t.Errorf("ExecutionLog[0].Timestamp: want %v, got %v", want, state.ExecutionLog[0].Timestamp)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_Summary(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Summary != "Plan created" {
		t.Errorf("ExecutionLog[0].Summary: want %q, got %q", "Plan created", state.ExecutionLog[0].Summary)
	}
}

func TestParse_CanonicalFile_ExecutionLog_FirstRow_DashCheckpoint_ReturnsEmptyString(t *testing.T) {
	// Checkpoint "-" in the table → "" in ArtifactState (same empty-value convention as Stage).
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Checkpoint != "" {
		t.Errorf("ExecutionLog[0].Checkpoint: want %q for dash cell, got %q", "", state.ExecutionLog[0].Checkpoint)
	}
}

// ---- Parse: ArtifactRegistry happy path ----

func TestParse_CanonicalFile_ArtifactRegistry_RowCount(t *testing.T) {
	state := mustReadCanonical(t)

	if len(state.ArtifactRegistry) != 3 {
		t.Errorf("ArtifactRegistry: want 3 entries, got %d", len(state.ArtifactRegistry))
	}
}

func TestParse_CanonicalFile_ArtifactRegistry_FirstEntry_Artifact(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ArtifactRegistry[0].Artifact != "Plan.md" {
		t.Errorf("ArtifactRegistry[0].Artifact: want %q, got %q", "Plan.md", state.ArtifactRegistry[0].Artifact)
	}
}

func TestParse_CanonicalFile_ArtifactRegistry_FirstEntry_CreatedIn(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ArtifactRegistry[0].CreatedIn != "PLANNING" {
		t.Errorf("ArtifactRegistry[0].CreatedIn: want %q, got %q", "PLANNING", state.ArtifactRegistry[0].CreatedIn)
	}
}

func TestParse_CanonicalFile_ArtifactRegistry_FirstEntry_CreatedBy(t *testing.T) {
	state := mustReadCanonical(t)

	if state.ArtifactRegistry[0].CreatedBy != "planner-tdd-soft#1" {
		t.Errorf("ArtifactRegistry[0].CreatedBy: want %q, got %q", "planner-tdd-soft#1", state.ArtifactRegistry[0].CreatedBy)
	}
}

func TestParse_CanonicalFile_ArtifactRegistry_StagedEntry_CreatedIn(t *testing.T) {
	// Entry for Stage-1/PlanProgress.md has CreatedIn="EXECUTION.Stage-1".
	// This "Phase.Stage" notation is stored verbatim, not split.
	state := mustReadCanonical(t)

	var found bool
	for _, e := range state.ArtifactRegistry {
		if e.Artifact == "Stage-1/PlanProgress.md" {
			found = true
			if e.CreatedIn != "EXECUTION.Stage-1" {
				t.Errorf("ArtifactRegistry[Stage-1/PlanProgress.md].CreatedIn: want %q, got %q", "EXECUTION.Stage-1", e.CreatedIn)
			}
		}
	}
	if !found {
		t.Error("ArtifactRegistry: entry for Stage-1/PlanProgress.md not found")
	}
}

// ---- Parse: WorkflowNotes happy path ----

func TestParse_CanonicalFile_WorkflowNotes_Count(t *testing.T) {
	state := mustReadCanonical(t)

	if len(state.WorkflowNotes) != 1 {
		t.Errorf("WorkflowNotes: want 1 note, got %d", len(state.WorkflowNotes))
	}
}

func TestParse_CanonicalFile_WorkflowNotes_Seq(t *testing.T) {
	state := mustReadCanonical(t)

	if state.WorkflowNotes[0].Seq != 1 {
		t.Errorf("WorkflowNotes[0].Seq: want 1, got %d", state.WorkflowNotes[0].Seq)
	}
}

func TestParse_CanonicalFile_WorkflowNotes_NoteText(t *testing.T) {
	state := mustReadCanonical(t)

	if state.WorkflowNotes[0].Note != "Timeout value is 30s per RFC-1234" {
		t.Errorf("WorkflowNotes[0].Note: want %q, got %q", "Timeout value is 30s per RFC-1234", state.WorkflowNotes[0].Note)
	}
}

// ---- Parse: refusal cases ----

func TestParse_MissingTypeField_ReturnsError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when type field is missing")
	}
}

func TestParse_MissingTypeField_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	asRefusalError(t, err)
}

func TestParse_OldTemplateFormat_ReturnsRefusalError(t *testing.T) {
	// Old template format uses markdown headings instead of [[SECTION:...]] tags
	// and may have the "Subgent" typo.  Both signatures indicate a non-canonical file.
	data, err := os.ReadFile(fixturePath("old-template.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error for the old template format")
	}
	asRefusalError(t, err)
}

func TestParse_MissingExecutionLogSection_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("missing-exec-log.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when [[SECTION:ExecutionLog]] is absent")
	}
	asRefusalError(t, err)
}

func TestParse_MissingArtifactsSection_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("missing-artifacts-section.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error when [[SECTION:Artifacts]] is absent")
	}
	asRefusalError(t, err)
}

func TestParse_TruncatedFile_ReturnsRefusalError(t *testing.T) {
	data, err := os.ReadFile(fixturePath("truncated.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse must return an error for a truncated file")
	}
	asRefusalError(t, err)
}

func TestParse_RefusalError_ComponentIsArtifact(t *testing.T) {
	// Every refusal from artifact.Parse must name "artifact" as the component.
	data, err := os.ReadFile(fixturePath("no-type.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	re := asRefusalError(t, err)
	if re.Component != "artifact" {
		t.Errorf("RefusalError.Component: want %q, got %q", "artifact", re.Component)
	}
}

// ---- Read: file-based ArtifactStore ----

func TestRead_NonExistentFile_ReturnsErrNotExist(t *testing.T) {
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "does-not-exist.md"))
	ctx := context.Background()

	_, err := store.Read(ctx)

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read on non-existent file: want os.ErrNotExist, got %v", err)
	}
}

func TestRead_NonCanonicalFile_ReturnsRefusalError(t *testing.T) {
	// A file that exists but is not canonical must return a RefusalError, not os.ErrNotExist.
	store := artifact.NewFileStore(fixturePath("no-type.md"))
	ctx := context.Background()

	_, err := store.Read(ctx)

	if err == nil {
		t.Fatal("Read must return an error for a non-canonical file")
	}
	asRefusalError(t, err)
}

func TestRead_RefusalError_ResourceNamesFilePath(t *testing.T) {
	// RefusalError.Resource must name the file path so the user can locate the problem.
	path := fixturePath("no-type.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()

	_, err := store.Read(ctx)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the file path, not be empty")
	}
}

// ---- Create ----

func TestCreate_WorkflowID_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "some task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Workflow != domain.WorkflowID("quick-fix") {
		t.Errorf("state.Workflow: want %q, got %q", "quick-fix", state.Workflow)
	}
}

func TestCreate_WorkflowVersion_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "some task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.WorkflowVersion != domain.WorkflowVersion("3.0") {
		t.Errorf("state.WorkflowVersion: want %q, got %q", "3.0", state.WorkflowVersion)
	}
}

func TestCreate_Task_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "My important task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Task != "My important task" {
		t.Errorf("state.Task: want %q, got %q", "My important task", state.Task)
	}
}

func TestCreate_CheckpointsEnabled_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", true, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if !state.Checkpoints {
		t.Error("state.Checkpoints: want true (enabled), got false")
	}
}

func TestCreate_CheckpointsDisabled_SetInState(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Checkpoints {
		t.Error("state.Checkpoints: want false (disabled), got true")
	}
}

func TestCreate_Type_SetInState(t *testing.T) {
	// A freshly created artifact must have Type="orchestration-artifact" so that
	// it passes its own Parse check when read back (the "type" field is required
	// for canonical format identification).
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.Type != "orchestration-artifact" {
		t.Errorf("state.Type: want %q, got %q", "orchestration-artifact", state.Type)
	}
}

func TestCreate_Started_SetFromNowParameter(t *testing.T) {
	// The now parameter passed to Create must appear as state.Started.
	// This allows the caller to control the start timestamp precisely.
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	state, err := store.Create(ctx, info, "task", false, now)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if !state.Started.Equal(now) {
		t.Errorf("state.Started: want %v (now parameter), got %v", now, state.Started)
	}
}

func TestCreate_GlobalSequence_InitiallyZero(t *testing.T) {
	// A new artifact has no completed steps, so global_sequence starts at 0.
	// Each Apply call increments it; the zero value signals "no invocations yet".
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if state.GlobalSequence != 0 {
		t.Errorf("state.GlobalSequence: want 0 for new artifact, got %d", state.GlobalSequence)
	}
}

func TestCreate_ExecutionLog_Empty(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if len(state.ExecutionLog) != 0 {
		t.Errorf("ExecutionLog: want empty on new artifact, got %d entries", len(state.ExecutionLog))
	}
}

func TestCreate_ArtifactRegistry_Empty(t *testing.T) {
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	state, err := store.Create(ctx, info, "task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	if len(state.ArtifactRegistry) != 0 {
		t.Errorf("ArtifactRegistry: want empty on new artifact, got %d entries", len(state.ArtifactRegistry))
	}
}

func TestCreate_FailsIfFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	// Create the file once.
	if _, err := store.Create(ctx, info, "first", false, time.Now()); err != nil {
		t.Fatalf("first Create: unexpected error: %v", err)
	}

	// A second Create on the same path must fail.
	_, err := store.Create(ctx, info, "second", false, time.Now())
	if err == nil {
		t.Fatal("second Create: want error because file already exists, got nil")
	}
}

func TestCreate_FileReadableAfterCreate(t *testing.T) {
	// Create then immediately Read must produce consistent state.
	dir := t.TempDir()
	path := filepath.Join(dir, "Orchestration.md")
	store := artifact.NewFileStore(path)
	ctx := context.Background()
	now := time.Date(2026, 1, 29, 9, 0, 0, 0, time.UTC)
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}

	created, err := store.Create(ctx, info, "Fix something", false, now)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	read, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Create: unexpected error: %v", err)
	}

	if read.Workflow != created.Workflow {
		t.Errorf("Read.Workflow: want %q (same as Create), got %q", created.Workflow, read.Workflow)
	}
	if read.Task != created.Task {
		t.Errorf("Read.Task: want %q (same as Create), got %q", created.Task, read.Task)
	}
}

// ---- Apply ----

func makeStep(seq int, agent, phase, stage string, status domain.StatusCode, ts time.Time, artifacts []string) domain.CompletedStep {
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

func mustCreateStore(t *testing.T) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", false, time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, state
}

func TestApply_ExecutionLog_GrowsByOne(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	if len(after.ExecutionLog) != len(state.ExecutionLog)+1 {
		t.Errorf("ExecutionLog length: want %d, got %d", len(state.ExecutionLog)+1, len(after.ExecutionLog))
	}
}

func TestApply_ExecutionLog_Entry_Seq(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(3, "planner#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Seq != 3 {
		t.Errorf("last entry Seq: want 3, got %d", last.Seq)
	}
}

func TestApply_ExecutionLog_Entry_Agent(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner-tdd-soft#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Agent != "planner-tdd-soft#1" {
		t.Errorf("last entry Agent: want %q, got %q", "planner-tdd-soft#1", last.Agent)
	}
}

func TestApply_ExecutionLog_Entry_Phase(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Phase != "PLANNING" {
		t.Errorf("last entry Phase: want %q, got %q", "PLANNING", last.Phase)
	}
}

func TestApply_ExecutionLog_Entry_StageFormatting_Execution(t *testing.T) {
	// During EXECUTION phase, stage is "Stage-N". Must be stored as-is.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "test-writer#1", "EXECUTION", "Stage-1", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Stage != "Stage-1" {
		t.Errorf("last entry Stage: want %q for EXECUTION phase, got %q", "Stage-1", last.Stage)
	}
}

func TestApply_ExecutionLog_Entry_StageFormatting_NonExecution(t *testing.T) {
	// Outside EXECUTION phase, stage is "" (rendered as "-" in the table).
	// ArtifactState must carry "" not "-".
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Stage != "" {
		t.Errorf("last entry Stage: want %q for non-EXECUTION phase, got %q", "", last.Stage)
	}
}

func TestApply_ArtifactRegistry_NewEntry(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var found bool
	for _, e := range after.ArtifactRegistry {
		if e.Artifact == "Plan.md" {
			found = true
		}
	}
	if !found {
		t.Error("ArtifactRegistry: entry for Plan.md not found after Apply")
	}
}

func TestApply_ArtifactRegistry_ExistingEntry_UpdatedOnRework(t *testing.T) {
	// If the same artifact path appears in a later step, the registry entry
	// is updated in place (CreatedBy reflects the latest invocation).
	store, state := mustCreateStore(t)
	ctx := context.Background()
	now := time.Now()

	step1 := makeStep(1, "test-writer#1", "EXECUTION", "Stage-1", domain.StatusSUCCESS, now, []string{"Stage-1/PlanProgress.md"})
	state1, err := store.Apply(ctx, state, step1)
	if err != nil {
		t.Fatalf("Apply step1: %v", err)
	}

	step2 := makeStep(2, "test-writer#2", "EXECUTION", "Stage-1", domain.StatusSUCCESS, now.Add(time.Minute), []string{"Stage-1/PlanProgress.md"})
	state2, err := store.Apply(ctx, state1, step2)
	if err != nil {
		t.Fatalf("Apply step2: %v", err)
	}

	var count int
	var latestCreatedBy string
	for _, e := range state2.ArtifactRegistry {
		if e.Artifact == "Stage-1/PlanProgress.md" {
			count++
			latestCreatedBy = e.CreatedBy
		}
	}
	if count != 1 {
		t.Errorf("ArtifactRegistry: want 1 entry for Stage-1/PlanProgress.md (upsert), got %d", count)
	}
	if latestCreatedBy != "test-writer#2" {
		t.Errorf("ArtifactRegistry[Stage-1/PlanProgress.md].CreatedBy: want %q (latest), got %q", "test-writer#2", latestCreatedBy)
	}
}

func TestApply_CurrentState_PhaseUpdated(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.Phase != "PLANNING" {
		t.Errorf("CurrentState.Phase: want %q, got %q", "PLANNING", after.CurrentState.Phase)
	}
}

func TestApply_CurrentState_StageUpdated(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "test-writer#1", "EXECUTION", "Stage-2", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.Stage != "Stage-2" {
		t.Errorf("CurrentState.Stage: want %q, got %q", "Stage-2", after.CurrentState.Stage)
	}
}

func TestApply_CurrentState_LastStatusUpdated(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusCOMPLETED_NEEDS_ACTION, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.LastStatus != domain.StatusCOMPLETED_NEEDS_ACTION {
		t.Errorf("CurrentState.LastStatus: want %q, got %q", domain.StatusCOMPLETED_NEEDS_ACTION, after.CurrentState.LastStatus)
	}
}

func TestApply_CurrentState_LastAgentUpdated(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := makeStep(1, "planner-tdd-soft#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.LastAgent != "planner-tdd-soft#1" {
		t.Errorf("CurrentState.LastAgent: want %q, got %q", "planner-tdd-soft#1", after.CurrentState.LastAgent)
	}
}

func TestApply_GlobalSequence_Incremented(t *testing.T) {
	store, state := mustCreateStore(t)
	before := state.GlobalSequence
	ctx := context.Background()
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.GlobalSequence <= before {
		t.Errorf("GlobalSequence: want > %d after Apply, got %d", before, after.GlobalSequence)
	}
}

func TestApply_LastUpdated_Set(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)
	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, ts, nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if !after.LastUpdated.Equal(ts) {
		t.Errorf("LastUpdated: want %v (step timestamp), got %v", ts, after.LastUpdated)
	}
}

func TestApply_ExecutionLog_Entry_Checkpoint_Propagated(t *testing.T) {
	// When CompletedStep.Checkpoint is non-empty, the resulting ExecutionLogEntry
	// must carry the same value.  This covers the write path for checkpoint IDs.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := domain.CompletedStep{
		Seq:           1,
		AgentInstance: "test-writer-tdd#1",
		Phase:         "EXECUTION",
		Stage:         "Stage-1",
		Status:        domain.StatusSUCCESS,
		Timestamp:     time.Now(),
		Summary:       "tests written",
		Checkpoint:    "snap-01",
	}

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Checkpoint != "snap-01" {
		t.Errorf("ExecutionLogEntry.Checkpoint: want %q, got %q", "snap-01", last.Checkpoint)
	}
}

func TestApply_CurrentState_BLOCKED_ErrorCode_Populated(t *testing.T) {
	// When a step's Status is BLOCKED, the ErrorCode must be propagated into
	// CurrentState.ErrorCode.  A generic "refusal" catch-all would pass a
	// non-BLOCKED status test but would miss this field for the BLOCKED path.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := domain.CompletedStep{
		Seq:           1,
		AgentInstance: "planner#1",
		Phase:         "PLANNING",
		Stage:         "",
		Status:        domain.StatusBLOCKED,
		ErrorCode:     domain.ErrorINPUT_NOT_FOUND,
		Timestamp:     time.Now(),
		Summary:       "missing input",
	}

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.ErrorCode != domain.ErrorINPUT_NOT_FOUND {
		t.Errorf("CurrentState.ErrorCode: want %q for BLOCKED step, got %q",
			domain.ErrorINPUT_NOT_FOUND, after.CurrentState.ErrorCode)
	}
}

func TestApply_WorkflowNotes_PreservedUnchanged(t *testing.T) {
	// Workflow Notes must be preserved unchanged by Apply (FR-14a).
	// The runner reads them but never writes new ones.
	store := artifact.NewFileStore(fixturePath("canonical.md"))
	ctx := context.Background()

	state, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	originalNotes := append([]domain.WorkflowNote(nil), state.WorkflowNotes...)

	// Apply a step to a writable copy (we must write to a temp file).
	dir := t.TempDir()
	tempPath := filepath.Join(dir, "Orchestration.md")
	tempStore := artifact.NewFileStore(tempPath)

	// Seed the temp file.
	info := domain.WorkflowInfo{ID: state.Workflow, Version: state.WorkflowVersion}
	seedState, err := tempStore.Create(ctx, info, state.Task, state.Checkpoints, state.Started)
	if err != nil {
		t.Fatalf("Create temp: %v", err)
	}
	// Manually inject notes into the state before applying
	// (the Create function starts with empty notes; we pre-populate them
	// to simulate a state that already has workflow notes).
	seedState.WorkflowNotes = originalNotes

	step := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	after, err := tempStore.Apply(ctx, seedState, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(after.WorkflowNotes) != len(originalNotes) {
		t.Errorf("WorkflowNotes count: want %d (unchanged), got %d", len(originalNotes), len(after.WorkflowNotes))
	}
	for i, n := range originalNotes {
		if i >= len(after.WorkflowNotes) {
			break
		}
		if after.WorkflowNotes[i].Note != n.Note {
			t.Errorf("WorkflowNotes[%d].Note: want %q, got %q", i, n.Note, after.WorkflowNotes[i].Note)
		}
	}
}

// ---- Round-trip ----

func TestRoundTrip_CanonicalFile_IdenticalBytes(t *testing.T) {
	// Parse the canonical fixture, render it back to bytes, and verify that
	// the result is byte-identical to the original file.
	//
	// This test verifies that:
	// - Parse reads all fields correctly.
	// - Render produces exactly the same bytes for an unmodified state.
	// - No content is silently dropped or reformatted.
	original, err := os.ReadFile(fixturePath("canonical.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	state, err := artifact.Parse(original)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !bytes.Equal(original, got) {
		// Show a human-readable diff.
		origLines := strings.Split(string(original), "\n")
		gotLines := strings.Split(string(got), "\n")
		for i := 0; i < len(origLines) || i < len(gotLines); i++ {
			var origLine, gotLine string
			if i < len(origLines) {
				origLine = origLines[i]
			}
			if i < len(gotLines) {
				gotLine = gotLines[i]
			}
			if origLine != gotLine {
				t.Errorf("line %d differs:\n  original: %q\n       got: %q", i+1, origLine, gotLine)
				if i > 3 {
					t.Log("... (further differences omitted)")
					break
				}
			}
		}
		t.Fail()
	}
}

// ---- TruncateSummary ----

func TestTruncateSummary_ShortMessage_ReturnedUnchanged(t *testing.T) {
	input := "Short message"
	got := artifact.TruncateSummary(input)
	if got != input {
		t.Errorf("TruncateSummary(%q): want unchanged %q, got %q", input, input, got)
	}
}

func TestTruncateSummary_ExactlyHundredChars_NotTruncated(t *testing.T) {
	input := strings.Repeat("a", 100)
	got := artifact.TruncateSummary(input)
	if got != input {
		t.Errorf("TruncateSummary(100 chars): want unchanged (100 chars), got %d chars", len(got))
	}
}

func TestTruncateSummary_LongMessage_HeadFiftyEllipsisTailFifty(t *testing.T) {
	// Messages longer than 100 characters are truncated to head-50 + " … " + tail-50.
	// This ensures verbose messages retain both the opening context and final conclusion.
	head := strings.Repeat("h", 50)
	tail := strings.Repeat("t", 50)
	input := head + strings.Repeat("m", 20) + tail // 120 chars total

	got := artifact.TruncateSummary(input)

	want := head + " … " + tail
	if got != want {
		t.Errorf("TruncateSummary(120 chars):\n  want: %q\n   got: %q", want, got)
	}
}

func TestTruncateSummary_PipeCharacter_Stripped(t *testing.T) {
	// Pipe characters must be stripped because they would break the markdown table.
	input := "status: OK | count: 5"
	got := artifact.TruncateSummary(input)
	if strings.Contains(got, "|") {
		t.Errorf("TruncateSummary(%q): result must not contain pipe, got %q", input, got)
	}
}

func TestTruncateSummary_Newline_Stripped(t *testing.T) {
	// Newlines must be stripped because multi-line content breaks the markdown table.
	input := "first line\nsecond line"
	got := artifact.TruncateSummary(input)
	if strings.Contains(got, "\n") {
		t.Errorf("TruncateSummary(%q): result must not contain newline, got %q", input, got)
	}
}

func TestTruncateSummary_StripBeforeTruncation_HeadTailFromCleanString(t *testing.T) {
	// Stripping (pipe/newline removal) happens before truncation so that the
	// 50/50 split is counted on the clean string, not the raw input.
	// Input: 100 clean chars preceded by a pipe that would be stripped.
	clean := strings.Repeat("c", 100)
	input := "|" + clean // 101 chars raw, but 100 chars after strip
	got := artifact.TruncateSummary(input)
	// After stripping the pipe, we have exactly 100 chars — no truncation.
	if got != clean {
		t.Errorf("TruncateSummary(%q): want %q (stripped, no truncation), got %q", input, clean, got)
	}
}
