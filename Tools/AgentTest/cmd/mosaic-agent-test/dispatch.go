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
	"os"
	"path/filepath"
	"strings"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/fixtures"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/harness/opencode"
	"mosaic-agent-test/internal/launch"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/sideeffects"
	"mosaic-agent-test/internal/suite"
	"mosaic-agent-test/internal/tui"
	"mosaic-agent-test/internal/workspace"
)

// InterceptorSubcommand is this package's name for domain.InterceptorSubcommand
// — the argv token that routes this binary to its interceptor frontend. It is
// an alias, not a restatement: internal/domain owns the literal (it is the one
// layer both the composition root and internal/runner may import), and this
// binary's own files reference it under this short name rather than the
// qualified one throughout. Deliberately absent from any help surface: the
// subject under test may see this binary's usage output, and the
// transparency obligation reaches there too.
const InterceptorSubcommand = domain.InterceptorSubcommand

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
// flag's value token is never mistaken for a subcommand. It is derived from
// cli.ValueConsumingFlags() — the CLI package's own shared flag
// specification — rather than restating the list a second time, so a flag
// added there is recognised here without a second edit. "--tui" is
// deliberately absent from that source: it is boolean and consumes no
// following value.
var valueConsumingFlags = valueConsumingFlagSet()

func valueConsumingFlagSet() map[string]bool {
	out := make(map[string]bool)
	for _, name := range cli.ValueConsumingFlags() {
		out[name] = true
	}
	return out
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
	if len(args) > 0 && args[0] == domain.InterceptorSubcommand {
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

// supportedHarnesses returns the shared catalog entries this tool's
// newAdapter/decoderFor switches can actually resolve, in catalog order.
//
// AgentTest's adapter port is materially larger than Runner's (provisioning,
// capabilities, scope inspection, hook bridging, plus the internal/harness/contract
// conformance suite), so this tool gains harness support later than the shared
// spawn layer does. Expressing that gap here — rather than passing the full
// catalog directly to the frontends — makes it visible in flag validation
// instead of surfacing as a late ErrUnknownHarness.
//
// The returned slice is built by filtering a defensive copy of the catalog; it
// is fresh on every call. Labels are never restated: they come from the catalog
// entries passed through unchanged.
func supportedHarnesses() []commonharness.CLIHarness {
	all := commonharness.CLIHarnesses()
	out := make([]commonharness.CLIHarness, 0, len(all))
	for _, e := range all {
		if isSupportedHarness(e.ID) {
			out = append(out, e)
		}
	}
	return out
}

// isSupportedHarness reports whether id names a harness this tool supports.
// It derives from the shared catalog's own identity constants — never from a
// local restatement of identity strings.
func isSupportedHarness(id string) bool {
	switch id {
	case commonharness.HarnessIDClaudeCode, commonharness.HarnessIDOpenCode:
		return true
	default:
		return false
	}
}

// supportedModelCatalogs returns the model catalog entries for harnesses this
// tool supports, in the same stable order commonharness.ModelCatalogs uses.
//
// Filtering to the supported set keeps the frontend consistent with
// supportedHarnesses: a model identifier valid only for an unsupported harness
// is rejected at the CLI rather than surfacing later as an unrecognised harness
// inside the composition root.
func supportedModelCatalogs() []commonharness.ModelCatalog {
	all := commonharness.ModelCatalogs()
	out := make([]commonharness.ModelCatalog, 0, len(all))
	for _, e := range all {
		if isSupportedHarness(e.HarnessID) {
			out = append(out, e)
		}
	}
	return out
}

// adapterOptions configures newAdapter's construction of a concrete harness
// adapter.
type adapterOptions struct {
	ClaudeCode claudecode.Options
	OpenCode   opencode.Options
}

// ErrUnknownHarness reports a harness identity that resolves to no adapter.
// An unrecognised selection is a usage error, never a silent fallback to a
// default harness.
var ErrUnknownHarness = errors.New("mosaic-agent-test: unknown harness")

// newAdapter resolves a harness selection to its adapter. Case labels come
// from the shared CLI-harness catalog's own identity constants, so this
// switch and that catalog cannot drift apart silently.
func newAdapter(id string, opts adapterOptions) (domain.HarnessAdapter, error) {
	switch id {
	case commonharness.HarnessIDClaudeCode:
		return claudecode.New(opts.ClaudeCode), nil
	case commonharness.HarnessIDOpenCode:
		return opencode.New(opts.OpenCode), nil
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
	case commonharness.HarnessIDClaudeCode:
		return claudecode.DecodeEnvelope, nil
	case commonharness.HarnessIDOpenCode:
		return opencode.DecodeEnvelope, nil
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
	// DeployToolPath is the path to the mosaic-deploy binary. The default is
	// the binary-relative mosaic-deploy[.exe], matching how CostToolPath
	// defaults to the binary-relative mosaic-log-analyzer. An absent tool is
	// reported as an EnvironmentProblem naming this path and the override that
	// would change it — never a bare file-not-found.
	DeployToolPath string
	// MosaicRoot is the repository root passed to the deployment tool. It
	// defaults to the binary-relative repository root (three directories above
	// the binary's own directory in a correctly staged distribution), so the
	// deploy tool can locate the repository when invoked from an arbitrary
	// working directory. An empty value is never used as a default; when the
	// resolved path is non-empty it is passed as --mosaic-root.
	MosaicRoot string
	// CatalogFolder overrides the catalogue the deployment tool sources
	// agents and workflows from. Empty means "do not override". Resolved
	// through the same three-tier chain as MosaicRoot.
	CatalogFolder string
	// DeployInvoke is a test seam for the deployment port's CommandRunner.
	// When non-nil, buildDeps threads it into agentdeploy.Options.Invoke so
	// tests can capture the argument list the deployer would pass to the
	// subprocess without the real binary being present. nil selects real
	// process execution via execCommandRunner.
	DeployInvoke agentdeploy.CommandRunner
	// DeployTimeout bounds every render invocation. The composition root sets
	// this once; the port enforces it with context.WithTimeout on every call.
	DeployTimeout time.Duration
	// DeployScratchRoot is the workspace root preflight dry-run renders are
	// pointed at. Nothing is written there — DryRun suppresses the write and
	// the parent-directory creation — so it need not exist. A per-process
	// path under os.TempDir() is the default; it is configurable only so a
	// test can pin it.
	DeployScratchRoot string
	Diag              io.Writer
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
	// Deploy renders the subject and each declared stub collaborator into the
	// sandbox before the adapter's Provision is called. Both frontends receive
	// the same value via RunnerDeps, matching every other collaborator on Deps.
	Deploy domain.AgentDeployer

	// Pass-through values the runner hands to the adapter without
	// interpreting, carried here so neither frontend has to know them.
	SelfPath        string
	LoggerBundleDir string

	// InterpreterCmd is the interpreter the selected adapter's
	// CheckEnvironment resolved during preflight, carried into runner.Deps
	// verbatim so the next stage can put it into the provision request.
	// Empty is legal: it means the selected adapter runs no interpreter.
	InterpreterCmd string

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
	// LoggerBundleDir is threaded into the claudecode adapter's own Options
	// so its CheckEnvironment below actually validates the bundle this
	// invocation resolved (flag, environment variable or the
	// binary-relative default) rather than always seeing an empty,
	// unchecked directory. opencode declares no bundle option: it runs no
	// interpreter and has nothing to validate here.
	adapter, err := newAdapter(cfg.HarnessID, adapterOptions{
		ClaudeCode: claudecode.Options{LoggerBundleDir: cfg.LoggerBundleDir},
	})
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
		WorkingDir:     cfg.MosaicRoot,
	})

	// The environment check runs once, here, before either frontend does
	// anything: its report's Problems become preflight errors (see
	// environmentBakedPreflight below), and its resolved interpreter is
	// carried into the wired dependency set both frontends run against —
	// never re-resolved by a frontend or by the runner.
	envReport, err := adapter.CheckEnvironment(context.Background())
	if err != nil {
		return Deps{}, fmt.Errorf("buildDeps: checking environment for harness %q: %w", cfg.HarnessID, err)
	}

	// The cost tool's availability is checked here, harness-neutrally,
	// rather than left to surface only once a run tries and fails to
	// attribute cost: an absent tool is folded into the same
	// EnvironmentReport every harness's bundle/interpreter problems already
	// travel through, so it reaches preflight through the one existing seam
	// (environmentBakedPreflight) rather than a second one.
	if problem, absent := costToolProblem(cfg.CostToolPath); absent {
		envReport.Problems = append(envReport.Problems, problem)
	}

	// The deployment tool is checked here for the same reason: an absent tool
	// should surface as a preflight diagnostic before any sandbox is created or
	// any cost is incurred, never discovered only when the first render fails
	// at setup time.
	if problem, absent := deployToolProblem(cfg.DeployToolPath); absent {
		envReport.Problems = append(envReport.Problems, problem)
	}

	// Construct the deployment port once, here. Both Deps.Deploy (for the
	// runner) and Deps.Preflight (for dry-run declaration validation) close
	// over the same value, so preflight and the runner can never validate
	// against one deployer and render with another.
	deployProvider := agentdeploy.New(agentdeploy.Options{
		ExecutablePath: cfg.DeployToolPath,
		MosaicRoot:     cfg.MosaicRoot,
		CatalogFolder:  cfg.CatalogFolder,
		Timeout:        cfg.DeployTimeout,
		Invoke:         cfg.DeployInvoke,
	})

	return Deps{
		Adapter:   adapter,
		Decoder:   decoder,
		Launcher:  launcher,
		Fixtures:  resolver,
		Effects:   effects,
		Cost:      costProvider,
		Clock:     systemClock{},
		Preflight: environmentBakedPreflight(envReport, deployProvider, cfg.DeployScratchRoot),
		Deploy:    deployProvider,

		SelfPath:        cfg.SelfPath,
		LoggerBundleDir: cfg.LoggerBundleDir,
		InterpreterCmd:  envReport.InterpreterCmd,

		HarnessID:     cfg.HarnessID,
		FixtureRoot:   cfg.FixtureRoot,
		WorkspaceRoot: cfg.WorkspaceRoot,
	}, nil
}

// costToolProblem reports whether path names a cost-analysis tool this
// process cannot find, and if so the EnvironmentProblem naming the path
// searched and the override that would change it — never a bare
// file-not-found, per AC6.2. A stat error other than "does not exist"
// (permission denied, for example) is treated the same way: either way the
// tool is not usable from here.
func costToolProblem(path string) (domain.EnvironmentProblem, bool) {
	if _, err := os.Stat(path); err != nil {
		return domain.EnvironmentProblem{
			Kind: domain.ProblemCostToolUnavailable,
			Detail: fmt.Sprintf(
				"cost-analysis tool not found at %s (override with --cost-tool or the MOSAIC_AGENT_TEST_COST_TOOL environment variable): %v",
				path, err,
			),
		}, true
	}
	return domain.EnvironmentProblem{}, false
}

// deployToolProblem reports whether path names a deployment tool this process
// cannot find, and if so the EnvironmentProblem naming the path searched and
// the override that would change it — never a bare file-not-found, mirroring
// costToolProblem exactly. A stat error of any kind (not found, permission
// denied) is treated the same way: either way the tool is not usable from here.
func deployToolProblem(path string) (domain.EnvironmentProblem, bool) {
	if _, err := os.Stat(path); err != nil {
		return domain.EnvironmentProblem{
			Kind: domain.ProblemDeployToolUnavailable,
			Detail: fmt.Sprintf(
				"deployment tool not found at %s (override with --deploy-tool or the MOSAIC_AGENT_TEST_DEPLOY_TOOL environment variable): %v",
				path, err,
			),
		}, true
	}
	return domain.EnvironmentProblem{}, false
}

// environmentBakedPreflight returns a preflight.Validate that always
// validates with env as the environment report and dep as the deployer, so
// both frontends validate declarations against the real catalogue without
// either one constructing a deployment port. This is the one place either
// value is baked in; a frontend that set them would be a second source of
// truth for the same dependency.
func environmentBakedPreflight(
	env domain.EnvironmentReport,
	dep domain.AgentDeployer,
	scratchRoot string,
) func(preflight.Input) (preflight.Plan, authoring.Report) {
	return func(in preflight.Input) (preflight.Plan, authoring.Report) {
		in.Environment = env
		in.Deploy = dep
		in.DeployScratchRoot = scratchRoot
		return preflight.Validate(in)
	}
}

// composedSuiteRunner adapts a Deps and a workspace.Manager, both bound at
// construction, into cli.SuiteRunner / tui.SuiteRunner. It is the one thing
// newSuiteRunner returns, and the only place the per-attempt runner.Deps a
// run needs is assembled — deferred to Run, where the run's own progress
// sink is finally known.
type composedSuiteRunner struct {
	deps      Deps
	ws        workspace.Manager
	retention domain.RetentionPolicy
}

func (r composedSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error) {
	rd := r.deps.RunnerDeps(r.ws, sink)
	s := suite.New(suite.Options{
		Runner:    testRunnerAdapter{deps: rd, retention: r.retention},
		Progress:  sink,
		Clock:     r.deps.Clock,
		Retention: r.retention,
	})
	return s.Run(ctx, p)
}

// testRunnerAdapter adapts runner.Run — a free function over runner.Deps —
// into suite.TestRunner, the interface the suite's scheduling drives one
// attempt through. retention is bound once, at construction, the same way
// runner.Deps' own SelfPath and LoggerBundleDir are: suite.TestRunner's
// signature carries no room for a per-invocation value, so the one
// resolved retention policy for this run travels here instead.
type testRunnerAdapter struct {
	deps      runner.Deps
	retention domain.RetentionPolicy
}

func (a testRunnerAdapter) Run(ctx context.Context, key domain.RunKey, t preflight.ResolvedTest, eval domain.AttemptEvaluator) (domain.TestResult, error) {
	return runner.Run(ctx, a.deps, runner.Request{Key: key, Test: t, Settings: t.Settings, Retention: a.retention}, eval)
}

// tuiSuiteRunner adapts a Deps and a workspace.Manager, both bound at
// construction, into tui.SuiteRunner. Unlike composedSuiteRunner (whose
// retention is fixed at construction, matching cli.SuiteRunner's
// per-invocation RunConfig), tui.SuiteRunner's Run carries retention as a
// call-time argument: the TUI wires one long-lived Suite for the whole
// process, but the suite-select screen's toggle can change the policy on
// every run, so the value has to travel with the call rather than with
// construction. Run simply forwards to a composedSuiteRunner built fresh with
// the call's retention, so the two frontends still funnel through the exact
// same suite-construction logic.
type tuiSuiteRunner struct {
	deps Deps
	ws   workspace.Manager
}

func (r tuiSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink, retention domain.RetentionPolicy) (report.Result, error) {
	return composedSuiteRunner{deps: r.deps, ws: r.ws, retention: retention}.Run(ctx, p, sink)
}

// newTUISuiteRunner mirrors newSuiteRunner's workspace-root resolution and
// error behaviour, but returns a tui.SuiteRunner whose retention is supplied
// per Run call instead of being fixed at construction — see tuiSuiteRunner's
// doc comment for why the TUI needs this and the CLI does not.
func newTUISuiteRunner(d Deps) (tui.SuiteRunner, error) {
	if d.WorkspaceRoot == "" {
		return nil, fmt.Errorf("newTUISuiteRunner: workspace root is required")
	}
	return tuiSuiteRunner{deps: d, ws: workspace.NewManager(d.WorkspaceRoot, d.Clock)}, nil
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
	return composedSuiteRunner{deps: d, ws: ws, retention: rc.Retention}, nil
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
		Deploy:     d.Deploy,

		SelfPath:        d.SelfPath,
		LoggerBundleDir: d.LoggerBundleDir,
		InterpreterCmd:  d.InterpreterCmd,
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
// osWriteFile is the real WriteFileFunc supplied to both frontends by the
// composition root. It creates any missing parent directories before writing,
// so a report path like "reports/run.json" works without a prior mkdir.
func osWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// defaultReportPath returns the JSON report file location used when neither
// --report-path nor --no-report is supplied. Both frontends default to the
// same value, resolved once here so they cannot drift apart.
func defaultReportPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return filepath.Join(cwd, "report.json")
}

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

		// Sourced from the tool-local supported set, never restated: the same
		// set newAdapter/decoderFor switch on, so the CLI's flag validation
		// cannot drift from what the composition root can actually wire.
		Harnesses: supportedHarnesses(),

		// Filtered to the same supported set as Harnesses, so the frontend
		// cannot accept a model identifier for a harness the binary cannot wire.
		Models: supportedModelCatalogs(),

		DefaultReportPath: defaultReportPath(),
		WriteFile:         osWriteFile,
	}
}

func tuiOptions(d Deps, suites []string) (tui.Options, error) {
	// The TUI's retention affordance is the in-screen toggle (Options.Retention,
	// changed by Model's Space binding), not a flag: it starts at
	// domain.RetainNever, the zero value, exactly as an unset --keep-sandbox
	// pair resolves the CLI's RunConfig.Retention to domain.RetainNever.
	//
	// Suite is wired through newTUISuiteRunner, not newSuiteRunner: the TUI's
	// Options.Suite is bound once for the process's lifetime, so it cannot
	// carry a retention value fixed at construction the way the CLI's
	// per-invocation factory does — Run receives the live Model.retention on
	// every call instead (see tuiSuiteRunner's doc comment).
	runnerForDefault, err := newTUISuiteRunner(d)
	if err != nil {
		return tui.Options{}, err
	}

	return tui.Options{
		Preflight: d.Preflight,
		Suite:     runnerForDefault,
		Suites:    suites,
		Harness:   d.HarnessID,
		Retention: domain.RetainNever,

		// Same supported-set source as cliOptions: both frontends offer the
		// identical selectable set, so neither can drift from the other or
		// from what the composition root can actually wire.
		Harnesses: supportedHarnesses(),

		// Filtered to the same supported set as Harnesses for the same reason
		// as cliOptions.Models: a model identifier for an unsupported harness
		// must not be selectable here either.
		Models: supportedModelCatalogs(),

		ReportPath: defaultReportPath(),
		WriteFile:  osWriteFile,
	}, nil
}
