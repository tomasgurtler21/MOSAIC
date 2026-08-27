package evaluate

import (
	"fmt"

	"mosaic-agent-test/internal/domain"
)

// Aggregate combines the results of a test's repetitions against its
// declared pass rate.
//
// An excluded run is skipped for the denominator and recorded in Exclusions
// with the reason it was excluded, so the report can say why a run did not
// count without a reader inspecting a retained sandbox. len(Exclusions)
// always equals Excluded.
//
// InfrastructureFailure is set when two or more runs in one repetition set
// were excluded for the SAME reason — a fault that recurred after its single
// retry — or when a runner error carried the infrastructure reason directly,
// which needs no threshold because it was never retried. Requiring the same
// reason preserves the existing state-integrity behaviour exactly and stops
// one state-integrity fault plus one unrelated spawn failure from being read
// as a recurrence.
func Aggregate(results []domain.TestResult, policy domain.RepetitionPolicy) domain.AggregateResult {
	out := domain.AggregateResult{
		Runs:             results,
		RequiredPassRate: policy.PassRate,
		TotalCost:        domain.CostReport{Attribution: domain.AttributionAttributed},
		Exclusions:       []domain.ExcludedRun{}, // always initialized; wire renders [] not null
	}

	exclusionReasonCounts := make(map[domain.ExclusionReason]int)
	infrastructureReason := false

	for _, r := range results {
		if out.TestName == "" {
			out.TestName = r.Key.TestName
		}
		if out.NumericID == 0 {
			out.NumericID = r.NumericID
		}

		reason := ExclusionOf(r)
		if reason != "" {
			exclusionReasonCounts[reason]++
			out.Excluded++
			out.Exclusions = append(out.Exclusions, domain.ExcludedRun{
				Key:               r.Key,
				Reason:            reason,
				TerminationReason: r.TerminationReason,
				Detail:            exclusionDetail(r, reason),
			})
			continue
		}

		out.Counted++
		out.TotalCost = out.TotalCost.Add(r.Cost)
		if r.Verdict == domain.VerdictPass {
			out.Passed++
		}
		if hasReason(r.Reasons, domain.ReasonInfrastructure) {
			infrastructureReason = true
		}
	}

	if out.Counted > 0 {
		out.PassRate = float64(out.Passed) / float64(out.Counted)
	}

	// InfrastructureFailure is set when:
	//  - any exclusion reason recurred (same reason appearing 2+ times means
	//    the fault persisted after its single retry), or
	//  - a runner error carried ReasonInfrastructure directly (never retried,
	//    so no two-occurrence threshold applies).
	for _, count := range exclusionReasonCounts {
		if count >= 2 {
			out.InfrastructureFailure = true
			break
		}
	}
	if infrastructureReason {
		out.InfrastructureFailure = true
	}

	switch {
	case out.InfrastructureFailure:
		out.Verdict = domain.VerdictFail
		out.Reasons = append(out.Reasons, domain.ReasonInfrastructure)
	case out.Counted == 0:
		out.Verdict = domain.VerdictFail
	case out.PassRate >= policy.PassRate:
		out.Verdict = domain.VerdictPass
	default:
		out.Verdict = domain.VerdictFail
	}

	return out
}

// exclusionDetail returns a human-readable explanation for why a run was
// excluded, sufficient to understand the exclusion without inspecting the
// retained sandbox. Never returns the empty string.
func exclusionDetail(r domain.TestResult, reason domain.ExclusionReason) string {
	switch reason {
	case domain.ExclusionSpawnFailed:
		if r.SubjectResult.Stderr != "" {
			return "harness process exited non-zero: " + r.SubjectResult.Stderr
		}
		if r.SubjectResult.ExitCode != 0 {
			return fmt.Sprintf("harness process exited non-zero (exit status %d)", r.SubjectResult.ExitCode)
		}
		return "harness process exited non-zero"
	case domain.ExclusionStateIntegrity:
		return "lock was reclaimed; one or more state updates may have been lost"
	default:
		return string(reason)
	}
}

// hasReason reports whether reasons contains want.
func hasReason(reasons []domain.FailureReason, want domain.FailureReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
