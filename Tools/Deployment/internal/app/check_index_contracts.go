package app

// check_index_contracts.go declares the public contract for the workflow index staleness
// check: the result types returned to callers outside the app layer, and the method
// implementation on service.

import (
	"context"

	"mosaic-deploy/internal/catalog"
)

// IndexCheckResult reports workflow index staleness to a caller outside the app layer.
// It exists so internal/cli never needs to import internal/catalog. JSON tags follow
// the lowerCamelCase convention already used by PromoteResult.
type IndexCheckResult struct {
	// CatalogRoot is the absolute catalogue root inspected.
	CatalogRoot string `json:"catalogRoot"`
	// IndexPath is the absolute path of the index file inspected.
	IndexPath string `json:"indexPath"`
	// IndexPresent is false when no index file exists.
	IndexPresent bool `json:"indexPresent"`
	// FileOrphans lists files on disk missing from the index, sorted by Subject.
	FileOrphans []IndexCheckOrphan `json:"fileOrphans,omitempty"`
	// IndexOrphans lists index rows with no file on disk, sorted by Subject.
	IndexOrphans []IndexCheckOrphan `json:"indexOrphans,omitempty"`
	// DiskCount is the number of eligible workflow files on disk.
	DiskCount int `json:"diskCount"`
	// IndexCount is the number of index rows parsed.
	IndexCount int `json:"indexCount"`
	// Clean is true when no drift was found. An absent index is clean.
	Clean bool `json:"clean"`
}

// IndexCheckOrphan is one drift finding as reported outside the app layer.
type IndexCheckOrphan struct {
	// Code is "file-orphan" or "index-orphan".
	Code string `json:"code"`
	// Subject is the relative workflow path or the index row's workflow id.
	Subject string `json:"subject"`
	// Message is the human-readable explanation.
	Message string `json:"message"`
	// Path is the absolute path of the file involved.
	Path string `json:"path"`
}

// CheckWorkflowIndex implements Service.CheckWorkflowIndex. It delegates to
// catalog.CheckWorkflowIndex using the catalogue root from the loaded catalog,
// then maps the result into app-owned types so internal/cli never imports
// internal/catalog.
func (s *service) CheckWorkflowIndex(_ context.Context) (IndexCheckResult, error) {
	catRoot := s.deps.Catalog.CatalogRoot()
	report, err := catalog.CheckWorkflowIndex(catRoot)
	if err != nil {
		return IndexCheckResult{}, err
	}

	result := IndexCheckResult{
		CatalogRoot:  report.CatalogRoot,
		IndexPath:    report.IndexPath,
		IndexPresent: report.IndexPresent,
		DiskCount:    report.DiskCount,
		IndexCount:   report.IndexCount,
		Clean:        report.Clean(),
	}

	for _, o := range report.FileOrphans {
		result.FileOrphans = append(result.FileOrphans, IndexCheckOrphan{
			Code:    o.Code,
			Subject: o.Subject,
			Message: o.Message,
			Path:    o.Path,
		})
	}
	for _, o := range report.IndexOrphans {
		result.IndexOrphans = append(result.IndexOrphans, IndexCheckOrphan{
			Code:    o.Code,
			Subject: o.Subject,
			Message: o.Message,
			Path:    o.Path,
		})
	}

	return result, nil
}
