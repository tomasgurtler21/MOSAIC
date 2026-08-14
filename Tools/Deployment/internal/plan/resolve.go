package plan

import (
	"fmt"
	"sort"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// Selection is the complete set of user selections that determine a run's artifact set.
// A nil or empty slice means "none selected" for that dimension.
type Selection struct {
	WorkflowIDs            []string
	UtilityAgentIDs        []string
	InfrastructureAgentIDs []string
	StandaloneAgentIDs     []string
	HookIDs                []string
}

// ResolveArtifactsFrom derives the complete artifact set from sel. It is the full-fidelity
// entry point; ResolveArtifacts delegates to it.
//
// The resulting ArtifactSet contains:
//   - The orchestrator (always present, regardless of other selections)
//   - Every referenced agent from the selected workflows
//   - Every utility agent whose key appears in sel.UtilityAgentIDs
//   - Every infrastructure agent whose key appears in sel.InfrastructureAgentIDs
//   - Every standalone agent whose key appears in sel.StandaloneAgentIDs
//   - Every skill transitively required by any included agent
//   - Every hook bundle whose key appears in sel.HookIDs
//
// Deduplication is applied across all sources. Returned slices are ordered deterministically:
// Agents by key, Skills by key, Hooks by key.
//
// Errors:
//   - ErrUnknownWorkflow if any workflow ID is not found in the catalog
//   - ErrUnknownAgent if any workflow references an agent that does not exist, or if any
//     infrastructure or standalone agent ID is not found in the catalog
func ResolveArtifactsFrom(c catalog.Catalog, sel Selection) (ArtifactSet, error) {
	agentsSeen := make(map[string]bool)
	var agents []domain.Agent

	// The orchestrator is always included.
	orc := c.Orchestrator()
	agentsSeen[orc.Key] = true
	agents = append(agents, orc)

	// Collect agents referenced by selected workflows.
	for _, wfID := range sel.WorkflowIDs {
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
	for _, utilityKey := range sel.UtilityAgentIDs {
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
	for _, infraKey := range sel.InfrastructureAgentIDs {
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

	// Collect explicitly selected standalone agents. They enter artifact resolution as their
	// own class and contribute their required skills exactly as infrastructure agents do.
	for _, standaloneKey := range sel.StandaloneAgentIDs {
		if agentsSeen[standaloneKey] {
			continue
		}
		agent, ok := c.Agent(standaloneKey)
		if !ok {
			return ArtifactSet{}, fmt.Errorf("%w: standalone agent %q not found in catalog", ErrUnknownAgent, standaloneKey)
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
	for _, hookKey := range sel.HookIDs {
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

// ResolveArtifacts is the positional form retained for existing call sites. It is exactly
// ResolveArtifactsFrom with StandaloneAgentIDs left nil.
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
	return ResolveArtifactsFrom(c, Selection{
		WorkflowIDs:            workflowIDs,
		UtilityAgentIDs:        utilityAgentIDs,
		InfrastructureAgentIDs: infrastructureAgentIDs,
		HookIDs:                hookIDs,
	})
}

