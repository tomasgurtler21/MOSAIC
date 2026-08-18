package app

// deployhooks_contracts.go declares the public contract for the deploy-hooks mode:
// the request type and the Service interface binding. The real implementation lives in
// deployhooks_service.go, introduced during Stage 4 implementation tasks (I4.2, I4.3).

import (
	"context"

	"mosaic-deploy/internal/domain"
)

// DeployHooksRequest drives the deploy-hooks mode. It carries a fully non-interactive or
// entirely interactive run through the same method. Following CD-6: a set (non-nil) field is
// used directly without asking; an unset (nil) field causes the flow to ask through Interaction.
//
// There are no TierModels, AgentModels, or CustomTools fields: their absence is the structural
// guarantee that this mode asks no model or custom-tool question. No agent content is generated
// in this mode, so there is nothing for those fields to answer.
type DeployHooksRequest struct {
	HarnessID     string
	WorkspacePath string
	Scope         domain.Scope

	// HookIDs pre-answers QHooks. A nil slice triggers the question; a non-nil empty slice
	// means "none, do not ask".
	HookIDs []string

	// SkipAll pre-latches SkippedAll for specified QuestionIDs, bypassing the ask entirely.
	SkipAll         map[domain.QuestionID]bool
	AutoConfirmPlan bool
	DryRun          bool
	ConflictDefault domain.ConflictDecision
}

// DeployHooks implements Service.DeployHooks. The real implementation lives in
// deployhooks_service.go; this method is the Service interface binding.
func (s *service) DeployHooks(ctx context.Context, req DeployHooksRequest) (domain.RunSummary, error) {
	return deployHooks(ctx, s, req)
}
