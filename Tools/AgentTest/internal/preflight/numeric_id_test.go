package preflight_test

// Tests for duplicate numeric ID detection in Validate.
//
// When two test definitions in one preflight run declare the same numeric id
// value, Validate must produce a duplicate-numeric-id diagnostic. The check
// is cross-definition within one run; ParseTestDefinition itself is stateless
// and performs no cross-file uniqueness checks.

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mosaic-agent-test/internal/preflight"
)

// versionedDefinition returns YAML for a well-formed test definition that
// includes all required versioning and identity fields introduced in this
// schema extension. It is used to build multi-definition trees for
// duplicate-ID tests without depending on the shared validDefinition constant
// (which predates the versioning fields).
func versionedDefinition(name string, numericID int) string {
	return "schema_version: 1\n" +
		"name: " + name + "\n" +
		"id: " + strconv.Itoa(numericID) + "\n" +
		"version: 1\n" +
		"changelog:\n" +
		"  - version: 1\n" +
		"    date: \"2026-01-01\"\n" +
		"    changes: \"Initial.\"\n" +
		"layer: orchestrator\n" +
		"subject:\n" +
		"  identity: orchestrator\n" +
		"  agent: orchestrator\n" +
		"  infrastructure_agents: []\n" +
		"stub_registry: " + name + ".stubs.json\n"
}

// versionedRegistryFor returns stub-registry JSON whose test_id matches the
// given definition name. Using the definition name here ensures that after
// implementation the registry's test_id cross-reference check (registry.TestID
// vs definition.Name) passes cleanly, preventing spurious test-id-mismatch
// diagnostics from polluting the duplicate-numeric-id test assertions.
func versionedRegistryFor(name string) string {
	return "{\n" +
		`  "schema_version": 1,` + "\n" +
		`  "test_id": "` + name + `",` + "\n" +
		`  "on_unmatched": "halt",` + "\n" +
		`  "stubs": []` + "\n" +
		"}"
}

// TestValidate_DuplicateNumericID_ReportsDiagnostic verifies that when two
// test definitions in one preflight run declare the same numeric id, Validate
// produces a duplicate-numeric-id diagnostic. Both definitions must be
// processed; the check is not short-circuited on the first collision.
func TestValidate_DuplicateNumericID_ReportsDiagnostic(t *testing.T) {
	// Two definitions share numeric id 7.
	def1 := versionedDefinition("test-alpha", 7)
	def2 := versionedDefinition("test-beta", 7)

	suite := `
schema_version: 1
id: s
tests:
  - path: test-alpha.test.yaml
  - path: test-beta.test.yaml
`

	root := writeTree(t, map[string]string{
		"s.suite.yaml":          suite,
		"test-alpha.test.yaml":  def1,
		"test-beta.test.yaml":   def2,
		"test-alpha.stubs.json": versionedRegistryFor("test-alpha"),
		"test-beta.stubs.json":  versionedRegistryFor("test-beta"),
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	d, found := findDiag(report, "duplicate-numeric-id")
	if !found {
		t.Fatalf("expected a diagnostic with code %q when two definitions share numeric id 7, got: %v",
			"duplicate-numeric-id", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected duplicate numeric id to be reported as an error")
	}
	// The design spec message pattern is "duplicate numeric id %d: also used by %q".
	// Verify the message includes the duplicated ID value (7) so traceability information
	// is pinned against regression (e.g. emitting an empty message or wrong ID).
	if !strings.Contains(d.Message, "7") {
		t.Errorf("duplicate-numeric-id diagnostic message %q does not contain the duplicated id %q; "+
			"the message must include the numeric id value so users can identify the conflict",
			d.Message, "7")
	}
	// The design spec pattern promises %q for the first-seen definition's path so
	// users can locate the conflicting file. test-alpha is the first definition
	// written in the suite (listed first in s.suite.yaml), so its path must appear
	// in the message. A plain substring match on "test-alpha" covers both a file-
	// name reference and a quoted-path reference, regardless of OS separator.
	if !strings.Contains(d.Message, "test-alpha") {
		t.Errorf("duplicate-numeric-id diagnostic message %q does not reference the first-seen "+
			"definition %q; the message must identify the conflicting definition so users "+
			"know which file to check",
			d.Message, "test-alpha")
	}
}

// TestValidate_UniqueNumericIDs_NoDuplicateDiagnostic verifies that definitions
// with distinct numeric ids do not produce a duplicate-numeric-id diagnostic.
// This is the control case for TestValidate_DuplicateNumericID_ReportsDiagnostic.
func TestValidate_UniqueNumericIDs_NoDuplicateDiagnostic(t *testing.T) {
	def1 := versionedDefinition("test-alpha", 1)
	def2 := versionedDefinition("test-beta", 2)

	suite := `
schema_version: 1
id: s
tests:
  - path: test-alpha.test.yaml
  - path: test-beta.test.yaml
`

	root := writeTree(t, map[string]string{
		"s.suite.yaml":          suite,
		"test-alpha.test.yaml":  def1,
		"test-beta.test.yaml":   def2,
		"test-alpha.stubs.json": versionedRegistryFor("test-alpha"),
		"test-beta.stubs.json":  versionedRegistryFor("test-beta"),
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if hasDiagnosticCode(report, "duplicate-numeric-id") {
		t.Errorf("expected no duplicate-numeric-id diagnostic for definitions with distinct ids 1 and 2, got: %v",
			report.Diagnostics)
	}
}
