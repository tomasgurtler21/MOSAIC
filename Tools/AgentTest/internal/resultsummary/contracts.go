// Package resultsummary generates Markdown summary files from stored AgentTest
// report files in the OrchestrationTestResults/ directory tree. It is an adapter-layer
// package that reads through its FileSystem interface and writes summary.md
// files using a marker-based merge strategy so user-authored analysis sections
// are preserved across regenerations.
//
// This file defines the public data types (DTOs, enums, and the FileSystem
// interface) that the package's entry points accept and return. Function
// implementations live in generate.go, render.go, and merge.go.
package resultsummary

import (
	"time"

	"mosaic-agent-test/internal/resultstore"
)

// SummaryRequest configures a single Generate invocation.
type SummaryRequest struct {
	// TestResultsRoot is the absolute path to the OrchestrationTestResults/ directory.
	TestResultsRoot string

	// VersionFilter, when non-empty, restricts scanning to the named
	// version directory. When empty, all versions are scanned.
	VersionFilter string
}

// SummaryResult reports what Generate did.
type SummaryResult struct {
	// FilesWritten lists paths of newly created summary.md files.
	FilesWritten []string

	// FilesUpdated lists paths of existing summary.md files whose
	// generated blocks were refreshed.
	FilesUpdated []string
}

// SummaryFileOutcome records whether a summary file was created or updated.
type SummaryFileOutcome struct {
	Path    string
	Created bool // true = new file; false = existing file updated
}

// FileSystem abstracts file operations for summary generation.
// Intentionally parallel to resultstore.FileSystem; the composition
// root may supply the same concrete implementation to both.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	Stat(path string) (resultstore.FileInfo, error)
	MkdirAll(path string) error
	ListDir(path string) ([]string, error)
}

// RegionType names what kind of content a document region holds.
type RegionType string

const (
	RegionPlain     RegionType = "plain"
	RegionGenerated RegionType = "generated"
	RegionAnalysis  RegionType = "analysis"
)

// DocumentRegion is one contiguous section of a marked Markdown
// document. The marker parser produces a slice of these; the merge
// logic consumes them.
type DocumentRegion struct {
	Type    RegionType
	Name    string // block name from the marker (e.g. "model-comparison"); empty for plain
	Content string // the text between markers (excluding the marker lines themselves)
}

// HarnessModelStats holds aggregated metrics for one harness+model
// combination within a version (and optionally within a suite).
type HarnessModelStats struct {
	Harness     string
	Model       string
	TestCount   int
	PassCount   int
	PassRate    float64
	AvgDuration time.Duration
	TotalCost   float64
	// CostWarning is true when any contributing report has unresolved
	// cost attribution (unknown_bucket or unavailable). When true, the
	// summary renderer shows a warning marker instead of the dollar
	// amount.
	CostWarning bool
	// HasPartial is true when any contributing report has
	// infrastructure_failure entries or other markers of incomplete data.
	HasPartial bool
	// ExcludedCount is the total number of excluded runs across all tests
	// in this harness+model combination.
	ExcludedCount int
	// AttemptedCount is TestCount + ExcludedCount: the total number of runs
	// attempted before exclusions. Precomputed for rendering convenience.
	AttemptedCount int
}

// TestStats holds cross-combination stats for one test, used to
// identify problem areas.
type TestStats struct {
	SuiteID    string
	TestName   string // human-readable display name (renamed from TestID)
	NumericID  int    // stable numeric identity for cross-rename tracking
	BestRate   float64
	BestCombo  string // e.g. "claude-sonnet-4.6/claude-code"
	WorstRate  float64
	WorstCombo string
	Spread     float64 // BestRate - WorstRate
	// BestCounted is the counted sample size for the best combo.
	BestCounted int
	// BestExcluded is the excluded count for the best combo.
	BestExcluded int
	// WorstCounted is the counted sample size for the worst combo.
	WorstCounted int
	// WorstExcluded is the excluded count for the worst combo.
	WorstExcluded int
}

// ExclusionDetail holds one excluded run's detail for rendering in the
// internal report's exclusions-detail section.
type ExclusionDetail struct {
	// Suite is the suite ID from the parent report.
	Suite string
	// TestName is the human-readable test name from TestReportWire.TestName.
	TestName string
	// Reason is the exclusion reason (e.g. "spawn_failed"). Empty for older
	// reports where the field was absent.
	Reason string
	// TerminationReason is the run's termination reason. Empty when none was
	// recorded.
	TerminationReason string
	// Detail is a human-readable explanation of why the run was excluded.
	// Empty when not available.
	Detail string
}

// VersionSummary holds all aggregated data for one orchestrator version,
// ready for Markdown rendering by RenderVersionSummary.
type VersionSummary struct {
	Version     string
	ReportCount int
	Suites      []string // unique suite IDs, sorted
	Models      []string // unique model-short values, sorted
	Harnesses   []string // unique harness IDs, sorted
	TotalTests  int
	// ByModel is keyed by model-short, then by harness ID.
	ByModel map[string]map[string]HarnessModelStats
	// BySuite is keyed by suite ID, then model-short, then harness ID.
	BySuite map[string]map[string]map[string]HarnessModelStats
	// ProblemTests lists tests sorted by ascending best pass rate.
	ProblemTests []TestStats

	// InfraTests lists tests whose aggregate carries InfrastructureFailure == true,
	// sorted by suite then numeric ID for determinism. These tests are excluded
	// from ProblemTests. Same TestStats shape as ProblemTests for rendering
	// consistency.
	InfraTests []TestStats

	// ExclusionDetails lists every excluded run across all reports in this
	// version, sorted by suite then test numeric ID then exclusion order within
	// each test. Nil for versions whose stored reports predate the
	// exclusion-detail wire field.
	ExclusionDetails []ExclusionDetail
}

// RegressionFlag identifies one model+harness combination where the
// pass rate decreased between the two most recent versions.
type RegressionFlag struct {
	Model       string  // model-short
	Harness     string
	OldVersion  string
	NewVersion  string
	OldPassRate float64
	NewPassRate float64
	Delta       float64 // NewPassRate - OldPassRate (negative = regression)
}

// CrossVersionSummary holds aggregated data across all scanned versions,
// ready for Markdown rendering by RenderCrossVersionSummary.
type CrossVersionSummary struct {
	// Versions lists version strings in sorted order (newest first).
	Versions []string

	// Models lists all unique model-short values across all versions, sorted.
	Models []string

	// Harnesses lists all unique harness IDs across all versions, sorted.
	Harnesses []string

	// ByVersion is keyed by version string, giving the VersionSummary
	// for each.
	ByVersion map[string]VersionSummary

	// Regressions lists model+harness combinations whose pass rate
	// dropped between the two most recent versions.
	Regressions []RegressionFlag
}
