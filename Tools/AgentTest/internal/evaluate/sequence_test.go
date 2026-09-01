package evaluate_test

// Tests for the invocation-sequence assertion (ClassInvocationSequence),
// including parallel-group semantics: within a declared parallel group,
// order between members must not decide the verdict, while order between
// groups (and between a group and an ordered step) must.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_InvocationSequence_OrderedStepsInOrder_Passes(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, reviewer(), "", start.Add(2*time.Second), plainMessage("reviewer#1")),
		endRecord(2, 1, reviewer(), start.Add(3*time.Second)),
	}
	r1, r2 := researcher(), reviewer()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r1},
			{Identity: &r2},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass (researcher then reviewer matches the declared order): %+v", ar.Outcome, ar)
	}
}

func TestEvaluate_InvocationSequence_OrderedStepsOutOfOrder_Fails(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	// Observed order is reviewer then researcher.
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, reviewer(), "", start, plainMessage("reviewer#1")),
		endRecord(1, 1, reviewer(), start.Add(time.Second)),
		startRecord(2, 1, researcher(), "", start.Add(2*time.Second), plainMessage("researcher#1")),
		endRecord(2, 1, researcher(), start.Add(3*time.Second)),
	}
	r1, r2 := researcher(), reviewer()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		// Declared order is researcher then reviewer — the reverse of what
		// was observed.
		Steps: []domain.SequenceStep{
			{Identity: &r1},
			{Identity: &r2},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail (observed order is the reverse of the declared order)", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_InvocationSequence_ParallelGroupMembersAnyOrder_Passes is the
// decisive AC8.10 property: two runs whose only difference is which member
// of a declared parallel group happened to log first must both pass the
// same sequence assertion.
func TestEvaluate_InvocationSequence_ParallelGroupMembersAnyOrder_Passes(t *testing.T) {
	assertion := func(members []domain.CollaboratorIdentity) *domain.SequenceAssertion {
		return &domain.SequenceAssertion{
			Steps: []domain.SequenceStep{
				{Group: "fanout", Members: members},
			},
		}
	}
	group := []domain.CollaboratorIdentity{researcher(), reviewer()}
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	orderings := [][]domain.CollaboratorIdentity{
		{researcher(), reviewer()},
		{reviewer(), researcher()},
	}
	for i, order := range orderings {
		ev := baseEvidence()
		ev.Definition.ParallelGroups = []domain.ParallelGroup{{Name: "fanout", Members: group}}
		ev.Definition.Assertions.InvocationSequence = assertion(group)
		ev.Records = []domain.LogRecord{
			startRecord(1, 1, order[0], "fanout", start, plainMessage(order[0].AgentIdentity+"#1")),
			endRecord(1, 1, order[0], start.Add(time.Second)),
			startRecord(2, 1, order[1], "fanout", start.Add(500*time.Millisecond), plainMessage(order[1].AgentIdentity+"#1")),
			endRecord(2, 1, order[1], start.Add(2*time.Second)),
		}

		got := evaluate.Evaluate(ev)

		ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
		if ar.Outcome != domain.AssertionPass {
			t.Errorf("ordering %d (%v): Outcome = %q, want pass — order within a parallel group must not decide the verdict", i, order, ar.Outcome)
		}
	}
}

// TestEvaluate_InvocationSequence_OrderBetweenGroupsMatters_Fails is the
// other half of AC8.10: order between an ordered step and a parallel group
// (or between two groups) is decisive, unlike order within one group.
func TestEvaluate_InvocationSequence_OrderBetweenGroupsMatters_Fails(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	group := []domain.CollaboratorIdentity{reviewer(), testWriter()}
	ev := baseEvidence()
	ev.Definition.ParallelGroups = []domain.ParallelGroup{{Name: "fanout", Members: group}}
	tw := testWriter()
	// Declared: researcher first, then the fanout group.
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: func() *domain.CollaboratorIdentity { r := researcher(); return &r }()},
			{Group: "fanout", Members: group},
		},
	}
	// Observed: the fanout group logs before researcher — the group and the
	// ordered step are transposed.
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, reviewer(), "fanout", start, plainMessage("reviewer#1")),
		endRecord(1, 1, reviewer(), start.Add(time.Second)),
		startRecord(2, 1, tw, "fanout", start.Add(200*time.Millisecond), plainMessage("test-writer#1")),
		endRecord(2, 1, tw, start.Add(2*time.Second)),
		startRecord(3, 1, researcher(), "", start.Add(3*time.Second), plainMessage("researcher#1")),
		endRecord(3, 1, researcher(), start.Add(4*time.Second)),
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — order between the ordered step and the group must decide the verdict", ar.Outcome)
	}
}

// TestEvaluate_InvocationSequence_OrderBetweenTwoGroupsMatters_Fails is the
// more direct reading of AC8.10's "order between groups": two declared
// parallel groups whose relative order is transposed in the observed
// records, distinct from the ordered-step-vs-group case above.
func TestEvaluate_InvocationSequence_OrderBetweenTwoGroupsMatters_Fails(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	groupA := []domain.CollaboratorIdentity{researcher(), reviewer()}
	groupB := []domain.CollaboratorIdentity{testWriter(), implementer()}
	ev := baseEvidence()
	ev.Definition.ParallelGroups = []domain.ParallelGroup{
		{Name: "groupA", Members: groupA},
		{Name: "groupB", Members: groupB},
	}
	// Declared: groupA first, then groupB.
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Group: "groupA", Members: groupA},
			{Group: "groupB", Members: groupB},
		},
	}
	// Observed: groupB logs before groupA — the two declared groups are
	// transposed.
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, testWriter(), "groupB", start, plainMessage("test-writer#1")),
		endRecord(1, 1, testWriter(), start.Add(time.Second)),
		startRecord(2, 1, implementer(), "groupB", start.Add(200*time.Millisecond), plainMessage("implementer#1")),
		endRecord(2, 1, implementer(), start.Add(2*time.Second)),
		startRecord(3, 1, researcher(), "groupA", start.Add(3*time.Second), plainMessage("researcher#1")),
		endRecord(3, 1, researcher(), start.Add(4*time.Second)),
		startRecord(4, 1, reviewer(), "groupA", start.Add(3200*time.Millisecond), plainMessage("reviewer#1")),
		endRecord(4, 1, reviewer(), start.Add(5*time.Second)),
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — order between two declared parallel groups must decide the verdict", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

func TestEvaluate_InvocationSequence_ExactWithExtraInvocation_Fails(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	r1 := researcher()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{{Identity: &r1}},
		Exact: true,
	}
	// An unexpected second invocation the assertion never declared.
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, reviewer(), "", start.Add(2*time.Second), plainMessage("reviewer#1")),
		endRecord(2, 1, reviewer(), start.Add(3*time.Second)),
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — Exact requires nothing beyond the declared steps", ar.Outcome)
	}
}

func TestEvaluate_InvocationSequence_SubsequenceMatch_PassesWithExtraInvocation(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	r1 := researcher()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{{Identity: &r1}},
		Exact: false,
	}
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, reviewer(), "", start.Add(2*time.Second), plainMessage("reviewer#1")),
		endRecord(2, 1, reviewer(), start.Add(3*time.Second)),
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — a non-exact assertion is a subsequence match", ar.Outcome)
	}
}

// TestEvaluate_InvocationSequence_FailDetail_IncludesObservedIdentities verifies
// that when an expected identity is not found, the failure Detail includes the
// identities of the invocations actually observed at/after the search position,
// so the user can see what was there instead of the expected step.
func TestEvaluate_InvocationSequence_FailDetail_IncludesObservedIdentities(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	// Observed: reviewer then implementer — neither is researcher.
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, reviewer(), "", start, plainMessage("reviewer#1")),
		endRecord(1, 1, reviewer(), start.Add(time.Second)),
		startRecord(2, 1, implementer(), "", start.Add(2*time.Second), plainMessage("implementer#1")),
		endRecord(2, 1, implementer(), start.Add(3*time.Second)),
	}
	// Declared: researcher must appear, but researcher never logged.
	r := researcher()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{{Identity: &r}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — researcher was never observed", ar.Outcome)
	}
	// The Detail must name what was actually observed at/after the search position
	// so the user knows what was there instead of the expected step.
	if !strings.Contains(ar.Detail, reviewer().Key()) {
		t.Errorf("Detail = %q; want it to contain the observed identity %q", ar.Detail, reviewer().Key())
	}
	if !strings.Contains(ar.Detail, implementer().Key()) {
		t.Errorf("Detail = %q; want it to contain the observed identity %q", ar.Detail, implementer().Key())
	}
}

// TestEvaluate_InvocationSequence_FailDetail_AtSearchPosition verifies that
// the observed invocations reported in the failure Detail are those at/after
// the current search position, not invocations before it. When the first step
// passes (advancing the pointer), only invocations after the match position
// should appear in the next step's failure Detail.
func TestEvaluate_InvocationSequence_FailDetail_AtSearchPosition(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	// Observed: researcher (position 0), then implementer (position 1).
	// First declared step (researcher) will match at position 0 — pointer advances to 1.
	// Second declared step (reviewer) will not be found at/after position 1.
	// The Detail for the second step's failure must include implementer (at position 1)
	// but must NOT include researcher (at position 0, already consumed).
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, implementer(), "", start.Add(2*time.Second), plainMessage("implementer#1")),
		endRecord(2, 1, implementer(), start.Add(3*time.Second)),
	}
	r1, r2 := researcher(), reviewer()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r1}, // matches at position 0
			{Identity: &r2}, // not found at or after position 1
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — reviewer was never observed after researcher", ar.Outcome)
	}
	// implementer is at/after the search position (position 1) and must appear in Detail.
	if !strings.Contains(ar.Detail, implementer().Key()) {
		t.Errorf("Detail = %q; want it to contain the observed identity %q at/after the search position", ar.Detail, implementer().Key())
	}
	// researcher is before the search position (position 0, already consumed) and must NOT appear in Detail.
	if strings.Contains(ar.Detail, researcher().Key()) {
		t.Errorf("Detail must not contain researcher (consumed before the search position)")
	}
}

// TestEvaluate_InvocationSequence_Pass_PopulatesExpected verifies that on a
// passing InvocationSequence assertion, the Expected field is populated so the
// user can see what sequence was declared — not just that it passed.
func TestEvaluate_InvocationSequence_Pass_PopulatesExpected(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
		startRecord(2, 1, reviewer(), "", start.Add(2*time.Second), plainMessage("reviewer#1")),
		endRecord(2, 1, reviewer(), start.Add(3*time.Second)),
	}
	r1, r2 := researcher(), reviewer()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r1},
			{Identity: &r2},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass", ar.Outcome)
	}
	// Expected must be non-empty on pass so the user can see what sequence was declared.
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want it to be non-empty on a passing assertion to describe the declared sequence", ar.Expected)
	}
}

// TestEvaluate_InvocationSequence_Fail_PopulatesExpectedAndActual verifies
// that on a failing InvocationSequence assertion both Expected and Actual are
// populated so the user can compare the declared sequence against what was
// actually observed.
func TestEvaluate_InvocationSequence_Fail_PopulatesExpectedAndActual(t *testing.T) {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	// Observed: reviewer then researcher (wrong order).
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, reviewer(), "", start, plainMessage("reviewer#1")),
		endRecord(1, 1, reviewer(), start.Add(time.Second)),
		startRecord(2, 1, researcher(), "", start.Add(2*time.Second), plainMessage("researcher#1")),
		endRecord(2, 1, researcher(), start.Add(3*time.Second)),
	}
	// Declared: researcher then reviewer.
	r1, r2 := researcher(), reviewer()
	ev.Definition.Assertions.InvocationSequence = &domain.SequenceAssertion{
		Steps: []domain.SequenceStep{
			{Identity: &r1},
			{Identity: &r2},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassInvocationSequence, "")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — observed order is the reverse of the declared order", ar.Outcome)
	}
	// Both Expected and Actual must be non-empty on fail so the user can compare
	// the declared sequence against what was actually observed.
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want it to be non-empty on a failing assertion to describe the declared sequence", ar.Expected)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want it to be non-empty on a failing assertion to describe the observed sequence", ar.Actual)
	}
}
