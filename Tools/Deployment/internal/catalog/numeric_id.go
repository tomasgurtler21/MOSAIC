package catalog

import (
	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// numeric_id.go contains the AgentByNumericID method on catalogImpl and the helper
// that builds the numeric-id index during catalog load.

// AgentByNumericID looks up any agent by its frontmatter `id` scalar.
// See catalog.Catalog.AgentByNumericID for the full contract.
func (c *catalogImpl) AgentByNumericID(id string) (domain.Agent, bool) {
	if id == "" {
		return domain.Agent{}, false
	}
	a, ok := c.numericIDIdx[id]
	return a, ok
}

// buildNumericIDIndex constructs the id→agent map from the catalog's agentIdx.
// It is called once by loadCatalog after all agents have been loaded and sorted.
// When two agents declare the same id, a "duplicate-agent-id" issue is appended to issues
// and the first agent (by key order, since workers are already sorted) wins the slot.
func buildNumericIDIndex(cat *catalogImpl) []Issue {
	var issues []Issue
	idx := make(map[string]domain.Agent)

	// Process in a deterministic order: sorted workers first, then utilities, then orchestrator.
	// Workers are already sorted by Key after loadAgents.
	allAgents := make([]domain.Agent, 0, len(cat.workers)+len(cat.utilities)+1)
	allAgents = append(allAgents, cat.workers...)
	allAgents = append(allAgents, cat.utilities...)
	if cat.orchestr.Key != "" {
		allAgents = append(allAgents, cat.orchestr)
	}

	for _, agent := range allAgents {
		if agent.NumericID == "" {
			continue
		}
		if _, exists := idx[agent.NumericID]; exists {
			// Duplicate — record an issue; keep the first.
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "duplicate-agent-id",
				Subject:  agent.NumericID,
				Message:  "two catalog agents declare the same numeric id: " + agent.NumericID,
				Path:     agent.SourcePath,
			})
			continue
		}
		idx[agent.NumericID] = agent
	}

	cat.numericIDIdx = idx
	return issues
}
