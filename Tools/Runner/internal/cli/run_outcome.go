package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// runSessionAndReport starts the session, writes the COMPLETED phase marker
// on success, and reports the outcome: the mapped exit code, and either the
// consultant stop reason or the outcome message on errOut.
func runSessionAndReport(ctx context.Context, config domain.RunConfig, store domain.ArtifactStore, sess session.Session, out, errOut io.Writer, resolvedRunFolder string) int {
	outcome, err := sess.Start(ctx, config)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return ExitFailure
	}

	// Write COMPLETED phase marker when the run finishes successfully.
	// Use the injected store (tests) or construct from the resolved run-scoped
	// artifact path (production). This ensures the marker is written to the
	// correct Orchestration-{run_id}/Orchestration.md file.
	if outcome.Status == domain.RunCompleted {
		effectiveStore := store
		if effectiveStore == nil {
			artifactPath := filepath.Join(resolvedRunFolder, "Orchestration.md")
			effectiveStore = artifact.NewFileStore(artifactPath)
		}
		if _, setErr := effectiveStore.SetPhase(ctx, domain.ArtifactState{}, "COMPLETED", cliClock{}.Now()); setErr != nil {
			fmt.Fprintf(errOut, "warning: failed to write COMPLETED marker: %v\n", setErr)
		}
	}

	exitCode := outcomeToExitCode(outcome)
	// Print the stop reason to stderr when the consultant halted the run.
	// The artifact is left resumable; stderr is the operator's signal.
	if outcome.Status == domain.RunStoppedByConsultant && outcome.StopReason != "" {
		fmt.Fprintf(errOut, "%s\n", outcome.StopReason)
	} else if exitCode != ExitSuccess {
		// Print non-success messages to stderr so stdout stays machine-readable.
		fmt.Fprintf(errOut, "%s\n", outcome.Message)
	}
	return exitCode
}

// outcomeToExitCode maps a RunOutcome to the appropriate CLI exit code.
func outcomeToExitCode(outcome domain.RunOutcome) int {
	switch outcome.Status {
	case domain.RunCompleted:
		return ExitSuccess
	case domain.RunStopped:
		return ExitStopped
	case domain.RunDeviationUnresolved:
		return ExitDeviationUnresolved
	case domain.RunRefused:
		return ExitRefused
	case domain.RunFailed:
		return ExitFailure
	case domain.RunStoppedByConsultant:
		return ExitStoppedByConsultant
	default:
		return ExitFailure
	}
}
