// The interceptor route: the short-lived process a harness's interception
// point reaches once per intercepted call.
//
// This file wires internal/interceptor.Run to a concrete harness adapter,
// the real file-backed run state, invocation log and side-effect applier.
// main.go dispatches here — before any terminal detection and before any
// frontend selection — when the invocation's first argument is
// InterceptorSubcommand.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

// runIntercept parses the interceptor route's arguments, wires the real
// collaborators and drives interceptor.Run. It always returns the exit code
// interceptor.Run reports (always zero on the ordinary path — see that
// package's own contract), because a non-zero exit here is read by the
// harness as a failed hook, which damages the run this tool is measuring.
func runIntercept(args []string, stdin, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet(InterceptorSubcommand, flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", "", "the subject's working directory")
	controlDir := fs.String("control-dir", "", "the control directory, sibling of --workspace")
	harnessID := fs.String("harness", "", "the harness identity, e.g. claude-code")
	phase := fs.String("phase", "", "the interception phase: pre or post")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *controlDir == "" {
		fmt.Fprintln(stderr, "mosaic-agent-test: intercept: --control-dir is required")
		return 2
	}

	adapter, err := newAdapter(*harnessID, adapterOptions{})
	if err != nil {
		// No adapter means no way to answer in the harness's own terms, so
		// this is the one interceptor-route failure this binary cannot
		// contain behind interceptor.Run's boundary: there is nothing to
		// translate a neutral reply with.
		fmt.Fprintf(stderr, "mosaic-agent-test: intercept: %v\n", err)
		return 2
	}

	clock := systemClock{}
	store := runstate.NewStore(*controlDir, clock)
	log := invlog.NewLog(domain.Sandbox{ControlDir: *controlDir}.InvocationLogPath())

	resolver, err := fixtures.NewResolver(*controlDir)
	if err != nil {
		fmt.Fprintf(stderr, "mosaic-agent-test: intercept: resolving fixture root: %v\n", err)
		return 2
	}
	effects := sideeffects.NewApplier(resolver)

	registry, err := interceptor.LoadRegistry(*controlDir)
	if err != nil {
		fmt.Fprintf(stderr, "mosaic-agent-test: intercept: loading active registry: %v\n", err)
	}
	groups, err := interceptor.LoadGroups(*controlDir)
	if err != nil {
		fmt.Fprintf(stderr, "mosaic-agent-test: intercept: loading parallel groups: %v\n", err)
	}

	// TestID, RunNumber, and RunID are read from the run state document itself
	// (setup writes them at Initialize), not from command-line arguments:
	// the same sandbox is addressed by every interceptor process for the
	// run's lifetime. Best-effort only — a failure here is exactly one of
	// the failure classes interceptor.Run itself is built to contain, and it
	// still runs (with empty values) so the harness still gets a valid reply.
	var testID string
	var runNumber int
	var runID string
	if current, readErr := store.Read(); readErr == nil {
		testID = current.TestID
		runNumber = current.RunNumber
		runID = current.RunID
	}

	cfg := interceptor.Config{
		ControlDir: *controlDir,
		SubjectDir: *workspace,
		Phase:      domain.InterceptionPhase(*phase),

		Adapter: adapter,
		State:   store,
		Log:     log,
		Effects: effects,
		Clock:   clock,

		Registry: registry,
		Groups:   groups,

		TestID:    testID,
		RunNumber: runNumber,
		RunID:     runID,

		In:   stdin,
		Out:  stdout,
		Diag: stderr,
	}

	return interceptor.Run(context.Background(), cfg)
}

// systemClock is the real wall clock. Every other Clock in this module
// exists to make a protocol testable without sleeping; this process is the
// real thing, so it uses the real clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
