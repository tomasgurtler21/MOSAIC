package main

// Tests for resolveWiringConfig's resolution of the logger-bundle directory
// and the cost-tool path, per ContractsDesign.md's configuration resolution
// contract: a CLI flag wins over an environment variable, which wins over
// the binary-relative default. The default is retained, not replaced — a
// correctly staged dist/ must keep working with no flag and no variable —
// so these tests cover all three tiers, isolated from one another.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// expectedSelfDir mirrors resolveWiringConfig's own self-path resolution, so
// a default-tier test asserts against the same directory production code
// would compute rather than a value invented by the test.
func expectedSelfDir(t *testing.T) string {
	t.Helper()
	selfPath, err := os.Executable()
	if err != nil {
		selfPath = os.Args[0]
	}
	return filepath.Dir(selfPath)
}

func expectedExeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// ---------------------------------------------------------------------------
// LoggerBundleDir precedence
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_LoggerBundleDir_FlagWinsOverEverything asserts the
// --logger-bundle flag, in either accepted form, wins over both the
// environment variable and the default.
func TestResolveWiringConfig_LoggerBundleDir_FlagWinsOverEverything(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_LOGGER_BUNDLE", "C:/from-env/logger-bundle")

	cases := map[string][]string{
		"space":  {"run", "suite.yaml", "--logger-bundle", "C:/from-flag/logger-bundle"},
		"equals": {"run", "suite.yaml", "--logger-bundle=C:/from-flag/logger-bundle"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := resolveWiringConfig(args)
			if cfg.LoggerBundleDir != "C:/from-flag/logger-bundle" {
				t.Errorf("resolveWiringConfig(%v).LoggerBundleDir = %q, want the flag value %q (a flag must win over the environment variable)", args, cfg.LoggerBundleDir, "C:/from-flag/logger-bundle")
			}
		})
	}
}

// TestResolveWiringConfig_LoggerBundleDir_EnvWinsOverDefault asserts the
// MOSAIC_AGENT_TEST_LOGGER_BUNDLE variable, absent a flag, wins over the
// binary-relative default.
func TestResolveWiringConfig_LoggerBundleDir_EnvWinsOverDefault(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_LOGGER_BUNDLE", "C:/from-env/logger-bundle")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.LoggerBundleDir != "C:/from-env/logger-bundle" {
		t.Errorf("resolveWiringConfig(...).LoggerBundleDir = %q, want the environment value %q (the variable must win over the default when no flag is given)", cfg.LoggerBundleDir, "C:/from-env/logger-bundle")
	}
}

// TestResolveWiringConfig_LoggerBundleDir_DefaultsToBinaryRelativeDir
// asserts that with neither a flag nor the environment variable set, the
// default remains relative to the running binary's own directory — the
// property that keeps a correctly staged dist/ working with no
// configuration at all.
func TestResolveWiringConfig_LoggerBundleDir_DefaultsToBinaryRelativeDir(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_LOGGER_BUNDLE", "")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	want := filepath.Join(expectedSelfDir(t), "logger-bundle")
	if cfg.LoggerBundleDir != want {
		t.Errorf("resolveWiringConfig(...).LoggerBundleDir = %q, want the binary-relative default %q", cfg.LoggerBundleDir, want)
	}
}

// ---------------------------------------------------------------------------
// CostToolPath precedence
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_CostToolPath_FlagWinsOverEverything mirrors the
// LoggerBundleDir precedence tests for --cost-tool.
func TestResolveWiringConfig_CostToolPath_FlagWinsOverEverything(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_COST_TOOL", "C:/from-env/mosaic-log-analyzer.exe")

	cases := map[string][]string{
		"space":  {"run", "suite.yaml", "--cost-tool", "C:/from-flag/mosaic-log-analyzer.exe"},
		"equals": {"run", "suite.yaml", "--cost-tool=C:/from-flag/mosaic-log-analyzer.exe"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := resolveWiringConfig(args)
			if cfg.CostToolPath != "C:/from-flag/mosaic-log-analyzer.exe" {
				t.Errorf("resolveWiringConfig(%v).CostToolPath = %q, want the flag value %q (a flag must win over the environment variable)", args, cfg.CostToolPath, "C:/from-flag/mosaic-log-analyzer.exe")
			}
		})
	}
}

// TestResolveWiringConfig_CostToolPath_EnvWinsOverDefault mirrors the
// LoggerBundleDir env-over-default test for --cost-tool.
func TestResolveWiringConfig_CostToolPath_EnvWinsOverDefault(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_COST_TOOL", "C:/from-env/mosaic-log-analyzer.exe")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.CostToolPath != "C:/from-env/mosaic-log-analyzer.exe" {
		t.Errorf("resolveWiringConfig(...).CostToolPath = %q, want the environment value %q", cfg.CostToolPath, "C:/from-env/mosaic-log-analyzer.exe")
	}
}

// TestResolveWiringConfig_CostToolPath_DefaultsToBinaryRelativePath mirrors
// the LoggerBundleDir default test for --cost-tool, including the
// platform executable suffix.
func TestResolveWiringConfig_CostToolPath_DefaultsToBinaryRelativePath(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_COST_TOOL", "")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	want := filepath.Join(expectedSelfDir(t), "mosaic-log-analyzer"+expectedExeSuffix())
	if cfg.CostToolPath != want {
		t.Errorf("resolveWiringConfig(...).CostToolPath = %q, want the binary-relative default %q", cfg.CostToolPath, want)
	}
}

// ---------------------------------------------------------------------------
// AC6.6 — registered with positional detection
// ---------------------------------------------------------------------------

// TestValueConsumingFlags_IncludesTheNewPathOverrides asserts the
// composition root's positional detection recognises both new flags as
// value-consuming, so a space-separated value never gets mistaken for a
// positional subcommand (see selectFrontend's own flag-aware handling).
func TestValueConsumingFlags_IncludesTheNewPathOverrides(t *testing.T) {
	for _, name := range []string{"--logger-bundle", "--cost-tool"} {
		if !valueConsumingFlags[name] {
			t.Errorf("valueConsumingFlags[%q] = false, want true (AC6.6: every new value-consuming flag must be recognised by positional detection)", name)
		}
	}
}

// TestSelectFrontend_LoggerBundleFlagValue_NotMistakenForPositional is the
// behavioural form of the same requirement: a bare invocation carrying only
// "--logger-bundle <dir>" and nothing else must fall through to the
// terminal check, exactly as the existing --workspace-root case does,
// rather than resolving to the CLI frontend because the flag's own value was
// mistaken for a positional command.
func TestSelectFrontend_LoggerBundleFlagValue_NotMistakenForPositional(t *testing.T) {
	got := selectFrontend([]string{"--logger-bundle", "C:/bundles/logger"}, alwaysTerminal)
	if got != FrontendTUI {
		t.Errorf(`selectFrontend([--logger-bundle C:/bundles/logger], alwaysTerminal) = %q, want %q (the flag's value must not be mistaken for a positional command)`, got, FrontendTUI)
	}
}

// TestSelectFrontend_CostToolFlagValue_NotMistakenForPositional mirrors the
// above for --cost-tool.
func TestSelectFrontend_CostToolFlagValue_NotMistakenForPositional(t *testing.T) {
	got := selectFrontend([]string{"--cost-tool", "C:/bin/mosaic-log-analyzer.exe"}, alwaysTerminal)
	if got != FrontendTUI {
		t.Errorf(`selectFrontend([--cost-tool C:/bin/mosaic-log-analyzer.exe], alwaysTerminal) = %q, want %q (the flag's value must not be mistaken for a positional command)`, got, FrontendTUI)
	}
}
