package authoring

import (
	"errors"
	"fmt"
)

// StructuredFailure is satisfied by errors that carry a delegate tool's
// structured failure fields. It is declared here, rather than imported from
// the package that produces such errors, so that diagnostic construction
// depends on the shape of a failure and not on the adapter that produced it.
//
// An empty FailureReason means "undifferentiated failure": callers must not
// map it to any specific known condition.
type StructuredFailure interface {
	FailureReason() string
	FailureDetail() string
	FailureToolMessage() string
	FailureExitCode() int
}

// ErrReportWriterUnconfigured is returned by a frontend that was asked to
// write a report with no writer wired in. It is a distinct condition from a
// write that was attempted and failed, and ReportWriteFailureReport reports
// it as such.
var ErrReportWriterUnconfigured = errors.New("no report writer configured")

// reportWriteOverrideHint is the normative hint attached to every
// report-write-failure diagnostic. It names all three override mechanisms:
// the --report-path flag, the --no-report flag, and the TUI inline editor.
const reportWriteOverrideHint = "use --report-path to change the target path, " +
	"--no-report to suppress the write entirely, " +
	"or edit the path inline in the TUI suite-select screen (press 'e') before running"

// RunFailureReport builds the structured diagnostic report for a failed suite
// run. suitePath is the locator the diagnostics name; it may be empty, in
// which case the renderer substitutes "<unknown>".
//
// When err (or anything in its chain) satisfies StructuredFailure, each
// non-zero field becomes its own diagnostic so the delegate's reason, the
// input it named, its own message and its exit code are each individually
// visible. Otherwise the report degrades to a single well-formed diagnostic
// carrying err.Error().
//
// A nil err yields the zero Report, which renders as the empty string.
func RunFailureReport(suitePath string, err error) Report {
	if err == nil {
		return Report{}
	}

	var sf StructuredFailure
	if errors.As(err, &sf) {
		return runFailureReportStructured(suitePath, sf)
	}

	// Degraded case: a plain error that carries no structured fields.
	// Produce exactly one well-formed diagnostic carrying the error text.
	return Report{Diagnostics: []Diagnostic{
		{
			Severity: SeverityError,
			Code:     "suite-run-failed",
			Path:     suitePath,
			Pointer:  "failure",
			Message:  err.Error(),
		},
	}}
}

// runFailureReportStructured builds the per-field diagnostic set for an error
// that satisfies StructuredFailure. Each non-zero field becomes its own
// diagnostic so the values are individually visible rather than interpolated.
func runFailureReportStructured(suitePath string, sf StructuredFailure) Report {
	reason := sf.FailureReason()
	detail := sf.FailureDetail()
	toolMessage := sf.FailureToolMessage()
	exitCode := sf.FailureExitCode()

	// Lead diagnostic: summarises the failure class.
	// When the delegate supplied no structured reason, the hint tells the user
	// that the delegate's own output is the only detail available.
	leadHint := ""
	if reason == "" {
		leadHint = "the failing tool reported no structured reason; " +
			"its own output (shown below, where present) is the only detail available"
	}
	var rep Report
	rep.Add(Diagnostic{
		Severity: SeverityError,
		Code:     "suite-run-failed",
		Path:     suitePath,
		Pointer:  "failure",
		Message:  "the suite run failed",
		Hint:     leadHint,
	})

	// One diagnostic per non-zero/non-empty structured field.
	// Empty fields are omitted entirely (AD-5): never render a quoted empty
	// value and never substitute a placeholder condition.
	if reason != "" {
		rep.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "suite-run-failed",
			Path:     suitePath,
			Pointer:  "failure.reason",
			Message:  fmt.Sprintf("reason: %q", reason),
		})
	}
	if detail != "" {
		rep.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "suite-run-failed",
			Path:     suitePath,
			Pointer:  "failure.detail",
			Message:  fmt.Sprintf("offending input: %q", detail),
		})
	}
	if toolMessage != "" {
		rep.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "suite-run-failed",
			Path:     suitePath,
			Pointer:  "failure.tool_output",
			Message:  toolMessage,
		})
	}
	if exitCode != 0 {
		rep.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "suite-run-failed",
			Path:     suitePath,
			Pointer:  "failure.exit_code",
			Message:  fmt.Sprintf("exit code: %d", exitCode),
		})
	}

	return rep
}

// ReportWriteFailureReport builds the structured diagnostic report for a
// failed JSON report write. reportPath is the target path; it becomes the
// diagnostic Path so the failing location is named in the rendered line.
//
// errors.Is(err, ErrReportWriterUnconfigured) is reported as its own
// condition rather than as a generic write failure. Every diagnostic this
// function produces carries a hint naming the report-path overrides.
//
// A nil err yields the zero Report.
func ReportWriteFailureReport(reportPath string, err error) Report {
	if err == nil {
		return Report{}
	}

	if errors.Is(err, ErrReportWriterUnconfigured) {
		return Report{Diagnostics: []Diagnostic{
			{
				Severity: SeverityError,
				Code:     "report-writer-unconfigured",
				Path:     reportPath,
				Pointer:  "failure",
				Message:  "no report writer was configured; the write could not be attempted",
				Hint:     reportWriteOverrideHint,
			},
		}}
	}

	return Report{Diagnostics: []Diagnostic{
		{
			Severity: SeverityError,
			Code:     "report-unwritable",
			Path:     reportPath,
			Pointer:  "failure",
			Message:  err.Error(),
			Hint:     reportWriteOverrideHint,
		},
	}}
}
