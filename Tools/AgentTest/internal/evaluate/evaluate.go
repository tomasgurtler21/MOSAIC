// Package evaluate turns one run's evidence into a verdict with
// per-assertion detail, as a pure function.
//
// Pure — no I/O. The same evidence always yields the same verdict, so a
// run's evidence can be re-evaluated after an assertion is corrected
// without re-spawning an agent.
package evaluate

import (
	"sort"

	"mosaic-agent-test/internal/domain"
)

// Evaluate turns one run's evidence into a verdict with per-assertion
// detail. It performs no I/O: durations, file listings and cost arrive as
// evidence, gathered by the caller.
func Evaluate(ev domain.RunEvidence) domain.TestResult {
	// Expand {run_id} placeholders in a local copy of the assertions before
	// comparing declared paths against observed paths. ev is already a copy
	// (value receiver), so the caller's original evidence is not modified.
	ev.Definition.Assertions = ev.Definition.Assertions.ExpandRunID(ev.Key.RunID)

	assertions := evaluateAssertions(ev)
	conditions := evaluateConditions(ev)

	// When the subject process refused to start, surface the real one-line
	// cause and mark every assertion as not-evaluated. Nothing ran, so no
	// assertion could have been satisfied or failed — reporting AssertionFail
	// for assertions that nothing could have evaluated is misleading.
	if ev.SubjectResult.Disposition == domain.DispositionSpawnFailed {
		cause := findConditionDetail(conditions, domain.ConditionSubjectNeverStarted)
		for i := range assertions {
			assertions[i].Outcome = domain.AssertionNotEvaluated
			assertions[i].Detail = cause
		}
		return domain.TestResult{
			Key:                 ev.Key,
			Verdict:             domain.VerdictFail,
			Reasons:             []domain.FailureReason{domain.ReasonInfrastructure},
			Assertions:          assertions,
			Conditions:          conditions,
			Cost:                ev.Cost,
			Duration:            ev.Duration,
			RetainedSandboxPath: ev.RetainedSandboxPath,
			SubjectResult:       ev.SubjectResult,
			SubjectVersion:      ev.SubjectVersion,
			SubjectModel:        ev.SubjectModel,
			StubModel:           ev.StubModel,
			HarnessID:           ev.HarnessID,
			TerminationReason:   string(ev.SubjectResult.Disposition),
			Version:             ev.Definition.Version,
			NumericID:           ev.Definition.NumericID,
		}
	}

	negative := ev.Definition.Negative
	if negative {
		invertAssertions(assertions)
	}

	assertionFailed := false
	for _, ar := range assertions {
		if ar.Class == domain.ClassEchoFidelity {
			continue
		}
		if ar.Outcome == domain.AssertionFail {
			assertionFailed = true
		}
	}

	echoFailed := false
	for _, ar := range assertions {
		if ar.Class == domain.ClassEchoFidelity && ar.Outcome == domain.AssertionFail {
			echoFailed = true
		}
	}

	stateIntegrity := hasRunEvent(ev.Records, domain.RunEventLockReclaimed)
	timedOut := ev.SubjectResult.Disposition == domain.DispositionTimedOut

	// A definition that declared one or more assertion classes but whose
	// evaluation produced zero evaluated results must never read as a pass,
	// whatever caused the zero — including after negative-test inversion,
	// which cannot turn an empty set into a passing one. A definition that
	// genuinely declared nothing is unaffected: this guard only fires when
	// something was declared.
	vacuousAssertions := assertionsDeclared(ev.Definition.Assertions) && len(assertions) == 0

	echoFidelityMode := domain.EffectiveEchoFidelity(ev.Definition.Settings)

	var reasons []domain.FailureReason
	if assertionFailed || vacuousAssertions {
		reasons = append(reasons, domain.ReasonAssertion)
	}
	if echoFailed && echoFidelityMode != domain.EchoFidelityAdvisory {
		reasons = append(reasons, domain.ReasonEchoMismatch)
	}
	if stateIntegrity {
		reasons = append(reasons, domain.ReasonStateIntegrity)
	}
	if timedOut {
		reasons = append(reasons, domain.ReasonTimeout)
	}

	verdict := domain.VerdictPass
	switch {
	case timedOut:
		verdict = domain.VerdictTimeout
	case len(reasons) > 0:
		verdict = domain.VerdictFail
	}

	return domain.TestResult{
		Key:                 ev.Key,
		Verdict:             verdict,
		Reasons:             reasons,
		Assertions:          assertions,
		Conditions:          conditions,
		NegativeApplied:     negative,
		Cost:                ev.Cost,
		Duration:            ev.Duration,
		RetainedSandboxPath: ev.RetainedSandboxPath,
		SubjectResult:       ev.SubjectResult,
		SubjectVersion:      ev.SubjectVersion,
		SubjectModel:        ev.SubjectModel,
		StubModel:           ev.StubModel,
		HarnessID:           ev.HarnessID,
		TerminationReason:   string(ev.SubjectResult.Disposition),
		Version:             ev.Definition.Version,
		NumericID:           ev.Definition.NumericID,
	}
}

// invertAssertions flips each declared assertion's outcome in place.
// ClassEchoFidelity is never touched — a negative test cannot "expect" the
// infrastructure to be broken. AssertionNotEvaluated is left as-is: it is
// neither a pass nor a fail to invert.
func invertAssertions(assertions []domain.AssertionResult) {
	for i := range assertions {
		if assertions[i].Class == domain.ClassEchoFidelity {
			continue
		}
		switch assertions[i].Outcome {
		case domain.AssertionPass:
			assertions[i].Outcome = domain.AssertionFail
		case domain.AssertionFail:
			assertions[i].Outcome = domain.AssertionPass
		}
	}
}

func hasRunEvent(records []domain.LogRecord, kind domain.RunEventKind) bool {
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == kind {
			return true
		}
	}
	return false
}

// NeedsRetry reports whether a result is an infrastructure fault the caller
// should retry rather than count. Pure predicate, so the retry rule is
// testable without a runner.
//
// True for two cases, both of which are evidence about the tool's environment
// rather than about the subject:
//
//   - the state-integrity failure reason: a run whose lock was reclaimed may
//     rest on lost state;
//   - the spawn-failed subject disposition: the harness process exited
//     non-zero, so the subject may never have run at all.
//
// NeedsRetry(r) is true exactly when ExclusionOf(r) is non-empty.
func NeedsRetry(r domain.TestResult) bool {
	return ExclusionOf(r) != ""
}

// findConditionDetail returns the Detail of the first condition matching kind,
// or the empty string when none is found.
func findConditionDetail(conditions []domain.RunCondition, kind domain.RunConditionKind) string {
	for _, c := range conditions {
		if c.Kind == kind {
			return c.Detail
		}
	}
	return ""
}

// sortedKeys returns m's keys in ascending order, so a result built from a
// map is deterministic despite Go's randomized map iteration.
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
