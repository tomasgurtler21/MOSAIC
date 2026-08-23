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
	"sync"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/diaglog"
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

// HarnessBundle groups the per-harness collaborators that must agree with
// each other: the adapter, its decoder, its launcher, the environment report
// from its CheckEnvironment, and its resolved interpreter command. All are
// produced from one harness selection, so they cannot disagree.
type HarnessBundle struct {
	Adapter        domain.HarnessAdapter
	Decoder        launch.Decoder
	Launcher       domain.SubjectLauncher
	Environment    domain.EnvironmentReport
	InterpreterCmd string
}

// HarnessFactory constructs or retrieves a HarnessBundle for a given harness
// ID. It is safe for concurrent use. CheckEnvironment results are cached per
// harness ID so repeated runs with the same harness do not re-invoke it.
//
// Returns an error wrapping ErrUnknownHarness for an unrecognised ID.
type HarnessFactory interface {
	Bundle(ctx context.Context, harnessID string) (HarnessBundle, error)
}

// harnessCacheEntry is one lazily-constructed result, built at most once per
// harness ID via sync.Once.
type harnessCacheEntry struct {
	once   sync.Once
	bundle HarnessBundle
	err    error
}

// harnessCache implements HarnessFactory with lazy per-harness caching.
// The first Bundle call for a given ID invokes newAdapter, decoderFor, and
// CheckEnvironment exactly once; subsequent calls return the cached result.
type harnessCache struct {
	opts  adapterOptions
	mu    sync.Mutex
	items map[string]*harnessCacheEntry
}

func newHarnessFactory(opts adapterOptions) *harnessCache {
	return &harnessCache{
		opts:  opts,
		items: make(map[string]*harnessCacheEntry),
	}
}

// seed pre-populates the cache with a pre-built bundle for harnessID, so
// subsequent Bundle calls return the seeded value without any I/O. buildDeps
// uses this to store the default harness bundle (which includes
// harness-neutral tool-availability problems) before the factory is handed
// to the frontends.
func (f *harnessCache) seed(harnessID string, bundle HarnessBundle) {
	entry := &harnessCacheEntry{}
	entry.once.Do(func() {
		entry.bundle = bundle
	})
	f.mu.Lock()
	f.items[harnessID] = entry
	f.mu.Unlock()
}

// Bundle returns the cached HarnessBundle for harnessID, constructing it on
// the first call. The sync.Once inside each entry ensures CheckEnvironment is
// called at most once per harness ID, even under concurrent access.
func (f *harnessCache) Bundle(ctx context.Context, harnessID string) (HarnessBundle, error) {
	f.mu.Lock()
	entry, ok := f.items[harnessID]
	if !ok {
		entry = &harnessCacheEntry{}
		f.items[harnessID] = entry
	}
	f.mu.Unlock()

	entry.once.Do(func() {
		adapter, err := newAdapter(harnessID, f.opts)
		if err != nil {
			entry.err = err
			return
		}
		decoder, err := decoderFor(harnessID)
		if err != nil {
			entry.err = err
			return
		}
		envReport, err := adapter.CheckEnvironment(ctx)
		if err != nil {
			entry.err = fmt.Errorf("harness %q: checking environment: %w", harnessID, err)
			return
		}
		launcher := launch.New(decoder, launch.WithSinkFactory(diagSinkFactory))
		entry.bundle = HarnessBundle{
			Adapter:        adapter,
			Decoder:        decoder,
			Launcher:       launcher,
			Environment:    envReport,
			InterpreterCmd: envReport.InterpreterCmd,
		}
	})
	return entry.bundle, entry.err
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
	// SelfDir is the directory containing this binary, resolved once from
	// os.Executable(). It is the anchor for every binary-relative default
	// (LoggerBundleDir, CostToolPath, DeployToolPath, MosaicRoot) and is
	// exposed here so runTUIMode can derive the suite-discovery default
	// (SelfDir/..) without duplicating the os.Executable() computation.
	SelfDir         string
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
	// MosaicRootProvenance records which resolution tier produced MosaicRoot
	// and, for the default tier, the literal default expression. It is
	// threaded into preflight diagnostics so a user can see exactly which
	// configuration produced the failing path.
	MosaicRootProvenance ConfigProvenance
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
	// OutputMode is the active output presentation mode, pre-scanned from the
	// argument list before buildDeps runs. buildDeps passes it to
	// ResolveDiagnosticDestination to select the diagnostic sink destination.
	// Defaults to OutputModeCLI when the caller does not set it.
	OutputMode OutputMode
}

// Deps is the single wired dependency set this binary builds, once, for
// whichever frontend it dispatches to. Every field is a port value or a pure
// package's product, and no field is constructed anywhere but buildDeps.
//
// It exists so the same wired set is checkable by identity on every field:
// both frontend projections take exactly one Deps and call no constructor of
// their own, so "the same wired set" is a structural guarantee rather than a
// claim in a comment.
type Deps struct {
	// HarnessFactory resolves per-run adapter/decoder/launcher/environment
	// triads by harness ID, with CheckEnvironment results cached so repeated
	// runs with the same harness do not re-invoke it. Both frontends reach
	// their per-run runner through this factory (via NewSuiteRunner for the
	// TUI and via SuiteFactory for the CLI), never through single-valued
	// adapter/decoder/launcher fields on Deps.
	HarnessFactory HarnessFactory

	Fixtures       fixtures.Resolver
	FixtureFactory fixtures.ResolverFactory
	Effects        sideeffects.Applier
	Cost      domain.CostProvider
	Clock     domain.Clock
	Preflight func(preflight.Input) (preflight.Plan, authoring.Report)

	// Deploy renders the subject and each declared stub collaborator into the
	// sandbox before the adapter's Provision is called. Both frontends receive
	// the same value via RunnerDeps, matching every other collaborator on Deps.
	Deploy domain.AgentDeployer

	// NewDeployer constructs a deployer configured for the given catalog
	// folder. Called by composedSuiteRunner.Run when the plan's resolved
	// catalog folder differs from the process-wide default.
	//
	// The factory is trivially cheap (agentdeploy.New wraps options into a
	// struct, no I/O) and the returned deployer is immutable. Required field
	// -- the composition root always wires it.
	NewDeployer func(catalogFolder string) domain.AgentDeployer

	// CatalogFolder is the process-wide default catalog folder from
	// WiringConfig, threaded to the composition root's deployer resolution
	// logic and to preflight.Input so both can resolve overrides against it.
	CatalogFolder string

	// Pass-through values the runner hands to the adapter without
	// interpreting, carried here so neither frontend has to know them.
	SelfPath        string
	LoggerBundleDir string

	// DeployScratchRoot is the scratch directory for preflight dry-run
	// renders. Nothing is written there (dry-run suppresses the write and
	// directory creation). Stored here so NewSuiteRunner can construct
	// per-harness preflight closures using the same scratch root as the
	// process-start preflight.
	DeployScratchRoot string

	// The resolved defaults a frontend may surface or override.
	HarnessID     string
	FixtureRoot   string
	WorkspaceRoot string

	// SandboxDiagnostics is true when the composition root resolved the
	// diagnostic destination to DestRunSandbox. The runner reads this to
	// decide whether to stamp SpawnPlan.DiagnosticLog before launch.
	SandboxDiagnostics bool
}

// diagnosticSinkNopCloser is a trivial io.Closer for use when no file is opened
// by the diagnostic sink factory (for example when plan.DiagnosticLog is empty).
type diagnosticSinkNopCloser struct{}

func (diagnosticSinkNopCloser) Close() error { return nil }

// diagSinkFactory is the SinkFactory the launcher uses in production. It opens
// a file-backed Sink at plan.DiagnosticLog when that path is non-empty, writing
// an attribution header that names plan.RunID as the run identity. An empty
// DiagnosticLog means the runner resolved the destination to discard; the
// factory honours that by returning diaglog.Discard with no file opened.
//
// A factory failure degrades silently to the Discard sink: losing diagnostics
// must not fail a run whose subject behaved correctly.
func diagSinkFactory(plan domain.SpawnPlan) (commonharness.Sink, io.Closer, error) {
	if plan.DiagnosticLog == "" {
		return diaglog.Discard, diagnosticSinkNopCloser{}, nil
	}
	sink, err := diaglog.Open(plan.DiagnosticLog, plan.RunID)
	if err != nil {
		return diaglog.Discard, diagnosticSinkNopCloser{}, err
	}
	return sink, sink, nil
}

// buildDeps constructs every concrete collaborator exactly once. It is the
// only constructor call site in the binary outside newSuiteRunner's sandbox
// manager, and it is total: it either returns a fully populated Deps or an
// error, never a partially wired one.
//
// An unrecognised HarnessID surfaces here as ErrUnknownHarness, so the usage
// error is decided before any frontend starts.
func buildDeps(cfg WiringConfig) (Deps, error) {
	// Build the harness factory configured with adapter options. The default
	// harness bundle is constructed eagerly below (fail-fast on unknown or
	// broken default harness) and seeded into the factory's cache so
	// subsequent Bundle calls for the default harness return immediately
	// without re-invoking CheckEnvironment.
	//
	// LoggerBundleDir is threaded into the claudecode adapter's own Options
	// so its CheckEnvironment below actually validates the bundle this
	// invocation resolved (flag, environment variable or the binary-relative
	// default) rather than always seeing an empty, unchecked directory.
	// opencode declares no bundle option: it runs no interpreter and has
	// nothing to validate here.
	factory := newHarnessFactory(adapterOptions{
		ClaudeCode: claudecode.Options{LoggerBundleDir: cfg.LoggerBundleDir},
	})

	adapter, err := newAdapter(cfg.HarnessID, factory.opts)
	if err != nil {
		return Deps{}, err
	}
	decoder, err := decoderFor(cfg.HarnessID)
	if err != nil {
		return Deps{}, err
	}
	launcher := launch.New(decoder, launch.WithSinkFactory(diagSinkFactory))

	// The environment check runs once, here, for the default harness before
	// either frontend does anything: its report's Problems become preflight
	// errors (see environmentBakedPreflight below), and its resolved
	// interpreter is carried into the per-run runner.Deps via HarnessBundle.
	envReport, err := adapter.CheckEnvironment(context.Background())
	if err != nil {
		return Deps{}, fmt.Errorf("buildDeps: checking environment for harness %q: %w", cfg.HarnessID, err)
	}

	// The cost tool's availability is checked here, harness-neutrally,
	// rather than left to surface only once a run tries and fails to
	// attribute cost: an absent tool is folded into the same EnvironmentReport
	// the default harness bundle carries, so it reaches preflight through the
	// one existing seam (environmentBakedPreflight) rather than a second one.
	if problem, absent := costToolProblem(cfg.CostToolPath); absent {
		envReport.Problems = append(envReport.Problems, problem)
	}

	// The deployment tool is checked here for the same reason: an absent tool
	// should surface as a preflight diagnostic before any sandbox is created
	// or any cost is incurred, never discovered only when the first render
	// fails at setup time.
	if problem, absent := deployToolProblem(cfg.DeployToolPath); absent {
		envReport.Problems = append(envReport.Problems, problem)
	}

	// Seed the default harness bundle (with augmented environment report) into
	// the factory cache. This ensures the process-start preflight's environment
	// — including harness-neutral cost/deploy tool problems — is the same
	// environment the factory returns for the default harness on subsequent
	// Bundle calls, so a TUI run using the default harness sees the same
	// diagnostics as the CLI.
	defaultBundle := HarnessBundle{
		Adapter:        adapter,
		Decoder:        decoder,
		Launcher:       launcher,
		Environment:    envReport,
		InterpreterCmd: envReport.InterpreterCmd,
	}
	factory.seed(cfg.HarnessID, defaultBundle)

	resolver, err := fixtures.NewResolver(cfg.FixtureRoot)
	if err != nil {
		return Deps{}, fmt.Errorf("buildDeps: resolving fixture root %q: %w", cfg.FixtureRoot, err)
	}
	fixtureFactory, err := fixtures.NewResolverFactory(cfg.FixtureRoot)
	if err != nil {
		return Deps{}, fmt.Errorf("buildDeps: building resolver factory for %q: %w", cfg.FixtureRoot, err)
	}
	effects := sideeffects.NewContextualApplier(fixtureFactory)

	costProvider := cost.New(cost.Options{
		ExecutablePath: cfg.CostToolPath,
		Timeout:        cfg.CostTimeout,
		WorkingDir:     cfg.MosaicRoot,
	})

	// Construct the deployment port once, here. Both Deps.Deploy (for the
	// runner) and Deps.Preflight (for dry-run declaration validation) close
	// over the same value, so preflight and the runner can never validate
	// against one deployer and render with another.
	deployOpts := agentdeploy.Options{
		ExecutablePath: cfg.DeployToolPath,
		MosaicRoot:     cfg.MosaicRoot,
		CatalogFolder:  cfg.CatalogFolder,
		Timeout:        cfg.DeployTimeout,
		Invoke:         cfg.DeployInvoke,
	}
	deployProvider := agentdeploy.New(deployOpts)

	bakedPreflight := environmentBakedPreflight(defaultBundle.Environment, deployProvider, cfg.DeployScratchRoot)
	provenance := string(cfg.MosaicRootProvenance)

	// Resolve the diagnostic destination once at composition time so the
	// runner can stamp SpawnPlan.DiagnosticLog without knowing the output
	// mode itself. Every current mode resolves to DestRunSandbox; DestDiscard
	// exists so that remains an explicit decision rather than an omission.
	diagDest := ResolveDiagnosticDestination(cfg.OutputMode)

	return Deps{
		HarnessFactory: factory,
		Fixtures:       resolver,
		FixtureFactory: fixtureFactory,
		Effects:        effects,
		Cost:           costProvider,
		Clock:          systemClock{},
		Preflight: func(in preflight.Input) (preflight.Plan, authoring.Report) {
			in.MosaicRootProvenance = provenance
			in.CatalogFolder = cfg.CatalogFolder
			in.ResolverFactory = fixtureFactory
			return bakedPreflight(in)
		},
		Deploy: deployProvider,
		NewDeployer: func(catalogFolder string) domain.AgentDeployer {
			opts := agentdeploy.Options{
				ExecutablePath: deployOpts.ExecutablePath,
				MosaicRoot:     deployOpts.MosaicRoot,
				CatalogFolder:  catalogFolder,
				Timeout:        deployOpts.Timeout,
				Invoke:         deployOpts.Invoke,
			}
			return agentdeploy.New(opts)
		},
		CatalogFolder: cfg.CatalogFolder,

		SelfPath:          cfg.SelfPath,
		LoggerBundleDir:   cfg.LoggerBundleDir,
		DeployScratchRoot: cfg.DeployScratchRoot,

		HarnessID:     cfg.HarnessID,
		FixtureRoot:   cfg.FixtureRoot,
		WorkspaceRoot: cfg.WorkspaceRoot,

		SandboxDiagnostics: diagDest == DestRunSandbox,
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

// composedSuiteRunner adapts a Deps, a HarnessBundle, and a workspace.Manager,
// all bound at construction, into cli.SuiteRunner / tui.SuiteRunner. It is
// the one thing newSuiteRunner returns, and the only place the per-attempt
// runner.Deps a run needs is assembled — deferred to Run, where the run's
// own progress sink is finally known.
type composedSuiteRunner struct {
	deps   Deps
	bundle HarnessBundle
	ws     workspace.Manager

	retention domain.RetentionPolicy

	// maxConcurrentRuns is the resolved concurrency bound for this run. Zero
	// means "use suite.DefaultMaxConcurrentRuns" — the zero-means-default
	// convention suite.Options.MaxConcurrentRuns documents. It is threaded
	// into suite.Options.MaxConcurrentRuns when the suite is constructed.
	maxConcurrentRuns int

	// newSuite is an injectable constructor that replaces suite.New in tests,
	// allowing the caller to capture suite.Options before the suite is created
	// for field-for-field wiring assertions. nil uses suite.New.
	newSuite func(suite.Options) *suite.Suite
}

func (r composedSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error) {
	rd := r.deps.RunnerDeps(r.ws, sink, r.bundle)

	// Resolve the deployer for this run. When the plan's catalog folder
	// differs from the process-wide default, construct a per-run deployer
	// through the factory; otherwise rd.Deploy already carries the
	// process-wide deployer from RunnerDeps.
	if p.CatalogFolder != r.deps.CatalogFolder && r.deps.NewDeployer != nil {
		rd.Deploy = r.deps.NewDeployer(p.CatalogFolder)
	}

	ctor := r.newSuite
	if ctor == nil {
		ctor = suite.New
	}
	s := ctor(suite.Options{
		Runner:            testRunnerAdapter{deps: rd, retention: r.retention},
		Progress:          sink,
		Clock:             r.deps.Clock,
		Retention:         r.retention,
		MaxConcurrentRuns: r.maxConcurrentRuns,
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

// tuiSuiteRunner adapts a Deps, a HarnessBundle, and a workspace.Manager,
// all bound at construction, into tui.SuiteRunner. Unlike composedSuiteRunner
// (whose retention is fixed at construction, matching cli.SuiteRunner's
// per-invocation RunConfig), tui.SuiteRunner's Run carries retention as a
// call-time argument: the TUI wires one long-lived Suite for the whole
// process, but the suite-select screen's toggle can change the policy on
// every run, so the value has to travel with the call rather than with
// construction. Run simply forwards to a composedSuiteRunner built fresh with
// the call's retention, so the two frontends still funnel through the exact
// same suite-construction logic.
type tuiSuiteRunner struct {
	deps   Deps
	bundle HarnessBundle
	ws     workspace.Manager

	// maxConcurrentRuns is the resolved concurrency bound for this runner.
	// It is supplied by Options.NewSuiteRunner at run-start time, carrying
	// the live value the user configured on the settings screen. Unlike
	// retention (which travels as a call-time argument to Run), this value
	// is baked in at construction because the same per-call pattern is used:
	// a fresh tuiSuiteRunner is built for each run with the current bound.
	// Zero means suite.DefaultMaxConcurrentRuns.
	maxConcurrentRuns int

	// newSuite is an injectable constructor that replaces suite.New in tests,
	// allowing the caller to capture suite.Options before the suite is created
	// for field-for-field wiring assertions. nil uses suite.New.
	newSuite func(suite.Options) *suite.Suite
}

func (r tuiSuiteRunner) Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink, retention domain.RetentionPolicy) (report.Result, error) {
	return composedSuiteRunner{
		deps:              r.deps,
		bundle:            r.bundle,
		ws:                r.ws,
		retention:         retention,
		maxConcurrentRuns: r.maxConcurrentRuns,
		newSuite:          r.newSuite,
	}.Run(ctx, p, sink)
}

// newSuiteRunner binds a Deps to one invocation's resolved configuration and
// returns the runner both frontends consume. It is the single path from Deps
// to a running suite: the CLI reaches it through its SuiteFactory and the TUI
// reaches it through NewSuiteRunner, so a dependency added to one frontend's
// runs is added to the other's by construction.
//
// The sandbox manager is the one collaborator constructed here rather than in
// buildDeps, because its root is not known until the invocation resolves it.
// The HarnessBundle is resolved from HarnessFactory here so that both the
// adapter/launcher and the environment report come from the same harness
// selection, making it impossible for validation and execution to disagree.
func newSuiteRunner(d Deps, rc cli.RunConfig) (cli.SuiteRunner, error) {
	if rc.WorkspaceRoot == "" {
		return nil, fmt.Errorf("newSuiteRunner: workspace root is required")
	}
	bundle, err := d.HarnessFactory.Bundle(context.Background(), rc.HarnessID)
	if err != nil {
		return nil, fmt.Errorf("newSuiteRunner: harness %q: %w", rc.HarnessID, err)
	}
	ws := workspace.NewManager(rc.WorkspaceRoot, d.Clock)
	return composedSuiteRunner{deps: d, bundle: bundle, ws: ws, retention: rc.Retention, maxConcurrentRuns: rc.MaxConcurrentRuns}, nil
}

// RunnerDeps is the per-attempt collaborator set a Deps yields once the
// sandbox manager, the run's progress sink, and the resolved harness bundle
// are known. Both frontends' runs are executed against the value this returns,
// so a test can assert field-for-field that the CLI path and the TUI path run
// with the same values. The harness-specific fields (Adapter, Launcher,
// InterpreterCmd) come from bundle rather than from frozen Deps fields, so
// a per-run harness selection reaches the attempt without touching any shared
// state.
func (d Deps) RunnerDeps(ws workspace.Manager, progress domain.ProgressSink, bundle HarnessBundle) runner.Deps {
	return runner.Deps{
		Workspaces:     ws,
		Adapter:        bundle.Adapter,
		Launcher:       bundle.Launcher,
		Fixtures:       d.Fixtures,
		FixtureFactory: d.FixtureFactory,
		Effects:        d.Effects,
		Cost:           d.Cost,
		Clock:          d.Clock,
		Progress:       progress,
		Deploy:         d.Deploy,

		SelfPath:        d.SelfPath,
		LoggerBundleDir: d.LoggerBundleDir,
		InterpreterCmd:  bundle.InterpreterCmd,

		SandboxDiagnostics: d.SandboxDiagnostics,
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

// defaultReportPath returns a collision-free JSON report filename incorporating
// the suite name and a timestamp. suiteName is the suite's identifier
// (sanitised for filesystem safety). now is the time source.
// The filename follows the pattern report-<sanitized-suite>-<timestamp>.json
// and is relative to the current working directory.
func defaultReportPath(suiteName string, now time.Time) string {
	sanitized := sanitizeSuiteName(suiteName)
	timestamp := now.UTC().Format("20060102T150405")
	return fmt.Sprintf("report-%s-%s.json", sanitized, timestamp)
}

// sanitizeSuiteName replaces characters that are invalid in filenames with
// hyphens, so a suite name like "org/project/suite" becomes
// "org-project-suite" in the report filename. This keeps all components of
// the suite name visible while producing a valid, flat filename on all
// platforms.
func sanitizeSuiteName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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

		// ReportPathFor encodes the actual suite name and current time in the
		// report filename. The CLI parses the suite path from the invocation at
		// run time, so only the composition root needs to supply the naming
		// function here — the DefaultReportPath fallback is never reached when
		// ReportPathFor is set.
		ReportPathFor: func(suitePath string) string {
			return defaultReportPath(filepath.Base(suitePath), time.Now())
		},
		WriteFile: osWriteFile,
	}
}

func tuiOptions(d Deps, suites []string) (tui.Options, error) {
	// The TUI's retention affordance is the in-screen toggle (Options.Retention,
	// changed by Model's Space binding), not a flag: it starts at
	// domain.RetainNever, the zero value, exactly as an unset --keep-sandbox
	// pair resolves the CLI's RunConfig.Retention to domain.RetainNever.
	//
	// Suite and NewSuiteRunner are both wired here for different purposes:
	//
	//   Suite  — the process-lifetime runner using the default harness bundle.
	//             Used by the front end as a last-resort fallback and consulted
	//             by wiring tests that confirm the composition root is connected
	//             to the correct workspace root.
	//
	//   NewSuiteRunner — called fresh at each run-start, incorporating the live
	//             max-concurrent-runs bound and the selected harness. Returns
	//             both a runner and a per-harness PreflightFunc so validation
	//             and execution cannot disagree on the harness.
	//
	// Both share the same workspace manager so a single workspace root is used
	// for all runs regardless of which path is taken.
	if d.WorkspaceRoot == "" {
		return tui.Options{}, fmt.Errorf("tuiOptions: workspace root is required")
	}
	ws := workspace.NewManager(d.WorkspaceRoot, d.Clock)

	// Resolve the default harness bundle for the fallback Suite. This call
	// returns immediately because buildDeps already seeded the default bundle
	// into the factory cache. If resolution fails (misconfigured default
	// harness), surfacing the error here is the right behaviour: the TUI
	// cannot start without a working default.
	defaultBundle, err := d.HarnessFactory.Bundle(context.Background(), d.HarnessID)
	if err != nil {
		return tui.Options{}, fmt.Errorf("tuiOptions: default harness %q: %w", d.HarnessID, err)
	}

	return tui.Options{
		Preflight: d.Preflight,
		Suite:     tuiSuiteRunner{deps: d, bundle: defaultBundle, ws: ws},
		NewSuiteRunner: func(maxConcurrentRuns int, harnessID string) (tui.SuiteRunner, tui.PreflightFunc, error) {
			bundle, err := d.HarnessFactory.Bundle(context.Background(), harnessID)
			if err != nil {
				return nil, nil, fmt.Errorf("harness %q: %w", harnessID, err)
			}
			rawPf := environmentBakedPreflight(bundle.Environment, d.Deploy, d.DeployScratchRoot)
			pf := func(in preflight.Input) (preflight.Plan, authoring.Report) {
				in.ResolverFactory = d.FixtureFactory
				return rawPf(in)
			}
			r := tuiSuiteRunner{
				deps:              d,
				bundle:            bundle,
				ws:                ws,
				maxConcurrentRuns: maxConcurrentRuns,
			}
			return r, pf, nil
		},
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

		ReportPath: defaultReportPath("placeholder", time.Now()),

		// ReportPathFor encodes the selected suite name and current time in the
		// report filename when a run starts. The placeholder above is what the
		// suite-select screen shows before a suite is chosen; the actual
		// per-suite filename is computed here at the moment the user confirms a
		// run, so repeated runs of the same suite produce distinct report files.
		ReportPathFor: func(suitePath string) string {
			return defaultReportPath(filepath.Base(suitePath), time.Now())
		},
		WriteFile: osWriteFile,

		// Process-wide default catalog folder, editable in TUI.
		CatalogFolder: d.CatalogFolder,

		// ResolveSuiteDefaults reads the suite file and returns its declared
		// defaults so the TUI settings screen can display the number that will
		// actually be used rather than the generic "suite default" label. A
		// parse error or missing file returns an error, which resolveForSuite
		// handles by degrading to the unknown-provenance label — never a panic.
		ResolveSuiteDefaults: func(suitePath string) (tui.SuiteDefaults, error) {
			data, err := os.ReadFile(suitePath)
			if err != nil {
				return tui.SuiteDefaults{}, fmt.Errorf("reading suite file %q: %w", suitePath, err)
			}
			parsedSuite, suiteReport := authoring.ParseSuite(authoring.Source{Path: suitePath, Data: data})
			if suiteReport.HasErrors() {
				return tui.SuiteDefaults{}, fmt.Errorf("parsing suite file %q: %w", suitePath, suiteReport)
			}
			return tui.SuiteDefaults{Repetitions: parsedSuite.Defaults.Repetitions}, nil
		},
	}, nil
}
