package cli

// max_concurrent_runs_parity_test.go covers the CLI side of the parity
// declaration for --max-concurrent-runs (T12.3).
//
// This file is in package cli (not cli_test) so it can access the unexported
// valueFlags map directly, following the same convention as cli_tui_parity_test.go.
//
// RED-phase notes:
//   - TestCLITUIParity_MaxConcurrentRunsFlag_IsInParityDeclaration FAILS because
//     "max-concurrent-runs" is not yet in TUIParityFlags (I12.4).
//   - TestCLITUIParity_MaxConcurrentRunsFlag_IsRecognisedValueFlag FAILS because
//     "max-concurrent-runs" is not yet in valueFlags (I12.2).
//   - Both tests compile against the current code.

import "testing"

// TestCLITUIParity_MaxConcurrentRunsFlag_IsInParityDeclaration verifies that
// "max-concurrent-runs" is listed in TUIParityFlags. It must be listed so the
// cross-check tests can enforce that the corresponding TUI SettingKind
// (SettingMaxConcurrentRuns) is also present on the settings screen.
func TestCLITUIParity_MaxConcurrentRunsFlag_IsInParityDeclaration(t *testing.T) {
	for _, flag := range TUIParityFlags {
		if flag == "max-concurrent-runs" {
			return
		}
	}
	t.Errorf("TUIParityFlags does not include \"max-concurrent-runs\"; the TUI settings screen must have an equivalent SettingMaxConcurrentRuns entry, so this flag must appear in the parity declaration (I12.4)")
}

// TestCLITUIParity_MaxConcurrentRunsFlag_IsRecognisedValueFlag verifies that
// "max-concurrent-runs" is a recognised value-flag in the command surface.
// TUIParityFlags and valueFlags must be kept in sync: an entry in one must
// appear in the other, or the parity cross-check test
// TestCLITUIParity_AllTUIParityValueFlags_AreRecognized will fail.
func TestCLITUIParity_MaxConcurrentRunsFlag_IsRecognisedValueFlag(t *testing.T) {
	if !valueFlags["max-concurrent-runs"] {
		t.Errorf("valueFlags does not contain \"max-concurrent-runs\"; add it alongside the flag's parsing logic in cli/run.go (I12.2)")
	}
}
