package app_test

// e2e_protocol_propagation_test.go verifies the protocol propagation end-to-end path:
//
//   (a) Deploying an agent with a <CommunicationProtocol type="managed"> region produces a
//       version="X" attribute on that region's open tag, where X is the version declared in
//       the protocol source document.
//
//   (b) Writing a new version of the protocol source document and re-applying the transform
//       (simulating "rerun the tool") changes every deployed agent's protocol block with no
//       agent source file edited and no rebuild — the deployment tool reads protocol content
//       at runtime from the filesystem.
//
//   (c) The role-correct protocol block is selected: a worker agent receives the Subagent
//       block and an orchestrator receives the Orchestrator block.
//
// These tests exercise transform.Apply directly, using a synthetic agent source document
// so the assertions focus on protocol propagation rather than harness-specific field shaping.
// The protocol source file is written to a temp directory to isolate the test from the live
// repository and to make mutation observable without touching committed files.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Protocol source helpers
// ---------------------------------------------------------------------------

// protocolSourceTemplate is a minimal valid CommunicationProtocol.md template.
// The %s verbs receive version, subagentContent, and orchestratorContent.
const protocolSourceTemplate = `---
version: "%s"
---

<CommunicationProtocol type="core" name="Subagent">
%s
</CommunicationProtocol>

<CommunicationProtocol type="core" name="Orchestrator">
%s
</CommunicationProtocol>
`

// writeProtocolSource writes a CommunicationProtocol.md to the canonical path under root
// and returns the path to the created file.
func writeProtocolSource(t *testing.T, root, version, subagentContent, orchestratorContent string) string {
	t.Helper()
	dir := filepath.Join(root, "Development", "Designs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create protocol dir: %v", err)
	}
	content := fmt.Sprintf(protocolSourceTemplate, version, subagentContent, orchestratorContent)
	path := filepath.Join(dir, "CommunicationProtocol.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write protocol source: %v", err)
	}
	return path
}

// loadProtocolFrom loads ProtocolContent from the given root using the filesystem loader.
func loadProtocolFrom(t *testing.T, root string) domain.ProtocolContent {
	t.Helper()
	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("load protocol from %s: %v", root, err)
	}
	return content
}

// ---------------------------------------------------------------------------
// Synthetic agent source for protocol-only tests
// ---------------------------------------------------------------------------

// protocolOnlyAgentSource is a minimal agent source file that carries exactly one
// <CommunicationProtocol type="managed"> region and no harness-managed injections. Using a
// synthetic source isolates the protocol propagation behaviour from harness-specific shaping.
const protocolOnlyAgentSource = `---
id: 42
version: 1.0.0
name: protocol-e2e-agent
description: Synthetic agent for protocol propagation tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
This agent is used exclusively for protocol propagation testing.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>
`

// applyProtocolTest calls transform.Apply for the synthetic agent source with the given
// role and protocol content. Returns the output bytes or fails the test.
func applyProtocolTest(t *testing.T, role domain.AgentRole, protocol domain.ProtocolContent) []byte {
	t.Helper()
	req := transform.Request{
		Source:   []byte(protocolOnlyAgentSource),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-e2e-agent",
		Module:   newMinimalModule(),
		Model:    domain.ModelSelection{ModelID: "test-model", Origin: domain.OriginHarnessList},
		Scope:    domain.ScopeProject,
		Role:     role,
		Protocol: protocol,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	return result.Output
}

// ---------------------------------------------------------------------------
// (a) Version marker appears in deployed output
// ---------------------------------------------------------------------------

// TestE2E_ProtocolPropagation_VersionMarkerAppearsInOutput verifies that deploying an agent
// whose source carries an empty <CommunicationProtocol type="managed"> boundary produces a
// version="X" attribute on the region's open tag, where X matches the version field of the
// protocol source document loaded for the run.
func TestE2E_ProtocolPropagation_VersionMarkerAppearsInOutput(t *testing.T) {
	root := t.TempDir()
	const version = "1.9"
	writeProtocolSource(t, root, version,
		"## Communication Protocol\n\nSubagent block content.\n",
		"## Communication Protocol\n\nOrchestrator block content.\n",
	)

	protocol := loadProtocolFrom(t, root)
	output := applyProtocolTest(t, domain.RoleWorker, protocol)

	wantMarker := []byte("<CommunicationProtocol type=\"managed\" version=\"" + version + "\">")
	if !bytes.Contains(output, wantMarker) {
		t.Errorf("output does not contain protocol version attribute %q\n"+
			"output (first 1200 bytes):\n%s",
			wantMarker, e2eProtocolTruncate(output, 1200))
	}
}

// ---------------------------------------------------------------------------
// (b) Editing protocol source and redeploying changes the output — no rebuild required
// ---------------------------------------------------------------------------

// TestE2E_ProtocolPropagation_VersionChangesWhenSourceChanges verifies that writing a new
// protocol source version to disk and re-loading (simulating a tool rerun without rebuild)
// changes the deployed agent's protocol content and version marker.
//
// This test demonstrates that protocol content is read at runtime from the filesystem: there
// is no go:embed or compile-time constant that would require a rebuild for the change to
// reach deployed agents.
func TestE2E_ProtocolPropagation_VersionChangesWhenSourceChanges(t *testing.T) {
	root := t.TempDir()

	// First deploy — version 1.9.
	writeProtocolSource(t, root, "1.9",
		"## Communication Protocol\n\nSubagent content v1.9.\n",
		"## Communication Protocol\n\nOrchestrator content v1.9.\n",
	)
	protocol1 := loadProtocolFrom(t, root)
	output1 := applyProtocolTest(t, domain.RoleWorker, protocol1)

	if !bytes.Contains(output1, []byte("<CommunicationProtocol type=\"managed\" version=\"1.9\">")) {
		t.Fatalf("first deploy: expected protocol version attribute 1.9 in output; got (first 1200 bytes):\n%s",
			e2eProtocolTruncate(output1, 1200))
	}

	// Simulate "edit the protocol source file" — overwrite with version 2.0 in the same dir.
	// No rebuild is performed; the next deploy simply re-reads the file.
	writeProtocolSource(t, root, "2.0",
		"## Communication Protocol\n\nSubagent content v2.0.\n",
		"## Communication Protocol\n\nOrchestrator content v2.0.\n",
	)
	protocol2 := loadProtocolFrom(t, root)
	output2 := applyProtocolTest(t, domain.RoleWorker, protocol2)

	if !bytes.Contains(output2, []byte("<CommunicationProtocol type=\"managed\" version=\"2.0\">")) {
		t.Errorf("second deploy: expected protocol version attribute 2.0 in output; got (first 1200 bytes):\n%s",
			e2eProtocolTruncate(output2, 1200))
	}
	if bytes.Contains(output2, []byte("version=\"1.9\"")) {
		t.Errorf("second deploy: stale protocol version 1.9 still present after source was updated to 2.0")
	}
}

// ---------------------------------------------------------------------------
// (c) Role-correct block is selected
// ---------------------------------------------------------------------------

// TestE2E_ProtocolPropagation_WorkerReceivesSubagentBlock verifies that a worker agent
// receives the CommunicationProtocol:Subagent block, not the Orchestrator block.
func TestE2E_ProtocolPropagation_WorkerReceivesSubagentBlock(t *testing.T) {
	root := t.TempDir()
	writeProtocolSource(t, root, "1.9",
		"SUBAGENT_PROTOCOL_MARKER",
		"ORCHESTRATOR_PROTOCOL_MARKER",
	)
	protocol := loadProtocolFrom(t, root)
	output := applyProtocolTest(t, domain.RoleWorker, protocol)

	if !bytes.Contains(output, []byte("SUBAGENT_PROTOCOL_MARKER")) {
		t.Errorf("worker agent output does not contain the subagent protocol block marker; "+
			"want the Subagent block to be selected for worker agents")
	}
	if bytes.Contains(output, []byte("ORCHESTRATOR_PROTOCOL_MARKER")) {
		t.Errorf("worker agent output contains the orchestrator protocol block marker; "+
			"the Orchestrator block must not appear in a worker agent's deployed file")
	}
}

// TestE2E_ProtocolPropagation_OrchestratorReceivesOrchestratorBlock verifies that an
// orchestrator agent receives the CommunicationProtocol:Orchestrator block, not the
// Subagent block.
func TestE2E_ProtocolPropagation_OrchestratorReceivesOrchestratorBlock(t *testing.T) {
	root := t.TempDir()
	writeProtocolSource(t, root, "1.9",
		"SUBAGENT_PROTOCOL_MARKER",
		"ORCHESTRATOR_PROTOCOL_MARKER",
	)
	protocol := loadProtocolFrom(t, root)
	output := applyProtocolTest(t, domain.RoleOrchestrator, protocol)

	if !bytes.Contains(output, []byte("ORCHESTRATOR_PROTOCOL_MARKER")) {
		t.Errorf("orchestrator agent output does not contain the orchestrator protocol block marker; "+
			"want the Orchestrator block to be selected for orchestrator agents")
	}
	if bytes.Contains(output, []byte("SUBAGENT_PROTOCOL_MARKER")) {
		t.Errorf("orchestrator agent output contains the subagent protocol block marker; "+
			"the Subagent block must not appear in an orchestrator agent's deployed file")
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// e2eProtocolTruncate returns the first n bytes of b for use in diagnostic messages.
// It is package-internal to avoid name conflicts with other test files in this package.
func e2eProtocolTruncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
