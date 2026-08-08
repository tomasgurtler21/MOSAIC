package app

// harness_only_prompt.go implements askHarnessOnlyRefreshScope, the prompt that presents
// the two scope options (and their apply-to-all variants) to the user when a harness-only
// agent is found during an Update run.

import (
	"context"
	"strings"

	"mosaic-deploy/internal/domain"
)

// askHarnessOnlyRefreshScope prompts for how much of one harness-only agent to refresh.
// When applyToAll is true in the returned values, the caller must use the returned scope
// for every remaining harness-only agent without asking again.
//
// The option shape mirrors askLocalModification exactly: per-agent options carry the bare
// scope value as their Option.ID, and the apply-to-all variants carry the same value under
// an "all:" prefix with Group "Apply to all". An answer whose OptionID carries the prefix
// has it stripped to recover the scope and returns applyToAll true.
//
// Any non-Answered outcome — SkippedOne, SkippedAll, Cancelled, or a transport error —
// returns (RefreshProtocolOnly, false). The narrow scope is never escalated without an
// explicit answer.
func (s *service) askHarnessOnlyRefreshScope(
	ctx context.Context, agent HarnessOnlyAgent,
) (scope RefreshScope, applyToAll bool) {
	opts := []domain.Option{
		{ID: string(RefreshProtocolOnly), Label: "Refresh CommunicationProtocol only"},
		{ID: string(RefreshAllDeployed), Label: "Refresh all tool-managed DEPLOYED regions"},
		{ID: "all:" + string(RefreshProtocolOnly), Label: "Refresh CommunicationProtocol only for all remaining", Group: "Apply to all"},
		{ID: "all:" + string(RefreshAllDeployed), Label: "Refresh all tool-managed DEPLOYED regions for all remaining", Group: "Apply to all"},
	}
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:        domain.QHarnessOnlyRefreshScope,
			Subject:   agent.TargetPath,
			Title:     "Harness-only agent (no generic counterpart): " + agent.TargetPath,
			AllowSkip: true,
		},
		Options: opts,
	}
	ans, err := s.deps.Interaction.SelectOne(ctx, q)
	if err != nil || ans.Status != domain.Answered {
		return RefreshProtocolOnly, false
	}
	optID := ans.OptionID
	if strings.HasPrefix(optID, "all:") {
		remainder := strings.TrimPrefix(optID, "all:")
		sc := RefreshScope(remainder)
		if sc == "" {
			sc = RefreshProtocolOnly
		}
		return sc, true
	}
	if sc := RefreshScope(optID); sc != "" {
		return sc, false
	}
	return RefreshProtocolOnly, false
}
