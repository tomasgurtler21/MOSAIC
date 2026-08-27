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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/debuglog"
	"mosaic-run/internal/dispatchlog"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui"
	"mosaic-run/internal/tui/screens"
)

// wantsTUI reports whether mosaic-run should launch the interactive TUI.
// The TUI is launched when:
//   (a) --tui is given explicitly, OR
//   (b) no positional subcommand is present AND both stdin and stdout are
//       attached to a real terminal (not a pipe, redirect, or CI environment).
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

	if wantsTUI(args) {
		runTUIMode(args)
		return
	}

	// Resolve the working directory as early as possible so the debug logger can
	// be constructed before any other operation, capturing failures that occur
	// before run identity is known.
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Construct the process-level debug logger. One logger per process; the file
	// is created lazily on first use so that a failed run that never reaches the
	// store still produces a log entry. Closed via defer so writes are flushed
	// before the process exits along non-os.Exit paths.
	logger := debuglog.New(workDir)
	defer logger.Close()

	// Construct the process-level dispatch logger. One logger per process;
	// records every subagent ProtocolRequest/ProtocolResponse pair as JSONL.
	// File is created lazily on first use and closed via defer.
	dispLogger := dispatchlog.New(workDir)
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
	claudePathStr := scanFlag(args, "--claude-path")

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
		os.Exit(2)
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

	// Build the harness adapter via buildAdapter, passing the process logger
	// so that invocation I/O is captured in the debug log.
	h := buildAdapter(harnessStr, claudePathStr, invocationTimeout, logger)

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
	routingDeps := buildDeps(cliSettings, rawInvoker, interact, artifact.NewApprovalReader())

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
	})

	// Pass the pre-resolved store and identity so that cli.Run skips its own
	// resolution step and uses the same run folder that was used to wire the session.
	os.Exit(cli.Run(context.Background(), args, store, runIdentity, sess, os.Stdout, os.Stderr))
}

// runTUIMode launches the interactive TUI frontend. All session dependencies are
// constructed here; the TUI's ProgramRef provides the Interaction port and the
// TUIDeviationResolver handles deviation resolution through the TUI's deviation screen.
func runTUIMode(args []string) {
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Construct the process-level debug logger once here, before any other
	// operation, so that failures occurring before run identity is resolved are
	// still captured. The logger is shared across all sessFactory calls (sessFactory
	// runs more than once per process), ensuring exactly one log file per run.
	logger := debuglog.New(workDir)
	defer logger.Close()

	// Construct the process-level dispatch logger once here, shared across all
	// sessFactory calls, ensuring exactly one dispatch log file per process/run.
	dispLogger := dispatchlog.New(workDir)
	defer dispLogger.Close()

	// Pre-scan --claude-path so it is available to the session factory.
	claudePathTUI := scanFlag(args, "--claude-path")

	programRef := tui.NewProgramRef()

	// minter mints run identity for new runs created from inside the TUI (run-select
	// screen's "new run" choice). It is also used as the defensive fallback inside
	// sessFactory when an unresolved run folder is encountered.
	minter := newTUIRunIdentityMinter(workDir)

	// SessionFactory builds the session with the run-scoped artifact store and the
	// harness adapter selected in the config screen. orchFile is the path to the
	// orchestrator agent file (empty when called before the file screen completes);
	// it is accepted for interface compatibility but not currently used within this factory.
	// cfg carries the harness selection and timeout from the config screen.
	//
	// The process logger (defined above) is closed over and shared across all calls.
	// sessFactory is invoked more than once per process (once eagerly with a
	// placeholder config and again when the config screen completes), so the logger
	// must not be constructed here — doing so would produce multiple log files.
	sessFactory := func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
		// Build the harness adapter via buildAdapter, passing the process logger
		// so that invocation I/O is captured in the debug log. buildAdapter
		// handles the timeout default and the logger injection internally.
		// cfg.ExecutablePath is set when the user confirms an override on the
		// exec-override screen. It wins over the pre-scanned claudePathTUI so
		// that retrying with a different path actually takes effect.
		execPath := cfg.ExecutablePath
		if execPath == "" {
			execPath = claudePathTUI
		}
		h := buildAdapter(cfg.Harness, execPath, cfg.Timeout, logger)

		artifactPath, err := resolveTUIArtifactPath(runFolder)
		if errors.Is(err, errUnresolvedRunFolder) {
			// Defensive branch: an unresolved run folder is a contract violation
			// by the caller. Mint a fresh identity, write a notice, and build the
			// store at the minted scoped path rather than a bare CWD-relative path.
			_, mintedFolder := minter()
			logger.Log(domain.EventRunnerError, "run folder unresolved; minting new run",
				domain.F("path", mintedFolder))
			fmt.Fprintf(os.Stderr, "notice: run folder unresolved; minting new run at %s\n", mintedFolder)
			artifactPath = filepath.Join(mintedFolder, "Orchestration.md")
		}
		store := newLoggedArtifactStore(artifactPath, logger)

		// Extract the raw-JSON transport if the selected harness adapter implements it.
		var rawInvoker domain.RawInvoker
		if ri, ok := h.(domain.RawInvoker); ok {
			rawInvoker = ri
		}

		// Build the session's routing consultant, manual resolver, pre-consultation
		// capability and approval reader from the completed configuration selection.
		// This is the same builder the non-interactive path uses, so both frontends
		// share one place where consultant selection is expressed. The orchestrator
		// reference and routing table are not known here; they reach every consultant
		// that implements domain.RunContextBinder later, on the session's run-start path.
		routingDeps := buildDeps(cfg.Settings, rawInvoker, programRef, artifact.NewApprovalReader())

		return session.New(session.Deps{
			Harness:     h,
			Store:       store,
			Clock:       &realClock{},
			Interact:    programRef,
			Debug:       logger,
			DispatchLog: dispLogger,
			Routing:     routingDeps.Routing,
			Manual:      routingDeps.Manual,
			PreConsult:  routingDeps.PreConsult,
			Approvals:   routingDeps.Approvals,
		})
	}

	// Resolve run identity from flags and working-directory scan.
	identity, identErr := resolveRunIdentityForTUI(args, workDir)
	if identErr != nil {
		logger.Log(domain.EventRunnerError, identErr.Error())
		if errors.Is(identErr, errTUIUsage) {
			fmt.Fprintf(os.Stderr, "error: %v\n", identErr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", identErr)
		os.Exit(1)
	}

	// Associate the run_id with both log files if identity is already resolved
	// (single-candidate auto-resume, --run flag, or --new-run flag). When
	// identity is deferred to the run-select screen (multi-candidate), the
	// run_id will be associated via a separate SetRunID call once selected.
	if identity.RunID != "" {
		logger.SetRunID(identity.RunID)
		dispLogger.SetRunID(identity.RunID)
	}

	// Construct the initial session using the resolved identity (or placeholder for multi-candidate).
	// Harness config is not yet known (config screen has not run); defaults to fake adapter.
	initSess := sessFactory(identity.RunFolder, identity.IsNewRun, "", screens.ConfigSelection{})

	ctx := context.Background()
	if err := tui.Run(ctx, initSess, tui.Options{
		Interaction:            programRef,
		Selection:              identity.Selection,
		ScanResult:             identity.ScanResult,
		ResolvedRunID:          identity.RunID,
		IsNewRun:               identity.IsNewRun,
		InitialRunFolder:       identity.RunFolder,
		SessionFactory:         sessFactory,
		MintRunIdentity:        minter,
		OrchestratorDiscoverer: harness.DiscoverOrchestrator,
		ArtifactStoreFactory: func(runFolder string) domain.ArtifactStore {
			return newLoggedArtifactStore(filepath.Join(runFolder, "Orchestration.md"), logger)
		},
		Clock: &realClock{},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

// resolveRunIdentityForCLI pre-scans --run / --new-run and the working directory
// to determine the run folder before the session is constructed. It mirrors the
// pre-scan pattern used for --mode and --manual-resolution.
//
// When --run <run_id> is present, the folder is Orchestration-{run_id} under the
// working directory. When --new-run is present, a new run_id is minted here so
// that the same id is used by both the session's store and cli.Run's RunConfig.
// When neither flag is present, the working directory is scanned for resumable
// candidates; zero candidates mints a new run_id, one candidate uses that folder,
// and multiple candidates return an error (the multi-candidate rejection cannot be
// deferred to cli.Run because a non-nil identity skips cli.Run's internal check).
//
// The second return value is always nil. Callers are responsible for constructing
// the artifact store after identity is resolved, using the process logger so that
// path anomalies are captured in the debug log. The nil return preserves the
// three-value signature for call-site compatibility.
//
// An optional workDir may be passed as the last argument to avoid a redundant
// os.Getwd syscall when the caller has already resolved the working directory.
// When omitted, os.Getwd is called internally.
func resolveRunIdentityForCLI(args []string, workDirs ...string) (*cli.RunIdentity, domain.ArtifactStore, error) {
	var workDir string
	if len(workDirs) > 0 {
		workDir = workDirs[0]
	} else {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("getting working directory: %w", err)
		}
	}

	runIDFlag := scanFlag(args, "--run")
	isNewRunFlag := scanBoolFlag(args, "--new-run")

	// Refuse --input together with --run before any run-folder access. This check
	// must precede the switch so the refusal cannot come from reading Orchestration.md.
	if hasFlag(args, "--input") && runIDFlag != "" {
		return nil, nil, fmt.Errorf("--input and --run are mutually exclusive")
	}

	var runID, runFolder string
	var isNewRun bool
	var position *runselect.Position

	switch {
	case runIDFlag != "":
		// --run <run_id>: validate format, verify the run folder exists on disk,
		// and reject completed runs. These checks must be done here rather than
		// deferred to cli.Run, because cli.Run skips its own resolution step
		// whenever a non-nil identity is supplied (which is always the case in
		// production). Omitting them here would silently bypass AC5.3.
		if !domain.IsValidRunID(runIDFlag) {
			return nil, nil, fmt.Errorf("invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}", runIDFlag)
		}
		folderPath := filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
		artifactPath := filepath.Join(folderPath, "Orchestration.md")
		data, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("no run found with id %s", runIDFlag)
			}
			return nil, nil, fmt.Errorf("reading run artifact for %s: %w", runIDFlag, readErr)
		}
		// Treat parse errors as resumable: the session layer will surface real
		// format problems when it calls store.Read. Only reject when we can
		// confirm the run is completed. A successful parse also yields the
		// recorded position directly, so cli.Run's announcement need not read
		// the artifact a second time.
		if state, parseErr := artifact.Parse(data); parseErr == nil {
			if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
				return nil, nil, fmt.Errorf("run %s is completed and cannot be resumed", runIDFlag)
			}
			position = &runselect.Position{
				Phase:       state.CurrentState.Phase,
				Stage:       state.CurrentState.Stage,
				LastAgent:   state.CurrentState.LastAgent,
				LastUpdated: state.LastUpdated,
			}
		}
		runID = runIDFlag
		runFolder = folderPath
		isNewRun = false

	case isNewRunFlag:
		// --new-run: mint the run_id here so both the session's store and the
		// RunConfig use the same path. cli.Run receives the identity and skips
		// its own mint.
		newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		runID = newID
		runFolder = filepath.Join(workDir, domain.RunScopedFolder(newID))
		isNewRun = true

	default:
		// Neither flag: the selection is never inferred from what the
		// workspace happens to contain, whatever that is -- zero candidates
		// included (Plan.md: "Minting a new run because none existed is
		// exactly the inference this stage removes."). Scan the working
		// directory and ask runselect for the decision; a non-nil identity
		// returned to cli.Run would bypass cli.Run's own resolution step, so
		// the refusal must happen here rather than being deferred (AC2.2,
		// AC2.6, AC2.9).
		scanner := runscan.NewDirScanner()
		result, scanErr := scanner.Scan(workDir)
		if scanErr != nil {
			return nil, nil, fmt.Errorf("scanning for runs: %w", scanErr)
		}
		dec, resErr := runselect.Resolve(runselect.Request{Scan: result, WorkDir: workDir}, mainMinter(workDir))
		if resErr != nil {
			return nil, nil, resErr
		}
		if dec.Question != nil {
			return nil, nil, fmt.Errorf("%s", formatSelectionRefusal(*dec.Question))
		}
		runID = dec.Resolved.RunID
		runFolder = dec.Resolved.RunFolder
		isNewRun = dec.Resolved.IsNewRun
		position = dec.Resolved.Position
	}

	identity := &cli.RunIdentity{
		RunID:     runID,
		RunFolder: runFolder,
		IsNewRun:  isNewRun,
		Position:  position,
	}
	// Return nil for the store. The caller constructs the authoritative store
	// via newLoggedArtifactStore with the process logger, ensuring path anomalies
	// are captured in the debug log. Store construction here would require a
	// no-op logger (process logger not in scope) and would be immediately
	// discarded at the call site anyway.
	return identity, nil, nil
}

// scanBoolFlag reports whether a boolean flag (e.g. "--tui") appears anywhere in args.
func scanBoolFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// boolFlagState is the tri-state outcome of pre-scanning a boolean flag, needed
// because a flag whose cobra default is true cannot be pre-scanned with a
// two-state helper: "absent" and "explicitly false" must be distinguishable.
type boolFlagState int

const (
	boolFlagAbsent boolFlagState = iota // flag not present in args
	boolFlagTrue                        // "--flag" or "--flag=true"
	boolFlagFalse                       // "--flag=false"
)

// scanBoolFlagState classifies a boolean flag's presence in args, understanding
// the bare "--flag" form and the "--flag=true"/"--flag=false" forms. It mirrors
// cobra's boolean flag parsing for pre-scan purposes only.
func scanBoolFlagState(args []string, flag string) boolFlagState {
	prefix := flag + "="
	for _, arg := range args {
		if arg == flag {
			return boolFlagTrue
		}
		if strings.HasPrefix(arg, prefix) {
			val := strings.ToLower(strings.TrimPrefix(arg, prefix))
			if val == "false" {
				return boolFlagFalse
			}
			return boolFlagTrue
		}
	}
	return boolFlagAbsent
}

// scanBoolFlagDefault is the default-aware version of scanBoolFlag. It returns
// def when the flag is absent, true for the bare flag form, and honours
// --flag=true / --flag=false forms explicitly.
//
// Every boolean pre-scan in this file must pass the same def as the cobra flag
// declaration's default, so the pre-scan and the parsed flag cannot disagree.
func scanBoolFlagDefault(args []string, flag string, def bool) bool {
	switch scanBoolFlagState(args, flag) {
	case boolFlagTrue:
		return true
	case boolFlagFalse:
		return false
	default: // boolFlagAbsent
		return def
	}
}

// preConsultFromArgs resolves the effective pre-consultation setting from args,
// applying the same default (true) that the --pre-consult cobra flag declaration
// uses. This is the call site whose default literal pins agreement: any change
// to the cobra default that does not also update this call is caught by the
// TestPreConsultFromArgs_* tests.
func preConsultFromArgs(args []string) bool {
	return scanBoolFlagDefault(args, "--pre-consult", true)
}

// hasFlag reports whether args contains the named flag in either the
// "--flag value" or "--flag=value" form. Unlike scanFlag it returns no value,
// and unlike scanBoolFlag it matches the "--flag=value" form; it exists for
// flags whose mere presence is decisive during the pre-scan.
func hasFlag(args []string, flag string) bool {
	prefix := flag + "="
	for _, arg := range args {
		if arg == flag {
			return true
		}
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

// hasPositionalArg reports whether args contains at least one genuine positional
// argument — a token that is neither a flag nor the value of a preceding
// value-bearing flag. The value-bearing set comes from cli.ValueBearingFlagNames().
//
// Scan rules (left to right):
//   - A token that starts with "-" and is in the value-bearing set consumes the
//     following token as its value; that value is not positional.
//   - A token that starts with "-" and contains "=" has its value embedded; no
//     following token is consumed.
//   - A token that starts with "-" otherwise (boolean flag or unknown flag)
//     consumes nothing.
//   - A token that does not start with "-" and was not consumed as a value is a
//     genuine positional argument.
func hasPositionalArg(args []string) bool {
	valueBearing := make(map[string]bool)
	for _, name := range cli.ValueBearingFlagNames() {
		valueBearing[name] = true
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue // consumed as the value of the preceding value-bearing flag
		}
		if !strings.HasPrefix(arg, "-") {
			return true // not a flag and not consumed as a value: genuine positional
		}
		// It is a flag token. If it contains "=" the value is embedded; if not
		// and it names a value-bearing flag, the next token is its value.
		if !strings.Contains(arg, "=") && valueBearing[arg] {
			skipNext = true
		}
	}
	return false
}

// scanFlag does a minimal pre-scan of args for a named flag. It understands both
// "--flag value" and "--flag=value" forms, consistent with cobra's flag parsing.
func scanFlag(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		prefix := flag + "="
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

// newLoggedArtifactStore builds the run's artifact store and records a debug
// entry when the store path is anomalous. It is the single owner of the
// artifact.path.* event family: no other function in any package emits those
// events, and the artifact package itself never logs.
//
// Behaviour is identical to calling artifact.NewFileStore(path) directly — the
// returned store is always non-nil and no path is ever rewritten, substituted
// or rejected here. Emission is a pure side effect.
//
// At most one event is emitted per call; the two conditions are mutually
// exclusive so a log reader can distinguish a hard failure from an informational
// note by event name alone.
func newLoggedArtifactStore(path string, logger domain.DebugLogger) domain.ArtifactStore {
	if !filepath.IsAbs(path) {
		// Non-absolute path: every subsequent Create call will return an error
		// and nothing will be written. Record this so the failure is diagnosable.
		logger.Log(domain.EventArtifactPathRejected, "artifact store path is not absolute",
			domain.F("path", path))
	} else if !artifact.IsRunScopedArtifactPath(path) {
		// Absolute but not under an Orchestration-{run_id} folder: artifacts will
		// be written, but the path is outside the expected run-scoped hierarchy.
		logger.Log(domain.EventArtifactPathNonRunScoped, "artifact store path is not run-scoped",
			domain.F("path", path))
	}
	return artifact.NewFileStore(path)
}

// buildAdapter constructs the HarnessAdapter specified by harnessStr.
//
// When harnessStr is "claude-code" or "opencode", the corresponding CLI
// adapter is created with claudePathStr as the executable path and timeout
// as the invocation limit. A zero or negative timeout is treated as the
// default (30 minutes). claudePathStr is named for the Claude Code CLI
// historically, but it names whichever harness CLI harnessStr selected: the
// opencode case reuses the same parameter, defaulting to the published
// "opencode" command name when empty.
// For any other value (including "fake" and unknown strings), FakeAdapter is
// returned. Unknown values are not rejected here; cli.Run validates the
// --harness flag and surfaces usage errors for unknown values (AC3.8).
//
// An optional logger may be passed as the last argument. When provided, the
// CLI adapter is constructed with the logger so that invocation I/O is
// captured in the debug log. When omitted, the adapter uses a no-op logger.
// The fake adapter ignores the logger in all cases.
func buildAdapter(harnessStr, claudePathStr string, timeout time.Duration, loggers ...domain.DebugLogger) domain.HarnessAdapter {
	var logger domain.DebugLogger = domain.NopDebugLogger{}
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	switch harnessStr {
	case commonharness.HarnessIDClaudeCode:
		exe := claudePathStr
		if exe == "" {
			exe = "claude"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return harness.NewClaudeCodeAdapterWithLogger(exe, timeout, logger)
	case commonharness.HarnessIDOpenCode:
		exe := claudePathStr
		if exe == "" {
			exe = "opencode"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return harness.NewOpenCodeAdapterWithLogger(exe, timeout, logger)
	case commonharness.HarnessIDGHCPCLI:
		exe := claudePathStr
		if exe == "" {
			exe = "copilot"
		}
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return harness.NewGHCPCLIAdapterWithLogger(exe, timeout, logger)
	default: // "fake" or unknown
		return harness.NewFakeAdapter()
	}
}

// mainMinter returns a runselect.Minter that mints a new run_id rooted at workDir.
func mainMinter(workDir string) runselect.Minter {
	return func() (string, string) {
		newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		return newID, filepath.Join(workDir, domain.RunScopedFolder(newID))
	}
}

// formatSelectionRefusal renders the non-interactive refusal message for an
// unsettled selection: every resumable run_id the caller may pass to --run,
// every unresumable run with the reason it cannot be resumed (AC2.5), and
// --new-run as the always-available way to start fresh (AC2.3, AC2.6). This
// mirrors internal/cli.formatSelectionRefusal; it is duplicated here rather
// than exported across the package boundary because cli.Run and
// resolveRunIdentityForCLI are independent resolution sites that share the
// runselect decision but not their output plumbing.
func formatSelectionRefusal(q runselect.Question) string {
	var resumable []string
	var unresumable []string
	for _, c := range q.Choices {
		switch c.Kind {
		case runselect.ChoiceResume:
			resumable = append(resumable, c.ID)
		case runselect.ChoiceUnresumable:
			unresumable = append(unresumable, fmt.Sprintf("%s (%s)", c.ID, c.Reason.Description()))
		}
	}
	var sb strings.Builder
	sb.WriteString("run selection is required; ")
	if len(resumable) > 0 {
		sb.WriteString("use --run <run_id> to resume one of: ")
		sb.WriteString(strings.Join(resumable, ", "))
		sb.WriteString(", or ")
	}
	sb.WriteString("use --new-run to start a new run")
	if len(unresumable) > 0 {
		sb.WriteString("; cannot be resumed: ")
		sb.WriteString(strings.Join(unresumable, ", "))
	}
	return sb.String()
}

// realClock provides the current UTC time.
type realClock struct{}

func (c *realClock) Now() time.Time { return time.Now().UTC() }

