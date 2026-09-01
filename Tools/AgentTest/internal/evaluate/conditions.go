package evaluate

import (
	"fmt"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// evaluateConditions surfaces every reportable condition in ev that is not
// itself the verdict — a cost-attribution miss, an unterminated invocation
// interval, a degraded protocol extraction, an unmatched invocation, or an
// unreadable orchestration document. None of these may be silently dropped.
func evaluateConditions(ev domain.RunEvidence) []domain.RunCondition {
	var out []domain.RunCondition

	if ev.Cost.Attribution == domain.AttributionUnknownBucket || ev.Cost.Attribution == domain.AttributionUnavailable {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionCostUnattributed,
			Detail: ev.Cost.Detail,
		})
	}

	for _, seq := range ev.ConcurrencyProblems.UnterminatedSeqs {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionUnterminatedInterval,
			Detail: fmt.Sprintf("invocation seq %d has no matching end record", seq),
		})
	}

	for _, r := range ev.Records {
		if r.Kind == domain.RecordStart && r.Message != nil && r.Message.Extraction == domain.ExtractionDegraded {
			out = append(out, domain.RunCondition{
				Kind:   domain.ConditionExtractionDegraded,
				Detail: fmt.Sprintf("invocation seq %d's message was only degraded-recovered, not cleanly parsed", r.Seq),
			})
		}
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUnmatchedInvocation {
			out = append(out, domain.RunCondition{
				Kind:   domain.ConditionUnmatchedInvocation,
				Detail: r.Detail,
			})
		}
	}

	if ev.OrchestrationProblem != "" {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionOrchestrationUnreadable,
			Detail: ev.OrchestrationProblem,
		})
	}

	switch {
	case !ev.LogsProduced:
		// Genuine no-logs: nothing was written anywhere in the session tree.
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionNoLogsProduced,
			Detail: fmt.Sprintf("no logs found under %s", ev.LogRoot),
		})
	case ev.FallbackBucketPresent:
		// Logs exist but landed in the unknown-run fallback bucket, not the
		// expected per-run folder. The run's identity may not have bound
		// correctly. Name the fallback bucket so the user knows where to look.
		out = append(out, domain.RunCondition{
			Kind: domain.ConditionNoLogsProduced,
			Detail: fmt.Sprintf(
				"logs were found in the unknown-run fallback bucket alongside %s — "+
					"the run's identity may not have bound correctly; "+
					"check the unknown-run directory for this session's events",
				ev.LogRoot,
			),
		})
	}

	if ev.SubjectResult.Disposition == domain.DispositionSpawnFailed {
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionSubjectNeverStarted,
			Detail: subjectNeverStartedCause(ev.SubjectResult),
		})
	}

	for _, r := range ev.Records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventUncorrelatedCompletion {
			out = append(out, domain.RunCondition{
				Kind:   domain.ConditionUncorrelatedCompletion,
				Detail: r.Detail,
			})
		}
	}

	switch {
	case ev.Residue.Unreadable != "":
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionLeakedRunState,
			Detail: "run state was unreadable at snapshot time: " + ev.Residue.Unreadable,
		})
	case len(ev.Residue.PendingStubs) > 0 || len(ev.Residue.InFlight) > 0:
		out = append(out, domain.RunCondition{
			Kind:   domain.ConditionLeakedRunState,
			Detail: leakedRunStateDetail(ev.Residue),
		})
	}

	return out
}

// leakedRunStateDetail formats the detail string for ConditionLeakedRunState,
// naming the counts and sequence numbers of each leaked dispatch category.
func leakedRunStateDetail(r domain.StateResidue) string {
	var parts []string
	if len(r.PendingStubs) > 0 {
		seqs := make([]string, len(r.PendingStubs))
		for i, d := range r.PendingStubs {
			seqs[i] = fmt.Sprintf("%d", d.Seq)
		}
		parts = append(parts, fmt.Sprintf("%d pending stub(s) unresolved (seq: %s)", len(r.PendingStubs), strings.Join(seqs, ", ")))
	}
	if len(r.InFlight) > 0 {
		seqs := make([]string, len(r.InFlight))
		for i, d := range r.InFlight {
			seqs[i] = fmt.Sprintf("%d", d.Seq)
		}
		parts = append(parts, fmt.Sprintf("%d in-flight entry(ies) unreleased (seq: %s)", len(r.InFlight), strings.Join(seqs, ", ")))
	}
	return strings.Join(parts, "; ")
}

// subjectNeverStartedCause returns the one-line cause of a spawn failure. It
// uses the first non-blank line of sr.Stderr when one is present; otherwise
// it falls back to naming the exit code so the returned string is never empty.
func subjectNeverStartedCause(sr domain.SubjectResult) string {
	for _, line := range strings.Split(sr.Stderr, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return fmt.Sprintf("subject process did not start (exit code %d)", sr.ExitCode)
}
