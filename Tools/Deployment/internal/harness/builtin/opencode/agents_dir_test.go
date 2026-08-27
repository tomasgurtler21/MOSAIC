package opencode_test

// agents_dir_test.go is a cross-module consistency test. It asserts that the
// paths.agents.project value in the embedded opencode.yaml descriptor matches
// the AgentsDir field in the shared CLIHarness catalog entry for "opencode".
//
// This test guards against drift between the Deployment tool's YAML manifests
// and the Go-code catalog in Tools/Common/harness/catalog.go. If either side
// changes without updating the other, this test fails immediately.

import (
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-deploy/internal/harness/builtin/opencode"
)

// TestAgentsDirMatchesDescriptor asserts that the opencode embedded YAML's
// paths.agents.project value equals the AgentsDir field that the shared
// catalog exposes for the "opencode" harness ID.
func TestAgentsDirMatchesDescriptor(t *testing.T) {
	desc := opencode.DescriptorForTesting(t)

	entry, ok := commonharness.LookupCLIHarness(commonharness.HarnessIDOpenCode)
	if !ok {
		t.Fatalf("LookupCLIHarness(%q): not found in shared catalog", commonharness.HarnessIDOpenCode)
	}

	yamlValue := desc.Paths.Agents.Project
	if yamlValue == "" {
		t.Fatal("opencode descriptor paths.agents.project is empty: descriptor may be malformed")
	}

	if entry.AgentsDir != yamlValue {
		t.Errorf("catalog/YAML drift for opencode agents directory:\n  shared catalog AgentsDir = %q\n  YAML paths.agents.project = %q\n  Update catalog.go to match the YAML (or vice versa).",
			entry.AgentsDir, yamlValue)
	}
}
