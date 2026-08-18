package domain

import "time"

// CostQuery names the log location a cost figure is attributed from.
type CostQuery struct {
	LogRoot string // Sandbox.LogRoot()
	RunID   string
}

// CostAttribution signals whether a cost figure could be trusted.
type CostAttribution string

const (
	AttributionAttributed CostAttribution = "attributed"
	// Events landed in the unknown-run bucket: a real cost exists but could
	// not be tied to this run. Surfaced, never reported as zero.
	AttributionUnknownBucket CostAttribution = "unknown_bucket"
	AttributionUnavailable   CostAttribution = "unavailable"
)

// CostReport is a monetary cost figure plus its attribution quality.
type CostReport struct {
	TotalUSD    float64
	Attribution CostAttribution
	Detail      string
}

// Add combines two cost reports. The combined attribution is the weaker of
// the two: a report cannot claim full attribution when either input does
// not.
func (c CostReport) Add(other CostReport) CostReport {
	result := CostReport{
		TotalUSD:    c.TotalUSD + other.TotalUSD,
		Attribution: c.Attribution,
	}
	if c.Attribution == AttributionAttributed {
		result.Attribution = other.Attribution
	}
	switch {
	case c.Detail == "":
		result.Detail = other.Detail
	case other.Detail == "":
		result.Detail = c.Detail
	default:
		result.Detail = c.Detail + "; " + other.Detail
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
