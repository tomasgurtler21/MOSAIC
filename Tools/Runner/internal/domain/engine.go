package domain

// EngineDecision is the engine's answer to "what happens next".
// Exactly one of the fields is non-nil.
type EngineDecision struct {
	Dispatch  *DispatchDecision
	Complete  *CompleteDecision
	Deviation *DeviationDecision
	Consult   *ConsultDecision
	Stop      *StopDecision
}

// ConsultDecision signals that this decision belongs to the routing consultant,
// not to the engine. It is the only decision the engine produces in
// ExecutionModeOrchestrated.
//
// It differs from DeviationDecision in cause, not in handling: a Deviation
// means "the engine tried and could not decide"; a Consult means "the engine
// was never entitled to decide". Both are resolved by the same session routine
// and both produce a ConsultationRequest.
type ConsultDecision struct {
	// Trigger states why the consultant is being asked.
	Trigger ConsultTrigger

	// CurrentRow, CurrentPhase and CurrentStage describe the position the run
	// is at. CurrentRow is -1 on the first decision of a new run.
	CurrentRow   int
	CurrentPhase string
	CurrentStage string

	// ArtifactState is the state at the time of the decision.
	ArtifactState ArtifactState
}

// ConsultTrigger classifies why a consultation is being made.
type ConsultTrigger string

const (
	// ConsultTriggerOrchestratedMode: the run is in ExecutionModeOrchestrated,
	// where every routing decision is the consultant's.
	ConsultTriggerOrchestratedMode ConsultTrigger = "orchestrated-mode"

	// ConsultTriggerDeviation: the engine produced a DeviationDecision.
	ConsultTriggerDeviation ConsultTrigger = "deviation"
)

// DispatchDecision carries the steps to execute.
// The Steps slice contains exactly one element in this version, but carries
// a collection to preserve the concurrency seam so that parallel stage
// dispatch can be added later without rewriting the engine-session contract.
type DispatchDecision struct {
	Steps []DispatchStep
}

// CompleteDecision signals that the run has finished successfully.
type CompleteDecision struct {
	FinalState CurrentState // the current_state to write one last time
}

// DeviationDecision signals that the engine cannot decide deterministically.
type DeviationDecision struct {
	Info DeviationInfo
}

// StopDecision signals that the run should stop (e.g. graceful stop request).
type StopDecision struct {
	Reason string
}

// DeviationKind classifies why the engine could not decide.
type DeviationKind string

const (
	DeviationNonSuccess     DeviationKind = "non-success-status"
	DeviationAmbiguousRoute DeviationKind = "ambiguous-routing"
	DeviationHarnessError   DeviationKind = "harness-error"
)

// DeviationInfo carries everything the deviation resolver needs.
type DeviationInfo struct {
	Kind                DeviationKind
	Response            ProtocolResponse // the deviating response
	CurrentRow          int              // row index of the row that deviated
	CurrentPhase        string
	CurrentStage        string
	PlannedContinuation *DispatchStep // where the happy path would have gone (nil if unknown)
	ArtifactState       ArtifactState // current state at time of deviation
}

// ResumeInfo is the output of engine resume-point derivation: where to continue
// from after parsing an existing artifact.
type ResumeInfo struct {
	// RowIndex is the routing table row to dispatch next.
	// When RerunLast is true, this is the row of the interrupted invocation.
	// When RerunLast is false, this is the row after the last completed one.
	RowIndex int

	// Phase is the phase of the row identified by RowIndex.
	Phase string

	// Stage is the stage context for the row ("Stage-N" or "").
	Stage string

	// StageNumber is the stage number when inside a staged phase, or 0 otherwise.
	StageNumber StageNumber

	// GroupIndex is the index into AdmittedWorkflow.Groups for the row's
	// execution group (0 or 1), or -1 when the row is outside the staged phase.
	GroupIndex int

	// Seq is the global sequence number to use for the next invocation.
	// Derived from the artifact's global_sequence field.
	Seq int

	// RerunLast is true when the last logged invocation did not complete
	// (detected by comparing the execution log's last entry against the
	// current_state -- a mismatch indicates an interruption).
	// The session must re-dispatch the same row rather than advancing.
	RerunLast bool
}
