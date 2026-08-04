package transform_test

// pipeline_test.go tests the full transform pipeline end-to-end using the fixture harness
// descriptor and a real generic agent source file. The expected output is stored in
// testdata/transform/golden/ and compared byte-for-byte.
//
// Running with -update regenerates the golden files from the current engine output. The
// golden file policy (Stage 8 Plan) requires that deliberate deviations from the committed
// harness output carry a recorded reason; the golden files here are the authoritative
// reference, not the committed harness agent files.

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// TestGoldenFile_TestRunnerAgent applies the fixture harness to the generic test-runner
// agent and compares the output byte-for-byte with the golden file. The golden file
// captures the authoritative expected output of a correct implementation; every field,
// order, and serialisation detail is intentional.
//
// To regenerate: go test ./internal/transform/... -run TestGoldenFile -update
func TestGoldenFile_TestRunnerAgent(t *testing.T) {
	// Locate the generic test-runner source.
	srcPath := filepath.Join("..", "..", "..", "..", "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		t.Fatalf("resolve test-runner source path: %v", err)
	}
	src, err := os.ReadFile(srcAbs)
	if err != nil {
		t.Skipf("generic test-runner agent not found at %s: %v", srcAbs, err)
	}

	req := transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	goldenPath := filepath.Join(testdataDir, "golden", "test-runner.md")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, result.Output, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file: %s (%d bytes)", goldenPath, len(result.Output))
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v\n(run go test -update to generate it)", goldenPath, err)
	}

	if !bytes.Equal(result.Output, golden) {
		// Show the first point of divergence for diagnosis.
		diff := firstOutputDiff(result.Output, golden)
		t.Errorf("output does not match golden file %s\n"+
			"output:  %d bytes\n"+
			"golden:  %d bytes\n"+
			"first difference at byte %d\n\n"+
			"--- output (first 600 bytes) ---\n%s\n\n"+
			"--- golden (first 600 bytes) ---\n%s",
			goldenPath,
			len(result.Output), len(golden), diff,
			truncateBytes(result.Output, 600),
			truncateBytes(golden, 600),
		)
	}
}

// TestGoldenFile_OutputBytesMatchesOutputFieldLength verifies that Report.OutputBytes
// equals len(Result.Output), which the golden file test exercises but does not assert
// explicitly.
func TestGoldenFile_OutputBytesMatchesOutputFieldLength(t *testing.T) {
	const minimalSource = `---
id: 99
version: 1.0.0
name: bytes-check-agent
description: Checks that OutputBytes equals len(Output)
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
Body content for bytes check.
[[/SECTION:Identity]]
`

	req := transform.Request{
		Source: []byte(minimalSource),
		Kind:   domain.ArtifactAgent,
		Key:    "bytes-check-agent",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err) // RED: fails here
	}

	if result.Report.OutputBytes != len(result.Output) {
		t.Errorf("Report.OutputBytes = %d, len(Output) = %d; they must be equal",
			result.Report.OutputBytes, len(result.Output))
	}
}

// TestGoldenFile_OrchestratorAgent is a placeholder that documents the deferred golden
// file for the orchestrator. The orchestrator's {tool-permissions} placeholder expansion
// is the highest-risk transformation path and deserves a byte-level golden pin to catch
// serialisation regressions that the unit tests in special_cases_test.go cannot detect.
//
// This test is skipped until the Stage 8 implementation (I8.1–I8.6) is complete. Once
// complete, run with -update on the orchestrator source to generate the golden file, then
// remove the t.Skip call.
//
// Source: Agents/Generic/Orchestrator/orchestrator.md
// Golden: testdata/transform/golden/orchestrator.md
func TestGoldenFile_OrchestratorAgent(t *testing.T) {
	t.Skip("orchestrator golden file deferred: generate with -update once Stage 8 implementation is complete")
}

// firstOutputDiff returns the byte offset of the first difference between a and b.
func firstOutputDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
