package transform

import "mosaic-deploy/internal/domain"

// WorkflowBlock is one workflow's [[SECTION:Workflow:{id}]] block, assembled verbatim into
// the orchestrator's [[INJECTION:AvailableWorkflows]] region.
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
	Injections           []InjectionOutcome
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

// InjectionAction classifies what happened to one injection region in the document body.
type InjectionAction string

const (
	// InjectionFilled means the harness module supplied content and it was written to the region.
	InjectionFilled InjectionAction = "filled-from-harness"
	// InjectionPreserved means content from the deployed file was copied into the region.
	InjectionPreserved InjectionAction = "preserved-from-deployed"
	// InjectionEmptied means the region was left empty (no harness content, no deployed content).
	InjectionEmptied InjectionAction = "left-empty"
	// InjectionAssembled means the AvailableWorkflows injection was assembled from Workflows.
	InjectionAssembled InjectionAction = "assembled-workflows"
	// InjectionAssembledInfra means the InfrastructureAgents injection was assembled from
	// selected infrastructure agents.
	InjectionAssembledInfra InjectionAction = "assembled-infrastructure"
	// InjectionOrphaned means content existed in the deployed file at an injection point that
	// no longer exists in the source; the content has been discarded.
	InjectionOrphaned InjectionAction = "orphaned"
	// InjectionAdded means an injection point present in the source was absent from the deployed
	// file; it starts empty.
	InjectionAdded InjectionAction = "added"
)

// InjectionOutcome records what happened to one injection point in the document body.
type InjectionOutcome struct {
	Name   string
	Class  domain.InjectionClass
	Action InjectionAction
	Bytes  int // byte length of the content placed in the region
}
