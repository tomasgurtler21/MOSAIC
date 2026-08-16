package domain

import "fmt"

// ConsultContext tells the orchestrator which response schema the Runner expects.
type ConsultContext string

const (
	// ConsultContextRouting expects the two-action routing response.
	ConsultContextRouting ConsultContext = "routing"

	// ConsultContextPreConsultation expects the optional-string-pair pre-consultation response.
	ConsultContextPreConsultation ConsultContext = "pre_consultation"
)

// ConsultationRequest is the Runner's side of the consultation contract. It is
// deliberately minimal: the orchestration artifact is the orchestrator's single
// source of truth, and the only fact not derivable from it is the full
// untruncated status message of the agent that triggered the consultation.
type ConsultationRequest struct {
	// OrchestrationArtifact is the path to this run's Orchestration.md, as the
	// orchestrator should open it. Always non-empty.
	OrchestrationArtifact string

	// Context selects the expected response schema.
	Context ConsultContext

	// LastStatusMessage is the full, untruncated status_message of the
	// triggering agent. Nil — serialised as JSON null — on the first step of a
	// new run and for every pre-consultation. For a harness error it carries the
	// Runner-constructed error description rather than an agent message.
	LastStatusMessage *string

	// Deviation carries the engine's reason for consulting, for the benefit of
	// the ManualResolver's prompt only. It is NEVER serialised onto the wire:
	// the orchestrator derives its own context from the artifact. Nil when the
	// consultation was triggered by ExecutionModeOrchestrated rather than by a
	// deviation.
	Deviation *DeviationInfo
}

// RoutingInstruction is a routing consultant's answer. Exactly one field is
// non-nil; a value with neither or both set is a programming error and the
// session treats it as a consultation failure.
type RoutingInstruction struct {
	Dispatch *DispatchInstruction
	Stop     *StopInstruction
}

// DispatchInstruction directs the Runner to dispatch one agent. Optional
// fields are pointers so that "the consultant omitted this, fall back to the
// table row" is distinguishable from "the consultant explicitly supplied an
// empty value".
type DispatchInstruction struct {
	// Agent is the agent identifier, verbatim from the consultant. Required.
	Agent string

	// RowIndex is the routing table row the consultant's agent resolves to,
	// filled in by the consultant implementation before returning. It is the
	// row whose defaults supply every omitted field.
	RowIndex int

	// TaskDescription is used verbatim as the subagent's task description.
	// Required and non-empty.
	TaskDescription string

	// Constraints overrides table/deployment constraints when non-nil. A
	// non-nil pointer to "" means "dispatch with no constraints".
	Constraints *string

	// InputArtifacts overrides the row's Input column when non-nil. A non-nil
	// pointer to an empty slice means "dispatch with no input artifacts".
	InputArtifacts *[]string

	// OutputArtifacts overrides the row's Output column when non-nil, with the
	// same empty-slice semantics.
	OutputArtifacts *[]string

	// HITLOverride overrides the effective HITL when non-nil.
	HITLOverride *bool
}

// StopInstruction ends the run. The artifact is left exactly as it stands and
// remains resumable.
type StopInstruction struct {
	// Reason is human-readable and non-empty. It is surfaced in the CLI exit
	// message and the TUI stop screen, and recorded as the consultation row's
	// Execution Log summary.
	Reason string
}

// GenericTaskDescription is the entire task description for an auto-routed
// dispatch when the runner has no domain understanding to add. Pre-consultation
// advice, when non-empty, is appended after this baseline with a double newline.
const GenericTaskDescription = "Proceed with your task."

// PreConsultationAdvice carries environment-level strings the Runner appends
// mechanically to auto-routed dispatches. The Runner never interprets their
// content. Either or both may be empty; an entirely empty advice is a valid,
// successful pre-consultation.
type PreConsultationAdvice struct {
	TaskDescription string
	Constraints     string
}

// ConsultationFailure classifies why a consultation failed. Callers switch on
// it rather than matching message text.
type ConsultationFailure string

const (
	// ConsultFailTransport covers harness error, timeout, empty reply.
	ConsultFailTransport ConsultationFailure = "transport"

	// ConsultFailMalformedJSON covers a reply that is not valid JSON or not a JSON object.
	ConsultFailMalformedJSON ConsultationFailure = "malformed-json"

	// ConsultFailMissingField covers a required field that is absent or empty.
	ConsultFailMissingField ConsultationFailure = "missing-field"

	// ConsultFailUnknownAction covers an action value that is not "dispatch" or "stop".
	ConsultFailUnknownAction ConsultationFailure = "unknown-action"

	// ConsultFailUnknownAgent covers a dispatch agent identifier absent from the routing table.
	ConsultFailUnknownAgent ConsultationFailure = "unknown-agent"

	// ConsultFailUserAbandoned covers the manual resolver when the user exits without choosing.
	ConsultFailUserAbandoned ConsultationFailure = "user-abandoned"
)

// ConsultationError names the failing condition. Its message always states the
// specific condition; for ConsultFailUnknownAgent it additionally lists every
// agent identifier available in the routing table.
type ConsultationError struct {
	Failure ConsultationFailure
	Detail  string   // the specific condition, e.g. the missing field name
	Agents  []string // available routing table agents; set only for ConsultFailUnknownAgent
	Err     error    // wrapped transport or decode error, if any
}

func (e *ConsultationError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("consultation failed (%s): %s", e.Failure, e.Detail)
	}
	return fmt.Sprintf("consultation failed (%s)", e.Failure)
}

func (e *ConsultationError) Unwrap() error { return e.Err }
