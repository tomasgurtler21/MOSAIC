package report_test

// Tests for the harness version in the JSON and text reports. The field is
// additive: omitted from JSON, and absent from text, when the version is
// unknown, so output for runs without a version is unchanged.

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/report"
)

func fixtureResultWithHarnessVersion(version string) report.Result {
	r := fixtureResultWithHarnessID("claude-code")
	r.Tests[0].Runs[0].HarnessVersion = version
	return r
}

// decodeFirstRun decodes the first run object of the first test as a generic map,
// so key presence and absence can be asserted.
func decodeFirstRun(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []map[string]json.RawMessage `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or runs")
	}
	return doc.Tests[0].Runs[0]
}

func TestRenderJSON_HarnessVersionAppearsWhenKnown(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessVersion("2.1.284")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert
	run := decodeFirstRun(t, out)
	raw, ok := run["harness_version"]
	if !ok {
		t.Fatal("run has no harness_version key although the version is known")
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil || got != "2.1.284" {
		t.Errorf("harness_version = %s, want \"2.1.284\"", raw)
	}
}

func TestRenderJSON_HarnessVersionOmittedWhenUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessVersion("")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: omitted entirely, never a placeholder such as "unknown".
	if strings.Contains(string(out), "harness_version") {
		t.Errorf("JSON contains harness_version for an unknown version; it must be omitted:\n%s", out)
	}
}

func TestRenderJSON_HarnessVersionDoesNotDisturbHarnessID(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessVersion("2.1.284")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert
	if got := decodeJSONRunHarnessID(t, out); got != "claude-code" {
		t.Errorf("harness_id = %q, want %q", got, "claude-code")
	}
}

func TestRenderText_HarnessVersionLineAppearsWhenKnown(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessVersion("2.1.284")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: the version line follows the harness line.
	const want = "  harness: claude-code\n  harness version: 2.1.284\n"
	if !strings.Contains(out, want) {
		t.Errorf("terminal report lacks %q directly after the harness line\n--- got ---\n%s", want, out)
	}
}

func TestRenderText_UnchangedWhenHarnessVersionUnknown(t *testing.T) {
	// Arrange
	withoutVersion := fixtureResultWithHarnessVersion("")
	withVersion := fixtureResultWithHarnessVersion("2.1.284")

	// Act
	plain, err := renderText(t, withoutVersion)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	versioned, err := renderText(t, withVersion)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: no version line when unknown, and the only difference when known
	// is that added line.
	if strings.Contains(plain, "harness version") {
		t.Errorf("terminal report shows a harness version line for an unknown version:\n%s", plain)
	}
	if got := strings.Replace(versioned, "  harness version: 2.1.284\n", "", 1); got != plain {
		t.Errorf("known-version output differs from unknown-version output by more than the version line")
	}
}
