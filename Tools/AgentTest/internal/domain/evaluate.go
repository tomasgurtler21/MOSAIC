package domain

import "time"

// ViolationClassKey mirrors protocolcheck.ViolationClass as a domain-level
// string, so domain does not import a pure core to name a violation class.
type ViolationClassKey string

// ConcurrencyProblems mirrors concurrency.Report at the domain level, so
// RunEvidence can carry it without importing the concurrency package.
type ConcurrencyProblems struct {
	// UnterminatedSeqs lists the global sequence numbers of invocations
	// whose end record is missing.
	UnterminatedSeqs []int
	// Ungrouped counts start records that matched no declared group.
	Ungrouped int
}

// RunEvidence is the complete input to the verdict engine. Nothing else is
// consulted; there is no I/O behind this struct. Durations, file listings
// and cost arrive as evidence, gathered by the caller, so a run can be
// re-evaluated after an assertion is corrected without re-spawning an agent.
type RunEvidence struct {
	Definition TestDefinition
	Settings   RunSettings
	Key        RunKey

	Records       []LogRecord
	SubjectResult SubjectResult
	Orchestration OrchestrationState
	// OrchestrationProblem is set when the document was absent or
	// malformed, so that condition is never read as a valid zero state.
	OrchestrationProblem string

	// SnapshotFiles lists every file present in the subject directory after
	// the run, relative to it — the input to artifact creation assertions.
	SnapshotFiles []string

	ProtocolViolations        map[ViolationClassKey]int
	SubjectProtocolViolations map[ViolationClassKey]int
	PeakConcurrency           map[string]int
	ConcurrencyProblems       ConcurrencyProblems

	Cost     CostReport
	Duration time.Duration
}

// TestResult is the outcome of evaluating one run's evidence: a verdict with
// enough per-assertion detail for a report to explain a failure without
// re-reading the log.
type TestResult struct {
	Key        RunKey
	Verdict    Verdict
	Reasons    []FailureReason
	Assertions []AssertionResult
	// Conditions are things worth surfacing that are not themselves the
	// verdict — a cost-attribution miss, an unterminated invocation
	// interval, a degraded protocol extraction. They must never be
	// silently dropped.
	Conditions []RunCondition
	// NegativeApplied records that inversion was applied, so a report can
	// explain a PASS that came from failing assertions.
	NegativeApplied bool
	Cost            CostReport
	Duration        time.Duration
}

// AssertionOutcome is the per-assertion result of evaluating one class.
type AssertionOutcome string

const (
	AssertionPass         AssertionOutcome = "pass"
	AssertionFail         AssertionOutcome = "fail"
	AssertionNotEvaluated AssertionOutcome = "not_evaluated"
)

// AssertionResult is the outcome of evaluating one assertion class (or one
// instance of a repeatable class).
type AssertionResult struct {
	Class AssertionClass
	// Target names which instance of a repeatable class this is, e.g. the
	// parallel group name or the invocation sequence number.
	Target   string
	Outcome  AssertionOutcome
	Expected string
	Actual   string
	// Detail is enough for a report to explain the failure without
	// re-reading the log.
	Detail string
}

// RunCondition is a reportable condition that is not itself the verdict.
type RunCondition struct {
	Kind   RunConditionKind
	Detail string
}

// RunConditionKind names one class of reportable condition.
type RunConditionKind string

const (
	ConditionCostUnattributed       RunConditionKind = "cost_unattributed"
	ConditionUnterminatedInterval   RunConditionKind = "unterminated_interval"
	ConditionExtractionDegraded     RunConditionKind = "extraction_degraded"
	ConditionUnmatchedInvocation    RunConditionKind = "unmatched_invocation"
	ConditionOrchestrationUnreadable RunConditionKind = "orchestration_unreadable"
)

// RepetitionPolicy is a test's declared repetition count and required pass
// rate.
type RepetitionPolicy struct {
	Repetitions int
	PassRate    float64 // fraction of counted runs that must pass, 0..1
}

// AggregateResult combines the results of a test's repetitions against its
// declared pass rate.
type AggregateResult struct {
	TestID  string
	Verdict Verdict
	Reasons []FailureReason
	Runs    []TestResult

	// Counted excludes runs excluded for state integrity: the aggregate
	// must measure the subject, not the tool.
	Counted  int
	Passed   int
	Excluded int
	PassRate float64
	// InfrastructureFailure is set when a state-integrity run recurred
	// after its single retry.
	InfrastructureFailure bool
	TotalCost             CostReport
}
