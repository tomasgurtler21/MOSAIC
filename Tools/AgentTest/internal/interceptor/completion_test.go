package interceptor_test

// Coverage: T12.5/AC12.5 — the interceptor stays failure-contained on the
// completion event exactly as it already does on pre/post: it must never
// halt or deny once a collaborator has actually run, even when the run
// state would have halted a pre-invocation call (the early-exit threshold
// reached).
//
// This drives the full chain the shell composes — cfg.Adapter.TranslateCall,
// intercept.Decide, cfg.Adapter.TranslateOutcome — end to end through the
// scripted fake adapter, so the risk this stage exists to close (Decide
// still routing domain.PhaseCompletion through its pre-invocation decision
// path) is exercised at the shell boundary, not only inside internal/intercept's
// own package tests.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/domain"
)

func TestRun_Completion_NeverHaltsEvenWhenEarlyExitThresholdReached(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}

	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // threshold already reached

	h := newHarness(t, state, domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native, err := json.Marshal(wireCall{Phase: "completion", Tool: id.ToolName, Agent: id.AgentIdentity, Token: "corr-1"})
	if err != nil {
		t.Fatalf("marshal completion wire call: %v", err)
	}

	reply, exitCode, _ := run(t, h.Config, domain.PhaseCompletion, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind == "halt" {
		t.Errorf("reply Kind = %q, want anything but a halt — the completion event fires after the collaborator has already run and must never halt or deny", got.Kind)
	}
	if got.Kind != "passthrough" {
		t.Errorf("reply Kind = %q, want %q", got.Kind, "passthrough")
	}
}

// TestRun_FailureContainment_Completion_PayloadTheAdapterRejects mirrors
// TestRun_FailureContainment_PayloadTheAdapterRejects (internal/interceptor/failure_test.go)
// for domain.PhaseCompletion: a malformed/rejected completion payload must
// stay contained — exit code 0, a decodable reply, and no reach into state —
// exactly as it already does on PhasePre, closing the gap the new event
// would otherwise leave in T12.5/AC12.5's failure-containment coverage.
func TestRun_FailureContainment_Completion_PayloadTheAdapterRejects(t *testing.T) {
	h := newHarness(t, baseState(), domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	malformed := []byte(`{"phase":"completion"}`) // no tool, no token: the fake adapter's TranslateCall rejects this

	reply, exitCode, diag := run(t, h.Config, domain.PhaseCompletion, malformed)
	assertContained(t, exitCode, reply, diag)

	state := readState(t, h.Store)
	if state.SequenceCounter != 0 {
		t.Errorf("a payload the adapter rejects must never reach state: SequenceCounter = %d, want 0", state.SequenceCounter)
	}
}
