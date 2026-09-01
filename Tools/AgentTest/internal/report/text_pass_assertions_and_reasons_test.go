package report_test

// Tests for text rendering of passing assertion diagnostic fields and
// failure Reasons.
//
// Current gaps in writeTestLine:
//   - The switch on assertion outcome has no case for AssertionPass, so
//     passing assertions' Expected, Actual, Target and Detail fields are
//     never surfaced in the text report.
//   - run.Reasons is never read by the text renderer: only run.Conditions
//     is rendered (lines 87-91 of text.go), so the failure reason that
//     drove a verdict of FAIL is invisible in the text output.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// Passing assertions: diagnostic fields must appear in the text report
// ---------------------------------------------------------------------------

// TestRenderText_PassingAssertion_ShowsExpectedAndActual verifies that a
// passing assertion's Expected and Actual diagnostic fields appear somewhere
// in the text output. When a test passes the user should be able to confirm
// what was observed versus what was asserted, without re-reading the raw log.
func TestRenderText_PassingAssertion_ShowsExpectedAndActual(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "pass-assertion-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "happy-path",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "happy-path",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "happy-path", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Assertions: []domain.AssertionResult{
							{
								Class:    domain.ClassFinalStatus,
								Outcome:  domain.AssertionPass,
								Expected: "COMPLETED",
								Actual:   "COMPLETED",
							},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, "COMPLETED") {
		t.Errorf("text report does not mention the passing assertion's Expected/Actual value %q; diagnostic fields must be shown for passing assertions too.\ngot:\n%s", "COMPLETED", out)
	}
}

// TestRenderText_PassingAssertion_ShowsTarget verifies that when a passing
// assertion carries a Target (identifying which parallel group or invocation
// it evaluated), that Target appears in the text output alongside the class.
func TestRenderText_PassingAssertion_ShowsTarget(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "pass-target-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "with-target",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "with-target",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "with-target", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Assertions: []domain.AssertionResult{
							{
								Class:    domain.ClassArtifactCreated,
								Target:   "Orchestration/output.md",
								Outcome:  domain.AssertionPass,
								Expected: "exists",
								Actual:   "exists",
							},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, "Orchestration/output.md") {
		t.Errorf("text report does not mention the passing assertion's Target %q; the target identifies which artifact was checked.\ngot:\n%s", "Orchestration/output.md", out)
	}
}

// TestRenderText_PassingAssertion_ShowsDetail verifies that a passing
// assertion's Detail field, when non-empty, appears in the text output.
func TestRenderText_PassingAssertion_ShowsDetail(t *testing.T) {
	const passDetail = "artifact present at declared path"
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "pass-detail-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "detail-bearing-pass",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "detail-bearing-pass",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "detail-bearing-pass", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Assertions: []domain.AssertionResult{
							{
								Class:    domain.ClassArtifactCreated,
								Target:   "Orchestration/plan.md",
								Outcome:  domain.AssertionPass,
								Expected: "exists",
								Actual:   "exists",
								Detail:   passDetail,
							},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, passDetail) {
		t.Errorf("text report does not mention the passing assertion's Detail %q.\ngot:\n%s", passDetail, out)
	}
}

// TestRenderText_PassingAssertion_DistinctFromFailingAssertion verifies
// that the rendering of a passing assertion is visually distinguishable from
// a failing assertion. Two identical assertion classes with different outcomes
// must not produce identical output lines.
func TestRenderText_PassingAssertion_DistinctFromFailingAssertion(t *testing.T) {
	passing := report.RunReport{
		Key:     domain.RunKey{RunID: "run-001", TestName: "passing-test", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionPass,
				Expected: "COMPLETED",
				Actual:   "COMPLETED",
			},
		},
		Duration: time.Second,
	}
	failing := report.RunReport{
		Key:     domain.RunKey{RunID: "run-001", TestName: "failing-test", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionFail,
				Expected: "COMPLETED",
				Actual:   "REJECTED",
			},
		},
		Duration: time.Second,
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "distinction-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:    "passing-test",
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{TestName: "passing-test", Verdict: domain.VerdictPass, Counted: 1, Passed: 1},
				Runs:      []report.RunReport{passing},
			},
			{
				TestName:    "failing-test",
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{TestName: "failing-test", Verdict: domain.VerdictFail, Reasons: []domain.FailureReason{domain.ReasonAssertion}, Counted: 1},
				Runs:      []report.RunReport{failing},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1, domain.VerdictFail: 1},
	}

	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	passSection, failSection := splitAtTestSections(t, out, "passing-test", "failing-test")

	// The failing assertion uses the "- " prefix; the passing assertion must
	// use a different prefix or marker so the two are visually distinct.
	if !strings.Contains(failSection, "- ") {
		t.Errorf("failing assertion section does not use the expected '- ' prefix.\nsection:\n%s", failSection)
	}
	// If both sections have the exact same assertion line marker ("- "),
	// or neither has any marker, we can't distinguish them.
	if strings.Contains(passSection, "  - ") {
		t.Errorf("passing assertion section uses the same '  - ' prefix as failing assertions; pass and fail must render distinctly.\nsection:\n%s", passSection)
	}
}

// TestRenderText_PassingAssertion_WithNoAssertions_Unaffected pins that a
// run carrying no assertions at all is not changed by the new pass-assertion
// rendering path: no spurious assertion line is emitted.
func TestRenderText_PassingAssertion_WithNoAssertions_Unaffected(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "no-assertions-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "no-assertions",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "no-assertions",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "no-assertions", RunNumber: 1},
						Verdict:  domain.VerdictPass,
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	// The test-ID line should still appear.
	if !strings.Contains(out, "no-assertions: PASS") {
		t.Errorf("test line absent for run carrying no assertions.\ngot:\n%s", out)
	}
	// No assertion-detail lines (prefixed with "  +" or "  -") should appear.
	lines := strings.Split(out, "\n")
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  +") || strings.HasPrefix(ln, "  -") {
			t.Errorf("spurious assertion-detail line emitted for a run with no assertions: %q\nfull output:\n%s", ln, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Reasons: must appear in the text report when present
// ---------------------------------------------------------------------------

// TestRenderText_Reasons_RenderedWhenPresent verifies that run.Reasons
// entries are surfaced in the text report. Currently writeTestLine reads
// run.Conditions but never run.Reasons, so the failure reason driving a
// FAIL verdict is invisible in text output even though it is present in JSON.
func TestRenderText_Reasons_RenderedWhenPresent(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "reasons-visibility-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "assertion-failure",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "assertion-failure",
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion},
					Counted: 1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "assertion-failure", RunNumber: 1},
						Verdict: domain.VerdictFail,
						Reasons: []domain.FailureReason{domain.ReasonAssertion},
						Assertions: []domain.AssertionResult{
							{
								Class:    domain.ClassFinalStatus,
								Outcome:  domain.AssertionFail,
								Expected: "REJECTED",
								Actual:   "COMPLETED",
								Detail:   "final status did not match",
							},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictFail: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, string(domain.ReasonAssertion)) {
		t.Errorf("text report does not mention run.Reasons value %q; failure reasons must be surfaced in text output.\ngot:\n%s", domain.ReasonAssertion, out)
	}
}

// TestRenderText_Reasons_MultipleReasonsAllRendered verifies that when a run
// carries more than one failure reason, every reason appears in the output.
func TestRenderText_Reasons_MultipleReasonsAllRendered(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "multi-reasons-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "multi-reason-failure",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "multi-reason-failure",
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion, domain.ReasonEchoMismatch},
					Counted: 1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "multi-reason-failure", RunNumber: 1},
						Verdict: domain.VerdictFail,
						Reasons: []domain.FailureReason{domain.ReasonAssertion, domain.ReasonEchoMismatch},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictFail: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	for _, reason := range []domain.FailureReason{domain.ReasonAssertion, domain.ReasonEchoMismatch} {
		if !strings.Contains(out, string(reason)) {
			t.Errorf("text report does not mention run.Reasons value %q; all failure reasons must be surfaced.\ngot:\n%s", reason, out)
		}
	}
}

// TestRenderText_Reasons_AbsentWhenNone verifies that a run carrying no
// Reasons produces no spurious reason lines in the text output.
func TestRenderText_Reasons_AbsentWhenNone(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "no-reasons-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "passing-no-reasons",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "passing-no-reasons",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "passing-no-reasons", RunNumber: 1},
						Verdict:  domain.VerdictPass,
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	// No "? " prefix lines (the expected prefix for reasons) should appear.
	lines := strings.Split(out, "\n")
	for _, ln := range lines {
		if strings.HasPrefix(ln, "  ? ") {
			t.Errorf("spurious reason line emitted for a run with no Reasons: %q\nfull output:\n%s", ln, out)
		}
	}
}

// TestRenderText_Reasons_DistinctFromConditions verifies that Reasons and
// Conditions render with different prefixes or markers so a reader can
// distinguish them. Currently Conditions use "  ! "; Reasons must use a
// different prefix (e.g. "  ? ") so the two categories are visually separable.
func TestRenderText_Reasons_DistinctFromConditions(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "reasons-vs-conditions-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "run-with-both",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:  "run-with-both",
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion},
					Counted: 1,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "run-with-both", RunNumber: 1},
						Verdict: domain.VerdictFail,
						Reasons: []domain.FailureReason{domain.ReasonAssertion},
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionCostUnattributed, Detail: "log root missing"},
						},
						Duration: time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictFail: 1},
	}

	out, err := renderText(t, result)

	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Conditions must still appear.
	if !strings.Contains(out, string(domain.ConditionCostUnattributed)) {
		t.Errorf("conditions are no longer rendered after adding reasons rendering.\ngot:\n%s", out)
	}
	// Reasons must appear.
	if !strings.Contains(out, string(domain.ReasonAssertion)) {
		t.Errorf("reasons are not rendered alongside conditions.\ngot:\n%s", out)
	}

	// Find the lines carrying reasons and conditions and verify they use
	// different markers.
	var reasonLines, conditionLines []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, string(domain.ReasonAssertion)) {
			reasonLines = append(reasonLines, ln)
		}
		if strings.Contains(ln, string(domain.ConditionCostUnattributed)) {
			conditionLines = append(conditionLines, ln)
		}
	}
	if len(reasonLines) == 0 {
		t.Fatalf("no output line mentions the reason %q", domain.ReasonAssertion)
	}
	if len(conditionLines) == 0 {
		t.Fatalf("no output line mentions the condition %q", domain.ConditionCostUnattributed)
	}
	// The two categories must not use the exact same line-prefix.
	reasonPrefix := strings.TrimRight(reasonLines[0], " \t")[:min(4, len(reasonLines[0]))]
	conditionPrefix := strings.TrimRight(conditionLines[0], " \t")[:min(4, len(conditionLines[0]))]
	if reasonPrefix == conditionPrefix {
		t.Errorf("reasons and conditions use the same line prefix %q; they must be visually distinct.\nreason line: %q\ncondition line: %q", reasonPrefix, reasonLines[0], conditionLines[0])
	}
}

