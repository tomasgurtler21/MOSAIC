package intercept_test

// Tests for the pure interception core's hard cutoff decision (Stage 2):
//
//   - When the Nth dispatch's post/completion phase fires and
//     EarlyExitThreshold == N, the returned Decision carries
//     TerminateSubject = true and Outcome = OutcomePassthrough: the reply
//     must reach the subject before the process is terminated.
//   - No start record and no refusal outcome is ever produced for a call at
//     or beyond the threshold: the cutoff fires at the Nth reply's delivery,
//     not at an (N+1)th pre-invocation refusal.
//   - Behaviour below the threshold is entirely unchanged.
//
// Design references: ContractsDesign.md Stage 2.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// nthPostState returns a RunState whose fields reflect what the run would hold
// immediately before the Nth dispatch's post/completion fires: threshold is N,
// the counter reflects N completed dispatches, and the pending-stubs map holds
// an entry at seq N for the given correlation token.
func nthPostState(n int, token string, id domain.CollaboratorIdentity, expected json.RawMessage) domain.RunState {
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n // pre-call for dispatch N already incremented this
	state.PendingStubs[token] = domain.PendingStub{Seq: n, Identity: id, Expected: expected}
	state.InFlight[token] = domain.InFlight{Seq: n, Identity: id, StartedAt: time.Now()}
	return state
}

// --- Termination signal on the Nth completion ---

// TestDecide_Post_AtNthDispatch_SetsTerminateSubjectFlag verifies that the
// Nth dispatch's completion sets Decision.TerminateSubject = true. The
// interceptor shell reads this flag to decide when to write the sentinel the
// supervisor watches for — the flag is orthogonal to the outcome, which
// remains OutcomePassthrough so the reply is delivered first.
func TestDecide_Post_AtNthDispatch_SetsTerminateSubjectFlag(t *testing.T) {
	id := researcherIdentity()
	token := "corr-nth-terminate"
	n := 2
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    nthPostState(n, token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if !decision.TerminateSubject {
		t.Errorf("TerminateSubject = false, want true: the Nth dispatch's completion must signal process termination so the sentinel is written after the reply is delivered")
	}
}

// TestDecide_Post_AtNthDispatch_OutcomeIsPassthrough verifies that the Nth
// dispatch's outcome is OutcomePassthrough even though termination is signalled.
// Deliver-then-terminate is the contract: the reply reaches the subject, then
// the sentinel is written; the subject is never refused.
//
// Regression guard (not a RED test): the post/completion path already produces
// OutcomePassthrough before Stage 2 because the old cutoff fired at the
// (N+1)th pre-invocation, not here. This test passes immediately and exists to
// prevent any Stage 2 change from accidentally introducing a halt outcome on
// the Nth dispatch's completion.
func TestDecide_Post_AtNthDispatch_OutcomeIsPassthrough(t *testing.T) {
	id := researcherIdentity()
	token := "corr-nth-passthrough"
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    nthPostState(n, token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q: the Nth dispatch must still deliver the reply; the termination signal is orthogonal to the outcome",
			decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_Post_AtNthDispatch_EmitsEarlyExitTriggeredRecord verifies that
// the Nth dispatch's completion emits a RunEventEarlyExitTriggered run-level
// record after the end record, at the correct chronological position in the log.
func TestDecide_Post_AtNthDispatch_EmitsEarlyExitTriggeredRecord(t *testing.T) {
	id := researcherIdentity()
	token := "corr-nth-triggered"
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    nthPostState(n, token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventEarlyExitTriggered {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a RunEventEarlyExitTriggered record in the Nth dispatch's completion records, got: %+v", decision.Records)
	}
}

// TestDecide_Post_AtNthDispatch_NoNewStartRecordEmitted verifies that the Nth
// dispatch's completion path produces no start record. A start record at this
// point would become a phantom invocation-log entry that offsets every
// invocation-indexed assertion and prevents exact-count sequence assertions.
//
// Regression guard (not a RED test): the post/completion path never emits a
// start record before Stage 2 either. This test passes immediately and exists
// to prevent any Stage 2 change from accidentally emitting a start record on
// the Nth dispatch's completion — the very phantom-entry defect Stage 2
// removes at source in the pre-invocation path.
func TestDecide_Post_AtNthDispatch_NoNewStartRecordEmitted(t *testing.T) {
	id := researcherIdentity()
	token := "corr-nth-no-start"
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    nthPostState(n, token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordStart {
			t.Errorf("the post/completion path must never emit a start record; got: %+v", rec)
		}
	}
}

// TestDecide_Post_EarlyExitTriggeredRecord_AppearsAfterEndRecord verifies that
// the RunEventEarlyExitTriggered record appears after the RecordEnd in the
// returned slice, placing it at the correct chronological position in the
// invocation log (after the end record for the Nth dispatch).
func TestDecide_Post_EarlyExitTriggeredRecord_AppearsAfterEndRecord(t *testing.T) {
	id := researcherIdentity()
	token := "corr-nth-order"
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    nthPostState(n, token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	endIdx := -1
	triggeredIdx := -1
	for i, rec := range decision.Records {
		if rec.Kind == domain.RecordEnd {
			endIdx = i
		}
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventEarlyExitTriggered {
			triggeredIdx = i
		}
	}
	if endIdx == -1 {
		t.Fatalf("no RecordEnd found in records: %+v", decision.Records)
	}
	if triggeredIdx == -1 {
		t.Fatalf("no RunEventEarlyExitTriggered record found in records: %+v", decision.Records)
	}
	if triggeredIdx <= endIdx {
		t.Errorf("RunEventEarlyExitTriggered (index %d) must appear after RecordEnd (index %d) in the returned records", triggeredIdx, endIdx)
	}
}

// --- No termination below threshold ---

// TestDecide_Post_BelowThreshold_TerminateSubjectIsFalse verifies that
// post-invocations at dispatch counts below the early-exit threshold do not
// signal termination. Only the Nth dispatch triggers the cutoff.
//
// Regression guard (not a RED test): TerminateSubject is a new bool field
// whose zero value is false, so this test passes trivially before Stage 2
// adds any code that might set it. It exists to pin the below-threshold
// boundary condition so a future implementation cannot accidentally signal
// termination before the threshold is reached.
func TestDecide_Post_BelowThreshold_TerminateSubjectIsFalse(t *testing.T) {
	id := researcherIdentity()
	token := "corr-below-threshold"
	n := 3
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	// Completing the (N-1)th dispatch of an N-dispatch threshold.
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n - 1
	state.PendingStubs[token] = domain.PendingStub{Seq: n - 1, Identity: id, Expected: expected}
	state.InFlight[token] = domain.InFlight{Seq: n - 1, Identity: id, StartedAt: time.Now()}

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    state,
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.TerminateSubject {
		t.Errorf("TerminateSubject = true, want false: only the Nth dispatch (seq %d) must signal termination, not dispatch at seq %d", n, n-1)
	}
}

// TestDecide_Post_ThresholdZero_NoTermination verifies that when
// EarlyExitThreshold is zero (stop_after_invocations not configured), no
// termination is ever signalled regardless of how many dispatches complete.
//
// Regression guard (not a RED test): the zero-threshold case relies on the
// zero value of TerminateSubject (false), so this test passes trivially before
// Stage 2. It exists to guard the unconfigured-cutoff boundary: an
// implementation must never signal termination when stop_after_invocations is
// absent from the test declaration.
func TestDecide_Post_ThresholdZero_NoTermination(t *testing.T) {
	id := researcherIdentity()
	token := "corr-zero-threshold"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := baseState()
	state.EarlyExitThreshold = 0 // no cutoff configured
	state.SequenceCounter = 5
	state.PendingStubs[token] = domain.PendingStub{Seq: 5, Identity: id, Expected: expected}
	state.InFlight[token] = domain.InFlight{Seq: 5, Identity: id, StartedAt: time.Now()}

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    state,
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.TerminateSubject {
		t.Errorf("TerminateSubject = true, want false: a zero EarlyExitThreshold means no cutoff is configured")
	}
}

// --- No refusal at the pre-invocation point ---

// TestDecide_Pre_AtOrAboveThreshold_NeverProducesHaltEarlyExitOutcome verifies
// that the pre-invocation phase no longer halts when the early-exit threshold
// has been reached. Stage 2 moves the cutoff from the (N+1)th pre-call to the
// Nth reply's completion; no call is refused at the pre-invocation point.
//
// The preceding behaviour — halting with HaltEarlyExit at the pre-invocation
// point — is exactly what this test must not observe.
func TestDecide_Pre_AtOrAboveThreshold_NeverProducesHaltEarlyExitOutcome(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // threshold already reached; old code halts any pre-call here

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    state,
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind == domain.OutcomeHalt && decision.Outcome.HaltReason == domain.HaltEarlyExit {
		t.Fatalf("pre-invocation produced HaltEarlyExit: the cutoff must fire at the Nth reply's completion, not refuse the (N+1)th pre-invocation call; no call may be refused or recorded as a phantom entry")
	}
}

// TestDecide_Pre_AtOrAboveThreshold_NoRefusalMessageProduced verifies that no
// refusal message ("This operation cannot proceed further in this run.") is
// produced for any call at or beyond the threshold at the pre-invocation point.
// A refusal message would reach the subject and reveal that the run is
// artificially bounded.
func TestDecide_Pre_AtOrAboveThreshold_NoRefusalMessageProduced(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.EarlyExitThreshold = 2
	state.SequenceCounter = 2

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    state,
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Message != "" {
		t.Errorf("Outcome.Message = %q, want empty: no refusal message must be produced at the pre-invocation point for a call at or beyond the threshold", decision.Outcome.Message)
	}
}
