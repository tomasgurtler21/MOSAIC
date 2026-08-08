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
	// Returns an error if a file already exists at the location.
	Create(ctx context.Context, info WorkflowInfo, task string, checkpoints bool, now time.Time, runID string) (ArtifactState, error)

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

// DeviationResolver handles situations where the engine cannot decide the next step.
// Implementations: orchestrator delegate, manual (user-driven).
type DeviationResolver interface {
	// Resolve presents the deviation to whoever can decide (orchestrator agent or user)
	// and returns an instruction for how to proceed.
	//
	// The deviating response, the artifact state at the time, and the planned
	// continuation point (where the happy path would have gone) are provided.
	//
	// The instruction may direct the runner to rejoin the happy path at a specific row,
	// to dispatch a custom invocation, or to stop the run.
	Resolve(ctx context.Context, info DeviationInfo) (RejoinInstruction, error)
}

// Clock provides the current time. Injected so the engine and artifact
// writes produce deterministic output in tests.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
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
)
