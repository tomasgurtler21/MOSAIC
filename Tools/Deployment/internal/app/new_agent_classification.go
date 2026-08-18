package app

// new_agent_classification.go declares the three pure classification functions for the
// new-agent deployment path. They express the single authoritative answer to which agents
// and skills are new for this run, and which plan items may reach the executor.

import (
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// newlyRequiredAgents returns the agents from agents that have no deployed file in the
// workspace and therefore must be created by this run. The result preserves the input order.
//
// An agent qualifies as new only on an explicit absent probe result: its artifact ref must
// have an enumerated target path in paths AND that path must have an entry in deployedState
// whose Present is false. An agent whose target path was never enumerated, or that has no
// deployedState entry at all, is treated as NOT new — absence of evidence is not evidence of
// absence, and mis-classifying here would cause an already-deployed agent to be overwritten.
//
// The orchestrator is never returned: it is excluded both by domain.RoleOrchestrator and by
// key comparison against orchestratorKey.
//
// Pure: no I/O, no mutation of its arguments.
func newlyRequiredAgents(
	agents []domain.Agent,
	paths plan.PlannedPaths,
	deployedState map[string]domain.DeployedArtifactState,
	orchestratorKey string,
) []domain.Agent {
	var result []domain.Agent
	for _, agent := range agents {
		// Exclude the orchestrator by both role and key — belt-and-suspenders.
		if agent.Role == domain.RoleOrchestrator || agent.Key == orchestratorKey {
			continue
		}
		// Only an explicit absent probe result qualifies as new.
		ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key}
		targetPath, ok := paths.Path(ref)
		if !ok {
			continue // target path not enumerated → treat as not new
		}
		state, exists := deployedState[targetPath]
		if !exists {
			continue // no deployedState entry → treat as not new
		}
		if !state.Present {
			result = append(result, agent)
		}
	}
	return result
}

// newlyRequiredSkillKeys returns the set of skill keys that must be created by this run.
//
// Admission is decided by exactly two gates, in this order:
//  1. The skill key appears in the RequiredSkills list of at least one agent in newAgents.
//  2. That skill's enumerated target path is recorded absent in deployedState.
//
// catalogSkills is a lookup table only: it is consulted to resolve a required skill key to its
// domain.Skill record (and thereby its artifact ref and enumerated target path). A key that
// has no matching record in catalogSkills cannot be resolved to a path and is silently skipped.
//
// Presence classification follows the same rule as newlyRequiredAgents: only an explicit
// absent probe result qualifies.
//
// Pure: no I/O, no mutation of its arguments.
func newlyRequiredSkillKeys(
	newAgents []domain.Agent,
	catalogSkills []domain.Skill,
	paths plan.PlannedPaths,
	deployedState map[string]domain.DeployedArtifactState,
) map[string]bool {
	// Build a lookup from skill key to Skill record.
	skillByKey := make(map[string]domain.Skill, len(catalogSkills))
	for _, sk := range catalogSkills {
		skillByKey[sk.Key] = sk
	}

	var result map[string]bool
	for _, agent := range newAgents {
		for _, skillKey := range agent.RequiredSkills {
			// Gate 1: skill must be resolvable via the catalog.
			if _, ok := skillByKey[skillKey]; !ok {
				continue // not in catalog → silently skip
			}
			// Gate 2: skill's file must be explicitly absent.
			ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: skillKey}
			targetPath, ok := paths.Path(ref)
			if !ok {
				continue // path not enumerated → treat as not new
			}
			state, exists := deployedState[targetPath]
			if !exists || state.Present {
				continue // no explicit absent entry → not new
			}
			if result == nil {
				result = make(map[string]bool)
			}
			result[skillKey] = true
		}
	}
	return result
}

// admitWorkflowUpdateItem reports whether a plan item may reach the executor in the
// workflow-only update flow.
//
// Admitted:
//   - the orchestrator agent item, whatever its action (create, update, unchanged, conflict)
//   - an agent item whose key is in newAgentKeys AND whose action is domain.ActionCreate
//   - a skill item whose key is in newSkillKeys AND whose action is domain.ActionCreate
//
// Dropped: everything else. In particular every non-orchestrator agent item with
// ActionUpdate, ActionUnchanged or ActionConflict (already-deployed agents are never
// re-planned, even when stale or locally modified), every skill item outside newSkillKeys,
// every skill item whose action is not create, and every hook item.
//
// Pure: no I/O.
func admitWorkflowUpdateItem(
	item domain.PlanItem,
	orchestratorKey string,
	newAgentKeys map[string]bool,
	newSkillKeys map[string]bool,
) bool {
	switch item.Ref.Kind {
	case domain.ArtifactAgent:
		// The orchestrator is admitted for any action.
		if item.Ref.Key == orchestratorKey {
			return true
		}
		// Non-orchestrator agents are admitted only when explicitly classified as new
		// AND the plan action is create. ActionUpdate/Unchanged/Conflict all imply a
		// present deployed file and must never be admitted here.
		return item.Action == domain.ActionCreate && newAgentKeys[item.Ref.Key]

	case domain.ArtifactSkill:
		// Skills are admitted only when the key is in newSkillKeys AND the action is create.
		return item.Action == domain.ActionCreate && newSkillKeys[item.Ref.Key]

	default:
		// Hooks and any future artifact kinds are always dropped.
		return false
	}
}
