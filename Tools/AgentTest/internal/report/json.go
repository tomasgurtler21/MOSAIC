package report

import (
	"encoding/json"
	"io"
	"time"

	"mosaic-agent-test/internal/domain"
)

// The wire* types are the versioned, stable machine-readable shape.
// SchemaVersion identifies it; the shape is additive-only from here on.
// They exist separately from Result so a field can be added to the wire
// format without disturbing the domain-facing model, and so every slice
// renders as "[]" rather than "null" when empty — a caller parsing this
// output never has to distinguish an omitted collection from an empty one.

type wireResult struct {
	SchemaVersion          string           `json:"schema_version"`
	SuiteID                string           `json:"suite_id"`
	StartedAt              time.Time        `json:"started_at"`
	FinishedAt             time.Time        `json:"finished_at"`
	Tests                  []wireTestReport `json:"tests"`
	Counts                 map[string]int   `json:"counts"`
	TotalCost              wireCost         `json:"total_cost"`
	InfrastructureFailures int              `json:"infrastructure_failures"`
}

type wireTestReport struct {
	TestID      string          `json:"test_id"`
	Description string          `json:"description"`
	Layer       string          `json:"layer"`
	Aggregate   wireAggregate   `json:"aggregate"`
	Runs        []wireRunReport `json:"runs"`
}

type wireAggregate struct {
	TestID                string   `json:"test_id"`
	Verdict               string   `json:"verdict"`
	Reasons               []string `json:"reasons"`
	Counted               int      `json:"counted"`
	Passed                int      `json:"passed"`
	Excluded              int      `json:"excluded"`
	PassRate              float64  `json:"pass_rate"`
	InfrastructureFailure bool     `json:"infrastructure_failure"`
	TotalCost             wireCost `json:"total_cost"`
}

type wireRunReport struct {
	Run             wireRunKey      `json:"run"`
	Verdict         string          `json:"verdict"`
	Reasons         []string        `json:"reasons"`
	Assertions      []wireAssertion `json:"assertions"`
	Conditions      []wireCondition `json:"conditions"`
	DurationMS      int64           `json:"duration_ms"`
	Cost            wireCost        `json:"cost"`
	NegativeApplied bool            `json:"negative_applied"`
}

type wireRunKey struct {
	RunID     string `json:"run_id"`
	TestID    string `json:"test_id"`
	RunNumber int    `json:"run_number"`
}

type wireAssertion struct {
	Class    string `json:"class"`
	Target   string `json:"target"`
	Outcome  string `json:"outcome"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Detail   string `json:"detail"`
}

type wireCondition struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type wireCost struct {
	TotalUSD    float64 `json:"total_usd"`
	Attribution string  `json:"attribution"`
	Detail      string  `json:"detail"`
}

// renderJSON writes r as the versioned machine-readable report.
func renderJSON(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toWireResult(r))
}

func toWireResult(r Result) wireResult {
	tests := make([]wireTestReport, 0, len(r.Tests))
	for _, t := range r.Tests {
		tests = append(tests, toWireTestReport(t))
	}

	counts := make(map[string]int, len(r.Counts))
	for v, n := range r.Counts {
		counts[string(v)] = n
	}

	return wireResult{
		SchemaVersion:          r.SchemaVersion,
		SuiteID:                r.SuiteID,
		StartedAt:              r.StartedAt,
		FinishedAt:             r.FinishedAt,
		Tests:                  tests,
		Counts:                 counts,
		TotalCost:              toWireCost(r.TotalCost),
		InfrastructureFailures: r.InfrastructureFailures,
	}
}

func toWireTestReport(t TestReport) wireTestReport {
	runs := make([]wireRunReport, 0, len(t.Runs))
	for _, run := range t.Runs {
		runs = append(runs, toWireRunReport(run))
	}
	return wireTestReport{
		TestID:      t.TestID,
		Description: t.Description,
		Layer:       string(t.Layer),
		Aggregate:   toWireAggregate(t.Aggregate),
		Runs:        runs,
	}
}

func toWireAggregate(a domain.AggregateResult) wireAggregate {
	return wireAggregate{
		TestID:                a.TestID,
		Verdict:               string(a.Verdict),
		Reasons:               reasonStrings(a.Reasons),
		Counted:               a.Counted,
		Passed:                a.Passed,
		Excluded:              a.Excluded,
		PassRate:              a.PassRate,
		InfrastructureFailure: a.InfrastructureFailure,
		TotalCost:             toWireCost(a.TotalCost),
	}
}

func toWireRunReport(r RunReport) wireRunReport {
	return wireRunReport{
		Run: wireRunKey{
			RunID:     r.Key.RunID,
			TestID:    r.Key.TestID,
			RunNumber: r.Key.RunNumber,
		},
		Verdict:         string(r.Verdict),
		Reasons:         reasonStrings(r.Reasons),
		Assertions:      assertionWires(r.Assertions),
		Conditions:      conditionWires(r.Conditions),
		DurationMS:      r.Duration.Milliseconds(),
		Cost:            toWireCost(r.Cost),
		NegativeApplied: r.NegativeApplied,
	}
}

func toWireCost(c domain.CostReport) wireCost {
	return wireCost{
		TotalUSD:    c.TotalUSD,
		Attribution: string(c.Attribution),
		Detail:      c.Detail,
	}
}

func reasonStrings(reasons []domain.FailureReason) []string {
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		out = append(out, string(r))
	}
	return out
}

func assertionWires(assertions []domain.AssertionResult) []wireAssertion {
	out := make([]wireAssertion, 0, len(assertions))
	for _, a := range assertions {
		out = append(out, wireAssertion{
			Class:    string(a.Class),
			Target:   a.Target,
			Outcome:  string(a.Outcome),
			Expected: a.Expected,
			Actual:   a.Actual,
			Detail:   a.Detail,
		})
	}
	return out
}

func conditionWires(conditions []domain.RunCondition) []wireCondition {
	out := make([]wireCondition, 0, len(conditions))
	for _, c := range conditions {
		out = append(out, wireCondition{
			Kind:   string(c.Kind),
			Detail: c.Detail,
		})
	}
	return out
}
