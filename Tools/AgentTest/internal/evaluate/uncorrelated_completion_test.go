package evaluate_test

// Tests for the loud-correlation-failure contracts in the verdict engine:
//
//   - When records include an uncorrelated-completion run event, echo-fidelity
//     for that completion is reported as not-evaluated with a reason, rather
//     than silently absent from the result.
//   - When RunEvidence.Residue carries unresolved pending stubs or unreleased
//     in-flight entries, ConditionLeakedRunState is raised on the result.
//   - The two new condition kinds are additive: they fire alongside every
//     existing condition without replacing or suppressing any.
//   - A zero-valued Residue and no uncorrelated-completion events produce no
//     new conditions, so existing golden report comparisons remain stable.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// uncorrelatedEndRecord returns a RecordEnd with no echo and no identity,
// representing the silent empty record that used to be produced when
// correlation failed — a completion with no token and no pending stub resolved.
func uncorrelatedEndRecord(at time.Time) domain.LogRecord {
	return domain.LogRecord{
		Kind:      domain.RecordEnd,
		TestID:    "example",
		RunNumber: 1,
		Timestamp: at,
		// Seq, Identity and Echo are all zero — this is the defect: an end
		// record indistinguishable from a well-formed one with missing data.
	}
}

// uncorrelatedCompletionEvent returns the RecordRun that should accompany an
// uncorrelated completion once the decision core is updated to emit it.
func uncorrelatedCompletionEvent(at time.Time, detail string) domain.LogRecord {
	return domain.LogRecord{
		Kind:      domain.RecordRun,
		TestID:    "example",
		RunNumber: 1,
		Timestamp: at,
		Event:     domain.RunEventUncorrelatedCompletion,
		Detail:    detail,
	}
}

// --- T2.2: echo-fidelity non-evaluation is reported with its reason ---

// TestEvaluate_UncorrelatedCompletion_EchoFidelity_ReportedAsNotEvaluated
// asserts that when a run's records include RunEventUncorrelatedCompletion,
// the evaluate result contains an echo-fidelity assertion result with outcome
// AssertionNotEvaluated and a non-empty Detail naming the reason. The current
// implementation omits any echo-fidelity result for the uncorrelated
// completion — this test must fail (RED) until evaluateConditions and the
// assertion evaluator are updated to surface the non-evaluation explicitly.
func TestEvaluate_UncorrelatedCompletion_EchoFidelity_ReportedAsNotEvaluated(t *testing.T) {
	start := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	ev := baseEvidence()
	// Replace the default records with one that represents a run whose
	// completion could not be correlated: an empty end record (no seq, no
	// identity, no echo) accompanied by the run event that names the failure.
	ev.Records = []domain.LogRecord{
		uncorrelatedEndRecord(start),
		uncorrelatedCompletionEvent(start, "1 completion could not be correlated; 1 dispatch was outstanding"),
	}

	got := evaluate.Evaluate(ev)

	// The evaluate result must include an echo-fidelity assertion outcome
	// marked as not-evaluated, with a non-empty Detail explaining why.
	// A nil Echo in the end record means "not evaluated" in the current
	// implementation but is silently absent from the assertions list —
	// that silence is exactly what this test pins against.
	foundNotEvaluated := false
	for _, ar := range got.Assertions {
		if ar.Class == domain.ClassEchoFidelity && ar.Outcome == domain.AssertionNotEvaluated {
			foundNotEvaluated = true
			if ar.Detail == "" {
				t.Errorf("echo-fidelity AssertionNotEvaluated must carry a non-empty Detail naming the reason, got empty Detail")
			}
		}
	}
	if !foundNotEvaluated {
		t.Errorf("expected an echo-fidelity AssertionResult with Outcome=AssertionNotEvaluated for an uncorrelated completion, got Assertions = %+v", got.Assertions)
	}
}

// TestEvaluate_UncorrelatedCompletion_RaisesCondition asserts that the
// ConditionUncorrelatedCompletion is raised on the result when a run's
// records contain RunEventUncorrelatedCompletion. This tests the evaluate
// package's conditions path (I2.2 scope), separate from the intercept
// package's emission path.
func TestEvaluate_UncorrelatedCompletion_RaisesCondition(t *testing.T) {
	start := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		uncorrelatedEndRecord(start),
		uncorrelatedCompletionEvent(start, "1 completion could not be correlated; 1 dispatch was outstanding"),
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionUncorrelatedCompletion) {
		t.Errorf("Conditions = %+v, want ConditionUncorrelatedCompletion when records contain RunEventUncorrelatedCompletion", got.Conditions)
	}
	cond := findCondition(t, got.Conditions, domain.ConditionUncorrelatedCompletion)
	if cond.Detail == "" {
		t.Error("ConditionUncorrelatedCompletion.Detail must not be empty — it must name how many completions were affected")
	}
}

// TestEvaluate_NoUncorrelatedCompletionEvent_DoesNotRaiseCondition asserts
// that a clean run with no RunEventUncorrelatedCompletion records does not
// raise ConditionUncorrelatedCompletion. This is the stability guard: new
// conditions must not fire on runs that do not trigger them.
func TestEvaluate_NoUncorrelatedCompletionEvent_DoesNotRaiseCondition(t *testing.T) {
	ev := baseEvidence()
	// baseEvidence has a correlated end record (Echo.Match == true) and no
	// RunEventUncorrelatedCompletion.

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionUncorrelatedCompletion) {
		t.Errorf("Conditions = %+v, ConditionUncorrelatedCompletion must not fire on a run with no uncorrelated completions", got.Conditions)
	}
}

// --- T2.4: leaked pending/in-flight state reported at end of run ---

// TestEvaluate_LeakedPendingStubs_RaisesLeakedRunStateCondition asserts that
// when RunEvidence.Residue.PendingStubs is non-empty, ConditionLeakedRunState
// is present on the result. This tests the end-of-run infrastructure check
// (I2.3 scope) — the current evaluateConditions does not inspect Residue at
// all, so this test must fail (RED) until it does.
func TestEvaluate_LeakedPendingStubs_RaisesLeakedRunStateCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Residue = domain.StateResidue{
		PendingStubs: []domain.ResidualDispatch{
			{Seq: 1, Identity: researcher()},
		},
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, want ConditionLeakedRunState when pending stubs are still outstanding at end of run", got.Conditions)
	}
	cond := findCondition(t, got.Conditions, domain.ConditionLeakedRunState)
	if cond.Detail == "" {
		t.Error("ConditionLeakedRunState.Detail must not be empty — it must name the counts and sequence numbers of the leaked dispatches")
	}
}

// TestEvaluate_LeakedInFlight_RaisesLeakedRunStateCondition asserts that
// unreleased in-flight entries also trigger ConditionLeakedRunState. An
// in-flight entry without a corresponding pending stub (passthrough policy)
// can leak independently of a pending stub leak.
func TestEvaluate_LeakedInFlight_RaisesLeakedRunStateCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Residue = domain.StateResidue{
		InFlight: []domain.ResidualDispatch{
			{Seq: 2, Identity: researcher()},
		},
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, want ConditionLeakedRunState when in-flight entries are still outstanding at end of run", got.Conditions)
	}
	cond := findCondition(t, got.Conditions, domain.ConditionLeakedRunState)
	if cond.Detail == "" {
		t.Error("ConditionLeakedRunState.Detail must not be empty")
	}
}

// TestEvaluate_BothPendingAndInFlight_RaisesOneLeakedRunStateCondition asserts
// that both kinds of leaked state together raise exactly one
// ConditionLeakedRunState (not two): the condition covers all leaked state on
// a run, not one instance per entry.
func TestEvaluate_BothPendingAndInFlight_RaisesOneLeakedRunStateCondition(t *testing.T) {
	ev := baseEvidence()
	ev.Residue = domain.StateResidue{
		PendingStubs: []domain.ResidualDispatch{{Seq: 1, Identity: researcher()}},
		InFlight:     []domain.ResidualDispatch{{Seq: 1, Identity: researcher()}},
	}

	got := evaluate.Evaluate(ev)

	count := 0
	for _, c := range got.Conditions {
		if c.Kind == domain.ConditionLeakedRunState {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want exactly 1 ConditionLeakedRunState condition for a run with both leaked pending stubs and in-flight entries, got %d in %+v", count, got.Conditions)
	}
}

// TestEvaluate_ZeroResidue_DoesNotRaiseLeakedRunStateCondition asserts that
// a clean run with nothing outstanding produces no ConditionLeakedRunState.
// This is the stability guard: the zero-valued Residue field on existing
// RunEvidence must never trigger the new condition, so golden report
// comparisons remain stable.
func TestEvaluate_ZeroResidue_DoesNotRaiseLeakedRunStateCondition(t *testing.T) {
	ev := baseEvidence()
	// Residue is zero-valued — the expected, clean case.

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, zero-valued Residue must not raise ConditionLeakedRunState", got.Conditions)
	}
}

// TestEvaluate_UnreadableResidue_DoesNotSilentlyReadAsClean asserts that a
// run whose state could not be read at snapshot time (Residue.Unreadable is
// set) raises a condition rather than reading as "nothing leaked". The
// condition must name the unreadability, not suppress it.
func TestEvaluate_UnreadableResidue_DoesNotSilentlyReadAsClean(t *testing.T) {
	ev := baseEvidence()
	ev.Residue = domain.StateResidue{
		Unreadable: "run state file was inaccessible at snapshot time",
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, an unreadable residue must raise ConditionLeakedRunState rather than reading as a clean run", got.Conditions)
	}
	cond := findCondition(t, got.Conditions, domain.ConditionLeakedRunState)
	if cond.Detail == "" {
		t.Error("ConditionLeakedRunState.Detail must name the unreadability reason, not be empty")
	}
}

// --- T2.5: new conditions are additive, golden report comparisons stable ---

// TestEvaluate_LeakedRunState_IsAdditive_AlongsideCostUnattributed asserts
// that ConditionLeakedRunState fires alongside ConditionCostUnattributed on
// a run that triggers both. The new condition is additive — it never replaces
// or suppresses an existing one.
func TestEvaluate_LeakedRunState_IsAdditive_AlongsideCostUnattributed(t *testing.T) {
	ev := baseEvidence()
	ev.Cost = domain.CostReport{Attribution: domain.AttributionUnavailable, Detail: "cost: delegate could not be invoked"}
	ev.Residue = domain.StateResidue{
		PendingStubs: []domain.ResidualDispatch{{Seq: 1, Identity: researcher()}},
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionCostUnattributed) {
		t.Errorf("Conditions = %+v, existing ConditionCostUnattributed must still fire when ConditionLeakedRunState is also raised", got.Conditions)
	}
	if !hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, ConditionLeakedRunState must fire alongside existing ConditionCostUnattributed", got.Conditions)
	}
}

// TestEvaluate_UncorrelatedCompletion_IsAdditive_AlongsideExtractionDegraded
// asserts that ConditionUncorrelatedCompletion fires alongside
// ConditionExtractionDegraded on a run that has both. New conditions must
// not suppress or displace conditions that already fire.
func TestEvaluate_UncorrelatedCompletion_IsAdditive_AlongsideExtractionDegraded(t *testing.T) {
	start := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	ev := baseEvidence()
	// Build records that trigger both conditions: a degraded-extraction start
	// record and an uncorrelated completion.
	ev.Records = []domain.LogRecord{
		{
			Kind:      domain.RecordStart,
			TestID:    "example",
			RunNumber: 1,
			Seq:       1,
			Ordinal:   1,
			Identity:  researcher(),
			Timestamp: start,
			Outcome:   domain.OutcomeRewritePrompt,
			Message:   &domain.TaskMessage{Raw: "not valid json", Extraction: domain.ExtractionDegraded},
		},
		uncorrelatedEndRecord(start.Add(time.Second)),
		uncorrelatedCompletionEvent(start.Add(time.Second), "1 completion could not be correlated; 1 dispatch was outstanding"),
	}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionExtractionDegraded) {
		t.Errorf("Conditions = %+v, existing ConditionExtractionDegraded must still fire alongside new uncorrelated-completion condition", got.Conditions)
	}
	if !hasConditionKind(got.Conditions, domain.ConditionUncorrelatedCompletion) {
		t.Errorf("Conditions = %+v, ConditionUncorrelatedCompletion must fire alongside existing ConditionExtractionDegraded", got.Conditions)
	}
}

// TestEvaluate_CleanRun_NoNewConditions asserts that a run with no new
// triggers produces no new conditions — the baseline stability guarantee.
// This is the property that keeps golden report comparisons valid: adding
// new condition kinds to the vocabulary must not affect any run that does
// not exhibit those conditions.
func TestEvaluate_CleanRun_NoNewConditions(t *testing.T) {
	ev := baseEvidence()
	// baseEvidence is a clean run with no leaked state and no uncorrelated
	// completions; Residue is zero-valued.

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionUncorrelatedCompletion) {
		t.Errorf("Conditions = %+v, a clean run must not raise ConditionUncorrelatedCompletion", got.Conditions)
	}
	if hasConditionKind(got.Conditions, domain.ConditionLeakedRunState) {
		t.Errorf("Conditions = %+v, a clean run must not raise ConditionLeakedRunState", got.Conditions)
	}
}

// Verify that evaluate.Evaluate is actually called in these tests.
var _ = evaluate.Evaluate
