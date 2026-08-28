package cli

// Tests for T2.3: Pin the mapping from the shared Session.Outcome classification
// to CLI exit codes. Each test builds a result, verifies the shared classification
// produces the expected SessionOutcome, then verifies the exit code matches.
//
// These tests are written before the rewiring of exitCodeForResult so they
// guard against the rewiring accidentally changing any exit code an automation
// caller depends on. They are internal (package cli, not cli_test) so they can
// call exitCodeForResult directly rather than going through the full Execute path.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// sessionExitcodePassingResult returns a Result whose single test passed, so
// the shared classification produces SessionPassed.
func sessionExitcodePassingResult() report.Result {
	return report.Build("suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "passing-test",
			Layer:    domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName: "passing-test",
				Verdict:  domain.VerdictPass,
				Counted:  1,
				Passed:   1,
				PassRate: 1.0,
				TotalCost: domain.CostReport{
					Attribution: domain.AttributionAttributed,
				},
			},
		},
	}, "")
}

// sessionExitcodeFailedTestResult returns a Result whose single test failed an
// assertion, so the shared classification produces SessionTestsFailed.
func sessionExitcodeFailedTestResult() report.Result {
	return report.Build("suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "failing-test",
			Layer:    domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:  "failing-test",
				Verdict:   domain.VerdictFail,
				Reasons:   []domain.FailureReason{domain.ReasonAssertion},
				Counted:   1,
				Passed:    0,
				PassRate:  0,
				TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
			},
		},
	}, "")
}

// sessionExitcodeInfraFaultResult returns a Result whose single test ended by a
// repeated state-integrity fault, so the shared classification produces
// SessionInfrastructureFailure.
func sessionExitcodeInfraFaultResult() report.Result {
	return report.Build("suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "infra-fault-test",
			Layer:    domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:              "infra-fault-test",
				Verdict:               domain.VerdictFail,
				Reasons:               []domain.FailureReason{domain.ReasonStateIntegrity},
				Counted:               0,
				Excluded:              1,
				InfrastructureFailure: true,
				TotalCost:             domain.CostReport{Attribution: domain.AttributionAttributed},
			},
		},
	}, "")
}

// sessionExitcodeBothFaultResult returns a Result with both a failing assertion
// test and an infrastructure fault, so the shared classification produces
// SessionInfrastructureFailure (infrastructure takes precedence over regression).
func sessionExitcodeBothFaultResult() report.Result {
	return report.Build("suite", time.Time{}, time.Time{}, []report.TestReport{
		{
			TestName: "assertion-failure",
			Layer:    domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:  "assertion-failure",
				Verdict:   domain.VerdictFail,
				Reasons:   []domain.FailureReason{domain.ReasonAssertion},
				Counted:   1,
				Passed:    0,
				TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
			},
		},
		{
			TestName: "infra-fault",
			Layer:    domain.LayerOrchestrator,
			Aggregate: domain.AggregateResult{
				TestName:              "infra-fault",
				Verdict:               domain.VerdictFail,
				Reasons:               []domain.FailureReason{domain.ReasonStateIntegrity},
				Counted:               0,
				Excluded:              1,
				InfrastructureFailure: true,
				TotalCost:             domain.CostReport{Attribution: domain.AttributionAttributed},
			},
		},
	}, "")
}

func TestExitCodeForResult_WhenSessionPassed_ReturnsExitSuccess(t *testing.T) {
	// Arrange
	result := sessionExitcodePassingResult()

	// Verify the shared classification agrees this is a passing session.
	// This call is RED until NewSession is implemented (I2.5/I2.6).
	session := report.NewSession([]report.Result{result}, nil, false)
	if session.Outcome != report.SessionPassed {
		t.Fatalf("shared classification = %q, want %q", session.Outcome, report.SessionPassed)
	}

	// Act
	got := exitCodeForResult(result)

	// Assert: a passing session must exit with ExitSuccess
	if got != ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for a %q session", got, ExitSuccess, report.SessionPassed)
	}
}

func TestExitCodeForResult_WhenSessionTestsFailed_ReturnsExitTestsFailed(t *testing.T) {
	// Arrange
	result := sessionExitcodeFailedTestResult()

	// Verify the shared classification agrees this is a tests-failed session.
	session := report.NewSession([]report.Result{result}, nil, false)
	if session.Outcome != report.SessionTestsFailed {
		t.Fatalf("shared classification = %q, want %q", session.Outcome, report.SessionTestsFailed)
	}

	// Act
	got := exitCodeForResult(result)

	// Assert: a subject regression must exit with ExitTestsFailed, never ExitFailure.
	// A caller that cannot distinguish these two codes will treat a regression as a tool fault.
	if got != ExitTestsFailed {
		t.Errorf("exit code = %d, want ExitTestsFailed (%d) for a %q session", got, ExitTestsFailed, report.SessionTestsFailed)
	}
	if got == ExitFailure {
		t.Error("a subject regression must not be reported as ExitFailure")
	}
}

func TestExitCodeForResult_WhenSessionInfrastructureFailure_ReturnsExitFailure(t *testing.T) {
	// Arrange
	result := sessionExitcodeInfraFaultResult()

	// Verify the shared classification agrees this is an infrastructure failure.
	session := report.NewSession([]report.Result{result}, nil, false)
	if session.Outcome != report.SessionInfrastructureFailure {
		t.Fatalf("shared classification = %q, want %q", session.Outcome, report.SessionInfrastructureFailure)
	}

	// Act
	got := exitCodeForResult(result)

	// Assert: an infrastructure failure must exit with ExitFailure, never ExitTestsFailed.
	if got != ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) for a %q session", got, ExitFailure, report.SessionInfrastructureFailure)
	}
	if got == ExitTestsFailed {
		t.Error("an infrastructure failure must not be reported as ExitTestsFailed")
	}
}

func TestExitCodeForResult_WhenBothTestsFailedAndInfraFailure_ReturnsExitFailure(t *testing.T) {
	// When both a failing assertion verdict and an infrastructure failure are
	// present, ExitFailure must take precedence over ExitTestsFailed -- the
	// inability to measure takes precedence over a measured regression.

	// Arrange
	result := sessionExitcodeBothFaultResult()

	// Verify the shared classification agrees: infrastructure takes precedence.
	session := report.NewSession([]report.Result{result}, nil, false)
	if session.Outcome != report.SessionInfrastructureFailure {
		t.Fatalf("shared classification = %q, want %q", session.Outcome, report.SessionInfrastructureFailure)
	}

	// Act
	got := exitCodeForResult(result)

	// Assert
	if got != ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) when both tests failed and an infrastructure fault is present", got, ExitFailure)
	}
	if got == ExitTestsFailed {
		t.Error("an infrastructure failure must take precedence: exit code must be ExitFailure, not ExitTestsFailed")
	}
}
