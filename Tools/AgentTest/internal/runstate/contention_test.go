package runstate_test

// Contention tests for the run-state lock and store.
//
// These tests drive concurrent Update calls against a shared store at a level
// above the concurrency bound the scheduler is designed to run at (four by
// default). The accommodations for Windows transient sharing-violation and
// access-denied errors on lock and state files — already present in the store
// implementation — are exercised at higher frequency under concurrency. These
// tests confirm those accommodations hold.
//
// Drive contention through the store's own seam (Update), not through real
// subprocess spawning, so the test is fast and deterministic about what it
// exercises.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

// contentionGoroutines is intentionally above the scheduler's intended default
// bound of four so the test exercises the Windows retry accommodations at a
// higher frequency than normal operation would produce.
const contentionGoroutines = 8

// realClock is a domain.Clock backed by the real wall clock. Used for
// contention tests where the lock protocol's backoff depends on actual elapsed
// time; a fake clock that never advances would cause ErrLockTimeout immediately
// because the deadline check fires before the first retry.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

var _ domain.Clock = realClock{}

// TestStore_Update_SafeUnderSimultaneousAccess drives many goroutines calling
// Update on the same store simultaneously. Each goroutine increments the
// SequenceCounter once; after all goroutines finish, the counter must equal
// the number of goroutines — no increment must be lost to a lost-update race
// or an unretried transient error.
//
// The store serialises updates through its lock-acquire loop, which includes
// backoff and reclaim. This test verifies that serialisation holds when the
// number of concurrent callers exceeds the scheduler's intended default bound.
func TestStore_Update_SafeUnderSimultaneousAccess(t *testing.T) {
	dir := t.TempDir()
	// Use the real wall clock: the lock protocol's backoff depends on real
	// time. The test is bounded by AcquireTimeout (5 s), which is well within
	// Go's default test timeout.
	store := runstate.NewStore(dir, realClock{})

	if err := store.Initialize(newStateForTest("contention-test")); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(contentionGoroutines)
	errs := make([]error, contentionGoroutines)
	for i := 0; i < contentionGoroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
				s.SequenceCounter++
				return s, nil
			})
			errs[i] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Update returned unexpected error: %v", i, err)
		}
	}

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read after concurrent updates returned unexpected error: %v", err)
	}
	if got.SequenceCounter != contentionGoroutines {
		t.Errorf(
			"SequenceCounter = %d after %d concurrent increments, want %d — "+
				"at least one increment was lost; the lock or state-commit path has a gap",
			got.SequenceCounter, contentionGoroutines, contentionGoroutines,
		)
	}
}

// TestStore_ManyDistinctStores_SafeUnderSimultaneousAccess drives many
// goroutines each operating on their own store simultaneously, to verify that
// concurrent use of many independent stores (one per run, as the scheduler
// creates them) does not introduce cross-store interference or shared mutable
// state.
func TestStore_ManyDistinctStores_SafeUnderSimultaneousAccess(t *testing.T) {
	stores := make([]runstate.Store, contentionGoroutines)
	for i := 0; i < contentionGoroutines; i++ {
		dir := t.TempDir()
		s := runstate.NewStore(dir, realClock{})
		if err := s.Initialize(newStateForTest(fmt.Sprintf("distinct-store-%03d", i))); err != nil {
			t.Fatalf("store %d: Initialize returned unexpected error: %v", i, err)
		}
		stores[i] = s
	}

	const updatesPerStore = 5
	var wg sync.WaitGroup
	wg.Add(contentionGoroutines)
	errs := make([]error, contentionGoroutines)
	for i := 0; i < contentionGoroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < updatesPerStore; j++ {
				if _, err := stores[i].Update(func(s domain.RunState) (domain.RunState, error) {
					s.SequenceCounter++
					return s, nil
				}); err != nil {
					errs[i] = err
					return
				}
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Update returned unexpected error: %v", i, err)
		}
	}

	for i, s := range stores {
		got, err := s.Read()
		if err != nil {
			t.Errorf("store %d: Read returned unexpected error: %v", i, err)
			continue
		}
		if got.SequenceCounter != updatesPerStore {
			t.Errorf(
				"store %d: SequenceCounter = %d, want %d — "+
					"an update was lost or cross-contaminated from another store",
				i, got.SequenceCounter, updatesPerStore,
			)
		}
	}
}

// TestStore_InitializeAndUpdate_ConcurrentlyAboveDefaultBound drives
// contentionGoroutines goroutines each initialising their own store and then
// updating it, all simultaneously. This exercises the file-system paths at a
// combined concurrency level that raises the frequency of the Windows transient
// sharing-violation and access-denied errors the store accommodates, confirming
// the retry loop handles them without returning ErrLockTimeout.
func TestStore_InitializeAndUpdate_ConcurrentlyAboveDefaultBound(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(contentionGoroutines)
	errs := make([]error, contentionGoroutines)

	for i := 0; i < contentionGoroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			dir := t.TempDir()
			s := runstate.NewStore(dir, realClock{})
			if err := s.Initialize(newStateForTest(fmt.Sprintf("concurrent-init-%03d", i))); err != nil {
				errs[i] = fmt.Errorf("Initialize: %w", err)
				return
			}
			if _, err := s.Update(func(st domain.RunState) (domain.RunState, error) {
				st.SequenceCounter = 1
				return st, nil
			}); err != nil {
				errs[i] = fmt.Errorf("Update: %w", err)
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

// newStateForTest returns a minimal, valid RunState for the given test ID,
// suitable for use in contention tests that do not exercise specific state
// fields beyond SequenceCounter.
func newStateForTest(testID string) domain.RunState {
	return domain.RunState{
		SchemaVersion:        1,
		TestID:               testID,
		RunNumber:            1,
		CollaboratorCounters: map[string]int{},
		PendingStubs:         map[string]domain.PendingStub{},
		InFlight:             map[string]domain.InFlight{},
	}
}
