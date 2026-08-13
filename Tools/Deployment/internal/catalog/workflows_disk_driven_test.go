package catalog_test

// Tests for disk-driven workflow loading (TDD RED phase).
//
// These tests specify the new workflow-loading contract:
//
//   Authoritative set: every eligible .md file found on disk under Catalog/Workflows/
//   subdirectories, regardless of Index.md content or presence. A file present on disk
//   is loaded; an Index.md entry with no corresponding disk file has no effect.
//
//   Metadata source (field-by-field):
//     ID          — frontmatter `id`; falls back to base name without .md when absent or empty
//     Name        — frontmatter `name`; falls back to ID when absent or empty
//     Category    — the name of the containing directory (verbatim)
//     Version     — frontmatter `version`; empty when absent
//     Description — frontmatter `description`; empty when absent
//     Hint        — frontmatter `hint`; empty when absent
//
//   Ordering: WorkflowCategories() returns categories sorted ascending by name;
//   within a category, workflows are sorted ascending by ID.
//
//   Exclusions: .md files placed directly in Catalog/Workflows/ (not in a subdirectory)
//   and files whose base name begins with '_' are excluded regardless of content.
//
//   Issues: normal loading emits neither file-orphan nor index-orphan issues. Both
//   codes remain defined; their only producer under the new contract is the standalone
//   index-check command.
//
// All tests in this file run against a temporary directory fixture and do NOT touch the
// real repository tree.

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Authoritative set: disk files, not the index
// ---------------------------------------------------------------------------

// TestWorkflows_DiskOnlyFile_LoadedWithoutIndexEntry verifies that a workflow file
// present on disk but absent from Index.md is loaded and selectable. The index has
// no authority over which files are loaded; only disk presence determines membership.
func TestWorkflows_DiskOnlyFile_LoadedWithoutIndexEntry(t *testing.T) {
	root := t.TempDir()

	// Index.md exists but contains no entry for "disk-only".
	writeWorkflowIndex(t, root, [][7]string{
		{"indexed-only", "Alpha", "1.0.0", "Indexed Only", "Indexed workflow.", "Hint.", "Alpha/indexed-only.md"},
	})
	// Write a workflow file on disk that is NOT listed in the index.
	writeWorkflowFile(t, root, "Alpha", "disk-only.md", "disk-only", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	w, ok := cat.Workflow("disk-only")
	if !ok {
		t.Fatal("Workflow(\"disk-only\"): returned not-found; a file present on disk " +
			"but absent from Index.md must be loaded and selectable")
	}
	if w.ID != "disk-only" {
		t.Errorf("Workflow ID = %q, want %q", w.ID, "disk-only")
	}
}

// TestWorkflows_NoIndexMd_LoadsAllDiskWorkflows verifies that when Catalog/Workflows/Index.md
// does not exist, every eligible workflow file found on disk is still loaded. The absence
// of Index.md is not an error and does not prevent loading.
func TestWorkflows_NoIndexMd_LoadsAllDiskWorkflows(t *testing.T) {
	root := t.TempDir()

	// Write two workflow files with no Index.md.
	writeWorkflowFile(t, root, "Alpha", "workflow-a.md", "workflow-a", nil)
	writeWorkflowFile(t, root, "Beta", "workflow-b.md", "workflow-b", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	if len(cat.Workflows()) != 2 {
		t.Fatalf("Workflows() returned %d workflows, want 2; "+
			"absence of Index.md must not prevent loading disk files", len(cat.Workflows()))
	}
	if _, ok := cat.Workflow("workflow-a"); !ok {
		t.Error("Workflow(\"workflow-a\"): not found; expected loading from disk without Index.md")
	}
	if _, ok := cat.Workflow("workflow-b"); !ok {
		t.Error("Workflow(\"workflow-b\"): not found; expected loading from disk without Index.md")
	}
}

// TestWorkflows_IndexOnlyEntry_HasNoEffect verifies that an entry in Index.md for which
// no corresponding file exists on disk neither adds a workflow to the loaded set nor
// produces a spurious catalog entry. Index-only entries are silently ignored.
func TestWorkflows_IndexOnlyEntry_HasNoEffect(t *testing.T) {
	root := t.TempDir()

	// Index.md references "ghost-workflow" but no file exists on disk for it.
	writeWorkflowIndex(t, root, [][7]string{
		{"ghost-workflow", "Alpha", "1.0.0", "Ghost", "A ghost.", "Hint.", "Alpha/ghost-workflow.md"},
	})
	// Write a real disk file (not in the index) so the catalog has something to load.
	writeWorkflowFile(t, root, "Alpha", "real-workflow.md", "real-workflow", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// The disk file must be found.
	if _, ok := cat.Workflow("real-workflow"); !ok {
		t.Error("Workflow(\"real-workflow\"): not found; disk file must be loaded regardless of index content")
	}
	// The index-only entry must not appear.
	if _, ok := cat.Workflow("ghost-workflow"); ok {
		t.Error("Workflow(\"ghost-workflow\"): found but expected not-found; " +
			"an Index.md entry with no corresponding disk file must have no effect on the loaded set")
	}
}

// ---------------------------------------------------------------------------
// Metadata source: frontmatter and directory name
// ---------------------------------------------------------------------------

// TestWorkflow_Category_DerivedFromContainingDirectory verifies that the Category field
// of a loaded workflow is taken from the name of the subdirectory that contains its file,
// verbatim. No frontmatter field or index column provides the category.
func TestWorkflow_Category_DerivedFromContainingDirectory(t *testing.T) {
	root := t.TempDir()
	writeWorkflowFile(t, root, "MyCategory", "cat-wf.md", "cat-wf", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	w, ok := cat.Workflow("cat-wf")
	if !ok {
		t.Fatal("Workflow(\"cat-wf\"): not found")
	}
	if w.Category != "MyCategory" {
		t.Errorf("Category = %q, want %q (from containing directory name)", w.Category, "MyCategory")
	}
}

// TestWorkflow_ID_FallsBackToBasenameWhenFrontmatterAbsent verifies that when a workflow
// file carries no `id` frontmatter field (or an empty one), the workflow ID is derived
// from the file's base name without the .md extension.
func TestWorkflow_ID_FallsBackToBasenameWhenFrontmatterAbsent(t *testing.T) {
	root := t.TempDir()

	// Write a workflow file with no `id` frontmatter field.
	mustMkdir(t, root, "Catalog", "Workflows", "Alpha")
	content := []byte("---\nversion: \"1.0.0\"\nname: NoID Workflow\ndescription: No id field.\nhint: Hint.\n---\n\nContent.\n")
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", "Alpha", "my-workflow.md"), content)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	w, ok := cat.Workflow("my-workflow")
	if !ok {
		t.Fatal("Workflow(\"my-workflow\"): not found; " +
			"when frontmatter `id` is absent, the base name without .md must be used as the ID")
	}
	if w.ID != "my-workflow" {
		t.Errorf("ID = %q, want %q", w.ID, "my-workflow")
	}
}

// TestWorkflow_Name_FallsBackToIDWhenFrontmatterAbsent verifies that when a workflow
// file carries an `id` but no `name` frontmatter field (or an empty one), the Name
// field is set to the workflow's ID rather than left empty.
func TestWorkflow_Name_FallsBackToIDWhenFrontmatterAbsent(t *testing.T) {
	root := t.TempDir()

	// Write a workflow file with `id` but no `name` field.
	mustMkdir(t, root, "Catalog", "Workflows", "Alpha")
	content := []byte("---\nid: test-wf\nversion: \"1.0.0\"\ndescription: No name field.\nhint: Hint.\n---\n\nContent.\n")
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", "Alpha", "test-wf.md"), content)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	w, ok := cat.Workflow("test-wf")
	if !ok {
		t.Fatal("Workflow(\"test-wf\"): not found")
	}
	if w.Name == "" {
		t.Fatal("Name is empty; when frontmatter `name` is absent, ID must be used as the Name fallback")
	}
	if w.Name != w.ID {
		t.Errorf("Name = %q, want %q (ID fallback); "+
			"when frontmatter `name` is absent, Name must equal ID", w.Name, w.ID)
	}
}

// ---------------------------------------------------------------------------
// Ordering
// ---------------------------------------------------------------------------

// TestWorkflowCategories_SortedAlphabetically verifies that WorkflowCategories returns
// categories sorted ascending by category name, regardless of the filesystem enumeration
// order of category directories.
func TestWorkflowCategories_SortedAlphabetically(t *testing.T) {
	root := t.TempDir()

	// Write workflows in categories that are out of alphabetical order.
	writeWorkflowFile(t, root, "Zebra", "z-wf.md", "z-wf", nil)
	writeWorkflowFile(t, root, "Alpha", "a-wf.md", "a-wf", nil)
	writeWorkflowFile(t, root, "Middle", "m-wf.md", "m-wf", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	cats := cat.WorkflowCategories()
	if len(cats) != 3 {
		t.Fatalf("WorkflowCategories() returned %d categories, want 3", len(cats))
	}

	wantOrder := []string{"Alpha", "Middle", "Zebra"}
	for i, want := range wantOrder {
		if cats[i].Name != want {
			t.Errorf("WorkflowCategories()[%d].Name = %q, want %q; "+
				"categories must be sorted alphabetically by name", i, cats[i].Name, want)
		}
	}
}

// TestWorkflowsWithinCategory_SortedByID verifies that within a single category,
// workflows are sorted ascending by ID.
func TestWorkflowsWithinCategory_SortedByID(t *testing.T) {
	root := t.TempDir()

	// Write two workflow files in the same category, out of alphabetical ID order.
	writeWorkflowFile(t, root, "Mixed", "bravo.md", "bravo", nil)
	writeWorkflowFile(t, root, "Mixed", "alpha.md", "alpha", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	cats := cat.WorkflowCategories()
	if len(cats) != 1 {
		t.Fatalf("WorkflowCategories() returned %d categories, want 1", len(cats))
	}
	wfs := cats[0].Workflows
	if len(wfs) != 2 {
		t.Fatalf("category %q has %d workflows, want 2", cats[0].Name, len(wfs))
	}
	if wfs[0].ID != "alpha" || wfs[1].ID != "bravo" {
		t.Errorf("within-category order: got [%q, %q], want [%q, %q]; "+
			"workflows within a category must be sorted ascending by ID",
			wfs[0].ID, wfs[1].ID, "alpha", "bravo")
	}
}

// ---------------------------------------------------------------------------
// Exclusion rules
// ---------------------------------------------------------------------------

// TestWorkflows_RootLevelMdFile_ExcludedFromLoading verifies that .md files placed
// directly inside Catalog/Workflows/ (not inside a category subdirectory) are never
// loaded, even when they carry valid frontmatter.
func TestWorkflows_RootLevelMdFile_ExcludedFromLoading(t *testing.T) {
	root := t.TempDir()

	// Write a .md file directly in Catalog/Workflows/ — not in a subdirectory.
	mustMkdir(t, root, "Catalog", "Workflows")
	rootLevelContent := []byte("---\nid: root-level\nversion: \"1.0.0\"\nname: Root Level.\ndescription: Desc.\nhint: Hint.\n---\n\nContent.\n")
	mustWriteFile(t, root, filepath.Join("Catalog", "Workflows", "root-level.md"), rootLevelContent)

	// Also write a valid subdirectory workflow to confirm the loader works normally.
	writeWorkflowFile(t, root, "Alpha", "valid-wf.md", "valid-wf", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	if _, ok := cat.Workflow("root-level"); ok {
		t.Error("Workflow(\"root-level\"): found; root-level .md files must be excluded from loading")
	}
	if _, ok := cat.Workflow("valid-wf"); !ok {
		t.Error("Workflow(\"valid-wf\"): not found; subdirectory workflow must be loaded")
	}
}

// ---------------------------------------------------------------------------
// Issue emission from normal loading
// ---------------------------------------------------------------------------

// TestWorkflows_NormalLoad_EmitsNoFileOrphanIssues verifies that normal workflow loading
// does not emit any file-orphan issues, even when a disk file has no corresponding entry
// in Index.md. File-orphan detection belongs to the standalone index-check command.
func TestWorkflows_NormalLoad_EmitsNoFileOrphanIssues(t *testing.T) {
	root := t.TempDir()

	// Index.md names one workflow; a different file exists on disk — a classic file-orphan scenario.
	writeWorkflowIndex(t, root, [][7]string{
		{"indexed-workflow", "Alpha", "1.0.0", "Indexed", "Indexed.", "Hint.", "Alpha/indexed-workflow.md"},
	})
	writeWorkflowFile(t, root, "Alpha", "disk-only.md", "disk-only", nil)

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, iss := range cat.Issues() {
		if iss.Code == "file-orphan" {
			t.Errorf("normal loading emitted a file-orphan issue %+v; "+
				"file-orphan must not be emitted by the catalog loader", iss)
		}
	}
}

// TestWorkflows_NormalLoad_EmitsNoIndexOrphanIssues verifies that normal workflow loading
// does not emit any index-orphan issues, even when Index.md contains an entry for which
// no corresponding disk file exists. Index-orphan detection belongs to the standalone
// index-check command.
func TestWorkflows_NormalLoad_EmitsNoIndexOrphanIssues(t *testing.T) {
	root := t.TempDir()

	// Index.md references "ghost-workflow" — no file exists on disk for it.
	writeWorkflowIndex(t, root, [][7]string{
		{"ghost-workflow", "Alpha", "1.0.0", "Ghost", "A ghost.", "Hint.", "Alpha/ghost-workflow.md"},
	})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, iss := range cat.Issues() {
		if iss.Code == "index-orphan" {
			t.Errorf("normal loading emitted an index-orphan issue %+v; "+
				"index-orphan must not be emitted by the catalog loader", iss)
		}
	}
}
