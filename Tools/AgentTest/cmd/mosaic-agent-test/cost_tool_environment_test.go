package main

// Tests for the preflight error raised when the cost tool is absent, per
// AC6.2: the error must name the expected path and the override that would
// change it, surfaced through the same environment-check seam Stage 3 wired
// (buildDeps -> environmentBakedPreflight -> preflight.Validate), rather
// than only discovered later when a run's cost cannot be attributed.
//
// This exercises buildDeps end-to-end with a real "claude-code" harness
// rather than a fake one, because newAdapter only resolves catalog
// identities and the cost-tool check is harness-neutral: it must surface
// identically no matter which harness is selected. writeMinimalSuiteTree
// (environment_wiring_test.go) supplies the smallest suite tree that reaches
// this seam without any unrelated authoring diagnostic in the way.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/preflight"
)

// preflightInputFor builds the preflight.Input writeMinimalSuiteTree's tree
// satisfies, naming the given harness so every test in this file drives the
// same "claude-code" path buildDeps resolved cfg.HarnessID against.
func preflightInputFor(suitePath, fixtureRoot string) preflight.Input {
	return preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	}
}

// writeStandinCostTool creates an empty file standing in for an installed
// cost-tool executable — its content is never read by the check under test,
// only its existence.
func writeStandinCostTool(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mosaic-log-analyzer.exe")
	if err := os.WriteFile(path, []byte{}, 0o755); err != nil {
		t.Fatalf("writing stand-in cost tool at %q: %v", path, err)
	}
	return path
}

// TestBuildDeps_UnavailableCostTool_SurfacesAsPreflightDiagnostic asserts a
// CostToolPath naming a file that does not exist produces an
// "environment-unusable" preflight diagnostic naming that path — not
// discovered only once a run tries and fails to attribute cost.
func TestBuildDeps_UnavailableCostTool_SurfacesAsPreflightDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)
	missingCostTool := filepath.Join(t.TempDir(), "does-not-exist", "mosaic-log-analyzer.exe")

	cfg := WiringConfig{
		HarnessID:       "claude-code",
		FixtureRoot:     fixtureRoot,
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "",
		CostToolPath:    missingCostTool,
		CostTimeout:     30 * time.Second,
		Diag:            new(discardWriter),
	}

	d, err := buildDeps(cfg)
	if err != nil {
		t.Fatalf("buildDeps(%+v) returned an error: %v", cfg, err)
	}

	_, report := d.Preflight(preflightInputFor(suitePath, fixtureRoot))

	var found bool
	for _, diag := range report.Diagnostics {
		if diag.Code != "environment-unusable" {
			continue
		}
		if !strings.Contains(diag.Message, missingCostTool) {
			continue
		}
		found = true
		if diag.Severity != "error" {
			t.Errorf("environment-unusable diagnostic for the missing cost tool has Severity %q, want %q", diag.Severity, "error")
		}
	}
	if !found {
		t.Fatalf("buildDeps with an unavailable cost tool at %q produced no environment-unusable diagnostic naming that path; diagnostics=%+v (AC6.2)", missingCostTool, report.Diagnostics)
	}
}

// TestBuildDeps_AvailableCostTool_ProducesNoCostToolDiagnostic is the
// positive case the negative test above is proven against: a CostToolPath
// naming a file that does exist must not itself produce an
// environment-unusable diagnostic mentioning that path.
func TestBuildDeps_AvailableCostTool_ProducesNoCostToolDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)
	existingCostTool := writeStandinCostTool(t)

	cfg := WiringConfig{
		HarnessID:       "claude-code",
		FixtureRoot:     fixtureRoot,
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "",
		CostToolPath:    existingCostTool,
		CostTimeout:     30 * time.Second,
		Diag:            new(discardWriter),
	}

	d, err := buildDeps(cfg)
	if err != nil {
		t.Fatalf("buildDeps(%+v) returned an error: %v", cfg, err)
	}

	_, report := d.Preflight(preflightInputFor(suitePath, fixtureRoot))

	for _, diag := range report.Diagnostics {
		if diag.Code == "environment-unusable" && strings.Contains(diag.Message, existingCostTool) {
			t.Errorf("buildDeps with an available cost tool at %q produced an unexpected environment-unusable diagnostic %+v", existingCostTool, diag)
		}
	}
}
