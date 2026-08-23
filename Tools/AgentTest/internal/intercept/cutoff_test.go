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

// --- Stage 1: Count-based cutoff — unmatched dispatch cases ---
//
// The defect: the post/completion cutoff branch guards on hasPending (the
// presence of a pending stub for the correlation token). Under the
// unmatched-passthrough policy the pre-invocation call creates an in-flight
// entry but no pending stub, so hasPending is false and the threshold is never
// compared. A run with stop_after_invocations: N runs to turn_limit instead.
//
// The fix replaces the short-circuiting hasPending conjunct with a sequence
// number resolved from: (1) the pending stub; (2) the in-flight entry; (3) the
// global invocation counter. The cutoff is then count-based on every path,
// including the paths where the dispatch was unmatched and where the
// correlation token could not be recovered at all.

// unmatchedNthPostState returns the RunState that exists immediately before the
// Nth dispatch's post/completion phase when that dispatch was unmatched under
// the passthrough policy. The pre-invocation call incremented SequenceCounter
// to N and created an in-flight entry at seq N, but no pending stub was created
// — only matched or generic-response dispatches create pending stubs.
func unmatchedNthPostState(n int, token string, id domain.CollaboratorIdentity) domain.RunState {
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n
	state.InFlight[token] = domain.InFlight{Seq: n, Identity: id, StartedAt: time.Now()}
	// deliberately no PendingStubs entry for token
	return state
}

// --- T1.1: Cutoff fires on an unmatched-passthrough Nth dispatch ---

// TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_SetsTerminateSubject is
// the primary regression test for the Stage 1 defect. The Nth dispatch was
// unmatched under the passthrough policy: the pre-invocation call created an
// in-flight entry but no pending stub. The current code's hasPending short-
// circuit prevents the threshold from being compared, so TerminateSubject
// remains false. After the fix the in-flight entry's seq resolves the threshold
// comparison and TerminateSubject is set.
func TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_SetsTerminateSubject(t *testing.T) {
	id := researcherIdentity()
	token := "corr-unmatched-passthrough"
	n := 1

	in := intercept.Input{
		Call:     completionCall(id, token, "", domain.HarnessCapabilities{}),
		State:    unmatchedNthPostState(n, token, id),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if !decision.TerminateSubject {
		t.Errorf("TerminateSubject = false, want true: the Nth dispatch must trigger the cutoff even when that dispatch matched no stub — the count is the only thing that decides, not a pending-stub hit")
	}
}

// TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_OutcomeIsPassthrough verifies
// that the post/completion path never refuses, even when the cutoff fires. The reply
// must reach the subject before the sentinel is written; refusing at this point would
// lose the Nth reply.
func TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_OutcomeIsPassthrough(t *testing.T) {
	id := researcherIdentity()
	token := "corr-unmatched-passthrough-outcome"
	n := 1

	in := intercept.Input{
		Call:     completionCall(id, token, "", domain.HarnessCapabilities{}),
		State:    unmatchedNthPostState(n, token, id),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Errorf("Outcome.Kind = %q, want %q: the post/completion path must never refuse, even when the cutoff fires", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

// TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_EmitsEarlyExitTriggeredRecord
// verifies that the RunEventEarlyExitTriggered record is emitted after the end
// record for the Nth unmatched dispatch, exactly as it is for a matched Nth dispatch.
func TestDecide_Post_UnmatchedPassthrough_AtNthDispatch_EmitsEarlyExitTriggeredRecord(t *testing.T) {
	id := researcherIdentity()
	token := "corr-unmatched-passthrough-record"
	n := 1

	in := intercept.Input{
		Call:     completionCall(id, token, "", domain.HarnessCapabilities{}),
		State:    unmatchedNthPostState(n, token, id),
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
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
		t.Errorf("expected RunEventEarlyExitTriggered in records for an unmatched Nth dispatch, got: %+v", decision.Records)
	}
}

// --- T1.1: Cutoff fires on an unmatched-halt Nth dispatch (pre-invocation) ---

// TestDecide_Pre_UnmatchedHalt_AtNthDispatch_SetsTerminateSubject verifies
// that under the unmatched-halt policy the cutoff fires at the pre-invocation
// point when the Nth dispatch is refused. TerminatesAtPre must be true for
// OutcomeHalt (not only OutcomeSubstitute) so that the sentinel is written
// after the refusal reply is delivered — no post/completion event fires for a
// halted call.
func TestDecide_Pre_UnmatchedHalt_AtNthDispatch_SetsTerminateSubject(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "no-stub-identity"}
	n := 1

	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n - 1 // seq = (n-1)+1 = n at the pre-invocation point

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedHalt},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeHalt {
		t.Fatalf("Outcome.Kind = %q, want %q: the unmatched call under UnmatchedHalt policy must be refused", decision.Outcome.Kind, domain.OutcomeHalt)
	}
	if decision.Outcome.HaltReason != domain.HaltUnmatched {
		t.Fatalf("HaltReason = %q, want %q", decision.Outcome.HaltReason, domain.HaltUnmatched)
	}
	if !decision.TerminateSubject {
		t.Errorf("TerminateSubject = false, want true: the refusal reply IS the Nth answer delivered to the subject; no post/completion fires for it, so the cutoff must be signalled here")
	}
}

// --- T1.2: Count is the only thing that decides ---

// TestDecide_Post_CutoffIsCountBased_StubbedGenericUnmatchedAllTerminateAtN
// pins the T1.2 requirement: a stubbed, a generically-answered and an unmatched
// Nth dispatch must all trigger the cutoff. The match outcome of the dispatch is
// irrelevant; the sequence number is what the threshold compares against.
func TestDecide_Post_CutoffIsCountBased_StubbedGenericUnmatchedAllTerminateAtN(t *testing.T) {
	id := researcherIdentity()
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	cases := []struct {
		name  string
		state domain.RunState
		token string
	}{
		{
			name:  "stubbed",
			token: "corr-stubbed",
			state: nthPostState(n, "corr-stubbed", id, expected),
		},
		{
			// A generic-response dispatch also creates a pending stub entry,
			// so its state shape is identical to the stubbed case.
			name:  "generic_response",
			token: "corr-generic",
			state: nthPostState(n, "corr-generic", id, expected),
		},
		{
			name:  "unmatched_passthrough",
			token: "corr-unmatched",
			state: unmatchedNthPostState(n, "corr-unmatched", id),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := intercept.Input{
				Call:     completionCall(id, tc.token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
				State:    tc.state,
				Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
				Now:      time.Now(),
			}

			decision, err := intercept.Decide(in)

			if err != nil {
				t.Fatalf("Decide returned unexpected error: %v", err)
			}
			if !decision.TerminateSubject {
				t.Errorf("TerminateSubject = false for %q case, want true: the count is the only thing that decides, not the match outcome of the Nth dispatch", tc.name)
			}
			if decision.Outcome.Kind != domain.OutcomePassthrough {
				t.Errorf("Outcome.Kind = %q, want %q for %q case: the post/completion path must never refuse", decision.Outcome.Kind, domain.OutcomePassthrough, tc.name)
			}
		})
	}
}

// --- T1.3: Direct-substitution path is unchanged, plus new guards ---

// TestDecide_Pre_DirectSubstitution_AtNthDispatch_SetsTerminateSubjectAndRecord
// verifies that the pre-invocation substitution path still cuts off at the Nth
// dispatch. This is a regression guard: the existing path sets TerminateSubject
// correctly for OutcomeSubstitute and must continue to do so after the Stage 1
// implementation adds the !EarlyExitTriggered guard and generalises the
// TerminatesAtPre check.
func TestDecide_Pre_DirectSubstitution_AtNthDispatch_SetsTerminateSubjectAndRecord(t *testing.T) {
	id := researcherIdentity()
	n := 2
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n - 1 // seq = (n-1)+1 = n = threshold

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
	if decision.Outcome.Kind != domain.OutcomeSubstitute {
		t.Fatalf("Outcome.Kind = %q, want %q: the stubbed call must yield a substitute reply", decision.Outcome.Kind, domain.OutcomeSubstitute)
	}
	if !decision.TerminateSubject {
		t.Errorf("TerminateSubject = false, want true: the Nth substitute reply must signal termination so the sentinel is written after the substitute reply is delivered")
	}
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventEarlyExitTriggered {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RunEventEarlyExitTriggered in records for the Nth direct-substitute dispatch, got: %+v", decision.Records)
	}
}

// TestDecide_Pre_EarlyExitAlreadyTriggered_DoesNotSetTerminateSubjectAgain
// verifies that the !EarlyExitTriggered guard prevents the pre-invocation cutoff
// from firing a second time. Without this guard, a subsequent call at or beyond
// the threshold would append a second RunEventEarlyExitTriggered record and
// signal the sentinel write again. The sentinel write is idempotent, but the
// duplicate log record is not.
func TestDecide_Pre_EarlyExitAlreadyTriggered_DoesNotSetTerminateSubjectAgain(t *testing.T) {
	id := researcherIdentity()
	n := 1
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n         // seq = n+1 > threshold
	state.EarlyExitTriggered = true   // already triggered in a prior interception

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
	if decision.TerminateSubject {
		t.Errorf("TerminateSubject = true after EarlyExitTriggered is already set in state, want false: the cutoff must fire at most once per run")
	}
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventEarlyExitTriggered {
			t.Errorf("RunEventEarlyExitTriggered emitted after EarlyExitTriggered is already set: this record must appear exactly once in the invocation log")
		}
	}
}

// TestDecide_Post_EarlyExitAlreadyTriggered_DoesNotSetTerminateSubjectAgain
// verifies the !EarlyExitTriggered guard on the post/completion path: once the
// sentinel has been written and the state records EarlyExitTriggered = true, a
// subsequent post/completion event must not signal termination again.
func TestDecide_Post_EarlyExitAlreadyTriggered_DoesNotSetTerminateSubjectAgain(t *testing.T) {
	id := researcherIdentity()
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	token := "corr-already-triggered-post"

	state := nthPostState(n, token, id, expected)
	state.EarlyExitTriggered = true // already triggered in a prior interception

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.TerminateSubject {
		t.Errorf("TerminateSubject = true after EarlyExitTriggered is already set in state, want false: the cutoff must fire at most once per run")
	}
}

// TestDecide_Post_SeqBeyondThreshold_StillSetsTerminateSubject verifies the
// >= semantics: if the exact Nth observation was missed (a dispatch completed
// but the threshold comparison was skipped), the next observation at seq > N
// must still trigger the cutoff. Combined with the !EarlyExitTriggered guard,
// the RunEventEarlyExitTriggered record still appears exactly once.
func TestDecide_Post_SeqBeyondThreshold_StillSetsTerminateSubject(t *testing.T) {
	id := researcherIdentity()
	n := 1
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	token := "corr-beyond-threshold"

	// seq = n+1 (beyond threshold) — the exact Nth was not observed.
	state := baseState()
	state.EarlyExitThreshold = n
	state.SequenceCounter = n + 1
	state.PendingStubs[token] = domain.PendingStub{Seq: n + 1, Identity: id, Expected: expected}
	state.InFlight[token] = domain.InFlight{Seq: n + 1, Identity: id, StartedAt: time.Now()}

	in := intercept.Input{
		Call:     completionCall(id, token, `{"status_code":"SUCCESS"}`, domain.HarnessCapabilities{}),
		State:    state,
		Registry: domain.StubRegistry{OnUnmatched: domain.UnmatchedPassthrough},
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if !decision.TerminateSubject {
		t.Errorf("TerminateSubject = false for dispatch at seq %d with threshold %d, want true: the cutoff rule uses >= not ==, so a missed Nth observation is still caught by the next completion", n+1, n)
	}
}
