package artifact_test

// Tests for artifact.Parse: CurrentState, ExecutionLog, ArtifactRegistry,
// WorkflowNotes, Inputs column, and infrastructure_overrides block.

import (
	"os"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

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

// ---- Parse: Inputs column ----

func TestParse_CanonicalFile_ExecutionLog_FirstRow_DashInputs_ReturnsEmptyString(t *testing.T) {
	// Canonical fixture now has Inputs column; row 0 has "-" (no inputs).
	state := mustReadCanonical(t)

	if state.ExecutionLog[0].Inputs != "" {
		t.Errorf("ExecutionLog[0].Inputs: want %q for dash cell, got %q", "", state.ExecutionLog[0].Inputs)
	}
}

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

// ---- Parse: infrastructure_overrides frontmatter ----

func TestParse_InfrastructureOverrides_Absent_ReturnsNil(t *testing.T) {
	// minimalArtifactBytes (no overrides block) is the common baseline.
	data := minimalArtifactBytes(testRunID)

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	if state.InfrastructureOverrides != nil {
		t.Errorf("InfrastructureOverrides: want nil when block absent, got %v", state.InfrastructureOverrides)
	}
}

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
