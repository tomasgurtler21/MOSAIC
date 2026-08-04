package transform_test

// region_filling_test.go covers tool-managed region filling (T3.1):
//
//   - Harness content lands in [[DEPLOYED:HarnessConstraints]] on both create and update.
//   - Workflow content lands in [[DEPLOYED:AvailableWorkflows]] on both create and update.
//   - Infrastructure content lands in [[DEPLOYED:InfrastructureAgents]] on create and update.
//   - Tool-managed region content is regenerated from the module/request on every transform,
//     never lifted from the deployed file.
//   - RegionOutcome carries Marker == NodeDeployed and ToolManaged() == true for every
//     tool-managed region.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// sourceWithHarnessDeployedRegion is a source document whose Constraints section declares
// HarnessConstraints as a [[DEPLOYED:]] region, reflecting the new marker vocabulary.
const sourceWithHarnessDeployedRegion = `---
id: 30
version: 1.0.0
name: harness-deployed-test
description: Agent for testing harness content in a DEPLOYED region
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: harness deployed region testing
required_skills: []
---

[[SECTION:Identity]]
# HarnessDeployedTest Agent

You are the HarnessDeployedTest agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always use the approved tool list.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// deployedWithOldHarnessContent represents a deployed file that still has content inside the
// [[DEPLOYED:HarnessConstraints]] region — content that must NOT be lifted on an update
// (tool-managed regions are always regenerated from the module, never from the deployed file).
const deployedWithOldHarnessContent = `---
id: 30
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for testing harness content in a DEPLOYED region
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# HarnessDeployedTest Agent

You are the HarnessDeployedTest agent.

[[INJECTION:IdentityExtension]]
User identity extension: pre-existing content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always use the approved tool list.

[[DEPLOYED:HarnessConstraints]]
STALE HARNESS CONTENT FROM A PREVIOUS DEPLOYMENT — MUST NOT BE LIFTED.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// sourceWithWorkflowDeployedRegion is an orchestrator-like source that declares
// AvailableWorkflows as a [[DEPLOYED:]] region inside the Identity section.
const sourceWithWorkflowDeployedRegion = `---
version: 6.0.0
name: orchestrator-deployed-test
description: Orchestrator for testing workflow content in a DEPLOYED region
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: workflow deployed region testing
required_skills: []
---

[[SECTION:Identity]]
# OrchestratorDeployedTest Agent

You are the OrchestratorDeployedTest agent.

[[DEPLOYED:AvailableWorkflows]]
[[/DEPLOYED:AvailableWorkflows]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// sourceWithInfraDeployedRegion is an orchestrator-like source that declares
// InfrastructureAgents as a [[DEPLOYED:]] region inside the Identity section.
const sourceWithInfraDeployedRegion = `---
version: 6.0.0
name: orchestrator-infra-test
description: Orchestrator for testing infra content in a DEPLOYED region
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: infra deployed region testing
required_skills: []
---

[[SECTION:Identity]]
# OrchestratorInfraTest Agent

You are the OrchestratorInfraTest agent.

[[DEPLOYED:InfrastructureAgents]]
[[/DEPLOYED:InfrastructureAgents]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// ---------------------------------------------------------------------------
// T3.1: Harness content fills [[DEPLOYED:HarnessConstraints]]
// ---------------------------------------------------------------------------

// TestToolManagedRegion_HarnessContent_FilledOnCreate asserts that on a new deployment,
// the [[DEPLOYED:HarnessConstraints]] region is filled with the harness module's declared
// content and the corresponding RegionOutcome in Report.Regions has:
//   - Marker == NodeDeployed
//   - Class == InjectionHarness
//   - Action == RegionFilled
//   - ToolManaged() == true
func TestToolManagedRegion_HarnessContent_FilledOnCreate(t *testing.T) {
	mod := newInjectionFixtureModule(t)
	harnessContent, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok || harnessContent == "" {
		t.Fatalf("injection_fixture.yaml must declare HarnessConstraints content but does not")
	}

	req := transform.Request{
		Source:   []byte(sourceWithHarnessDeployedRegion),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "harness-deployed-test",
		Module:   mod,
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Locate the HarnessConstraints outcome in Report.Regions (not Report.Injections).
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "HarnessConstraints" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for HarnessConstraints in Report.Regions; got: %v", result.Report.Regions)
	}

	// The outcome must signal a tool-managed region that was filled from the harness.
	if outcome.Marker != docformat.NodeDeployed {
		t.Errorf("HarnessConstraints Marker: want %q, got %q", docformat.NodeDeployed, outcome.Marker)
	}
	if !outcome.ToolManaged() {
		t.Error("HarnessConstraints ToolManaged() must be true for a [[DEPLOYED:]] region")
	}
	if outcome.Class != domain.InjectionHarness {
		t.Errorf("HarnessConstraints Class: want %q, got %q", domain.InjectionHarness, outcome.Class)
	}
	if outcome.Action != transform.RegionFilled {
		t.Errorf("HarnessConstraints Action: want %q, got %q", transform.RegionFilled, outcome.Action)
	}

	// The harness content must appear in the output document's deployed region.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, nodeOK := outDoc.Body().Deployed("HarnessConstraints")
	if !nodeOK {
		t.Fatal("HarnessConstraints deployed region absent from output")
	}
	if !bytes.Contains(node.Content(), []byte(harnessContent)) {
		t.Errorf("HarnessConstraints deployed region content does not contain harness descriptor content\nwant (in): %q\ngot: %q",
			harnessContent, node.Content())
	}
}

// TestToolManagedRegion_HarnessContent_RegeneratedOnUpdate asserts that on an update, the
// [[DEPLOYED:HarnessConstraints]] region is filled from the harness module — NOT lifted from
// the deployed file. Stale harness content in the deployed file must not survive.
func TestToolManagedRegion_HarnessContent_RegeneratedOnUpdate(t *testing.T) {
	mod := newInjectionFixtureModule(t)
	harnessContent, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok || harnessContent == "" {
		t.Fatalf("injection_fixture.yaml must declare HarnessConstraints content but does not")
	}

	req := transform.Request{
		Source:   []byte(sourceWithHarnessDeployedRegion),
		Deployed: []byte(deployedWithOldHarnessContent), // has stale content in [[DEPLOYED:HarnessConstraints]]
		Kind:     domain.ArtifactAgent,
		Key:      "harness-deployed-test",
		Module:   mod,
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify the outcome is RegionFilled (not RegionPreserved).
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "HarnessConstraints" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for HarnessConstraints absent from Report.Regions")
	}
	if outcome.Action != transform.RegionFilled {
		t.Errorf("HarnessConstraints on update: Action want %q, got %q", transform.RegionFilled, outcome.Action)
	}

	// The stale deployed content must not appear in the output.
	if bytes.Contains(result.Output, []byte("STALE HARNESS CONTENT FROM A PREVIOUS DEPLOYMENT")) {
		t.Error("output contains stale harness content from the deployed file; tool-managed regions must be regenerated, not lifted")
	}

	// The current module content must appear.
	if !bytes.Contains(result.Output, []byte(harnessContent)) {
		t.Errorf("output does not contain current harness descriptor content %q", harnessContent)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Workflow content fills [[DEPLOYED:AvailableWorkflows]]
// ---------------------------------------------------------------------------

// TestToolManagedRegion_Workflows_AssembledInDeployedRegion asserts that when
// Request.Workflows is non-empty, the [[DEPLOYED:AvailableWorkflows]] region of the output
// contains the assembled workflow blocks and the RegionOutcome has Action == RegionAssembled.
func TestToolManagedRegion_Workflows_AssembledInDeployedRegion(t *testing.T) {
	const workflowBlock = "[[SECTION:Workflow:lean-tdd]]\n<!-- workflow-version: 2.0 -->\n## Lean TDD Workflow\n\nPhase table here.\n[[/SECTION:Workflow:lean-tdd]]\n"

	req := transform.Request{
		Source: []byte(sourceWithWorkflowDeployedRegion),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator-deployed-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "lean-tdd", Block: []byte(workflowBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Locate AvailableWorkflows outcome in Report.Regions.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "AvailableWorkflows" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for AvailableWorkflows absent from Report.Regions; got: %v", result.Report.Regions)
	}

	if outcome.Marker != docformat.NodeDeployed {
		t.Errorf("AvailableWorkflows Marker: want %q, got %q", docformat.NodeDeployed, outcome.Marker)
	}
	if !outcome.ToolManaged() {
		t.Error("AvailableWorkflows ToolManaged() must be true for a [[DEPLOYED:]] region")
	}
	if outcome.Action != transform.RegionAssembled {
		t.Errorf("AvailableWorkflows Action: want %q, got %q", transform.RegionAssembled, outcome.Action)
	}

	// The assembled workflow must appear inside the deployed region.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, nodeOK := outDoc.Body().Deployed("AvailableWorkflows")
	if !nodeOK {
		t.Fatal("AvailableWorkflows deployed region absent from output")
	}
	if !bytes.Contains(node.Content(), []byte("[[SECTION:Workflow:lean-tdd]]")) {
		t.Errorf("AvailableWorkflows region does not contain workflow block opening tag;\ncontent: %q", node.Content())
	}

	// Report.Workflows must list the assembled workflow ID.
	found := false
	for _, id := range result.Report.Workflows {
		if id == "lean-tdd" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("workflow ID %q expected in Report.Workflows; got: %v", "lean-tdd", result.Report.Workflows)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Infrastructure content fills [[DEPLOYED:InfrastructureAgents]]
// ---------------------------------------------------------------------------

// TestToolManagedRegion_Infrastructure_AssembledInDeployedRegion asserts that when
// Request.InfrastructureAgents is non-empty, the [[DEPLOYED:InfrastructureAgents]] region
// is assembled and the RegionOutcome has Action == RegionAssembledInfra.
func TestToolManagedRegion_Infrastructure_AssembledInDeployedRegion(t *testing.T) {
	infraBlock := transform.InfrastructureBlock{
		Key:         "checkpoint-agent",
		Version:     "1.0.0",
		Class:       "checkpoint",
		Description: "Saves progress after each stage.",
		OnFailure:   "halt",
		Triggers: []domain.InfrastructureTrigger{
			{Trigger: "STAGE_END", TriggerParam: ""},
		},
	}

	req := transform.Request{
		Source:               []byte(sourceWithInfraDeployedRegion),
		Kind:                 domain.ArtifactAgent,
		Key:                  "orchestrator-infra-test",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		InfrastructureAgents: []transform.InfrastructureBlock{infraBlock},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Locate InfrastructureAgents outcome in Report.Regions.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "InfrastructureAgents" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for InfrastructureAgents absent from Report.Regions; got: %v", result.Report.Regions)
	}

	if outcome.Marker != docformat.NodeDeployed {
		t.Errorf("InfrastructureAgents Marker: want %q, got %q", docformat.NodeDeployed, outcome.Marker)
	}
	if !outcome.ToolManaged() {
		t.Error("InfrastructureAgents ToolManaged() must be true for a [[DEPLOYED:]] region")
	}
	if outcome.Action != transform.RegionAssembledInfra {
		t.Errorf("InfrastructureAgents Action: want %q, got %q", transform.RegionAssembledInfra, outcome.Action)
	}

	// The infrastructure agent key must appear in the assembled content.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, nodeOK := outDoc.Body().Deployed("InfrastructureAgents")
	if !nodeOK {
		t.Fatal("InfrastructureAgents deployed region absent from output")
	}
	if !bytes.Contains(node.Content(), []byte("checkpoint-agent")) {
		t.Errorf("InfrastructureAgents region does not contain agent key;\ncontent: %q", node.Content())
	}

	// Report.InfrastructureAgents must list the assembled agent key.
	found := false
	for _, key := range result.Report.InfrastructureAgents {
		if key == "checkpoint-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent key %q expected in Report.InfrastructureAgents; got: %v",
			"checkpoint-agent", result.Report.InfrastructureAgents)
	}
}

// ---------------------------------------------------------------------------
// T3.1: Tool-managed region outcomes are marked NodeDeployed across all classes
// ---------------------------------------------------------------------------

// TestToolManagedRegion_AllOutcomesHaveDeployedMarker asserts that every RegionOutcome in
// Report.Regions that corresponds to a [[DEPLOYED:]] region has Marker == NodeDeployed and
// ToolManaged() == true, regardless of the specific class (harness, workflow, infra).
func TestToolManagedRegion_AllOutcomesHaveDeployedMarker(t *testing.T) {
	// sourceWithAllToolManagedRegions has all three tool-managed region classes in one
	// document (harness, workflow, infra) declared with [[DEPLOYED:]] markers.
	const sourceWithAllToolManagedRegions = `---
version: 6.0.0
name: orchestrator-all-regions-test
description: Orchestrator with all tool-managed region classes
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: all regions testing
required_skills: []
---

[[SECTION:Identity]]
# AllRegions Agent

You are the AllRegions agent.

[[DEPLOYED:InfrastructureAgents]]
[[/DEPLOYED:InfrastructureAgents]]

[[DEPLOYED:AvailableWorkflows]]
[[/DEPLOYED:AvailableWorkflows]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

	req := transform.Request{
		Source: []byte(sourceWithAllToolManagedRegions),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator-all-regions-test",
		Module: newInjectionFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "sample-wf", Block: []byte("[[SECTION:Workflow:sample-wf]]\ncontent\n[[/SECTION:Workflow:sample-wf]]\n")},
		},
		InfrastructureAgents: []transform.InfrastructureBlock{
			{
				Key: "sample-infra", Version: "1.0.0", Class: "checkpoint",
				Description: "desc", OnFailure: "halt",
				Triggers: []domain.InfrastructureTrigger{{Trigger: "STAGE_END"}},
			},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	toolManagedNames := map[string]bool{
		"HarnessConstraints":   true,
		"AvailableWorkflows":   true,
		"InfrastructureAgents": true,
	}

	for _, outcome := range result.Report.Regions {
		if !toolManagedNames[outcome.Name] {
			continue
		}
		if outcome.Marker != docformat.NodeDeployed {
			t.Errorf("tool-managed region %q: Marker want %q, got %q",
				outcome.Name, docformat.NodeDeployed, outcome.Marker)
		}
		if !outcome.ToolManaged() {
			t.Errorf("tool-managed region %q: ToolManaged() must be true", outcome.Name)
		}
	}

	// All three tool-managed names must appear in the outcomes.
	seen := make(map[string]bool, 3)
	for _, outcome := range result.Report.Regions {
		if toolManagedNames[outcome.Name] {
			seen[outcome.Name] = true
		}
	}
	for name := range toolManagedNames {
		if !seen[name] {
			t.Errorf("tool-managed region %q absent from Report.Regions", name)
		}
	}
}
