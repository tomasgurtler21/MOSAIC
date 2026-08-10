package main

// Tests for the composition-root wiring seam T3.4 names explicitly: a
// problem-bearing CheckEnvironment report must flow into a rejected
// preflight before any subject is spawned. environmentBakedPreflight is the
// exact function that owns this seam ("sets in.Environment = env before
// delegating to preflight.Validate"), so it is exercised directly rather
// than only through preflight.Validate's own suite (which proves the
// diagnostic-emission half in isolation) or claudecode's CheckEnvironment
// suite (which proves the problem-detection half in isolation). Neither of
// those proves the wiring between them actually holds.
//
// A regression that silently dropped "in.Environment = env" inside
// environmentBakedPreflight — leaving in.Environment at its zero value no
// matter what report buildDeps resolved — would pass every other test in
// this stage yet defeat AC3.5's "before any subject is spawned" guarantee at
// the one place it is supposed to be enforced. These tests fail if that
// regression is introduced.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// writeMinimalSuiteTree materializes the smallest suite tree preflight.Validate
// accepts without any authoring or fixture errors of its own, so a test can
// isolate the environment-diagnostic behaviour from every other diagnostic
// Validate can produce. Validate returns before running any check
// (including the environment one) when the suite file itself cannot be
// read, so a valid tree is required to reach environmentBakedPreflight's
// contribution at all.
func writeMinimalSuiteTree(t *testing.T) (suitePath, fixtureRoot string) {
	t.Helper()
	root := t.TempDir()

	const suite = `
schema_version: 1
id: s
tests:
  - path: happy-path.test.yaml
`
	const definition = `
schema_version: 1
id: happy-path
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: happy-path.stubs.json
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`
	const registry = `{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

	files := map[string]string{
		"s.suite.yaml":          suite,
		"happy-path.test.yaml":  definition,
		"happy-path.stubs.json": registry,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", full, err)
		}
	}

	return filepath.Join(root, "s.suite.yaml"), filepath.Join(root, "fixtures")
}

// TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic asserts
// the composition-root seam end-to-end: a problem-bearing EnvironmentReport
// given to environmentBakedPreflight reaches preflight.Validate and comes
// back out as an "environment-unusable" error diagnostic, carrying the
// problem's own Detail verbatim — even though the caller's own
// preflight.Input never set Environment itself. This is only possible if
// environmentBakedPreflight actually assigns in.Environment = env, which is
// the exact statement this test pins.
func TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	const detail = `no usable interpreter found: tried python3, py, python; install one of these or set an interpreter explicitly`
	envReport := domain.EnvironmentReport{
		Problems: []domain.EnvironmentProblem{
			{Kind: domain.ProblemInterpreterUnresolved, Detail: detail},
		},
	}

	validate := environmentBakedPreflight(envReport)

	// Deliberately leave Environment unset on the caller's own Input: proving
	// the diagnostic appears anyway is what distinguishes this test from one
	// that would pass even if environmentBakedPreflight forgot to bake the
	// report in, so long as the caller happened to set it too.
	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	var found bool
	for _, d := range report.Diagnostics {
		if d.Code != "environment-unusable" {
			continue
		}
		found = true
		if d.Severity != "error" {
			t.Errorf("environment-unusable diagnostic Severity = %q, want %q", d.Severity, "error")
		}
		if !strings.Contains(d.Message, detail) {
			t.Errorf("environment-unusable diagnostic Message = %q, does not preserve the problem's Detail (%q) verbatim", d.Message, detail)
		}
	}
	if !found {
		t.Fatalf("environmentBakedPreflight(envReport) with a Problems-bearing report produced no %q diagnostic; diagnostics=%+v (AC3.5: a failing environment check must surface as a preflight error before any subject is spawned)", "environment-unusable", report.Diagnostics)
	}
}

// TestEnvironmentBakedPreflight_HealthyReportProducesNoEnvironmentDiagnostic
// is the positive case TestEnvironmentBakedPreflight_ProblemsSurfaceAsPreflightDiagnostic
// is proven against: an OK() report must not manufacture a diagnostic of its
// own, so the negative-path test above is proven against a wiring that can
// actually pass through cleanly, not one that always refuses.
func TestEnvironmentBakedPreflight_HealthyReportProducesNoEnvironmentDiagnostic(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	validate := environmentBakedPreflight(domain.EnvironmentReport{})

	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
	})

	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			t.Errorf("environmentBakedPreflight(EnvironmentReport{}) produced an unexpected %q diagnostic %+v for a report with no problems", "environment-unusable", d)
		}
	}
}

// TestEnvironmentBakedPreflight_OverridesAnyCallerSuppliedEnvironment
// asserts environmentBakedPreflight's own report — not whatever the caller's
// Input already carried — is what preflight.Validate sees. A frontend never
// constructs its own EnvironmentReport (per this seam's doc comment), so a
// stray non-zero Input.Environment must never leak into the result in place
// of the baked-in one.
func TestEnvironmentBakedPreflight_OverridesAnyCallerSuppliedEnvironment(t *testing.T) {
	suitePath, fixtureRoot := writeMinimalSuiteTree(t)

	// The closure is baked with a healthy report...
	validate := environmentBakedPreflight(domain.EnvironmentReport{})

	// ...but the caller's own Input carries a problem the closure must
	// override rather than merge or defer to.
	const staleDetail = "stale caller-supplied problem that must not surface"
	_, report := validate(preflight.Input{
		SuitePath:   suitePath,
		FixtureRoot: fixtureRoot,
		HarnessID:   "claude-code",
		Environment: domain.EnvironmentReport{
			Problems: []domain.EnvironmentProblem{
				{Kind: domain.ProblemInterpreterUnresolved, Detail: staleDetail},
			},
		},
	})

	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			t.Errorf("environmentBakedPreflight(EnvironmentReport{}) let the caller-supplied Input.Environment leak through as diagnostic %+v; the baked-in report must be the one used", d)
		}
	}
}
