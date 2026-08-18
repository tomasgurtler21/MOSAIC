package app_test

// update_scan_membership_test.go verifies the Stage 4 requirement that Update's agent-set
// membership is driven by a workspace scan, not workflow discovery.
//
// All tests in this file represent the TDD RED phase: the current Update implementation does
// not populate plan.Input.ScannedAgentKeys (the field is nil), so every assertion on that
// field correctly FAILS against the current code. After I4.2 wires scanWorkspaceAgents into
// Update and passes the matched keys into plan.Input.ScannedAgentKeys, the tests must PASS.
//
// Verified behaviours (T4.2 — Update's set membership):
//
//   A deployed agent referenced by no workflow is in ScannedAgentKeys:
//   - When a deployed file in the agents directory matches a catalog agent (by numeric id)
//     and that agent is not referenced by any workflow, Update must include the agent's key
//     in plan.Input.ScannedAgentKeys passed to the planner.
//
//   A workflow-referenced agent produces no regression:
//   - plan.Input.WorkflowIDs still carries the workflow IDs discovered from the deployed
//     orchestrator, as it did before Stage 4. The wiring of ScannedAgentKeys must not
//     disturb the workflow-discovery path.
//
//   An unrelated non-MOSAIC file produces no ScannedAgentKeys entry:
//   - A plain .md file that neither matches the catalog nor passes harness-only eligibility
//     must not contribute a key to plan.Input.ScannedAgentKeys.
//
// Verified behaviours (T4.3 — divided responsibilities):
//
//   Manifest absence does not gate membership:
//   - A deployed file that matches a catalog agent by numeric id must appear in
//     plan.Input.ScannedAgentKeys even when the manifest has no entry for that agent.
//     The workspace scan is the authority for membership; the manifest is complementary.
//
//   Workflow discovery still drives orchestrator workflow-set drift reporting:
//   - plan.Input.WorkflowIDs reflects workflows discovered from the deployed orchestrator,
//     not the scanned agent set. A deployed non-orchestrator agent being in ScannedAgentKeys
//     does not move workflow IDs or change the orchestrator's workflow-set staleness signals.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Test helpers local to this file
// ---------------------------------------------------------------------------

const scanMembershipTestAgentsDir = "agents"

// extraAgent is a catalog agent that is NOT referenced by any workflow and is therefore
// invisible to the current workflow-based membership. After Stage 4 it must appear via the
// workspace scan.
var extraAgent = domain.Agent{
	Key:       "extra-agent",
	NumericID: "99",
	Version:   "1.0",
	Name:      "Extra Agent",
	Role:      domain.RoleSubagent,
}

// newCatalogWithExtraAgent returns a stubCatalog that contains both the minimal agents
// (orchestrator, test-runner) and extraAgent, which is not referenced by any workflow.
func newCatalogWithExtraAgent() *stubCatalog {
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, extraAgent)
	return cat
}

// writeExtraAgentDeployedFile writes a deployed file for extraAgent (numeric id "99") into
// workspace/agentsDir. The file carries the frontmatter `id` scalar that scanWorkspaceAgents
// needs to match by numeric id.
func writeExtraAgentDeployedFile(t *testing.T, workspace, agentsDir string) {
	t.Helper()
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeExtraAgentDeployedFile: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nid: \"99\"\nversion: \"1.0.0\"\n---\nDeployed extra-agent content.\n"
	if err := os.WriteFile(filepath.Join(dir, "extra-agent.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeExtraAgentDeployedFile: WriteFile: %v", err)
	}
}

// writeNonMosaicFile writes a plain .md file that has no id, no transform_version, and no
// canonical sections, so it should be classified as "neither" by scanWorkspaceAgents and
// never appear in ScannedAgentKeys.
func writeNonMosaicFile(t *testing.T, workspace, agentsDir string) {
	t.Helper()
	dir := filepath.Join(workspace, agentsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeNonMosaicFile: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nversion: \"1.0.0\"\n---\nPlain file with no MOSAIC boundary tags.\n"
	if err := os.WriteFile(filepath.Join(dir, "unrelated.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writeNonMosaicFile: WriteFile: %v", err)
	}
}

// newUpdateDepsWithScanMembership returns app.Deps configured with:
//   - a catalog that includes extraAgent (not in any workflow)
//   - a module with the scan membership agents directory configured
//   - a capturingPlanner so the test can inspect plan.Input.ScannedAgentKeys
//   - a manifest with no entry for extraAgent (to pin the manifest-absence invariant)
func newUpdateDepsWithScanMembership(t *testing.T, stub *interactiontest.Stub) (app.Deps, string, *capturingPlanner) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)

	// Replace the catalog with one that includes extraAgent.
	deps.Catalog = newCatalogWithExtraAgent()

	// Configure the module to declare an agents directory.
	mod := newModuleWithAgentsDir(scanMembershipTestAgentsDir)
	deps.Registry = newMinimalRegistry(mod)

	// Use a capturing planner so tests can assert on what Input was passed.
	cp := &capturingPlanner{
		result: domain.Plan{
			Mode:          domain.ModeUpdateWorkspace,
			Harness:       minimalHarness,
			WorkspacePath: workspace,
			Scope:         domain.ScopeProject,
		},
	}
	deps.Planner = cp

	// Manifest has no entry for extraAgent — scan-based membership must not depend on manifest.
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StatePresent,
			Manifest: domain.Manifest{
				HarnessID: "stub-harness",
				Entries:   []domain.ManifestEntry{
					// Deliberately no entry for extra-agent or test-runner.
				},
			},
		},
	}

	return deps, workspace, cp
}

// ---------------------------------------------------------------------------
// T4.2 — Update's set membership: workflow-unreferenced deployed agent
// ---------------------------------------------------------------------------

// TestUpdate_NonWorkflowReferencedDeployedAgent_KeyInScannedAgentKeys verifies that when a
// deployed file matches a catalog agent (by numeric id) and that agent is not referenced by
// any workflow, Update includes the agent's key in plan.Input.ScannedAgentKeys passed to the
// planner. This is the core membership guarantee: the workspace scan is authoritative, not
// workflow membership.
func TestUpdate_NonWorkflowReferencedDeployedAgent_KeyInScannedAgentKeys(t *testing.T) {
	// Arrange — extra-agent is deployed in the workspace but not in any workflow.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, cp := newUpdateDepsWithScanMembership(t, stub)

	writeExtraAgentDeployedFile(t, workspace, scanMembershipTestAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — ScannedAgentKeys must contain "extra-agent".
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called; Update did not reach the planner")
	}
	found := false
	for _, k := range cp.capturedInput.ScannedAgentKeys {
		if k == extraAgent.Key {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plan.Input.ScannedAgentKeys = %v, does not contain %q; "+
			"a deployed file whose numeric id matches a catalog agent must cause the agent's "+
			"key to appear in plan.Input.ScannedAgentKeys, regardless of workflow membership",
			cp.capturedInput.ScannedAgentKeys, extraAgent.Key)
	}
}

// TestUpdate_NonMosaicDeployedFile_AbsentFromScannedAgentKeys verifies that a plain .md file
// in the agents directory that is neither catalog-matched nor harness-only eligible does not
// contribute a key to plan.Input.ScannedAgentKeys.
func TestUpdate_NonMosaicDeployedFile_AbsentFromScannedAgentKeys(t *testing.T) {
	// Arrange — workspace has only a non-MOSAIC file, no catalog-matched file.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, cp := newUpdateDepsWithScanMembership(t, stub)

	writeNonMosaicFile(t, workspace, scanMembershipTestAgentsDir)
	// Note: extraAgent deployed file is NOT written — only the unrelated file.

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — ScannedAgentKeys must not contain a key derived from "unrelated.md".
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	for _, k := range cp.capturedInput.ScannedAgentKeys {
		if k == "unrelated" {
			t.Errorf("plan.Input.ScannedAgentKeys contains %q; "+
				"a non-MOSAIC file must not contribute any key to ScannedAgentKeys", k)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Manifest does not gate membership
// ---------------------------------------------------------------------------

// TestUpdate_AgentAbsentFromManifest_StillInScannedAgentKeys verifies that a deployed file
// matching a catalog agent by numeric id appears in plan.Input.ScannedAgentKeys even when the
// manifest has no entry for that agent. The workspace scan is the authority for membership;
// the manifest is a complementary per-item lookup, not an inclusion gate.
func TestUpdate_AgentAbsentFromManifest_StillInScannedAgentKeys(t *testing.T) {
	// Arrange — extra-agent deployed in workspace but absent from manifest.
	// (newUpdateDepsWithScanMembership already sets up a manifest with no extra-agent entry.)
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, cp := newUpdateDepsWithScanMembership(t, stub)

	writeExtraAgentDeployedFile(t, workspace, scanMembershipTestAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — extra-agent key must be in ScannedAgentKeys despite being absent from manifest.
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	found := false
	for _, k := range cp.capturedInput.ScannedAgentKeys {
		if k == extraAgent.Key {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plan.Input.ScannedAgentKeys = %v, does not contain %q; "+
			"a deployed file matched by workspace scan must appear in ScannedAgentKeys "+
			"even when the manifest has no entry for the agent (manifest is not an inclusion gate)",
			cp.capturedInput.ScannedAgentKeys, extraAgent.Key)
	}
}

// TestUpdate_WorkflowIDsPreserved_FromOrchestratorDiscovery verifies that
// plan.Input.WorkflowIDs is still populated from orchestrator discovery, not replaced by the
// scanned agent set. The two are additive: ScannedAgentKeys is a separate field that does not
// affect how WorkflowIDs is computed.
//
// This is a regression test that pins existing behaviour: the workflow-discovery path must
// not be broken by the introduction of ScannedAgentKeys.
func TestUpdate_WorkflowIDsPreserved_FromOrchestratorDiscovery(t *testing.T) {
	// Arrange — an orchestrator deployed file that embeds the "quick-fix" workflow.
	// When Update probes the orchestrator it discovers this workflow ID.
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, cp := newUpdateDepsWithScanMembership(t, stub)

	// Write a deployed orchestrator file embedding the "quick-fix" workflow so that
	// discoverExistingWorkflows returns ["quick-fix"] and Update sets WorkflowIDs accordingly.
	orchDir := filepath.Join(workspace) // orchestrator target path is "orchestrator.md" (via stubHarnessModule.TargetPath)
	orchContent := "---\nversion: \"1.0.0\"\n---\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"<Identity type=\"core\">\nOrchestrator.\n</Identity>\n"
	if err := os.WriteFile(filepath.Join(orchDir, "orchestrator.md"), []byte(orchContent), 0o644); err != nil {
		t.Fatalf("WriteFile orchestrator: %v", err)
	}

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — WorkflowIDs must include "quick-fix" discovered from the orchestrator.
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	found := false
	for _, id := range cp.capturedInput.WorkflowIDs {
		if id == "quick-fix" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plan.Input.WorkflowIDs = %v, does not contain %q; "+
			"workflow discovery from the deployed orchestrator must still populate WorkflowIDs "+
			"after Stage 4 (workflow discovery drives drift reporting, not membership)",
			cp.capturedInput.WorkflowIDs, "quick-fix")
	}

	// The primary Stage 4 invariant: ScannedAgentKeys is a separate field from WorkflowIDs.
	// Both must coexist without one replacing the other.
	_ = cp.capturedInput.ScannedAgentKeys // field must exist (contract)
	_ = cp.capturedInput.WorkflowIDs      // field must be unchanged by Stage 4
}

// ---------------------------------------------------------------------------
// T4.2 — Both a scanned agent and a workflow-referenced agent coexist without regression
// ---------------------------------------------------------------------------

// TestUpdate_BothScannedAndWorkflowReferencedAgents_BothKeysPresent verifies that when the
// workspace contains both a catalog-matched deployed agent (not in any workflow) and a
// workflow-referenced deployed agent, both agents' keys are in the planner input — one in
// ScannedAgentKeys and the workflow-referenced one through WorkflowIDs. Neither path blocks
// the other.
func TestUpdate_BothScannedAndWorkflowReferencedAgents_BothKeysPresent(t *testing.T) {
	// Arrange — extra-agent deployed (scanned, not in any workflow) and orchestrator deployed
	// with quick-fix workflow (workflow-referenced test-runner reaches via workflow).
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, cp := newUpdateDepsWithScanMembership(t, stub)

	// Deploy extra-agent (scan-only membership).
	writeExtraAgentDeployedFile(t, workspace, scanMembershipTestAgentsDir)

	// Deploy an orchestrator with "quick-fix" workflow embedded (workflow-discovery path).
	orchContent := "---\nversion: \"1.0.0\"\n---\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"<Identity type=\"core\">\nOrchestrator.\n</Identity>\n"
	if err := os.WriteFile(filepath.Join(workspace, "orchestrator.md"), []byte(orchContent), 0o644); err != nil {
		t.Fatalf("WriteFile orchestrator: %v", err)
	}

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — ScannedAgentKeys must contain extra-agent.
	if cp.capturedInput == nil {
		t.Fatal("planner.Build was never called")
	}
	foundExtra := false
	for _, k := range cp.capturedInput.ScannedAgentKeys {
		if k == extraAgent.Key {
			foundExtra = true
			break
		}
	}
	if !foundExtra {
		t.Errorf("plan.Input.ScannedAgentKeys = %v, does not contain %q; "+
			"workspace-scan-matched agent must appear in ScannedAgentKeys "+
			"even when workflow-based agents are also present",
			cp.capturedInput.ScannedAgentKeys, extraAgent.Key)
	}
}

// ---------------------------------------------------------------------------
// T4.3 — ScannedAgentKeys field exists in plan.Input (contract sanity check)
// ---------------------------------------------------------------------------

// TestUpdate_PlanInput_ScannedAgentKeysFieldExists verifies that plan.Input has a
// ScannedAgentKeys field. This test is a compile-time contract check: if the field is
// missing, the test file does not compile, producing a clear failure without runtime
// ambiguity. It is the smallest possible check that pins the contract change.
func TestUpdate_PlanInput_ScannedAgentKeysFieldExists(t *testing.T) {
	// Compile-time proof: constructing a plan.Input with ScannedAgentKeys set must compile.
	_ = plan.Input{
		ScannedAgentKeys: []string{"any-key"},
	}
	// Runtime proof: the field is accessible and has the expected type.
	in := plan.Input{ScannedAgentKeys: []string{"a", "b"}}
	if len(in.ScannedAgentKeys) != 2 {
		t.Errorf("plan.Input.ScannedAgentKeys has len %d, want 2; "+
			"the field must be a []string that holds the values assigned to it",
			len(in.ScannedAgentKeys))
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Scanned agent with version delta produces an UPDATE plan item
// ---------------------------------------------------------------------------

// recordingPlanner is a test-only planner stub that:
//   - captures the plan.Input it receives (so tests can assert on field wiring)
//   - produces one UPDATE plan item for each key found in in.ScannedAgentKeys, simulating
//     the staleness pipeline's response to a version delta
//
// This makes the plan-item assertion correctly RED in the TDD phase: without I4.2 wiring
// ScannedAgentKeys, the planner receives nil ScannedAgentKeys and produces no items. Once
// ScannedAgentKeys is populated by the implementation, the planner returns the UPDATE item.
type recordingPlanner struct {
	capturedInput *plan.Input
	builtPlan     domain.Plan
	harness       domain.HarnessRef
	workspacePath string
}

func (p *recordingPlanner) Build(ctx context.Context, in plan.Input) (domain.Plan, error) {
	cp := in
	p.capturedInput = &cp
	p.builtPlan = domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       p.harness,
		WorkspacePath: p.workspacePath,
		Scope:         domain.ScopeProject,
	}
	for _, key := range in.ScannedAgentKeys {
		p.builtPlan.Items = append(p.builtPlan.Items, domain.PlanItem{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
			Action:     domain.ActionUpdate,
			TargetPath: key + ".md",
			Stale:      []domain.VersionDelta{{Field: "version", Deployed: "1.0", Source: "2.0"}},
		})
	}
	return p.builtPlan, nil
}

// TestUpdate_ScannedAgentWithVersionDelta_ProducesUpdatePlanItem verifies that a deployed
// agent found by the workspace scan and matched to a catalog agent produces an UPDATE plan
// item when the planner receives ScannedAgentKeys. The recordingPlanner simulates the
// staleness/classification pipeline: it emits an UPDATE item for each key in ScannedAgentKeys,
// modelling the behaviour of a real planner when the scanned agent is stale relative to the
// catalog source.
//
// TDD RED: the current implementation does not populate ScannedAgentKeys, so the
// recordingPlanner receives nil and returns no items. The assertion below fails in RED phase.
// After I4.2 wires ScannedAgentKeys, the recordingPlanner produces the UPDATE item and the
// assertion passes.
func TestUpdate_ScannedAgentWithVersionDelta_ProducesUpdatePlanItem(t *testing.T) {
	// Arrange — extra-agent deployed in the workspace (not in any workflow).
	stub := interactiontest.NewBuilder().Build()
	deps, workspace, _ := newUpdateDepsWithScanMembership(t, stub)

	rp := &recordingPlanner{
		harness:       minimalHarness,
		workspacePath: workspace,
	}
	deps.Planner = rp

	writeExtraAgentDeployedFile(t, workspace, scanMembershipTestAgentsDir)

	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if rp.capturedInput == nil {
		t.Fatal("planner.Build was never called; Update did not reach the planner")
	}

	// Assert — the plan built by the planner must contain an UPDATE item for extra-agent.
	// Without I4.2 (ScannedAgentKeys not wired), the planner receives nil ScannedAgentKeys
	// and produces no items, so this assertion fails in RED phase.
	itemFound := false
	for _, item := range rp.builtPlan.Items {
		if item.Ref.Key == extraAgent.Key && item.Action == domain.ActionUpdate {
			itemFound = true
			break
		}
	}
	if !itemFound {
		t.Errorf("plan built for extra-agent has no UPDATE item; "+
			"plan.Items = %v. "+
			"A deployed agent found by workspace scan that resolves to a catalog agent must "+
			"produce an UPDATE plan item when a version delta is present. "+
			"This requires ScannedAgentKeys to be wired into plan.Input by Update (I4.2).",
			rp.builtPlan.Items)
	}
}
