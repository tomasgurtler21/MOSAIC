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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/harness/claudecode"
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

	return WiringConfig{
		HarnessID:       scanFlag(args, "--harness", claudecode.HarnessID),
		FixtureRoot:     scanFlag(args, "--fixtures", "fixtures"),
		WorkspaceRoot:   scanFlag(args, "--workspace-root", filepath.Join(os.TempDir(), "mosaic-agent-test-workspaces")),
		SelfPath:        selfPath,
		LoggerBundleDir: filepath.Join(selfDir, "logger-bundle"),
		CostToolPath:    filepath.Join(selfDir, "mosaic-log-analyzer"+exeSuffix()),
		CostTimeout:     30 * time.Second,
		Diag:            os.Stderr, // never stdout: stdout carries the machine-readable report in --format json
	}
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
	d, err := buildDeps(resolveWiringConfig(args))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mosaic-agent-test: %v\n", err)
		return wiringFailureExitCode(err)
	}

	opts, err := tuiOptions(d, discoverSuites())
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
// frontend's suite-select screen. It looks in the invocation's own working
// directory, which is where a suite is authored and run from.
func discoverSuites() []string {
	matches, err := filepath.Glob("*.suite.yaml")
	if err != nil {
		return nil
	}
	return matches
}
