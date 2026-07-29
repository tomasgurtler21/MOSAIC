package main

// main_test.go covers the TUI-routing helper functions added to main.go during the
// Stage-10 review-fix phase. These helpers control whether mosaic-run launches the
// TUI frontend or the CLI frontend and must be correct for the TUI to be reachable
// in production.

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
