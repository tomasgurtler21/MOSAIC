package evaluate_test

// Tests for the invocation-sequence assertion (ClassInvocationSequence),
// including parallel-group semantics: within a declared parallel group,
// order between members must not decide the verdict, while order between
// groups (and between a group and an ordered step) must.

import (
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
