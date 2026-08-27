package evaluate_test

// Tests for the second, independent verdict-integrity guard: Evaluate must
// refuse to report VerdictPass for a test whose definition declared one or
// more assertions but whose evaluation produced zero evaluated assertion
// results — regardless of what caused the zero, and never defeated by
// negative-test inversion. This is deliberately separate from — and not a
// replacement for — the suite-level guard on a discarded runner error: a
// vacuous evidence set must never read as a pass no matter which layer
// produced it.
//
// domain.Assertions documents the trigger directly: "a nil pointer or nil
// slice means the class is not evaluated for this test, which is distinct
// from an empty non-nil slice meaning 'assert the empty set'." A test author
// who declares e.g. artifact_created: [] has declared the class, but today's
// evaluateAssertions loops zero times over it and contributes nothing — the
// exact vacuous condition this guard exists to catch.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// vacuousDeclaredDefinition declares the artifact_created class via a
// non-nil, empty slice — a real assertion declaration, per domain.Assertions'
// own documented distinction between "not evaluated" (nil) and "assert the
// empty set" (empty non-nil) — and declares nothing else, so no other
// assertion class supplies a result either.
func vacuousDeclaredDefinition() domain.TestDefinition {
	return domain.TestDefinition{
		SchemaVersion: 1,
		Name:          "example",
		Layer:         domain.LayerSubagent,
		Assertions: domain.Assertions{
			ArtifactCreated: []string{},
		},
	}
}

// TestEvaluate_DeclaredAssertionsYieldZeroEvaluatedResults_NeverReportsPass
// is the direct AC1.3 case: assertions were declared, evidence produced no
// records (so echo fidelity — the only unconditional class — contributes
// nothing either), and the run must not read as a pass.
func TestEvaluate_DeclaredAssertionsYieldZeroEvaluatedResults_NeverReportsPass(t *testing.T) {
	ev := domain.RunEvidence{
		Definition:    vacuousDeclaredDefinition(),
		Key:           baseKey(),
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}

	got := evaluate.Evaluate(ev)

	if len(got.Assertions) != 0 {
		t.Fatalf("got %d evaluated assertions, want 0 for this fixture — the test setup itself is wrong if this fails", len(got.Assertions))
	}
	if got.Verdict == domain.VerdictPass {
		t.Error("Verdict = PASS, want anything but PASS — assertions were declared but zero were evaluated, which must never read as a pass")
	}
}

// TestEvaluate_DeclaredAssertionsYieldZeroEvaluatedResults_NegativeInversion_NeverReportsPass
// covers the inversion interaction the plan calls out explicitly: a negative
// test inverts assertion outcomes, but inverting an empty set is still an
// empty set, and a vacuous evidence set must not become a PASS by inversion
// either.
func TestEvaluate_DeclaredAssertionsYieldZeroEvaluatedResults_NegativeInversion_NeverReportsPass(t *testing.T) {
	def := vacuousDeclaredDefinition()
	def.Negative = true
	ev := domain.RunEvidence{
		Definition:    def,
		Key:           baseKey(),
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}

	got := evaluate.Evaluate(ev)

	if len(got.Assertions) != 0 {
		t.Fatalf("got %d evaluated assertions, want 0 for this fixture — the test setup itself is wrong if this fails", len(got.Assertions))
	}
	if got.Verdict == domain.VerdictPass {
		t.Error("Verdict = PASS, want anything but PASS — a vacuous evidence set must not become a pass by negative inversion")
	}
	if !got.NegativeApplied {
		t.Error("NegativeApplied = false, want true — the definition declared a negative test")
	}
}

// TestEvaluate_NoAssertionsDeclaredAndNoEvidence_StillPasses is AC1.4: a test
// that genuinely declares no assertions at all (every Assertions field left
// at its nil/zero value) is unaffected by the vacuous-evidence guard and
// keeps its existing behaviour.
func TestEvaluate_NoAssertionsDeclaredAndNoEvidence_StillPasses(t *testing.T) {
	ev := domain.RunEvidence{
		Definition: domain.TestDefinition{
			SchemaVersion: 1,
			Name:          "example",
			Layer:         domain.LayerSubagent,
			// Assertions is left entirely zero-valued: nothing declared.
		},
		Key:           baseKey(),
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}

	got := evaluate.Evaluate(ev)

	if len(got.Assertions) != 0 {
		t.Fatalf("got %d evaluated assertions, want 0 for this fixture — the test setup itself is wrong if this fails", len(got.Assertions))
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — a test genuinely declaring no assertions must keep its existing behaviour, unchanged by the vacuous-evidence guard", got.Verdict)
	}
}
