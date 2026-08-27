package ghcpcli_test

// agents_dir_test.go is a cross-module consistency test. It asserts that the
// paths.agents.project value in the embedded ghcp-cli.yaml descriptor matches
// the AgentsDir field in the shared CLIHarness catalog entry for "ghcp-cli".
//
// This test guards against drift between the Deployment tool's YAML manifests
// and the Go-code catalog in Tools/Common/harness/catalog.go. If either side
// changes without updating the other, this test fails immediately.
//
// Note: the correct agents directory for GHCP CLI is ".github/agents"
// (not ".ghcp/agents"). This is what the ghcp-cli.yaml descriptor specifies.

import (
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-deploy/internal/harness/builtin/ghcpcli"
)

// TestAgentsDirMatchesDescriptor asserts that the ghcp-cli embedded YAML's
// paths.agents.project value equals the AgentsDir field that the shared
// catalog exposes for the "ghcp-cli" harness ID.
func TestAgentsDirMatchesDescriptor(t *testing.T) {
	desc := ghcpcli.DescriptorForTesting(t)

	entry, ok := commonharness.LookupCLIHarness(commonharness.HarnessIDGHCPCLI)
	if !ok {
		t.Fatalf("LookupCLIHarness(%q): not found in shared catalog", commonharness.HarnessIDGHCPCLI)
	}

	yamlValue := desc.Paths.Agents.Project
	if yamlValue == "" {
		t.Fatal("ghcp-cli descriptor paths.agents.project is empty: descriptor may be malformed")
	}

	if entry.AgentsDir != yamlValue {
		t.Errorf("catalog/YAML drift for ghcp-cli agents directory:\n  shared catalog AgentsDir = %q\n  YAML paths.agents.project = %q\n  Update catalog.go to match the YAML (or vice versa).",
			entry.AgentsDir, yamlValue)
	}
}
