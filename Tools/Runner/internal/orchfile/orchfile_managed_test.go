package orchfile_test

import (
	"bytes"
	"testing"

	"mosaic-run/internal/orchfile"
)

//
// deploy-managed.md is a snapshot of what mosaic-deploy produces: workflow regions carry
// type="managed" (NodeDeployed) nested inside <AvailableWorkflows type="managed">.
// The current implementation uses SectionsDeep(), which only visits NodeSection nodes and
// therefore misses these regions entirely.  Every test in this group must fail (RED) until
// the traversal is made kind-agnostic.

func TestEnumerateWorkflows_DeployManaged_ReturnsBothWorkflows(t *testing.T) {
	// deploy-managed.md declares two workflows: quick-fix and greenfield-tdd.
	// EnumerateWorkflows must return both when the traversal is kind-agnostic.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))

	if err != nil {
		t.Fatalf("EnumerateWorkflows returned unexpected error for deploy-managed fixture: %v", err)
	}
	if len(regions) != 2 {
		t.Errorf("want 2 workflow regions from deploy-managed fixture, got %d", len(regions))
	}
}

func TestEnumerateWorkflows_DeployManaged_FirstWorkflow_ID(t *testing.T) {
	// The first injected workflow must be quick-fix (document order).
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 1 {
		t.Fatalf("want at least 1 region, got 0")
	}

	got := string(regions[0].Info.ID)
	if got != "quick-fix" {
		t.Errorf("regions[0].Info.ID: want %q, got %q", "quick-fix", got)
	}
}

func TestEnumerateWorkflows_DeployManaged_FirstWorkflow_Version(t *testing.T) {
	// The version attribute on the managed workflow tag must be read correctly.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 1 {
		t.Fatalf("want at least 1 region, got 0")
	}

	got := string(regions[0].Info.Version)
	if got != "3.0" {
		t.Errorf("regions[0].Info.Version: want %q, got %q", "3.0", got)
	}
}

func TestEnumerateWorkflows_DeployManaged_SecondWorkflow_ID(t *testing.T) {
	// The second injected workflow must be greenfield-tdd (document order preserved).
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 2 {
		t.Fatalf("want at least 2 regions, got %d", len(regions))
	}

	got := string(regions[1].Info.ID)
	if got != "greenfield-tdd" {
		t.Errorf("regions[1].Info.ID: want %q, got %q", "greenfield-tdd", got)
	}
}

func TestEnumerateWorkflows_DeployManaged_SecondWorkflow_Version(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 2 {
		t.Fatalf("want at least 2 regions, got %d", len(regions))
	}

	got := string(regions[1].Info.Version)
	if got != "3.4" {
		t.Errorf("regions[1].Info.Version: want %q, got %q", "3.4", got)
	}
}

func TestGetWorkflow_DeployManaged_SelectFirstByID_ReturnsRegion(t *testing.T) {
	// GetWorkflow must find a workflow region by its identifier even when the region
	// carries type="managed" (NodeDeployed) rather than type="core" (NodeSection).
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "quick-fix")

	if err != nil {
		t.Fatalf("GetWorkflow returned unexpected error for deploy-managed fixture: %v", err)
	}
	got := string(region.Info.ID)
	if got != "quick-fix" {
		t.Errorf("Info.ID: want %q, got %q", "quick-fix", got)
	}
}

func TestGetWorkflow_DeployManaged_SelectFirstByID_ReturnsVersion(t *testing.T) {
	// The returned region must carry the correct version attribute from the managed tag.
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "quick-fix")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	got := string(region.Info.Version)
	if got != "3.0" {
		t.Errorf("Info.Version: want %q, got %q", "3.0", got)
	}
}

func TestGetWorkflow_DeployManaged_SelectFirstByID_ContentIsNonEmpty(t *testing.T) {
	// The routing-table content between the managed boundary tags must be returned.
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "quick-fix")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	if len(bytes.TrimSpace(region.Content)) == 0 {
		t.Error("Content must not be empty for a managed workflow region with table content")
	}
}

func TestGetWorkflow_DeployManaged_SelectFirstByID_ContentContainsRoutingTable(t *testing.T) {
	// The routing table rows declared inside the workflow region must appear in Content.
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "quick-fix")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	// The quick-fix workflow table in deploy-managed.md uses these subagent names.
	if !bytes.Contains(region.Content, []byte("planner-tdd-soft")) {
		t.Error("Content must contain the routing table rows from the quick-fix workflow")
	}
}

func TestGetWorkflow_DeployManaged_SelectSecondByID_ReturnsRegion(t *testing.T) {
	// GetWorkflow must select the correct workflow when multiple managed regions exist.
	region, err := orchfile.GetWorkflow(orchfileFixture("deploy-managed.md"), "greenfield-tdd")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}

	got := string(region.Info.ID)
	if got != "greenfield-tdd" {
		t.Errorf("Info.ID: want %q, got %q", "greenfield-tdd", got)
	}
	if string(region.Info.Version) != "3.4" {
		t.Errorf("Info.Version: want %q, got %q", "3.4", string(region.Info.Version))
	}
}

// --- EnumerateWorkflows: mixed authored/managed fixture ---
//
// mixed-workflows.md declares one type="core" (NodeSection) workflow followed by one
// type="managed" (NodeDeployed) workflow.  The traversal must interleave both in document
// order and must not return either region twice.

func TestEnumerateWorkflows_MixedTypes_ReturnsBothWorkflows(t *testing.T) {
	// A file containing one authored (type="core") and one managed (type="managed") workflow
	// must enumerate both.  The current SectionsDeep() traversal misses the managed one,
	// returning only 1 region instead of 2.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("mixed-workflows.md"))

	if err != nil {
		t.Fatalf("EnumerateWorkflows returned unexpected error for mixed fixture: %v", err)
	}
	if len(regions) != 2 {
		t.Errorf("want exactly 2 workflow regions from mixed fixture, got %d", len(regions))
	}
}

func TestEnumerateWorkflows_MixedTypes_PreservesDocumentOrder(t *testing.T) {
	// The authored workflow appears first in the file; the managed workflow second.
	// Document order must be preserved regardless of region kind.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("mixed-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d — cannot verify order", len(regions))
	}

	firstID := string(regions[0].Info.ID)
	secondID := string(regions[1].Info.ID)
	if firstID != "authored-workflow" {
		t.Errorf("regions[0].Info.ID: want %q (authored, appears first), got %q", "authored-workflow", firstID)
	}
	if secondID != "managed-workflow" {
		t.Errorf("regions[1].Info.ID: want %q (managed, appears second), got %q", "managed-workflow", secondID)
	}
}

func TestEnumerateWorkflows_MixedTypes_NoDuplicates(t *testing.T) {
	// The kind-agnostic traversal must not emit either region twice.
	// A naive merge of SectionsDeep() and DeployedRegions() could produce duplicates
	// when both lists are walked and the same node appears in both.
	//
	// NOTE: This test passes trivially in the RED phase. The broken SectionsDeep()
	// traversal returns only 1 region (the authored type="core" workflow; the managed
	// one is missed), so no identifier can appear more than once in a 1-element slice.
	// The sibling test TestEnumerateWorkflows_MixedTypes_ReturnsBothWorkflows is what
	// correctly fails RED and guards the prerequisite. This test becomes meaningful only
	// after the kind-agnostic traversal fix lands, at which point it verifies that both
	// regions appear exactly once rather than twice.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("mixed-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}

	seen := map[string]int{}
	for _, r := range regions {
		seen[string(r.Info.ID)]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("workflow identifier %q appears %d times; want exactly 1 (no duplicates)", id, count)
		}
	}
}

func TestEnumerateWorkflows_MixedTypes_AuthoredWorkflow_VersionCorrect(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("mixed-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 1 {
		t.Fatalf("want at least 1 region, got 0")
	}

	got := string(regions[0].Info.Version)
	if got != "2.0" {
		t.Errorf("authored workflow version: want %q, got %q", "2.0", got)
	}
}

func TestEnumerateWorkflows_MixedTypes_ManagedWorkflow_VersionCorrect(t *testing.T) {
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("mixed-workflows.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 2 {
		t.Fatalf("want at least 2 regions, got %d", len(regions))
	}

	got := string(regions[1].Info.Version)
	if got != "5.1" {
		t.Errorf("managed workflow version: want %q, got %q", "5.1", got)
	}
}

// --- EnumerateInfrastructureAgents: deploy-managed fixture ---
//
// deploy-managed.md declares one infrastructure agent inside
// <InfrastructureAgents type="managed">.  Individual InfrastructureAgent blocks carry
// type="core" (NodeSection), so the current SectionsDeep() traversal finds them even
// inside a NodeDeployed container.  The test below confirms the enumeration works
// correctly against the deploy-managed fixture and documents the expected count —
// informing whether I1.2 must apply the same kind-agnostic fix to infra agent discovery.

func TestEnumerateInfrastructureAgents_DeployManaged_ReturnsOneDeclaredAgent(t *testing.T) {
	// deploy-managed.md declares exactly one infrastructure agent (orchestration-review).
	// Individual InfrastructureAgent blocks use type="core" (NodeSection), so SectionsDeep()
	// reaches them even though their parent InfrastructureAgents region uses type="managed".
	// This test confirms infra agent enumeration is NOT broken by the same defect as workflow
	// enumeration, and documents the expected count for the deploy-managed fixture.
	agents, err := orchfile.EnumerateInfrastructureAgents(orchfileFixture("deploy-managed.md"))

	if err != nil {
		t.Fatalf("EnumerateInfrastructureAgents returned unexpected error: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("want 1 infrastructure agent from deploy-managed fixture, got %d", len(agents))
	}
}

func TestEnumerateInfrastructureAgents_DeployManaged_AgentName(t *testing.T) {
	// The declared agent must be identified by name from the InfrastructureAgent tag.
	agents, err := orchfile.EnumerateInfrastructureAgents(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateInfrastructureAgents: %v", err)
	}
	if len(agents) < 1 {
		t.Fatalf("want at least 1 agent, got 0")
	}

	if agents[0].Name != "orchestration-review" {
		t.Errorf("agents[0].Name: want %q, got %q", "orchestration-review", agents[0].Name)
	}
}

func TestEnumerateInfrastructureAgents_DeployManaged_AgentClass(t *testing.T) {
	// The Class column from the agent's declaration table must be read correctly.
	agents, err := orchfile.EnumerateInfrastructureAgents(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateInfrastructureAgents: %v", err)
	}
	if len(agents) < 1 {
		t.Fatalf("want at least 1 agent, got 0")
	}

	if agents[0].Class != "review" {
		t.Errorf("agents[0].Class: want %q, got %q", "review", agents[0].Class)
	}
}

// --- EnumerateWorkflows: boundary-tag exclusion for managed fixture Content ---

func TestEnumerateWorkflows_DeployManaged_Content_ExcludesBoundaryTagLines(t *testing.T) {
	// Content must be the raw bytes between the boundary tags, not including the opening
	// <Workflow type="managed" ...> and closing </Workflow> lines themselves.
	// This is the managed-fixture counterpart of TestEnumerateWorkflows_BareFile_Content_ExcludesBoundaryTagLines.
	// If boundary-tag stripping is inadvertently kind-specific, the managed opening tag
	// could leak into Content; this test catches that regression.
	regions, err := orchfile.EnumerateWorkflows(orchfileFixture("deploy-managed.md"))
	if err != nil {
		t.Fatalf("EnumerateWorkflows: %v", err)
	}
	if len(regions) < 1 {
		t.Fatalf("want at least 1 region, got 0")
	}

	for i, r := range regions {
		if bytes.Contains(r.Content, []byte("<Workflow type=\"managed\"")) {
			t.Errorf("regions[%d].Content must not contain the opening boundary tag line", i)
		}
		if bytes.Contains(r.Content, []byte("</Workflow>")) {
			t.Errorf("regions[%d].Content must not contain the closing boundary tag line", i)
		}
	}
}
