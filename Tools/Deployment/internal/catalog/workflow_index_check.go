package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ---------------------------------------------------------------------------
// Workflow index drift types
// ---------------------------------------------------------------------------

// WorkflowIndexReport is the outcome of CheckWorkflowIndex. It describes drift only; it
// carries no remediation and implies no action.
type WorkflowIndexReport struct {
	// CatalogRoot is the absolute catalogue root that was inspected.
	CatalogRoot string
	// IndexPath is the absolute path of the Index.md that was inspected, whether or not it
	// exists.
	IndexPath string
	// IndexPresent is false when no Index.md exists at IndexPath. Both orphan slices are
	// then empty: an absent index is reported as its own state, never as every workflow
	// file being a file-orphan.
	IndexPresent bool
	// FileOrphans lists workflow files present on disk but absent from the index, sorted
	// ascending by Subject.
	FileOrphans []WorkflowIndexOrphan
	// IndexOrphans lists index rows with no corresponding file on disk, sorted ascending
	// by Subject.
	IndexOrphans []WorkflowIndexOrphan
	// DiskCount is the number of eligible workflow files found on disk.
	DiskCount int
	// IndexCount is the number of rows parsed from the index; zero when IndexPresent is false.
	IndexCount int
}

// Clean reports that the check found no drift. An absent index is clean: there is nothing
// for the disk to disagree with.
func (r WorkflowIndexReport) Clean() bool {
	return len(r.FileOrphans) == 0 && len(r.IndexOrphans) == 0
}

// WorkflowIndexOrphan is one drift finding. It mirrors the Subject/Message/Path fields of
// catalog.Issue so the two remain comparable, without carrying Severity or Node — a
// staleness finding has no severity beyond its class.
type WorkflowIndexOrphan struct {
	// Code is "file-orphan" or "index-orphan", matching the historical Issue codes.
	Code string
	// Subject is the relative-to-Workflows path for a file-orphan, or the index row's
	// workflow id for an index-orphan.
	Subject string
	// Message is the human-readable explanation, worded as the historical Issue messages were.
	Message string
	// Path is the absolute path of the file involved: the existing file for a file-orphan,
	// the expected-but-absent file for an index-orphan.
	Path string
}

// ---------------------------------------------------------------------------
// CheckWorkflowIndex
// ---------------------------------------------------------------------------

// CheckWorkflowIndex compares Catalog/Workflows/Index.md against the workflow files present
// on disk under the same catalogue root and reports the drift between them. It is a
// read-only diagnostic: it never writes, regenerates, or deletes Index.md, and it is never
// invoked from the deploy, update, or workflow-update paths.
//
// catalogRoot is the catalogue root (the directory containing Workflows/), matching the value
// returned by Catalog.CatalogRoot. The workflow set considered on disk is exactly the set
// used by workflow loading: files one level below Workflows/, with a .md extension and a base
// name not starting with '_'.
//
// A missing Index.md is not an error and is not reported as drift: the returned report has
// IndexPresent false, both orphan slices empty, and Clean() returns true.
//
// A non-nil error means the check could not be performed at all (unreadable catalogue root,
// unreadable Index.md); it never signals drift.
func CheckWorkflowIndex(catalogRoot string) (WorkflowIndexReport, error) {
	wfRoot := filepath.Join(catalogRoot, "Workflows")
	indexPath := filepath.Join(wfRoot, "Index.md")

	report := WorkflowIndexReport{
		CatalogRoot: catalogRoot,
		IndexPath:   indexPath,
	}

	// Scan disk files — always, regardless of whether an index is present.
	diskFiles := scanWorkflowDiskFiles(wfRoot)
	report.DiskCount = len(diskFiles)

	// Check whether Index.md exists and is readable.
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Absent index is a clean state, not drift and not an error.
			report.IndexPresent = false
			return report, nil
		}
		// Existing but unreadable index is a hard error.
		return WorkflowIndexReport{}, fmt.Errorf("CheckWorkflowIndex: reading index: %w", err)
	}

	report.IndexPresent = true

	// Parse index rows.
	rows, _ := parseWorkflowIndex(data)
	report.IndexCount = len(rows)

	// Build a set of index-listed relative paths for fast lookup.
	indexByFile := make(map[string]indexRow, len(rows))
	for _, row := range rows {
		if row.File != "" {
			indexByFile[row.File] = row
		}
	}

	// File-orphans: disk files not referenced by any index row.
	for relPath, absPath := range diskFiles {
		if _, inIndex := indexByFile[relPath]; !inIndex {
			report.FileOrphans = append(report.FileOrphans, WorkflowIndexOrphan{
				Code:    "file-orphan",
				Subject: relPath,
				Message: "workflow file exists on disk but is not listed in Workflows/Index.md",
				Path:    absPath,
			})
		}
	}

	// Index-orphans: index rows whose referenced file is absent from disk.
	for _, row := range rows {
		if _, onDisk := diskFiles[row.File]; !onDisk {
			expectedPath := filepath.Join(wfRoot, row.File)
			report.IndexOrphans = append(report.IndexOrphans, WorkflowIndexOrphan{
				Code:    "index-orphan",
				Subject: row.ID,
				Message: "workflow listed in index has no corresponding file on disk",
				Path:    expectedPath,
			})
		}
	}

	// Sort both slices ascending by Subject for deterministic output.
	sort.Slice(report.FileOrphans, func(i, j int) bool {
		return report.FileOrphans[i].Subject < report.FileOrphans[j].Subject
	})
	sort.Slice(report.IndexOrphans, func(i, j int) bool {
		return report.IndexOrphans[i].Subject < report.IndexOrphans[j].Subject
	})

	return report, nil
}
