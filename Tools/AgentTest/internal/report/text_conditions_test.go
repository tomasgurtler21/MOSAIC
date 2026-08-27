package report_test

// Tests for AC2.3/AC2.4: run conditions must be visible in the text report,
// and a degraded-extraction failure must read distinctly from an ordinary
// assertion mismatch rather than presenting as the same bare mismatch line.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderText_SurfacesRunConditionKindAndDetail is the direct AC2.4
// check: today writeTestLine never reads run.Conditions at all, so a
// condition present on a run is invisible in the text report even though
// JSON carries it.
func TestRenderText_SurfacesRunConditionKindAndDetail(t *testing.T) {
	// Arrange
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "condition-visibility-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "unterminated-invocation",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "unterminated-invocation",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "unterminated-invocation", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionUnterminatedInterval, Detail: "invocation seq 3 has no matching end record"},
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
	if !strings.Contains(out, string(domain.ConditionUnterminatedInterval)) {
		t.Errorf("rendering does not mention the condition kind %q; run conditions must be visible in the text report.\ngot:\n%s", domain.ConditionUnterminatedInterval, out)
	}
	if !strings.Contains(out, "invocation seq 3 has no matching end record") {
		t.Errorf("rendering does not mention the condition detail; a condition without its detail cannot be diagnosed.\ngot:\n%s", out)
	}
}

// TestRenderText_DegradedExtractionFailureDistinctFromAssertionMismatch
// builds two runs that fail the same assertion class with a similarly bare
// mismatch — the shape the plan calls out (task_description "" does not
// contain declared substring): one because extraction genuinely degraded,
// the other for an ordinary reason. The two renderings must not read alike.
func TestRenderText_DegradedExtractionFailureDistinctFromAssertionMismatch(t *testing.T) {
	// Arrange
	ordinaryMismatch := report.RunReport{
		Key:     domain.RunKey{RunID: "run-001", TestName: "ordinary-mismatch", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassTaskMessage,
				Target:   "1",
				Outcome:  domain.AssertionFail,
				Expected: "widgets",
				Actual:   "",
				Detail:   `task_description "" does not contain declared substring "widgets"`,
			},
		},
		Duration: time.Second,
	}
	degradedExtraction := report.RunReport{
		Key:     domain.RunKey{RunID: "run-002", TestName: "degraded-extraction", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassTaskMessage,
				Target:   "1",
				Outcome:  domain.AssertionFail,
				Expected: "widgets",
				Actual:   "",
				Detail:   `task_description "" does not contain declared substring "widgets"`,
			},
		},
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionExtractionDegraded, Detail: "invocation seq 1's message was only degraded-recovered, not cleanly parsed"},
		},
		Duration: time.Second,
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "distinguishability-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:    "ordinary-mismatch",
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{TestName: "ordinary-mismatch", Verdict: domain.VerdictFail, Reasons: []domain.FailureReason{domain.ReasonAssertion}, Counted: 1},
				Runs:      []report.RunReport{ordinaryMismatch},
			},
			{
				TestName:    "degraded-extraction",
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{TestName: "degraded-extraction", Verdict: domain.VerdictFail, Reasons: []domain.FailureReason{domain.ReasonAssertion}, Counted: 1},
				Runs:      []report.RunReport{degradedExtraction},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictFail: 2},
	}

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the degraded-extraction run's condition must appear, giving
	// its section of the report a marker the ordinary-mismatch run's
	// section does not carry.
	if !strings.Contains(out, string(domain.ConditionExtractionDegraded)) {
		t.Fatalf("rendering does not mention %q anywhere; a degraded extraction must be visible, not just a bare assertion mismatch.\ngot:\n%s", domain.ConditionExtractionDegraded, out)
	}

	ordinarySection, degradedSection := splitAtTestSections(t, out, "ordinary-mismatch", "degraded-extraction")
	if strings.Contains(ordinarySection, string(domain.ConditionExtractionDegraded)) {
		t.Errorf("the ordinary-mismatch section wrongly carries the extraction-degraded marker.\nsection:\n%s", ordinarySection)
	}
	if !strings.Contains(degradedSection, string(domain.ConditionExtractionDegraded)) {
		t.Errorf("the degraded-extraction section does not carry the extraction-degraded marker that distinguishes it from an ordinary mismatch.\nsection:\n%s", degradedSection)
	}
	if ordinarySection == degradedSection {
		t.Error("the two sections render identically; a degraded extraction must not read the same as an ordinary assertion mismatch")
	}
}

// splitAtTestSections carves out of out the portion belonging to each named
// test's line-group, from its own "TestName:" header up to (but not
// including) the next one. Both ids must appear in out.
func splitAtTestSections(t *testing.T, out, firstID, secondID string) (first, second string) {
	t.Helper()
	firstIdx := strings.Index(out, firstID+":")
	secondIdx := strings.Index(out, secondID+":")
	if firstIdx == -1 || secondIdx == -1 {
		t.Fatalf("expected both %q and %q to appear as test lines in:\n%s", firstID, secondID, out)
	}
	if firstIdx < secondIdx {
		return out[firstIdx:secondIdx], out[secondIdx:]
	}
	return out[firstIdx:], out[secondIdx:firstIdx]
}
