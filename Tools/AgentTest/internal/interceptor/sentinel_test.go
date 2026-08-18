package interceptor_test

// Coverage: T12.6/AC12.7 — when the decision core reports the early-exit
// threshold reached, the shell writes the sentinel the driver's supervisor
// watches for; a later intercepted call in the same workspace halts on
// entry rather than proceeding, and — per the Stage 12 design's ordering
// table — needs no state to do so.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runstate"
)

func TestRun_PreInvocation_EarlyExitThresholdReached_WritesTheSentinelAndRecordsIt(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // already at the threshold
	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "halt" {
		t.Fatalf("reply Kind = %q, want %q once the early-exit threshold is reached", got.Kind, "halt")
	}

	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("expected the early-exit sentinel to be written at %q: %v", sentinelPath, err)
	}

	records := readLog(t, h.Log)
	found := false
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventEarlyExitTriggered {
			found = true
		}
	}
	if !found {
		t.Error("expected a RunEventEarlyExitTriggered run-level record")
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
