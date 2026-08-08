package runstate_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

func newState() domain.RunState {
	return domain.RunState{
		SchemaVersion:        1,
		TestID:               "example-test",
		RunNumber:            1,
		CollaboratorCounters: map[string]int{},
		PendingStubs:         map[string]domain.PendingStub{},
		InFlight:             map[string]domain.InFlight{},
	}
}

func TestInitialize_ThenRead_RoundTripsTheDocument(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	want := newState()

	// Act
	if err := store.Initialize(want); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	got, err := store.Read()

	// Assert
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if got.TestID != want.TestID || got.RunNumber != want.RunNumber {
		t.Fatalf("Read did not round-trip Initialize's document: got %+v, want %+v", got, want)
	}
}

func TestInitialize_FailsWhenAStateDocumentAlreadyExists(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("first Initialize returned unexpected error: %v", err)
	}

	// Act
	err := store.Initialize(newState())

	// Assert
	if err == nil {
		t.Fatal("expected a second Initialize for the same run to fail, got nil error")
	}
}

func TestRead_ReturnsErrStateAbsent_WhenSetupNeverRan(t *testing.T) {
	// Arrange
	store := runstate.NewStore(t.TempDir(), newFakeClock())

	// Act
	_, err := store.Read()

	// Assert
	if !errors.Is(err, runstate.ErrStateAbsent) {
		t.Fatalf("expected ErrStateAbsent, got %v", err)
	}
}

func TestRead_ReturnsErrStateCorrupt_WhenTheDocumentIsMalformedJSON(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain.StateFileName), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("failed to corrupt the state document: %v", err)
	}

	// Act
	_, err := store.Read()

	// Assert
	if !errors.Is(err, runstate.ErrStateCorrupt) {
		t.Fatalf("expected ErrStateCorrupt, got %v", err)
	}
}

func TestRead_ReturnsErrStateUnreadable_WhenTheDocumentCannotBeOpened(t *testing.T) {
	// Arrange: a directory occupying the state document's path can never be
	// opened as a file, which is a distinct condition from "absent" (setup
	// never ran) and "corrupt" (setup ran and the content is broken).
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	if err := os.Mkdir(filepath.Join(dir, domain.StateFileName), 0o755); err != nil {
		t.Fatalf("failed to place a directory at the state document's path: %v", err)
	}

	// Act
	_, err := store.Read()

	// Assert
	if !errors.Is(err, runstate.ErrStateUnreadable) {
		t.Fatalf("expected ErrStateUnreadable, got %v", err)
	}
	if errors.Is(err, runstate.ErrStateAbsent) || errors.Is(err, runstate.ErrStateCorrupt) {
		t.Fatalf("ErrStateUnreadable must not also satisfy ErrStateAbsent or ErrStateCorrupt, got %v", err)
	}
}

func TestUpdate_ConcurrentReadModifyWriteCyclesLoseNoIncrements(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	const goroutines = 20
	const incrementsEach = 10

	// Act: many independent processes/goroutines racing to increment the
	// same counter through the same guarded read-modify-write path.
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsEach; j++ {
				_, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
					s.SequenceCounter++
					return s, nil
				})
				if err != nil {
					t.Errorf("Update returned unexpected error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Assert
	final, err := store.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	want := goroutines * incrementsEach
	if final.SequenceCounter != want {
		t.Fatalf("lost updates under concurrency: got SequenceCounter=%d, want %d", final.SequenceCounter, want)
	}
}

func TestUpdate_WhenMutateReturnsAnError_CommitsNothingAndReleasesTheLock(t *testing.T) {
	// Arrange: mutate returning an error must not corrupt the stored state,
	// and must not leave the lock held — a stuck lock on this path would
	// deadlock every subsequent Update on the live agent-turn critical path.
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	seed := newState()
	seed.SequenceCounter = 5
	if err := store.Initialize(seed); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	sentinel := errors.New("mutate: deliberate failure")

	// Act
	_, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		s.SequenceCounter = 999
		return s, sentinel
	})

	// Assert: the error propagates unchanged.
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the sentinel mutate error to propagate, got %v", err)
	}

	// Assert: nothing was committed — Read still returns the pre-Update state.
	got, readErr := store.Read()
	if readErr != nil {
		t.Fatalf("Read returned unexpected error: %v", readErr)
	}
	if got.SequenceCounter != 5 {
		t.Fatalf("mutate error must not commit: got SequenceCounter=%d, want 5 (unchanged)", got.SequenceCounter)
	}

	// Assert: the lock was released — a subsequent Update succeeds without
	// contention or timeout.
	result, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		s.SequenceCounter++
		return s, nil
	})
	if err != nil {
		t.Fatalf("subsequent Update returned unexpected error (lock may not have been released): %v", err)
	}
	if result.State.SequenceCounter != 6 {
		t.Fatalf("subsequent Update did not apply cleanly: got SequenceCounter=%d, want 6", result.State.SequenceCounter)
	}
}

func TestUpdate_LeftoverPartialCommitArtifactDoesNotCorruptWhatReadReturns(t *testing.T) {
	// Arrange: commits go through write-temp-then-rename. A crash between
	// those two steps leaves a temp artifact on disk but must never affect
	// what a reader observes.
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	seed := newState()
	seed.SequenceCounter = 1
	if err := store.Initialize(seed); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	leftover := filepath.Join(dir, domain.StateFileName+".tmp")
	if err := os.WriteFile(leftover, []byte("{ incomplete"), 0o644); err != nil {
		t.Fatalf("failed to plant a leftover partial-commit artifact: %v", err)
	}

	// Act
	got, err := store.Read()

	// Assert
	if err != nil {
		t.Fatalf("Read returned unexpected error in the presence of a leftover temp artifact: %v", err)
	}
	if got.SequenceCounter != 1 {
		t.Fatalf("Read must return the last successfully committed state, got SequenceCounter=%d, want 1", got.SequenceCounter)
	}
}
