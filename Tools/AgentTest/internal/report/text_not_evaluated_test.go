package report_test

// Tests for T13.2 / AC13.5 / AC13.6: writeTestLine's assertion loop today
// skips every outcome that is not domain.AssertionFail, so a
// domain.AssertionNotEvaluated assertion — the outcome layer: orchestrator
// produces for a declared protocol_violations assertion — is invisible in
// the text report even though it is present in the JSON report. This file
// pins that a not_evaluated assertion is listed explicitly with the reason
// it was not evaluated, and that existing failed-assertion and passing
// output is otherwise unchanged.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

const suppressionDetail = "layer: orchestrator suppresses protocol-message validation"

// TestRenderText_ListsNotEvaluatedAssertionWithReason is the direct AC13.5
// check: a not_evaluated assertion must appear in the text report, carrying
// the reason it was not evaluated.
func TestRenderText_ListsNotEvaluatedAssertionWithReason(t *testing.T) {
	// Arrange
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "not-evaluated-suite",
		StartedAt:     fixedStart(),
		FinishedAt:    fixedFinish(),
		Tests: []report.TestReport{
			{
				TestID: "orchestrator-suppressed",
				Layer:  domain.LayerOrchestrator,
				Aggregate: domain.AggregateResult{
					TestID:  "orchestrator-suppressed",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestID: "orchestrator-suppressed", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Assertions: []domain.AssertionResult{
							{
								Class:   domain.ClassProtocolViolations,
								Outcome: domain.AssertionNotEvaluated,
								Detail:  suppressionDetail,
							},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	// Act
	out, err := renderText(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, string(domain.ClassProtocolViolations)) {
		t.Errorf("rendering does not mention the not_evaluated assertion's class %q.\ngot:\n%s", domain.ClassProtocolViolations, out)
	}
	if !strings.Contains(out, suppressionDetail) {
		t.Errorf("rendering does not mention the reason the assertion was not evaluated (%q).\ngot:\n%s", suppressionDetail, out)
	}
}

// TestRenderText_NotEvaluatedAssertionDistinctFromFailedAssertion asserts a
// not_evaluated assertion does not render with the failed-assertion
// "expected ... got ..." phrasing: the two are different diagnoses and must
// not read alike, the same distinguishability discipline the package
// already applies to conditions (see text_conditions_test.go).
func TestRenderText_NotEvaluatedAssertionDistinctFromFailedAssertion(t *testing.T) {
	// Arrange
	failedRun := report.RunReport{
		Key:     domain.RunKey{RunID: "run-001", TestID: "failed-assertion", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionFail,
				Expected: "SUCCESS",
				Actual:   "FAILURE",
			},
		},
		Duration: time.Second,
	}
	notEvaluatedRun := report.RunReport{
		Key:     domain.RunKey{RunID: "run-002", TestID: "not-evaluated-assertion", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Assertions: []domain.AssertionResult{
			{
				Class:   domain.ClassProtocolViolations,
				Outcome: domain.AssertionNotEvaluated,
				Detail:  suppressionDetail,
			},
		},
		Duration: time.Second,
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "distinguishability-suite",
		StartedAt:     fixedStart(),
		FinishedAt:    fixedFinish(),
		Tests: []report.TestReport{
			{
				TestID:    "failed-assertion",
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{TestID: "failed-assertion", Verdict: domain.VerdictFail, Reasons: []domain.FailureReason{domain.ReasonAssertion}, Counted: 1},
				Runs:      []report.RunReport{failedRun},
			},
			{
				TestID:    "not-evaluated-assertion",
				Layer:     domain.LayerOrchestrator,
				Aggregate: domain.AggregateResult{TestID: "not-evaluated-assertion", Verdict: domain.VerdictPass, Counted: 1, Passed: 1},
				Runs:      []report.RunReport{notEvaluatedRun},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictFail: 1, domain.VerdictPass: 1},
	}

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the not_evaluated section carries the suppression detail, but
	// never phrases it as "expected ... got ..." — that phrasing is reserved
	// for an assertion that was actually evaluated and failed.
	failedSection, notEvaluatedSection := splitAtTestSections(t, out, "failed-assertion", "not-evaluated-assertion")
	if !strings.Contains(failedSection, `expected "SUCCESS", got "FAILURE"`) {
		t.Errorf("existing failed-assertion phrasing changed unexpectedly.\nsection:\n%s", failedSection)
	}
	if strings.Contains(notEvaluatedSection, "expected") || strings.Contains(notEvaluatedSection, "got \"\"") {
		t.Errorf("a not_evaluated assertion must not render with failed-assertion phrasing (there was no evidence to compare).\nsection:\n%s", notEvaluatedSection)
	}
	if !strings.Contains(notEvaluatedSection, suppressionDetail) {
		t.Errorf("not_evaluated section does not carry the suppression reason.\nsection:\n%s", notEvaluatedSection)
	}
}

// TestRenderText_PassingRunWithNoNotEvaluatedAssertions_Unaffected pins
// AC13.6: a run with no not_evaluated assertions renders exactly as it did
// before this feature — no stray not_evaluated marker leaks into ordinary
// passing output.
func TestRenderText_PassingRunWithNoNotEvaluatedAssertions_Unaffected(t *testing.T) {
	// Arrange
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "ordinary-pass-suite",
		StartedAt:     fixedStart(),
		FinishedAt:    fixedFinish(),
		Tests: []report.TestReport{
			{
				TestID: "ordinary-pass",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:  "ordinary-pass",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestID: "ordinary-pass", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Assertions: []domain.AssertionResult{
							{Class: domain.ClassFinalStatus, Outcome: domain.AssertionPass, Expected: "SUCCESS", Actual: "SUCCESS"},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	// Act
	out, err := renderText(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if strings.Contains(out, "not_evaluated") {
		t.Errorf("rendering mentions not_evaluated for a run that declares no not_evaluated assertion.\ngot:\n%s", out)
	}
	if !strings.Contains(out, "ordinary-pass: PASS") {
		t.Errorf("ordinary passing test-line output changed unexpectedly.\ngot:\n%s", out)
	}
}

func fixedStart() time.Time  { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func fixedFinish() time.Time { return time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC) }
