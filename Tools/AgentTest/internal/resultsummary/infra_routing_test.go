package resultsummary_test

// Tests for buildVersionSummary infrastructure failure routing.
//
// These tests verify that tests whose aggregate carries
// InfrastructureFailure==true are routed to VersionSummary.InfraTests and
// excluded from VersionSummary.ProblemTests. Tests use the existing fakeFS +
// in-memory JSON fixture pattern and call Generate, then inspect the written
// summary.md content.
//
// Fixture design:
//   - infra_routing_harness_a.json: claude-code / claude-sonnet-4.6
//     - "normal-test" (id=10): pass_rate=1.0, infrastructure_failure=false
//     - "infra-test"  (id=20): pass_rate=1.0, infrastructure_failure=true
//   - infra_routing_harness_b.json: open-code / gpt-4o
//     - "normal-test" (id=10): pass_rate=0.5, infrastructure_failure=false
//     - "infra-test"  (id=20): pass_rate=0.0, infrastructure_failure=true
//
// Expected routing:
//   - "normal-test": spread=0.5 > 0, infrastructure_failure=false  -> ProblemTests
//   - "infra-test":  spread=1.0,     infrastructure_failure=true   -> InfraTests, NOT ProblemTests
//
// RED state: Tests that assert absence of "infra-test" from problem-areas and
// presence in infrastructure-failures fail at runtime because the current
// implementation routes all tests with spread > 0 to ProblemTests and does
// not render the infrastructure-failures section.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// seedInfraRoutingReports seeds both infra routing fixture reports into the
// fake filesystem for infra routing tests.
func seedInfraRoutingReports(fs *fakeFS, root string, t *testing.T) {
	t.Helper()
	seedReport(fs, root, "v1.0.0", "infra-suite", "claude-code",
		"claude-sonnet-4.6", "20260901T100000",
		loadFixture(t, "infra_routing_harness_a.json"))
	seedReport(fs, root, "v1.0.0", "infra-suite", "open-code",
		"gpt-4o", "20260901T110000",
		loadFixture(t, "infra_routing_harness_b.json"))
}

// TestGenerate_InfraFailedTest_ExcludedFromProblemAreas verifies that a test
// with InfrastructureFailure==true does not appear in the generated:problem-areas
// block, even though it has a non-zero spread that would otherwise qualify it.
//
// "infra-test" has spread=1.0 across the two harness fixtures but
// infrastructure_failure=true in both; it must be routed to InfraTests and
// absent from ProblemTests/problem-areas.
func TestGenerate_InfraFailedTest_ExcludedFromProblemAreas(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	const root = "/TestResults"
	seedInfraRoutingReports(fs, root, t)

	req := resultsummary.SummaryRequest{TestResultsRoot: root}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, root+"/Orchestrator/v1.0.0/summary.md")

	blockStart := strings.Index(content, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output; implementation required")
	}
	problemBlock := content[blockStart:blockEnd]

	// "infra-test" must not appear in the problem-areas block.
	if strings.Contains(problemBlock, "infra-test") {
		t.Errorf("problem-areas block must NOT contain 'infra-test' (infrastructure_failure=true tests belong in InfraTests, not ProblemTests);\nproblem-areas block content:\n%s", problemBlock)
	}
}

// TestGenerate_NormalTest_AppearsInProblemAreas verifies that a test with
// infrastructure_failure=false and spread > 0 still appears in the
// generated:problem-areas block. This is a regression guard: routing infra
// tests away must not accidentally remove normal problem tests.
//
// "normal-test" has spread=0.5 (1.0 - 0.5) and infrastructure_failure=false
// in both harness fixtures; it must appear in ProblemTests/problem-areas.
func TestGenerate_NormalTest_AppearsInProblemAreas(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	const root = "/TestResults"
	seedInfraRoutingReports(fs, root, t)

	req := resultsummary.SummaryRequest{TestResultsRoot: root}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, root+"/Orchestrator/v1.0.0/summary.md")

	blockStart := strings.Index(content, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output; implementation required")
	}
	problemBlock := content[blockStart:blockEnd]

	// "normal-test" must appear in the problem-areas block.
	if !strings.Contains(problemBlock, "normal-test") {
		t.Errorf("problem-areas block must contain 'normal-test' (spread=0.5, infrastructure_failure=false); routing infra tests away must not remove normal problem tests;\nproblem-areas block content:\n%s", problemBlock)
	}
}

// TestGenerate_InfraFailedTest_AppearsInInfraFailuresSection verifies that a
// test with InfrastructureFailure==true appears in the
// generated:infrastructure-failures block in the summary.md.
//
// This is a direct routing test: "infra-test" must be in InfraTests, and the
// rendering must surface it in the infrastructure-failures section.
func TestGenerate_InfraFailedTest_AppearsInInfraFailuresSection(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	const root = "/TestResults"
	seedInfraRoutingReports(fs, root, t)

	req := resultsummary.SummaryRequest{TestResultsRoot: root}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, root+"/Orchestrator/v1.0.0/summary.md")

	blockStart := strings.Index(content, "<!-- generated:infrastructure-failures -->")
	blockEnd := strings.Index(content, "<!-- /generated:infrastructure-failures -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:infrastructure-failures block not found in rendered output; " +
			"renderInfrastructureFailuresSection must be added to RenderVersionSummary between problem-areas and analysis:overall-analysis")
	}
	infraBlock := content[blockStart:blockEnd]

	// "infra-test" must appear in the infrastructure-failures block.
	if !strings.Contains(infraBlock, "infra-test") {
		t.Errorf("infrastructure-failures block must contain 'infra-test' (infrastructure_failure=true);\ninfrastructure-failures block content:\n%s", infraBlock)
	}
}

// TestGenerate_MixedSuite_OnlyNormalTestsInProblemAreas verifies the combined
// routing requirement: with one infra-failed test and one normal test that
// both have spread > 0, only the normal test appears in problem-areas and
// only the infra test appears in infrastructure-failures.
func TestGenerate_MixedSuite_OnlyNormalTestsInProblemAreas(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	const root = "/TestResults"
	seedInfraRoutingReports(fs, root, t)

	req := resultsummary.SummaryRequest{TestResultsRoot: root}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, root+"/Orchestrator/v1.0.0/summary.md")

	// Check problem-areas does not contain infra-test.
	paStart := strings.Index(content, "<!-- generated:problem-areas -->")
	paEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if paStart < 0 || paEnd < 0 || paEnd <= paStart {
		t.Fatal("generated:problem-areas block not found; implementation required")
	}
	problemBlock := content[paStart:paEnd]

	if strings.Contains(problemBlock, "infra-test") {
		t.Errorf("problem-areas must NOT contain 'infra-test'; it belongs in infrastructure-failures")
	}
	if !strings.Contains(problemBlock, "normal-test") {
		t.Errorf("problem-areas must contain 'normal-test' (spread=0.5, not infra-failed)")
	}

	// Check infrastructure-failures contains infra-test and not normal-test.
	ifStart := strings.Index(content, "<!-- generated:infrastructure-failures -->")
	ifEnd := strings.Index(content, "<!-- /generated:infrastructure-failures -->")
	if ifStart < 0 || ifEnd < 0 || ifEnd <= ifStart {
		t.Fatal("generated:infrastructure-failures block not found; renderInfrastructureFailuresSection required")
	}
	infraBlock := content[ifStart:ifEnd]

	if !strings.Contains(infraBlock, "infra-test") {
		t.Errorf("infrastructure-failures must contain 'infra-test'")
	}
	if strings.Contains(infraBlock, "normal-test") {
		t.Errorf("infrastructure-failures must NOT contain 'normal-test'; it belongs in problem-areas")
	}
}

// TestGenerate_InfraFailuresSection_AlwaysPresentEvenIfEmpty verifies that
// the infrastructure-failures section appears in the rendered output even
// when no infra-failed tests exist, so re-generation does not drop the
// section markers that users may have referenced.
func TestGenerate_InfraFailuresSection_AlwaysPresentEvenIfEmpty(t *testing.T) {
	// Arrange: use a fixture with NO infra-failed tests (v1_claude_code_report.json
	// has only normal tests).
	fs := newFakeFS()
	const root = "/TestResults"
	seedReport(fs, root, "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: root}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, root+"/Orchestrator/v1.0.0/summary.md")

	if !strings.Contains(content, "<!-- generated:infrastructure-failures -->") {
		t.Error("generated:infrastructure-failures opening marker must always appear in summary.md, even when InfraTests is empty")
	}
	if !strings.Contains(content, "<!-- /generated:infrastructure-failures -->") {
		t.Error("generated:infrastructure-failures closing marker must always appear in summary.md, even when InfraTests is empty")
	}
}
