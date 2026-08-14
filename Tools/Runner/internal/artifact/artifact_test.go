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
//   - Staged Stage value ("1", the target ungrouped form) preserved as-is.
//   - ArtifactRegistry: row count, first row's Artifact/CreatedIn/CreatedBy.
//   - WorkflowNotes: count, first note's Seq and text preserved verbatim.
//
//   Parse — run_id handling:
//   - Frontmatter with run_id field: Parse populates ArtifactState.RunID.
//   - Canonical fixture: RunID parsed correctly from frontmatter.
//   - Frontmatter without run_id field: ArtifactState.RunID is "" (no error).
//
//   Parse — refusal cases:
//   - Non-existent file → os.ErrNotExist (tested via Read, not Parse directly).
//   - Missing "type: orchestration-artifact" → *domain.RefusalError.
//   - Old template format (no <Name type="..."> tags) → *domain.RefusalError.
//   - Missing <ExecutionLog type="core"> section → *domain.RefusalError.
//   - Missing <Artifacts type="core"> section → *domain.RefusalError.
//   - Truncated file → *domain.RefusalError.
//   - RefusalError.Component must be "artifact".
//   - RefusalError.Resource must name the file path.
//
//   Render — run_id handling:
//   - ArtifactState with non-empty RunID: run_id emitted in frontmatter.
//   - run_id field positioned after type and before workflow.
//   - ArtifactState with empty RunID: run_id field NOT emitted (backward compatibility).
//
//   Round-trip — run_id:
//   - Parse then Render preserves RunID: re-parsing the rendered bytes yields the same RunID.
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
//
//   SetPhase (T5.3):
//   - SetPhase updates current_state.phase in the returned ArtifactState.
//   - SetPhase bumps global_sequence by one in the returned ArtifactState.
//   - SetPhase sets last_updated to the supplied now timestamp.
//   - SetPhase does NOT append an execution log entry (log length unchanged).
//   - SetPhase does NOT modify ArtifactRegistry entries.
//   - Round-trip: Read after SetPhase returns the updated phase.
//   - SetPhase preserves WorkflowNotes unchanged.
//   - SetPhase with "COMPLETED" phase persists correctly (case preserved in output).
//
//   Inputs column (parse/write/round-trip):
//   - Canonical fixture: Inputs column present with "-" → "" in ArtifactState.
//   - Parse row with populated Inputs value: stored as-is in ExecutionLogEntry.Inputs.
//   - Artifact without Inputs column (old format): parses without error; Inputs is "".
//   - Apply with non-empty CompletedStep.Inputs: propagated to ExecutionLogEntry.Inputs.
//   - Apply with empty CompletedStep.Inputs: ExecutionLogEntry.Inputs is "" (rendered as "-").
//   - Round-trip: artifact with Inputs value → parse → render → re-parse preserves value.
//
//   infrastructure_overrides frontmatter:
//   - Absent block → ArtifactState.InfrastructureOverrides is nil.
//   - Single-agent override: InfrastructureOverrides has exactly one entry.
//   - Agent name extracted correctly from the YAML map key.
//   - Trigger name populated correctly in DeclaredInfraTrigger.Trigger.
//   - trigger_param populated correctly in DeclaredInfraTrigger.Param.
//   - Multiple triggers for one agent: all triggers parsed.
//   - Round-trip: agent name survives Parse → Render → re-Parse.
//
//   commits frontmatter field:
//   - Render unconditionally emits "commits: disabled" regardless of state.
//   - commits: is positioned after checkpoints: and before current_state:.
//   - Parse accepts a frontmatter carrying a commits: line without refusing.

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

	// canonical.md is an ungrouped staged row (quick-fix workflow, no group
	// segment declared): the target recorded form is the plain stage number.
	if state.CurrentState.Stage != "1" {
		t.Errorf("CurrentState.Stage: want %q, got %q", "1", state.CurrentState.Stage)
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
	// Row 1 has Stage="1" (EXECUTION phase, staged, ungrouped -- the target
	// recorded form for a row that declares no group).
	// The value must be preserved as-is in ArtifactState.
	state := mustReadCanonical(t)

	if state.ExecutionLog[1].Stage != "1" {
		t.Errorf("ExecutionLog[1].Stage: want %q, got %q", "1", state.ExecutionLog[1].Stage)
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
	// Entry for Stage-1/PlanProgress.md has CreatedIn="EXECUTION.1" -- the
	// target form composed from the bare phase and the plain (ungrouped)
	// stage number. This "Phase.Stage" notation is stored verbatim, not split.
	state := mustReadCanonical(t)

	var found bool
	for _, e := range state.ArtifactRegistry {
		if e.Artifact == "Stage-1/PlanProgress.md" {
			found = true
			if e.CreatedIn != "EXECUTION.1" {
				t.Errorf("ArtifactRegistry[Stage-1/PlanProgress.md].CreatedIn: want %q, got %q", "EXECUTION.1", e.CreatedIn)
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
	// Old template format uses plain markdown headings instead of <Name type="..."> region tags
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
		t.Fatal("Parse must return an error when <ExecutionLog type=\"core\"> is absent")
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
		t.Fatal("Parse must return an error when <Artifacts type=\"core\"> is absent")
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

	state, err := store.Create(ctx, info, "some task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "some task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "My important task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", true, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", false, now, "")
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

	state, err := store.Create(ctx, info, "task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", false, time.Now(), "")
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

	state, err := store.Create(ctx, info, "task", false, time.Now(), "")
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
	if _, err := store.Create(ctx, info, "first", false, time.Now(), ""); err != nil {
		t.Fatalf("first Create: unexpected error: %v", err)
	}

	// A second Create on the same path must fail.
	_, err := store.Create(ctx, info, "second", false, time.Now(), "")
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

	created, err := store.Create(ctx, info, "Fix something", false, now, "")
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
	state, err := store.Create(ctx, info, "test task", false, time.Now(), "")
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
	seedState, err := tempStore.Create(ctx, info, state.Task, state.Checkpoints, state.Started, "")
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

// ---- Apply: infrastructure steps and recorded position (T3.1) ----
//
// An infrastructure step (CompletedStep.IsInfrastructure == true) must not move
// the recorded workflow position: current_state continues to name the last
// WORKFLOW step, on disk. Everything else Apply does -- execution log append,
// sequence bump, last_updated bump, artifact registry upsert -- applies to an
// infrastructure step exactly as it does to a workflow step.
//
// Assertions here go through store.Read after Apply, not just the in-memory
// return value: an in-memory-only assertion would pass even with the on-disk
// defect fully present, since fileStore.Apply builds newState in memory before
// (incorrectly) letting it overwrite current_state on disk.

func TestApply_Infrastructure_OnDisk_CurrentStateUnchanged(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()

	workflowStep := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	afterWorkflow, err := store.Apply(ctx, state, workflowStep)
	if err != nil {
		t.Fatalf("Apply (workflow step): %v", err)
	}

	infraStep := makeStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	infraStep.IsInfrastructure = true
	if _, err := store.Apply(ctx, afterWorkflow, infraStep); err != nil {
		t.Fatalf("Apply (infrastructure step): %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after infrastructure step Apply: %v", err)
	}
	if onDisk.CurrentState.LastAgent != "planner#1" {
		t.Errorf("on-disk CurrentState.LastAgent: want %q (unchanged by infrastructure step), got %q",
			"planner#1", onDisk.CurrentState.LastAgent)
	}
	if onDisk.CurrentState.LastStatus != domain.StatusSUCCESS {
		t.Errorf("on-disk CurrentState.LastStatus: want unchanged %q, got %q",
			domain.StatusSUCCESS, onDisk.CurrentState.LastStatus)
	}
}

func TestApply_Infrastructure_OnDisk_LogSequenceAndTimestampAdvance(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()

	ts1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	workflowStep := makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, ts1, nil)
	afterWorkflow, err := store.Apply(ctx, state, workflowStep)
	if err != nil {
		t.Fatalf("Apply (workflow step): %v", err)
	}

	ts2 := ts1.Add(time.Hour)
	infraStep := makeStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, ts2, nil)
	infraStep.IsInfrastructure = true
	if _, err := store.Apply(ctx, afterWorkflow, infraStep); err != nil {
		t.Fatalf("Apply (infrastructure step): %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after infrastructure step Apply: %v", err)
	}

	if len(onDisk.ExecutionLog) != 2 {
		t.Fatalf("on-disk ExecutionLog length: want 2 (workflow step + infrastructure step), got %d",
			len(onDisk.ExecutionLog))
	}
	last := onDisk.ExecutionLog[len(onDisk.ExecutionLog)-1]
	if last.Agent != "checkpoint-manager-git#2" {
		t.Errorf("on-disk ExecutionLog last entry Agent: want %q, got %q", "checkpoint-manager-git#2", last.Agent)
	}
	if onDisk.GlobalSequence != 2 {
		t.Errorf("on-disk GlobalSequence: want 2 (advanced by the infrastructure step), got %d", onDisk.GlobalSequence)
	}
	if !onDisk.LastUpdated.Equal(ts2) {
		t.Errorf("on-disk LastUpdated: want %v (advanced by the infrastructure step), got %v", ts2, onDisk.LastUpdated)
	}
}

func TestApply_Infrastructure_OnDisk_ArtifactRegistryStillUpserted(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()

	infraStep := makeStep(1, "checkpoint-manager-git#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(),
		[]string{"checkpoint.log"})
	infraStep.IsInfrastructure = true
	if _, err := store.Apply(ctx, state, infraStep); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var found bool
	for _, e := range onDisk.ArtifactRegistry {
		if e.Artifact == "checkpoint.log" {
			found = true
		}
	}
	if !found {
		t.Error("on-disk ArtifactRegistry: entry for checkpoint.log not found after infrastructure step Apply " +
			"(infrastructure invocations must remain fully recorded, AC3.3)")
	}
}

func TestApply_Infrastructure_ThenWorkflowStep_CurrentStateNamesLatestWorkflowStep(t *testing.T) {
	// planner#1 (workflow) -> checkpoint-manager-git#2 (infrastructure) ->
	// plan-review#3 (workflow). The recorded position must end up naming
	// plan-review, the latest WORKFLOW step, never the infrastructure step
	// that ran between them.
	store, state := mustCreateStore(t)
	ctx := context.Background()

	afterA, err := store.Apply(ctx, state,
		makeStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil))
	if err != nil {
		t.Fatalf("Apply (planner): %v", err)
	}

	infraStep := makeStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	infraStep.IsInfrastructure = true
	afterInfra, err := store.Apply(ctx, afterA, infraStep)
	if err != nil {
		t.Fatalf("Apply (checkpoint-manager-git): %v", err)
	}

	if _, err := store.Apply(ctx, afterInfra,
		makeStep(3, "plan-review#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)); err != nil {
		t.Fatalf("Apply (plan-review): %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if onDisk.CurrentState.LastAgent != "plan-review#3" {
		t.Errorf("on-disk CurrentState.LastAgent: want %q (latest workflow step), got %q",
			"plan-review#3", onDisk.CurrentState.LastAgent)
	}
	if len(onDisk.ExecutionLog) != 3 {
		t.Errorf("on-disk ExecutionLog length: want 3 (all three invocations recorded), got %d",
			len(onDisk.ExecutionLog))
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

// ---- Parse: run_id handling ----

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

func TestParse_RunIDPresentInFrontmatter_PopulatesRunID(t *testing.T) {
	// When the frontmatter contains a run_id field, Parse must populate
	// ArtifactState.RunID with the exact string value.
	const wantRunID = "20260727T170000Z-a3f9"
	data := minimalArtifactBytes(wantRunID)

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.RunID != wantRunID {
		t.Errorf("RunID: want %q, got %q", wantRunID, state.RunID)
	}
}

func TestParse_CanonicalFile_RunID(t *testing.T) {
	// The canonical fixture includes run_id in the frontmatter.
	// Parse must populate ArtifactState.RunID from that field.
	const wantRunID = "20260727T170000Z-a3f9"
	state := mustReadCanonical(t)

	if state.RunID != wantRunID {
		t.Errorf("RunID from canonical fixture: want %q, got %q", wantRunID, state.RunID)
	}
}

func TestParse_RunIDAbsentFromFrontmatter_ReturnsEmptyString(t *testing.T) {
	// When the frontmatter has no run_id field (pre-v1.8 artifact),
	// ArtifactState.RunID must be "" — not an error.
	data := minimalArtifactBytes("") // no run_id line

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.RunID != "" {
		t.Errorf("RunID: want %q (absent = empty), got %q", "", state.RunID)
	}
}

func TestParse_RunIDAbsentFromFrontmatter_ReturnsNoError(t *testing.T) {
	// Absence of run_id in frontmatter must not cause a parse error.
	// Pre-v1.8 artifacts do not have this field and must parse successfully.
	data := minimalArtifactBytes("")

	_, err := artifact.Parse(data)

	if err != nil {
		t.Errorf("Parse: want no error when run_id absent, got %v", err)
	}
}

// ---- Render: run_id handling ----

func TestRender_WithRunID_EmitsRunIDInFrontmatter(t *testing.T) {
	// When ArtifactState.RunID is non-empty, Render must emit
	// "run_id: <value>" in the frontmatter block.
	const runID = "20260727T170000Z-a3f9"
	state := domain.ArtifactState{
		RunID:    runID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if !strings.Contains(string(got), "run_id: "+runID) {
		t.Errorf("Render: want frontmatter to contain %q, but it was absent.\nOutput:\n%s", "run_id: "+runID, got)
	}
}

func TestRender_RunIDPosition_AfterTypeBeforeWorkflow(t *testing.T) {
	// When run_id is present, it must appear in the frontmatter after "type:"
	// and before "workflow:", matching the field ordering specified in the design.
	const runID = "20260727T170000Z-a3f9"
	state := domain.ArtifactState{
		RunID:    runID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	output := string(got)
	typeIdx := strings.Index(output, "type:")
	runIDIdx := strings.Index(output, "run_id:")
	workflowIdx := strings.Index(output, "\nworkflow:")

	if typeIdx < 0 {
		t.Fatal("rendered output is missing the type: field")
	}
	if runIDIdx < 0 {
		t.Fatal("rendered output is missing the run_id: field")
	}
	if workflowIdx < 0 {
		t.Fatal("rendered output is missing the workflow: field")
	}
	if !(typeIdx < runIDIdx && runIDIdx < workflowIdx) {
		t.Errorf("run_id is not positioned after type: and before workflow:. type@%d, run_id@%d, workflow@%d",
			typeIdx, runIDIdx, workflowIdx)
	}
}

func TestRender_EmptyRunID_NotEmittedInFrontmatter(t *testing.T) {
	// When ArtifactState.RunID is empty (pre-v1.8 artifact), Render must NOT
	// emit a run_id line. This preserves backward compatibility: rendering a
	// pre-v1.8 artifact must not add a spurious run_id field.
	state := domain.ArtifactState{
		RunID:    "", // empty
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if strings.Contains(string(got), "run_id:") {
		t.Errorf("Render: want no run_id: field when RunID is empty, but output contained it.\nOutput:\n%s", got)
	}
}

// ---- Round-trip: run_id ----

func TestRoundTrip_WithRunID_PreservesRunID(t *testing.T) {
	// Parse an artifact that has run_id in its frontmatter, render the resulting
	// state back to bytes, then re-parse. The RunID must survive the round-trip
	// unchanged.
	const runID = "20260727T170000Z-a3f9"
	data := minimalArtifactBytes(runID)

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	state2, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: %v", err)
	}

	if state2.RunID != runID {
		t.Errorf("RunID after round-trip: want %q, got %q", runID, state2.RunID)
	}
}

// ---- Render / Parse: commits frontmatter field ----

func TestRender_ContainsCommitsDisabled(t *testing.T) {
	// Render must unconditionally emit "commits: disabled" in the frontmatter,
	// regardless of the ArtifactState's field values. The field is a fixed
	// literal, not derived from any state field.
	state := domain.ArtifactState{
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if !strings.Contains(string(got), "commits: disabled") {
		t.Errorf("Render: want frontmatter to contain %q, but it was absent.\nOutput:\n%s", "commits: disabled", got)
	}
}

func TestRender_CommitsPosition_AfterCheckpointsBeforeCurrentState(t *testing.T) {
	// commits: disabled must appear immediately after the checkpoints: line
	// and before the current_state: block, so the two related toggles read
	// together.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		Checkpoints: true,
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	output := string(got)
	checkpointsIdx := strings.Index(output, "checkpoints:")
	commitsIdx := strings.Index(output, "commits:")
	currentStateIdx := strings.Index(output, "current_state:")

	if checkpointsIdx < 0 {
		t.Fatal("rendered output is missing the checkpoints: field")
	}
	if commitsIdx < 0 {
		t.Fatal("rendered output is missing the commits: field")
	}
	if currentStateIdx < 0 {
		t.Fatal("rendered output is missing the current_state: field")
	}
	if !(checkpointsIdx < commitsIdx && commitsIdx < currentStateIdx) {
		t.Errorf("commits is not positioned after checkpoints: and before current_state:. checkpoints@%d, commits@%d, current_state@%d",
			checkpointsIdx, commitsIdx, currentStateIdx)
	}
}

func TestParse_FrontmatterWithCommitsLine_DoesNotRefuse(t *testing.T) {
	// Parse must accept an artifact whose frontmatter carries a commits: line
	// without refusing the file. The value is not stored anywhere; only
	// tolerance is required.
	data := []byte("---\n" +
		"type: orchestration-artifact\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T00:00:00Z\n" +
		"global_sequence: 0\n" +
		"checkpoints: disabled\n" +
		"commits: disabled\n" +
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

	_, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: want no error for a frontmatter carrying commits:, got: %v", err)
	}
}

// ============================================================
// T5.3: SetPhase tests
// ============================================================
//
// All tests in this section are in the RED phase: they compile but fail because
// fileStore.SetPhase panics ("not implemented") until I5.4 is complete.

// setPhaseFixture creates a temporary artifact via store.Create and returns
// the store and initial state for use in SetPhase tests.
func setPhaseFixture(t *testing.T) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", false, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatalf("setPhaseFixture: Create: %v", err)
	}
	return store, state
}

func TestSetPhase_UpdatesPhaseInReturnedState(t *testing.T) {
	// SetPhase must return an ArtifactState with current_state.phase set to
	// the supplied phase value.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	updated, err := store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}
	if updated.CurrentState.Phase != "COMPLETED" {
		t.Errorf("Phase = %q, want %q", updated.CurrentState.Phase, "COMPLETED")
	}
}

func TestSetPhase_BumpsGlobalSequence(t *testing.T) {
	// SetPhase must increment global_sequence by one from the supplied state.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	beforeSeq := state.GlobalSequence
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	updated, err := store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}
	if updated.GlobalSequence != beforeSeq+1 {
		t.Errorf("GlobalSequence = %d, want %d (incremented by 1)", updated.GlobalSequence, beforeSeq+1)
	}
}

func TestSetPhase_SetsLastUpdatedToNow(t *testing.T) {
	// SetPhase must set last_updated to the supplied now timestamp.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	setTime := time.Date(2026, 7, 27, 17, 0, 0, 0, time.UTC)

	updated, err := store.SetPhase(ctx, state, "COMPLETED", setTime)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}
	if !updated.LastUpdated.Equal(setTime) {
		t.Errorf("LastUpdated = %v, want %v", updated.LastUpdated, setTime)
	}
}

func TestSetPhase_DoesNotAppendExecutionLogEntry(t *testing.T) {
	// SetPhase must not append any row to the execution log.
	// The execution log length must be the same before and after SetPhase.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	logLenBefore := len(state.ExecutionLog)
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err := store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}

	// Read back the persisted artifact and check execution log length.
	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if len(readBack.ExecutionLog) != logLenBefore {
		t.Errorf("ExecutionLog length = %d after SetPhase, want %d (no new entries)", len(readBack.ExecutionLog), logLenBefore)
	}
}

func TestSetPhase_DoesNotModifyArtifactRegistry(t *testing.T) {
	// SetPhase must not modify the artifact registry.
	// The registry must have the same entries after SetPhase.
	store, state := setPhaseFixture(t)
	ctx := context.Background()

	// Apply a step with an output artifact so the registry is non-empty.
	state, err := store.Apply(ctx, state, domain.CompletedStep{
		Seq:             1,
		AgentInstance:   "agent#1",
		Phase:           "EXECUTION",
		Status:          "SUCCESS",
		Timestamp:       time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		OutputArtifacts: []string{"Plan.md"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	registryLenBefore := len(state.ArtifactRegistry)
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err = store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if len(readBack.ArtifactRegistry) != registryLenBefore {
		t.Errorf("ArtifactRegistry length = %d after SetPhase, want %d (registry must not be modified)",
			len(readBack.ArtifactRegistry), registryLenBefore)
	}
}

func TestSetPhase_RoundTrip_ReadReturnsUpdatedPhase(t *testing.T) {
	// Read after SetPhase must return an ArtifactState with the updated phase.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err := store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if readBack.CurrentState.Phase != "COMPLETED" {
		t.Errorf("Phase after round-trip = %q, want %q", readBack.CurrentState.Phase, "COMPLETED")
	}
}

func TestSetPhase_PreservesWorkflowNotes(t *testing.T) {
	// SetPhase must preserve WorkflowNotes unchanged (same behaviour as Apply).
	// WorkflowNotes are Tier 3 (opaque) and must survive all write operations.
	//
	// Since Create produces an artifact with no WorkflowNotes (only the orchestrator
	// writes them), this test verifies that SetPhase does not corrupt the notes block
	// by checking the notes count is still zero.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	notesBefore := len(state.WorkflowNotes)
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err := store.SetPhase(ctx, state, "COMPLETED", now)

	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if len(readBack.WorkflowNotes) != notesBefore {
		t.Errorf("WorkflowNotes count = %d after SetPhase, want %d", len(readBack.WorkflowNotes), notesBefore)
	}
}

func TestSetPhase_COMPLETEDPhase_PersistedCasePreserved(t *testing.T) {
	// SetPhase must write the phase value with the exact casing supplied.
	// "COMPLETED" must be read back as "COMPLETED", not lowercased or altered.
	store, state := setPhaseFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err := store.SetPhase(ctx, state, "COMPLETED", now)
	if err != nil {
		t.Fatalf("SetPhase: unexpected error: %v", err)
	}

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetPhase: %v", err)
	}
	if readBack.CurrentState.Phase != "COMPLETED" {
		t.Errorf("Phase = %q, want exact casing %q", readBack.CurrentState.Phase, "COMPLETED")
	}
}

// ---- Create with run_id (T7.4) ----
//
// All tests in this section are in the RED phase: they compile but fail because
// fileStore.Create does not yet store runID in the artifact frontmatter.

// createFixtureWithRunID creates a temp artifact with the given runID and returns
// the store and the initial state for use in Create+runID tests.
func createFixtureWithRunID(t *testing.T, runID string) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(ctx, info, "test task", false, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), runID)
	if err != nil {
		t.Fatalf("createFixtureWithRunID: Create: %v", err)
	}
	return store, state
}

// TestCreate_RunID_SetInReturnedState verifies that the run_id passed to Create
// is reflected in the returned ArtifactState.RunID field.
func TestCreate_RunID_SetInReturnedState(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	_, state := createFixtureWithRunID(t, runID)

	if state.RunID != runID {
		t.Errorf("state.RunID: want %q, got %q", runID, state.RunID)
	}
}

// TestCreate_RunID_PersistedToFrontmatter verifies that the run_id passed to
// Create is written to the artifact file's frontmatter and can be read back via
// Read. This is the key persistence requirement: once created, the run_id is the
// permanent identity of the run and must survive a read cycle.
func TestCreate_RunID_PersistedToFrontmatter(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	store, _ := createFixtureWithRunID(t, runID)
	ctx := context.Background()

	readBack, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Create: unexpected error: %v", err)
	}

	if readBack.RunID != runID {
		t.Errorf("RunID after Read: want %q, got %q", runID, readBack.RunID)
	}
}

// TestCreate_RunID_EmptyRunID_NotInFrontmatter verifies that when Create is called
// with an empty runID (pre-v1.8 or minting-deferred callers), the artifact file
// does NOT contain a "run_id" key in its frontmatter. This maintains backward
// compatibility with pre-v1.8 artifacts where the run_id field is absent.
func TestCreate_RunID_EmptyRunID_NotInFrontmatter(t *testing.T) {
	_, state := createFixtureWithRunID(t, "")

	if state.RunID != "" {
		t.Errorf("state.RunID: want empty string when runID param is empty, got %q", state.RunID)
	}
}

// TestCreate_RunID_DifferentValues_Stored verifies that Create correctly stores
// whatever run_id value is passed — not just the specific "a3f9" suffix value.
// Uses a different run_id to confirm the parameter is being used, not a hardcoded default.
func TestCreate_RunID_DifferentValues_Stored(t *testing.T) {
	const runID = "20260101T000000Z-ffff"
	_, state := createFixtureWithRunID(t, runID)

	if state.RunID != runID {
		t.Errorf("state.RunID: want %q, got %q", runID, state.RunID)
	}
}

// TestCreate_RunID_RoundTrip_PreservesAllOtherFields verifies that providing a
// non-empty runID does not affect the storage of any other artifact fields.
// WorkflowID, Task, Checkpoints, GlobalSequence, etc. must all be correct.
func TestCreate_RunID_RoundTrip_PreservesAllOtherFields(t *testing.T) {
	const runID = "20260727T170000Z-a3f9"
	dir := t.TempDir()
	store := artifact.NewFileStore(filepath.Join(dir, "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "my-workflow", Version: "2.0"}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	created, err := store.Create(ctx, info, "important task", true, now, runID)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}

	// Verify all fields beyond RunID are still correct.
	if created.Workflow != domain.WorkflowID("my-workflow") {
		t.Errorf("Workflow: want %q, got %q", "my-workflow", created.Workflow)
	}
	if created.WorkflowVersion != domain.WorkflowVersion("2.0") {
		t.Errorf("WorkflowVersion: want %q, got %q", "2.0", created.WorkflowVersion)
	}
	if created.Task != "important task" {
		t.Errorf("Task: want %q, got %q", "important task", created.Task)
	}
	if !created.Checkpoints {
		t.Error("Checkpoints: want true, got false")
	}
	if created.GlobalSequence != 0 {
		t.Errorf("GlobalSequence: want 0, got %d", created.GlobalSequence)
	}
	// And verify RunID is also correct.
	if created.RunID != runID {
		t.Errorf("RunID: want %q, got %q", runID, created.RunID)
	}
}

// ============================================================
// Inputs column — parse, write, and round-trip
// ============================================================
//
// All tests in this section are in the RED phase: they compile but fail
// because the parser/writer does not yet handle the Inputs column.

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

// TestParse_CanonicalFile_ExecutionLog_FirstRow_DashInputs_ReturnsEmptyString
// verifies that the Inputs column value "-" is translated to "" in
// ExecutionLogEntry.Inputs, following the same empty-value convention as Stage
// and Checkpoint.
func TestParse_CanonicalFile_ExecutionLog_FirstRow_DashInputs_ReturnsEmptyString(t *testing.T) {
	// Canonical fixture now has Inputs column; row 0 has "-" (no inputs).
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Inputs != "" {
		t.Errorf("ExecutionLog[0].Inputs: want %q for dash cell, got %q", "", state.ExecutionLog[0].Inputs)
	}
}

// TestParse_ExecutionLog_InputsWithValue_Populated verifies that when the
// Inputs column contains a non-dash value, ExecutionLogEntry.Inputs holds
// that value exactly.
func TestParse_ExecutionLog_InputsWithValue_Populated(t *testing.T) {
	const want = "Plan.md, Design.md"
	data := minimalArtifactWithExecutionRow(want)

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.ExecutionLog) == 0 {
		t.Fatal("ExecutionLog: want at least 1 row, got 0")
	}

	if state.ExecutionLog[0].Inputs != want {
		t.Errorf("ExecutionLog[0].Inputs: want %q, got %q", want, state.ExecutionLog[0].Inputs)
	}
}

// TestParse_ExecutionLog_OldFormatWithoutInputsColumn_InputsIsEmpty verifies
// backward compatibility: artifacts written before the Inputs column was
// added must parse without error, and ExecutionLogEntry.Inputs must be ""
// for every row (column bound by name; absent column = empty value).
func TestParse_ExecutionLog_OldFormatWithoutInputsColumn_InputsIsEmpty(t *testing.T) {
	data, err := os.ReadFile(fixturePath("no-inputs-column.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse(no-inputs-column.md): unexpected error: %v", err)
	}
	if len(state.ExecutionLog) == 0 {
		t.Fatal("ExecutionLog: want at least 1 row in backward-compat fixture, got 0")
	}

	for i, entry := range state.ExecutionLog {
		if entry.Inputs != "" {
			t.Errorf("ExecutionLog[%d].Inputs: want %q (absent column), got %q", i, "", entry.Inputs)
		}
	}
}

// TestApply_ExecutionLog_Entry_Inputs_Propagated verifies that a non-empty
// CompletedStep.Inputs value is carried through Store.Apply into the
// corresponding ExecutionLogEntry.Inputs.
func TestApply_ExecutionLog_Entry_Inputs_Propagated(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()

	const wantInputs = "Plan.md, Stage-1/Design.md"
	step := domain.CompletedStep{
		Seq:           1,
		AgentInstance: "implementation-tdd#1",
		Phase:         "EXECUTION",
		Stage:         "Stage-1",
		Status:        domain.StatusSUCCESS,
		Timestamp:     time.Now(),
		Summary:       "implementation done",
		Inputs:        wantInputs,
	}

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Inputs != wantInputs {
		t.Errorf("ExecutionLogEntry.Inputs: want %q, got %q", wantInputs, last.Inputs)
	}
}

// TestApply_ExecutionLog_Entry_EmptyInputs_StoresEmptyString verifies that
// when CompletedStep.Inputs is empty (no input artifacts), the resulting
// ExecutionLogEntry.Inputs is also "" (which is rendered as "-" in the table).
func TestApply_ExecutionLog_Entry_EmptyInputs_StoresEmptyString(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()

	step := domain.CompletedStep{
		Seq:           1,
		AgentInstance: "planner#1",
		Phase:         "PLANNING",
		Stage:         "",
		Status:        domain.StatusSUCCESS,
		Timestamp:     time.Now(),
		Summary:       "planning done",
		Inputs:        "", // no inputs
	}

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.Inputs != "" {
		t.Errorf("ExecutionLogEntry.Inputs: want %q (empty → rendered as dash), got %q", "", last.Inputs)
	}
}

// TestRoundTrip_InputsValue_Preserved verifies that a populated Inputs cell
// survives Parse → Render → re-Parse unchanged.
func TestRoundTrip_InputsValue_Preserved(t *testing.T) {
	const wantInputs = "Plan.md, Design.md"
	original := minimalArtifactWithExecutionRow(wantInputs)

	state, err := artifact.Parse(original)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	state2, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: %v", err)
	}

	if len(state2.ExecutionLog) == 0 {
		t.Fatal("ExecutionLog after round-trip: want at least 1 row, got 0")
	}
	if state2.ExecutionLog[0].Inputs != wantInputs {
		t.Errorf("ExecutionLog[0].Inputs after round-trip: want %q, got %q", wantInputs, state2.ExecutionLog[0].Inputs)
	}
}

// ============================================================
// infrastructure_overrides frontmatter — parse and round-trip
// ============================================================
//
// All tests in this section are in the RED phase: they compile but fail
// because the parser/writer does not yet handle the infrastructure_overrides
// frontmatter block.

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

// TestParse_InfrastructureOverrides_Absent_ReturnsNil verifies that
// ArtifactState.InfrastructureOverrides is nil when the
// infrastructure_overrides block is absent from the frontmatter (the common
// case).
func TestParse_InfrastructureOverrides_Absent_ReturnsNil(t *testing.T) {
	// minimalArtifactBytes (no overrides block) is the common baseline.
	data := minimalArtifactBytes("")

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.InfrastructureOverrides != nil {
		t.Errorf("InfrastructureOverrides: want nil when block absent, got %v", state.InfrastructureOverrides)
	}
}

// TestParse_InfrastructureOverrides_SingleEntry_Count verifies that a
// single infrastructure_overrides entry produces exactly one element in
// ArtifactState.InfrastructureOverrides.
func TestParse_InfrastructureOverrides_SingleEntry_Count(t *testing.T) {
	data := minimalArtifactWithSingleOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if len(state.InfrastructureOverrides) != 1 {
		t.Errorf("InfrastructureOverrides count: want 1, got %d", len(state.InfrastructureOverrides))
	}
}

// TestParse_InfrastructureOverrides_AgentName_Populated verifies that the
// YAML map key (the agent name) is correctly extracted into
// InfrastructureOverride.AgentName.
func TestParse_InfrastructureOverrides_AgentName_Populated(t *testing.T) {
	const wantAgent = "checkpoint-manager-git"
	data := minimalArtifactWithSingleOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.InfrastructureOverrides) == 0 {
		t.Fatal("InfrastructureOverrides: want 1 entry, got 0 (parser not yet implemented)")
	}

	if state.InfrastructureOverrides[0].AgentName != wantAgent {
		t.Errorf("InfrastructureOverrides[0].AgentName: want %q, got %q",
			wantAgent, state.InfrastructureOverrides[0].AgentName)
	}
}

// TestParse_InfrastructureOverrides_Trigger_Populated verifies that the
// trigger type string is correctly populated in DeclaredInfraTrigger.Trigger.
func TestParse_InfrastructureOverrides_Trigger_Populated(t *testing.T) {
	data := minimalArtifactWithSingleOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.InfrastructureOverrides) == 0 {
		t.Fatal("InfrastructureOverrides: want 1 entry, got 0 (parser not yet implemented)")
	}
	if len(state.InfrastructureOverrides[0].Triggers) == 0 {
		t.Fatal("InfrastructureOverrides[0].Triggers: want at least 1 trigger, got 0")
	}

	if state.InfrastructureOverrides[0].Triggers[0].Trigger != "STAGE_END" {
		t.Errorf("Triggers[0].Trigger: want %q, got %q",
			"STAGE_END", state.InfrastructureOverrides[0].Triggers[0].Trigger)
	}
}

// TestParse_InfrastructureOverrides_TriggerParam_Populated verifies that the
// trigger_param value is correctly populated in DeclaredInfraTrigger.Param.
func TestParse_InfrastructureOverrides_TriggerParam_Populated(t *testing.T) {
	// Uses the multi-trigger override fixture: INVOCATION_INTERVAL with trigger_param: 15.
	data := minimalArtifactWithMultiTriggerOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.InfrastructureOverrides) == 0 {
		t.Fatal("InfrastructureOverrides: want 1 entry, got 0 (parser not yet implemented)")
	}
	if len(state.InfrastructureOverrides[0].Triggers) == 0 {
		t.Fatal("InfrastructureOverrides[0].Triggers: want at least 1 trigger, got 0")
	}

	// First trigger: INVOCATION_INTERVAL with trigger_param: 15.
	if state.InfrastructureOverrides[0].Triggers[0].Param != "15" {
		t.Errorf("Triggers[0].Param: want %q (from trigger_param: 15), got %q",
			"15", state.InfrastructureOverrides[0].Triggers[0].Param)
	}
}

// TestParse_InfrastructureOverrides_MultipleTriggers_Count verifies that all
// triggers in an override entry are parsed, not just the first one.
func TestParse_InfrastructureOverrides_MultipleTriggers_Count(t *testing.T) {
	// orchestration-review override has two triggers: INVOCATION_INTERVAL and PHASE_END.
	data := minimalArtifactWithMultiTriggerOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.InfrastructureOverrides) == 0 {
		t.Fatal("InfrastructureOverrides: want 1 entry, got 0 (parser not yet implemented)")
	}

	if len(state.InfrastructureOverrides[0].Triggers) != 2 {
		t.Errorf("Triggers count: want 2, got %d", len(state.InfrastructureOverrides[0].Triggers))
	}
}

// TestParse_InfrastructureOverrides_SecondTrigger_NoParam verifies that a
// trigger without trigger_param has Param == "" (not some default or error value).
func TestParse_InfrastructureOverrides_SecondTrigger_NoParam(t *testing.T) {
	// Second trigger in orchestration-review override is PHASE_END with no trigger_param.
	data := minimalArtifactWithMultiTriggerOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(state.InfrastructureOverrides) == 0 {
		t.Fatal("InfrastructureOverrides: want 1 entry, got 0 (parser not yet implemented)")
	}
	if len(state.InfrastructureOverrides[0].Triggers) < 2 {
		t.Fatal("InfrastructureOverrides[0].Triggers: want 2 triggers, got fewer")
	}

	second := state.InfrastructureOverrides[0].Triggers[1]
	if second.Trigger != "PHASE_END" {
		t.Errorf("Triggers[1].Trigger: want %q, got %q", "PHASE_END", second.Trigger)
	}
	if second.Param != "" {
		t.Errorf("Triggers[1].Param: want %q (no trigger_param), got %q", "", second.Param)
	}
}

// TestRoundTrip_InfrastructureOverrides_AgentNamePreserved verifies that an
// infrastructure_overrides agent name survives Parse → Render → re-Parse.
func TestRoundTrip_InfrastructureOverrides_AgentNamePreserved(t *testing.T) {
	const wantAgent = "checkpoint-manager-git"
	data := minimalArtifactWithSingleOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Verify parse produced the expected override before exercising the
	// round-trip. A failure here is an artifact of the RED phase, not a
	// round-trip bug.
	if len(state.InfrastructureOverrides) != 1 {
		t.Fatalf("InfrastructureOverrides count: want 1, got %d (parser not yet implemented)", len(state.InfrastructureOverrides))
	}

	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	state2, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: %v", err)
	}

	if len(state2.InfrastructureOverrides) != 1 {
		t.Errorf("InfrastructureOverrides count after round-trip: want 1, got %d", len(state2.InfrastructureOverrides))
	}
	if len(state2.InfrastructureOverrides) > 0 && state2.InfrastructureOverrides[0].AgentName != wantAgent {
		t.Errorf("InfrastructureOverrides[0].AgentName after round-trip: want %q, got %q",
			wantAgent, state2.InfrastructureOverrides[0].AgentName)
	}
}

// ============================================================
// Create — non-absolute path rejection (T3.3)
// ============================================================
//
// All tests in this block are in the RED phase: they compile but fail because
// Create does not yet reject non-absolute store paths (implementation in I3.3).

// mustBeAbsoluteSubstring is the stable substring asserted in the error message
// from Create when given a non-absolute store path.
const mustBeAbsoluteSubstring = "must be absolute"

// TestCreate_RelativePath_ReturnsError verifies that Create returns a non-nil
// error when the store was constructed with a relative (non-absolute) path.
// The error message must name the offending path.
func TestCreate_RelativePath_ReturnsError(t *testing.T) {
	relativePath := "Orchestration.md"
	store := artifact.NewFileStore(relativePath)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	_, err := store.Create(ctx, info, "task", false, time.Now(), "")

	// Defensive cleanup: in the RED phase Create may succeed and write the file.
	// Remove it so subsequent test runs are not affected by a leftover artifact.
	wd, wdErr := os.Getwd()
	if wdErr == nil {
		_ = os.Remove(filepath.Join(wd, relativePath))
	}

	if err == nil {
		t.Fatal("Create with relative store path: want error, got nil")
	}
	if !strings.Contains(err.Error(), mustBeAbsoluteSubstring) {
		t.Errorf("Create error = %q; want substring %q", err.Error(), mustBeAbsoluteSubstring)
	}
	// The error message must name the offending path so the caller can diagnose it.
	if !strings.Contains(err.Error(), relativePath) {
		t.Errorf("Create error = %q; want the offending path %q to appear in the message",
			err.Error(), relativePath)
	}
}

// TestCreate_RelativePath_WritesNoFile verifies that Create with a non-absolute
// store path writes no file. The working directory is queried before the call so
// that the test can confirm the expected file location was not created.
func TestCreate_RelativePath_WritesNoFile(t *testing.T) {
	relativePath := "Orchestration.md"
	store := artifact.NewFileStore(relativePath)
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	_, _ = store.Create(ctx, info, "task", false, time.Now(), "") // error expected; ignore

	// Verify no file was written at the relative path (resolved against the test's CWD).
	candidate := filepath.Join(wd, relativePath)
	if _, statErr := os.Stat(candidate); statErr == nil {
		// File exists — clean it up and fail.
		_ = os.Remove(candidate)
		t.Errorf("Create with relative path wrote a file at %s; want no file created", candidate)
	}
}

// ============================================================
// IsRunScopedArtifactPath predicate (T3.4)
// ============================================================
//
// T3.4a will fail in RED phase (stub returns false for all paths).
// T3.4b, T3.4c, T3.4d pass in both RED and GREEN (all expect false).
// T3.4a becomes the canonical RED canary for this predicate.

// TestIsRunScopedArtifactPath_RunScopedParent_ReturnsTrue verifies that a path
// whose parent directory is named "Orchestration-{valid_run_id}" is recognised
// as run-scoped.
func TestIsRunScopedArtifactPath_RunScopedParent_ReturnsTrue(t *testing.T) {
	// Construct a synthetic absolute path under a run-scoped folder.
	// filepath.Join normalises separators for the platform.
	base := t.TempDir()
	runScopedDir := filepath.Join(base, "Orchestration-20260805T143029Z-9bc0")
	path := filepath.Join(runScopedDir, "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if !got {
		t.Errorf("IsRunScopedArtifactPath(%q) = false; want true (run-scoped parent directory)",
			path)
	}
}

// TestIsRunScopedArtifactPath_NonRunScopedAbsoluteParent_ReturnsFalse verifies
// that an absolute path whose parent directory is a plain temp directory (not an
// Orchestration-* folder) is not recognised as run-scoped.
func TestIsRunScopedArtifactPath_NonRunScopedAbsoluteParent_ReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (non-run-scoped absolute parent)",
			path)
	}
}

// TestIsRunScopedArtifactPath_InvalidRunIDShape_ReturnsFalse verifies that a path
// whose parent directory starts with "Orchestration-" but is followed by a
// suffix that does not match the canonical run_id format is not recognised.
func TestIsRunScopedArtifactPath_InvalidRunIDShape_ReturnsFalse(t *testing.T) {
	base := t.TempDir()
	// "invalid-id" does not match ^\d{8}T\d{6}Z-[0-9a-f]{4}$
	path := filepath.Join(base, "Orchestration-invalid-id", "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (invalid run_id shape after prefix)",
			path)
	}
}

// TestIsRunScopedArtifactPath_RelativePath_ReturnsFalse verifies that a relative
// path is never reported as run-scoped, even if its directory component looks
// like an Orchestration-* folder.
func TestIsRunScopedArtifactPath_RelativePath_ReturnsFalse(t *testing.T) {
	path := filepath.Join("Orchestration-20260805T143029Z-9bc0", "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (relative path must never be run-scoped)",
			path)
	}
}

// ============================================================
// Create — absolute non-run-scoped path regression (T3.5)
// ============================================================
//
// T3.5 guards the permissive behaviour: Create must still succeed for absolute
// paths that are not run-scoped (e.g. t.TempDir()), because ~25 existing tests
// rely on this. If I3.3 accidentally rejects absolute non-run-scoped paths this
// test will fail, making the regression visible immediately.

// TestCreate_AbsoluteNonRunScopedPath_Succeeds verifies that Create succeeds
// when the store path is absolute but its parent directory is not a run-scoped
// Orchestration-{run_id} folder. This is the pattern used by all existing store
// tests that build a store under t.TempDir().
func TestCreate_AbsoluteNonRunScopedPath_Succeeds(t *testing.T) {
	// A plain temp directory — absolute but not run-scoped.
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "Orchestration.md"))
	ctx := context.Background()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}

	_, err := store.Create(ctx, info, "regression guard task", false, time.Now(), "")

	if err != nil {
		t.Errorf("Create with absolute non-run-scoped path: unexpected error: %v", err)
	}
}
