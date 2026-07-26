package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
)

// catalogImpl is the concrete, immutable implementation of Catalog populated by loadCatalog.
type catalogImpl struct {
	root        string
	workers     []domain.Agent  // sorted by Key
	orchestr    domain.Agent
	utilities   []domain.Agent
	agentIdx    map[string]domain.Agent  // all agents by key (workers + orchestrator + utilities)
	skills      []domain.Skill
	skillIdx    map[string]domain.Skill
	hooks       []domain.HookBundle
	hookIdx     map[string]domain.HookBundle
	workflows   []domain.Workflow
	wfIdx       map[string]domain.Workflow
	categories  []domain.WorkflowCategory // in index order
	tiers       []domain.TierInfo
	issues      []Issue
	sourcePaths map[string]bool // every absolute path emitted by the catalog
}

// Root returns the absolute MOSAIC repository root passed to Load.
func (c *catalogImpl) Root() string { return c.root }

// Agents returns all worker agents sorted by Key.
func (c *catalogImpl) Agents() []domain.Agent { return c.workers }

// Agent looks up any agent by key regardless of role.
func (c *catalogImpl) Agent(key string) (domain.Agent, bool) {
	a, ok := c.agentIdx[key]
	return a, ok
}

// Orchestrator returns the single orchestrator agent.
func (c *catalogImpl) Orchestrator() domain.Agent { return c.orchestr }

// UtilityAgents returns all utility agents.
func (c *catalogImpl) UtilityAgents() []domain.Agent { return c.utilities }

// Skills returns all skills.
func (c *catalogImpl) Skills() []domain.Skill { return c.skills }

// Skill looks up a skill by its folder-name key.
func (c *catalogImpl) Skill(key string) (domain.Skill, bool) {
	s, ok := c.skillIdx[key]
	return s, ok
}

// Hooks returns all hook bundles.
func (c *catalogImpl) Hooks() []domain.HookBundle { return c.hooks }

// Hook looks up a hook bundle by its id key.
func (c *catalogImpl) Hook(key string) (domain.HookBundle, bool) {
	h, ok := c.hookIdx[key]
	return h, ok
}

// Workflows returns all workflows listed in the index, excluding underscore-prefixed files.
func (c *catalogImpl) Workflows() []domain.Workflow { return c.workflows }

// Workflow looks up a workflow by its id.
func (c *catalogImpl) Workflow(id string) (domain.Workflow, bool) {
	w, ok := c.wfIdx[id]
	return w, ok
}

// WorkflowCategories returns workflows grouped by source-folder category, in index order.
func (c *catalogImpl) WorkflowCategories() []domain.WorkflowCategory { return c.categories }

// Tiers returns exactly the tier strings present in source, unnormalised and unvalidated.
func (c *catalogImpl) Tiers() []domain.TierInfo { return c.tiers }

// WorkflowSection extracts the [[SECTION:Workflow:{id}]] block byte-identically.
func (c *catalogImpl) WorkflowSection(id string) ([]byte, error) {
	w, ok := c.wfIdx[id]
	if !ok {
		return nil, fmt.Errorf("catalog: workflow %q not found", id)
	}
	data, err := os.ReadFile(w.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("catalog: read workflow %q: %w", id, err)
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("catalog: parse workflow %q: %w", id, err)
	}
	sectionName := "Workflow:" + id
	node, ok := doc.Body().Section(sectionName)
	if !ok {
		return nil, fmt.Errorf("catalog: workflow %q has no [[SECTION:%s]] block in source file", id, sectionName)
	}
	return node.Bytes(), nil
}

// ReadSource returns raw bytes for a path previously reported by the catalog.
// Returns an error for any path not emitted by the catalog.
func (c *catalogImpl) ReadSource(path string) ([]byte, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: ReadSource: %w", err)
	}
	if !c.sourcePaths[absPath] {
		return nil, fmt.Errorf("catalog: path %q was not emitted by this catalog", path)
	}
	return os.ReadFile(absPath)
}

// Issues reports all index/disk reconciliation mismatches and hook integrity errors.
func (c *catalogImpl) Issues() []Issue { return c.issues }

// loadCatalog reads the full MOSAIC source tree rooted at root and returns an immutable Catalog.
func loadCatalog(root string) (Catalog, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("catalog.Load: %w", err)
	}

	cat := &catalogImpl{
		root:        absRoot,
		agentIdx:    make(map[string]domain.Agent),
		skillIdx:    make(map[string]domain.Skill),
		hookIdx:     make(map[string]domain.HookBundle),
		wfIdx:       make(map[string]domain.Workflow),
		sourcePaths: make(map[string]bool),
	}

	cat.issues = append(cat.issues, cat.loadAgents(absRoot)...)
	cat.issues = append(cat.issues, cat.loadSkills(absRoot)...)
	cat.issues = append(cat.issues, cat.loadHooks(absRoot)...)
	cat.issues = append(cat.issues, cat.loadWorkflows(absRoot)...)
	cat.tiers = buildTiers(cat.agentIdx)

	return cat, nil
}
