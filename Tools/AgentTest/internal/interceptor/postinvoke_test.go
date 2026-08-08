package interceptor_test

// Coverage: T12.2/AC12.1 — the post-invocation completion path: the
// correlation token is recovered, the pending stub is retrieved from state,
// the collaborator's actual response is compared against the expected stub
// by normalized comparison rather than raw text equality, an end record
// carrying the comparison outcome is appended, the in-flight bookkeeping is
// closed out, and the reply always passes through — a mismatch is recorded
// as evidence, never failed here.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

// statePendingOne builds the state a real pre-invocation would have left
// behind: one collaborator invoked once, its stub still pending its echo
// check.
func statePendingOne(id domain.CollaboratorIdentity, token string, expected json.RawMessage) domain.RunState {
	s := baseState()
	s.SequenceCounter = 1
	s.CollaboratorCounters[id.Key()] = 1
	s.PendingStubs[token] = domain.PendingStub{Seq: 1, Identity: id, Expected: expected}
	s.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}
	return s
}

func TestRun_PostInvocation_FaithfulEcho_RecordsAMatchAndClosesBookkeeping(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	script := map[string][]fake.Turn{id.Key(): {{Body: string(expected)}}}

	h := newHarness(t, statePendingOne(id, "corr-1", expected), domain.StubRegistry{TestID: "stage12-example"}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, script)
	native := encodePost(t, id, "corr-1")

	reply, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "passthrough" {
		t.Fatalf("reply Kind = %q, want %q — the post phase never halts or denies", got.Kind, "passthrough")
	}
	if got.Token != "corr-1" {
		t.Fatalf("reply Token = %q, want %q", got.Token, "corr-1")
	}

	records := readLog(t, h.Log)
	if len(records) != 1 {
		t.Fatalf("expected exactly one log record, got %d: %+v", len(records), records)
	}
	rec := records[0]
	if rec.Kind != domain.RecordEnd {
		t.Fatalf("record Kind = %q, want %q", rec.Kind, domain.RecordEnd)
	}
	if rec.Echo == nil {
		t.Fatal("expected the end record to carry an echo comparison outcome")
	}
	if !rec.Echo.Match {
		t.Errorf("Echo.Match = false, want true for a faithful echo (diff: %s)", rec.Echo.Diff)
	}
	if rec.Seq != 1 {
		t.Errorf("record Seq = %d, want 1 (recovered from the pending stub)", rec.Seq)
	}

	state := readState(t, h.Store)
	if _, stillPending := state.PendingStubs["corr-1"]; stillPending {
		t.Error("expected the pending stub to be resolved after its echo check")
	}
	if _, stillInFlight := state.InFlight["corr-1"]; stillInFlight {
		t.Error("expected the in-flight entry to be cleared after completion")
	}
}

// TestRun_PostInvocation_MismatchedEcho_RecordsTheMismatchButStillPassesThrough
// is the decisive property named in the plan: a mismatch is evidence, not a
// verdict this process renders. Failing the test is the verdict engine's
// job.
func TestRun_PostInvocation_MismatchedEcho_RecordsTheMismatchButStillPassesThrough(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	script := map[string][]fake.Turn{id.Key(): {{Body: `{"status_code":"FAILURE"}`}}}

	h := newHarness(t, statePendingOne(id, "corr-1", expected), domain.StubRegistry{TestID: "stage12-example"}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, script)
	native := encodePost(t, id, "corr-1")

	reply, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "passthrough" {
		t.Fatalf("reply Kind = %q, want %q even on a mismatch — refusing a post-invocation call can only damage the subject's run", got.Kind, "passthrough")
	}

	records := readLog(t, h.Log)
	if len(records) != 1 {
		t.Fatalf("expected exactly one log record, got %d: %+v", len(records), records)
	}
	if records[0].Echo == nil {
		t.Fatal("expected the end record to carry an echo comparison outcome")
	}
	if records[0].Echo.Match {
		t.Error("Echo.Match = true, want false for a mismatched echo")
	}
	if records[0].Echo.Diff == "" {
		t.Error("expected a non-empty Diff explaining the mismatch")
	}
}

// TestRun_PostInvocation_NormalizedComparison_IgnoresKeyOrderAndWhitespace
// is T12.2's explicit requirement: comparison is normalized, never raw text
// equality.
func TestRun_PostInvocation_NormalizedComparison_IgnoresKeyOrderAndWhitespace(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	expected := json.RawMessage(`{"a":1,"b":2}`)
	script := map[string][]fake.Turn{id.Key(): {{Body: "  { \"b\":  2,   \"a\": 1 }  "}}}

	h := newHarness(t, statePendingOne(id, "corr-1", expected), domain.StubRegistry{TestID: "stage12-example"}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, script)
	native := encodePost(t, id, "corr-1")

	_, exitCode, _ := run(t, h.Config, domain.PhasePost, native)
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	records := readLog(t, h.Log)
	if len(records) != 1 || records[0].Echo == nil {
		t.Fatalf("expected exactly one end record carrying an echo outcome, got %+v", records)
	}
	if !records[0].Echo.Match {
		t.Errorf("Echo.Match = false, want true: differing key order and whitespace must not read as a mismatch (diff: %s)", records[0].Echo.Diff)
	}
}

// TestRun_PostInvocation_NoPendingStub_StillPassesThroughWithNoEchoOutcome
// covers the boundary where the post-invocation call's correlation token has
// no matching pending stub (e.g. an unmatched-passthrough call at the pre
// phase never registered one): the call must still complete cleanly.
func TestRun_PostInvocation_NoPendingStub_StillPassesThroughWithNoEchoOutcome(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	state := baseState()
	state.InFlight["corr-1"] = domain.InFlight{Seq: 1, Identity: id}
	script := map[string][]fake.Turn{id.Key(): {{Body: `{"anything":true}`}}}

	h := newHarness(t, state, domain.StubRegistry{TestID: "stage12-example"}, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, script)
	native := encodePost(t, id, "corr-1")

	reply, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "passthrough" {
		t.Fatalf("reply Kind = %q, want %q", got.Kind, "passthrough")
	}

	finalState := readState(t, h.Store)
	if _, stillInFlight := finalState.InFlight["corr-1"]; stillInFlight {
		t.Error("expected the in-flight entry to be cleared even with no pending stub to resolve")
	}
}
