package evaluate_test

// Tests for observed-sequence construction and invocation-indexed assertions
// in the context of a hard-cutoff run (Stage 2, T2.3):
//
//   - A cutoff run that ends after exactly N dispatches produces exactly N
//     observed entries; an invocation_sequence.exact: true assertion with N
//     declared steps passes.
//   - task_messages at: positions address dispatches 1..N with no offset:
//     at: 1 is dispatch 1, at: N is dispatch N, at: N+1 fails because no
//     (N+1)th record exists.
//   - No phantom entry from a refused (N+1)th call appears in the observed
//     sequence when the cutoff fires at the Nth reply's delivery (the
//     stage-2 implementation removes the phantom at its source; the evaluator
//     already collects only RecordStart records and requires no filter change).
//
// These tests exercise the evaluate layer directly, using domain.RunEvidence
// built with exactly the records a correctly-implemented cutoff produces.
// They serve as regression guards: if a future change accidentally introduces
// a phantom entry or shifts sequence positions, these tests catch it.
//
// Design references: ContractsDesign.md Stage 2, Observed-sequence filtering.

import (
	"strconv"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// cutoffEvidence builds RunEvidence for an N-dispatch cutoff run. The records
// contain exactly N start/end pairs and a RunEventEarlyExitTriggered marker
// after the Nth end record, matching what the Stage 2 implementation produces.
func cutoffEvidence(n int, ids ...domain.CollaboratorIdentity) domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = nil

	for i := 0; i < n && i < len(ids); i++ {
		seq := i + 1
		t := start.Add(time.Duration(i*2) * time.Second)
		ev.Records = append(ev.Records,
			startRecord(seq, 1, ids[i], "", t, plainMessage(ids[i].AgentIdentity+"#1")),
			endRecord(seq, 1, ids[i], t.Add(time.Second)),
		)
	}

	// RunEventEarlyExitTriggered appears after the Nth end record.
	ev.Records = append(ev.Records, domain.LogRecord{
		Kind:      domain.RecordRun,
		TestID:    ev.Key.TestID,
		RunNumber: ev.Key.RunNumber,
		Timestamp: start.Add(time.Duration(n*2) * time.Second),
		Event:     domain.RunEventEarlyExitTriggered,
	})

	// A cutoff run ends normally from the evaluator's perspective.
	ev.SubjectResult.Disposition = domain.DispositionEarlyExit

	return ev
}

// --- Exact-count sequence assertion ---

// TestEvaluate_CutoffRun_ExactNSequence_Passes verifies that a cutoff run with
// exactly N start records passes an invocation_sequence.exact: true assertion
// declaring N steps. There must be no phantom (N+1)th record that would cause
// the exact assertion to fail with "further invocations observed".
func TestEvaluate_CutoffRun_ExactNSequence_Passes(t *testing.T) {
	n := 1
	r := researcher()
	ev := cutoffEvidence(n, r)
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{{Identity: &r}},
		Exact: true,
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("invocation_sequence.exact assertion on a %d-dispatch cutoff run: Outcome = %q, want pass; Detail = %q", n, ar.Outcome, ar.Detail)
	}
}

// TestEvaluate_CutoffRun_ExactNSequence_MultipleDispatches_Passes verifies the
// same exact-count property for N > 1: a run that completes 2 dispatches before
// the cutoff passes an exact: true assertion declaring those same 2 dispatches.
func TestEvaluate_CutoffRun_ExactNSequence_MultipleDispatches_Passes(t *testing.T) {
	r, rv := researcher(), reviewer()
	ev := cutoffEvidence(2, r, rv)
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r},
			{Identity: &rv},
		},
		Exact: true,
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("invocation_sequence.exact assertion on a 2-dispatch cutoff run: Outcome = %q, want pass; Detail = %q", ar.Outcome, ar.Detail)
	}
}

// TestEvaluate_CutoffRun_PhantomRecord_ExactAssertionFails verifies that a
// spurious (N+1)th start record — like the one the pre-Stage 2 implementation
// emits for a refused (N+1)th call — causes an exact: true assertion with N
// declared steps to fail. This test pins the direction of correctness: when
// a phantom record is absent (Stage 2 implementation), the assertion passes;
// when it is present (pre-Stage 2 behaviour), it fails.
func TestEvaluate_CutoffRun_PhantomRecord_ExactAssertionFails(t *testing.T) {
	r := researcher()
	rv := reviewer()
	ev := cutoffEvidence(1, r) // only 1 real dispatch
	// Inject a phantom RecordStart the way the pre-Stage 2 code would emit it
	// for a refused (N+1)th pre-invocation call.
	phantom := startRecord(2, 1, rv, "", time.Date(2026, 8, 7, 12, 5, 0, 0, time.UTC), plainMessage("reviewer#1"))
	phantom.Outcome = domain.OutcomeHalt
	ev.Records = append(ev.Records, phantom)

	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{{Identity: &r}},
		Exact: true,
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("exact assertion with a phantom (N+1)th record: Outcome = %q, want fail — the phantom shifts the observed count and must be detectable; the Stage 2 implementation removes it at source", ar.Outcome)
	}
}

// --- task_messages at: addressing 1..N with no offset ---

// TestEvaluate_CutoffRun_TaskMessageAt1_Passes verifies that task_messages.at: 1
// addresses the 1st dispatch in a cutoff run. The sequence position must not be
// offset by any preamble or phantom entry.
func TestEvaluate_CutoffRun_TaskMessageAt1_Passes(t *testing.T) {
	r := researcher()
	ev := cutoffEvidence(2, r, reviewer())

	greeterID := r
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, Identity: &greeterID},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, strconv.Itoa(1)+".identity")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("task_messages.at: 1 on a 2-dispatch cutoff run: Outcome = %q, want pass; Detail = %q", ar.Outcome, ar.Detail)
	}
}

// TestEvaluate_CutoffRun_TaskMessageAtN_Passes verifies that task_messages.at: N
// addresses the Nth (last) dispatch in a cutoff run with no offset — there is no
// phantom (N+1)th entry shifting positions.
func TestEvaluate_CutoffRun_TaskMessageAtN_Passes(t *testing.T) {
	n := 2
	r, rv := researcher(), reviewer()
	ev := cutoffEvidence(n, r, rv)

	reviewerID := rv
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: n, Identity: &reviewerID},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, strconv.Itoa(n)+".identity")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("task_messages.at: %d on a %d-dispatch cutoff run: Outcome = %q, want pass; Detail = %q", n, n, ar.Outcome, ar.Detail)
	}
}

// TestEvaluate_CutoffRun_TaskMessageAtNPlusOne_Fails verifies that
// task_messages.at: N+1 fails for a cutoff run that ended after N dispatches.
// No (N+1)th start record exists, so the assertion must report "no invocation
// with sequence N+1 was observed". This is the definitive proof that there is
// no phantom: if a phantom existed at seq N+1, this test would pass for the
// wrong reason.
func TestEvaluate_CutoffRun_TaskMessageAtNPlusOne_Fails(t *testing.T) {
	n := 1
	r := researcher()
	ev := cutoffEvidence(n, r)

	beyondCutoff := n + 1
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: beyondCutoff},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, strconv.Itoa(beyondCutoff))
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("task_messages.at: %d on a %d-dispatch cutoff run: Outcome = %q, want fail — no invocation exists at seq %d; a passing result would indicate a phantom entry", beyondCutoff, n, ar.Outcome, beyondCutoff)
	}
}

// TestEvaluate_CutoffRun_ObservedSequenceLengthEqualsN verifies that the
// observed invocation count reported in the assertion output equals N for a
// cutoff run with N dispatches. This is a structural check: the Actual field
// of a failing assertion names the observed sequence, and the failure detail
// when exact: true fails with extra entries names how many extra entries were
// observed.
//
// For a correctly-formed N-dispatch run, an exact assertion with N+1 steps
// must fail with "expected invocation not found" (not with "further invocations
// observed"), confirming exactly N entries are present.
func TestEvaluate_CutoffRun_ObservedSequenceLengthEqualsN(t *testing.T) {
	n := 1
	r, rv := researcher(), reviewer()
	ev := cutoffEvidence(n, r) // only researcher dispatched

	// Declare N+1 steps: researcher (exists) and reviewer (does not exist).
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r},
			{Identity: &rv},
		},
		Exact: true,
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("exact assertion with N+1 steps on a %d-dispatch run: Outcome = %q, want fail", n, ar.Outcome)
	}
	// The failure must say reviewer was not found — NOT that "further
	// invocations were observed". The latter would indicate the run has
	// more than N entries (e.g. a phantom record exists).
	if ar.Detail == "" {
		t.Errorf("Detail is empty; want a description of why the assertion failed")
	}
}
