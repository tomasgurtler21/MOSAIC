package runstate_test

// Tests for the run-ID field on domain.RunState: what the driver persists
// at setup time is what a subsequent read observes, through both the direct
// Initialize+Read path and the lock-guarded read-modify-write Update path.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

// TestInitialize_ThenRead_RunIDRoundTripsTheDocument asserts that a RunState
// written by Initialize with a non-empty RunID is read back with that same
// RunID — the same round-trip guarantee the existing TestID and RunNumber
// fields already satisfy, extended to the new field.
func TestInitialize_ThenRead_RunIDRoundTripsTheDocument(t *testing.T) {
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	want := newState()
	want.RunID = "20260815T111456Z-6b68"

	if err := store.Initialize(want); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}

	if got.RunID != want.RunID {
		t.Errorf("RunID round-trip failed: got %q, want %q", got.RunID, want.RunID)
	}
}

// TestUpdate_RunIDSurvivesReadModifyWrite asserts that the lock-guarded
// read-modify-write path (the only way the run-state contract permits a
// write) does not discard or zero the RunID field. A partial or incorrect
// serialise/deserialise cycle would silently clear it, breaking every
// subsequent interceptor read.
func TestUpdate_RunIDSurvivesReadModifyWrite(t *testing.T) {
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	seed := newState()
	seed.RunID = "20260815T111456Z-6b68"

	if err := store.Initialize(seed); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	_, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		// Any mutation unrelated to RunID — the field must survive regardless.
		s.SequenceCounter++
		return s, nil
	})
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}

	if got.RunID != seed.RunID {
		t.Errorf("RunID must survive a read-modify-write cycle: got %q, want %q", got.RunID, seed.RunID)
	}
}

// TestInitialize_ThenRead_EmptyRunID_RoundTripsAsEmpty asserts that an empty
// RunID is persisted as empty and read back as empty. The best-effort
// semantics for the interceptor's run-state read require that a missed read
// yields an empty RunID (not an error), and expansion of an empty ID must
// yield an empty segment rather than a missing-field panic. Both behaviours
// depend on empty being a valid, distinguishable persisted value.
func TestInitialize_ThenRead_EmptyRunID_RoundTripsAsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	state := newState()
	// RunID deliberately left empty — the best-effort-miss path used when the
	// driver's setup never ran or the read failed.
	state.RunID = ""

	if err := store.Initialize(state); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}

	if got.RunID != "" {
		t.Errorf("empty RunID must round-trip as empty: got %q, want empty string", got.RunID)
	}
}
