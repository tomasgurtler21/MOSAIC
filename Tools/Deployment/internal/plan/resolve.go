package plan

import (
	"fmt"
	"sort"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ResolveArtifacts derives the complete artifact set from the given selections.
//
// The resulting ArtifactSet contains:
//   - The orchestrator (always present, regardless of other selections)
//   - Every referenced agent from the selected workflows
//   - Every utility agent whose key appears in utilityAgentIDs (must already be allow-listed)
//   - Every infrastructure agent whose key appears in infrastructureAgentIDs
//   - Every skill transitively required by any included agent
//   - Every hook bundle whose key appears in hookIDs
//
// Deduplication is applied across all sources. The returned slices are ordered
// deterministically: Agents by key, Skills by key, Hooks by key.
//
// Errors:
//   - ErrUnknownWorkflow if any workflowID is not found in the catalog
//   - ErrUnknownAgent if any workflow references an agent that does not exist in the catalog,
//     or if any infrastructureAgentID is not found in the catalog
func ResolveArtifacts(c catalog.Catalog, workflowIDs, utilityAgentIDs, infrastructureAgentIDs, hookIDs []string) (ArtifactSet, error) {
	agentsSeen := make(map[string]bool)
	var agents []domain.Agent

	// The orchestrator is always included.
	orc := c.Orchestrator()
	agentsSeen[orc.Key] = true
	agents = append(agents, orc)

	// Collect agents referenced by selected workflows.
	for _, wfID := range workflowIDs {
		wf, ok := c.Workflow(wfID)
		if !ok {
			return ArtifactSet{}, fmt.Errorf("%w: %q", ErrUnknownWorkflow, wfID)
		}
		for _, agentKey := range wf.ReferencedAgents {
			agent, ok := c.Agent(agentKey)
			if !ok {
				return ArtifactSet{}, fmt.Errorf("%w: %q referenced by workflow %q", ErrUnknownAgent, agentKey, wfID)
			}
			if !agentsSeen[agent.Key] {
				agentsSeen[agent.Key] = true
				agents = append(agents, agent)
			}
		}
	}

	// Collect explicitly selected utility agents.
	for _, utilityKey := range utilityAgentIDs {
		if agentsSeen[utilityKey] {
			continue
		}
		agent, ok := c.Agent(utilityKey)
		if !ok {
			return ArtifactSet{}, fmt.Errorf("%w: utility agent %q not found in catalog", ErrUnknownAgent, utilityKey)
		}
		agentsSeen[agent.Key] = true
		agents = append(agents, agent)
	}

	// Collect explicitly selected infrastructure agents. Multiple agents of the same class
	// may be selected without restriction (the "at most one active per class" rule is
	// enforced at run start, not deploy time).
	for _, infraKey := range infrastructureAgentIDs {
		if agentsSeen[infraKey] {
			continue
		}
		agent, ok := c.Agent(infraKey)
		if !ok {
			return ArtifactSet{}, fmt.Errorf("%w: infrastructure agent %q not found in catalog", ErrUnknownAgent, infraKey)
		}
		agentsSeen[agent.Key] = true
		agents = append(agents, agent)
	}

	// Collect skills transitively required by all included agents.
	skillsSeen := make(map[string]bool)
	var skills []domain.Skill
	for _, agent := range agents {
		for _, skillKey := range agent.RequiredSkills {
			if skillsSeen[skillKey] {
				continue
			}
			skill, ok := c.Skill(skillKey)
			if !ok {
				// Missing skills are surfaced via catalog.Issues, not here.
				continue
			}
			skillsSeen[skillKey] = true
			skills = append(skills, skill)
		}
	}

	// Collect selected hook bundles.
	hooksSeen := make(map[string]bool)
	var hooks []domain.HookBundle
	for _, hookKey := range hookIDs {
		if hooksSeen[hookKey] {
			continue
		}
		hook, ok := c.Hook(hookKey)
		if !ok {
			continue
		}
		hooksSeen[hookKey] = true
		hooks = append(hooks, hook)
	}

	// Sort all slices deterministically by key.
	sort.Slice(agents, func(i, j int) bool { return agents[i].Key < agents[j].Key })
	sort.Slice(skills, func(i, j int) bool { return skills[i].Key < skills[j].Key })
	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Key < hooks[j].Key })

	return ArtifactSet{Agents: agents, Skills: skills, Hooks: hooks}, nil
}
