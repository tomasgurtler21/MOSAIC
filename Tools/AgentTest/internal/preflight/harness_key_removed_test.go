package preflight_test

// Stage 5 (Harness-Agnostic Suites and Selection): the aggregated,
// preflight.Validate-level half of the removed harness: key contract.
// ContractsDesign.md's Preflight diagnostic codes table names the code
// exactly: "removed-key-harness", raised when a suite or test still
// declares harness:, with a message naming the removal. See
// internal/authoring's own harness_key_removed_test.go for the
// component-level (ParseSuite/ParseTestDefinition) coverage this file
// exercises through the full aggregated pass.
//
// AC5.6 ("a single suite runs against any one selected harness without
// modification") has a second half here: once a suite declares no harness:
// at all, preflight.Validate must resolve it cleanly regardless of which
// external HarnessID the invocation supplies — the schema and the selected
// adapter are independent by construction, never cross-checked against each
// other by content.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

func TestValidate_SuiteDeclaringHarnessKey_ReportsRemovedKeyDiagnostic(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": `
schema_version: 1
id: s
defaults:
  harness: claude-code
tests:
  - path: happy-path.test.yaml
`,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !report.HasErrors() {
		t.Fatal("expected a suite still declaring defaults.harness to be rejected by preflight")
	}
	d, ok := diagnosticWithHarnessCode(report)
	if !ok {
		t.Fatalf("expected a diagnostic with code %q, got: %v", "removed-key-harness", report.Diagnostics)
	}
	if !strings.Contains(strings.ToLower(d.Message), "harness") {
		t.Errorf("removed-key-harness diagnostic message %q does not name the removed key", d.Message)
	}
}

func TestValidate_TestDefinitionDeclaringHarnessKey_ReportsRemovedKeyDiagnostic(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml": validSuite,
		"happy-path.test.yaml": `
schema_version: 1
name: happy-path
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
harness: opencode
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: happy-path.stubs.json
`,
		"happy-path.stubs.json": validRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if !report.HasErrors() {
		t.Fatal("expected a test definition still declaring harness: to be rejected by preflight")
	}
	if !hasDiagnosticCode(report, "removed-key-harness") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "removed-key-harness", report.Diagnostics)
	}
}

// TestValidate_NoHarnessKeyDeclared_ValidatesCleanly_RegardlessOfInputHarnessID
// is AC5.6's independence claim: a suite declaring no harness: key at all
// validates cleanly no matter which external harness identity the
// invocation names — the schema never reconciles against it, because a
// harness-agnostic suite has nothing of its own to reconcile.
func TestValidate_NoHarnessKeyDeclared_ValidatesCleanly_RegardlessOfInputHarnessID(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	for _, id := range []string{"claude-code", "opencode", "fake", "anything-at-all"} {
		t.Run(id, func(t *testing.T) {
			_, report := preflight.Validate(preflight.Input{
				SuitePath:   filepath.Join(root, "s.suite.yaml"),
				FixtureRoot: filepath.Join(root, "fixtures"),
				HarnessID:   id,
			})
			if report.HasErrors() {
				t.Errorf("HarnessID = %q: expected a suite with no harness: key to validate cleanly, got: %v", id, report.Diagnostics)
			}
		})
	}
}

// diagnosticWithHarnessCode returns the first "removed-key-harness"
// diagnostic in report, so a test can assert on its Message beyond mere
// presence — mirroring internal/authoring's own diagnosticWithCode helper.
func diagnosticWithHarnessCode(report authoring.Report) (authoring.Diagnostic, bool) {
	for _, d := range report.Diagnostics {
		if d.Code == "removed-key-harness" {
			return d, true
		}
	}
	return authoring.Diagnostic{}, false
}
