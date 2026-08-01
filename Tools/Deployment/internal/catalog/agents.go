package catalog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// loadAgents scans the three agent directories and populates workers, orchestrator,
// utilities, agentIdx, and sourcePaths on the receiver.
func (c *catalogImpl) loadAgents(root string) []Issue {
	var issues []Issue

	// Orchestrator: single file at Agents/Generic/Orchestrator/orchestrator.md
	orchPath := filepath.Join(root, "Agents", "Generic", "Orchestrator", "orchestrator.md")
	if orch, err := parseAgentFile(orchPath, domain.RoleOrchestrator, ""); err == nil {
		c.orchestr = orch
		c.agentIdx[orch.Key] = orch
		c.sourcePaths[orchPath] = true
	}
	// Missing orchestrator is not a hard error — loadCatalog still returns successfully.

	// Worker agents: Agents/Generic/Agents/{category}/*.md (excluding README.md)
	agentsDir := filepath.Join(root, "Agents", "Generic", "Agents")
	if catEntries, err := os.ReadDir(agentsDir); err == nil {
		for _, catEntry := range catEntries {
			if !catEntry.IsDir() {
				continue
			}
			category := catEntry.Name()
			catDir := filepath.Join(agentsDir, category)
			fileEntries, err := os.ReadDir(catDir)
			if err != nil {
				continue
			}
			for _, fe := range fileEntries {
				if fe.IsDir() {
					continue
				}
				name := fe.Name()
				if name == "README.md" || !strings.HasSuffix(name, ".md") {
					continue
				}
				agentPath := filepath.Join(catDir, name)
				agent, err := parseAgentFile(agentPath, domain.RoleWorker, category)
				if err != nil {
					continue
				}
				c.workers = append(c.workers, agent)
				c.agentIdx[agent.Key] = agent
				c.sourcePaths[agentPath] = true
			}
		}
	}
	sort.Slice(c.workers, func(i, j int) bool {
		return c.workers[i].Key < c.workers[j].Key
	})

	// Utility agents: Agents/Generic/UtilityAgents/*.md (excluding README.md)
	utilDir := filepath.Join(root, "Agents", "Generic", "UtilityAgents")
	if entries, err := os.ReadDir(utilDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "README.md" || !strings.HasSuffix(name, ".md") {
				continue
			}
			utilPath := filepath.Join(utilDir, name)
			agent, err := parseAgentFile(utilPath, domain.RoleUtility, "")
			if err != nil {
				continue
			}
			c.utilities = append(c.utilities, agent)
			c.agentIdx[agent.Key] = agent
			c.sourcePaths[utilPath] = true
		}
	}
	sort.Slice(c.utilities, func(i, j int) bool {
		return c.utilities[i].Key < c.utilities[j].Key
	})

	return issues
}

// parseAgentFile reads and parses a single agent markdown file, populating an Agent struct
// from its frontmatter. The key is derived from the file's base name without the .md extension.
func parseAgentFile(path string, role domain.AgentRole, category string) (domain.Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Agent{}, err
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.Agent{}, err
	}
	fm := doc.Frontmatter()

	key := strings.TrimSuffix(filepath.Base(path), ".md")
	agent := domain.Agent{
		Key:            key,
		Role:           role,
		Category:       category,
		SourcePath:     path,
		RequiredSkills: []string{}, // always non-nil
	}

	if v, ok := fm.Get("id"); ok && v.Kind == domain.KindScalar {
		agent.NumericID = v.Scalar
	}
	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar {
		agent.Version = v.Scalar
	}
	if v, ok := fm.Get("name"); ok && v.Kind == domain.KindScalar {
		agent.Name = v.Scalar
	}
	if v, ok := fm.Get("description"); ok && v.Kind == domain.KindScalar {
		agent.Description = v.Scalar
	}
	if v, ok := fm.Get("recommended_tier"); ok && v.Kind == domain.KindScalar {
		agent.RecommendedTier = domain.Tier(v.Scalar)
	}
	if v, ok := fm.Get("tier_rationale"); ok && v.Kind == domain.KindScalar {
		agent.TierRationale = v.Scalar
	}

	// tools: either a list (real tools) or a scalar (placeholder like {tool-permissions})
	if v, ok := fm.Get("tools"); ok {
		switch v.Kind {
		case domain.KindList:
			tools := make([]string, 0, len(v.Items))
			for _, item := range v.Items {
				tools = append(tools, item.Scalar)
			}
			agent.Tools = tools
		case domain.KindScalar:
			agent.ToolsPlaceholder = v.Scalar
		}
	}

	// required_skills: list of skill keys; defaults to empty non-nil slice
	if v, ok := fm.Get("required_skills"); ok && v.Kind == domain.KindList {
		for _, item := range v.Items {
			agent.RequiredSkills = append(agent.RequiredSkills, item.Scalar)
		}
	}

	// Infrastructure fields — only present on infrastructure agents.

	// infrastructure: scalar class name ("checkpoint", "commit", "review")
	if v, ok := fm.Get("infrastructure"); ok && v.Kind == domain.KindScalar {
		agent.Infrastructure = v.Scalar
	}

	// triggers: list of maps, each with "trigger" and "trigger_param" keys.
	// The triggers field is only set when the key exists in the frontmatter.
	if v, ok := fm.Get("triggers"); ok && v.Kind == domain.KindList {
		for _, item := range v.Items {
			if item.Kind != domain.KindMapping {
				continue
			}
			var trig domain.InfrastructureTrigger
			for _, pair := range item.Pairs {
				if pair.Value.Kind != domain.KindScalar {
					continue
				}
				switch pair.Key {
				case "trigger":
					trig.Trigger = pair.Value.Scalar
				case "trigger_param":
					// Normalise YAML null ("null") to empty string; non-null values are kept verbatim.
					if pair.Value.Scalar != "null" {
						trig.TriggerParam = pair.Value.Scalar
					}
				}
			}
			agent.Triggers = append(agent.Triggers, trig)
		}
	}

	// on_failure: scalar policy ("halt" or "continue")
	if v, ok := fm.Get("on_failure"); ok && v.Kind == domain.KindScalar {
		agent.OnFailure = v.Scalar
	}

	return agent, nil
}
