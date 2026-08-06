package app

// service.go declares the public contract for the app use-case layer: the Service interface,
// the two request types, and the dependency set. No implementation lives in this file.
// Implementation is introduced in a later stage (I18.3, I18.4).

import (
	"context"
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
