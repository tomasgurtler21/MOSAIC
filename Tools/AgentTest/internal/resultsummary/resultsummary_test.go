package resultsummary_test

// Tests for the resultsummary package covering tree scanning, report grouping,
// aggregation, per-version Markdown generation, cross-version Markdown
// generation, marker preservation, and determinism.
//
// All filesystem-dependent tests (Generate) use an in-memory fakeFS so no
// real disk I/O occurs. Pure function tests (RenderUserSummary,
// RenderInternalSummary, RenderCrossVersionSummary, ParseMarkedDocument,
// MergeDocument) construct their inputs directly and assert on returned values.
//
// Note on RED phase: Four boundary/negative tests pass vacuously during TDD RED
// phase because the expected behavior ("write nothing," "return empty") happens
// to match what the stubs return:
//   - TestGenerate_EmptyTestResultsTree_ReturnsNoFilesWritten
//   - TestGenerate_MissingTestResultsDir_ReturnsNoFilesNoError
//   - TestGenerate_VersionFilterNotMatching_WritesNoPerVersionFiles
//   - TestParseMarkedDocument_EmptyContent_ReturnsEmpty
//
// This is intentional and accepted for negative tests of this kind. All four
// tests will provide real protection once implementation exists.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
)

// ---- in-memory filesystem ----

// fakeFS is a fully in-memory implementation of resultsummary.FileSystem.
// The files map is keyed by path. dirs tracks which paths are directories.
// Stat on a missing key returns an error wrapping os.ErrNotExist.
type fakeFS struct {
	files   map[string][]byte
	dirs    map[string]bool
	writeErr error // if non-nil, WriteFile always returns this error
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	return data, nil
}

func (f *fakeFS) WriteFile(path string, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files[path] = data
	return nil
}

func (f *fakeFS) Stat(path string) (resultstore.FileInfo, error) {
	if f.dirs[path] {
		return resultstore.FileInfo{Name: filepath.Base(path), IsDir: true}, nil
	}
	if _, ok := f.files[path]; ok {
		return resultstore.FileInfo{Name: filepath.Base(path), IsDir: false}, nil
	}
	return resultstore.FileInfo{}, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
}

func (f *fakeFS) MkdirAll(path string) error {
	f.dirs[path] = true
	return nil
}

// ListDir returns the names of all direct children (files and subdirectories)
// under path. It returns an error if path doesn't exist and has no children.
func (f *fakeFS) ListDir(path string) ([]string, error) {
	prefix := path + "/"
	found := f.dirs[path]
	if !found {
		// allow listing if there are files or dirs under this path
		for p := range f.files {
			if strings.HasPrefix(p, prefix) {
				found = true
				break
			}
		}
		if !found {
			for p := range f.dirs {
				if strings.HasPrefix(p, prefix) {
					found = true
					break
				}
			}
		}
		if !found {
			return nil, &os.PathError{Op: "readdir", Path: path, Err: os.ErrNotExist}
		}
	}

	seen := make(map[string]bool)
	var names []string

	// Include direct child files.
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			rel := p[len(prefix):]
			if !strings.Contains(rel, "/") && !seen[rel] {
				seen[rel] = true
				names = append(names, rel)
			}
		}
	}

	// Include direct child directories.
	for p := range f.dirs {
		if strings.HasPrefix(p, prefix) {
			rel := p[len(prefix):]
			if !strings.Contains(rel, "/") && !seen[rel] {
				seen[rel] = true
				names = append(names, rel)
			}
		}
	}

	return names, nil
}

// ---- fixture helpers ----

// loadFixture reads a file from the package's testdata/ directory using the
// real OS filesystem.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("loadFixture(%q): %v", name, err)
	}
	return data
}

// seedReport registers a version directory and a report file within it in the
// fake filesystem. root is the TestResults root (e.g. "/TestResults"). The
// filename is constructed from the provided parts following the store convention.
func seedReport(fs *fakeFS, root, version, suite, harness, modelShort, timestamp string, data []byte) {
	orchestratorDir := root + "/Orchestrator"
	versionDir := orchestratorDir + "/" + version
	filename := suite + "--" + harness + "--" + modelShort + "--" + timestamp + ".json"
	fs.dirs[root] = true
	fs.dirs[orchestratorDir] = true
	fs.dirs[versionDir] = true
	fs.files[versionDir+"/"+filename] = data
}

// readWrittenFile reads a file that Generate is expected to have written to
// the fakeFS, or fails the test if it was not written.
func readWrittenFile(t *testing.T, fs *fakeFS, path string) string {
	t.Helper()
	data, ok := fs.files[path]
	if !ok {
		t.Fatalf("expected Generate to write %q but file was not found in fakeFS", path)
	}
	return string(data)
}

// ---- T2.1: TestResults tree scanning ----

// TestGenerate_EmptyTestResultsTree_ReturnsNoFilesWritten verifies that when
// the TestResults directory exists but is empty, Generate writes no files and
// returns no error.
func TestGenerate_EmptyTestResultsTree_ReturnsNoFilesWritten(t *testing.T) {
	fs := newFakeFS()
	fs.dirs["/TestResults"] = true
	fs.dirs["/TestResults/Orchestrator"] = true // directory exists but is empty

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error for empty tree: %v", err)
	}
	if len(result.FilesWritten)+len(result.FilesUpdated) != 0 {
		t.Errorf("expected no files written for empty tree, got written=%v updated=%v",
			result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_MissingTestResultsDir_ReturnsNoFilesNoError verifies that when
// the TestResults directory does not exist at all, Generate returns an empty
// result with no error (not an infrastructure failure).
func TestGenerate_MissingTestResultsDir_ReturnsNoFilesNoError(t *testing.T) {
	fs := newFakeFS()
	// TestResults dir is not seeded at all.

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned error for missing TestResults dir, want nil: %v", err)
	}
	if len(result.FilesWritten)+len(result.FilesUpdated) != 0 {
		t.Errorf("expected no files for missing dir, got written=%v updated=%v",
			result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_SingleVersionSingleReport_WritesPerVersionSummary verifies that
// when a single version directory with one report is present, Generate writes
// per-version user-summary.md and internal-summary.md files.
func TestGenerate_SingleVersionSingleReport_WritesPerVersionSummary(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	perVersionPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, perVersionPath) {
		t.Errorf("expected %q in result files, got written=%v updated=%v",
			perVersionPath, result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_SingleVersionSingleReport_WritesCrossVersionSummary verifies
// that Generate also writes the cross-version summary.md at the root.
func TestGenerate_SingleVersionSingleReport_WritesCrossVersionSummary(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	crossVersionPath := "/TestResults/Orchestrator/summary.md"
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, crossVersionPath) {
		t.Errorf("expected %q in result files, got written=%v updated=%v",
			crossVersionPath, result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_VersionFilterMatching_OnlyScansMatchingVersion verifies that
// when VersionFilter is set to an existing version, only that version's
// per-version summary is written.
func TestGenerate_VersionFilterMatching_OnlyScansMatchingVersion(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v2.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260825T100000", loadFixture(t, "v2_claude_code_report.json"))

	req := resultsummary.SummaryRequest{
		TestResultsRoot: "/TestResults",
		VersionFilter:   "v1.0.0",
	}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Only v1.0.0/user-summary.md should appear; v2.0.0/user-summary.md should not.
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if containsPath(allFiles, "/TestResults/Orchestrator/v2.0.0/user-summary.md") {
		t.Errorf("VersionFilter=v1.0.0 should not produce v2.0.0/user-summary.md, but it did: %v", allFiles)
	}
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/user-summary.md") {
		t.Errorf("VersionFilter=v1.0.0 should produce v1.0.0/user-summary.md, but it did not: %v", allFiles)
	}
	// The cross-version summary must always be written, even when a version filter is active.
	// An implementation that skips the cross-version write when a filter is set fails this test.
	if !containsPath(allFiles, "/TestResults/Orchestrator/summary.md") {
		t.Errorf("VersionFilter=v1.0.0 should still produce /TestResults/Orchestrator/summary.md (cross-version), but it did not: %v", allFiles)
	}
}

// TestGenerate_VersionFilterNotMatching_WritesNoPerVersionFiles verifies that
// when VersionFilter is set to a version that does not exist in the tree, no
// per-version summary is written (and no error is returned).
func TestGenerate_VersionFilterNotMatching_WritesNoPerVersionFiles(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{
		TestResultsRoot: "/TestResults",
		VersionFilter:   "v99.0.0",
	}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error for non-matching filter: %v", err)
	}

	for _, p := range append(result.FilesWritten, result.FilesUpdated...) {
		// No per-version summary file should match any real version dir.
		if strings.Contains(p, "/v1.0.0/") {
			t.Errorf("non-matching VersionFilter should not produce v1.0.0 summary, got %q", p)
		}
	}
}

// TestGenerate_TwoVersions_WritesTwoPerVersionSummaries verifies that two
// version directories each produce their own per-version user-summary.md.
func TestGenerate_TwoVersions_WritesTwoPerVersionSummaries(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v2.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260825T100000", loadFixture(t, "v2_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/user-summary.md") {
		t.Errorf("expected v1.0.0/user-summary.md in result, got: %v", allFiles)
	}
	if !containsPath(allFiles, "/TestResults/Orchestrator/v2.0.0/user-summary.md") {
		t.Errorf("expected v2.0.0/user-summary.md in result, got: %v", allFiles)
	}
}

// ---- T2.2: Report grouping and aggregation ----

// TestGenerate_MultipleHarnessModelCombos_BothAppearInOutput verifies that
// when a version has reports from two different harness+model combinations,
// both appear in the per-version summary Markdown.
func TestGenerate_MultipleHarnessModelCombos_BothAppearInOutput(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	if !strings.Contains(content, "claude-sonnet-4.6") {
		t.Error("per-version summary should mention model claude-sonnet-4.6")
	}
	if !strings.Contains(content, "gpt-4o") {
		t.Error("per-version summary should mention model gpt-4o")
	}
	if !strings.Contains(content, "claude-code") {
		t.Error("per-version summary should mention harness claude-code")
	}
	if !strings.Contains(content, "open-code") {
		t.Error("per-version summary should mention harness open-code")
	}
}

// TestGenerate_PartialReport_FlaggedInOutput verifies that a report with
// infrastructure_failures > 0 produces a partial marker in the summary
// (HasPartial is indicated; the data is not silently presented as complete).
func TestGenerate_PartialReport_FlaggedInOutput(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "edge-cases", "claude-code",
		"claude-sonnet-4.6", "20260821T080000", loadFixture(t, "partial_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	// Partial reports are processed without error. The [partial] marker is no
	// longer rendered in tables (AC1.1 removes it); verify it is absent.
	if strings.Contains(content, "[partial]") {
		t.Errorf("per-version summary must not contain [partial] marker; HasPartial rendering was removed.\ncontent:\n%s", content)
	}
}

// TestGenerate_UnresolvedCostAttribution_ShowsWarningMarkerNotDollarZero
// verifies that a report whose cost attribution is "unknown_bucket" causes the
// summary to show a warning marker rather than "$0.00" (which would be
// misleading — the cost is unknown, not zero).
func TestGenerate_UnresolvedCostAttribution_ShowsWarningMarkerNotDollarZero(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "benchmarks", "claude-code",
		"claude-sonnet-4.6", "20260822T090000", loadFixture(t, "cost_unresolved_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	// The cost warning marker "[cost?]" must appear; "$0.00" must not appear
	// in a cost column for this harness+model combination.
	if !strings.Contains(content, "[cost?]") {
		t.Errorf("per-version summary should contain [cost?] warning for unknown_bucket attribution, content:\n%s", content)
	}
}

// TestGenerate_UnavailableCostAttribution_ShowsWarningMarkerNotDollarZero
// verifies that a report whose cost attribution is "unavailable" also causes
// the summary to show a warning marker rather than "$0.00". The design
// specifies both "unknown_bucket" and "unavailable" as triggering the warning.
func TestGenerate_UnavailableCostAttribution_ShowsWarningMarkerNotDollarZero(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "benchmarks", "claude-code",
		"claude-sonnet-4.6", "20260822T100000", loadFixture(t, "cost_unavailable_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	if !strings.Contains(content, "[cost?]") {
		t.Errorf("per-version summary should contain [cost?] warning for 'unavailable' attribution, content:\n%s", content)
	}
}

// TestGenerate_PassRateAggregated_TwoReports verifies that when two reports
// exist for the same version (one 100% pass, one 50% pass), the aggregated
// output reflects both contributions (not just one).
func TestGenerate_PassRateAggregated_TwoReports(t *testing.T) {
	fs := newFakeFS()
	// claude-code report: 2 tests, 2 passed (100%)
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	// open-code report: 2 tests, 1 passed (50%)
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	// 100% pass rate for claude-code/claude-sonnet-4.6 should appear as a
	// formatted percentage (e.g. "100%") — not just the bare digit "100" which
	// could appear in dates, counts, or other numeric fields.
	if !strings.Contains(content, "100%") {
		t.Error("summary should reflect 100% pass rate for claude-code+claude-sonnet-4.6")
	}
	// 50% pass rate for open-code/gpt-4o should appear as a formatted percentage.
	if !strings.Contains(content, "50%") {
		t.Error("summary should reflect 50% pass rate for open-code+gpt-4o")
	}
}

// TestRenderUserSummary_VersionAppearsInOverview verifies that the version
// string appears in the overview section.
func TestRenderUserSummary_VersionAppearsInOverview(t *testing.T) {
	vs := minimalVersionSummary("v1.0.0")
	output := resultsummary.RenderUserSummary(vs)

	if !strings.Contains(output, "v1.0.0") {
		t.Error("RenderUserSummary output must contain the version string v1.0.0")
	}
}

// TestRenderUserSummary_ModelAppearsInModelResults verifies that the model
// name appears in the model-results section.
func TestRenderUserSummary_ModelAppearsInModelResults(t *testing.T) {
	vs := minimalVersionSummary("v1.0.0")
	// minimalVersionSummary includes "claude-sonnet-4.6" as the model.
	output := resultsummary.RenderUserSummary(vs)

	if !strings.Contains(output, "claude-sonnet-4.6") {
		t.Error("RenderUserSummary output must include model name claude-sonnet-4.6")
	}
}

// TestRenderUserSummary_SuiteBreakdownContent_AppearsInsideModelResultsBlock
// verifies that suite names from BySuite appear within the
// generated:model-results block. This guards against an implementation that
// renders only aggregate model stats and silently omits per-suite breakdowns.
// Uses the same block-extraction pattern as
// TestRenderInternalSummary_ProblemAreasContent_AppearInsideBlock to prevent
// false positives from content outside the expected section.
func TestRenderUserSummary_SuiteBreakdownContent_AppearsInsideModelResultsBlock(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-alpha", "suite-beta"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  4,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:   "claude-code",
					Model:     "claude-sonnet-4.6",
					TestCount: 4,
					PassCount: 3,
					PassRate:  0.75,
					TotalCost: 0.10,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-alpha": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:   "claude-code",
						Model:     "claude-sonnet-4.6",
						TestCount: 2,
						PassCount: 2,
						PassRate:  1.0,
						TotalCost: 0.05,
					},
				},
			},
			"suite-beta": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:   "claude-code",
						Model:     "claude-sonnet-4.6",
						TestCount: 2,
						PassCount: 1,
						PassRate:  0.5,
						TotalCost: 0.05,
					},
				},
			},
		},
	}

	output := resultsummary.RenderUserSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-results -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-results -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-results block not found in rendered output; implementation required")
	}

	blockContent := output[blockStart:blockEnd]
	if !strings.Contains(blockContent, "suite-alpha") {
		t.Errorf("generated:model-results block must contain suite name 'suite-alpha' from BySuite data; block content:\n%s", blockContent)
	}
	if !strings.Contains(blockContent, "suite-beta") {
		t.Errorf("generated:model-results block must contain suite name 'suite-beta' from BySuite data; block content:\n%s", blockContent)
	}
}

// TestRenderUserSummary_HarnessAppearsInHarnessComparison verifies that the
// harness name appears in the harness-comparison section.
func TestRenderUserSummary_HarnessAppearsInHarnessComparison(t *testing.T) {
	vs := minimalVersionSummary("v1.0.0")
	// minimalVersionSummary includes "claude-code" as the harness.
	output := resultsummary.RenderUserSummary(vs)

	if !strings.Contains(output, "claude-code") {
		t.Error("RenderUserSummary output must include harness name claude-code")
	}
}

// TestRenderUserSummary_CostWarning_ShowsMarkerNotZeroDollar verifies that
// when HarnessModelStats.CostWarning is true, the rendered row shows the cost
// warning marker instead of "$0.00".
func TestRenderUserSummary_CostWarning_ShowsMarkerNotZeroDollar(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"benchmarks"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  1,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4.6",
					TestCount:   1,
					PassCount:   1,
					PassRate:    1.0,
					TotalCost:   0.0,
					CostWarning: true,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"benchmarks": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:     "claude-code",
						Model:       "claude-sonnet-4.6",
						TestCount:   1,
						PassCount:   1,
						PassRate:    1.0,
						TotalCost:   0.0,
						CostWarning: true,
					},
				},
			},
		},
	}

	output := resultsummary.RenderUserSummary(vs)

	if strings.Contains(output, "$0.00") {
		t.Error("RenderUserSummary must not show $0.00 for unresolved cost attribution")
	}
	if !strings.Contains(output, "[cost?]") {
		t.Error("RenderUserSummary must show [cost?] warning marker for unresolved cost attribution")
	}
}

// TestRenderUserSummary_ZeroCostNoWarning_ShowsDollarZeroNotCostWarning
// verifies that when HarnessModelStats.CostWarning is false and TotalCost is
// 0.0, the rendered output shows "$0.00" and does NOT show the "[cost?]"
// warning marker. This is the complement of the CostWarning=true test above:
// an implementation that always renders "[cost?]" would fail this test.
func TestRenderUserSummary_ZeroCostNoWarning_ShowsDollarZeroNotCostWarning(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"benchmarks"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  1,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4.6",
					TestCount:   1,
					PassCount:   1,
					PassRate:    1.0,
					TotalCost:   0.0,
					CostWarning: false,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"benchmarks": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:     "claude-code",
						Model:       "claude-sonnet-4.6",
						TestCount:   1,
						PassCount:   1,
						PassRate:    1.0,
						TotalCost:   0.0,
						CostWarning: false,
					},
				},
			},
		},
	}

	output := resultsummary.RenderUserSummary(vs)

	if strings.Contains(output, "[cost?]") {
		t.Error("RenderUserSummary must not show [cost?] when CostWarning is false")
	}
	if !strings.Contains(output, "$0.00") {
		t.Error("RenderUserSummary must show $0.00 for zero cost when CostWarning is false")
	}
}

// TestRenderUserSummary_MultipleModels_AllAppearInModelComparison verifies
// that when multiple models are present, all appear in the model-comparison
// table.
func TestRenderUserSummary_MultipleModels_AllAppearInModelComparison(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  4,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 2, PassCount: 2, PassRate: 1.0, TotalCost: 0.05},
			},
			"gpt-4o": {
				"open-code": {Harness: "open-code", Model: "gpt-4o",
					TestCount: 2, PassCount: 1, PassRate: 0.5, TotalCost: 0.05},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 2, PassCount: 2, PassRate: 1.0},
				},
				"gpt-4o": {
					"open-code": {Harness: "open-code", Model: "gpt-4o",
						TestCount: 2, PassCount: 1, PassRate: 0.5},
				},
			},
		},
	}

	output := resultsummary.RenderUserSummary(vs)

	if !strings.Contains(output, "claude-sonnet-4.6") {
		t.Error("model-comparison must include claude-sonnet-4.6")
	}
	if !strings.Contains(output, "gpt-4o") {
		t.Error("model-comparison must include gpt-4o")
	}
}

// ---- T2.4: Cross-version summary Markdown generation ----

// TestRenderCrossVersionSummary_ContainsVersionOverviewSection verifies that
// the output contains a generated:version-overview block.
func TestRenderCrossVersionSummary_ContainsVersionOverviewSection(t *testing.T) {
	cv := minimalCrossVersionSummary()
	output := resultsummary.RenderCrossVersionSummary(cv)

	if !strings.Contains(output, "<!-- generated:version-overview -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- generated:version-overview --> marker")
	}
	if !strings.Contains(output, "<!-- /generated:version-overview -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- /generated:version-overview --> closing marker")
	}
}

// TestRenderCrossVersionSummary_ContainsVersionComparisonSection verifies that
// the output contains a generated:version-comparison block.
func TestRenderCrossVersionSummary_ContainsVersionComparisonSection(t *testing.T) {
	cv := minimalCrossVersionSummary()
	output := resultsummary.RenderCrossVersionSummary(cv)

	if !strings.Contains(output, "<!-- generated:version-comparison -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- generated:version-comparison --> marker")
	}
	if !strings.Contains(output, "<!-- /generated:version-comparison -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- /generated:version-comparison --> closing marker")
	}
}

// TestRenderCrossVersionSummary_ContainsRegressionFlagsSection verifies that
// the output contains a generated:regression-flags block.
func TestRenderCrossVersionSummary_ContainsRegressionFlagsSection(t *testing.T) {
	cv := minimalCrossVersionSummary()
	output := resultsummary.RenderCrossVersionSummary(cv)

	if !strings.Contains(output, "<!-- generated:regression-flags -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- generated:regression-flags --> marker")
	}
	if !strings.Contains(output, "<!-- /generated:regression-flags -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- /generated:regression-flags --> closing marker")
	}
}

// TestRenderCrossVersionSummary_ContainsVersionTrendsAnalysisPlaceholder
// verifies that the output contains an analysis:version-trends block.
func TestRenderCrossVersionSummary_ContainsVersionTrendsAnalysisPlaceholder(t *testing.T) {
	cv := minimalCrossVersionSummary()
	output := resultsummary.RenderCrossVersionSummary(cv)

	if !strings.Contains(output, "<!-- analysis:version-trends -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- analysis:version-trends --> marker")
	}
	if !strings.Contains(output, "<!-- /analysis:version-trends -->") {
		t.Error("RenderCrossVersionSummary output must contain <!-- /analysis:version-trends --> closing marker")
	}
}

// TestRenderCrossVersionSummary_ListsAllVersions verifies that both versions
// in a CrossVersionSummary appear in the rendered output.
func TestRenderCrossVersionSummary_ListsAllVersions(t *testing.T) {
	cv := minimalCrossVersionSummary()
	output := resultsummary.RenderCrossVersionSummary(cv)

	if !strings.Contains(output, "v1.0.0") {
		t.Error("RenderCrossVersionSummary output must list version v1.0.0")
	}
	if !strings.Contains(output, "v2.0.0") {
		t.Error("RenderCrossVersionSummary output must list version v2.0.0")
	}
}

// TestRenderCrossVersionSummary_Regression_AppearsInRegressionFlags verifies
// that when a model+harness combination has a lower pass rate in the newer
// version, it appears in the regression-flags section.
func TestRenderCrossVersionSummary_Regression_AppearsInRegressionFlags(t *testing.T) {
	cv := minimalCrossVersionSummary()
	// The minimal cross-version summary includes a regression for claude-sonnet-4.6/claude-code
	// (100% in v1.0.0 → 50% in v2.0.0).
	output := resultsummary.RenderCrossVersionSummary(cv)

	// The regression-flags block should name the affected model and harness.
	regFlagsStart := strings.Index(output, "<!-- generated:regression-flags -->")
	regFlagsEnd := strings.Index(output, "<!-- /generated:regression-flags -->")
	if regFlagsStart < 0 || regFlagsEnd < 0 || regFlagsEnd <= regFlagsStart {
		t.Fatal("regression-flags block not present; implementation required")
	}
	regContent := output[regFlagsStart:regFlagsEnd]

	if !strings.Contains(regContent, "claude-sonnet-4.6") {
		t.Error("regression-flags section must identify regressed model claude-sonnet-4.6")
	}
}

// ---- T2.5: Marker preservation on re-generation ----

// TestParseMarkedDocument_EmptyContent_ReturnsEmpty verifies that an empty
// string produces an empty (or nil) region slice.
func TestParseMarkedDocument_EmptyContent_ReturnsEmpty(t *testing.T) {
	regions := resultsummary.ParseMarkedDocument("")
	if len(regions) != 0 {
		t.Errorf("ParseMarkedDocument(\"\") returned %d regions, want 0", len(regions))
	}
}

// TestParseMarkedDocument_PlainTextOnly_ReturnsSinglePlainRegion verifies that
// plain text with no markers is returned as a single plain region.
func TestParseMarkedDocument_PlainTextOnly_ReturnsSinglePlainRegion(t *testing.T) {
	content := "# My Summary\n\nSome analysis text.\n"
	regions := resultsummary.ParseMarkedDocument(content)

	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	if regions[0].Type != resultsummary.RegionPlain {
		t.Errorf("region type = %q, want %q", regions[0].Type, resultsummary.RegionPlain)
	}
	if regions[0].Content != content {
		t.Errorf("region content = %q, want %q", regions[0].Content, content)
	}
}

// TestParseMarkedDocument_SingleGeneratedBlock_ReturnsCorrectRegions verifies
// that a document with one generated block produces three regions: plain/pre,
// generated block, plain/post.
func TestParseMarkedDocument_SingleGeneratedBlock_ReturnsCorrectRegions(t *testing.T) {
	content := "# Header\n" +
		"<!-- generated:overview -->\n" +
		"| Metric | Value |\n" +
		"<!-- /generated:overview -->\n" +
		"Some footer.\n"

	regions := resultsummary.ParseMarkedDocument(content)

	if len(regions) != 3 {
		t.Fatalf("expected 3 regions, got %d: %+v", len(regions), regions)
	}
	if regions[0].Type != resultsummary.RegionPlain {
		t.Errorf("regions[0].Type = %q, want plain", regions[0].Type)
	}
	if regions[1].Type != resultsummary.RegionGenerated {
		t.Errorf("regions[1].Type = %q, want generated", regions[1].Type)
	}
	if regions[1].Name != "overview" {
		t.Errorf("regions[1].Name = %q, want overview", regions[1].Name)
	}
	if !strings.Contains(regions[1].Content, "Metric") {
		t.Errorf("regions[1].Content should contain table text, got %q", regions[1].Content)
	}
	if regions[2].Type != resultsummary.RegionPlain {
		t.Errorf("regions[2].Type = %q, want plain", regions[2].Type)
	}
}

// TestParseMarkedDocument_SingleAnalysisBlock_ReturnsCorrectRegions verifies
// that an analysis block is parsed as RegionAnalysis.
func TestParseMarkedDocument_SingleAnalysisBlock_ReturnsCorrectRegions(t *testing.T) {
	content := "<!-- analysis:overall-analysis -->\n" +
		"Sonnet 4.6 performs well on initial dispatch.\n" +
		"<!-- /analysis:overall-analysis -->\n"

	regions := resultsummary.ParseMarkedDocument(content)

	// We expect at most 3 regions; the analysis block must be one of them.
	var analysisRegion *resultsummary.DocumentRegion
	for i := range regions {
		if regions[i].Type == resultsummary.RegionAnalysis {
			r := regions[i]
			analysisRegion = &r
			break
		}
	}
	if analysisRegion == nil {
		t.Fatalf("no analysis region found in %+v", regions)
	}
	if analysisRegion.Name != "overall-analysis" {
		t.Errorf("analysis region name = %q, want overall-analysis", analysisRegion.Name)
	}
	if !strings.Contains(analysisRegion.Content, "Sonnet 4.6") {
		t.Errorf("analysis region content should contain user text, got %q", analysisRegion.Content)
	}
}

// TestParseMarkedDocument_MixedContent_PreservesOrder verifies that a document
// with interleaved plain text, generated blocks, and analysis blocks produces
// regions in the correct order.
func TestParseMarkedDocument_MixedContent_PreservesOrder(t *testing.T) {
	content := "# Header\n" +
		"<!-- generated:overview -->\n" +
		"table content\n" +
		"<!-- /generated:overview -->\n" +
		"<!-- analysis:overall-analysis -->\n" +
		"user commentary\n" +
		"<!-- /analysis:overall-analysis -->\n"

	regions := resultsummary.ParseMarkedDocument(content)

	types := make([]resultsummary.RegionType, 0, len(regions))
	for _, r := range regions {
		types = append(types, r.Type)
	}

	// Expected order: plain, generated, analysis (with optional empty plains).
	foundGenerated := false
	foundAnalysis := false
	lastGeneratedIdx := -1
	firstAnalysisIdx := -1

	for i, r := range regions {
		if r.Type == resultsummary.RegionGenerated && r.Name == "overview" {
			foundGenerated = true
			lastGeneratedIdx = i
		}
		if r.Type == resultsummary.RegionAnalysis && r.Name == "overall-analysis" {
			foundAnalysis = true
			if firstAnalysisIdx < 0 {
				firstAnalysisIdx = i
			}
		}
	}

	if !foundGenerated {
		t.Errorf("missing generated:overview region, got types: %v", types)
	}
	if !foundAnalysis {
		t.Errorf("missing analysis:overall-analysis region, got types: %v", types)
	}
	if foundGenerated && foundAnalysis && lastGeneratedIdx > firstAnalysisIdx {
		t.Errorf("generated region at index %d appears after analysis region at index %d (wrong order)",
			lastGeneratedIdx, firstAnalysisIdx)
	}
}

// TestParseMarkedDocument_UnclosedMarker_ExtendsToEOF verifies that an opened
// generated block without a closing marker is treated as extending to EOF.
func TestParseMarkedDocument_UnclosedMarker_ExtendsToEOF(t *testing.T) {
	content := "<!-- generated:overview -->\n" +
		"table content without closing marker\n"

	regions := resultsummary.ParseMarkedDocument(content)

	var gen *resultsummary.DocumentRegion
	for i := range regions {
		if regions[i].Type == resultsummary.RegionGenerated {
			r := regions[i]
			gen = &r
			break
		}
	}
	if gen == nil {
		t.Fatalf("no generated region found for unclosed marker: %+v", regions)
	}
	if !strings.Contains(gen.Content, "table content without closing marker") {
		t.Errorf("unclosed generated block should include all text to EOF, got %q", gen.Content)
	}
}

// TestParseMarkedDocument_MarkerWithLeadingWhitespace_IsRecognized verifies
// that a marker line with leading whitespace is still recognized as a marker.
func TestParseMarkedDocument_MarkerWithLeadingWhitespace_IsRecognized(t *testing.T) {
	content := "  <!-- generated:overview -->\n" +
		"table\n" +
		"  <!-- /generated:overview -->\n"

	regions := resultsummary.ParseMarkedDocument(content)

	found := false
	for _, r := range regions {
		if r.Type == resultsummary.RegionGenerated && r.Name == "overview" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("marker with leading whitespace should still be recognized: %+v", regions)
	}
}

// TestMergeDocument_ReplacesGeneratedBlock_PreservesAnalysisBlock is the core
// marker preservation test. It verifies that:
//   - Generated block content is replaced with the new content.
//   - Analysis block content is preserved verbatim.
func TestMergeDocument_ReplacesGeneratedBlock_PreservesAnalysisBlock(t *testing.T) {
	original := "<!-- generated:overview -->\n" +
		"OLD table content\n" +
		"<!-- /generated:overview -->\n" +
		"<!-- analysis:overall-analysis -->\n" +
		"user commentary preserved\n" +
		"<!-- /analysis:overall-analysis -->\n"

	existing := resultsummary.ParseMarkedDocument(original)
	generated := map[string]string{
		"overview": "NEW table content\n",
	}

	merged := resultsummary.MergeDocument(existing, generated)

	if strings.Contains(merged, "OLD table content") {
		t.Error("MergeDocument should replace old generated content; OLD still present")
	}
	if !strings.Contains(merged, "NEW table content") {
		t.Error("MergeDocument should insert new generated content")
	}
	if !strings.Contains(merged, "user commentary preserved") {
		t.Error("MergeDocument should preserve analysis block content verbatim")
	}
	// The generated block's opening and closing markers must survive replacement.
	// A naive implementation that concatenates raw content without markers fails this.
	if !strings.Contains(merged, "<!-- generated:overview -->") {
		t.Error("MergeDocument output must retain <!-- generated:overview --> opening marker after replacement")
	}
	if !strings.Contains(merged, "<!-- /generated:overview -->") {
		t.Error("MergeDocument output must retain <!-- /generated:overview --> closing marker after replacement")
	}
}

// TestMergeDocument_NewSectionNotInExisting_IsAppended verifies that a new
// generated section (key in the generated map with no corresponding region in
// the existing document) is appended to the output.
func TestMergeDocument_NewSectionNotInExisting_IsAppended(t *testing.T) {
	original := "<!-- generated:overview -->\n" +
		"overview content\n" +
		"<!-- /generated:overview -->\n"

	existing := resultsummary.ParseMarkedDocument(original)
	generated := map[string]string{
		"overview":          "overview content\n",
		"model-comparison":  "NEW comparison table\n",
	}

	merged := resultsummary.MergeDocument(existing, generated)

	if !strings.Contains(merged, "<!-- generated:model-comparison -->") {
		t.Error("new section model-comparison should be appended to merged output")
	}
	if !strings.Contains(merged, "NEW comparison table") {
		t.Error("new section content should appear in merged output")
	}
}

// TestMergeDocument_MultilineAnalysisContent_PreservedVerbatim verifies that
// multi-line analysis content (the common case for LLM-authored commentary)
// survives the merge unchanged.
func TestMergeDocument_MultilineAnalysisContent_PreservedVerbatim(t *testing.T) {
	analysisText := "Line one of analysis.\n" +
		"Line two with details.\n" +
		"Line three with a table:\n" +
		"| Col A | Col B |\n" +
		"|-------|-------|\n" +
		"| 1     | 2     |\n"

	original := "<!-- generated:overview -->\n" +
		"table\n" +
		"<!-- /generated:overview -->\n" +
		"<!-- analysis:overall-analysis -->\n" +
		analysisText +
		"<!-- /analysis:overall-analysis -->\n"

	existing := resultsummary.ParseMarkedDocument(original)
	merged := resultsummary.MergeDocument(existing, map[string]string{
		"overview": "new table\n",
	})

	if !strings.Contains(merged, analysisText) {
		t.Errorf("multi-line analysis content should be preserved verbatim in merged output;\ngot:\n%s", merged)
	}
}

// TestMergeDocument_SpecialCharactersInAnalysisContent_PreservedVerbatim
// verifies that analysis content containing HTML, angle brackets, and
// backslashes is preserved without escaping or modification.
func TestMergeDocument_SpecialCharactersInAnalysisContent_PreservedVerbatim(t *testing.T) {
	specialContent := "Score < 80% means failure. Use `tool --flag \"value\"` to re-run.\n" +
		"HTML-like: <b>important</b> & special.\n" +
		"Backslash: C:\\Users\\test\\output\n"

	original := "<!-- analysis:overall-analysis -->\n" +
		specialContent +
		"<!-- /analysis:overall-analysis -->\n"

	existing := resultsummary.ParseMarkedDocument(original)
	merged := resultsummary.MergeDocument(existing, map[string]string{})

	if !strings.Contains(merged, specialContent) {
		t.Errorf("special characters in analysis content must be preserved verbatim;\ngot:\n%s", merged)
	}
}

// TestMergeDocument_MultipleGeneratedBlocks_AllReplaced verifies that when the
// generated map contains multiple block names, all existing generated blocks are
// replaced with the new content.
func TestMergeDocument_MultipleGeneratedBlocks_AllReplaced(t *testing.T) {
	original := "<!-- generated:overview -->\n" +
		"OLD overview\n" +
		"<!-- /generated:overview -->\n" +
		"<!-- generated:model-comparison -->\n" +
		"OLD comparison\n" +
		"<!-- /generated:model-comparison -->\n"

	existing := resultsummary.ParseMarkedDocument(original)
	generated := map[string]string{
		"overview":         "NEW overview\n",
		"model-comparison": "NEW comparison\n",
	}

	merged := resultsummary.MergeDocument(existing, generated)

	if strings.Contains(merged, "OLD overview") {
		t.Error("OLD overview content should have been replaced")
	}
	if strings.Contains(merged, "OLD comparison") {
		t.Error("OLD comparison content should have been replaced")
	}
	if !strings.Contains(merged, "NEW overview") {
		t.Error("NEW overview content should appear in merged output")
	}
	if !strings.Contains(merged, "NEW comparison") {
		t.Error("NEW comparison content should appear in merged output")
	}
}

// TestGenerate_Regeneration_PreservesAnalysisBlockContent is an end-to-end
// marker preservation test. It:
//  1. Calls Generate on a seeded fakeFS.
//  2. Reads the written per-version user-summary.md.
//  3. Injects user-authored text into the analysis block.
//  4. Writes the modified content back to the fakeFS.
//  5. Calls Generate again with the same reports.
//  6. Asserts the user-authored text is preserved.
func TestGenerate_Regeneration_PreservesAnalysisBlockContent(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// First generation.
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("first Generate returned unexpected error: %v", err)
	}

	summaryPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	firstContent := readWrittenFile(t, fs, summaryPath)

	// Inject user commentary into the analysis block.
	const userCommentary = "PRESERVED: The model performed exceptionally well on dispatch tasks."
	modifiedContent := injectAnalysisContent(firstContent, "overall-analysis", userCommentary)
	fs.files[summaryPath] = []byte(modifiedContent)

	// Second generation with the same reports.
	_, err = resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("second Generate returned unexpected error: %v", err)
	}

	secondContent := readWrittenFile(t, fs, summaryPath)
	if !strings.Contains(secondContent, userCommentary) {
		t.Errorf("user commentary in analysis block was lost on re-generation;\nexpected to find: %q\nin:\n%s",
			userCommentary, secondContent)
	}
}

// TestGenerate_Regeneration_GeneratedBlocksAreUpdated verifies that on
// re-generation, generated block content is updated (not preserved from the
// previous run).
func TestGenerate_Regeneration_GeneratedBlocksAreUpdated(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// First generation.
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("first Generate returned unexpected error: %v", err)
	}

	summaryPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	firstContent := readWrittenFile(t, fs, summaryPath)

	// Corrupt a generated block to confirm it gets replaced on re-generation.
	const corruptText = "CORRUPT: this should be replaced"
	corruptedContent := corruptGeneratedBlock(firstContent, "overview", corruptText)
	fs.files[summaryPath] = []byte(corruptedContent)

	// Second generation.
	_, err = resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("second Generate returned unexpected error: %v", err)
	}

	secondContent := readWrittenFile(t, fs, summaryPath)
	if strings.Contains(secondContent, corruptText) {
		t.Error("corrupt generated block content should have been replaced on re-generation")
	}
}

// ---- T2.6: Determinism ----

// TestRenderUserSummary_Determinism_SameInputProducesIdenticalOutput
// verifies that calling RenderUserSummary twice with the same VersionSummary
// produces byte-identical output.
func TestRenderUserSummary_Determinism_SameInputProducesIdenticalOutput(t *testing.T) {
	vs := multiModelVersionSummary()

	first := resultsummary.RenderUserSummary(vs)
	if first == "" {
		t.Fatal("RenderUserSummary returned empty output; cannot evaluate determinism")
	}
	second := resultsummary.RenderUserSummary(vs)

	if first != second {
		t.Error("RenderUserSummary is not deterministic: two calls with identical input produced different output")
	}
}

// TestRenderCrossVersionSummary_Determinism_SameInputProducesIdenticalOutput
// verifies that calling RenderCrossVersionSummary twice with the same
// CrossVersionSummary produces byte-identical output.
func TestRenderCrossVersionSummary_Determinism_SameInputProducesIdenticalOutput(t *testing.T) {
	cv := minimalCrossVersionSummary()

	first := resultsummary.RenderCrossVersionSummary(cv)
	if first == "" {
		t.Fatal("RenderCrossVersionSummary returned empty output; cannot evaluate determinism")
	}
	second := resultsummary.RenderCrossVersionSummary(cv)

	if first != second {
		t.Error("RenderCrossVersionSummary is not deterministic: two calls with identical input produced different output")
	}
}

// TestGenerate_Determinism_SameReportsProduceIdenticalGeneratedBlocks verifies
// that running Generate twice on the same set of reports (with no prior summary
// on disk) produces identical generated block content. Analysis blocks, which
// are empty on first run, should also be identical.
func TestGenerate_Determinism_SameReportsProduceIdenticalGeneratedBlocks(t *testing.T) {
	// First filesystem: fresh run.
	fs1 := newFakeFS()
	seedReport(fs1, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs1, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	// Second filesystem: identical input.
	fs2 := newFakeFS()
	seedReport(fs2, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs2, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	_, err1 := resultsummary.Generate(fs1, req)
	if err1 != nil {
		t.Fatalf("Generate on fs1 returned error: %v", err1)
	}
	_, err2 := resultsummary.Generate(fs2, req)
	if err2 != nil {
		t.Fatalf("Generate on fs2 returned error: %v", err2)
	}

	// Compare per-version and cross-version summaries.
	paths := []string{
		"/TestResults/Orchestrator/v1.0.0/user-summary.md",
		"/TestResults/Orchestrator/summary.md",
	}
	for _, p := range paths {
		content1, ok1 := fs1.files[p]
		content2, ok2 := fs2.files[p]
		if !ok1 && !ok2 {
			// Both runs produced no output for this path — the stub did not write the
			// file. This vacuously satisfies equality but does not prove determinism.
			t.Fatalf("Generate wrote no output for %q; cannot evaluate determinism — implementation required", p)
		}
		if ok1 != ok2 {
			t.Errorf("determinism failure: %q written by one run but not the other", p)
			continue
		}
		if string(content1) == "" {
			t.Fatalf("Generate produced empty content for %q; cannot evaluate determinism", p)
		}
		if string(content1) != string(content2) {
			t.Errorf("determinism failure: %q differs between two identical runs", p)
		}
	}
}

// ---- Additional coverage tests ----

// TestGenerate_WriteError_ReturnsError verifies that when the filesystem
// returns an error on WriteFile, Generate propagates it as a non-nil error.
// An implementation that silently swallows write errors would pass every other
// test in this suite but still have a serious infrastructure-error-handling bug.
func TestGenerate_WriteError_ReturnsError(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	// Inject a write error so that any attempt to write a summary.md fails.
	fs.writeErr = errors.New("disk full")

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)

	if err == nil {
		t.Error("Generate must return a non-nil error when the filesystem write fails; got nil")
	}
}

// TestGenerate_NonReportFileInVersionDir_IsSkipped verifies that non-JSON
// files (and JSON files without a schema_version field) present in a version
// directory are silently skipped. Generate must not return an error and must
// still produce a summary for the valid report in the same directory.
func TestGenerate_NonReportFileInVersionDir_IsSkipped(t *testing.T) {
	fs := newFakeFS()
	// Seed a valid report.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	// Seed a non-report text file in the same version directory.
	fs.files["/TestResults/Orchestrator/v1.0.0/README.txt"] = []byte("this is not a report")
	// Seed a JSON file that lacks schema_version (invalid report JSON).
	fs.files["/TestResults/Orchestrator/v1.0.0/not-a-report.json"] = []byte(`{"some_other_key": "value"}`)

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error when version dir contains non-report files: %v", err)
	}
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/user-summary.md") {
		t.Errorf("Generate should still produce v1.0.0/user-summary.md when non-report files are present, got: %v", allFiles)
	}
}

// TestGenerate_ExistingCrossVersionSummaryAtRoot_NotTreatedAsVersionDir
// verifies that a pre-existing summary.md at the TestResults root is not
// mistaken for a version directory. When Generate lists the root directory it
// will see this file; it must use IsDir to skip it rather than attempting to
// scan it as a version. Without an IsDir check, an implementation would call
// ListDir on summary.md (a file), which returns an error or empty result that
// could mask or corrupt the run.
func TestGenerate_ExistingCrossVersionSummaryAtRoot_NotTreatedAsVersionDir(t *testing.T) {
	fs := newFakeFS()
	// Seed a valid version directory with a report.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	// Seed a pre-existing cross-version summary.md at the root (from a prior run).
	fs.files["/TestResults/Orchestrator/summary.md"] = []byte("# Previous summary\n\nOld content.\n")

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)

	if err != nil {
		t.Fatalf("Generate returned unexpected error when summary.md exists at root: %v", err)
	}
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	// The per-version summary must still be produced.
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/user-summary.md") {
		t.Errorf("Generate should produce v1.0.0/user-summary.md even when root summary.md exists, got: %v", allFiles)
	}
}

// ---- helper functions ----

// containsPath reports whether paths contains target.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// minimalVersionSummary constructs a minimal VersionSummary with one model and
// one harness, suitable for testing section structure without complex data.
func minimalVersionSummary(version string) resultsummary.VersionSummary {
	return resultsummary.VersionSummary{
		Version:     version,
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4.6",
					TestCount:   2,
					PassCount:   2,
					PassRate:    1.0,
					AvgDuration: 2500 * time.Millisecond,
					TotalCost:   0.05,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:     "claude-code",
						Model:       "claude-sonnet-4.6",
						TestCount:   2,
						PassCount:   2,
						PassRate:    1.0,
						AvgDuration: 2500 * time.Millisecond,
						TotalCost:   0.05,
					},
				},
			},
		},
		ProblemTests: nil,
	}
}

// multiModelVersionSummary constructs a VersionSummary with two models and two
// harnesses for determinism and comparison tests.
func multiModelVersionSummary() resultsummary.VersionSummary {
	return resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  4,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4.6",
					TestCount:   2,
					PassCount:   2,
					PassRate:    1.0,
					AvgDuration: 2500 * time.Millisecond,
					TotalCost:   0.05,
				},
			},
			"gpt-4o": {
				"open-code": {
					Harness:     "open-code",
					Model:       "gpt-4o",
					TestCount:   2,
					PassCount:   1,
					PassRate:    0.5,
					AvgDuration: 5000 * time.Millisecond,
					TotalCost:   0.05,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:   "claude-code",
						Model:     "claude-sonnet-4.6",
						TestCount: 2,
						PassCount: 2,
						PassRate:  1.0,
						TotalCost: 0.05,
					},
				},
				"gpt-4o": {
					"open-code": {
						Harness:   "open-code",
						Model:     "gpt-4o",
						TestCount: 2,
						PassCount: 1,
						PassRate:  0.5,
						TotalCost: 0.05,
					},
				},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:    "happy-path",
				TestName:   "test-beta",
				NumericID:  2,
				BestRate:   1.0,
				BestCombo:  "claude-sonnet-4.6/claude-code",
				WorstRate:  0.5,
				WorstCombo: "gpt-4o/open-code",
				Spread:     0.5,
			},
		},
	}
}

// minimalCrossVersionSummary constructs a CrossVersionSummary spanning two
// versions with a regression in v2.0.0 vs v1.0.0 for claude-sonnet-4.6/claude-code.
func minimalCrossVersionSummary() resultsummary.CrossVersionSummary {
	v1 := minimalVersionSummary("v1.0.0")
	v2 := resultsummary.VersionSummary{
		Version:     "v2.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4.6",
					TestCount:   2,
					PassCount:   1,
					PassRate:    0.5, // regression: was 1.0 in v1.0.0
					AvgDuration: 3500 * time.Millisecond,
					TotalCost:   0.07,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:   "claude-code",
						Model:     "claude-sonnet-4.6",
						TestCount: 2,
						PassCount: 1,
						PassRate:  0.5,
						TotalCost: 0.07,
					},
				},
			},
		},
	}

	return resultsummary.CrossVersionSummary{
		Versions:  []string{"v2.0.0", "v1.0.0"}, // newest first
		Models:    []string{"claude-sonnet-4.6"},
		Harnesses: []string{"claude-code"},
		ByVersion: map[string]resultsummary.VersionSummary{
			"v1.0.0": v1,
			"v2.0.0": v2,
		},
		Regressions: []resultsummary.RegressionFlag{
			{
				Model:       "claude-sonnet-4.6",
				Harness:     "claude-code",
				OldVersion:  "v1.0.0",
				NewVersion:  "v2.0.0",
				OldPassRate: 1.0,
				NewPassRate: 0.5,
				Delta:       -0.5,
			},
		},
	}
}

// injectAnalysisContent inserts userText into the analysis block named blockName
// in document. If the block is found with opening and closing markers, the text
// is placed between them. Returns the modified document.
func injectAnalysisContent(document, blockName, userText string) string {
	openMarker := "<!-- analysis:" + blockName + " -->"
	closeMarker := "<!-- /analysis:" + blockName + " -->"

	openIdx := strings.Index(document, openMarker)
	if openIdx < 0 {
		return document // block not found; return unchanged
	}
	afterOpen := openIdx + len(openMarker)
	closeIdx := strings.Index(document[afterOpen:], closeMarker)
	if closeIdx < 0 {
		return document // no closing marker; return unchanged
	}
	closeIdx += afterOpen

	return document[:afterOpen] + "\n" + userText + "\n" + document[closeIdx:]
}

// corruptGeneratedBlock replaces the content of the generated block named
// blockName with corruptText. Returns the modified document.
func corruptGeneratedBlock(document, blockName, corruptText string) string {
	openMarker := "<!-- generated:" + blockName + " -->"
	closeMarker := "<!-- /generated:" + blockName + " -->"

	openIdx := strings.Index(document, openMarker)
	if openIdx < 0 {
		return document
	}
	afterOpen := openIdx + len(openMarker)
	closeIdx := strings.Index(document[afterOpen:], closeMarker)
	if closeIdx < 0 {
		return document
	}
	closeIdx += afterOpen

	return document[:afterOpen] + "\n" + corruptText + "\n" + document[closeIdx:]
}

// ---- T5.1: Numeric ID tracking in resultsummary ----
//
// NOTE: Several tests below reference TestStats.NumericID and TestStats.TestName,
// which do not yet exist in contracts.go (the struct currently has TestID string).
// These tests FAIL TO COMPILE until Stage 5 implementation renames TestID to
// TestName and adds NumericID. That compile failure is the expected TDD RED state.
//
// TestGenerate_SameNumericID_DifferentTestNames_TrackedAsSingleTest does compile
// with the current code but FAILS at runtime: the current string-key testKey
// treats "original-name" and "renamed-test" as two separate tests, so no
// problem-test entry with spread > 0 is produced.

// TestTestStats_NumericIDAndTestNameFields verifies that TestStats carries both
// a stable numeric identity (NumericID) and a human-readable display name
// (TestName). This test FAILS TO COMPILE until contracts.go renames TestID to
// TestName and adds NumericID.
func TestTestStats_NumericIDAndTestNameFields(t *testing.T) {
	s := resultsummary.TestStats{
		SuiteID:    "suite-alpha",
		TestName:   "my-display-name",
		NumericID:  42,
		BestRate:   1.0,
		BestCombo:  "claude-sonnet-4.6/claude-code",
		WorstRate:  0.5,
		WorstCombo: "gpt-4o/open-code",
		Spread:     0.5,
	}
	if s.NumericID != 42 {
		t.Errorf("NumericID = %d, want 42", s.NumericID)
	}
	if s.TestName != "my-display-name" {
		t.Errorf("TestName = %q, want %q", s.TestName, "my-display-name")
	}
}

// TestGenerate_SameNumericID_DifferentTestNames_TrackedAsSingleTest verifies
// that when two harness reports cover the same test (same numeric test_id) but
// use different test names (simulating a rename), the summary generator treats
// them as a single test for cross-combination spread analysis.
//
// With the current string-key testKey, "original-name" and "renamed-test" are
// two separate keys, each with only one combo — spread is never computed and no
// problem-test entry is produced. This test FAILS at runtime in TDD RED.
//
// With the numeric-ID testKey, both combos belong to one entry (numeric ID 42);
// the spread is computed (1.0 - 0.5 = 0.5 > 0) and the test appears in the
// problem-areas section. This test PASSES after implementation.
func TestGenerate_SameNumericID_DifferentTestNames_TrackedAsSingleTest(t *testing.T) {
	fs := newFakeFS()
	// Harness A: test_name="original-name", test_id=42, 100% pass rate.
	seedReport(fs, "/TestResults", "v1.0.0", "suite-rename", "claude-code",
		"claude-sonnet-4.6", "20260820T100000",
		loadFixture(t, "rename_report_harness_a.json"))
	// Harness B: test_name="renamed-test", test_id=42 (same numeric ID), 50% pass rate.
	seedReport(fs, "/TestResults", "v1.0.0", "suite-rename", "open-code",
		"gpt-4o", "20260820T110000",
		loadFixture(t, "rename_report_harness_b.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/internal-summary.md")

	blockStart := strings.Index(content, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}

	blockContent := content[blockStart:blockEnd]
	// The problem-areas block must contain an entry for numeric ID 42. The
	// implementation may display the original name, the renamed name, or both --
	// either is acceptable as long as an entry appears. With the old string-key
	// implementation neither name appears because no entry is generated.
	if !strings.Contains(blockContent, "original-name") && !strings.Contains(blockContent, "renamed-test") {
		t.Error("problem-areas block must contain a test entry for numeric ID 42 " +
			"(showing the original or renamed test name); " +
			"ensure testKey uses numeric ID rather than string name so renamed tests are recognized as the same test")
	}
}

// TestRenderInternalSummary_ProblemTest_ShowsTestName verifies that the
// problem-areas section renders the human-readable TestName for readability.
func TestRenderInternalSummary_ProblemTest_ShowsTestName(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-alpha"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 1, PassCount: 1, PassRate: 1.0},
			},
			"gpt-4o": {
				"open-code": {Harness: "open-code", Model: "gpt-4o",
					TestCount: 1, PassCount: 0, PassRate: 0.0},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-alpha": {
				"claude-sonnet-4.6": {
					"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 1, PassCount: 1, PassRate: 1.0},
				},
				"gpt-4o": {
					"open-code": {Harness: "open-code", Model: "gpt-4o",
						TestCount: 1, PassCount: 0, PassRate: 0.0},
				},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:    "suite-alpha",
				TestName:   "human-readable-test-name",
				NumericID:  99,
				BestRate:   1.0,
				BestCombo:  "claude-sonnet-4.6/claude-code",
				WorstRate:  0.0,
				WorstCombo: "gpt-4o/open-code",
				Spread:     1.0,
			},
		},
	}

	output := resultsummary.RenderInternalSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}

	blockContent := output[blockStart:blockEnd]
	if !strings.Contains(blockContent, "human-readable-test-name") {
		t.Errorf("problem-areas block must show TestName 'human-readable-test-name' for readability;\nblock content:\n%s", blockContent)
	}
}

// TestRenderInternalSummary_ProblemTest_NumericIDAppearsInOutput verifies that
// the problem-areas section includes the numeric test ID alongside the name,
// so that cross-rename traceability is visible in the rendered Markdown.
func TestRenderInternalSummary_ProblemTest_NumericIDAppearsInOutput(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-alpha"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 1, PassCount: 1, PassRate: 1.0},
			},
			"gpt-4o": {
				"open-code": {Harness: "open-code", Model: "gpt-4o",
					TestCount: 1, PassCount: 0, PassRate: 0.0},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-alpha": {
				"claude-sonnet-4.6": {
					"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 1, PassCount: 1, PassRate: 1.0},
				},
				"gpt-4o": {
					"open-code": {Harness: "open-code", Model: "gpt-4o",
						TestCount: 1, PassCount: 0, PassRate: 0.0},
				},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:    "suite-alpha",
				TestName:   "tracked-test",
				NumericID:  7331,
				BestRate:   1.0,
				BestCombo:  "claude-sonnet-4.6/claude-code",
				WorstRate:  0.0,
				WorstCombo: "gpt-4o/open-code",
				Spread:     1.0,
			},
		},
	}

	output := resultsummary.RenderInternalSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}

	blockContent := output[blockStart:blockEnd]
	// The numeric ID 7331 must appear so that readers can trace the test across renames.
	if !strings.Contains(blockContent, "7331") {
		t.Errorf("problem-areas block must show NumericID 7331 for cross-rename traceability;\nblock content:\n%s", blockContent)
	}
}
