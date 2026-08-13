package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Workflow index row
// ---------------------------------------------------------------------------

type indexRow struct {
	ID          string
	Category    string
	Version     string
	Name        string
	Description string
	Hint        string
	File        string // relative to Workflows/ directory
}

// colIndices holds the 0-based column positions for the workflow index table header.
type colIndices struct {
	id, cat, ver, name, desc, hint, file int
}

// ---------------------------------------------------------------------------
// Scanner
// ---------------------------------------------------------------------------

// loadWorkflows scans the Workflows/ directory for eligible .md files and loads every
// file it finds as an authoritative workflow. Index.md is never read during loading;
// its content, staleness, or absence has no effect on which workflows are loaded.
func (c *catalogImpl) loadWorkflows(root string) []Issue {
	wfRoot := filepath.Join(root, "Workflows")

	// The set of eligible disk files is the authoritative workflow set.
	diskFiles := scanWorkflowDiskFiles(wfRoot)

	wfByCategory := make(map[string][]domain.Workflow)

	for relPath, absPath := range diskFiles {
		// Extract category from the relative path (e.g., "Alpha/workflow.md" → "Alpha").
		parts := strings.SplitN(relPath, "/", 2)
		if len(parts) != 2 {
			continue
		}
		category := parts[0]

		wf, err := parseWorkflowFile(absPath, category)
		if err != nil {
			continue
		}

		c.wfIdx[wf.ID] = wf
		c.sourcePaths[absPath] = true
		wfByCategory[category] = append(wfByCategory[category], wf)
	}

	// Build WorkflowCategories in alphabetical order by category name.
	categoryNames := make([]string, 0, len(wfByCategory))
	for cat := range wfByCategory {
		categoryNames = append(categoryNames, cat)
	}
	sort.Strings(categoryNames)

	for _, catName := range categoryNames {
		wfs := wfByCategory[catName]
		// Sort workflows within each category ascending by ID.
		sort.Slice(wfs, func(i, j int) bool {
			return wfs[i].ID < wfs[j].ID
		})
		c.categories = append(c.categories, domain.WorkflowCategory{
			Name:      catName,
			Workflows: wfs,
		})
		c.workflows = append(c.workflows, wfs...)
	}

	return nil
}

// scanWorkflowDiskFiles walks Workflows/ subdirectories and returns a map of
// relative-to-Workflows paths → absolute paths for all eligible .md files.
// Eligible files: in a subdirectory (not directly in Workflows/), base name does not start with '_'.
func scanWorkflowDiskFiles(wfRoot string) map[string]string {
	result := make(map[string]string)
	entries, err := os.ReadDir(wfRoot)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			// Skip root-level files (Index.md, _Template.md, _Legacy-Appendices.md, etc.)
			continue
		}
		catDir := filepath.Join(wfRoot, entry.Name())
		catEntries, err := os.ReadDir(catDir)
		if err != nil {
			continue
		}
		for _, fe := range catEntries {
			if fe.IsDir() {
				continue
			}
			name := fe.Name()
			if strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".md") {
				continue
			}
			relPath := entry.Name() + "/" + name
			absPath := filepath.Join(catDir, name)
			result[relPath] = absPath
		}
	}
	return result
}

// parseWorkflowFile reads a workflow .md file and builds a domain.Workflow from its
// frontmatter. category is the containing directory name and is used verbatim.
//
// Field sourcing rules:
//   - ID: frontmatter `id`; when absent or empty, the file's base name without .md.
//   - Name: frontmatter `name`; when absent or empty, falls back to ID.
//   - Version, Description, Hint: frontmatter scalars; empty when absent.
//   - ReferencedAgents: frontmatter `referenced_agents` list; nil when absent.
//   - Category: the category parameter, verbatim.
//   - SourcePath: path, verbatim.
func parseWorkflowFile(path, category string) (domain.Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Workflow{}, err
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.Workflow{}, err
	}
	fm := doc.Frontmatter()

	// ID: prefer frontmatter; fall back to base name without .md.
	id := ""
	if v, ok := fm.Get("id"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		id = v.Scalar
	}
	if id == "" {
		base := filepath.Base(path)
		id = strings.TrimSuffix(base, ".md")
	}

	wf := domain.Workflow{
		ID:         id,
		Category:   category,
		SourcePath: path,
	}

	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Version = v.Scalar
	}

	// Name: prefer frontmatter; fall back to ID.
	if v, ok := fm.Get("name"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Name = v.Scalar
	}
	if wf.Name == "" {
		wf.Name = id
	}

	if v, ok := fm.Get("description"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Description = v.Scalar
	}
	if v, ok := fm.Get("hint"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Hint = v.Scalar
	}

	// referenced_agents is a block list of agent slugs.
	if v, ok := fm.Get("referenced_agents"); ok && v.Kind == domain.KindList {
		for _, item := range v.Items {
			wf.ReferencedAgents = append(wf.ReferencedAgents, item.Scalar)
		}
	}

	return wf, nil
}

// ---------------------------------------------------------------------------
// Index parser
// ---------------------------------------------------------------------------

// parseWorkflowIndex parses Workflows/Index.md and returns the ordered list of index rows
// along with the category names in the order they first appear.
func parseWorkflowIndex(data []byte) ([]indexRow, []string) {
	var rows []indexRow
	var categoryOrder []string
	categorySet := make(map[string]bool)

	var cols *colIndices
	lines := bytes.Split(data, []byte("\n"))

	for _, rawLine := range lines {
		// Normalise CRLF.
		line := strings.TrimRight(string(rawLine), "\r")
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "|") {
			// Non-table line — reset header state so the next table gets fresh detection.
			cols = nil
			continue
		}

		cells := splitTableRow(trimmed)

		// Separator line (all cells are dash-only).
		if isSeparatorRow(cells) {
			continue
		}

		// Try to detect a header row when we don't have column indices yet.
		if cols == nil {
			if c := detectColIndices(cells); c != nil {
				cols = c
			}
			continue
		}

		// Data row.
		row := indexRow{
			ID:          safeCell(cells, cols.id),
			Category:    safeCell(cells, cols.cat),
			Version:     safeCell(cells, cols.ver),
			Name:        safeCell(cells, cols.name),
			Description: safeCell(cells, cols.desc),
			Hint:        safeCell(cells, cols.hint),
			File:        stripBackticks(safeCell(cells, cols.file)),
		}
		if row.ID == "" || row.File == "" {
			continue
		}
		rows = append(rows, row)

		if !categorySet[row.Category] {
			categorySet[row.Category] = true
			categoryOrder = append(categoryOrder, row.Category)
		}
	}

	return rows, categoryOrder
}

// splitTableRow splits a markdown table row like "| a | b | c |" into trimmed cells.
// Returns nil for lines that do not look like table rows.
func splitTableRow(s string) []string {
	if !strings.HasPrefix(s, "|") {
		return nil
	}
	parts := strings.Split(s, "|")
	// parts[0] is "" (before first |), parts[last] is "" (after last |)
	if len(parts) < 3 {
		return nil
	}
	cells := make([]string, 0, len(parts)-2)
	for _, p := range parts[1 : len(parts)-1] {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// isSeparatorRow returns true when at least one cell is non-empty and all non-empty
// cells consist only of '-' and ':' characters (standard Markdown table separator row).
func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	hasNonEmpty := false
	for _, c := range cells {
		if c == "" {
			continue
		}
		hasNonEmpty = true
		for _, ch := range c {
			if ch != '-' && ch != ':' {
				return false
			}
		}
	}
	return hasNonEmpty
}

// detectColIndices inspects a header row and maps column names to positions.
// Returns nil when the row is not a recognised workflow index table header.
func detectColIndices(cells []string) *colIndices {
	c := &colIndices{id: -1, cat: -1, ver: -1, name: -1, desc: -1, hint: -1, file: -1}
	for i, cell := range cells {
		switch strings.ToUpper(cell) {
		case "ID":
			c.id = i
		case "CATEGORY":
			c.cat = i
		case "VERSION":
			c.ver = i
		case "NAME":
			c.name = i
		case "DESCRIPTION":
			c.desc = i
		case "HINT":
			c.hint = i
		case "FILE":
			c.file = i
		}
	}
	if c.id < 0 || c.file < 0 {
		return nil
	}
	return c
}

// safeCell returns cells[idx] when idx is in range, otherwise "".
func safeCell(cells []string, idx int) string {
	if idx < 0 || idx >= len(cells) {
		return ""
	}
	return cells[idx]
}

// stripBackticks removes surrounding backtick characters from s.
func stripBackticks(s string) string {
	return strings.Trim(s, "`")
}
