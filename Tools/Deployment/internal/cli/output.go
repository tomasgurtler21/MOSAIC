package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
