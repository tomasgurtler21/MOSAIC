package app

// render_contracts.go declares the public contract for the single-agent render mode:
// request type, result type, sentinel errors, and the Service method binding.
// The real implementation lives in render_service_impl.go.
//
// Sentinel naming follows the ErrRender<Condition> pattern used by ErrPromote* and
// ErrTransform*. Feature sentinels live here, feature-adjacent to the types they belong to;
// they are not placed in sentinel.go (which holds one unrelated family).

import (
	"context"
	"errors"
)

// RenderAgentRequest drives the single-agent render mode. Unlike every other request type in
// this package it pre-answers nothing and asks nothing: the mode has no interactive
// fallback, so there is no SkipAll field and no domain.QuestionID is ever raised.
type RenderAgentRequest struct {
	// SourcePath is a generic-form MOSAIC agent file at any path, inside or outside the
	// catalogue. Mutually exclusive with SourceAgentKey; exactly one is required.
	SourcePath string
	// SourceAgentKey names an agent in the catalog (Catalog/Subagents/ or Catalog/UtilityAgents/). Mutually exclusive
	// with SourcePath; exactly one is required.
	SourceAgentKey string

	// TargetHarnessID is the harness to render for. Required; there is no interactive
	// fallback.
	TargetHarnessID string

	// DestinationPath is the exact file path to write. Mutually exclusive with
	// WorkspaceRoot; exactly one is required.
	DestinationPath string
	// WorkspaceRoot is a workspace root under which the target harness descriptor's own
	// project-scope path for this agent key is used. Mutually exclusive with
	// DestinationPath; exactly one is required.
	WorkspaceRoot string

	// TargetModel is the model identifier written into the output. Empty leaves the
	// output's model field empty; no question is asked.
	TargetModel string

	// Workflows is the workflow set an orchestrator is rendered with. nil means "not
	// specified"; a non-nil empty slice means "explicitly none" and is honoured as such.
	// Meaningful only when the resolved role is orchestrator.
	Workflows []string

	// InfrastructureAgents is the infrastructure-agent set an orchestrator is rendered
	// with, following the same nil/empty convention. Kept separate from Workflows because
	// the two fill different regions of the orchestrator and an orchestrator may
	// legitimately have one without the other.
	InfrastructureAgents []string

	// Overwrite permits replacing an existing destination file. Without it, an existing
	// destination is ErrRenderDestinationExists, never a silent overwrite.
	Overwrite bool
	// DryRun computes and validates every outcome but writes no file and creates no
	// directory. The overwrite check still runs so a preview sees what a real run would
	// refuse.
	DryRun bool
}

// RenderAgentResult describes what a successful render produced. It is populated only when
// RenderAgent returns a nil error; on any failure it is the zero value, so a caller may
// treat err != nil as "nothing was written" without inspecting it.
type RenderAgentResult struct {
	// SourcePath is the generic file that was read, whether it was named directly or
	// resolved from the catalogue.
	SourcePath string `json:"sourcePath"`
	// SourceAgentKey is the catalogue key when the source was resolved from the catalogue,
	// empty when a path was named directly.
	SourceAgentKey string `json:"sourceAgentKey,omitempty"`
	// AgentKey is the artifact slug the output was rendered under.
	AgentKey string `json:"agentKey"`
	// SourceVersion is the source file's `version` frontmatter scalar. Empty when the
	// source declared none — a legal state that the consumer renders as unknown rather
	// than blank.
	SourceVersion string `json:"sourceVersion"`
	// Role is the resolved agent role, "orchestrator" or "subagent".
	Role string `json:"role"`

	TargetHarnessID string `json:"targetHarnessId"`
	// TargetModel is the model written into the output. Empty when none was supplied.
	TargetModel string `json:"targetModel,omitempty"`

	// DestinationPath is the absolute path written. Under DryRun it is the path that
	// would have been written. Callers depend on this rather than reconstructing a layout.
	DestinationPath string `json:"destinationPath"`
	// DestinationResolvedBy is "explicit" when the caller named the path and "workspace"
	// when the target harness descriptor resolved it, so a caller can confirm which
	// resolution it actually got.
	DestinationResolvedBy string `json:"destinationResolvedBy"`
	// CreatedDirectories lists directories this run created on the way to the destination,
	// outermost first, empty when every parent already existed. Reported so a caller with
	// a teardown ledger can remove exactly what appeared.
	CreatedDirectories []string `json:"createdDirectories,omitempty"`

	// Workflows are the workflow ids present in the rendered output, in emitted order.
	// Empty for a subagent and for an orchestrator rendered with none.
	Workflows []string `json:"workflows,omitempty"`
	// InfrastructureAgents are the infrastructure agent keys present in the rendered
	// output, in emitted order.
	InfrastructureAgents []string `json:"infrastructureAgents,omitempty"`
	// Tools are the target-harness tool names written to the output, in emission order.
	Tools []string `json:"tools,omitempty"`
	// StrippedFields lists frontmatter fields removed from the output. Currently always
	// empty: the transform package does not yet expose stripped-field data in its report,
	// so this field is reserved for a future stage when that data becomes available.
	StrippedFields []StrippedField `json:"strippedFields,omitempty"`

	// DryRun reports that nothing was written.
	DryRun bool `json:"dryRun"`
}

// ---------------------------------------------------------------------------
// Sentinel errors
//
// Every sentinel below means nothing was written. The Reason column in the design maps
// each sentinel to a stable cross-tool reason code for the CLI's JSON failure envelope;
// the mapping is enforced by a table test introduced with the CLI layer in Stage 2.
// ---------------------------------------------------------------------------

// ErrRenderSourceRequired is returned when neither SourcePath nor SourceAgentKey is supplied.
var ErrRenderSourceRequired = errors.New("render requires exactly one source: supply --source or --source-agent")

// ErrRenderSourceAmbiguous is returned when both SourcePath and SourceAgentKey are supplied.
var ErrRenderSourceAmbiguous = errors.New("render requires exactly one source: --source and --source-agent are mutually exclusive")

// ErrRenderSourceNotFound is returned when SourcePath does not exist on the filesystem.
var ErrRenderSourceNotFound = errors.New("render source file does not exist")

// ErrRenderSourceNotFile is returned when SourcePath exists but is not a regular file
// (for example, it is a directory).
var ErrRenderSourceNotFile = errors.New("render source path is not a regular file")

// ErrRenderAgentNotInCatalog is returned when SourceAgentKey names no agent in the
// catalog (Catalog/Subagents/ or Catalog/UtilityAgents/).
var ErrRenderAgentNotInCatalog = errors.New("render source agent key is not in the catalogue")

// ErrRenderSourceNotGeneric is returned when the source file at SourcePath is not a
// parseable generic-form MOSAIC agent — either its bytes fail docformat.Parse or it
// lacks the expected structure.
var ErrRenderSourceNotGeneric = errors.New("render source is not a parseable generic-form MOSAIC agent")

// ErrRenderInvalidRole is returned when the source frontmatter carries a `role` field
// that is present but not recognised by domain.ParseAgentRole.
var ErrRenderInvalidRole = errors.New("render source has an unrecognised frontmatter role")

// ErrRenderHarnessRequired is returned when TargetHarnessID is empty.
var ErrRenderHarnessRequired = errors.New("render requires a target harness id")

// ErrRenderHarnessUnresolvable is returned when TargetHarnessID names no registered
// harness. The wrapping error carries the registry's own failure reason.
var ErrRenderHarnessUnresolvable = errors.New("render target harness id names no registered harness")

// ErrRenderDestinationRequired is returned when neither DestinationPath nor WorkspaceRoot
// is supplied.
var ErrRenderDestinationRequired = errors.New("render requires exactly one destination: supply --dest or --workspace")

// ErrRenderDestinationAmbiguous is returned when both DestinationPath and WorkspaceRoot
// are supplied.
var ErrRenderDestinationAmbiguous = errors.New("render requires exactly one destination: --dest and --workspace are mutually exclusive")

// ErrRenderDestinationExists is returned when the destination file already exists and
// Overwrite is false. The wrapping error carries the destination path.
var ErrRenderDestinationExists = errors.New("render destination already exists; pass --overwrite to replace it")

// ErrRenderUnknownWorkflow is returned when a requested workflow id in Workflows is not
// present in the catalogue. The wrapping error names the offending id.
var ErrRenderUnknownWorkflow = errors.New("render workflow id is not in the catalogue")

// ErrRenderWorkflowsNotApplicable is returned when a non-empty Workflows or
// InfrastructureAgents slice is supplied for an agent whose resolved role is not
// orchestrator. A non-nil but empty slice is "explicitly none" and is always valid.
var ErrRenderWorkflowsNotApplicable = errors.New("render workflows and infrastructure-agents are only applicable to an orchestrator")

// RenderAgent renders exactly one generic-form MOSAIC agent into one target harness's
// deployed form at a caller-chosen destination. The real implementation is in
// render_service_impl.go, introduced by Stage 1 implementation tasks.
func (s *service) RenderAgent(ctx context.Context, req RenderAgentRequest) (RenderAgentResult, error) {
	return renderAgent(ctx, s, req)
}
