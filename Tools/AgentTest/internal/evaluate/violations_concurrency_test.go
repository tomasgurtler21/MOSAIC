package evaluate_test

// Tests for the protocol-violation assertion (ClassProtocolViolations) and
// the peak-concurrency assertion (ClassMinConcurrency). The concurrency
// cases build their evidence from concurrency.Peaks itself — real interval
// reconstruction — rather than a hand-written peak number, per the plan's
// requirement that this assertion be tested against the real producer.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/concurrency"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func intp(i int) *int { return &i }

func TestEvaluate_ProtocolViolations_MatchesDeclaredCount_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.ProtocolViolations = map[domain.ViolationClassKey]int{}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — zero violations recorded, zero declared", ar.Outcome)
	}
}

func TestEvaluate_ProtocolViolations_ExceedsDeclaredCount_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.ProtocolViolations = map[domain.ViolationClassKey]int{
		"missing_required_field": 2,
	}
	ev.Definition.Assertions.ProtocolViolations = intp(0)

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassProtocolViolations, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — 2 violations were recorded against a declared 0", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// buildOverlappingGroupRecords produces start/end records for two
// collaborators in the same declared group, overlapping for one second, so
// concurrency.Peaks reconstructs a real peak of 2.
func buildOverlappingGroupRecords() ([]domain.LogRecord, []domain.ParallelGroup) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	groups := []domain.ParallelGroup{
		{Name: "fanout", Members: []domain.CollaboratorIdentity{researcher(), reviewer()}},
	}
	records := []domain.LogRecord{
		{Kind: domain.RecordStart, Seq: 1, Identity: researcher(), Group: "fanout", CorrelationToken: "tok-1", Timestamp: start},
		{Kind: domain.RecordStart, Seq: 2, Identity: reviewer(), Group: "fanout", CorrelationToken: "tok-2", Timestamp: start.Add(200 * time.Millisecond)},
		{Kind: domain.RecordEnd, Seq: 1, Identity: researcher(), CorrelationToken: "tok-1", Timestamp: start.Add(time.Second), Echo: &domain.EchoOutcome{Match: true}},
		{Kind: domain.RecordEnd, Seq: 2, Identity: reviewer(), CorrelationToken: "tok-2", Timestamp: start.Add(1200 * time.Millisecond), Echo: &domain.EchoOutcome{Match: true}},
	}
	return records, groups
}

func TestEvaluate_MinConcurrency_MetAgainstRealReconstruction_Passes(t *testing.T) {
	records, groups := buildOverlappingGroupRecords()
	peaks, report := concurrency.Peaks(records, groups)
	if peaks["fanout"] != 2 {
		t.Fatalf("test setup error: reconstructed peak = %d, want 2", peaks["fanout"])
	}

	ev := baseEvidence()
	ev.Records = records
	ev.Definition.ParallelGroups = groups
	ev.PeakConcurrency = peaks
	ev.ConcurrencyProblems = domain.ConcurrencyProblems{
		UnterminatedSeqs: report.UnterminatedSeqs,
		Ungrouped:        report.Ungrouped,
	}
	ev.Definition.Assertions.MinConcurrency = map[string]int{"fanout": 2}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassMinConcurrency, "fanout")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — the reconstructed peak (2) meets the declared minimum (2)", ar.Outcome)
	}
}

func TestEvaluate_MinConcurrency_NotMetAgainstRealReconstruction_Fails(t *testing.T) {
	records, groups := buildOverlappingGroupRecords()
	peaks, _ := concurrency.Peaks(records, groups)

	ev := baseEvidence()
	ev.Records = records
	ev.Definition.ParallelGroups = groups
	ev.PeakConcurrency = peaks
	ev.Definition.Assertions.MinConcurrency = map[string]int{"fanout": 3}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassMinConcurrency, "fanout")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — the reconstructed peak (2) does not meet the declared minimum (3)", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_UnterminatedInterval_SurfacesAsCondition covers the "a
// missing measurement is never a zero" rule for concurrency evidence: an
// invocation with no end record is excluded from the peak rather than
// silently folded in, and that exclusion must be visible on the result.
func TestEvaluate_UnterminatedInterval_SurfacesAsCondition(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	groups := []domain.ParallelGroup{
		{Name: "fanout", Members: []domain.CollaboratorIdentity{researcher()}},
	}
	records := []domain.LogRecord{
		// No matching end record: this invocation crashed or timed out.
		{Kind: domain.RecordStart, Seq: 1, Identity: researcher(), Group: "fanout", CorrelationToken: "tok-1", Timestamp: start},
	}
	peaks, report := concurrency.Peaks(records, groups)

	ev := baseEvidence()
	ev.Records = records
	ev.Definition.ParallelGroups = groups
	ev.PeakConcurrency = peaks
	ev.ConcurrencyProblems = domain.ConcurrencyProblems{UnterminatedSeqs: report.UnterminatedSeqs}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionUnterminatedInterval) {
		t.Errorf("Conditions = %+v, want ConditionUnterminatedInterval surfaced for the crashed invocation", got.Conditions)
	}
}
