package report_test

// Tests for report.Build infrastructure failure run counting (TDD RED phase).
//
// The key behavior under test: when a test has Aggregate.InfrastructureFailure == true,
// Build must sum Aggregate.Excluded (not increment by 1). After the fix,
// Result.InfrastructureFailures equals the sum of Excluded across all
// infra-flagged tests.
//
// All tests in this file compile against the existing API but fail at runtime
// because Build currently increments infra by 1 per test, not by Excluded.
// Exception: TestBuild_NoInfraFailures_ProducesZero and
// TestBuild_InfraFailure_ExcludedOneRun_CountsAsOne pass vacuously (the
// current and expected values coincide for these boundary/zero cases), which
// is accepted for regression guards of this kind.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// infraTestReport returns a TestReport whose aggregate carries
// InfrastructureFailure=true and Excluded=excludedCount.
func infraTestReport(name string, excludedCount int) report.TestReport {
	return report.TestReport{
		TestName: name,
		Aggregate: domain.AggregateResult{
			TestName:              name,
			Verdict:               domain.VerdictFail,
			Reasons:               []domain.FailureReason{domain.ReasonInfrastructure},
			Counted:               0,
			Excluded:              excludedCount,
			InfrastructureFailure: true,
			TotalCost:             domain.CostReport{Attribution: domain.AttributionAttributed},
		},
	}
}

// normalTestReport returns a TestReport with a standard (non-infra) aggregate.
func normalTestReport(name string, counted, passed int) report.TestReport {
	var passRate float64
	if counted > 0 {
		passRate = float64(passed) / float64(counted)
	}
	verdict := domain.VerdictPass
	if passed < counted {
		verdict = domain.VerdictFail
	}
	return report.TestReport{
		TestName: name,
		Aggregate: domain.AggregateResult{
			TestName:  name,
			Verdict:   verdict,
			Counted:   counted,
			Passed:    passed,
			PassRate:  passRate,
			TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		},
	}
}

// fixtureTime is a stable timestamp for test suite build calls.
var fixtureTime = time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)

// TestBuild_InfraFailure_SingleTest_SumsExcludedRuns verifies that when one
// test has InfrastructureFailure=true with Excluded=3, Build sets
// Result.InfrastructureFailures to 3 (not 1).
func TestBuild_InfraFailure_SingleTest_SumsExcludedRuns(t *testing.T) {
	// Arrange
	tests := []report.TestReport{
		infraTestReport("flaky-checkout", 3),
	}

	// Act
	result := report.Build("suite-infra-single", fixtureTime, fixtureTime, tests, "")

	// Assert
	if result.InfrastructureFailures != 3 {
		t.Errorf("InfrastructureFailures = %d, want 3 (sum of Excluded for the single infra-failed test, not a count of tests)",
			result.InfrastructureFailures)
	}
}

// TestBuild_InfraFailure_MultipleTests_SumsExcludedAcrossAll verifies that
// when multiple tests are infra-failed, Build sums their Excluded counts.
// Two tests with Excluded=2 and Excluded=5 must produce
// InfrastructureFailures=7, not 2.
func TestBuild_InfraFailure_MultipleTests_SumsExcludedAcrossAll(t *testing.T) {
	// Arrange
	tests := []report.TestReport{
		infraTestReport("infra-alpha", 2),
		infraTestReport("infra-beta", 5),
	}

	// Act
	result := report.Build("suite-infra-multi", fixtureTime, fixtureTime, tests, "")

	// Assert: 2 + 5 = 7
	if result.InfrastructureFailures != 7 {
		t.Errorf("InfrastructureFailures = %d, want 7 (2+5 Excluded summed across both infra-failed tests)",
			result.InfrastructureFailures)
	}
}

// TestBuild_InfraFailure_MixedTests_OnlySumsInfraExcluded verifies that
// normal tests do not contribute to InfrastructureFailures. One infra test
// (Excluded=4) among normal tests must produce InfrastructureFailures=4.
func TestBuild_InfraFailure_MixedTests_OnlySumsInfraExcluded(t *testing.T) {
	// Arrange: two normal tests bracketing one infra test with Excluded=4
	tests := []report.TestReport{
		normalTestReport("checkout-happy", 3, 3),
		infraTestReport("state-integrity-fault", 4),
		normalTestReport("checkout-reject", 2, 1),
	}

	// Act
	result := report.Build("suite-infra-mixed", fixtureTime, fixtureTime, tests, "")

	// Assert: only the infra test's Excluded count (4) is summed
	if result.InfrastructureFailures != 4 {
		t.Errorf("InfrastructureFailures = %d, want 4 (Excluded from infra-failed test only; normal tests must not contribute)",
			result.InfrastructureFailures)
	}
}

// TestBuild_NoInfraFailures_ProducesZero verifies that when no test is
// infra-failed, InfrastructureFailures is 0. This is a regression guard:
// any implementation that counts non-infra tests would break it.
func TestBuild_NoInfraFailures_ProducesZero(t *testing.T) {
	// Arrange: two normal tests with no infra failure
	tests := []report.TestReport{
		normalTestReport("normal-a", 2, 2),
		normalTestReport("normal-b", 2, 1),
	}

	// Act
	result := report.Build("suite-no-infra", fixtureTime, fixtureTime, tests, "")

	// Assert
	if result.InfrastructureFailures != 0 {
		t.Errorf("InfrastructureFailures = %d, want 0 when no infra-failed tests are present",
			result.InfrastructureFailures)
	}
}

// TestBuild_InfraFailure_ExcludedOneRun_CountsAsOne verifies the boundary
// case where Excluded=1: the result is 1. Both the test-counting and the
// run-counting implementations agree at this boundary, but the test
// documents the expected semantic (run count, not test count).
func TestBuild_InfraFailure_ExcludedOneRun_CountsAsOne(t *testing.T) {
	// Arrange
	tests := []report.TestReport{
		infraTestReport("single-exclusion", 1),
	}

	// Act
	result := report.Build("suite-infra-one", fixtureTime, fixtureTime, tests, "")

	// Assert
	if result.InfrastructureFailures != 1 {
		t.Errorf("InfrastructureFailures = %d, want 1 for a single excluded run", result.InfrastructureFailures)
	}
}

// TestBuild_InfraFailure_ExcludedZeroRuns_IsNotCounted verifies that an
// infra-failed test with Excluded=0 contributes 0 to InfrastructureFailures.
// This edge case matters because a future implementation error might
// unconditionally add 1 for every infra-flagged test regardless of Excluded.
func TestBuild_InfraFailure_ExcludedZeroRuns_IsNotCounted(t *testing.T) {
	// Arrange: infra-flagged but Excluded=0 (unusual but structurally valid)
	tests := []report.TestReport{
		infraTestReport("infra-no-exclusions", 0),
	}

	// Act
	result := report.Build("suite-infra-zero", fixtureTime, fixtureTime, tests, "")

	// Assert: 0 excluded runs means 0 infrastructure failures
	if result.InfrastructureFailures != 0 {
		t.Errorf("InfrastructureFailures = %d, want 0 for an infra-flagged test with Excluded=0",
			result.InfrastructureFailures)
	}
}

// TestNewSession_InfraRunCount_PropagatesFromBuildResults verifies that
// NewSession correctly sums run-based InfrastructureFailures from multiple
// suite Results. Once Build emits run counts (not test counts), session-level
// sums are automatically run counts.
//
// Suite A: one infra test with Excluded=5 -> Build produces
// InfrastructureFailures=5 (after fix).
// Suite B: one infra test with Excluded=3 -> Build produces
// InfrastructureFailures=3 (after fix).
// Session should total 5+3=8.
func TestNewSession_InfraRunCount_PropagatesFromBuildResults(t *testing.T) {
	// Arrange
	suiteATests := []report.TestReport{infraTestReport("infra-in-suite-a", 5)}
	suiteA := report.Build("suite-a", fixtureTime, fixtureTime, suiteATests, "")

	suiteBTests := []report.TestReport{infraTestReport("infra-in-suite-b", 3)}
	suiteB := report.Build("suite-b", fixtureTime, fixtureTime, suiteBTests, "")

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB}, nil, false)

	// Assert: 5 + 3 = 8 run-based infra failures across both suites
	if session.InfrastructureFailures != 8 {
		t.Errorf("Session.InfrastructureFailures = %d, want 8 (5 from suite-a + 3 from suite-b run counts)",
			session.InfrastructureFailures)
	}
}
