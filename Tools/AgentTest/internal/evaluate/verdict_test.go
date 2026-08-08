package evaluate_test

// Tests for verdict determination and integrity: TIMEOUT is a distinct
// verdict rather than a species of FAIL; the presence of any
// lock-reclamation record produces a distinct STATE_INTEGRITY failure
// reported separately from an assertion failure; and a cost-attribution
// miss surfaces rather than reporting zero cost silently.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_OrdinaryAssertionFailure_ReasonIsAssertionOnly(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.Phase = "in_progress"
	ev.Definition.Assertions.FinalPhase = strp("complete")

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Fatalf("Verdict = %q, want FAIL", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want ReasonAssertion", got.Reasons)
	}
	if hasReason(got.Reasons, domain.ReasonStateIntegrity) || hasReason(got.Reasons, domain.ReasonTimeout) {
		t.Errorf("Reasons = %+v, an ordinary assertion failure must not also carry an infrastructure reason", got.Reasons)
	}
}

func TestEvaluate_SubjectTimedOut_YieldsDistinctTimeoutVerdict(t *testing.T) {
	ev := baseEvidence()
	ev.SubjectResult.Disposition = domain.DispositionTimedOut

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictTimeout {
		t.Errorf("Verdict = %q, want TIMEOUT — not a species of FAIL", got.Verdict)
	}
	if got.Verdict == domain.VerdictFail {
		t.Error("VerdictTimeout must not equal VerdictFail")
	}
	if !hasReason(got.Reasons, domain.ReasonTimeout) {
		t.Errorf("Reasons = %+v, want ReasonTimeout", got.Reasons)
	}
}

func TestEvaluate_LockReclaimed_YieldsDistinctStateIntegrityReason(t *testing.T) {
	ev := baseEvidence()
	ev.Records = append(ev.Records, domain.LogRecord{
		Kind:      domain.RecordRun,
		Event:     domain.RunEventLockReclaimed,
		Timestamp: ev.Records[0].Timestamp,
		Detail:    "lock reclaimed from PID 4242 after staleness threshold",
	})

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Fatalf("Verdict = %q, want FAIL", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonStateIntegrity) {
		t.Errorf("Reasons = %+v, want ReasonStateIntegrity", got.Reasons)
	}
	if hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, a lock reclamation must not be reported as an ordinary assertion failure — an infrastructure fault must never read as a subject regression", got.Reasons)
	}
}

func TestEvaluate_CostUnattributed_SurfacesAsConditionRatherThanSilentZero(t *testing.T) {
	ev := baseEvidence()
	ev.Cost = domain.CostReport{TotalUSD: 0, Attribution: domain.AttributionUnknownBucket, Detail: "events landed in the unknown-run bucket"}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionCostUnattributed) {
		t.Errorf("Conditions = %+v, want ConditionCostUnattributed", got.Conditions)
	}
	if got.Cost.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Cost.Attribution = %q, want %q carried through unchanged — a cost-attribution miss must never read as an attributed zero", got.Cost.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestEvaluate_CostAttributed_DoesNotSurfaceAsCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Cost = domain.CostReport{TotalUSD: 0.12, Attribution: domain.AttributionAttributed}

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionCostUnattributed) {
		t.Errorf("Conditions = %+v, a genuinely attributed cost must not surface a cost-unattributed condition", got.Conditions)
	}
	if got.Cost.TotalUSD != 0.12 {
		t.Errorf("Cost.TotalUSD = %v, want 0.12 carried through", got.Cost.TotalUSD)
	}
}

// TestEvaluate_ExtractionDegraded_SurfacesAsCondition covers a
// RunConditionKind the plan requires be surfaced, not silently dropped: a
// degraded message extraction on one invocation.
func TestEvaluate_ExtractionDegraded_SurfacesAsCondition(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	rec := startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1"))
	rec.Message.Extraction = domain.ExtractionDegraded
	ev.Records = []domain.LogRecord{
		rec,
		endRecord(1, 1, researcher(), start.Add(time.Second)),
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionExtractionDegraded) {
		t.Errorf("Conditions = %+v, want ConditionExtractionDegraded — the invocation's message was only degraded-recovered, not cleanly parsed", got.Conditions)
	}
}

// TestEvaluate_UnmatchedInvocation_SurfacesAsCondition covers the
// counterpart run-level event: an intercepted call matched no declared stub.
func TestEvaluate_UnmatchedInvocation_SurfacesAsCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Records = append(ev.Records, domain.LogRecord{
		Kind:      domain.RecordRun,
		Event:     domain.RunEventUnmatchedInvocation,
		Timestamp: ev.Records[0].Timestamp,
		Detail:    "an intercepted call matched no declared stub",
	})

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionUnmatchedInvocation) {
		t.Errorf("Conditions = %+v, want ConditionUnmatchedInvocation", got.Conditions)
	}
}

// TestEvaluate_OrchestrationUnreadable_SurfacesAsCondition is the direct
// verification that RunEvidence.OrchestrationProblem — carried specifically
// so an absent or malformed document is never read as a valid zero state —
// actually surfaces as a reportable condition.
func TestEvaluate_OrchestrationUnreadable_SurfacesAsCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration = domain.OrchestrationState{Present: false}
	ev.OrchestrationProblem = "orchestration document not found"

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionOrchestrationUnreadable) {
		t.Errorf("Conditions = %+v, want ConditionOrchestrationUnreadable — Orchestration.Present is false and OrchestrationProblem is set", got.Conditions)
	}
}

// TestEvaluate_AssertionFailureAlongsideStateIntegrity_BothReasonsReported
// confirms Reasons reports co-occurring failures fully: an ordinary
// assertion failure in the same run as a lock reclamation must not have one
// reason mask the other.
func TestEvaluate_AssertionFailureAlongsideStateIntegrity_BothReasonsReported(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.Phase = "in_progress"
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Records = append(ev.Records, domain.LogRecord{
		Kind:      domain.RecordRun,
		Event:     domain.RunEventLockReclaimed,
		Timestamp: ev.Records[0].Timestamp,
		Detail:    "lock reclaimed from PID 4242 after staleness threshold",
	})

	got := evaluate.Evaluate(ev)

	if !hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want ReasonAssertion — the declared final-phase assertion failed", got.Reasons)
	}
	if !hasReason(got.Reasons, domain.ReasonStateIntegrity) {
		t.Errorf("Reasons = %+v, want ReasonStateIntegrity — a lock reclamation occurred in the same run and must not be masked by the assertion failure", got.Reasons)
	}
}
