package evaluate_test

// Tests that evaluate.Evaluate expands the {run_id} placeholder in every
// path-bearing assertion field before comparing declared paths against
// observed paths. The run id in scope is ev.Key.RunID.
//
// Two properties are pinned for each path-bearing field:
//
//  1. A declared path that contains the placeholder is satisfied by the
//     corresponding real observed path (placeholder expands to ev.Key.RunID).
//  2. A declared path that contains no placeholder is compared exactly as
//     authored — the expansion is a no-op for literal paths.
//
// A third property is pinned on the evidence value itself: Evaluate must not
// modify the caller's evidence — the original declared paths must survive the
// call unchanged, so re-evaluating the same evidence yields the same verdict.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// The run ID used across these tests — distinct from the baseEvidence value
// ("run-1") so a test that accidentally uses the wrong ID is detectable.
const expansionRunID = "20260816T092714Z-df8b"

// baseEvidenceForExpansion returns a minimal, otherwise-clean evidence value
// whose Key.RunID is expansionRunID. All expansion tests start from here so
// only the field under test varies.
func baseEvidenceForExpansion() domain.RunEvidence {
	ev := baseEvidence()
	ev.Key.RunID = expansionRunID
	return ev
}

// evidenceWithOneInvocationForExpansion is the expansion-test equivalent of
// the taskmessage helpers: one invocation with msg as its task message, using
// the expansion run ID.
func evidenceWithOneInvocationForExpansion(msg domain.TaskMessage) domain.RunEvidence {
	start := time.Date(2026, 8, 16, 9, 27, 14, 0, time.UTC)
	ev := baseEvidenceForExpansion()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, msg),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	return ev
}

// realPath returns the path that would be observed in a real run: the
// placeholder replaced by the actual run ID.
func realPath(template string) string {
	result := ""
	ph := domain.RunIDPlaceholder
	for i := 0; i < len(template); {
		if i+len(ph) <= len(template) && template[i:i+len(ph)] == ph {
			result += expansionRunID
			i += len(ph)
		} else {
			result += string(template[i])
			i++
		}
	}
	return result
}

// --- ArtifactCreated with placeholder ---

func TestEvaluate_ArtifactCreated_PlaceholderMatchesRealPath_Passes(t *testing.T) {
	// Declare an artifact path with the placeholder; the snapshot contains
	// the real path (placeholder expanded). Evaluate should pass the
	// assertion after expanding the declared path.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	observed := "Orchestration-" + expansionRunID + "/output.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{observed}
	ev.Definition.Assertions.ArtifactCreated = []string{declared}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, observed)
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared path with placeholder should match the real observed path", ar.Outcome)
	}
	if got.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS", got.Verdict)
	}
}

func TestEvaluate_ArtifactCreated_NoPlaceholder_ComparedLiterally_Passes(t *testing.T) {
	// A path without the placeholder must be compared as-is: no substitution.
	plain := "Research.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{plain}
	ev.Definition.Assertions.ArtifactCreated = []string{plain}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, plain)
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — literal path should match literally", ar.Outcome)
	}
}

func TestEvaluate_ArtifactCreated_PlaceholderWithAbsentFile_Fails(t *testing.T) {
	// Declare with placeholder, but the real file is absent. The expansion
	// should happen and the assertion should fail because the file is not
	// in the snapshot.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/missing.md"
	realFile := "Orchestration-" + expansionRunID + "/missing.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{"other.md"} // real file is absent
	ev.Definition.Assertions.ArtifactCreated = []string{declared}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, realFile)
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — %q is absent from the snapshot", ar.Outcome, realFile)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// --- ArtifactNotCreated with placeholder ---

func TestEvaluate_ArtifactNotCreated_PlaceholderAndFileAbsent_Passes(t *testing.T) {
	// Declare a not-created assertion with placeholder; the real path is
	// truly absent from the snapshot. Should pass after expansion.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/temp.md"
	realFile := "Orchestration-" + expansionRunID + "/temp.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{"Research.md"} // real file is absent
	ev.Definition.Assertions.ArtifactNotCreated = []string{declared}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, realFile)
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — %q is truly absent from the snapshot", ar.Outcome, realFile)
	}
}

func TestEvaluate_ArtifactNotCreated_PlaceholderAndFilePresent_Fails(t *testing.T) {
	// Declare a not-created assertion with placeholder; the real path is
	// present. Should fail after expansion.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/unexpected.md"
	realFile := "Orchestration-" + expansionRunID + "/unexpected.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{realFile} // unexpectedly present
	ev.Definition.Assertions.ArtifactNotCreated = []string{declared}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, realFile)
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — declared not created but %q is present", ar.Outcome, realFile)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

func TestEvaluate_ArtifactNotCreated_NoPlaceholder_ComparedLiterally(t *testing.T) {
	plain := "ephemeral.log"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{"Research.md"} // plain is absent
	ev.Definition.Assertions.ArtifactNotCreated = []string{plain}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassArtifactNotCreated, plain)
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — literal path absent from snapshot", ar.Outcome)
	}
}

// --- TaskMessage RequiredInputArtifacts with placeholder ---

func TestEvaluate_TaskMessage_RequiredInputArtifacts_PlaceholderMatchesRealPath_Passes(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/Plan.md"
	realInput := "Orchestration-" + expansionRunID + "/Plan.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{realInput}
	msg.OutputArtifacts = []string{}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			RequiredInputArtifacts: []string{declared},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared RequiredInputArtifacts with placeholder should match the real path", ar.Outcome)
	}
}

func TestEvaluate_TaskMessage_RequiredInputArtifacts_NoPlaceholder_ComparedLiterally_Passes(t *testing.T) {
	plain := "Requirements.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{plain}
	msg.OutputArtifacts = []string{}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			RequiredInputArtifacts: []string{plain},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — literal RequiredInputArtifacts path should match literally", ar.Outcome)
	}
}

// --- TaskMessage OptionalInputArtifacts with placeholder ---

func TestEvaluate_TaskMessage_OptionalInputArtifacts_PlaceholderMatchesRealPath_Passes(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/context.md"
	realInput := "Orchestration-" + expansionRunID + "/context.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{realInput}
	msg.OutputArtifacts = []string{}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			OptionalInputArtifacts: []string{declared},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — OptionalInputArtifacts with placeholder should match the real path", ar.Outcome)
	}
}

// --- TaskMessage RequiredOutputArtifacts with placeholder ---

func TestEvaluate_TaskMessage_RequiredOutputArtifacts_PlaceholderMatchesRealPath_Passes(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/result.md"
	realOutput := "Orchestration-" + expansionRunID + "/result.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{}
	msg.OutputArtifacts = []string{realOutput}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			RequiredOutputArtifacts: []string{declared},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — RequiredOutputArtifacts with placeholder should match the real path", ar.Outcome)
	}
}

func TestEvaluate_TaskMessage_RequiredOutputArtifacts_NoPlaceholder_ComparedLiterally_Passes(t *testing.T) {
	plain := "Research.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{}
	msg.OutputArtifacts = []string{plain}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			RequiredOutputArtifacts: []string{plain},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — literal RequiredOutputArtifacts path should match literally", ar.Outcome)
	}
}

// --- TaskMessage OptionalOutputArtifacts with placeholder ---

func TestEvaluate_TaskMessage_OptionalOutputArtifacts_PlaceholderMatchesRealPath_Passes(t *testing.T) {
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/notes.md"
	realOutput := "Orchestration-" + expansionRunID + "/notes.md"

	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{}
	msg.OutputArtifacts = []string{realOutput}
	ev := evidenceWithOneInvocationForExpansion(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			OptionalOutputArtifacts: []string{declared},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — OptionalOutputArtifacts with placeholder should match the real path", ar.Outcome)
	}
}

// --- Caller's evidence is not modified ---

func TestEvaluate_DoesNotModifyCaller_DeclaredPathsPreservedAfterEvaluate(t *testing.T) {
	// Evaluate takes ev by value and must not write through any slice header
	// it received. The caller's original declared paths must be unchanged.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	realFile := "Orchestration-" + expansionRunID + "/output.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{realFile}
	ev.Definition.Assertions.ArtifactCreated = []string{declared}

	_ = evaluate.Evaluate(ev)

	if ev.Definition.Assertions.ArtifactCreated[0] != declared {
		t.Errorf("caller's ArtifactCreated[0] was mutated: got %q, want %q (original declared path)",
			ev.Definition.Assertions.ArtifactCreated[0], declared)
	}
}

func TestEvaluate_ReEvaluatingSameEvidence_YieldsSameVerdict(t *testing.T) {
	// Verifies that evaluate is pure with respect to the caller's evidence:
	// evaluating the same evidence twice yields the same result, so a test
	// framework can safely re-evaluate stored evidence without re-running the
	// subject.
	declared := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	realFile := "Orchestration-" + expansionRunID + "/output.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{realFile}
	ev.Definition.Assertions.ArtifactCreated = []string{declared}

	first := evaluate.Evaluate(ev)
	second := evaluate.Evaluate(ev)

	if first.Verdict != second.Verdict {
		t.Errorf("first verdict %q != second verdict %q — Evaluate is not pure with respect to the caller's evidence", first.Verdict, second.Verdict)
	}
}

// --- Mixed: placeholder and literal paths in the same evidence ---

func TestEvaluate_MixedPlaceholderAndLiteralPaths_BothEvaluatedCorrectly(t *testing.T) {
	// One artifact uses the placeholder; another is a plain literal. Both
	// must evaluate correctly in the same Evaluate call.
	declaredWithPH := "Orchestration-" + domain.RunIDPlaceholder + "/output.md"
	realExpanded := "Orchestration-" + expansionRunID + "/output.md"
	plainPath := "Research.md"

	ev := baseEvidenceForExpansion()
	ev.SnapshotFiles = []string{realExpanded, plainPath}
	ev.Definition.Assertions.ArtifactCreated = []string{declaredWithPH, plainPath}

	got := evaluate.Evaluate(ev)

	arExpanded := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, realExpanded)
	if arExpanded.Outcome != domain.AssertionPass {
		t.Errorf("placeholder path: Outcome = %q, want pass", arExpanded.Outcome)
	}

	arPlain := findAssertion(t, got.Assertions, domain.ClassArtifactCreated, plainPath)
	if arPlain.Outcome != domain.AssertionPass {
		t.Errorf("literal path: Outcome = %q, want pass", arPlain.Outcome)
	}
}
