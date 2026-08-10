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
	// the interception configuration, the bridge, the stub collaborator
	// definitions, and the MOSAIC logger bundle. It writes only under
	// req.Sandbox and returns a ledger of exactly what it created.
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
