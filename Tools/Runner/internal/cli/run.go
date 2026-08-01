package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/session"
)

// RunIdentity holds a pre-resolved run identity for main.go wiring.
//
// When passed to Run as a non-nil pointer, the resolution step (scanner,
// flag validation, run-id minting) is skipped and these values are used
// directly to build the RunConfig. This ensures that the run folder
// resolved in main.go (used to construct the session's ArtifactStore)
// matches the RunFolder that cli.Run sets on the RunConfig.
//
// Tests pass nil so that the full internal resolution logic is exercised.
type RunIdentity struct {
	RunID     string
	RunFolder string
	IsNewRun  bool
}

// Run implements the mosaic-run CLI entry point.
//
// store is the ArtifactStore used by the session and to write the COMPLETED
// phase marker after a run finishes with RunCompleted. When non-nil (production),
// the caller has already constructed the store at the run-scoped path. When nil
// (legacy test helper calls), the store is constructed internally from the
// resolved run folder path.
//
// identity is the pre-resolved run identity (RunID, RunFolder, IsNewRun).
// When non-nil, the internal resolution step (scanner, flag validation,
// run-id minting) is skipped and these values are used directly. Tests
// pass nil to exercise the full internal resolution logic.
//
// sess is the Session implementation to use for runs. It is injected so that
// tests can supply a scripted session without constructing real infrastructure.
//
// out receives progress and notification output from the session's Interaction
// port (per-step lines, notices). errOut receives error and usage messages.
//
// Returns an exit code per the ExitXxx constants.
func Run(ctx context.Context, args []string, store domain.ArtifactStore, identity *RunIdentity, sess session.Session, out, errOut io.Writer) int {
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
		allowVersionDrift bool
		checkpoints       string
		runIDFlag         string // --run: resume a specific run by run_id
		isNewRunFlag      bool   // --new-run: force creation of a new run
		harnessFlag       string // --harness: adapter selection (fake|claude-code)
		timeoutFlag       string // --timeout: invocation timeout duration string
		claudePathFlag    string // --claude-path: Claude Code CLI executable path
		infraClassFlag    string // --infra-class: comma-separated class=agent mappings
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

			// Check --run and --new-run mutual exclusivity.
			if runIDFlag != "" && isNewRunFlag {
				fmt.Fprintf(errOut, "error: --run and --new-run are mutually exclusive\n")
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

			// Validate --harness.
			switch harnessFlag {
			case "fake", "claude-code":
				// valid
			default:
				fmt.Fprintf(errOut, "error: invalid --harness value %q; valid values: fake, claude-code\n", harnessFlag)
				exitCode = ExitUsage
				return nil
			}

			// Validate --timeout (must be a parseable duration).
			if _, err := time.ParseDuration(timeoutFlag); err != nil {
				fmt.Fprintf(errOut, "error: invalid --timeout value %q: %v\n", timeoutFlag, err)
				exitCode = ExitUsage
				return nil
			}

			// --claude-path is accepted as-is (any non-empty string is valid).
			_ = claudePathFlag

			// Resolve run identity from flags or scanner.
			// When a pre-resolved identity is provided (production path via main.go),
			// skip the resolution step entirely and use the caller-supplied values.
			// This ensures the RunConfig's RunFolder matches the store path that was
			// constructed in main.go before the session was created.
			var resolvedRunID, resolvedRunFolder string
			var resolvedIsNewRun bool

			if identity != nil {
				// Pre-resolved by caller: use directly, no scanning or minting.
				resolvedRunID = identity.RunID
				resolvedRunFolder = identity.RunFolder
				resolvedIsNewRun = identity.IsNewRun
			} else {
				workDir, err := os.Getwd()
				if err != nil {
					fmt.Fprintf(errOut, "error: getting working directory: %v\n", err)
					exitCode = ExitUsage
					return nil
				}

				if runIDFlag != "" {
					// --run <run_id>: validate format, verify existence, reject if completed.
					if !domain.IsValidRunID(runIDFlag) {
						fmt.Fprintf(errOut, "error: invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}\n", runIDFlag)
						exitCode = ExitUsage
						return nil
					}

					folderPath := filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
					artifactPath := filepath.Join(folderPath, "Orchestration.md")

					data, readErr := os.ReadFile(artifactPath)
					if readErr != nil {
						if errors.Is(readErr, os.ErrNotExist) {
							fmt.Fprintf(errOut, "error: no run found with id %s\n", runIDFlag)
						} else {
							fmt.Fprintf(errOut, "error: reading run artifact for %s: %v\n", runIDFlag, readErr)
						}
						exitCode = ExitUsage
						return nil
					}

					// Check whether the run is completed. Parse errors are treated as
					// resumable (the session layer will surface any real parse issues).
					if state, parseErr := artifact.Parse(data); parseErr == nil {
						if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
							fmt.Fprintf(errOut, "error: run %s is completed and cannot be resumed\n", runIDFlag)
							exitCode = ExitUsage
							return nil
						}
					}

					resolvedRunID = runIDFlag
					resolvedRunFolder = folderPath
					resolvedIsNewRun = false

				} else if isNewRunFlag {
					// --new-run: mint a new run_id.
					newRunID := domain.NewRunID(cliClock{}, domain.DefaultRandomSource())
					resolvedRunID = newRunID
					resolvedRunFolder = filepath.Join(workDir, domain.RunScopedFolder(newRunID))
					resolvedIsNewRun = true

				} else {
					// Neither flag: scan working directory for resumable candidates.
					scanner := runscan.NewDirScanner()
					scanResult, scanErr := scanner.Scan(workDir)
					if scanErr != nil {
						fmt.Fprintf(errOut, "error: scanning for runs: %v\n", scanErr)
						exitCode = ExitUsage
						return nil
					}

					switch len(scanResult.Candidates) {
					case 0:
						// No resumable candidates: start a new run.
						newRunID := domain.NewRunID(cliClock{}, domain.DefaultRandomSource())
						resolvedRunID = newRunID
						resolvedRunFolder = filepath.Join(workDir, domain.RunScopedFolder(newRunID))
						resolvedIsNewRun = true

					case 1:
						// Exactly one candidate: auto-resume.
						c := scanResult.Candidates[0]
						resolvedRunID = c.RunID
						resolvedRunFolder = c.FolderPath
						resolvedIsNewRun = false

					default:
						// Multiple candidates: non-interactive mode cannot resolve ambiguity.
						fmt.Fprintf(errOut, "error: multiple resumable runs found; use --run <run_id> to select one:\n")
						for _, c := range scanResult.Candidates {
							fmt.Fprintf(errOut, "  %s\n", c.RunID)
						}
						exitCode = ExitUsage
						return nil
					}
				}
			}

			// Parse --infra-class into a class-to-agent map.
			var infraClassSelections map[string]string
			if infraClassFlag != "" {
				infraClassSelections = make(map[string]string)
				for _, pair := range strings.Split(infraClassFlag, ",") {
					pair = strings.TrimSpace(pair)
					if pair == "" {
						continue
					}
					eqIdx := strings.Index(pair, "=")
					if eqIdx < 0 {
						fmt.Fprintf(errOut, "error: invalid --infra-class value %q: expected format class=agent\n", pair)
						exitCode = ExitUsage
						return nil
					}
					class := strings.TrimSpace(pair[:eqIdx])
					agent := strings.TrimSpace(pair[eqIdx+1:])
					infraClassSelections[class] = agent
				}
				if len(infraClassSelections) == 0 {
					infraClassSelections = nil
				}
			}

			// Build the run configuration from parsed flags and resolved run identity.
			config := domain.RunConfig{
				OrchestratorFilePath: orchestratorFile,
				WorkflowID:           domain.WorkflowID(workflowID),
				Task:                 task,
				OnDeviation:          devMode,
				AllowVersionDrift:    allowVersionDrift,
				Checkpoints:          checkpointsEnabled,
				RunID:                resolvedRunID,
				RunFolder:            resolvedRunFolder,
				IsNewRun:             resolvedIsNewRun,
				InfraClassSelections: infraClassSelections,
			}

			// Start the session and map the outcome to an exit code.
			outcome, err := sess.Start(ctx, config)
			if err != nil {
				fmt.Fprintf(errOut, "error: %v\n", err)
				exitCode = ExitFailure
				return nil
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
	runCmd.Flags().BoolVar(&allowVersionDrift, "allow-version-drift", false, "Allow workflow version mismatch when resuming")
	runCmd.Flags().StringVar(&checkpoints, "checkpoints", "disabled", "Checkpoint support (disabled|enabled)")
	runCmd.Flags().StringVar(&runIDFlag, "run", "", "Resume a specific run by run_id")
	runCmd.Flags().BoolVar(&isNewRunFlag, "new-run", false, "Force creation of a new run")
	runCmd.Flags().StringVar(&harnessFlag, "harness", "fake", "Harness adapter to use (fake|claude-code)")
	runCmd.Flags().StringVar(&timeoutFlag, "timeout", "30m", "Invocation timeout for the harness adapter (e.g. 30m, 1h)")
	runCmd.Flags().StringVar(&claudePathFlag, "claude-path", "claude", "Path to the Claude Code CLI binary")
	runCmd.Flags().StringVar(&infraClassFlag, "infra-class", "", "Comma-separated class=agent mappings for non-interactive agent-per-class selection (e.g. checkpoint=checkpoint-manager-git,commit=commit-manager-git)")

	root.AddCommand(runCmd)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		// cobra failed to parse flags or find a subcommand; print the error so
		// callers can detect which flag was unknown or what went wrong.
		fmt.Fprintf(errOut, "error: %v\n", err)
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

// cliClock implements domain.Clock using the real system clock.
type cliClock struct{}

func (cliClock) Now() time.Time { return time.Now().UTC() }
