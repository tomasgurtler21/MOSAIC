package artifact_test

// Tests for fileStore.Apply: infrastructure steps, inputs propagation,
// and RunSettings persistence through Apply.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

// ---- Apply: infrastructure steps and recorded position ----
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

	workflowStep := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	afterWorkflow, err := store.Apply(ctx, state, workflowStep)
	if err != nil {
		t.Fatalf("Apply (workflow step): %v", err)
	}

	infraStep := newTestStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
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
	workflowStep := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, ts1, nil)
	afterWorkflow, err := store.Apply(ctx, state, workflowStep)
	if err != nil {
		t.Fatalf("Apply (workflow step): %v", err)
	}

	ts2 := ts1.Add(time.Hour)
	infraStep := newTestStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, ts2, nil)
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

	infraStep := newTestStep(1, "checkpoint-manager-git#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(),
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
		newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil))
	if err != nil {
		t.Fatalf("Apply (planner): %v", err)
	}

	infraStep := newTestStep(2, "checkpoint-manager-git#2", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)
	infraStep.IsInfrastructure = true
	afterInfra, err := store.Apply(ctx, afterA, infraStep)
	if err != nil {
		t.Fatalf("Apply (checkpoint-manager-git): %v", err)
	}

	if _, err := store.Apply(ctx, afterInfra,
		newTestStep(3, "plan-review#3", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)); err != nil {
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

// ---- Apply: inputs propagation ----

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

// ---- Apply: RunSettings persisted through disk ----

func TestApply_RunSettings_ModePersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, Mode must equal the value that was in
	// the state passed to Apply.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.Mode = domain.ExecutionModeAuto
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if onDisk.Mode != domain.ExecutionModeAuto {
		t.Errorf("on-disk Mode: want %q (persisted through Apply), got %q",
			domain.ExecutionModeAuto, onDisk.Mode)
	}
}

func TestApply_RunSettings_CommitsPersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, Commits must equal the value that was
	// in the state passed to Apply.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.Commits = true
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if !onDisk.Commits {
		t.Error("on-disk Commits: want true (persisted through Apply), got false")
	}
}

func TestApply_RunSettings_CommitBranchVariantPersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, CommitBranchVariant must equal the
	// value that was in the state passed to Apply when commits are enabled.
	// CommitBranchVariant is only meaningful when Commits=true; Render omits
	// the key when commits are disabled so this test must enable commits to
	// exercise the meaningful round-trip case.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.Commits = true
	state.CommitBranchVariant = domain.CommitBranchUserOwn
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if onDisk.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("on-disk CommitBranchVariant: want %q (persisted through Apply), got %q",
			domain.CommitBranchUserOwn, onDisk.CommitBranchVariant)
	}
}

func TestApply_RunSettings_CommitBranchPersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, CommitBranch must equal the value that
	// was in the state passed to Apply.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.CommitBranch = "mosaic/run/testrun"
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if onDisk.CommitBranch != "mosaic/run/testrun" {
		t.Errorf("on-disk CommitBranch: want %q (persisted through Apply), got %q",
			"mosaic/run/testrun", onDisk.CommitBranch)
	}
}

func TestApply_RunSettings_PreConsultationPersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, PreConsultation must equal the value
	// that was in the state passed to Apply.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.PreConsultation = true
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if !onDisk.PreConsultation {
		t.Error("on-disk PreConsultation: want true (persisted through Apply), got false")
	}
}

func TestApply_RunSettings_ManualResolutionPersistsOnDisk(t *testing.T) {
	// After Apply and a subsequent Read, ManualResolution must equal the value
	// that was in the state passed to Apply.
	store, state := mustCreateStore(t)
	ctx := context.Background()
	state.ManualResolution = true
	step := newTestStep(1, "planner#1", "PLANNING", "", domain.StatusSUCCESS, time.Now(), nil)

	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	onDisk, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Apply: unexpected error: %v", err)
	}

	if !onDisk.ManualResolution {
		t.Error("on-disk ManualResolution: want true (persisted through Apply), got false")
	}
}
