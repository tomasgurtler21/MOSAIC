package domain

import (
	"context"
	"time"

	"mosaic-common/interaction"
)

// Interaction re-exports the shared interaction port from mosaic-common.
// It is the only channel through which a use case may consult a user.
// Both frontends (TUI and CLI) provide an implementation. No implementation
// may block indefinitely; the CLI implementation must never block.
// Notify and Progress never fail and never block.
type Interaction = interaction.Interaction

// HarnessAdapter is the seam between the runner and whatever actually runs a subagent.
// Implementations: fake (ships with this feature), real harness adapters (follow-up).
type HarnessAdapter interface {
	// Invoke dispatches a single subagent invocation and blocks until it completes,
	// is cancelled, or fails. The agent reference identifies the target; the request
	// carries the Communication Protocol v1.7 message.
	//
	// On success, returns the parsed protocol response.
	// On harness-level failure (timeout, crash, malformed output, missing response),
	// returns a non-nil error. The caller treats this as a deviation, never a crash.
	// On context cancellation, returns ctx.Err().
	Invoke(ctx context.Context, agent AgentReference, request ProtocolRequest) (ProtocolResponse, error)
}

// CompletedStep carries everything needed to record one completed invocation.
type CompletedStep struct {
	Seq             int
	AgentInstance   string     // "{AgentName}#{Seq}"
	Phase           string
	Stage           string     // "Stage-N" during EXECUTION, "" otherwise
	Status          StatusCode
	ErrorCode       ErrorCode  // populated only when Status == StatusBLOCKED
	Summary         string     // from ProtocolResponse.StatusMessage, truncated per format spec
	Timestamp       time.Time
	Checkpoint      string   // empty unless a checkpoint was taken
	OutputArtifacts []string // paths exactly as dispatched
	// Inputs carries the formatted comma-separated input_artifacts for the
	// Inputs column of the Execution Log row. Empty string when no inputs.
	Inputs string
	// IsInfrastructure is true when this step is an infrastructure agent
	// dispatch. The dispatch loop uses this to enforce the no-cascades rule:
	// when true, trigger evaluation is skipped after recording the step.
	IsInfrastructure bool
	// HITLRejected is true when this step was persisted as the result of an
	// HITL non-compliance rejection (redispatch or escalation), rather than as
	// a final accepted step. When true, the execution-log row records a
	// dispatch attempt that was subsequently superseded by a redispatch or
	// escalation -- it is not the step's final outcome.
	//
	// Resume semantics: a row with HITLRejected=true is "completed with
	// rejection". Because it is always recorded with IsInfrastructure=true,
	// Store.Apply does not update CurrentState for rejected rows. This means
	// ResumePoint sees the rejected row as a workflow log entry whose agent
	// does not match CurrentState.LastAgent, correctly concluding the step
	// was interrupted and setting RerunLast=true.
	//
	// The zero value (false) preserves backward compatibility: all existing
	// CompletedStep construction sites produce HITLRejected=false.
	HITLRejected bool
}

// ArtifactStore is the single component that touches Orchestration.md.
// Implementations: file-based (production), in-memory (tests).
type ArtifactStore interface {
	// Read parses the Orchestration.md at the configured location and returns its state.
	// Returns RefusalError if the file exists but is not in the canonical format.
	// Returns os.ErrNotExist if no file exists at the location (normal for a new run).
	Read(ctx context.Context) (ArtifactState, error)

	// Create writes a new Orchestration.md with the initial frontmatter.
	// The runID is stored in the frontmatter's run_id field, set once and never modified.
	// settings carries every run-configuration value the artifact must persist, so a
	// resumed run reads them back instead of re-asking the user.
	// Returns an error if a file already exists at the location.
	Create(ctx context.Context, info WorkflowInfo, task string, settings RunSettings, now time.Time, runID string) (ArtifactState, error)

	// Apply records a completed step: appends an execution log entry, upserts
	// artifact registry entries for each output artifact, and bumps
	// global_sequence and last_updated. The write is atomic
	// (write-temp-then-rename). The step's output artifact paths are recorded
	// exactly as provided. Workflow Notes are preserved unchanged.
	//
	// current_state is updated only when step.IsInfrastructure is false.
	// An infrastructure step leaves phase, stage, last_status, last_agent,
	// and error_code exactly as they were, on disk as well as in the
	// returned state, so the recorded workflow position continues to name
	// the last workflow step. Everything else above applies to
	// infrastructure steps unchanged: the invocation is fully recorded.
	Apply(ctx context.Context, state ArtifactState, step CompletedStep) (ArtifactState, error)

	// SetPhase updates only current_state.phase (and bumps last_updated and
	// global_sequence) without appending an execution log entry or modifying
	// the artifact registry. The write is atomic (write-temp-then-rename).
	//
	// This is the designated path for writing the COMPLETED phase marker after
	// the session finishes. Using Apply for this purpose would require a
	// synthetic CompletedStep with meaningless values, producing a spurious
	// execution log row.
	//
	// Returns the updated ArtifactState with the new phase, bumped sequence,
	// and updated timestamp.
	SetPhase(ctx context.Context, state ArtifactState, phase string, now time.Time) (ArtifactState, error)
}

// RoutingConsultant answers "what happens next" when the Runner cannot or
// must not decide for itself. Implementations: OrchestratorConsultant
// (script-mode orchestrator agent over the raw transport) and ManualResolver
// (the user, via the Interaction port).
//
// Exactly one routing decision is produced per call. The consultant is
// re-invoked for the next decision; no state carries between calls.
type RoutingConsultant interface {
	// ConsultRouting returns either a dispatch instruction or a stop instruction.
	// req.Context is always ConsultContextRouting.
	//
	// Returns a non-nil error for every failure class named in ConsultationFailure:
	// malformed payload, missing required field, unknown action, an agent absent from
	// the routing table, and a transport-level error. The error is always a
	// *ConsultationError so callers can classify without matching on message text.
	ConsultRouting(ctx context.Context, req ConsultationRequest) (RoutingInstruction, error)
}

// PreConsultant performs the opt-in, one-shot pre-consultation at run start
// (auto and auto-review only). Implemented by OrchestratorConsultant.
type PreConsultant interface {
	// PreConsult invokes the orchestrator with ConsultContextPreConsultation
	// and returns environment-level strings the Runner appends to auto-routed
	// dispatches only. Both returned fields may be empty; that is success, not failure.
	//
	// Returns a *ConsultationError on transport failure or a payload that is
	// not a JSON object. A pre-consultation failure refuses the run.
	PreConsult(ctx context.Context, req ConsultationRequest) (PreConsultationAdvice, error)
}

// ExecutableRevealer is an optional capability a HarnessAdapter may implement
// to report the executable path it was constructed with. It exists so that
// executable resolution can be asserted behaviorally at the composition root
// without reaching into concrete adapter types, and so that a launch failure
// can name the path that was tried.
//
// The fake adapter does not implement it: it spawns no process and has no
// executable path.
type ExecutableRevealer interface {
	// ExecutablePath returns the resolved executable path or command name the
	// adapter will spawn. It is never empty for a CLI-backed adapter that was
	// constructed with a per-harness default applied.
	ExecutablePath() string
}

// RawInvoker is the transport seam for payloads that are NOT Communication
// Protocol messages. The Runner-to-orchestrator consultation contract has its
// own request and response schemas, so HarnessAdapter.Invoke — which marshals a
// ProtocolRequest and unmarshals a ProtocolResponse — cannot carry it.
//
// Implementations live in leaf packages (internal/harness). The production
// adapters implement both HarnessAdapter and RawInvoker over the same spawner.
type RawInvoker interface {
	// InvokeRaw sends payload verbatim as the agent's prompt and returns the
	// agent's reply bytes verbatim, with no schema interpretation of either.
	//
	// agent.InvocationKind must be InvocationOrchestrator for orchestrator
	// consultations, so the agent retains native subagent-spawning capability.
	//
	// Returns a non-nil error on harness-level failure (timeout, crash, empty
	// output, non-zero exit). The returned bytes are the raw reply on success
	// and are never nil on a nil error.
	InvokeRaw(ctx context.Context, agent AgentReference, payload []byte) ([]byte, error)
}

// ApprovalReader reads the human_approved frontmatter field of an artifact
// file. Implemented in internal/artifact over the existing frontmatter parser.
//
// It never returns an error: every failure mode (missing file, no frontmatter
// block, malformed frontmatter, absent field, unparseable value) maps to a
// distinct HumanApproval value. A logging or read failure must not fail a run.
type ApprovalReader interface {
	ReadApproval(ctx context.Context, artifactPath string) HumanApproval
}

// ApprovalCapability is an optional companion to ApprovalReader by which a
// reader declares that it cannot read approvals at all — as opposed to
// reading them and finding a gate undischarged.
//
// A reader that does not implement this interface is treated as capable.
// Every production reader therefore needs no change.
//
// The session consults it once, at run start. When the reader reports false
// and the selected workflow declares at least one human-review row, the run
// is refused before any artifact is created, rather than each such row
// failing closed at execution time after spending its re-dispatch.
//
// This interface does not weaken the ApprovalReader contract: an incapable
// reader still returns ApprovalUnreadable from ReadApproval and still never
// returns an error.
type ApprovalCapability interface {
	ApprovalsReadable() bool
}

// Clock provides the current time. Injected so the engine and artifact
// writes produce deterministic output in tests.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// DispatchLogger records subagent dispatch requests and responses to a
// dedicated log. Each method serialises its argument as a complete JSON
// object and writes it as a single line (JSON Lines format).
//
// Contract: implementations never return an error, never panic, and never
// block a run. A logging failure (unwritable folder, closed file, disk full)
// must degrade silently to a no-op. Callers therefore never check a result
// and never guard a call.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type DispatchLogger interface {
	// LogRequest records the full protocol request dispatched to a subagent.
	// Called immediately before the harness invocation. The request is
	// serialised as-is with no field omission or truncation.
	LogRequest(req ProtocolRequest)

	// LogResponse records the full protocol response returned by a subagent.
	// Called immediately after the harness invocation succeeds. The response
	// is serialised as-is with no field omission or truncation.
	LogResponse(resp ProtocolResponse)

	// LogError records a harness-level error for an invocation that never
	// produced a ProtocolResponse. The agentInstanceID ties the error entry
	// to the preceding LogRequest entry for the same invocation. errText is
	// the result of err.Error() on the harness error.
	LogError(agentInstanceID string, errText string)

	// SetRunID associates the log with a run_id. Called before the first
	// write, it creates the file at RunnerLogs/{run_id}/{run_id}-dispatch.log;
	// called after, it records a correlation entry into the existing
	// out-of-run file (RunnerLogs/startup-{timestamp}-dispatch.log). An invalid
	// or empty runID is silently ignored. Safe to call more than once; only the
	// first effective call names the file.
	SetRunID(runID string)

	// Close permanently disables the logger. No further entries are written.
	// Safe to call on a disabled or never-used logger, and safe to call more
	// than once. Never returns an error.
	Close()

	// Path returns the absolute path of the log file. Returns "" only if no
	// file has ever been successfully created. Once a file has been created,
	// Path continues to return that path even after Close.
	Path() string
}

// DebugLogger is the seam between the runner and its always-on file-based
// debug log. It exists so that harness I/O, parse failures and dispatch events
// — including steps that never reach Orchestration.md — leave a diagnosable
// trace on disk.
//
// Contract: implementations never return an error, never panic, and never
// block a run. A logging failure (unwritable folder, closed file, disk full)
// must degrade silently to a no-op. Callers therefore never check a result and
// never guard a Log call.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// This port is intentionally minimal: one method, no levels, no rotation, no
// retention. It is a diagnostic record, not a logging framework.
type DebugLogger interface {
	// Log records one entry. event is a short dotted category from the
	// documented vocabulary (e.g. "harness.invoke.start"). message is the
	// entry body and may be multi-line and arbitrarily large (raw CLI output
	// is written in full, never truncated). fields carry optional structured
	// context and may be empty.
	Log(event string, message string, fields ...DebugField)
}

// DebugField is one key/value pair of structured context on a debug log entry.
// Values are rendered as-is; keep them short and single-line. Large or
// multi-line payloads belong in the entry message, not in a field.
//
// Value is a string and the caller is responsible for formatting. Numeric and
// duration values are pre-formatted by the caller as plain base-10 decimals
// with no unit suffix, no thousands separators and no padding.
type DebugField struct {
	Key   string
	Value string
}

// F is a shorthand constructor for a DebugField.
func F(key, value string) DebugField {
	return DebugField{Key: key, Value: value}
}

// Event name constants for the debug log. These are the only valid event names;
// emitters and tests use these constants so the vocabulary is a closed set.
// Adding an event requires extending this list.
const (
	EventRunnerStart                = "runner.start"
	EventRunnerRunID                = "runner.run_id"
	EventRunnerError                = "runner.error"
	EventHarnessInvokeStart         = "harness.invoke.start"
	EventHarnessStdout              = "harness.stdout"
	EventHarnessStderr              = "harness.stderr"
	EventHarnessInvokeOK            = "harness.invoke.ok"
	EventHarnessInvokeError         = "harness.invoke.error"
	EventHarnessParseFailed         = "harness.parse.failed"
	EventHarnessParseRecovered      = "harness.parse.recovered"
	EventSessionRefusal             = "session.refusal"
	EventSessionDispatchStart       = "session.dispatch.start"
	EventSessionStepDone            = "session.step.done"
	EventSessionHarnessError        = "session.harness.error"
	EventSessionDeviation           = "session.deviation"
	EventSessionDeviationUnresolved = "session.deviation.unresolved"
	EventSessionApplyFailed         = "session.apply.failed"
	EventArtifactPathRejected       = "artifact.path.rejected"
	EventArtifactPathNonRunScoped   = "artifact.path.non_run_scoped"

	// Consultation event names.
	EventSessionConsultStart    = "session.consult.start"
	EventSessionConsultDispatch = "session.consult.dispatch"
	EventSessionConsultStop     = "session.consult.stop"
	EventSessionConsultFailed   = "session.consult.failed"
	EventSessionPreConsult      = "session.preconsult"
	EventSessionCommitSetup     = "session.commit.setup"
	EventSessionHITLVerify      = "session.hitl.verify"
	EventSessionHITLRedispatch  = "session.hitl.redispatch"
	EventSessionHITLEscalate    = "session.hitl.escalate"
	EventSessionManualResolve   = "session.manual.resolve"

	// Snapshot event names.
	EventSnapshotCleanupFailed = "session.snapshot.cleanup_failed"
)
