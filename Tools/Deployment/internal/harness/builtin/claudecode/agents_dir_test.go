package claudecode_test

// agents_dir_test.go is a cross-module consistency test. It asserts that the
// paths.agents.project value in the embedded claude-code.yaml descriptor
// matches the AgentsDir field in the shared CLIHarness catalog entry for
// "claude-code".
//
// This test guards against drift between the Deployment tool's YAML manifests
// and the Go-code catalog in Tools/Common/harness/catalog.go. If either side
// changes without updating the other, this test fails immediately.

import (
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-deploy/internal/harness/builtin/claudecode"
)

// TestAgentsDirMatchesDescriptor asserts that the claude-code embedded YAML's
// paths.agents.project value equals the AgentsDir field that the shared
// catalog exposes for the "claude-code" harness ID.
func TestAgentsDirMatchesDescriptor(t *testing.T) {
	desc := claudecode.DescriptorForTesting(t)

	entry, ok := commonharness.LookupCLIHarness(commonharness.HarnessIDClaudeCode)
	if !ok {
		t.Fatalf("LookupCLIHarness(%q): not found in shared catalog", commonharness.HarnessIDClaudeCode)
	}

	yamlValue := desc.Paths.Agents.Project
	if yamlValue == "" {
		t.Fatal("claude-code descriptor paths.agents.project is empty: descriptor may be malformed")
	}

	if entry.AgentsDir != yamlValue {
		t.Errorf("catalog/YAML drift for claude-code agents directory:\n  shared catalog AgentsDir = %q\n  YAML paths.agents.project = %q\n  Update catalog.go to match the YAML (or vice versa).",
			entry.AgentsDir, yamlValue)
	}
}
