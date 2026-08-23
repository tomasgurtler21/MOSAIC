package cost_test

// Concurrent-use safety test for the cost provider.
//
// The composition root hands one cost provider value to every attempt. Under a
// concurrency bound above 1, Cost is called from many goroutines at once
// against the same value. This test drives that scenario under the race
// detector to confirm that no shared mutable state exists in the provider.
//
// The provider holds only its Options value, set once at construction and
// never written thereafter. This test catches a future addition of mutable
// state (for example, a per-provider result cache or connection) that would
// silently become a data race under concurrent runs.
//
// Run with -race.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// TestCostProvider_ConcurrentCost_ProducesNoDataRaces runs many goroutines each
// calling Cost on the same shared provider. An Invoke function that fails
// immediately is injected so no subprocess is spawned and each call returns at
// once. The race detector will flag any unguarded concurrent read or write to
// provider fields.
//
// Scenario: the composition root constructs one cost provider and passes it to
// every attempt's runner.Deps; under a concurrency bound above 1, all attempts
// call Cost simultaneously. The provider's contract is "holds only its options
// value", which is immutable after construction — this test turns that claim
// into an enforced contract that fails if the provider later gains mutable
// state.
func TestCostProvider_ConcurrentCost_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	// failingInvoke fails immediately so Cost returns without spawning any
	// subprocess. Cost maps a non-nil invokeErr to a CostReport result (not
	// an error return), so goroutines return quickly without blocking.
	failingInvoke := func(_ context.Context, _ string, _ []string, _ string) ([]byte, int, error) {
		return nil, 1, errors.New("concurrent-test: no cost tool to invoke")
	}

	provider := cost.New(cost.Options{
		ExecutablePath: "/concurrent-test/nonexistent-tool",
		Invoke:         failingInvoke,
	})

	query := domain.CostQuery{
		RunID:   "concurrent-test-run",
		LogRoot: t.TempDir(),
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// The race detector verifies no goroutine writes to provider fields.
			// Cost reads only immutable opts state; the returned CostReport is
			// discarded — the race detector's verdict is the test's output.
			provider.Cost(context.Background(), query) //nolint:errcheck
		}()
	}
	wg.Wait()
}
