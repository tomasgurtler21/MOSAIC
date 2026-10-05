package artifact_test

// Tests for fileStore.Apply: execution log, artifact registry, current state,
// global sequence, and workflow notes preservation.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

func TestApply_ExecutionLog_GrowsByOne(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(3, "planner#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "planner-tdd-soft#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "test-writer#1", "EXECUTION", "Stage-1", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), []string{"Plan.md"})

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

	step1 := newTestStep(1, "test-writer#1", "EXECUTION", "Stage-1", domain.StatusSUCCESS, now, []string{"Stage-1/PlanProgress.md"})
	state1, err := store.Apply(ctx, state, step1)
	if err != nil {
		t.Fatalf("Apply step1: %v", err)
	}

	step2 := newTestStep(2, "test-writer#2", "EXECUTION", "Stage-1", domain.StatusSUCCESS, now.Add(time.Minute), []string{"Stage-1/PlanProgress.md"})
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
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "test-writer#1", "EXECUTION", "Stage-2", domain.StatusSUCCESS, time.Now(), nil)

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
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusCOMPLETED_NEEDS_ACTION, time.Now(), nil)

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
	step := newTestStep(1, "planner-tdd-soft#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.CurrentState.LastAgent != "planner-tdd-soft#1" {
		t.Errorf("CurrentState.LastAgent: want %q, got %q", "planner-tdd-soft#1", after.CurrentState.LastAgent)
	}
}

func TestApply_GlobalSequence_EqualsStepSeq(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := newTestStep(3, "planner#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.GlobalSequence != 3 {
		t.Errorf("GlobalSequence: want step.Seq 3 (last-allocated semantics), got %d", after.GlobalSequence)
	}
}

func TestApply_GlobalSequence_StoredHigherThanStepSeq_KeepsStored(t *testing.T) {
	// A stored value above the step Seq marks an interrupted allocation; the
	// stored number must never move backwards: global_sequence = max(stored, Seq).
	store, _ := writeArtifact(t, withGlobalSequence("7"))
	ctx := context.Background()
	state, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	step := newTestStep(2, "planner#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if after.GlobalSequence != 7 {
		t.Errorf("GlobalSequence: want max(7, 2) = 7, got %d", after.GlobalSequence)
	}
}

func TestApply_SeqNotAboveLoggedSeq_RefusedNothingWritten(t *testing.T) {
	cases := []struct {
		name string
		seq  int
	}{
		{"duplicate of highest logged seq", 1},
		{"below highest logged seq", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, path := writeArtifact(t, minimalArtifactWithExecutionRow(""))
			ctx := context.Background()
			state, err := store.Read(ctx)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			step := newTestStep(tc.seq, fmt.Sprintf("planner#%d", tc.seq), "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

			_, err = store.Apply(ctx, state, step)

			if err == nil {
				t.Fatal("Apply: want error for Seq not above the highest logged Seq, got nil")
			}
			after, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("ReadFile: %v", rerr)
			}
			if !bytes.Equal(before, after) {
				t.Error("artifact changed on disk despite refused Apply")
			}
		})
	}
}

func TestApply_AgentInstanceSuffixMismatchesSeq_RefusedNothingWritten(t *testing.T) {
	cases := []struct {
		name  string
		agent string
	}{
		{"suffix differs from seq", "planner#1"},
		{"no numeric suffix", "planner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, path := writeArtifact(t, minimalArtifactWithExecutionRow(""))
			ctx := context.Background()
			state, err := store.Read(ctx)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			step := newTestStep(2, tc.agent, "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

			_, err = store.Apply(ctx, state, step)

			if err == nil {
				t.Fatal("Apply: want error when AgentInstance does not end in #Seq, got nil")
			}
			after, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("ReadFile: %v", rerr)
			}
			if !bytes.Equal(before, after) {
				t.Error("artifact changed on disk despite refused Apply")
			}
		})
	}
}

func TestApply_LastUpdated_Set(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, ts, nil)

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
	seedState, err := tempStore.Create(ctx, info, state.Task, domain.RunSettings{Checkpoints: state.Checkpoints}, state.Started, testRunID)
	if err != nil {
		t.Fatalf("Create temp: %v", err)
	}
	// Manually inject notes into the state before applying
	// (the Create function starts with empty notes; we pre-populate them
	// to simulate a state that already has workflow notes).
	seedState.WorkflowNotes = originalNotes

	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
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
