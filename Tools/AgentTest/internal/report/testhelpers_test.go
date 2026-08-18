package report_test

// Shared fixtures for the report package's golden-output and agreement
// tests. The fixture model is built directly as a report.Result literal, not
// derived from evaluate.Aggregate: the renderers and the agreement test must
// treat Result as the single source of truth regardless of how one was
// assembled, and building it independently of the verdict engine keeps that
// property honest.

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// renderText runs report.RenderText and converts a panic into a plain
// error, so a renderer that is not yet implemented fails the calling test
// cleanly instead of crashing the whole test binary.
func renderText(t *testing.T, r report.Result) (out string, err error) {
	t.Helper()
	var buf bytes.Buffer
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("RenderText panicked: %v", rec)
		}
	}()
	err = report.RenderText(&buf, r)
	out = buf.String()
	return out, err
}

// renderJSON runs report.RenderJSON and converts a panic into a plain
// error, for the same reason renderText does.
func renderJSON(t *testing.T, r report.Result) (out []byte, err error) {
	t.Helper()
	var buf bytes.Buffer
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("RenderJSON panicked: %v", rec)
		}
	}()
	err = report.RenderJSON(&buf, r)
	out = buf.Bytes()
	return out, err
}

func fixtureStarted() time.Time {
	return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
}

func fixtureFinished() time.Time {
	return time.Date(2026, 8, 7, 12, 5, 0, 0, time.UTC)
}

// fixtureFullResult covers a pass, an assertion failure with per-assertion
// detail, a timeout, a state-integrity failure, and a cost-attribution
// miss — the five outcome classes AC14.3/AC14.7 require to be visibly
// distinguishable.
func fixtureFullResult() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "checkout-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestID:      "checkout-happy-path",
				Description: "a valid cart checks out",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:   "checkout-happy-path",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					PassRate: 1,
					TotalCost: domain.CostReport{
						TotalUSD:    0.05,
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestID: "checkout-happy-path", RunNumber: 1},
						Verdict:  domain.VerdictPass,
						Duration: 2 * time.Second,
						Cost: domain.CostReport{
							TotalUSD:    0.05,
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
			{
				TestID:      "checkout-rejects-invalid-cart",
				Description: "an empty cart is rejected rather than completed",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:  "checkout-rejects-invalid-cart",
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion},
					Counted: 1,
					Passed:  0,
					TotalCost: domain.CostReport{
						TotalUSD:    0.02,
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestID: "checkout-rejects-invalid-cart", RunNumber: 1},
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
						Duration: 1500 * time.Millisecond,
						Cost: domain.CostReport{
							TotalUSD:    0.02,
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
			{
				TestID:      "checkout-times-out",
				Description: "the subject exceeds its declared timeout",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:  "checkout-times-out",
					Verdict: domain.VerdictTimeout,
					Reasons: []domain.FailureReason{domain.ReasonTimeout},
					Counted: 1,
					Passed:  0,
					TotalCost: domain.CostReport{
						TotalUSD:    0.10,
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestID: "checkout-times-out", RunNumber: 1},
						Verdict:  domain.VerdictTimeout,
						Reasons:  []domain.FailureReason{domain.ReasonTimeout},
						Duration: 30 * time.Second,
						Cost: domain.CostReport{
							TotalUSD:    0.10,
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
			{
				TestID:      "checkout-state-integrity-fault",
				Description: "a lock reclaim recurs and ends the test as an infrastructure failure",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:                "checkout-state-integrity-fault",
					Verdict:               domain.VerdictFail,
					Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
					Counted:               0,
					Passed:                0,
					Excluded:              1,
					InfrastructureFailure: true,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestID: "checkout-state-integrity-fault", RunNumber: 1},
						Verdict:  domain.VerdictFail,
						Reasons:  []domain.FailureReason{domain.ReasonStateIntegrity},
						Duration: 5 * time.Second,
						Cost: domain.CostReport{
							TotalUSD:    0.03,
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
			{
				TestID:      "checkout-cost-unattributed",
				Description: "the subject passes but its cost could not be attributed",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:   "checkout-cost-unattributed",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					PassRate: 1,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionUnavailable,
						Detail:      "log root missing",
					},
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestID: "checkout-cost-unattributed", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Conditions: []domain.RunCondition{
							{Kind: domain.ConditionCostUnattributed, Detail: "log root missing"},
						},
						Duration: 1 * time.Second,
						Cost: domain.CostReport{
							Attribution: domain.AttributionUnavailable,
							Detail:      "log root missing",
						},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{
			domain.VerdictPass:    2,
			domain.VerdictFail:    2,
			domain.VerdictTimeout: 1,
		},
		TotalCost: domain.CostReport{
			TotalUSD:    0.17,
			Attribution: domain.AttributionUnavailable,
			Detail:      "log root missing",
		},
		InfrastructureFailures: 1,
	}
}

// fixtureEmptyResult is a suite with no tests at all.
func fixtureEmptyResult() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "empty-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureStarted(),
		Tests:         nil,
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}
}

// fixtureAllExcludedResult is a suite whose only test's every repetition was
// excluded (repeated state-integrity fault), so the aggregate counts nothing
// — the "all-skipped" shape-stability case AC14.8 calls for.
func fixtureAllExcludedResult() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "all-excluded-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestID:      "always-loses-its-lock",
				Description: "every repetition is excluded for state integrity",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:                "always-loses-its-lock",
					Verdict:               domain.VerdictFail,
					Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
					Counted:               0,
					Excluded:              2,
					InfrastructureFailure: true,
					TotalCost:             domain.CostReport{Attribution: domain.AttributionAttributed},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-001", TestID: "always-loses-its-lock", RunNumber: 1},
						Verdict:  domain.VerdictFail,
						Reasons:  []domain.FailureReason{domain.ReasonStateIntegrity},
						Duration: 500 * time.Millisecond,
						Cost:     domain.CostReport{Attribution: domain.AttributionAttributed},
					},
					{
						Key:      domain.RunKey{RunID: "run-001", TestID: "always-loses-its-lock", RunNumber: 2},
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
