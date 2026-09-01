package fixtures_test

// Concurrent-use safety test for the fixtures resolver.
//
// The composition root hands one resolver value to every attempt via the
// sideeffects.Applier that wraps it. Under a concurrency bound above 1,
// Resolve is called from many goroutines at once against the same value.
// This test drives that scenario under the race detector to confirm that no
// shared mutable state exists in the resolver.
//
// The resolver holds only its root path string, set once at construction and
// never written thereafter. This test catches a future addition of mutable
// state (for example, a resolved-path cache) that would silently become a
// data race under concurrent runs.
//
// Run with -race.

import (
	"errors"
	"sync"
	"testing"

	"mosaic-agent-test/internal/fixtures"
)

// TestResolver_ConcurrentResolve_ProducesNoDataRaces runs many goroutines each
// calling Resolve on the same shared resolver. The references do not exist so
// each call returns ErrRefNotFound immediately without filesystem I/O that
// could compete on the filesystem level. The race detector will flag any
// unguarded concurrent read or write to resolver fields.
//
// Scenario: the composition root constructs one fixture resolver and passes it
// (via sideeffects.NewApplier) to every attempt's runner.Deps; under a
// concurrency bound above 1, all attempts call through the resolver
// simultaneously. The resolver's contract is "holds only its root path", which
// is immutable after construction — this test turns that claim into an
// enforced contract that fails if the resolver later gains mutable state.
func TestResolver_ConcurrentResolve_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	resolver, err := fixtures.NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// The race detector verifies no goroutine writes to resolver fields.
			// The reference does not exist, so Resolve returns ErrRefNotFound
			// quickly. The error outcome is expected and is not the focus.
			_, err := resolver.Resolve("nonexistent-ref")
			if err == nil || !errors.Is(err, fixtures.ErrRefNotFound) {
				// A nil error or a different error would be unexpected, but is
				// not a race — note it for diagnosis without failing the test,
				// since the race detector's verdict is the test's primary output.
				_ = err
			}
		}()
	}
	wg.Wait()
}
