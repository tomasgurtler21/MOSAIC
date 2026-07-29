package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// Run implements the mosaic-run CLI entry point.
//
// sess is the Session implementation to use for runs. It is injected so that
// tests can supply a scripted session without constructing real infrastructure.
//
// out receives progress and notification output from the session's Interaction
// port (per-step lines, notices). errOut receives error and usage messages.
//
// Returns an exit code per the ExitXxx constants.
func Run(ctx context.Context, args []string, sess session.Session, out, errOut io.Writer) int {
	exitCode := ExitUsage

	root := &cobra.Command{
		Use:           "mosaic-run",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("a subcommand is required: run")
		},
	}
	// Redirect cobra's own output (help, usage) to errOut so it does not
	// intermix with the machine-readable output written to out.
	root.SetOut(errOut)
	root.SetErr(errOut)

	// ------------------------------------------------------------------
	// run subcommand: non-interactive orchestration execution
	// ------------------------------------------------------------------
	var (
		orchestratorFile  string
		workflowID        string
		task              string
		onDeviation       string
		existingArtifact  string
		allowVersionDrift bool
		artifactLocation  string
		checkpoints       string
	)

	runCmd := &cobra.Command{
		Use:           "run",
		Short:         "Run a workflow non-interactively",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate required flags.
			if orchestratorFile == "" {
				fmt.Fprintf(errOut, "error: --orchestrator-file is required\n")
				exitCode = ExitUsage
				return nil
			}
			if workflowID == "" {
				fmt.Fprintf(errOut, "error: --workflow is required\n")
				exitCode = ExitUsage
				return nil
			}
			if task == "" {
				fmt.Fprintf(errOut, "error: --task is required\n")
				exitCode = ExitUsage
				return nil
			}

			// Parse --on-deviation.
			var devMode domain.DeviationMode
			switch onDeviation {
			case "delegate":
				devMode = domain.DeviationDelegate
			case "stop":
				devMode = domain.DeviationStop
			default:
				fmt.Fprintf(errOut, "error: invalid --on-deviation value %q; valid values: delegate, stop\n", onDeviation)
				exitCode = ExitUsage
				return nil
			}

			// Parse --existing-artifact.
			var existingMode domain.ExistingArtifactMode
			switch existingArtifact {
			case "resume":
				existingMode = domain.ExistingResume
			case "fresh":
				existingMode = domain.ExistingFresh
			case "fail":
				existingMode = domain.ExistingFail
			default:
				fmt.Fprintf(errOut, "error: invalid --existing-artifact value %q; valid values: resume, fresh, fail\n", existingArtifact)
				exitCode = ExitUsage
				return nil
			}

			// Parse --checkpoints.
			var checkpointsEnabled bool
			switch checkpoints {
			case "disabled":
				checkpointsEnabled = false
			case "enabled":
				checkpointsEnabled = true
			default:
				fmt.Fprintf(errOut, "error: invalid --checkpoints value %q; valid values: disabled, enabled\n", checkpoints)
				exitCode = ExitUsage
				return nil
			}

			// Build the run configuration from parsed flags.
			config := domain.RunConfig{
				OrchestratorFilePath: orchestratorFile,
				WorkflowID:           domain.WorkflowID(workflowID),
				Task:                 task,
				ArtifactLocation:     artifactLocation,
				OnDeviation:          devMode,
				ExistingArtifact:     existingMode,
				AllowVersionDrift:    allowVersionDrift,
				Checkpoints:          checkpointsEnabled,
			}

			// Start the session and map the outcome to an exit code.
			outcome, err := sess.Start(ctx, config)
			if err != nil {
				fmt.Fprintf(errOut, "error: %v\n", err)
				exitCode = ExitFailure
				return nil
			}

			exitCode = outcomeToExitCode(outcome)
			// Print non-success messages to stderr so stdout stays machine-readable.
			if exitCode != ExitSuccess {
				fmt.Fprintf(errOut, "%s\n", outcome.Message)
			}
			return nil
		},
	}

	// FR-3 flags with stated defaults.
	runCmd.Flags().StringVar(&orchestratorFile, "orchestrator-file", "", "Path to the orchestrator agent file (required)")
	runCmd.Flags().StringVar(&workflowID, "workflow", "", "Workflow identifier (required)")
	runCmd.Flags().StringVar(&task, "task", "", "Task description (required)")
	runCmd.Flags().StringVar(&onDeviation, "on-deviation", "delegate", "How to handle deviations (delegate|stop)")
	runCmd.Flags().StringVar(&existingArtifact, "existing-artifact", "resume", "How to handle an existing artifact (resume|fresh|fail)")
	runCmd.Flags().BoolVar(&allowVersionDrift, "allow-version-drift", false, "Allow workflow version mismatch when resuming (FR-7b override)")
	runCmd.Flags().StringVar(&artifactLocation, "artifact-location", "", "Override canonical Orchestration.md path (FR-1b)")
	runCmd.Flags().StringVar(&checkpoints, "checkpoints", "disabled", "Checkpoint support (disabled|enabled)")

	root.AddCommand(runCmd)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		// cobra failed to parse flags or find a subcommand.
		return ExitUsage
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
	default:
		return ExitFailure
	}
}
