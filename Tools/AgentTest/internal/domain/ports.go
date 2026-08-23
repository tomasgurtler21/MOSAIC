package domain

import "context"

// HarnessAdapter is the only place harness-specific knowledge may live.
// Implementations: internal/harness/claudecode, internal/harness/fake.
// No package outside internal/harness/ may import a concrete implementation.
//
// The conformance suite (internal/harness/contract) is the authority on its
// obligations; this port answers exactly the questions the suite asks.
type HarnessAdapter interface {
	// ID returns the stable harness identifier used to select a variant of a
	// hook bundle and to name the adapter in reports. Example: "claude-code".
	ID() string

	// Capabilities declares what this harness's interception layer can do.
	// The value MUST be constant for the lifetime of the adapter and MUST be
	// truthful: the conformance suite drives a stubbed invocation and rejects
	// an adapter whose observed effect contradicts what it declares here.
	Capabilities() HarnessCapabilities

	// ConfigScopes enumerates every configuration scope this harness merges,
	// including scopes outside the sandbox (user, enterprise). Ordered from
	// lowest to highest precedence.
	ConfigScopes() []ConfigScope

	// InspectScopes examines the non-sandbox scopes returned by ConfigScopes
	// and reports whether any of them registers a hook that rewrites the
	// intercepted call's input. The adapter never writes outside the sandbox;
	// where the harness offers scope isolation, the adapter reports the
	// scope as neutralized rather than merely inspected.
	InspectScopes(ctx context.Context) ([]ScopeFinding, error)

	// Provision installs everything this harness needs inside the sandbox:
	// the interception configuration, the bridge, and the MOSAIC logger
	// bundle. It writes only under req.Sandbox and returns a ledger of
	// exactly what it created.
	//
	// Provision MUST fail rather than proceed when more than one entry in
	// the composed configuration would rewrite the intercepted call's input.
	Provision(ctx context.Context, req ProvisionRequest) (Provisioning, error)

	// Deprovision removes exactly what Provisioning records and nothing
	// else. It is idempotent and must not fail when a recorded path is
	// already gone.
	Deprovision(ctx context.Context, p Provisioning) error

	// SpawnPlan describes how to start the subject. It performs no process
	// control itself; the returned plan is executed through a
	// SubjectLauncher.
	SpawnPlan(ctx context.Context, subject SubjectUnderTest, p Provisioning) (SpawnPlan, error)

	// TranslateCall converts a native interception payload into the
	// normalized model. phase distinguishes the pre-invocation from the
	// post-invocation interception point. An unrecognised or malformed
	// payload MUST be returned as an error, never a panic and never a
	// zero-valued call.
	TranslateCall(phase InterceptionPhase, native []byte) (InterceptedCall, error)

	// TranslateOutcome converts a decision back into whatever the harness's
	// interception point expects. call is the call the outcome answers, so
	// the adapter can echo native identifiers the harness requires.
	TranslateOutcome(outcome InterceptionOutcome, call InterceptedCall) ([]byte, error)

	// CheckEnvironment validates everything that must hold before the first
	// subject is spawned, and yields the values the neutral layers need from
	// the adapter — most importantly the resolved interpreter command.
	//
	// It performs no provisioning and spawns no subject. It is called during
	// preflight, before any cost is incurred.
	//
	// The returned report is always usable, even when problems were found:
	// Problems is the authority on whether the environment is usable, and
	// the error return is reserved for a fault that prevented the check
	// from being performed at all (never for a problem the check exists to
	// find).
	//
	// Honesty obligation, enforced by the conformance suite: an adapter MUST
	// NOT report a problem-free environment while having failed to resolve
	// something it needs, and MUST NOT report a resolved value it does not
	// actually use. An adapter that runs no interpreter leaves
	// EnvironmentReport.Interpreter zero-valued and says so in
	// InterpreterApplicable.
	CheckEnvironment(ctx context.Context) (EnvironmentReport, error)
}

// SubjectLauncher executes a SpawnPlan and reports what the subject did.
//
// It is a separate port from HarnessAdapter on purpose: process control
// must not live behind the harness port, or the per-test runner cannot both
// "spawn nothing itself" and "name no harness".
//
// Launch returns a SubjectResult on every path on which the subject actually
// started, including a run the caller cancelled. It returns an error only
// when the subject could not be started at all, and then the returned
// SubjectResult carries DispositionSpawnFailed rather than being
// zero-valued.
type SubjectLauncher interface {
	Launch(ctx context.Context, plan SpawnPlan) (SubjectResult, error)
}

// CostProvider reports the monetary cost recorded for a run. Implemented in
// internal/cost by delegating to the existing MOSAIC log-analysis
// capability. Never re-implements log parsing.
type CostProvider interface {
	// Cost reports the monetary cost recorded for a run.
	// Implementations MUST distinguish "cost is zero" from "cost could not
	// be attributed" via CostReport.Attribution; returning a zero total with
	// AttributionAttributed when attribution actually failed is a contract
	// violation, because it makes a broken run read as a free one.
	Cost(ctx context.Context, q CostQuery) (CostReport, error)
}

// ProgressSink receives ordered progress events.
//
// Contract: Emit never returns an error, never panics, and never blocks the
// caller. A sink that cannot keep up drops events rather than stalling a
// run. Implementations must be safe for concurrent use.
type ProgressSink interface {
	Emit(ev ProgressEvent)
}

// AgentDeployer renders one generic-form MOSAIC agent definition into one
// harness's form at a destination inside a sandbox. Implemented in
// internal/agentdeploy by invoking the compiled deployment tool; this module
// never imports that tool (see the harness isolation boundary), and never
// itself knows where a harness expects an agent file to live.
//
// Render reports the destination path the tool actually wrote and the source
// agent's version. Callers MUST use the reported path rather than
// reconstructing one: reconstructing it would reintroduce the harness-layout
// knowledge this port exists to keep out.
//
// Render never hangs: every implementation bounds the delegate's runtime and
// maps a timeout to an error like any other failure. It returns an error for
// every outcome in which nothing usable was produced, and a nil error only
// when the definition was written (or, under DryRun, fully validated).
//
// Render writes no hook file and no settings file. The harness adapter remains
// the sole author of the sandbox's composed configuration.
type AgentDeployer interface {
	Render(ctx context.Context, req RenderAgentRequest) (RenderAgentResult, error)

	// Deploy deploys an orchestrator together with every agent its selected
	// workflows reference, in one call, into a sandbox. It is the catalogue
	// path's counterpart to the per-agent Render loop.
	//
	// Deploy never hangs: every implementation bounds the delegate's runtime
	// and maps a timeout to an error like any other failure.
	//
	// Deploy never blocks on interactive input, and never permits the
	// delegate to resolve an unspecified selection to a silent default: the
	// implementation always supplies a workflow selection on the wire, and
	// always confirms the delegate's plan gate, including on a dry run.
	//
	// A nil error means every artifact the delegate planned was written
	// (or, under DryRun, fully validated). Deploy returns an error for
	// every outcome in which nothing usable was produced.
	//
	// Deploy writes no hook file and no settings file. The harness adapter
	// remains the sole author of the sandbox's composed configuration.
	Deploy(ctx context.Context, req DeployRequest) (DeployResult, error)
}

// RenderAgentRequest asks for one generic-form agent definition to be rendered
// into a sandbox. It is harness-neutral in everything but HarnessID, which is
// passed through from the selected adapter's own ID and never interpreted by
// the caller.
type RenderAgentRequest struct {
	// CatalogAgentKey names an agent in the product's real generic catalogue —
	// how the subject under test is sourced, so a run exercises the real,
	// current agent. Mutually exclusive with SourcePath; exactly one required.
	CatalogAgentKey string
	// SourcePath is a generic-form definition file in this module's own
	// workspace — how a stub collaborator is sourced, so test-only definitions
	// never enter the product catalogue. Mutually exclusive with CatalogAgentKey.
	SourcePath string

	// HarnessID is the harness to render for, taken from the selected adapter's ID.
	HarnessID string
	// WorkspaceRoot is where the rendered file is placed — the sandbox's subject
	// directory. The destination beneath it is resolved by the delegate, never
	// by this module.
	WorkspaceRoot string

	// Model is the model identifier the rendered agent declares. Empty leaves it
	// unset.
	Model string
	// Workflows pins the workflow set an orchestrator subject is rendered with.
	// nil means "not specified"; non-nil empty means "explicitly none". An
	// orchestrator rendered with a different workflow set is a materially
	// different agent, which is why a test can pin it rather than inherit a
	// default.
	Workflows []string

	// DryRun validates everything and writes nothing. Used by preflight to
	// refuse a bad declaration before a sandbox exists.
	DryRun bool
}

// DeployRequest asks for an orchestrator and every agent its selected
// workflows reference to be deployed into a sandbox in one call. It is
// harness-neutral in everything but HarnessID, which is passed through from
// the selected adapter's own ID and never interpreted by the caller.
//
// The catalogue the artifacts are sourced from is deliberately NOT a field
// here. It lives on agentdeploy.Options and is applied uniformly to every
// subprocess call, exactly as it is for Render.
type DeployRequest struct {
	// HarnessID is the harness to deploy for, taken from the selected
	// adapter's ID.
	HarnessID string

	// WorkspaceRoot is the sandbox's subject directory. Destinations beneath
	// it are resolved by the delegate, never by this module.
	WorkspaceRoot string

	// Workflows pins the workflow set to deploy. nil means "not specified";
	// non-nil empty means "explicitly none".
	//
	// Note the asymmetry with RenderAgentRequest: the implementation MUST
	// still emit a workflow selection on the wire for a nil value, because a
	// nil selection reaching the delegate resolves to an empty list, succeeds,
	// and deploys the orchestrator alone while silently omitting every
	// workflow-referenced agent. nil here means "the caller did not choose",
	// not "send nothing".
	Workflows []string

	// TierModels pre-answers model selection, keyed by tier name. Catalogue
	// agent definitions carry a model placeholder rather than a real
	// identifier, and identifiers are harness-specific, so a tier left
	// unanswered yields agents with no usable model on a run that still
	// reports success. Callers deploying a runnable subject supply an entry
	// for every tier the target catalogue declares.
	//
	// Tier names are caller vocabulary. This module and the agentdeploy port
	// both serialise the map verbatim and neither may spell a tier literal.
	//
	// A nil or empty map means "pre-answer nothing": no selections file is
	// written and no --selections argument is emitted, per the package's
	// empty-means-omitted convention.
	TierModels map[string]string

	// DryRun validates everything and writes nothing. Used by preflight to
	// refuse a bad declaration before a sandbox exists.
	DryRun bool

	// LogDir is the directory the delegate writes its own per-run log files to.
	//
	// Empty means "do not override" — the flag is omitted and the delegate
	// writes to its own default location under the shared repository root,
	// following this port's empty-means-omitted convention for root-scoped
	// overrides. A non-empty value is emitted as --log-dir so that the
	// delegate writes its two sink files (latest.log, history.log) there
	// instead.
	//
	// It exists because the delegate's default log location is derived from
	// the repository root, which is the same for every attempt: without an
	// override, N concurrent attempts truncate and append to the same two
	// files, outside every sandbox. The runner sets this to
	// sandbox.DeployLogDir() so each attempt writes to its own location
	// and the run's retention policy governs the logs uniformly.
	LogDir string
}

// DeployResult is what the delegate reported it did. Every field is reported
// data, never something this module reconstructed.
type DeployResult struct {
	// Agents are the agent definitions the delegate reported writing, in the
	// order it reported them. Non-agent artifacts the run also touched
	// (hooks, skills, manifests) are not listed: this port's callers place
	// agents and nothing else.
	Agents []DeployedAgent

	// CreatedDirectories are directories the delegate created, outermost
	// first, so a teardown ledger can record exactly what appeared. Empty
	// when the delegate reported none.
	CreatedDirectories []string
}

// DeployedAgent is one agent definition the delegate reported writing.
type DeployedAgent struct {
	// Key is the slug the definition was deployed under — the same
	// vocabulary a test definition's subject agent key and a workflow's
	// referenced-agent entry use. It is how a caller identifies the subject
	// among everything the run deployed.
	Key string

	// DestinationPath is the absolute path the delegate wrote. The subject's
	// sandbox-relative definition path is derived from this and from nothing
	// else. Callers MUST NOT reconstruct it from harness layout knowledge.
	DestinationPath string
}

// RenderAgentResult is what the delegate reported it did. Every field is
// reported data, never something this module reconstructed.
type RenderAgentResult struct {
	// DestinationPath is the absolute path the delegate wrote. The subject's
	// sandbox-relative definition path is derived from this and from nothing
	// else.
	DestinationPath string
	// CreatedDirectories are directories the delegate created, outermost first,
	// so a teardown ledger can record exactly what appeared.
	CreatedDirectories []string
	// SourceVersion is the source agent's declared version. Empty means the
	// source declared none — a legal state that must be reported as unknown,
	// never as a blank that reads like a real value.
	SourceVersion string
	// AgentKey is the slug the definition was rendered under.
	AgentKey string
}
