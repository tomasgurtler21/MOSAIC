package report_test

// Tests for condition deduplication in the per-run text report.
//
// The requirement: when multiple runs for the same test carry identical
// (Kind, Detail) condition entries, each unique condition must appear
// exactly once in that test's section of the rendered output. The current
// implementation renders conditions per-run without deduplication, so a
// condition shared across N runs appears N times. These tests fail against
// the current implementation and pass once writeTestLine deduplicates
// conditions across all runs before rendering.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderText_ConditionDedup_IdenticalConditionsAcrossRunsAppearsOnce
// asserts that when all three runs for a single test carry the same
// (Kind, Detail) condition, that condition line appears exactly once in the
// rendered output. This is the primary failing test for the dedup requirement.
func TestRenderText_ConditionDedup_IdenticalConditionsAcrossRunsAppearsOnce(t *testing.T) {
	// Arrange: a test with three runs, all sharing the same condition.
	sharedCondition := domain.RunCondition{
		Kind:   domain.ConditionCostUnattributed,
		Detail: "log root missing",
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "dedup-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "repeated-condition-test",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "repeated-condition-test",
					Verdict:  domain.VerdictPass,
					Counted:  3,
					Passed:   3,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "repeated-condition-test", RunNumber: 1},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "repeated-condition-test", RunNumber: 2},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "repeated-condition-test", RunNumber: 3},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the condition body line ("! kind: detail") appears exactly once.
	// Note: the condition kind may also appear in the test headline class label
	// [cost_unattributed]; we count only the rendered body lines prefixed with "! ".
	conditionLine := "  ! " + string(domain.ConditionCostUnattributed) + ":"
	count := strings.Count(out, conditionLine)
	if count != 1 {
		t.Errorf("condition body line %q appears %d times; want exactly 1 (deduplication across runs required).\ngot:\n%s",
			conditionLine, count, out)
	}

	// The condition detail must still appear.
	if !strings.Contains(out, "log root missing") {
		t.Errorf("condition detail %q missing from output; deduplicated condition must still show its detail.\ngot:\n%s",
			"log root missing", out)
	}
}

// TestRenderText_ConditionDedup_TwoRunsSameConditionAppearsOnce asserts the
// minimal dedup case: two runs sharing one condition produce one condition line.
func TestRenderText_ConditionDedup_TwoRunsSameConditionAppearsOnce(t *testing.T) {
	sharedCondition := domain.RunCondition{
		Kind:   domain.ConditionUnterminatedInterval,
		Detail: "invocation seq 3 has no matching end record",
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "dedup-two-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "two-run-condition-test",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "two-run-condition-test",
					Verdict:  domain.VerdictPass,
					Counted:  2,
					Passed:   2,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "two-run-condition-test", RunNumber: 1},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "two-run-condition-test", RunNumber: 2},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
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

	conditionLine := "  ! " + string(domain.ConditionUnterminatedInterval) + ":"
	count := strings.Count(out, conditionLine)
	if count != 1 {
		t.Errorf("condition body line %q appears %d times across 2 identical runs; want exactly 1.\ngot:\n%s",
			conditionLine, count, out)
	}
}

// TestRenderText_ConditionDedup_DistinctConditionsAllAppear asserts that when
// runs carry different (Kind, Detail) conditions, each unique condition appears
// in the output. Deduplication must not suppress distinct conditions.
func TestRenderText_ConditionDedup_DistinctConditionsAllAppear(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "dedup-distinct-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "distinct-conditions-test",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "distinct-conditions-test",
					Verdict:  domain.VerdictPass,
					Counted:  2,
					Passed:   2,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "distinct-conditions-test", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionCostUnattributed, Detail: "log root missing"},
						},
						Duration: time.Second,
					},
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "distinct-conditions-test", RunNumber: 2},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionUnterminatedInterval, Detail: "invocation seq 2 has no end"},
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

	if !strings.Contains(out, string(domain.ConditionCostUnattributed)) {
		t.Errorf("condition %q missing from output; distinct conditions must all appear.\ngot:\n%s",
			domain.ConditionCostUnattributed, out)
	}
	if !strings.Contains(out, string(domain.ConditionUnterminatedInterval)) {
		t.Errorf("condition %q missing from output; distinct conditions must all appear.\ngot:\n%s",
			domain.ConditionUnterminatedInterval, out)
	}
}

// TestRenderText_ConditionDedup_SameKindDifferentDetailBothAppear asserts
// that conditions with the same Kind but different Detail are treated as
// distinct entries and both appear in the output. The deduplication key is
// (Kind, Detail) not just Kind.
func TestRenderText_ConditionDedup_SameKindDifferentDetailBothAppear(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "dedup-detail-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "varied-detail-test",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "varied-detail-test",
					Verdict:  domain.VerdictPass,
					Counted:  2,
					Passed:   2,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "varied-detail-test", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionUnterminatedInterval, Detail: "invocation seq 1 has no end"},
						},
						Duration: time.Second,
					},
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "varied-detail-test", RunNumber: 2},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionUnterminatedInterval, Detail: "invocation seq 3 has no end"},
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

	if !strings.Contains(out, "invocation seq 1 has no end") {
		t.Errorf("detail \"invocation seq 1 has no end\" missing; same-kind different-detail conditions must both appear.\ngot:\n%s", out)
	}
	if !strings.Contains(out, "invocation seq 3 has no end") {
		t.Errorf("detail \"invocation seq 3 has no end\" missing; same-kind different-detail conditions must both appear.\ngot:\n%s", out)
	}
}

// TestRenderText_ConditionDedup_DeduplicationIsScopedPerTest asserts that
// deduplication is per-test, not global. If two different tests each have the
// same condition, each test's section should show the condition once (not zero
// times because the other test already showed it).
func TestRenderText_ConditionDedup_DeduplicationIsScopedPerTest(t *testing.T) {
	sharedCondition := domain.RunCondition{
		Kind:   domain.ConditionCostUnattributed,
		Detail: "log root missing",
	}
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "dedup-scope-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "test-alpha",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "test-alpha",
					Verdict:  domain.VerdictPass,
					Counted:  2,
					Passed:   2,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "test-alpha", RunNumber: 1},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "test-alpha", RunNumber: 2},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
				},
			},
			{
				TestName: "test-beta",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "test-beta",
					Verdict:  domain.VerdictPass,
					Counted:  2,
					Passed:   2,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "test-beta", RunNumber: 1},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
					{
						Key:        domain.RunKey{RunID: "run-001", TestName: "test-beta", RunNumber: 2},
						Verdict:    domain.VerdictPass,
						Conditions: []domain.RunCondition{sharedCondition},
						Duration:   time.Second,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 2},
	}

	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Each test should show the condition body line once: total count must be exactly 2.
	conditionLine := "  ! " + string(domain.ConditionCostUnattributed) + ":"
	count := strings.Count(out, conditionLine)
	if count != 2 {
		t.Errorf("condition body line %q appears %d times across 2 tests (each with 2 identical runs); want exactly 2 (once per test).\ngot:\n%s",
			conditionLine, count, out)
	}
}

// TestRenderText_NoDurationInTextReport asserts that no duration value is
// rendered in the per-run text report. The text renderer intentionally omits
// duration; this test guards against re-introduction. Note: this test passes
// against the current implementation (duration is already not rendered) and
// serves as a regression guard only.
func TestRenderText_NoDurationInTextReport(t *testing.T) {
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "no-duration-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName: "duration-check-test",
				Layer:    domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName: "duration-check-test",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					PassRate: 1.0,
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestName: "duration-check-test", RunNumber: 1},
						Verdict:  domain.VerdictPass,
						Duration: 42 * time.Second,
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

	// "42s" is the Duration.String() for 42*time.Second. It must not appear
	// as a labeled "duration:" line in the text report.
	if strings.Contains(out, "duration: 42s") || strings.Contains(out, "duration:42s") {
		t.Errorf("text report renders a duration line; duration must not appear in the per-run text report.\ngot:\n%s", out)
	}
}
