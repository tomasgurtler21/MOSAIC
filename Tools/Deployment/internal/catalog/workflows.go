package catalog

import (
	"bytes"
	"os"
	"path/filepath"
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

// loadWorkflows reads Workflows/Index.md, scans the Workflows/ directory, reconciles
// the two sets, parses workflow frontmatter, and builds category groups.
func (c *catalogImpl) loadWorkflows(root string) []Issue {
	wfRoot := filepath.Join(root, "Workflows")
	indexPath := filepath.Join(wfRoot, "Index.md")

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		// Missing index is not a hard error — just no workflows.
		return nil
	}

	rows, categoryOrder := parseWorkflowIndex(indexData)

	// Build a set of file-relative paths (relative to Workflows/) expected by the index.
	expected := make(map[string]indexRow, len(rows)) // key: relative path
	for _, row := range rows {
		expected[row.File] = row
	}

	// Scan Workflows/ subdirectories for .md files, excluding _ prefixed files.
	diskFiles := scanWorkflowDiskFiles(wfRoot)

	// Compute file-orphans: on disk but not in index.
	var issues []Issue
	for relPath, absPath := range diskFiles {
		if _, ok := expected[relPath]; !ok {
			issues = append(issues, Issue{
				Severity: docformat.SeverityWarning,
				Code:     "file-orphan",
				Subject:  relPath,
				Message:  "workflow file exists on disk but is not listed in Workflows/Index.md",
				Path:     absPath,
			})
		}
	}

	// Process index rows in declaration order.
	// Track per-category workflows for WorkflowCategories.
	wfByCategory := make(map[string][]domain.Workflow)

	for _, row := range rows {
		absFilePath := filepath.Join(wfRoot, filepath.FromSlash(row.File))

		if _, exists := diskFiles[row.File]; !exists {
			// Index entry with no file on disk.
			issues = append(issues, Issue{
				Severity: docformat.SeverityWarning,
				Code:     "index-orphan",
				Subject:  row.ID,
				Message:  "workflow listed in index has no corresponding file on disk",
				Path:     absFilePath,
			})
			continue
		}

		wf, err := parseWorkflowFile(absFilePath, row)
		if err != nil {
			continue
		}

		c.workflows = append(c.workflows, wf)
		c.wfIdx[wf.ID] = wf
		c.sourcePaths[absFilePath] = true
		wfByCategory[wf.Category] = append(wfByCategory[wf.Category], wf)
	}

	// Build WorkflowCategories in index order.
	for _, catName := range categoryOrder {
		if wfs, ok := wfByCategory[catName]; ok {
			c.categories = append(c.categories, domain.WorkflowCategory{
				Name:      catName,
				Workflows: wfs,
			})
		}
	}

	return issues
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

// parseWorkflowFile reads a workflow .md file and extracts metadata from its frontmatter,
// supplemented by the index row for fields not in the frontmatter.
func parseWorkflowFile(path string, row indexRow) (domain.Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Workflow{}, err
	}
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.Workflow{}, err
	}
	fm := doc.Frontmatter()

	wf := domain.Workflow{
		ID:         row.ID,
		Category:   row.Category,
		SourcePath: path,
		// Seed from index row (may be overridden by frontmatter below).
		Version:     row.Version,
		Name:        row.Name,
		Description: row.Description,
		Hint:        row.Hint,
	}

	// Prefer frontmatter values when present.
	if v, ok := fm.Get("id"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.ID = v.Scalar
	}
	if v, ok := fm.Get("version"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Version = v.Scalar
	}
	if v, ok := fm.Get("name"); ok && v.Kind == domain.KindScalar && v.Scalar != "" {
		wf.Name = v.Scalar
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
