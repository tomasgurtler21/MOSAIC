package app

// harness_only_prompt.go implements askHarnessOnlyRefreshScope, the prompt that presents
// the two scope options (and their apply-to-all variants) to the user when a harness-only
// agent is found during an Update run.

import (
	"context"
	"strings"

	"mosaic-deploy/internal/domain"
)

// askHarnessOnlyRefreshScope prompts for how much of one harness-only agent to refresh, and
// returns the user's decision.
//
// Consent contract: only an explicit answer authorises a refresh. Every non-Answered outcome
// — SkippedOne, SkippedAll, Cancelled, or a transport error from the Interaction port —
// yields a declined decision, meaning the caller must produce no plan item, no content-plan
// entry, and no refresh for that file. Skip is a true opt-out, not a fall back to the narrow
// scope.
//
// The option shape mirrors askLocalModification exactly: per-agent options carry the bare
// scope value as their Option.ID, and the apply-to-all variants carry the same value under an
// "all:" prefix with Group "Apply to all". An answer whose OptionID carries the prefix has it
// stripped to recover the scope and sets ApplyToAll.
//
// A SkippedAll outcome yields Refresh=false with ApplyToAll=true: decline latched for every
// remaining harness-only agent. SkippedOne, Cancelled and transport errors yield
// Refresh=false with ApplyToAll=false: this file only.
func (s *service) askHarnessOnlyRefreshScope(
	ctx context.Context, agent HarnessOnlyAgent,
) RefreshDecision {
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
		// A SkippedAll outcome latches the decline for all remaining agents.
		if err == nil && ans.Status == domain.SkippedAll {
			return RefreshDecision{Refresh: false, ApplyToAll: true}
		}
		// Every other non-answered outcome (SkippedOne, Cancelled, transport error) declines
		// this file only without latching.
		return RefreshDecision{}
	}
	optID := ans.OptionID
	if strings.HasPrefix(optID, "all:") {
		remainder := strings.TrimPrefix(optID, "all:")
		sc := RefreshScope(remainder)
		if sc == "" {
			sc = RefreshProtocolOnly
		}
		return RefreshDecision{Refresh: true, Scope: sc, ApplyToAll: true}
	}
	if sc := RefreshScope(optID); sc != "" {
		return RefreshDecision{Refresh: true, Scope: sc}
	}
	// An Answered status with an unrecognised option ID is treated as a decline.
	return RefreshDecision{}
}
