package catalog_test

// Tests for the workflow catalog (T7.6) and byte-identical section extraction (T7.7).
//
// Workflows are listed in Workflows/Index.md (the canonical registry) and stored in
// per-category subfolders. The catalog must:
//
// T7.6:
//   - Return exactly the 15 workflows listed in the index, excluding underscore-prefixed
//     files (_Template.md, _Legacy-Appendices.md) which are not workflows
//   - Expose per-workflow frontmatter fields: id, name, description, hint, version,
//     category, and referenced_agents
//   - Group workflows by category folder for the browse UI (WorkflowCategories)
//   - Maintain the ordering of workflows within each category as declared in the index
//   - Detect and report index/disk mismatches:
//       "index-orphan" — workflow in index with no matching file
//       "file-orphan"  — workflow file on disk not listed in the index
//
// T7.7:
//   - WorkflowSection extracts the [[SECTION:Workflow:{id}]] block including its
//     boundary tags, byte-identically from the source file.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// T7.6 — Enumeration
// ---------------------------------------------------------------------------

// TestWorkflows_Count_Matches15 verifies that Workflows() returns exactly 15 workflows,
// matching the total number of entries in Workflows/Index.md.
// Underscore-prefixed files (_Template.md, _Legacy-Appendices.md) must be excluded.
func TestWorkflows_Count_Matches15(t *testing.T) {
	const wantCount = 15
	cat := loadRealCatalog(t)
	workflows := cat.Workflows()
	if len(workflows) != wantCount {
		var ids []string
		for _, w := range workflows {
			ids = append(ids, w.ID)
		}
		t.Errorf("Workflows() returned %d workflows, want %d (got IDs: %v)", len(workflows), wantCount, ids)
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
// T7.6 — Known workflow: quick-fix
// ---------------------------------------------------------------------------

// TestWorkflow_QuickFix_Fields verifies specific field values for the well-known
// quick-fix workflow, which is a stable representative workflow file.
func TestWorkflow_QuickFix_Fields(t *testing.T) {
	cat := loadRealCatalog(t)

	w, ok := cat.Workflow("quick-fix")
	if !ok {
		t.Fatal("Workflow(\"quick-fix\"): returned not-found; expected this workflow to be present")
	}

	if w.ID != "quick-fix" {
		t.Errorf("quick-fix ID = %q, want %q", w.ID, "quick-fix")
	}
	if w.Name == "" {
		t.Error("quick-fix Name is empty")
	}
	if w.Category != "Build" {
		t.Errorf("quick-fix Category = %q, want %q", w.Category, "Build")
	}
	if w.Description == "" {
		t.Error("quick-fix Description is empty")
	}
	if w.Hint == "" {
		t.Error("quick-fix Hint is empty")
	}
	if w.Version == "" {
		t.Error("quick-fix Version is empty")
	}
}

// TestWorkflow_QuickFix_ReferencedAgents verifies that the quick-fix workflow's
// ReferencedAgents list contains the expected agent slugs.
func TestWorkflow_QuickFix_ReferencedAgents(t *testing.T) {
	cat := loadRealCatalog(t)
	w, ok := cat.Workflow("quick-fix")
	if !ok {
		t.Fatal("Workflow(\"quick-fix\"): not found")
	}

	if len(w.ReferencedAgents) == 0 {
		t.Fatal("quick-fix ReferencedAgents is empty; expected at least one agent slug")
	}

	// Verify the known referenced agents are present.
	wantAgents := []string{"planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner"}
	referenced := make(map[string]bool, len(w.ReferencedAgents))
	for _, slug := range w.ReferencedAgents {
		referenced[slug] = true
	}
	for _, want := range wantAgents {
		if !referenced[want] {
			t.Errorf("quick-fix ReferencedAgents missing expected agent %q (got: %v)", want, w.ReferencedAgents)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6 — WorkflowCategories
// ---------------------------------------------------------------------------

// TestWorkflowCategories_FiveCategories verifies that WorkflowCategories() returns
// exactly 5 category groups (Build, Audit, Research, Design, DataPreprocessing).
func TestWorkflowCategories_FiveCategories(t *testing.T) {
	const wantCount = 5
	cat := loadRealCatalog(t)
	cats := cat.WorkflowCategories()
	if len(cats) != wantCount {
		var names []string
		for _, c := range cats {
			names = append(names, c.Name)
		}
		t.Errorf("WorkflowCategories() returned %d categories, want %d (got: %v)", len(cats), wantCount, names)
	}
}

// TestWorkflowCategories_AllKnownCategoriesPresent verifies that all five known
// category names appear in the result.
func TestWorkflowCategories_AllKnownCategoriesPresent(t *testing.T) {
	knownCategories := []string{"Build", "Audit", "Research", "Design", "DataPreprocessing"}
	cat := loadRealCatalog(t)
	cats := cat.WorkflowCategories()

	catNames := make(map[string]bool, len(cats))
	for _, c := range cats {
		catNames[c.Name] = true
	}
	for _, name := range knownCategories {
		if !catNames[name] {
			t.Errorf("WorkflowCategories() missing expected category %q", name)
		}
	}
}

// TestWorkflowCategories_Build_FiveWorkflows verifies that the Build category contains
// exactly 5 workflows.
func TestWorkflowCategories_Build_FiveWorkflows(t *testing.T) {
	const wantCount = 5
	cat := loadRealCatalog(t)
	for _, c := range cat.WorkflowCategories() {
		if c.Name == "Build" {
			if len(c.Workflows) != wantCount {
				var ids []string
				for _, w := range c.Workflows {
					ids = append(ids, w.ID)
				}
				t.Errorf("Build category has %d workflows, want %d (got: %v)", len(c.Workflows), wantCount, ids)
			}
			return
		}
	}
	t.Error("Build category not found in WorkflowCategories()")
}

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

// TestWorkflowCategories_QuickFix_InBuildCategory verifies that quick-fix appears in
// the Build category's Workflows list.
func TestWorkflowCategories_QuickFix_InBuildCategory(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, c := range cat.WorkflowCategories() {
		if c.Name == "Build" {
			for _, w := range c.Workflows {
				if w.ID == "quick-fix" {
					return // found
				}
			}
			t.Error("quick-fix not found in Build category Workflows")
			return
		}
	}
	t.Error("Build category not found in WorkflowCategories()")
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
// T7.6 — Index / disk reconciliation
// ---------------------------------------------------------------------------

// TestIssues_IndexOrphan_ReportedAsIssue verifies that when an index entry has no
// corresponding file on disk, the catalog reports an Issue with code "index-orphan".
//
// The workflow-orphan fixture has an index entry "missing-workflow" with no file.
func TestIssues_IndexOrphan_ReportedAsIssue(t *testing.T) {
	cat := loadFixtureCatalog(t, "workflow-orphan")

	_, found := findIssue(cat.Issues(), "index-orphan")
	if !found {
		t.Errorf("Issues() does not contain an index-orphan issue; got: %v", cat.Issues())
	}
}

// TestIssues_IndexOrphan_SubjectIsWorkflowID verifies that the index-orphan Issue's
// Subject identifies the missing workflow by its id.
func TestIssues_IndexOrphan_SubjectIsWorkflowID(t *testing.T) {
	cat := loadFixtureCatalog(t, "workflow-orphan")

	iss, found := findIssue(cat.Issues(), "index-orphan")
	if !found {
		t.Skip("No index-orphan issue found; skipping Subject check")
	}
	if iss.Subject != "missing-workflow" {
		t.Errorf("index-orphan Issue.Subject = %q, want %q", iss.Subject, "missing-workflow")
	}
}

// TestIssues_FileOrphan_ReportedAsIssue verifies that when a workflow file exists on
// disk but is not listed in the index, the catalog reports an Issue with code "file-orphan".
//
// The workflow-orphan fixture has a file "orphan-on-disk.md" not in the index.
func TestIssues_FileOrphan_ReportedAsIssue(t *testing.T) {
	cat := loadFixtureCatalog(t, "workflow-orphan")

	_, found := findIssue(cat.Issues(), "file-orphan")
	if !found {
		t.Errorf("Issues() does not contain a file-orphan issue; got: %v", cat.Issues())
	}
}

// TestIssues_FileOrphan_SubjectIdentifiesFile verifies that the file-orphan Issue's
// Subject or Path field identifies the orphaned file.
func TestIssues_FileOrphan_SubjectIdentifiesFile(t *testing.T) {
	cat := loadFixtureCatalog(t, "workflow-orphan")

	iss, found := findIssue(cat.Issues(), "file-orphan")
	if !found {
		t.Skip("No file-orphan issue found; skipping Subject/Path check")
	}
	if iss.Subject == "" && iss.Path == "" {
		t.Error("file-orphan Issue has empty Subject and empty Path; expected at least one to identify the file")
	}
}

// TestIssues_RealRepo_NoIndexOrFileOrphans verifies that the real repository's workflow
// index and disk files are perfectly in sync (no orphans in either direction).
func TestIssues_RealRepo_NoIndexOrFileOrphans(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, iss := range cat.Issues() {
		if iss.Code == "index-orphan" || iss.Code == "file-orphan" {
			t.Errorf("real repository has unexpected workflow reconciliation issue: %+v", iss)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.7 — Byte-identical workflow section extraction
// ---------------------------------------------------------------------------

// TestWorkflowSection_QuickFix_ByteIdentical verifies that WorkflowSection("quick-fix")
// returns the [[SECTION:Workflow:quick-fix]] block byte-identically to what is stored in
// the quick-fix.md source file. No normalisation of any kind is allowed.
func TestWorkflowSection_QuickFix_ByteIdentical(t *testing.T) {
	cat := loadRealCatalog(t)

	section, err := cat.WorkflowSection("quick-fix")
	if err != nil {
		t.Fatalf("WorkflowSection(\"quick-fix\"): %v", err)
	}
	if len(section) == 0 {
		t.Fatal("WorkflowSection(\"quick-fix\"): returned empty bytes")
	}

	// Read the raw source file and extract the same block for comparison.
	w, ok := cat.Workflow("quick-fix")
	if !ok {
		t.Fatal("Workflow(\"quick-fix\"): not found")
	}
	raw, err := os.ReadFile(w.SourcePath)
	if err != nil {
		t.Fatalf("read quick-fix source: %v", err)
	}

	// The returned bytes must be a contiguous sub-slice of the raw source file,
	// starting with the opening boundary tag and ending with the closing boundary tag.
	if !bytes.Contains(raw, section) {
		t.Error("WorkflowSection(\"quick-fix\") returned bytes that are not a byte-identical " +
			"contiguous substring of the raw source file")
	}
}

// TestWorkflowSection_QuickFix_IncludesOpeningBoundary verifies that the extracted section
// includes the [[SECTION:Workflow:quick-fix]] opening boundary tag.
func TestWorkflowSection_QuickFix_IncludesOpeningBoundary(t *testing.T) {
	cat := loadRealCatalog(t)
	section, err := cat.WorkflowSection("quick-fix")
	if err != nil {
		t.Fatalf("WorkflowSection(\"quick-fix\"): %v", err)
	}

	openTag := []byte("[[SECTION:Workflow:quick-fix]]")
	if !bytes.Contains(section, openTag) {
		t.Errorf("WorkflowSection(\"quick-fix\") does not contain the opening boundary tag %q", openTag)
	}
}

// TestWorkflowSection_QuickFix_IncludesClosingBoundary verifies that the extracted section
// includes the [[/SECTION:Workflow:quick-fix]] closing boundary tag.
func TestWorkflowSection_QuickFix_IncludesClosingBoundary(t *testing.T) {
	cat := loadRealCatalog(t)
	section, err := cat.WorkflowSection("quick-fix")
	if err != nil {
		t.Fatalf("WorkflowSection(\"quick-fix\"): %v", err)
	}

	closeTag := []byte("[[/SECTION:Workflow:quick-fix]]")
	if !bytes.Contains(section, closeTag) {
		t.Errorf("WorkflowSection(\"quick-fix\") does not contain the closing boundary tag %q", closeTag)
	}
}

// TestWorkflowSection_QuickFix_StartsWithOpeningBoundary verifies that the first line
// of the extracted section IS the opening boundary tag. No content must appear before it.
func TestWorkflowSection_QuickFix_StartsWithOpeningBoundary(t *testing.T) {
	cat := loadRealCatalog(t)
	section, err := cat.WorkflowSection("quick-fix")
	if err != nil {
		t.Fatalf("WorkflowSection(\"quick-fix\"): %v", err)
	}

	openTag := "[[SECTION:Workflow:quick-fix]]"
	if !bytes.HasPrefix(section, []byte(openTag)) {
		first := string(section)
		if len(first) > 60 {
			first = first[:60] + "..."
		}
		t.Errorf("WorkflowSection(\"quick-fix\") does not start with the opening boundary tag;\n  got prefix: %q", first)
	}
}

// TestWorkflowSection_QuickFix_EndsWithClosingBoundary verifies that the extracted section
// ends with the closing boundary tag (optionally followed by a single newline).
func TestWorkflowSection_QuickFix_EndsWithClosingBoundary(t *testing.T) {
	cat := loadRealCatalog(t)
	section, err := cat.WorkflowSection("quick-fix")
	if err != nil {
		t.Fatalf("WorkflowSection(\"quick-fix\"): %v", err)
	}

	closeTag := "[[/SECTION:Workflow:quick-fix]]"
	trimmed := bytes.TrimRight(section, "\n")
	if !bytes.HasSuffix(trimmed, []byte(closeTag)) {
		last := string(section)
		if len(last) > 80 {
			last = "..." + last[len(last)-80:]
		}
		t.Errorf("WorkflowSection(\"quick-fix\") does not end with the closing boundary tag;\n  got suffix: %q", last)
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
// returns an error when a workflow is present in the catalog (its file exists on disk and
// is listed in the index) but the file does not contain a [[SECTION:Workflow:{id}]] block.
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
