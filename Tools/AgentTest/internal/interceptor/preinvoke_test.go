package interceptor_test

// Coverage: T12.1/AC12.1 — the pre-invocation happy path end to end within
// the process boundary, driven through the fake adapter so no real harness
// is needed: a native payload in, and out the other side a valid native
// reply, a committed state increment, an appended start record, and the
// stub's declared side-effect files on disk.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
)

func TestRun_PreInvocation_StubbedCall_WithDirectSubstitution_SubstitutesAndCommitsEndToEnd(t *testing.T) {
	// Arrange
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage12-example",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{
				{Path: "artifact.md", Content: "hello from the stub"},
			},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	native := encodePre(t, id, "corr-1", "researcher#1", "do work")

	// Act
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	// Assert: the reply
	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "substitute" {
		t.Fatalf("reply Kind = %q, want %q", got.Kind, "substitute")
	}
	if got.Token != "corr-1" {
		t.Fatalf("reply Token = %q, want %q", got.Token, "corr-1")
	}
	if string(got.Body) != `{"status_code":"SUCCESS"}` {
		t.Fatalf("reply Body = %s, want the stub response verbatim", got.Body)
	}

	// Assert: the committed state increment
	state := readState(t, h.Store)
	if state.SequenceCounter != 1 {
		t.Errorf("SequenceCounter = %d, want 1", state.SequenceCounter)
	}
	if state.CollaboratorCounters[id.Key()] != 1 {
		t.Errorf("CollaboratorCounters[%q] = %d, want 1", id.Key(), state.CollaboratorCounters[id.Key()])
	}
	pending, ok := state.PendingStubs["corr-1"]
	if !ok {
		t.Fatal("expected a pending stub recorded under the correlation token")
	}
	if string(pending.Expected) != `{"status_code":"SUCCESS"}` {
		t.Errorf("pending stub Expected = %s, want the stub response verbatim", pending.Expected)
	}
	if _, ok := state.InFlight["corr-1"]; !ok {
		t.Error("expected the correlation token to be marked in flight")
	}

	// Assert: the appended start record
	records := readLog(t, h.Log)
	if len(records) != 1 {
		t.Fatalf("expected exactly one log record, got %d: %+v", len(records), records)
	}
	rec := records[0]
	if rec.Kind != domain.RecordStart {
		t.Errorf("record Kind = %q, want %q", rec.Kind, domain.RecordStart)
	}
	if rec.Outcome != domain.OutcomeSubstitute {
		t.Errorf("record Outcome = %q, want %q", rec.Outcome, domain.OutcomeSubstitute)
	}
	if rec.CorrelationToken != "corr-1" {
		t.Errorf("record CorrelationToken = %q, want %q", rec.CorrelationToken, "corr-1")
	}
	if rec.Seq != 1 || rec.Ordinal != 1 {
		t.Errorf("record Seq/Ordinal = %d/%d, want 1/1", rec.Seq, rec.Ordinal)
	}

	// Assert: the stub's declared side-effect file on disk
	effectPath := filepath.Join(h.SubjectDir, "artifact.md")
	content, err := os.ReadFile(effectPath)
	if err != nil {
		t.Fatalf("expected the stub's side effect on disk, ReadFile: %v", err)
	}
	if string(content) != "hello from the stub" {
		t.Errorf("side-effect content = %q, want %q", content, "hello from the stub")
	}
}

// TestRun_PreInvocation_StubbedCall_WithoutDirectSubstitution_RewritesThePrompt
// is the other half of the capability-driven outcome selection the pure core
// implements: this shell must carry OutcomeRewritePrompt through to the
// native reply unchanged, exercising the path a harness with no direct
// substitution capability takes.
func TestRun_PreInvocation_StubbedCall_WithoutDirectSubstitution_RewritesThePrompt(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		TestID:      "stage12-example",
		OnUnmatched: domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: false}, nil)
	native := encodePre(t, id, "corr-2", "researcher#1", "do work")

	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "rewrite_prompt" {
		t.Fatalf("reply Kind = %q, want %q", got.Kind, "rewrite_prompt")
	}
	if got.Prompt == "" {
		t.Error("expected a non-empty rewritten prompt")
	}
}

// TestRun_PreInvocation_UnmatchedCall_ProceedsUntouchedUnderPassthroughPolicy
// exercises the registry's unmatched policy reaching all the way through the
// shell to a passthrough reply with no side effects applied.
func TestRun_PreInvocation_UnmatchedCall_ProceedsUntouchedUnderPassthroughPolicy(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{TestID: "stage12-example", OnUnmatched: domain.UnmatchedPassthrough}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)
	native := encodePre(t, id, "corr-3", "researcher#1", "do work")

	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind != "passthrough" {
		t.Fatalf("reply Kind = %q, want %q", got.Kind, "passthrough")
	}

	state := readState(t, h.Store)
	if len(state.PendingStubs) != 0 {
		t.Errorf("an unmatched passthrough call must not register a pending stub, got %+v", state.PendingStubs)
	}
}
