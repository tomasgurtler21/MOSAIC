package domain

import (
	"sort"
	"time"
)

// CostQuery names the log location a cost figure is attributed from.
type CostQuery struct {
	LogRoot  string // Sandbox.LogRoot() — the per-run folder
	LogsRoot string // Sandbox.LogsRoot() — the OrchestrationLogs parent
	RunID    string
}

// CostAttribution signals whether a cost figure could be trusted.
type CostAttribution string

const (
	AttributionAttributed CostAttribution = "attributed"
	// AttributionPartial means some cost was attributed but one or more
	// models were unpriced. The report shows the attributable amount and
	// names the unpriced model(s).
	AttributionPartial CostAttribution = "partial"
	// Events landed in the unknown-run bucket: a real cost exists but could
	// not be tied to this run. Surfaced, never reported as zero.
	AttributionUnknownBucket CostAttribution = "unknown_bucket"
	AttributionUnavailable   CostAttribution = "unavailable"
)

// attributionRank is the total order used by Add to select the weaker
// attribution. Higher rank = weaker.
var attributionRank = map[CostAttribution]int{
	AttributionAttributed:    0,
	AttributionPartial:       1,
	AttributionUnknownBucket: 2,
	AttributionUnavailable:   3,
}

// CostReport is a monetary cost figure plus its attribution quality.
type CostReport struct {
	TotalUSD    float64
	Attribution CostAttribution
	Detail      string

	// UnpricedModels names the model(s) that could not be priced, populated
	// only when Attribution is AttributionPartial. Empty otherwise.
	// Carries structured model identity so callers do not need to parse Detail.
	UnpricedModels []string
}

// Add combines two cost reports. The combined attribution is the weaker of
// the two, determined by a total rank ordering. UnpricedModels from both
// operands are unioned and deduplicated in the result.
func (c CostReport) Add(other CostReport) CostReport {
	rankC := attributionRank[c.Attribution]
	rankO := attributionRank[other.Attribution]

	attribution := c.Attribution
	if rankO > rankC {
		attribution = other.Attribution
	}

	result := CostReport{
		TotalUSD:    c.TotalUSD + other.TotalUSD,
		Attribution: attribution,
	}
	switch {
	case c.Detail == "":
		result.Detail = other.Detail
	case other.Detail == "":
		result.Detail = c.Detail
	default:
		result.Detail = c.Detail + "; " + other.Detail
	}

	// Union of UnpricedModels, deduplicated and sorted.
	if len(c.UnpricedModels) > 0 || len(other.UnpricedModels) > 0 {
		seen := make(map[string]struct{})
		for _, m := range c.UnpricedModels {
			seen[m] = struct{}{}
		}
		for _, m := range other.UnpricedModels {
			seen[m] = struct{}{}
		}
		union := make([]string, 0, len(seen))
		for m := range seen {
			union = append(union, m)
		}
		sort.Strings(union)
		result.UnpricedModels = union
	}

	return result
}

// ProgressKind names the kind of a ProgressEvent.
type ProgressKind string

const (
	ProgressSuiteStarted  ProgressKind = "suite_started"
	ProgressTestStarted   ProgressKind = "test_started"
	ProgressInvocation    ProgressKind = "invocation"
	ProgressTestFinished  ProgressKind = "test_finished"
	ProgressSuiteFinished ProgressKind = "suite_finished"
)

// ProgressEvent is the only channel through which a frontend learns about
// progress. Both frontends consume the same stream, which is what makes the
// CLI and the TUI equivalent by construction rather than by parallel
// maintenance.
type ProgressEvent struct {
	Kind ProgressKind
	At   time.Time

	// ProgressSuiteStarted
	SuiteID    string
	TotalTests int
	// TotalRuns is the total number of runs (repetitions) across all tests in
	// the plan. It is exact rather than estimated: it is computed from the
	// resolved plan, which fixes each test's repetition count before scheduling
	// begins, so a suite whose tests declare different repetition counts still
	// gets a correct total. It counts declared repetitions, not attempts: a
	// retry does not increase it.
	TotalRuns int

	// Run carries the identity of the run that produced this event.
	// Zero-valued for suite-level events (suite-started, suite-finished),
	// which belong to no single run.
	//
	// Per-event-kind semantics:
	//   - ProgressTestStarted: the repetition's first attempt's key.
	//   - ProgressInvocation: the key of the attempt that produced the invocation.
	//   - ProgressTestFinished: the key of the attempt the repetition settled on.
	Run RunKey

	// ProgressTestStarted / ProgressTestFinished
	TestID      string
	Repetition  int
	Repetitions int

	// ProgressInvocation
	Seq      int
	Identity CollaboratorIdentity
	Outcome  OutcomeKind

	// ProgressTestFinished
	Verdict          Verdict
	Duration         time.Duration
	Cost             CostReport
	FailedAssertions []string

	// ProgressSuiteFinished
	Counts    map[Verdict]int
	TotalCost CostReport
}
