package cli_test

import (
	"bytes"
	"context"
	"sync"
	"time"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// fakeSuiteRunner is a scripted cli.SuiteRunner. It never spawns anything
// and never touches a filesystem: it exists so command-surface, exit-code,
// streaming and cancellation behaviour can be driven and observed without a
// real suite.
type fakeSuiteRunner struct {
	mu sync.Mutex

	// events, when non-empty, are emitted through the sink in order before
	// Run returns.
	events []domain.ProgressEvent

	// result and err are returned verbatim.
	result report.Result
	err    error

	// block, when non-nil, makes Run wait for either ctx.Done() or block to
	// be closed before returning, so cancellation can be observed.
	block chan struct{}

	called    bool
	gotPlan   preflight.Plan
	gotCtx    context.Context
	cancelled bool
}

func (f *fakeSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error) {
	f.mu.Lock()
	f.called = true
	f.gotPlan = p
	f.gotCtx = ctx
	f.mu.Unlock()

	for _, ev := range f.events {
		sink.Emit(ev)
	}

	if f.block != nil {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = true
			f.mu.Unlock()
		case <-f.block:
		}
	}

	return f.result, f.err
}

func (f *fakeSuiteRunner) wasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func (f *fakeSuiteRunner) sawCancellation() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelled
}

// fakeSuiteFactory is a scripted cli.SuiteFactory. It records how many
// times it was called and with what RunConfig, so tests can assert the
// factory is called at most once, only on the `run` path, and with the
// resolved workspace root — "the factory was not called" is the
// observable form of AC15.6's "no workspace created" on the `validate`
// path and on a rejected pre-flight.
type fakeSuiteFactory struct {
	mu sync.Mutex

	// runner and err are returned verbatim from build.
	runner cli.SuiteRunner
	err    error

	calls  int
	gotCfg cli.RunConfig
}

func (f *fakeSuiteFactory) build(cfg cli.RunConfig) (cli.SuiteRunner, error) {
	f.mu.Lock()
	f.calls++
	f.gotCfg = cfg
	f.mu.Unlock()
	return f.runner, f.err
}

func (f *fakeSuiteFactory) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSuiteFactory) lastConfig() cli.RunConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotCfg
}

// scriptedPreflight returns a cli.PreflightFunc that always answers plan/rep
// and records the last Input it was called with into *captured, so a test
// can assert on exactly what the command surface parsed.
func scriptedPreflight(plan preflight.Plan, rep authoring.Report, captured *preflight.Input) cli.PreflightFunc {
	return func(in preflight.Input) (preflight.Plan, authoring.Report) {
		if captured != nil {
			*captured = in
		}
		return plan, rep
	}
}

// baseOptions builds an Options wired to bufs and the given collaborators,
// following the ecosystem's convention: a fully pre-wired dependency set,
// standard output for results, standard error for diagnostics.
//
// Suite is wrapped as a cli.SuiteFactory that ignores the RunConfig it is
// called with and always returns sr, nil — the common case for tests that
// don't assert on workspace-root threading or factory failure. Tests that
// need to observe the RunConfig a factory was called with, or need the
// factory to fail, use baseOptionsWithFactory instead.
func baseOptions(stdout, stderr *bytes.Buffer, pf cli.PreflightFunc, sr cli.SuiteRunner) cli.Options {
	return baseOptionsWithFactory(stdout, stderr, pf, func(cli.RunConfig) (cli.SuiteRunner, error) {
		return sr, nil
	})
}

// baseOptionsWithFactory is baseOptions for tests that supply their own
// cli.SuiteFactory directly — typically a *fakeSuiteFactory's build method
// — so they can assert on the RunConfig the factory was called with, how
// many times it was called, or a factory error.
func baseOptionsWithFactory(stdout, stderr *bytes.Buffer, pf cli.PreflightFunc, sf cli.SuiteFactory) cli.Options {
	return cli.Options{
		Preflight:      pf,
		Suite:          sf,
		Stdout:         stdout,
		Stderr:         stderr,
		WorkspaceRoot:  "workspace-root",
		FixtureRoot:    "fixture-root",
		DefaultHarness: "claude-code",
	}
}

// cleanReport is an authoring.Report with no diagnostics: a plan is usable.
func cleanReport() authoring.Report {
	return authoring.Report{}
}

// failingReport returns a report carrying several unrelated errors, so
// pre-flight-failure tests can assert every one of them surfaces in one run.
func failingReport() authoring.Report {
	var r authoring.Report
	r.Add(
		authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "missing-test-definition",
			Path:     "suite.yaml",
			Pointer:  "tests[1].path",
			Message:  "test definition \"tests/missing.test.yaml\" not found",
		},
		authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "unknown-collaborator",
			Path:     "tests/other.test.yaml",
			Pointer:  "assertions.task_messages[0].identity",
			Message:  "collaborator {Task planner} is not declared in the stub registry",
		},
		authoring.Diagnostic{
			Severity: authoring.SeverityError,
			Code:     "unresolvable-ref",
			Path:     "tests/other.test.yaml",
			Pointer:  "seed_files[0].ref",
			Message:  "referenced fixture \"fixtures/missing.md\" does not exist",
		},
	)
	return r
}

// passingResult is a report.Result whose single test passed cleanly.
func passingResult(suiteID string) report.Result {
	return report.Build(suiteID, time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "happy-path",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:   "happy-path",
				Verdict:  domain.VerdictPass,
				Counted:  1,
				Passed:   1,
				PassRate: 1.0,
				TotalCost: domain.CostReport{
					TotalUSD:    0.42,
					Attribution: domain.AttributionAttributed,
				},
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "happy-path-1", TestName: "happy-path", RunNumber: 1},
					Verdict: domain.VerdictPass,
					Cost:    domain.CostReport{TotalUSD: 0.42, Attribution: domain.AttributionAttributed},
				},
			},
		},
	}, "")
}

// failedTestResult is a report.Result whose single test failed an
// assertion — the subject regressed, not the tool.
func failedTestResult(suiteID string) report.Result {
	return report.Build(suiteID, time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "regression",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:   "regression",
				Verdict:  domain.VerdictFail,
				Reasons:  []domain.FailureReason{domain.ReasonAssertion},
				Counted:  1,
				Passed:   0,
				PassRate: 0,
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "regression-1", TestName: "regression", RunNumber: 1},
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonAssertion},
					Assertions: []domain.AssertionResult{
						{
							Class:    domain.ClassFinalStatus,
							Outcome:  domain.AssertionFail,
							Expected: "SUCCESS",
							Actual:   "BLOCKED",
						},
					},
				},
			},
		},
	}, "")
}

// infrastructureFaultResult is a report.Result whose single test ended by a
// repeated state-integrity fault: a statement about the tool, not the
// subject, and never a FAIL verdict on its own.
func infrastructureFaultResult(suiteID string) report.Result {
	return report.Build(suiteID, time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "flaky-infra",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:                "flaky-infra",
				Verdict:               domain.VerdictFail,
				Reasons:               []domain.FailureReason{domain.ReasonStateIntegrity},
				Counted:               0,
				Excluded:              2,
				InfrastructureFailure: true,
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "flaky-infra-1", TestName: "flaky-infra", RunNumber: 1},
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonStateIntegrity},
				},
			},
		},
	}, "")
}

// infrastructureFaultResultFromRunnerError is a report.Result whose single
// test ended by a runner error — a single infrastructure-reason result, never
// requiring a recurrence the way a state-integrity fault does. It exercises
// the same exit-code path infrastructureFaultResult does, from the other
// stage-1 guard.
func infrastructureFaultResultFromRunnerError(suiteID string) report.Result {
	return report.Build(suiteID, time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "unspawnable-subject",
			Layer:  domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:                "unspawnable-subject",
				Verdict:               domain.VerdictFail,
				Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
				Counted:               1,
				Passed:                0,
				InfrastructureFailure: true,
			},
			Runs: []report.RunReport{
				{
					Key:     domain.RunKey{RunID: "unspawnable-subject-1", TestName: "unspawnable-subject", RunNumber: 1},
					Verdict: domain.VerdictFail,
					Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
					Conditions: []domain.RunCondition{
						{Kind: domain.ConditionRunNotStarted, Detail: "spawn-plan failed: exec: \"claude\": executable file not found in $PATH"},
					},
				},
			},
		},
	}, "")
}

func durationPtr(d time.Duration) *time.Duration { return &d }
func intPtr(i int) *int                          { return &i }
