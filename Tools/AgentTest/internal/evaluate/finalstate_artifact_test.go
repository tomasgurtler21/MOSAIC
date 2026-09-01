package evaluate_test

// Tests for final-state assertions (ClassFinalPhase, ClassFinalStatus) and
// for artifact creation / non-creation assertions (ClassArtifactCreated,
// ClassArtifactNotCreated). Asserting a file was *not* created is as
// load-bearing as asserting it was, and is the easier of the two to get
// quietly wrong, so both directions get a passing and a failing case.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func strp(s string) *string { return &s }

func TestEvaluate_FinalPhase_Matches_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.Phase = "complete"
	ev.Definition.Assertions.FinalPhase = strp("complete")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassFinalPhase, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass", ar.Outcome)
	}
}

func TestEvaluate_FinalPhase_Mismatches_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.Phase = "in_progress"
	ev.Definition.Assertions.FinalPhase = strp("complete")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassFinalPhase, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — phase is %q, declared %q", ar.Outcome, ev.Orchestration.Phase, "complete")
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

func TestEvaluate_FinalStatus_Matches_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.LastStatus = "SUCCESS"
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassFinalStatus, "")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass", ar.Outcome)
	}
}

func TestEvaluate_FinalStatus_Mismatches_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.Orchestration.LastStatus = "BLOCKED"
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassFinalStatus, "")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — last_status is %q, declared %q", ar.Outcome, ev.Orchestration.LastStatus, "SUCCESS")
	}
}

func TestEvaluate_ArtifactCreated_Present_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md", "Requirements.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — Research.md is in the snapshot", ar.Outcome)
	}
}

func TestEvaluate_ArtifactCreated_Absent_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Requirements.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — Research.md was declared created but is absent from the snapshot", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_ArtifactNotCreated_TrulyAbsent_Passes covers the easy-to-get-
// wrong direction the plan calls out: a negative existence claim.
func TestEvaluate_ArtifactNotCreated_TrulyAbsent_Passes(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — scratch.tmp is truly absent from the snapshot", ar.Outcome)
	}
}

func TestEvaluate_ArtifactNotCreated_UnexpectedlyPresent_Fails(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md", "scratch.tmp"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — scratch.tmp was declared not created but is present in the snapshot", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_ArtifactCreated_Pass_PopulatesExpectedAndActual verifies that
// Expected and Actual carry meaningful, non-empty content when an artifact is
// present in the snapshot (pass outcome), so the evidence is always available
// regardless of outcome — not only on fail.
func TestEvaluate_ArtifactCreated_Pass_PopulatesExpectedAndActual(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass — Research.md is in the snapshot", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want non-empty on pass — the assertion must always carry its expectation for diagnostic context", ar.Expected)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want non-empty on pass — the assertion must always carry what was observed for diagnostic context", ar.Actual)
	}
}

// TestEvaluate_ArtifactCreated_Fail_PopulatesExpectedAndActual verifies that
// Expected and Actual carry meaningful, non-empty content when an artifact is
// absent from the snapshot (fail outcome).
func TestEvaluate_ArtifactCreated_Fail_PopulatesExpectedAndActual(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Requirements.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — Research.md is absent from the snapshot", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want non-empty on fail — the assertion must always carry its expectation for diagnostic context", ar.Expected)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want non-empty on fail — the assertion must always carry what was observed for diagnostic context", ar.Actual)
	}
}

// TestEvaluate_ArtifactNotCreated_Pass_PopulatesExpectedAndActual verifies that
// Expected and Actual carry meaningful, non-empty content when an artifact is
// truly absent from the snapshot (pass outcome for a not-created assertion).
func TestEvaluate_ArtifactNotCreated_Pass_PopulatesExpectedAndActual(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass — scratch.tmp is absent from the snapshot", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want non-empty on pass — the assertion must always carry its expectation for diagnostic context", ar.Expected)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want non-empty on pass — the assertion must always carry what was observed for diagnostic context", ar.Actual)
	}
}

// TestEvaluate_ArtifactNotCreated_Fail_PopulatesExpectedAndActual verifies that
// Expected and Actual carry meaningful, non-empty content when an artifact is
// unexpectedly present in the snapshot (fail outcome for a not-created assertion).
func TestEvaluate_ArtifactNotCreated_Fail_PopulatesExpectedAndActual(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md", "scratch.tmp"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — scratch.tmp is present in the snapshot but was declared not created", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Errorf("Expected = %q; want non-empty on fail — the assertion must always carry its expectation for diagnostic context", ar.Expected)
	}
	if ar.Actual == "" {
		t.Errorf("Actual = %q; want non-empty on fail — the assertion must always carry what was observed for diagnostic context", ar.Actual)
	}
}

func TestEvaluate_ArtifactCreated_EmptyDeclaredSet_TriviallyPasses(t *testing.T) {
	ev := baseEvidence()
	ev.Definition.Assertions.ArtifactCreated = []string{}

	got := evaluate.Evaluate(ev)

	for _, ar := range got.Assertions {
		if ar.Class == domain.ClassArtifactCreated {
			t.Errorf("expected no ClassArtifactCreated entries for an empty declared set, found %+v", ar)
		}
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — an empty declared set asserts nothing and nothing else fails here", got.Verdict)
	}
}

// TestEvaluate_ArtifactCreated_Pass_PopulatesTarget verifies that the Target
// field carries the artifact name on a passing ArtifactCreated assertion, so
// the diagnostic context is always present regardless of outcome.
func TestEvaluate_ArtifactCreated_Pass_PopulatesTarget(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md", "Requirements.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass", ar.Outcome)
	}
	if ar.Target != "Research.md" {
		t.Errorf("Target = %q, want %q — Target must be populated on pass for diagnostic context", ar.Target, "Research.md")
	}
}

// TestEvaluate_ArtifactCreated_Fail_PopulatesTargetAndDetail verifies that
// both Target and Detail are populated on a failing ArtifactCreated assertion,
// so the user can see which artifact was missing and why it failed.
func TestEvaluate_ArtifactCreated_Fail_PopulatesTargetAndDetail(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Requirements.md"}
	ev.Definition.Assertions.ArtifactCreated = []string{"Research.md"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, "Research.md")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — Research.md is absent from the snapshot", ar.Outcome)
	}
	if ar.Target != "Research.md" {
		t.Errorf("Target = %q, want %q — Target must be populated on fail for diagnostic context", ar.Target, "Research.md")
	}
	if ar.Detail == "" {
		t.Errorf("Detail = %q; want it to be non-empty on fail to explain why the assertion failed", ar.Detail)
	}
}

// TestEvaluate_ArtifactNotCreated_Pass_PopulatesTarget verifies that the
// Target field carries the artifact name on a passing ArtifactNotCreated
// assertion, so the diagnostic context is always present regardless of outcome.
func TestEvaluate_ArtifactNotCreated_Pass_PopulatesTarget(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionPass {
		t.Fatalf("Outcome = %q, want pass — scratch.tmp is absent from the snapshot", ar.Outcome)
	}
	if ar.Target != "scratch.tmp" {
		t.Errorf("Target = %q, want %q — Target must be populated on pass for diagnostic context", ar.Target, "scratch.tmp")
	}
}

// TestEvaluate_ArtifactNotCreated_Fail_PopulatesTargetAndDetail verifies that
// both Target and Detail are populated on a failing ArtifactNotCreated
// assertion, so the user can see which artifact was unexpectedly present and
// why it failed.
func TestEvaluate_ArtifactNotCreated_Fail_PopulatesTargetAndDetail(t *testing.T) {
	ev := baseEvidence()
	ev.SnapshotFiles = []string{"Research.md", "scratch.tmp"}
	ev.Definition.Assertions.ArtifactNotCreated = []string{"scratch.tmp"}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, "scratch.tmp")
	if ar.Outcome != domain.AssertionFail {
		t.Fatalf("Outcome = %q, want fail — scratch.tmp is present in the snapshot but was declared not created", ar.Outcome)
	}
	if ar.Target != "scratch.tmp" {
		t.Errorf("Target = %q, want %q — Target must be populated on fail for diagnostic context", ar.Target, "scratch.tmp")
	}
	if ar.Detail == "" {
		t.Errorf("Detail = %q; want it to be non-empty on fail to explain why the assertion failed", ar.Detail)
	}
}
