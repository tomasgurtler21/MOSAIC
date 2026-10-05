package cli_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// TestInfrastructureFlag_Omitted_LeavesFilterNil verifies that when
// --infrastructure is not passed, RunConfig.InfrastructureFilter is nil.
// nil means "not specified; all declared agents remain active" -- the
// backwards-compatible default.
//
// This test is trivially GREEN today (InfrastructureFilter defaults to nil)
// and becomes a regression guard once I5.2 parses the flag.
func TestInfrastructureFlag_Omitted_LeavesFilterNil(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.InfrastructureFilter != nil {
		t.Errorf("InfrastructureFilter = %v, want nil when --infrastructure is not passed",
			sess.config.InfrastructureFilter)
	}
}

// TestInfrastructureFlag_WithoutDevTestMode_IsRejected verifies that passing
// --infrastructure without --dev-test-mode is rejected with ExitUsage. The
// dev-mode guard ensures --infrastructure cannot be used in production runs.
//
// TDD RED: fails until I5.2 implements the dev-mode guard in run.go.
func TestInfrastructureFlag_WithoutDevTestMode_IsRejected(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
		"--infrastructure", "mosaictest-review",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when --infrastructure is used without --dev-test-mode",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "dev test mode") && !strings.Contains(errOut, "dev-test-mode") {
		t.Errorf("stderr %q does not mention dev test mode", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when the dev-mode guard rejects --infrastructure")
	}
}

// TestInfrastructureFlag_WithDevTestMode_IsAccepted verifies that passing
// --infrastructure together with --dev-test-mode passes the dev-mode guard and
// causes session.Start to be called normally.
//
// TDD RED: fails until I5.1 and I5.2 register the flags and implement the guard.
func TestInfrastructureFlag_WithDevTestMode_IsAccepted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
		"--infrastructure", "mosaictest-review",
		"--dev-test-mode",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess when --infrastructure is used with --dev-test-mode; stderr: %q",
			code, errOut)
	}
	if !sess.called {
		t.Error("session.Start was not called; --infrastructure with --dev-test-mode must be accepted")
	}
}

// TestInfrastructureFlag_ParsedToFilterSlice verifies that --infrastructure k1,k2
// (comma-separated) populates RunConfig.InfrastructureFilter as ["k1","k2"].
//
// TDD RED: fails until I5.2 parses the flag and populates InfrastructureFilter.
func TestInfrastructureFlag_ParsedToFilterSlice(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
		"--infrastructure", "k1,k2",
		"--dev-test-mode",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	filter := sess.config.InfrastructureFilter
	if len(filter) != 2 || filter[0] != "k1" || filter[1] != "k2" {
		t.Errorf("InfrastructureFilter = %v, want [\"k1\" \"k2\"] for --infrastructure k1,k2", filter)
	}
}

// TestInfrastructureFlag_EmptyValue_ParsedToNonNilEmpty verifies that
// --infrastructure= (empty value, single-token = form) populates
// RunConfig.InfrastructureFilter as []string{} (non-nil empty, not nil).
// nil would mean "all agents active"; non-nil empty means "no agents active".
//
// TDD RED: fails until I5.2 parses the flag and populates InfrastructureFilter.
func TestInfrastructureFlag_EmptyValue_ParsedToNonNilEmpty(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
		"--infrastructure=",
		"--dev-test-mode",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	filter := sess.config.InfrastructureFilter
	if filter == nil {
		t.Error("InfrastructureFilter = nil, want non-nil empty []string{} for --infrastructure= (empty value); " +
			"nil means \"all agents active\" which is the opposite of the intended empty allowlist")
	}
	if len(filter) != 0 {
		t.Errorf("InfrastructureFilter = %v (len %d), want empty []string{} for --infrastructure=",
			filter, len(filter))
	}
}

// TestInfrastructureFlag_CommaSplitEdgeCases verifies comma-split edge cases
// for the --infrastructure flag value. The split-and-trim logic must:
//   - Drop empty tokens produced by adjacent commas ("a,,b" -> ["a","b"])
//   - Trim whitespace from each token ("a, b," -> ["a","b"])
//   - Produce non-nil empty []string{} when all tokens are empty after trimming
//     ("," and " " -> []string{}, not nil)
//
// TDD RED: fails until I5.2 implements the comma-split logic in run.go.
func TestInfrastructureFlag_CommaSplitEdgeCases(t *testing.T) {
	cases := []struct {
		raw       string
		wantNil   bool
		wantLen   int
		wantSlice []string
	}{
		{"a,,b", false, 2, []string{"a", "b"}},
		{"a, b,", false, 2, []string{"a", "b"}},
		{",", false, 0, []string{}},
		{" ", false, 0, []string{}},
	}

	for _, c := range cases {
		c := c
		t.Run("raw="+c.raw, func(t *testing.T) {
			sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
			args := []string{
				"run",
				"--workflow", "w1",
				"--task", "do work",
				"--mode", "auto",
				"--new-run",
				"--review-loop-limit", "3",
				"--infrastructure", c.raw,
				"--dev-test-mode",
			}
			code, _, errOut := runCLIWithStore(t, args, &spyStore{}, sess)
			if code != cli.ExitSuccess {
				t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
			}
			if !sess.called {
				t.Fatal("session.Start was not called")
			}
			filter := sess.config.InfrastructureFilter
			if c.wantNil {
				if filter != nil {
					t.Errorf("InfrastructureFilter = %v, want nil", filter)
				}
				return
			}
			if filter == nil {
				t.Errorf("InfrastructureFilter = nil, want non-nil %v", c.wantSlice)
				return
			}
			if len(filter) != c.wantLen {
				t.Errorf("InfrastructureFilter = %v (len %d), want len %d (%v)",
					filter, len(filter), c.wantLen, c.wantSlice)
				return
			}
			for i, want := range c.wantSlice {
				if filter[i] != want {
					t.Errorf("InfrastructureFilter[%d] = %q, want %q (full: %v)",
						i, filter[i], want, filter)
				}
			}
		})
	}
}
