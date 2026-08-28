// Command mosaic-agent-test exercises a real MOSAIC agent under an agent
// harness, replaces every collaborator invocation with a declarative stub at
// the harness's interception layer, and evaluates the resulting invocation
// sequence and orchestration state against declarative assertions.
//
// This binary is one of three things, and this file — the composition
// root — is the only place that decides which and the only place that
// constructs a concrete adapter, store or provider. Dispatch order is a
// correctness property, not an ordering that happens to be right today:
//
//  1. The interceptor route ("intercept" as the first argument). The
//     harness invokes this route non-interactively, from an arbitrary
//     working directory, with the interception payload on standard input
//     and the reply expected on standard output. This route is decided
//     before any terminal detection and before any frontend selection runs,
//     because an interception from an interactive context that reached
//     terminal detection first could launch a user interface into the pipe
//     the harness is reading its reply from — corrupting the reply and
//     hanging the subject's turn. It is deliberately absent from the
//     ordinary help surface: the subject under test may see this binary's
//     usage output, and the transparency obligation extends there too.
//  2. The interactive TUI frontend (internal/tui), selected by the explicit
//     "--tui" override from anywhere in the arguments, or — absent both the
//     override and a positional command — by both standard input and
//     standard output being terminals.
//  3. The non-interactive CLI frontend (internal/cli), selected by a
//     positional command, or by either stream not being a terminal.
//
// Every concrete adapter, store and provider this binary constructs is built
// exactly once, in this package, and handed to whichever frontend dispatch
// selects; neither frontend constructs infrastructure of its own.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/tui"
)

func main() {
	args := os.Args[1:]

	switch selectFrontend(args, realTerminalCheck) {
	case FrontendInterceptor:
		os.Exit(runIntercept(args[1:], os.Stdin, os.Stdout, os.Stderr))
	case FrontendTUI:
		os.Exit(runFrontend(func() int { return runTUIMode(args) }))
	default:
		os.Exit(runFrontend(func() int { return runCLIMode(args) }))
	}
}

// realTerminalCheck is the real terminal-detection seam main wires
// selectFrontend to. Every test in this package supplies its own
// TerminalCheck instead, which is what makes dispatch testable without a
// real terminal.
func realTerminalCheck() (stdinIsTerminal, stdoutIsTerminal bool) {
	return isatty.IsTerminal(os.Stdin.Fd()), isatty.IsTerminal(os.Stdout.Fd())
}

// scanFlag pre-scans args for a named flag's value in either the
// space-separated ("--flag value") or equals-separated ("--flag=value")
// form, matching the frontends' own flag parsing so both styles are always
// equivalent. Returns def when the flag is absent. Used only to resolve the
// harness selection before any frontend — and therefore before either
// frontend's own flag parser — runs.
func scanFlag(args []string, name, def string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(arg, name+"="); ok {
			return v
		}
	}
	return def
}

// exeSuffix names the platform executable suffix, used to locate this
// module's companion binaries beside this one.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// resolveWiringConfig turns one invocation's arguments and this process's
// own identity into the composition root's wiring input. --harness is the
// one flag pre-scanned here, because harness selection must be resolved
// before buildDeps runs and before either frontend's own flag parser sees
// the arguments; every other default here is exactly that — a default —
// and remains open to the CLI's own flags through cli.Options.
func resolveWiringConfig(args []string) WiringConfig {
	selfPath, err := os.Executable()
	if err != nil {
		selfPath = os.Args[0]
	}
	selfDir := filepath.Dir(selfPath)

	mosaicRoot, mosaicRootProvenance := resolveConfiguredPathWithProvenance(
		args, "--mosaic-root", "MOSAIC_AGENT_TEST_MOSAIC_ROOT",
		filepath.Join(selfDir, "../../.."), "selfDir/../../..",
	)

	return WiringConfig{
		// SelfDir is the anchor for every binary-relative default. It is
		// resolved once here and shared so no consumer duplicates the
		// os.Executable()/filepath.Dir computation.
		SelfDir: selfDir,

		// The default names the shared CLI-harness catalog's Claude Code
		// identity, not an adapter package's own constant, so the resolved
		// default cannot silently diverge from the catalog. The resolved
		// value is unchanged: commonharness.HarnessIDClaudeCode and
		// claudecode.HarnessID are the same string.
		HarnessID:     scanFlag(args, "--harness", commonharness.HarnessIDClaudeCode),
		FixtureRoot:   scanFlag(args, "--fixtures", "fixtures"),
		WorkspaceRoot: scanFlag(args, "--workspace-root", filepath.Join(os.TempDir(), "mosaic-agent-test-workspaces")),
		SelfPath:      selfPath,

		// Resolution order, highest precedence first: the CLI flag, then
		// the environment variable, then the binary-relative default — see
		// ContractsDesign.md's configuration resolution contract. The
		// default is retained, not replaced, so a correctly staged dist/
		// keeps working with no flag and no variable.
		LoggerBundleDir: resolveConfiguredPath(
			args, "--logger-bundle", "MOSAIC_AGENT_TEST_LOGGER_BUNDLE",
			filepath.Join(selfDir, "logger-bundle"),
		),
		CostToolPath: resolveConfiguredPath(
			args, "--cost-tool", "MOSAIC_AGENT_TEST_COST_TOOL",
			filepath.Join(selfDir, "mosaic-log-analyzer"+exeSuffix()),
		),
		// DeployToolPath defaults to the binary-relative mosaic-deploy[.exe],
		// so a correctly staged distribution resolves it with no flag and no
		// environment variable, exactly as CostToolPath does for the cost tool.
		DeployToolPath: resolveConfiguredPath(
			args, "--deploy-tool", "MOSAIC_AGENT_TEST_DEPLOY_TOOL",
			filepath.Join(selfDir, "mosaic-deploy"+exeSuffix()),
		),
		// MosaicRoot defaults to the binary-relative repository root, derived
		// from the same selfDir anchor as LoggerBundleDir, CostToolPath, and
		// DeployToolPath. In a correctly staged distribution the binary lives
		// three directories below the repository root (dist/ inside the
		// AgentTest module directory, itself inside Tools/), so the default
		// resolves the repository root without requiring a flag or an
		// environment variable. An explicit --mosaic-root always wins.
		// MosaicRootProvenance records which tier produced the value so it can
		// be embedded verbatim in delegate-tool failure diagnostics.
		MosaicRoot:           mosaicRoot,
		MosaicRootProvenance: mosaicRootProvenance,
		// CatalogFolder defaults to the MosaicRoot-anchored test-stub catalog
		// directory. This deliberately deviates from the selfDir anchor used
		// by LoggerBundleDir, CostToolPath, and DeployToolPath, because the
		// test-stub catalog lives in the source tree, not in dist/. The
		// --catalog-folder flag and MOSAIC_AGENT_TEST_CATALOG_FOLDER env var
		// overrides take precedence via resolveConfiguredPath.
		CatalogFolder: resolveConfiguredPath(
			args, "--catalog-folder", "MOSAIC_AGENT_TEST_CATALOG_FOLDER",
			filepath.Join(mosaicRoot, "Tools", "AgentTest", "catalog"),
		),
		// DeployScratchRoot is a per-process path under os.TempDir(). Nothing
		// is written there (dry-run suppresses the write and directory
		// creation), so it is never created and never cleaned up.
		DeployScratchRoot: filepath.Join(os.TempDir(), fmt.Sprintf("mosaic-agent-test-deploy-scratch-%d", os.Getpid())),

		CostTimeout:   30 * time.Second,
		DeployTimeout: 60 * time.Second,
		Diag:          os.Stderr, // never stdout: stdout carries the machine-readable report in --format json

		// Pre-scan the output mode so buildDeps can call
		// ResolveDiagnosticDestination before either frontend's own flag
		// parser runs. The same two-pass pattern as --harness above.
		OutputMode: resolveOutputMode(args),
	}
}

// resolveOutputMode pre-scans args for the output mode without a full flag
// parse, matching selectFrontend's own pre-scan logic. --tui anywhere in the
// args implies TUI mode; --format json implies JSON mode; everything else is
// the human-readable CLI mode.
func resolveOutputMode(args []string) OutputMode {
	for _, arg := range args {
		if arg == "--tui" {
			return OutputModeTUI
		}
	}
	if scanFlag(args, "--format", "text") == "json" {
		return OutputModeJSON
	}
	return OutputModeCLI
}

// suitesRootFrom returns the resolved suite-discovery root directory for a TUI
// invocation. When args contains a --suites flag, that path is returned
// unconditionally. Otherwise the default is selfDir/.. — one level above the
// running binary's own directory — which is the AgentTest module root in a
// correctly staged distribution. The current working directory is never
// consulted; suite discovery anchors to the binary location, not CWD.
//
// FR-3: no environment variable override; precedence is flag > default only.
func suitesRootFrom(args []string, selfDir string) string {
	if v := scanFlag(args, "--suites", ""); v != "" {
		return v
	}
	return filepath.Join(selfDir, "..")
}

// ConfigProvenance records which resolution tier produced a configured
// path value. It is a human-readable string suitable for embedding in a
// diagnostic message, e.g. "--mosaic-root default: selfDir/../../..",
// "--mosaic-root flag", "--mosaic-root env: MOSAIC_AGENT_TEST_MOSAIC_ROOT".
type ConfigProvenance string

// resolveConfiguredPath resolves one configurable path through the
// three-tier precedence every override in this binary follows: a CLI flag
// (scanFlag, matching both frontends' own accepted forms), then the named
// environment variable, then def — never replaced, only overridden.
func resolveConfiguredPath(args []string, flagName, envName, def string) string {
	if v := scanFlag(args, flagName, ""); v != "" {
		return v
	}
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return def
}

// resolveConfiguredPathWithProvenance resolves one configurable path
// through the same three-tier precedence as resolveConfiguredPath and
// additionally reports which tier produced the value. The provenance
// string is human-readable and includes the flag name and, for the
// default tier, the literal default expression.
//
// defExpr is the human-readable expression for the default value
// (e.g. "selfDir/../../.."), distinct from def which is the evaluated
// filepath. defExpr appears in the provenance string only when the
// default tier is selected.
func resolveConfiguredPathWithProvenance(
	args []string,
	flagName, envName string,
	def string,
	defExpr string,
) (value string, provenance ConfigProvenance) {
	if v := scanFlag(args, flagName, ""); v != "" {
		return v, ConfigProvenance(flagName + " flag")
	}
	if v := os.Getenv(envName); v != "" {
		return v, ConfigProvenance(flagName + " env: " + envName)
	}
	return def, ConfigProvenance(flagName + " default: " + defExpr)
}

// wiringFailureExitCode maps a buildDeps failure onto the CLI's own exit
// codes: an unrecognised harness selection is the usage error AC17.6
// requires, and anything else is an infrastructure fault this binary itself
// could not act on.
func wiringFailureExitCode(err error) int {
	if errors.Is(err, ErrUnknownHarness) {
		return cli.ExitUsage
	}
	return cli.ExitFailure
}

func runCLIMode(args []string) int {
	d, err := buildDeps(resolveWiringConfig(args))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mosaic-agent-test: %v\n", err)
		return wiringFailureExitCode(err)
	}
	return cli.Execute(context.Background(), args, cliOptions(d, os.Stdout, os.Stderr))
}

func runTUIMode(args []string) int {
	cfg := resolveWiringConfig(args)
	d, err := buildDeps(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mosaic-agent-test: %v\n", err)
		return wiringFailureExitCode(err)
	}

	suitesRoot := suitesRootFrom(args, cfg.SelfDir)

	opts, err := tuiOptions(d, discoverSuites(suitesRoot))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mosaic-agent-test: %v\n", err)
		return cli.ExitFailure
	}

	if err := tui.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "mosaic-agent-test: %v\n", err)
		return cli.ExitFailure
	}
	return cli.ExitSuccess
}

// discoverSuites lists the authored suite files offered on the interactive
// frontend's suite-select screen.
//
// root is resolved by the composition root: the --suites value when present,
// the process working directory otherwise. It may name either a directory or
// a single suite file.
//
// A directory is searched recursively for files named *.suite.yaml. A
// directory entry whose name begins with "." is not descended into, so a
// repository root does not drag version-control and tooling directories into
// the scan.
//
// A root naming an existing file is returned as a one-element list, whatever
// its name — a user who points the tool at a suite gets that suite.
//
// A root that does not exist, is unreadable, or contains no suite yields an
// empty list and no error: "no suites here" is a legitimate answer the
// frontend already renders, not a startup failure.
//
// Results are sorted so the selection list is stable across invocations.
func discoverSuites(root string) []string {
	info, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return []string{root}
	}

	var results []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if path != root && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".suite.yaml") {
			results = append(results, path)
		}
		return nil
	})

	sort.Strings(results)
	return results
}
