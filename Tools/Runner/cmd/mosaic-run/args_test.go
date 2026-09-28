package main

import "testing"

// ---------------------------------------------------------------------------
// scanBoolFlag
// ---------------------------------------------------------------------------

func TestScanBoolFlag_FlagPresent_ReturnsTrue(t *testing.T) {
	if !scanBoolFlag([]string{"--tui"}, "--tui") {
		t.Error("scanBoolFlag([--tui], --tui) = false, want true")
	}
}

func TestScanBoolFlag_FlagAmongOtherArgs_ReturnsTrue(t *testing.T) {
	args := []string{"--artifact-location", "/tmp/art.md", "--tui"}
	if !scanBoolFlag(args, "--tui") {
		t.Error("scanBoolFlag should find --tui among mixed args")
	}
}

func TestScanBoolFlag_FlagAbsent_ReturnsFalse(t *testing.T) {
	if scanBoolFlag([]string{"run", "--artifact-location", "/tmp/art.md"}, "--tui") {
		t.Error("scanBoolFlag should return false when flag is not present")
	}
}

func TestScanBoolFlag_EmptyArgs_ReturnsFalse(t *testing.T) {
	if scanBoolFlag([]string{}, "--tui") {
		t.Error("scanBoolFlag with empty args should return false")
	}
}

func TestScanBoolFlag_PartialMatch_ReturnsFalse(t *testing.T) {
	// "--tuix" must not match "--tui".
	if scanBoolFlag([]string{"--tuix"}, "--tui") {
		t.Error("scanBoolFlag(--tuix, --tui) must not be a prefix match; should return false")
	}
}

// ---------------------------------------------------------------------------
// scanBoolFlagState
//
// scanBoolFlagState is the tri-state classifier underlying scanBoolFlagDefault.
// It distinguishes "flag absent" from "flag explicitly false" — a distinction
// that a two-state helper cannot make for flags whose cobra default is true.
// The design's testability notes name it as "table-testable across the four
// invocation forms"; these tests provide that direct coverage.
//
// All four tests are in RED until scanBoolFlagState is implemented: the panic
// stub ensures no coincidental pass is possible.
// ---------------------------------------------------------------------------

// TestScanBoolFlagState_FlagAbsent_ReturnsBoolFlagAbsent verifies that when the
// named flag does not appear in args, scanBoolFlagState returns boolFlagAbsent.
func TestScanBoolFlagState_FlagAbsent_ReturnsBoolFlagAbsent(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--mode", "auto"}
	got := scanBoolFlagState(args, "--pre-consult")
	if got != boolFlagAbsent {
		t.Errorf("scanBoolFlagState(flag absent) = %d, want boolFlagAbsent (%d)", got, boolFlagAbsent)
	}
}

// TestScanBoolFlagState_BareFlagPresent_ReturnsBoolFlagTrue verifies that the
// bare flag form ("--pre-consult" with no =value) is classified as boolFlagTrue.
// A bare boolean flag always means "enable" regardless of the cobra default.
func TestScanBoolFlagState_BareFlagPresent_ReturnsBoolFlagTrue(t *testing.T) {
	args := []string{"run", "--pre-consult", "--mode", "auto"}
	got := scanBoolFlagState(args, "--pre-consult")
	if got != boolFlagTrue {
		t.Errorf("scanBoolFlagState(bare --pre-consult) = %d, want boolFlagTrue (%d)", got, boolFlagTrue)
	}
}

// TestScanBoolFlagState_ExplicitTrue_ReturnsBoolFlagTrue verifies that the
// --flag=true form is classified as boolFlagTrue.
func TestScanBoolFlagState_ExplicitTrue_ReturnsBoolFlagTrue(t *testing.T) {
	args := []string{"run", "--pre-consult=true", "--mode", "auto"}
	got := scanBoolFlagState(args, "--pre-consult")
	if got != boolFlagTrue {
		t.Errorf("scanBoolFlagState(--pre-consult=true) = %d, want boolFlagTrue (%d)", got, boolFlagTrue)
	}
}

// TestScanBoolFlagState_ExplicitFalse_ReturnsBoolFlagFalse verifies that the
// --flag=false form is classified as boolFlagFalse, not as boolFlagAbsent.
// This distinction is the entire reason for the tri-state: without boolFlagFalse,
// a pre-scan for a true-default flag cannot distinguish "not supplied" (use the
// default of true) from "explicitly disabled" (use false).
func TestScanBoolFlagState_ExplicitFalse_ReturnsBoolFlagFalse(t *testing.T) {
	args := []string{"run", "--pre-consult=false", "--mode", "auto"}
	got := scanBoolFlagState(args, "--pre-consult")
	if got != boolFlagFalse {
		t.Errorf("scanBoolFlagState(--pre-consult=false) = %d, want boolFlagFalse (%d)", got, boolFlagFalse)
	}
}

// ---------------------------------------------------------------------------
// preConsultFromArgs (call-site agreement test)
//
// preConsultFromArgs is the thin wrapper in main.go that encapsulates the
// scanBoolFlagDefault(args, "--pre-consult", true) call. Testing this function
// rather than scanBoolFlagDefault directly pins the CALL SITE'S DEFAULT LITERAL:
// if the implementer writes scanBoolFlagDefault(args, "--pre-consult", false),
// the flag-omitted case below will fail even though all scanBoolFlagDefault unit
// tests pass. This is the AC2.4 coverage that the original T2.2 tests could not
// provide — they verified the helper's behavior for any caller who passes true,
// but not that main.go will call it with true.
// ---------------------------------------------------------------------------

// TestPreConsultFromArgs_FlagOmitted_ReturnsTrue verifies that when --pre-consult
// is absent from args, preConsultFromArgs returns true — the new cobra default.
// This is the primary agreement case: both the pre-scan and the cobra flag must
// agree on enabled when the flag is omitted.
func TestPreConsultFromArgs_FlagOmitted_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run"}
	if !preConsultFromArgs(args) {
		t.Error("preConsultFromArgs(flag absent) = false, want true; " +
			"the call site in main.go must pass true as the default to scanBoolFlagDefault, " +
			"matching the cobra flag declaration's default")
	}
}

// TestPreConsultFromArgs_BareFlagPresent_ReturnsTrue verifies that the bare
// --pre-consult form yields true from the call site in main.go.
func TestPreConsultFromArgs_BareFlagPresent_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run", "--pre-consult"}
	if !preConsultFromArgs(args) {
		t.Error("preConsultFromArgs(bare --pre-consult) = false, want true")
	}
}

// TestPreConsultFromArgs_ExplicitFalse_ReturnsFalse verifies that --pre-consult=false
// yields false from the call site in main.go. The =false form is the only way
// users can disable pre-consultation now that it defaults to true; if the call
// site did not pass the correct default, this form would still return false and
// the test would pass coincidentally — but the flag-omitted case would fail,
// catching the bug.
func TestPreConsultFromArgs_ExplicitFalse_ReturnsFalse(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run", "--pre-consult=false"}
	if preConsultFromArgs(args) {
		t.Error("preConsultFromArgs(--pre-consult=false) = true, want false; " +
			"the explicit-false form must override the default")
	}
}

// ---------------------------------------------------------------------------
// scanBoolFlagDefault
//
// scanBoolFlagDefault is the default-aware successor to scanBoolFlag. Unlike
// scanBoolFlag (which can only detect presence and implicitly defaults to
// false), scanBoolFlagDefault accepts an explicit default value and
// recognises the --flag=false and --flag=true forms in addition to the bare
// --flag form. This is required for --pre-consult, whose cobra default changes
// to true: the pre-scan in main() must mirror that default so that the
// pre-scan result and the cobra-parsed result always agree across all three
// invocation forms (omitted, bare, explicit-false).
//
// The three agreement cases must hold:
//   - Flag omitted: pre-scan returns true (the new default), cobra also returns true
//   - Bare flag (--pre-consult): pre-scan returns true, cobra also returns true
//   - Explicit false (--pre-consult=false): pre-scan returns false, cobra also returns false
// ---------------------------------------------------------------------------

// TestScanBoolFlagDefault_FlagAbsent_ReturnsGivenDefault_True verifies that when
// the flag is absent and the default is true, scanBoolFlagDefault returns true.
// This is the critical behaviour for --pre-consult: omitting it must yield enabled.
func TestScanBoolFlagDefault_FlagAbsent_ReturnsGivenDefault_True(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--mode", "auto"}
	if !scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("scanBoolFlagDefault(flag absent, default=true) = false, want true; " +
			"when the flag is absent the default must be returned")
	}
}

// TestScanBoolFlagDefault_FlagAbsent_ReturnsGivenDefault_False verifies that when
// the flag is absent and the default is false, scanBoolFlagDefault returns false.
// Mirrors the above case for other boolean flags whose default remains false.
func TestScanBoolFlagDefault_FlagAbsent_ReturnsGivenDefault_False(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md"}
	if scanBoolFlagDefault(args, "--manual-resolution", false) {
		t.Error("scanBoolFlagDefault(flag absent, default=false) = true, want false")
	}
}

// TestScanBoolFlagDefault_BareFlagPresent_ReturnsTrue verifies that the bare
// flag form (--pre-consult with no =value) is recognised as true regardless of
// the default value. A bare boolean flag always means "enable".
func TestScanBoolFlagDefault_BareFlagPresent_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--pre-consult", "--mode", "auto"}
	if !scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("scanBoolFlagDefault(bare --pre-consult, default=true) = false, want true")
	}
}

// TestScanBoolFlagDefault_ExplicitTrue_ReturnsTrue verifies that the --flag=true
// form returns true.
func TestScanBoolFlagDefault_ExplicitTrue_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--pre-consult=true", "--mode", "auto"}
	if !scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("scanBoolFlagDefault(--pre-consult=true) = false, want true")
	}
}

// TestScanBoolFlagDefault_ExplicitFalse_ReturnsFalse verifies that passing
// --flag=false returns false even when the caller-supplied default is true.
// This is the opt-out path for flags that default to enabled: the =false form
// is the only way to override a true default.
func TestScanBoolFlagDefault_ExplicitFalse_ReturnsFalse(t *testing.T) {
	args := []string{"run", "--pre-consult=false", "--mode", "auto"}
	if scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("scanBoolFlagDefault(--pre-consult=false, default=true) = true, want false; " +
			"the explicit-false form must override the default")
	}
}

// TestScanBoolFlagDefault_PreConsultAgreement_FlagOmitted verifies the first
// agreement case: when --pre-consult is absent, both the pre-scan and cobra
// must agree on the enabled default. The pre-scan side is checked here;
// the CLI flag side is covered by TestPreConsultFlag_DefaultIsTrue_WhenOmitted.
func TestScanBoolFlagDefault_PreConsultAgreement_FlagOmitted(t *testing.T) {
	// Simulate a real invocation without --pre-consult.
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run"}
	if !scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("pre-scan: --pre-consult absent → scanBoolFlagDefault should return true (new default); " +
			"pre-scan and cobra default must agree: both enabled when flag is omitted")
	}
}

// TestScanBoolFlagDefault_PreConsultAgreement_BareFlagPresent verifies the second
// agreement case: a bare --pre-consult flag must produce true in both the pre-scan
// and cobra parsing paths.
func TestScanBoolFlagDefault_PreConsultAgreement_BareFlagPresent(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run", "--pre-consult"}
	if !scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("pre-scan: bare --pre-consult → scanBoolFlagDefault should return true; " +
			"pre-scan and cobra must agree: both enabled for bare flag")
	}
}

// TestScanBoolFlagDefault_PreConsultAgreement_ExplicitFalse verifies the third
// agreement case: --pre-consult=false must produce false in both the pre-scan
// and cobra parsing paths. Without this, the pre-scan would wire a consultant
// while cobra disables pre-consultation, causing a silent mismatch.
func TestScanBoolFlagDefault_PreConsultAgreement_ExplicitFalse(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1",
		"--task", "do work", "--mode", "auto", "--new-run", "--pre-consult=false"}
	if scanBoolFlagDefault(args, "--pre-consult", true) {
		t.Error("pre-scan: --pre-consult=false → scanBoolFlagDefault should return false; " +
			"pre-scan and cobra must agree: both disabled for explicit-false form")
	}
}

// ---------------------------------------------------------------------------
// hasPositionalArg
// ---------------------------------------------------------------------------

func TestHasPositionalArg_WithPositionalArg_ReturnsTrue(t *testing.T) {
	if !hasPositionalArg([]string{"run"}) {
		t.Error("hasPositionalArg([run]) = false, want true")
	}
}

func TestHasPositionalArg_MixedFlagsAndPositional_ReturnsTrue(t *testing.T) {
	args := []string{"--orchestrator-file", "Orch.md", "run"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg should detect positional arg among flags")
	}
}

func TestHasPositionalArg_OnlyBoolFlags_ReturnsFalse(t *testing.T) {
	// Boolean flags (no separate value token) all start with "-"; no positional present.
	args := []string{"--tui", "--verbose"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--tui --verbose]) should return false — no positional arg")
	}
}

func TestHasPositionalArg_EmptyArgs_ReturnsFalse(t *testing.T) {
	if hasPositionalArg([]string{}) {
		t.Error("hasPositionalArg(empty) should return false")
	}
}

func TestHasPositionalArg_SingleDashArgs_AreNotPositional(t *testing.T) {
	// Single-dash args like "-v" are flags, not positional args.
	args := []string{"-v", "--verbose"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg should treat single-dash args as flags, not positional")
	}
}

// ---------------------------------------------------------------------------
// scanFlag
//
// scanFlag is the pre-scan helper that extracts flag values from os.Args before
// cobra parses them. It is used in main() to pick up --executable-path, --harness,
// --timeout, and --orchestrator-file before the session is constructed.
//
// These tests focus on --executable-path propagation (review issue: AC3.4 propagation
// was only validated by code inspection, not by a test). They confirm that scanFlag
// reads the value correctly in both "--flag value" and "--flag=value" forms, which
// is the entire mechanism by which the user's --executable-path reaches
// claudecode.NewClaudeCodeAdapter in main().
// ---------------------------------------------------------------------------

func TestScanFlag_SpaceSeparated_ReturnsValue(t *testing.T) {
	// "--executable-path /usr/local/bin/claude" form — most common shell form.
	got := scanFlag([]string{"run", "--executable-path", "/usr/local/bin/claude"}, "--executable-path")
	if got != "/usr/local/bin/claude" {
		t.Errorf("scanFlag space-separated = %q, want %q", got, "/usr/local/bin/claude")
	}
}

func TestScanFlag_EqualsSeparated_ReturnsValue(t *testing.T) {
	// "--executable-path=/usr/local/bin/claude" form — shell quoting alternative.
	got := scanFlag([]string{"run", "--executable-path=/usr/local/bin/claude"}, "--executable-path")
	if got != "/usr/local/bin/claude" {
		t.Errorf("scanFlag equals-separated = %q, want %q", got, "/usr/local/bin/claude")
	}
}

func TestScanFlag_FlagAbsent_ReturnsEmpty(t *testing.T) {
	// When --executable-path is omitted, scanFlag returns "". buildAdapter then
	// substitutes the per-harness default value, which NewClaudeCodeAdapter receives.
	got := scanFlag([]string{"run", "--harness", "claude-code"}, "--executable-path")
	if got != "" {
		t.Errorf("scanFlag absent flag = %q, want empty string", got)
	}
}

func TestScanFlag_EmptyArgs_ReturnsEmpty(t *testing.T) {
	got := scanFlag([]string{}, "--executable-path")
	if got != "" {
		t.Errorf("scanFlag empty args = %q, want empty string", got)
	}
}

func TestScanFlag_FlagWithNoFollowingValue_ReturnsEmpty(t *testing.T) {
	// "--executable-path" appears as the last arg with no value token — scanFlag
	// must not panic and must return "".
	got := scanFlag([]string{"run", "--executable-path"}, "--executable-path")
	if got != "" {
		t.Errorf("scanFlag flag with no value = %q, want empty string", got)
	}
}

func TestScanFlag_FlagAmongMixedArgs_ReturnsValue(t *testing.T) {
	// Verifies scanFlag correctly finds --executable-path among other flags, which
	// mirrors real invocations like: mosaic-run run --harness claude-code
	// --orchestrator-file orch.md --executable-path /custom/claude --workflow w1 ...
	args := []string{
		"run",
		"--harness", "claude-code",
		"--orchestrator-file", "orch.md",
		"--executable-path", "/custom/claude",
		"--workflow", "w1",
	}
	got := scanFlag(args, "--executable-path")
	if got != "/custom/claude" {
		t.Errorf("scanFlag among mixed args = %q, want %q", got, "/custom/claude")
	}
}

func TestScanFlag_PartialPrefixDoesNotMatch(t *testing.T) {
	// "--executable-pathx" must not match "--executable-path" for the space-separated
	// form. The flag check uses exact equality, not prefix matching.
	got := scanFlag([]string{"--executable-pathx", "/should-not-match"}, "--executable-path")
	if got != "" {
		t.Errorf("scanFlag partial prefix match = %q, want empty string", got)
	}
}

func TestScanFlag_HarnessFlag_SpaceSeparated(t *testing.T) {
	// Spot-check that scanFlag works correctly for --harness as well, since
	// it is the primary flag controlling adapter construction in main().
	got := scanFlag([]string{"run", "--harness", "claude-code"}, "--harness")
	if got != "claude-code" {
		t.Errorf("scanFlag --harness = %q, want %q", got, "claude-code")
	}
}

func TestScanFlag_TimeoutFlag_EqualsSeparated(t *testing.T) {
	// Spot-check that scanFlag correctly extracts --timeout in equals form,
	// since timeout parsing in main() depends on scanFlag.
	got := scanFlag([]string{"run", "--timeout=45m"}, "--timeout")
	if got != "45m" {
		t.Errorf("scanFlag --timeout= = %q, want %q", got, "45m")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Process-startup pre-scan tests for --mode and --manual-resolution
//
// These tests verify that scanFlag and scanBoolFlag correctly extract the new
// Stage 7 flags from the argument list, confirming the pre-scan mechanism that
// main() uses to wire consultants before cobra parses flags.
// ---------------------------------------------------------------------------

// TestScanFlag_ModeFlag_SpaceSeparated verifies that --mode in the common
// "--flag value" form is extracted correctly by scanFlag.
func TestScanFlag_ModeFlag_SpaceSeparated(t *testing.T) {
	got := scanFlag([]string{"run", "--mode", "orchestrated"}, "--mode")
	if got != "orchestrated" {
		t.Errorf("scanFlag(--mode orchestrated) = %q, want %q", got, "orchestrated")
	}
}

// TestScanFlag_ModeFlag_EqualsSeparated verifies that --mode in the
// "--flag=value" form is extracted correctly by scanFlag.
func TestScanFlag_ModeFlag_EqualsSeparated(t *testing.T) {
	got := scanFlag([]string{"run", "--mode=auto-review"}, "--mode")
	if got != "auto-review" {
		t.Errorf("scanFlag(--mode=auto-review) = %q, want %q", got, "auto-review")
	}
}

// TestScanFlag_ModeFlag_Absent_ReturnsEmpty verifies that when --mode is not
// present, scanFlag returns "" so main() can detect the absent-mode condition.
func TestScanFlag_ModeFlag_Absent_ReturnsEmpty(t *testing.T) {
	got := scanFlag([]string{"run", "--orchestrator-file", "orch.md"}, "--mode")
	if got != "" {
		t.Errorf("scanFlag absent --mode = %q, want empty string", got)
	}
}

// TestScanBoolFlag_ManualResolutionFlag_Present verifies that --manual-resolution
// is detected correctly by scanBoolFlag.
func TestScanBoolFlag_ManualResolutionFlag_Present(t *testing.T) {
	args := []string{"run", "--mode", "orchestrated", "--manual-resolution"}
	if !scanBoolFlag(args, "--manual-resolution") {
		t.Error("scanBoolFlag should return true when --manual-resolution is present")
	}
}

// TestScanBoolFlag_ManualResolutionFlag_Absent verifies that scanBoolFlag returns
// false when --manual-resolution is not in the argument list.
func TestScanBoolFlag_ManualResolutionFlag_Absent(t *testing.T) {
	args := []string{"run", "--mode", "auto", "--orchestrator-file", "orch.md"}
	if scanBoolFlag(args, "--manual-resolution") {
		t.Error("scanBoolFlag should return false when --manual-resolution is absent")
	}
}

// TestScanFlag_ModeFlag_AmongMixedArgs verifies that scanFlag correctly finds
// --mode among other flags and values — mirroring the real invocation shape
// where --mode appears after several other flags.
func TestScanFlag_ModeFlag_AmongMixedArgs(t *testing.T) {
	args := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--harness", "claude-code",
	}
	got := scanFlag(args, "--mode")
	if got != "auto" {
		t.Errorf("scanFlag(--mode) among mixed args = %q, want %q", got, "auto")
	}
}
