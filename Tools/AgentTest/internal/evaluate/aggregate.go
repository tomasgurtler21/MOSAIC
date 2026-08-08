package evaluate

import "mosaic-agent-test/internal/domain"

// Aggregate combines the results of a test's repetitions against its
// declared pass rate.
//
// A run that failed for state integrity is excluded from the denominator:
// the aggregate must measure the subject, not the tool. The caller retries
// such a run once; a second occurrence in results is reported as an
// infrastructure failure rather than as a subject regression.
func Aggregate(results []domain.TestResult, policy domain.RepetitionPolicy) domain.AggregateResult {
	out := domain.AggregateResult{
		Runs:      results,
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	stateIntegrityCount := 0
	for _, r := range results {
		if out.TestID == "" {
			out.TestID = r.Key.TestID
		}
		if NeedsRetry(r) {
			stateIntegrityCount++
			out.Excluded++
			continue
		}
		out.Counted++
		out.TotalCost = out.TotalCost.Add(r.Cost)
		if r.Verdict == domain.VerdictPass {
			out.Passed++
		}
	}

	if out.Counted > 0 {
		out.PassRate = float64(out.Passed) / float64(out.Counted)
	}

	out.InfrastructureFailure = stateIntegrityCount >= 2

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
