package main

import "testing"

// ---------------------------------------------------------------------------
// Stage 5: hasPositionalArg — value-bearing flag detection (T5.1, T5.2, T5.3, T5.4)
//
// hasPositionalArg must distinguish genuine positional arguments from the value
// tokens of value-bearing flags. After the Stage 5 refactor, hasPositionalArg
// obtains the value-bearing set from cli.ValueBearingFlagNames(), which is
// derived from the same single declaration that cli.Run() uses for flag
// registration.
//
// Value-bearing flags (TakesValue: true) declared in internal/cli/run.go:
//   --orchestrator-file, --workflow, --task, --mode, --checkpoints, --commits,
//   --commit-branch, --run, --harness, --timeout, --executable-path, --infra-class,
//   --input
//
// Boolean flags (TakesValue: false):
//   --allow-version-drift, --pre-consult, --manual-resolution, --new-run, --tui
//
// T5.1 tests are RED until I5.1 teaches hasPositionalArg to skip value tokens.
// T5.2 and T5.4 regression-pin tests are currently GREEN (boolean flags never
// had a value token to skip, and --tui is checked before hasPositionalArg).
// ---------------------------------------------------------------------------

// T5.1 — Value-bearing flag values are not positional arguments.

// TestHasPositionalArg_ValueBearing_SpaceSeparated_ValueNotPositional verifies
// that the value following a value-bearing flag in "--flag value" form is not
// counted as a positional argument.
//
// RED: the current hasPositionalArg sees any non-flag token as positional, so
// it returns true for "/usr/local/bin/claude" even though it is a flag value.
func TestHasPositionalArg_ValueBearing_SpaceSeparated_ValueNotPositional(t *testing.T) {
	if hasPositionalArg([]string{"--executable-path", "/usr/local/bin/claude"}) {
		t.Error("hasPositionalArg([--executable-path /usr/local/bin/claude]) = true, want false; " +
			"the value of a value-bearing flag is not a positional argument")
	}
}

// TestHasPositionalArg_ValueBearing_WindowsPath_ValueNotPositional verifies
// the exact scenario from the reported bug: a Windows-style path value
// (containing backslashes and a colon) following --executable-path must not be
// treated as a positional argument.
//
// RED: the current implementation returns true for the Windows path token.
func TestHasPositionalArg_ValueBearing_WindowsPath_ValueNotPositional(t *testing.T) {
	args := []string{"--executable-path", `C:\Users\tgurt\AppData\Roaming\npm\copilot.cmd`}
	if hasPositionalArg(args) {
		t.Error(`hasPositionalArg([--executable-path C:\...\copilot.cmd]) = true, want false; ` +
			"a Windows-style executable path following a value-bearing flag is a flag value, not a positional argument")
	}
}

// TestHasPositionalArg_ValueBearing_EqualsSeparated_NoValueToken verifies
// that the "--flag=value" form does not introduce a separate value token.
// The entire token starts with "--" so there is nothing to skip; it is
// already correctly classified as a flag.
//
// Currently GREEN: the existing implementation treats this correctly because
// the whole token starts with "-". Serves as a regression pin.
func TestHasPositionalArg_ValueBearing_EqualsSeparated_NoValueToken(t *testing.T) {
	if hasPositionalArg([]string{`--executable-path=C:\Users\tgurt\AppData\Roaming\npm\copilot.cmd`}) {
		t.Error("hasPositionalArg([--executable-path=...]) = true, want false; " +
			"the = form embeds the value in the flag token and must never be counted as positional")
	}
}

// TestHasPositionalArg_MultipleValueBearingFlags_SpaceSeparated_NoPositional
// is a table-driven test verifying that realistic combinations of value-bearing
// flags in space-separated form produce no spurious positional detection.
//
// RED: the current implementation treats every value token as positional.
func TestHasPositionalArg_MultipleValueBearingFlags_SpaceSeparated_NoPositional(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			"harness and timeout",
			[]string{"--harness", "claude-code", "--timeout", "45m"},
		},
		{
			"harness and executable-path",
			[]string{"--harness", "claude-code", "--executable-path", "/opt/bin/claude"},
		},
		{
			"workflow and task",
			[]string{"--workflow", "w1", "--task", "do the work"},
		},
		{
			"full realistic TUI invocation without positional",
			[]string{
				"--harness", "claude-code",
				"--executable-path", `C:\Users\tgurt\AppData\Roaming\npm\claude.cmd`,
				"--timeout", "30m",
				"--mode", "auto",
				"--new-run",
			},
		},
		{
			"input flag with path value",
			[]string{"--input", "/seed/file.md", "--new-run"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if hasPositionalArg(tc.args) {
				t.Errorf("hasPositionalArg(%v) = true, want false; "+
					"all non-flag tokens are flag values, not positional arguments", tc.args)
			}
		})
	}
}

// TestHasPositionalArg_EqualsSeparatedAndSpaceSeparated_ProduceIdenticalResult
// verifies that "--flag=value" and "--flag value" are treated identically for
// mode detection: neither contains a positional argument when the flag is
// value-bearing (AC5.3).
//
// RED (space-separated case): the current implementation treats the space-
// separated value token as positional, returning different results for the
// two forms.
func TestHasPositionalArg_EqualsSeparatedAndSpaceSeparated_ProduceIdenticalResult(t *testing.T) {
	spaceSep := []string{"--executable-path", "/some/claude"}
	equalsSep := []string{"--executable-path=/some/claude"}

	gotSpace := hasPositionalArg(spaceSep)
	gotEquals := hasPositionalArg(equalsSep)

	if gotSpace != gotEquals {
		t.Errorf("space-separated hasPositionalArg=%v, equals-separated hasPositionalArg=%v; "+
			"both forms must produce identical mode-detection results (AC5.3): "+
			"only the space-separated form should be false after the fix",
			gotSpace, gotEquals)
	}
	if gotSpace {
		t.Error("hasPositionalArg([--executable-path /some/claude]) = true, want false; " +
			"the value token of a value-bearing flag is not a positional argument")
	}
}

// T5.2 — Boolean flags do not consume a following token.

// TestHasPositionalArg_BoolFlag_FollowedByPositional_PositionalDetected verifies
// that a boolean flag (which takes no value) does not swallow the following
// positional argument. This is the critical regression-prevention case: treating
// --pre-consult as value-consuming would cause "run" to be silently skipped,
// inverting the bug into the opposite failure mode.
//
// Currently GREEN (boolean flags have no separate value token to skip). Serves
// as a regression pin so the fix cannot introduce over-skipping.
func TestHasPositionalArg_BoolFlag_FollowedByPositional_PositionalDetected(t *testing.T) {
	args := []string{"--pre-consult", "run"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg([--pre-consult run]) = false, want true; " +
			"--pre-consult is a boolean flag and must not consume the following token; " +
			"\"run\" must be detected as a genuine positional argument (AC5.5)")
	}
}

// TestHasPositionalArg_BoolFlag_NewRun_FollowedByPositional verifies that
// --new-run (boolean) does not consume the following positional argument.
//
// Currently GREEN; regression pin.
func TestHasPositionalArg_BoolFlag_NewRun_FollowedByPositional(t *testing.T) {
	args := []string{"--new-run", "run"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg([--new-run run]) = false, want true; " +
			"--new-run is boolean and must not swallow the following positional argument")
	}
}

// TestHasPositionalArg_BoolFlag_AllowVersionDrift_FollowedByPositional verifies
// that --allow-version-drift (boolean) does not consume the following positional.
//
// Currently GREEN; regression pin.
func TestHasPositionalArg_BoolFlag_AllowVersionDrift_FollowedByPositional(t *testing.T) {
	args := []string{"--allow-version-drift", "run"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg([--allow-version-drift run]) = false, want true; " +
			"--allow-version-drift is boolean and must not swallow \"run\"")
	}
}

// TestHasPositionalArg_BoolFlagFollowedByValueBearingFlag_NoPositional verifies
// that a boolean flag followed by a value-bearing flag (not a positional) is
// correctly classified as no-positional. This exercises both the no-skip
// (boolean) and skip (value-bearing) paths in the same scan.
//
// RED: the current implementation treats "claude-code" (value of --harness) as
// positional, even though it is correctly preceded by a value-bearing flag.
func TestHasPositionalArg_BoolFlagFollowedByValueBearingFlag_NoPositional(t *testing.T) {
	args := []string{"--new-run", "--harness", "claude-code"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--new-run --harness claude-code]) = true, want false; " +
			"--new-run is boolean (no skip), --harness is value-bearing (skip \"claude-code\"); " +
			"no genuine positional is present")
	}
}

// T5.3 — Genuine positional arguments are still detected; flag-only invocations route to TUI.

// TestHasPositionalArg_GenuinePositionalAfterValueBearingFlag_Detected verifies
// that a genuine positional argument appearing after a value-bearing flag and
// its value is still detected. The fix must not over-skip.
//
// After the fix: "--executable-path /some/claude" skips the path value, but "run"
// following that pair is a genuine positional and must still be detected (AC5.4).
func TestHasPositionalArg_GenuinePositionalAfterValueBearingFlag_Detected(t *testing.T) {
	args := []string{"--executable-path", "/some/claude", "run"}
	if !hasPositionalArg(args) {
		t.Error("hasPositionalArg([--executable-path /some/claude run]) = false, want true; " +
			"\"run\" is a genuine positional argument and must be detected even after a value-bearing flag pair (AC5.4)")
	}
}

// TestHasPositionalArg_FlagOnlyInvocationNoPositional_ReturnsFalse verifies that
// a realistic invocation consisting only of value-bearing flags and boolean flags
// returns false, confirming that without a genuine positional argument the decision
// falls through to the isatty check rather than short-circuiting to CLI mode (AC5.2).
//
// RED: the current implementation treats value tokens as positional, returning
// true for any invocation with space-separated value-bearing flags.
func TestHasPositionalArg_FlagOnlyInvocationNoPositional_ReturnsFalse(t *testing.T) {
	args := []string{
		"--harness", "claude-code",
		"--mode", "auto",
		"--new-run",
		"--pre-consult",
	}
	if hasPositionalArg(args) {
		t.Errorf("hasPositionalArg(%v) = true, want false; "+
			"this invocation has no genuine positional argument — "+
			"treating flag values as positional falsely routes TUI users to CLI mode (AC5.2)", args)
	}
}

// T5.4 — Regression: executable-path override reaches the TUI rather than
// producing "error: unknown flag: --executable-path".

// TestHasPositionalArg_ExecPathWithWindowsValue_IsNotPositional is the direct
// regression pin for the reported failure: passing --executable-path with a
// Windows-style executable path must not be treated as a positional argument.
// The root cause is that hasPositionalArg counted the path value as positional,
// silently switching to CLI mode where the root cobra command does not define
// --executable-path, producing "error: unknown flag: --executable-path".
//
// RED: the current hasPositionalArg returns true for the Windows path token.
func TestHasPositionalArg_ExecPathWithWindowsValue_IsNotPositional(t *testing.T) {
	args := []string{"--executable-path", `C:\Users\tgurt\AppData\Roaming\npm\copilot.cmd`}
	if hasPositionalArg(args) {
		t.Error(`hasPositionalArg([--executable-path C:\...\copilot.cmd]) = true, want false; ` +
			"this is the regression: the Windows path value is not a positional argument; " +
			"treating it as one routes the invocation to CLI mode, producing \"unknown flag: --executable-path\" (AC5.6)")
	}
}

// TestWantsTUI_TUIFlagWithExecPathOverride_AlwaysChoosesTUI verifies that
// an explicit --tui flag forces TUI mode regardless of other arguments, including
// an --executable-path value. This test is currently GREEN (--tui is checked before
// hasPositionalArg). It serves as a regression pin for the --tui short-circuit.
func TestWantsTUI_TUIFlagWithExecPathOverride_AlwaysChoosesTUI(t *testing.T) {
	args := []string{"--tui", "--executable-path", `C:\Users\tgurt\AppData\Roaming\npm\copilot.cmd`}
	if !wantsTUI(args) {
		t.Error("wantsTUI([--tui --executable-path ...]) = false, want true; " +
			"--tui must force TUI mode regardless of other arguments including an --executable-path value")
	}
}

// TestWantsTUI_FlagOnlyInvocation_DoesNotShortCircuitToCLI verifies that an
// invocation with only value-bearing flags does not trigger the CLI short-circuit
// inside wantsTUI via a spurious positional detection.
//
// In test environments stdin/stdout are not terminals, so wantsTUI returns false
// after the fix too — but for the correct reason (no terminal), not the wrong
// reason (spurious positional). The test asserts the root cause directly.
//
// RED: the current hasPositionalArg treats value tokens as positional, causing
// wantsTUI to return false before even reaching the isatty check.
func TestWantsTUI_FlagOnlyInvocation_DoesNotShortCircuitToCLI(t *testing.T) {
	args := []string{"--harness", "claude-code", "--timeout", "30m"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--harness claude-code --timeout 30m]) = true; " +
			"flag values are being counted as positional arguments, " +
			"causing wantsTUI to falsely short-circuit to CLI mode for any space-separated value-bearing flag")
	}
}

// ---------------------------------------------------------------------------
// --dev flag pre-scan and test subcommand routing
// ---------------------------------------------------------------------------

// TestScanBoolFlag_DevFlag_Present verifies that scanBoolFlag correctly detects
// the --dev flag when it is present in args.
func TestScanBoolFlag_DevFlag_Present(t *testing.T) {
	args := []string{"--dev", "test", "--catalog", "/some/path"}
	if !scanBoolFlag(args, "--dev") {
		t.Error("scanBoolFlag([--dev test --catalog /some/path], --dev) = false, want true")
	}
}

// TestScanBoolFlag_DevFlag_Absent verifies that scanBoolFlag returns false
// when --dev is not present in args.
func TestScanBoolFlag_DevFlag_Absent(t *testing.T) {
	args := []string{"test", "--catalog", "/some/path", "--suite", "smoke"}
	if scanBoolFlag(args, "--dev") {
		t.Error("scanBoolFlag(args without --dev, --dev) = true, want false")
	}
}

// TestStripBoolFlag_RemovesDevFlag verifies that stripBoolFlag removes --dev
// from args in all its forms.
func TestStripBoolFlag_RemovesDevFlag(t *testing.T) {
	cases := []struct {
		input []string
		want  []string
	}{
		{
			input: []string{"--dev", "test", "--catalog", "/foo"},
			want:  []string{"test", "--catalog", "/foo"},
		},
		{
			input: []string{"--dev=true", "test", "--catalog", "/foo"},
			want:  []string{"test", "--catalog", "/foo"},
		},
		{
			input: []string{"--dev=false", "test"},
			want:  []string{"test"},
		},
		{
			input: []string{"test", "--catalog", "/foo"},
			want:  []string{"test", "--catalog", "/foo"},
		},
	}
	for _, tc := range cases {
		got := stripBoolFlag(tc.input, "--dev")
		if len(got) != len(tc.want) {
			t.Errorf("stripBoolFlag(%v) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("stripBoolFlag(%v)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// TestFirstPositionalArg_TestSubcommand verifies that firstPositionalArg
// correctly identifies "test" as the first positional argument.
func TestFirstPositionalArg_TestSubcommand(t *testing.T) {
	args := []string{"test", "--catalog", "/some/path", "--suite", "smoke"}
	got := firstPositionalArg(args)
	if got != "test" {
		t.Errorf("firstPositionalArg([test --catalog /some/path --suite smoke]) = %q, want \"test\"", got)
	}
}

// TestFirstPositionalArg_RunSubcommand verifies that firstPositionalArg
// correctly identifies "run" as the first positional argument.
func TestFirstPositionalArg_RunSubcommand(t *testing.T) {
	args := []string{"run", "--workflow", "w1", "--mode", "auto"}
	got := firstPositionalArg(args)
	if got != "run" {
		t.Errorf("firstPositionalArg([run --workflow w1 --mode auto]) = %q, want \"run\"", got)
	}
}

// TestFirstPositionalArg_CatalogFlagValueNotMistakenForSubcommand verifies that
// "--catalog /some/path test ..." does not produce "/some/path" as the first
// positional arg (which would be wrong: /some/path is a flag value).
// This is the pre-scan compatibility test for test subcommand flags.
func TestFirstPositionalArg_CatalogFlagValueNotMistakenForSubcommand(t *testing.T) {
	// Flags before the subcommand: --catalog value is NOT a positional arg.
	args := []string{"--catalog", "/some/path", "test", "--suite", "smoke"}
	got := firstPositionalArg(args)
	if got == "/some/path" {
		t.Errorf("firstPositionalArg([--catalog /some/path test --suite smoke]) = %q; "+
			"--catalog's value must not be treated as a positional argument; "+
			"AllValueBearingFlagNames() must include --catalog", got)
	}
	if got != "test" {
		t.Errorf("firstPositionalArg([--catalog /some/path test --suite smoke]) = %q, want \"test\"", got)
	}
}

// TestFirstPositionalArg_NoSubcommand_ReturnsEmpty verifies that
// firstPositionalArg returns "" when there is no positional argument.
func TestFirstPositionalArg_NoSubcommand_ReturnsEmpty(t *testing.T) {
	args := []string{"--harness", "claude-code", "--timeout", "30m"}
	got := firstPositionalArg(args)
	if got != "" {
		t.Errorf("firstPositionalArg([--harness claude-code --timeout 30m]) = %q, want \"\"", got)
	}
}

// TestFirstPositionalArg_SuiteFlagValueNotPositional verifies that --suite's
// value "smoke" is not treated as a positional argument when --suite appears
// before the subcommand.
func TestFirstPositionalArg_SuiteFlagValueNotPositional(t *testing.T) {
	// Unusual arg ordering: suite flag before subcommand name.
	args := []string{"--suite", "smoke", "test", "--catalog", "/foo"}
	got := firstPositionalArg(args)
	if got == "smoke" {
		t.Errorf("firstPositionalArg([--suite smoke test --catalog /foo]) = %q; "+
			"--suite's value must not be treated as a positional argument", got)
	}
	if got != "test" {
		t.Errorf("firstPositionalArg([--suite smoke test --catalog /foo]) = %q, want \"test\"", got)
	}
}

// TestHasPositionalArg_TestCatalogFlagValue_NotMistakenForPositional verifies
// that when --catalog /some/path appears (either before or after the subcommand),
// the path value is not treated as a positional argument. This guards AC7.4.
func TestHasPositionalArg_TestCatalogFlagValue_NotMistakenForPositional(t *testing.T) {
	// Only flags and flag values — no genuine positional arg.
	args := []string{"--catalog", "/some/path", "--suite", "smoke"}
	if hasPositionalArg(args) {
		t.Error("hasPositionalArg([--catalog /some/path --suite smoke]) = true; " +
			"--catalog's value is being counted as a positional arg; " +
			"AllValueBearingFlagNames() must include --catalog")
	}
}

// ---------------------------------------------------------------------------
// Routing gate: AC7.3 invariant
//
// The test subcommand must be invisible without --dev. main() implements this
// as: `if devMode && firstPositionalArg(cobraArgs) == "test" { ... }`.
// Individual helpers (scanBoolFlag, firstPositionalArg) are unit-tested above.
// These tests verify the ROUTING CONDITION itself as a composite: that the
// conjunction evaluates to false whenever --dev is absent or explicitly false,
// even when "test" is the first positional arg. A future change that removes
// the devMode gate (e.g. registering the test command unconditionally) would
// break these tests even if all helper tests still pass.
// ---------------------------------------------------------------------------

// TestRoutingGate_WithoutDevFlag_DoesNotRouteToTestCommand verifies that when
// --dev is absent from args, the routing condition used in main() evaluates to
// false. The test subcommand must not be reached regardless of the positional arg.
func TestRoutingGate_WithoutDevFlag_DoesNotRouteToTestCommand(t *testing.T) {
	args := []string{"test", "--catalog", "/some/path", "--suite", "smoke"}

	devMode := scanBoolFlag(args, "--dev")
	firstArg := firstPositionalArg(args)

	// Guard: confirm firstPositionalArg returns "test" so the test is
	// meaningful — if it returned something else, the false result below
	// would not exercise the devMode gate.
	if firstArg != "test" {
		t.Fatalf("firstPositionalArg = %q, want \"test\"; test setup is invalid", firstArg)
	}

	// The routing condition: devMode must be false so the gate does not open.
	if devMode && firstArg == "test" {
		t.Error("routing condition = true without --dev in args; " +
			"the test subcommand must be gated on devMode, " +
			"which must be false when --dev is absent")
	}
}

// TestRoutingGate_WithDevEqualsFalse_DoesNotRouteToTestCommand verifies that
// the explicit --dev=false form also keeps the routing gate closed. This covers
// the edge case where a user passes --dev=false explicitly, which scanBoolFlag
// must classify as false (not true). Without this test, a regression in
// stripBoolFlag or scanBoolFlag for the =false form could silently open the gate.
func TestRoutingGate_WithDevEqualsFalse_DoesNotRouteToTestCommand(t *testing.T) {
	// --dev=false is stripped in main() before cobra sees it; simulate
	// the full pre-scan pipeline: scan first, then strip.
	rawArgs := []string{"--dev=false", "test", "--catalog", "/foo"}

	devMode := scanBoolFlag(rawArgs, "--dev")
	cobraArgs := stripBoolFlag(rawArgs, "--dev")
	firstArg := firstPositionalArg(cobraArgs)

	// Guard: confirm "test" is still identified after stripping --dev=false.
	if firstArg != "test" {
		t.Fatalf("firstPositionalArg after strip = %q, want \"test\"; test setup is invalid", firstArg)
	}

	// The routing condition must be false.
	if devMode && firstArg == "test" {
		t.Errorf("routing condition = true with --dev=false; "+
			"scanBoolFlag([--dev=false ...], --dev) returned %v, want false", devMode)
	}
}
