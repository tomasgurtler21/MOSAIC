package domain

// StatusCode is a Communication Protocol v1.8 status code.
type StatusCode string

const (
	StatusSUCCESS                StatusCode = "SUCCESS"
	StatusCOMPLETED_NEEDS_ACTION StatusCode = "COMPLETED_NEEDS_ACTION"
	StatusPARTIALLY_DONE         StatusCode = "PARTIALLY_DONE"
	StatusNEEDS_CLARIFICATION    StatusCode = "NEEDS_CLARIFICATION"
	StatusCAPABILITY_EXCEEDED    StatusCode = "CAPABILITY_EXCEEDED"
	StatusBLOCKED                StatusCode = "BLOCKED"
)

// ErrorCode is a Communication Protocol v1.8 error code (BLOCKED only).
type ErrorCode string

const (
	ErrorNone               ErrorCode = ""
	ErrorINPUT_NOT_FOUND    ErrorCode = "E101"
	ErrorDEPENDENCY_MISSING ErrorCode = "E401"
	ErrorTOOL_UNAVAILABLE   ErrorCode = "E501"
	ErrorPERMISSION_DENIED  ErrorCode = "E502"
	ErrorUSER_CONTACT       ErrorCode = "E503"
)

// ProtocolRequest is the Communication Protocol v1.8 invocation message
// sent to a subagent.
type ProtocolRequest struct {
	AgentInstanceID      string   `json:"agent_instance_id"`
	RunID                string   `json:"run_id,omitempty"`
	TaskDescription      string   `json:"task_description"`
	InputArtifacts       []string `json:"input_artifacts"`
	OutputArtifacts      []string `json:"output_artifacts"`
	InputFiles           []string `json:"input_files,omitempty"`
	OutputFiles          []string `json:"output_files,omitempty"`
	Constraints          string   `json:"constraints,omitempty"`
	IncludeResultSummary bool     `json:"include_result_summary"`
	HumanInTheLoop       bool     `json:"human_in_the_loop"`
}

// ProtocolResponse is the Communication Protocol v1.8 response from a subagent.
type ProtocolResponse struct {
	AgentInstanceID string     `json:"agent_instance_id"`
	RunID           string     `json:"run_id,omitempty"`
	StatusCode      StatusCode `json:"status_code"`
	StatusMessage   string     `json:"status_message"`
	ResultData      string     `json:"result_data,omitempty"`
	ErrorCode       ErrorCode  `json:"error_code,omitempty"`
	ErrorReason     string     `json:"error_reason,omitempty"`
}

// InvocationKind identifies the CLI invocation strategy the harness adapter
// should use for a given agent dispatch.
type InvocationKind string

const (
	// InvocationOrdinary is a standard single-agent dispatch. The adapter uses
	// --append-system-prompt-file to inject the agent definition while preserving
	// the CLI's default system prompt and <env> block.
	InvocationOrdinary InvocationKind = "ordinary"

	// InvocationOrchestrator is a deviation-resolution dispatch where the
	// orchestrator agent needs native subagent-spawning capability. The adapter
	// uses --agent <identifier> to launch the orchestrator as the CLI's primary
	// thread, and synthesizes an <env> block to compensate for the lost default
	// system prompt.
	InvocationOrchestrator InvocationKind = "orchestrator"
)

// AgentReference is a resolved agent: the workflow's identifier plus the
// path to the definition file the harness adapter needs.
type AgentReference struct {
	Identifier     string         // agent identifier from the routing table
	DefinitionPath string         // absolute path to the .md file in the orchestrator file's directory
	InvocationKind InvocationKind // CLI strategy: ordinary or orchestrator
}

// DispatchStep is everything the session needs to execute one invocation.
type DispatchStep struct {
	RowIndex      int            // which routing table row this invocation is for
	Agent         AgentReference
	Request       ProtocolRequest
	EffectiveHITL bool   // the computed HITL value, or an override
	Phase         string // the phase to record in the artifact
	Stage         string // "Stage-N" or "" -- what to write to the artifact
}
