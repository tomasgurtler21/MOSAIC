package transform

import "mosaic-deploy/internal/domain"

// WorkflowBlock is one workflow's <Workflow type="core" name="{id}"> block, assembled verbatim
// into the orchestrator's <AvailableWorkflows type="project"> region.
type WorkflowBlock struct {
	ID    string
	Block []byte // the section block including its boundary tags, in source order
}

// Request carries every input that Apply needs. Apply performs no filesystem, network,
// clock, or randomness operations; all inputs arrive here (CD-7, AC8.2).
type Request struct {
	Source       []byte              // the generic source file bytes, verbatim
	Kind         domain.ArtifactKind
	Key          string              // artifact slug (agent key, skill key, …)
	Module       domain.HarnessModule
	Model        domain.ModelSelection
	CustomTools  map[string]string   // generic tool name → user-supplied MCP server name
	SkippedTools map[string]bool
	Scope        domain.Scope
	// Deployed is the currently-deployed file bytes, or nil on create. Injection content
	// for InjectionProject class is lifted from here and reinstated in the output.
	Deployed             []byte
	Workflows            []WorkflowBlock        // non-empty only for the orchestrator agent
	InfrastructureAgents []InfrastructureBlock  // non-empty only for the orchestrator agent
	// ToolMappingsVersion is the hash of the effective tool-destination mapping set for this
	// run, computed by config.HashToolDestinations. It is written to the deployed file as the
	// `tool_mappings_version` frontmatter stamp so the planner can detect staleness on
	// subsequent runs when the config-declared mapping set changes. An empty string means no
	// config mappings are active and no stamp is written to the deployed file.
	ToolMappingsVersion string

	// Role is the deploying agent's role. It selects the protocol variant for
	// <CommunicationProtocol type="managed"> regions.
	Role domain.AgentRole

	// Protocol carries the role-keyed protocol blocks and the protocol source version,
	// loaded once per run by the app layer. Required for agents whose source declares a
	// <CommunicationProtocol type="managed"> region.
	Protocol domain.ProtocolContent

	// Bundle carries the canonical blocks and the bundle version, loaded once per run by
	// the app layer. Required for agents whose source declares any bundle-sourced managed
	// region; ignored for agents that declare none.
	Bundle domain.BundleContent

	// InjectionRenames overrides the package-level InjectionRenames table for this call.
	// Nil means use transform.InjectionRenames. Intended for tests so that conflict,
	// chain, precedence, and no-op cases are exercisable without mutating package state.
	InjectionRenames []RenameEntry

	// Timestamp is an RFC3339 UTC instant supplied by the caller that owns the clock.
	// The transform never reads a clock (enforced by the build-time purity check). It is
	// stamped onto parking TODO entries so a notice remains meaningful after the wholesale
	// regeneration of the TODO report. Empty is legal and omits the timestamp clause from
	// the parking gap detail message.
	Timestamp string

	// InjectionsVersion is the harness descriptor's injection version for non-orchestrator
	// agents. Used by applyHarnessRegion to stamp version attributes on InjectionHarness-class
	// regions. Populated by the app layer from desc.InjectionsVersion. Empty string means no
	// version attribute is written.
	InjectionsVersion string

	// OrchestratorInjectionsVersion is the injection version for orchestrator agents.
	// Used by applyHarnessRegion when req.Key == "orchestrator" to select the orchestrator-
	// specific value. Populated by the app layer from desc.OrchestratorInjectionsVersion.
	// Empty string means no version attribute is written for orchestrators.
	OrchestratorInjectionsVersion string
}

// Result is the output of a successful Apply call.
type Result struct {
	Output []byte
	Report Report
}

// Report is the audit trail of one transformation. It is the input to per-item update
// logging, gap collection, and the TODO checklist (AC8.5, AC15.2).
type Report struct {
	Fields               []FieldChange
	Tools                []domain.ToolResolution
	Regions              []RegionOutcome    // covers both injection and managed regions
	Gaps                 []domain.Gap
	Workflows            []string // workflow IDs present in the assembled injection, in emitted order
	InfrastructureAgents []string // agent keys present in the assembled InfrastructureAgents injection, in emitted order
	OutputBytes          int
}

// FieldChange records what happened to one frontmatter field: whether it was added,
// overwritten, or removed, and why. Before is empty when the field was added; After is
// empty when the field was dropped.
type FieldChange struct {
	Key    string
	Before string // rendered form of the value before the transform; empty when added
	After  string // rendered form of the value after the transform; empty when dropped
	Reason string // human-readable rationale, e.g. "descriptor add", "model selection", "version stamp"
}

