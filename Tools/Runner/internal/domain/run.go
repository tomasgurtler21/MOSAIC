package domain

// RunConfig carries all user-supplied configuration for a run.
type RunConfig struct {
	OrchestratorFilePath string        // path to the orchestrator agent file
	WorkflowID           WorkflowID    // selected workflow identifier
	Task                 string        // task description
	RunID                string        // resolved run_id (minted or from scan); empty triggers minting
	RunFolder            string        // resolved run-scoped folder path (absolute, e.g. "/workspace/Orchestration-20260727T170000Z-a3f9")
	AllowVersionDrift    bool          // override version check
	IsNewRun             bool          // true = create new artifact; false = resume existing

	// RunSettings holds every run-configuration decision that is settled at run
	// start, immutable for the run, and persisted in the artifact frontmatter so
	// a resumed run reads it back instead of re-asking. Embedding promotes all
	// seven fields (Mode, Checkpoints, Commits, CommitBranchVariant, CommitBranch,
	// PreConsultation, ManualResolution) directly onto RunConfig.
	RunSettings

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

	// SeedInputs holds user-supplied seed source paths, each naming a file or a
	// directory, in the order the user gave them. They are copied into the run
	// folder after it is created and before the first dispatch.
	//
	// Applied only when creating a new run. On a resume (IsNewRun false) this
	// field is ignored entirely — not validated, not copied — because files
	// already in a resumed run folder encode run state that re-copying would
	// silently rewind. A non-empty value on a resume is not an error at this
	// layer; the CLI already refuses --input together with --run.
	//
	// The CLI --input flag populates this. It is nil for TUI-started runs,
	// which therefore seed nothing.
	SeedInputs []string

	// ManualDispatch signals to the session layer that the user chose Manual
	// Dispatch from the stop recovery screen. When true, the session must use
	// ManualResolver for the first routing decision of this restart and then
	// return to the configured RoutingConsultant for subsequent decisions. When
	// false (the default and for normal Retry), the configured consultant handles
	// all routing decisions from the start.
	ManualDispatch bool
}

// RunSettings carries every run-configuration decision that is settled at run
// start, immutable for the run, and persisted in the artifact frontmatter so a
// resumed run reads it back instead of re-asking.
//
// It is passed to ArtifactStore.Create so all six decisions are recorded in
// the Orchestration.md frontmatter on run creation.
type RunSettings struct {
	// Mode is required. ExecutionModeUnset is a refusal at run start, both for
	// a new run and on resume.
	Mode ExecutionMode

	// Checkpoints enables checkpoint-class infrastructure dispatch.
	Checkpoints bool

	// Commits enables commit-class infrastructure dispatch.
	Commits bool

	// CommitBranchVariant selects which branch a commits-enabled run commits to.
	// Defaults to CommitBranchMOSAICOwned.
	CommitBranchVariant CommitBranchVariant

	// CommitBranch is the branch name the commit setup dispatch reported.
	// Empty when Commits is false.
	CommitBranch string

	// PreConsultation enables the one-shot run-start pre-consultation.
	PreConsultation bool

	// ManualResolution puts the user in the resolver's place.
	ManualResolution bool
}

// RunOutcome is the result of a session run.
type RunOutcome struct {
	Status       RunStatus
	Message      string        // human-readable description
	ArtifactPath string        // path to the final Orchestration.md
	LastState    *CurrentState // nil if no invocation was made (e.g. refusal)

	// StopReason carries the consultant's verbatim reason when Status is
	// RunStoppedByConsultant. Empty for every other status. It is what the CLI
	// prints and what the TUI stop screen presents above its recovery options.
	StopReason string
}

// RunStatus classifies the run's outcome for exit code mapping.
type RunStatus string

const (
	RunCompleted           RunStatus = "completed"            // run finished successfully
	RunStopped             RunStatus = "stopped"              // graceful stop, resumable
	RunDeviationUnresolved RunStatus = "deviation-unresolved" // deviation could not be resolved
	RunRefused             RunStatus = "refused"              // workflow or artifact refused before any invocation
	RunFailed              RunStatus = "failed"               // unexpected error

	// RunStoppedByConsultant: the routing consultant returned a stop instruction,
	// or an orchestrator failure ended the run. The artifact is left resumable.
	// The CLI prints StopReason and exits non-zero; the TUI presents StopReason
	// with retry and manual-dispatch recovery.
	RunStoppedByConsultant RunStatus = "stopped-by-consultant"
)
