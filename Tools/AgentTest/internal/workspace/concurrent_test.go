package workspace_test

// Concurrent-create safety tests for workspace.Manager.
//
// These tests verify that the manager's create path is safe under simultaneous
// calls and that the non-empty-directory guard holds in all cases. They cover
// the concurrent-scheduler scenario where many repetitions, each running in
// its own goroutine, call Create at the same instant.

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/workspace"
)

// TestCreate_SafeUnderSimultaneousCallsForDistinctKeys verifies that many
// goroutines creating sandboxes for distinct run keys simultaneously all
// succeed, each producing a distinct sandbox root. No data race must be
// detectable by the race detector, and no two sandboxes may share a root.
//
// Run with -race to confirm synchronisation is complete.
func TestCreate_SafeUnderSimultaneousCallsForDistinctKeys(t *testing.T) {
	const goroutines = 20
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())

	sandboxes := make([]domain.Sandbox, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := domain.RunKey{
				RunID:     fmt.Sprintf("20260807T120000Z-%04x", i),
				TestID:    fmt.Sprintf("concurrent-test-%03d", i),
				RunNumber: 1,
			}
			sb, err := mgr.Create(key)
			sandboxes[i] = sb
			errs[i] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Create returned unexpected error: %v", i, err)
		}
	}

	roots := make(map[string]int, goroutines)
	for i, sb := range sandboxes {
		if sb.Root == "" {
			continue // already reported above
		}
		if prev, dup := roots[sb.Root]; dup {
			t.Errorf(
				"sandbox root collision: goroutines %d and %d both received %q — "+
					"concurrent runs must always receive distinct sandboxes",
				prev, i, sb.Root,
			)
		}
		roots[sb.Root] = i
	}
}

// TestCreate_SimultaneousCallsForSameKey_ExactlyOneSucceeds verifies that when
// many goroutines race to create the same run key, exactly one wins and all
// others receive an error. No two runs may share a sandbox root, so
// at-most-one-winner is a correctness property, not a performance one.
//
// Errors from the losing goroutines must be ErrSandboxExists or
// ErrDirectoryNotEmpty: both signal "this directory is already taken" and
// neither is an unexpected internal fault.
func TestCreate_SimultaneousCallsForSameKey_ExactlyOneSucceeds(t *testing.T) {
	const goroutines = 20
	mgr := workspace.NewManager(t.TempDir(), newFakeClock())
	key := domain.RunKey{
		RunID:     "20260807T120000Z-abcd",
		TestID:    "race-test",
		RunNumber: 1,
	}

	var mu sync.Mutex
	var successes int
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := mgr.Create(key)
			errs[i] = err
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf(
			"expected exactly 1 goroutine to succeed creating the same key, got %d — "+
				"concurrent creates for the same run key must not let two runs share a sandbox",
			successes,
		)
	}
	for i, err := range errs {
		if err != nil &&
			!errors.Is(err, workspace.ErrSandboxExists) &&
			!errors.Is(err, workspace.ErrDirectoryNotEmpty) {
			t.Errorf(
				"goroutine %d: losing Create returned %v, want ErrSandboxExists or ErrDirectoryNotEmpty — "+
					"only those two signal that the slot is taken",
				i, err,
			)
		}
	}
}

// TestCreate_RefusesNonEmptyDirectory_UnderConcurrentLoad verifies that the
// non-empty-directory guard holds even when many goroutines probe the same path
// simultaneously. The guard is what makes teardown a deletion rather than a
// restore protocol: if Create ever adopted existing content, a prior run's
// state could contaminate the new run.
//
// This test uses a second manager with no in-memory record of the key, so the
// ErrSandboxExists fast path is never taken — every call reaches the actual
// directory-emptiness check.
func TestCreate_RefusesNonEmptyDirectory_UnderConcurrentLoad(t *testing.T) {
	const goroutines = 10
	root := t.TempDir()

	// Populate the target directory via a first manager so it exists and is
	// non-empty before any of the concurrent goroutines run.
	firstMgr := workspace.NewManager(root, newFakeClock())
	key := domain.RunKey{
		RunID:     "20260807T120000Z-ff00",
		TestID:    "nonempty-test",
		RunNumber: 1,
	}
	if _, err := firstMgr.Create(key); err != nil {
		t.Fatalf("initial Create failed: %v", err)
	}

	// A second manager has no in-memory record of this key, so each Create
	// call must reach the directory-emptiness check directly.
	secondMgr := workspace.NewManager(root, newFakeClock())

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := secondMgr.Create(key)
			if err == nil {
				t.Errorf(
					"Create succeeded against a non-empty directory — " +
						"it must always refuse to adopt existing content regardless of concurrency",
				)
			}
		}()
	}
	wg.Wait()
}
