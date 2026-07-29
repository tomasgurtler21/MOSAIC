package domain

import "time"

// ArtifactState is the parsed content of Orchestration.md.
// It is the runner's only durable state.
type ArtifactState struct {
	// Frontmatter (Tier 1)
	Type            string          // "orchestration-artifact"
	Workflow        WorkflowID
	WorkflowVersion WorkflowVersion
	Task            string
	Started         time.Time
	LastUpdated     time.Time
	GlobalSequence  int
	Checkpoints     bool         // true = enabled
	CurrentState    CurrentState

	// Structured sections (Tier 2)
	ExecutionLog     []ExecutionLogEntry
	ArtifactRegistry []ArtifactRegistryEntry

	// Opaque section (Tier 3) -- preserved on write, never modified by the runner
	WorkflowNotes []WorkflowNote
}

// CurrentState is the mutable nested block in the frontmatter.
type CurrentState struct {
	Phase      string     // standard workflow phase
	Stage      string     // "Stage-N" during EXECUTION with stages, "" otherwise
	LastStatus StatusCode // zero value before any invocation
	LastAgent  string     // "{AgentName}#{Seq}", "" before any invocation
	ErrorCode  ErrorCode  // populated only when LastStatus is BLOCKED
}

// ExecutionLogEntry is one row of the Execution Log table (append-only).
type ExecutionLogEntry struct {
	Seq        int
	Agent      string     // "{AgentName}#{Seq}"
	Phase      string
	Stage      string     // "Stage-N" during EXECUTION, "" otherwise
	Status     StatusCode
	Timestamp  time.Time
	Summary    string // from ProtocolResponse.StatusMessage, truncated
	Checkpoint string // "" unless a checkpoint was taken
}

// ArtifactRegistryEntry is one row of the Artifacts table (keyed registry).
// Keyed by Artifact path; upserted on rework.
type ArtifactRegistryEntry struct {
	Artifact  string // path, exactly as dispatched -- the table's key
	CreatedIn string // "Phase" or "Phase.Stage"
	CreatedBy string // "{AgentName}#{Seq}"
}

// WorkflowNote is one row of the Workflow Notes table.
// The runner reads and preserves these but never writes them.
// Only the orchestrator agent writes these during deviation handling.
type WorkflowNote struct {
	Seq  int
	Note string
}
