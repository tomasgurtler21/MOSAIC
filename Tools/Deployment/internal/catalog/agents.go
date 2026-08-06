package catalog

import (
	"fmt"
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
	if orch, orchIssues, err := parseAgentFile(orchPath, domain.RoleOrchestrator, ""); err == nil {
		c.orchestr = orch
		c.agentIdx[orch.Key] = orch
		c.sourcePaths[orchPath] = true
		issues = append(issues, orchIssues...)
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
				agent, agentIssues, err := parseAgentFile(agentPath, domain.RoleSubagent, category)
				if err != nil {
					continue
				}
				issues = append(issues, agentIssues...)
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
			agent, utilIssues, err := parseAgentFile(utilPath, domain.RoleUtility, "")
			if err != nil {
				continue
			}
			issues = append(issues, utilIssues...)
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
//
// When frontmatter carries a `role` scalar, ParseAgentRole decides the agent's role and the
// path-derived role (the fallback parameter) is ignored. When the field is absent, the
// path-derived role is used. When the field is present but unrecognised, the path-derived
// role is used and a catalog.Issue with code "invalid-role" (severity error) is returned —
// a malformed role must not silently drop an agent from the catalog.
func parseAgentFile(path string, role domain.AgentRole, category string) (domain.Agent, []Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Agent{}, nil, err
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.Agent{}, nil, err
	}
	fm := doc.Frontmatter()

	key := strings.TrimSuffix(filepath.Base(path), ".md")

	var issues []Issue

	// Read role from frontmatter when present; fall back to the path-derived role otherwise.
	if v, ok := fm.Get("role"); ok && v.Kind == domain.KindScalar {
		if parsed, ok := domain.ParseAgentRole(v.Scalar); ok {
			role = parsed
		} else {
			// Unrecognised role: keep path-derived role and record an issue.
			issues = append(issues, Issue{
				Severity: docformat.SeverityError,
				Code:     "invalid-role",
				Subject:  key,
				Message:  fmt.Sprintf("agent %q has unrecognised frontmatter role %q; expected \"subagent\" or \"orchestrator\"", key, v.Scalar),
				Path:     path,
			})
		}
	}

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

	return agent, issues, nil
}
