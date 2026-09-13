// Package resultstore implements all store business logic: parsing report JSON
// files, validating them, extracting filing metadata, constructing target paths,
// copying reports, detecting duplicates, and producing summary lines.
//
// It sits at the adapter layer and imports only internal/report (for wire type
// field-name conventions), internal/domain, and the standard library.
package resultstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// FileSystem abstracts the file operations Store needs, so tests can supply an
// in-memory implementation. Every method mirrors an os or filepath function; no
// business logic lives here.
type FileSystem interface {
	// ReadFile reads the named file and returns its contents.
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to the named file, creating it if it does not exist
	// and truncating it if it does. It creates any missing parent directories.
	WriteFile(path string, data []byte) error

	// Stat returns file info for the named file. Returns an error wrapping
	// os.ErrNotExist when the file does not exist.
	Stat(path string) (FileInfo, error)

	// MkdirAll creates a directory path and all parents that do not exist.
	MkdirAll(path string) error

	// ListDir returns the names of all entries in the named directory. Returns
	// an empty slice (not an error) if the directory is empty. Returns an error
	// if the directory does not exist.
	ListDir(path string) ([]string, error)
}

// FileInfo carries the subset of os.FileInfo that Store inspects.
type FileInfo struct {
	Name  string
	IsDir bool
}

// StoreRequest configures a single Store invocation.
type StoreRequest struct {
	// TestResultsRoot is the absolute path to the OrchestrationTestResults/ directory.
	TestResultsRoot string

	// ReportFiles is the list of report file paths to process.
	ReportFiles []string
}

// StoreFromPathsRequest is the higher-level input accepted by StoreFromPaths,
// carrying both the file list and directory options that the CLI and TUI present
// to the user.
type StoreFromPathsRequest struct {
	// TestResultsRoot is the absolute path to the OrchestrationTestResults/ directory.
	TestResultsRoot string

	// Files is the list of report file paths. Mutually exclusive with Dir.
	Files []string

	// Dir is the directory to scan for report files. Mutually exclusive with Files.
	Dir string
}

// StoreResult reports what Store did. Skip/refuse conditions are recorded here,
// not as errors.
type StoreResult struct {
	// Stored is the number of reports successfully filed.
	Stored int

	// Reports contains one entry per input file, in input order.
	Reports []StoredReport

	// Counts for the summary line.
	SkippedNonReport int
	SkippedUnknown   int
	SkippedDuplicate int
}

// SummaryLine returns the human-readable summary, e.g.
// "Stored 12 reports (4 skipped: 2 non-report, 1 unknown version, 1 duplicate)".
// When there are no skips, it returns "Stored N reports".
// The count is always plural ("N reports") regardless of N.
func (r StoreResult) SummaryLine() string {
	total := r.SkippedNonReport + r.SkippedUnknown + r.SkippedDuplicate
	if total == 0 {
		return fmt.Sprintf("Stored %d reports", r.Stored)
	}

	var parts []string
	if r.SkippedNonReport > 0 {
		parts = append(parts, fmt.Sprintf("%d non-report", r.SkippedNonReport))
	}
	if r.SkippedUnknown > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown version", r.SkippedUnknown))
	}
	if r.SkippedDuplicate > 0 {
		parts = append(parts, fmt.Sprintf("%d duplicate", r.SkippedDuplicate))
	}
	return fmt.Sprintf("Stored %d reports (%d skipped: %s)",
		r.Stored, total, strings.Join(parts, ", "))
}

// StoredReport records the outcome for one input report file.
type StoredReport struct {
	// SourcePath is the original file path.
	SourcePath string

	// TargetPath is where the report was filed (empty if skipped).
	TargetPath string

	// Skipped is true when the report was not filed.
	Skipped bool

	// SkipReason explains why the report was skipped (zero value when not skipped).
	SkipReason SkipReason

	// Message is a human-readable explanation.
	Message string
}

// SkipReason names why a report was not filed.
type SkipReason string

const (
	SkipNone           SkipReason = ""
	SkipNotReport      SkipReason = "non_report"
	SkipUnknownVersion SkipReason = "unknown_version"
	SkipDuplicate      SkipReason = "duplicate"
)

// ParsedReport is the validated, metadata-extracted view of one report JSON
// file. Exported so resultsummary can reuse it for reading stored reports.
type ParsedReport struct {
	// Raw is the full parsed wire-format report.
	Raw ReportWire

	// Metadata extracted for filing.
	SubjectVersion string
	SuiteID        string
	HarnessID      string
	SubjectModel   string
	ModelShort     string
	Timestamp      time.Time // from Raw.StartedAt
}

// ReportWire is the minimal subset of the report JSON wire format that store
// and summary need. It mirrors report/json.go's wireResult shape for the
// relevant fields only.
type ReportWire struct {
	SchemaVersion          string           `json:"schema_version"`
	SuiteID                string           `json:"suite_id"`
	StartedAt              time.Time        `json:"started_at"`
	FinishedAt             time.Time        `json:"finished_at"`
	Tests                  []TestReportWire `json:"tests"`
	Counts                 map[string]int   `json:"counts"`
	TotalCost              CostWire         `json:"total_cost"`
	InfrastructureFailures int              `json:"infrastructure_failures"`
	// ToolVersion is the version of mosaic-agent-test that produced this report.
	// Additive-only: omitempty ensures pre-feature report JSON (lacking the field)
	// deserializes with empty string rather than causing an error.
	ToolVersion            string           `json:"tool_version,omitempty"`
}

// TestReportWire is the minimal wire shape for one test entry.
type TestReportWire struct {
	TestName    string          `json:"test_name"` // human-readable display name (renamed from string test_id)
	TestID      int             `json:"test_id"`   // stable numeric identity
	Description string          `json:"description"`
	Layer       string          `json:"layer"`
	Aggregate   AggregateWire   `json:"aggregate"`
	Runs        []RunReportWire `json:"runs"`
}

// RunKeyWire holds run identification fields from an exclusion entry.
// The exact sub-fields are an implementation detail; Reason, TerminationReason,
// and Detail on ExclusionWire are the fields consumed by the rendering layer.
type RunKeyWire struct {
	RunID     string `json:"run_id"`
	TestName  string `json:"test_name"`
	RunNumber int    `json:"run_number"`
}

// ExclusionWire is the wire shape for one excluded run, deserialized from
// stored report JSON at test.aggregate.exclusions[]. Absent in older stored
// reports (the parent slice decodes to nil -- backward compatible).
type ExclusionWire struct {
	Key               RunKeyWire `json:"key"`
	Reason            string     `json:"reason"`
	TerminationReason string     `json:"termination_reason"`
	Detail            string     `json:"detail"`
}

// AggregateWire is the minimal wire shape for aggregate stats.
type AggregateWire struct {
	Verdict               string          `json:"verdict"`
	Counted               int             `json:"counted"`
	Passed                int             `json:"passed"`
	PassRate              float64         `json:"pass_rate"`
	InfrastructureFailure bool            `json:"infrastructure_failure"`
	TotalCost             CostWire        `json:"total_cost"`
	// Excluded is the number of runs excluded from the denominator.
	// Absent in older report JSON; decodes to 0 (backward compatible).
	Excluded int `json:"excluded"`
	// Exclusions is the per-exclusion detail array. Absent in older stored
	// reports (decodes to nil -- backward compatible). Additive-only.
	Exclusions []ExclusionWire `json:"exclusions"`
}

// RunReportWire is the minimal wire shape for one run entry.
type RunReportWire struct {
	SubjectVersion    string          `json:"subject_version"`
	SubjectModel      string          `json:"subject_model"`
	HarnessID         string          `json:"harness_id"`
	Verdict           string          `json:"verdict"`
	DurationMS        int64           `json:"duration_ms"`
	Cost              CostWire        `json:"cost"`
	TerminationReason string          `json:"termination_reason"`
	Conditions        []ConditionWire `json:"conditions"`
	TestVersion       int             `json:"test_version"` // content version; zero until wire field is populated
}

// CostWire is the minimal wire shape for cost data.
type CostWire struct {
	TotalUSD    float64 `json:"total_usd"`
	Attribution string  `json:"attribution"`
}

// ConditionWire is the minimal wire shape for a run condition.
type ConditionWire struct {
	Kind string `json:"kind"`
}

// ModelShort strips the provider prefix from a model identifier.
// "github-copilot/claude-sonnet-4.6" becomes "claude-sonnet-4.6".
// A model with no "/" is returned unchanged.
func ModelShort(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// ParseAndValidate reads a report file, parses it into the wire shape, and
// validates it has the required fields (schema_version, suite_id, tests).
// Returns the parsed report and extracted metadata, or an error describing why
// validation failed.
//
// This function is exported so resultsummary can reuse the same parsing logic
// rather than duplicating it.
func ParseAndValidate(data []byte) (ParsedReport, error) {
	var wire ReportWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return ParsedReport{}, fmt.Errorf("invalid JSON: %w", err)
	}

	if wire.SchemaVersion == "" {
		return ParsedReport{}, errors.New("missing required field: schema_version")
	}
	if wire.SuiteID == "" {
		return ParsedReport{}, errors.New("missing required field: suite_id")
	}
	if len(wire.Tests) == 0 {
		return ParsedReport{}, errors.New("missing required field: tests (empty or absent)")
	}
	if len(wire.Tests[0].Runs) == 0 {
		return ParsedReport{}, errors.New("tests[0].runs is empty: cannot extract filing metadata")
	}

	run0 := wire.Tests[0].Runs[0]
	ms := ModelShort(run0.SubjectModel)

	return ParsedReport{
		Raw:            wire,
		SubjectVersion: run0.SubjectVersion,
		SuiteID:        wire.SuiteID,
		HarnessID:      run0.HarnessID,
		SubjectModel:   run0.SubjectModel,
		ModelShort:     ms,
		Timestamp:      wire.StartedAt,
	}, nil
}

// Store processes one or more report files according to req and returns a result
// summarising what happened. It never returns an error for skip/refuse
// conditions; those are recorded in StoreResult. It returns an error only for
// infrastructure failures.
//
// Store never moves or deletes original files. It copies reports to the target path.
func Store(fs FileSystem, req StoreRequest) (StoreResult, error) {
	var result StoreResult

	for _, srcPath := range req.ReportFiles {
		data, err := fs.ReadFile(srcPath)
		if err != nil {
			return result, fmt.Errorf("reading report file %q: %w", srcPath, err)
		}

		parsed, parseErr := ParseAndValidate(data)
		if parseErr != nil {
			result.SkippedNonReport++
			result.Reports = append(result.Reports, StoredReport{
				SourcePath: srcPath,
				Skipped:    true,
				SkipReason: SkipNotReport,
				Message:    parseErr.Error(),
			})
			continue
		}

		// Refuse reports with subject_version == "unknown" per design spec.
		if parsed.SubjectVersion == "unknown" {
			result.SkippedUnknown++
			result.Reports = append(result.Reports, StoredReport{
				SourcePath: srcPath,
				Skipped:    true,
				SkipReason: SkipUnknownVersion,
				Message:    "unknown version",
			})
			continue
		}

		// Construct target path: {root}/Orchestrator/{version}/{suite}--{harness}--{model-short}--{timestamp}.json
		ts := parsed.Timestamp.UTC().Format("20060102T150405")
		filename := fmt.Sprintf("%s--%s--%s--%s.json",
			parsed.SuiteID, parsed.HarnessID, parsed.ModelShort, ts)
		versionDir := req.TestResultsRoot + "/Orchestrator/" + parsed.SubjectVersion
		targetPath := versionDir + "/" + filename

		// Detect duplicate: if a file already exists at the target path, skip.
		if _, statErr := fs.Stat(targetPath); statErr == nil {
			result.SkippedDuplicate++
			result.Reports = append(result.Reports, StoredReport{
				SourcePath: srcPath,
				Skipped:    true,
				SkipReason: SkipDuplicate,
				Message:    fmt.Sprintf("duplicate of %s", targetPath),
			})
			continue
		}

		// Create version directory if needed.
		if mkErr := fs.MkdirAll(versionDir); mkErr != nil {
			return result, fmt.Errorf("creating directory %q: %w", versionDir, mkErr)
		}

		// Copy report to target path.
		if writeErr := fs.WriteFile(targetPath, data); writeErr != nil {
			return result, fmt.Errorf("writing report to %q: %w", targetPath, writeErr)
		}

		result.Stored++
		result.Reports = append(result.Reports, StoredReport{
			SourcePath: srcPath,
			TargetPath: targetPath,
			Skipped:    false,
			SkipReason: SkipNone,
		})
	}

	return result, nil
}

// StoreFromPaths handles the Dir-or-Files resolution. When req.Dir is non-empty,
// it calls ScanDirectory to discover report files, then delegates to Store.
// When req.Dir is empty, it delegates to Store with req.Files directly.
//
// req.Dir and req.Files are mutually exclusive; StoreFromPaths returns an error
// if both are non-empty.
func StoreFromPaths(fs FileSystem, req StoreFromPathsRequest) (StoreResult, error) {
	if req.Dir != "" && len(req.Files) > 0 {
		return StoreResult{}, errors.New("--dir and positional file arguments are mutually exclusive")
	}

	files := req.Files
	if req.Dir != "" {
		found, err := ScanDirectory(fs, req.Dir)
		if err != nil {
			return StoreResult{}, err
		}
		files = found
	}

	return Store(fs, StoreRequest{
		TestResultsRoot: req.TestResultsRoot,
		ReportFiles:     files,
	})
}

// ScanDirectory finds all files in dir that look like AgentTest report JSON
// (have a top-level "schema_version" key). It returns their paths.
// Non-report JSON and non-JSON files are silently skipped.
// ScanDirectory does not recurse into subdirectories.
func ScanDirectory(fs FileSystem, dir string) ([]string, error) {
	names, err := fs.ListDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing directory %q: %w", dir, err)
	}

	var paths []string
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		fullPath := dir + "/" + name

		data, readErr := fs.ReadFile(fullPath)
		if readErr != nil {
			// Skip unreadable files silently.
			continue
		}

		// Check for the schema_version key to identify report-shaped JSON.
		var probe map[string]json.RawMessage
		if jsonErr := json.Unmarshal(data, &probe); jsonErr != nil {
			continue
		}
		if _, ok := probe["schema_version"]; ok {
			paths = append(paths, fullPath)
		}
	}
	return paths, nil
}
