package domain

// RunConfig carries all user-supplied configuration for a run.
type RunConfig struct {
	OrchestratorFilePath string        // path to the orchestrator agent file
	WorkflowID           WorkflowID    // selected workflow identifier
	Task                 string        // task description
	RunID                string        // resolved run_id (minted or from scan); empty triggers minting
	RunFolder            string        // resolved run-scoped folder path (absolute, e.g. "/workspace/Orchestration-20260727T170000Z-a3f9")
	OnDeviation          DeviationMode // how to handle deviations in non-interactive mode
	AllowVersionDrift    bool          // override version check
	Checkpoints          bool          // checkpoint support
	IsNewRun             bool          // true = create new artifact; false = resume existing

	// InfraClassSelections maps gated infrastructure class names to the selected
	// agent name for this run. Only populated when multiple agents of the same
	// gated class are declared. The TUI ConfigScreen wizard step and the CLI
	// --infra-class flag both populate this before calling session.Start.
	//
	// When a gated class has exactly one declared agent, that agent is auto-
	// selected and does not appear in this map.
	//
	// Non-gated classes (e.g. "review") are never in this map; all declared
	// agents of non-gated classes have their triggers evaluated unconditionally.
	InfraClassSelections map[string]string
}

// DeviationMode controls non-interactive deviation handling.
type DeviationMode string

const (
	DeviationDelegate DeviationMode = "delegate" // default: delegate to orchestrator
	DeviationStop     DeviationMode = "stop"      // stop the run
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
