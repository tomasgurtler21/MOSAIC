package report_test

// Tests for both report rendering surfaces when the subject process refused
// to start. Both the text and JSON renderings must:
//   - name the real one-line cause from ConditionSubjectNeverStarted.Detail
//   - not present the run as an ordinary assertion failure with per-assertion
//     "expected ... got ..." diagnostics
//
// A contrast case pins that a subject that genuinely ran and failed its
// assertions still produces its full per-assertion diagnostics in both
// renderings unchanged — the regression guard against the new path
// swallowing real verdicts in the presentation layer.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

const neverStartedCause = "Authentication error: not logged in. Run `claude login` to authenticate."

// neverStartedRunReport is one run whose subject refused to start: it carries
// ConditionSubjectNeverStarted with the one-line cause, and every declared
// assertion has AssertionNotEvaluated with the same cause — exactly the model
// evaluate.Evaluate will produce for a DispositionSpawnFailed result.
func neverStartedRunReport() report.RunReport {
	return report.RunReport{
		Key:     domain.RunKey{RunID: "never-started-1", TestID: "subject-never-started", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionSubjectNeverStarted, Detail: neverStartedCause},
		},
		Assertions: []domain.AssertionResult{
			{
				Class:   domain.ClassFinalStatus,
				Outcome: domain.AssertionNotEvaluated,
				Detail:  neverStartedCause,
			},
		},
	}
}

// neverStartedResult wraps neverStartedRunReport in a minimal Result so the
// render helpers can operate on it.
func neverStartedResult() report.Result {
	run := neverStartedRunReport()
	return report.Build("never-started-suite", fixtureStarted(), fixtureFinished(), []report.TestReport{
		{
			TestID: "subject-never-started",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestID:  "subject-never-started",
				Verdict: domain.VerdictFail,
				Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
				Counted: 1,
			},
			Runs: []report.RunReport{run},
		},
	})
}

// genuineAssertionFailureResult builds a Result whose run carries a genuine
// AssertionFail with expected and actual values — the contrast case model.
func genuineAssertionFailureResult() report.Result {
	return report.Build("genuine-failure-suite", fixtureStarted(), fixtureFinished(), []report.TestReport{
		{
			TestID: "genuine-failure",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestID:  "genuine-failure",
				Verdict: domain.VerdictFail,
				Reasons: []domain.FailureReason{domain.ReasonAssertion},
				Counted: 1,
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "genuine-failure-1", TestID: "genuine-failure", RunNumber: 1},
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion},
					Assertions: []domain.AssertionResult{
						{
							Class:    domain.ClassFinalStatus,
							Outcome:  domain.AssertionFail,
							Expected: "SUCCESS",
							Actual:   "FAILURE",
							Detail:   `final_state.last_status "FAILURE" does not equal declared "SUCCESS"`,
						},
					},
				},
			},
		},
	})
}

// --- Text surface: never-started case ---

func TestRenderText_SubjectNeverStarted_ConditionKindAndDetailVisible(t *testing.T) {
	// The text report must name the never-started condition kind and its
	// one-line cause, so a reader can diagnose the problem without
	// reproducing it outside the tool.
	out, err := renderText(t, neverStartedResult())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if !strings.Contains(out, string(domain.ConditionSubjectNeverStarted)) {
		t.Errorf("text report does not mention the condition kind %q; the reader must be able to identify that this is a never-started failure, not an ordinary assertion failure.\ngot:\n%s", domain.ConditionSubjectNeverStarted, out)
	}
	if !strings.Contains(out, neverStartedCause) {
		t.Errorf("text report does not contain the one-line cause %q; the real cause from the subject's stderr must be what the reader sees first.\ngot:\n%s", neverStartedCause, out)
	}
}

func TestRenderText_SubjectNeverStarted_NoAssertionFailurePhrasing(t *testing.T) {
	// A run whose subject never started must not present as an ordinary
	// assertion failure. The "expected X, got Y" phrasing is reserved for
	// assertions that were actually evaluated against evidence; framing a
	// never-started run in that phrasing is misleading.
	out, err := renderText(t, neverStartedResult())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if strings.Contains(out, "expected \"") {
		t.Errorf("text report uses assertion-failure phrasing (\"expected ...\") for a run whose subject never started; that phrasing must not appear when no assertion was evaluated against evidence.\ngot:\n%s", out)
	}
}

func TestRenderText_SubjectNeverStarted_NotEvaluatedAssertionVisible(t *testing.T) {
	// The not_evaluated assertions must appear in the text report (not be
	// silently dropped), so the reader can see which assertions were skipped
	// and why. The marker is "~", distinct from the failed-assertion "-" marker.
	out, err := renderText(t, neverStartedResult())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if !strings.Contains(out, "not_evaluated") {
		t.Errorf("text report does not mention not_evaluated for the skipped assertion; the reader must see that the assertion was not evaluated, not just absent.\ngot:\n%s", out)
	}
}

// --- JSON surface: never-started case ---

func TestRenderJSON_SubjectNeverStarted_ConditionKindAndDetailPresent(t *testing.T) {
	// A machine consumer must be able to identify a never-started condition
	// by kind and extract the one-line cause from the detail.
	out, err := renderJSON(t, neverStartedResult())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if !strings.Contains(string(out), string(domain.ConditionSubjectNeverStarted)) {
		t.Errorf("JSON report does not contain the condition kind %q; a machine consumer must be able to identify a never-started condition.\ngot:\n%s", domain.ConditionSubjectNeverStarted, out)
	}
	if !strings.Contains(string(out), neverStartedCause) {
		t.Errorf("JSON report does not contain the one-line cause %q; a machine consumer must be able to surface the real cause.\ngot:\n%s", neverStartedCause, out)
	}
}

func TestRenderJSON_SubjectNeverStarted_AssertionOutcomeIsNotEvaluated(t *testing.T) {
	// Assertions for a never-started run must carry "not_evaluated", not
	// "fail" — a machine consumer must be able to distinguish "the subject
	// never started" from "the subject ran and failed its assertions".
	out, err := renderJSON(t, neverStartedResult())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if strings.Contains(string(out), `"outcome": "fail"`) {
		t.Errorf("JSON report contains an assertion with outcome \"fail\" for a run whose subject never started; assertions must carry \"not_evaluated\" when nothing could have been evaluated.\ngot:\n%s", out)
	}
	if !strings.Contains(string(out), `"outcome": "not_evaluated"`) {
		t.Errorf("JSON report does not contain an assertion with outcome \"not_evaluated\"; declared assertions must appear as not_evaluated, not be silently absent.\ngot:\n%s", out)
	}
}

// --- Contrast cases: genuine assertion failures must render in full ---

func TestRenderText_GenuineAssertionFailure_ShowsFullPerAssertionDiagnostics(t *testing.T) {
	// A subject that genuinely ran and failed its assertions must produce the
	// full "expected ... got ..." diagnostic in the text report. The spawn-failed
	// path must not swallow real assertion evidence in the presentation layer.
	out, err := renderText(t, genuineAssertionFailureResult())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	if !strings.Contains(out, `expected "SUCCESS"`) {
		t.Errorf("text report does not show the expected value for a genuine assertion failure; full per-assertion diagnostics must be preserved for subjects that actually ran.\ngot:\n%s", out)
	}
	if !strings.Contains(out, `got "FAILURE"`) {
		t.Errorf("text report does not show the actual value for a genuine assertion failure; full per-assertion diagnostics must be preserved for subjects that actually ran.\ngot:\n%s", out)
	}
	if strings.Contains(out, string(domain.ConditionSubjectNeverStarted)) {
		t.Errorf("text report contains the never-started condition for a run whose subject completed normally; ConditionSubjectNeverStarted is reserved for spawn failures.\ngot:\n%s", out)
	}
}

func TestRenderJSON_GenuineAssertionFailure_AssertionsHaveFailOutcome(t *testing.T) {
	// The contrast case on the JSON surface: genuine assertion failures must
	// carry outcome "fail". A machine consumer must be able to distinguish a
	// real assertion failure from a not_evaluated one.
	out, err := renderJSON(t, genuineAssertionFailureResult())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	if !strings.Contains(string(out), `"outcome": "fail"`) {
		t.Errorf("JSON report does not contain an assertion with outcome \"fail\" for a genuine assertion failure; a machine consumer must be able to identify a real failure.\ngot:\n%s", out)
	}
	if strings.Contains(string(out), `"outcome": "not_evaluated"`) {
		t.Errorf("JSON report contains a not_evaluated assertion for a genuine assertion failure; not_evaluated must be reserved for subjects that never started.\ngot:\n%s", out)
	}
}
