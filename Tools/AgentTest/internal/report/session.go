package report

import "mosaic-agent-test/internal/domain"

// SessionOutcome classifies a session of one or more suites into one of three
// states. The distinction between a subject regression and an infrastructure
// fault must survive: a caller that cannot tell them apart will eventually
// treat one as the other.
type SessionOutcome string

const (
	// SessionPassed: every test in every suite met its required pass rate.
	SessionPassed SessionOutcome = "passed"

	// SessionTestsFailed: at least one test was measured and did not meet its
	// required pass rate. No infrastructure failure is present -- if one were,
	// the outcome would be SessionInfrastructureFailure instead, because the
	// inability to measure takes precedence over a measured regression.
	SessionTestsFailed SessionOutcome = "tests_failed"

	// SessionInfrastructureFailure: the session could not be measured. This
	// covers any of:
	//   - at least one test ended with an infrastructure failure,
	//   - the queue did not run to completion (Aborted is true).
	// This state takes precedence over SessionTestsFailed when both conditions
	// are present.
	SessionInfrastructureFailure SessionOutcome = "infrastructure_failure"
)

// Session is the model for a complete multi-suite session. It holds the
// ordered suite results, the combined figures, and the overall outcome. Like
// Result, it is the single model a rendering or a frontend may traverse for
// session-level information: no consumer computes a session-level count,
// total or outcome of its own.
type Session struct {
	// Suites is the ordered list of per-suite results for suites that
	// actually executed, in execution order.
	Suites []Result

	// UnrunSuites names the selected suites that never started, in selection
	// order. Present only when Aborted is true.
	UnrunSuites []string

	// Aborted is true when the queue did not run to completion -- a suite
	// returned an error, or a report file could not be written, and the
	// remaining suites were skipped.
	Aborted bool

	// Counts is the combined verdict counts across all executed suites.
	// Each suite's Counts map is summed; no individual test is re-examined.
	Counts map[domain.Verdict]int

	// TotalCost is the combined cost across all executed suites.
	TotalCost domain.CostReport

	// InfrastructureFailures is the combined infrastructure-failure count
	// across all executed suites.
	InfrastructureFailures int

	// Outcome is the three-state classification of the session.
	Outcome SessionOutcome
}

// NewSession builds a Session from the suites that executed, the names of
// suites that were selected but never started, and whether the queue ran to
// completion. Combined counts, cost and infrastructure failures are derived
// once from the suite results' own fields -- never re-examined from the
// tests -- following the same derive-once rule as Build.
//
// Classification rules:
//   - If aborted is true, or the combined InfrastructureFailures > 0,
//     Outcome is SessionInfrastructureFailure.
//   - Otherwise, if the combined Counts[VerdictFail] > 0,
//     Outcome is SessionTestsFailed.
//   - Otherwise, Outcome is SessionPassed.
//
// An empty suites slice with aborted false produces a well-formed Session
// with zeroed figures and SessionPassed.
func NewSession(suites []Result, unrunSuites []string, aborted bool) Session {
	combined := make(map[domain.Verdict]int)
	var totalCost domain.CostReport
	var infraFailures int

	for _, s := range suites {
		for verdict, count := range s.Counts {
			combined[verdict] += count
		}
		totalCost = totalCost.Add(s.TotalCost)
		infraFailures += s.InfrastructureFailures
	}

	var outcome SessionOutcome
	switch {
	case aborted || infraFailures > 0:
		outcome = SessionInfrastructureFailure
	case combined[domain.VerdictFail] > 0:
		outcome = SessionTestsFailed
	default:
		outcome = SessionPassed
	}

	return Session{
		Suites:                 suites,
		UnrunSuites:            unrunSuites,
		Aborted:                aborted,
		Counts:                 combined,
		TotalCost:              totalCost,
		InfrastructureFailures: infraFailures,
		Outcome:                outcome,
	}
}
