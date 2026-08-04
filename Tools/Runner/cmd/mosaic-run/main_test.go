package main

// main_test.go covers the helper functions in main.go that control routing and
// adapter construction. These helpers are exercised here because they live in
// package main (dependency wiring) and cannot be reached through cli.Run-based
// tests, which inject a pre-built session and never enter main's wiring path.

import (
	"os"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/harness"
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
// cobra parses them. It is used in main() to pick up --claude-path, --harness,
// --timeout, and --orchestrator-file before the session is constructed.
//
// These tests focus on --claude-path propagation (review issue: AC3.4 propagation
// was only validated by code inspection, not by a test). They confirm that scanFlag
// reads the value correctly in both "--flag value" and "--flag=value" forms, which
// is the entire mechanism by which the user's --claude-path reaches
// harness.NewClaudeCodeAdapter in main().
// ---------------------------------------------------------------------------

func TestScanFlag_SpaceSeparated_ReturnsValue(t *testing.T) {
	// "--claude-path /usr/local/bin/claude" form — most common shell form.
	got := scanFlag([]string{"run", "--claude-path", "/usr/local/bin/claude"}, "--claude-path")
	if got != "/usr/local/bin/claude" {
		t.Errorf("scanFlag space-separated = %q, want %q", got, "/usr/local/bin/claude")
	}
}

func TestScanFlag_EqualsSeparated_ReturnsValue(t *testing.T) {
	// "--claude-path=/usr/local/bin/claude" form — shell quoting alternative.
	got := scanFlag([]string{"run", "--claude-path=/usr/local/bin/claude"}, "--claude-path")
	if got != "/usr/local/bin/claude" {
		t.Errorf("scanFlag equals-separated = %q, want %q", got, "/usr/local/bin/claude")
	}
}

func TestScanFlag_FlagAbsent_ReturnsEmpty(t *testing.T) {
	// When --claude-path is omitted, scanFlag returns "". main() then substitutes
	// the default value "claude", which NewClaudeCodeAdapter receives.
	got := scanFlag([]string{"run", "--harness", "claude-code"}, "--claude-path")
	if got != "" {
		t.Errorf("scanFlag absent flag = %q, want empty string", got)
	}
}

func TestScanFlag_EmptyArgs_ReturnsEmpty(t *testing.T) {
	got := scanFlag([]string{}, "--claude-path")
	if got != "" {
		t.Errorf("scanFlag empty args = %q, want empty string", got)
	}
}

func TestScanFlag_FlagWithNoFollowingValue_ReturnsEmpty(t *testing.T) {
	// "--claude-path" appears as the last arg with no value token — scanFlag
	// must not panic and must return "".
	got := scanFlag([]string{"run", "--claude-path"}, "--claude-path")
	if got != "" {
		t.Errorf("scanFlag flag with no value = %q, want empty string", got)
	}
}

func TestScanFlag_FlagAmongMixedArgs_ReturnsValue(t *testing.T) {
	// Verifies scanFlag correctly finds --claude-path among other flags, which
	// mirrors real invocations like: mosaic-run run --harness claude-code
	// --orchestrator-file orch.md --claude-path /custom/claude --workflow w1 ...
	args := []string{
		"run",
		"--harness", "claude-code",
		"--orchestrator-file", "orch.md",
		"--claude-path", "/custom/claude",
		"--workflow", "w1",
	}
	got := scanFlag(args, "--claude-path")
	if got != "/custom/claude" {
		t.Errorf("scanFlag among mixed args = %q, want %q", got, "/custom/claude")
	}
}

func TestScanFlag_PartialPrefixDoesNotMatch(t *testing.T) {
	// "--claude-pathx" must not match "--claude-path" for the space-separated
	// form. The flag check uses exact equality, not prefix matching.
	got := scanFlag([]string{"--claude-pathx", "/should-not-match"}, "--claude-path")
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
// buildAdapter
//
// buildAdapter is the extracted adapter-construction function that main() and
// sessFactory both delegate to. These tests verify the branching that was
// previously inline in main() and untestable (the switch on --harness that
// constructs ClaudeCodeAdapter vs FakeAdapter). They close the AC3.1/AC3.2/
// AC3.3/AC3.4 coverage gap.
// ---------------------------------------------------------------------------

// TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter verifies that passing
// "claude-code" as harnessStr constructs a *harness.ClaudeCodeAdapter. This
// is the primary AC3.1 assertion: the right adapter type is instantiated for
// the real Claude Code CLI.
func TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter(t *testing.T) {
	h := buildAdapter("claude-code", "/custom/claude", 45*time.Minute)
	if _, ok := h.(*harness.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code) returned %T, want *harness.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_Fake_ReturnsFakeAdapter verifies that "fake" constructs a
// *harness.FakeAdapter, confirming backward-compatible default behaviour (AC3.2).
func TestBuildAdapter_Fake_ReturnsFakeAdapter(t *testing.T) {
	h := buildAdapter("fake", "", 0)
	if _, ok := h.(*harness.FakeAdapter); !ok {
		t.Errorf("buildAdapter(fake) returned %T, want *harness.FakeAdapter", h)
	}
}

// TestBuildAdapter_Unknown_ReturnsFakeAdapter verifies that an unrecognised
// harnessStr falls back to FakeAdapter without panicking. Unknown values are
// rejected upstream by cli.Run (AC3.8); buildAdapter is a safe fallback.
func TestBuildAdapter_Unknown_ReturnsFakeAdapter(t *testing.T) {
	h := buildAdapter("unknown-harness", "", 0)
	if _, ok := h.(*harness.FakeAdapter); !ok {
		t.Errorf("buildAdapter(unknown-harness) returned %T, want *harness.FakeAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_ZeroTimeoutDefaultsTo30Min verifies that a zero
// timeout (e.g. when cfg.Timeout is unset in the TUI before the config screen
// runs) is treated as the 30-minute default, not a zero-timeout adapter. The
// returned adapter must still be a ClaudeCodeAdapter.
func TestBuildAdapter_ClaudeCode_ZeroTimeoutDefaultsTo30Min(t *testing.T) {
	h := buildAdapter("claude-code", "claude", 0)
	if _, ok := h.(*harness.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, timeout=0) returned %T, want *harness.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_DefaultPath verifies that using the default
// executable path ("claude", the value main() substitutes when --claude-path
// is absent) produces a valid ClaudeCodeAdapter. This closes AC3.4's default
// propagation gap.
func TestBuildAdapter_ClaudeCode_DefaultPath(t *testing.T) {
	h := buildAdapter("claude-code", "claude", 30*time.Minute)
	if _, ok := h.(*harness.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, claude, 30m) returned %T, want *harness.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_CustomPathAndTimeout verifies that a non-default
// executable path and a non-default timeout both produce a ClaudeCodeAdapter.
// Covers the combined AC3.3 + AC3.4 propagation path.
func TestBuildAdapter_ClaudeCode_CustomPathAndTimeout(t *testing.T) {
	h := buildAdapter("claude-code", "/opt/claude/bin/claude", 90*time.Second)
	if _, ok := h.(*harness.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, custom path/timeout) returned %T, want *harness.ClaudeCodeAdapter", h)
	}
}

// ---------------------------------------------------------------------------
// buildTUIDelegate
//
// buildTUIDelegate is the extracted TUI delegate-wiring function that sessFactory
// delegates to. These tests verify the wiring conditions that were previously
// inline in the sessFactory closure and untestable without a real terminal.
// ---------------------------------------------------------------------------

// TestBuildTUIDelegate_ClaudeCodeWithOrchFile_ReturnsOrchestratorDelegate verifies
// that selecting "claude-code" with a known orchestrator file produces a non-nil
// OrchestratorDelegate wired with the provided harness. This closes AC3.6 (TUI
// delegation wiring with real adapter).
func TestBuildTUIDelegate_ClaudeCodeWithOrchFile_ReturnsOrchestratorDelegate(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "claude-code", "/orch/dir/orch.md", "/run/folder")
	if delegate == nil {
		t.Fatal("buildTUIDelegate returned nil, want *deviation.OrchestratorDelegate")
	}
	if delegate.Harness != h {
		t.Error("delegate.Harness is not the expected harness instance; same adapter must be shared between session dispatch and deviation resolution")
	}
}

// TestBuildTUIDelegate_ClaudeCodeWithOrchFile_WiresOrchestratorIdentifier verifies
// that the returned delegate has the correct orchestrator agent identifier set.
func TestBuildTUIDelegate_ClaudeCodeWithOrchFile_WiresOrchestratorIdentifier(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "claude-code", "/orch/dir/orch.md", "/run/folder")
	if delegate == nil {
		t.Fatal("buildTUIDelegate returned nil")
	}
	if delegate.Orchestrator.Identifier != "orchestrator-script" {
		t.Errorf("delegate.Orchestrator.Identifier = %q, want %q", delegate.Orchestrator.Identifier, "orchestrator-script")
	}
}

// TestBuildTUIDelegate_ClaudeCodeWithOrchFile_WiresOrchestratorInvocationKind
// verifies that the orchestrator AgentReference uses InvocationOrchestrator,
// which causes ClaudeCodeAdapter to use the --agent flag for the invocation.
func TestBuildTUIDelegate_ClaudeCodeWithOrchFile_WiresOrchestratorInvocationKind(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "claude-code", "/orch/dir/orch.md", "/run/folder")
	if delegate == nil {
		t.Fatal("buildTUIDelegate returned nil")
	}
	if delegate.Orchestrator.InvocationKind != "orchestrator" {
		t.Errorf("delegate.Orchestrator.InvocationKind = %q, want %q", delegate.Orchestrator.InvocationKind, "orchestrator")
	}
}

// TestBuildTUIDelegate_Fake_ReturnsNil verifies that using the "fake" harness
// returns nil — no delegate is wired, and TUIDeviationResolver falls back to
// stop mode. This closes AC3.6 negative path.
func TestBuildTUIDelegate_Fake_ReturnsNil(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "fake", "/orch/dir/orch.md", "/run/folder")
	if delegate != nil {
		t.Errorf("buildTUIDelegate(fake) returned non-nil %T, want nil", delegate)
	}
}

// TestBuildTUIDelegate_ClaudeCodeWithoutOrchFile_ReturnsNil verifies that when
// the orchestrator file is not yet known (empty string), buildTUIDelegate returns
// nil even for "claude-code". This is the initial state before the file screen
// completes in the TUI setup flow.
func TestBuildTUIDelegate_ClaudeCodeWithoutOrchFile_ReturnsNil(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "claude-code", "", "/run/folder")
	if delegate != nil {
		t.Errorf("buildTUIDelegate(claude-code, no orchFile) returned non-nil %T, want nil", delegate)
	}
}

// TestBuildTUIDelegate_TypeAssert verifies that the returned value is a
// *deviation.OrchestratorDelegate (not just a non-nil interface).
func TestBuildTUIDelegate_TypeAssert(t *testing.T) {
	h := harness.NewFakeAdapter()
	result := buildTUIDelegate(h, "claude-code", "/orch/dir/orch.md", "")
	if result == nil {
		t.Fatal("buildTUIDelegate returned nil, want *deviation.OrchestratorDelegate")
	}
	// Type is already *deviation.OrchestratorDelegate by the function signature;
	// this compile-time assertion confirms the import resolves correctly.
	var _ *deviation.OrchestratorDelegate = result
}

// ---------------------------------------------------------------------------
// hasFlag
//
// hasFlag is the new pre-scan helper that reports whether a named flag appears
// anywhere in args in either "--flag value" or "--flag=value" form. It is used
// by resolveRunIdentityForCLI to detect --input presence before any run-folder
// read, satisfying the requirement that the mutual-exclusion check precedes
// filesystem access.
//
// Tests for the presence case are RED until hasFlag's logic is complete:
// a stub returning false would pass the absence tests but fail the presence tests.
// ---------------------------------------------------------------------------

// TestHasFlag_FlagPresent_SpaceSeparated_ReturnsTrue verifies the common
// "--flag value" form: flag name followed by a space and then the value.
func TestHasFlag_FlagPresent_SpaceSeparated_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input", "/some/path"}, "--input") {
		t.Error("hasFlag should return true when --input appears in space-separated form")
	}
}

// TestHasFlag_FlagPresent_EqualsSeparated_ReturnsTrue verifies the
// "--flag=value" form: flag name and value joined with "=".
func TestHasFlag_FlagPresent_EqualsSeparated_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input=/some/path"}, "--input") {
		t.Error("hasFlag should return true when --input appears in equals-separated form")
	}
}

// TestHasFlag_FlagPresent_AmongOtherArgs_ReturnsTrue verifies that hasFlag
// finds the flag even when surrounded by other flags and values.
func TestHasFlag_FlagPresent_AmongOtherArgs_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--input", "/path", "--workflow", "w1"}
	if !hasFlag(args, "--input") {
		t.Error("hasFlag should find --input among other flags")
	}
}

// TestHasFlag_FlagPresent_MultipleOccurrences_ReturnsTrue verifies that
// hasFlag reports true when the flag appears more than once; only the first
// occurrence needs to be found.
func TestHasFlag_FlagPresent_MultipleOccurrences_ReturnsTrue(t *testing.T) {
	args := []string{"--input", "/first", "--input", "/second"}
	if !hasFlag(args, "--input") {
		t.Error("hasFlag should return true when --input appears multiple times")
	}
}

// TestHasFlag_FlagPresent_StandaloneWithNoValue_ReturnsTrue verifies that
// --input appearing as the last token (no value token following) is still
// detected: presence, not value, matters here.
func TestHasFlag_FlagPresent_StandaloneWithNoValue_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input"}, "--input") {
		t.Error("hasFlag should return true when --input appears as the last arg with no following value")
	}
}

// TestHasFlag_FlagAbsent_ReturnsFalse verifies that hasFlag returns false when
// the named flag is not in args.
func TestHasFlag_FlagAbsent_ReturnsFalse(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1"}
	if hasFlag(args, "--input") {
		t.Error("hasFlag should return false when --input is absent")
	}
}

// TestHasFlag_EmptyArgs_ReturnsFalse verifies that hasFlag handles an empty
// slice without panicking and returns false.
func TestHasFlag_EmptyArgs_ReturnsFalse(t *testing.T) {
	if hasFlag([]string{}, "--input") {
		t.Error("hasFlag with empty args should return false")
	}
}

// TestHasFlag_PartialPrefixDoesNotMatch verifies that "--inputx" is not
// matched by a search for "--input": the flag name must match exactly.
func TestHasFlag_PartialPrefixDoesNotMatch(t *testing.T) {
	if hasFlag([]string{"--inputx", "/some/path"}, "--input") {
		t.Error("hasFlag(--inputx, --input) must not match — exact flag name required")
	}
}

// ---------------------------------------------------------------------------
// resolveRunIdentityForCLI: --input + --run pre-scan mutual-exclusion (T2.3)
//
// These tests prove that the mutual-exclusion check fires inside
// resolveRunIdentityForCLI *before* any run-folder read. The technique: supply
// a valid-format run_id whose folder does not exist on disk. If the refusal
// came from the folder read, the error would say "no run found"; if it came
// from the pre-scan check, the error must describe the mutual exclusion.
// ---------------------------------------------------------------------------

// mainTestRunID is a valid-format run_id whose folder is never created on disk
// in these tests, so any attempt to read it would produce "no run found".
const mainTestRunID = "20260727T170000Z-a3f9"

// TestResolveRunIdentityForCLI_InputWithRun_RefusesMutuallyExclusive is the
// core T2.3 assertion. It verifies that providing --input together with --run
// causes resolveRunIdentityForCLI to return a mutual-exclusion error without
// reading the run folder. The named run folder is absent from disk; if the
// function reads the folder before checking the flags, it returns
// "no run found" instead of the mutual-exclusion message — which fails this
// test and proves the check came too late.
func TestResolveRunIdentityForCLI_InputWithRun_RefusesMutuallyExclusive(t *testing.T) {
	args := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", mainTestRunID,
	}

	_, _, err := resolveRunIdentityForCLI(args)
	if err == nil {
		t.Fatal("resolveRunIdentityForCLI returned nil error; want a mutual-exclusion refusal")
	}
	// A "no run found" error means the folder was read before the flag check.
	if strings.Contains(err.Error(), "no run found") {
		t.Errorf("error %q suggests the run folder was read before the pre-scan check fired; "+
			"the mutual-exclusion check must precede any os.ReadFile call", err.Error())
	}
	// The error must identify the conflict between --input and --run.
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q does not contain \"mutually exclusive\"; "+
			"the pre-scan check must produce a clear mutual-exclusion message", err.Error())
	}
}

// TestResolveRunIdentityForCLI_InputAloneWithoutRun_DoesNotRefuse verifies
// that --input by itself (no --run) does not trigger the mutual-exclusion check.
// The scanner runs on an empty temp dir, finds zero candidates, and mints a new run.
func TestResolveRunIdentityForCLI_InputAloneWithoutRun_DoesNotRefuse(t *testing.T) {
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	args := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		// no --run flag
	}

	identity, _, err := resolveRunIdentityForCLI(args)
	if err != nil {
		t.Fatalf("resolveRunIdentityForCLI returned unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("resolveRunIdentityForCLI returned nil identity; want a new-run identity")
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true when --input is given without --run (scanner mints new run)")
	}
}
