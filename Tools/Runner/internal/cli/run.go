package cli

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"mosaic-run/internal/domain"
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

	runCmd := &cobra.Command{
		Use:           "run",
		Short:         "Run a workflow non-interactively",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Recover from any panic in sess.Start so that a session-layer crash
			// surfaces as a diagnostic error message rather than a silent process
			// termination. The panic value and a truncated stack trace are written to
			// errOut so the operator can diagnose the failure.
			defer func() {
				if p := recover(); p != nil {
					stack := debug.Stack()
					if len(stack) > 4096 {
						stack = stack[:4096]
					}
					fmt.Fprintf(errOut, "error: panic in session: %v\n%s\n", p, stack)
					exitCode = ExitFailure
				}
			}()

			exitCode = executeRun(ctx, cmd, store, identity, sess, out, errOut)
			return nil
		},
	}

	// Register all run subcommand flags via the shared declaration in flagspecs.go.
	// This single call keeps flag registration and arity-publication (RunFlagSpecs)
	// derived from one source: adding a flag here requires editing RegisterRunFlags,
	// which RunFlagSpecs introspects, so both stay automatically in sync.
	RegisterRunFlags(runCmd.Flags())

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
