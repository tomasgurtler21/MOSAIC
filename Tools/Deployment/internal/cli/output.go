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

// renderTransformOutput writes the transform result and returns the appropriate exit code.
// When format is "json", a TransformHarnessResult JSON document is written to out; otherwise
// a human-readable summary is written to out. When svcErr is non-nil the error is written
// to errOut and ExitFailure is returned.
func renderTransformOutput(out, errOut io.Writer, format string, result app.TransformHarnessResult, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}
	if strings.EqualFold(format, "json") {
		enc := json.NewEncoder(out)
		_ = enc.Encode(result)
	} else {
		renderTransformHuman(out, result)
	}
	return ExitSuccess
}

// renderTransformHuman writes a human-readable summary of the transform run to out.
func renderTransformHuman(out io.Writer, result app.TransformHarnessResult) {
	dryPrefix := ""
	if result.DryRun {
		dryPrefix = "dry run: "
	}
	fmt.Fprintf(out, "%stransform complete\nsource: %s → %s\ninput: %s\n",
		dryPrefix, result.SourceHarnessID, result.TargetHarnessID, result.InputPath)
	fmt.Fprintf(out, "transformed: %d  skipped-mismatch: %d  skipped-not-agent: %d  failed: %d\n",
		result.Transformed, result.SkippedMismatch, result.SkippedNotAgent, result.Failed)
	for _, f := range result.Files {
		switch f.Status {
		case app.StatusTransformed:
			if result.DryRun {
				fmt.Fprintf(out, "  [dry-run] %s → %s\n", f.SourcePath, f.DestinationPath)
			} else {
				fmt.Fprintf(out, "  transformed: %s → %s\n", f.SourcePath, f.DestinationPath)
			}
			if f.Warning != "" {
				fmt.Fprintf(out, "    warning: %s\n", f.Warning)
			}
			if len(f.Tools) > 0 {
				fmt.Fprintf(out, "    tools: %s\n", strings.Join(f.Tools, ", "))
			}
			if len(f.CarriedVerbatimTools) > 0 {
				fmt.Fprintf(out, "    carried verbatim tools: %s\n", strings.Join(f.CarriedVerbatimTools, ", "))
			}
			for _, sf := range f.StrippedFields {
				if len(sf.Values) > 0 {
					fmt.Fprintf(out, "    stripped field: %s (%s): %s\n", sf.Key, string(sf.Reason), strings.Join(sf.Values, ", "))
				} else {
					fmt.Fprintf(out, "    stripped field: %s (%s)\n", sf.Key, string(sf.Reason))
				}
			}
		case app.StatusSkippedMismatch:
			fmt.Fprintf(out, "  skipped (mismatch): %s — %s\n", f.SourcePath, f.Reason)
		case app.StatusSkippedNotAgent:
			fmt.Fprintf(out, "  skipped (not agent): %s — %s\n", f.SourcePath, f.Reason)
		case app.StatusFailed:
			fmt.Fprintf(out, "  failed: %s — %s\n", f.SourcePath, f.Reason)
		}
	}
}

// renderAgentsOutput writes the deploy-agents run summary and returns the exit code.
// The contract mirrors renderStandaloneOutput: svcErr → error on errOut + ExitFailure;
// json → domain.RunSummary document; otherwise the human summary with optional dry-run prefix.
func renderAgentsOutput(out, errOut io.Writer, format string, dryRun bool, summary domain.RunSummary, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}
	if strings.EqualFold(format, "json") {
		renderJSON(out, summary)
	} else {
		renderAgentsHuman(out, dryRun, summary)
	}
	return outcomeExitCode(summary.Outcome)
}

// renderAgentsHuman writes a human-readable summary of a deploy-agents run.
func renderAgentsHuman(out io.Writer, dryRun bool, summary domain.RunSummary) {
	prefix := ""
	if dryRun {
		prefix = "dry run: "
	}
	switch summary.Outcome {
	case domain.OutcomeSuccess:
		fmt.Fprintf(out, "%sagents deployment complete\nworkspace: %s\n", prefix, summary.WorkspacePath)
	case domain.OutcomeCompletedWithGaps:
		fmt.Fprintf(out, "%scompleted with skips: some items were skipped and recorded in the TODO list\nworkspace: %s\n", prefix, summary.WorkspacePath)
	case domain.OutcomeFailed:
		fmt.Fprintf(out, "%sagents deployment encountered errors\nworkspace: %s\n", prefix, summary.WorkspacePath)
	default:
		fmt.Fprintf(out, "%soutcome: %s\nworkspace: %s\n", prefix, string(summary.Outcome), summary.WorkspacePath)
	}
}

// renderHooksOutput writes the deploy-hooks run summary and returns the exit code.
// The contract mirrors renderAgentsOutput.
func renderHooksOutput(out, errOut io.Writer, format string, dryRun bool, summary domain.RunSummary, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}
	if strings.EqualFold(format, "json") {
		renderJSON(out, summary)
	} else {
		renderHooksHuman(out, dryRun, summary)
	}
	return outcomeExitCode(summary.Outcome)
}

// renderHooksHuman writes a human-readable summary of a deploy-hooks run.
func renderHooksHuman(out io.Writer, dryRun bool, summary domain.RunSummary) {
	prefix := ""
	if dryRun {
		prefix = "dry run: "
	}
	switch summary.Outcome {
	case domain.OutcomeSuccess:
		fmt.Fprintf(out, "%shooks deployment complete\nworkspace: %s\n", prefix, summary.WorkspacePath)
	case domain.OutcomeCompletedWithGaps:
		fmt.Fprintf(out, "%scompleted with skips: some items were skipped and recorded in the TODO list\nworkspace: %s\n", prefix, summary.WorkspacePath)
	case domain.OutcomeFailed:
		fmt.Fprintf(out, "%shooks deployment encountered errors\nworkspace: %s\n", prefix, summary.WorkspacePath)
	default:
		fmt.Fprintf(out, "%soutcome: %s\nworkspace: %s\n", prefix, string(summary.Outcome), summary.WorkspacePath)
	}
}

// renderCheckIndexOutput writes the index-check result and returns the appropriate exit code.
// When format is "json", the IndexCheckResult is encoded as a single JSON document.
// Otherwise a human-readable summary is written. A non-nil error from the service means
// the check could not be performed; it is written to errOut and ExitFailure is returned.
// When the check ran and found drift, ExitWithSkips (2) is returned so the command is
// usable as a CI/pre-push gate. A clean result (including an absent index) returns ExitSuccess.
func renderCheckIndexOutput(out, errOut io.Writer, format string, result app.IndexCheckResult, svcErr error) int {
	if svcErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", svcErr)
		return ExitFailure
	}
	if strings.EqualFold(format, "json") {
		enc := json.NewEncoder(out)
		_ = enc.Encode(result)
	} else {
		renderCheckIndexHuman(out, result)
	}
	if result.Clean {
		return ExitSuccess
	}
	return ExitWithSkips
}

// renderCheckIndexHuman writes a human-readable summary of the index-check result to out.
// When the index is absent, a single line notes that fact. When drift was found, orphans
// are listed by class. When the catalog is clean, a single clean-state line is printed.
func renderCheckIndexHuman(out io.Writer, result app.IndexCheckResult) {
	if !result.IndexPresent {
		fmt.Fprintf(out, "no index: Catalog/Workflows/Index.md does not exist; nothing to compare\n")
		return
	}
	if result.Clean {
		fmt.Fprintf(out, "clean: no drift between Index.md (%d entries) and disk (%d files)\n",
			result.IndexCount, result.DiskCount)
		return
	}
	if len(result.FileOrphans) > 0 {
		fmt.Fprintf(out, "file-orphans: workflow files on disk not listed in Index.md:\n")
		for _, o := range result.FileOrphans {
			fmt.Fprintf(out, "  %s — %s\n", o.Subject, o.Path)
		}
	}
	if len(result.IndexOrphans) > 0 {
		fmt.Fprintf(out, "index-orphans: index entries with no file on disk:\n")
		for _, o := range result.IndexOrphans {
			fmt.Fprintf(out, "  %s — %s\n", o.Subject, o.Path)
		}
	}
}

// splitTrimmedParts splits a comma-separated string into trimmed, non-empty parts.
// An empty input string yields a non-nil empty slice, preserving the CD-6 nil/empty semantics:
// the caller passes a non-nil empty slice to mean "none, do not ask".
func splitTrimmedParts(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

// renderPromoteHuman writes a human-readable summary of the promote to out. The output
// always names the destination path and the assigned numeric id, and states explicitly that
// nothing was written when result.DryRun is true. Diverted tools recovered from harness
// frontmatter fields and any stripped fields are reported after the primary summary.
func renderPromoteHuman(out io.Writer, result app.PromoteResult) {
	if result.DryRun {
		fmt.Fprintf(out, "dry run: promote validated\nsource: %s\ndestination: %s (not written)\nkey: %s\nid: %s\n",
			result.SourcePath, result.DestinationPath, result.Key, result.NumericID)
	} else {
		fmt.Fprintf(out, "promoted: %s\ndestination: %s\nkey: %s\nid: %s\n",
			result.SourcePath, result.DestinationPath, result.Key, result.NumericID)
	}
	if len(result.DivertedTools) > 0 {
		fmt.Fprintf(out, "diverted tools recovered: %s\n", strings.Join(result.DivertedTools, ", "))
	}
	for _, sf := range result.StrippedFields {
		if len(sf.Values) > 0 {
			fmt.Fprintf(out, "stripped field: %s (%s): %s\n", sf.Key, string(sf.Reason), strings.Join(sf.Values, ", "))
		} else {
			fmt.Fprintf(out, "stripped field: %s (%s)\n", sf.Key, string(sf.Reason))
		}
	}
}
