package resultstore_test

// A report carrying a per-run harness_version still parses and files, and the
// stored copy retains the field. The store copies the raw report bytes, so
// this guards against a future re-serialisation dropping unknown fields.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/resultstore"
)

func TestParseAndValidate_ReportWithHarnessVersion_ParsesSuccessfully(t *testing.T) {
	// Act
	parsed, err := resultstore.ParseAndValidate(loadFixture(t, "harness_version_report.json"))

	// Assert
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}
	if parsed.Raw.SuiteID != "harness-version" {
		t.Errorf("SuiteID = %q, want %q", parsed.Raw.SuiteID, "harness-version")
	}
}

func TestStore_ReportWithHarnessVersion_StoredCopyRetainsVersion(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	src := loadFixture(t, "harness_version_report.json")
	seedFile(fs, "/reports/harness_version_report.json", src)
	req := resultstore.StoreRequest{
		TestResultsRoot: "/TestResults",
		ReportFiles:     []string{"/reports/harness_version_report.json"},
	}

	// Act
	result, err := resultstore.Store(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("Store returned unexpected error: %v", err)
	}
	if len(result.Reports) != 1 || result.Reports[0].TargetPath == "" {
		t.Fatalf("want one filed report with a target path, got %+v", result.Reports)
	}
	stored, readErr := fs.ReadFile(result.Reports[0].TargetPath)
	if readErr != nil {
		t.Fatalf("could not read stored copy: %v", readErr)
	}
	var doc struct {
		Tests []struct {
			Runs []struct {
				HarnessVersion string `json:"harness_version"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatalf("stored copy is not valid JSON: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("stored copy has no runs")
	}
	if got := doc.Tests[0].Runs[0].HarnessVersion; got != "2.1.284" {
		t.Errorf("stored copy harness_version = %q, want %q", got, "2.1.284")
	}
}
