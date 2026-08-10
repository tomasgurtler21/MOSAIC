package domain

import (
	"encoding/json"
	"time"
)

// InterceptionPhase distinguishes the pre-invocation from the
// post-invocation interception point.
type InterceptionPhase string

const (
	PhasePre  InterceptionPhase = "pre"
	PhasePost InterceptionPhase = "post"

	// PhaseCompletion is the harness's own completion signal for a dispatched
	// collaborator — the event at which the real reply exists, on a harness
	// whose post-invocation point fires at launch.
	//
	// A call translated at this phase carries ObservedResponse (the recovered
	// reply) and CorrelationToken, and nothing else is required of it.
	// Observation only: an outcome for this phase is never a substitution, a
	// rewrite or a halt.
	PhaseCompletion InterceptionPhase = "completion"
)

// InterceptedCall is the normalized inbound model every harness adapter
// translates a native payload to.
type InterceptedCall struct {
	Phase            InterceptionPhase
	Identity         CollaboratorIdentity
	Message          TaskMessage
	CorrelationToken string          // opaque, innocuous; survives pre to post/completion
	RawPayload       json.RawMessage // the adapter's native payload, for diagnostics
	Capabilities     HarnessCapabilities
	// ObservedResponse is what the collaborator actually produced.
	// Populated on PhasePost and PhaseCompletion; the input to echo-fidelity
	// comparison. On PhaseCompletion, an empty CorrelationToken is a
	// legitimate outcome for an un-stubbed or partially-stubbed dispatch, not
	// an error: there is nothing pending to compare it against.
	ObservedResponse string
}

// OutcomeKind is the kind of decision an interception yields.
type OutcomeKind string

const (
	OutcomeSubstitute    OutcomeKind = "substitute"
	OutcomeRewritePrompt OutcomeKind = "rewrite_prompt"
	OutcomePassthrough   OutcomeKind = "passthrough"
	OutcomeHalt          OutcomeKind = "halt"
)

// InterceptionOutcome is the decision made for one intercepted call, plus
// the stub response payload and the reason for halting when applicable.
type InterceptionOutcome struct {
	Kind OutcomeKind

	// StubResponse is the payload to return directly. Set on OutcomeSubstitute.
	StubResponse json.RawMessage
	// RewrittenPrompt replaces the call's input. Set on OutcomeRewritePrompt.
	RewrittenPrompt string
	// CorrelationToken is echoed back so the adapter can plant it in whatever
	// field its harness preserves to the post-invocation point.
	CorrelationToken string

	HaltReason HaltReason
	// Message is surfaced to the harness on a halt. It must be plausible to
	// the subject and must contain no test vocabulary.
	Message string
}

// HaltReason names why an interception halted a call.
type HaltReason string

const (
	HaltNone             HaltReason = ""
	HaltEarlyExit        HaltReason = "early_exit"
	HaltUnmatched        HaltReason = "unmatched"
	HaltExtractionFailed HaltReason = "extraction_failed"
)

// HarnessCapabilities is what drives outcome selection. No harness name ever
// reaches a decision — only these flags do.
type HarnessCapabilities struct {
	// SupportsDirectSubstitution decides Substitute versus RewritePrompt.
	// False for a harness whose interception layer can only rewrite a call's
	// input; flipping it to true is the entire change needed if that gap
	// closes.
	SupportsDirectSubstitution bool

	// SupportsPostInterception is false for a harness with no post-invocation
	// interception point. Echo fidelity for such a harness must be recovered
	// from the run's residue rather than compared in-flight.
	SupportsPostInterception bool

	// CorrelationField names the native field the adapter plants the
	// correlation token in. Declared so the conformance suite can verify it.
	CorrelationField string

	// SupportsReplyRecovery declares that this adapter can observe the
	// collaborator's real reply through a later completion event, rather than
	// only whatever its post-invocation point happens to carry.
	//
	// It exists because SupportsPostInterception was true in letter and false
	// in spirit on a harness whose post point fires at launch and carries an
	// async acknowledgement. Echo fidelity is evaluated against the recovered
	// reply when this is true, and against the post point's observed response
	// when it is false. Echo fidelity itself stays unconditional either way.
	SupportsReplyRecovery bool

	RegistrationModel RegistrationModel
	BridgeKind        BridgeKind

	// ProducibleToolNames lists every normalized dispatch-tool name this
	// harness can actually produce at its interception point. Preflight
	// rejects a declared identity absent from this list, so an unproducible
	// identity fails before a run instead of silently going unmatched during
	// one.
	//
	// Returned as a fresh slice per call; the value is otherwise constant for
	// the adapter's lifetime, as the whole type is.
	ProducibleToolNames []string
}

// RegistrationModel is how a harness's interception configuration is
// registered.
type RegistrationModel string

const (
	// Plugin files placed in a directory; presence alone activates them.
	RegistrationDirectoryDrop RegistrationModel = "directory_drop"
	// Each bundle owns its own file in a hooks directory; purely additive.
	RegistrationPerBundleFile RegistrationModel = "per_bundle_file"
	// Every hook must be declared in one shared configuration document.
	RegistrationSharedFile RegistrationModel = "shared_file"
)

// BridgeKind is how a harness reaches the interceptor.
type BridgeKind string

const (
	// The harness spawns a command per intercepted call.
	BridgeSpawned BridgeKind = "spawned"
	// The harness loads a plugin in its own runtime; a generated shim
	// forwards to the same interceptor subcommand as a child process.
	BridgeInProcess BridgeKind = "in_process"
)

// ConfigScope is one configuration scope a harness merges.
type ConfigScope struct {
	Name       string // "sandbox" | "user" | "enterprise" | harness-specific
	Path       string
	InSandbox  bool
	Isolatable bool // the harness offers a way to neutralize this scope
}

// ScopeFinding reports what InspectScopes found about one configuration
// scope.
type ScopeFinding struct {
	Scope         ConfigScope
	RewritesInput bool // registers a hook that rewrites the intercepted call's input
	Neutralized   bool // the adapter isolated the scope rather than merely inspecting it

	// PreservesSubjectFunction declares that the subject can still run after
	// whatever this adapter did to the scope.
	//
	// Neutralized answers "was the configuration isolated". This answers
	// "and does the subject still work". They are different questions: an
	// adapter that relocates a scope holding both configuration and
	// credentials answers the first yes and the second no unless it seeds
	// the credentials back.
	//
	// An adapter that neither isolates nor breaks anything reports true: it
	// preserved function by not interfering.
	//
	// Conformance obligation: Neutralized && !PreservesSubjectFunction fails
	// the neutralization check. Isolating a scope at the cost of the
	// subject's ability to run is a failure, not a pass.
	PreservesSubjectFunction bool

	// FunctionDetail explains a false PreservesSubjectFunction, naming what
	// the isolation cost the subject.
	FunctionDetail string

	Detail string
}

// InterceptorSubcommand is the argv token that routes this binary to its
// interceptor frontend. It is owned here, in the one layer every package may
// import, because two packages must agree on it and neither may import the
// other: the composition root routes on it, and the runner puts it in
// ProvisionRequest.InterceptorArgs so the registered hook actually reaches
// the interceptor rather than falling through to the CLI frontend.
//
// It must never appear in help output: the subject under test may read this
// binary's usage text.
const InterceptorSubcommand = "intercept"

// ProvisionRequest is what an adapter needs to populate a sandbox.
type ProvisionRequest struct {
	Sandbox         Sandbox
	Subject         SubjectUnderTest
	Collaborators   []StubCollaborator // definitions standing in for real collaborators
	LoggerBundleDir string             // the MOSAIC logger bundle to deploy, read as data
	InterpreterCmd  string             // resolved and validated before provisioning
	InterceptorPath string             // absolute path of the running binary
	InterceptorArgs []string           // subcommand plus --workspace / --harness
}

// StubCollaborator is one collaborator definition an adapter writes into the
// sandbox in place of the real one.
type StubCollaborator struct {
	Identity   CollaboratorIdentity
	Definition []byte // the definition file content to write
	TargetPath string // relative to the sandbox subject dir
}

// Provisioning is the ledger of what an adapter installed. Deprovision
// removes exactly what this records and nothing else.
type Provisioning struct {
	Sandbox       Sandbox
	Files         []string // absolute paths, in creation order
	Dirs          []string // directories the adapter created, in creation order
	ScopeFindings []ScopeFinding

	// Sensitive lists absolute paths, a subset of Files, holding material that
	// must never be left on disk when a sandbox is retained — harness
	// credentials above all.
	//
	// Declaring it here rather than deleting it in the adapter is what lets a
	// harness-neutral retention path scrub without knowing what a credential
	// is called on any particular harness.
	Sensitive []string
}

// SpawnPlan is a declarative description of how to start the subject. The
// adapter builds it; something else — a SubjectLauncher — executes it.
type SpawnPlan struct {
	Executable string
	Args       []string
	WorkingDir string
	Env        []string
	Stdin      []byte
	Timeout    time.Duration
	// EarlyExitSentinel is the path the supervisor watches to terminate the
	// subject before its natural end.
	EarlyExitSentinel string
}
