package catalog_test

// Tests for the workflow catalog (T7.6) and byte-identical section extraction (T7.7).
//
// Workflows are stored in per-category subfolders under Catalog/Workflows/. The catalog must:
//
// T7.6:
//   - Load every eligible .md file found on disk under Catalog/Workflows/ subdirectories,
//     excluding underscore-prefixed files (_Template.md, _Legacy-Appendices.md)
//   - Expose per-workflow frontmatter fields: id, name, description, hint, version,
//     category, and referenced_agents
//   - Group workflows by category folder for the browse UI (WorkflowCategories)
//
// T7.7:
//   - WorkflowSection extracts the <Workflow type="core" name="{id}"> block including its
//     boundary tags, byte-identically from the source file.
//
// Classification record (every loadRealCatalog-based assertion in this file):
//   - Content-coupled, rewritten to a structural invariant: the known-workflow-ID
//     allowlist (was TestWorkflows_KnownIDs_AllPresent) and both known-category
//     allowlists (was TestWorkflowCategories_AllSixKnownCategoriesPresent and
//     TestWorkflowCategories_AllKnownCategoriesPresent, near-duplicates of each other) are
//     replaced by TestWorkflowCategories_EveryOnDiskCategoryYieldsAtLeastOneWorkflow, which
//     walks the real Workflows/ directory tree and asserts every category folder with an
//     eligible file produces a matching WorkflowCategories() entry, without naming any
//     workflow or category. Individual-workflow-loss detection within an existing category
//     is deliberately traded away by this move — recorded here, not discovered later.
//   - Content-coupled, removed (guaranteed structurally by the loader): the Build-has-5
//     count (was TestWorkflowCategories_Build_FiveWorkflows). WorkflowCategories() can
//     never contain an empty category — categories are only appended when at least one
//     workflow was found for them — so an exact-count assertion added detection power only
//     over content, not over behaviour.
//   - Content-coupled, generalised to a structural invariant: the quick-fix-in-Build
//     spot-check (was TestWorkflowCategories_QuickFix_InBuildCategory) is replaced by
//     TestWorkflowCategories_EveryWorkflowAppearsInItsCategory, which asserts the same
//     membership property for every workflow instead of one named one.
//   - Content-coupled, moved onto a synthetic fixture: the quick-fix field and
//     referenced-agent spot-checks (was TestWorkflow_QuickFix_Fields and
//     TestWorkflow_QuickFix_ReferencedAgents) now build their own workflow file and index
//     row via writeWorkflowIndex/writeWorkflowFile instead of reading quick-fix from the
//     real repository. All original assertions are preserved.
//   - Behaviour-under-test, moved onto a synthetic fixture: the byte-identical section
//     extraction tests (were TestWorkflowSection_QuickFix_*) exist to prove
//     WorkflowSection's extraction logic, not to confirm quick-fix's presence or content.
//     They now build their own workflow file with a section block via
//     makeWorkflowWithSectionFixture. All original assertions are preserved, plus one new
//     assertion that outer maintainer prose is excluded from the extracted section.
//   - The underscore-prefix exclusion test (TestWorkflows_UnderscorePrefixed_Excluded) is a
//     loader rule, not content coupling, and needed no change.
//   - Everything else (non-empty-field checks, lookup-by-key, and the missing-section
//     fixture-backed test) asserts a property that holds over whatever the catalog
//     returns, or already used a synthetic/testdata fixture, and needed no change.
//   - Index/disk-orphan detection tests (TestIssues_IndexOrphan_*, TestIssues_FileOrphan_*,
//     TestIssues_RealRepo_NoIndexOrFileOrphans) were retired: the new loader emits neither
//     code from the normal load path; both codes are now the sole responsibility of the
//     standalone index-check command.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Synthetic workflow fixture helpers
// ---------------------------------------------------------------------------

// writeWorkflowIndex writes a minimal Catalog/Workflows/Index.md table listing the given rows.
// Each row is {id, category, version, name, description, hint, file}.
// Workflows are loaded from the catalogue root (Catalog/Workflows/), so the index is written
// there rather than at the top-level Workflows/ path.
func writeWorkflowIndex(t *testing.T, root string, rows [][7]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("# Workflows Index\n\n")
	sb.WriteString("| ID | Category | Version | Name | Description | Hint | File |\n")
	sb.WriteString("|----|----------|---------|------|--------------|------|------|\n")
	for _, r := range rows {
		sb.WriteString("| " + r[0] + " | " + r[1] + " | " + r[2] + " | " + r[3] + " | " + r[4] + " | " + r[5] + " | `" + r[6] + "` |\n")
	}
	mustMkdir(t, root, "Catalog", "Workflows")
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", "Index.md"), []byte(sb.String()))
}

// writeWorkflowFile writes a minimal workflow markdown file with frontmatter at
// <root>/Catalog/Workflows/<category>/<fileName>.
func writeWorkflowFile(t *testing.T, root, category, fileName, id string, referencedAgents []string) {
	t.Helper()
	mustMkdir(t, root, "Catalog", "Workflows", category)
	fm := "---\nid: " + id + "\nversion: \"1.0.0\"\nname: Sample Workflow\ndescription: Sample workflow for tests.\nhint: A quick hint.\n"
	if len(referencedAgents) > 0 {
		fm += "referenced_agents:\n"
		for _, a := range referencedAgents {
			fm += "  - " + a + "\n"
		}
	}
	fm += "---\nContent.\n"
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", category, fileName), []byte(fm))
}

// makeWorkflowWithSectionFixture builds a synthetic MOSAIC root containing one workflow,
// "sample-workflow", whose source file carries maintainer prose before and after its
// <Workflow type="core" name="sample-workflow"> block, and loads the catalog from it. It returns
// the loaded catalog and the raw source bytes, for byte-identity comparison.
func makeWorkflowWithSectionFixture(t *testing.T) (catalog.Catalog, []byte) {
	t.Helper()
	root := makeTempMosaicRoot(t)
	// No Index.md is written: the disk-driven loader finds the workflow file directly.
	content := []byte("---\nid: sample-workflow\nversion: \"1.0.0\"\nname: Sample Workflow\ndescription: Sample.\nhint: Hint.\n---\n\n" +
		"OUTER_MAINTAINER_TEXT: prose that must never appear in the extracted section.\n\n" +
		"<Workflow type=\"core\" name=\"sample-workflow\">\nThis is the workflow body.\n</Workflow>\n\n" +
		"Trailing prose.\n")
	mustMkdir(t, root, "Catalog", "Workflows", "SampleCategory")
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", "SampleCategory", "sample-workflow.md"), content)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return cat, content
}

// ---------------------------------------------------------------------------
// T7.6 — Enumeration
// ---------------------------------------------------------------------------

// TestWorkflowCategories_EveryOnDiskCategoryYieldsAtLeastOneWorkflow verifies that every
// subdirectory of the real repository's Workflows/ folder that contains at least one
// eligible (non-underscore-prefixed) .md file produces a matching category in
// WorkflowCategories(), and that WorkflowCategories() names no category absent from disk.
// This is a structural invariant: it detects a whole category silently disappearing from
// the catalog without naming any specific workflow or category, so it survives content
// moves like relocating a whole category out of the scanned tree.
func TestWorkflowCategories_EveryOnDiskCategoryYieldsAtLeastOneWorkflow(t *testing.T) {
	cat := loadRealCatalog(t)
	wfRoot := filepath.Join(cat.CatalogRoot(), "Workflows")

	entries, err := os.ReadDir(wfRoot)
	if err != nil {
		t.Fatalf("os.ReadDir(%q): %v", wfRoot, err)
	}
	onDiskCategories := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		catDir := filepath.Join(wfRoot, e.Name())
		fileEntries, err := os.ReadDir(catDir)
		if err != nil {
			continue
		}
		for _, fe := range fileEntries {
			name := fe.Name()
			if fe.IsDir() || strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".md") {
				continue
			}
			onDiskCategories[e.Name()] = true
			break
		}
	}
	if len(onDiskCategories) == 0 {
		t.Fatal("no eligible workflow files found on disk under Workflows/; cannot verify the invariant")
	}

	categoriesInCatalog := make(map[string]bool)
	for _, c := range cat.WorkflowCategories() {
		categoriesInCatalog[c.Name] = true
	}

	for category := range onDiskCategories {
		if !categoriesInCatalog[category] {
			t.Errorf("workflow category directory %q has eligible files on disk but WorkflowCategories() has no matching entry", category)
		}
	}
	for category := range categoriesInCatalog {
		if !onDiskCategories[category] {
			t.Errorf("WorkflowCategories() contains category %q with no matching on-disk directory under %q", category, wfRoot)
		}
	}
}

// TestWorkflows_UnderscorePrefixed_Excluded verifies that underscore-prefixed files are
// not returned by Workflows(). _Template.md and _Legacy-Appendices.md are utility files,
// not actual workflow documents.
func TestWorkflows_UnderscorePrefixed_Excluded(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		base := filepath.Base(w.SourcePath)
		if len(base) > 0 && base[0] == '_' {
			t.Errorf("underscore-prefixed file %q appeared in Workflows(); it must be excluded", base)
		}
	}
}

// TestWorkflows_AllHaveNonEmptyID verifies that every workflow has a non-empty id.
func TestWorkflows_AllHaveNonEmptyID(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.ID == "" {
			t.Errorf("workflow at SourcePath %q has empty ID", w.SourcePath)
		}
	}
}

// TestWorkflows_AllHaveName verifies that every workflow has a non-empty name.
func TestWorkflows_AllHaveName(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.Name == "" {
			t.Errorf("workflow %q has empty Name", w.ID)
		}
	}
}

// TestWorkflows_AllHaveDescription verifies that every workflow has a non-empty description.
func TestWorkflows_AllHaveDescription(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.Description == "" {
			t.Errorf("workflow %q has empty Description", w.ID)
		}
	}
}

// TestWorkflows_AllHaveHint verifies that every workflow has a non-empty hint.
// The hint is shown in the workflow selection screen to help users pick quickly.
func TestWorkflows_AllHaveHint(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.Hint == "" {
			t.Errorf("workflow %q has empty Hint", w.ID)
		}
	}
}

// TestWorkflows_AllHaveVersion verifies that every workflow has a non-empty version.
func TestWorkflows_AllHaveVersion(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.Version == "" {
			t.Errorf("workflow %q has empty Version", w.ID)
		}
	}
}

// TestWorkflows_AllHaveCategory verifies that every workflow has a non-empty category
// (the source folder name, e.g. "Build").
func TestWorkflows_AllHaveCategory(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if w.Category == "" {
			t.Errorf("workflow %q has empty Category", w.ID)
		}
	}
}

// TestWorkflows_AllHaveAbsoluteSourcePath verifies that each workflow's SourcePath is
// absolute and the file exists on disk.
func TestWorkflows_AllHaveAbsoluteSourcePath(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		if !filepath.IsAbs(w.SourcePath) {
			t.Errorf("workflow %q SourcePath is not absolute: %q", w.ID, w.SourcePath)
			continue
		}
		if _, err := os.Stat(w.SourcePath); os.IsNotExist(err) {
			t.Errorf("workflow %q SourcePath %q does not exist on disk", w.ID, w.SourcePath)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6 — Workflow field and referenced-agent parsing (synthetic fixture)
// ---------------------------------------------------------------------------

// TestWorkflow_Fields_PopulatedFromFrontmatter verifies that a workflow's fields are
// populated from its own frontmatter on a synthetic fixture, rather than reading a
// specific workflow out of the real repository. No Index.md is written; the loader
// discovers the file from disk directly.
func TestWorkflow_Fields_PopulatedFromFrontmatter(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeWorkflowFile(t, root, "SampleCategory", "sample-workflow.md", "sample-workflow", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	w, ok := cat.Workflow("sample-workflow")
	if !ok {
		t.Fatal("Workflow(\"sample-workflow\"): returned not-found; expected this workflow to be present")
	}

	if w.ID != "sample-workflow" {
		t.Errorf("ID = %q, want %q", w.ID, "sample-workflow")
	}
	if w.Name == "" {
		t.Error("Name is empty")
	}
	if w.Category != "SampleCategory" {
		t.Errorf("Category = %q, want %q", w.Category, "SampleCategory")
	}
	if w.Description == "" {
		t.Error("Description is empty")
	}
	if w.Hint == "" {
		t.Error("Hint is empty")
	}
	if w.Version == "" {
		t.Error("Version is empty")
	}
}

// TestWorkflow_ReferencedAgents_PopulatedFromFrontmatter verifies that a workflow's
// ReferencedAgents list is populated from its referenced_agents frontmatter, on a
// synthetic fixture rather than a specific real workflow. No Index.md is written;
// the loader discovers the file from disk directly.
func TestWorkflow_ReferencedAgents_PopulatedFromFrontmatter(t *testing.T) {
	root := makeTempMosaicRoot(t)
	wantAgents := []string{"agent-one", "agent-two", "agent-three"}
	writeWorkflowFile(t, root, "SampleCategory", "sample-workflow.md", "sample-workflow", wantAgents)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	w, ok := cat.Workflow("sample-workflow")
	if !ok {
		t.Fatal("Workflow(\"sample-workflow\"): not found")
	}

	if len(w.ReferencedAgents) == 0 {
		t.Fatal("ReferencedAgents is empty; expected at least one agent slug")
	}
	referenced := make(map[string]bool, len(w.ReferencedAgents))
	for _, slug := range w.ReferencedAgents {
		referenced[slug] = true
	}
	for _, want := range wantAgents {
		if !referenced[want] {
			t.Errorf("ReferencedAgents missing expected agent %q (got: %v)", want, w.ReferencedAgents)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6 — WorkflowCategories
// ---------------------------------------------------------------------------

// TestWorkflowCategories_TotalMatchesWorkflowsCount verifies that the total number of
// workflows across all categories equals the count returned by Workflows().
func TestWorkflowCategories_TotalMatchesWorkflowsCount(t *testing.T) {
	cat := loadRealCatalog(t)
	total := 0
	for _, c := range cat.WorkflowCategories() {
		total += len(c.Workflows)
	}
	direct := len(cat.Workflows())
	if total != direct {
		t.Errorf("total workflows in WorkflowCategories() = %d, want %d (from Workflows())", total, direct)
	}
}

// TestWorkflowCategories_EveryWorkflowAppearsInItsCategory verifies that every workflow
// returned by Workflows() appears in the Workflows list of its own Category within
// WorkflowCategories(). This asserts the category-membership property for every workflow
// rather than spot-checking one named workflow against one named category.
func TestWorkflowCategories_EveryWorkflowAppearsInItsCategory(t *testing.T) {
	cat := loadRealCatalog(t)
	idsByCategory := make(map[string]map[string]bool)
	for _, c := range cat.WorkflowCategories() {
		ids := make(map[string]bool, len(c.Workflows))
		for _, w := range c.Workflows {
			ids[w.ID] = true
		}
		idsByCategory[c.Name] = ids
	}
	for _, w := range cat.Workflows() {
		if !idsByCategory[w.Category][w.ID] {
			t.Errorf("workflow %q (Category %q) does not appear in WorkflowCategories()[%q].Workflows",
				w.ID, w.Category, w.Category)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6 — Lookup
// ---------------------------------------------------------------------------

// TestWorkflowLookup_AllWorkflows_FoundByID verifies that every workflow returned by
// Workflows() can be looked up by id via Workflow(id).
func TestWorkflowLookup_AllWorkflows_FoundByID(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		found, ok := cat.Workflow(w.ID)
		if !ok {
			t.Errorf("Workflow(%q): returned not-found, but id was returned by Workflows()", w.ID)
			continue
		}
		if found.ID != w.ID {
			t.Errorf("Workflow(%q).ID = %q, want %q", w.ID, found.ID, w.ID)
		}
	}
}

// TestWorkflowLookup_UnknownID_ReturnsFalse verifies that Workflow(id) returns false
// for an unknown id.
func TestWorkflowLookup_UnknownID_ReturnsFalse(t *testing.T) {
	cat := loadRealCatalog(t)
	_, ok := cat.Workflow("this-workflow-does-not-exist")
	if ok {
		t.Error("Workflow(\"this-workflow-does-not-exist\"): returned true, want false")
	}
}

// ---------------------------------------------------------------------------
// T7.7 — Byte-identical workflow section extraction
// ---------------------------------------------------------------------------

// TestWorkflowSection_ByteIdentical verifies that WorkflowSection returns the
// <Workflow type="core" name="{id}"> block byte-identically to what is stored in the source file.
// No normalisation of any kind is allowed.
func TestWorkflowSection_ByteIdentical(t *testing.T) {
	cat, raw := makeWorkflowWithSectionFixture(t)

	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}
	if len(section) == 0 {
		t.Fatal("WorkflowSection(\"sample-workflow\"): returned empty bytes")
	}

	// The returned bytes must be a contiguous sub-slice of the raw source file,
	// starting with the opening boundary tag and ending with the closing boundary tag.
	if !bytes.Contains(raw, section) {
		t.Error("WorkflowSection(\"sample-workflow\") returned bytes that are not a byte-identical " +
			"contiguous substring of the raw source file")
	}
}

// TestWorkflowSection_ExcludesOuterProse verifies that the extracted section does not
// contain maintainer prose that sits outside the section's boundary tags in the source
// file.
func TestWorkflowSection_ExcludesOuterProse(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)
	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}
	if bytes.Contains(section, []byte("OUTER_MAINTAINER_TEXT")) {
		t.Error("WorkflowSection returned bytes containing outer maintainer prose; expected only the bounded section")
	}
}

// TestWorkflowSection_IncludesOpeningBoundary verifies that the extracted section
// includes the <Workflow type="core" name="{id}"> opening boundary tag.
func TestWorkflowSection_IncludesOpeningBoundary(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)
	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	openTag := []byte(`<Workflow type="core" name="sample-workflow">`)
	if !bytes.Contains(section, openTag) {
		t.Errorf("WorkflowSection does not contain the opening boundary tag %q", openTag)
	}
}

// TestWorkflowSection_IncludesClosingBoundary verifies that the extracted section
// includes the </Workflow> closing boundary tag.
func TestWorkflowSection_IncludesClosingBoundary(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)
	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	closeTag := []byte("</Workflow>")
	if !bytes.Contains(section, closeTag) {
		t.Errorf("WorkflowSection does not contain the closing boundary tag %q", closeTag)
	}
}

// TestWorkflowSection_StartsWithOpeningBoundary verifies that the first bytes of the
// extracted section ARE the opening boundary tag. No content must appear before it.
func TestWorkflowSection_StartsWithOpeningBoundary(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)
	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	openTag := `<Workflow type="core" name="sample-workflow">`
	if !bytes.HasPrefix(section, []byte(openTag)) {
		first := string(section)
		if len(first) > 60 {
			first = first[:60] + "..."
		}
		t.Errorf("WorkflowSection does not start with the opening boundary tag;\n  got prefix: %q", first)
	}
}

// TestWorkflowSection_EndsWithClosingBoundary verifies that the extracted section ends
// with the closing boundary tag (optionally followed by a single newline).
func TestWorkflowSection_EndsWithClosingBoundary(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)
	section, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	closeTag := "</Workflow>"
	trimmed := bytes.TrimRight(section, "\n")
	if !bytes.HasSuffix(trimmed, []byte(closeTag)) {
		last := string(section)
		if len(last) > 80 {
			last = "..." + last[len(last)-80:]
		}
		t.Errorf("WorkflowSection does not end with the closing boundary tag;\n  got suffix: %q", last)
	}
}

// TestWorkflowSection_AllWorkflows_ReturnNonEmpty verifies that WorkflowSection succeeds
// and returns non-empty bytes for every workflow in the catalog.
func TestWorkflowSection_AllWorkflows_ReturnNonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, w := range cat.Workflows() {
		section, err := cat.WorkflowSection(w.ID)
		if err != nil {
			t.Errorf("WorkflowSection(%q): %v", w.ID, err)
			continue
		}
		if len(section) == 0 {
			t.Errorf("WorkflowSection(%q): returned empty bytes", w.ID)
		}
	}
}

// TestWorkflowSection_UnknownID_ReturnsError verifies that WorkflowSection returns an
// error for a workflow id that does not exist in the catalog.
func TestWorkflowSection_UnknownID_ReturnsError(t *testing.T) {
	cat := loadRealCatalog(t)
	_, err := cat.WorkflowSection("this-workflow-does-not-exist")
	if err == nil {
		t.Error("WorkflowSection(\"this-workflow-does-not-exist\"): expected error, got nil")
	}
}

// TestWorkflowSection_FileWithoutSectionBlock_ReturnsError verifies that WorkflowSection
// returns an error when a workflow is present in the catalog (its file exists on disk)
// but the file does not contain a <Workflow type="core" name="{id}"> block.
//
// This is a realistic failure mode for a malformed workflow file. The contract states that
// section blocks must be byte-identically extractable (AC7.4); a missing block cannot
// satisfy that guarantee, so an error is the only valid response.
//
// The fixture at testdata/catalog/missing-section/ contains "no-section-workflow" which
// has a file on disk with frontmatter but no section boundary tags.
func TestWorkflowSection_FileWithoutSectionBlock_ReturnsError(t *testing.T) {
	cat := loadFixtureCatalog(t, "missing-section")

	_, err := cat.WorkflowSection("no-section-workflow")
	if err == nil {
		t.Error("WorkflowSection(\"no-section-workflow\"): expected error for a file with no section block, got nil")
	}
}
