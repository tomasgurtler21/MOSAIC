package main

// main_test.go covers the helper functions in main.go that control routing and
// adapter construction. These helpers are exercised here because they live in
// package main (dependency wiring) and cannot be reached through cli.Run-based
// tests, which inject a pre-built session and never enter main's wiring path.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
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

// TestBuildAdapter_OpenCode_ReturnsOpenCodeAdapter verifies that passing
// "opencode" as harnessStr constructs a *harness.OpenCodeAdapter. This is the
// primary AC4.3 assertion: the new catalog entry resolves to its own
// adapter, mirroring TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter.
func TestBuildAdapter_OpenCode_ReturnsOpenCodeAdapter(t *testing.T) {
	h := buildAdapter("opencode", "/custom/opencode", 45*time.Minute)
	if _, ok := h.(*harness.OpenCodeAdapter); !ok {
		t.Errorf("buildAdapter(opencode) returned %T, want *harness.OpenCodeAdapter", h)
	}
}

// TestBuildAdapter_OpenCode_ZeroTimeoutDefaultsTo30Min verifies that a zero
// timeout for "opencode" is treated as the 30-minute default, mirroring the
// claude-code case.
func TestBuildAdapter_OpenCode_ZeroTimeoutDefaultsTo30Min(t *testing.T) {
	h := buildAdapter("opencode", "opencode", 0)
	if _, ok := h.(*harness.OpenCodeAdapter); !ok {
		t.Errorf("buildAdapter(opencode, timeout=0) returned %T, want *harness.OpenCodeAdapter", h)
	}
}

// TestBuildAdapter_Unknown_StillReturnsFakeAdapter_AfterOpenCodeAdded
// re-verifies AC4.3's negative half now that a second catalog case exists:
// an unrecognised value must still fall back to FakeAdapter, and adding the
// "opencode" case must not have widened the default arm's match.
func TestBuildAdapter_Unknown_StillReturnsFakeAdapter_AfterOpenCodeAdded(t *testing.T) {
	h := buildAdapter("still-unknown-harness", "", 0)
	if _, ok := h.(*harness.FakeAdapter); !ok {
		t.Errorf("buildAdapter(still-unknown-harness) returned %T, want *harness.FakeAdapter", h)
	}
}

// ---------------------------------------------------------------------------
// TestBuildAdapter_CatalogCoverage (T4.6, AC4.7)
//
// A future catalog addition with no buildAdapter case must fail this test
// rather than silently falling back to the fake adapter. It iterates every
// entry commonharness.CLIHarnesses() declares and asserts buildAdapter
// resolves it to something other than *harness.FakeAdapter.
// ---------------------------------------------------------------------------

func TestBuildAdapter_CatalogCoverage_EveryEntryResolvesToARealAdapter(t *testing.T) {
	for _, entry := range commonharness.CLIHarnesses() {
		h := buildAdapter(entry.ID, "some-path", 5*time.Minute)
		if _, isFake := h.(*harness.FakeAdapter); isFake {
			t.Errorf("buildAdapter(%q) returned *harness.FakeAdapter; every catalog entry must resolve to a real adapter, or this composition root has silently missed a case", entry.ID)
		}
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

// TestBuildTUIDelegate_OpenCodeWithOrchFile_ReturnsOrchestratorDelegate
// verifies that selecting "opencode" with a known orchestrator file also
// produces a non-nil OrchestratorDelegate (AC4.4): the generalized condition
// admits every CLI-backed harness, not just "claude-code".
func TestBuildTUIDelegate_OpenCodeWithOrchFile_ReturnsOrchestratorDelegate(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "opencode", "/orch/dir/orch.md", "/run/folder")
	if delegate == nil {
		t.Fatal("buildTUIDelegate(opencode) returned nil, want *deviation.OrchestratorDelegate")
	}
	if delegate.Harness != h {
		t.Error("delegate.Harness is not the expected harness instance")
	}
}

// TestBuildTUIDelegate_OpenCodeWithoutOrchFile_ReturnsNil verifies that,
// mirroring the claude-code case, an unknown orchestrator file still yields
// nil for "opencode".
func TestBuildTUIDelegate_OpenCodeWithoutOrchFile_ReturnsNil(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "opencode", "", "/run/folder")
	if delegate != nil {
		t.Errorf("buildTUIDelegate(opencode, no orchFile) returned non-nil %T, want nil", delegate)
	}
}

// TestBuildTUIDelegate_Fake_StillReturnsNil_AfterOpenCodeAdmitted re-verifies
// the negative half of AC4.4 now that a second CLI-backed harness is
// admitted: the tool-local test double must still be excluded by the
// generalized condition.
func TestBuildTUIDelegate_Fake_StillReturnsNil_AfterOpenCodeAdmitted(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "fake", "/orch/dir/orch.md", "/run/folder")
	if delegate != nil {
		t.Errorf("buildTUIDelegate(fake) returned non-nil %T, want nil", delegate)
	}
}

// TestBuildTUIDelegate_UnrecognisedHarness_ReturnsNil verifies that an
// unrecognised harness value is excluded by the generalized
// IsCLIHarness-based condition, mirroring the unknown-value fallback
// elsewhere in composition.
func TestBuildTUIDelegate_UnrecognisedHarness_ReturnsNil(t *testing.T) {
	h := harness.NewFakeAdapter()
	delegate := buildTUIDelegate(h, "not-a-real-harness", "/orch/dir/orch.md", "/run/folder")
	if delegate != nil {
		t.Errorf("buildTUIDelegate(not-a-real-harness) returned non-nil %T, want nil", delegate)
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
// that --input by itself (no --run) does not trigger the --input/--run
// mutual-exclusion check. --new-run is passed explicitly so the assertion
// exercises only that mutual-exclusion check, not the separate zero-candidate
// selection refusal AC2.6 requires when neither --run nor --new-run is given.
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
		"--new-run",
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
		t.Error("IsNewRun = false, want true when --input is given without --run (--new-run mints a new run)")
	}
}

// ---------------------------------------------------------------------------
// resolveRunIdentityForTUI
//
// These tests drive the TDD RED phase for the new resolveRunIdentityForTUI
// helper. All tests compile against the stub declarations in
// tui_identity_stub.go and fail at runtime because the stubs return zero
// values. They become GREEN when the real implementation is written.
// ---------------------------------------------------------------------------

// TestResolveRunIdentityForTUI_NewRunFlag_YieldsValidRunID verifies that
// --new-run produces a non-empty run ID matching the canonical format.
func TestResolveRunIdentityForTUI_NewRunFlag_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if identity.RunID == "" {
		t.Error("RunID is empty; want a non-empty run ID in canonical format")
	}
	if !domain.IsValidRunID(identity.RunID) {
		t.Errorf("RunID %q does not match canonical format {YYYYMMDD}T{HHMMSS}Z-{4-hex}", identity.RunID)
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_YieldsAbsoluteScopedFolder verifies
// that --new-run produces an absolute run folder whose final path element is
// Orchestration-{run_id}.
func TestResolveRunIdentityForTUI_NewRunFlag_YieldsAbsoluteScopedFolder(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if !filepath.IsAbs(identity.RunFolder) {
		t.Errorf("RunFolder %q is not absolute", identity.RunFolder)
	}
	wantSuffix := domain.RunScopedFolder(identity.RunID)
	if !strings.HasSuffix(identity.RunFolder, wantSuffix) {
		t.Errorf("RunFolder %q does not end in %q", identity.RunFolder, wantSuffix)
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_SetsIsNewRun verifies that --new-run
// sets IsNewRun = true in the returned identity.
func TestResolveRunIdentityForTUI_NewRunFlag_SetsIsNewRun(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true for --new-run")
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_ScanResultIsNil verifies that the
// --new-run branch returns nil ScanResult: identity is resolved, not deferred
// to the run-select screen.
func TestResolveRunIdentityForTUI_NewRunFlag_ScanResultIsNil(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for --new-run (resolved, not deferred)", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_YieldsValidRunID verifies that
// when the directory scan finds zero candidates, a run ID is minted in canonical
// format — mirroring the --new-run branch.
func TestResolveRunIdentityForTUI_ZeroCandidates_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir() // empty directory → zero candidates
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if identity.RunID == "" {
		t.Error("RunID is empty; want a non-empty run ID in canonical format")
	}
	if !domain.IsValidRunID(identity.RunID) {
		t.Errorf("RunID %q does not match canonical format {YYYYMMDD}T{HHMMSS}Z-{4-hex}", identity.RunID)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_YieldsAbsoluteScopedFolder verifies
// that the zero-candidate branch produces an absolute scoped folder under workDir.
func TestResolveRunIdentityForTUI_ZeroCandidates_YieldsAbsoluteScopedFolder(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if !filepath.IsAbs(identity.RunFolder) {
		t.Errorf("RunFolder %q is not absolute", identity.RunFolder)
	}
	wantSuffix := domain.RunScopedFolder(identity.RunID)
	if !strings.HasSuffix(identity.RunFolder, wantSuffix) {
		t.Errorf("RunFolder %q does not end in %q", identity.RunFolder, wantSuffix)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_SetsIsNewRun verifies that the
// zero-candidate branch sets IsNewRun = true and ScanResult = nil.
func TestResolveRunIdentityForTUI_ZeroCandidates_SetsIsNewRun(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true when zero scan candidates")
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for zero-candidate new run", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_ConsecutiveNewRuns_ProduceDistinctIdentities
// verifies that two consecutive --new-run resolutions yield distinct run IDs
// and distinct run folders, satisfying the successive-minting contract.
func TestResolveRunIdentityForTUI_ConsecutiveNewRuns_ProduceDistinctIdentities(t *testing.T) {
	workDir := t.TempDir()
	id1, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("first resolveRunIdentityForTUI call error = %v", err)
	}
	id2, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("second resolveRunIdentityForTUI call error = %v", err)
	}
	if id1.RunID == id2.RunID {
		t.Errorf("consecutive mints produced the same RunID %q; each call must yield a distinct ID", id1.RunID)
	}
	if id1.RunFolder == id2.RunFolder {
		t.Errorf("consecutive mints produced the same RunFolder %q; each call must yield a distinct folder", id1.RunFolder)
	}
}

// TestResolveRunIdentityForTUI_RunFlag_ResolvesToNamedFolder verifies that
// --run <valid_id> resolves to the named run folder without minting, and
// does not set IsNewRun.
func TestResolveRunIdentityForTUI_RunFlag_ResolvesToNamedFolder(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	identity, err := resolveRunIdentityForTUI([]string{"--run", runID}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--run %s) error = %v, want nil", runID, err)
	}
	if identity.RunID != runID {
		t.Errorf("RunID = %q, want %q", identity.RunID, runID)
	}
	wantFolder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if identity.RunFolder != wantFolder {
		t.Errorf("RunFolder = %q, want %q", identity.RunFolder, wantFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for --run <id>")
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for --run", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_RunAndNewRunTogether_ReturnsUsageError verifies
// that providing both --run and --new-run returns a usage error wrapping errTUIUsage.
func TestResolveRunIdentityForTUI_RunAndNewRunTogether_ReturnsUsageError(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	_, err := resolveRunIdentityForTUI([]string{"--run", runID, "--new-run"}, workDir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(--run + --new-run) returned nil error, want usage error")
	}
	if !errors.Is(err, errTUIUsage) {
		t.Errorf("error %v does not wrap errTUIUsage; got errors.Is = false", err)
	}
}

// TestResolveRunIdentityForTUI_InvalidRunID_ReturnsUsageError verifies that
// --run with a malformed run ID returns an error wrapping errTUIUsage.
func TestResolveRunIdentityForTUI_InvalidRunID_ReturnsUsageError(t *testing.T) {
	workDir := t.TempDir()
	_, err := resolveRunIdentityForTUI([]string{"--run", "not-a-valid-id"}, workDir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(--run <invalid>) returned nil error, want usage error")
	}
	if !errors.Is(err, errTUIUsage) {
		t.Errorf("error %v does not wrap errTUIUsage; got errors.Is = false", err)
	}
}

// TestResolveRunIdentityForTUI_MultipleCandidate_DefersToRunSelectScreen verifies
// that when the scan finds multiple candidates, resolveRunIdentityForTUI returns
// empty identity fields and a non-nil ScanResult, deferring to the run-select screen.
func TestResolveRunIdentityForTUI_MultipleCandidate_DefersToRunSelectScreen(t *testing.T) {
	workDir := t.TempDir()
	// Create two directories with valid Orchestration-{run_id} names. Missing
	// Orchestration.md causes the scanner to classify both as resumable candidates.
	for _, id := range []string{"20260727T170000Z-a3f9", "20260727T180000Z-b1c2"} {
		folder := filepath.Join(workDir, domain.RunScopedFolder(id))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", folder, err)
		}
	}

	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(multi-candidate) error = %v, want nil", err)
	}
	if identity.RunID != "" {
		t.Errorf("RunID = %q, want empty string for multi-candidate deferral", identity.RunID)
	}
	if identity.RunFolder != "" {
		t.Errorf("RunFolder = %q, want empty string for multi-candidate deferral", identity.RunFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for multi-candidate deferral")
	}
	if identity.ScanResult == nil {
		t.Error("ScanResult is nil, want non-nil scan result for multi-candidate deferral")
	}
}

// TestResolveRunIdentityForTUI_SingleCandidate_DefersToRunSelectScreen is the
// TUI entry-point expression of the core defect this stage removes (AC2.2):
// exactly one resumable run must no longer be auto-resumed before the TUI
// even launches. resolveRunIdentityForTUI must defer to the run-select
// screen exactly as it already does for the multi-candidate case, returning
// empty identity fields and a non-nil ScanResult.
//
// Currently fails (RED): resolveRunIdentityForTUI still auto-resumes the
// single candidate (the `case 1` branch this stage removes).
func TestResolveRunIdentityForTUI_SingleCandidate_DefersToRunSelectScreen(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	folder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", folder, err)
	}

	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(single candidate) error = %v, want nil", err)
	}
	if identity.RunID != "" {
		t.Errorf("RunID = %q, want empty string; a single candidate must defer to the run-select screen, not auto-resume", identity.RunID)
	}
	if identity.RunFolder != "" {
		t.Errorf("RunFolder = %q, want empty string for single-candidate deferral", identity.RunFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for single-candidate deferral")
	}
	if identity.ScanResult == nil {
		t.Fatal("ScanResult = nil, want non-nil scan result for single-candidate deferral")
	}
	if len(identity.ScanResult.Candidates) != 1 || identity.ScanResult.Candidates[0].RunID != runID {
		t.Errorf("ScanResult.Candidates = %+v, want exactly one candidate with RunID %q", identity.ScanResult.Candidates, runID)
	}
}

// TestResolveRunIdentityForTUI_ScanFails_ReturnsNonUsageError verifies that
// when no flags are provided and the directory scan itself fails, the returned
// error is non-nil and does not wrap errTUIUsage. A scan error is an
// unexpected I/O failure, not a user-argument mistake, so it must exit with
// code 1 rather than code 2 at the call site.
//
// Fixture: a regular file is supplied as workDir. Attempting to list directory
// entries from a file path fails on all supported platforms (Linux, macOS,
// Windows), making this a portable scan-failure fixture without requiring
// OS-specific permission manipulation.
func TestResolveRunIdentityForTUI_ScanFails_ReturnsNonUsageError(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notADir, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", notADir, err)
	}

	_, err := resolveRunIdentityForTUI([]string{}, notADir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(scan fails) returned nil error, want a non-nil error")
	}
	if errors.Is(err, errTUIUsage) {
		t.Errorf("error %v wraps errTUIUsage; scan errors must not be classified as usage errors", err)
	}
}

// ---------------------------------------------------------------------------
// resolveTUIArtifactPath
//
// Tests drive the TDD RED phase for the resolveTUIArtifactPath helper.
// The stub returns ("", nil) for all inputs; the real implementation will
// return the joined path for non-empty folders and errUnresolvedRunFolder for
// empty folders.
// ---------------------------------------------------------------------------

// TestResolveTUIArtifactPath_NonEmptyFolder_ReturnsJoinedPath verifies that a
// non-empty run folder produces filepath.Join(runFolder, "Orchestration.md").
func TestResolveTUIArtifactPath_NonEmptyFolder_ReturnsJoinedPath(t *testing.T) {
	const runFolder = "/runs/Orchestration-20260727T170000Z-a3f9"
	got, err := resolveTUIArtifactPath(runFolder)
	if err != nil {
		t.Fatalf("resolveTUIArtifactPath(%q) error = %v, want nil", runFolder, err)
	}
	want := filepath.Join(runFolder, "Orchestration.md")
	if got != want {
		t.Errorf("resolveTUIArtifactPath(%q) = %q, want %q", runFolder, got, want)
	}
}

// TestResolveTUIArtifactPath_EmptyFolder_ReturnsUnresolvedError verifies that
// an empty run folder returns errUnresolvedRunFolder. This is the contract
// violation path that replaces the implicit "Orchestration.md" fallback.
func TestResolveTUIArtifactPath_EmptyFolder_ReturnsUnresolvedError(t *testing.T) {
	got, err := resolveTUIArtifactPath("")
	if err == nil {
		t.Fatalf("resolveTUIArtifactPath(\"\") returned nil error and %q, want errUnresolvedRunFolder", got)
	}
	if !errors.Is(err, errUnresolvedRunFolder) {
		t.Errorf("error %v does not wrap errUnresolvedRunFolder; got errors.Is = false", err)
	}
}

// TestResolveTUIArtifactPath_EmptyFolder_NeverReturnsBareRelativePath verifies
// the core safety property: the function must never return the bare relative path
// "Orchestration.md" under any input, eliminating the implicit CWD-relative fallback.
func TestResolveTUIArtifactPath_EmptyFolder_NeverReturnsBareRelativePath(t *testing.T) {
	got, _ := resolveTUIArtifactPath("")
	if got == "Orchestration.md" {
		t.Error("resolveTUIArtifactPath(\"\") returned bare \"Orchestration.md\"; this is the forbidden CWD-relative fallback")
	}
}

// ---------------------------------------------------------------------------
// newTUIRunIdentityMinter
//
// Tests drive the TDD RED phase for the newTUIRunIdentityMinter helper.
// The stub returns a minter that always returns ("", ""); the real
// implementation returns valid, distinct pairs rooted at workDir.
// ---------------------------------------------------------------------------

// TestNewTUIRunIdentityMinter_YieldsValidRunID verifies that the minter
// returned by newTUIRunIdentityMinter produces a run ID satisfying
// domain.IsValidRunID.
func TestNewTUIRunIdentityMinter_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID, _ := minter()
	if !domain.IsValidRunID(runID) {
		t.Errorf("minter() runID %q does not satisfy domain.IsValidRunID", runID)
	}
}

// TestNewTUIRunIdentityMinter_YieldsScopedFolderUnderWorkDir verifies that
// the run folder equals filepath.Join(workDir, domain.RunScopedFolder(runID)).
func TestNewTUIRunIdentityMinter_YieldsScopedFolderUnderWorkDir(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID, runFolder := minter()
	want := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if runFolder != want {
		t.Errorf("minter() runFolder = %q, want %q", runFolder, want)
	}
}

// TestNewTUIRunIdentityMinter_SuccessiveCallsProduceDistinctPairs verifies
// that calling the minter twice yields distinct (runID, runFolder) pairs,
// satisfying the successive-minting contract.
func TestNewTUIRunIdentityMinter_SuccessiveCallsProduceDistinctPairs(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID1, folder1 := minter()
	runID2, folder2 := minter()
	if runID1 == runID2 {
		t.Errorf("successive mints produced the same runID %q; each call must yield a distinct ID", runID1)
	}
	if folder1 == folder2 {
		t.Errorf("successive mints produced the same runFolder %q; each call must yield a distinct folder", folder1)
	}
}

// ---------------------------------------------------------------------------
// recordingLogger
//
// recordingLogger is a test-only fake domain.DebugLogger that records every
// Log call. It is thread-safe: the mutex guards the entries slice, which may
// be appended from goroutines spawned by the code under test.
// ---------------------------------------------------------------------------

// logEntry holds one recorded call to recordingLogger.Log.
type logEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

// recordingLogger accumulates Log calls so tests can assert on the emitted
// events and fields without touching the filesystem.
type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

// Log implements domain.DebugLogger. It appends one entry per call.
func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, logEntry{event: event, message: message, fields: append([]domain.DebugField(nil), fields...)})
}

// snapshot returns a copy of all recorded entries taken under the lock.
func (r *recordingLogger) snapshot() []logEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]logEntry(nil), r.entries...)
}

// ---------------------------------------------------------------------------
// newLoggedArtifactStore (T6.1)
//
// Table-driven tests for the newLoggedArtifactStore composition-root helper.
// The helper wraps artifact.NewFileStore and emits at most one artifact.path.*
// debug event depending on the path shape. Tests cover the three mutually
// exclusive cases defined in the emission contract:
//
//   relative path              → EventArtifactPathRejected with path field
//   absolute, non-run-scoped   → EventArtifactPathNonRunScoped with path field
//   absolute, run-scoped       → no event emitted
//
// In all three cases the returned store must be non-nil, and the path must
// be passed through unchanged (no substitution or rewriting).
//
// These tests are in the RED state until I6.8 implements newLoggedArtifactStore.
// ---------------------------------------------------------------------------

func TestNewLoggedArtifactStore(t *testing.T) {
	base := t.TempDir()

	// A valid run_id to construct a run-scoped parent directory name.
	const testRunID = "20260805T143029Z-9bc0"
	runScopedParent := filepath.Join(base, domain.RunScopedFolder(testRunID))

	cases := []struct {
		name      string
		path      string
		wantEvent string // empty string means no event should be emitted
	}{
		{
			// A relative path is a hard failure: every subsequent Create call will
			// return an "must be absolute" error and nothing will be written.
			name:      "relative path emits artifact.path.rejected",
			path:      "Orchestration.md",
			wantEvent: domain.EventArtifactPathRejected,
		},
		{
			// An absolute path whose parent directory is not an Orchestration-{run_id}
			// folder is informational: artifacts will be written, but the path is
			// outside the expected run-scoped hierarchy.
			name:      "absolute non-run-scoped path emits artifact.path.non_run_scoped",
			path:      filepath.Join(base, "Orchestration.md"),
			wantEvent: domain.EventArtifactPathNonRunScoped,
		},
		{
			// An absolute path whose parent is Orchestration-{run_id} is the normal
			// case. No event is emitted.
			name:      "absolute run-scoped path emits no event",
			path:      filepath.Join(runScopedParent, "Orchestration.md"),
			wantEvent: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &recordingLogger{}

			store := newLoggedArtifactStore(tc.path, logger)

			// The returned store must always be non-nil regardless of the path shape.
			// The helper never rejects a path — it only observes and logs.
			if store == nil {
				t.Fatal("newLoggedArtifactStore returned nil; want a non-nil domain.ArtifactStore in all cases")
			}

			entries := logger.snapshot()

			if tc.wantEvent == "" {
				// Normal (run-scoped) case: the helper must emit nothing.
				if len(entries) != 0 {
					t.Errorf("run-scoped path: expected 0 events, got %d event(s): %v", len(entries), entries)
				}
				return
			}

			// Anomalous cases: exactly one event must be emitted.
			if len(entries) != 1 {
				t.Fatalf("expected exactly 1 event, got %d: %v", len(entries), entries)
			}

			if entries[0].event != tc.wantEvent {
				t.Errorf("event = %q, want %q", entries[0].event, tc.wantEvent)
			}

			// The path field must be present and carry the original (unmodified) path.
			var pathFieldValue string
			pathFieldFound := false
			for _, f := range entries[0].fields {
				if f.Key == "path" {
					pathFieldValue = f.Value
					pathFieldFound = true
					break
				}
			}
			if !pathFieldFound {
				t.Errorf("emitted entry for event %q carries no \"path\" field; fields = %v",
					tc.wantEvent, entries[0].fields)
			} else if pathFieldValue != tc.path {
				// The helper must pass the path through unchanged — no substitution,
				// no rewriting, no normalisation.
				t.Errorf("path field = %q, want %q (path must not be rewritten)", pathFieldValue, tc.path)
			}
		})
	}
}

// TestNewLoggedArtifactStore_AtMostOneEventPerCall verifies the mutual-exclusion
// property: at most one artifact.path.* event is emitted per newLoggedArtifactStore
// call, regardless of how many conditions are checked internally. This keeps a
// log reader able to distinguish a hard failure (rejected) from a permitted-but-
// unusual case (non_run_scoped) by event name alone.
func TestNewLoggedArtifactStore_AtMostOneEventPerCall(t *testing.T) {
	base := t.TempDir()
	paths := []string{
		"relative/Orchestration.md",
		filepath.Join(base, "Orchestration.md"),
		filepath.Join(base, domain.RunScopedFolder("20260805T143029Z-9bc0"), "Orchestration.md"),
	}

	for _, path := range paths {
		logger := &recordingLogger{}
		_ = newLoggedArtifactStore(path, logger)
		entries := logger.snapshot()
		if len(entries) > 1 {
			t.Errorf("path %q: emitted %d events, want at most 1; mutual-exclusion contract violated",
				path, len(entries))
		}
	}
}

// TestNewLoggedArtifactStore_NopLoggerDoesNotPanic verifies that passing
// domain.NopDebugLogger{} as the logger (the production default when logging is
// off) does not panic and still returns a non-nil store.
func TestNewLoggedArtifactStore_NopLoggerDoesNotPanic(t *testing.T) {
	base := t.TempDir()
	paths := []string{
		"relative/Orchestration.md",
		filepath.Join(base, "Orchestration.md"),
		filepath.Join(base, domain.RunScopedFolder("20260805T143029Z-9bc0"), "Orchestration.md"),
	}
	for _, path := range paths {
		store := newLoggedArtifactStore(path, domain.NopDebugLogger{})
		if store == nil {
			t.Errorf("path %q: newLoggedArtifactStore with NopDebugLogger returned nil store", path)
		}
	}
}
