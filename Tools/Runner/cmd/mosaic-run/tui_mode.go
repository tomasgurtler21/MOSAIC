package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"mosaic-run/internal/debuglog"
	"mosaic-run/internal/dispatchlog"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui"
	"mosaic-run/internal/tui/screens/runconfig"
)

// runTUIMode launches the interactive TUI frontend. All session dependencies are
// constructed here; the TUI's ProgramRef provides the Interaction port and the
// TUIDeviationResolver handles deviation resolution through the TUI's deviation screen.
//
// devMode enables the test-mode flow in the TUI (DevMode field on tui.Options).
// When true, a "Run Tests" entry point is visible in the TUI; when false, the
// test flow is hidden.
func runTUIMode(args []string, devMode bool) {
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Construct the process-level debug logger once here, before any other
	// operation, so that failures occurring before run identity is resolved are
	// still captured. The logger is shared across all session constructions (the
	// factory runs more than once per process), ensuring exactly one log file per run.
	logger := debuglog.New(workDir)
	logger.SetToolVersion(ToolVersion)
	defer logger.Close()

	// Construct the process-level dispatch logger once here, shared across all
	// session constructions, ensuring exactly one dispatch log file per process/run.
	dispLogger := dispatchlog.New(workDir)
	dispLogger.SetToolVersion(ToolVersion)
	defer dispLogger.Close()

	// Pre-scan --executable-path so it is available to the session factory.
	execPathTUI := scanFlag(args, "--executable-path")

	programRef := tui.NewProgramRef()

	// The graceful-stop flag is constructed exactly once per process, here,
	// alongside the loggers and the ProgramRef and for the same reason: the
	// session factory runs more than once per process (eager placeholder
	// construction, config-screen completion, exec-override retry, done-screen
	// continue), and every rebuilt session must observe the flag the TUI arms.
	// Constructing it inside the factory would leave each rebuilt session on
	// its own orphan flag.
	stopSignal := session.NewStopSignal()

	// minter mints run identity for new runs created from inside the TUI (run-select
	// screen's "new run" choice). It is also used as the defensive fallback inside
	// the session factory when an unresolved run folder is encountered.
	minter := newTUIRunIdentityMinter(workDir)

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

	// Assemble the interactive composition. The seam is the single place where
	// the interactive session.Deps and the tui.Options are constructed, so the
	// stop signal reaches both consumers from one source. Nothing below adds to
	// either value.
	wiring := buildInteractiveWiring(interactiveWiringInput{
		ExecutablePath: execPathTUI,
		ProgramRef:     programRef,
		Minter:         minter,
		Identity:       identity,
		StopSignal:     stopSignal,
		Debug:          logger,
		DispatchLog:    dispLogger,
		Clock:          &realClock{},
		DevMode:        devMode,
		// The run-id association needs SetRunID on the two concrete loggers,
		// which are in scope here and not inside the seam.
		OnRunIDResolved: func(runID string) {
			logger.SetRunID(runID)
			dispLogger.SetRunID(runID)
		},
	})

	// Construct the initial session using the resolved identity (or placeholder for multi-candidate).
	// Harness config is not yet known (config screen has not run); defaults to fake adapter.
	// Built through the seam's own factory so no second construction path exists.
	initSess := wiring.Options.SessionFactory(identity.RunFolder, identity.IsNewRun, "", runconfig.ConfigSelection{})

	ctx := context.Background()
	if err := tui.Run(ctx, initSess, wiring.Options); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
