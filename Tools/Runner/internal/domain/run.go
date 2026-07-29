package domain

// RunConfig carries all user-supplied configuration for a run.
type RunConfig struct {
	OrchestratorFilePath string               // path to the orchestrator agent file
	WorkflowID           WorkflowID           // selected workflow identifier
	Task                 string               // task description
	ArtifactLocation     string               // override, or "" for canonical path
	OnDeviation          DeviationMode        // how to handle deviations in non-interactive mode
	ExistingArtifact     ExistingArtifactMode // how a pre-existing Orchestration.md is handled
	AllowVersionDrift    bool                 // override version check
	Checkpoints          bool                 // checkpoint support
}

// DeviationMode controls non-interactive deviation handling.
type DeviationMode string

const (
	DeviationDelegate DeviationMode = "delegate" // default: delegate to orchestrator
	DeviationStop     DeviationMode = "stop"      // stop the run
)

// ExistingArtifactMode controls how a pre-existing Orchestration.md is handled.
type ExistingArtifactMode string

const (
	ExistingResume ExistingArtifactMode = "resume" // default
	ExistingFresh  ExistingArtifactMode = "fresh"
	ExistingFail   ExistingArtifactMode = "fail"
)

// RunOutcome is the result of a session run.
type RunOutcome struct {
	Status       RunStatus
	Message      string        // human-readable description
	ArtifactPath string        // path to the final Orchestration.md
	LastState    *CurrentState // nil if no invocation was made (e.g. refusal)
}

// RunStatus classifies the run's outcome for exit code mapping.
type RunStatus string

const (
	RunCompleted           RunStatus = "completed"           // run finished successfully
	RunStopped             RunStatus = "stopped"             // graceful stop, resumable
	RunDeviationUnresolved RunStatus = "deviation-unresolved" // deviation could not be resolved
	RunRefused             RunStatus = "refused"             // workflow or artifact refused before any invocation
	RunFailed              RunStatus = "failed"              // unexpected error
)
