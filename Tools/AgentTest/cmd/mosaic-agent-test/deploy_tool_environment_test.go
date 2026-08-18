package main

// Tests for the preflight error raised when the deploy tool is absent (T4.4),
// per the design: the error must report a named environment problem with the
// path searched and the override that would change it — never a bare
// file-not-found — surfaced through the same environment-check seam as the
// cost tool's preflight problem (buildDeps -> environmentBakedPreflight ->
// preflight.Validate).
//
// The direct unit tests for deployToolProblem mirror the shape of costToolProblem's
// specification exactly: the function must detect a missing tool, produce a
// domain.EnvironmentProblem with Kind=ProblemDeployToolUnavailable, and name
// both the path searched and the flag/env-var that would change it.
//
// The end-to-end test through buildDeps proves the plumbing from cfg.DeployToolPath
// all the way to a preflight diagnostic, matching what
// TestBuildDeps_UnavailableCostTool_SurfacesAsPreflightDiagnostic proves for
// the cost tool.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

// writeStandinDeployTool creates an empty file standing in for an installed
// deploy tool executable — its content is never read by the check under test,
// only its existence.
func writeStandinDeployTool(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mosaic-deploy.exe")
	if err := os.WriteFile(path, []byte{}, 0o755); err != nil {
		t.Fatalf("writing stand-in deploy tool at %q: %v", path, err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Direct unit tests for deployToolProblem
// ---------------------------------------------------------------------------

// TestDeployToolProblem_MissingTool_ReturnsAbsentTrue asserts the basic
// detection: a path that does not exist produces absent=true.
func TestDeployToolProblem_MissingTool_ReturnsAbsentTrue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent-dir", "mosaic-deploy.exe")

	_, absent := deployToolProblem(path)

	if !absent {
		t.Fatal("deployToolProblem returned absent=false for a path that does not exist, want true")
	}
}

// TestDeployToolProblem_MissingTool_ProblemKindIsDeployToolUnavailable asserts
// the problem's Kind so preflight can distinguish a missing deploy tool from a
// missing cost tool, a missing interpreter, or any other environment problem.
func TestDeployToolProblem_MissingTool_ProblemKindIsDeployToolUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent-dir", "mosaic-deploy.exe")

	problem, _ := deployToolProblem(path)

	if problem.Kind != domain.ProblemDeployToolUnavailable {
		t.Errorf("problem.Kind = %q, want ProblemDeployToolUnavailable (%q)", problem.Kind, domain.ProblemDeployToolUnavailable)
	}
}

// TestDeployToolProblem_MissingTool_DetailNamesSearchedPath asserts the
// "always diagnosable" requirement: the problem's Detail must name the path
// that was searched, so the operator knows exactly what was not found.
func TestDeployToolProblem_MissingTool_DetailNamesSearchedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent-dir", "mosaic-deploy.exe")

	problem, _ := deployToolProblem(path)

	if !strings.Contains(problem.Detail, path) {
		t.Errorf("problem.Detail = %q, want it to name the searched path %q", problem.Detail, path)
	}
}

// TestDeployToolProblem_MissingTool_DetailNamesOverride asserts that the
// problem's Detail names the override that would change the searched path —
// the --deploy-tool flag or its environment variable equivalent — so the
// operator knows what to set rather than having to find it in documentation.
func TestDeployToolProblem_MissingTool_DetailNamesOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent-dir", "mosaic-deploy.exe")

	problem, _ := deployToolProblem(path)

	if !strings.Contains(problem.Detail, "--deploy-tool") {
		t.Errorf("problem.Detail = %q, want it to name the --deploy-tool override so the operator knows what to set", problem.Detail)
	}
}

// TestDeployToolProblem_PresentTool_ReturnsAbsentFalse is the positive case
// the negative tests above are proven against: a path that names a file that
// actually exists must not produce an environment problem.
func TestDeployToolProblem_PresentTool_ReturnsAbsentFalse(t *testing.T) {
	path := writeStandinDeployTool(t)

	_, absent := deployToolProblem(path)

	if absent {
		t.Errorf("deployToolProblem returned absent=true for a path that exists (%q), want false", path)
	}
}

// ---------------------------------------------------------------------------
// End-to-end test through buildDeps
// ---------------------------------------------------------------------------

// TestBuildDeps_UnavailableDeployTool_SurfacesAsPreflightDiagnostic asserts
// that a DeployToolPath naming a file that does not exist produces an
// "environment-unusable" preflight diagnostic naming that path — not
// discovered only when a render fails during setup. This is the same
// "fail fast, before any cost is incurred" guarantee the cost tool check
// provides.
func TestBuildDeps_UnavailableDeployTool_SurfacesAsPreflightDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)
	missingDeployTool := filepath.Join(t.TempDir(), "does-not-exist", "mosaic-deploy.exe")

	cfg := WiringConfig{
		HarnessID:       "claude-code",
		FixtureRoot:     fixtureRoot,
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "",
		CostToolPath:    writeStandinCostTool(t),
		CostTimeout:     30 * time.Second,
		DeployToolPath:  missingDeployTool,
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
		if !strings.Contains(diag.Message, missingDeployTool) {
			continue
		}
		found = true
		if diag.Severity != "error" {
			t.Errorf("environment-unusable diagnostic for missing deploy tool has Severity %q, want %q", diag.Severity, "error")
		}
	}
	if !found {
		t.Fatalf("buildDeps with an unavailable deploy tool at %q produced no environment-unusable diagnostic naming that path; diagnostics=%+v", missingDeployTool, report.Diagnostics)
	}
}

// TestBuildDeps_AvailableDeployTool_ProducesNoDeployToolDiagnostic is the
// positive case the above test is proven against: a DeployToolPath naming a
// file that does exist must not itself produce an environment-unusable
// diagnostic.
func TestBuildDeps_AvailableDeployTool_ProducesNoDeployToolDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)
	existingDeployTool := writeStandinDeployTool(t)

	cfg := WiringConfig{
		HarnessID:       "claude-code",
		FixtureRoot:     fixtureRoot,
		WorkspaceRoot:   "C:/workspaces",
		SelfPath:        "C:/bin/mosaic-agent-test.exe",
		LoggerBundleDir: "",
		CostToolPath:    writeStandinCostTool(t),
		CostTimeout:     30 * time.Second,
		DeployToolPath:  existingDeployTool,
		Diag:            new(discardWriter),
	}

	d, err := buildDeps(cfg)
	if err != nil {
		t.Fatalf("buildDeps(%+v) returned an error: %v", cfg, err)
	}

	_, report := d.Preflight(preflightInputFor(suitePath, fixtureRoot))

	for _, diag := range report.Diagnostics {
		if diag.Code == "environment-unusable" && strings.Contains(diag.Message, existingDeployTool) {
			t.Errorf("buildDeps with an available deploy tool at %q produced an unexpected environment-unusable diagnostic %+v", existingDeployTool, diag)
		}
	}
}
