package evaluate_test

// Tests for the per-field split behavior of task-message assertions
// (TDD RED phase). Each test specifies expected behavior of the refactored
// evaluateTaskMessage which returns one AssertionResult per declared sub-field
// rather than a single combined result.
//
// Target convention used throughout: "<seq>.<subfield>", e.g. "1.identity",
// "1.human_in_the_loop", "1.input_artifacts", "1.output_artifacts",
// "1.task_description".
//
// For the no-invocation-found early-exit path, a single result is produced
// (no per-field split is possible without an invocation to inspect), and its
// Target is the bare sequence number, e.g. "2".

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// degradedStartRecord builds a start record whose message extraction quality
// is set to ExtractionDegraded, bypassing the ExtractionParsed override in
// startRecord. Used by tests that verify the degraded-extraction early-exit
// path produces per-field evidence.
func degradedStartRecord(seq, ordinal int, id domain.CollaboratorIdentity, at time.Time, msg domain.TaskMessage, quality domain.ExtractionQuality) domain.LogRecord {
	msg.Extraction = quality
	return domain.LogRecord{
		Kind:      domain.RecordStart,
		Seq:       seq,
		Ordinal:   ordinal,
		Identity:  id,
		Timestamp: at,
		Outcome:   domain.OutcomeRewritePrompt,
		Message:   &msg,
	}
}

// evidenceWithDegradedExtraction returns RunEvidence whose single invocation
// at seq 1 carries the given extraction quality (Degraded or Failed).
func evidenceWithDegradedExtraction(quality domain.ExtractionQuality) domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		degradedStartRecord(1, 1, researcher(), start, plainMessage("researcher#1"), quality),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	return ev
}

// countTaskMessageResults counts how many AssertionResults in results have
// ClassTaskMessage, scoped optionally to a specific sequence number prefix
// (e.g. "1.") — pass an empty prefix to count all.
func countTaskMessageResults(results []domain.AssertionResult, seqPrefix string) int {
	n := 0
	for _, r := range results {
		if r.Class != domain.ClassTaskMessage {
			continue
		}
		if seqPrefix == "" || len(r.Target) >= len(seqPrefix) && r.Target[:len(seqPrefix)] == seqPrefix {
			n++
		}
	}
	return n
}

// ---- T2.1: Per-field split behavior ----------------------------------------

// TestEvaluate_TaskMessage_PerField_MultipleFieldsAllPass verifies that when
// three sub-fields are declared (identity, human_in_the_loop,
// required_input_artifacts) and all pass, three separate AssertionResults are
// produced, each identified by a Target of the form "<seq>.<subfield>".
func TestEvaluate_TaskMessage_PerField_MultipleFieldsAllPass(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = true
	msg.InputArtifacts = []string{"Requirements.md"}
	ev := evidenceWithOneInvocation(msg)

	id := researcher()
	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			Identity:               &id,
			HumanInTheLoop:         &hitl,
			RequiredInputArtifacts: []string{"Requirements.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	identityAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if identityAR.Outcome != domain.AssertionPass {
		t.Errorf("identity: Outcome = %q, want pass — identity matches declared value", identityAR.Outcome)
	}

	hitlAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if hitlAR.Outcome != domain.AssertionPass {
		t.Errorf("human_in_the_loop: Outcome = %q, want pass — both declared and observed true", hitlAR.Outcome)
	}

	inputAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if inputAR.Outcome != domain.AssertionPass {
		t.Errorf("input_artifacts: Outcome = %q, want pass — required artifact is present", inputAR.Outcome)
	}
}

// TestEvaluate_TaskMessage_PerField_MultipleFieldsSomePassSomeFail verifies
// that when three sub-fields are declared and one fails (human_in_the_loop
// mismatch), the failing field's result is AssertionFail while the passing
// fields remain AssertionPass — each in its own separate result.
func TestEvaluate_TaskMessage_PerField_MultipleFieldsSomePassSomeFail(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = false // declared true below — will fail
	msg.InputArtifacts = []string{"Requirements.md"}
	ev := evidenceWithOneInvocation(msg)

	id := researcher()
	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			Identity:               &id,
			HumanInTheLoop:         &hitl,
			RequiredInputArtifacts: []string{"Requirements.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	identityAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if identityAR.Outcome != domain.AssertionPass {
		t.Errorf("identity: Outcome = %q, want pass — identity is correct", identityAR.Outcome)
	}

	hitlAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if hitlAR.Outcome != domain.AssertionFail {
		t.Errorf("human_in_the_loop: Outcome = %q, want fail — declared true, observed false", hitlAR.Outcome)
	}

	inputAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if inputAR.Outcome != domain.AssertionPass {
		t.Errorf("input_artifacts: Outcome = %q, want pass — required artifact is present", inputAR.Outcome)
	}
}

// TestEvaluate_TaskMessage_PerField_SingleField_ExactlyOneResult verifies
// that declaring a single sub-field produces exactly one ClassTaskMessage
// AssertionResult — not zero, not more than one.
func TestEvaluate_TaskMessage_PerField_SingleField_ExactlyOneResult(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = false
	ev := evidenceWithOneInvocation(msg)

	hitl := false
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, HumanInTheLoop: &hitl},
	}

	got := evaluate.Evaluate(ev)

	n := countTaskMessageResults(got.Assertions, "1.")
	if n != 1 {
		t.Errorf("got %d ClassTaskMessage results with prefix \"1.\", want exactly 1 — only human_in_the_loop was declared", n)
	}

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared and observed both false", ar.Outcome)
	}
}

// ---- T2.2: Expected/Actual evidence on each sub-field type -----------------

// TestEvaluate_TaskMessage_PerField_Identity_Pass_HasEvidence verifies that
// a passing identity check carries non-empty Expected and Actual strings.
func TestEvaluate_TaskMessage_PerField_Identity_Pass_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	ev := evidenceWithOneInvocation(msg)

	id := researcher()
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, Identity: &id},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on pass; identity assertion must carry expected identity key")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on pass; identity assertion must carry observed identity key")
	}
}

// TestEvaluate_TaskMessage_PerField_Identity_Fail_HasEvidence verifies that
// a failing identity check carries non-empty Expected and Actual strings
// scoped to the identity mismatch.
func TestEvaluate_TaskMessage_PerField_Identity_Fail_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	ev := evidenceWithOneInvocation(msg)

	wrongID := reviewer()
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, Identity: &wrongID},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — invocation 1 carries researcher identity, not reviewer", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on fail; must carry the declared identity key")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on fail; must carry the observed identity key")
	}
}

// TestEvaluate_TaskMessage_PerField_InputArtifacts_Pass_HasEvidence verifies
// that a passing input-artifact check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_InputArtifacts_Pass_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{"Requirements.md"}
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, RequiredInputArtifacts: []string{"Requirements.md"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — required artifact is present", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on pass; input_artifacts assertion must carry declared set")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on pass; input_artifacts assertion must carry observed set")
	}
}

// TestEvaluate_TaskMessage_PerField_InputArtifacts_Fail_HasEvidence verifies
// that a failing input-artifact check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_InputArtifacts_Fail_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{}
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, RequiredInputArtifacts: []string{"Requirements.md"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — required artifact is missing", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on fail; must carry the declared required input artifact set")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on fail; must carry the observed input artifact set")
	}
}

// TestEvaluate_TaskMessage_PerField_OutputArtifacts_Pass_HasEvidence verifies
// that a passing output-artifact check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_OutputArtifacts_Pass_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.OutputArtifacts = []string{"Research.md"}
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, RequiredOutputArtifacts: []string{"Research.md"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — required output artifact is present", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on pass; output_artifacts assertion must carry declared set")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on pass; output_artifacts assertion must carry observed set")
	}
}

// TestEvaluate_TaskMessage_PerField_OutputArtifacts_Fail_HasEvidence verifies
// that a failing output-artifact check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_OutputArtifacts_Fail_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.OutputArtifacts = []string{}
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, RequiredOutputArtifacts: []string{"Research.md"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — required output artifact is missing", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on fail; must carry the declared required output artifact set")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on fail; must carry the observed output artifact set")
	}
}

// TestEvaluate_TaskMessage_PerField_HITL_Pass_HasEvidence verifies that a
// passing human_in_the_loop check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_HITL_Pass_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = true
	ev := evidenceWithOneInvocation(msg)

	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, HumanInTheLoop: &hitl},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared and observed both true", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on pass; human_in_the_loop assertion must carry declared value")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on pass; human_in_the_loop assertion must carry observed value")
	}
}

// TestEvaluate_TaskMessage_PerField_HITL_Fail_HasEvidence verifies that a
// failing human_in_the_loop check carries non-empty Expected and Actual.
func TestEvaluate_TaskMessage_PerField_HITL_Fail_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = false
	ev := evidenceWithOneInvocation(msg)

	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, HumanInTheLoop: &hitl},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — declared true, observed false", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on fail; must carry declared human_in_the_loop value")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on fail; must carry observed human_in_the_loop value")
	}
}

// TestEvaluate_TaskMessage_PerField_TaskDescription_Pass_HasEvidence verifies
// that a passing task_description_contains check carries non-empty Expected
// and Actual.
func TestEvaluate_TaskMessage_PerField_TaskDescription_Pass_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Research the topic and write findings."
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"Research"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — task description contains declared substring", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on pass; task_description assertion must carry declared substring(s)")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on pass; task_description assertion must carry observed description content")
	}
}

// TestEvaluate_TaskMessage_PerField_TaskDescription_Fail_HasEvidence verifies
// that a failing task_description_contains check carries non-empty Expected
// and Actual.
func TestEvaluate_TaskMessage_PerField_TaskDescription_Fail_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Write the design document."
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"research"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — task description does not contain declared substring", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty on fail; must carry declared substring(s)")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty on fail; must carry observed task description content")
	}
}

// ---- T2.3: Degraded/failed extraction early-exit path ----------------------

// TestEvaluate_TaskMessage_PerField_DegradedExtraction_ProducesPerFieldResults
// verifies that when a message has ExtractionDegraded quality and two
// sub-fields are declared (identity and human_in_the_loop), two AssertionFail
// results are produced — one per declared field — each with non-empty
// Expected and Actual evidence.
func TestEvaluate_TaskMessage_PerField_DegradedExtraction_ProducesPerFieldResults(t *testing.T) {
	ev := evidenceWithDegradedExtraction(domain.ExtractionDegraded)

	id := researcher()
	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:             1,
			Identity:       &id,
			HumanInTheLoop: &hitl,
		},
	}

	got := evaluate.Evaluate(ev)

	identityAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if identityAR.Outcome != domain.AssertionFail {
		t.Errorf("identity: Outcome = %q, want fail — extraction degraded, assertions must not pass", identityAR.Outcome)
	}
	if identityAR.Expected == "" {
		t.Error("identity: Expected is empty; degraded-extraction result must carry expected value")
	}
	if identityAR.Actual == "" {
		t.Error("identity: Actual is empty; degraded-extraction result must carry extraction status as evidence")
	}

	hitlAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if hitlAR.Outcome != domain.AssertionFail {
		t.Errorf("human_in_the_loop: Outcome = %q, want fail — extraction degraded, assertions must not pass", hitlAR.Outcome)
	}
	if hitlAR.Expected == "" {
		t.Error("human_in_the_loop: Expected is empty; degraded-extraction result must carry expected value")
	}
	if hitlAR.Actual == "" {
		t.Error("human_in_the_loop: Actual is empty; degraded-extraction result must carry extraction status as evidence")
	}
}

// TestEvaluate_TaskMessage_PerField_FailedExtraction_ProducesPerFieldResults
// verifies that when a message has ExtractionFailed quality and two sub-fields
// are declared, two AssertionFail results are produced with non-empty evidence.
func TestEvaluate_TaskMessage_PerField_FailedExtraction_ProducesPerFieldResults(t *testing.T) {
	ev := evidenceWithDegradedExtraction(domain.ExtractionFailed)

	id := researcher()
	hitl := false
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:             1,
			Identity:       &id,
			HumanInTheLoop: &hitl,
		},
	}

	got := evaluate.Evaluate(ev)

	identityAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if identityAR.Outcome != domain.AssertionFail {
		t.Errorf("identity: Outcome = %q, want fail — extraction failed", identityAR.Outcome)
	}
	if identityAR.Expected == "" {
		t.Error("identity: Expected is empty; failed-extraction result must carry expected value")
	}
	if identityAR.Actual == "" {
		t.Error("identity: Actual is empty; failed-extraction result must carry extraction status as evidence")
	}

	hitlAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if hitlAR.Outcome != domain.AssertionFail {
		t.Errorf("human_in_the_loop: Outcome = %q, want fail — extraction failed", hitlAR.Outcome)
	}
	if hitlAR.Expected == "" {
		t.Error("human_in_the_loop: Expected is empty; failed-extraction result must carry expected value")
	}
	if hitlAR.Actual == "" {
		t.Error("human_in_the_loop: Actual is empty; failed-extraction result must carry extraction status as evidence")
	}
}

// ---- T2.4: Undeclared sub-fields produce no results ------------------------

// TestEvaluate_TaskMessage_PerField_UndeclaredFieldsProduceNoResults verifies
// that a task_message assertion declaring only human_in_the_loop and identity
// produces no AssertionResult entries for input_artifacts, output_artifacts,
// or task_description.
func TestEvaluate_TaskMessage_PerField_UndeclaredFieldsProduceNoResults(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = true
	ev := evidenceWithOneInvocation(msg)

	id := researcher()
	hitl := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:             1,
			Identity:       &id,
			HumanInTheLoop: &hitl,
			// RequiredInputArtifacts, RequiredOutputArtifacts, and
			// TaskDescriptionContains are intentionally not declared.
		},
	}

	got := evaluate.Evaluate(ev)

	// Verify the declared fields produce results (sanity check).
	findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")

	// Verify undeclared fields produce no results.
	for _, r := range got.Assertions {
		if r.Class != domain.ClassTaskMessage {
			continue
		}
		switch r.Target {
		case "1.input_artifacts":
			t.Errorf("unexpected AssertionResult for undeclared input_artifacts sub-field")
		case "1.output_artifacts":
			t.Errorf("unexpected AssertionResult for undeclared output_artifacts sub-field")
		case "1.task_description":
			t.Errorf("unexpected AssertionResult for undeclared task_description sub-field")
		}
	}

	// Total count for this assertion must be exactly 2.
	n := countTaskMessageResults(got.Assertions, "1.")
	if n != 2 {
		t.Errorf("got %d ClassTaskMessage results for seq 1, want exactly 2 (identity + human_in_the_loop)", n)
	}
}

// ---- T2.5: No-invocation-found early-exit path -----------------------------

// TestEvaluate_TaskMessage_PerField_NoInvocation_HasEvidence verifies that
// when the declared sequence number has no corresponding start record, the
// result carries non-empty Expected and Actual evidence — not just a Detail
// string — so a report can explain the failure without re-reading the log.
func TestEvaluate_TaskMessage_PerField_NoInvocation_HasEvidence(t *testing.T) {
	// evidenceWithOneInvocation gives us only seq 1; we assert on seq 2.
	msg := plainMessage("researcher#1")
	ev := evidenceWithOneInvocation(msg)

	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 2},
	}

	got := evaluate.Evaluate(ev)

	// The no-invocation path produces a single result (no per-field split
	// is possible without an invocation to inspect). Its Target is the bare
	// sequence number.
	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "2")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — no invocation at seq 2 was recorded", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty; no-invocation result must describe what was expected to be found")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty; no-invocation result must describe what was observed (no invocation at that sequence)")
	}
}

// TestEvaluate_TaskMessage_PerField_NoInvocation_WithIdentityDeclared_HasEvidence
// verifies that the no-invocation-found path produces a single result with
// evidence even when specific sub-fields (identity) are declared — because no
// per-field split can be performed without a start record to inspect.
func TestEvaluate_TaskMessage_PerField_NoInvocation_WithIdentityDeclared_HasEvidence(t *testing.T) {
	msg := plainMessage("researcher#1")
	ev := evidenceWithOneInvocation(msg)

	id := researcher()
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 2, Identity: &id},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "2")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — no invocation at seq 2", ar.Outcome)
	}
	if ar.Expected == "" {
		t.Error("Expected is empty; must describe what invocation was expected")
	}
	if ar.Actual == "" {
		t.Error("Actual is empty; must state that no invocation was observed at sequence 2")
	}
}
