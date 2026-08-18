package catalog_test

// workflows_no_orphan_regression_test.go is a regression guard asserting that the normal
// catalog loading path never emits file-orphan or index-orphan issue codes.
//
// Both codes were historically produced by loadWorkflows when it read Index.md during load.
// Stage 1 removed that read: normal loading is now disk-driven and never consults Index.md.
// The two codes now belong exclusively to CheckWorkflowIndex (the on-demand check). This
// test file pins that separation so future changes cannot accidentally reintroduce the
// orphan codes into the normal load path.
//
// Mapped from plan task T2.3.

import (
	"testing"

	"mosaic-deploy/internal/catalog"
)

// TestCatalogLoad_NormalLoad_NeverEmitsFileOrphanIssue verifies that loading the catalog
// through the normal path (catalog.Load) never produces an Issue with code "file-orphan",
// even when Index.md exists and real drift might be present. The file-orphan code is the
// exclusive responsibility of CheckWorkflowIndex.
func TestCatalogLoad_NormalLoad_NeverEmitsFileOrphanIssue(t *testing.T) {
	cat := loadRealCatalog(t)

	for _, issue := range cat.Issues() {
		if issue.Code == "file-orphan" {
			t.Errorf("catalog.Load emitted issue with code %q (subject=%q); "+
				"this code must only be produced by CheckWorkflowIndex, never by normal catalog loading",
				issue.Code, issue.Subject)
		}
	}
}

// TestCatalogLoad_NormalLoad_NeverEmitsIndexOrphanIssue verifies that loading the catalog
// through the normal path never produces an Issue with code "index-orphan". The index-orphan
// code is the exclusive responsibility of CheckWorkflowIndex.
func TestCatalogLoad_NormalLoad_NeverEmitsIndexOrphanIssue(t *testing.T) {
	cat := loadRealCatalog(t)

	for _, issue := range cat.Issues() {
		if issue.Code == "index-orphan" {
			t.Errorf("catalog.Load emitted issue with code %q (subject=%q); "+
				"this code must only be produced by CheckWorkflowIndex, never by normal catalog loading",
				issue.Code, issue.Subject)
		}
	}
}

// TestCatalogLoad_SyntheticMissingIndex_NeverEmitsOrphanIssues verifies that loading a
// catalog whose Workflows/ directory contains files but no Index.md never produces
// file-orphan or index-orphan issues. This is the exact scenario that previously caused
// every workflow file to appear as a file-orphan.
func TestCatalogLoad_SyntheticMissingIndex_NeverEmitsOrphanIssues(t *testing.T) {
	// Arrange: MOSAIC root with workflow files on disk but no Index.md.
	root := makeTempMosaicRoot(t)
	writeWorkflowFile(t, root, "Alpha", "wf-a.md", "wf-a", nil)
	writeWorkflowFile(t, root, "Alpha", "wf-b.md", "wf-b", nil)
	// No Index.md is written.

	// Act
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Assert
	for _, issue := range cat.Issues() {
		if issue.Code == "file-orphan" || issue.Code == "index-orphan" {
			t.Errorf("catalog.Load with no Index.md emitted issue with code %q (subject=%q); "+
				"orphan codes must never appear in normal loading output", issue.Code, issue.Subject)
		}
	}
}

// TestCatalogLoad_SyntheticWithIndex_NeverEmitsOrphanIssues verifies that loading a catalog
// with an Index.md that does not match the disk files never produces file-orphan or
// index-orphan issues from the normal load path — even when real drift exists.
func TestCatalogLoad_SyntheticWithIndex_NeverEmitsOrphanIssues(t *testing.T) {
	// Arrange: index lists "indexed-wf", disk has "indexed-wf" and "extra-wf".
	// If orphan detection were still in loadWorkflows, "extra-wf" would be a file-orphan.
	root := makeTempMosaicRoot(t)
	writeWorkflowIndex(t, root, [][7]string{
		{"indexed-wf", "Beta", "1.0.0", "Indexed", "Desc", "Hint", "Beta/indexed-wf.md"},
	})
	writeWorkflowFile(t, root, "Beta", "indexed-wf.md", "indexed-wf", nil)
	writeWorkflowFile(t, root, "Beta", "extra-wf.md", "extra-wf", nil)

	// Act
	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Assert
	for _, issue := range cat.Issues() {
		if issue.Code == "file-orphan" || issue.Code == "index-orphan" {
			t.Errorf("catalog.Load emitted issue with code %q (subject=%q) despite drift existing; "+
				"orphan detection must be confined to CheckWorkflowIndex", issue.Code, issue.Subject)
		}
	}
}
