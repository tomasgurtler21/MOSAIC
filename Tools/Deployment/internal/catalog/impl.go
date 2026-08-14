package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// catalogImpl is the concrete, immutable implementation of Catalog populated by loadCatalog.
type catalogImpl struct {
	root         string
	catalogRoot  string
	workers      []domain.Agent  // sorted by Key
	orchestr     domain.Agent
	utilities    []domain.Agent
	agentIdx     map[string]domain.Agent  // all agents by key (workers + orchestrator + utilities)
	numericIDIdx map[string]domain.Agent  // agents by numeric frontmatter `id`; excludes empty-id agents
	skills       []domain.Skill
	skillIdx     map[string]domain.Skill
	hooks        []domain.HookBundle
	hookIdx      map[string]domain.HookBundle
	workflows    []domain.Workflow
	wfIdx        map[string]domain.Workflow
	categories   []domain.WorkflowCategory // in alphabetical order by category name
	tiers        []domain.TierInfo
	issues       []Issue
	sourcePaths  map[string]bool // every absolute path emitted by the catalog
}

// Root returns the absolute MOSAIC repository root passed to Load.
func (c *catalogImpl) Root() string { return c.root }

// CatalogRoot returns the absolute catalogue root used by this catalog.
func (c *catalogImpl) CatalogRoot() string { return c.catalogRoot }

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

// InfrastructureAgents returns all worker agents with a non-empty Infrastructure field,
// in the same sorted order as Agents().
func (c *catalogImpl) InfrastructureAgents() []domain.Agent {
	var result []domain.Agent
	for _, a := range c.workers {
		if a.Infrastructure != "" {
			result = append(result, a)
		}
	}
	return result
}

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

// Workflows returns all eligible workflow files found on disk under Catalog/Workflows/
// subdirectories, excluding root-level files and underscore-prefixed files.
func (c *catalogImpl) Workflows() []domain.Workflow { return c.workflows }

// Workflow looks up a workflow by its id.
func (c *catalogImpl) Workflow(id string) (domain.Workflow, bool) {
	w, ok := c.wfIdx[id]
	return w, ok
}

// WorkflowCategories returns workflows grouped by source-folder category, in alphabetical
// order by category name. Within a category, workflows are sorted ascending by ID.
func (c *catalogImpl) WorkflowCategories() []domain.WorkflowCategory { return c.categories }

// Tiers returns exactly the tier strings present in source, unnormalised and unvalidated.
func (c *catalogImpl) Tiers() []domain.TierInfo { return c.tiers }

// WorkflowSection extracts the <Workflow type="core" name="{id}"> block byte-identically.
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
		return nil, fmt.Errorf("catalog: workflow %q has no <Workflow type=\"core\" name=%q> block in source file", id, id)
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

// Issues reports hook bundle integrity errors encountered during Load. It does not emit
// file-orphan or index-orphan codes; those are produced exclusively by CheckWorkflowIndex.
func (c *catalogImpl) Issues() []Issue { return c.issues }

// loadCatalog reads the full MOSAIC source tree and returns an immutable Catalog.
// mosaicRoot is the MOSAIC repository root; catalogRoot is the (already-absolute) catalogue root.
// Agents and workflows are loaded exclusively from catalogRoot.
// Skills and hooks are merged from both the default catalogue root and catalogRoot,
// with catalogRoot winning on key collision. Bundle and protocol files are always read
// from mosaicRoot regardless of catalogRoot.
func loadCatalog(mosaicRoot, catalogRoot string) (Catalog, error) {
	absRoot, err := filepath.Abs(mosaicRoot)
	if err != nil {
		return nil, fmt.Errorf("catalog.Load: %w", err)
	}

	// defaultCatalogRoot is the Catalog/ directory under the MOSAIC root.
	defaultCatalogRoot := DefaultCatalogRoot(absRoot)

	cat := &catalogImpl{
		root:        absRoot,
		catalogRoot: catalogRoot,
		agentIdx:    make(map[string]domain.Agent),
		skillIdx:    make(map[string]domain.Skill),
		hookIdx:     make(map[string]domain.HookBundle),
		wfIdx:       make(map[string]domain.Workflow),
		sourcePaths: make(map[string]bool),
	}

	// Agents and workflows are loaded exclusively from the catalogue root.
	cat.issues = append(cat.issues, cat.loadAgents(catalogRoot)...)
	cat.issues = append(cat.issues, buildNumericIDIndex(cat)...)
	cat.issues = append(cat.issues, cat.loadWorkflows(catalogRoot)...)

	// Skills and hooks merge from both the default catalogue root and the catalogue root.
	// The catalogue root wins on key collision; shadowing is silent (no Issue produced).
	cat.issues = append(cat.issues, cat.loadSkillsMerged(defaultCatalogRoot, catalogRoot)...)
	cat.issues = append(cat.issues, cat.loadHooksMerged(defaultCatalogRoot, catalogRoot)...)

	cat.tiers = buildTiers(cat.agentIdx)

	return cat, nil
}
