package preflight_test

// Tests for the environment-check integration named in Stage 3's plan
// (AC3.5): a failing environment check surfaces as a preflight error before
// any subject is spawned. Validate must turn every domain.EnvironmentProblem
// on Input.Environment.Problems into an "environment-unusable" error
// diagnostic, preserving the problem's own Detail text verbatim, per
// ContractsDesign.md's "Preflight diagnostic codes".

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// TestValidate_EnvironmentProblem_ReportedAsError asserts that a single
// environment problem becomes an error diagnostic, so an unusable
// environment is refused before a run rather than discovered mid-run.
func TestValidate_EnvironmentProblem_ReportedAsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
	})

	const detail = `no usable interpreter found: tried python3, py, python; install one of these or set an interpreter explicitly`

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Environment: domain.EnvironmentReport{
			Problems: []domain.EnvironmentProblem{
				{Kind: domain.ProblemInterpreterUnresolved, Detail: detail},
			},
		},
	})

	if !report.HasErrors() {
		t.Fatalf("Validate: report has no errors, want an environment-unusable error for the given EnvironmentProblem")
	}

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
		t.Errorf("Validate: no diagnostic with Code %q found in %+v", "environment-unusable", report.Diagnostics)
	}
}

// TestValidate_MultipleEnvironmentProblems_EachReported asserts accumulation:
// every problem in Environment.Problems becomes its own diagnostic, none
// swallowed after the first, consistent with Validate's existing
// accumulate-everything behaviour for every other check it performs.
func TestValidate_MultipleEnvironmentProblems_EachReported(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Environment: domain.EnvironmentReport{
			Problems: []domain.EnvironmentProblem{
				{Kind: domain.ProblemInterpreterUnresolved, Detail: "interpreter problem"},
				{Kind: domain.ProblemBundleUnreadable, Detail: "bundle problem"},
			},
		},
	})

	count := 0
	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("Validate: found %d environment-unusable diagnostics, want 2 (one per EnvironmentProblem); diagnostics=%+v", count, report.Diagnostics)
	}
}

// TestValidate_NoEnvironmentProblems_ReportsNoEnvironmentError asserts the
// negative case: an OK() environment report (the zero value included)
// contributes no environment-unusable diagnostic, so the reference example
// tree stays clean exactly as TestValidate_HappyPath_NoErrors already
// requires.
func TestValidate_NoEnvironmentProblems_ReportsNoEnvironmentError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Environment: domain.EnvironmentReport{},
	})

	for _, d := range report.Diagnostics {
		if d.Code == "environment-unusable" {
			t.Errorf("Validate: unexpected environment-unusable diagnostic %+v for an Environment reporting no problems", d)
		}
	}
}
