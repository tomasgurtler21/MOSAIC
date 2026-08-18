package cli

// Exit codes for the mosaic-run CLI (FR-4).
// Values 0, 1, 3 match the deployment tool for consistency.
// Values 2, 4, 5 are runner-specific outcomes.
const (
	// ExitSuccess indicates the run completed successfully.
	ExitSuccess = 0

	// ExitFailure indicates an unexpected infrastructure error.
	ExitFailure = 1

	// ExitStopped indicates a resumable stop (graceful stop, state saved).
	// Reuses the same slot as the deployment tool's ExitWithSkips — conceptually
	// "completed but not fully".
	ExitStopped = 2

	// ExitUsage indicates an argument or flag parsing error.
	ExitUsage = 3

	// ExitRefused indicates the workflow or artifact was refused before any invocation.
	ExitRefused = 4

	// ExitDeviationUnresolved indicates a deviation could not be resolved; run state
	// is saved and the run can be resumed with --existing-artifact=resume.
	ExitDeviationUnresolved = 5

	// ExitStoppedByConsultant indicates the routing consultant issued a stop
	// instruction, or an orchestrator failure ended the run. The artifact is left
	// resumable; the caller may retry with --run <run_id>.
	ExitStoppedByConsultant = 6
)
