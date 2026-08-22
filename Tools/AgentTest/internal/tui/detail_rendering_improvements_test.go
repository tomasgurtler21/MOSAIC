package tui

// detail_rendering_improvements_test.go verifies that the TUI detail screen
// surfaces fields that are present in report.RunReport but currently unread
// by viewDetail:
//
//   - run.RetainedSandboxPath when a sandbox was retained, and an explicit
//     "no sandbox retained" message when none was.
//   - Assertion diagnostic fields (Expected, Actual, Target, Detail) for
//     both passing and failing assertions, not just a.Detail for failed ones.
//   - run.Reasons entries when present.
//   - run.Conditions entries when present.
//
// All data originates from the existing report.RunReport model -- no new
// fields or computation inside internal/tui.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// Retained sandbox path
// ---------------------------------------------------------------------------

// TestDetail_RetainedSandboxPath_ShownWhenPresent verifies that when a run
// has a non-empty RetainedSandboxPath, that path appears on the detail screen.
// A retention feature whose path a user has to guess is not a feature.
func TestDetail_RetainedSandboxPath_ShownWhenPresent(t *testing.T) {
	const sandboxPath = "/tmp/agent-test-sandbox/run-001"
	run := report.RunReport{
		Key:                 domain.RunKey{TestID: "sandbox-test", RunNumber: 1},
		Verdict:             domain.VerdictFail,
		Reasons:             []domain.FailureReason{domain.ReasonAssertion},
		RetainedSandboxPath: sandboxPath,
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionFail,
				Expected: "REJECTED",
				Actual:   "COMPLETED",
				Detail:   "status mismatch",
			},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, sandboxPath) {
		t.Errorf("detail view does not show the retained sandbox path %q; the user needs this path to diagnose the failure.\ngot:\n%s", sandboxPath, view)
	}
}

// TestDetail_RetainedSandboxPath_ExplicitAbsenceWhenEmpty verifies that when
// no sandbox was retained (RetainedSandboxPath is empty), the detail screen
// shows an explicit "no sandbox retained" message rather than silently
// omitting the field. Silence about retention is ambiguous; an explicit
// negative is unambiguous.
func TestDetail_RetainedSandboxPath_ExplicitAbsenceWhenEmpty(t *testing.T) {
	run := report.RunReport{
		Key:                 domain.RunKey{TestID: "no-sandbox-test", RunNumber: 1},
		Verdict:             domain.VerdictPass,
		RetainedSandboxPath: "", // no sandbox retained
		Duration:            time.Second,
		Cost:                domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, "no sandbox retained") {
		t.Errorf("detail view does not show 'no sandbox retained' for a run with no retained sandbox; explicit negative is required.\ngot:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Assertion diagnostic fields for all outcomes
// ---------------------------------------------------------------------------

// TestDetail_FailingAssertion_ShowsAllDiagnosticFields verifies that a
// failing assertion's Expected, Actual, Target and Detail fields all appear
// on the detail screen -- not just Detail as the current implementation
// surfaces.
func TestDetail_FailingAssertion_ShowsAllDiagnosticFields(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "diagnostic-fields-test", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassArtifactCreated,
				Target:   "Orchestration/plan.md",
				Outcome:  domain.AssertionFail,
				Expected: "exists",
				Actual:   "missing",
				Detail:   "artifact was not created at the declared path",
			},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	checks := []struct {
		field string
		value string
	}{
		{"Expected", "exists"},
		{"Actual", "missing"},
		{"Target", "Orchestration/plan.md"},
		{"Detail", "artifact was not created at the declared path"},
	}
	for _, c := range checks {
		if !strings.Contains(view, c.value) {
			t.Errorf("detail view does not show failing assertion's %s field value %q.\ngot:\n%s", c.field, c.value, view)
		}
	}
}

// TestDetail_PassingAssertion_ShowsDiagnosticFields verifies that a passing
// assertion's diagnostic fields also appear on the detail screen. Currently
// viewDetail only surfaces a.Detail for AssertionFail; passing assertions'
// fields are invisible.
func TestDetail_PassingAssertion_ShowsDiagnosticFields(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "pass-fields-test", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionPass,
				Expected: "COMPLETED",
				Actual:   "COMPLETED",
			},
		},
		Duration: time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, "COMPLETED") {
		t.Errorf("detail view does not show passing assertion's Expected/Actual value %q; both pass and fail diagnostic fields must be surfaced.\ngot:\n%s", "COMPLETED", view)
	}
	// The assertion class must also appear so the user knows what was checked.
	if !strings.Contains(view, string(domain.ClassFinalStatus)) {
		t.Errorf("detail view does not mention the passing assertion's class %q.\ngot:\n%s", domain.ClassFinalStatus, view)
	}
}

// TestDetail_PassingAssertion_WithTarget_ShowsTarget verifies that when a
// passing assertion carries a Target field, that target appears on the detail
// screen, so the user knows which specific artifact or group was checked.
func TestDetail_PassingAssertion_WithTarget_ShowsTarget(t *testing.T) {
	const artifactPath = "Orchestration/output.md"
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "pass-target-test", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassArtifactCreated,
				Target:   artifactPath,
				Outcome:  domain.AssertionPass,
				Expected: "exists",
				Actual:   "exists",
			},
		},
		Duration: time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, artifactPath) {
		t.Errorf("detail view does not show passing assertion's Target %q.\ngot:\n%s", artifactPath, view)
	}
}

// TestDetail_MixedAssertions_BothPassAndFailShown verifies that when a run
// has both passing and failing assertions, the detail screen shows diagnostic
// fields from both, not only from the failed ones.
func TestDetail_MixedAssertions_BothPassAndFailShown(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "mixed-assertions-test", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionPass,
				Expected: "COMPLETED",
				Actual:   "COMPLETED",
			},
			{
				Class:    domain.ClassFinalPhase,
				Outcome:  domain.AssertionFail,
				Expected: "closed",
				Actual:   "open",
				Detail:   "phase did not match expected value",
			},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.02, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	// The passing assertion's values should appear.
	if !strings.Contains(view, "COMPLETED") {
		t.Errorf("detail view does not show the passing assertion's value %q.\ngot:\n%s", "COMPLETED", view)
	}
	// The failing assertion's values should also appear.
	if !strings.Contains(view, "closed") || !strings.Contains(view, "open") {
		t.Errorf("detail view does not show the failing assertion's Expected/Actual values.\ngot:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Reasons
// ---------------------------------------------------------------------------

// TestDetail_Reasons_ShownWhenPresent verifies that run.Reasons entries are
// visible on the TUI detail screen. Currently viewDetail does not read
// run.Reasons at all.
func TestDetail_Reasons_ShownWhenPresent(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "reasons-test", RunNumber: 1},
		Verdict: domain.VerdictFail,
		Reasons: []domain.FailureReason{domain.ReasonAssertion},
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionFail,
				Expected: "REJECTED",
				Actual:   "COMPLETED",
				Detail:   "status mismatch",
			},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, string(domain.ReasonAssertion)) {
		t.Errorf("detail view does not mention run.Reasons value %q; the failure reason must be visible on the detail screen.\ngot:\n%s", domain.ReasonAssertion, view)
	}
}

// TestDetail_Reasons_MultipleReasonsAllShown verifies that when a run carries
// multiple Reasons, all of them appear on the detail screen.
func TestDetail_Reasons_MultipleReasonsAllShown(t *testing.T) {
	run := report.RunReport{
		Key:      domain.RunKey{TestID: "multi-reasons-test", RunNumber: 1},
		Verdict:  domain.VerdictFail,
		Reasons:  []domain.FailureReason{domain.ReasonAssertion, domain.ReasonEchoMismatch},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	for _, reason := range []domain.FailureReason{domain.ReasonAssertion, domain.ReasonEchoMismatch} {
		if !strings.Contains(view, string(reason)) {
			t.Errorf("detail view does not mention run.Reasons value %q; all failure reasons must be visible.\ngot:\n%s", reason, view)
		}
	}
}

// TestDetail_Reasons_AbsentWhenNone verifies that a passing run with no
// Reasons does not show spurious reason entries on the detail screen.
func TestDetail_Reasons_AbsentWhenNone(t *testing.T) {
	run := report.RunReport{
		Key:      domain.RunKey{TestID: "pass-no-reasons", RunNumber: 1},
		Verdict:  domain.VerdictPass,
		Duration: time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	for _, reason := range []domain.FailureReason{
		domain.ReasonAssertion, domain.ReasonTimeout,
		domain.ReasonStateIntegrity, domain.ReasonEchoMismatch,
	} {
		if strings.Contains(view, string(reason)) {
			t.Errorf("detail view for a passing run with no Reasons mentions %q; no reasons should appear.\ngot:\n%s", reason, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Conditions
// ---------------------------------------------------------------------------

// TestDetail_Conditions_ShownWhenPresent verifies that run.Conditions entries
// are visible on the TUI detail screen. The current viewDetail does not read
// run.Conditions.
func TestDetail_Conditions_ShownWhenPresent(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "conditions-test", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionCostUnattributed, Detail: "log root missing for this run"},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.00, Attribution: domain.AttributionUnknownBucket},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	if !strings.Contains(view, string(domain.ConditionCostUnattributed)) {
		t.Errorf("detail view does not mention run.Conditions kind %q; conditions must be visible on the detail screen.\ngot:\n%s", domain.ConditionCostUnattributed, view)
	}
	if !strings.Contains(view, "log root missing for this run") {
		t.Errorf("detail view does not mention the condition detail %q; the detail is needed to diagnose the condition.\ngot:\n%s", "log root missing for this run", view)
	}
}

// TestDetail_Conditions_MultipleConditionsAllShown verifies that when a run
// carries multiple Conditions, all of them appear on the detail screen.
func TestDetail_Conditions_MultipleConditionsAllShown(t *testing.T) {
	run := report.RunReport{
		Key:     domain.RunKey{TestID: "multi-conditions-test", RunNumber: 1},
		Verdict: domain.VerdictPass,
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionCostUnattributed, Detail: "log root missing"},
			{Kind: domain.ConditionUnterminatedInterval, Detail: "invocation seq 2 has no end record"},
		},
		Duration: 2 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.00, Attribution: domain.AttributionUnknownBucket},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	for _, cond := range run.Conditions {
		if !strings.Contains(view, string(cond.Kind)) {
			t.Errorf("detail view does not mention condition kind %q; all conditions must be visible.\ngot:\n%s", cond.Kind, view)
		}
		if !strings.Contains(view, cond.Detail) {
			t.Errorf("detail view does not mention condition detail %q.\ngot:\n%s", cond.Detail, view)
		}
	}
}

// TestDetail_Conditions_AbsentWhenNone verifies that a passing run with no
// Conditions does not show spurious condition entries on the detail screen.
func TestDetail_Conditions_AbsentWhenNone(t *testing.T) {
	run := report.RunReport{
		Key:      domain.RunKey{TestID: "pass-no-conditions", RunNumber: 1},
		Verdict:  domain.VerdictPass,
		Duration: time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.01, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	knownConditions := []domain.RunConditionKind{
		domain.ConditionCostUnattributed,
		domain.ConditionUnterminatedInterval,
		domain.ConditionExtractionDegraded,
		domain.ConditionUnmatchedInvocation,
	}
	for _, kind := range knownConditions {
		if strings.Contains(view, string(kind)) {
			t.Errorf("detail view for a run with no conditions mentions condition kind %q; no conditions should appear.\ngot:\n%s", kind, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Combined: all new fields in a single run
// ---------------------------------------------------------------------------

// TestDetail_AllNewFields_ShownTogether verifies that a run carrying all of
// the newly-required fields (RetainedSandboxPath, passing assertion fields,
// Reasons, Conditions) renders them all on the detail screen simultaneously,
// with no field hiding another.
func TestDetail_AllNewFields_ShownTogether(t *testing.T) {
	const sandboxPath = "/tmp/agent-test-sandbox/combined-run"
	run := report.RunReport{
		Key:                 domain.RunKey{TestID: "all-fields-test", RunNumber: 1},
		Verdict:             domain.VerdictFail,
		Reasons:             []domain.FailureReason{domain.ReasonAssertion},
		RetainedSandboxPath: sandboxPath,
		Assertions: []domain.AssertionResult{
			{
				Class:    domain.ClassFinalStatus,
				Outcome:  domain.AssertionPass,
				Expected: "COMPLETED",
				Actual:   "COMPLETED",
			},
			{
				Class:    domain.ClassFinalPhase,
				Outcome:  domain.AssertionFail,
				Expected: "closed",
				Actual:   "open",
				Detail:   "phase mismatch",
			},
		},
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionCostUnattributed, Detail: "log root absent"},
		},
		Duration: 3 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.05, Attribution: domain.AttributionAttributed},
	}
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	checks := []struct {
		desc  string
		value string
	}{
		{"retained sandbox path", sandboxPath},
		{"passing assertion Expected value", "COMPLETED"},
		{"failing assertion Expected value", "closed"},
		{"failing assertion Actual value", "open"},
		{"failure reason", string(domain.ReasonAssertion)},
		{"condition kind", string(domain.ConditionCostUnattributed)},
		{"condition detail", "log root absent"},
	}
	for _, c := range checks {
		if !strings.Contains(view, c.value) {
			t.Errorf("detail view does not show %s (%q).\ngot:\n%s", c.desc, c.value, view)
		}
	}
}
