package cli

import (
	"context"
	"io"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// PreflightFunc runs pre-flight validation over an authored suite and
// returns a fully resolved plan together with every diagnostic found.
// A plan is usable only when the accompanying Report has no errors.
//
// Matches internal/preflight.Validate's signature exactly, so the
// composition root passes that function directly.
type PreflightFunc func(preflight.Input) (preflight.Plan, authoring.Report)

// SuiteRunner executes a validated plan, streaming progress through sink,
// and returns the single result model both renderers and both frontends
// consume. Implemented by internal/suite.Suite.
type SuiteRunner interface {
	Run(ctx context.Context, p preflight.Plan, sink domain.ProgressSink) (report.Result, error)
}

// RunConfig is the invocation-resolved infrastructure configuration that
// must be known before the run's collaborators can be constructed. It
// carries only values the frontend resolves and cannot pass through
// preflight.Input, because they configure construction rather than
// validation.
type RunConfig struct {
	// WorkspaceRoot is the directory under which per-run sandboxes are
	// created. Always non-empty: it is the --workspace-root value when
	// the flag is present and Options.WorkspaceRoot otherwise.
	WorkspaceRoot string

	// Retention is the sandbox retention policy resolved from --keep-sandbox
	// and --keep-sandbox-on-failure: domain.RetainNever when neither is
	// present, domain.RetainOnFailure for --keep-sandbox-on-failure alone,
	// and domain.RetainAlways for --keep-sandbox alone or both together (the
	// stronger policy wins; specifying both is not an error).
	Retention domain.RetentionPolicy
}

// SuiteFactory binds an invocation's resolved configuration to a fully
// wired suite. It is supplied by the composition root and is the only
// thing that constructs anything; Execute calls it and constructs nothing
// itself, so AC15.8 still holds.
//
// The error return is for a configuration the wiring cannot satisfy — an
// unusable workspace root, most of all. It is an ExitFailure, never an
// ExitUsage: the invocation was well formed and the tool could not act on
// it.
type SuiteFactory func(RunConfig) (SuiteRunner, error)

// Options is the pre-wired dependency set the composition root hands in,
// following the ecosystem's convention (see Tools/LogAnalyzer/internal/cli
// and Tools/Runner/internal/cli): Execute constructs nothing itself.
type Options struct {
	Preflight PreflightFunc

	// Suite is bound late, because one invocation-resolved value — the
	// workspace root — is an argument to the sandbox manager's
	// construction rather than an argument to a run. Execute resolves
	// the root (the --workspace-root flag when present, WorkspaceRoot
	// otherwise), parsed at the single flag-parse site, and calls Suite
	// with that RunConfig exactly once, on the `run` path only. The
	// `validate` path never calls it: pre-flight creates no sandbox, so
	// nothing may construct a sandbox manager on that path.
	Suite SuiteFactory

	Stdout io.Writer
	Stderr io.Writer

	// Defaults the flags may override.
	WorkspaceRoot  string
	FixtureRoot    string
	DefaultHarness string

	// Harnesses is the selectable harness catalog, supplied by the
	// composition root from mosaic-common/harness. --harness is validated
	// against it: a value naming no catalog entry is a usage error, never a
	// silent default. The CLI restates no harness identity of its own, so
	// flag validation cannot drift from the catalog.
	Harnesses []commonharness.CLIHarness
}

// Execute runs the command line and returns the process exit code. It
// writes no code itself to the process; the composition root does that,
// unchanged.
//
// No path blocks on input. A frontend that waits for a human is a frontend
// that hangs a pipeline.
//
// Command surface: `run <suite>` pre-flights then executes the suite;
// `validate <suite>` pre-flights only and neither creates a sandbox nor
// spawns an agent. Both accept, in space-separated and equals-separated
// forms: --tests (comma-separated subset), --harness, --format (text|json),
// --fixtures, --workspace-root, --timeout, --repetitions, --logger-bundle
// and --cost-tool (both pre-scanned and consumed by the composition root;
// this package only recognises them so it does not reject them as
// unknown). --tui is recognised and ignored — it too is consumed by the
// composition root. --help/-h and a bare invocation (no arguments) both
// write the usage surface (see help.go) rather than only an error. An
// unknown flag or a flag missing its value is a usage error (ExitUsage).
//
// See run.go for the implementation: flag parsing, pre-flight dispatch, the
// deferred suite-factory call, progress streaming and output-format
// selection. See help.go for the usage surface itself: Commands, Flags,
// ValueConsumingFlags and Usage.
func Execute(ctx context.Context, args []string, o Options) int {
	return execute(ctx, args, o)
}
