package artifact_test

// Tests for fileStore.SetPhase: phase update, sequence bump, timestamp,
// execution log / registry immutability, round-trip, workflow notes, and
// casing preservation.

import (
	"context"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

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
