// This file implements the composition root's dispatch order, harness
// selection and dependency wiring described by the Stage 17 contracts:
// which of the binary's three modes an invocation resolves to
// (selectFrontend), the harness-selection pair a chosen harness ID resolves
// to (newAdapter, decoderFor), the process's exit-code pass-through
// (runFrontend), and the single wired dependency set both frontends run
// against (WiringConfig, Deps, buildDeps, newSuiteRunner, RunnerDeps,
// cliOptions, tuiOptions).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/launch"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/suite"
	"mosaic-agent-test/internal/tui"
	"mosaic-agent-test/internal/workspace"
)

// InterceptorSubcommand is the interceptor route's only recognised
// argument. Deliberately absent from any help surface: the subject under
// test may see this binary's usage output, and the transparency obligation
// reaches there too.
const InterceptorSubcommand = "intercept"

// Frontend names the three things this binary can be.
type Frontend string

const (
	FrontendInterceptor Frontend = "interceptor"
	FrontendCLI         Frontend = "cli"
	FrontendTUI         Frontend = "tui"
)

// TerminalCheck is the injectable seam that makes dispatch testable without
// a terminal.
type TerminalCheck func() (stdinIsTerminal, stdoutIsTerminal bool)

// valueConsumingFlags is the named set positional detection consults so a
// flag's value token is never mistaken for a subcommand. It names every
// space-separated-value flag the CLI frontend's command surface accepts
// (see ContractsDesign.md, cli's command-surface table). "--tui" is
// deliberately absent: it is boolean and consumes no following value.
var valueConsumingFlags = map[string]bool{
	"--tests":          true,
	"--harness":        true,
	"--format":         true,
	"--fixtures":       true,
	"--workspace-root": true,
	"--timeout":        true,
	"--repetitions":    true,
}

// selectFrontend implements the dispatch rules: the interceptor route first
// and unconditionally, before isTerminal is ever consulted; then the
// explicit "--tui" override; then a flag-aware positional argument; then
// both streams must be terminals.
//
// The interceptor route is decided from args[0] alone, before anything else
// runs: the harness invokes it non-interactively, from an arbitrary working
// directory, with the interception payload on standard input and the reply
// expected on standard output. Consulting isTerminal ahead of that decision
// could launch a user interface into the pipe the harness is reading its
// reply from.
func selectFrontend(args []string, isTerminal TerminalCheck) Frontend {
	if len(args) > 0 && args[0] == InterceptorSubcommand {
		return FrontendInterceptor
	}

	positional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--tui" {
			return FrontendTUI
		}

		if !strings.HasPrefix(arg, "-") {
			positional = true
			continue
		}

		// A flag token. The equals-separated form ("--flag=value") never
		// produces a separate value token, so only the space-separated
		// form skips one.
		if !strings.Contains(arg, "=") && valueConsumingFlags[arg] && i+1 < len(args) {
			i++
		}
	}

	if positional {
		return FrontendCLI
	}

	stdinIsTerminal, stdoutIsTerminal := isTerminal()
	if stdinIsTerminal && stdoutIsTerminal {
		return FrontendTUI
	}
	return FrontendCLI
}

// adapterOptions configures newAdapter's construction of a concrete harness
// adapter.
type adapterOptions struct {
	ClaudeCode claudecode.Options
}

// ErrUnknownHarness reports a harness identity that resolves to no adapter.
// An unrecognised selection is a usage error, never a silent fallback to a
// default harness.
var ErrUnknownHarness = errors.New("mosaic-agent-test: unknown harness")

// newAdapter resolves a harness selection to its adapter. Only the Claude
// Code adapter exists in this module today; a future adapter is added here
// as it lands, not guessed at ahead of it.
func newAdapter(id string, opts adapterOptions) (domain.HarnessAdapter, error) {
	switch id {
	case claudecode.HarnessID:
		return claudecode.New(opts.ClaudeCode), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownHarness, id)
	}
}

// decoderFor resolves a harness selection to that same adapter package's
// exported envelope-decoding function, which the composition root passes to
// the subject launcher's constructor. This is the only place in the program
// where the adapter and the launcher meet. Same id space and same failure
// as newAdapter — an unrecognised selection is ErrUnknownHarness, never a
// default decoder.
func decoderFor(id string) (launch.Decoder, error) {
	switch id {
	case claudecode.HarnessID:
		return claudecode.DecodeEnvelope, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownHarness, id)
	}
}

// runFrontend executes fn — whichever frontend's Execute the dispatch
// selected — and returns its result unchanged. This is the whole contract
// the composition root owes a frontend's exit code: a code correct inside a
// frontend and lost here is worse than no code at all.
func runFrontend(fn func() int) int {
	return fn()
}

// WiringConfig is the invocation-level input to wiring: the values dispatch
// has resolved before any frontend runs. Everything here is a decision, not
// a collaborator; buildDeps turns it into collaborators.
type WiringConfig struct {
	HarnessID       string
	FixtureRoot     string
	WorkspaceRoot   string
	SelfPath        string
	LoggerBundleDir string
	CostToolPath    string
	CostTimeout     time.Duration
	Diag            io.Writer
}

// Deps is the single wired dependency set this binary builds, once, for
// whichever frontend it dispatches to. Every field is a port value or a pure
// package's product, and no field is constructed anywhere but buildDeps.
//
// It exists so AC17.5 is a comparison between two values rather than a claim
// in a comment: both frontend projections take exactly one Deps and call no
// constructor of their own, so "the same wired set" is checkable by identity
// on every field.
type Deps struct {
	Adapter   domain.HarnessAdapter
	Decoder   launch.Decoder
	Launcher  domain.SubjectLauncher
	Fixtures  fixtures.Resolver
	Effects   sideeffects.Applier
	Cost      domain.CostProvider
	Clock     domain.Clock
	Preflight func(preflight.Input) (preflight.Plan, authoring.Report)

	// Pass-through values the runner hands to the adapter without
	// interpreting, carried here so neither frontend has to know them.
	SelfPath        string
	LoggerBundleDir string

	// The resolved defaults a frontend may surface or override.
	HarnessID     string
	FixtureRoot   string
	WorkspaceRoot string
}

// writerSink adapts a plain io.Writer into the mosaic-common/harness
// diagnostic Sink the launcher logs process output through. It exists only
// so buildDeps can hand WiringConfig.Diag — an io.Writer, never stdout — to
// launch.WithSink without teaching this package the harness-neutral
// launcher's own diagnostic vocabulary.
type writerSink struct{ w io.Writer }

func (s writerSink) Log(ev commonharness.Event) {
	fmt.Fprintf(s.w, "%s: %s\n", ev.Name, ev.Message)
}

// buildDeps constructs every concrete collaborator exactly once. It is the
// only constructor call site in the binary outside newSuiteRunner's sandbox
// manager, and it is total: it either returns a fully populated Deps or an
// error, never a partially wired one.
//
// An unrecognised HarnessID surfaces here as ErrUnknownHarness, so the usage
// error is decided before any frontend starts.
func buildDeps(cfg WiringConfig) (Deps, error) {
	adapter, err := newAdapter(cfg.HarnessID, adapterOptions{})
	if err != nil {
		return Deps{}, err
	}

	decoder, err := decoderFor(cfg.HarnessID)
	if err != nil {
		return Deps{}, err
	}

	launcher := launch.New(decoder, launch.WithSink(writerSink{w: cfg.Diag}))

	resolver, err := fixtures.NewResolver(cfg.FixtureRoot)
	if err != nil {
		return Deps{}, fmt.Errorf("buildDeps: resolving fixture root %q: %w", cfg.FixtureRoot, err)
	}
	effects := sideeffects.NewApplier(resolver)

	costProvider := cost.New(cost.Options{
		ExecutablePath: cfg.CostToolPath,
		Timeout:        cfg.CostTimeout,
	})

	return Deps{
		Adapter:   adapter,
		Decoder:   decoder,
		Launcher:  launcher,
		Fixtures:  resolver,
		Effects:   effects,
		Cost:      costProvider,
		Clock:     systemClock{},
		Preflight: preflight.Validate,

		SelfPath:        cfg.SelfPath,
		LoggerBundleDir: cfg.LoggerBundleDir,

		HarnessID:     cfg.HarnessID,
		FixtureRoot:   cfg.FixtureRoot,
		WorkspaceRoot: cfg.WorkspaceRoot,
	}, nil
}

// composedSuiteRunner adapts a Deps and a workspace.Manager, both bound at
// construction, into cli.SuiteRunner / tui.SuiteRunner. It is the one thing
// newSuiteRunner returns, and the only place the per-attempt runner.Deps a
// run needs is assembled — deferred to Run, where the run's own progress
// sink is finally known.
type composedSuiteRunner struct {
	deps Deps
	ws   workspace.Manager
}

func (r composedSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error) {
	rd := r.deps.RunnerDeps(r.ws, sink)
	s := suite.New(suite.Options{
		Runner:   testRunnerAdapter{deps: rd},
		Progress: sink,
		Clock:    r.deps.Clock,
	})
	return s.Run(ctx, p)
}

// testRunnerAdapter adapts runner.Run — a free function over runner.Deps —
// into suite.TestRunner, the interface the suite's scheduling drives one
// attempt through.
type testRunnerAdapter struct{ deps runner.Deps }

func (a testRunnerAdapter) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest) (domain.RunEvidence, error) {
	return runner.Run(ctx, a.deps, runner.Request{Key: key, Test: t, Settings: t.Settings})
}

// newSuiteRunner binds a Deps to one invocation's resolved configuration and
// returns the runner both frontends consume. It is the single path from Deps
// to a running suite: the CLI reaches it through its factory and the TUI
// reaches it directly, so a dependency added to one frontend's runs is added
// to the other's by construction.
//
// The sandbox manager is the one collaborator constructed here rather than in
// buildDeps, because its root is not known until the invocation resolves it.
// Nothing else is deferred.
func newSuiteRunner(d Deps, rc cli.RunConfig) (cli.SuiteRunner, error) {
	if rc.WorkspaceRoot == "" {
		return nil, fmt.Errorf("newSuiteRunner: workspace root is required")
	}
	ws := workspace.NewManager(rc.WorkspaceRoot, d.Clock)
	return composedSuiteRunner{deps: d, ws: ws}, nil
}

// RunnerDeps is the per-attempt collaborator set a Deps yields once the
// sandbox manager and the run's progress sink exist. Both frontends' runs are
// executed against the value this returns, and it reads every collaborator
// straight off d — so a test can assert field-for-field that what the CLI
// path runs with and what the TUI path runs with are the same values.
func (d Deps) RunnerDeps(ws workspace.Manager, progress domain.ProgressSink) runner.Deps {
	return runner.Deps{
		Workspaces: ws,
		Adapter:    d.Adapter,
		Launcher:   d.Launcher,
		Fixtures:   d.Fixtures,
		Effects:    d.Effects,
		Cost:       d.Cost,
		Clock:      d.Clock,
		Progress:   progress,

		SelfPath:        d.SelfPath,
		LoggerBundleDir: d.LoggerBundleDir,
	}
}

// cliOptions and tuiOptions are the two projections of one Deps onto the two
// frontends' option structs. Both are pure functions of their arguments: they
// construct nothing, resolve nothing and consult no package-level state, so
// the only way a frontend can receive a dependency is for that dependency to
// be in Deps.
//
// The asymmetry between them is confined to the suite field and is forced by
// the workspace-root flag: the CLI receives a factory closing over d, the TUI
// receives a runner already bound over d.WorkspaceRoot. Both go through
// newSuiteRunner, so the asymmetry is when construction happens and never
// what is constructed.
func cliOptions(d Deps, stdout, stderr io.Writer) cli.Options {
	return cli.Options{
		Preflight: d.Preflight,
		Suite: func(rc cli.RunConfig) (cli.SuiteRunner, error) {
			return newSuiteRunner(d, rc)
		},

		Stdout: stdout,
		Stderr: stderr,

		WorkspaceRoot:  d.WorkspaceRoot,
		FixtureRoot:    d.FixtureRoot,
		DefaultHarness: d.HarnessID,
	}
}

func tuiOptions(d Deps, suites []string) (tui.Options, error) {
	runnerForDefault, err := newSuiteRunner(d, cli.RunConfig{WorkspaceRoot: d.WorkspaceRoot})
	if err != nil {
		return tui.Options{}, err
	}

	return tui.Options{
		Preflight: d.Preflight,
		Suite:     runnerForDefault,
		Suites:    suites,
		Harness:   d.HarnessID,
	}, nil
}
