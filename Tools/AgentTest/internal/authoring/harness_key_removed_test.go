package authoring_test

// Stage 5 (Harness-Agnostic Suites and Selection): the harness: key is
// removed from the suite/test schema entirely. A suite is harness-agnostic
// and carries no harness identity of its own — the harness is always
// selected externally, by the invocation (see ContractsDesign.md's
// "removed-key-harness" preflight diagnostic and Stage-5/Plan.md).
//
// This file covers the authoring-layer half of that contract: a document
// still declaring harness: — in a suite's defaults, in a suite entry's
// per-test override, or in a test definition's own settings — is rejected,
// and a document that declares none of that validates cleanly. Coverage of
// the aggregated preflight.Validate diagnostic (code "removed-key-harness")
// lives in internal/preflight's own test file.

import (
	"reflect"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
)

func TestParseSuite_HarnessKeyInDefaults_RejectedAsRemoved(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  harness: claude-code
  timeout: 15m
tests:
  - path: tests/a.test.yaml
`),
	}

	_, report := authoring.ParseSuite(src)

	if !report.HasErrors() {
		t.Fatal("expected a suite still declaring defaults.harness to be rejected")
	}
	if !hasDiagnosticCode(report, "removed-key-harness") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "removed-key-harness", report.Diagnostics)
	}
}

func TestParseSuite_HarnessKeyOnEntryOverride_RejectedAsRemoved(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
tests:
  - path: tests/a.test.yaml
  - path: tests/b.test.yaml
    harness: opencode
`),
	}

	_, report := authoring.ParseSuite(src)

	if !report.HasErrors() {
		t.Fatal("expected a suite entry still declaring its own harness override to be rejected")
	}
	if !hasDiagnosticCode(report, "removed-key-harness") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "removed-key-harness", report.Diagnostics)
	}
}

// TestParseSuite_NoHarnessKeyDeclared_ValidatesCleanly is the positive half
// of AC5.1: a suite that never mentions harness: at all — the only shape a
// suite may now take — parses without error.
func TestParseSuite_NoHarnessKeyDeclared_ValidatesCleanly(t *testing.T) {
	src := authoring.Source{
		Path: "s.suite.yaml",
		Data: []byte(`
schema_version: 1
id: s
defaults:
  timeout: 15m
  repetitions: 1
tests:
  - path: tests/a.test.yaml
  - path: tests/b.test.yaml
    repetitions: 3
`),
	}

	_, report := authoring.ParseSuite(src)

	if report.HasErrors() {
		t.Fatalf("expected a suite declaring no harness: key to validate cleanly, got: %v", report.Diagnostics)
	}
	if hasDiagnosticCode(report, "removed-key-harness") {
		t.Error("a suite that never declares harness: must never be flagged for it")
	}
}

func TestParseTestDefinition_HarnessKeyInSettings_RejectedAsRemoved(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
harness: claude-code
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !report.HasErrors() {
		t.Fatal("expected a test definition still declaring harness: to be rejected")
	}
	if !hasDiagnosticCode(report, "removed-key-harness") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "removed-key-harness", report.Diagnostics)
	}
}

// TestParseTestDefinition_NoHarnessKeyDeclared_ValidatesCleanly is the
// positive half of AC5.1 for a test definition.
func TestParseTestDefinition_NoHarnessKeyDeclared_ValidatesCleanly(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("expected a test definition declaring no harness: key to validate cleanly, got: %v", report.Diagnostics)
	}
	if hasDiagnosticCode(report, "removed-key-harness") {
		t.Error("a test definition that never declares harness: must never be flagged for it")
	}
}

// TestRunSettings_NoLongerCarriesHarnessIdentity pins the "now-dead
// HarnessID plumbing" half of Stage-5/Plan.md's remit: once the schema
// carries no harness key, nothing resolved from authored content may keep a
// field to hold it. A single run's harness is supplied exactly once, by the
// invocation (preflight.Input.HarnessID) — never by anything parsed from a
// suite or test definition — which is what makes "one run never mixes
// harnesses across its tests" true by construction rather than by
// convention.
//
// Structural rather than behavioural: it inspects domain.RunSettings' own
// field set via reflection, so it compiles unchanged whether or not the
// field has been removed yet, and fails only while the field is still
// present.
func TestRunSettings_NoLongerCarriesHarnessIdentity(t *testing.T) {
	typ := reflect.TypeOf(domain.RunSettings{})
	if _, ok := typ.FieldByName("HarnessID"); ok {
		t.Error("domain.RunSettings still declares a HarnessID field; Stage 5 removes the harness: key from the suite/test schema entirely, so a value resolved from authored content must carry no harness identity of its own")
	}
}
