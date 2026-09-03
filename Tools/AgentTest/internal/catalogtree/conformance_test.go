package catalogtree_test

// Conformance tests for Tools/AgentTest/catalog/ (the test catalogue tree).
//
// These tests run in the TDD RED phase, before the catalogue tree is authored.
// They compile and fail against the absent tree, giving valid RED-phase failures.
//
// Expected paths are derived from the same conventions that
// Tools/Deployment/internal/catalog/catalogpaths/catalogpaths.go uses, without
// importing that package (it lives in the separate mosaic-deploy module). The
// path-builder functions below are a direct translation of the same relative
// segments. Any mismatch in those segments will surface here as a test failure.
//
// Tests:
//   T6.1 — structural conformance: tree exists at the deployment-tool-resolved paths
//   T6.2 — referenced-agent resolution and workflow index listing
//   T6.3 — TestStubs and agents/ stub sets stay in sync (same keys, same format)
//   T6.4 — every catalogue agent declares one of the two tier constants; no third tier

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	"mosaic-agent-test/internal/domain"
)

// ---------------------------------------------------------------------------
// Tier name constants
//
// These are the single source of truth for the test catalogue tier names,
// declared in domain and referenced here via import. No literal is spelled
// in this file; a rename is a one-line change in domain/tiers.go.
// ---------------------------------------------------------------------------

const (
	tierTestSubject = domain.TierTestSubject
	tierTestStub    = domain.TierTestStub
)

// Catalogue structural constants — mirror catalogpaths conventions.
const (
	testStubsCategory    = "TestStubs"     // SubagentCategoryDir category name
	testWorkflowCategory = "AgentTest"     // workflow category subdirectory
	testWorkflowID       = "brownfield-tdd" // workflow file base name
)

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// sourceFileDir returns the absolute directory of this source file.
// It uses runtime.Caller so the result is correct regardless of working
// directory at test-run time.
func sourceFileDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate test source file")
	}
	return filepath.Dir(filename)
}

// agentTestRoot returns the absolute path to Tools/AgentTest/.
// This file lives at Tools/AgentTest/internal/catalogtree/conformance_test.go.
// Two directory levels up: catalogtree/ -> internal/ -> AgentTest/
func agentTestRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(sourceFileDir(t), "..", ".."))
}

// catalogRoot returns the absolute path to Tools/AgentTest/catalog/ — the
// catalogue root, which is what --catalog-folder would be set to.
func catalogRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(agentTestRoot(t), "catalog")
}

// ---------------------------------------------------------------------------
// Path builders (mirroring catalogpaths)
//
// Each function mirrors its counterpart in
// Tools/Deployment/internal/catalog/catalogpaths/catalogpaths.go, using the
// same relative segment names. catalogpaths is a leaf with no logic beyond
// filepath.Join, so the translation is exact.
// ---------------------------------------------------------------------------

// orchestratorFile mirrors catalogpaths.OrchestratorFile(root).
func orchestratorFile(root string) string {
	return filepath.Join(root, "Orchestrator", "orchestrator.md")
}

// subagentCategoryDir mirrors catalogpaths.SubagentCategoryDir(root, category).
func subagentCategoryDir(root, category string) string {
	return filepath.Join(root, "Subagents", category)
}

// subagentFile mirrors catalogpaths.SubagentFile(root, category, key).
func subagentFile(root, category, key string) string {
	return filepath.Join(subagentCategoryDir(root, category), key+".md")
}

// workflowsDir mirrors catalogpaths.WorkflowsDir(root).
func workflowsDir(root string) string {
	return filepath.Join(root, "Workflows")
}

// workflowsIndexFile returns the path to Workflows/Index.md.
func workflowsIndexFile(root string) string {
	return filepath.Join(workflowsDir(root), "Index.md")
}

// workflowFile returns the path to a workflow file under the workflows directory.
func workflowFile(root, category, id string) string {
	return filepath.Join(workflowsDir(root), category, id+".md")
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// parseFrontmatter extracts the YAML frontmatter from a markdown file and
// returns (frontmatter map, body text). Reports a test error and returns
// (nil, body) on any parse failure.
//
// Callers that need a non-nil map should skip further checks when nil is
// returned, because parseFrontmatter already recorded the failure.
func parseFrontmatter(t *testing.T, path string) (map[string]any, string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("%s: ReadFile error: %v", filepath.Base(path), err)
		return nil, ""
	}
	content := string(data)

	const delim = "---"
	firstDelim := strings.Index(content, delim)
	if firstDelim < 0 {
		t.Errorf("%s: no frontmatter delimiter found — file must begin with ---", filepath.Base(path))
		return nil, content
	}
	after := content[firstDelim+len(delim):]
	nl := strings.Index(after, "\n")
	if nl < 0 {
		t.Errorf("%s: malformed frontmatter — no newline after opening ---", filepath.Base(path))
		return nil, content
	}
	yamlStart := after[nl+1:]
	closeMarker := "\n" + delim
	closeIdx := strings.Index(yamlStart, closeMarker)
	if closeIdx < 0 {
		t.Errorf("%s: malformed frontmatter — no closing ---", filepath.Base(path))
		return nil, content
	}
	yamlContent := yamlStart[:closeIdx]
	body := yamlStart[closeIdx+len(closeMarker):]

	var fm map[string]any
	if err := goyaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		t.Errorf("%s: YAML parse error in frontmatter: %v", filepath.Base(path), err)
		return nil, body
	}
	return fm, body
}

// requireStringField asserts that fm[field] is present, is a string, and is
// non-empty. Returns the trimmed value, or the empty string on failure.
func requireStringField(t *testing.T, fm map[string]any, fileBase, field string) string {
	t.Helper()
	v, ok := fm[field]
	if !ok {
		t.Errorf("%s: frontmatter field %q is absent", fileBase, field)
		return ""
	}
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s: frontmatter field %q has type %T, want string", fileBase, field, v)
		return ""
	}
	if strings.TrimSpace(s) == "" {
		t.Errorf("%s: frontmatter field %q is present but empty", fileBase, field)
		return ""
	}
	return s
}

// extractStringSlice reads a []string from a YAML frontmatter field.
// Returns nil when the field is absent. Reports a test error when the field
// exists but is not a list of strings.
func extractStringSlice(t *testing.T, fm map[string]any, fileBase, field string) []string {
	t.Helper()
	v, ok := fm[field]
	if !ok {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		t.Errorf("%s: frontmatter field %q has type %T, want a list", fileBase, field, v)
		return nil
	}
	var result []string
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Errorf("%s: frontmatter field %q: list element has type %T, want string", fileBase, field, item)
			continue
		}
		result = append(result, s)
	}
	return result
}

// mdFileKeys returns the base names (without .md) of every *.md file in dir
// that does not equal excludeFile. Calls t.Fatalf when the directory cannot
// be read. Returns an empty slice (not nil) when the directory is empty.
func mdFileKeys(t *testing.T, dir, excludeFile string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) = %v", dir, err)
		return nil
	}
	var keys []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == excludeFile {
			continue
		}
		keys = append(keys, strings.TrimSuffix(name, ".md"))
	}
	return keys
}

// toStringSet converts a key slice to a presence-map.
func toStringSet(keys []string) map[string]bool {
	s := make(map[string]bool, len(keys))
	for _, k := range keys {
		s[k] = true
	}
	return s
}

// isSeparatorRow reports whether a markdown table row consists only of dashes,
// pipes, and spaces — i.e., it is a header-separator row like |---|---|.
func isSeparatorRow(line string) bool {
	for _, c := range strings.Trim(line, "| ") {
		if c != '-' && c != ' ' {
			return false
		}
	}
	return true
}

// parseMarkdownTableRows parses a markdown document and returns every non-header,
// non-separator table row as a slice of trimmed column strings. Each inner slice
// corresponds to the columns between the leading and trailing pipe characters.
func parseMarkdownTableRows(data string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if isSeparatorRow(line) {
			continue
		}
		parts := strings.Split(line, "|")
		// Split on "|" yields an empty string at index 0 (before the first |)
		// and one more empty string at the end (after the last |). Trim those.
		if len(parts) < 3 {
			continue
		}
		var cols []string
		for _, p := range parts[1 : len(parts)-1] {
			cols = append(cols, strings.TrimSpace(p))
		}
		rows = append(rows, cols)
	}
	return rows
}

// allCatalogAgentFiles collects every agent *.md file in the catalogue tree:
// the orchestrator file and every file under Subagents/TestStubs/. Files or
// directories that do not exist are silently skipped — callers that need the
// list to be non-empty should assert len > 0 themselves.
func allCatalogAgentFiles(root string) []string {
	var paths []string

	orch := orchestratorFile(root)
	if _, err := os.Stat(orch); err == nil {
		paths = append(paths, orch)
	}

	stubsDir := subagentCategoryDir(root, testStubsCategory)
	entries, err := os.ReadDir(stubsDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && strings.HasSuffix(name, ".md") {
				paths = append(paths, filepath.Join(stubsDir, name))
			}
		}
	}

	return paths
}

// ---------------------------------------------------------------------------
// T6.1: structural conformance
// ---------------------------------------------------------------------------

// TestCatalogueTree_StructuralConformance asserts that the test catalogue tree
// exists at the paths the deployment tool's own path builders resolve.
//
// Checked locations:
//   - Orchestrator/orchestrator.md (catalogpaths.OrchestratorFile)
//   - Subagents/TestStubs/ directory (catalogpaths.SubagentCategoryDir)
//     — the category level is mandatory; stubs placed flat under Subagents/
//     are not found by SubagentFile.
//   - Workflows/ directory (catalogpaths.WorkflowsDir)
//   - Workflows/Index.md
//
// Fails until I6.1 and I6.4 are complete.
func TestCatalogueTree_StructuralConformance(t *testing.T) {
	root := catalogRoot(t)

	cases := []struct {
		name    string
		path    string
		wantDir bool
	}{
		{
			name:    "orchestrator_file",
			path:    orchestratorFile(root),
			wantDir: false,
		},
		{
			name:    "TestStubs_category_directory_under_Subagents",
			path:    subagentCategoryDir(root, testStubsCategory),
			wantDir: true,
		},
		{
			name:    "Workflows_directory",
			path:    workflowsDir(root),
			wantDir: true,
		},
		{
			name:    "Workflows_Index.md",
			path:    workflowsIndexFile(root),
			wantDir: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fi, err := os.Stat(tc.path)
			if os.IsNotExist(err) {
				t.Errorf("path does not exist: %s", tc.path)
				return
			}
			if err != nil {
				t.Errorf("os.Stat(%q) error: %v", tc.path, err)
				return
			}
			if tc.wantDir && !fi.IsDir() {
				t.Errorf("expected a directory, got a file: %s", tc.path)
			}
			if !tc.wantDir && fi.IsDir() {
				t.Errorf("expected a file, got a directory: %s", tc.path)
			}
		})
	}

}

// ---------------------------------------------------------------------------
// T6.2: referenced-agent resolution and workflow index listing
// ---------------------------------------------------------------------------

// TestCatalogueTree_ReferencedAgentsResolveAndWorkflowIsIndexed asserts that:
//   - Every agent listed in the test workflow's referenced_agents frontmatter
//     has a matching file under catalog/Subagents/TestStubs/.
//   - The test workflow (brownfield-tdd) is listed in Workflows/Index.md.
//
// The expected agent set is derived from the test workflow's own frontmatter
// rather than hardcoded, so this test stays correct if the workflow's agent
// list changes.
//
// Fails until I6.2, I6.3, and I6.4 are complete.
func TestCatalogueTree_ReferencedAgentsResolveAndWorkflowIsIndexed(t *testing.T) {
	root := catalogRoot(t)
	wfPath := workflowFile(root, testWorkflowCategory, testWorkflowID)

	// The test workflow file must exist.
	if _, err := os.Stat(wfPath); os.IsNotExist(err) {
		t.Fatalf("test workflow does not exist at %s — implement I6.3", wfPath)
	}

	fm, _ := parseFrontmatter(t, wfPath)
	if fm == nil {
		t.Fatal("test workflow: frontmatter is missing or unparseable — subsequent checks skipped")
	}

	referencedAgents := extractStringSlice(t, fm, testWorkflowID+".md", "referenced_agents")
	if len(referencedAgents) == 0 {
		t.Fatal("test workflow: referenced_agents is empty or absent — cannot derive expected agent set")
	}

	// Every referenced agent must resolve to an existing TestStubs file.
	t.Run("referenced_agents_have_TestStubs_files", func(t *testing.T) {
		for _, key := range referencedAgents {
			key := key
			t.Run(key, func(t *testing.T) {
				stubPath := subagentFile(root, testStubsCategory, key)
				if _, err := os.Stat(stubPath); os.IsNotExist(err) {
					t.Errorf("Subagents/TestStubs/%s.md: missing for workflow-referenced agent %q", key, key)
				} else if err != nil {
					t.Errorf("os.Stat(%q) error: %v", stubPath, err)
				}
			})
		}
	})

	// The test workflow must appear in Workflows/Index.md using the 8-column
	// table format (AC6.5). Each column must match the workflow's own frontmatter
	// so that the index and the file cannot silently drift apart.
	//
	// Expected columns (0-based): ID | Category | Version | Name | Description |
	//   Hint | Author | File
	// The File column must be backtick-quoted and relative to Workflows/.
	t.Run("brownfield_tdd_listed_in_index", func(t *testing.T) {
		indexPath := workflowsIndexFile(root)
		indexData, err := os.ReadFile(indexPath)
		if os.IsNotExist(err) {
			t.Fatalf("Workflows/Index.md does not exist — implement I6.4")
		}
		if err != nil {
			t.Fatalf("ReadFile(%q) error: %v", indexPath, err)
		}

		const (
			colID          = 0
			colCategory    = 1
			colVersion     = 2
			colName        = 3
			colDescription = 4
			colHint        = 5
			colAuthor      = 6
			colFile        = 7
			colCount       = 8
		)

		rows := parseMarkdownTableRows(string(indexData))
		var workflowRow []string
		for _, row := range rows {
			if len(row) > colID && row[colID] == testWorkflowID {
				workflowRow = row
				break
			}
		}
		if workflowRow == nil {
			t.Fatalf("Workflows/Index.md: no table row found with ID %q — the workflow must appear in an 8-column markdown table", testWorkflowID)
		}

		if len(workflowRow) != colCount {
			t.Errorf("Workflows/Index.md: row for %q has %d columns, want %d (ID | Category | Version | Name | Description | Hint | Author | File)", testWorkflowID, len(workflowRow), colCount)
		}

		// Helper: assert a column matches an expected value.
		checkCol := func(col int, colLabel, want string) {
			t.Helper()
			if col >= len(workflowRow) {
				return // column count mismatch already reported above
			}
			got := strings.TrimSpace(workflowRow[col])
			if got != want {
				t.Errorf("Workflows/Index.md row[%s] = %q, want %q (derived from workflow frontmatter)", colLabel, got, want)
			}
		}

		// Column 0: ID — always the workflow's own id field.
		checkCol(colID, "ID", testWorkflowID)

		// Column 1: Category — always the workflow category subdirectory.
		checkCol(colCategory, "Category", testWorkflowCategory)

		// Columns 2–6: derived from the workflow file's frontmatter.
		// fmt.Sprintf("%v", v) gives the Go default representation; YAML
		// integers and strings both round-trip cleanly for the values used here.
		checkCol(colVersion, "Version", fmt.Sprintf("%v", fm["version"]))
		checkCol(colName, "Name", fmt.Sprintf("%v", fm["name"]))
		checkCol(colDescription, "Description", fmt.Sprintf("%v", fm["description"]))
		checkCol(colHint, "Hint", fmt.Sprintf("%v", fm["hint"]))
		checkCol(colAuthor, "Author", fmt.Sprintf("%v", fm["author"]))

		// Column 7: File — must be backtick-quoted and relative to Workflows/.
		expectedFile := fmt.Sprintf("`%s/%s.md`", testWorkflowCategory, testWorkflowID)
		if colFile < len(workflowRow) {
			got := strings.TrimSpace(workflowRow[colFile])
			if got != expectedFile {
				t.Errorf("Workflows/Index.md row[File] = %q, want %q (backtick-quoted, relative to Workflows/)", got, expectedFile)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// T6.4: tier constants conformance
// ---------------------------------------------------------------------------

// TestCatalogueTree_TierConformance asserts that every agent file in the test
// catalogue tree declares a recommended_tier equal to one of the two permitted
// values, that the orchestrator declares the subject tier, every stub declares
// the stub tier, and no third tier value appears anywhere in the tree.
//
// The tier values used here (tierTestSubject, tierTestStub) are local constants
// that mirror what domain.TierTestSubject and domain.TierTestStub will hold
// once I6.0 is implemented. References here should be replaced with the domain
// constants after I6.0 lands, so that no package other than domain spells
// either literal.
//
// Fails until I6.1 and I6.2 are complete.
func TestCatalogueTree_TierConformance(t *testing.T) {
	root := catalogRoot(t)

	// The orchestrator file must declare the subject tier.
	t.Run("orchestrator_declares_subject_tier", func(t *testing.T) {
		path := orchestratorFile(root)
		fm, _ := parseFrontmatter(t, path)
		if fm == nil {
			return
		}
		tier, _ := fm["recommended_tier"].(string)
		if tier == "" {
			t.Errorf("orchestrator.md: frontmatter field \"recommended_tier\" is absent or empty, want %q", tierTestSubject)
			return
		}
		if tier != tierTestSubject {
			t.Errorf("orchestrator.md: recommended_tier = %q, want %q (domain.TierTestSubject)", tier, tierTestSubject)
		}
	})

	// Every stub in TestStubs must declare the stub tier.
	t.Run("TestStubs_declare_stub_tier", func(t *testing.T) {
		stubsDir := subagentCategoryDir(root, testStubsCategory)
		entries, err := os.ReadDir(stubsDir)
		if err != nil {
			t.Fatalf("ReadDir(%q) = %v — TestStubs directory does not exist yet", stubsDir, err)
		}

		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".md") {
				continue
			}
			base := strings.TrimSuffix(name, ".md")
			path := filepath.Join(stubsDir, name)

			t.Run(base, func(t *testing.T) {
				fm, _ := parseFrontmatter(t, path)
				if fm == nil {
					return
				}
				tier, _ := fm["recommended_tier"].(string)
				if tier == "" {
					t.Errorf("%s: frontmatter field \"recommended_tier\" is absent or empty, want %q", base, tierTestStub)
					return
				}
				if tier != tierTestStub {
					t.Errorf("%s: recommended_tier = %q, want %q (domain.TierTestStub)", base, tier, tierTestStub)
				}
			})
		}
	})

	// No third tier value appears anywhere in the catalogue tree.
	// The tree is a closed set of exactly two tiers so that the caller's
	// two-entry tier-model map is always total.
	t.Run("no_third_tier_in_tree", func(t *testing.T) {
		agentFiles := allCatalogAgentFiles(root)
		if len(agentFiles) == 0 {
			t.Fatal("no agent files found in catalogue tree — tree does not exist yet (implement I6.1, I6.2)")
		}

		admissibleTiers := map[string]bool{
			tierTestSubject: true,
			tierTestStub:    true,
		}

		for _, path := range agentFiles {
			base := strings.TrimSuffix(filepath.Base(path), ".md")
			fm, _ := parseFrontmatter(t, path)
			if fm == nil {
				continue
			}

			tierVal, ok := fm["recommended_tier"]
			if !ok {
				t.Errorf("%s: frontmatter field \"recommended_tier\" is absent — every catalogue agent must declare one", base)
				continue
			}
			tier, ok := tierVal.(string)
			if !ok {
				t.Errorf("%s: frontmatter field \"recommended_tier\" has type %T, want string", base, tierVal)
				continue
			}
			if !admissibleTiers[tier] {
				t.Errorf(
					"%s: recommended_tier = %q — only %q and %q are permitted in the test catalogue; a third tier produces a silent model-placeholder failure",
					base, tier, tierTestSubject, tierTestStub,
				)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// AC6.7: single-workflow constraint
// ---------------------------------------------------------------------------

// TestCatalogueTree_SingleWorkflowConstraint asserts that the Workflows/ tree
// contains exactly one workflow file — Workflows/AgentTest/brownfield-tdd.md —
// and no other (AC6.7). Adding a second workflow during I6.3/I6.4 would
// violate the initial scope boundary and this test surfaces it immediately.
//
// Fails until I6.3 creates the single expected workflow file, and also fails
// if any additional workflow file is ever added under Workflows/.
func TestCatalogueTree_SingleWorkflowConstraint(t *testing.T) {
	root := catalogRoot(t)
	wfDir := workflowsDir(root)

	if _, err := os.Stat(wfDir); os.IsNotExist(err) {
		t.Fatalf("Workflows/ directory does not exist at %s — implement I6.3/I6.4", wfDir)
	}

	// Collect every *.md file under Workflows/ that is not Index.md.
	var found []string
	walkErr := filepath.WalkDir(wfDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".md") || name == "Index.md" {
			return nil
		}
		rel, relErr := filepath.Rel(wfDir, path)
		if relErr != nil {
			return relErr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("WalkDir(%q) error: %v", wfDir, walkErr)
	}

	expectedFile := testWorkflowCategory + "/" + testWorkflowID + ".md"

	if len(found) == 0 {
		t.Errorf("Workflows/ contains no workflow files — expected exactly %q (implement I6.3)", expectedFile)
		return
	}
	if len(found) != 1 || found[0] != expectedFile {
		t.Errorf(
			"Workflows/ contains workflow files %v — expected exactly [%q]; no workflow other than brownfield-tdd belongs in the initial test catalogue",
			found, expectedFile,
		)
	}
}

// ---------------------------------------------------------------------------
// AC6.9: tier literal single-spelling enforcement
// ---------------------------------------------------------------------------

// TestCatalogueTree_TierLiteralSingleSpelling walks every *.go file under
// Tools/AgentTest/ and asserts that the tier name strings (domain.TierTestSubject
// and domain.TierTestStub) do not appear as literals in any file outside internal/domain/
// (AC6.9). These values must be referenced exclusively through
// domain.TierTestSubject and domain.TierTestStub so that a rename is a
// one-line change rather than a repository-wide hunt.
//
// During the RED phase this test fails because the local tier constants
// (tierTestSubject, tierTestStub) in this file still spell the literals. Once
// I6.0 is implemented and those constants are replaced with domain imports, the
// literals will be absent from all non-domain files and this test will pass.
//
// The prohibited strings are built by concatenation so that this source file
// itself does not contain them as contiguous text (and thus does not
// self-report as a violation once I6.0 is done and the local constants are
// removed).
func TestCatalogueTree_TierLiteralSingleSpelling(t *testing.T) {
	agentRoot := agentTestRoot(t)
	domainDir := filepath.Join(agentRoot, "internal", "domain")

	// Build prohibited patterns without spelling them literally here.
	// After I6.0, this file switches to domain imports and these runtime
	// values are the only mention of the text in non-domain code.
	prohibitedSubject := "TEST-" + "SUBJECT"
	prohibitedStub := "TEST-" + "STUB"

	absDomainDir, err := filepath.Abs(domainDir)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error: %v", domainDir, err)
	}

	var violations []string
	walkErr := filepath.WalkDir(agentRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip internal/domain — that is the one permitted spelling location.
			absPath, absErr := filepath.Abs(path)
			if absErr == nil && absPath == absDomainDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("ReadFile(%q) error: %v", path, readErr)
			return nil
		}
		content := string(data)
		if strings.Contains(content, prohibitedSubject) || strings.Contains(content, prohibitedStub) {
			rel, _ := filepath.Rel(agentRoot, path)
			violations = append(violations, filepath.ToSlash(rel))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("WalkDir(%q) error: %v", agentRoot, walkErr)
	}

	if len(violations) > 0 {
		t.Errorf(
			"tier name literals found in Go files outside internal/domain — use domain.TierTestSubject / domain.TierTestStub instead:\n  %s\n\n"+
				"After I6.0 is implemented, replace the local tier constants in the conformance test with domain imports.",
			strings.Join(violations, "\n  "),
		)
	}
}

// ---------------------------------------------------------------------------
// T6.2: stub definitions must not contain the protocol region
// ---------------------------------------------------------------------------

// TestStubDefinitions_DoNotContainProtocolRegion asserts that every stub
// definition file under Tools/AgentTest/catalog/Subagents/TestStubs/ carries no
// <CommunicationProtocol type="managed"> region.
//
// A deployed stub that receives the full communication-protocol region learns to
// reason about task structure and refuses or rewrites the substituted reply
// instead of echoing it faithfully. The region must therefore be absent from the
// source file; the deployment tool fills regions that exist and skips files where
// no region is present, so removal here is enough.
//
// This test reads only files under Tools/AgentTest/ and never touches the live
// Catalog/ tree, which changes as ordinary project evolution and would break the
// test on every such change.
//
// TDD RED: this test currently fails because every stub file carries an empty
// <CommunicationProtocol type="managed"> region. It passes once I6.2 removes
// that region from every stub definition.
func TestStubDefinitions_DoNotContainProtocolRegion(t *testing.T) {
	stubsDir := filepath.Join(agentTestRoot(t), "catalog", "Subagents", "TestStubs")
	entries, err := os.ReadDir(stubsDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) = %v — catalog/Subagents/TestStubs/ must exist", stubsDir, err)
	}

	const protocolRegionTag = `<CommunicationProtocol type="managed">`

	found := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(stubsDir, name)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Errorf("%s: ReadFile error: %v", name, readErr)
			continue
		}
		found = true
		if strings.Contains(string(data), protocolRegionTag) {
			t.Errorf(
				"%s: contains a <CommunicationProtocol type=\"managed\"> region — "+
					"test stubs must not receive the protocol block; the deployment tool "+
					"fills regions that exist, so the region must be absent from the source file",
				name,
			)
		}
	}

	if !found {
		t.Error("no stub definition files found in catalog/Subagents/TestStubs/ — the directory must contain at least one *.md file")
	}
}
