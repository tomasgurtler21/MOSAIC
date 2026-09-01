package evaluate_test

// Tests for Stage 8 (T8.2): the no-logs condition's message must never claim
// "no logs found" when logs for the same session exist under an identifiable
// different bucket.
//
// The new evidence fields LogsProduced and FallbackBucketPresent let
// evaluateConditions distinguish three states:
//
//  1. LogsProduced == false: genuine no-logs — detail names the log root.
//  2. LogsProduced == true && FallbackBucketPresent == true: logs exist but
//     in the fallback bucket alongside the run-id folder — a condition fires
//     with detail naming the fallback bucket, never saying "no logs found".
//  3. LogsProduced == true && FallbackBucketPresent == false: logs are in the
//     per-run folder — no no-logs condition fires.
//
// All tests build on baseEvidence() from helpers_test.go (a minimal clean
// run) and mutate only the fields their case is about.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// TestEvaluate_FallbackBucketPresent_ConditionFiresWhenLogsMayBeInFallback
// asserts that a condition is still raised when FallbackBucketPresent is true
// (even though LogsProduced is true). The run's events may have landed in the
// fallback bucket rather than the expected run-id path — the user needs to
// know, even though the tool still saw some logs.
func TestEvaluate_FallbackBucketPresent_ConditionFiresWhenLogsMayBeInFallback(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = true
	ev.FallbackBucketPresent = true

	got := evaluate.Evaluate(ev)

	hasNoLogsCondition := false
	for _, c := range got.Conditions {
		if c.Kind == domain.ConditionNoLogsProduced {
			hasNoLogsCondition = true
		}
	}
	if !hasNoLogsCondition {
		t.Errorf(
			"Conditions = %+v, want ConditionNoLogsProduced — "+
				"when the fallback bucket is present the user must be told that events may have landed in it, "+
				"not under the expected run-id path; silencing the condition hides the misbinding",
			got.Conditions,
		)
	}
}

// TestEvaluate_FallbackBucketPresent_ConditionDetailDoesNotSayNoLogsFound
// is the primary AC8.2 guard: when FallbackBucketPresent is true, the
// condition detail must NOT contain the phrase "no logs found". That phrase
// is misleading because logs DO exist — they are just in the wrong bucket.
func TestEvaluate_FallbackBucketPresent_ConditionDetailDoesNotSayNoLogsFound(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = true
	ev.FallbackBucketPresent = true
	ev.LogRoot = "/sandbox/OrchestrationLogs/20260822T120000Z-buck"

	got := evaluate.Evaluate(ev)

	for _, c := range got.Conditions {
		if c.Kind != domain.ConditionNoLogsProduced {
			continue
		}
		lower := strings.ToLower(c.Detail)
		if strings.Contains(lower, "no logs found") {
			t.Errorf(
				"ConditionNoLogsProduced.Detail = %q, must NOT contain 'no logs found' when FallbackBucketPresent is true — "+
					"that phrase is false: logs DO exist for this session, just not under the expected run-id path",
				c.Detail,
			)
		}
	}
}

// TestEvaluate_FallbackBucketPresent_ConditionDetailNamesFallbackBucket
// asserts that the condition detail positively names the fallback bucket
// location ("unknown-run") when FallbackBucketPresent is true, so the user
// knows exactly where to look rather than being told nothing was found.
func TestEvaluate_FallbackBucketPresent_ConditionDetailNamesFallbackBucket(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = true
	ev.FallbackBucketPresent = true
	ev.LogRoot = "/sandbox/OrchestrationLogs/20260822T120000Z-buck"

	got := evaluate.Evaluate(ev)

	for _, c := range got.Conditions {
		if c.Kind != domain.ConditionNoLogsProduced {
			continue
		}
		// The detail must name the fallback bucket in some identifiable way.
		// "unknown-run" is its standard name; the detail must include it so a
		// user who inspects OrchestrationLogs/ knows which directory to check.
		if !strings.Contains(c.Detail, "unknown-run") {
			t.Errorf(
				"ConditionNoLogsProduced.Detail = %q, want it to name the fallback bucket ('unknown-run') "+
					"so the user knows where the session's events actually landed",
				c.Detail,
			)
		}
	}
}

// TestEvaluate_LogsProducedTrueAndFallbackAbsent_NoNoLogsCondition asserts
// that no ConditionNoLogsProduced is raised when LogsProduced is true and
// FallbackBucketPresent is false: logs are in the per-run folder, which is
// the expected case and requires no reporting.
func TestEvaluate_LogsProducedTrueAndFallbackAbsent_NoNoLogsCondition(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = true
	ev.FallbackBucketPresent = false

	got := evaluate.Evaluate(ev)

	for _, c := range got.Conditions {
		if c.Kind == domain.ConditionNoLogsProduced {
			t.Errorf(
				"Conditions contains ConditionNoLogsProduced (detail: %q), want no such condition — "+
					"LogsProduced is true and no fallback bucket is present, so the run logged normally",
				c.Detail,
			)
		}
	}
}

// TestEvaluate_LogsProducedFalse_ConditionDetailNamesLogRoot asserts the
// existing AC8.1 behaviour is preserved: when LogsProduced is false (no logs
// anywhere), the condition detail names the queried log root, not the
// fallback bucket (which may not even exist).
func TestEvaluate_LogsProducedFalse_ConditionDetailNamesLogRoot(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/20260822T120000Z-buck"
	ev := baseEvidence()
	ev.LogsProduced = false
	ev.FallbackBucketPresent = false
	ev.LogRoot = logRoot

	got := evaluate.Evaluate(ev)

	cond := findCondition(t, got.Conditions, domain.ConditionNoLogsProduced)
	if !strings.Contains(cond.Detail, logRoot) {
		t.Errorf(
			"ConditionNoLogsProduced.Detail = %q, want it to name the queried log root %q "+
				"when no logs exist anywhere — the user needs to know which path was searched",
			cond.Detail, logRoot,
		)
	}
}

// TestEvaluate_FallbackBucketPresent_ConditionAndNoLogsAreDistinct asserts
// that the fallback-bucket condition does not suppress the genuine-no-logs
// path: when LogsProduced is false and FallbackBucketPresent is false, only
// the genuine-no-logs detail fires (not a fallback-bucket message).
func TestEvaluate_FallbackBucketPresent_ConditionAndNoLogsAreDistinct(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/run-99"
	ev := baseEvidence()
	ev.LogsProduced = false
	ev.FallbackBucketPresent = false
	ev.LogRoot = logRoot

	got := evaluate.Evaluate(ev)

	for _, c := range got.Conditions {
		if c.Kind != domain.ConditionNoLogsProduced {
			continue
		}
		// Detail must name the log root (genuine no-logs case), not
		// "unknown-run" (which would imply the fallback was found).
		if strings.Contains(c.Detail, "unknown-run") && !strings.Contains(c.Detail, logRoot) {
			t.Errorf(
				"ConditionNoLogsProduced.Detail = %q, names 'unknown-run' but not the queried log root — "+
					"when FallbackBucketPresent is false the detail must describe the genuine no-logs case, not a fallback bucket",
				c.Detail,
			)
		}
	}
}
