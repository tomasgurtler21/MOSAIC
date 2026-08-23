package launch_test

// Concurrent-use safety test for the harness-neutral launcher.
//
// The composition root hands one launcher value to every attempt. Under a
// concurrency bound above 1, Launch is called from many goroutines at once
// against the same value. This test drives that scenario under the race
// detector to confirm that no shared mutable state exists in the launcher.
//
// The launcher holds only its Decoder function value and a config struct
// containing further function values, all set once at construction and never
// written thereafter. This test catches a future addition of mutable state
// (for example, a cached sink reference or connection pool) that would
// silently become a data race under concurrent runs.
//
// Run with -race.

import (
	"context"
	"errors"
	"sync"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/launch"
)

// TestLauncher_ConcurrentLaunch_ProducesNoDataRaces runs many goroutines each
// calling Launch on the same shared launcher. A resolver that fails immediately
// is injected so no subprocess is spawned and each call returns at once. The
// race detector will flag any unguarded concurrent read or write to launcher
// fields.
//
// Scenario: the composition root constructs one launcher and passes it to
// every attempt's runner.Deps; under a concurrency bound above 1, all attempts
// call Launch simultaneously. The launcher's contract is "holds only its
// decoder and config values", which are immutable after construction — this
// test turns that claim into an enforced contract that fails if the launcher
// later gains mutable state.
func TestLauncher_ConcurrentLaunch_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	// failingResolver fails immediately so Launch returns without spawning any
	// subprocess. The test exercises Launch's field-access path (reading dec
	// and cfg) without incurring real process creation overhead.
	failingResolver := func(_ string) (commonharness.Command, error) {
		return commonharness.Command{}, errors.New("concurrent-test: no executable to resolve")
	}

	// noopDecoder satisfies the Decoder parameter. It is never called because
	// the resolver above fails before the decoder is reached; it exists only
	// to satisfy the type.
	noopDecoder := func(_ commonharness.Response, err error) (domain.SubjectResult, error) {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, err
	}

	launcher := launch.New(noopDecoder, launch.WithResolveExecutable(failingResolver))

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// The race detector verifies no goroutine writes to launcher fields.
			// Launch reads only immutable state (dec, cfg); errors are expected
			// and are not the focus — the race detector's verdict is the output.
			launcher.Launch(context.Background(), domain.SpawnPlan{Executable: "nonexistent"}) //nolint:errcheck
		}()
	}
	wg.Wait()
}
