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

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/runscan"
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

	// CLI mode: pre-scan flags needed for dependency wiring before cobra parses them,
	// then resolve run identity (run_id, run folder, is-new-run) before constructing
	// the session. Resolving run identity here ensures the session's ArtifactStore is
	// wired to the correct run-scoped Orchestration.md path from the start.
	orchFile := scanFlag(args, "--orchestrator-file")
	onDeviationStr := scanFlag(args, "--on-deviation")
	if onDeviationStr == "" {
		onDeviationStr = "delegate" // matches the flag default
	}
	harnessStr := scanFlag(args, "--harness")
	if harnessStr == "" {
		harnessStr = "fake" // matches the flag default
	}
	timeoutStr := scanFlag(args, "--timeout")
	if timeoutStr == "" {
		timeoutStr = "30m" // matches the flag default
	}
	claudePathStr := scanFlag(args, "--claude-path")
	if claudePathStr == "" {
		claudePathStr = "claude" // matches the flag default
	}

	// Parse the timeout duration; fall back to 30 minutes on invalid input
	// (cli.Run will surface the parse error to the user with ExitUsage).
	invocationTimeout := 30 * time.Minute
	if d, err := time.ParseDuration(timeoutStr); err == nil && d > 0 {
		invocationTimeout = d
	}

	// Resolve run identity and construct the store from the run-scoped path.
	// This pre-scan mirrors the --orchestrator-file and --on-deviation pre-scans:
	// it reads the same flags (--run / --new-run) that cli.Run will parse via cobra,
	// so both this site and cli.Run converge on the same run folder.
	// When cli.Run receives a non-nil identity, it skips its own resolution step.
	runIdentity, store, identErr := resolveRunIdentityForCLI(args)
	if identErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", identErr)
		os.Exit(2)
	}

	// Build the CLI Interaction port. The same instance is used as the session's
	// Interaction (for per-step progress and notices) and writes to os.Stdout.
	interact := cli.NewInteraction(os.Stdout)

	// Build the harness adapter based on the --harness flag.
	// When --harness=claude-code, construct the real adapter with the configured
	// executable path and timeout. The fake adapter is the default and is used
	// for development/testing without an actual Claude Code CLI.
	h := buildAdapter(harnessStr, claudePathStr, invocationTimeout)

	// Build the deviation resolver based on the --on-deviation flag.
	artifactPath := filepath.Join(runIdentity.RunFolder, "Orchestration.md")
	var dev domain.DeviationResolver
	switch onDeviationStr {
	case "stop":
		dev = &stopDeviationResolver{}
	default: // "delegate"
		if orchFile != "" {
			// Use the OrchestratorDelegate when an orchestrator file is provided.
			// The same harness instance (h) is used for both the session dispatch
			// and the orchestrator deviation resolution.
			orchDir := orchFileDir(orchFile)
			var orchSeq int
			dev = &deviation.OrchestratorDelegate{
				Harness: h,
				Orchestrator: domain.AgentReference{
					Identifier:     "orchestrator-script",
					DefinitionPath: orchDir + "/orchestrator-script.md",
					InvocationKind: domain.InvocationOrchestrator,
				},
				ArtifactPath: artifactPath,
				NextSeq:      func() int { orchSeq++; return orchSeq },
			}
		} else {
			// No orchestrator file known at startup: fall back to stop mode.
			dev = &stopDeviationResolver{}
		}
	}

	// Wire the session with the resolved run-scoped store and all port dependencies.
	// The store path matches runIdentity.RunFolder, so session I/O and the COMPLETED
	// marker write both target the same Orchestration-{run_id}/Orchestration.md file.
	sess := session.New(session.Deps{
		Harness:   h,
		Store:     store,
		Deviation: dev,
		Clock:     &realClock{},
		Interact:  interact,
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

	// Pre-scan --claude-path so it is available to the session factory.
	claudePathTUI := scanFlag(args, "--claude-path")
	if claudePathTUI == "" {
		claudePathTUI = "claude"
	}

	programRef := tui.NewProgramRef()

	// TUIDeviationResolver routes deviation decisions through the TUI's deviation
	// screen. Delegate starts nil (orchestrator file and harness are not known until
	// setup completes). The sessFactory wires Delegate when the user picks
	// "Claude Code CLI" in the config screen and an orchestrator file is known.
	tuiDev := &tui.TUIDeviationResolver{
		Program: programRef,
	}

	// SessionFactory builds the session with the run-scoped artifact store and the
	// harness adapter selected in the config screen. orchFile is the path to the
	// orchestrator agent file (empty when called before the file screen completes).
	// cfg carries the harness selection and timeout from the config screen.
	sessFactory := func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
		// Build the harness adapter based on the user's selection.
		h := buildAdapter(cfg.Harness, claudePathTUI, cfg.Timeout)

		// Wire OrchestratorDelegate into tuiDev when using the real adapter and
		// the orchestrator file path is known (entered in the file screen).
		// This enables deviation resolution through the real Claude Code CLI.
		// The same harness instance (h) is used for both session dispatch and
		// orchestrator invocation.
		tuiDev.Delegate = buildTUIDelegate(h, cfg.Harness, orchFile, runFolder)

		artifactPath := "Orchestration.md"
		if runFolder != "" {
			artifactPath = filepath.Join(runFolder, "Orchestration.md")
		}
		store := artifact.NewFileStore(artifactPath)
		return session.New(session.Deps{
			Harness:   h,
			Store:     store,
			Deviation: tuiDev,
			Clock:     &realClock{},
			Interact:  programRef,
		})
	}

	// Resolve run identity from --run / --new-run flags.
	runIDFlag := scanFlag(args, "--run")
	isNewRunFlag := scanBoolFlag(args, "--new-run")

	if runIDFlag != "" && isNewRunFlag {
		fmt.Fprintf(os.Stderr, "error: --run and --new-run are mutually exclusive\n")
		os.Exit(2)
	}

	var resolvedRunID, resolvedRunFolder string
	var isNewRun bool
	var scanResult *runscan.ScanResult

	switch {
	case runIDFlag != "":
		// --run <run_id>: validate format and resolve the run folder.
		if !domain.IsValidRunID(runIDFlag) {
			fmt.Fprintf(os.Stderr, "error: invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}\n", runIDFlag)
			os.Exit(2)
		}
		resolvedRunID = runIDFlag
		resolvedRunFolder = filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
		isNewRun = false

	case isNewRunFlag:
		// --new-run: identity will be minted by the session factory (runFolder="").
		resolvedRunID = ""
		resolvedRunFolder = ""
		isNewRun = true

	default:
		// Neither flag: scan the working directory for resumable candidates.
		scanner := runscan.NewDirScanner()
		result, scanErr := scanner.Scan(workDir)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "error: scanning for runs: %v\n", scanErr)
			os.Exit(1)
		}
		switch len(result.Candidates) {
		case 0:
			// No candidates: new run (identity minted by session factory).
			isNewRun = true
		case 1:
			// Single candidate: auto-resume.
			resolvedRunID = result.Candidates[0].RunID
			resolvedRunFolder = result.Candidates[0].FolderPath
			isNewRun = false
		default:
			// Multiple candidates: show the RunSelectScreen.
			scanResult = &result
		}
	}

	// Construct the initial session using the resolved identity (or placeholder for multi-candidate).
	// Harness config is not yet known (config screen has not run); defaults to fake adapter.
	initialFolder := resolvedRunFolder
	initSess := sessFactory(initialFolder, isNewRun, "", screens.ConfigSelection{})

	ctx := context.Background()
	if err := tui.Run(ctx, initSess, tui.Options{
		Interaction:      programRef,
		ScanResult:       scanResult,
		ResolvedRunID:    resolvedRunID,
		IsNewRun:         isNewRun,
		InitialRunFolder: resolvedRunFolder,
		SessionFactory:   sessFactory,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

// resolveRunIdentityForCLI pre-scans --run / --new-run and the working directory
// to determine the run folder before the session is constructed. It mirrors the
// pre-scan pattern used for --orchestrator-file and --on-deviation.
//
// When --run <run_id> is present, the folder is Orchestration-{run_id} under the
// working directory. When --new-run is present, a new run_id is minted here so
// that the same id is used by both the session's store and cli.Run's RunConfig.
// When neither flag is present, the working directory is scanned for resumable
// candidates; zero candidates mints a new run_id, one candidate uses that folder,
// and multiple candidates return an error (the multi-candidate rejection cannot be
// deferred to cli.Run because a non-nil identity skips cli.Run's internal check).
func resolveRunIdentityForCLI(args []string) (*cli.RunIdentity, domain.ArtifactStore, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("getting working directory: %w", err)
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
		// confirm the run is completed.
		if state, parseErr := artifact.Parse(data); parseErr == nil {
			if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
				return nil, nil, fmt.Errorf("run %s is completed and cannot be resumed", runIDFlag)
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
		// Neither flag: scan the working directory for resumable candidates.
		scanner := runscan.NewDirScanner()
		result, scanErr := scanner.Scan(workDir)
		if scanErr != nil {
			return nil, nil, fmt.Errorf("scanning for runs: %w", scanErr)
		}
		switch len(result.Candidates) {
		case 0:
			// No candidates: mint a new run_id for a fresh run.
			newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
			runID = newID
			runFolder = filepath.Join(workDir, domain.RunScopedFolder(newID))
			isNewRun = true
		case 1:
			// Exactly one candidate: auto-resume that run.
			runID = result.Candidates[0].RunID
			runFolder = result.Candidates[0].FolderPath
			isNewRun = false
		default:
			// Multiple candidates: surface the ambiguity as an error.
			// A non-nil identity returned to cli.Run would bypass cli.Run's own
			// multi-candidate rejection check (non-nil identity skips all internal
			// resolution). Reject here so the production path behaves identically
			// to the nil-identity path that unit tests exercise.
			var sb strings.Builder
			sb.WriteString("multiple resumable runs found; use --run <run_id> to select one:")
			for _, c := range result.Candidates {
				sb.WriteString("\n  ")
				sb.WriteString(c.RunID)
			}
			return nil, nil, fmt.Errorf("%s", sb.String())
		}
	}

	identity := &cli.RunIdentity{
		RunID:     runID,
		RunFolder: runFolder,
		IsNewRun:  isNewRun,
	}
	store := artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md"))
	return identity, store, nil
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

// hasPositionalArg reports whether args contains at least one non-flag argument.
// Non-flag arguments are those that do not start with "-".
func hasPositionalArg(args []string) bool {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return true
		}
	}
	return false
}

// orchFileDir returns the directory containing the orchestrator file.
func orchFileDir(orchFile string) string {
	return filepath.Dir(orchFile)
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

// buildAdapter constructs the HarnessAdapter specified by harnessStr.
//
// When harnessStr is "claude-code", a ClaudeCodeAdapter is created with
// claudePathStr as the executable path and timeout as the invocation limit.
// A zero or negative timeout is treated as the default (30 minutes).
// For any other value (including "fake" and unknown strings), FakeAdapter is
// returned. Unknown values are not rejected here; cli.Run validates the
// --harness flag and surfaces usage errors for unknown values (AC3.8).
func buildAdapter(harnessStr, claudePathStr string, timeout time.Duration) domain.HarnessAdapter {
	switch harnessStr {
	case "claude-code":
		if timeout <= 0 {
			timeout = 30 * time.Minute
		}
		return harness.NewClaudeCodeAdapter(claudePathStr, timeout)
	default: // "fake" or unknown
		return harness.NewFakeAdapter()
	}
}

// buildTUIDelegate constructs the OrchestratorDelegate for TUI deviation
// resolution. Returns a non-nil delegate only when harnessStr is "claude-code"
// and orchFile is non-empty; otherwise returns nil (falling back to stop mode).
//
// The same harness instance h must be the one used for both session dispatch
// and orchestrator invocation, ensuring deviation resolution uses the same
// adapter as the primary session.
func buildTUIDelegate(h domain.HarnessAdapter, harnessStr, orchFile, runFolder string) *deviation.OrchestratorDelegate {
	if harnessStr != "claude-code" || orchFile == "" {
		return nil
	}
	orchDir := orchFileDir(orchFile)
	artifactPath := "Orchestration.md"
	if runFolder != "" {
		artifactPath = filepath.Join(runFolder, "Orchestration.md")
	}
	var orchSeq int
	return &deviation.OrchestratorDelegate{
		Harness: h,
		Orchestrator: domain.AgentReference{
			Identifier:     "orchestrator-script",
			DefinitionPath: orchDir + "/orchestrator-script.md",
			InvocationKind: domain.InvocationOrchestrator,
		},
		ArtifactPath: artifactPath,
		NextSeq:      func() int { orchSeq++; return orchSeq },
	}
}

// realClock provides the current UTC time.
type realClock struct{}

func (c *realClock) Now() time.Time { return time.Now().UTC() }

// stopDeviationResolver is a DeviationResolver that always stops the run.
// Used for --on-deviation=stop and as a fallback when the orchestrator delegate
// cannot be wired (no orchestrator file provided at startup).
type stopDeviationResolver struct{}

func (r *stopDeviationResolver) Resolve(_ context.Context, info domain.DeviationInfo) (domain.RejoinInstruction, error) {
	reason := fmt.Sprintf("deviation from %s (status %s): run stopped per --on-deviation setting",
		info.Response.AgentInstanceID, info.Response.StatusCode)
	return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: reason}}, nil
}
