package report_test

// Tests for the repetition statistics appearing in the human-readable (text)
// report per test: the counted and passed repetition counts, the achieved pass
// rate and the required threshold.
//
// These are TDD RED phase tests — they specify the expected output and fail
// until the text renderer is extended to include repetition statistics.
//
// The invariant this feature depends on: every number in the rendering comes
// from the report model, never from a calculation inside the renderer.
// RequiredPassRate is on AggregateResult, populated at aggregation time;
// the text renderer reads it directly. A renderer that recomputes counts from
// the runs list violates the report package's documented contract.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderText_RepetitionStats_ShowsCountedPassedAndRates asserts that when
// an aggregate has counted repetitions, the per-test line in the human-readable
// report includes the counted/passed counts, the achieved rate and the required
// threshold. A developer reading the terminal output must be able to judge
// whether the achieved rate met the declared requirement without opening the
// machine-readable report.
func TestRenderText_RepetitionStats_ShowsCountedPassedAndRates(t *testing.T) {
	// Arrange: 3 of 5 repetitions passed against a required 80%
	result := fixtureResultWithRepetitionStats(3, 5, 0.6, 0.8, domain.VerdictFail)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: counted and passed counts appear on the per-test line
	if !strings.Contains(out, "3/5") {
		t.Errorf("terminal report has no '3/5' fragment;\n"+
			"want the per-test line to show passed/counted repetition counts "+
			"(e.g. '3/5 passed') so a reader can see how many repetitions ran and passed.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: the required threshold marker appears
	if !strings.Contains(out, "required") {
		t.Errorf("terminal report has no 'required' fragment;\n"+
			"want the per-test line to show the required pass-rate threshold "+
			"so a reader can see what rate was needed to pass.\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_RepetitionStats_PassingTest_ShowsFullInfo asserts that a
// passing test with multiple repetitions shows the full repetition statistics
// including the achieved rate meeting the required threshold.
func TestRenderText_RepetitionStats_PassingTest_ShowsFullInfo(t *testing.T) {
	// Arrange: 4 of 5 repetitions passed against a required 60%
	result := fixtureResultWithRepetitionStats(4, 5, 0.8, 0.6, domain.VerdictPass)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: passed/counted counts appear
	if !strings.Contains(out, "4/5") {
		t.Errorf("terminal report has no '4/5' fragment;\n"+
			"want the per-test line to show passed/counted repetition counts.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: the verdict is PASS
	if !strings.Contains(out, "PASS") {
		t.Errorf("terminal report does not contain 'PASS' for a passing test.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: the required threshold marker appears
	if !strings.Contains(out, "required") {
		t.Errorf("terminal report has no 'required' fragment for a passing test;\n"+
			"the required threshold must appear on passing tests too, not only on "+
			"failing ones.\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_RepetitionStats_ZeroCounted_ShowsNotApplicableRate asserts
// that when no repetitions were counted (e.g. all were excluded for state
// integrity, or the only repetition was a setup failure), the per-test line
// renders the counts as 0/0 and the achieved rate as an explicit
// not-applicable marker — not as "0%", which would read as a real measured
// failure rate when in fact nothing was measured.
func TestRenderText_RepetitionStats_ZeroCounted_ShowsNotApplicableRate(t *testing.T) {
	// Arrange: zero counted repetitions, required pass rate of 80%
	result := fixtureResultWithRepetitionStats(0, 0, 0.0, 0.8, domain.VerdictFail)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: 0/0 counts appear
	if !strings.Contains(out, "0/0") {
		t.Errorf("terminal report has no '0/0' fragment for a zero-counted test;\n"+
			"want the per-test line to show '0/0' counts so a reader can distinguish "+
			"zero counted repetitions from a counted failure.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: a not-applicable marker appears rather than "0%"
	// "0%" as the achieved rate would mislead — it reads as a real measured failure.
	if !strings.Contains(out, "n/a") {
		t.Errorf("terminal report has no 'n/a' marker for a zero-counted test's achieved rate;\n"+
			"want a not-applicable marker rather than '0%%', which would read as a "+
			"real measured failure rate when in fact nothing was measured.\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_RepetitionStats_ZeroCounted_RequiredThresholdStillRendered
// asserts that the required threshold renders even when counted is zero. A
// reader looking at a zero-counted result must still be able to see what rate
// was required, so they know what the test expected to achieve.
func TestRenderText_RepetitionStats_ZeroCounted_RequiredThresholdStillRendered(t *testing.T) {
	// Arrange: zero counted repetitions, required pass rate of 80%
	result := fixtureResultWithRepetitionStats(0, 0, 0.0, 0.8, domain.VerdictFail)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the required threshold still appears
	if !strings.Contains(out, "required") {
		t.Errorf("terminal report has no 'required' fragment for a zero-counted test;\n"+
			"want the required threshold to still render so a reader can see what "+
			"rate was expected, even when nothing was counted.\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_RepetitionStats_AllExcluded_RendersWellFormed asserts that a
// test where every repetition was excluded (a recurring state-integrity fault)
// still renders a well-formed per-test line with repetition statistics.
func TestRenderText_RepetitionStats_AllExcluded_RendersWellFormed(t *testing.T) {
	// Arrange: all-excluded test — counted is 0, excluded is 2
	result := fixtureResultWithAllExcludedRepetitions()

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error for an all-excluded test: %v", err)
	}

	// Assert: the output is non-empty and well-formed (no panic, no empty output)
	if out == "" {
		t.Error("RenderText produced empty output for an all-excluded test; " +
			"want a well-formed per-test line")
	}

	// Assert: 0/0 appears (counted is 0, passed is 0)
	if !strings.Contains(out, "0/0") {
		t.Errorf("terminal report has no '0/0' fragment for an all-excluded test;\n"+
			"an all-excluded test has zero counted repetitions and should render "+
			"the same zero-counted statistics (0/0).\n"+
			"--- got ---\n%s", out)
	}
}

// TestRenderText_RepetitionStats_ReadFromModel_NoRendererCalculation asserts
// that the stats in the per-test line come from the AggregateResult fields on
// the model, not computed by the renderer from the runs list. This is the
// report package's core invariant: no consumer computes a count, a total or an
// outcome class of its own.
//
// The fixture sets Counted=7, Passed=3 with an empty Runs list. A renderer
// that reads from the model produces "3/7"; a renderer that recomputes from
// Runs would produce "0/0" because no runs are present.
func TestRenderText_RepetitionStats_ReadFromModel_NoRendererCalculation(t *testing.T) {
	// Arrange: AggregateResult with specific counts but an empty runs list.
	// The counts are deliberately inconsistent with the empty runs list so
	// that a renderer recomputing from runs would produce different output.
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "no-recalculation-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "from-model-test",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:           "from-model-test",
					Verdict:          domain.VerdictFail,
					Counted:          7,
					Passed:           3,
					PassRate:         0.4286,
					RequiredPassRate: 0.5,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{}, // empty — a recomputing renderer would see 0/0
			},
		},
		Counts:    map[domain.Verdict]int{domain.VerdictFail: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the model's Counted and Passed values appear in the output.
	// A renderer that recomputes from the empty Runs list would produce "0/0",
	// not "3/7", exposing the contract violation.
	if !strings.Contains(out, "3/7") {
		t.Errorf("terminal report has no '3/7' fragment;\n"+
			"want repetition counts to come from AggregateResult.Passed and "+
			"AggregateResult.Counted on the model, not recomputed from the Runs list.\n"+
			"A renderer that recomputes would show '0/0' (runs list is empty).\n"+
			"--- got ---\n%s", out)
	}
}

// --- fixtures ---

// fixtureResultWithRepetitionStats builds a minimal report.Result with one
// test whose AggregateResult carries the specified repetition statistics.
// It is the minimal fixture needed to exercise the repetition-stats rendering
// path without pulling in the full fixtureFullResult fixture.
//
// This fixture is also used by repetition_stats_json_test.go since both files
// are compiled together in the report_test package.
func fixtureResultWithRepetitionStats(passed, counted int, passRate, requiredPassRate float64, verdict domain.Verdict) report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "repetition-stats-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "repetition-stats-test",
				Description: "repetition statistics appear in the per-test line",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:           "repetition-stats-test",
					Verdict:          verdict,
					Counted:          counted,
					Passed:           passed,
					PassRate:         passRate,
					RequiredPassRate: requiredPassRate,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "repetition-stats-test", RunNumber: 1},
						Verdict:  verdict,
						Duration: 1 * time.Second,
						Cost: domain.CostReport{
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{
			verdict: 1,
		},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}
}

// fixtureResultWithAllExcludedRepetitions builds a Result where the only test's
// every repetition was excluded for state integrity. This is the all-excluded
// edge case: counted is 0, excluded is 2.
func fixtureResultWithAllExcludedRepetitions() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "all-excluded-repetitions-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "always-excluded-test",
				Description: "every repetition excluded for state integrity",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:                "always-excluded-test",
					Verdict:               domain.VerdictFail,
					Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
					Counted:               0,
					Passed:                0,
					Excluded:              2,
					RequiredPassRate:      0.8,
					InfrastructureFailure: true,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "always-excluded-test", RunNumber: 1},
						Verdict:  domain.VerdictFail,
						Reasons:  []domain.FailureReason{domain.ReasonStateIntegrity},
						Duration: 500 * time.Millisecond,
						Cost:     domain.CostReport{Attribution: domain.AttributionAttributed},
					},
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "always-excluded-test", RunNumber: 2},
						Verdict:  domain.VerdictFail,
						Reasons:  []domain.FailureReason{domain.ReasonStateIntegrity},
						Duration: 500 * time.Millisecond,
						Cost:     domain.CostReport{Attribution: domain.AttributionAttributed},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}
}
