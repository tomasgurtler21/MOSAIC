package report_test

import (
	"os"
	"strings"
	"testing"
)

// TestRenderText_MatchesGoldenOutput golden-tests the terminal report
// against a fixture covering a pass, an assertion failure with per-assertion
// detail, a timeout, a state-integrity failure and a cost-attribution miss.
func TestRenderText_MatchesGoldenOutput(t *testing.T) {
	// Arrange
	result := fixtureFullResult()
	golden := readGolden(t, "full.golden.txt")

	// Act
	out, err := renderText(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if out != golden {
		t.Errorf("terminal report does not match golden output.\n--- got ---\n%s\n--- want ---\n%s", out, golden)
	}
}

// TestRenderText_EmptySuite_IsWellFormed asserts an empty suite renders a
// well-formed document instead of erroring or omitting the summary section.
func TestRenderText_EmptySuite_IsWellFormed(t *testing.T) {
	// Arrange
	result := fixtureEmptyResult()
	golden := readGolden(t, "empty.golden.txt")

	// Act
	out, err := renderText(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderText returned an error for an empty suite: %v", err)
	}
	if out != golden {
		t.Errorf("empty-suite terminal report does not match golden output.\n--- got ---\n%s\n--- want ---\n%s", out, golden)
	}
}

// TestRenderText_AllExcludedSuite_IsWellFormed asserts a suite whose only
// test's every repetition was excluded still renders a well-formed
// document, and that the recurring state-integrity fault is marked as an
// infrastructure failure rather than a subject regression.
func TestRenderText_AllExcludedSuite_IsWellFormed(t *testing.T) {
	// Arrange
	result := fixtureAllExcludedResult()
	golden := readGolden(t, "all_excluded.golden.txt")

	// Act
	out, err := renderText(t, result)

	// Assert
	if err != nil {
		t.Fatalf("RenderText returned an error for an all-excluded suite: %v", err)
	}
	if out != golden {
		t.Errorf("all-excluded terminal report does not match golden output.\n--- got ---\n%s\n--- want ---\n%s", out, golden)
	}
}

// TestRenderText_OutcomeClassesAreVisiblyDistinct is the explicit AC14.7
// check: an assertion failure, a timeout, a state-integrity failure and a
// cost-attribution miss must each carry their own marker in the rendering,
// so a reader cannot mistake an infrastructure fault for a subject
// regression by reading the same word for both.
func TestRenderText_OutcomeClassesAreVisiblyDistinct(t *testing.T) {
	// Arrange
	result := fixtureFullResult()

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert
	markers := map[string]string{
		"assertion failure":     "assertion_failure",
		"timeout":               "timeout",
		"state-integrity fault": "state_integrity",
		"cost-attribution miss": "cost_unattributed",
	}
	seen := map[string]bool{}
	for name, marker := range markers {
		if !strings.Contains(out, marker) {
			t.Errorf("rendering has no distinct marker for the %s case (expected to find %q)", name, marker)
			continue
		}
		if seen[marker] {
			t.Errorf("marker %q is reused across more than one outcome class in %q", marker, out)
		}
		seen[marker] = true
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/report/" + name)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(data)
}
