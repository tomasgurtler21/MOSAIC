package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// renderOutput writes the run summary and returns the appropriate exit code.
// When format is "json", a domain.RunSummary JSON document is written to out.
// Otherwise a human-readable summary is written to out.
// When svcErr is non-nil (an unrecoverable service error), the error is written to errOut
// and ExitFailure is returned without a summary (the summary is empty in this case).
func renderOutput(out, errOut io.Writer, format string, summary domain.RunSummary, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}

	if strings.EqualFold(format, "json") {
		renderJSON(out, summary)
	} else {
		renderHuman(out, summary)
	}
	return outcomeExitCode(summary.Outcome)
}

// renderJSON encodes summary as a single JSON document and writes it to out.
func renderJSON(out io.Writer, summary domain.RunSummary) {
	enc := json.NewEncoder(out)
	_ = enc.Encode(summary)
}

// renderHuman writes a human-readable one-paragraph summary of the run to out.
// The output always includes the workspace path and an outcome word (success / fail / skip).
func renderHuman(out io.Writer, summary domain.RunSummary) {
	switch summary.Outcome {
	case domain.OutcomeSuccess:
		fmt.Fprintf(out, "success: deployment complete\nworkspace: %s\n", summary.WorkspacePath)
	case domain.OutcomeCompletedWithGaps:
		fmt.Fprintf(out, "completed with skips: some items were skipped and recorded in the TODO list\nworkspace: %s\n", summary.WorkspacePath)
	case domain.OutcomeFailed:
		fmt.Fprintf(out, "failed: deployment encountered errors\nworkspace: %s\n", summary.WorkspacePath)
	default:
		fmt.Fprintf(out, "outcome: %s\nworkspace: %s\n", string(summary.Outcome), summary.WorkspacePath)
	}
}

// outcomeExitCode maps a RunSummary Outcome to an exit code per the exit code contract.
func outcomeExitCode(outcome domain.Outcome) int {
	switch outcome {
	case domain.OutcomeSuccess:
		return ExitSuccess
	case domain.OutcomeCompletedWithGaps:
		return ExitWithSkips
	case domain.OutcomeFailed:
		return ExitFailure
	default:
		return ExitFailure
	}
}

// renderPromoteOutput writes the promote result and returns the appropriate exit code.
// When format is "json", a PromoteResult JSON document is written to out; otherwise a
// human-readable summary is written to out.
// When svcErr is non-nil the error is written to errOut and ExitFailure is returned without
// a result. Promote has no partial-success state, so unlike renderOutput there is no
// outcome-to-exit-code mapping: success is ExitSuccess and any service error is ExitFailure.
func renderPromoteOutput(out, errOut io.Writer, format string, result app.PromoteResult, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}
	if strings.EqualFold(format, "json") {
		renderPromoteJSON(out, result)
	} else {
		renderPromoteHuman(out, result)
	}
	return ExitSuccess
}

// renderPromoteJSON encodes result as a single JSON document and writes it to out.
func renderPromoteJSON(out io.Writer, result app.PromoteResult) {
	enc := json.NewEncoder(out)
	_ = enc.Encode(result)
}

// renderPromoteHuman writes a human-readable summary of the promote to out. The output
// always names the destination path and the assigned numeric id, and states explicitly that
// nothing was written when result.DryRun is true.
func renderPromoteHuman(out io.Writer, result app.PromoteResult) {
	if result.DryRun {
		fmt.Fprintf(out, "dry run: promote validated\nsource: %s\ndestination: %s (not written)\nkey: %s\nid: %s\n",
			result.SourcePath, result.DestinationPath, result.Key, result.NumericID)
		return
	}
	fmt.Fprintf(out, "promoted: %s\ndestination: %s\nkey: %s\nid: %s\n",
		result.SourcePath, result.DestinationPath, result.Key, result.NumericID)
}
