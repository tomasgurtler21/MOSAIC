package preflight_test

// Tests for three-level settings precedence for the EchoFidelity field in
// mergeSettings, exercised through the exported Validate function.
//
// The mergeSettings function is called twice inside Validate:
//
//	effective = mergeSettings(mergeSettings(suite.Defaults, entry.Settings), def.Settings)
//
// This produces three levels of precedence (lowest to highest):
//  1. Suite defaults (lowest)
//  2. Suite entry override
//  3. Test definition settings (highest)
//
// Four cases cover the complete nil/non-nil combinations for EchoFidelity:
//  a. Suite default propagates when entry and definition are nil
//  b. Entry override wins over suite default
//  c. Definition setting wins over entry override
//  d. All nil at all three levels resolves to nil in merged settings
//     (EffectiveEchoFidelity resolves nil to required at evaluation time)
//
// These tests will not compile until I2.1 adds EchoFidelity to
// domain.RunSettings and defines EchoFidelityRequired / EchoFidelityAdvisory.
// They will then fail at runtime until I2.2 wires the YAML field through
// toDomain and I2.4 adds the nil-check merge for EchoFidelity.

import (
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// echoFidelityBaseDefinition is a minimal valid orchestrator test definition
// without an echo_fidelity field. Used for cases where the definition level
// should contribute nil to the merge.
const echoFidelityBaseDefinition = `
schema_version: 1
name: echo-merge-test
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
stub_registry: echo-merge-test.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`

// echoFidelityAdvisoryDefinition is a minimal valid orchestrator test
// definition with echo_fidelity: advisory at the definition level.
const echoFidelityAdvisoryDefinition = `
schema_version: 1
name: echo-merge-test
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: orchestrator
echo_fidelity: advisory
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: echo-merge-test.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`

// echoMergeRegistry is the stub registry referenced by the above definitions.
const echoMergeRegistry = `{
  "schema_version": 1,
  "test_id": "echo-merge-test",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "Task", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

// echoFidelityMergeTree builds a fixture tree under t.TempDir() with the
// given suite YAML and definition YAML, plus the shared stub registry and
// stub agent placeholder.
func echoFidelityMergeTree(t *testing.T, suiteYAML, definitionYAML string) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"s.suite.yaml":                suiteYAML,
		"echo-merge-test.test.yaml":   definitionYAML,
		"echo-merge-test.stubs.json":  echoMergeRegistry,
		"agents/researcher.md":        "# stub researcher placeholder",
	})
}

// TestValidate_EchoFidelityMerge_SuiteDefaultPropagates verifies that when
// the suite declares echo_fidelity in its defaults and neither the suite
// entry nor the definition overrides it, the resolved test settings carry the
// suite default value. This is the "nil override leaves base unchanged" rule
// from mergeSettings applied to the EchoFidelity pointer field.
func TestValidate_EchoFidelityMerge_SuiteDefaultPropagates(t *testing.T) {
	suiteYAML := `
schema_version: 1
id: s
defaults:
  echo_fidelity: advisory
tests:
  - path: echo-merge-test.test.yaml
`
	root := echoFidelityMergeTree(t, suiteYAML, echoFidelityBaseDefinition)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("plan.Tests is empty; want one resolved test")
	}

	got := plan.Tests[0].Settings.EchoFidelity
	if got == nil {
		t.Fatal("Settings.EchoFidelity is nil; want pointer to advisory — " +
			"suite default must propagate when entry and definition do not override it")
	}
	if *got != domain.EchoFidelityAdvisory {
		t.Errorf("Settings.EchoFidelity = %q, want %q; "+
			"suite default must propagate when entry and definition are nil",
			*got, domain.EchoFidelityAdvisory)
	}
}

// TestValidate_EchoFidelityMerge_EntryOverrideWins verifies that when the
// suite default and the suite entry both declare echo_fidelity with different
// values, the entry value wins over the suite default.
func TestValidate_EchoFidelityMerge_EntryOverrideWins(t *testing.T) {
	suiteYAML := `
schema_version: 1
id: s
defaults:
  echo_fidelity: advisory
tests:
  - path: echo-merge-test.test.yaml
    echo_fidelity: required
`
	root := echoFidelityMergeTree(t, suiteYAML, echoFidelityBaseDefinition)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("plan.Tests is empty; want one resolved test")
	}

	got := plan.Tests[0].Settings.EchoFidelity
	if got == nil {
		t.Fatal("Settings.EchoFidelity is nil; want pointer to required — " +
			"entry override must win over suite default")
	}
	if *got != domain.EchoFidelityRequired {
		t.Errorf("Settings.EchoFidelity = %q, want %q; "+
			"entry override must take priority over suite default",
			*got, domain.EchoFidelityRequired)
	}
}

// TestValidate_EchoFidelityMerge_DefinitionWinsOverEntry verifies that when
// the suite default, the suite entry, and the definition all declare
// echo_fidelity, the definition value takes the highest precedence.
func TestValidate_EchoFidelityMerge_DefinitionWinsOverEntry(t *testing.T) {
	// Suite and entry both say "required"; definition says "advisory".
	// The definition must win.
	suiteYAML := `
schema_version: 1
id: s
defaults:
  echo_fidelity: required
tests:
  - path: echo-merge-test.test.yaml
    echo_fidelity: required
`
	root := echoFidelityMergeTree(t, suiteYAML, echoFidelityAdvisoryDefinition)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("plan.Tests is empty; want one resolved test")
	}

	got := plan.Tests[0].Settings.EchoFidelity
	if got == nil {
		t.Fatal("Settings.EchoFidelity is nil; want pointer to advisory — " +
			"definition-level setting must win over suite default and entry override")
	}
	if *got != domain.EchoFidelityAdvisory {
		t.Errorf("Settings.EchoFidelity = %q, want %q; "+
			"definition setting must take the highest precedence in the three-level merge",
			*got, domain.EchoFidelityAdvisory)
	}
}

// TestValidate_EchoFidelityMerge_AllNilResolvesToNil verifies that when no
// level declares echo_fidelity, the merged settings carry nil for the field.
// nil is the correct "not stated" sentinel; domain.EffectiveEchoFidelity
// resolves nil to EchoFidelityRequired at evaluation time — the test verifies
// the merge output, not the evaluation-time resolution.
func TestValidate_EchoFidelityMerge_AllNilResolvesToNil(t *testing.T) {
	suiteYAML := `
schema_version: 1
id: s
tests:
  - path: echo-merge-test.test.yaml
`
	root := echoFidelityMergeTree(t, suiteYAML, echoFidelityBaseDefinition)

	plan, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Fatalf("Validate returned unexpected errors: %v", report.Diagnostics)
	}
	if len(plan.Tests) == 0 {
		t.Fatal("plan.Tests is empty; want one resolved test")
	}

	if plan.Tests[0].Settings.EchoFidelity != nil {
		t.Errorf("Settings.EchoFidelity = %v, want nil; "+
			"when no level states echo_fidelity the merged field must remain nil "+
			"(EffectiveEchoFidelity resolves nil to required at evaluation time)",
			plan.Tests[0].Settings.EchoFidelity)
	}
}
