package evaluate_test

// Tests for the subject-never-started diagnosis path in evaluate.Evaluate.
//
// When the subject process refused to start (DispositionSpawnFailed), the
// evaluator must:
//   - raise ConditionSubjectNeverStarted with the real one-line cause from stderr
//   - report the run as ReasonInfrastructure / VerdictFail
//   - set every declared assertion to AssertionNotEvaluated (not AssertionFail)
//   - never produce an empty condition detail
//
// A contrast case pins that a subject that genuinely ran and failed its
// assertions still reports its full per-assertion diagnostics unchanged —
// the regression guard against the new path swallowing real verdicts.

import (
	"fmt"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// spawnFailedEvidence builds run evidence for a subject process that refused
// to start, carrying the given stderr content and exit code. All other fields
// are taken from baseEvidence so only the disposition-related change is visible
// to each test.
func spawnFailedEvidence(stderr string, exitCode int) domain.RunEvidence {
	ev := baseEvidence()
	ev.SubjectResult = domain.SubjectResult{
		Disposition: domain.DispositionSpawnFailed,
		ExitCode:    exitCode,
		Stderr:      stderr,
	}
	return ev
}

func TestEvaluate_SpawnFailed_RaisesConditionSubjectNeverStarted(t *testing.T) {
	ev := spawnFailedEvidence("Authentication error: not logged in", 1)

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionSubjectNeverStarted) {
		t.Errorf("Conditions = %+v, want ConditionSubjectNeverStarted when disposition is DispositionSpawnFailed — the reader must see the real cause, not a page of assertion diagnostics for assertions nothing could have satisfied", got.Conditions)
	}
}

func TestEvaluate_SpawnFailed_ConditionDetailIsFirstNonEmptyStderrLine(t *testing.T) {
	const oneLine = "Authentication error: not logged in"
	ev := spawnFailedEvidence(oneLine, 1)

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionSubjectNeverStarted)
	if !strings.Contains(cond.Detail, oneLine) {
		t.Errorf("ConditionSubjectNeverStarted.Detail = %q, want it to contain the one-line cause %q from stderr — the real cause must be what the reader sees first", cond.Detail, oneLine)
	}
}

func TestEvaluate_SpawnFailed_MultiLineStderr_DetailIsFirstNonEmptyLine(t *testing.T) {
	// Stderr may begin with blank lines; the first non-empty line is the cause.
	const firstNonEmpty = "Authentication error: not logged in"
	ev := spawnFailedEvidence("\n  \n"+firstNonEmpty+"\nmore lines here", 1)

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionSubjectNeverStarted)
	if !strings.Contains(cond.Detail, firstNonEmpty) {
		t.Errorf("ConditionSubjectNeverStarted.Detail = %q, want the first non-empty stderr line %q — not a blank line or a later line", cond.Detail, firstNonEmpty)
	}
	if strings.Contains(cond.Detail, "more lines") {
		t.Errorf("ConditionSubjectNeverStarted.Detail = %q, must contain only the first non-empty stderr line, not subsequent content", cond.Detail)
	}
}

func TestEvaluate_SpawnFailed_EmptyStderr_DetailNamesExitCode(t *testing.T) {
	const exitCode = 127
	ev := spawnFailedEvidence("", exitCode)

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionSubjectNeverStarted)
	if !strings.Contains(cond.Detail, fmt.Sprintf("%d", exitCode)) {
		t.Errorf("ConditionSubjectNeverStarted.Detail = %q, want it to name the exit code %d when stderr yields no non-empty line — the detail must never be the empty string", cond.Detail, exitCode)
	}
}

func TestEvaluate_SpawnFailed_BlankOnlyStderr_DetailIsNeverEmpty(t *testing.T) {
	// Blank-only stderr must also produce a non-empty detail via the exit
	// code fallback, carrying the same "never the empty string" obligation
	// ConditionRunNotStarted already carries.
	ev := spawnFailedEvidence("   \n  \n", 2)

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionSubjectNeverStarted)
	if cond.Detail == "" {
		t.Error("ConditionSubjectNeverStarted.Detail is empty; detail must never be the empty string — an empty detail is indistinguishable from an absent one in a rendered report")
	}
}

func TestEvaluate_SpawnFailed_VerdictIsFailWithReasonInfrastructureOnly(t *testing.T) {
	ev := spawnFailedEvidence("Error: API key invalid", 1)

	got := evaluate.Evaluate(ev)

	if got.Verdict != domain.VerdictFail {
		t.Errorf("Verdict = %q, want FAIL for a subject that never started", got.Verdict)
	}
	if !hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, want ReasonInfrastructure — a subject that never started is a fact about the environment, not a subject regression", got.Reasons)
	}
	if hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, must not contain ReasonAssertion — no assertion could have been evaluated when the subject never started; asserting failure is misleading", got.Reasons)
	}
}

func TestEvaluate_SpawnFailed_AllDeclaredAssertionsAreNotEvaluated(t *testing.T) {
	// Declare an assertion that would fail under normal evaluation (the base
	// evidence has LastStatus "SUCCESS"; we assert "EXPECTED_BUT_NEVER") so we
	// can verify the spawn-failed path overrides the outcome to
	// AssertionNotEvaluated rather than leaving it as AssertionFail.
	ev := spawnFailedEvidence("Error: not logged in", 1)
	ev.Definition.Assertions.FinalStatus = strp("EXPECTED_BUT_NEVER")

	got := evaluate.Evaluate(ev)

	for _, ar := range got.Assertions {
		if ar.Outcome == domain.AssertionFail {
			t.Errorf("AssertionResult{Class:%q, Outcome:%q}: a subject that never started cannot have failed an assertion — outcome must be AssertionNotEvaluated, not AssertionFail", ar.Class, ar.Outcome)
		}
	}
	// The set must be non-empty: not-evaluated is not the same as absent.
	if len(got.Assertions) == 0 {
		t.Error("Assertions is empty; a declared assertion must appear as AssertionNotEvaluated, not be silently dropped — an absent assertion looks the same as a passing one to a reader")
	}
}

func TestEvaluate_SpawnFailed_NotEvaluatedAssertionDetailMatchesConditionDetail(t *testing.T) {
	// The not-evaluated assertions carry the same one-line cause as the
	// condition, so the reader can see why each assertion was skipped without
	// having to cross-reference the condition block.
	const stderrLine = "Error: not logged in"
	ev := spawnFailedEvidence(stderrLine, 1)
	ev.Definition.Assertions.FinalStatus = strp("EXPECTED_BUT_NEVER")

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionSubjectNeverStarted)
	for _, ar := range got.Assertions {
		if ar.Outcome != domain.AssertionNotEvaluated {
			continue
		}
		if !strings.Contains(ar.Detail, cond.Detail) {
			t.Errorf("AssertionResult{Class:%q}.Detail = %q, want it to carry the same one-line cause %q that ConditionSubjectNeverStarted carries", ar.Class, ar.Detail, cond.Detail)
		}
	}
}

// --- Contrast cases: a subject that genuinely ran and failed must report in full ---

func TestEvaluate_GenuineAssertionFailure_ConditionSubjectNeverStartedAbsent(t *testing.T) {
	// A subject that completed normally but failed an assertion must not raise
	// ConditionSubjectNeverStarted — that condition is reserved for spawn failures.
	ev := baseEvidence()
	ev.SubjectResult.Disposition = domain.DispositionCompleted
	ev.Definition.Assertions.FinalStatus = strp("SUCCESS")
	ev.Orchestration.LastStatus = "FAILURE" // causes FinalStatus assertion to fail

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionSubjectNeverStarted) {
		t.Errorf("Conditions = %+v, must not raise ConditionSubjectNeverStarted for a subject that completed normally — raising it here would suppress real assertion diagnostics", got.Conditions)
	}
}

func TestEvaluate_GenuineAssertionFailure_AssertionsRetainFullDiagnostics(t *testing.T) {
	// The core regression guard: a subject that ran and failed its assertions
	// must still produce AssertionFail results with their per-assertion detail.
	// The new spawn-failed path must never swallow real verdicts.
	ev := baseEvidence()
	ev.SubjectResult.Disposition = domain.DispositionCompleted
	ev.Definition.Assertions.FinalPhase = strp("complete")
	ev.Orchestration.Phase = "in_progress" // makes FinalPhase assertion fail

	got := evaluate.Evaluate(ev)

	foundFail := false
	for _, ar := range got.Assertions {
		if ar.Outcome == domain.AssertionFail {
			foundFail = true
		}
	}
	if !foundFail {
		t.Errorf("Assertions = %+v, want at least one AssertionFail for a subject that ran and failed — full per-assertion diagnostics must be preserved, not swallowed by the spawn-failed path", got.Assertions)
	}
	if hasReason(got.Reasons, domain.ReasonInfrastructure) {
		t.Errorf("Reasons = %+v, must not contain ReasonInfrastructure for a subject that genuinely ran and failed — an assertion failure is a subject regression, not an infrastructure fault", got.Reasons)
	}
	if !hasReason(got.Reasons, domain.ReasonAssertion) {
		t.Errorf("Reasons = %+v, want ReasonAssertion for a subject that ran and failed its assertions", got.Reasons)
	}
}
