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
}

// ArtifactStore is the single component that touches Orchestration.md.
// Implementations: file-based (production), in-memory (tests).
type ArtifactStore interface {
	// Read parses the Orchestration.md at the configured location and returns its state.
	// Returns RefusalError if the file exists but is not in the canonical format.
	// Returns os.ErrNotExist if no file exists at the location (normal for a new run).
	Read(ctx context.Context) (ArtifactState, error)

	// Create writes a new Orchestration.md with the initial frontmatter.
	// Returns an error if a file already exists at the location.
	Create(ctx context.Context, info WorkflowInfo, task string, checkpoints bool, now time.Time) (ArtifactState, error)

	// Apply records a completed step: updates current_state, appends an execution log
	// entry, upserts artifact registry entries for each output artifact, bumps
	// global_sequence and last_updated. The write is atomic (write-temp-then-rename).
	// The step's output artifact paths are recorded exactly as provided.
	// Workflow Notes are preserved unchanged.
	Apply(ctx context.Context, state ArtifactState, step CompletedStep) (ArtifactState, error)
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
