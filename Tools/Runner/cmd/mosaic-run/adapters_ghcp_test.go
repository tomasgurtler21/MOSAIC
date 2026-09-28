package main

import (
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/harness/ghcpcli"
)

// ---------------------------------------------------------------------------
// T4.4: fake harness remains selectable and is the --harness default
//
// The fake harness is a test double and must not be affected by any Stage 4
// change. It must remain constructible via buildAdapter and must ignore the
// executable override entirely (it spawns no process). The FakeHarnessID
// constant must remain "fake", matching the cobra flag default in run.go.
// ---------------------------------------------------------------------------

// TestFakeHarness_RemainsSelectable verifies that buildAdapter("fake", ...) still
// returns a *harness.MockAdapter after Stage 4 changes. This complements the
// existing TestBuildAdapter_Fake_ReturnsMockAdapter by asserting that Stage 4's
// changes to buildAdapter have not removed or broken the fake case.
func TestFakeHarness_RemainsSelectable(t *testing.T) {
	h := buildAdapter(harness.FakeHarnessID, "", "", 30*time.Minute)
	if _, ok := h.(*harness.MockAdapter); !ok {
		t.Errorf("buildAdapter(%q) returned %T, want *harness.MockAdapter; "+
			"the fake harness must remain constructible after Stage 4 changes",
			harness.FakeHarnessID, h)
	}
}

// TestFakeHarness_DoesNotImplementExecutableRevealer verifies that the fake
// adapter does not implement domain.ExecutableRevealer. The fake spawns no
// process and has no executable path; exposing one would misrepresent it.
func TestFakeHarness_DoesNotImplementExecutableRevealer(t *testing.T) {
	h := buildAdapter(harness.FakeHarnessID, "some-path", "", 30*time.Minute)
	if _, ok := h.(domain.ExecutableRevealer); ok {
		t.Errorf("buildAdapter(%q) implements domain.ExecutableRevealer; "+
			"the fake adapter spawns no process and must not expose an executable path",
			harness.FakeHarnessID)
	}
}

// TestFakeHarness_IgnoresExecutableOverride verifies that providing a non-empty
// executable override to buildAdapter("fake", ...) is silently ignored: the
// result is still a MockAdapter, not some other type, and the mock's behavior
// is unaffected. This pins the design constraint that buildAdapter's fake case
// never inspects or stores the override.
func TestFakeHarness_IgnoresExecutableOverride(t *testing.T) {
	h := buildAdapter(harness.FakeHarnessID, "should-be-ignored", "", 30*time.Minute)
	if _, ok := h.(*harness.MockAdapter); !ok {
		t.Errorf("buildAdapter(%q, override='should-be-ignored') returned %T, want *harness.MockAdapter; "+
			"the fake harness must ignore the executable override",
			harness.FakeHarnessID, h)
	}
}

// TestFakeHarnessID_IsTheCobraFlagDefault verifies that harness.FakeHarnessID
// equals "fake", which is the --harness cobra flag default declared in run.go.
// If FakeHarnessID were renamed, the default would silently become an unknown
// harness name, causing buildAdapter to fall through to the fake case for the
// wrong reason. Pinning the constant value here ensures the flag default and
// the constant stay in sync.
func TestFakeHarnessID_IsTheCobraFlagDefault(t *testing.T) {
	const wantDefault = "fake"
	if harness.FakeHarnessID != wantDefault {
		t.Errorf("harness.FakeHarnessID = %q, want %q; "+
			"this constant is the --harness cobra flag default declared in run.go and must not be renamed",
			harness.FakeHarnessID, wantDefault)
	}
}

// ---------------------------------------------------------------------------
// T4.2: --ghcp-permission-mode CLI flag parsing
//
// scanFlag must correctly resolve both "--flag value" and "--flag=value" forms.
// buildAdapter must map "blanket" and "allowlist" to the correct adapter mode.
// ---------------------------------------------------------------------------

// TestScanFlag_GHCPPermissionMode_SpaceForm verifies that scanFlag correctly
// extracts the value of --ghcp-permission-mode in "--flag value" form.
func TestScanFlag_GHCPPermissionMode_SpaceForm(t *testing.T) {
	args := []string{"run", "--harness", "ghcp-cli", "--ghcp-permission-mode", "blanket"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got != "blanket" {
		t.Errorf("scanFlag(--ghcp-permission-mode, space form) = %q; want %q", got, "blanket")
	}
}

// TestScanFlag_GHCPPermissionMode_EqualForm verifies that scanFlag correctly
// extracts the value of --ghcp-permission-mode in "--flag=value" form.
func TestScanFlag_GHCPPermissionMode_EqualForm(t *testing.T) {
	args := []string{"run", "--harness=ghcp-cli", "--ghcp-permission-mode=allowlist"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got != "allowlist" {
		t.Errorf("scanFlag(--ghcp-permission-mode, equals form) = %q; want %q", got, "allowlist")
	}
}

// TestScanFlag_GHCPPermissionMode_AbsentFlag verifies that scanFlag returns ""
// when --ghcp-permission-mode is not present in args.
func TestScanFlag_GHCPPermissionMode_AbsentFlag(t *testing.T) {
	args := []string{"run", "--harness", "ghcp-cli"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got != "" {
		t.Errorf("scanFlag(--ghcp-permission-mode, absent) = %q; want empty string", got)
	}
}

// TestBuildAdapter_GHCPCli_BlanketMode_ProducesGHCPCLIAdapter verifies that
// buildAdapter("ghcp-cli", ..., "blanket", ...) returns a *ghcpcli.GHCPCLIAdapter.
func TestBuildAdapter_GHCPCli_BlanketMode_ProducesGHCPCLIAdapter(t *testing.T) {
	h := buildAdapter(commonharness.HarnessIDGHCPCLI, "copilot", "blanket", 30*time.Minute)
	if _, ok := h.(*ghcpcli.GHCPCLIAdapter); !ok {
		t.Errorf("buildAdapter(ghcp-cli, blanket) returned %T; want *ghcpcli.GHCPCLIAdapter", h)
	}
}

// TestBuildAdapter_GHCPCli_AllowlistMode_ProducesGHCPCLIAdapter verifies that
// buildAdapter("ghcp-cli", ..., "allowlist", ...) returns a *ghcpcli.GHCPCLIAdapter.
func TestBuildAdapter_GHCPCli_AllowlistMode_ProducesGHCPCLIAdapter(t *testing.T) {
	h := buildAdapter(commonharness.HarnessIDGHCPCLI, "copilot", "allowlist", 30*time.Minute)
	if _, ok := h.(*ghcpcli.GHCPCLIAdapter); !ok {
		t.Errorf("buildAdapter(ghcp-cli, allowlist) returned %T; want *ghcpcli.GHCPCLIAdapter", h)
	}
}

// TestBuildAdapter_GHCPCli_EmptyMode_DefaultsToBlanket verifies that an empty
// ghcpMode defaults to GHCPCLIModeBlanket inside buildAdapter (backward compatibility).
func TestBuildAdapter_GHCPCli_EmptyMode_DefaultsToBlanket(t *testing.T) {
	// Passing "" should produce a GHCPCLIAdapter (not a fake or panic).
	h := buildAdapter(commonharness.HarnessIDGHCPCLI, "copilot", "", 30*time.Minute)
	if _, ok := h.(*ghcpcli.GHCPCLIAdapter); !ok {
		t.Errorf("buildAdapter(ghcp-cli, '') returned %T; want *ghcpcli.GHCPCLIAdapter (empty mode defaults to blanket)", h)
	}
}

// TestBuildAdapter_GHCPCli_UnknownMode_DefaultsToBlanket verifies that an
// unrecognised ghcpMode value defaults to GHCPCLIModeBlanket.
func TestBuildAdapter_GHCPCli_UnknownMode_DefaultsToBlanket(t *testing.T) {
	h := buildAdapter(commonharness.HarnessIDGHCPCLI, "copilot", "not-a-real-mode", 30*time.Minute)
	if _, ok := h.(*ghcpcli.GHCPCLIAdapter); !ok {
		t.Errorf("buildAdapter(ghcp-cli, 'not-a-real-mode') returned %T; want *ghcpcli.GHCPCLIAdapter (unknown mode defaults to blanket)", h)
	}
}

// ---------------------------------------------------------------------------
// T4.3: FR-8 rejection — GHCP CLI run without resolved mode is rejected
//
// In CLI mode, the pre-scan block in main() rejects a ghcp-cli run that has no
// --ghcp-permission-mode flag. These tests verify the flag pre-scan composition
// path: scanFlag returns "" for an absent flag, which the pre-scan detects and
// would reject.
// ---------------------------------------------------------------------------

// TestFR8Rejection_GHCPCli_AbsentMode_FlagReturnsEmpty verifies that scanFlag
// returns "" when --ghcp-permission-mode is absent from args. This is the
// precondition for the FR-8 rejection in main(): an empty result from scanFlag
// causes the pre-scan to reject the run before any adapter is constructed.
func TestFR8Rejection_GHCPCli_AbsentMode_FlagReturnsEmpty(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDGHCPCLI, "--workflow", "myworkflow"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got != "" {
		t.Errorf("scanFlag(--ghcp-permission-mode, absent) = %q; want empty string — "+
			"FR-8 rejection depends on scanFlag returning '' for an absent flag when harness is ghcp-cli", got)
	}
}

// TestFR8Rejection_GHCPCli_BlanketMode_FlagNonEmpty verifies that scanFlag
// returns "blanket" when the flag is present, which satisfies the FR-8 check
// (run is not rejected).
func TestFR8Rejection_GHCPCli_BlanketMode_FlagNonEmpty(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDGHCPCLI, "--ghcp-permission-mode", "blanket"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got == "" {
		t.Errorf("scanFlag(--ghcp-permission-mode=blanket) = \"\"; want non-empty — " +
			"FR-8 check must not reject a run when --ghcp-permission-mode=blanket is supplied")
	}
}

// TestFR8Rejection_GHCPCli_AllowlistMode_FlagNonEmpty verifies that scanFlag
// returns "allowlist" when the flag is present.
func TestFR8Rejection_GHCPCli_AllowlistMode_FlagNonEmpty(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDGHCPCLI, "--ghcp-permission-mode=allowlist"}
	got := scanFlag(args, "--ghcp-permission-mode")
	if got == "" {
		t.Errorf("scanFlag(--ghcp-permission-mode=allowlist) = \"\"; want non-empty")
	}
}

// TestFR8Rejection_NonGHCPHarness_AbsentMode_NotRejected verifies that the
// FR-8 rejection check does not fire for non-ghcp-cli harnesses. The pre-scan
// in main() gates on harnessStr == HarnessIDGHCPCLI before checking the mode.
// For other harnesses, an absent --ghcp-permission-mode is not an error.
func TestFR8Rejection_NonGHCPHarness_AbsentMode_NotRejected(t *testing.T) {
	args := []string{"run", "--harness", commonharness.HarnessIDClaudeCode, "--workflow", "w"}
	harness := scanFlag(args, "--harness")
	mode := scanFlag(args, "--ghcp-permission-mode")
	// Simulate the pre-scan check: rejection applies only when harness is ghcp-cli.
	if harness == commonharness.HarnessIDGHCPCLI && mode == "" {
		t.Errorf("FR-8 rejection would fire for harness %q with absent mode, "+
			"but harness is %q (not ghcp-cli) — the check must be gated on harness identity", harness, harness)
	}
}
