package resultstore_test

// Tests for the resultstore package covering report validation, metadata
// extraction, target path construction, duplicate detection, store
// orchestration, and directory scanning.
//
// All tests use an in-memory fakeFS so no real disk I/O occurs.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/resultstore"
)

// ---- in-memory filesystem ----

// fakeFS is a fully in-memory FileSystem implementation for tests.
// The files map is keyed by path. Stat on a missing key returns an
// error wrapping os.ErrNotExist.
type fakeFS struct {
	files map[string][]byte
	dirs  map[string]bool // directories that exist (created via MkdirAll)
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
	f.files[path] = data
	return nil
}

func (f *fakeFS) Stat(path string) (resultstore.FileInfo, error) {
	if _, ok := f.files[path]; ok {
		return resultstore.FileInfo{Name: filepath.Base(path), IsDir: false}, nil
	}
	if f.dirs[path] {
		return resultstore.FileInfo{Name: filepath.Base(path), IsDir: true}, nil
	}
	return resultstore.FileInfo{}, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
}

func (f *fakeFS) MkdirAll(path string) error {
	f.dirs[path] = true
	return nil
}

func (f *fakeFS) ListDir(path string) ([]string, error) {
	if !f.dirs[path] {
		// Also allow listing a dir that has files under it even without an
		// explicit MkdirAll call.
		prefix := path + "/"
		found := false
		for p := range f.files {
			if strings.HasPrefix(p, prefix) {
				found = true
				break
			}
		}
		if !found {
			return nil, &os.PathError{Op: "readdir", Path: path, Err: os.ErrNotExist}
		}
	}
	prefix := path + "/"
	seen := make(map[string]bool)
	var names []string
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			rel := p[len(prefix):]
			// Only direct children (no nested slash).
			if !strings.Contains(rel, "/") && !seen[rel] {
				seen[rel] = true
				names = append(names, rel)
			}
		}
	}
	return names, nil
}

// ---- failure-injection helpers ----

// readFailFS wraps fakeFS and injects a read error for a specific path.
// All other FileSystem operations delegate to the embedded fakeFS.
type readFailFS struct {
	*fakeFS
	failPath string
	failErr  error
}

func (r *readFailFS) ReadFile(path string) ([]byte, error) {
	if path == r.failPath {
		return nil, r.failErr
	}
	return r.fakeFS.ReadFile(path)
}

// writeFailFS wraps fakeFS and returns a sentinel error from every WriteFile
// call. All other FileSystem operations delegate to the embedded fakeFS.
type writeFailFS struct {
	*fakeFS
	failErr error
}

func (w *writeFailFS) WriteFile(path string, data []byte) error {
	return w.failErr
}

// ---- fixture helpers ----

// loadFixture reads a testdata file into bytes using the real OS.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("loadFixture(%q): %v", name, err)
	}
	return data
}

// seedFile adds data to the fake filesystem at the given path, ensuring the
// parent directory is also registered.
func seedFile(fs *fakeFS, path string, data []byte) {
	fs.files[path] = data
	fs.dirs[filepath.Dir(path)] = true
}

// ---- T1.1: ParseAndValidate – report validation ----

func TestParseAndValidate_ValidReport_ReturnsNoError(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	_, err := resultstore.ParseAndValidate(data)

	if err != nil {
		t.Errorf("expected no error for a valid report, got: %v", err)
	}
}

func TestParseAndValidate_ValidReport_HasSchemaVersion(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Raw.SchemaVersion == "" {
		t.Error("expected Raw.SchemaVersion to be non-empty")
	}
}

func TestParseAndValidate_MissingSchemaVersion_ReturnsError(t *testing.T) {
	// JSON with suite_id and tests but no schema_version.
	input := []byte(`{
		"suite_id": "happy-path",
		"tests": [{"test_id": "t1", "runs": []}]
	}`)

	_, err := resultstore.ParseAndValidate(input)

	if err == nil {
		t.Error("expected an error when schema_version is absent, got nil")
	}
}

func TestParseAndValidate_MissingSuiteID_ReturnsError(t *testing.T) {
	input := []byte(`{
		"schema_version": "1",
		"tests": [{"test_id": "t1", "runs": []}]
	}`)

	_, err := resultstore.ParseAndValidate(input)

	if err == nil {
		t.Error("expected an error when suite_id is absent, got nil")
	}
}

func TestParseAndValidate_MissingTests_ReturnsError(t *testing.T) {
	input := []byte(`{
		"schema_version": "1",
		"suite_id": "happy-path"
	}`)

	_, err := resultstore.ParseAndValidate(input)

	if err == nil {
		t.Error("expected an error when tests array is absent or empty, got nil")
	}
}

func TestParseAndValidate_InvalidJSON_ReturnsError(t *testing.T) {
	data := loadFixture(t, "invalid_json.json")

	_, err := resultstore.ParseAndValidate(data)

	if err == nil {
		t.Error("expected an error for invalid JSON, got nil")
	}
}

func TestParseAndValidate_NonReportJSON_ReturnsError(t *testing.T) {
	data := loadFixture(t, "not_a_report.json")

	_, err := resultstore.ParseAndValidate(data)

	if err == nil {
		t.Error("expected an error for JSON that is not a report, got nil")
	}
}

func TestParseAndValidate_EmptyTests_ReturnsError(t *testing.T) {
	// Explicitly empty tests array should fail — we need at least one test with
	// one run to extract filing metadata.
	input := []byte(`{
		"schema_version": "1",
		"suite_id": "happy-path",
		"tests": []
	}`)

	_, err := resultstore.ParseAndValidate(input)

	if err == nil {
		t.Error("expected an error when tests array is empty, got nil")
	}
}

func TestParseAndValidate_EmptyRunsInFirstTest_ReturnsError(t *testing.T) {
	// A report with a non-empty tests array but an empty runs array in the first
	// test entry must be rejected. Without this guard, ParseAndValidate would
	// silently return zero-value filing metadata (empty SubjectVersion, HarnessID,
	// SubjectModel, zero Timestamp) for a structurally malformed report.
	input := []byte(`{"schema_version":"1","suite_id":"s","tests":[{"test_id":"t","runs":[]}]}`)

	_, err := resultstore.ParseAndValidate(input)

	if err == nil {
		t.Error("expected an error when tests[0].runs is empty, got nil")
	}
}

// ---- T1.2: ParseAndValidate – metadata extraction ----

func TestParseAndValidate_ExtractsSubjectVersion(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.SubjectVersion != "v1.2.3" {
		t.Errorf("SubjectVersion = %q, want %q", parsed.SubjectVersion, "v1.2.3")
	}
}

func TestParseAndValidate_ExtractsSuiteID(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.SuiteID != "happy-path" {
		t.Errorf("SuiteID = %q, want %q", parsed.SuiteID, "happy-path")
	}
}

func TestParseAndValidate_ExtractsHarnessID(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.HarnessID != "claude-code" {
		t.Errorf("HarnessID = %q, want %q", parsed.HarnessID, "claude-code")
	}
}

func TestParseAndValidate_ExtractsSubjectModel(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.SubjectModel != "github-copilot/claude-sonnet-4.6" {
		t.Errorf("SubjectModel = %q, want %q", parsed.SubjectModel, "github-copilot/claude-sonnet-4.6")
	}
}

func TestParseAndValidate_ExtractsModelShort(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.ModelShort != "claude-sonnet-4.6" {
		t.Errorf("ModelShort = %q, want %q", parsed.ModelShort, "claude-sonnet-4.6")
	}
}

func TestParseAndValidate_ExtractsTimestamp(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := time.Date(2026, 8, 23, 19, 11, 26, 0, time.UTC)
	if !parsed.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", parsed.Timestamp, want)
	}
}

func TestParseAndValidate_UnknownVersionReport_ParsesSuccessfully(t *testing.T) {
	// ParseAndValidate must parse the report successfully even when
	// subject_version is "unknown". The refusal is Store's responsibility.
	data := loadFixture(t, "unknown_version_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.SubjectVersion != "unknown" {
		t.Errorf("SubjectVersion = %q, want %q", parsed.SubjectVersion, "unknown")
	}
}

// ---- T1.3: ModelShort – target path construction ----

func TestModelShort_StripsSingleProviderPrefix(t *testing.T) {
	got := resultstore.ModelShort("github-copilot/claude-sonnet-4.6")
	want := "claude-sonnet-4.6"
	if got != want {
		t.Errorf("ModelShort(%q) = %q, want %q", "github-copilot/claude-sonnet-4.6", got, want)
	}
}

func TestModelShort_StripsLastSegmentOnly_MultipleSlashes(t *testing.T) {
	// When multiple slashes exist, only the part after the last slash is kept.
	got := resultstore.ModelShort("provider/org/claude-sonnet-4.6")
	want := "claude-sonnet-4.6"
	if got != want {
		t.Errorf("ModelShort(%q) = %q, want %q", "provider/org/claude-sonnet-4.6", got, want)
	}
}

func TestModelShort_NoSlash_ReturnsUnchanged(t *testing.T) {
	got := resultstore.ModelShort("claude-sonnet-4.6")
	want := "claude-sonnet-4.6"
	if got != want {
		t.Errorf("ModelShort(%q) = %q, want %q", "claude-sonnet-4.6", got, want)
	}
}

func TestModelShort_EmptyString_ReturnsEmpty(t *testing.T) {
	got := resultstore.ModelShort("")
	if got != "" {
		t.Errorf("ModelShort(%q) = %q, want %q", "", got, "")
	}
}

func TestModelShort_TableDriven(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"github-copilot/claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"openai/gpt-4o", "gpt-4o"},
		{"anthropic/claude-3-5-sonnet", "claude-3-5-sonnet"},
		{"claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"provider/org/model-name", "model-name"},
		{"", ""},
	}
	for _, tc := range cases {
		got := resultstore.ModelShort(tc.input)
		if got != tc.want {
			t.Errorf("ModelShort(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestStore_TargetPath_HasCorrectFormat verifies the filed path follows
// {testResultsRoot}/{version}/{suite}--{harness}--{model-short}--{timestamp}.json
func TestStore_TargetPath_HasCorrectFormat(t *testing.T) {
	fs := newFakeFS()
	root := "/TestResults"
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: root,
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}
	if len(result.Reports) != 1 {
		t.Fatalf("expected 1 report entry, got %d", len(result.Reports))
	}

	want := "/TestResults/v1.2.3/happy-path--claude-code--claude-sonnet-4.6--20260823T191126.json"
	if result.Reports[0].TargetPath != want {
		t.Errorf("TargetPath = %q, want %q", result.Reports[0].TargetPath, want)
	}
}

// TestStore_TargetPath_TimestampIsISO8601Compact verifies the timestamp segment
// uses ISO 8601 compact format (YYYYMMDDTHHmmss).
func TestStore_TargetPath_TimestampIsISO8601Compact(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, _ := resultstore.Store(fs, req)
	if len(result.Reports) == 0 {
		t.Fatal("expected at least one report entry")
	}

	// The timestamp segment is 20260823T191126 — format YYYYMMDDTHHmmss.
	if !strings.Contains(result.Reports[0].TargetPath, "20260823T191126") {
		t.Errorf("TargetPath %q does not contain expected timestamp %q",
			result.Reports[0].TargetPath, "20260823T191126")
	}
}

// TestStore_TargetPath_ModelShortStripsProviderPrefix verifies the model-short
// derivation in the path (provider prefix is stripped).
func TestStore_TargetPath_ModelShortStripsProviderPrefix(t *testing.T) {
	fs := newFakeFS()
	// valid_report.json has subject_model "github-copilot/claude-sonnet-4.6"
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, _ := resultstore.Store(fs, req)
	if len(result.Reports) == 0 {
		t.Fatal("expected at least one report entry")
	}

	// Should use "claude-sonnet-4.6" not "github-copilot/claude-sonnet-4.6".
	if strings.Contains(result.Reports[0].TargetPath, "github-copilot") {
		t.Errorf("TargetPath %q should not contain provider prefix %q",
			result.Reports[0].TargetPath, "github-copilot")
	}
	if !strings.Contains(result.Reports[0].TargetPath, "claude-sonnet-4.6") {
		t.Errorf("TargetPath %q should contain model-short %q",
			result.Reports[0].TargetPath, "claude-sonnet-4.6")
	}
}

// ---- T1.4: Duplicate detection ----

// TestStore_DuplicateReport_IsSkippedWithWarning verifies that when the target
// file already exists Store skips and records a duplicate skip reason.
func TestStore_DuplicateReport_IsSkippedWithWarning(t *testing.T) {
	fs := newFakeFS()
	root := "/TestResults"
	srcPath := "/reports/valid_report.json"
	targetPath := "/TestResults/v1.2.3/happy-path--claude-code--claude-sonnet-4.6--20260823T191126.json"

	seedFile(fs, srcPath, loadFixture(t, "valid_report.json"))
	// Pre-seed the target to simulate an already-stored report.
	seedFile(fs, targetPath, loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: root,
		ReportFiles:     []string{srcPath},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error for duplicate: %v", err)
	}

	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 for a duplicate", result.Stored)
	}
	if result.SkippedDuplicate != 1 {
		t.Errorf("SkippedDuplicate = %d, want 1", result.SkippedDuplicate)
	}
	if len(result.Reports) != 1 || result.Reports[0].SkipReason != resultstore.SkipDuplicate {
		t.Errorf("expected SkipReason %q, got %q", resultstore.SkipDuplicate, result.Reports[0].SkipReason)
	}
	if !result.Reports[0].Skipped {
		t.Error("expected Skipped = true for duplicate")
	}
}

// TestStore_NonDuplicate_ProceedsToFile verifies that a report whose target
// path does not yet exist is filed normally.
func TestStore_NonDuplicate_ProceedsToFile(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	if result.Stored != 1 {
		t.Errorf("Stored = %d, want 1", result.Stored)
	}
	if result.SkippedDuplicate != 0 {
		t.Errorf("SkippedDuplicate = %d, want 0", result.SkippedDuplicate)
	}
}

// ---- T1.5: Store orchestration ----

// TestStore_ValidReport_CopiesFileToTargetPath verifies the report content
// reaches the target path unmodified.
func TestStore_ValidReport_CopiesFileToTargetPath(t *testing.T) {
	fs := newFakeFS()
	srcData := loadFixture(t, "valid_report.json")
	seedFile(fs, "/reports/valid_report.json", srcData)

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}
	if len(result.Reports) == 0 || result.Reports[0].TargetPath == "" {
		t.Fatal("expected a non-empty TargetPath in the result")
	}

	stored, readErr := fs.ReadFile(result.Reports[0].TargetPath)
	if readErr != nil {
		t.Fatalf("could not read stored file at %q: %v", result.Reports[0].TargetPath, readErr)
	}
	if string(stored) != string(srcData) {
		t.Error("stored file content does not match source")
	}
}

// TestStore_ValidReport_SourceFileIsPreserved verifies the original source file
// still exists after filing (copy, not move).
func TestStore_ValidReport_SourceFileIsPreserved(t *testing.T) {
	fs := newFakeFS()
	srcPath := "/reports/valid_report.json"
	seedFile(fs, srcPath, loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{srcPath},
	}

	_, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	if _, statErr := fs.Stat(srcPath); statErr != nil {
		t.Errorf("source file %q was removed after Store (must be copy-only)", srcPath)
	}
}

// TestStore_UnknownVersionReport_RefusesWithoutError verifies that a report
// whose subject_version is "unknown" is refused as a skip, not an error.
func TestStore_UnknownVersionReport_RefusesWithoutError(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/unknown_version.json", loadFixture(t, "unknown_version_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/unknown_version.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned an error for unknown-version report, want nil: %v", err)
	}

	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 for unknown-version report", result.Stored)
	}
	if result.SkippedUnknown != 1 {
		t.Errorf("SkippedUnknown = %d, want 1", result.SkippedUnknown)
	}
	if len(result.Reports) == 0 || result.Reports[0].SkipReason != resultstore.SkipUnknownVersion {
		t.Errorf("expected SkipReason %q, got %q", resultstore.SkipUnknownVersion, result.Reports[0].SkipReason)
	}
}

// TestStore_NonReportFile_SkipsWithNonReportReason verifies that a JSON file
// that isn't a valid report is counted as SkippedNonReport.
func TestStore_NonReportFile_SkipsWithNonReportReason(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/not_a_report.json", loadFixture(t, "not_a_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/not_a_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 for non-report", result.Stored)
	}
	if result.SkippedNonReport != 1 {
		t.Errorf("SkippedNonReport = %d, want 1", result.SkippedNonReport)
	}
	if len(result.Reports) == 0 || result.Reports[0].SkipReason != resultstore.SkipNotReport {
		t.Errorf("expected SkipReason %q, got %q", resultstore.SkipNotReport, result.Reports[0].SkipReason)
	}
}

// TestStore_CreatesVersionDirectoryIfNeeded verifies that the version directory
// is created when it does not already exist.
func TestStore_CreatesVersionDirectoryIfNeeded(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	wantDir := "/TestResults/v1.2.3"
	if !fs.dirs[wantDir] {
		t.Errorf("version directory %q was not created", wantDir)
	}
	_ = result
}

// TestStore_MultipleReports_AccumulatesAllCounts verifies that processing
// several files (stored, skipped, refused) produces correct aggregate counts.
func TestStore_MultipleReports_AccumulatesAllCounts(t *testing.T) {
	fs := newFakeFS()
	// One valid report.
	seedFile(fs, "/r/valid.json", loadFixture(t, "valid_report.json"))
	// One non-report.
	seedFile(fs, "/r/not_report.json", loadFixture(t, "not_a_report.json"))
	// One unknown-version report.
	seedFile(fs, "/r/unknown.json", loadFixture(t, "unknown_version_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles: []string{
			"/r/valid.json",
			"/r/not_report.json",
			"/r/unknown.json",
		},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}

	if result.Stored != 1 {
		t.Errorf("Stored = %d, want 1", result.Stored)
	}
	if result.SkippedNonReport != 1 {
		t.Errorf("SkippedNonReport = %d, want 1", result.SkippedNonReport)
	}
	if result.SkippedUnknown != 1 {
		t.Errorf("SkippedUnknown = %d, want 1", result.SkippedUnknown)
	}
	if result.SkippedDuplicate != 0 {
		t.Errorf("SkippedDuplicate = %d, want 0", result.SkippedDuplicate)
	}
	if len(result.Reports) != 3 {
		t.Errorf("len(Reports) = %d, want 3", len(result.Reports))
	}
}

// TestStore_ReportEntriesAreInInputOrder verifies that result.Reports preserves
// the input order of files.
func TestStore_ReportEntriesAreInInputOrder(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/r/a.json", loadFixture(t, "valid_report.json"))
	seedFile(fs, "/r/b.json", loadFixture(t, "second_valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/r/a.json", "/r/b.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}
	if len(result.Reports) != 2 {
		t.Fatalf("len(Reports) = %d, want 2", len(result.Reports))
	}

	if result.Reports[0].SourcePath != "/r/a.json" {
		t.Errorf("Reports[0].SourcePath = %q, want %q", result.Reports[0].SourcePath, "/r/a.json")
	}
	if result.Reports[1].SourcePath != "/r/b.json" {
		t.Errorf("Reports[1].SourcePath = %q, want %q", result.Reports[1].SourcePath, "/r/b.json")
	}
}

// ---- SummaryLine ----

func TestSummaryLine_AllStored_NoSkipClause(t *testing.T) {
	r := resultstore.StoreResult{Stored: 3}
	got := r.SummaryLine()
	want := "Stored 3 reports"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

func TestSummaryLine_ZeroStored_NoSkips(t *testing.T) {
	r := resultstore.StoreResult{Stored: 0}
	got := r.SummaryLine()
	want := "Stored 0 reports"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

func TestSummaryLine_WithAllSkipTypes(t *testing.T) {
	r := resultstore.StoreResult{
		Stored:           12,
		SkippedNonReport: 2,
		SkippedUnknown:   1,
		SkippedDuplicate: 1,
	}
	got := r.SummaryLine()
	want := "Stored 12 reports (4 skipped: 2 non-report, 1 unknown version, 1 duplicate)"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

func TestSummaryLine_OnlyDuplicates(t *testing.T) {
	r := resultstore.StoreResult{
		Stored:           5,
		SkippedDuplicate: 2,
	}
	got := r.SummaryLine()
	want := "Stored 5 reports (2 skipped: 2 duplicate)"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

func TestSummaryLine_OnlyNonReport(t *testing.T) {
	r := resultstore.StoreResult{
		Stored:           0,
		SkippedNonReport: 3,
	}
	got := r.SummaryLine()
	want := "Stored 0 reports (3 skipped: 3 non-report)"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

func TestSummaryLine_OnlyUnknownVersion(t *testing.T) {
	// Intentionally uses "Stored 1 reports" (always-plural). The implementation
	// must NOT add singular/plural switching — the format is always "N reports"
	// regardless of count. This mirrors the design spec examples.
	r := resultstore.StoreResult{
		Stored:         1,
		SkippedUnknown: 1,
	}
	got := r.SummaryLine()
	want := "Stored 1 reports (1 skipped: 1 unknown version)"
	if got != want {
		t.Errorf("SummaryLine() = %q, want %q", got, want)
	}
}

// ---- T1.6: StoreFromPaths / ScanDirectory ----

// TestStoreFromPaths_DirAndFiles_ReturnsError verifies the mutual exclusion
// between Dir and Files. The error is a plain error (not ExitUsage — the CLI
// layer applies that mapping).
func TestStoreFromPaths_DirAndFiles_ReturnsError(t *testing.T) {
	fs := newFakeFS()

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: "/TestResults",
		Files:           []string{"/reports/valid_report.json"},
		Dir:             "/reports",
	}

	_, err := resultstore.StoreFromPaths(fs, req)
	if err == nil {
		t.Error("expected an error when both Dir and Files are non-empty, got nil")
	}
}

// TestStoreFromPaths_DirAndFiles_ErrorIsNotExitUsage verifies that the error
// returned for the mutual-exclusion violation is a plain untyped error (not
// os.ErrNotExist or any other sentinel). The mapping of this plain error to
// ExitUsage is the CLI layer's job — this package returns a plain error only.
//
// The companion test TestStoreFromPaths_DirAndFiles_ReturnsError already
// confirms the error is non-nil. This test documents the intent that the error
// carries no special type — if that invariant matters in a future refactor,
// assert with errors.As against any known special type.
func TestStoreFromPaths_DirAndFiles_ErrorIsNotExitUsage(t *testing.T) {
	fs := newFakeFS()

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: "/TestResults",
		Files:           []string{"/reports/valid_report.json"},
		Dir:             "/reports",
	}

	_, err := resultstore.StoreFromPaths(fs, req)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	// Confirm the error is not an infrastructure error (os.ErrNotExist).
	// The error is expected to be a plain usage-level error returned directly
	// by StoreFromPaths — the CLI layer maps it to ExitUsage, not this package.
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("error should not be os.ErrNotExist (that would indicate wrong error path): %v", err)
	}
}

// TestStoreFromPaths_FilesOnly_DelegatesToStore verifies that when Dir is empty
// StoreFromPaths delegates to Store with the provided files.
func TestStoreFromPaths_FilesOnly_DelegatesToStore(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: "/TestResults",
		Files:           []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.StoreFromPaths(fs, req)
	if err != nil {
		t.Fatalf("StoreFromPaths returned unexpected error: %v", err)
	}
	if result.Stored != 1 {
		t.Errorf("Stored = %d, want 1", result.Stored)
	}
}

// TestStoreFromPaths_DirOnly_ScansAndStores verifies that when Dir is set and
// Files is empty, ScanDirectory is used to find reports and they are filed.
func TestStoreFromPaths_DirOnly_ScansAndStores(t *testing.T) {
	fs := newFakeFS()
	// Seed the scan directory with one valid report and one non-report.
	seedFile(fs, "/scan-dir/valid_report.json", loadFixture(t, "valid_report.json"))
	seedFile(fs, "/scan-dir/not_a_report.json", loadFixture(t, "not_a_report.json"))
	fs.dirs["/scan-dir"] = true

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: "/TestResults",
		Dir:             "/scan-dir",
	}

	result, err := resultstore.StoreFromPaths(fs, req)
	if err != nil {
		t.Fatalf("StoreFromPaths returned unexpected error: %v", err)
	}
	if result.Stored != 1 {
		t.Errorf("Stored = %d, want 1 (the non-report should be silently skipped)", result.Stored)
	}
}

// TestScanDirectory_FindsReportShapedJSON verifies that ScanDirectory returns
// paths for JSON files that have a schema_version field.
func TestScanDirectory_FindsReportShapedJSON(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/scan-dir/report1.json", loadFixture(t, "valid_report.json"))
	seedFile(fs, "/scan-dir/report2.json", loadFixture(t, "second_valid_report.json"))
	seedFile(fs, "/scan-dir/other.json", loadFixture(t, "not_a_report.json"))
	fs.dirs["/scan-dir"] = true

	paths, err := resultstore.ScanDirectory(fs, "/scan-dir")
	if err != nil {
		t.Fatalf("ScanDirectory returned unexpected error: %v", err)
	}

	// We expect exactly the two report-shaped files.
	if len(paths) != 2 {
		t.Errorf("ScanDirectory returned %d paths, want 2", len(paths))
	}
	for _, p := range paths {
		if strings.Contains(p, "other.json") {
			t.Errorf("ScanDirectory returned non-report file %q", p)
		}
	}
}

// TestScanDirectory_SilentlySkipsNonReportJSON verifies that non-report JSON
// files produce no error and are absent from the returned slice.
func TestScanDirectory_SilentlySkipsNonReportJSON(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/scan-dir/not_a_report.json", loadFixture(t, "not_a_report.json"))
	seedFile(fs, "/scan-dir/invalid.json", loadFixture(t, "invalid_json.json"))
	fs.dirs["/scan-dir"] = true

	paths, err := resultstore.ScanDirectory(fs, "/scan-dir")
	if err != nil {
		t.Fatalf("ScanDirectory returned unexpected error: %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("ScanDirectory returned %d paths for a directory with no reports, want 0", len(paths))
	}
}

// TestScanDirectory_DoesNotRecurse verifies that ScanDirectory does not descend
// into subdirectories.
func TestScanDirectory_DoesNotRecurse(t *testing.T) {
	fs := newFakeFS()
	// A report in a subdirectory should not be returned.
	seedFile(fs, "/scan-dir/sub/report.json", loadFixture(t, "valid_report.json"))
	fs.dirs["/scan-dir"] = true

	paths, err := resultstore.ScanDirectory(fs, "/scan-dir")
	if err != nil {
		t.Fatalf("ScanDirectory returned unexpected error: %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("ScanDirectory returned %d paths (should not recurse into subdirectories)", len(paths))
	}
}

// TestScanDirectory_NonexistentDirectory_ReturnsError verifies that scanning
// a directory that does not exist returns an error.
func TestScanDirectory_NonexistentDirectory_ReturnsError(t *testing.T) {
	fs := newFakeFS()

	_, err := resultstore.ScanDirectory(fs, "/nonexistent-dir")
	if err == nil {
		t.Error("expected an error for a nonexistent directory, got nil")
	}
}

// ---- Skipped report fields ----

// TestStore_SkippedReport_HasEmptyTargetPath verifies that skipped reports have
// an empty TargetPath (they were not filed anywhere).
func TestStore_SkippedReport_HasEmptyTargetPath(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/r/not_a_report.json", loadFixture(t, "not_a_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/r/not_a_report.json"},
	}

	result, _ := resultstore.Store(fs, req)
	if len(result.Reports) == 0 {
		t.Fatal("expected one report entry")
	}
	if result.Reports[0].TargetPath != "" {
		t.Errorf("TargetPath = %q, want empty for a skipped report", result.Reports[0].TargetPath)
	}
}

// TestStore_SkippedReport_HasSourcePath verifies that even skipped reports
// record the original source path.
func TestStore_SkippedReport_HasSourcePath(t *testing.T) {
	fs := newFakeFS()
	srcPath := "/r/not_a_report.json"
	seedFile(fs, srcPath, loadFixture(t, "not_a_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{srcPath},
	}

	result, _ := resultstore.Store(fs, req)
	if len(result.Reports) == 0 {
		t.Fatal("expected one report entry")
	}
	if result.Reports[0].SourcePath != srcPath {
		t.Errorf("SourcePath = %q, want %q", result.Reports[0].SourcePath, srcPath)
	}
}

// TestStore_StoredReport_HasSkippedFalse verifies that successfully stored
// reports have Skipped = false.
func TestStore_StoredReport_HasSkippedFalse(t *testing.T) {
	fs := newFakeFS()
	seedFile(fs, "/r/valid.json", loadFixture(t, "valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/r/valid.json"},
	}

	result, _ := resultstore.Store(fs, req)
	if len(result.Reports) == 0 {
		t.Fatal("expected one report entry")
	}
	if result.Reports[0].Skipped {
		t.Error("Skipped = true for a successfully stored report, want false")
	}
	if result.Reports[0].SkipReason != resultstore.SkipNone {
		t.Errorf("SkipReason = %q, want %q for a stored report", result.Reports[0].SkipReason, resultstore.SkipNone)
	}
}

// ---- Timestamp edge-case ----

// TestStore_SecondReport_HasCorrectTimestampInPath ensures that a second report
// with a different timestamp gets the correct path segment.
func TestStore_SecondReport_HasCorrectTimestampInPath(t *testing.T) {
	fs := newFakeFS()
	// second_valid_report.json has started_at "2026-08-24T08:30:00Z"
	seedFile(fs, "/r/b.json", loadFixture(t, "second_valid_report.json"))

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/r/b.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}
	if len(result.Reports) == 0 {
		t.Fatal("expected one report entry")
	}

	want := "/TestResults/v1.2.3/edge-cases--open-code--gpt-4o--20260824T083000.json"
	if result.Reports[0].TargetPath != want {
		t.Errorf("TargetPath = %q, want %q", result.Reports[0].TargetPath, want)
	}
}

// ---- Infrastructure failure paths ----

// TestStore_ReadFileFailure_ReturnsError verifies that when the FileSystem
// returns an error from ReadFile for an input report file, Store propagates
// the error (infrastructure failure, not a skip). The design contract states:
// "It returns an error only for infrastructure failures (cannot read a file,
// cannot write to TestResults/)."
func TestStore_ReadFileFailure_ReturnsError(t *testing.T) {
	base := newFakeFS()
	// The file is registered in the base fakeFS so it looks like it exists,
	// but readFailFS will return an error when Store tries to read it.
	base.files["/reports/valid_report.json"] = nil
	base.dirs["/reports"] = true

	errSentinel := errors.New("disk read error")
	fs := &readFailFS{fakeFS: base, failPath: "/reports/valid_report.json", failErr: errSentinel}

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err == nil {
		t.Error("expected Store to return a non-nil error when ReadFile fails, got nil")
	}
	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 when ReadFile fails", result.Stored)
	}
}

// TestStore_WriteFileFailure_ReturnsError verifies that when the FileSystem
// returns an error from WriteFile, Store propagates the error rather than
// silently counting the report as stored or skipping it.
func TestStore_WriteFileFailure_ReturnsError(t *testing.T) {
	base := newFakeFS()
	seedFile(base, "/reports/valid_report.json", loadFixture(t, "valid_report.json"))

	errSentinel := errors.New("disk write error")
	fs := &writeFailFS{fakeFS: base, failErr: errSentinel}

	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/valid_report.json"},
	}

	result, err := resultstore.Store(fs, req)
	if err == nil {
		t.Error("expected Store to return a non-nil error when WriteFile fails, got nil")
	}
	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 when WriteFile fails", result.Stored)
	}
}

// ---- StoreFromPaths boundary: both Dir and Files empty ----

// TestStoreFromPaths_BothEmpty_ReturnsZeroAndNoError verifies that when both
// Dir and Files are empty, StoreFromPaths delegates to Store with an empty
// file list, producing StoreResult{Stored:0} and no error.
func TestStoreFromPaths_BothEmpty_ReturnsZeroAndNoError(t *testing.T) {
	fs := newFakeFS()

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: "/TestResults",
		// Dir and Files are both zero-value (empty).
	}

	result, err := resultstore.StoreFromPaths(fs, req)
	if err != nil {
		t.Fatalf("StoreFromPaths with empty Dir and Files returned unexpected error: %v", err)
	}
	if result.Stored != 0 {
		t.Errorf("Stored = %d, want 0 when no files are provided", result.Stored)
	}
}

// ---- SkipReason constant values ----

func TestSkipReason_ConstantValues(t *testing.T) {
	// Verify the string values of SkipReason constants; they appear in output
	// messages and the wire format of StoredReport.
	cases := []struct {
		constant resultstore.SkipReason
		want     string
	}{
		{resultstore.SkipNone, ""},
		{resultstore.SkipNotReport, "non_report"},
		{resultstore.SkipUnknownVersion, "unknown_version"},
		{resultstore.SkipDuplicate, "duplicate"},
	}
	for _, tc := range cases {
		if string(tc.constant) != tc.want {
			t.Errorf("SkipReason constant = %q, want %q", tc.constant, tc.want)
		}
	}
}

