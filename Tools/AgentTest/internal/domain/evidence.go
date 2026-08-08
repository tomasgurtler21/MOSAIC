package domain

import "time"

// OrchestrationState is what the subject's orchestration document says at
// the end of the run (or at whatever point it was read).
//
// Present distinguishes "the document was read and is empty of a phase yet"
// from "the document could not be read at all" — a malformed or absent
// document is never reported as a valid zero state (see orchstate's
// ErrDocumentAbsent / ErrDocumentMalformed).
type OrchestrationState struct {
	Present    bool
	Phase      string // current_state.phase
	LastStatus string // current_state.last_status
	// LastErrorCode is current_state.error_code. It lives here and not on a
	// log row because the execution-log table has no error-code column: the
	// code is run state, recorded once for the most recent invocation.
	LastErrorCode string
	ExecutionLog  []ExecutionLogRow
}

// ExecutionLogRow mirrors one row of the orchestration document's
// [[SECTION:ExecutionLog]] table, whose columns are exactly:
//
//	Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint
//
// One field per column, no field without a column. There is deliberately no
// OutputArtifacts: the document records only the dispatched input_artifacts,
// so a field for outputs could never be populated and would read to a later
// implementer as parsing left unfinished.
type ExecutionLogRow struct {
	Seq           int       // Seq
	AgentInstance string    // Agent, "{AgentName}#{Seq}"
	Phase         string    // Phase
	Stage         string    // Stage
	Status        string    // Status
	Timestamp     time.Time // Timestamp, ISO-8601
	Summary       string    // Summary, the subagent's own status_message
	// InputArtifacts is the Inputs column, split on commas and trimmed.
	InputArtifacts []string
	// Checkpoint is the content-reference a checkpoint agent returned,
	// present only on that agent's own row.
	Checkpoint string
}
