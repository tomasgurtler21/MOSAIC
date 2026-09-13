package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui"
	"mosaic-run/internal/tui/screens"
)

// interactiveWiringInput carries the process-scoped values runTUIMode hoists
// and hands to the seam. Everything here is constructed once per process.
type interactiveWiringInput struct {
	// ClaudePath is the pre-scanned --claude-path value.
	ClaudePath string

	// ProgramRef is the Interaction port, shared by the TUI and the session.
	ProgramRef *tui.ProgramRef

	// Minter mints run identity for new runs created from inside the TUI, and
	// is the defensive fallback when an unresolved run folder is encountered.
	Minter tui.RunIdentityMinter

	// Identity is the resolved (or deliberately deferred) run identity.
	Identity tuiRunIdentity

	// StopSignal is the shared graceful-stop flag, constructed exactly once in
	// runTUIMode alongside the ProgramRef. The seam never constructs one:
	// passing nil is a programming error, not a normalised default. A default
	// here would recreate the orphan-signal defect the seam exists to fix.
	StopSignal *session.StopSignal

	// Debug is the process debug logger, reaching both the session and (from
	// the stop-observability work) the TUI, so one ordered log holds both
	// halves of the stop lifecycle. Deliberately typed as the narrow
	// domain.DebugLogger port: it declares Log and nothing else. Do not widen
	// it to a concrete logger or to a port carrying SetRunID.
	Debug domain.DebugLogger

	// DispatchLog is the process dispatch logger, shared for the same reason.
	DispatchLog domain.DispatchLogger

	// Clock provides timestamps to both the session and the TUI.
	Clock domain.Clock

	// OnRunIDResolved associates a late-resolved run_id with the process log
	// files, and is carried to Options.OnRunIDResolved verbatim. The seam does
	// not synthesise it: association calls SetRunID on the two concrete
	// loggers, which the seam does not hold -- it holds the narrow ports, and
	// domain.DebugLogger deliberately does not declare SetRunID. runTUIMode
	// holds both concrete loggers and so supplies the closure ready-made.
	//
	// Nil is permitted (the field is nil-safe in the TUI), so a test may leave
	// it unset; production must not.
	OnRunIDResolved func(runID string)
}

// interactiveWiring is the assembled interactive composition. Both fields are
// whole values the caller uses as-is.
type interactiveWiring struct {
	// Options is handed to tui.Run verbatim. It already carries SessionFactory
	// and StopSignal; runTUIMode adds, overwrites and removes nothing.
	Options tui.Options

	// NewDeps produces the complete interactive session.Deps for one session
	// construction. Options.SessionFactory is a one-line adapter over it.
	//
	// It is exposed rather than kept private because the composition-root tests
	// must observe the Deps value before session.New consumes it:
	// sessionImpl.deps is unexported, so a constructed session cannot be
	// interrogated.
	NewDeps func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Deps
}

// buildInteractiveWiring assembles the interactive frontend's composition seam.
// It is the single place where the interactive session.Deps and the tui.Options
// are constructed; runTUIMode assembles neither.
//
// The stop signal reaches both consumers from here, so the shared-instance
// invariant that production previously violated is expressible as a test over
// the returned value.
func buildInteractiveWiring(in interactiveWiringInput) interactiveWiring {
	// newDeps assembles the complete interactive session.Deps for one session
	// construction. It closes over the process-scoped values on the input --
	// notably the stop signal and the two loggers -- and derives everything
	// else from its per-invocation arguments, so a session rebuilt after a
	// configuration change or a retry keeps observing the same shared state.
	newDeps := func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Deps {
		// cfg.ExecutablePath is set when the user confirms an override on the
		// exec-override screen. It wins over the pre-scanned path so that
		// retrying with a different executable actually takes effect.
		execPath := cfg.ExecutablePath
		if execPath == "" {
			execPath = in.ClaudePath
		}
		h := buildAdapter(cfg.Harness, execPath, cfg.GHCPCLIMode, cfg.Timeout, in.Debug)

		artifactPath, err := resolveTUIArtifactPath(runFolder)
		if errors.Is(err, errUnresolvedRunFolder) {
			// Defensive branch: an unresolved run folder is a contract violation
			// by the caller. Mint a fresh identity, write a notice, and build the
			// store at the minted scoped path rather than a bare CWD-relative path.
			_, mintedFolder := in.Minter()
			in.Debug.Log(domain.EventRunnerError, "run folder unresolved; minting new run",
				domain.F("path", mintedFolder))
			fmt.Fprintf(os.Stderr, "notice: run folder unresolved; minting new run at %s\n", mintedFolder)
			artifactPath = filepath.Join(mintedFolder, "Orchestration.md")
		}
		store := newLoggedArtifactStore(artifactPath, in.Debug)

		// Extract the raw-JSON transport if the selected harness adapter
		// implements it, so consultation runs through the session's own
		// adapter rather than a second, separately constructed transport.
		var rawInvoker domain.RawInvoker
		if ri, ok := h.(domain.RawInvoker); ok {
			rawInvoker = ri
		}

		// The same builder the non-interactive path uses, so both frontends
		// share one place where consultant selection is expressed. It returns
		// the routing fields only; the stop dependency belongs to this seam
		// alone, because the non-interactive frontend has no stop affordance.
		routingDeps := buildDeps(cfg.Settings, rawInvoker, in.ProgramRef, artifact.NewApprovalReader(), in.DispatchLog)

		return session.Deps{
			Harness:       h,
			Store:         store,
			Clock:         in.Clock,
			Interact:      in.ProgramRef,
			Debug:         in.Debug,
			DispatchLog:   in.DispatchLog,
			Routing:       routingDeps.Routing,
			Manual:        routingDeps.Manual,
			PreConsult:    routingDeps.PreConsult,
			Approvals:     routingDeps.Approvals,
			StopRequested: in.StopSignal.Requested,
		}
	}

	return interactiveWiring{
		Options: tui.Options{
			Interaction:        in.ProgramRef,
			Selection:          in.Identity.Selection,
			ScanResult:         in.Identity.ScanResult,
			ResolvedRunID:      in.Identity.RunID,
			IsNewRun:           in.Identity.IsNewRun,
			RecordedWorkflowID: domain.WorkflowID(in.Identity.Workflow),
			InitialRunFolder:   in.Identity.RunFolder,
			SessionFactory: func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
				return session.New(newDeps(runFolder, isNewRun, orchFile, cfg))
			},
			MintRunIdentity:        in.Minter,
			OrchestratorDiscoverer: harness.DiscoverOrchestrator,
			ArtifactStoreFactory: func(runFolder string) domain.ArtifactStore {
				return newLoggedArtifactStore(filepath.Join(runFolder, "Orchestration.md"), in.Debug)
			},
			Clock:           in.Clock,
			OnRunIDResolved: in.OnRunIDResolved,
			StopSignal:      in.StopSignal,
			Debug:           in.Debug,
			ToolVersion:     ToolVersion,
		},
		NewDeps: newDeps,
	}
}

// buildDeps constructs the session's routing consultant, manual resolver,
// pre-consultation capability and approval reader from the run's settings.
// It is the single dependency builder for both frontends: the non-interactive
// path calls it with settings derived from the pre-scanned CLI flags, the
// interactive path with the settings carried by the completed configuration
// selection. Neither frontend constructs a consultant of its own.
//
// The orchestrator reference and the routing table are deliberately absent:
// neither is known at this point on either frontend. Both reach the
// consultants later, through domain.RunContextBinder, on the session's
// run-start path.
//
//   - settings.Mode selects which consultant is wired as Deps.Routing.
//   - settings.ManualResolution controls whether a ManualResolver is wired
//     as Deps.Manual.
//   - settings.PreConsultation controls whether the OrchestratorConsultant is
//     also wired as Deps.PreConsult (it implements both ports).
//   - invoker is the consultation transport used by OrchestratorConsultant.
//   - interact is the Interaction port used by ManualResolver.
//   - approvals is the HITL approval reader. Both frontends pass the real
//     reader; a nil value is normalised by session.New as it is today.
func buildDeps(
	settings domain.RunSettings,
	invoker domain.RawInvoker,
	interact domain.Interaction,
	approvals domain.ApprovalReader,
	dispLogger domain.DispatchLogger,
) session.Deps {
	consultant := &deviation.OrchestratorConsultant{
		Invoker:        invoker,
		DispatchLogger: dispLogger,
	}

	deps := session.Deps{
		Routing:   consultant,
		Approvals: approvals,
	}

	if settings.ManualResolution {
		deps.Manual = &deviation.ManualResolver{
			Interact: interact,
		}
	}

	if settings.PreConsultation {
		deps.PreConsult = consultant
	}

	return deps
}
