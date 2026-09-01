package e2e

// Cost end-to-end tests.
//
// The original two tests (T18.9) verify that a run with a well-formed log
// tree reports a cost alongside its verdict, and that an attribution miss is
// reported as a condition rather than as a silent cost of zero.
//
// The Stage 8 tests (T8.5) verify the end-to-end journey of the non-zero
// attributed figure from a domain.CostProvider double through the full
// pipeline to the reported run result. They also pin that the runner now
// passes LogsRoot in the cost query (I8.3), so the provider receives the
// OrchestrationLogs parent directory rather than only the per-run folder.
//
// All tests supply a domain.CostProvider double. The real internal/cost
// provider (which shells out to the log-analyzer tool) is out of scope for
// an end-to-end suite that must stay free of external processes.

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

// --------------------------------------------------------------------------
// Stage 8 (T8.5): the cost figure's journey through the full pipeline
// --------------------------------------------------------------------------

// capturingCostProvider records the CostQuery it receives AND returns a
// configured CostReport, so a test can assert both what was queried and what
// was reported.
type capturingCostProvider struct {
	report    domain.CostReport
	lastQuery domain.CostQuery
}

func (p *capturingCostProvider) Cost(ctx context.Context, q domain.CostQuery) (domain.CostReport, error) {
	p.lastQuery = q
	return p.report, nil
}

// TestCost_RunnerPassesLogsRootInCostQuery asserts that the runner populates
// CostQuery.LogsRoot when it calls the cost provider, so the provider can
// scan the full OrchestrationLogs tree (including sibling buckets) rather
// than only the per-run folder.
//
// This test fails before I8.3 populates LogsRoot in the cost query: the
// runner currently passes only LogRoot and RunID.
func TestCost_RunnerPassesLogsRootInCostQuery(t *testing.T) {
	cap := &capturingCostProvider{
		report: domain.CostReport{TotalUSD: 0.50, Attribution: domain.AttributionAttributed},
	}
	sc := Scenario{
		Dir:       "testdata/e2e/cost",
		SuitePath: "cost.suite.yaml",
		Script:    fakeOptionsFor(domain.HarnessCapabilities{SupportsDirectSubstitution: true}, costCheckScript()),
		Cost:      cap,
	}

	RunScenario(t, sc)

	if cap.lastQuery.LogsRoot == "" {
		t.Error(
			"CostQuery.LogsRoot is empty, want a non-empty value — " +
				"the runner must pass the OrchestrationLogs parent directory so the provider can detect sibling buckets",
		)
	}
	// LogsRoot must be the PARENT of LogRoot (one directory up), not equal to it.
	if cap.lastQuery.LogRoot == cap.lastQuery.LogsRoot {
		t.Errorf(
			"CostQuery.LogRoot == CostQuery.LogsRoot == %q, but they must be different — "+
				"LogRoot is the per-run subfolder and LogsRoot is its parent directory",
			cap.lastQuery.LogRoot,
		)
	}
}

// TestCost_AttributedNonZeroCostReachesRunResult_WhenLogsRootIsPopulated
// asserts that a non-zero attributed cost returned by the provider (which now
// receives LogsRoot in the query) reaches the run result intact. This is the
// end-to-end journey: provider → runner.BuildEvidence → TestResult → report.
//
// Together with TestCost_RunnerPassesLogsRootInCostQuery, this closes the
// claim that a protocol-compliant run's cost figure survives the journey from
// provider to reported result rather than silently becoming $0.00 (unavailable).
func TestCost_AttributedNonZeroCostReachesRunResult_WhenLogsRootIsPopulated(t *testing.T) {
	const wantTotal = 0.75

	cap := &capturingCostProvider{
		report: domain.CostReport{TotalUSD: wantTotal, Attribution: domain.AttributionAttributed},
	}
	sc := Scenario{
		Dir:       "testdata/e2e/cost",
		SuitePath: "cost.suite.yaml",
		Script:    fakeOptionsFor(domain.HarnessCapabilities{SupportsDirectSubstitution: true}, costCheckScript()),
		Cost:      cap,
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "cost-check", domain.VerdictPass, cli.ExitSuccess)

	run := out.Result.Tests[0].Runs[0]
	if run.Cost.Attribution != string(domain.AttributionAttributed) {
		t.Errorf(
			"run.Cost.Attribution = %q, want %q — "+
				"the attributed cost must survive the journey from provider to reported result",
			run.Cost.Attribution, domain.AttributionAttributed,
		)
	}
	if run.Cost.TotalUSD != wantTotal {
		t.Errorf(
			"run.Cost.TotalUSD = %v, want %v — "+
				"the non-zero figure must reach the run result rather than becoming $0.00 (unavailable)",
			run.Cost.TotalUSD, wantTotal,
		)
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
