package contract

import (
	"testing"

	"mosaic-agent-test/internal/domain"
)

// testEnvironmentCheck drives every adapter through the new
// domain.HarnessAdapter.CheckEnvironment obligation:
//   - every adapter answers it, with no fault preventing the check from
//     being performed at all against a freshly constructed adapter;
//   - an adapter never reports a problem-free environment (OK() == true)
//     while having failed to resolve something it needs — expressed
//     generically as: InterpreterApplicable and OK() together require a
//     non-empty InterpreterCmd;
//   - the report is honest about what the adapter actually does — an
//     adapter that declares no interpreter (InterpreterApplicable == false)
//     must not also report a resolved InterpreterCmd, which would be a
//     resolved value it does not actually use.
func testEnvironmentCheck(t *testing.T, cfg Config) {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())

	report, err := adapter.CheckEnvironment(testContext())
	if err != nil {
		t.Fatalf("CheckEnvironment: %v", err)
	}

	if !report.InterpreterApplicable && report.InterpreterCmd != "" {
		t.Errorf(
			"CheckEnvironment: InterpreterApplicable = false but InterpreterCmd = %q — an adapter must not report a resolved value it does not actually use",
			report.InterpreterCmd,
		)
	}

	if report.OK() && report.InterpreterApplicable && report.InterpreterCmd == "" {
		t.Errorf(
			"CheckEnvironment: OK() = true and InterpreterApplicable = true but InterpreterCmd is empty — an adapter must not report a problem-free environment while having failed to resolve something it needs",
		)
	}

	if !report.OK() {
		for i, p := range report.Problems {
			if p.Kind == domain.EnvironmentProblemKind("") {
				t.Errorf("CheckEnvironment: Problems[%d] has an empty Kind", i)
			}
			if p.Detail == "" {
				t.Errorf("CheckEnvironment: Problems[%d] (Kind=%q) has an empty Detail; it must name what was tried and what to do about it", i, p.Kind)
			}
		}
	}
}
