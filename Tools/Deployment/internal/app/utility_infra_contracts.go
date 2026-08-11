package app

// utility_infra_contracts.go declares the public contract for the utility/infrastructure-only
// deploy mode: request type and the Service method that drives it. The real implementation
// will live in a separate file introduced during Stage 7 implementation tasks.

import (
	"context"

	"mosaic-deploy/internal/domain"
)

// UtilityInfraRequest drives the Utility/Infrastructure-only deploy mode. It is a narrowed
// DeployRequest — the same field names and types for every field it keeps, so the existing
// resolver and model-resolution code paths accept it without translation. Workflow- and
// hook-related fields are absent: those questions are never asked in this mode.
//
// Following CD-6: a set field is used directly without asking; an unset field causes the
// flow to ask through Interaction.
type UtilityInfraRequest struct {
	HarnessID     string
	WorkspacePath string
	Scope         domain.Scope

	// UtilityAgentIDs pre-answers QUtilityAgents. A nil slice triggers the question; a
	// non-nil empty slice means "none, do not ask", matching DeployRequest semantics.
	UtilityAgentIDs []string
	// InfrastructureAgentIDs pre-answers QInfrastructureAgents, with the same nil/empty
	// semantics.
	InfrastructureAgentIDs []string

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

// DeployUtilityInfrastructure implements Service.DeployUtilityInfrastructure. The real
// implementation is introduced in Stage 7 implementation tasks.
func (s *service) DeployUtilityInfrastructure(ctx context.Context, req UtilityInfraRequest) (domain.RunSummary, error) {
	return deployUtilityInfrastructure(ctx, s, req)
}
