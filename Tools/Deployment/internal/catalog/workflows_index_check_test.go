package catalog_test

// workflows_index_check_test.go covers CheckWorkflowIndex, the on-demand staleness check
// that compares Catalog/Workflows/Index.md against the workflow files present on disk.
//
// This is the TDD RED phase for I2.1: all tests compile but fail until the real
// implementation of CheckWorkflowIndex replaces the current stub.
//
// Verified behaviours (mapped from plan task T2.1):
//
// Core cases (four fixture layouts):
//   - A file on disk absent from the index is reported as a file-orphan finding.
//   - An index row with no file on disk is reported as an index-orphan finding.
//   - A fully consistent catalog (every disk file indexed, every index row on disk) reports
//     no orphans of either class and Clean() returns true.
//   - A catalog with no Index.md at all: IndexPresent is false, both orphan slices are empty,
//     and Clean() returns true — the absent index is not treated as drift.
//
// Correctness of findings:
//   - file-orphan Code is "file-orphan".
//   - index-orphan Code is "index-orphan".
//   - file-orphan Message matches the historical issue text verbatim.
//   - index-orphan Message matches the historical issue text verbatim.
//   - file-orphan Subject is the relative-to-Workflows path of the orphaned file.
//   - index-orphan Subject is the workflow id from the index row.
//   - Mixed catalog with both orphan classes produces findings in both slices.
//
// Determinism:
//   - Multiple file-orphans are sorted ascending by Subject.
//   - Multiple index-orphans are sorted ascending by Subject.
//
// Report metadata:
//   - DiskCount equals the number of eligible workflow files found on disk.
//   - IndexCount equals the number of rows parsed from the index.
//   - IndexPath is the absolute path of Workflows/Index.md inside the catalogRoot.
//   - Clean() returns false when at least one orphan exists, true otherwise.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Fixture helpers for index-check tests
// ---------------------------------------------------------------------------

// makeCatalogRootWithIndex creates a minimal MOSAIC root in a temp directory,
// writes a workflow Index.md with the given rows, and returns both the MOSAIC
// root and the catalogRoot (root/Catalog).
// rows is a slice of [7]string: {id, category, version, name, description, hint, file}.
func makeCatalogRootWithIndex(t *testing.T, rows [][7]string) (mosaicRoot, catRoot string) {
	t.Helper()
	mosaicRoot = makeTempMosaicRoot(t)
	catRoot = filepath.Join(mosaicRoot, "Catalog")
	if len(rows) > 0 {
		writeWorkflowIndex(t, mosaicRoot, rows)
	}
	return mosaicRoot, catRoot
}

// writeDiskWorkflow writes a minimal workflow file at
// <root>/Catalog/Workflows/<category>/<fileName>.
// Returns the relative-to-Workflows path (e.g., "Alpha/workflow.md").
func writeDiskWorkflow(t *testing.T, mosaicRoot, category, fileName string) string {
	t.Helper()
	mustMkdir(t, mosaicRoot, "Catalog", "Workflows", category)
	content := []byte("---\nid: " + fileName[:len(fileName)-3] + "\n---\nBody.\n")
	mustWriteFile(t, mosaicRoot, filepath.Join("Catalog", "Workflows", category, fileName), content)
	return category + "/" + fileName
}

// ---------------------------------------------------------------------------
// T2.1: Core cases — four fixture layouts
// ---------------------------------------------------------------------------

// TestCheckWorkflowIndex_FileOrphan_ReportedWhenFileOnDiskMissingFromIndex verifies that a
// workflow file present on disk but absent from Index.md is reported as a file-orphan finding.
// The file-orphan slice must be non-empty and contain the orphaned file's relative path.
func TestCheckWorkflowIndex_FileOrphan_ReportedWhenFileOnDiskMissingFromIndex(t *testing.T) {
	// Arrange: index has one row ("indexed-wf"), disk has two files: "indexed-wf" and
	// "orphaned-wf". "orphaned-wf" is on disk but not in the index.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"indexed-wf", "Alpha", "1.0.0", "Indexed", "Desc", "Hint", "Alpha/indexed-wf.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Alpha", "indexed-wf.md")
	orphanRelPath := writeDiskWorkflow(t, mosaicRoot, "Alpha", "orphaned-wf.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)

	// Assert
	if err != nil {
		t.Fatalf("CheckWorkflowIndex returned unexpected error: %v", err)
	}
	if len(report.FileOrphans) == 0 {
		t.Fatal("FileOrphans is empty; expected at least one file-orphan for the unlisted disk file")
	}
	found := false
	for _, o := range report.FileOrphans {
		if o.Subject == orphanRelPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FileOrphans does not contain orphan with Subject %q; got %v", orphanRelPath, report.FileOrphans)
	}

	// Assert o.Path for the file-orphan: must be an absolute path to the existing orphaned file on disk.
	for _, o := range report.FileOrphans {
		if o.Subject == orphanRelPath {
			if !filepath.IsAbs(o.Path) {
				t.Errorf("FileOrphan.Path = %q is not absolute; want absolute path to the orphaned file", o.Path)
			}
			if _, statErr := os.Stat(o.Path); statErr != nil {
				t.Errorf("FileOrphan.Path = %q: stat failed: %v; want a path to an existing file on disk", o.Path, statErr)
			}
			break
		}
	}
}

// TestCheckWorkflowIndex_IndexOrphan_ReportedWhenIndexRowHasNoFileOnDisk verifies that an
// index row whose corresponding file does not exist on disk is reported as an index-orphan
// finding.
func TestCheckWorkflowIndex_IndexOrphan_ReportedWhenIndexRowHasNoFileOnDisk(t *testing.T) {
	// Arrange: index has two rows ("present-wf" and "ghost-wf"). Only "present-wf" has a
	// file on disk. "ghost-wf" is listed in the index but has no file.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"present-wf", "Beta", "1.0.0", "Present", "Desc", "Hint", "Beta/present-wf.md"},
		{"ghost-wf", "Beta", "1.0.0", "Ghost", "Desc", "Hint", "Beta/ghost-wf.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Beta", "present-wf.md")
	// ghost-wf.md is intentionally NOT written to disk.

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)

	// Assert
	if err != nil {
		t.Fatalf("CheckWorkflowIndex returned unexpected error: %v", err)
	}
	if len(report.IndexOrphans) == 0 {
		t.Fatal("IndexOrphans is empty; expected at least one index-orphan for the missing file")
	}
	found := false
	for _, o := range report.IndexOrphans {
		if o.Subject == "ghost-wf" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("IndexOrphans does not contain orphan with Subject %q; got %v", "ghost-wf", report.IndexOrphans)
	}

	// Assert o.Path for the index-orphan: must be a non-empty absolute path pointing to where
	// the ghost file would be (the expected-but-absent file that the index references).
	for _, o := range report.IndexOrphans {
		if o.Subject == "ghost-wf" {
			if o.Path == "" {
				t.Error("IndexOrphan.Path is empty; want the absolute path where the ghost file would be")
			}
			if !filepath.IsAbs(o.Path) {
				t.Errorf("IndexOrphan.Path = %q is not absolute", o.Path)
			}
			if filepath.Base(o.Path) != "ghost-wf.md" {
				t.Errorf("IndexOrphan.Path base = %q, want %q", filepath.Base(o.Path), "ghost-wf.md")
			}
			break
		}
	}
}

// TestCheckWorkflowIndex_Clean_WhenCatalogFullyConsistent verifies that when every disk file
// is listed in the index and every index row has a corresponding file on disk, no orphans are
// reported and Clean() returns true.
func TestCheckWorkflowIndex_Clean_WhenCatalogFullyConsistent(t *testing.T) {
	// Arrange: index and disk both have exactly "wf-a" and "wf-b".
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"wf-a", "Gamma", "1.0.0", "WF A", "Desc", "Hint", "Gamma/wf-a.md"},
		{"wf-b", "Gamma", "1.0.0", "WF B", "Desc", "Hint", "Gamma/wf-b.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Gamma", "wf-a.md")
	writeDiskWorkflow(t, mosaicRoot, "Gamma", "wf-b.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)

	// Assert
	if err != nil {
		t.Fatalf("CheckWorkflowIndex returned unexpected error: %v", err)
	}
	if len(report.FileOrphans) != 0 {
		t.Errorf("FileOrphans = %v, want empty; catalog is fully consistent", report.FileOrphans)
	}
	if len(report.IndexOrphans) != 0 {
		t.Errorf("IndexOrphans = %v, want empty; catalog is fully consistent", report.IndexOrphans)
	}
	if !report.Clean() {
		t.Error("Clean() = false, want true for a fully consistent catalog")
	}
	if report.CatalogRoot != catRoot {
		t.Errorf("CatalogRoot = %q, want %q (the catalogRoot argument passed to CheckWorkflowIndex)", report.CatalogRoot, catRoot)
	}
}

// TestCheckWorkflowIndex_NoIndex_HandledGracefully verifies that when no Index.md file exists,
// CheckWorkflowIndex returns a report with IndexPresent false, both orphan slices empty, and
// Clean() returning true. The absent index must not be treated as drift.
func TestCheckWorkflowIndex_NoIndex_HandledGracefully(t *testing.T) {
	// Arrange: MOSAIC root with workflow files on disk but no Index.md.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil) // no index rows → no Index.md written
	writeDiskWorkflow(t, mosaicRoot, "Delta", "wf-x.md")
	writeDiskWorkflow(t, mosaicRoot, "Delta", "wf-y.md")
	// Ensure no Index.md exists.
	indexPath := filepath.Join(catRoot, "Workflows", "Index.md")
	os.Remove(indexPath) // safe even if it doesn't exist

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)

	// Assert
	if err != nil {
		t.Fatalf("CheckWorkflowIndex returned unexpected error for absent Index.md: %v", err)
	}
	if report.IndexPresent {
		t.Error("IndexPresent = true, want false; no Index.md was written")
	}
	if len(report.FileOrphans) != 0 {
		t.Errorf("FileOrphans = %v, want empty; absent index must not produce file-orphans", report.FileOrphans)
	}
	if len(report.IndexOrphans) != 0 {
		t.Errorf("IndexOrphans = %v, want empty; absent index must not produce index-orphans", report.IndexOrphans)
	}
	if !report.Clean() {
		t.Error("Clean() = false, want true; an absent index is a clean state")
	}
}

// ---------------------------------------------------------------------------
// Correctness of findings
// ---------------------------------------------------------------------------

// TestCheckWorkflowIndex_FileOrphan_HasCorrectCode verifies that every file-orphan finding
// carries Code "file-orphan", matching the historical Issue code.
func TestCheckWorkflowIndex_FileOrphan_HasCorrectCode(t *testing.T) {
	// Arrange: one file-orphan situation.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"listed", "Epsilon", "1.0.0", "Listed", "Desc", "Hint", "Epsilon/listed.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Epsilon", "listed.md")
	writeDiskWorkflow(t, mosaicRoot, "Epsilon", "unlisted.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	for _, o := range report.FileOrphans {
		if o.Code != "file-orphan" {
			t.Errorf("FileOrphan.Code = %q, want %q", o.Code, "file-orphan")
		}
	}
	if len(report.FileOrphans) == 0 {
		t.Error("FileOrphans is empty; expected the unlisted file to appear")
	}
}

// TestCheckWorkflowIndex_IndexOrphan_HasCorrectCode verifies that every index-orphan finding
// carries Code "index-orphan", matching the historical Issue code.
func TestCheckWorkflowIndex_IndexOrphan_HasCorrectCode(t *testing.T) {
	// Arrange: one index-orphan situation.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"existing-wf", "Zeta", "1.0.0", "Existing", "Desc", "Hint", "Zeta/existing-wf.md"},
		{"missing-wf", "Zeta", "1.0.0", "Missing", "Desc", "Hint", "Zeta/missing-wf.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Zeta", "existing-wf.md")
	// missing-wf.md is not written to disk.

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	for _, o := range report.IndexOrphans {
		if o.Code != "index-orphan" {
			t.Errorf("IndexOrphan.Code = %q, want %q", o.Code, "index-orphan")
		}
	}
	if len(report.IndexOrphans) == 0 {
		t.Error("IndexOrphans is empty; expected the missing-wf index row to appear")
	}
}

// TestCheckWorkflowIndex_FileOrphan_MessageMatchesHistoricalText verifies that the Message
// field of a file-orphan finding is the exact text used by the retired Issue emission, so
// existing documentation and user familiarity carry over.
func TestCheckWorkflowIndex_FileOrphan_MessageMatchesHistoricalText(t *testing.T) {
	const wantMessage = "workflow file exists on disk but is not listed in Workflows/Index.md"

	// Arrange: makeCatalogRootWithIndex with nil rows does not create an Index.md (the helper
	// only calls writeWorkflowIndex when len(rows) > 0), so no os.Remove is needed before
	// writing the empty-table index below.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	// Write an index file with no rows so we get an orphan via a real index.
	writeWorkflowIndex(t, mosaicRoot, nil) // empty table
	writeDiskWorkflow(t, mosaicRoot, "Eta", "orphaned.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.FileOrphans) == 0 {
		t.Fatal("FileOrphans is empty; expected a file-orphan for the unlisted disk file")
	}
	for _, o := range report.FileOrphans {
		if o.Message != wantMessage {
			t.Errorf("FileOrphan.Message = %q, want %q", o.Message, wantMessage)
		}
	}
}

// TestCheckWorkflowIndex_IndexOrphan_MessageMatchesHistoricalText verifies that the Message
// field of an index-orphan finding is the exact text used by the retired Issue emission.
func TestCheckWorkflowIndex_IndexOrphan_MessageMatchesHistoricalText(t *testing.T) {
	const wantMessage = "workflow listed in index has no corresponding file on disk"

	// Arrange
	_, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"ghost", "Theta", "1.0.0", "Ghost", "Desc", "Hint", "Theta/ghost.md"},
	})
	// ghost.md is not written to disk.

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.IndexOrphans) == 0 {
		t.Fatal("IndexOrphans is empty; expected index-orphan for the missing file")
	}
	for _, o := range report.IndexOrphans {
		if o.Message != wantMessage {
			t.Errorf("IndexOrphan.Message = %q, want %q", o.Message, wantMessage)
		}
	}
}

// TestCheckWorkflowIndex_FileOrphan_SubjectIsRelativeToWorkflows verifies that a file-orphan's
// Subject is the relative-to-Workflows path of the unlisted file (e.g., "Alpha/orphan.md"),
// not an absolute path.
func TestCheckWorkflowIndex_FileOrphan_SubjectIsRelativeToWorkflows(t *testing.T) {
	// Arrange
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeWorkflowIndex(t, mosaicRoot, nil) // empty index
	writeDiskWorkflow(t, mosaicRoot, "Iota", "orphan-file.md")
	wantSubject := "Iota/orphan-file.md"

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.FileOrphans) == 0 {
		t.Fatal("FileOrphans is empty; expected the orphaned file to appear")
	}
	for _, o := range report.FileOrphans {
		if filepath.IsAbs(o.Subject) {
			t.Errorf("FileOrphan.Subject %q is an absolute path; expected relative-to-Workflows path", o.Subject)
		}
		if o.Subject != wantSubject {
			t.Errorf("FileOrphan.Subject = %q, want %q", o.Subject, wantSubject)
		}
	}
}

// TestCheckWorkflowIndex_IndexOrphan_SubjectIsWorkflowID verifies that an index-orphan's
// Subject is the workflow id from the index row, not a file path.
func TestCheckWorkflowIndex_IndexOrphan_SubjectIsWorkflowID(t *testing.T) {
	// Arrange
	_, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"my-ghost-workflow", "Kappa", "1.0.0", "Ghost", "Desc", "Hint", "Kappa/my-ghost-workflow.md"},
	})
	// file not written to disk

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.IndexOrphans) == 0 {
		t.Fatal("IndexOrphans is empty; expected index-orphan for missing file")
	}
	found := false
	for _, o := range report.IndexOrphans {
		if o.Subject == "my-ghost-workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("IndexOrphans does not contain entry with Subject %q; got %v",
			"my-ghost-workflow", report.IndexOrphans)
	}
}

// TestCheckWorkflowIndex_MixedOrphans_BothClassesReported verifies that when a catalog has
// both a file-orphan (disk file not in index) and an index-orphan (index row with no file),
// both classes appear in the returned report.
func TestCheckWorkflowIndex_MixedOrphans_BothClassesReported(t *testing.T) {
	// Arrange: index lists "indexed" and "ghost"; disk has "indexed" and "extra".
	// "ghost" → index-orphan (in index, not on disk)
	// "extra" → file-orphan (on disk, not in index)
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"indexed", "Lambda", "1.0.0", "Indexed", "Desc", "Hint", "Lambda/indexed.md"},
		{"ghost", "Lambda", "1.0.0", "Ghost", "Desc", "Hint", "Lambda/ghost.md"},
	})
	writeDiskWorkflow(t, mosaicRoot, "Lambda", "indexed.md")
	writeDiskWorkflow(t, mosaicRoot, "Lambda", "extra.md")
	// ghost.md is intentionally NOT written to disk.

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.FileOrphans) == 0 {
		t.Error("FileOrphans is empty; expected file-orphan for 'extra.md'")
	}
	if len(report.IndexOrphans) == 0 {
		t.Error("IndexOrphans is empty; expected index-orphan for 'ghost'")
	}
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

// TestCheckWorkflowIndex_FileOrphans_SortedBySubject verifies that when multiple file-orphan
// findings exist, they are sorted ascending by Subject so CLI output is stable.
func TestCheckWorkflowIndex_FileOrphans_SortedBySubject(t *testing.T) {
	// Arrange: empty index, three disk files in an order that may not match alphabetical.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeWorkflowIndex(t, mosaicRoot, nil) // empty table
	writeDiskWorkflow(t, mosaicRoot, "Mu", "zebra.md")
	writeDiskWorkflow(t, mosaicRoot, "Mu", "alpha.md")
	writeDiskWorkflow(t, mosaicRoot, "Mu", "middle.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.FileOrphans) < 2 {
		t.Fatalf("FileOrphans has %d entries; need at least 2 to verify sort order", len(report.FileOrphans))
	}
	for i := 1; i < len(report.FileOrphans); i++ {
		if report.FileOrphans[i].Subject < report.FileOrphans[i-1].Subject {
			t.Errorf("FileOrphans not sorted: entry[%d].Subject=%q is before entry[%d].Subject=%q",
				i, report.FileOrphans[i].Subject, i-1, report.FileOrphans[i-1].Subject)
		}
	}
}

// TestCheckWorkflowIndex_IndexOrphans_SortedBySubject verifies that when multiple
// index-orphan findings exist, they are sorted ascending by Subject.
func TestCheckWorkflowIndex_IndexOrphans_SortedBySubject(t *testing.T) {
	// Arrange: index has three rows whose files are all absent from disk.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"zz-last", "Nu", "1.0.0", "Last", "Desc", "Hint", "Nu/zz-last.md"},
		{"aa-first", "Nu", "1.0.0", "First", "Desc", "Hint", "Nu/aa-first.md"},
		{"mm-middle", "Nu", "1.0.0", "Middle", "Desc", "Hint", "Nu/mm-middle.md"},
	})
	// No disk files are written.
	// Ensure the Workflows/Nu directory exists so the scanner can find zero files.
	mustMkdir(t, mosaicRoot, "Catalog", "Workflows", "Nu")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if len(report.IndexOrphans) < 2 {
		t.Fatalf("IndexOrphans has %d entries; need at least 2 to verify sort order", len(report.IndexOrphans))
	}
	for i := 1; i < len(report.IndexOrphans); i++ {
		if report.IndexOrphans[i].Subject < report.IndexOrphans[i-1].Subject {
			t.Errorf("IndexOrphans not sorted: entry[%d].Subject=%q before entry[%d].Subject=%q",
				i, report.IndexOrphans[i].Subject, i-1, report.IndexOrphans[i-1].Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// Report metadata
// ---------------------------------------------------------------------------

// TestCheckWorkflowIndex_DiskCount_ReflectsEligibleFilesOnDisk verifies that the DiskCount
// field equals the number of eligible workflow files found on disk (one level below Workflows/,
// .md extension, base name not starting with '_').
func TestCheckWorkflowIndex_DiskCount_ReflectsEligibleFilesOnDisk(t *testing.T) {
	// Arrange: three eligible files and one underscore-prefixed file (excluded).
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeWorkflowIndex(t, mosaicRoot, nil) // empty index
	writeDiskWorkflow(t, mosaicRoot, "Xi", "wf-one.md")
	writeDiskWorkflow(t, mosaicRoot, "Xi", "wf-two.md")
	writeDiskWorkflow(t, mosaicRoot, "Xi", "wf-three.md")
	// Write an underscore-prefixed file that must be excluded.
	mustMkdir(t, mosaicRoot, "Catalog", "Workflows", "Xi")
	mustWriteFile(t, mosaicRoot, filepath.Join("Catalog", "Workflows", "Xi", "_template.md"), []byte("template"))

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if report.DiskCount != 3 {
		t.Errorf("DiskCount = %d, want 3; underscore-prefixed files must not be counted", report.DiskCount)
	}
}

// TestCheckWorkflowIndex_IndexCount_ReflectsParsedRows verifies that IndexCount equals the
// number of valid rows parsed from Index.md.
func TestCheckWorkflowIndex_IndexCount_ReflectsParsedRows(t *testing.T) {
	// Arrange: index with exactly two data rows; no disk files.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, [][7]string{
		{"row-one", "Omicron", "1.0.0", "One", "Desc", "Hint", "Omicron/row-one.md"},
		{"row-two", "Omicron", "1.0.0", "Two", "Desc", "Hint", "Omicron/row-two.md"},
	})
	// No disk files; we're only testing IndexCount here.
	mustMkdir(t, mosaicRoot, "Catalog", "Workflows", "Omicron")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if report.IndexCount != 2 {
		t.Errorf("IndexCount = %d, want 2", report.IndexCount)
	}
}

// TestCheckWorkflowIndex_IndexCount_ZeroWhenIndexAbsent verifies that IndexCount is zero when
// no Index.md exists.
func TestCheckWorkflowIndex_IndexCount_ZeroWhenIndexAbsent(t *testing.T) {
	// Arrange: MOSAIC root with no Index.md.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeDiskWorkflow(t, mosaicRoot, "Pi", "wf.md")
	os.Remove(filepath.Join(catRoot, "Workflows", "Index.md"))

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if report.IndexCount != 0 {
		t.Errorf("IndexCount = %d, want 0 when Index.md is absent", report.IndexCount)
	}
}

// TestCheckWorkflowIndex_IndexPath_IsAbsolutePathToIndexMd verifies that IndexPath in the
// returned report is the absolute path of Workflows/Index.md inside the catalogRoot.
func TestCheckWorkflowIndex_IndexPath_IsAbsolutePathToIndexMd(t *testing.T) {
	// Arrange
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeWorkflowIndex(t, mosaicRoot, nil)
	wantIndexPath := filepath.Join(catRoot, "Workflows", "Index.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if report.IndexPath != wantIndexPath {
		t.Errorf("IndexPath = %q, want %q", report.IndexPath, wantIndexPath)
	}
}

// TestCheckWorkflowIndex_Clean_FalseWhenOrphansExist verifies that Clean() returns false
// when the report contains at least one orphan of any class.
func TestCheckWorkflowIndex_Clean_FalseWhenOrphansExist(t *testing.T) {
	// Arrange: one file-orphan.
	mosaicRoot, catRoot := makeCatalogRootWithIndex(t, nil)
	writeWorkflowIndex(t, mosaicRoot, nil) // empty index
	writeDiskWorkflow(t, mosaicRoot, "Rho", "unlisted.md")

	// Act
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		t.Fatalf("CheckWorkflowIndex: %v", err)
	}

	// Assert
	if report.Clean() {
		t.Error("Clean() = true, want false; report contains file-orphan findings")
	}
}

// TestCheckWorkflowIndex_RealRepo_NoUnexpectedErrors verifies that CheckWorkflowIndex runs
// against the real MOSAIC repository without returning an error. This is a basic smoke test
// that the function can navigate the real filesystem layout.
func TestCheckWorkflowIndex_RealRepo_NoUnexpectedErrors(t *testing.T) {
	cat := loadRealCatalog(t)

	// Act
	_, err := catalog.CheckWorkflowIndex(cat.CatalogRoot())

	// Assert
	if err != nil {
		t.Fatalf("CheckWorkflowIndex against real repo returned error: %v", err)
	}
}
