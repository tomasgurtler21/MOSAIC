package resultsummary_test

// Tests for Infrastructure Failures markdown section rendering.
//
// These tests call RenderVersionSummary directly with VersionSummary values
// that have InfraTests populated, and assert on the rendered Markdown.
//
// RED state: All tests that check the infrastructure-failures section content
// fail at runtime because RenderVersionSummary does not yet render that
// section. Tests compile because InfraTests was added to VersionSummary.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// minimalVersionSummaryWithInfraTests returns a VersionSummary with one
// InfraTests entry and no ProblemTests, for testing the infra section in
// isolation.
func minimalVersionSummaryWithInfraTests() resultsummary.VersionSummary {
	return resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"infra-suite"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  1,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:   "claude-code",
					Model:     "claude-sonnet-4.6",
					TestCount: 1,
					PassCount: 0,
					PassRate:  0.0,
					TotalCost: 0.02,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"infra-suite": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:   "claude-code",
						Model:     "claude-sonnet-4.6",
						TestCount: 1,
						PassCount: 0,
						PassRate:  0.0,
						TotalCost: 0.02,
					},
				},
			},
		},
		ProblemTests: nil,
		InfraTests: []resultsummary.TestStats{
			{
				SuiteID:    "infra-suite",
				TestName:   "state-reclaim-test",
				NumericID:  42,
				BestRate:   1.0,
				BestCombo:  "claude-sonnet-4.6/claude-code",
				WorstRate:  0.0,
				WorstCombo: "gpt-4o/open-code",
				Spread:     1.0,
			},
		},
	}
}

// TestRenderVersionSummary_ContainsInfrastructureFailuresSection verifies
// that RenderVersionSummary output includes both the opening and closing
// generated:infrastructure-failures markers.
func TestRenderVersionSummary_ContainsInfrastructureFailuresSection(t *testing.T) {
	// Arrange
	vs := minimalVersionSummaryWithInfraTests()

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// Assert: both markers must be present
	if !strings.Contains(output, "<!-- generated:infrastructure-failures -->") {
		t.Error("RenderVersionSummary output must contain <!-- generated:infrastructure-failures --> opening marker")
	}
	if !strings.Contains(output, "<!-- /generated:infrastructure-failures -->") {
		t.Error("RenderVersionSummary output must contain <!-- /generated:infrastructure-failures --> closing marker")
	}
}

// TestRenderVersionSummary_InfraFailuresSection_ListsInfraTests verifies
// that when InfraTests is non-empty, the infrastructure-failures section
// contains the infra test's name.
func TestRenderVersionSummary_InfraFailuresSection_ListsInfraTests(t *testing.T) {
	// Arrange
	vs := minimalVersionSummaryWithInfraTests()

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// Extract the infrastructure-failures block.
	blockStart := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	blockEnd := strings.Index(output, "<!-- /generated:infrastructure-failures -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:infrastructure-failures block not found in rendered output; " +
			"renderInfrastructureFailuresSection must be added to RenderVersionSummary")
	}
	infraBlock := output[blockStart:blockEnd]

	// The test name must appear in the section.
	if !strings.Contains(infraBlock, "state-reclaim-test") {
		t.Errorf("infrastructure-failures block must contain the infra test name 'state-reclaim-test';\nblock content:\n%s", infraBlock)
	}
}

// TestRenderVersionSummary_InfraFailuresSection_IncludesTestDetails verifies
// that the infrastructure-failures section includes suite, numeric test ID,
// and test name for each infra test entry.
func TestRenderVersionSummary_InfraFailuresSection_IncludesTestDetails(t *testing.T) {
	// Arrange: use an infra test with known suite, ID, and name
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"lockout-suite"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  1,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 1, PassCount: 0, TotalCost: 0.01,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"lockout-suite": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 1, PassCount: 0, TotalCost: 0.01,
					},
				},
			},
		},
		InfraTests: []resultsummary.TestStats{
			{
				SuiteID:    "lockout-suite",
				TestName:   "lock-reclaim-on-retry",
				NumericID:  99,
				BestRate:   0.8,
				BestCombo:  "claude-sonnet-4.6/claude-code",
				WorstRate:  0.2,
				WorstCombo: "gpt-4o/open-code",
				Spread:     0.6,
			},
		},
	}

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// Extract block.
	blockStart := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	blockEnd := strings.Index(output, "<!-- /generated:infrastructure-failures -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:infrastructure-failures block not found; implementation required")
	}
	infraBlock := output[blockStart:blockEnd]

	// Suite must appear.
	if !strings.Contains(infraBlock, "lockout-suite") {
		t.Errorf("infrastructure-failures block must include the suite 'lockout-suite';\nblock:\n%s", infraBlock)
	}
	// Numeric ID must appear.
	if !strings.Contains(infraBlock, "99") {
		t.Errorf("infrastructure-failures block must include the numeric test ID '99';\nblock:\n%s", infraBlock)
	}
	// Test name must appear.
	if !strings.Contains(infraBlock, "lock-reclaim-on-retry") {
		t.Errorf("infrastructure-failures block must include test name 'lock-reclaim-on-retry';\nblock:\n%s", infraBlock)
	}
}

// TestRenderVersionSummary_InfraFailuresSection_EmptyInfraTests_ShowsNoFailuresMessage
// verifies that when InfraTests is nil or empty, the infrastructure-failures
// section renders a "No infrastructure failures." message rather than an
// empty or missing block.
func TestRenderVersionSummary_InfraFailuresSection_EmptyInfraTests_ShowsNoFailuresMessage(t *testing.T) {
	// Arrange: a summary with no infra tests
	vs := minimalVersionSummary("v1.0.0")
	vs.InfraTests = nil

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// The section must still be present.
	if !strings.Contains(output, "<!-- generated:infrastructure-failures -->") {
		t.Error("infrastructure-failures section must always be present in output, even when InfraTests is empty")
	}

	// Extract block.
	blockStart := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	blockEnd := strings.Index(output, "<!-- /generated:infrastructure-failures -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:infrastructure-failures block not found; implementation required")
	}
	infraBlock := output[blockStart:blockEnd]

	// Must contain the no-failures message.
	if !strings.Contains(infraBlock, "No infrastructure failures") {
		t.Errorf("infrastructure-failures block with empty InfraTests must contain 'No infrastructure failures';\nblock:\n%s", infraBlock)
	}
}

// TestRenderVersionSummary_InfraFailuresSection_AppearsAfterProblemAreas
// verifies that the infrastructure-failures section is positioned after the
// problem-areas section and before the analysis:overall-analysis placeholder,
// as specified in the design.
func TestRenderVersionSummary_InfraFailuresSection_AppearsAfterProblemAreas(t *testing.T) {
	// Arrange
	vs := minimalVersionSummaryWithInfraTests()

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	problemAreasEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	infraStart := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	analysisStart := strings.Index(output, "<!-- analysis:overall-analysis -->")

	if problemAreasEnd < 0 {
		t.Fatal("<!-- /generated:problem-areas --> marker not found in output")
	}
	if infraStart < 0 {
		t.Fatal("<!-- generated:infrastructure-failures --> marker not found in output; " +
			"the section must be inserted between problem-areas and analysis:overall-analysis")
	}
	if analysisStart < 0 {
		t.Fatal("<!-- analysis:overall-analysis --> marker not found in output")
	}

	// infrastructure-failures must come after problem-areas closing marker.
	if infraStart < problemAreasEnd {
		t.Errorf("infrastructure-failures section (at offset %d) must appear AFTER <!-- /generated:problem-areas --> (at offset %d)",
			infraStart, problemAreasEnd)
	}
	// infrastructure-failures must come before analysis:overall-analysis.
	if infraStart > analysisStart {
		t.Errorf("infrastructure-failures section (at offset %d) must appear BEFORE <!-- analysis:overall-analysis --> (at offset %d)",
			infraStart, analysisStart)
	}
}

// TestRenderVersionSummary_InfraTestNotInProblemAreas verifies that an infra
// test placed only in InfraTests (not in ProblemTests) does not appear in
// the generated:problem-areas block. This acts as a regression guard for the
// case where both collections are populated independently.
func TestRenderVersionSummary_InfraTestNotInProblemAreas(t *testing.T) {
	// Arrange: infra test ONLY in InfraTests; ProblemTests is empty.
	vs := minimalVersionSummaryWithInfraTests()
	vs.ProblemTests = nil

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// Extract problem-areas block.
	paStart := strings.Index(output, "<!-- generated:problem-areas -->")
	paEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	if paStart < 0 || paEnd < 0 || paEnd <= paStart {
		t.Fatal("generated:problem-areas block not found in output")
	}
	problemBlock := output[paStart:paEnd]

	// The infra test must NOT appear in problem-areas.
	if strings.Contains(problemBlock, "state-reclaim-test") {
		t.Errorf("problem-areas block must NOT contain 'state-reclaim-test' (only in InfraTests, not ProblemTests);\nproblem-areas block:\n%s", problemBlock)
	}
}

// TestRenderVersionSummary_InfraFailuresSection_MultipleEntries_AllListed
// verifies that when InfraTests contains multiple entries, all appear in the
// infrastructure-failures section.
func TestRenderVersionSummary_InfraFailuresSection_MultipleEntries_AllListed(t *testing.T) {
	// Arrange: two infra test entries
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"suite-one", "suite-two"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6", TestCount: 2},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-one": {"claude-sonnet-4.6": {"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6", TestCount: 1}}},
			"suite-two": {"claude-sonnet-4.6": {"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6", TestCount: 1}}},
		},
		InfraTests: []resultsummary.TestStats{
			{
				SuiteID:   "suite-one",
				TestName:  "infra-alpha",
				NumericID: 1,
				BestRate:  0.5, BestCombo: "claude-sonnet-4.6/claude-code",
				WorstRate: 0.0, WorstCombo: "gpt-4o/open-code",
				Spread: 0.5,
			},
			{
				SuiteID:   "suite-two",
				TestName:  "infra-beta",
				NumericID: 2,
				BestRate:  0.8, BestCombo: "claude-sonnet-4.6/claude-code",
				WorstRate: 0.2, WorstCombo: "gpt-4o/open-code",
				Spread: 0.6,
			},
		},
	}

	// Act
	output := resultsummary.RenderVersionSummary(vs)

	// Extract block.
	blockStart := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	blockEnd := strings.Index(output, "<!-- /generated:infrastructure-failures -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:infrastructure-failures block not found; implementation required")
	}
	infraBlock := output[blockStart:blockEnd]

	// Both infra tests must appear in the section.
	if !strings.Contains(infraBlock, "infra-alpha") {
		t.Errorf("infrastructure-failures block must contain 'infra-alpha';\nblock:\n%s", infraBlock)
	}
	if !strings.Contains(infraBlock, "infra-beta") {
		t.Errorf("infrastructure-failures block must contain 'infra-beta';\nblock:\n%s", infraBlock)
	}
}
