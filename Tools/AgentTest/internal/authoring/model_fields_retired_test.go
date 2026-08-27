package authoring_test

// Tests for the retirement of the model and stub_model fields from the
// test-definition subject block. After this stage, model selection is a
// runtime concern only: a definition that sets either field is rejected as
// carrying an unknown field, with the offending field named in the diagnostic.
// A definition carrying neither field continues to validate cleanly.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
)

// TestParseTestDefinition_SubjectModelField_RejectedAsUnknownField verifies
// that a definition setting subject.model is rejected as carrying an unknown
// field. Model selection is a runtime concern; the authoring layer must not
// accept a model identifier in a test file.
func TestParseTestDefinition_SubjectModelField_RejectedAsUnknownField(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  model: claude-sonnet-4-5
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !report.HasErrors() {
		t.Fatal("expected subject.model to be rejected as an unknown field; " +
			"model selection is now a runtime concern and must not be authored")
	}
	if !hasDiagnosticCode(report, "unknown-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v; "+
			"the retired model field must be reported through the existing unknown-field mechanism",
			"unknown-field", report.Diagnostics)
	}
}

// TestParseTestDefinition_SubjectModelField_DiagnosticNamesOffendingField
// verifies that the unknown-field diagnostic produced for subject.model
// identifies the offending field by name, so an author can see which field
// to remove.
func TestParseTestDefinition_SubjectModelField_DiagnosticNamesOffendingField(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  model: claude-sonnet-4-5
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	d, ok := diagnosticWithCode(report, "unknown-field")
	if !ok {
		t.Fatalf("expected a diagnostic with code %q, none found in: %v",
			"unknown-field", report.Diagnostics)
	}
	if !strings.Contains(d.Pointer, "model") && !strings.Contains(d.Message, "model") {
		t.Errorf("diagnostic Pointer=%q Message=%q; expected one of them to name the offending field %q",
			d.Pointer, d.Message, "model")
	}
}

// TestParseTestDefinition_SubjectStubModelField_RejectedAsUnknownField
// verifies that a definition setting subject.stub_model is rejected as
// carrying an unknown field. Stub model selection is a runtime concern; the
// authoring layer must not accept a stub model identifier in a test file.
func TestParseTestDefinition_SubjectStubModelField_RejectedAsUnknownField(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  stub_model: claude-haiku-3-5
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !report.HasErrors() {
		t.Fatal("expected subject.stub_model to be rejected as an unknown field; " +
			"stub model selection is now a runtime concern and must not be authored")
	}
	if !hasDiagnosticCode(report, "unknown-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v; "+
			"the retired stub_model field must be reported through the existing unknown-field mechanism",
			"unknown-field", report.Diagnostics)
	}
}

// TestParseTestDefinition_SubjectStubModelField_DiagnosticNamesOffendingField
// verifies that the unknown-field diagnostic produced for subject.stub_model
// identifies the offending field by name.
func TestParseTestDefinition_SubjectStubModelField_DiagnosticNamesOffendingField(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  stub_model: claude-haiku-3-5
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	d, ok := diagnosticWithCode(report, "unknown-field")
	if !ok {
		t.Fatalf("expected a diagnostic with code %q, none found in: %v",
			"unknown-field", report.Diagnostics)
	}
	if !strings.Contains(d.Pointer, "stub_model") && !strings.Contains(d.Message, "stub_model") {
		t.Errorf("diagnostic Pointer=%q Message=%q; expected one of them to name the offending field %q",
			d.Pointer, d.Message, "stub_model")
	}
}

// TestParseTestDefinition_BothModelFields_EachRejectedAsUnknownField verifies
// that when a definition sets both subject.model and subject.stub_model, both
// are rejected — the parser does not stop at the first unknown field.
func TestParseTestDefinition_BothModelFields_EachRejectedAsUnknownField(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  model: claude-sonnet-4-5
  stub_model: claude-haiku-3-5
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !report.HasErrors() {
		t.Fatal("expected a definition setting both model and stub_model to be rejected")
	}

	// Both field names must appear in the set of unknown-field diagnostics so
	// an author sees exactly which fields to remove.
	var modelNamed, stubModelNamed bool
	for _, d := range report.Diagnostics {
		if d.Code != "unknown-field" {
			continue
		}
		text := d.Pointer + " " + d.Message
		if strings.Contains(text, "stub_model") {
			stubModelNamed = true
		} else if strings.Contains(text, "model") {
			modelNamed = true
		}
	}
	if !modelNamed {
		t.Errorf("expected an unknown-field diagnostic naming %q; got diagnostics: %v",
			"model", report.Diagnostics)
	}
	if !stubModelNamed {
		t.Errorf("expected an unknown-field diagnostic naming %q; got diagnostics: %v",
			"stub_model", report.Diagnostics)
	}
}

// TestParseTestDefinition_NeitherModelField_ValidatesCleanly verifies that a
// definition declaring neither subject.model nor subject.stub_model validates
// without errors — the common case once both fields have been retired from all
// authored files.
func TestParseTestDefinition_NeitherModelField_ValidatesCleanly(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: d
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
  opening_message: "Build the thing."
  invocation_kind: orchestrator
  allowed_tools: [Task, Read, Write, Edit]
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("expected a definition with no model or stub_model fields to validate cleanly, got: %v",
			report.Diagnostics)
	}
	if hasDiagnosticCode(report, "unknown-field") {
		t.Error("a definition that never declares model or stub_model must not trigger unknown-field diagnostics for either field")
	}
}

// TestParseTestDefinition_NeitherModelField_YieldsRunnablePlan verifies that
// a definition carrying neither model field produces a parsed definition whose
// required fields are populated — it is fit to be run with runtime-supplied
// model choices rather than being blocked by a parse error.
func TestParseTestDefinition_NeitherModelField_YieldsRunnablePlan(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
name: minimal-no-model
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/minimal.stubs.json
timeout: 5m
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`),
	}

	def, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Name != "minimal-no-model" {
		t.Errorf("def.Name = %q, want %q; a definition without model fields must parse to a complete record",
			def.Name, "minimal-no-model")
	}
	if def.Subject.Identity != "orchestrator" {
		t.Errorf("def.Subject.Identity = %q, want %q",
			def.Subject.Identity, "orchestrator")
	}
	if def.Assertions.FinalPhase == nil || *def.Assertions.FinalPhase != "COMPLETED" {
		t.Errorf("def.Assertions.FinalPhase = %v, want pointer to %q",
			def.Assertions.FinalPhase, "COMPLETED")
	}
}
