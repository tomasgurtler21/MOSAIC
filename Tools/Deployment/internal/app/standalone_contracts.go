package app

// standalone_contracts.go declares the public contract for the standalone-only deploy
// mode: the request type and the Service method stub. The real implementation lives in
// standalone_service.go, introduced during Stage 5 implementation tasks (I5.3).

import (
	"context"

	"mosaic-deploy/internal/domain"
)

// StandaloneRequest drives the standalone-only deploy mode. It is a narrowed DeployRequest:
// the same field names and types for every field it keeps, so existing resolver and
// model-resolution code paths accept it without translation. Workflow-, hook-, utility- and
// infrastructure-related fields are absent; those questions are never asked in this mode.
//
// Following CD-6: a set field is used directly without asking; an unset field causes the
// flow to ask through Interaction.
type StandaloneRequest struct {
	HarnessID     string
	WorkspacePath string
	Scope         domain.Scope

	// StandaloneAgentIDs pre-answers QStandaloneAgents. A nil slice triggers the question;
	// a non-nil empty slice means "none, do not ask".
	StandaloneAgentIDs []string

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

// DeployStandalone implements Service.DeployStandalone. The real implementation lives in
// standalone_service.go; this method is the Service interface binding.
func (s *service) DeployStandalone(ctx context.Context, req StandaloneRequest) (domain.RunSummary, error) {
	return deployStandalone(ctx, s, req)
}
