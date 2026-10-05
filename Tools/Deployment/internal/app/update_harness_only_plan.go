package app

// update_harness_only_plan.go holds the harness-only refresh-plan step of the update flow.

import (
	"context"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// planHarnessOnlyRefresh builds the harness-only refresh plan and appends one ActionUpdate
// item per consented agent to p.Items; it returns the content plan keyed by target path.
//
// Build the harness-only refresh plan. Each eligible agent is asked once for its scope,
// with apply-to-all latching mirroring the conflict loop's applyToAllLatch pattern.
//
// Consent contract: the refresh-scope prompt is the sole consent mechanism for
// harness-only agents (they bypass the local-modification conflict prompt by design).
// Only an explicit answer (Answered status) authorises a refresh. A declined outcome
// — SkippedOne, SkippedAll, Cancelled, or transport error — produces no plan item and
// no content-plan entry; the file is left byte-identical on disk.
//
// Prompt guard: the scope question is suppressed entirely when no eligible agent exists.
// Never prompt for an empty set.
//
// Conflict-loop interaction: harness-only agents never enter the conflict loop above.
// They are appended here with ActionUpdate only when the user explicitly answered the
// scope prompt. A harness-only agent is user-authored by definition, so it would trip
// the local-modification prompt on every run; the refresh-scope prompt replaces that
// mechanism entirely with explicit consent.
//
// Version-stamping decision: no entry is added to versionStamps for harness-only agents.
// Stamping implies a source version to stamp from; there is none for a harness-only
// agent, and inventing a stamp would make the file appear catalog-backed on the next run.
//
// Manifest decision: harness-only agents remain manifest-invisible; detection stays
// purely the two-signal rule.
//
// Dry-run decision: discovery and prompting still occur when DryRun is true; no byte is
// written because DryRun is forwarded to the executor via ExecRequest.DryRun.
func (s *service) planHarnessOnlyRefresh(ctx context.Context, harnessOnlyAgents []HarnessOnlyAgent, p *domain.Plan) map[string]harnessOnlyContentPlan {
	harnessOnlyPlan := make(map[string]harnessOnlyContentPlan, len(harnessOnlyAgents))
	if len(harnessOnlyAgents) > 0 {
		var latchedDecision RefreshDecision
		harnessApplyToAllLatch := false
		for _, agent := range harnessOnlyAgents {
			var decision RefreshDecision
			if harnessApplyToAllLatch {
				decision = latchedDecision
			} else {
				decision = s.askHarnessOnlyRefreshScope(ctx, agent)
				if decision.ApplyToAll {
					harnessApplyToAllLatch = true
					latchedDecision = decision
				}
			}

			// Consent gate: a declined outcome means no plan item and no content-plan entry.
			// The file is left byte-identical on disk.
			if !decision.Refresh {
				continue
			}

			harnessOnlyPlan[agent.TargetPath] = harnessOnlyContentPlan{Agent: agent, Scope: decision.Scope}

			// Emit an observability event identifying this agent as harness-only and its scope.
			// Only emitted when the user explicitly authorised a refresh; declined agents must
			// not be reported with a scope that was never applied.
			// The harness_only and scope fields are the contract a caller or a test reads to
			// determine which agents received degraded-quality treatment and at what breadth.
			s.deps.Logger.Event(logging.Event{
				Kind:    "transform",
				Subject: agent.TargetPath,
				Message: "harness-only agent refreshed (degraded: no generic counterpart)",
				Fields: map[string]string{
					"harness_only": "true",
					"scope":        string(decision.Scope),
					"regions":      strings.Join(decision.Scope.Regions(), ","),
				},
			})

			// Append the harness-only agent to the plan. SourcePath is deliberately empty:
			// it is the visible marker that this item has no catalog source and must never
			// be passed to Catalog.ReadSource.
			p.Items = append(p.Items, domain.PlanItem{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key},
				SourcePath: "",
				TargetPath: agent.TargetPath,
				Action:     domain.ActionUpdate,
				Reason:     "harness-only agent (no generic counterpart): refreshing " + string(decision.Scope),
			})
		}
	}
	return harnessOnlyPlan
}
