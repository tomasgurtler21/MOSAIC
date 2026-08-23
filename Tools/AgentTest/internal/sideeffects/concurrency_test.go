package sideeffects_test

// Concurrent-use safety test for the sideeffects applier.
//
// The composition root hands one applier value to every attempt. Under a
// concurrency bound above 1, Apply is called from many goroutines at once
// against the same value. This test drives that scenario under the race
// detector to confirm that no shared mutable state exists in the applier.
//
// The applier holds only its fixtures.Resolver reference, set once at
// construction and never written thereafter. This test catches a future
// addition of mutable state (for example, a write ledger accumulated across
// calls) that would silently become a data race under concurrent runs.
//
// Run with -race.

import (
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/sideeffects"
)

// TestApplier_ConcurrentApply_ProducesNoDataRaces runs many goroutines each
// calling Apply on the same shared applier with an empty effects slice. An
// empty slice produces an empty ledger without any filesystem writes, so the
// test isolates the applier's field-access path rather than competing on the
// filesystem. The race detector will flag any unguarded concurrent read or
// write to applier fields.
//
// Scenario: the composition root constructs one applier and passes it to every
// attempt's runner.Deps; under a concurrency bound above 1, all attempts call
// Apply simultaneously. The applier's contract is "holds only its resolver
// reference", which is immutable after construction — this test turns that
// claim into an enforced contract that fails if the applier later gains
// mutable state.
func TestApplier_ConcurrentApply_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	resolver, err := fixtures.NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	applier := sideeffects.NewApplier(resolver)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// An empty effects slice produces an empty ledger without touching
			// the filesystem. The race detector verifies no goroutine writes
			// to applier fields; the result is discarded as it is not the focus.
			applier.Apply(t.TempDir(), []domain.FileEffect{}) //nolint:errcheck
		}()
	}
	wg.Wait()
}
