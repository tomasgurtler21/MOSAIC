package report_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"mosaic-agent-test/internal/report"
)

// TestRenderJSON_MatchesGoldenShape golden-tests the machine-readable report
// against the same fixture the terminal report is tested against, comparing
// decoded JSON documents rather than raw bytes so the test specifies the
// wire shape without coupling to incidental whitespace.
func TestRenderJSON_MatchesGoldenShape(t *testing.T) {
	// Arrange
	result := fixtureFullResult()

	// Act
	out, err := renderJSON(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	assertJSONMatchesGolden(t, out, "full.golden.json")
}

// TestRenderJSON_EmptySuite_KeepsShape asserts an empty suite renders the
// same top-level shape as a populated one — an empty tests array and zero
// counts, not an omitted field or a null in its place.
func TestRenderJSON_EmptySuite_KeepsShape(t *testing.T) {
	// Arrange
	result := fixtureEmptyResult()

	// Act
	out, err := renderJSON(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderJSON returned an error for an empty suite: %v", err)
	}
	assertJSONMatchesGolden(t, out, "empty.golden.json")
}

// TestRenderJSON_AllExcludedSuite_KeepsShape asserts a suite whose only
// test's every repetition was excluded still renders the same shape, with
// the recurring state-integrity fault distinctly represented as an
// infrastructure failure.
func TestRenderJSON_AllExcludedSuite_KeepsShape(t *testing.T) {
	// Arrange
	result := fixtureAllExcludedResult()

	// Act
	out, err := renderJSON(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderJSON returned an error for an all-excluded suite: %v", err)
	}
	assertJSONMatchesGolden(t, out, "all_excluded.golden.json")
}

// TestRenderJSON_CarriesSchemaVersion asserts the output names the schema
// version it was produced under, so a caller can tell which shape it is
// parsing without guessing from field presence.
func TestRenderJSON_CarriesSchemaVersion(t *testing.T) {
	// Arrange
	result := fixtureFullResult()

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded["schema_version"] != report.SchemaVersion {
		t.Errorf("schema_version = %v, want %v", decoded["schema_version"], report.SchemaVersion)
	}
}

// TestRenderJSON_DistinguishesOutcomeClasses asserts an assertion failure, a
// timeout, a state-integrity failure and a cost-attribution miss each carry
// their own reason/condition marker in the decoded document, separately
// representing the four outcome classes AC14.7 requires.
func TestRenderJSON_DistinguishesOutcomeClasses(t *testing.T) {
	// Arrange
	result := fixtureFullResult()

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert
	text := string(out)
	for _, marker := range []string{"ASSERTION", "TIMEOUT", "STATE_INTEGRITY", "cost_unattributed"} {
		if !strings.Contains(text, marker) {
			t.Errorf("machine-readable report has no marker for %q", marker)
		}
	}
}

// assertJSONMatchesGolden decodes both the actual output and the named
// golden file into generic JSON documents and compares them, so the test is
// insensitive to indentation and key order while still fully specifying the
// wire shape.
func assertJSONMatchesGolden(t *testing.T, got []byte, goldenName string) {
	t.Helper()

	var gotDoc any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}

	wantData, err := os.ReadFile("../../testdata/report/" + goldenName)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", goldenName, err)
	}
	var wantDoc any
	if err := json.Unmarshal(wantData, &wantDoc); err != nil {
		t.Fatalf("golden file %s is not valid JSON: %v", goldenName, err)
	}

	if !reflect.DeepEqual(gotDoc, wantDoc) {
		t.Errorf("machine-readable report does not match golden shape %s.\n--- got ---\n%s\n--- want ---\n%s", goldenName, got, wantData)
	}
}
