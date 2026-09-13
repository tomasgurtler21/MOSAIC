// Package suite owns suite-level orchestration: scheduling a suite's tests
// in deterministic order, running each test's declared repetitions,
// delegating per-repetition evaluation and pass-rate aggregation to the
// verdict engine, and applying the state-integrity retry-and-exclude rule.
// It is the sole producer of the progress-event stream.
package suite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// TestRunner is the per-attempt use case, behind an interface so the suite's
// scheduling, repetition and retry rules are testable without a sandbox.
// Implemented by internal/runner.
//
// Run owns the whole life of one attempt's sandbox: it creates it, runs the
// subject in it, calls eval with the gathered evidence while the sandbox is
// still on disk, and only then tears down — so the retention decision can
// account for the verdict eval produced. The caller never triggers teardown
// and therefore cannot forget to.
//
// eval is called at most once, and exactly once on every path where evidence
// was gathered. It is called synchronously on Run's own goroutine, before
// teardown and while the sandbox is still on disk, and it completes before
// the Run call that invoked it returns.
//
// What changed, and what a caller may no longer assume: under a bound above
// one, many Run calls execute on many goroutines at once, so one caller's
// eval closure may be entered concurrently with other entries of itself. A
// closure that emits progress or accumulates state must be safe for
// concurrent use. What remains guaranteed is per-call, not global: this
// call's eval finishes before this call returns, and this call's sandbox is
// intact throughout it.
//
// A nil eval means "no verdict is wanted": the attempt still runs and still
// tears down, the retention decision falls back to the launch-failure signal
// alone, and the returned TestResult is zero-valued. Only test doubles that
// do not exercise evaluation pass nil; the composition root never does.
//
// The returned TestResult is eval's own return value with one field resolved
// by Run after teardown: RetainedSandboxPath, which is only knowable once the
// retention decision has been carried out. Run is the single place that value
// is resolved onto the result.
//
// The error return is reserved for a fault that prevented the attempt from
// starting at all — a setup failure, a spawn-plan failure or a recovered
// panic. Teardown has still run when it is returned, and eval has not been
// called: no evidence exists to evaluate.
type TestRunner interface {
	Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error)
}

// Options are the collaborators and knobs one suite run needs.
type Options struct {
	Runner   TestRunner
	Progress domain.ProgressSink
	Clock    domain.Clock

	// MaxConcurrentRuns bounds how many repetitions of a single test execute
	// concurrently during the fan-out phase (reps 2..N after the warm-up
	// repetition completes). Tests within a suite execute one at a time: all
	// repetitions of one test complete before any repetition of the next test
	// starts. Within one test's fan-out the bound caps simultaneous in-flight
	// repetitions; it no longer spans the full (test x repetition) matrix.
	//
	// 1 is strictly sequential: the warm-up rep runs alone and the single
	// fan-out slot means each remaining rep also runs alone in order, giving
	// identical behaviour, results and lifecycle-event ordering to the
	// pre-concurrency sequential loop.
	//
	// It replaces MaxConcurrentTests, which was declared, never read, and
	// carried a rationale for sequential repetitions ("overlapping samples
	// make a burst of load part of what is being measured") that does not
	// apply here: this tool asserts routing decisions, not latency or
	// throughput under load.
	//
	// Deliberately not named for "concurrency": internal/concurrency already
	// means peak collaborator invocations in flight within one run, which is
	// a different thing entirely.
	//
	// Zero selects DefaultMaxConcurrentRuns.
	MaxConcurrentRuns int

	// RunID authors the run identity for one attempt. The third
	// argument is the zero-based attempt index within the repetition:
	// 0 for the original attempt, 1 for the state-integrity retry.
	// Injected so evidence, log locations and golden reports are
	// reproducible in tests; each call for the same (testID,
	// runNumber) pair must produce a different id for a different
	// attempt, so the retry can get its own sandbox.
	//
	// Contract: whatever this returns MUST satisfy domain.ValidRunID,
	// because the logger bundle silently buckets anything else under
	// "unknown-run" and cost attribution then fails for a reason no
	// message names.
	RunID func(testID string, runNumber, attempt int) string

	// Retention is the run-level sandbox retention policy, decided by the
	// frontend. The concrete TestRunner this Suite is bound to (see
	// internal/runner and the composition root's wiring) is the one that
	// actually carries it into each attempt's runner.Request.Retention: a
	// TestRunner implementation binds Retention at construction alongside
	// its other per-invocation configuration, the same way it already binds
	// SelfPath and LoggerBundleDir. Recorded here so the value the suite was
	// configured with and the value reaching the runner are the same one,
	// checkable field-for-field rather than trusted by comment.
	Retention domain.RetentionPolicy

	// ToolVersion is the version of the mosaic-agent-test binary for this
	// invocation. Set on the Result returned by report.Build at the production
	// call site (Run method), not by Build itself, so Build's parameter list
	// remains unchanged. Empty means not recorded (test doubles that do not
	// supply a version, or pre-feature invocations).
	ToolVersion string

	// Pause, when non-nil, is checked by workers before picking up each new
	// work item. When paused, workers block until resumed or the context is
	// cancelled. When nil, workers never pause (backward compatible).
	//
	// The caller retains a reference to the same PauseControl it passes here,
	// enabling external triggering (TUI keybinding, CLI signal) without the
	// suite needing to know about frontends.
	Pause *PauseControl
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

// StateIntegrityRetries is fixed at one by design. A run that triggers the
// retry-and-exclude rule — a lock reclaim (state-integrity fault) or a
// non-zero harness exit (spawn-failed) — may rest on lost or absent subject
// state, so it is evidence about the tool rather than about the subject: it
// is retried once and excluded from the pass-rate denominator, and a second
// occurrence of the same fault ends the test as an infrastructure failure.
// Letting the tool's own faults count against the subject would stop the
// aggregate measuring the subject at all.
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

// testSlot holds the pre-allocated result buffers for one test's repetitions.
// All fields are written before scheduling and read after wg.Wait(). Each
// slice element is written by exactly one worker (the one that claimed that
// repIdx work item), so concurrent writes are always to non-overlapping
// positions and Go's memory model makes them race-free without a mutex.
// wg.Wait() provides the happens-before edge that makes every worker's writes
// visible to the assembler goroutine that reads them afterwards.
type testSlot struct {
	rt          preflight.ResolvedTest
	repetitions int
	passRate    float64
	allResults  [][]domain.TestResult // [rep-1] = every attempt result for that repetition
	runReports  []report.RunReport    // [rep-1] = the run report for that repetition
}

// workItem is one scheduling unit: one repetition of one test.
type workItem struct {
	slotIdx int // index into the slots array (= test index in plan order)
	repIdx  int // 0-based (rep - 1)
	rep     int // 1-based repetition number
}

// Run executes a validated plan and returns the single result model both
// renderers and both frontends consume.
//
// Scheduling. Tests execute one at a time in plan order. For each test, the
// first repetition (rep 1, the warm-up) runs alone and must complete before
// any of the remaining repetitions start. After the warm-up completes, reps
// 2..N fan out concurrently, bounded by Options.MaxConcurrentRuns. The next
// test only starts after all repetitions of the current test have completed.
// No provider- or model-conditional branching affects this ordering: the
// warm-up-then-fan-out sequence applies uniformly to every test.
//
// Ordering. Every result is written into a slot pre-allocated by (test index,
// repetition number) before scheduling begins, never appended on completion.
// Report ordering is therefore by test plan order and repetition number
// regardless of fan-out completion order, which is what the report builder
// already assumes when it consumes reports in the order the suite produced
// them.
//
// Identity and isolation. Each repetition's attempts receive distinct run
// identities and therefore distinct sandboxes, distinct run-state files,
// distinct locks and distinct invocation logs. No file, counter or lock is
// shared across repetitions, so two fan-out reps executing at the same instant
// can neither observe nor corrupt each other's evidence.
//
// Cancellation. Run returns only after every repetition it started has
// returned. Cancellation during a warm-up prevents fan-out reps from being
// dispatched; reps already in flight receive the same ctx and complete their
// own guaranteed teardown. Results completed before cancellation are returned
// rather than discarded, and a cancelled suite is not itself a failure.
//
// A bound of 1 is strictly sequential: the warm-up rep runs alone, and the
// single fan-out slot means each remaining rep also runs alone in order,
// producing identical behaviour, results and lifecycle-event ordering to the
// pre-concurrency sequential loop.
func (s *Suite) Run(ctx context.Context, p preflight.Plan) (report.Result, error) {
	clock := s.opts.Clock
	sink := s.opts.Progress

	started := clock.Now()

	// totalRuns is the number of runs this plan implies, derived once from
	// the plan so the suite-started event and every frontend agree on the count.
	totalRuns := p.TotalRuns()

	emitSafe(sink, domain.ProgressEvent{
		Kind:       domain.ProgressSuiteStarted,
		At:         started,
		SuiteID:    p.Suite.ID,
		TotalTests: len(p.Tests),
		TotalRuns:  totalRuns,
	})

	bound := s.opts.MaxConcurrentRuns
	if bound <= 0 {
		// Zero means unset: apply the package default so that the exported
		// field contract ("Zero selects DefaultMaxConcurrentRuns") is honoured.
		// Callers that want strictly sequential execution must supply bound=1
		// explicitly.
		bound = DefaultMaxConcurrentRuns
	}

	// Pre-allocate result slots in plan order. Positions are fixed before
	// any goroutine starts so completion order cannot affect the final
	// report ordering.
	slots := make([]testSlot, len(p.Tests))
	for i, rt := range p.Tests {
		reps := 1
		if rt.Settings.Repetitions != nil {
			reps = *rt.Settings.Repetitions
		}
		rate := 1.0
		if rt.Settings.PassRate != nil {
			rate = *rt.Settings.PassRate
		}
		slots[i] = testSlot{
			rt:          rt,
			repetitions: reps,
			passRate:    rate,
			allResults:  make([][]domain.TestResult, reps),
			runReports:  make([]report.RunReport, reps),
		}
	}

	// recordResult writes the final result and run report for one repetition
	// into its pre-allocated slot position, then emits a ProgressTestFinished
	// event. Each repIdx is owned by exactly one call path, so concurrent
	// fan-out goroutines always write to non-overlapping positions — no mutex
	// needed. fanOutWg.Wait() provides the happens-before edge that makes
	// fan-out writes visible to the assembler goroutine that reads them.
	recordResult := func(slot *testSlot, item workItem, final domain.TestResult, attempts []domain.TestResult) {
		slot.allResults[item.repIdx] = attempts
		slot.runReports[item.repIdx] = report.RunReport{
			Key:                 final.Key,
			Verdict:             final.Verdict,
			Reasons:             final.Reasons,
			Assertions:          final.Assertions,
			Conditions:          final.Conditions,
			Duration:            final.Duration,
			Cost:                final.Cost,
			NegativeApplied:     final.NegativeApplied,
			RetainedSandboxPath: final.RetainedSandboxPath,
			SubjectVersion:      final.SubjectVersion,
			SubjectModel:        final.SubjectModel,
			StubModel:           final.StubModel,
			HarnessID:           final.HarnessID,
			TerminationReason:   final.TerminationReason,
			TestVersion:         final.Version,
			NumericID:           final.NumericID,
			Subject: report.SubjectFailure{
				ExitCode:  final.SubjectResult.ExitCode,
				Stderr:    final.SubjectResult.Stderr,
				RawOutput: final.SubjectResult.RawOutput,
			},
		}
		emitSafe(sink, domain.ProgressEvent{
			Kind:             domain.ProgressTestFinished,
			At:               clock.Now(),
			TestID:           slot.rt.Definition.Name,
			Repetition:       item.rep,
			Repetitions:      slot.repetitions,
			Verdict:          final.Verdict,
			Duration:         final.Duration,
			Cost:             final.Cost,
			FailedAssertions: failedAssertionNames(final.Assertions),
			Run:              final.Key,
		})
	}

	// Execute tests one at a time in plan order. For each test:
	//   - Rep 1 (warm-up) runs alone; no fan-out rep may start before it
	//     completes.
	//   - After the warm-up completes, reps 2..N fan out concurrently,
	//     bounded by Options.MaxConcurrentRuns.
	//   - The next test begins only after all repetitions of the current
	//     test have completed.
	for i := range slots {
		slot := &slots[i]

		// Respect cancellation: stop scheduling further tests.
		if ctx.Err() != nil {
			break
		}

		// Block while paused. If ctx is cancelled during the wait, stop
		// scheduling. Pause is independent of ctx: in-flight runs are
		// unaffected by Pause, only new scheduling points are blocked.
		if s.opts.Pause != nil {
			if err := s.opts.Pause.WaitIfPaused(ctx); err != nil {
				break // context cancelled while paused
			}
		}

		// --- Warm-up: rep 1 runs alone. ---
		// Rep 1 must complete before any fan-out rep may be dispatched.
		warmUpItem := workItem{slotIdx: i, repIdx: 0, rep: 1}
		warmFinal, warmAttempts := s.executeWork(ctx, warmUpItem, slot, sink, clock)
		recordResult(slot, warmUpItem, warmFinal, warmAttempts)

		// Skip fan-out for a single-rep test, or if ctx was cancelled
		// during the warm-up (the fan-out must never be dispatched after
		// cancellation — the warm-up result is already recorded above).
		if slot.repetitions <= 1 || ctx.Err() != nil {
			continue
		}

		// --- Fan-out: reps 2..N concurrently, bounded by MaxConcurrentRuns. ---
		// The semaphore caps simultaneously in-flight fan-out reps. Each
		// goroutine releases its slot when its executeWork call returns.
		sem := make(chan struct{}, bound)
		var fanOutWg sync.WaitGroup

	fanOutLoop:
		for rep := 2; rep <= slot.repetitions; rep++ {
			// Respect cancellation: stop dispatching fan-out reps.
			if ctx.Err() != nil {
				break fanOutLoop
			}

			// Pause interop before dispatching each fan-out rep.
			if s.opts.Pause != nil {
				if err := s.opts.Pause.WaitIfPaused(ctx); err != nil {
					break fanOutLoop // context cancelled while paused
				}
			}

			// Acquire a semaphore slot, blocking until one is free or
			// ctx is cancelled.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break fanOutLoop
			}

			item := workItem{slotIdx: i, repIdx: rep - 1, rep: rep}
			fanOutWg.Add(1)
			go func(item workItem) {
				defer func() {
					<-sem
					fanOutWg.Done()
				}()
				f, att := s.executeWork(ctx, item, slot, sink, clock)
				recordResult(slot, item, f, att)
			}(item)
		}

		// Wait for every dispatched fan-out goroutine to return before
		// moving to the next test.
		fanOutWg.Wait()
	}

	// Assemble test reports from the pre-allocated slots, in plan order.
	// Repetitions that were cancelled before execution have a nil allResults
	// entry (the slot was pre-allocated but never written). Those are excluded
	// from the report so the output matches the old sequential implementation,
	// which simply produced fewer entries when cancelled mid-run rather than
	// including zero-valued placeholders.
	testReports := make([]report.TestReport, len(slots))
	for i, slot := range slots {
		var allResults []domain.TestResult
		var runReports []report.RunReport
		for repIdx, attempts := range slot.allResults {
			if attempts == nil {
				// This repetition was not executed (context cancelled before
				// the worker reached it). Omit it from the report.
				continue
			}
			allResults = append(allResults, attempts...)
			runReports = append(runReports, slot.runReports[repIdx])
		}
		agg := evaluate.Aggregate(allResults, domain.RepetitionPolicy{
			Repetitions: slot.repetitions,
			PassRate:    slot.passRate,
		})
		testReports[i] = report.TestReport{
			TestName:    slot.rt.Definition.Name,
			NumericID:   agg.NumericID,
			Description: slot.rt.Definition.Description,
			Layer:       slot.rt.Definition.Layer,
			Aggregate:   agg,
			Runs:        runReports,
		}
	}

	finished := clock.Now()
	result := report.Build(p.Suite.ID, started, finished, testReports, p.CatalogFolder)
	result.ToolVersion = s.opts.ToolVersion

	emitSafe(sink, domain.ProgressEvent{
		Kind:      domain.ProgressSuiteFinished,
		At:        finished,
		Counts:    result.Counts,
		TotalCost: result.TotalCost,
	})

	return result, nil
}

// runRepetition executes one declared repetition, applying the
// retry-and-exclude rule: a run that fails for state integrity or exits
// with a non-zero harness exit (spawn-failed) is retried once, and a
// second such fault of the same kind in the same repetition ends it as
// evidence the verdict engine's aggregation marks as an infrastructure
// failure. It returns the repetition's final result —
// what the displayed report and the ProgressTestFinished event carry — plus
// every raw attempt's result, so the excluded attempt still reaches the
// verdict engine's aggregation.
//
// ProgressTestStarted is emitted here at attempt 0, after the run key is
// computed, so the event carries the same run identity as the invocation
// events it opens — without calling RunID a second time.
//
// runRepetition is safe for concurrent calls from many goroutines: it
// writes only to its own stack and through the emitSafe sink, whose own
// goroutine isolation makes concurrent callers safe.
func (s *Suite) runRepetition(ctx context.Context, rt preflight.ResolvedTest, rep, repetitions int, sink domain.ProgressSink, clock domain.Clock) (domain.TestResult, []domain.TestResult) {
	var attempts []domain.TestResult
	var final domain.TestResult

	for attempt := 0; attempt <= StateIntegrityRetries; attempt++ {
		key := domain.RunKey{RunID: s.runID(rt.Definition.Name, rep, attempt), TestName: rt.Definition.Name, RunNumber: rep}

		// Emit ProgressTestStarted for the first attempt only. The key is
		// already in scope so the event carries the same run identity the
		// invocation events inside the evaluator closure will carry.
		if attempt == 0 {
			emitSafe(sink, domain.ProgressEvent{
				Kind:        domain.ProgressTestStarted,
				At:          clock.Now(),
				TestID:      rt.Definition.Name,
				Repetition:  rep,
				Repetitions: repetitions,
				Run:         key,
			})
		}

		// The evaluator closes over key and sink so it stamps the run key
		// and emits invocation progress events from inside the runner, while
		// the sandbox is still on disk. The runner calls it synchronously
		// before teardown, so all side effects are visible to the caller by
		// the time Run returns.
		//
		// Under a bound above one, many such closures execute concurrently on
		// many goroutines. emitSafe's per-event goroutine isolation makes the
		// sink writes safe; the key and sink captures are read-only within the
		// closure. No shared mutable state is accessed.
		eval := func(ev domain.RunEvidence) domain.TestResult {
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
					Run:      key,
				})
			}
			return evaluate.Evaluate(ev)
		}

		result, err := s.opts.Runner.Run(ctx, key, rt, eval)
		if err != nil {
			// Nothing ran: no evidence exists to evaluate, so none is
			// evaluated. The runner's error is the only evidence there is,
			// and it must never be discarded in favor of a zero-valued
			// evidence set reading as a pass. This ends the retry loop for
			// this attempt — retrying against the same unstarted-run fault
			// would not help, unlike the state-integrity retry rule, which
			// only fires when a run actually happened.
			result = domain.TestResult{
				Key:     key,
				Verdict: domain.VerdictFail,
				Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
				Conditions: []domain.RunCondition{{
					Kind:   domain.ConditionRunNotStarted,
					Detail: err.Error(),
				}},
			}
			attempts = append(attempts, result)
			final = result
			break
		}
		attempts = append(attempts, result)
		final = result

		if !evaluate.NeedsRetry(result) {
			break
		}
	}

	return final, attempts
}

// executeWork runs one scheduling unit and catches any panic that escapes
// runRepetition. If a panic occurs, it returns an infrastructure failure result
// so the worker goroutine is not terminated — per-goroutine containment is
// what makes "a panic in one unit does not affect sibling units" true at the
// scheduler level. runner.Run already recovers panics inside its own scope;
// this recovery catches any panic that somehow escapes that inner recovery,
// preventing the scheduler goroutine from crashing the process.
func (s *Suite) executeWork(ctx context.Context, item workItem, slot *testSlot, sink domain.ProgressSink, clock domain.Clock) (final domain.TestResult, attempts []domain.TestResult) {
	defer func() {
		if r := recover(); r != nil {
			key := domain.RunKey{
				RunID:     s.runID(slot.rt.Definition.Name, item.rep, 0),
				TestName:  slot.rt.Definition.Name,
				RunNumber: item.rep,
			}
			final = domain.TestResult{
				Key:     key,
				Verdict: domain.VerdictFail,
				Reasons: []domain.FailureReason{domain.ReasonInfrastructure},
				Conditions: []domain.RunCondition{{
					Kind:   domain.ConditionRunNotStarted,
					Detail: fmt.Sprintf("runner panicked: %v", r),
				}},
			}
			attempts = []domain.TestResult{final}
		}
	}()
	return s.runRepetition(ctx, slot.rt, item.rep, slot.repetitions, sink, clock)
}

func (s *Suite) runID(testID string, runNumber, attempt int) string {
	if s.opts.RunID != nil {
		return s.opts.RunID(testID, runNumber, attempt)
	}
	return defaultRunID(s.opts.Clock, testID, runNumber, attempt)
}

// defaultRunID is the package's own run-identity generator, used whenever
// Options.RunID is left nil. It produces an id in the shape
// domain.FormatRunID renders: the instant comes from clock, so the id is
// deterministic under an injected clock, and the four-hex-digit suffix is
// derived from (testID, runNumber, attempt) so two different tests, two
// repetitions of the same test, or two attempts of the same repetition —
// even under a clock that has not advanced between them — never collide.
func defaultRunID(clock domain.Clock, testID string, runNumber, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d-%d", testID, runNumber, attempt)))
	suffix := hex.EncodeToString(sum[:2])
	return domain.FormatRunID(clock.Now(), suffix)
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
