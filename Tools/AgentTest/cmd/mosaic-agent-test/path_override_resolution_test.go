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
// composition root's positional detection recognises all value-consuming path
// override flags, so a space-separated value never gets mistaken for a
// positional subcommand (see selectFrontend's own flag-aware handling).
func TestValueConsumingFlags_IncludesTheNewPathOverrides(t *testing.T) {
	for _, name := range []string{"--logger-bundle", "--cost-tool", "--deploy-tool", "--mosaic-root"} {
		if !valueConsumingFlags[name] {
			t.Errorf("valueConsumingFlags[%q] = false, want true (every value-consuming flag must be recognised by positional detection)", name)
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

// ---------------------------------------------------------------------------
// DeployToolPath precedence
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_DeployToolPath_FlagWinsOverEverything mirrors the
// LoggerBundleDir and CostToolPath precedence tests for --deploy-tool.
func TestResolveWiringConfig_DeployToolPath_FlagWinsOverEverything(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_DEPLOY_TOOL", "C:/from-env/mosaic-deploy.exe")

	cases := map[string][]string{
		"space":  {"run", "suite.yaml", "--deploy-tool", "C:/from-flag/mosaic-deploy.exe"},
		"equals": {"run", "suite.yaml", "--deploy-tool=C:/from-flag/mosaic-deploy.exe"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := resolveWiringConfig(args)
			if cfg.DeployToolPath != "C:/from-flag/mosaic-deploy.exe" {
				t.Errorf("resolveWiringConfig(%v).DeployToolPath = %q, want the flag value %q (a flag must win over the environment variable)", args, cfg.DeployToolPath, "C:/from-flag/mosaic-deploy.exe")
			}
		})
	}
}

// TestResolveWiringConfig_DeployToolPath_EnvWinsOverDefault asserts the
// MOSAIC_AGENT_TEST_DEPLOY_TOOL variable, absent a flag, wins over the
// binary-relative default.
func TestResolveWiringConfig_DeployToolPath_EnvWinsOverDefault(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_DEPLOY_TOOL", "C:/from-env/mosaic-deploy.exe")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.DeployToolPath != "C:/from-env/mosaic-deploy.exe" {
		t.Errorf("resolveWiringConfig(...).DeployToolPath = %q, want the environment value %q (the variable must win over the default when no flag is given)", cfg.DeployToolPath, "C:/from-env/mosaic-deploy.exe")
	}
}

// TestResolveWiringConfig_DeployToolPath_DefaultsToBinaryRelativePath asserts
// that with neither a flag nor the environment variable set, the default
// remains relative to the running binary's own directory — the property that
// keeps a correctly staged distribution working with no configuration at all.
// The binary-relative default includes the platform executable suffix
// (empty on Linux/macOS, ".exe" on Windows), matching CostToolPath's pattern.
func TestResolveWiringConfig_DeployToolPath_DefaultsToBinaryRelativePath(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_DEPLOY_TOOL", "")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	want := filepath.Join(expectedSelfDir(t), "mosaic-deploy"+expectedExeSuffix())
	if cfg.DeployToolPath != want {
		t.Errorf("resolveWiringConfig(...).DeployToolPath = %q, want the binary-relative default %q", cfg.DeployToolPath, want)
	}
}

// ---------------------------------------------------------------------------
// MosaicRoot precedence
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_MosaicRoot_FlagWinsOverEverything mirrors the
// DeployToolPath flag-wins tests for --mosaic-root.
func TestResolveWiringConfig_MosaicRoot_FlagWinsOverEverything(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_MOSAIC_ROOT", "C:/from-env/mosaic-root")

	cases := map[string][]string{
		"space":  {"run", "suite.yaml", "--mosaic-root", "C:/from-flag/mosaic-root"},
		"equals": {"run", "suite.yaml", "--mosaic-root=C:/from-flag/mosaic-root"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := resolveWiringConfig(args)
			if cfg.MosaicRoot != "C:/from-flag/mosaic-root" {
				t.Errorf("resolveWiringConfig(%v).MosaicRoot = %q, want the flag value %q (a flag must win over the environment variable)", args, cfg.MosaicRoot, "C:/from-flag/mosaic-root")
			}
		})
	}
}

// TestResolveWiringConfig_MosaicRoot_EnvWinsOverDefault asserts the
// MOSAIC_AGENT_TEST_MOSAIC_ROOT variable, absent a flag, wins over the
// binary-relative default.
func TestResolveWiringConfig_MosaicRoot_EnvWinsOverDefault(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_MOSAIC_ROOT", "C:/from-env/mosaic-root")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.MosaicRoot != "C:/from-env/mosaic-root" {
		t.Errorf("resolveWiringConfig(...).MosaicRoot = %q, want the environment value %q (the variable must win over the default when no flag is given)", cfg.MosaicRoot, "C:/from-env/mosaic-root")
	}
}

// TestResolveWiringConfig_MosaicRoot_DefaultsToBinaryRelativePath asserts that
// with neither a flag nor the environment variable set, MosaicRoot defaults to a
// non-empty binary-relative path — the same selfDir anchor that LoggerBundleDir,
// CostToolPath, and DeployToolPath already use. An empty default leaves the
// deploy tool unable to locate the repository root when invoked from a directory
// other than the repository root, which is the failure this change fixes.
//
// The exact relative offset from the binary directory to the repository root is
// an implementation determination against the staged distribution layout; what is
// contractual is that the result is non-empty and is an absolute path derived
// from the binary's location, not from the working directory.
func TestResolveWiringConfig_MosaicRoot_DefaultsToBinaryRelativePath(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_MOSAIC_ROOT", "")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.MosaicRoot == "" {
		t.Fatal("resolveWiringConfig(...).MosaicRoot = empty string, want a non-empty binary-relative default; an empty value causes the deploy tool to resolve the MOSAIC root from the working directory, which fails when invoked from outside the repository tree")
	}
	if !filepath.IsAbs(cfg.MosaicRoot) {
		t.Errorf("resolveWiringConfig(...).MosaicRoot = %q, want an absolute binary-relative path (the same selfDir anchor as LoggerBundleDir, CostToolPath, and DeployToolPath)", cfg.MosaicRoot)
	}
}

// ---------------------------------------------------------------------------
// Positional-detection coverage for --deploy-tool and --mosaic-root
// ---------------------------------------------------------------------------

// TestSelectFrontend_DeployToolFlagValue_NotMistakenForPositional mirrors the
// existing --logger-bundle and --cost-tool tests for --deploy-tool: a bare
// invocation carrying only "--deploy-tool <path>" must fall through to the
// terminal check rather than routing to the CLI frontend because the flag's
// value was mistaken for a positional command.
func TestSelectFrontend_DeployToolFlagValue_NotMistakenForPositional(t *testing.T) {
	got := selectFrontend([]string{"--deploy-tool", "C:/bin/mosaic-deploy.exe"}, alwaysTerminal)
	if got != FrontendTUI {
		t.Errorf(`selectFrontend([--deploy-tool C:/bin/mosaic-deploy.exe], alwaysTerminal) = %q, want %q (the flag's value must not be mistaken for a positional command)`, got, FrontendTUI)
	}
}

// TestSelectFrontend_MosaicRootFlagValue_NotMistakenForPositional mirrors the
// above for --mosaic-root.
func TestSelectFrontend_MosaicRootFlagValue_NotMistakenForPositional(t *testing.T) {
	got := selectFrontend([]string{"--mosaic-root", "C:/mosaic"}, alwaysTerminal)
	if got != FrontendTUI {
		t.Errorf(`selectFrontend([--mosaic-root C:/mosaic], alwaysTerminal) = %q, want %q (the flag's value must not be mistaken for a positional command)`, got, FrontendTUI)
	}
}

// ---------------------------------------------------------------------------
// CatalogFolder precedence
// ---------------------------------------------------------------------------

// TestResolveWiringConfig_CatalogFolder_FlagWinsOverEverything mirrors the
// MosaicRoot flag-wins tests for --catalog-folder: the CLI flag must win over
// both the MOSAIC_AGENT_TEST_CATALOG_FOLDER environment variable and the
// default empty string, in both the space-separated and equals-separated forms.
func TestResolveWiringConfig_CatalogFolder_FlagWinsOverEverything(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_CATALOG_FOLDER", "C:/from-env/catalog")

	cases := map[string][]string{
		"space":  {"run", "suite.yaml", "--catalog-folder", "C:/from-flag/catalog"},
		"equals": {"run", "suite.yaml", "--catalog-folder=C:/from-flag/catalog"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := resolveWiringConfig(args)
			if cfg.CatalogFolder != "C:/from-flag/catalog" {
				t.Errorf("resolveWiringConfig(%v).CatalogFolder = %q, want the flag value %q (a flag must win over the environment variable)", args, cfg.CatalogFolder, "C:/from-flag/catalog")
			}
		})
	}
}

// TestResolveWiringConfig_CatalogFolder_EnvWinsOverDefault asserts the
// MOSAIC_AGENT_TEST_CATALOG_FOLDER variable, absent a flag, wins over the
// default empty string. This mirrors the MosaicRoot env-over-default test.
func TestResolveWiringConfig_CatalogFolder_EnvWinsOverDefault(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_CATALOG_FOLDER", "C:/from-env/catalog")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.CatalogFolder != "C:/from-env/catalog" {
		t.Errorf("resolveWiringConfig(...).CatalogFolder = %q, want the environment value %q (the variable must win over the default when no flag is given)", cfg.CatalogFolder, "C:/from-env/catalog")
	}
}

// TestResolveWiringConfig_CatalogFolder_DefaultsToEmpty asserts that with
// neither a flag nor the environment variable set, CatalogFolder defaults to
// the empty string. An empty CatalogFolder means "do not override" — the
// --catalog-folder flag is not passed to the deployment tool, which then
// resolves its own catalogue under its own root. A correctly staged
// distribution therefore works with no flag and no environment variable,
// exactly as MosaicRoot does.
func TestResolveWiringConfig_CatalogFolder_DefaultsToEmpty(t *testing.T) {
	t.Setenv("MOSAIC_AGENT_TEST_CATALOG_FOLDER", "")

	cfg := resolveWiringConfig([]string{"run", "suite.yaml"})

	if cfg.CatalogFolder != "" {
		t.Errorf("resolveWiringConfig(...).CatalogFolder = %q, want empty string (empty means the deploy tool resolves its own catalogue; a correctly staged distribution needs no flag and no variable)", cfg.CatalogFolder)
	}
}

// ---------------------------------------------------------------------------
// Positional-detection coverage for --catalog-folder
// ---------------------------------------------------------------------------

// TestValueConsumingFlags_IncludesCatalogFolder asserts the composition root's
// positional detection recognises --catalog-folder, so a space-separated value
// never gets mistaken for a positional subcommand — the same property the
// existing --mosaic-root test pins for that flag.
func TestValueConsumingFlags_IncludesCatalogFolder(t *testing.T) {
	if !valueConsumingFlags["--catalog-folder"] {
		t.Errorf("valueConsumingFlags[%q] = false, want true (every value-consuming flag must be recognised by positional detection so its value is not mistaken for a subcommand)", "--catalog-folder")
	}
}

// TestSelectFrontend_CatalogFolderFlagValue_NotMistakenForPositional mirrors
// the existing path-override flag tests: a bare invocation carrying only
// "--catalog-folder <path>" must fall through to the terminal check rather than
// routing to the CLI frontend because the flag's own value was mistaken for a
// positional command.
func TestSelectFrontend_CatalogFolderFlagValue_NotMistakenForPositional(t *testing.T) {
	got := selectFrontend([]string{"--catalog-folder", "C:/AgentTest/catalog"}, alwaysTerminal)
	if got != FrontendTUI {
		t.Errorf(`selectFrontend([--catalog-folder C:/AgentTest/catalog], alwaysTerminal) = %q, want %q (the flag's value must not be mistaken for a positional command)`, got, FrontendTUI)
	}
}
