package contract

import (
	"fmt"
	"testing"
)

// CheckPreservesSubjectFunction is the verdict-only counterpart of the
// ConfigScopeInspection subtest's neutralization-honesty obligation:
// instead of failing t directly via t.Errorf, it reports the same verdict
// as a returned error, mirroring CheckCapabilityHonesty, so a caller outside
// the t.Run subtest tree can inspect the verdict without the parent
// *testing.T being marked failed as a side effect.
//
// domain.ScopeFinding.Neutralized answers "was the configuration isolated".
// PreservesSubjectFunction answers "and does the subject still work". An
// adapter that isolates a scope at the cost of the subject's ability to run
// is reporting a neutralization it has not actually achieved, and must fail
// here rather than pass a check it should fail (see ContractsDesign.md's
// domain.ScopeFinding "Conformance obligation").
func CheckPreservesSubjectFunction(t *testing.T, cfg Config) error {
	t.Helper()

	adapter := cfg.New(t, t.TempDir())

	findings, err := adapter.InspectScopes(testContext())
	if err != nil {
		t.Fatalf("InspectScopes: %v", err)
	}

	for _, f := range findings {
		if f.Neutralized && !f.PreservesSubjectFunction {
			return fmt.Errorf(
				"neutralization check: scope %q is reported Neutralized=true but PreservesSubjectFunction=false (FunctionDetail: %q) — isolating a scope at the cost of the subject's ability to run must fail this check, not pass it",
				f.Scope.Name, f.FunctionDetail,
			)
		}
	}
	return nil
}
