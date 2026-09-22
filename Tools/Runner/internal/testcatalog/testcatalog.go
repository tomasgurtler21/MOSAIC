// Package testcatalog reads workflow .md files from the test catalog directory,
// parses their YAML frontmatter (the modes and smoke_set fields), and returns
// structured catalog data. It is the single programmatic interface to catalog
// metadata used by the test orchestrator, CLI, and TUI.
//
// Usage:
//
//	cat, err := testcatalog.Load("/path/to/Tools/Runner/TestCatalog")
//	if err != nil {
//	    // catalog is unusable
//	}
//	entries := cat.SmokeSet()
//
// Load scans Workflows/MosaicTest/ for .md files (skipping sub-directories),
// parses each file's YAML frontmatter, validates mode and smoke_set
// declarations, and verifies that each workflow's Fixtures/{id}/ directory
// exists under Workflows/MosaicTest/. An error is returned if any file fails
// validation.
//
// Layering: this package sits alongside cli/tui at the peer layer.
// It must not be imported by session, engine, or domain.
package testcatalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrWorkflowNotFound is returned by WorkflowByID and WorkflowModes when the
// requested workflow ID is not present in the catalog.
var ErrWorkflowNotFound = errors.New("testcatalog: workflow not found")

// validModes is the approved mode vocabulary.
var validModes = map[string]bool{
	"auto":         true,
	"auto-review":  true,
	"orchestrated": true,
}

// CatalogEntry represents one (workflow, mode) pair from the catalog.
// Each workflow that declares N modes produces N CatalogEntry values.
type CatalogEntry struct {
	// WorkflowID is the workflow identifier (e.g. "smoke-single").
	WorkflowID string

	// Mode is one specific execution mode declared for this workflow.
	Mode string

	// FixturePath is the absolute path to the workflow's fixture directory:
	//   <catalogRoot>/Workflows/MosaicTest/Fixtures/<WorkflowID>/
	FixturePath string

	// AllModes lists every mode this workflow declares (superset of Mode),
	// sorted alphabetically. Useful when the caller needs the full mode list
	// for a single workflow without constructing all entries.
	AllModes []string

	// InSmokeSet is true when this specific (WorkflowID, Mode) pair is a
	// member of the Smoke Set as declared in the workflow's smoke_set field.
	InSmokeSet bool

	// PreConsult indicates whether the pre-consultation path should be
	// exercised for test runs of this workflow. Defaults to true when
	// the workflow's frontmatter omits the pre_consult field.
	PreConsult bool

	// InfrastructureAgents lists the infrastructure agent keys this workflow
	// declares. Always non-nil: absent frontmatter field produces []string{}.
	// Consumed by the run invoker to emit --infrastructure= per subprocess.
	InfrastructureAgents []string

	// Checkpoints is the workflow's checkpoint declaration: "enabled" or
	// "disabled". Consumed by the run invoker to emit --checkpoints.
	Checkpoints string

	// Commits is the workflow's commit declaration: "enabled" or "disabled".
	// Consumed by the run invoker to emit --commits.
	Commits string
}

// Catalog is the read-only view of the test catalog's workflow metadata.
// Obtain a Catalog via Load; do not construct directly.
type Catalog struct {
	// root is the catalogRoot supplied to Load.
	root string
	// workflows holds parsed metadata in sorted order by workflow ID.
	workflows []*workflowMeta
	// index maps workflow ID to its position in workflows for O(1) lookup.
	index map[string]int
}

// workflowMeta holds parsed and validated metadata for a single workflow file.
type workflowMeta struct {
	id                   string
	modes                []string // sorted alphabetically
	smokeSet             map[string]bool
	fixturePath          string
	preConsult           bool // defaults to true; false only when frontmatter declares pre_consult: false
	infrastructureAgents []string
	checkpoints          string
	commits              string
}

// workflowFrontmatter is the YAML structure parsed from a workflow .md file.
type workflowFrontmatter struct {
	ID       string   `yaml:"id"`
	Modes    []string `yaml:"modes"`
	SmokeSet []string `yaml:"smoke_set"`
	// PreConsult declares whether the pre-consultation path is exercised
	// for this workflow's test runs. Pointer type so nil (absent) is
	// distinguishable from explicit false. Nil defaults to true.
	PreConsult           *bool    `yaml:"pre_consult"`
	InfrastructureAgents []string `yaml:"infrastructure_agents"`
	Checkpoints          string   `yaml:"checkpoints"`
	Commits              string   `yaml:"commits"`
}

// Load reads the test catalog rooted at catalogRoot. It scans
// Workflows/MosaicTest/ for .md files, parses their YAML frontmatter,
// validates mode/smoke_set declarations, and verifies that each workflow's
// Fixtures/{id}/ directory exists. Returns an error if the catalog root
// is missing, contains no workflows, or any workflow has invalid metadata.
func Load(catalogRoot string) (*Catalog, error) {
	workflowsDir := filepath.Join(catalogRoot, "Workflows", "MosaicTest")

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("testcatalog: cannot read catalog directory %q: %w", workflowsDir, err)
	}

	var metas []*workflowMeta
	for _, entry := range entries {
		// Skip directories (e.g. Fixtures/) and non-.md files.
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		filePath := filepath.Join(workflowsDir, name)
		meta, err := parseWorkflowFile(filePath, workflowsDir)
		if err != nil {
			return nil, fmt.Errorf("testcatalog: error in %q: %w", name, err)
		}
		metas = append(metas, meta)
	}

	if len(metas) == 0 {
		return nil, fmt.Errorf("testcatalog: catalog at %q contains no workflow .md files", catalogRoot)
	}

	// Sort by workflow ID for deterministic ordering.
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].id < metas[j].id
	})

	index := make(map[string]int, len(metas))
	for i, m := range metas {
		index[m.id] = i
	}

	return &Catalog{
		root:      catalogRoot,
		workflows: metas,
		index:     index,
	}, nil
}

// parseWorkflowFile reads and validates a single workflow .md file.
// workflowsDir is the Workflows/MosaicTest/ directory, used to resolve fixture paths.
func parseWorkflowFile(filePath string, workflowsDir string) (*workflowMeta, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	content := string(data)
	fm, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}

	// Validate required fields.
	if fm.ID == "" {
		// Use filename stem as fallback only for error messages.
		base := strings.TrimSuffix(filepath.Base(filePath), ".md")
		return nil, fmt.Errorf("workflow %q: frontmatter missing required field 'id'", base)
	}
	if len(fm.Modes) == 0 {
		return nil, fmt.Errorf("workflow %q: frontmatter 'modes' field is missing or empty", fm.ID)
	}

	// Validate mode vocabulary.
	for _, mode := range fm.Modes {
		if !validModes[mode] {
			return nil, fmt.Errorf("workflow %q: unknown mode %q (valid: auto, auto-review, orchestrated)", fm.ID, mode)
		}
	}

	// Validate smoke_set entries are a subset of modes.
	modeSet := make(map[string]bool, len(fm.Modes))
	for _, m := range fm.Modes {
		modeSet[m] = true
	}
	for _, s := range fm.SmokeSet {
		if !modeSet[s] {
			return nil, fmt.Errorf("workflow %q: smoke_set entry %q not declared in modes", fm.ID, s)
		}
	}

	// Verify fixture directory exists.
	fixturePath, err := filepath.Abs(filepath.Join(workflowsDir, "Fixtures", fm.ID))
	if err != nil {
		return nil, fmt.Errorf("workflow %q: cannot resolve fixture path: %w", fm.ID, err)
	}
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("workflow %q: fixture directory %q does not exist", fm.ID, fixturePath)
	}

	// Sort modes alphabetically.
	modes := make([]string, len(fm.Modes))
	copy(modes, fm.Modes)
	sort.Strings(modes)

	smokeSet := make(map[string]bool, len(fm.SmokeSet))
	for _, s := range fm.SmokeSet {
		smokeSet[s] = true
	}

	// Resolve pre_consult: nil (absent) defaults to true; dereference when explicit.
	preConsult := true
	if fm.PreConsult != nil {
		preConsult = *fm.PreConsult
	}

	// Validate checkpoints value.
	if fm.Checkpoints != "" && fm.Checkpoints != "enabled" && fm.Checkpoints != "disabled" {
		return nil, fmt.Errorf("workflow %q: invalid checkpoints value %q; valid values: disabled, enabled", fm.ID, fm.Checkpoints)
	}
	checkpoints := fm.Checkpoints
	if checkpoints == "" {
		checkpoints = "disabled"
	}

	// Validate commits value.
	if fm.Commits != "" && fm.Commits != "enabled" && fm.Commits != "disabled" {
		return nil, fmt.Errorf("workflow %q: invalid commits value %q; valid values: disabled, enabled", fm.ID, fm.Commits)
	}
	commits := fm.Commits
	if commits == "" {
		commits = "disabled"
	}

	// Validate and normalize infrastructure_agents.
	// Nil from YAML is normalized to non-nil empty []string{}.
	infraAgents := make([]string, 0)
	seen := make(map[string]bool, len(fm.InfrastructureAgents))
	for _, key := range fm.InfrastructureAgents {
		if key == "" {
			return nil, fmt.Errorf("workflow %q: infrastructure_agents contains empty entry", fm.ID)
		}
		if seen[key] {
			return nil, fmt.Errorf("workflow %q: duplicate infrastructure_agents entry %q", fm.ID, key)
		}
		seen[key] = true
		infraAgents = append(infraAgents, key)
	}

	return &workflowMeta{
		id:                   fm.ID,
		modes:                modes,
		smokeSet:             smokeSet,
		fixturePath:          fixturePath,
		preConsult:           preConsult,
		infrastructureAgents: infraAgents,
		checkpoints:          checkpoints,
		commits:              commits,
	}, nil
}

// extractFrontmatter parses YAML frontmatter from a markdown file's content.
// Frontmatter must be delimited by "---" lines at the start of the file.
func extractFrontmatter(content string) (*workflowFrontmatter, error) {
	// Normalize line endings.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, "---\n") {
		return nil, errors.New("missing YAML frontmatter (file must start with '---')")
	}

	// Find the closing delimiter.
	rest := content[4:] // skip opening "---\n"
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, errors.New("missing closing YAML frontmatter delimiter '---'")
	}

	yamlContent := rest[:end]

	var fm workflowFrontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	return &fm, nil
}

// buildEntry constructs a CatalogEntry from a workflowMeta for a given mode.
func buildEntry(m *workflowMeta, mode string) CatalogEntry {
	allModes := make([]string, len(m.modes))
	copy(allModes, m.modes)
	// Copy infrastructure_agents into a fresh non-nil slice per entry (non-aliasing guarantee).
	infraAgents := make([]string, len(m.infrastructureAgents))
	copy(infraAgents, m.infrastructureAgents)
	return CatalogEntry{
		WorkflowID:           m.id,
		Mode:                 mode,
		FixturePath:          m.fixturePath,
		AllModes:             allModes,
		InSmokeSet:           m.smokeSet[mode],
		PreConsult:           m.preConsult,
		InfrastructureAgents: infraAgents,
		Checkpoints:          m.checkpoints,
		Commits:              m.commits,
	}
}

// Workflows returns all catalog entries, sorted by workflow ID then mode.
// Each workflow appears once per declared mode.
func (c *Catalog) Workflows() []CatalogEntry {
	return c.FullSuite()
}

// SmokeSet returns only the (workflow, mode) combinations that belong
// to the Smoke Set, sorted by workflow ID then mode.
func (c *Catalog) SmokeSet() []CatalogEntry {
	var result []CatalogEntry
	for _, m := range c.workflows {
		for _, mode := range m.modes {
			if m.smokeSet[mode] {
				result = append(result, buildEntry(m, mode))
			}
		}
	}
	return result
}

// FullSuite returns every (workflow, mode) combination in the catalog,
// sorted by workflow ID then mode. Each workflow appears once per
// declared mode.
func (c *Catalog) FullSuite() []CatalogEntry {
	var result []CatalogEntry
	for _, m := range c.workflows {
		for _, mode := range m.modes {
			result = append(result, buildEntry(m, mode))
		}
	}
	return result
}

// WorkflowByID returns all CatalogEntry values for the given workflow ID
// (one per declared mode, sorted by mode), or ErrWorkflowNotFound if the
// ID is not in the catalog. Each returned entry has its Mode field set to
// one specific mode and AllModes populated with the full set.
func (c *Catalog) WorkflowByID(id string) ([]CatalogEntry, error) {
	idx, ok := c.index[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	m := c.workflows[idx]
	entries := make([]CatalogEntry, len(m.modes))
	for i, mode := range m.modes {
		entries[i] = buildEntry(m, mode)
	}
	return entries, nil
}

// WorkflowModes returns the declared modes for a workflow, sorted
// alphabetically, or ErrWorkflowNotFound if the workflow ID is not in the
// catalog. Prefer this over WorkflowByID when only the mode list is needed.
func (c *Catalog) WorkflowModes(id string) ([]string, error) {
	idx, ok := c.index[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	m := c.workflows[idx]
	result := make([]string, len(m.modes))
	copy(result, m.modes)
	return result, nil
}

// WorkflowIDs returns the sorted list of all workflow IDs in the catalog.
func (c *Catalog) WorkflowIDs() []string {
	ids := make([]string, len(c.workflows))
	for i, m := range c.workflows {
		ids[i] = m.id
	}
	return ids
}

// SidecarPath returns the expected-outcome sidecar file path for a
// (workflowID, mode) pair. The path is under the catalog root:
//
//	<catalogRoot>/Workflows/MosaicTest/<workflowID>-<mode>.expected.json
//
// SidecarPath does not verify that the file exists.
func (c *Catalog) SidecarPath(workflowID string, mode string) string {
	filename := workflowID + "-" + mode + ".expected.json"
	return filepath.Join(c.root, "Workflows", "MosaicTest", filename)
}

// UnionInfrastructureAgentKeys returns the sorted, deduplicated union of
// infrastructure_agents declared across all workflows in the catalog.
// Always returns a non-nil slice: when no workflow declares any agents,
// returns []string{} (non-nil empty). This is critical: nil would cause
// buildDeployArgs to omit --infrastructure, deploying the default set.
func (c *Catalog) UnionInfrastructureAgentKeys() []string {
	seen := make(map[string]bool)
	for _, m := range c.workflows {
		for _, key := range m.infrastructureAgents {
			seen[key] = true
		}
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
