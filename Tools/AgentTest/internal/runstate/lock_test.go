package runstate_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

func writeLockFile(t *testing.T, dir string, info domain.LockInfo) {
	t.Helper()
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal lock info: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain.LockFileName), b, 0o644); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}
}

func TestUpdate_AcquiresTheLockExclusivelyWhenUncontended(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := runstate.NewStore(dir, newFakeClock())
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	// Act
	result, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		s.SequenceCounter = 1
		return s, nil
	})

	// Assert
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}
	if result.LockReclaimed {
		t.Fatalf("an uncontended acquisition must not report LockReclaimed")
	}
	if result.State.SequenceCounter != 1 {
		t.Fatalf("Update did not commit the mutated state: got %+v", result.State)
	}
}

func TestUpdate_ContendedAcquisitionSucceedsBeforeTheAcquisitionTimeout(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	clock := newFakeClock()
	holder := runstate.NewStore(dir, clock)
	waiter := runstate.NewStore(dir, clock)
	if err := holder.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	release := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, err := holder.Update(func(s domain.RunState) (domain.RunState, error) {
			<-release // hold the lock until the waiter has had a chance to contend
			s.SequenceCounter = 1
			return s, nil
		})
		if err != nil {
			t.Errorf("holder Update returned unexpected error: %v", err)
		}
	}()

	// Give the holder a real-time head start so it wins initial acquisition.
	time.Sleep(20 * time.Millisecond)
	close(release) // release well within the 5s acquisition timeout

	// Act
	result, err := waiter.Update(func(s domain.RunState) (domain.RunState, error) {
		s.SequenceCounter = 2
		return s, nil
	})
	<-holderDone

	// Assert
	if err != nil {
		t.Fatalf("waiter Update returned unexpected error after contended acquisition: %v", err)
	}
	if result.LockReclaimed {
		t.Fatalf("acquiring after a normal release must not report LockReclaimed")
	}
}

func TestUpdate_FailsCleanlyOnceAcquisitionExceedsTheTimeout(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	clock := newFakeClock()
	holder := runstate.NewStore(dir, clock)
	waiter := runstate.NewStore(dir, clock)
	if err := holder.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	release := make(chan struct{})
	holderDone := make(chan struct{})
	defer func() {
		close(release)  // unblock the holder goroutine
		<-holderDone    // and wait for its Update call to finish releasing the lock
		// before returning, so t.TempDir()'s cleanup never races the holder's
		// still-in-flight file operations.
	}()
	holderStarted := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, _ = holder.Update(func(s domain.RunState) (domain.RunState, error) {
			close(holderStarted)
			<-release // never released during this test: the waiter must time out
			return s, nil
		})
	}()
	<-holderStarted

	stopTicking := clock.startTicking(500 * time.Millisecond) // fast-forward past the 5s timeout
	defer stopTicking()

	// Act
	_, err := waiter.Update(func(s domain.RunState) (domain.RunState, error) {
		return s, nil
	})

	// Assert
	if !errors.Is(err, runstate.ErrLockTimeout) {
		t.Fatalf("expected ErrLockTimeout once acquisition exceeds %s, got %v", runstate.AcquireTimeout, err)
	}
}

func TestUpdate_ReclaimsALockHeldByAVerifiablyDeadHolder(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	clock := newFakeClock()
	store := runstate.NewStore(dir, clock)
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	deadHolder := domain.LockInfo{PID: 999999992, Host: "nowhere", AcquiredAt: clock.Now(), ControlDir: dir}
	writeLockFile(t, dir, deadHolder)

	// Act
	result, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		s.SequenceCounter = 1
		return s, nil
	})

	// Assert
	if err != nil {
		t.Fatalf("Update returned unexpected error reclaiming a dead holder's lock: %v", err)
	}
	if !result.LockReclaimed {
		t.Fatal("expected LockReclaimed to be true when the prior holder is verifiably dead")
	}
	if result.PriorHolder.PID != deadHolder.PID {
		t.Fatalf("expected PriorHolder to name the dead holder, got %+v", result.PriorHolder)
	}
}

func TestUpdate_ReclaimsALockOlderThanTheStalenessThreshold(t *testing.T) {
	// Arrange: a live-looking holder (this process's own PID) whose lock has
	// simply aged past the staleness threshold must still be reclaimable —
	// staleness is a standalone reclamation condition, not merely a proxy
	// for liveness.
	dir := t.TempDir()
	clock := newFakeClock()
	store := runstate.NewStore(dir, clock)
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	staleHolder := domain.LockInfo{PID: os.Getpid(), Host: "here", AcquiredAt: clock.Now(), ControlDir: dir}
	writeLockFile(t, dir, staleHolder)
	clock.Advance(31 * time.Second)

	// Act
	result, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		return s, nil
	})

	// Assert
	if err != nil {
		t.Fatalf("Update returned unexpected error reclaiming a stale lock: %v", err)
	}
	if !result.LockReclaimed {
		t.Fatal("expected LockReclaimed to be true once the lock exceeds the staleness threshold")
	}
}

func TestUpdate_NeverDisplacesAHealthyHolder(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	clock := newFakeClock()
	holder := runstate.NewStore(dir, clock)
	waiter := runstate.NewStore(dir, clock)
	if err := holder.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}

	release := make(chan struct{})
	holderStarted := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, err := holder.Update(func(s domain.RunState) (domain.RunState, error) {
			close(holderStarted)
			<-release
			return s, nil
		})
		if err != nil {
			t.Errorf("holder Update returned unexpected error: %v", err)
		}
	}()
	<-holderStarted

	waiterResult := make(chan struct{})
	go func() {
		defer close(waiterResult)
		result, err := waiter.Update(func(s domain.RunState) (domain.RunState, error) {
			return s, nil
		})
		if err != nil {
			t.Errorf("waiter Update returned unexpected error: %v", err)
			return
		}
		if result.LockReclaimed {
			t.Errorf("a healthy holder's lock must never be reported as reclaimed")
		}
	}()

	// Act & Assert: the waiter must still be blocked well before the
	// timeout while the holder is healthy and the lock is fresh.
	select {
	case <-waiterResult:
		t.Fatal("waiter acquired the lock while a healthy holder still held it")
	case <-time.After(200 * time.Millisecond):
		// expected: still contending
	}

	close(release)
	<-holderDone
	<-waiterResult
}

func TestUpdate_SimultaneousReclamationAttemptsResultInExactlyOneHolderAtATime(t *testing.T) {
	// Arrange: pre-plant a lock held by a verifiably dead holder so every
	// contender races to reclaim it at once.
	dir := t.TempDir()
	clock := newFakeClock()
	if err := runstate.NewStore(dir, clock).Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	writeLockFile(t, dir, domain.LockInfo{PID: 999999993, Host: "nowhere", AcquiredAt: clock.Now(), ControlDir: dir})

	const contenders = 8
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := runstate.NewStore(dir, clock)
			_, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
				s.SequenceCounter++
				return s, nil
			})
			if err != nil {
				t.Errorf("contender Update returned unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Assert: mutual exclusion held throughout the reclamation contest, so
	// exactly `contenders` increments landed with none lost or duplicated —
	// the observable proof that exactly one goroutine ever held the lock at
	// a time, even while several raced to reclaim it.
	final, err := runstate.NewStore(dir, clock).Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if final.SequenceCounter != contenders {
		t.Fatalf("simultaneous reclamation lost mutual exclusion: got SequenceCounter=%d, want %d", final.SequenceCounter, contenders)
	}
}
