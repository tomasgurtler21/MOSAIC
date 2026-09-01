package fake_test

// Concurrent-use safety test for the scripted fake adapter.
//
// The composition root hands one Adapter value to every attempt. Under a
// concurrency bound above 1, TranslateCall and TranslateOutcome are called
// from many goroutines at once against the same value. This test drives that
// scenario to surface any unsynchronised mutable state under the race detector.
//
// Run with -race. This test is in the TDD RED phase: it currently fails under
// the race detector because Adapter.scriptIdx and Adapter.invocations are
// written from many goroutines without synchronisation. It becomes GREEN when
// the adapter guards those fields with a mutex (I13.2).

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

// makeNativeCall encodes a minimal fake-native pre-phase payload for the given
// collaborator tool name and token. It uses the private wire shape the fake
// package tests have always driven — see fake_test.go for its definition —
// encoded here from first principles so this test has no dependency on the
// test-internal helpers of another file.
//
// AgentIdentity is left empty so the collaborator's canonical key equals
// ToolName alone, matching the script keys this test constructs.
func makeNativeCall(tool, token string) []byte {
	b, _ := json.Marshal(map[string]string{
		"phase": "pre",
		"tool":  tool,
		"token": token,
	})
	return b
}

// makeNativePostCall encodes a minimal fake-native post-phase payload.
// AgentIdentity is left empty for the same reason as makeNativeCall.
func makeNativePostCall(tool, token string) []byte {
	b, _ := json.Marshal(map[string]string{
		"phase": "post",
		"tool":  tool,
		"token": token,
	})
	return b
}

// TestFakeAdapter_ConcurrentTranslateCallAndTranslateOutcome_ProducesNoDataRaces
// runs many goroutines each performing a complete pre-phase TranslateCall
// followed by a TranslateOutcome on the same shared adapter. The race detector
// will flag any unguarded concurrent write to scriptIdx or invocations.
//
// Scenario: the composition root hands one adapter to every attempt; under a
// bound above 1, the end-to-end harness is the first place this concurrency
// is exercised.
func TestFakeAdapter_ConcurrentTranslateCallAndTranslateOutcome_ProducesNoDataRaces(t *testing.T) {
	const goroutines = 20

	// One scripted turn per collaborator key so every post call has something
	// to consume. Nil script means unscripted; we use a populated but
	// independent-per-key script so each goroutine touches a separate map entry
	// — the race is on the map and slice internals, not on which turn is consumed.
	script := make(map[string][]fake.Turn, goroutines)
	for i := 0; i < goroutines; i++ {
		key := domain.CollaboratorIdentity{ToolName: fmt.Sprintf("tool-%03d", i)}.Key()
		script[key] = []fake.Turn{{Body: fmt.Sprintf("response-%03d", i)}}
	}

	adapter := fake.New(fake.Options{
		Capabilities: domain.HarnessCapabilities{},
		Script:       script,
	})

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			tool := fmt.Sprintf("tool-%03d", i)
			token := fmt.Sprintf("token-%03d", i)

			// Pre phase — translate the call.
			preNative := makeNativeCall(tool, token)
			preCall, err := adapter.TranslateCall(domain.PhasePre, preNative)
			if err != nil {
				t.Errorf("goroutine %d: TranslateCall(PhasePre) returned error: %v", i, err)
				return
			}

			// Translate the outcome — this appends to invocations.
			outcome := domain.InterceptionOutcome{
				Kind:             domain.OutcomePassthrough,
				CorrelationToken: token,
			}
			if _, err := adapter.TranslateOutcome(outcome, preCall); err != nil {
				t.Errorf("goroutine %d: TranslateOutcome returned error: %v", i, err)
			}
		}()
	}
	wg.Wait()
}

// TestFakeAdapter_ConcurrentPostPhase_ScriptConsumedExactlyOncePerTurn
// drives many goroutines each consuming their own scripted post-phase turn
// from a shared adapter. Every turn must be consumed exactly once: neither
// lost nor double-consumed. After all goroutines finish, RemainingScript must
// be zero.
//
// This test also fails under the race detector if scriptIdx is not guarded,
// because two goroutines may read and increment the same index simultaneously.
func TestFakeAdapter_ConcurrentPostPhase_ScriptConsumedExactlyOncePerTurn(t *testing.T) {
	const goroutines = 20

	// One scripted turn per collaborator identity, keyed distinctly.
	script := make(map[string][]fake.Turn, goroutines)
	for i := 0; i < goroutines; i++ {
		key := domain.CollaboratorIdentity{ToolName: fmt.Sprintf("post-tool-%03d", i)}.Key()
		script[key] = []fake.Turn{{Body: fmt.Sprintf("body-%03d", i)}}
	}

	adapter := fake.New(fake.Options{
		Capabilities: domain.HarnessCapabilities{},
		Script:       script,
	})

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			tool := fmt.Sprintf("post-tool-%03d", i)
			token := fmt.Sprintf("post-token-%03d", i)
			postNative := makeNativePostCall(tool, token)
			_, err := adapter.TranslateCall(domain.PhasePost, postNative)
			if err != nil {
				t.Errorf("goroutine %d: TranslateCall(PhasePost) returned error: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if remaining := adapter.RemainingScript(); remaining != 0 {
		t.Errorf(
			"RemainingScript() = %d after all goroutines finished, want 0 — "+
				"concurrent script consumption must consume each turn exactly once",
			remaining,
		)
	}
}
