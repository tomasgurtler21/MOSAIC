package report_test

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// --- Session model and combined figures ---

func TestNewSession_FromMultipleSuiteResults_CombinesVerdictCounts(t *testing.T) {
	// Arrange
	suiteA := report.Result{
		SuiteID: "suite-a",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 3,
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{TotalUSD: 1.00, Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}
	suiteB := report.Result{
		SuiteID: "suite-b",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 2,
			domain.VerdictFail: 2,
		},
		TotalCost:              domain.CostReport{TotalUSD: 2.50, Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}
	suiteC := report.Result{
		SuiteID: "suite-c",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 4,
		},
		TotalCost:              domain.CostReport{TotalUSD: 0.75, Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB, suiteC}, nil, false)

	// Assert combined verdict counts
	if session.Counts[domain.VerdictPass] != 9 {
		t.Errorf("combined PASS count = %d, want 9 (3+2+4)", session.Counts[domain.VerdictPass])
	}
	if session.Counts[domain.VerdictFail] != 3 {
		t.Errorf("combined FAIL count = %d, want 3 (1+2+0)", session.Counts[domain.VerdictFail])
	}
}

func TestNewSession_FromMultipleSuiteResults_CombinesTotalCost(t *testing.T) {
	// Arrange
	suiteA := report.Result{
		SuiteID:   "suite-a",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{TotalUSD: 1.00, Attribution: domain.AttributionAttributed},
	}
	suiteB := report.Result{
		SuiteID:   "suite-b",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{TotalUSD: 2.50, Attribution: domain.AttributionAttributed},
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB}, nil, false)

	// Assert combined total cost (within floating-point tolerance)
	const want = 3.50
	diff := session.TotalCost.TotalUSD - want
	if diff > 0.001 || diff < -0.001 {
		t.Errorf("combined TotalCost.TotalUSD = %f, want %f (1.00 + 2.50)", session.TotalCost.TotalUSD, want)
	}
}

func TestNewSession_FromMultipleSuiteResults_CombinesInfrastructureFailures(t *testing.T) {
	// Arrange
	suiteA := report.Result{
		SuiteID:                "suite-a",
		Counts:                 map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 2,
	}
	suiteB := report.Result{
		SuiteID:                "suite-b",
		Counts:                 map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 3,
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB}, nil, false)

	// Assert combined infrastructure failures
	if session.InfrastructureFailures != 5 {
		t.Errorf("combined InfrastructureFailures = %d, want 5 (2+3)", session.InfrastructureFailures)
	}
}

func TestNewSession_FromSingleSuiteResult_ReportsThatSuitesOwnFigures(t *testing.T) {
	// Arrange
	suite := report.Result{
		SuiteID: "solo-suite",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 5,
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{TotalUSD: 4.25, Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 2,
	}

	// Act
	session := report.NewSession([]report.Result{suite}, nil, false)

	// Assert session figures are identical to the single suite's own figures --
	// not recomputed from tests, not altered in any way.
	if session.Counts[domain.VerdictPass] != suite.Counts[domain.VerdictPass] {
		t.Errorf("PASS count = %d, want %d (same as suite)", session.Counts[domain.VerdictPass], suite.Counts[domain.VerdictPass])
	}
	if session.Counts[domain.VerdictFail] != suite.Counts[domain.VerdictFail] {
		t.Errorf("FAIL count = %d, want %d (same as suite)", session.Counts[domain.VerdictFail], suite.Counts[domain.VerdictFail])
	}
	diff := session.TotalCost.TotalUSD - suite.TotalCost.TotalUSD
	if diff > 0.001 || diff < -0.001 {
		t.Errorf("TotalCost.TotalUSD = %f, want %f (same as suite)", session.TotalCost.TotalUSD, suite.TotalCost.TotalUSD)
	}
	if session.InfrastructureFailures != suite.InfrastructureFailures {
		t.Errorf("InfrastructureFailures = %d, want %d (same as suite)", session.InfrastructureFailures, suite.InfrastructureFailures)
	}
}

func TestNewSession_FromNoSuiteResults_IsWellFormedWithZeroedFigures(t *testing.T) {
	// Act
	session := report.NewSession(nil, nil, false)

	// Assert zeroed figures -- not nil maps, not panics.
	// The zero-value-is-valid pattern requires an empty session to be usable,
	// not a special case that callers must branch on.
	if session.Counts == nil {
		t.Error("Counts is nil, want initialised empty map")
	}
	if session.Counts[domain.VerdictPass] != 0 {
		t.Errorf("Counts[VerdictPass] = %d, want 0", session.Counts[domain.VerdictPass])
	}
	if session.Counts[domain.VerdictFail] != 0 {
		t.Errorf("Counts[VerdictFail] = %d, want 0", session.Counts[domain.VerdictFail])
	}
	diff := session.TotalCost.TotalUSD
	if diff > 0.001 || diff < -0.001 {
		t.Errorf("TotalCost.TotalUSD = %f, want 0", session.TotalCost.TotalUSD)
	}
	if session.InfrastructureFailures != 0 {
		t.Errorf("InfrastructureFailures = %d, want 0", session.InfrastructureFailures)
	}
}

func TestNewSession_FromNoSuiteResults_SuitesFieldIsEmpty(t *testing.T) {
	// Act
	session := report.NewSession(nil, nil, false)

	// Assert Suites holds no entries -- a well-formed empty slice, not an error.
	if len(session.Suites) != 0 {
		t.Errorf("len(Suites) = %d, want 0", len(session.Suites))
	}
}

func TestNewSession_SuitesFieldPreservesExecutionOrder(t *testing.T) {
	// Arrange
	suiteA := report.Result{SuiteID: "a", Counts: map[domain.Verdict]int{}, TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed}}
	suiteB := report.Result{SuiteID: "b", Counts: map[domain.Verdict]int{}, TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed}}
	suiteC := report.Result{SuiteID: "c", Counts: map[domain.Verdict]int{}, TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed}}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB, suiteC}, nil, false)

	// Assert suites are in the original input order
	if len(session.Suites) != 3 {
		t.Fatalf("len(Suites) = %d, want 3", len(session.Suites))
	}
	if session.Suites[0].SuiteID != "a" || session.Suites[1].SuiteID != "b" || session.Suites[2].SuiteID != "c" {
		t.Errorf("suite order = [%s, %s, %s], want [a, b, c]",
			session.Suites[0].SuiteID, session.Suites[1].SuiteID, session.Suites[2].SuiteID)
	}
}

// --- Three-state session outcome ---

func TestNewSession_AllPassing_OutcomeIsSessionPassed(t *testing.T) {
	// Arrange
	suiteA := report.Result{
		SuiteID:   "suite-a",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 3},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}
	suiteB := report.Result{
		SuiteID:   "suite-b",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 2},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB}, nil, false)

	// Assert
	if session.Outcome != report.SessionPassed {
		t.Errorf("Outcome = %q, want %q", session.Outcome, report.SessionPassed)
	}
}

func TestNewSession_WithFailingVerdictAndNoInfraFailure_OutcomeIsSessionTestsFailed(t *testing.T) {
	// Arrange
	suite := report.Result{
		SuiteID: "failing-suite",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 2,
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}

	// Act
	session := report.NewSession([]report.Result{suite}, nil, false)

	// Assert
	if session.Outcome != report.SessionTestsFailed {
		t.Errorf("Outcome = %q, want %q", session.Outcome, report.SessionTestsFailed)
	}
	if session.Outcome == report.SessionInfrastructureFailure {
		t.Error("a subject regression without infrastructure failure must not be classified as SessionInfrastructureFailure")
	}
}

func TestNewSession_WithInfrastructureFailure_OutcomeIsSessionInfrastructureFailure(t *testing.T) {
	// Arrange
	suite := report.Result{
		SuiteID:                "infra-fault-suite",
		Counts:                 map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}

	// Act
	session := report.NewSession([]report.Result{suite}, nil, false)

	// Assert
	if session.Outcome != report.SessionInfrastructureFailure {
		t.Errorf("Outcome = %q, want %q", session.Outcome, report.SessionInfrastructureFailure)
	}
	if session.Outcome == report.SessionTestsFailed {
		t.Error("an infrastructure failure must not be classified as SessionTestsFailed")
	}
}

func TestNewSession_WithBothFailingVerdictAndInfraFailure_OutcomeIsSessionInfrastructureFailure(t *testing.T) {
	// The unmeasurable state takes precedence over the regression state.
	// A caller that conflates them will eventually treat a tool fault as a
	// subject regression -- the exact confusion this distinction exists to prevent.

	// Arrange
	suite := report.Result{
		SuiteID: "combined-fault-suite",
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 1,
			domain.VerdictFail: 1,
		},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 1,
	}

	// Act
	session := report.NewSession([]report.Result{suite}, nil, false)

	// Assert infrastructure failure takes precedence
	if session.Outcome != report.SessionInfrastructureFailure {
		t.Errorf("Outcome = %q, want %q; infrastructure failure must take precedence over a failing verdict", session.Outcome, report.SessionInfrastructureFailure)
	}
	if session.Outcome == report.SessionTestsFailed {
		t.Error("when both a failing verdict and an infrastructure failure are present, outcome must be SessionInfrastructureFailure, not SessionTestsFailed")
	}
}

func TestNewSession_WhenAborted_OutcomeIsSessionInfrastructureFailure_EvenWhenAllSuitesPassed(t *testing.T) {
	// A session that did not run to completion cannot be treated as passing,
	// even when every suite that did run reported all tests passing.

	// Arrange
	passingSuite := report.Result{
		SuiteID:                "passing-suite",
		Counts:                 map[domain.Verdict]int{domain.VerdictPass: 5},
		TotalCost:              domain.CostReport{Attribution: domain.AttributionAttributed},
		InfrastructureFailures: 0,
	}
	unrunSuites := []string{"pending-suite-a", "pending-suite-b"}

	// Act
	session := report.NewSession([]report.Result{passingSuite}, unrunSuites, true)

	// Assert
	if session.Outcome != report.SessionInfrastructureFailure {
		t.Errorf("Outcome = %q, want %q; an aborted session is unmeasurable regardless of what completed", session.Outcome, report.SessionInfrastructureFailure)
	}
}

func TestNewSession_WhenAborted_AbortedFlagAndUnrunSuitesArePreserved(t *testing.T) {
	// Arrange
	suite := report.Result{
		SuiteID:   "completed-suite",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}
	unrunSuites := []string{"skipped-suite-a", "skipped-suite-b"}

	// Act
	session := report.NewSession([]report.Result{suite}, unrunSuites, true)

	// Assert
	if !session.Aborted {
		t.Error("Aborted = false, want true")
	}
	if len(session.UnrunSuites) != 2 {
		t.Fatalf("len(UnrunSuites) = %d, want 2", len(session.UnrunSuites))
	}
	if session.UnrunSuites[0] != "skipped-suite-a" || session.UnrunSuites[1] != "skipped-suite-b" {
		t.Errorf("UnrunSuites = %v, want [skipped-suite-a, skipped-suite-b]", session.UnrunSuites)
	}
}

func TestNewSession_EmptySuites_NotAborted_OutcomeIsSessionPassed(t *testing.T) {
	// An empty session with aborted=false is the zero-value-is-valid case.
	// It must be well-formed and classify as SessionPassed, not a special case
	// that callers must handle separately.

	// Act
	session := report.NewSession(nil, nil, false)

	// Assert
	if session.Outcome != report.SessionPassed {
		t.Errorf("Outcome = %q, want %q for an empty non-aborted session", session.Outcome, report.SessionPassed)
	}
}
