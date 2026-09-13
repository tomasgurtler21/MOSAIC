// Package report defines the single result model both renderings and both
// frontends consume, and the two renderings built from it.
//
// Two renderings built from two traversals of the evidence eventually
// disagree, and a test tool whose human answer and machine answer differ is
// worse than one that only has the machine answer. report.Result is the
// only model a rendering or a frontend may traverse: no consumer computes a
// count, a total or an outcome class of its own.
package report

import (
	"fmt"
	"io"
	"time"

	"mosaic-agent-test/internal/domain"
)

// SchemaVersion is the machine-readable report's version. The shape is
// additive-only: a field is never removed or retyped, so a caller parsing
// today's output keeps working.
const SchemaVersion = "1"

// ReportErrorKind is a stable string identifying a report-level error type.
type ReportErrorKind string

const (
	// ErrorUnknownRunResidual means usage records in the unknown-run bucket
	// could not be attributed to any run after merging.
	ErrorUnknownRunResidual ReportErrorKind = "unknown_run_residual"
)

// ReportError is one report-level error condition.
type ReportError struct {
	// Kind identifies the error type. Machine-readable, stable string.
	Kind ReportErrorKind
	// Detail is a human-readable description.
	Detail string
	// Count is the number of affected items (e.g., residual records).
	Count int
}

// Result is the one model every rendering and every frontend derives from.
type Result struct {
	SchemaVersion string
	SuiteID       string
	StartedAt     time.Time
	FinishedAt    time.Time
	Tests         []TestReport
	Counts        map[domain.Verdict]int
	TotalCost     domain.CostReport
	// InfrastructureFailures counts tests ended by a repeated state-integrity
	// fault, kept separate from the verdict counts because it is a statement
	// about the tool rather than about the subject.
	InfrastructureFailures int

	// EffectiveRepetitions, when non-nil, is the repetitions count that was in
	// effect for this run — either a CLI/TUI override or the suite's authored
	// default as resolved by the caller. Nil means the caller did not record it.
	// The text renderer surfaces this in the report header so a user can
	// confirm the value without examining the suite file.
	EffectiveRepetitions *int

	// CatalogFolder is the agent catalog directory that was used for
	// this run's agent deployments. Suite-level (not per-run on
	// RunReport) because the catalog folder is process-wide and
	// constant for all runs in a single invocation. Empty means no
	// catalog folder was configured (production catalog was used).
	CatalogFolder string

	// Errors is a list of report-level error conditions that are not
	// attributable to any single test or run. Machine-readable so downstream
	// tooling can check for them without parsing per-run detail.
	// Renders as [] (never null) on the wire.
	Errors []ReportError

	// ToolVersion is the version of the mosaic-agent-test binary that produced
	// this report. Distinct from SchemaVersion (wire-format version) and per-run
	// SubjectVersion (tested agent's version). Set on the Result returned by
	// Build at the production call site in suite.go, not by Build itself.
	// Empty means not recorded (e.g. pre-feature invocation or test double).
	ToolVersion string
}

// TestReport is one test's aggregate outcome plus its per-repetition detail.
type TestReport struct {
	TestName    string // human-readable display name of the test
	NumericID   int    // Stable numeric identity, sourced from Definition.NumericID.
	Description string
	Layer       domain.TestLayer
	Aggregate   domain.AggregateResult
	Runs        []RunReport
}

// RunReport is one repetition's outcome.
type RunReport struct {
	Key             domain.RunKey
	Verdict         domain.Verdict
	Reasons         []domain.FailureReason
	Assertions      []domain.AssertionResult
	Conditions      []domain.RunCondition
	Duration        time.Duration
	Cost            domain.CostReport
	NegativeApplied bool

	// RetainedSandboxPath is the sandbox left on disk for diagnosis, empty
	// when none was retained. Printed by both renderings: a retention
	// feature whose path a user has to guess is not a feature.
	RetainedSandboxPath string

	// Subject carries the subject's own failure output on a non-zero exit.
	// Zero-valued when the subject exited zero.
	Subject SubjectFailure

	// SubjectVersion is the version of the subject definition this run
	// exercised, carried from RunEvidence through TestResult. Empty means the
	// source declared no version; both renderings show it as unknown rather
	// than blank, so a reader never mistakes an absent version for a real one.
	SubjectVersion string

	// SubjectModel and StubModel are the models this repetition actually ran
	// on, carried from RunEvidence through TestResult. Empty means none was
	// recorded; both renderings show that as unknown, never as blank.
	SubjectModel string
	StubModel    string

	// HarnessID names the harness adapter that produced this run, carried
	// from RunEvidence through TestResult. Per-run placement (not
	// report-level) because a single invocation may run different attempts
	// against different harnesses once harness selection is resolved.
	//
	// Empty means no harness identity was recorded; both renderings show
	// that as unknown, never as blank.
	HarnessID string

	// TerminationReason names why this run ended. Values match
	// domain.RunDisposition: "completed", "early_exit", "timed_out",
	// "turn_limit", "spawn_failed". Empty only when disposition was not
	// recorded, which both renderings show as "unknown" rather than blank.
	TerminationReason string

	// TestVersion is the content version of the test definition this run
	// exercised, sourced from domain.TestResult.Version and copied by suite.go.
	// Zero means the value was not recorded.
	TestVersion int

	// NumericID is the stable numeric identity of the test definition this run
	// exercised, sourced from domain.TestResult.NumericID and copied by suite.go.
	// Zero means the value was not recorded.
	NumericID int
}

// SubjectFailure is what a subject that exited non-zero told us. Zero-valued
// when the subject exited zero.
type SubjectFailure struct {
	ExitCode int
	// Stderr and RawOutput are truncated for display by the renderings,
	// never dropped by the model.
	Stderr    string
	RawOutput string
}

// Build assembles the model from what the suite produced. Pure, so both
// renderers can be golden-tested from a fixture model with no run behind it.
//
// Counts, TotalCost and InfrastructureFailures are derived here, once, from
// each test's aggregate — the same rule report.Result documents: no other
// consumer computes a count or a total of its own.
func Build(suiteID string, started, finished time.Time, tests []TestReport, catalogFolder string) Result {
	counts := map[domain.Verdict]int{}
	total := domain.CostReport{Attribution: domain.AttributionAttributed}
	infra := 0
	var totalResidual int

	for _, t := range tests {
		counts[t.Aggregate.Verdict]++
		total = total.Add(t.Aggregate.TotalCost)
		if t.Aggregate.InfrastructureFailure {
			infra += t.Aggregate.Excluded
		}
		totalResidual += t.Aggregate.TotalCost.UnknownRunResidual
	}

	errors := []ReportError{}
	if totalResidual > 0 {
		errors = append(errors, ReportError{
			Kind: ErrorUnknownRunResidual,
			Detail: fmt.Sprintf(
				"%d unknown-run usage record(s) could not be attributed after merging",
				totalResidual,
			),
			Count: totalResidual,
		})
	}

	return Result{
		SchemaVersion:          SchemaVersion,
		SuiteID:                suiteID,
		StartedAt:              started,
		FinishedAt:             finished,
		Tests:                  tests,
		Counts:                 counts,
		TotalCost:              total,
		InfrastructureFailures: infra,
		CatalogFolder:          catalogFolder,
		Errors:                 errors,
	}
}

// RenderText writes the human-readable terminal report.
//
// Both RenderText and RenderJSON are total functions over Result: an empty
// suite and an all-excluded suite render a well-formed document rather than
// a special case, so a caller parsing the output never has to branch on "no
// tests ran".
func RenderText(w io.Writer, r Result) error {
	return renderText(w, r)
}

// RenderJSON writes the versioned machine-readable report.
func RenderJSON(w io.Writer, r Result) error {
	return renderJSON(w, r)
}

// OutcomeClass names one visibly distinct outcome a run can exhibit.
//
// The obligation that an infrastructure fault must not read like a subject
// regression appears in the terminal report, in the machine-readable report
// and in the interactive frontend's detail view. Deriving it three times
// would let the three drift, so it is derived once, here.
type OutcomeClass string

const (
	ClassPass             OutcomeClass = "pass"
	ClassAssertionFailure OutcomeClass = "assertion_failure"
	ClassTimeout          OutcomeClass = "timeout"
	ClassStateIntegrity   OutcomeClass = "state_integrity"
	ClassCostUnattributed OutcomeClass = "cost_unattributed"
	ClassEchoMismatch     OutcomeClass = "echo_mismatch"

	// ClassSubjectFailure is a subject that exited non-zero.
	ClassSubjectFailure OutcomeClass = "subject_failure"

	// ClassCutoff is a run that was terminated by the hard subject cutoff
	// (stop_after_invocations). Present in Classify's output if and only if
	// TerminationReason equals domain.DispositionEarlyExit.
	ClassCutoff OutcomeClass = "cutoff"
)

// Classify reports every class a run exhibits, in a stable order. A run can
// exhibit more than one — a timed-out run whose cost was also unattributed
// is two facts, not a precedence question.
func Classify(r RunReport) []OutcomeClass {
	var out []OutcomeClass
	for _, reason := range r.Reasons {
		switch reason {
		case domain.ReasonAssertion:
			out = append(out, ClassAssertionFailure)
		case domain.ReasonTimeout:
			out = append(out, ClassTimeout)
		case domain.ReasonStateIntegrity:
			out = append(out, ClassStateIntegrity)
		case domain.ReasonEchoMismatch:
			out = append(out, ClassEchoMismatch)
		}
	}
	for _, c := range r.Conditions {
		if c.Kind == domain.ConditionCostUnattributed {
			out = append(out, ClassCostUnattributed)
		}
	}
	if r.Subject.ExitCode != 0 {
		out = append(out, ClassSubjectFailure)
	}
	if r.TerminationReason == string(domain.DispositionEarlyExit) {
		out = append(out, ClassCutoff)
	}
	if len(out) == 0 && r.Verdict == domain.VerdictPass {
		out = append(out, ClassPass)
	}
	return out
}

// testClasses reports every non-pass class any of t's runs exhibits, unique
// and in first-occurrence order — the set the terminal and machine-readable
// reports both surface for one test.
func testClasses(t TestReport) []OutcomeClass {
	seen := map[OutcomeClass]bool{}
	var out []OutcomeClass
	for _, run := range t.Runs {
		for _, c := range Classify(run) {
			if c == ClassPass || seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
