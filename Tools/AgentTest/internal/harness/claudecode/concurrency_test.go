package claudecode_test

// Concurrent-use safety test for the claudecode harness adapter.
//
// The composition root hands one adapter value to every attempt. Under a
// concurrency bound above 1, TranslateCall is called from many goroutines at
// once against the same value. This test drives that scenario under the race
// detector to confirm that no shared mutable state exists in the adapter.
//
// The adapter holds only its Options value, set once at construction and never
// written thereafter. This test catches a future addition of mutable state
// that would silently become a data race under concurrent runs.
//
// Run with -race.

import (
	"encoding/json"
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// TestClaudecodeAdapter_ConcurrentTranslateCall_ProducesNoDataRaces runs many
// goroutines each calling TranslateCall on the same shared adapter. The race
// detector will flag any unguarded concurrent read or write to adapter fields.
//
// Scenario: the composition root constructs one adapter and passes it to every
// attempt's runner.Deps; under a concurrency bound above 1, all attempts call
// TranslateCall simultaneously against the same value. The adapter's contract
// is "holds only its options value", which is immutable after construction —
// this test turns that claim into an enforced contract that fails if the
// adapter later gains mutable state.
func TestClaudecodeAdapter_ConcurrentTranslateCall_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	adapter := claudecode.New(claudecode.Options{})

	// Build a valid pre-phase payload. hook_event_name must be "PreToolUse",
	// tool_name must be the adapter's native dispatch-tool name, and
	// tool_input may be an empty object (decodes to the zero TaskToolInput).
	payload, err := json.Marshal(map[string]interface{}{
		"hook_event_name": "PreToolUse",
		"tool_name":       claudecode.NativeDispatchToolName,
		"tool_input":      map[string]string{},
		"tool_use_id":     "concurrent-test-token",
	})
	if err != nil {
		t.Fatalf("marshalling test payload: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// The race detector verifies no goroutine writes to adapter.opts.
			// TranslateCall reads only immutable state; this test catches any
			// future mutable-state addition that would make sharing unsafe.
			//
			// Errors are expected here (the adapter needs a real session ID
			// and other fields for some code paths) but are not the focus:
			// the race detector's verdict is the test's output.
			adapter.TranslateCall(domain.PhasePre, payload) //nolint:errcheck
		}()
	}
	wg.Wait()
}
