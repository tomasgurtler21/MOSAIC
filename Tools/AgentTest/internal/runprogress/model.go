// Package runprogress is the display-only projection of the progress-event
// stream that both frontends fold events into.
//
// It exists in one place, imported by both, so CLI/TUI equivalence for the
// multi-run display is structural rather than maintained in parallel and
// checked afterwards. The parity tests continue to hold the two frontends'
// settings surfaces equivalent; this makes their progress model equivalent
// by construction.
//
// Display-only, and never a source of truth. Counts, totals and pass rates
// are computed by the verdict engine's aggregation and the report builder,
// each in exactly one place, and nothing here may become a second.
package runprogress

import (
	"sort"
	"time"

	"mosaic-agent-test/internal/domain"
)

// RunProgress is one run's display state.
type RunProgress struct {
	Key domain.RunKey

	// Repetitions is the total repetition count declared for this run's test.
	Repetitions int

	// ObservedInvocations is how many invocation events were seen for this
	// run. Approximate and never authoritative: invocation events are the
	// only kind the async sink drops under saturation. Presented as observed,
	// not as a count.
	ObservedInvocations int

	// StartedAt is the timestamp from the ProgressTestStarted event.
	StartedAt time.Time
}

// Tally is the display-only running count of runs.
//
// Derived from lifecycle events alone, which are never dropped under sink
// saturation, so unlike an invocation count it is sound. It is a display
// figure and never a source of truth: the report's own counts and pass rates
// are computed by the verdict engine and the report builder, and the tally
// must never contradict them nor be substituted for them.
type Tally struct {
	Running  int
	Finished int

	// Remaining is TotalRuns - Finished - Running, floored at zero. TotalRuns
	// comes from the suite-started event and is exact.
	Remaining int
}

// Model is the folded state of a progress stream. The zero value is a valid
// empty model. Fold is pure: it returns the next model and mutates neither
// the receiver nor the event.
//
// Per-run events are keyed by ev.Run, so one run's invocation event never
// increments another's count and no run's state is reset by another's start.
type Model struct {
	suiteID    string
	totalTests int
	totalRuns  int

	// inFlight holds every run that has started but not yet finished,
	// keyed by RunID. The map is replaced (never mutated) on each Fold so
	// the receiver is never modified and pure semantics hold.
	inFlight map[string]RunProgress

	// finished counts how many ProgressTestFinished events have been folded.
	// Lifecycle events are never dropped, so this count is sound.
	finished int

	// counts and totalCost are carried verbatim from the suite-finished event.
	// They are the report's own authoritative figures, never recomputed here.
	counts    map[domain.Verdict]int
	totalCost domain.CostReport
}

// Fold returns the model that results from applying ev.
//
// Per-run events are keyed by ev.Run, so one run's invocation event never
// increments another's count and no run's state is reset by another's start.
//
// Out-of-order completion is tolerated: a finish for a run never started
// leaves the in-flight set unchanged and still increments the finished tally,
// because a lifecycle event is never dropped and so must always be counted.
func (m Model) Fold(ev domain.ProgressEvent) Model {
	switch ev.Kind {
	case domain.ProgressSuiteStarted:
		m.suiteID = ev.SuiteID
		m.totalTests = ev.TotalTests
		m.totalRuns = ev.TotalRuns

	case domain.ProgressTestStarted:
		// Copy the in-flight map, then add the new run. Copying preserves
		// pure semantics: the receiver's map is never mutated.
		newFlight := make(map[string]RunProgress, len(m.inFlight)+1)
		for k, v := range m.inFlight {
			newFlight[k] = v
		}
		newFlight[ev.Run.RunID] = RunProgress{
			Key:         ev.Run,
			Repetitions: ev.Repetitions,
			StartedAt:   ev.At,
		}
		m.inFlight = newFlight

	case domain.ProgressInvocation:
		// Only count an invocation if its run is in the in-flight set.
		// Under saturation, invocation events may be dropped, but never
		// lifecycle events — this count is approximate by design.
		if rp, ok := m.inFlight[ev.Run.RunID]; ok {
			newFlight := make(map[string]RunProgress, len(m.inFlight))
			for k, v := range m.inFlight {
				newFlight[k] = v
			}
			rp.ObservedInvocations++
			newFlight[ev.Run.RunID] = rp
			m.inFlight = newFlight
		}

	case domain.ProgressTestFinished:
		// Remove the run from the in-flight set. If the run was never started
		// (out-of-order completion), the delete is a no-op and the in-flight
		// set is unchanged — exactly the required behaviour.
		if _, ok := m.inFlight[ev.Run.RunID]; ok {
			newFlight := make(map[string]RunProgress, len(m.inFlight))
			for k, v := range m.inFlight {
				newFlight[k] = v
			}
			delete(newFlight, ev.Run.RunID)
			m.inFlight = newFlight
		}
		// Lifecycle events are never dropped, so finished must always be
		// incremented — even for an out-of-order completion.
		m.finished++

	case domain.ProgressSuiteFinished:
		// Carry the report's authoritative counts and total cost verbatim.
		// Never recompute them from the stream: doing so would make this
		// model a second source of truth, which the design forbids.
		m.counts = ev.Counts
		m.totalCost = ev.TotalCost
	}

	return m
}

// Running returns every in-flight run, ordered by StartedAt and then by
// RunID, so the ordering is total and stable rather than map-iteration order.
func (m Model) Running() []RunProgress {
	if len(m.inFlight) == 0 {
		return nil
	}
	result := make([]RunProgress, 0, len(m.inFlight))
	for _, rp := range m.inFlight {
		result = append(result, rp)
	}
	sort.Slice(result, func(i, j int) bool {
		ti := result[i].StartedAt
		tj := result[j].StartedAt
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return result[i].Key.RunID < result[j].Key.RunID
	})
	return result
}

// Tally returns the running/finished/remaining counts.
func (m Model) Tally() Tally {
	running := len(m.inFlight)
	remaining := m.totalRuns - m.finished - running
	if remaining < 0 {
		remaining = 0
	}
	return Tally{
		Running:   running,
		Finished:  m.finished,
		Remaining: remaining,
	}
}

// SuiteID reports what the suite-started event disclosed.
func (m Model) SuiteID() string {
	return m.suiteID
}

// TotalTests reports what the suite-started event disclosed.
func (m Model) TotalTests() int {
	return m.totalTests
}

// Counts reports what the suite-finished event disclosed, and is nil before
// it arrives. The figures are the report's own, carried through the stream,
// never recomputed here.
func (m Model) Counts() map[domain.Verdict]int {
	return m.counts
}

// TotalCost reports what the suite-finished event disclosed, and is
// zero-valued before it arrives.
func (m Model) TotalCost() domain.CostReport {
	return m.totalCost
}
