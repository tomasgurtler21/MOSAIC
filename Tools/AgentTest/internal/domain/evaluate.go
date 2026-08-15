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

	// LogRoot is the log location this run's cost was queried from, carried
	// here so a no-logs condition's detail can name the path that was
	// searched without evaluate importing anything path- or I/O-related.
	LogRoot string

	// LogsProduced reports whether LogRoot contained any log records at all.
	// False raises ConditionNoLogsProduced. Only meaningful when the run
	// actually started: RunEvidence is never built on the path where it did
	// not (see ConditionRunNotStarted).
	LogsProduced bool

	// RetainedSandboxPath is the sandbox left on disk for diagnosis by this
	// attempt's teardown, empty when none was retained. Carried here so the
	// report a user reads can print an openable path — a retention feature
	// whose path has to be guessed is not a feature.
	RetainedSandboxPath string

	// SubjectVersion is the declared version of the subject's source
	// definition, captured at render time from the deployment port's own
	// result. It is carried here because the report cannot obtain it
	// elsewhere: the rendered file in the sandbox is gone by report time, and
	// re-deriving it later could disagree with what was actually deployed.
	//
	// Empty means the source declared no version — a legal state the
	// renderings show as unknown, never as a blank that reads like a real
	// value.
	SubjectVersion string
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

	// RetainedSandboxPath is carried through from RunEvidence unchanged, so
	// the report a user reads can print the path a retained sandbox was left
	// at, empty when none was retained.
	RetainedSandboxPath string

	// SubjectResult carries the subject's exit code, stderr and raw output
	// from RunEvidence so the report can surface a diagnosable authentication
	// or environment failure without the user having to reproduce outside the
	// tool. Zero-valued when the subject exited zero; the report only renders
	// it when ExitCode != 0.
	SubjectResult SubjectResult

	// SubjectVersion is carried through from RunEvidence unchanged, exactly
	// as RetainedSandboxPath is, so the report can attribute a result to a
	// specific version of the agent under test. Empty means the source
	// declared no version; the renderings show it as unknown rather than
	// blank.
	SubjectVersion string
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
	ConditionCostUnattributed        RunConditionKind = "cost_unattributed"
	ConditionUnterminatedInterval    RunConditionKind = "unterminated_interval"
	ConditionExtractionDegraded      RunConditionKind = "extraction_degraded"
	ConditionUnmatchedInvocation     RunConditionKind = "unmatched_invocation"
	ConditionOrchestrationUnreadable RunConditionKind = "orchestration_unreadable"

	// ConditionRunNotStarted carries the detail of a fault that stopped an
	// attempt before it began: a setup, provisioning or spawn-plan failure, or
	// a recovered panic. Nothing ran, so there is no evidence of any kind.
	//
	// It duplicates no reason kind. The verdict's reason stays
	// ReasonInfrastructure — already documented as "the tool itself could not
	// complete the run" and already consumed by aggregation. The reason
	// answers why the verdict is FAIL; this answers what happened, which is
	// what a condition is for. No existing kind describes it honestly: the
	// other kinds all describe a property of a run that did happen.
	//
	// Detail is the runner error's text, prefixed with the phase that failed,
	// and never the empty string.
	ConditionRunNotStarted RunConditionKind = "run_not_started"

	// ConditionNoLogsProduced reports that a run that DID start finished
	// having produced no logs at all, so cost could not be attributed for a
	// reason that is about the run rather than about the delegate.
	//
	// The distinction from ConditionRunNotStarted is load-bearing and must
	// stay legible in Detail as well as in Kind: this one means "started,
	// produced nothing", that one means "never started". Detail names the
	// log root that was queried and found empty. The two are mutually
	// exclusive by construction: ConditionRunNotStarted is raised only where
	// runner.Run returned an error, which is precisely the path on which no
	// evidence — and therefore no log root to inspect — exists.
	ConditionNoLogsProduced RunConditionKind = "no_logs_produced"

	// ConditionSubjectNeverStarted reports that the subject process was launched
	// but refused to start, so nothing was exercised and no assertion could be
	// evaluated. Detail is the real one-line cause, taken from the subject's own
	// standard error, and is never the empty string.
	//
	// It is distinct from ConditionRunNotStarted, which reports that the tool
	// could not begin the attempt at all — there, no subject result exists. Here
	// one does, and it is the evidence.
	ConditionSubjectNeverStarted RunConditionKind = "subject_never_started"
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
