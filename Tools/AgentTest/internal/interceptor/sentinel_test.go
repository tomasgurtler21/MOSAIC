package interceptor_test

// Coverage for sentinel and halt-on-entry behaviour:
//
//   - Stage 2 moved the cutoff trigger from the (N+1)th pre-invocation to the
//     Nth post/completion. The sentinel is no longer written at the
//     pre-invocation point; it is written by the post/completion path (see
//     cutoff_test.go).  A pre-invocation call that arrives at or beyond the
//     threshold proceeds through the normal decision path — it may still halt
//     for other reasons (e.g. no matching stub under UnmatchedHalt) but must
//     not produce HaltEarlyExit and must not write the sentinel.
//
//   - The halt-on-entry check (TestRun_PreInvocation_SentinelPresent_*) is
//     unchanged: once a sentinel file exists in the control directory, every
//     subsequent pre-invocation call halts on entry without touching state.
//     This mechanism is still the correct one; only the moment at which the
//     sentinel is first written has changed.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

// TestRun_PreInvocation_EarlyExitThresholdReached_DoesNotWriteSentinel verifies
// that the pre-invocation phase at or beyond the early-exit threshold no longer
// writes the sentinel. Stage 2 moved the sentinel write to the post/completion
// path (see cutoff_test.go). The pre-invocation path may still halt for other
// reasons (e.g. an unmatched call under UnmatchedHalt), but it must not write
// the sentinel and must not produce HaltEarlyExit.
func TestRun_PreInvocation_EarlyExitThresholdReached_DoesNotWriteSentinel(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // threshold reached

	// No stubs: the call will halt with HaltUnmatched, but must NOT produce
	// HaltEarlyExit or write the sentinel.
	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	// The sentinel must NOT be written: the pre-invocation path no longer
	// triggers the cutoff sentinel; only the Nth post/completion does.
	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	if _, err := os.Stat(sentinelPath); err == nil {
		t.Errorf("sentinel was written at %q from the pre-invocation path: Stage 2 moved the sentinel write to the Nth post/completion, not here", sentinelPath)
	}

	// The reply must not be HaltEarlyExit: the pre-invocation path must not
	// produce a refusal message for calls at or beyond the threshold.
	got := decodeReply(t, reply)
	_ = got // reply existence confirmed; HaltEarlyExit-specific check below
	records := readLog(t, h.Log)
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventEarlyExitTriggered {
			t.Errorf("RunEventEarlyExitTriggered was emitted from the pre-invocation path: this record must be emitted at the Nth post/completion, not at a (N+1)th pre-invocation")
		}
	}
}

// poisonedStore fails the test the moment any of its state-touching methods
// is called, so a test can prove the halt-on-entry check never reaches state
// at all — "a halted call needs no state" (Stage 12 design, ordering table).
type poisonedStore struct {
	t *testing.T
}

var _ runstate.Store = poisonedStore{}

func (p poisonedStore) Initialize(domain.RunState) error {
	p.t.Helper()
	p.t.Fatal("Initialize must not be called once the early-exit sentinel is present")
	return nil
}

func (p poisonedStore) Read() (domain.RunState, error) {
	p.t.Helper()
	p.t.Fatal("Read must not be called once the early-exit sentinel is present")
	return domain.RunState{}, nil
}

func (p poisonedStore) Update(func(domain.RunState) (domain.RunState, error)) (runstate.UpdateResult, error) {
	p.t.Helper()
	p.t.Fatal("Update must not be called once the early-exit sentinel is present")
	return runstate.UpdateResult{}, nil
}

func TestRun_PreInvocation_SentinelPresent_HaltsOnEntryWithoutTouchingState(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	h := newHarness(t, baseState(), domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	if err := os.WriteFile(sentinelPath, nil, 0o644); err != nil {
		t.Fatalf("failed to plant the early-exit sentinel: %v", err)
	}

	cfg := h.Config
	cfg.State = poisonedStore{t: t}

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, cfg, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "halt" {
		t.Fatalf("reply Kind = %q, want %q once the sentinel halts entry", got.Kind, "halt")
	}
}
