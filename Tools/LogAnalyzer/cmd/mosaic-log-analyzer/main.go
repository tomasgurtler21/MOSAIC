// Command mosaic-log-analyzer analyses MOSAIC orchestration logs to report
// per-run token usage and estimated cost. The composition root (this file)
// is the only place where concrete adapter implementations are constructed
// and wired together; all dispatch logic lives in internal/app, internal/cli
// and internal/tui.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/logread"
	"mosaic-log-analyzer/internal/logscan"
	"mosaic-log-analyzer/internal/pricing"
	"mosaic-log-analyzer/internal/tui"
)

// version is substituted at link time; "dev" is the fallback for local builds.
const version = "dev"

// valueFlagNames are the flags that consume a following token in space-separated
// form (--flag value). Boolean flags like --tui are NOT in this set.
var valueFlagNames = map[string]bool{
	"--path":   true,
	"--run":    true,
	"--format": true,
}

// wantsTUI reports whether the composition root should launch the interactive TUI.
// isTerminal is an injectable seam — func(fd uintptr) bool — so the per-fd
// terminal check is testable without a real TTY.
//
// Dispatch rules (binding):
//   - "--tui" present anywhere in args → TUI (seam is never called).
//   - A positional (non-flag) argument present → CLI (seam is never called).
//   - "--tui" wins when both "--tui" and a positional are present → TUI.
//   - Otherwise → isTerminal(stdin) && isTerminal(stdout); both file descriptors
//     must be terminals for TUI to be selected.
func wantsTUI(args []string, isTerminal func(fd uintptr) bool) bool {
	if scanBoolFlag(args, "--tui") {
		return true
	}
	if hasPositionalArg(args) {
		return false
	}
	return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stdout.Fd())
}

// scanBoolFlag reports whether a boolean flag (e.g. "--tui") appears anywhere
// in args. Only exact matches count; prefix matches are not accepted.
func scanBoolFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// hasPositionalArg reports whether args contains at least one positional
// (non-flag) token. It is flag-aware: it knows which flags consume a following
// value token in space-separated form so it does not misidentify those values
// as positional arguments.
//
// Value-consuming flags (space-separated form): --path, --run, --format.
// Boolean flags (consume no following value): --tui.
// Equals-separated form (--flag=value) is a single token starting with "-" and
// never produces a separate positional-looking value token.
func hasPositionalArg(args []string) bool {
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			// Flag token (or equals-separated value flag): not a positional.
			// Determine the bare flag name for the value-flag lookup.
			name := arg
			if idx := strings.Index(arg, "="); idx >= 0 {
				name = arg[:idx]
			}
			// Only skip the next token when the flag is space-separated (no "=" in arg).
			if valueFlagNames[name] && !strings.Contains(arg, "=") {
				skipNext = true
			}
			continue
		}
		// Non-flag, non-skipped token: this is a positional argument.
		return true
	}
	return false
}

// scanFlag does a minimal pre-scan of args for a named flag. It supports both
// "--flag value" (space-separated) and "--flag=value" (equals-separated) forms,
// matching cobra's own flag parsing so both styles are always equivalent.
// Returns def when the flag is absent or when "--flag" appears as the last
// argument with no following value token.
func scanFlag(args []string, name, def string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		prefix := name + "="
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return def
}

// realClock provides the current UTC time.
type realClock struct{}

func (c *realClock) Now() time.Time { return time.Now().UTC() }

func main() {
	args := os.Args[1:]

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Pre-scan --path before dispatching; passed to tui.Options.InitialPath.
	explicitPath := scanFlag(args, "--path", "")

	// Construct all infrastructure adapters here — the only site in the module
	// where concrete implementations are instantiated.
	scanner := logscan.NewScanner()
	reader := logread.NewReader()
	pricingStore := pricing.NewStore(workDir)
	clock := &realClock{}

	if wantsTUI(args, isatty.IsTerminal) {
		// TUI mode: create the TUI interaction first so it can be shared between
		// the service (which calls it for source prompts and pricing gaps) and the
		// TUI model (which renders the overlay and supplies answers).
		tuiInteract, tuiQuestions := tui.NewInteraction()

		svc := app.New(app.Deps{
			Source:      scanner,
			Reader:      reader,
			Pricing:     pricingStore,
			Clock:       clock,
			Interaction: tuiInteract,
			WorkDir:     workDir,
		})

		os.Exit(tui.Run(tui.Options{
			Service:     svc,
			InitialPath: explicitPath,
			Version:     version,
			Interaction: tuiInteract,
			Questions:   tuiQuestions,
		}))
	}

	// CLI mode: build the non-blocking Interaction from any flag-derived answers,
	// then hand the fully-wired service to the cobra command tree.
	answers := cli.Answers{}
	interact := cli.NewInteraction(os.Stdout, answers)

	svc := app.New(app.Deps{
		Source:      scanner,
		Reader:      reader,
		Pricing:     pricingStore,
		Clock:       clock,
		Interaction: interact,
		WorkDir:     workDir,
	})

	os.Exit(cli.Execute(cli.Options{
		Service: svc,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
	}, args))
}
