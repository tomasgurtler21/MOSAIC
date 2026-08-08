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
	"mosaic-run/internal/runselect"
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

	// Position is the resumed run's recorded position, already known to the
	// caller (main.go reads the artifact once to check completion). When
	// set, the announcement uses it directly instead of reading the
	// artifact a second time. Nil for a new run, or when the caller has not
	// already determined it.
	Position *runselect.Position
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
		infraClassFlag    string   // --infra-class: comma-separated class=agent mappings
		inputFlags        []string // --input: seed source paths (repeatable)
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

			// Check --input and --run mutual exclusivity. Seeding only applies when
			// creating a new run; silently ignoring --input on a resume would let a
			// user believe inputs were seeded when they were not.
			if len(inputFlags) > 0 && runIDFlag != "" {
				fmt.Fprintf(errOut, "error: --input and --run are mutually exclusive\n")
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
			var resolvedPosition *runselect.Position

			if identity != nil {
				// Pre-resolved by caller: use directly, no scanning or minting.
				resolvedRunID = identity.RunID
				resolvedRunFolder = identity.RunFolder
				resolvedIsNewRun = identity.IsNewRun
				resolvedPosition = identity.Position
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
					// A successful parse also yields the recorded position directly,
					// so the announcement below need not read the artifact again.
					if state, parseErr := artifact.Parse(data); parseErr == nil {
						if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
							fmt.Fprintf(errOut, "error: run %s is completed and cannot be resumed\n", runIDFlag)
							exitCode = ExitUsage
							return nil
						}
						resolvedPosition = &runselect.Position{
							Phase:       state.CurrentState.Phase,
							Stage:       state.CurrentState.Stage,
							LastAgent:   state.CurrentState.LastAgent,
							LastUpdated: state.LastUpdated,
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
					// Neither flag: the selection is never inferred from how many runs
					// exist. Scan the working directory and ask runselect for the
					// decision; a Question means the CLI refuses and names the
					// available choices rather than guessing (AC2.2, AC2.6).
					scanner := runscan.NewDirScanner()
					scanResult, scanErr := scanner.Scan(workDir)
					if scanErr != nil {
						fmt.Fprintf(errOut, "error: scanning for runs: %v\n", scanErr)
						exitCode = ExitUsage
						return nil
					}

					dec, resErr := runselect.Resolve(runselect.Request{Scan: scanResult, WorkDir: workDir}, cliMinter(workDir))
					if resErr != nil {
						fmt.Fprintf(errOut, "error: %v\n", resErr)
						exitCode = ExitUsage
						return nil
					}
					if dec.Question != nil {
						fmt.Fprintf(errOut, "error: %s\n", formatSelectionRefusal(*dec.Question))
						exitCode = ExitUsage
						return nil
					}

					resolvedRunID = dec.Resolved.RunID
					resolvedRunFolder = dec.Resolved.RunFolder
					resolvedIsNewRun = dec.Resolved.IsNewRun
					resolvedPosition = dec.Resolved.Position
				}
			}

			// State the chosen run before any dispatch (AC2.7): which run, whether
			// new or resumed, and for a resumed run its recorded position.
			fmt.Fprintln(out, runselect.Announce(announceIdentity(resolvedRunID, resolvedRunFolder, resolvedIsNewRun, resolvedPosition)))

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
				SeedInputs:           inputFlags,
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
	runCmd.Flags().StringArrayVar(&inputFlags, "input", nil,
		"Path to a file or directory to copy into a new run's folder before the first dispatch; repeatable. Not permitted with --run.")

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

// cliMinter returns a runselect.Minter that mints a new run_id rooted at workDir.
func cliMinter(workDir string) runselect.Minter {
	return func() (string, string) {
		newRunID := domain.NewRunID(cliClock{}, domain.DefaultRandomSource())
		return newRunID, filepath.Join(workDir, domain.RunScopedFolder(newRunID))
	}
}

// formatSelectionRefusal renders the non-interactive refusal message for an
// unsettled selection: every resumable run_id the caller may pass to --run,
// every unresumable run with the reason it cannot be resumed (AC2.5), and
// --new-run as the always-available way to start fresh (AC2.3, AC2.6).
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

// announceIdentity builds the runselect.Identity used to render the
// chosen-run announcement. When position is already known to the caller
// (the artifact was already read to resolve identity), it is used directly.
// Otherwise, for a resumed run, the artifact is read once here to recover
// it; a read or parse failure simply leaves Position nil, which
// runselect.Announce handles without panicking.
func announceIdentity(runID, runFolder string, isNewRun bool, position *runselect.Position) runselect.Identity {
	id := runselect.Identity{RunID: runID, RunFolder: runFolder, IsNewRun: isNewRun, Position: position}
	if isNewRun || position != nil {
		return id
	}
	data, err := os.ReadFile(filepath.Join(runFolder, "Orchestration.md"))
	if err != nil {
		return id
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return id
	}
	id.Position = &runselect.Position{
		Phase:       state.CurrentState.Phase,
		Stage:       state.CurrentState.Stage,
		LastAgent:   state.CurrentState.LastAgent,
		LastUpdated: state.LastUpdated,
	}
	return id
}
