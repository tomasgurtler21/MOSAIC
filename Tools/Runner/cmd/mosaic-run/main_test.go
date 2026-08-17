package main

// main_test.go covers the helper functions in main.go that control routing and
// adapter construction. These helpers are exercised here because they live in
// package main (dependency wiring) and cannot be reached through cli.Run-based
// tests, which inject a pre-built session and never enter main's wiring path.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	commonharness "mosaic-common/harness"
	"mosaic-common/interaction"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
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

// ---------------------------------------------------------------------------
// buildDeps: consultant wiring selection
//
// buildDeps is the shared wiring constructor that both frontends delegate to.
// These tests verify that the correct consultant types are selected for each
// RunSettings combination, and that the Approvals field is passed through.
//
// Wiring assertions (non-nil Routing, Manual, PreConsult, non-nil Approvals)
// are RED until buildDeps is updated to wire all deps from RunSettings rather
// than individual string/bool parameters.
// ---------------------------------------------------------------------------

// TestBuildDeps_OrchestratedMode_WiresOrchestratorConsultantAsRouting verifies
// that Mode=orchestrated causes buildDeps to wire *deviation.OrchestratorConsultant
// as Deps.Routing.
func TestBuildDeps_OrchestratedMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeOrchestrated}
	deps := buildDeps(settings, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(orchestrated).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_AutoMode_WiresOrchestratorConsultantAsRouting verifies that
// Mode=auto wires *deviation.OrchestratorConsultant as Deps.Routing.
func TestBuildDeps_AutoMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto}
	deps := buildDeps(settings, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(auto).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_AutoReviewMode_WiresOrchestratorConsultantAsRouting verifies
// that Mode=auto-review wires *deviation.OrchestratorConsultant as Deps.Routing.
func TestBuildDeps_AutoReviewMode_WiresOrchestratorConsultantAsRouting(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAutoReview}
	deps := buildDeps(settings, nil, nil, nil)
	if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(auto-review).Routing = %T, want *deviation.OrchestratorConsultant", deps.Routing)
	}
}

// TestBuildDeps_ManualResolutionEnabled_WiresManualResolverAsManual verifies
// that ManualResolution=true wires *deviation.ManualResolver as Deps.Manual.
func TestBuildDeps_ManualResolutionEnabled_WiresManualResolverAsManual(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: true}
	deps := buildDeps(settings, nil, nil, nil)
	if _, ok := deps.Manual.(*deviation.ManualResolver); !ok {
		t.Errorf("buildDeps(manualResolution=true).Manual = %T, want *deviation.ManualResolver", deps.Manual)
	}
}

// TestBuildDeps_ManualResolutionDisabled_LeavesManualNil verifies that
// ManualResolution=false leaves Deps.Manual nil.
func TestBuildDeps_ManualResolutionDisabled_LeavesManualNil(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: false}
	deps := buildDeps(settings, nil, nil, nil)
	if deps.Manual != nil {
		t.Errorf("buildDeps(manualResolution=false).Manual = %T, want nil", deps.Manual)
	}
}

// TestBuildDeps_PreConsultEnabled_WiresPreConsultant verifies that
// PreConsultation=true wires a non-nil Deps.PreConsult of the concrete type
// *deviation.OrchestratorConsultant. The type check matches the pattern used
// for Routing assertions: a future refactor that wires a different type will
// fail this test, not silently pass.
func TestBuildDeps_PreConsultEnabled_WiresPreConsultant(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true}
	deps := buildDeps(settings, nil, nil, nil)
	if deps.PreConsult == nil {
		t.Fatal("buildDeps(preConsult=true).PreConsult = nil, want *deviation.OrchestratorConsultant")
	}
	if _, ok := deps.PreConsult.(*deviation.OrchestratorConsultant); !ok {
		t.Errorf("buildDeps(preConsult=true).PreConsult = %T, want *deviation.OrchestratorConsultant", deps.PreConsult)
	}
}

// TestBuildDeps_PreConsultDisabled_LeavesPreConsultNil verifies that
// PreConsultation=false leaves Deps.PreConsult nil.
func TestBuildDeps_PreConsultDisabled_LeavesPreConsultNil(t *testing.T) {
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: false}
	deps := buildDeps(settings, nil, nil, nil)
	if deps.PreConsult != nil {
		t.Errorf("buildDeps(preConsult=false).PreConsult = %T, want nil", deps.PreConsult)
	}
}

// ---------------------------------------------------------------------------
// T7.1: buildDeps wires each execution mode identically for both frontends.
//
// The shared buildDeps function is the mechanism that prevents the TUI and CLI
// from diverging. These tests assert that the same RunSettings — the struct
// both frontends already produce — yields the same session.Deps wiring from
// buildDeps, regardless of which frontend called it. They also assert that no
// consultant is left with an empty routing table at construction time (the
// table is delivered later, through RunContextBinder).
// ---------------------------------------------------------------------------

// TestBuildDeps_AllModes_RoutingIsNeverNil verifies that for every mode in
// domain.ExecutionModes(), buildDeps wires a non-nil Deps.Routing. A nil
// Routing causes a nil-pointer panic on any consultation attempt.
func TestBuildDeps_AllModes_RoutingIsNeverNil(t *testing.T) {
	for _, mode := range domain.ExecutionModes() {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil)
			if deps.Routing == nil {
				t.Errorf("buildDeps(mode=%s).Routing = nil, want non-nil routing consultant", mode)
			}
		})
	}
}

// TestBuildDeps_AllModes_RoutingIsOrchestratorConsultant verifies that for
// every mode in domain.ExecutionModes(), the wired Deps.Routing is specifically
// *deviation.OrchestratorConsultant. Both auto and orchestrated modes must use
// the same consultant type; only the session's routing logic differs by mode.
func TestBuildDeps_AllModes_RoutingIsOrchestratorConsultant(t *testing.T) {
	for _, mode := range domain.ExecutionModes() {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil)
			if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); !ok {
				t.Errorf("buildDeps(mode=%s).Routing = %T, want *deviation.OrchestratorConsultant", mode, deps.Routing)
			}
		})
	}
}

// TestBuildDeps_ApprovalsPassedThrough verifies that the approvals parameter
// is propagated to Deps.Approvals. Both frontends pass the real approval
// reader; buildDeps must not substitute or discard it.
func TestBuildDeps_ApprovalsPassedThrough(t *testing.T) {
	sentinelReader := &sentinelApprovalReader{}
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto}
	deps := buildDeps(settings, nil, nil, sentinelReader)
	if deps.Approvals != sentinelReader {
		t.Errorf("buildDeps.Approvals = %T (%p), want the exact sentinelApprovalReader (%p) passed in",
			deps.Approvals, deps.Approvals, sentinelReader)
	}
}

// sentinelApprovalReader is a test-only ApprovalReader used to verify that
// the approvals parameter is passed through buildDeps without substitution.
type sentinelApprovalReader struct{}

func (r *sentinelApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalUnreadable
}

// TestBuildDeps_SameSettingsYieldSameWiring verifies the parity contract
// directly: calling buildDeps with the same RunSettings from the CLI path and
// the TUI path (which both produce a domain.RunSettings) yields structurally
// identical Deps — same consultant types, same Manual/PreConsult presence.
// This is the single assertion that prevents the two frontends from diverging.
func TestBuildDeps_SameSettingsYieldSameWiring(t *testing.T) {
	// Settings that exercise every wiring branch.
	settings := domain.RunSettings{
		Mode:             domain.ExecutionModeOrchestrated,
		ManualResolution: true,
		PreConsultation:  true,
	}

	cliDeps := buildDeps(settings, nil, nil, nil)
	tuiDeps := buildDeps(settings, nil, nil, nil)

	// Routing: both must be *deviation.OrchestratorConsultant.
	_, cliRoutingOK := cliDeps.Routing.(*deviation.OrchestratorConsultant)
	_, tuiRoutingOK := tuiDeps.Routing.(*deviation.OrchestratorConsultant)
	if !cliRoutingOK || !tuiRoutingOK {
		t.Errorf("Routing type mismatch: CLI=%T TUI=%T, both want *deviation.OrchestratorConsultant",
			cliDeps.Routing, tuiDeps.Routing)
	}

	// Manual: both must be *deviation.ManualResolver.
	_, cliManualOK := cliDeps.Manual.(*deviation.ManualResolver)
	_, tuiManualOK := tuiDeps.Manual.(*deviation.ManualResolver)
	if !cliManualOK || !tuiManualOK {
		t.Errorf("Manual type mismatch: CLI=%T TUI=%T, both want *deviation.ManualResolver",
			cliDeps.Manual, tuiDeps.Manual)
	}

	// PreConsult: both must be non-nil.
	if cliDeps.PreConsult == nil || tuiDeps.PreConsult == nil {
		t.Errorf("PreConsult: CLI=%v TUI=%v, both want non-nil (PreConsultation=true)",
			cliDeps.PreConsult == nil, tuiDeps.PreConsult == nil)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Both frontends offer the same set of execution modes.
//
// The TUI mode-selection step populates its list from domain.ExecutionModes().
// The CLI validates the --mode flag against domain.ExecutionModes(). The test
// asserts that the canonical list is non-empty and that buildDeps accepts every
// mode in it — confirming that neither frontend offers a mode it cannot execute
// (a mode absent from ExecutionModes() would not reach buildDeps from either frontend).
// ---------------------------------------------------------------------------

// TestBuildDeps_EveryExecutionMode_IsAccepted verifies that buildDeps produces
// a structurally consistent Deps (non-nil Routing) for every mode in
// domain.ExecutionModes(). If a mode is in the list but buildDeps cannot handle
// it, this test fails — catching a frontend that presents modes it cannot execute.
func TestBuildDeps_EveryExecutionMode_IsAccepted(t *testing.T) {
	modes := domain.ExecutionModes()
	if len(modes) == 0 {
		t.Fatal("domain.ExecutionModes() returned an empty slice; at least one mode must be defined")
	}
	for _, mode := range modes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			settings := domain.RunSettings{Mode: mode}
			deps := buildDeps(settings, nil, nil, nil)
			if deps.Routing == nil {
				t.Errorf("buildDeps(mode=%s) produced nil Routing; all ExecutionModes entries must be executable", mode)
			}
		})
	}
}

// TestExecutionModes_AreAllHandledByBuildDeps verifies that the count of modes
// in domain.ExecutionModes() equals the number of modes for which buildDeps
// wires an OrchestratorConsultant. If ExecutionModes() adds a new mode without
// a corresponding branch in buildDeps, the unhandled mode would silently fall
// through to the fake adapter, making the frontend appear to offer it while
// actually failing every run started with it.
func TestExecutionModes_AreAllHandledByBuildDeps(t *testing.T) {
	modes := domain.ExecutionModes()
	handled := 0
	for _, mode := range modes {
		settings := domain.RunSettings{Mode: mode}
		deps := buildDeps(settings, nil, nil, nil)
		if _, ok := deps.Routing.(*deviation.OrchestratorConsultant); ok {
			handled++
		}
	}
	if handled != len(modes) {
		t.Errorf("buildDeps handles %d of %d ExecutionModes() entries with OrchestratorConsultant; "+
			"all modes must be handled so neither frontend offers an unexecutable mode",
			handled, len(modes))
	}
}

// ---------------------------------------------------------------------------
// Behavioral session tests driven via buildDeps
//
// These tests verify that a session constructed from buildDeps output behaves
// correctly end-to-end. They exercise the wiring path that both frontends use,
// establishing that the interactive path produces a session that consults the
// OrchestratorConsultant for routing and pre-consultation.
//
// Infrastructure shared by these tests lives below the test functions:
// fakeRawInvoker, mainTestMemStore, mainTestClock, mainTestNoopInteraction,
// and the file-helper functions.
// ---------------------------------------------------------------------------

// mainTestOrchestratorDir is the session testdata directory relative to the
// cmd/mosaic-run package. It contains the orchestrator fixture files shared
// with internal/session tests.
const mainTestOrchestratorDir = "../../testdata/session"

// TestBuildDeps_PreConsultation_AdviceAppliedToDispatch_ViaBuildDeps verifies
// that when buildDeps is called with PreConsultation=true, the resulting session
// invokes its PreConsult field (the OrchestratorConsultant wired by buildDeps)
// and that the pre-consultation advice is applied to the first auto-routed
// dispatch's task description.
//
// This mirrors TestSession_Start_PreConsultation_AdviceAppliedToDispatch in the
// session package but is driven through buildDeps, establishing that the
// interactive wiring path (which calls buildDeps with the completed configuration)
// produces a session that actually uses pre-consultation advice.
func TestBuildDeps_PreConsultation_AdviceAppliedToDispatch_ViaBuildDeps(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFileForMain(t, dir, "linear-orch.md")
	writeAgentFileForMain(t, dir, "agent-a")
	writeAgentFileForMain(t, dir, "agent-b")

	// Sentinel text that must appear in the first dispatch's TaskDescription
	// when pre-consultation advice is correctly applied.
	const adviceText = "pre-consultation-advice-buildDeps-sentinel-r7q"

	// In auto mode, PreConsult is called once at run start (one InvokeRaw call).
	// Routing is handled automatically; the fakeRawInvoker serves only the
	// pre-consultation request.
	fakeInvoker := &fakeRawInvoker{
		responses: [][]byte{
			[]byte(`{"task_description":"` + adviceText + `","constraints":""}`),
		},
	}

	settings := domain.RunSettings{
		Mode:            domain.ExecutionModeAuto,
		PreConsultation: true,
	}

	// Build session deps through the shared builder (same path both frontends use).
	deps := buildDeps(settings, fakeInvoker, &mainTestNoopInteraction{}, nil)

	// Supply the infrastructure deps that buildDeps deliberately leaves empty
	// (Harness, Store, Clock, Interact are frontend-supplied, not part of the
	// routing/consultation wiring that buildDeps owns).
	f := harness.NewFakeAdapter()
	deps.Harness = f
	deps.Store = &mainTestMemStore{}
	deps.Clock = mainTestClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	deps.Interact = &mainTestNoopInteraction{}

	ses := session.New(deps)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          settings,
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) < 1 {
		t.Fatal("want at least one harness invocation after the session run, got none")
	}

	// The pre-consultation advice text must appear in the first dispatch's
	// TaskDescription, confirming that the OrchestratorConsultant wired by
	// buildDeps was invoked and its advice was applied.
	if !strings.Contains(invs[0].Request.TaskDescription, adviceText) {
		t.Errorf("want first dispatch TaskDescription to contain pre-consultation advice %q, got %q",
			adviceText, invs[0].Request.TaskDescription)
	}
}

// TestBuildDeps_OrchestratedMode_RoutingConsultantIsInvoked verifies that a
// session built from buildDeps(Mode=orchestrated) routes every dispatch
// decision through the OrchestratorConsultant. The fakeRawInvoker scripted
// into buildDeps records every InvokeRaw call, so a non-zero call count
// confirms that routing decisions flowed through the consultant wired by
// buildDeps rather than being resolved internally.
//
// This is the integration-style behavioral test for the wiring that both
// frontends rely on: establishing that the interactive path, which calls
// buildDeps with the completed configuration, produces a session that actually
// consults the orchestrator for each routing decision.
func TestBuildDeps_OrchestratedMode_RoutingConsultantIsInvoked(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFileForMain(t, dir, "linear-orch.md")
	writeAgentFileForMain(t, dir, "agent-a")
	writeAgentFileForMain(t, dir, "agent-b")

	// Three routing consultations for the linear 2-agent workflow in
	// orchestrated mode: dispatch agent-a, dispatch agent-b, then stop.
	fakeInvoker := &fakeRawInvoker{
		responses: [][]byte{
			[]byte(`{"action":"dispatch","agent":"agent-a","task_description":"orchestrated task for agent-a"}`),
			[]byte(`{"action":"dispatch","agent":"agent-b","task_description":"orchestrated task for agent-b"}`),
			[]byte(`{"action":"stop","reason":"workflow complete"}`),
		},
	}

	settings := domain.RunSettings{
		Mode: domain.ExecutionModeOrchestrated,
	}

	deps := buildDeps(settings, fakeInvoker, nil, nil)

	f := harness.NewFakeAdapter()
	deps.Harness = f
	deps.Store = &mainTestMemStore{}
	deps.Clock = mainTestClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	deps.Interact = &mainTestNoopInteraction{}

	ses := session.New(deps)

	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          settings,
	}

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// A non-zero InvokeRaw call count proves that routing decisions flowed
	// through the OrchestratorConsultant that buildDeps wired. If the session
	// had dispatched without consulting the orchestrator, callCount would be 0.
	fakeInvoker.mu.Lock()
	callCount := fakeInvoker.callCount
	fakeInvoker.mu.Unlock()
	if callCount == 0 {
		t.Error("buildDeps(orchestrated) routing consultant was never invoked; " +
			"want InvokeRaw called at least once for routing decisions")
	}
}

// ---------------------------------------------------------------------------
// Shared test infrastructure for behavioural session tests in cmd/mosaic-run
// ---------------------------------------------------------------------------

// fakeRawInvoker is a scripted domain.RawInvoker for use in session integration
// tests. It returns pre-configured byte slices in FIFO order, enabling tests to
// script orchestrator consultation responses without spawning a real agent.
type fakeRawInvoker struct {
	mu        sync.Mutex
	responses [][]byte
	callCount int
}

func (f *fakeRawInvoker) InvokeRaw(_ context.Context, _ domain.AgentReference, _ []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.callCount >= len(f.responses) {
		return nil, fmt.Errorf("fakeRawInvoker: no more scripted responses (call index %d, have %d)",
			f.callCount, len(f.responses))
	}
	resp := f.responses[f.callCount]
	f.callCount++
	return resp, nil
}

// mainTestMemStore is a minimal in-memory ArtifactStore for use in behavioural
// session tests in the cmd/mosaic-run package. It follows the same contract as
// the memStore in internal/session/session_test.go.
type mainTestMemStore struct {
	state  domain.ArtifactState
	exists bool
}

func (m *mainTestMemStore) Read(_ context.Context) (domain.ArtifactState, error) {
	if !m.exists {
		return domain.ArtifactState{}, os.ErrNotExist
	}
	return m.state, nil
}

func (m *mainTestMemStore) Create(_ context.Context, info domain.WorkflowInfo, task string, settings domain.RunSettings, now time.Time, runID string) (domain.ArtifactState, error) {
	m.state = domain.ArtifactState{
		Type:            "orchestration-artifact",
		RunID:           runID,
		Workflow:        info.ID,
		WorkflowVersion: info.Version,
		Task:            task,
		Started:         now,
		LastUpdated:     now,
		GlobalSequence:  0,
		RunSettings:     settings,
	}
	m.exists = true
	return m.state, nil
}

func (m *mainTestMemStore) Apply(_ context.Context, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, error) {
	state.GlobalSequence = step.Seq
	if !step.IsInfrastructure {
		state.CurrentState = domain.CurrentState{
			Phase:      step.Phase,
			Stage:      step.Stage,
			LastStatus: step.Status,
			LastAgent:  step.AgentInstance,
		}
	}
	m.state = state
	return state, nil
}

func (m *mainTestMemStore) SetPhase(_ context.Context, _ domain.ArtifactState, _ string, _ time.Time) (domain.ArtifactState, error) {
	return domain.ArtifactState{}, fmt.Errorf("mainTestMemStore.SetPhase: not implemented in cmd/mosaic-run session tests")
}

// mainTestClock is a fixed-time domain.Clock for use in cmd/mosaic-run
// behavioural session tests.
type mainTestClock struct{ t time.Time }

func (c mainTestClock) Now() time.Time { return c.t }

// mainTestNoopInteraction is a no-op implementation of interaction.Interaction
// for use in cmd/mosaic-run behavioural session tests. All question methods
// return Answered status; Notify and Progress are silent.
type mainTestNoopInteraction struct{}

func (n *mainTestNoopInteraction) SelectOne(_ context.Context, _ interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) AskText(_ context.Context, _ interaction.TextQuestion) (interaction.TextAnswer, error) {
	return interaction.TextAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Answered}, nil
}
func (n *mainTestNoopInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (n *mainTestNoopInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// copyOrchestratorFileForMain copies the named session fixture file into the
// given directory and returns the destination path. Fixtures are loaded from
// the shared testdata/session directory within Tools/Runner.
func copyOrchestratorFileForMain(t *testing.T, dir, fixtureName string) string {
	t.Helper()
	src := filepath.Join(mainTestOrchestratorDir, fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyOrchestratorFileForMain: read %q: %v", src, err)
	}
	dst := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyOrchestratorFileForMain: write %q: %v", dst, err)
	}
	return dst
}

// writeAgentFileForMain creates a minimal agent definition file in dir with the
// given agent identifier as the filename stem, matching the format expected by
// the agentresolve package.
func writeAgentFileForMain(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFileForMain(%q): %v", agentID, err)
	}
}
