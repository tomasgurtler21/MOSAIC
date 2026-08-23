package evaluate

import "mosaic-agent-test/internal/domain"

// ExclusionOf reports why r would be excluded from the pass-rate denominator,
// or the empty string when it would be counted.
//
// State integrity takes precedence over spawn failure when both are present,
// so one run yields exactly one exclusion reason and the two are never
// double-counted.
//
// NeedsRetry(r) is true exactly when ExclusionOf(r) is non-empty.
func ExclusionOf(r domain.TestResult) domain.ExclusionReason {
	// State integrity takes precedence: a lock reclaim makes all subsequent
	// verdicts suspect, regardless of other disposal markers.
	if hasReason(r.Reasons, domain.ReasonStateIntegrity) {
		return domain.ExclusionStateIntegrity
	}
	// Spawn failure: the harness process exited non-zero, so the subject may
	// never have run. Evidence about the tool's environment, not the subject.
	if r.SubjectResult.Disposition == domain.DispositionSpawnFailed {
		return domain.ExclusionSpawnFailed
	}
	return ""
}
