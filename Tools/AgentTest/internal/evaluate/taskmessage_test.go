package evaluate_test

// Tests for the per-invocation task-message assertion (ClassTaskMessage):
// optional-versus-required artifact set semantics, the human-in-the-loop
// flag, task-description substring matching, and identity drift detection.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

func evidenceWithOneInvocation(msg domain.TaskMessage) domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	ev := baseEvidence()
	ev.Records = []domain.LogRecord{
		startRecord(1, 1, researcher(), "", start, msg),
		endRecord(1, 1, researcher(), start.Add(time.Second)),
	}
	return ev
}

func TestEvaluate_TaskMessage_RequiredArtifactsAllPresent_Passes(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{"Requirements.md"}
	msg.OutputArtifacts = []string{"Research.md"}
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			RequiredInputArtifacts:  []string{"Requirements.md"},
			RequiredOutputArtifacts: []string{"Research.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	// Both input_artifacts and output_artifacts are declared; check each.
	inputAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if inputAR.Outcome != domain.AssertionPass {
		t.Errorf("input_artifacts Outcome = %q, want pass — every required input artifact is present", inputAR.Outcome)
	}
	outputAR := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if outputAR.Outcome != domain.AssertionPass {
		t.Errorf("output_artifacts Outcome = %q, want pass — every required output artifact is present", outputAR.Outcome)
	}
}

func TestEvaluate_TaskMessage_RequiredArtifactMissing_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.InputArtifacts = []string{}
	msg.OutputArtifacts = []string{"Research.md"}
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			RequiredInputArtifacts: []string{"Requirements.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — Requirements.md was declared required but never sent", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

func TestEvaluate_TaskMessage_OptionalArtifactMayOrMayNotBePresent_BothPass(t *testing.T) {
	withOptional := plainMessage("researcher#1")
	withOptional.InputArtifacts = []string{"Requirements.md", "Research.md"}
	withoutOptional := plainMessage("researcher#1")
	withoutOptional.InputArtifacts = []string{"Requirements.md"}

	for name, msg := range map[string]domain.TaskMessage{"with optional": withOptional, "without optional": withoutOptional} {
		ev := evidenceWithOneInvocation(msg)
		ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
			{
				At:                     1,
				RequiredInputArtifacts: []string{"Requirements.md"},
				OptionalInputArtifacts: []string{"Research.md"},
			},
		}

		got := evaluate.Evaluate(ev)

		ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
		if ar.Outcome != domain.AssertionPass {
			t.Errorf("%s: Outcome = %q, want pass — an optional entry may or may not be present", name, ar.Outcome)
		}
	}
}

func TestEvaluate_TaskMessage_ArtifactInNeitherSet_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	// SystemDesign.md is present but declared neither required nor optional.
	msg.InputArtifacts = []string{"Requirements.md", "SystemDesign.md"}
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                     1,
			RequiredInputArtifacts: []string{"Requirements.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.input_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — SystemDesign.md is in neither the required nor the optional set", ar.Outcome)
	}
}

// TestEvaluate_TaskMessage_RequiredOutputArtifactMissing_Fails is the
// output-side counterpart of the required-input case above: if the
// implementation handles input and output artifacts via separate code
// paths, a bug unique to the output path must be caught here.
func TestEvaluate_TaskMessage_RequiredOutputArtifactMissing_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.OutputArtifacts = []string{}
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			RequiredOutputArtifacts: []string{"Research.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — Research.md was declared a required output but never produced", ar.Outcome)
	}
	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL", got.Verdict)
	}
}

// TestEvaluate_TaskMessage_OptionalOutputArtifactMayOrMayNotBePresent_BothPass
// is the output-side counterpart of the optional-input case above.
func TestEvaluate_TaskMessage_OptionalOutputArtifactMayOrMayNotBePresent_BothPass(t *testing.T) {
	withOptional := plainMessage("researcher#1")
	withOptional.OutputArtifacts = []string{"Research.md", "Notes.md"}
	withoutOptional := plainMessage("researcher#1")
	withoutOptional.OutputArtifacts = []string{"Research.md"}

	for name, msg := range map[string]domain.TaskMessage{"with optional": withOptional, "without optional": withoutOptional} {
		ev := evidenceWithOneInvocation(msg)
		ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
			{
				At:                      1,
				RequiredOutputArtifacts: []string{"Research.md"},
				OptionalOutputArtifacts: []string{"Notes.md"},
			},
		}

		got := evaluate.Evaluate(ev)

		ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
		if ar.Outcome != domain.AssertionPass {
			t.Errorf("%s: Outcome = %q, want pass — an optional output entry may or may not be present", name, ar.Outcome)
		}
	}
}

// TestEvaluate_TaskMessage_OutputArtifactInNeitherSet_Fails is the
// output-side counterpart of TestEvaluate_TaskMessage_ArtifactInNeitherSet_Fails.
func TestEvaluate_TaskMessage_OutputArtifactInNeitherSet_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	// scratch.tmp is present but declared neither required nor optional.
	msg.OutputArtifacts = []string{"Research.md", "scratch.tmp"}
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{
			At:                      1,
			RequiredOutputArtifacts: []string{"Research.md"},
		},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.output_artifacts")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — scratch.tmp is in neither the required nor the optional output set", ar.Outcome)
	}
}

func TestEvaluate_TaskMessage_HumanInTheLoop_MismatchFails(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = false
	ev := evidenceWithOneInvocation(msg)
	want := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, HumanInTheLoop: &want},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — declared human_in_the_loop true, observed false", ar.Outcome)
	}
}

func TestEvaluate_TaskMessage_HumanInTheLoop_MatchPasses(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.HumanInTheLoop = true
	ev := evidenceWithOneInvocation(msg)
	want := true
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, HumanInTheLoop: &want},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.human_in_the_loop")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — declared and observed human_in_the_loop both true", ar.Outcome)
	}
}

func TestEvaluate_TaskMessage_TaskDescriptionSubstring_AbsentFails(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Write the design document."
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"research"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — %q does not contain %q", ar.Outcome, msg.TaskDescription, "research")
	}
}

func TestEvaluate_TaskMessage_TaskDescriptionSubstring_PresentPasses(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Research the topic and write findings."
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"Research"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — the task description contains the declared substring", ar.Outcome)
	}
}

// TestEvaluate_TaskMessage_TaskDescriptionMultipleSubstrings_AllPresent_Passes
// exercises a multi-element TaskDescriptionContains, not just the
// single-element case above — a bug that only checked the first element
// would pass every other test in this file.
func TestEvaluate_TaskMessage_TaskDescriptionMultipleSubstrings_AllPresent_Passes(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Research the topic and write findings."
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"Research", "findings"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionPass {
		t.Errorf("Outcome = %q, want pass — the task description contains every declared substring", ar.Outcome)
	}
}

// TestEvaluate_TaskMessage_TaskDescriptionMultipleSubstrings_OneMissing_Fails
// declares a first substring that is present and a second that is not, so a
// bug that only checks the first element would wrongly pass.
func TestEvaluate_TaskMessage_TaskDescriptionMultipleSubstrings_OneMissing_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	msg.TaskDescription = "Research the topic and write findings."
	ev := evidenceWithOneInvocation(msg)
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, TaskDescriptionContains: []string{"Research", "recommendations"}},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.task_description")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — %q is declared but absent from the task description", ar.Outcome, "recommendations")
	}
}

// TestEvaluate_TaskMessage_IdentityDrift_ReportsAsDriftNotAsAMismatchedMessage
// covers the plan's requirement that a sequence drift is distinguishable
// from an ordinary message mismatch: the assertion targets seq 1 but
// declares an identity that seq 1 does not carry.
func TestEvaluate_TaskMessage_IdentityDrift_Fails(t *testing.T) {
	msg := plainMessage("researcher#1")
	ev := evidenceWithOneInvocation(msg)
	wrongIdentity := reviewer()
	ev.Definition.Assertions.TaskMessages = []domain.TaskMessageAssertion{
		{At: 1, Identity: &wrongIdentity},
	}

	got := evaluate.Evaluate(ev)

	ar := findAssertion(t, got.Assertions, domain.ClassTaskMessage, "1.identity")
	if ar.Outcome != domain.AssertionFail {
		t.Errorf("Outcome = %q, want fail — invocation 1 is the researcher, not the reviewer", ar.Outcome)
	}
	if ar.Detail == "" {
		t.Error("Detail is empty; a drifted identity should be explained without re-reading the log")
	}
}
