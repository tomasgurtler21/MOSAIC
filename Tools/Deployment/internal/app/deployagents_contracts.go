package app

// deployagents_contracts.go declares the public contract for the deploy-agents mode:
// the request type and the Service interface binding. The real implementation lives in
// deployagents_service.go, introduced during Stage 3 implementation tasks (I3.4, I3.5).

import (
	"context"

	"mosaic-deploy/internal/domain"
)

// DeployAgentsRequest drives the deploy-agents mode. It carries a fully non-interactive or
// entirely interactive run through the same method. Following CD-6: a set (non-nil) field is
// used directly without asking; an unset (nil) field causes the flow to ask through Interaction.
//
// The four per-class ID slices (SubagentIDs, UtilityAgentIDs, InfrastructureAgentIDs,
// StandaloneAgentIDs) together pre-answer QDeployAgents. QDeployAgents is asked only when all
// four are nil; any non-nil field means the caller has already classified the selection and the
// merged question is skipped entirely.
type DeployAgentsRequest struct {
	HarnessID     string
	WorkspacePath string
	Scope         domain.Scope

	// The four per-class pre-answers for the merged agent selection. Following CD-6,
	// a nil slice means "ask QDeployAgents"; a non-nil empty slice means "none, do not ask".
	// QDeployAgents is asked only when all four are nil.
	SubagentIDs            []string
	UtilityAgentIDs        []string
	InfrastructureAgentIDs []string
	StandaloneAgentIDs     []string

	// TierModels pre-answers per-tier model selection (QTierModel) for each tier key.
	TierModels map[domain.Tier]string
	// AgentModels pre-answers per-agent model selection (QAgentModel) for each agent key.
	AgentModels map[string]string
	// CustomTools pre-answers custom tool mapping (QCustomTool) for each generic tool.
	CustomTools map[string]string

	// SkipAll pre-latches SkippedAll for specified QuestionIDs, bypassing the ask entirely.
	SkipAll         map[domain.QuestionID]bool
	AutoConfirmPlan bool
	DryRun          bool
	ConflictDefault domain.ConflictDecision
}

// DeployAgents implements Service.DeployAgents. The real implementation lives in
// deployagents_service.go; this method is the Service interface binding.
func (s *service) DeployAgents(ctx context.Context, req DeployAgentsRequest) (domain.RunSummary, error) {
	return deployAgents(ctx, s, req)
}
