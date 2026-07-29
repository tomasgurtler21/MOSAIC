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
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui"
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

	// CLI mode: parse --artifact-location and --orchestrator-file for dependency wiring.
	artifactLocation := scanFlag(args, "--artifact-location")
	artifactPath := resolveArtifactPath(artifactLocation)

	orchFile := scanFlag(args, "--orchestrator-file")
	onDeviationStr := scanFlag(args, "--on-deviation")
	if onDeviationStr == "" {
		onDeviationStr = "delegate" // matches the flag default
	}

	// Build the CLI Interaction port. The same instance is used as the session's
	// Interaction (for per-step progress and notices) and writes to os.Stdout.
	interact := cli.NewInteraction(os.Stdout)

	// Build the artifact store pointed at the resolved artifact path.
	store := artifact.NewFileStore(artifactPath)

	// Build the harness adapter. A real Claude Code harness adapter will replace this
	// placeholder when it is implemented in a follow-up stage. For now, all invocations
	// return an error and the deviation resolver handles the outcome.
	h := harness.NewFakeAdapter()

	// Build the deviation resolver based on the --on-deviation flag.
	var dev domain.DeviationResolver
	switch onDeviationStr {
	case "stop":
		dev = &stopDeviationResolver{}
	default: // "delegate"
		if orchFile != "" {
			// Use the OrchestratorDelegate when an orchestrator file is provided.
			orchDir := orchFileDir(orchFile)
			var orchSeq int
			dev = &deviation.OrchestratorDelegate{
				Harness: h,
				Orchestrator: domain.AgentReference{
					Identifier:     "orchestrator-script",
					DefinitionPath: orchDir + "/orchestrator-script.md",
				},
				ArtifactPath: artifactPath,
				NextSeq:      func() int { orchSeq++; return orchSeq },
			}
		} else {
			// No orchestrator file known at startup: fall back to stop mode.
			dev = &stopDeviationResolver{}
		}
	}

	// Wire the session with all port dependencies.
	sess := session.New(session.Deps{
		Harness:   h,
		Store:     store,
		Deviation: dev,
		Clock:     &realClock{},
		Interact:  interact,
	})

	os.Exit(cli.Run(context.Background(), args, sess, os.Stdout, os.Stderr))
}

// runTUIMode launches the interactive TUI frontend. All session dependencies are
// constructed here; the TUI's ProgramRef provides the Interaction port and the
// TUIDeviationResolver handles deviation resolution through the TUI's deviation screen.
func runTUIMode(args []string) {
	// Build common infrastructure used by both modes.
	artifactLocation := scanFlag(args, "--artifact-location")
	artifactPath := resolveArtifactPath(artifactLocation)
	store := artifact.NewFileStore(artifactPath)
	h := harness.NewFakeAdapter()

	// The ProgramRef implements the Interaction port and also drives the TUI overlays.
	// It must be wired into both the session and the TUI Options so that session
	// goroutine calls reach the Bubble Tea event loop.
	programRef := tui.NewProgramRef()

	// TUIDeviationResolver routes deviation decisions through the TUI's deviation
	// screen. Delegate is nil here because the orchestrator file path is not known
	// until the user enters it in the first setup screen. When the user picks
	// "Delegate to orchestrator" in the deviation screen and no delegate is wired,
	// the resolver returns a stop instruction as a safe fallback.
	tuiDev := &tui.TUIDeviationResolver{
		Program: programRef,
	}

	sess := session.New(session.Deps{
		Harness:   h,
		Store:     store,
		Deviation: tuiDev,
		Clock:     &realClock{},
		Interact:  programRef,
	})

	ctx := context.Background()
	if err := tui.Run(ctx, sess, tui.Options{Interaction: programRef}); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
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

// resolveArtifactPath returns the artifact path: the explicit override, or the
// canonical default ("Orchestration.md" in the working directory).
func resolveArtifactPath(override string) string {
	if override != "" {
		return override
	}
	return "Orchestration.md"
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
