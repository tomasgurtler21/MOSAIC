package tui

// detail_test.go verifies the drill-down detail screen: a finished test's
// detail shows its failed assertions with enough context to act on, its
// duration and its cost, and renders an assertion failure, a timeout, a
// state-integrity failure and a cost-attribution miss as visibly different
// things (AC16.3).
//
// An infrastructure fault that reads like a subject regression sends the
// reader down the wrong path — which is the failure this tool exists to
// prevent, so its own interface must not commit it. This file tests that
// obligation directly: the four classes must never render identically, and
// the class distinction is derived from report.Classify, not re-invented
// here.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// fixtureRunReport builds a report.RunReport exhibiting exactly one outcome
// class, for detail-view rendering tests.
func fixtureRunReport(testID string, class report.OutcomeClass) report.RunReport {
	base := report.RunReport{
		Key:      domain.RunKey{TestName: testID, RunNumber: 1},
		Duration: 3 * time.Second,
		Cost:     domain.CostReport{TotalUSD: 0.15, Attribution: domain.AttributionAttributed},
	}
	switch class {
	case report.ClassPass:
		base.Verdict = domain.VerdictPass
	case report.ClassAssertionFailure:
		base.Verdict = domain.VerdictFail
		base.Reasons = []domain.FailureReason{domain.ReasonAssertion}
		base.Assertions = []domain.AssertionResult{{
			Class:    domain.ClassFinalStatus,
			Target:   "",
			Outcome:  domain.AssertionFail,
			Expected: "REJECTED",
			Actual:   "COMPLETED",
			Detail:   "final status did not match the declared expectation",
		}}
	case report.ClassTimeout:
		base.Verdict = domain.VerdictTimeout
		base.Reasons = []domain.FailureReason{domain.ReasonTimeout}
	case report.ClassStateIntegrity:
		base.Verdict = domain.VerdictFail
		base.Reasons = []domain.FailureReason{domain.ReasonStateIntegrity}
	case report.ClassCostUnattributed:
		base.Verdict = domain.VerdictPass
		base.Conditions = []domain.RunCondition{{Kind: domain.ConditionCostUnattributed, Detail: "cost landed in the unknown-run bucket"}}
		base.Cost = domain.CostReport{Attribution: domain.AttributionUnknownBucket}
	}
	return base
}

// modelWithFinishedResult builds a Model already on the results screen with
// one finished test detailed in its result model, so detail-screen tests
// need not drive suite selection and completion to reach ScreenDetail.
//
// It relies only on exported behaviour (Fold, Update) plus the
// SuiteFinishedMsg the design documents as carrying the full result model
// into the loop — never on unexported fields — so it exercises the same
// path a real run does.
func modelWithFinishedResult(t *testing.T, run report.RunReport) Model {
	t.Helper()

	testReport := report.TestReport{
		TestName: run.Key.TestName,
		Runs:     []report.RunReport{run},
	}
	result := report.Build("suite-under-test", time.Now(), time.Now(), []report.TestReport{testReport}, "")

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	// Drill into the (only) finished test's detail.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	return m
}

// ---------------------------------------------------------------------------
// The four outcome classes render as visibly distinct things
// ---------------------------------------------------------------------------

func TestDetail_OutcomeClassesRenderDistinctly(t *testing.T) {
	classes := []report.OutcomeClass{
		report.ClassAssertionFailure,
		report.ClassTimeout,
		report.ClassStateIntegrity,
		report.ClassCostUnattributed,
	}

	views := map[report.OutcomeClass]string{}
	for _, class := range classes {
		run := fixtureRunReport("test-under-test", class)
		m := modelWithFinishedResult(t, run)
		if m.Screen() != ScreenDetail {
			t.Fatalf("Screen() = %q after drilling into the finished test, want %q", m.Screen(), ScreenDetail)
		}
		views[class] = safeView(t, m)
	}

	for i, a := range classes {
		for _, b := range classes[i+1:] {
			if views[a] == views[b] {
				t.Errorf("detail view for %q and %q render identically; the four outcome classes must be visibly distinct", a, b)
			}
		}
	}

	// Each view must reference its own class from the shared vocabulary
	// (report.Classify), so the distinction is derived rather than
	// reinvented per screen.
	for _, class := range classes {
		if !strings.Contains(views[class], string(class)) {
			t.Errorf("detail view for %q does not mention its own outcome class %q", class, class)
		}
	}
}

// ---------------------------------------------------------------------------
// Failed assertions show enough context to act on
// ---------------------------------------------------------------------------

func TestDetail_AssertionFailure_ShowsActionableContext(t *testing.T) {
	run := fixtureRunReport("test-under-test", report.ClassAssertionFailure)
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	want := run.Assertions[0].Detail
	if !strings.Contains(view, want) {
		t.Errorf("detail view does not contain the failed assertion's detail %q; a user cannot act on the failure without it", want)
	}
}

// ---------------------------------------------------------------------------
// Duration and cost are shown
// ---------------------------------------------------------------------------

func TestDetail_ShowsDurationAndCost(t *testing.T) {
	// Duration/cost surfacing must not depend on the outcome class carrying
	// assertion detail: ClassTimeout carries no AssertionResult at all, so
	// this is checked for both an assertion-bearing class and one that has
	// none, rather than for ClassAssertionFailure alone.
	classes := []report.OutcomeClass{report.ClassAssertionFailure, report.ClassTimeout}
	for _, class := range classes {
		t.Run(string(class), func(t *testing.T) {
			run := fixtureRunReport("test-under-test", class)
			m := modelWithFinishedResult(t, run)
			view := safeView(t, m)

			if view == "" {
				t.Fatalf("detail view is empty")
			}
			// The exact formatting is an implementation decision; what is
			// tested is that the duration and the cost figure the
			// RunReport carries both reach the rendered text in some
			// form.
			if !strings.Contains(view, "3") { // 3s duration
				t.Errorf("detail view does not appear to mention the run's duration (%v)", run.Duration)
			}
			if !strings.Contains(view, "0.15") {
				t.Errorf("detail view does not appear to mention the run's cost (%v)", run.Cost.TotalUSD)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// A passing run's detail does not claim any failure class
// ---------------------------------------------------------------------------

func TestDetail_PassingRun_ShowsNoFailureClass(t *testing.T) {
	run := fixtureRunReport("test-under-test", report.ClassPass)
	m := modelWithFinishedResult(t, run)
	view := safeView(t, m)

	failureClasses := []report.OutcomeClass{
		report.ClassAssertionFailure,
		report.ClassTimeout,
		report.ClassStateIntegrity,
		report.ClassCostUnattributed,
	}
	for _, class := range failureClasses {
		if strings.Contains(view, string(class)) {
			t.Errorf("passing run's detail view mentions failure class %q; a pass must not read like a fault", class)
		}
	}
}
