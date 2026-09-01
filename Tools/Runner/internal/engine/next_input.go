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

	// ArtifactRegistry carries the current artifact registry entries from
	// the ArtifactState, with run-scoped path prefixes stripped.
	//
	// The engine uses it as a read-only lookup to detect whether a target
	// row's resolved output artifacts have already been created in a prior
	// invocation, enabling the FR-7/FR-8 creator-artifact injection on
	// review loop-back. Nil or empty means no prior artifacts exist
	// (first-invocation case; no injection occurs).
	//
	// COMPARISON CONTRACT: The engine compares registry entries against
	// step.Request.OutputArtifacts -- the resolved, bare paths produced by
	// buildDispatchStep for the target row -- NOT against raw
	// row.OutputArtifacts (which may still contain unresolved template
	// tokens like {StageNumber} or Stage-*). This ensures injection fires
	// correctly even for rows whose declared output artifacts use templates.
	//
	// NORMALIZATION INVARIANT: Each entry's Artifact field must contain the
	// bare (non-run-scoped) path, matching the format of resolved artifact
	// paths (post-ResolveArtifacts, pre-run-scoping). Session is responsible
	// for stripping the run-scoped prefix via StripRunPrefix before
	// populating this field. The engine never performs path normalization.
	//
	// This field is populated by session from state.ArtifactRegistry
	// (after prefix stripping) before calling Next. The engine never
	// mutates it.
	ArtifactRegistry []domain.ArtifactRegistryEntry
}
