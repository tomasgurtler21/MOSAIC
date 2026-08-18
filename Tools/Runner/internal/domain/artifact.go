package domain

import "time"

// InfrastructureOverride represents one agent's trigger override from the
// infrastructure_overrides frontmatter block. The YAML format uses agent
// names as map keys; this struct carries the key as AgentName for iteration
// convenience in the session.
//
// An override replaces (not merges with) the agent's declared trigger list
// for the duration of the run. An AgentName that does not match any declared
// infrastructure agent is a start-up error.
type InfrastructureOverride struct {
	AgentName string
	Triggers  []DeclaredInfraTrigger // replacement trigger list
}

// ArtifactState is the parsed content of Orchestration.md.
// It is the runner's only durable state.
type ArtifactState struct {
	// Frontmatter (Tier 1)
	RunID           string          // set once at creation, never modified; empty for pre-v1.8 artifacts
	Type            string          // "orchestration-artifact"
	Workflow        WorkflowID
	WorkflowVersion WorkflowVersion
	Task            string
	Started         time.Time
	LastUpdated     time.Time
	GlobalSequence  int

	// RunSettings holds every run-configuration decision that is settled at run
	// start, immutable for the run, and persisted in the artifact frontmatter so
	// a resumed run reads it back instead of re-asking. Embedding promotes all
	// seven fields (Mode, Checkpoints, Commits, CommitBranchVariant, CommitBranch,
	// PreConsultation, ManualResolution) directly onto ArtifactState.
	RunSettings

	// InfrastructureOverrides carries the optional infrastructure_overrides
	// frontmatter block. Nil when the block is absent (the common case).
	// Each entry replaces the named agent's declared trigger list at run start.
	InfrastructureOverrides []InfrastructureOverride
	CurrentState            CurrentState

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
	Inputs     string // comma-separated input_artifacts, or "" if none; "-" in the table
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
