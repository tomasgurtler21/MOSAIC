package cost_test

// Integration coverage for Stage 8 (T8.4): exercise the cost provider
// against a tool-local fixture log tree using the REAL mosaic-log-analyzer
// binary and assert that the query returns a non-zero, attributed figure keyed
// to the queried run id.
//
// Why this test is not redundant with the stub-based tests in this package:
// Every existing test supplies a stub CommandRunner via Options.Invoke and
// asserts the MAPPING of a synthesised analyser outcome. None ever runs the
// analyser itself. A stub cannot show that a real log tree containing usage
// records for a priced model yields a non-zero number — only this test can.
//
// Fixture tree (testdata/cost/logtree/):
//
//	20260822T120000Z-ab41/   <- the queried run, with claude-sonnet-4-6 usage
//	  00_orchestrator_events.jsonl
//	unknown-run/             <- sibling fallback bucket (attribution keying check)
//	  00_orchestrator_events.jsonl
//	20260822T130000Z-ab42/   <- second run (attribution isolation check)
//	  00_orchestrator_events.jsonl
//
// The fixture must match the on-disk shape that the logger bundle produces
// (OrchestrationLogs/00_orchestrator_events.jsonl within each run folder),
// or this test proves something about a layout that never occurs.
//
// Pricing: the test stages a minimal pricing.yaml in a test-owned working
// directory (never the repository-root MosaicLogAnalyzer/config/) and sets
// Options.WorkingDir so the analyser resolves it from there. Using the
// live repository pricing file would couple this test to unrelated pricing
// edits and violate the codebase's own "no live Catalog/ content in fixtures"
// rule.
//
// Binary: the analyser is built fresh for every test run. If it cannot be
// built, the test skips with an explicit reason — a silent no-op would
// recreate the false-assurance gap this test exists to close.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// TestCost_RealAnalyserWithFixtureLogTree_ReturnsNonZeroAttributedFigure is
// the end-to-end attribution proof: a tool-local fixture log tree that
// reproduces what a protocol-compliant run leaves on disk yields a non-zero,
// fully-attributed cost figure keyed to the queried run id when the real
// analyser binary is run against it.
func TestCost_RealAnalyserWithFixtureLogTree_ReturnsNonZeroAttributedFigure(t *testing.T) {
	binPath := buildAnalyserBinary(t)
	logTreeRoot := fixtureLogTreeRoot(t)
	workDir := stageTestPricingConfig(t)

	const queriedRunID = "20260822T120000Z-ab41"

	p := cost.New(cost.Options{
		ExecutablePath: binPath,
		WorkingDir:     workDir,
		Timeout:        30 * time.Second,
		// No Invoke override: use the real subprocess.
		// No StatDir override: use the real filesystem.
	})

	report, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  filepath.Join(logTreeRoot, queriedRunID),
		LogsRoot: logTreeRoot,
		RunID:    queriedRunID,
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if report.Attribution != domain.AttributionAttributed {
		t.Errorf(
			"Attribution = %q, want %q — the fixture log tree has usage records for a priced model; "+
				"the analyser must return a fully attributed result",
			report.Attribution, domain.AttributionAttributed,
		)
	}
	if report.TotalUSD == 0 {
		t.Error(
			"TotalUSD = 0, want a non-zero figure — " +
				"the fixture run has turn events with input and output tokens for claude-sonnet-4-6; " +
				"the analyser must compute a real cost, not zero",
		)
	}
}

// TestCost_RealAnalyserWithFixtureLogTree_AttributionIsKeyedToQueriedRun
// asserts that the cost figure is attributed to the queried run id, not to
// the sibling runs (the second run and the fallback bucket). Attribution
// isolation is load-bearing: broadening the scan scope must not collapse two
// separate runs' costs into one.
func TestCost_RealAnalyserWithFixtureLogTree_AttributionIsKeyedToQueriedRun(t *testing.T) {
	binPath := buildAnalyserBinary(t)
	logTreeRoot := fixtureLogTreeRoot(t)
	workDir := stageTestPricingConfig(t)

	const queriedRunID = "20260822T120000Z-ab41"
	const secondRunID = "20260822T130000Z-ab42"

	p := cost.New(cost.Options{
		ExecutablePath: binPath,
		WorkingDir:     workDir,
		Timeout:        30 * time.Second,
	})

	// Query the first run.
	run1, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  filepath.Join(logTreeRoot, queriedRunID),
		LogsRoot: logTreeRoot,
		RunID:    queriedRunID,
	})
	if err != nil {
		t.Fatalf("Cost (run 1) returned unexpected error: %v", err)
	}

	// Query the second run.
	run2, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  filepath.Join(logTreeRoot, secondRunID),
		LogsRoot: logTreeRoot,
		RunID:    secondRunID,
	})
	if err != nil {
		t.Fatalf("Cost (run 2) returned unexpected error: %v", err)
	}

	// Both should be attributed (fixture contains usage for both).
	if run1.Attribution != domain.AttributionAttributed {
		t.Errorf("run1.Attribution = %q, want %q", run1.Attribution, domain.AttributionAttributed)
	}
	if run2.Attribution != domain.AttributionAttributed {
		t.Errorf("run2.Attribution = %q, want %q", run2.Attribution, domain.AttributionAttributed)
	}

	// The two runs must have distinct non-zero costs: the fixture gives run1
	// more tokens than run2, so their costs cannot be equal.
	if run1.TotalUSD == 0 {
		t.Error("run1.TotalUSD = 0, want non-zero")
	}
	if run2.TotalUSD == 0 {
		t.Error("run2.TotalUSD = 0, want non-zero")
	}
	if run1.TotalUSD == run2.TotalUSD {
		t.Errorf(
			"run1.TotalUSD = run2.TotalUSD = %v — the two fixture runs have different token counts; "+
				"if costs are equal, the analyser is not filtering by run id and is attributing the combined tree to both",
			run1.TotalUSD,
		)
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// buildAnalyserBinary builds mosaic-log-analyzer from source and returns the
// path to the resulting binary. If the build fails, the test is skipped with
// an explicit reason — a silent no-op would recreate the false-assurance gap
// this test exists to close.
func buildAnalyserBinary(t *testing.T) string {
	t.Helper()

	// Locate the LogAnalyzer module root relative to this file's own path.
	// This file is at Tools/AgentTest/internal/cost/cost_integration_test.go,
	// so the LogAnalyzer module is at Tools/LogAnalyzer.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("buildAnalyserBinary: cannot resolve this file's own path")
	}
	// thisFile: .../Tools/AgentTest/internal/cost/cost_integration_test.go
	// filepath.Dir x4: .../Tools
	toolsDir := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	logAnalyzerDir := filepath.Join(toolsDir, "LogAnalyzer")

	if _, err := os.Stat(logAnalyzerDir); err != nil {
		t.Skipf("buildAnalyserBinary: LogAnalyzer module not found at %s (skipping, not silently passing): %v", logAnalyzerDir, err)
		return ""
	}

	binName := "mosaic-log-analyzer"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/mosaic-log-analyzer")
	cmd.Dir = logAnalyzerDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf(
			"buildAnalyserBinary: cannot build mosaic-log-analyzer (skipping, not silently passing — "+
				"a test that no-ops when the binary is missing would recreate the false-assurance gap this test closes): %v\n%s",
			err, out,
		)
		return ""
	}
	return binPath
}

// fixtureLogTreeRoot returns the absolute path to the tool-local fixture log
// tree at testdata/cost/logtree. This directory reproduces the on-disk shape
// a protocol-compliant run leaves: usage records with a priced model under
// the run's own bucket, a sibling fallback bucket, and a second run's bucket.
func fixtureLogTreeRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixtureLogTreeRoot: cannot resolve this file's own path")
	}
	// thisFile: .../Tools/AgentTest/internal/cost/cost_integration_test.go
	// agentTestRoot: .../Tools/AgentTest
	agentTestRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	logTreeRoot := filepath.Join(agentTestRoot, "testdata", "cost", "logtree")

	if _, err := os.Stat(logTreeRoot); err != nil {
		t.Fatalf("fixtureLogTreeRoot: fixture log tree not found at %s: %v", logTreeRoot, err)
	}
	return logTreeRoot
}

// stageTestPricingConfig creates a test-owned working directory containing a
// minimal MosaicLogAnalyzer/config/pricing.yaml for the model used in the
// fixture log tree (claude-sonnet-4-6). The directory is cleaned up by
// t.Cleanup automatically.
//
// The analyser resolves MosaicLogAnalyzer/config/pricing.yaml relative to
// its working directory (Options.WorkingDir). A test-owned config file ensures
// the test does not depend on the repository-root pricing file, which changes
// as normal project evolution and would make this test fail on unrelated
// pricing edits.
func stageTestPricingConfig(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()
	configDir := filepath.Join(workDir, "MosaicLogAnalyzer", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("stageTestPricingConfig: creating config dir: %v", err)
	}

	// Minimal pricing config: only the model used in the fixture log tree.
	// Rates are in USD per 1,000,000 tokens (quoted strings per the schema).
	pricingYAML := `# Test-owned pricing config for cost integration tests.
# Contains only the model used in the fixture log tree (claude-sonnet-4-6).
# Do NOT replace this with the repository-root pricing.yaml: that file
# changes as normal project evolution and would make this test fragile.
models:
  claude-sonnet-4-6:
    input: "3.00"
    cached_input: "0.30"
    cache_write: "3.75"
    output_under_threshold: "15.00"
`
	pricingPath := filepath.Join(configDir, "pricing.yaml")
	if err := os.WriteFile(pricingPath, []byte(pricingYAML), 0o644); err != nil {
		t.Fatalf("stageTestPricingConfig: writing pricing.yaml: %v", err)
	}
	return workDir
}
