package interceptor_test

// Tests for the interceptor shell's handling of a terminating decision
// (Stage 2, T2.2):
//
//   - When the decision core signals TerminateSubject = true (which it does
//     on the Nth dispatch's post/completion), the shell writes the early-exit
//     sentinel the supervisor watches for.
//   - The reply for dispatch N is still delivered to the subject: the sentinel
//     is written only after the reply leaves cfg.Out, so the supervisor cannot
//     observe the sentinel before the subject has received the reply.
//   - A failure in the sentinel write is contained by the existing single
//     outermost failure boundary: the intercepted call exits zero, no reply is
//     lost, and the failure is logged rather than propagated.
//   - Below the threshold: no sentinel is written by the post-invocation path.
//
// Design references: ContractsDesign.md Stage 2, interceptor shell section.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/interceptor"
)

// --- Sentinel written on Nth completion ---

// TestRun_Post_AtNthDispatch_WritesSentinel verifies that the interceptor shell
// writes the early-exit sentinel when the decision core signals
// TerminateSubject = true on the Nth dispatch's post/completion phase.
// Without the sentinel, the supervisor cannot observe the cutoff and the
// subject process is never terminated.
func TestRun_Post_AtNthDispatch_WritesSentinel(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	token := "corr-nth-post"
	expected := `{"status_code":"SUCCESS"}`

	// State has EarlyExitThreshold = 1 and the Nth pending stub at seq 1,
	// so the decision core's decidePost fires at the threshold.
	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // the Nth pre-call already incremented this
	state.PendingStubs[token] = domain.PendingStub{
		Seq:      1,
		Identity: id,
		Expected: []byte(expected),
	}
	state.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}

	registry := domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}
	h := newHarness(t, state, registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native := encodePost(t, id, token)
	_, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("expected the early-exit sentinel to be written at %q after the Nth dispatch completes, but stat failed: %v", sentinelPath, err)
	}
}

// orderCheckWriter wraps a bytes.Buffer and records, at the moment the first
// reply bytes are written to cfg.Out, whether the early-exit sentinel file
// already existed on disk. The field is false when the sentinel was not yet
// present (correct ordering: reply before sentinel) and true when it was
// (incorrect ordering: sentinel before reply).
//
// The check runs exactly once — on the first Write call that carries non-zero
// bytes — because that is when the reply first becomes observable to the
// subject; subsequent Write calls carrying the same content could race with
// the sentinel write in theory, though in practice interceptor.Run's execution
// is sequential and there is no concurrent writer.
type orderCheckWriter struct {
	buf             bytes.Buffer
	sentinelPath    string
	sentinelExisted bool
	checked         bool
}

func (w *orderCheckWriter) Write(p []byte) (int, error) {
	if !w.checked && len(p) > 0 {
		w.checked = true
		_, err := os.Stat(w.sentinelPath)
		w.sentinelExisted = err == nil
	}
	return w.buf.Write(p)
}

// TestRun_Post_AtNthDispatch_DeliversReplyBeforeSentinelIsObservable verifies
// the ordering contract documented in ContractsDesign.md Stage 2: the reply
// for the Nth dispatch must be written to cfg.Out before the early-exit
// sentinel is written to the control directory. If the sentinel appeared first,
// the supervisor's watchSentinel poll could cancel the subject's context and
// kill the subject's OS process before the Nth reply reached it — losing the
// Nth reply entirely.
//
// The test injects an orderCheckWriter as cfg.Out. When the interceptor writes
// the reply, the writer records whether the sentinel file already existed at
// that instant. After Run returns the writer also verifies the sentinel was
// ultimately written (to distinguish "correct ordering" from "sentinel never
// written at all"), and reports an error if the sentinel was already present
// during the reply write.
func TestRun_Post_AtNthDispatch_DeliversReplyBeforeSentinelIsObservable(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	token := "corr-nth-reply"
	expected := `{"status_code":"SUCCESS"}`

	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1
	state.PendingStubs[token] = domain.PendingStub{
		Seq:      1,
		Identity: id,
		Expected: []byte(expected),
	}
	state.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}

	registry := domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}
	h := newHarness(t, state, registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	out := &orderCheckWriter{sentinelPath: sentinelPath}

	native := encodePost(t, id, token)

	var diagBuf bytes.Buffer
	cfg := h.Config
	cfg.Phase = domain.PhasePost
	cfg.In = bytes.NewReader(native)
	cfg.Out = out
	cfg.Diag = &diagBuf
	exitCode := interceptor.Run(context.Background(), cfg)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	if len(out.buf.Bytes()) == 0 {
		t.Errorf("expected a non-empty reply to be written to cfg.Out after the Nth dispatch's post-invocation, got empty reply")
	}
	// Verify the sentinel was written at all (separate from ordering; also covered
	// by TestRun_Post_AtNthDispatch_WritesSentinel, but required here so that
	// sentinelExisted == false cannot be mistaken for "correct ordering" when the
	// sentinel was simply never written.
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Errorf("expected the early-exit sentinel to exist after Run returned, but stat failed: %v — cannot verify ordering when the sentinel was never written", err)
	}
	// Ordering check: sentinel must not have existed when the reply bytes were
	// first written to cfg.Out.
	if out.sentinelExisted {
		t.Errorf("ordering violation: the sentinel already existed when the reply was written to cfg.Out; the sentinel must be written AFTER the reply so the supervisor cannot cancel the subject's context before the subject receives the Nth reply")
	}
}

// TestRun_Post_AtNthDispatch_RecordsEarlyExitTriggeredInLog verifies that the
// shell appends the RunEventEarlyExitTriggered record emitted by the decision
// core to the invocation log. Without this record, a cut-off run's log is
// missing the chronological marker that explains why the subject stopped.
func TestRun_Post_AtNthDispatch_RecordsEarlyExitTriggeredInLog(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	token := "corr-nth-log"
	expected := `{"status_code":"SUCCESS"}`

	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1
	state.PendingStubs[token] = domain.PendingStub{
		Seq:      1,
		Identity: id,
		Expected: []byte(expected),
	}
	state.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}

	registry := domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}
	h := newHarness(t, state, registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native := encodePost(t, id, token)
	_, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	records := readLog(t, h.Log)
	found := false
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventEarlyExitTriggered {
			found = true
		}
	}
	if !found {
		t.Error("expected a RunEventEarlyExitTriggered run-level record in the invocation log after the Nth dispatch's post-invocation")
	}
}

// --- No sentinel below threshold ---

// TestRun_Post_BelowThreshold_DoesNotWriteSentinel verifies that the sentinel
// is not written for post-invocations that complete below the early-exit
// threshold. Only the Nth dispatch's completion may write the sentinel.
func TestRun_Post_BelowThreshold_DoesNotWriteSentinel(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	token := "corr-below-threshold"
	expected := `{"status_code":"SUCCESS"}`

	// EarlyExitThreshold = 2 but this is the 1st dispatch completing: no cutoff.
	state := baseState()
	state.EarlyExitThreshold = 2
	state.SequenceCounter = 1
	state.PendingStubs[token] = domain.PendingStub{
		Seq:      1,
		Identity: id,
		Expected: []byte(expected),
	}
	state.InFlight[token] = domain.InFlight{Seq: 1, Identity: id}

	registry := domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt}
	h := newHarness(t, state, registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	native := encodePost(t, id, token)
	_, exitCode, _ := run(t, h.Config, domain.PhasePost, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	sentinelPath := filepath.Join(h.ControlDir, domain.EarlyExitSentinelName)
	if _, err := os.Stat(sentinelPath); err == nil {
		t.Errorf("sentinel was written at %q for a post-invocation below the early-exit threshold (seq 1 of threshold 2); only the Nth completion may write it", sentinelPath)
	}
}

// Note: failure containment for the sentinel write is not unit-tested here
// because writeSentinel is called directly (no injected dependency) and the
// control directory cannot be made selectively unwritable without also
// blocking the lock file that runstate.Store.Update writes. The property
// "a failed sentinel write is contained by handleFailure and does not crash
// the interceptor" is covered by the design specification and the existing
// end-to-end failure containment tests in failure_test.go.
//
// The ordering guarantee (reply written before sentinel) is verified directly
// by TestRun_Post_AtNthDispatch_DeliversReplyBeforeSentinelIsObservable above,
// using an orderCheckWriter that observes the sentinel's file-system state at
// the exact moment the reply bytes are written to cfg.Out.
