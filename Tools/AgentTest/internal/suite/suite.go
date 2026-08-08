// Package suite owns suite-level orchestration: scheduling a suite's tests
// in deterministic order, running each test's declared repetitions,
// delegating per-repetition evaluation and pass-rate aggregation to the
// verdict engine, and applying the state-integrity retry-and-exclude rule.
// It is the sole producer of the progress-event stream.
package suite

import (
	"context"
	"fmt"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// TestRunner is the per-attempt use case, behind an interface so the
// suite's scheduling, repetition and retry rules are testable without a
// sandbox. Implemented by internal/runner.
type TestRunner interface {
	Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest) (domain.RunEvidence, error)
}

// Options are the collaborators and knobs one suite run needs.
type Options struct {
	Runner   TestRunner
	Progress domain.ProgressSink
	Clock    domain.Clock

	// MaxConcurrentTests bounds how many tests execute at once. 1, the
	// default, is sequential. Concurrency here is across tests; a single
	// test's repetitions always run in order, because a repetition's
	// purpose is to sample a non-deterministic subject and overlapping
	// samples make a burst of load part of what is being measured.
	MaxConcurrentTests int

	// RunID authors the run identity for one attempt. Injected so
	// evidence, log locations and golden reports are reproducible in
	// tests.
	RunID func(testID string, runNumber int) string
}

// Suite runs a validated plan and renders the single result model both
// renderers and both frontends consume.
type Suite struct {
	opts Options
}

// New constructs a Suite bound to o.
func New(o Options) *Suite {
	return &Suite{opts: o}
}

// StateIntegrityRetries is fixed at one by design. A run whose lock was
// reclaimed may rest on lost state, so it is evidence about the tool rather
// than about the subject: it is retried once and excluded from the
// pass-rate denominator, and a second occurrence ends the test as an
// infrastructure failure. Letting the tool's own faults count against the
// subject would stop the aggregate measuring the subject at all.
const StateIntegrityRetries = 1

// emitTimeout bounds how long the suite waits for one progress event to be
// delivered before moving on to the next lifecycle point. A well-behaved
// sink delivers in microseconds, so the bound is invisible in practice and
// event order is preserved; a sink that is slow, permanently blocking or
// panics simply loses that one event without holding up the suite or
// changing any verdict — a display problem is not a test result.
const emitTimeout = 200 * time.Millisecond

// emitSafe delivers ev to sink on its own goroutine so a slow, blocking or
// failing sink can neither stall nor fail the suite. The caller waits only
// up to emitTimeout for delivery to complete.
func emitSafe(sink domain.ProgressSink, ev domain.ProgressEvent) {
	if sink == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			recover()
			close(done)
		}()
		sink.Emit(ev)
	}()
	select {
	case <-done:
	case <-time.After(emitTimeout):
	}
}

// Run executes a validated plan and returns the single result model both
// renderers and both frontends consume.
//
// Cancellation propagates to every in-flight attempt through ctx, so the
// per-test lifecycle's guaranteed teardown still runs. A cancelled suite
// returns the results it completed rather than discarding them.
func (s *Suite) Run(ctx context.Context, p preflight.Plan) (report.Result, error) {
	clock := s.opts.Clock
	sink := s.opts.Progress

	started := clock.Now()
	emitSafe(sink, domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		At:         started,
		SuiteID:    p.Suite.ID,
		TotalTests: len(p.Tests),
	})

	var testReports []report.TestReport
	for _, rt := range p.Tests {
		testReports = append(testReports, s.runTest(ctx, rt, sink, clock))
		if ctx.Err() != nil {
			// The completed results stand; a cancelled context is not
			// itself a suite failure, and no further test is scheduled.
			break
		}
	}

	finished := clock.Now()
	result := report.Build(p.Suite.ID, started, finished, testReports)

	emitSafe(sink, domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		At:        finished,
		Counts:    result.Counts,
		TotalCost: result.TotalCost,
	})

	return result, nil
}

// runTest executes one test's declared repetitions and delegates
// pass-rate aggregation to the verdict engine over every raw attempt
// (including any state-integrity retry), never recomputing it here.
func (s *Suite) runTest(ctx context.Context, rt preflight.ResolvedTest, sink domain.ProgressSink, clock domain.Clock) report.TestReport {
	testID := rt.Definition.ID

	repetitions := 1
	if rt.Settings.Repetitions != nil {
		repetitions = *rt.Settings.Repetitions
	}
	passRate := 1.0
	if rt.Settings.PassRate != nil {
		passRate = *rt.Settings.PassRate
	}

	var allResults []domain.TestResult
	var runReports []report.RunReport

	for rep := 1; rep <= repetitions; rep++ {
		emitSafe(sink, domain.ProgressEvent{
			Kind:        domain.ProgressTestStarted,
			At:          clock.Now(),
			TestID:      testID,
			Repetition:  rep,
			Repetitions: repetitions,
		})

		final, attempts := s.runRepetition(ctx, rt, rep, sink, clock)
		allResults = append(allResults, attempts...)
		runReports = append(runReports, report.RunReport{
			Key:             final.Key,
			Verdict:         final.Verdict,
			Reasons:         final.Reasons,
			Assertions:      final.Assertions,
			Conditions:      final.Conditions,
			Duration:        final.Duration,
			Cost:            final.Cost,
			NegativeApplied: final.NegativeApplied,
		})

		emitSafe(sink, domain.ProgressEvent{
			Kind:             domain.ProgressTestFinished,
			At:               clock.Now(),
			TestID:           testID,
			Repetition:       rep,
			Repetitions:      repetitions,
			Verdict:          final.Verdict,
			Duration:         final.Duration,
			Cost:             final.Cost,
			FailedAssertions: failedAssertionNames(final.Assertions),
		})
	}

	agg := evaluate.Aggregate(allResults, domain.RepetitionPolicy{Repetitions: repetitions, PassRate: passRate})

	return report.TestReport{
		TestID:      testID,
		Description: rt.Definition.Description,
		Layer:       rt.Definition.Layer,
		Aggregate:   agg,
		Runs:        runReports,
	}
}

// runRepetition executes one declared repetition, applying the
// state-integrity retry-and-exclude rule: a run that fails for state
// integrity is retried once, and a second such fault in the same
// repetition ends it as evidence the verdict engine's aggregation marks as
// an infrastructure failure. It returns the repetition's final result —
// what the displayed report and the ProgressTestFinished event carry — plus
// every raw attempt's result, so the excluded attempt still reaches the
// verdict engine's aggregation.
func (s *Suite) runRepetition(ctx context.Context, rt preflight.ResolvedTest, rep int, sink domain.ProgressSink, clock domain.Clock) (domain.TestResult, []domain.TestResult) {
	var attempts []domain.TestResult
	var final domain.TestResult

	for attempt := 0; attempt <= StateIntegrityRetries; attempt++ {
		key := domain.RunKey{RunID: s.runID(rt.Definition.ID, rep), TestID: rt.Definition.ID, RunNumber: rep}

		ev, _ := s.opts.Runner.Run(ctx, key, rt)
		ev.Key = key

		for _, rec := range ev.Records {
			if rec.Kind != domain.RecordStart {
				continue
			}
			emitSafe(sink, domain.ProgressEvent{
				Kind:     domain.ProgressInvocation,
				At:       clock.Now(),
				Seq:      rec.Seq,
				Identity: rec.Identity,
				Outcome:  rec.Outcome,
			})
		}

		result := evaluate.Evaluate(ev)
		attempts = append(attempts, result)
		final = result

		if !evaluate.NeedsRetry(result) {
			break
		}
	}

	return final, attempts
}

func (s *Suite) runID(testID string, runNumber int) string {
	if s.opts.RunID != nil {
		return s.opts.RunID(testID, runNumber)
	}
	return fmt.Sprintf("%s-%d", testID, runNumber)
}

// failedAssertionNames names the classes of assertions that failed, for the
// ProgressTestFinished event.
func failedAssertionNames(assertions []domain.AssertionResult) []string {
	var out []string
	for _, a := range assertions {
		if a.Outcome == domain.AssertionFail {
			out = append(out, string(a.Class))
		}
	}
	return out
}
