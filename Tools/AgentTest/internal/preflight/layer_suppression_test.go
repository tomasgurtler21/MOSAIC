package preflight_test

// Tests for T13.1 / AC13.3: preflight must warn or reject when a test
// declares an assertion class its declared layer suppresses, so this
// authoring mistake is caught before a run rather than discovered by reading
// raw JSON output. ContractsDesign.md's "Preflight diagnostic codes" table
// names the code "suppressed-assertion" (severity: warning, so the shipped
// layer-rule fixtures that declare suppression deliberately keep validating)
// and states the rule is sourced from domain.LayerSuppresses /
// domain.SuppressionReason — the single shared home for a rule internal/
// evaluate already implements, so preflight and evaluate cannot drift.
//
// Current rule content (ContractsDesign.md, "domain layer-suppression
// rule"): layer: orchestrator suppresses protocol_violations. Nothing else
// is suppressed, across every layer the tool supports (orchestrator,
// subagent, integration).

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

const orchestratorLayerProtocolViolationsDefinition = `
schema_version: 1
name: happy-path
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
stub_registry: happy-path.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  protocol_violations: 0
`

const subagentLayerProtocolViolationsDefinition = `
schema_version: 1
name: happy-path
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: subagent
subject:
  identity: researcher
  agent: researcher
  infrastructure_agents: []
stub_registry: happy-path.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  protocol_violations: 0
`

const integrationLayerProtocolViolationsDefinition = `
schema_version: 1
name: happy-path
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial."
layer: integration
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: happy-path.stubs.json
stub_agents:
  - identity:
      tool: Task
      agent: researcher
    source: agents/researcher.md
assertions:
  protocol_violations: 0
`

// TestValidate_LayerSuppressedAssertion_ReportsWarning is the direct AC13.3
// check: a test declaring layer: orchestrator alongside protocol_violations
// — the exact combination internal/evaluate silently ignores — must produce
// a "suppressed-assertion" diagnostic naming both the class and the layer,
// rather than validating cleanly and letting the mistake surface only in
// raw JSON output after a run.
func TestValidate_LayerSuppressedAssertion_ReportsWarning(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  orchestratorLayerProtocolViolationsDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	var found bool
	for _, d := range report.Diagnostics {
		if d.Code != "suppressed-assertion" {
			continue
		}
		found = true
		if d.Severity != "warning" {
			t.Errorf("suppressed-assertion diagnostic Severity = %q, want %q — a warning catches the mistake without making a deliberate layer-rule fixture unrunnable", d.Severity, "warning")
		}
		if !strings.Contains(d.Message, string(domain.ClassProtocolViolations)) {
			t.Errorf("suppressed-assertion diagnostic Message = %q, does not name the suppressed class %q", d.Message, domain.ClassProtocolViolations)
		}
		if !strings.Contains(d.Message, string(domain.LayerOrchestrator)) {
			t.Errorf("suppressed-assertion diagnostic Message = %q, does not name the suppressing layer %q", d.Message, domain.LayerOrchestrator)
		}
	}
	if !found {
		t.Errorf("expected a diagnostic with code %q, got: %v", "suppressed-assertion", report.Diagnostics)
	}
}

// TestValidate_LayerSuppressedAssertion_IsWarningNotError asserts the
// severity is a warning, not an error: report.HasErrors() must stay false so
// the shipped layer-rule fixtures — which declare suppression deliberately
// to exercise it — keep validating cleanly (AC13.2).
func TestValidate_LayerSuppressedAssertion_IsWarningNotError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  orchestratorLayerProtocolViolationsDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	if report.HasErrors() {
		t.Errorf("Validate: a suppressed-assertion diagnostic must be a warning, not an error; report.HasErrors() = true, diagnostics: %+v", report.Diagnostics)
	}
}

// TestValidate_UnsuppressedAssertion_ValidatesCleanly is the negative case
// across every layer the tool supports: a test declaring only assertion
// classes its layer actually evaluates raises no suppressed-assertion
// diagnostic.
func TestValidate_UnsuppressedAssertion_ValidatesCleanly(t *testing.T) {
	cases := []struct {
		name       string
		definition string
	}{
		{"subagent", subagentLayerProtocolViolationsDefinition},
		{"integration", integrationLayerProtocolViolationsDefinition},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"s.suite.yaml":          validSuite,
				"happy-path.test.yaml":  tc.definition,
				"happy-path.stubs.json": validRegistry,
				"agents/researcher.md":  "# stub researcher placeholder",
			})

			_, report := preflight.Validate(preflight.Input{
				SuitePath:   filepath.Join(root, "s.suite.yaml"),
				FixtureRoot: filepath.Join(root, "fixtures"),
				HarnessID:   "claude-code",
			})

			for _, d := range report.Diagnostics {
				if d.Code == "suppressed-assertion" {
					t.Errorf("Validate: unexpected suppressed-assertion diagnostic %+v for layer %q, which evaluates protocol_violations in full", d, tc.name)
				}
			}
		})
	}
}

// TestValidate_HappyPath_NoSuppressedAssertionDiagnostic pins the reference
// example tree (layer: orchestrator, no protocol_violations declared): it
// must not gain a suppressed-assertion diagnostic it never had before this
// guardrail existed, keeping TestValidate_HappyPath_NoErrors's "clean tree"
// guarantee intact.
func TestValidate_HappyPath_NoSuppressedAssertionDiagnostic(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry,
		"agents/researcher.md":  "# stub researcher placeholder",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
	})

	for _, d := range report.Diagnostics {
		if d.Code == "suppressed-assertion" {
			t.Errorf("Validate: unexpected suppressed-assertion diagnostic %+v for a definition that declares no protocol_violations assertion", d)
		}
	}
}
