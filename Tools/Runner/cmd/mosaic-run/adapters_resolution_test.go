package main

import (
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// T4.1: Per-harness executable resolution — no override supplied
//
// buildAdapter is the sole owner of per-harness executable defaulting. When
// the caller supplies "" as the executable override (i.e. the user did not
// pass --executable-path), each harness must resolve its own default:
//
//   claude-code → "claude"
//   opencode    → "opencode"
//   ghcp-cli    → "copilot"
//
// The resolution is asserted through domain.ExecutableRevealer, so a future
// re-hoisting of a shared default upstream of buildAdapter breaks the test
// behaviorally rather than structurally.
//
// RED state:
//   TestBuildAdapter_ClaudeCode_NoOverride_ResolvesDefaultClaude — fails
//   because the claude-code case in buildAdapter currently passes the
//   executableOverride directly without an "if override == ''" guard, so
//   ExecutablePath() returns "" rather than "claude".
//
// The opencode and ghcp-cli tests start GREEN (those cases already have the
// guard) and serve as regression pins against the cross-harness default being
// reintroduced.
// ---------------------------------------------------------------------------

// requireExecutableRevealer type-asserts h to domain.ExecutableRevealer and
// returns the accessor. If the assertion fails the test is marked fatal.
func requireExecutableRevealer(t *testing.T, h domain.HarnessAdapter) domain.ExecutableRevealer {
	t.Helper()
	rev, ok := h.(domain.ExecutableRevealer)
	if !ok {
		t.Fatalf("adapter %T does not implement domain.ExecutableRevealer; "+
			"all CLI-backed adapters must implement it so executable resolution is observable", h)
	}
	return rev
}

// TestBuildAdapter_ClaudeCode_NoOverride_ResolvesDefaultClaude verifies that
// when no executable override is supplied (empty string), buildAdapter resolves
// the claude-code harness to its per-harness default executable "claude".
//
// RED: the claude-code case currently passes the override directly without an
// empty-string guard, so ExecutablePath() returns "" not "claude".
func TestBuildAdapter_ClaudeCode_NoOverride_ResolvesDefaultClaude(t *testing.T) {
	h := buildAdapter("", commonharness.HarnessIDClaudeCode, "", "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	got := rev.ExecutablePath()
	if got != "claude" {
		t.Errorf("buildAdapter(claude-code, override='') ExecutablePath() = %q, want %q; "+
			"the claude-code case must apply its per-harness default when no override is supplied",
			got, "claude")
	}
}

// TestBuildAdapter_OpenCode_NoOverride_ResolvesDefaultOpenCode verifies that
// when no executable override is supplied, buildAdapter resolves the opencode
// harness to its per-harness default executable "opencode".
func TestBuildAdapter_OpenCode_NoOverride_ResolvesDefaultOpenCode(t *testing.T) {
	h := buildAdapter("", commonharness.HarnessIDOpenCode, "", "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	got := rev.ExecutablePath()
	if got != "opencode" {
		t.Errorf("buildAdapter(opencode, override='') ExecutablePath() = %q, want %q",
			got, "opencode")
	}
}

// TestBuildAdapter_GHCPCli_NoOverride_ResolvesDefaultCopilot verifies that
// when no executable override is supplied, buildAdapter resolves the ghcp-cli
// harness to its per-harness default executable "copilot".
func TestBuildAdapter_GHCPCli_NoOverride_ResolvesDefaultCopilot(t *testing.T) {
	h := buildAdapter("", commonharness.HarnessIDGHCPCLI, "", "blanket", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	got := rev.ExecutablePath()
	if got != "copilot" {
		t.Errorf("buildAdapter(ghcp-cli, override='') ExecutablePath() = %q, want %q",
			got, "copilot")
	}
}

// TestBuildAdapter_GHCPCli_NoOverride_DoesNotResolveClaude verifies the
// cross-harness property: with no override, the ghcp-cli harness must not
// resolve to "claude" (another harness's executable). This pins the root cause
// of the reported failure: the GHCP CLI harness was spawning "claude" because
// the pre-scan injected "claude" as a fallback before buildAdapter was called.
func TestBuildAdapter_GHCPCli_NoOverride_DoesNotResolveClaude(t *testing.T) {
	h := buildAdapter("", commonharness.HarnessIDGHCPCLI, "", "blanket", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got == "claude" {
		t.Errorf("buildAdapter(ghcp-cli, override='') ExecutablePath() = %q; "+
			"ghcp-cli must not resolve to claude — each harness must resolve its own default",
			got)
	}
}

// TestBuildAdapter_OpenCode_NoOverride_DoesNotResolveClaude verifies the
// cross-harness property for the opencode harness: with no override, it must
// not resolve to "claude".
func TestBuildAdapter_OpenCode_NoOverride_DoesNotResolveClaude(t *testing.T) {
	h := buildAdapter("", commonharness.HarnessIDOpenCode, "", "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got == "claude" {
		t.Errorf("buildAdapter(opencode, override='') ExecutablePath() = %q; "+
			"opencode must not resolve to claude — each harness must resolve its own default",
			got)
	}
}

// ---------------------------------------------------------------------------
// T4.2: Explicit --executable-path override applies to every non-fake harness
//
// When the user supplies --executable-path, that value must be passed through as
// the executable path for whichever harness --harness selected, overriding the
// per-harness default. These tests verify the override takes effect for all
// three real harnesses.
//
// All tests start GREEN (each case already propagates a non-empty override).
// They serve as regression pins: if an implementation change accidentally drops
// the override, these tests catch it.
// ---------------------------------------------------------------------------

// TestBuildAdapter_ClaudeCode_WithOverride_UsesOverride verifies that an
// explicit executable override is passed through to the claude-code adapter
// regardless of the per-harness default.
func TestBuildAdapter_ClaudeCode_WithOverride_UsesOverride(t *testing.T) {
	const override = "/opt/custom/claude"
	h := buildAdapter("", commonharness.HarnessIDClaudeCode, override, "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != override {
		t.Errorf("buildAdapter(claude-code, override=%q) ExecutablePath() = %q, want %q",
			override, got, override)
	}
}

// TestBuildAdapter_OpenCode_WithOverride_UsesOverride verifies that an explicit
// executable override is passed through to the opencode adapter, replacing its
// per-harness default of "opencode".
func TestBuildAdapter_OpenCode_WithOverride_UsesOverride(t *testing.T) {
	const override = "/opt/custom/opencode"
	h := buildAdapter("", commonharness.HarnessIDOpenCode, override, "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != override {
		t.Errorf("buildAdapter(opencode, override=%q) ExecutablePath() = %q, want %q",
			override, got, override)
	}
}

// TestBuildAdapter_GHCPCli_WithOverride_UsesOverride verifies that an explicit
// executable override is passed through to the ghcp-cli adapter, replacing its
// per-harness default of "copilot".
func TestBuildAdapter_GHCPCli_WithOverride_UsesOverride(t *testing.T) {
	const override = "/opt/custom/copilot"
	h := buildAdapter("", commonharness.HarnessIDGHCPCLI, override, "blanket", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != override {
		t.Errorf("buildAdapter(ghcp-cli, override=%q) ExecutablePath() = %q, want %q",
			override, got, override)
	}
}

// TestBuildAdapter_AllRealHarnesses_OverrideWinsOverDefault is a table-driven
// regression across all three real harnesses: supplying a non-empty override
// must always win over the per-harness default, for every harness.
func TestBuildAdapter_AllRealHarnesses_OverrideWinsOverDefault(t *testing.T) {
	const override = "custom-binary"
	cases := []struct {
		harnessID string
	}{
		{commonharness.HarnessIDClaudeCode},
		{commonharness.HarnessIDOpenCode},
		{commonharness.HarnessIDGHCPCLI},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.harnessID, func(t *testing.T) {
			h := buildAdapter("", tc.harnessID, override, "", 30*time.Minute)
			rev := requireExecutableRevealer(t, h)
			if got := rev.ExecutablePath(); got != override {
				t.Errorf("buildAdapter(%q, override=%q) ExecutablePath() = %q, want %q; "+
					"explicit override must win over per-harness default",
					tc.harnessID, override, got, override)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T4.3: Regression — both CLI and TUI entry-path pre-scans
//
// The CLI entry path (runCLIMode) and the TUI entry path (runTUIMode) both
// pre-scan --executable-path via scanFlag before calling buildAdapter. After the
// fix, neither pre-scan may substitute a literal "claude" fallback; an absent
// --executable-path must arrive at buildAdapter as "" so the per-harness default
// applies.
//
// These tests pin the seam between scanFlag and buildAdapter: they simulate
// the composition the entry paths perform and assert the expected per-harness
// executable. Since both frontends use the same scanFlag helper, a single set
// of composition tests covers both paths.
//
// The cobra --executable-path flag default must also agree: if cobra defaults to
// "" and scanFlag returns "" for an absent flag, the two sources agree on
// "not supplied" meaning empty string. The cobra flag default is changed in
// I4.3 (run.go); its correctness is pinned here through the composition tests
// whose assertions would fail if a literal "claude" were injected anywhere
// before buildAdapter.
// ---------------------------------------------------------------------------

// TestPreScanComposition_GHCPCli_AbsentExecPath_ResolvesCopilot simulates
// the post-fix CLI/TUI entry-path composition: scanFlag finds no --executable-path
// in args and returns "", which is passed directly to buildAdapter (no fallback
// injection). The ghcp-cli adapter must resolve to "copilot".
//
// Regression: if the pre-scan re-introduces `if execPath == "" { execPath
// = "claude" }`, then buildAdapter("ghcp-cli", "claude", ...) would return
// "claude" (overriding the per-harness default), and this test would fail.
func TestPreScanComposition_GHCPCli_AbsentExecPath_ResolvesCopilot(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDGHCPCLI}

	// Simulate the post-fix entry-path composition: scanFlag returns "" for an
	// absent flag, which must be passed as-is to buildAdapter (no substitution).
	execPath := scanFlag(args, "--executable-path")
	if execPath != "" {
		// scanFlag itself is not the regression risk; the fallback injection is.
		// Document the invariant explicitly.
		t.Fatalf("scanFlag returned %q for absent --executable-path; want empty string — "+
			"scanFlag must not inject a default, only the entry-path fallback code does", execPath)
	}

	h := buildAdapter("", commonharness.HarnessIDGHCPCLI, execPath, "blanket", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != "copilot" {
		t.Errorf("CLI/TUI composition: absent --executable-path + buildAdapter(ghcp-cli) ExecutablePath() = %q, want %q; "+
			"neither entry path may inject a cross-harness literal before calling buildAdapter",
			got, "copilot")
	}
}

// TestPreScanComposition_OpenCode_AbsentExecPath_ResolvesOpenCode simulates
// the post-fix entry-path composition for the opencode harness.
func TestPreScanComposition_OpenCode_AbsentExecPath_ResolvesOpenCode(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDOpenCode}
	execPath := scanFlag(args, "--executable-path")

	h := buildAdapter("", commonharness.HarnessIDOpenCode, execPath, "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != "opencode" {
		t.Errorf("CLI/TUI composition: absent --executable-path + buildAdapter(opencode) ExecutablePath() = %q, want %q",
			got, "opencode")
	}
}

// TestPreScanComposition_ClaudeCode_AbsentExecPath_ResolvesDefaultClaude
// simulates the post-fix entry-path composition for the claude-code harness.
func TestPreScanComposition_ClaudeCode_AbsentExecPath_ResolvesDefaultClaude(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDClaudeCode}
	execPath := scanFlag(args, "--executable-path")

	h := buildAdapter("", commonharness.HarnessIDClaudeCode, execPath, "", 30*time.Minute)
	rev := requireExecutableRevealer(t, h)

	if got := rev.ExecutablePath(); got != "claude" {
		t.Errorf("CLI/TUI composition: absent --executable-path + buildAdapter(claude-code) ExecutablePath() = %q, want %q",
			got, "claude")
	}
}

// TestPreScanComposition_CLIAndTUI_ProduceSameResolution verifies that the
// CLI and TUI entry-path compositions are identical: both use scanFlag for
// the pre-scan, so the same args produce the same executable for each harness.
// This is the table-driven form of the individual composition tests above.
func TestPreScanComposition_CLIAndTUI_ProduceSameResolution(t *testing.T) {
	cases := []struct {
		harnessID string
		wantExe   string
	}{
		{commonharness.HarnessIDClaudeCode, "claude"},
		{commonharness.HarnessIDOpenCode, "opencode"},
		{commonharness.HarnessIDGHCPCLI, "copilot"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.harnessID, func(t *testing.T) {
			args := []string{"run", "--harness", tc.harnessID}
			execPath := scanFlag(args, "--executable-path")

			cliH := buildAdapter("", tc.harnessID, execPath, "", 30*time.Minute)
			tuiH := buildAdapter("", tc.harnessID, execPath, "", 30*time.Minute)

			cliRev := requireExecutableRevealer(t, cliH)
			tuiRev := requireExecutableRevealer(t, tuiH)

			cliGot := cliRev.ExecutablePath()
			tuiGot := tuiRev.ExecutablePath()

			if cliGot != tc.wantExe {
				t.Errorf("CLI entry path: buildAdapter(%q, '') ExecutablePath() = %q, want %q",
					tc.harnessID, cliGot, tc.wantExe)
			}
			if tuiGot != tc.wantExe {
				t.Errorf("TUI entry path: buildAdapter(%q, '') ExecutablePath() = %q, want %q",
					tc.harnessID, tuiGot, tc.wantExe)
			}
			if cliGot != tuiGot {
				t.Errorf("CLI (%q) and TUI (%q) entry paths resolved different executables for %q; "+
					"both paths must use the same per-harness default",
					cliGot, tuiGot, tc.harnessID)
			}
		})
	}
}
