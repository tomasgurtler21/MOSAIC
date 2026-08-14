package app

// service.go declares the public contract for the app use-case layer: the Service interface,
// the two request types, and the dependency set. No implementation lives in this file.
// Implementation is introduced in a later stage (I18.3, I18.4).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// Service is the use-case layer entry point. Both frontends (tui, cli) call exactly these
// methods. app imports neither frontend; the dependency arrow is strictly inward.
type Service interface {
	// ListHarnesses is exposed so a frontend can render a harness picker before a flow starts.
	ListHarnesses() []domain.HarnessRef

	// DeployNew runs the full deploy-new flow, consulting the user through the Interaction port
	// for any selection not pre-answered in req. Returns a RunSummary on both success and partial
	// success; a non-nil error is reserved for unrecoverable failures (cancelled by user, etc.).
	DeployNew(ctx context.Context, req DeployRequest) (domain.RunSummary, error)

	// Update runs the update flow against an existing workspace deployment. Detects staleness,
	// prompts for conflict decisions, and optionally adds new workflows in the same run.
	//
	// The run is all-or-nothing. Any failure during execution reverses every write: files this
	// run created are deleted, files this run overwrote are restored to their pre-run bytes, and
	// the manifest and checklist file are left exactly as they were before the run. On reversal
	// the method returns a *RevertedRunError carrying the original cause and, when the reversal
	// itself was incomplete, the paths that could not be restored. The workspace is otherwise
	// unchanged and the run can simply be repeated.
	//
	// Conflict backup copies under .mosaic/backups/ survive a reversal by design. Fallback runs
	// (workspace unwritable, files written to a fallback root) are excluded from reversal and
	// behave as they do today.
	//
	// DeployNew keeps today's partial-failure semantics and does not opt in to atomic execution.
	Update(ctx context.Context, req UpdateRequest) (domain.RunSummary, error)

	// UpdateWorkflows runs the workflow-only update flow against an existing workspace
	// deployment. The selected workflow set fully replaces the set currently embedded in
	// the deployed orchestrator; workflows that are not reselected are removed.
	//
	// Already-deployed artifacts are never planned, written, or version-stamped. An artifact
	// counts as deployed when a file exists at its target path, regardless of staleness or
	// local modification. This is the invariant that separates this mode from Update — it is
	// no longer "only the orchestrator is ever written", but it remains the whole of that
	// invariant.
	//
	// Agents required by the selected workflows that have no file in the workspace are
	// deployed in the same run, using the deploy-new question flow: the same questions, in
	// the same order, including tier-level model questions and custom tool mapping. A skipped
	// or unanswered model question produces the same warning and gap outcome as deploy-new,
	// and the run still completes.
	//
	// Skills required by those newly deployed agents that have no file in the workspace are
	// deployed in the same run. Skills required only by already-deployed agents are not.
	//
	// No model question of any kind is asked when every workflow-required agent already has a
	// deployed file. This replaces the former unconditional "no model questions" guarantee;
	// the guarantee is now behavioural, enforced by gating the whole model-resolution call on
	// a non-empty new-agent set.
	//
	// Conflict handling for a locally-modified orchestrator file is identical to Update's:
	// the same decision options, the same backup behaviour, and a skip decision records a
	// GapSkippedFile gap.
	//
	// Hook artifacts remain entirely out of scope and registrations are always cleared.
	UpdateWorkflows(ctx context.Context, req WorkflowUpdateRequest) (domain.RunSummary, error)

	// TransformHarness converts one or more already-deployed agents from a source harness
	// into a target harness's deployed form.
	//
	// req.Path may name a single agent file or a directory. A directory is enumerated
	// non-recursively; only regular files with the source harness's agent extension are
	// considered, and every other entry is reported as a skip rather than an error.
	//
	// Per-file independence is the contract: a mismatched, non-agent, or failing file is
	// recorded in the result and the batch continues. A non-nil error is reserved for
	// failures that make the whole run meaningless — an unresolvable harness id, an
	// unreadable input path, or user cancellation of a question.
	//
	// No destination file is overwritten unless req.Overwrite is true; without it, an
	// existing destination yields a per-file outcome with StatusFailed and
	// ErrTransformDestinationExists as its reason.
	TransformHarness(ctx context.Context, req TransformHarnessRequest) (TransformHarnessResult, error)

	// DeployUtilityInfrastructure deploys only Utility and Infrastructure agents plus the
	// artifacts they require (their skills). It asks exactly the QUtilityAgents and
	// QInfrastructureAgents questions and the model questions those agents imply. It never
	// asks QWorkflows or QHooks, never plans a workflow-driven agent, and never rewrites
	// the deployed orchestrator.
	//
	// Infrastructure-agent model resolution behaves exactly as in DeployNew, including the
	// skip path and the resulting GapNoModel entries.
	DeployUtilityInfrastructure(ctx context.Context, req UtilityInfraRequest) (domain.RunSummary, error)

	// DeployStandalone deploys only standalone agents plus the artifacts they require
	// (their skills). It asks exactly the QStandaloneAgents question and the model questions
	// those agents imply. It never asks QWorkflows, QHooks, QUtilityAgents, or
	// QInfrastructureAgents, and never rewrites the deployed orchestrator's workflow or
	// infrastructure regions.
	DeployStandalone(ctx context.Context, req StandaloneRequest) (domain.RunSummary, error)

	// RenderAgent renders exactly one generic-form MOSAIC agent into one target harness's
	// deployed form at a caller-chosen destination.
	//
	// Source: exactly one of req.SourcePath (a generic-form file at any path, read directly)
	// or req.SourceAgentKey (an agent resolved from the catalog). Neither
	// is ErrRenderSourceRequired; both is ErrRenderSourceAmbiguous.
	//
	// Destination: exactly one of req.DestinationPath (honoured verbatim) or req.WorkspaceRoot
	// (the target harness descriptor's own project-scope path for this agent key, joined onto
	// that root). Neither is ErrRenderDestinationRequired; both is ErrRenderDestinationAmbiguous.
	//
	// The agent's key is derived from the source file's base name without extension. Role and
	// version come from the source file's own frontmatter. An absent role defaults to
	// domain.RoleSubagent; an unrecognised role is ErrRenderInvalidRole. An absent version
	// yields an empty SourceVersion in the result.
	//
	// Workflows: req.Workflows is nil for "not specified" and non-nil empty for "explicitly
	// none". A non-nil non-empty set for a non-orchestrator is ErrRenderWorkflowsNotApplicable.
	// An id not in the catalogue is ErrRenderUnknownWorkflow.
	//
	// RenderAgent is all-or-nothing: nil error means the file was written (or computed under
	// DryRun); non-nil error means nothing was written. It never consults domain.Interaction.
	//
	// An existing destination is never silently replaced: without req.Overwrite it is
	// ErrRenderDestinationExists. The overwrite check runs under DryRun too.
	RenderAgent(ctx context.Context, req RenderAgentRequest) (RenderAgentResult, error)

	// CheckWorkflowIndex reports staleness between Catalog/Workflows/Index.md and the workflow
	// files on disk. It is a read-only, non-interactive diagnostic intended for manual and
	// CI/pre-push use: it never consults domain.Interaction, never writes any file, and never
	// participates in a deploy, update, or workflow-update run.
	//
	// The catalogue root inspected is the one the service's catalog was loaded from.
	//
	// Drift is reported in the result, never as an error. A non-nil error means the check could
	// not be performed; the result is then the zero value.
	CheckWorkflowIndex(ctx context.Context) (IndexCheckResult, error)

	// Promote generates a generic agent source file from a single already boundary-tagged
	// harness-only agent file, so that a one-off harness-specific agent becomes reusable
	// across harnesses through the normal Deploy/Update flow.
	//
	// The source file must satisfy the two-signal eligibility rule — `transform_version` in
	// frontmatter AND a structurally valid set of canonical boundary tags. An ineligible file
	// is rejected with an error wrapping ErrPromoteNotTransformed and nothing is written.
	//
	// The target category is asked through Interaction when req.Category is empty; it is
	// never inferred from the source file.
	//
	// Registration is automatic and is a consequence of placement: the catalog discovers
	// agents by scanning Catalog/Orchestrator/, Catalog/Subagents/{category}/, and
	// Catalog/UtilityAgents/, so writing a well-formed file into the chosen directory
	// with a unique numeric id IS the registration. No catalog file is edited.
	//
	// The harness-only source file is never modified or deleted. Deploying the promoted agent
	// out to a harness is a separate, user-initiated step.
	//
	// Promote is never invoked by Update.
	//
	// Unlike DeployNew/Update/UpdateWorkflows, Promote has no partial-success state: it either
	// produces the generic file (or, under DryRun, fully validates it) or it produces nothing.
	// Returns a populated PromoteResult with a nil error on success, and the zero-value
	// PromoteResult with a non-nil error on every failure. A caller may therefore treat
	// err != nil as "nothing was written" without inspecting the result.
	//
	// Error set:
	//   - req.FilePath == ""                          → ErrPromoteFileRequired
	//   - req.FilePath does not exist                 → wraps ErrPromoteSourceNotFound
	//   - req.FilePath exists but is not a file       → wraps ErrPromoteSourceNotFile
	//   - req.HarnessID == ""                         → ErrPromoteHarnessRequired
	//   - req.HarnessID names no registered harness   → wraps ErrPromoteHarnessUnresolvable
	//   - source ineligible                           → wraps ErrPromoteNotTransformed
	//   - destination occupied and Overwrite false    → wraps ErrPromoteDestinationExists
	//   - no category supplied and none obtained      → ErrPromoteCategoryRequired
	//
	// Source-path validation (not-found, not-a-file) runs before any read, before eligibility
	// evaluation, before the category question, and before any destination computation.
	// Neither source-path failure writes any file or asks the category question. Harness
	// validation runs after source-path validation and before the source read, eligibility
	// evaluation, and the category question.
	Promote(ctx context.Context, req PromoteRequest) (PromoteResult, error)
}

// PromoteCategoryUtility is the sentinel Category value selecting
// Catalog/UtilityAgents/ rather than a subdirectory of Catalog/Subagents/.
const PromoteCategoryUtility = "UtilityAgents"

// ErrPromoteFileRequired reports a PromoteRequest with an empty FilePath.
var ErrPromoteFileRequired = errors.New("promote requires a source file path")

// ErrPromoteHarnessRequired reports a PromoteRequest with an empty HarnessID. Promote has
// no interactive harness fallback; the caller must supply the harness it already knows.
var ErrPromoteHarnessRequired = errors.New("promote requires a harness id")

// ErrPromoteHarnessUnresolvable reports a HarnessID that names no registered harness.
// The wrapping error carries the registry's own failure reason.
var ErrPromoteHarnessUnresolvable = errors.New("promote harness id names no registered harness")

// ErrPromoteDestinationExists reports a generic file already present at the computed
// destination path when req.Overwrite is false. Promote never silently overwrites an
// existing generic agent.
var ErrPromoteDestinationExists = errors.New("a generic agent already exists at the destination path")

// ErrPromoteSourceNotFound reports a PromoteRequest whose FilePath does not exist.
var ErrPromoteSourceNotFound = errors.New("promote source file does not exist")

// ErrPromoteSourceNotFile reports a PromoteRequest whose FilePath exists but is not a
// regular file — most commonly a directory. Promote takes exactly one harness-only agent
// file; a directory is never valid input.
var ErrPromoteSourceNotFile = errors.New("promote source path is not a file; a single agent file is required")

// ErrPromoteCategoryRequired reports that no category was supplied and none was obtained
// from the user (the question was cancelled or skipped).
var ErrPromoteCategoryRequired = errors.New("promote requires a target category")

// PromoteRequest carries the caller's pre-answers for the promote flow. A set field is
// used directly without asking; an unset field causes the flow to ask through Interaction.
type PromoteRequest struct {
	// FilePath is the harness-only agent file to promote. Required; there is no
	// interactive fallback for it.
	FilePath string
	// Category is the destination placement. An empty value causes QPromoteCategory to
	// be asked. A value of PromoteCategoryUtility places the file under
	// Catalog/UtilityAgents/; any other value names a subdirectory under
	// Catalog/Subagents/.
	Category string
	// Overwrite is explicit consent to replace an existing file at the destination
	// path. Without it a collision is refused.
	Overwrite bool
	// DryRun computes and validates everything but writes no file.
	DryRun bool
	// HarnessID names the harness that produced the deployed file being promoted. It is
	// required and has no interactive fallback: the TUI supplies its already-selected
	// harness and the CLI supplies the --harness flag. An empty value is
	// ErrPromoteHarnessRequired; a value naming no registered harness is
	// ErrPromoteHarnessUnresolvable. Neither writes a file.
	HarnessID string
}

// PromoteResult describes what a successful promote produced. It is returned populated only
// when Service.Promote returns a nil error; on any failure it is the zero value.
type PromoteResult struct {
	// SourcePath is the harness-only file that was read. It is never modified or deleted.
	SourcePath string `json:"sourcePath"`
	// DestinationPath is the generic file written, relative to the MOSAIC root.
	DestinationPath string `json:"destinationPath"`
	// Key is the agent key the catalog will derive from the written file.
	Key string `json:"key"`
	// NumericID is the id assigned to the new agent.
	NumericID string `json:"numericId"`
	// Category is the resolved placement, either PromoteCategoryUtility or a
	// subdirectory name under Catalog/Subagents/.
	Category string `json:"category"`
	// DryRun reports that nothing was written.
	DryRun bool `json:"dryRun"`
	// HarnessID echoes the harness the promotion was interpreted against, so a caller
	// can confirm the outcome without re-reading the file.
	HarnessID string `json:"harnessId"`
	// Tools is the generic tools list written to the promoted file, in written order.
	// Empty when the source declared no harness-side tool entries.
	Tools []string `json:"tools,omitempty"`
	// VerbatimTools names the harness-side tool entries that reached the generic tools
	// list unchanged because no reverse mapping existed and no generic name was supplied.
	// A non-empty value is the documented limitation of the autonomous path, not an error.
	VerbatimTools []string `json:"verbatimTools,omitempty"`
	// RecoveredFields names the generic-only frontmatter fields the user supplied during
	// this run, in ask order. Fields left absent are not listed.
	RecoveredFields []string `json:"recoveredFields,omitempty"`
	// StrippedFields lists frontmatter fields removed from the generated generic agent
	// because they could not be recovered — unmappable diverted tool values and fields
	// unknown to MOSAIC. Empty when nothing was stripped.
	StrippedFields []StrippedField `json:"strippedFields,omitempty"`
	// DivertedTools lists the harness-side tool entries recovered from diverted
	// frontmatter fields (as opposed to the harness's main tools key), in extraction
	// order. Reported separately from Tools so the user can see the recovery happened.
	DivertedTools []string `json:"divertedTools,omitempty"`
}

// New constructs a Service with the supplied dependency set.
func New(d Deps) Service {
	return &service{deps: d}
}

// service is the production Service implementation. It orchestrates the deploy-new and
// update use cases, consulting the caller through Interaction for any selection not
// pre-answered in the request (CD-6).
type service struct {
	deps Deps
}

// ListHarnesses returns every harness the registry knows about, including gated ones.
func (s *service) ListHarnesses() []domain.HarnessRef {
	return s.deps.Registry.List()
}

// loadProtocol loads the protocol source document once for the run. It is called before
// any plan is built and before any output is written; a failure aborts the run.
func (s *service) loadProtocol() (domain.ProtocolContent, error) {
	if s.deps.ProtocolLoader == nil {
		return domain.ProtocolContent{}, fmt.Errorf("misconfigured service: ProtocolLoader dependency is nil")
	}
	return s.deps.ProtocolLoader.LoadProtocol(s.deps.MosaicRoot)
}

// loadBundle loads the deployed-sections bundle once for the run. It is called before
// any plan is built and before any output is written; a failure aborts the run.
func (s *service) loadBundle() (domain.BundleContent, error) {
	if s.deps.BundleLoader == nil {
		return domain.BundleContent{}, fmt.Errorf("misconfigured service: BundleLoader dependency is nil")
	}
	return s.deps.BundleLoader.LoadBundle(s.deps.MosaicRoot)
}

// now returns the injected clock, defaulting to time.Now when Deps.Now is nil.
func (s *service) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now()
}

// Deps is the full dependency set the app service requires. Every field is mandatory except
// Now (defaults to time.Now when nil).
type Deps struct {
	Catalog        catalog.Catalog
	Registry       registry.Registry
	Planner        plan.Planner
	Executor       deploy.Executor
	Manifest       manifest.Store
	ToolConfig     config.ToolConfigStore
	UserConfig     config.UserConfigStore
	Logger         logging.Logger
	Todo           todo.Collector
	Interaction    domain.Interaction
	MosaicRoot     string
	GOOS           string
	// ProtocolLoader reads the canonical protocol source document once per run and returns
	// the two role blocks and the source version. Mandatory; supplied at the composition root.
	ProtocolLoader catalog.ProtocolLoader
	// BundleLoader reads the deployed-sections bundle once per run and returns its version
	// and blocks. Mandatory; supplied at the composition root. A nil loader is a
	// misconfiguration reported as an error, never a panic.
	BundleLoader catalog.BundleLoader
	// Now is injected so run records and backup filenames are deterministic in tests.
	Now func() time.Time
}

// DeployRequest carries the caller's pre-answers for the deploy-new flow. A set field is
// used directly without asking; an unset field causes the flow to ask through Interaction
// (CD-6). This is the single mechanism that makes CLI flags and TUI screens interchangeable.
type DeployRequest struct {
	HarnessID               string
	WorkspacePath           string
	Scope                   domain.Scope
	WorkflowIDs             []string
	UtilityAgentIDs         []string
	InfrastructureAgentIDs  []string
	HookIDs                 []string
	// TierModels pre-answers the per-tier model selection step (QTierModel) for each tier key.
	TierModels  map[domain.Tier]string
	AgentModels map[string]string // agent key -> model id; pre-answers QAgentModel per agent
	CustomTools map[string]string // generic tool -> MCP server name; pre-answers QCustomTool
	// SkipAll pre-latches SkippedAll for specified QuestionIDs, bypassing the ask entirely.
	SkipAll         map[domain.QuestionID]bool
	AutoConfirmPlan bool
	DryRun          bool

	// ConflictDefault is the decision applied to locally-modified files when
	// non-interactive. When zero (empty string), the flow asks through Interaction
	// for each conflict (subject to the apply-to-all latch). This mirrors
	// UpdateRequest.ConflictDefault and is populated by the deploy --conflict CLI flag.
	ConflictDefault domain.ConflictDecision
}

// UpdateRequest carries the caller's pre-answers for the update flow.
type UpdateRequest struct {
	HarnessID string
	// WorkspacePath is the path to the existing deployment workspace.
	WorkspacePath string
	// AddWorkflowIDs lists additional workflows to add in this run (FR-18).
	AddWorkflowIDs []string
	// ConflictDefault is the decision applied to locally-modified files when non-interactive.
	// When zero (empty string), the flow asks through Interaction for each conflict.
	ConflictDefault domain.ConflictDecision
	TierModels      map[domain.Tier]string
	AgentModels     map[string]string
	CustomTools     map[string]string
	SkipAll         map[domain.QuestionID]bool
	AutoConfirmPlan bool
	DryRun          bool
}

// WorkflowUpdateRequest carries the caller's pre-answers for the workflow-only update
// flow. A set field is used directly without asking; an unset field causes the flow to ask
// through Interaction (CD-6).
//
// No TierModels, AgentModels, or CustomTools fields: this mode does not support headless
// pre-answering of model selections. Model answers reach the flow through the Interaction
// port and through tier models already persisted in user config. The "no model questions"
// guarantee now holds only when every workflow-required agent is already deployed; when
// newly-required agents are detected, model and tool questions are asked through Interaction,
// exactly as the deploy-new flow asks them.
type WorkflowUpdateRequest struct {
	HarnessID string
	// WorkspacePath is the path to the existing deployment workspace.
	WorkspacePath string
	// WorkflowIDs is the complete replacement workflow set. When non-nil it is used
	// directly and QWorkflows is not asked. A nil slice means "ask"; an explicitly empty
	// non-nil slice means "deploy the orchestrator with no workflows" and is honoured.
	WorkflowIDs []string
	// ConflictDefault is the decision applied to a locally-modified orchestrator file when
	// non-interactive. When zero, the flow asks through Interaction.
	ConflictDefault domain.ConflictDecision
	SkipAll         map[domain.QuestionID]bool
	AutoConfirmPlan bool
	DryRun          bool
}
