package engine

import (
	"time"

	"mosaic-run/internal/domain"
)

// NextInput carries every input to the routing decision. It replaces the
// previous positional parameter list plus its trailing variadic StageSource,
// so that mode and the previously-dispatched output artifacts can be added
// without a further signature change and without a variadic tail that new
// parameters cannot safely precede.
type NextInput struct {
	Workflow domain.AdmittedWorkflow
	Stages   *domain.StageSet
	State    domain.ArtifactState

	// LastResponse is the response that triggered this decision, or nil when
	// there is none (the first decision of a new run).
	LastResponse *domain.ProtocolResponse

	// LastOutputArtifacts are the output artifact paths of the step that
	// produced LastResponse, exactly as they were dispatched. They are the
	// injection source for the auto-review auto-route-back and are ignored in
	// every other case. Empty or nil is valid and injects nothing.
	LastOutputArtifacts []string

	Agents          map[string]domain.AgentReference
	Seq             int
	Now             time.Time
	RefreshedStages *domain.StageSet
	StageSource     domain.StageSource

	// Mode selects the routing behaviour. ExecutionModeUnset is a programming
	// error: Next never coerces it to ExecutionModeOrchestrated or to any
	// other mode. On ExecutionModeUnset, Next returns a DeviationDecision of
	// kind DeviationAmbiguousRoute, surfacing the condition rather than
	// silently picking a mode.
	Mode domain.ExecutionMode
}
