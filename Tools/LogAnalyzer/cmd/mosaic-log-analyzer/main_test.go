package main

// main_test.go covers the helper functions in main.go that control dispatch
// routing and flag pre-scanning. These helpers live in package main because
// they are composition-root concerns and cannot be reached through the cli or
// tui packages, which receive fully-wired dependencies.

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// scanBoolFlag
// ---------------------------------------------------------------------------

func TestScanBoolFlag_FlagPresent_ReturnsTrue(t *testing.T) {
	if !scanBoolFlag([]string{"--tui"}, "--tui") {
		t.Error("scanBoolFlag([--tui], --tui) = false, want true")
	}
}

func TestScanBoolFlag_FlagAmongOtherArgs_ReturnsTrue(t *testing.T) {
	args := []string{"--path", "/tmp/logs", "--tui"}
	if !scanBoolFlag(args, "--tui") {
		t.Error("scanBoolFlag should find --tui among mixed args")
	}
}

func TestScanBoolFlag_FlagAbsent_ReturnsFalse(t *testing.T) {
	args := []string{"report", "--path", "/tmp/logs"}
	if scanBoolFlag(args, "--tui") {
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
//
// hasPositionalArg is flag-aware: it knows which flags consume a following
// value token so it does not misidentify those values as positional arguments.
// Only tokens that are neither a flag nor a flag-value count as positional.
//
// The flags that consume a value token in both "--flag value" and "--flag=value"
// forms are: --path, --run, --format.
// The flags that are boolean (consume no following value): --tui.
// ---------------------------------------------------------------------------

func TestHasPositionalArg_SubcommandPresent_ReturnsTrue(t *testing.T) {
	// "report" is a positional subcommand that does not start with "-".
	if !hasPositionalArg([]string{"report"}) {
		t.Error("hasPositionalArg([report]) = false, want true")
	}
}

func TestHasPositionalArg_TotalSubcommand_ReturnsTrue(t *testing.T) {
	if !hasPositionalArg([]string{"total", "--run", "20260802T074635Z-abcd"}) {
		t.Error("hasPositionalArg should detect 'total' as a positional arg")
	}
}

func TestHasPositionalArg_PricingSubcommand_ReturnsTrue(t *testing.T) {
	if !hasPositionalArg([]string{"pricing", "path"}) {
		t.Error("hasPositionalArg should detect 'pricing' as a positional arg")
	}
}

func TestHasPositionalArg_SubcommandWithFlagsAndPath_ReturnsTrue(t *testing.T) {
	// "report" is positional; "--path" value and "--format" value are not.
	args := []string{"report", "--path", "/my/logs", "--format", "json"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg should detect 'report' as positional even when value flags are present")
	}
}

func TestHasPositionalArg_OnlyBoolFlag_ReturnsFalse(t *testing.T) {
	// "--tui" is a boolean flag; no positional present.
	if hasPositionalArg([]string{"--tui"}) {
		t.Error("hasPositionalArg([--tui]) should return false — no positional arg")
	}
}

func TestHasPositionalArg_OnlyValueFlagSpaceSeparated_ReturnsFalse(t *testing.T) {
	// "--path /some/path" — the value "/some/path" is the flag's argument, not
	// a positional subcommand. The scanner must recognise --path as a value flag
	// and skip its following token.
	args := []string{"--path", "/some/path"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--path /some/path]) should return false — /some/path is a flag value, not a positional arg")
	}
}

func TestHasPositionalArg_OnlyValueFlagEqualsSeparated_ReturnsFalse(t *testing.T) {
	// "--path=/some/path" is entirely one token; no following value token exists.
	args := []string{"--path=/some/path"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--path=/some/path]) should return false — equals-form has no separate value token")
	}
}

func TestHasPositionalArg_RunFlagSpaceSeparated_ReturnsFalse(t *testing.T) {
	// "--run <id>" — the run ID is the flag value, not a positional arg.
	args := []string{"--run", "20260802T074635Z-abcd"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--run id]) should return false — run id is a flag value")
	}
}

func TestHasPositionalArg_FormatFlagSpaceSeparated_ReturnsFalse(t *testing.T) {
	// "--format json" — "json" is the flag value, not a positional arg.
	args := []string{"--format", "json"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--format json]) should return false — format value is not positional")
	}
}

func TestHasPositionalArg_MultipleFlagOnlyArgs_ReturnsFalse(t *testing.T) {
	// Combination of boolean and value flags with no subcommand.
	args := []string{"--tui", "--path", "/my/logs"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--tui --path /my/logs]) should return false — all are flags or flag values")
	}
}

func TestHasPositionalArg_EmptyArgs_ReturnsFalse(t *testing.T) {
	if hasPositionalArg([]string{}) {
		t.Error("hasPositionalArg(empty) should return false")
	}
}

// ---------------------------------------------------------------------------
// scanFlag
//
// scanFlag does a minimal pre-scan of args for a named flag. It supports both
// "--flag value" (space-separated) and "--flag=value" (equals-separated) forms,
// matching cobra's own flag parsing so both styles are always equivalent.
// When the flag is absent, the caller-supplied def is returned unchanged.
//
// Tests cover the three flags the composition root pre-scans: --path, --run,
// and --format.
// ---------------------------------------------------------------------------

func TestScanFlag_Path_SpaceSeparated_ReturnsValue(t *testing.T) {
	got := scanFlag([]string{"report", "--path", "/my/logs"}, "--path", "")
	if got != "/my/logs" {
		t.Errorf("scanFlag --path space-separated = %q, want %q", got, "/my/logs")
	}
}

func TestScanFlag_Path_EqualsSeparated_ReturnsValue(t *testing.T) {
	got := scanFlag([]string{"report", "--path=/my/logs"}, "--path", "")
	if got != "/my/logs" {
		t.Errorf("scanFlag --path equals-separated = %q, want %q", got, "/my/logs")
	}
}

func TestScanFlag_Path_Absent_ReturnsEmptyStringDefault(t *testing.T) {
	// When --path is omitted and def is "", the composition root receives ""
	// and applies its own default (OrchestrationLogs/ under the working directory).
	got := scanFlag([]string{"report", "--format", "json"}, "--path", "")
	if got != "" {
		t.Errorf("scanFlag --path absent (def=%q) = %q, want empty string", "", got)
	}
}

func TestScanFlag_Path_Absent_ReturnsSuppliedDefault(t *testing.T) {
	// When --path is omitted, the caller-supplied def is returned unchanged.
	// This verifies that default substitution is the caller's responsibility
	// (the composition root may supply a non-empty default directly here).
	got := scanFlag([]string{"report", "--format", "json"}, "--path", "/default/logs")
	if got != "/default/logs" {
		t.Errorf("scanFlag --path absent (def=%q) = %q, want %q", "/default/logs", got, "/default/logs")
	}
}

func TestScanFlag_Run_SpaceSeparated_ReturnsValue(t *testing.T) {
	got := scanFlag([]string{"total", "--run", "20260802T074635Z-abcd"}, "--run", "")
	if got != "20260802T074635Z-abcd" {
		t.Errorf("scanFlag --run space-separated = %q, want %q", got, "20260802T074635Z-abcd")
	}
}

func TestScanFlag_Run_EqualsSeparated_ReturnsValue(t *testing.T) {
	got := scanFlag([]string{"total", "--run=20260802T074635Z-abcd"}, "--run", "")
	if got != "20260802T074635Z-abcd" {
		t.Errorf("scanFlag --run equals-separated = %q, want %q", got, "20260802T074635Z-abcd")
	}
}

func TestScanFlag_Format_EqualsSeparated_ReturnsValue(t *testing.T) {
	got := scanFlag([]string{"report", "--format=json"}, "--format", "")
	if got != "json" {
		t.Errorf("scanFlag --format= = %q, want %q", got, "json")
	}
}

func TestScanFlag_EmptyArgs_ReturnsDef(t *testing.T) {
	got := scanFlag([]string{}, "--path", "")
	if got != "" {
		t.Errorf("scanFlag empty args = %q, want empty string", got)
	}
}

func TestScanFlag_FlagWithNoFollowingValue_ReturnsDef(t *testing.T) {
	// "--path" appears as the last arg with no value token — scanFlag must not
	// panic and must return def so the default substitution is reached.
	got := scanFlag([]string{"report", "--path"}, "--path", "")
	if got != "" {
		t.Errorf("scanFlag flag with no trailing value = %q, want empty string", got)
	}
}

func TestScanFlag_PartialPrefixDoesNotMatch(t *testing.T) {
	// "--pathx" must not match "--path" in the space-separated form. The flag
	// check uses exact equality, not prefix matching.
	got := scanFlag([]string{"--pathx", "/should-not-match"}, "--path", "")
	if got != "" {
		t.Errorf("scanFlag partial prefix match = %q, want empty string", got)
	}
}

func TestScanFlag_FlagAmongMixedArgs_ReturnsValue(t *testing.T) {
	// Mirrors a real invocation: mosaic-log-analyzer report --run id
	// --format json --path /my/logs
	args := []string{
		"report",
		"--run", "20260802T074635Z-abcd",
		"--format", "json",
		"--path", "/my/logs",
	}
	got := scanFlag(args, "--path", "")
	if got != "/my/logs" {
		t.Errorf("scanFlag among mixed args = %q, want %q", got, "/my/logs")
	}
}

// ---------------------------------------------------------------------------
// wantsTUI
//
// wantsTUI reports whether the composition root should launch the interactive
// TUI. It accepts an injectable isTerminal seam — func(fd uintptr) bool — so
// the per-fd terminal check is testable without a real TTY.
//
// Binding rules (from the Dispatch Rule in Plan.md and ContractsDesign.md):
//   - "--tui" present anywhere in args → TUI (seam is never called)
//   - A positional (non-flag) argument present → CLI (seam is never called)
//   - "--tui" wins when both "--tui" and a positional are present → TUI
//   - Otherwise → isTerminal(stdin) && isTerminal(stdout); both file
//     descriptors must be terminals for TUI to be selected
// ---------------------------------------------------------------------------

func TestWantsTUI_TuiFlag_ReturnsTrue(t *testing.T) {
	// "--tui" alone must select TUI mode. The seam must not be called.
	seamCalled := false
	seam := func(fd uintptr) bool { seamCalled = true; return false }
	if !wantsTUI([]string{"--tui"}, seam) {
		t.Error("wantsTUI([--tui]) = false, want true")
	}
	if seamCalled {
		t.Error("wantsTUI with --tui must not call the terminal seam")
	}
}

func TestWantsTUI_TuiFlagAmongOtherFlags_ReturnsTrue(t *testing.T) {
	args := []string{"--path", "/my/logs", "--tui"}
	seam := func(fd uintptr) bool { return false }
	if !wantsTUI(args, seam) {
		t.Error("wantsTUI should find --tui among mixed flags and return true")
	}
}

func TestWantsTUI_PositionalArgPresent_ReturnsFalse(t *testing.T) {
	// A positional subcommand forces CLI mode. The seam must not be called.
	seamCalled := false
	seam := func(fd uintptr) bool { seamCalled = true; return true }
	if wantsTUI([]string{"report"}, seam) {
		t.Error("wantsTUI([report]) = true, want false — positional arg forces CLI")
	}
	if seamCalled {
		t.Error("wantsTUI with positional arg must not call the terminal seam")
	}
}

func TestWantsTUI_TuiFlagWinsOverPositional(t *testing.T) {
	// When both "--tui" and a positional arg are present, "--tui" takes priority.
	args := []string{"--tui", "report"}
	seam := func(fd uintptr) bool { return false }
	if !wantsTUI(args, seam) {
		t.Error("wantsTUI([--tui report]) = false, want true — --tui wins over positional")
	}
}

func TestWantsTUI_FlagOnlyArgs_ConsultsTerminalSeam_True(t *testing.T) {
	// No positional subcommand and no --tui: the seam determines the outcome.
	// When the seam reports both stdin and stdout are terminals, TUI is selected.
	args := []string{"--path", "/my/logs"}
	seam := func(fd uintptr) bool { return true }
	if !wantsTUI(args, seam) {
		t.Error("wantsTUI(flag-only args, seam=true) = false, want true")
	}
}

func TestWantsTUI_FlagOnlyArgs_ConsultsTerminalSeam_False(t *testing.T) {
	// When the seam reports non-terminal (e.g. piped output in CI), CLI fallback.
	args := []string{"--path", "/my/logs"}
	seam := func(fd uintptr) bool { return false }
	if wantsTUI(args, seam) {
		t.Error("wantsTUI(flag-only args, seam=false) = true, want false")
	}
}

func TestWantsTUI_EmptyArgs_TerminalSeam_True(t *testing.T) {
	// No args at all: fall through to terminal detection.
	seam := func(fd uintptr) bool { return true }
	if !wantsTUI([]string{}, seam) {
		t.Error("wantsTUI(empty, seam=true) = false, want true")
	}
}

func TestWantsTUI_EmptyArgs_TerminalSeam_False(t *testing.T) {
	seam := func(fd uintptr) bool { return false }
	if wantsTUI([]string{}, seam) {
		t.Error("wantsTUI(empty, seam=false) = true, want false")
	}
}

func TestWantsTUI_ValueFlagValueNotTreatedAsPositional_ConsultsSeam(t *testing.T) {
	// "--run 20260802T074635Z-abcd" — the run ID does not start with "-" but it
	// is the value of --run, not a positional subcommand. wantsTUI must not
	// treat it as a positional arg and must consult the seam instead.
	seamCalled := false
	seam := func(fd uintptr) bool { seamCalled = true; return true }
	result := wantsTUI([]string{"--run", "20260802T074635Z-abcd"}, seam)
	if !seamCalled {
		t.Error("wantsTUI should have consulted the terminal seam — run id is a flag value, not a positional arg")
	}
	if !result {
		t.Error("wantsTUI(flag-only, seam=true) should return true")
	}
}

func TestWantsTUI_PathEqualsSeparated_ConsultsSeam(t *testing.T) {
	// "--path=/my/logs" in equals form: the entire token starts with "-", so no
	// separate value token exists. Seam must be consulted.
	seamCalled := false
	seam := func(fd uintptr) bool { seamCalled = true; return false }
	wantsTUI([]string{"--path=/my/logs"}, seam)
	if !seamCalled {
		t.Error("wantsTUI([--path=/my/logs]) should consult the terminal seam")
	}
}

// TestWantsTUI_SeamCalledForBothFds_WhenFallbackReached confirms the seam is
// called for both stdin and stdout when the fallback path is reached. The
// binding rule is isTerminal(stdin) && isTerminal(stdout); both fds must be
// checked, so the seam is invoked twice per wantsTUI call on the fallback path.
func TestWantsTUI_SeamCalledForBothFds_WhenFallbackReached(t *testing.T) {
	callCount := 0
	seam := func(fd uintptr) bool { callCount++; return true }
	wantsTUI([]string{}, seam)
	if callCount != 2 {
		t.Errorf("terminal seam called %d times on fallback path, want 2 (once for stdin fd, once for stdout fd)", callCount)
	}
}

// TestWantsTUI_StdinTerminalStdoutNotTerminal_ReturnsFalse validates the AND
// rule: if stdout is not a terminal even when stdin is, wantsTUI must return
// false. This is the AC11.2 requirement — "both stdin and stdout must be
// terminals" — expressed as a test that can distinguish the two fd checks.
func TestWantsTUI_StdinTerminalStdoutNotTerminal_ReturnsFalse(t *testing.T) {
	// The seam returns true only for the stdin fd. When wantsTUI checks stdout
	// the seam returns false, so the AND-combination is false.
	seam := func(fd uintptr) bool {
		return fd == os.Stdin.Fd()
	}
	if wantsTUI([]string{}, seam) {
		t.Error("wantsTUI should return false when stdout is not a terminal, even if stdin is")
	}
}

// TestWantsTUI_StdoutTerminalStdinNotTerminal_ReturnsFalse validates the AND
// rule from the other direction: if stdin is not a terminal, wantsTUI must
// return false regardless of stdout's state.
func TestWantsTUI_StdoutTerminalStdinNotTerminal_ReturnsFalse(t *testing.T) {
	// The seam returns true only for the stdout fd. Stdin is not a terminal,
	// so isTerminal(stdin) is false and the AND-combination is false.
	seam := func(fd uintptr) bool {
		return fd == os.Stdout.Fd()
	}
	if wantsTUI([]string{}, seam) {
		t.Error("wantsTUI should return false when stdin is not a terminal, even if stdout is")
	}
}
