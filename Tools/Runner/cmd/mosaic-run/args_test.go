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
