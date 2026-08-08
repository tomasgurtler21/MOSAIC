package authoring_test

// Tests for ParseSuite: parsing and schema-validating a test-suite document
// (`*.suite.yaml`) into domain.TestSuite, accumulating diagnostics rather
// than stopping at the first problem.

import (
	"testing"

	"mosaic-agent-test/internal/authoring"
)

func TestParseSuite_WellFormed_PopulatesFields(t *testing.T) {
	src := authoring.Source{
		Path: "orchestrator-routing.suite.yaml",
		Data: []byte(`
schema_version: 1
id: orchestrator-routing
description: Routing behaviour of the standard orchestrator
defaults:
  harness: claude-code
  timeout: 15m
  turn_limit: 60
  repetitions: 1
  pass_rate: 1.0
tests:
  - path: tests/happy-path.test.yaml
  - path: tests/blocked-escalation.test.yaml
    repetitions: 3
    pass_rate: 0.67
`),
	}

	suite, report := authoring.ParseSuite(src)

	if report.HasErrors() {
		t.Fatalf("expected a well-formed suite to parse without errors, got: %v", report.Diagnostics)
	}
	if suite.ID != "orchestrator-routing" {
		t.Errorf("ID = %q, want %q", suite.ID, "orchestrator-routing")
	}
	if suite.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", suite.SchemaVersion)
	}
	if len(suite.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(suite.Entries))
	}
	if suite.Entries[0].Path != "tests/happy-path.test.yaml" {
		t.Errorf("Entries[0].Path = %q, want %q", suite.Entries[0].Path, "tests/happy-path.test.yaml")
	}
}

func TestParseSuite_EntryOverridesSuiteDefaults(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
tests:
  - path: tests/a.test.yaml
  - path: tests/b.test.yaml
    repetitions: 3
    pass_rate: 0.67
`),
	}

	suite, report := authoring.ParseSuite(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if len(suite.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(suite.Entries))
	}

	overridden := suite.Entries[1].Settings
	if overridden.Repetitions == nil || *overridden.Repetitions != 3 {
		t.Errorf("Entries[1].Settings.Repetitions = %v, want pointer to 3", overridden.Repetitions)
	}
	if overridden.PassRate == nil || *overridden.PassRate != 0.67 {
		t.Errorf("Entries[1].Settings.PassRate = %v, want pointer to 0.67", overridden.PassRate)
	}

	// An entry that states no override must not silently inherit the
	// suite defaults into its own Settings — RunSettings.Repetitions being
	// nil is how "not stated at this level" is distinguished from
	// "resolved to the default value", and that resolution is preflight's
	// job, not the parser's.
	unoverridden := suite.Entries[0].Settings
	if unoverridden.Repetitions != nil {
		t.Errorf("Entries[0].Settings.Repetitions = %v, want nil (not stated)", unoverridden.Repetitions)
	}
}

func TestParseSuite_UnknownField_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
not_a_real_field: true
tests:
  - path: tests/a.test.yaml
`),
	}

	_, report := authoring.ParseSuite(src)

	if !report.HasErrors() {
		t.Fatal("expected an unknown field to be reported as a diagnostic")
	}
	d, ok := diagnosticWithCode(report, "unknown-field")
	if !ok {
		t.Fatalf("expected a diagnostic with code %q, got: %v", "unknown-field", report.Diagnostics)
	}
	// Plan.md's T4.1 calls for "errors with a source location (file, and
	// line where the format allows it)"; that obligation is only actually
	// enforced once a test asserts on Line/Pointer directly rather than on
	// Code alone — an implementation that always left Line/Pointer at their
	// zero values would still pass every other test in this file.
	if d.Line <= 0 {
		t.Errorf("Line = %d, want > 0: YAML supplies a line number for the offending field", d.Line)
	}
	if d.Pointer == "" {
		t.Error("Pointer = \"\", want a non-empty structural location naming the offending field")
	}
}

func TestParseSuite_MissingRequiredID_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
tests:
  - path: tests/a.test.yaml
`),
	}

	_, report := authoring.ParseSuite(src)

	if !hasDiagnosticCode(report, "missing-required-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-required-field", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a missing required 'id' field to be reported as a diagnostic")
	}
}

// hasDiagnosticCode reports whether any diagnostic in report carries code.
func hasDiagnosticCode(report authoring.Report, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// diagnosticWithCode returns the first diagnostic in report carrying code,
// so a test can assert on its fields beyond mere presence.
func diagnosticWithCode(report authoring.Report, code string) (authoring.Diagnostic, bool) {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return authoring.Diagnostic{}, false
}
