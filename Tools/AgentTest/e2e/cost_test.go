package e2e

// T18.9 — the cost end-to-end test: a run with a well-formed log tree
// reports a cost alongside its verdict, and a run whose events landed in
// the fallback attribution bucket is reported as a cost-attribution failure
// rather than as a cost of zero.
//
// internal/runner.BuildEvidence's Cost field is genuinely wired end to end
// already (unlike protocol violations/concurrency, which needed a Coder
// session earlier in this stage): it is whatever Deps.Cost.Cost returns,
// unconditionally. This scenario supplies its own domain.CostProvider
// double per Scenario.Cost — internal/cost itself (which shells out to the
// log-analyzer tool) is out of scope for an end-to-end suite that must stay
// free of external processes.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// fixedCostProvider reports whatever CostReport it was constructed with,
// regardless of the query — this scenario only needs to prove the reported
// value travels end to end, not that a real query is interpreted correctly
// (internal/cost's own package tests cover that).
type fixedCostProvider struct{ report domain.CostReport }

func (p fixedCostProvider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	return p.report, nil
}

func costCheckScript() []subjectStep {
	return []subjectStep{
		invoke("dispatch", "effecter"),
		finish(`{"agent_instance_id":"standin-subject#1","run_id":"cost-run","status_code":"SUCCESS","status_message":"Done."}`),
	}
}

func TestCost_WellFormedLog_ReportsCostAlongsideVerdict(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/cost",
		SuitePath: "cost.suite.yaml",
		Script:    fakeOptionsFor(domain.HarnessCapabilities{SupportsDirectSubstitution: true}, costCheckScript()),
		Cost:      fixedCostProvider{report: domain.CostReport{TotalUSD: 1.25, Attribution: domain.AttributionAttributed}},
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "cost-check", domain.VerdictPass, cli.ExitSuccess)

	run := out.Result.Tests[0].Runs[0]
	if run.Cost.Attribution != string(domain.AttributionAttributed) {
		t.Errorf("run.Cost.Attribution = %q, want %q\nstdout: %s", run.Cost.Attribution, domain.AttributionAttributed, out.Stdout)
	}
	if run.Cost.TotalUSD != 1.25 {
		t.Errorf("run.Cost.TotalUSD = %v, want 1.25\nstdout: %s", run.Cost.TotalUSD, out.Stdout)
	}
	for _, c := range run.Conditions {
		if c.Kind == "cost_unattributed" {
			t.Errorf("run.Conditions contains cost_unattributed for a genuinely attributed cost: %+v\nstdout: %s", run.Conditions, out.Stdout)
		}
	}
}

func TestCost_AttributionMiss_ReportedAsFailureNotZero(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/cost",
		SuitePath: "cost.suite.yaml",
		Script:    fakeOptionsFor(domain.HarnessCapabilities{SupportsDirectSubstitution: true}, costCheckScript()),
		Cost:      fixedCostProvider{report: domain.CostReport{TotalUSD: 0, Attribution: domain.AttributionUnavailable, Detail: "log root missing"}},
	}

	out := RunScenario(t, sc)

	// The assertion suite (artifact_created only) still passes: an
	// attribution miss must be surfaced as its own condition, not folded
	// into the verdict as an ordinary failure — the same principle
	// evaluateConditions documents for every reportable condition.
	assertSingleTestVerdict(t, out, "cost-check", domain.VerdictPass, cli.ExitSuccess)

	run := out.Result.Tests[0].Runs[0]
	if run.Cost.Attribution == string(domain.AttributionAttributed) {
		t.Errorf("run.Cost.Attribution = %q, want anything but %q for an unavailable cost\nstdout: %s",
			run.Cost.Attribution, domain.AttributionAttributed, out.Stdout)
	}

	var sawCondition bool
	for _, c := range run.Conditions {
		if c.Kind == "cost_unattributed" {
			sawCondition = true
		}
	}
	if !sawCondition {
		t.Errorf("run.Conditions = %+v, want a cost_unattributed condition — an attribution miss must never be reported as a silent cost of zero\nstdout: %s",
			run.Conditions, out.Stdout)
	}
}
