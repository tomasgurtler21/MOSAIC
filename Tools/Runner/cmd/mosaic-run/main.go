// Command mosaic-run is the entry point for the MOSAIC script-driven orchestration runner.
//
// When the "run" subcommand is provided in the arguments, the CLI frontend handles the
// invocation non-interactively. When no subcommand is provided and a terminal is attached
// to stdin and stdout (or when --tui is given), the TUI frontend is launched interactively.
//
// Dependency construction (harness, artifact store, deviation resolver, clock) is done here
// before dispatching to the chosen frontend. Each frontend receives a fully-wired session
// and never constructs its own infrastructure.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/debuglog"
	"mosaic-run/internal/dispatchlog"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"

	"github.com/mattn/go-isatty"
)

const ToolVersion = "1.3.0"

// wantsTUI reports whether mosaic-run should launch the interactive TUI.
// The TUI is launched when:
//
//	(a) --tui is given explicitly, OR
//	(b) no positional subcommand is present AND both stdin and stdout are
//	    attached to a real terminal (not a pipe, redirect, or CI environment).
//
// This mirrors the deployment tool's wantsTUI pattern to ensure consistent
// behaviour across mosaic-run and mosaic-deploy.
func wantsTUI(args []string) bool {
	if scanBoolFlag(args, "--tui") {
		return true
	}
	if hasPositionalArg(args) {
		return false
	}
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func main() {
	args := os.Args[1:]

	// Pre-scan --dev before any other processing. The --dev flag gates the test
	// subcommand and the TUI test flow. It is an entry-point-only flag (like
	// --tui): it is consumed here and must be stripped before cobra sees it,
	// since neither the run nor the test cobra subcommand registers it.
	devMode := scanBoolFlag(args, "--dev")
	cobraArgs := stripBoolFlag(args, "--dev")

	if wantsTUI(cobraArgs) {
		runTUIMode(cobraArgs, devMode)
		return
	}

	// When --dev is present and the first positional argument is "test", route to
	// the test subcommand entry point before any run-specific wiring (run identity
	// resolution, session construction, etc.). The test subcommand manages its own
	// dependency construction from its flags.
	if devMode && firstPositionalArg(cobraArgs) == "test" {
		os.Exit(runDevTestMode(cobraArgs))
	}

	os.Exit(runCLIMode(args, cobraArgs))
}

// runDevTestMode routes to the --dev test subcommand entry point. The test
// subcommand manages its own dependency construction from its flags, so no
// run-specific wiring (run identity resolution, session construction) happens
// before this call.
func runDevTestMode(cobraArgs []string) int {
	testWorkDir, wdErr := os.Getwd()
	if wdErr != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", wdErr)
		return 1
	}
	return cli.RunTestCommand(context.Background(), cobraArgs, testWorkDir, os.Stdout, os.Stderr)
}

// runCLIMode performs the non-interactive CLI frontend's dependency wiring and
// returns the process exit code. args is the full, unstripped argument list
// (used for pre-scans that must see --dev-adjacent flags exactly as main()
// received them); cobraArgs is the --dev-stripped list handed to cli.Run.
func runCLIMode(args, cobraArgs []string) int {
	// Resolve the working directory as early as possible so the debug logger can
	// be constructed before any other operation, capturing failures that occur
	// before run identity is known.
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", err)
		return 1
	}

	// Construct the process-level debug logger. One logger per process; the file
	// is created lazily on first use so that a failed run that never reaches the
	// store still produces a log entry. Closed via defer so writes are flushed
	// before the process exits along non-os.Exit paths.
	logger := debuglog.New(workDir)
	logger.SetToolVersion(ToolVersion)
	defer logger.Close()

	// Construct the process-level dispatch logger. One logger per process;
	// records every subagent ProtocolRequest/ProtocolResponse pair as JSONL.
	// File is created lazily on first use and closed via defer.
	dispLogger := dispatchlog.New(workDir)
	dispLogger.SetToolVersion(ToolVersion)
	defer dispLogger.Close()

	// CLI mode: pre-scan flags needed for dependency wiring before cobra parses them,
	// then resolve run identity (run_id, run folder, is-new-run) before constructing
	// the session. Resolving run identity here ensures the session's ArtifactStore is
	// wired to the correct run-scoped Orchestration.md path from the start.
	harnessStr := scanFlag(args, "--harness")
	if harnessStr == "" {
		harnessStr = "fake" // matches the flag default
	}
	timeoutStr := scanFlag(args, "--timeout")
	if timeoutStr == "" {
		timeoutStr = "30m" // matches the flag default
	}
	execPathStr := scanFlag(args, "--executable-path")

	// Pre-scan the flags that select which consultants are wired before the session
	// is constructed. These mirror the cobra flag defaults: --mode has no default
	// (the session will refuse an absent mode), --manual-resolution defaults to false,
	// and --pre-consult defaults to true. The pre-scan happens here so that buildDeps
	// can wire the correct consultant types into session.Deps before session.New is called.
	modeStr := scanFlag(args, "--mode")
	manualResolution := scanBoolFlag(args, "--manual-resolution")
	preConsult := preConsultFromArgs(args)

	// Parse the timeout duration; fall back to 30 minutes on invalid input
	// (cli.Run will surface the parse error to the user with ExitUsage).
	invocationTimeout := 30 * time.Minute
	if d, err := time.ParseDuration(timeoutStr); err == nil && d > 0 {
		invocationTimeout = d
	}

	// Resolve run identity. The working directory is passed explicitly to avoid a
	// redundant os.Getwd call. The store return value is always nil; the
	// authoritative store is constructed below with the process logger.
	runIdentity, _, identErr := resolveRunIdentityForCLI(args, workDir)
	if identErr != nil {
		logger.Log(domain.EventRunnerError, identErr.Error())
		fmt.Fprintf(os.Stderr, "error: %v\n", identErr)
		return 2
	}

	// Associate the run_id with both log files now that identity is resolved.
	logger.SetRunID(runIdentity.RunID)
	dispLogger.SetRunID(runIdentity.RunID)

	// Build the artifact store with the process logger so that path anomalies
	// (non-absolute or non-run-scoped paths) are captured in the debug log.
	store := newLoggedArtifactStore(filepath.Join(runIdentity.RunFolder, "Orchestration.md"), logger)

	// Build the CLI Interaction port. The same instance is used as the session's
	// Interaction (for per-step progress and notices) and writes to os.Stdout.
	interact := cli.NewInteraction(os.Stdout)

	// Pre-scan the GHCP CLI permission mode flag. Only relevant when
	// --harness=ghcp-cli; ignored for other harnesses. The flag is optional;
	// an absent or unrecognised value defaults to blanket mode inside buildAdapter.
	ghcpPermissionMode := scanFlag(args, "--ghcp-permission-mode")

	// FR-8: Reject a GHCP CLI run without a resolved mode before spawning.
	// In CLI mode the flag must be supplied when the harness is ghcp-cli.
	if harnessStr == commonharness.HarnessIDGHCPCLI && ghcpPermissionMode == "" {
		fmt.Fprintf(os.Stderr, "error: --ghcp-permission-mode is required when --harness=ghcp-cli (accepted values: blanket, allowlist)\n")
		return 2
	}
	if ghcpPermissionMode != "" && ghcpPermissionMode != "blanket" && ghcpPermissionMode != "allowlist" {
		fmt.Fprintf(os.Stderr, "error: --ghcp-permission-mode must be \"blanket\" or \"allowlist\", got %q\n", ghcpPermissionMode)
		return 2
	}

	// Build the harness adapter via buildAdapter, passing the process logger
	// so that invocation I/O is captured in the debug log.
	h := buildAdapter(harnessStr, execPathStr, ghcpPermissionMode, invocationTimeout, logger)

	// Extract the raw-JSON transport if the selected harness adapter implements it.
	// Production adapters implement both HarnessAdapter and RawInvoker over the same
	// spawner; adapters that do not yet implement RawInvoker yield nil here, so the
	// consultation system degrades gracefully until the adapter is extended.
	var rawInvoker domain.RawInvoker
	if ri, ok := h.(domain.RawInvoker); ok {
		rawInvoker = ri
	}

	// Build the consultation routing deps from the pre-scanned flags. The orchestrator
	// reference and routing table are not known at process startup; the session hands
	// them to every consultant that implements domain.RunContextBinder at run start.
	cliSettings := domain.RunSettings{
		Mode:             domain.ExecutionMode(modeStr),
		ManualResolution: manualResolution,
		PreConsultation:  preConsult,
	}
	routingDeps := buildDeps(cliSettings, rawInvoker, interact, artifact.NewApprovalReader(), dispLogger)

	// Wire the session with the resolved run-scoped store and all port dependencies.
	// The store path matches runIdentity.RunFolder, so session I/O and the COMPLETED
	// marker write both target the same Orchestration-{run_id}/Orchestration.md file.
	sess := session.New(session.Deps{
		Harness:     h,
		Store:       store,
		Clock:       &realClock{},
		Interact:    interact,
		Debug:       logger,
		DispatchLog: dispLogger,
		Routing:     routingDeps.Routing,
		Manual:      routingDeps.Manual,
		PreConsult:  routingDeps.PreConsult,
		Approvals:   routingDeps.Approvals,
		Outputs:     artifact.NewOutputWriteDetector(),
	})

	// Pass the pre-resolved store and identity so that cli.Run skips its own
	// resolution step and uses the same run folder that was used to wire the session.
	// Use cobraArgs (not args) so the entry-point-only --dev flag does not reach cobra.
	return cli.Run(context.Background(), cobraArgs, store, runIdentity, sess, os.Stdout, os.Stderr)
}
