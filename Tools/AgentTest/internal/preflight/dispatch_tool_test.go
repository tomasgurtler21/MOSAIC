package preflight_test

// Tests for T11.3: preflight rejects a declared dispatch-tool identity the
// selected harness cannot produce, rather than letting the dispatch fail
// silently as "unmatched" at run time. ContractsDesign.md's "Preflight
// diagnostic codes" table names the code: "unproducible-dispatch-tool",
// checked against Input.Capabilities.ProducibleToolNames.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// unproducibleRegistry declares a stub for an identity whose tool name will
// not appear in the Capabilities this test supplies.
const unproducibleRegistry = `{
  "schema_version": 1,
  "test_id": "happy-path",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "some-tool-this-harness-cannot-produce", "agent": "researcher", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

func TestValidate_DeclaredIdentityUnproducibleByHarness_ReportsError(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": unproducibleRegistry,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Capabilities: domain.HarnessCapabilities{
			ProducibleToolNames: []string{domain.DispatchToolName},
		},
	})

	if !report.HasErrors() {
		t.Fatal("expected a declared identity whose tool name the selected harness cannot produce to be rejected by preflight")
	}

	var found bool
	for _, d := range report.Diagnostics {
		if d.Code != "unproducible-dispatch-tool" {
			continue
		}
		found = true
		if d.Severity != "error" {
			t.Errorf("unproducible-dispatch-tool diagnostic Severity = %q, want %q", d.Severity, "error")
		}
		if !strings.Contains(d.Message, "some-tool-this-harness-cannot-produce") {
			t.Errorf("unproducible-dispatch-tool diagnostic Message = %q, does not name the declared tool name", d.Message)
		}
		if !strings.Contains(d.Message, domain.DispatchToolName) {
			t.Errorf("unproducible-dispatch-tool diagnostic Message = %q, does not name the producible set", d.Message)
		}
	}
	if !found {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unproducible-dispatch-tool", report.Diagnostics)
	}
}

// TestValidate_DeclaredIdentityProducibleByHarness_ValidatesCleanly is the
// negative case: a declared identity whose tool name IS in the selected
// harness's producible set raises no unproducible-dispatch-tool diagnostic.
func TestValidate_DeclaredIdentityProducibleByHarness_ValidatesCleanly(t *testing.T) {
	root := writeTree(t, map[string]string{
		"s.suite.yaml":          validSuite,
		"happy-path.test.yaml":  validDefinition,
		"happy-path.stubs.json": validRegistry, // declares { tool: "Task", agent: "researcher", ... }
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "s.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Capabilities: domain.HarnessCapabilities{
			ProducibleToolNames: []string{"Task"},
		},
	})

	for _, d := range report.Diagnostics {
		if d.Code == "unproducible-dispatch-tool" {
			t.Errorf("Validate: unexpected unproducible-dispatch-tool diagnostic %+v for an identity the selected harness can produce", d)
		}
	}
}
