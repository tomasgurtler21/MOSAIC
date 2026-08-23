package cli_test

// Concurrent-use safety test for the progress sink (cli.NewLineSink).
//
// The composition root passes one domain.ProgressSink to every attempt for a
// given run. Under a concurrency bound above 1, Emit is called from many
// goroutines at once against the same sink. This test drives that scenario
// under the race detector to confirm that the sink is safe for concurrent use
// as the domain.ProgressSink contract requires.
//
// The domain contract states: "Implementations must be safe for concurrent
// use." The lineSink guards its writer with a mutex; this test turns that
// implementation claim into an enforced contract that fails if the mutex is
// removed or a new unguarded field is added.
//
// Run with -race.

import (
	"io"
	"sync"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// TestLineSink_ConcurrentEmit_ProducesNoDataRaces runs many goroutines each
// calling Emit on the same shared sink. Events are discarded via io.Discard
// so no writer contention obscures the race detector's report. The race
// detector will flag any unguarded concurrent write to sink fields.
//
// Scenario: the composition root passes one progress sink to a composedSuiteRunner.Run
// call, which hands it to every concurrent attempt's runner.Deps via
// RunnerDeps; under a concurrency bound above 1, all attempts call Emit
// simultaneously on the same value. The domain.ProgressSink contract requires
// implementations to be safe for concurrent use — this test enforces that
// contract for the CLI's lineSink and catches any future regression.
func TestLineSink_ConcurrentEmit_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	sink := cli.NewLineSink(io.Discard)

	// A minimal progress event: kind ProgressSuiteStarted with no run fields
	// set. Its content is irrelevant — Emit's write path and the mutex
	// protecting it are what the race detector checks.
	ev := domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		TotalTests: 1,
		TotalRuns:  1,
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// The race detector verifies the mutex in lineSink correctly
			// serialises concurrent writes. Emit does not return an error;
			// the race detector's verdict is the test's output.
			sink.Emit(ev)
		}()
	}
	wg.Wait()
}
