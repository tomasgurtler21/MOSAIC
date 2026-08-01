package app

// service.go declares the public contract for the app use-case layer: the Service interface,
// the two request types, and the dependency set. No implementation lives in this file.
// Implementation is introduced in a later stage (I18.3, I18.4).

import (
	"context"
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
	Update(ctx context.Context, req UpdateRequest) (domain.RunSummary, error)
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
	Catalog     catalog.Catalog
	Registry    registry.Registry
	Planner     plan.Planner
	Executor    deploy.Executor
	Manifest    manifest.Store
	ToolConfig  config.ToolConfigStore
	UserConfig  config.UserConfigStore
	Logger      logging.Logger
	Todo        todo.Collector
	Interaction domain.Interaction
	MosaicRoot  string
	GOOS        string
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
