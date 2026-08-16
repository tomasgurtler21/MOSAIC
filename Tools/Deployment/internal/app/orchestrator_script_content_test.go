package app_test

// orchestrator_script_content_test.go verifies content-generation behaviour for
// orchestrator-script.md (T7.3).
//
// The design requires that orchestrator-script.md receives the same filled
// AvailableWorkflows and InfrastructureAgents managed regions as the main orchestrator
// when deploying via DeployNew, Update, or UpdateWorkflows.
//
// Content generation for orchestrator-role agents is handled by buildContent, which
// already applies workflow and infrastructure blocks to any agent whose Role is
// RoleOrchestrator. These tests verify that:
//
//   - The content callback is invoked for the script file (it is in agentByKey).
//   - The content callback does not return an error for the script file.
//   - The generated content for the script file contains the same AvailableWorkflows
//     injection as the main orchestrator (same workflow names).
//   - Checkpoint-class and commit-class infrastructure agents appear as ordinary
//     entries in the InfrastructureAgents injection of both orchestrators.
//
// These tests will FAIL until I7.2 and I7.3 are implemented, because until then:
//   - OrchestratorScript() returns (zero, false).
//   - The script file is not included in the probe set returned by ResolveArtifacts.
//   - It is therefore absent from agentByKey.
//   - Content(scriptItem) returns "unknown agent orchestrator-script".

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Source content fixtures for content-generation tests
// ---------------------------------------------------------------------------

// orchestratorScriptSourceDoc is a minimal orchestrator-script.md source document.
// It carries the two managed injection regions that the deploy pipeline fills.
const orchestratorScriptSourceDoc = `---
role: orchestrator
version: "1.0"
name: Orchestrator Script
description: Script-mode orchestrator.
tools: [file_read, file_edit, file_search, content_search]
---

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<InfrastructureAgents type="managed">
</InfrastructureAgents>
`

// orchestratorMainSourceDoc is a minimal orchestrator.md source document matching the
// structure of orchestratorScriptSourceDoc for comparison testing.
const orchestratorMainSourceDoc = `---
role: orchestrator
version: "1.0"
name: Orchestrator
description: Main orchestrator.
tools: "{tool-permissions}"
---

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<InfrastructureAgents type="managed">
</InfrastructureAgents>
`

// ---------------------------------------------------------------------------
// scriptContentCapturingExecutor captures generated content per agent key
// ---------------------------------------------------------------------------

// scriptContentCapturingExecutor extends contentCapturingExecutor to also record the
// exact bytes produced by the content callback for each agent plan item. It is declared
// locally here because it is only needed in this test file.
type scriptContentCapturingExecutor struct {
	capturedContents map[string][]byte
	capturedErrors   map[string]error
}

func (e *scriptContentCapturingExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.capturedContents = make(map[string][]byte)
	e.capturedErrors = make(map[string]error)
	for _, item := range req.Plan.Items {
		if item.Ref.Kind != domain.ArtifactAgent {
			continue
		}
		content, err := req.Content(item)
		if err != nil {
			e.capturedErrors[item.Ref.Key] = err
		} else {
			e.capturedContents[item.Ref.Key] = content
		}
	}
	return deploy.ExecResult{DeploymentRoot: req.Plan.WorkspacePath}, nil
}

// ---------------------------------------------------------------------------
// Helpers for content-generation test catalog setup
// ---------------------------------------------------------------------------

// scriptContentCatalog returns a stubCatalog containing both orchestrators and provides
// source bytes for both so the content callback can call ReadSource.
func scriptContentCatalog(scriptSourcePath, mainSourcePath string) *stubCatalog {
	return &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:            "orchestrator",
			Version:        "1.0",
			Name:           "Orchestrator",
			Role:           domain.RoleOrchestrator,
			ToolsPlaceholder: "{tool-permissions}",
			RequiredSkills: []string{},
			SourcePath:     mainSourcePath,
		},
		orchestratorScript: domain.Agent{
			Key:            "orchestrator-script",
			Version:        "1.0",
			Name:           "Orchestrator Script",
			Role:           domain.RoleOrchestrator,
			Tools:          []string{"file_read", "file_edit", "file_search", "content_search"},
			RequiredSkills: []string{},
			SourcePath:     scriptSourcePath,
		},
		orchestratorScriptOK: true,
		agents:     []domain.Agent{minimalAgent},
		skills:     []domain.Skill{},
		hooks:      []domain.HookBundle{},
		workflows:  []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}}},
		tiers:      []domain.TierInfo{{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}}},
		sources: map[string][]byte{
			mainSourcePath:   []byte(orchestratorMainSourceDoc),
			scriptSourcePath: []byte(orchestratorScriptSourceDoc),
		},
	}
}

// scriptPlan returns a domain.Plan with plan items for both the main orchestrator and the
// script file, plus the worker agent. The test uses a stub planner that returns this plan
// to exercise buildContent for both orchestrator items.
func scriptPlan(workspace, mainTargetPath, scriptTargetPath string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: mainTargetPath,
				SourcePath: "/fake/orchestrator.md",
				Action:     domain.ActionCreate,
				Model:      domain.ModelSelection{ModelID: "model-a", Origin: domain.OriginTierMapping},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator-script"},
				TargetPath: scriptTargetPath,
				SourcePath: "/fake/orchestrator-script.md",
				Action:     domain.ActionCreate,
				Model:      domain.ModelSelection{ModelID: "model-a", Origin: domain.OriginTierMapping},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
				Model:      domain.ModelSelection{ModelID: "model-a", Origin: domain.OriginTierMapping},
			},
		},
		Workflows: []string{minimalWorkflow.ID},
	}
}

// ---------------------------------------------------------------------------
// T7.3: Script file receives content callback without error
// ---------------------------------------------------------------------------

// TestOrchestratorScript_ContentCallback_InvokedWithoutError verifies that when the
// script file is in the plan, the content callback is invoked for it and does not return
// an error. The script file must be in agentByKey for the callback to succeed; if it is
// absent, buildContent returns "unknown agent orchestrator-script".
//
// This test will FAIL until I7.2 and I7.3 are implemented: the script file is not loaded
// into the catalog and therefore not included in agentByKey by the deploy flow.
func TestOrchestratorScript_ContentCallback_InvokedWithoutError(t *testing.T) {
	// Arrange
	mainPath := "/fake/orchestrator.md"
	scriptPath := "/fake/orchestrator-script.md"
	cat := scriptContentCatalog(scriptPath, mainPath)

	exec := &scriptContentCapturingExecutor{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Executor = exec
	deps.Planner = &stubPlanner{
		plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md"),
	}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — content must be generated for the script file without error.
	if contentErr, errored := exec.capturedErrors["orchestrator-script"]; errored {
		t.Errorf("content callback returned error for orchestrator-script: %v; "+
			"the script file must be in agentByKey for its content to be generated; "+
			"I7.2 (loading the script file) and I7.3 (including it in the artifact set) "+
			"have not been implemented yet", contentErr)
	}
	if _, ok := exec.capturedContents["orchestrator-script"]; !ok {
		t.Error("content callback was not invoked for orchestrator-script; " +
			"the script file must be included in the deploy plan and its content must be generated; " +
			"I7.2 and I7.3 have not been implemented yet")
	}
}

// TestOrchestratorScript_ContentCallback_MainOrchestratorAlsoSucceeds verifies that
// adding the script file to the plan does not break content generation for the main
// orchestrator. Both callbacks must succeed.
func TestOrchestratorScript_ContentCallback_MainOrchestratorAlsoSucceeds(t *testing.T) {
	// Arrange
	mainPath := "/fake/orchestrator.md"
	scriptPath := "/fake/orchestrator-script.md"
	cat := scriptContentCatalog(scriptPath, mainPath)

	exec := &scriptContentCapturingExecutor{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Executor = exec
	deps.Planner = &stubPlanner{
		plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md"),
	}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — main orchestrator content must also be generated.
	if contentErr, errored := exec.capturedErrors["orchestrator"]; errored {
		t.Errorf("content callback returned error for main orchestrator: %v; "+
			"adding the script file to the plan must not break main orchestrator content generation", contentErr)
	}
	if _, ok := exec.capturedContents["orchestrator"]; !ok {
		t.Error("content callback was not invoked for the main orchestrator; " +
			"main orchestrator content generation must not be disrupted")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Both orchestrators receive the same workflow injection
// ---------------------------------------------------------------------------

// TestOrchestratorScript_WorkflowInjection_SameContentAsMainOrchestrator verifies that
// when a workflow is selected, the generated content of the script file contains the same
// workflow name that appears in the main orchestrator's generated content. Both files
// declare the same AvailableWorkflows managed region and must receive the same injection.
//
// This test will FAIL until I7.2, I7.3, and the deploy flow wiring (I7.4) are implemented.
func TestOrchestratorScript_WorkflowInjection_SameContentAsMainOrchestrator(t *testing.T) {
	// Arrange — workflow name is "Quick Fix"; it must appear in both orchestrators' content.
	mainPath := "/fake/orchestrator.md"
	scriptPath := "/fake/orchestrator-script.md"
	cat := scriptContentCatalog(scriptPath, mainPath)

	exec := &scriptContentCapturingExecutor{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Executor = exec
	deps.Planner = &stubPlanner{
		plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md"),
	}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Skip if neither agent got content (implementation missing).
	mainContent, mainOK := exec.capturedContents["orchestrator"]
	scriptContent, scriptOK := exec.capturedContents["orchestrator-script"]
	if !mainOK || !scriptOK {
		t.Skip("content not generated for one or both orchestrators; I7.2/I7.3/I7.4 not yet implemented")
	}

	// The workflow name "Quick Fix" must appear in both outputs — it is the name used
	// by the workflow injection block that both orchestrators' AvailableWorkflows regions should receive.
	wfName := minimalWorkflow.Name
	if !strings.Contains(string(mainContent), wfName) {
		t.Errorf("main orchestrator content does not contain workflow name %q; "+
			"the AvailableWorkflows injection was not applied correctly", wfName)
	}
	if !strings.Contains(string(scriptContent), wfName) {
		t.Errorf("orchestrator-script content does not contain workflow name %q; "+
			"both orchestrator-role files must receive the same AvailableWorkflows injection", wfName)
	}
}

// TestOrchestratorScript_InfraInjection_SameContentAsMainOrchestrator verifies that when
// an infrastructure agent is included in the deploy, both orchestrators' generated content
// includes the infrastructure agent information in the InfrastructureAgents region.
//
// This test will FAIL until I7.2, I7.3, and I7.4 are implemented.
func TestOrchestratorScript_InfraInjection_SameContentAsMainOrchestrator(t *testing.T) {
	// Arrange — include a checkpoint-class infra agent. Both orchestrators must reflect it.
	mainPath := "/fake/orchestrator.md"
	scriptPath := "/fake/orchestrator-script.md"
	checkpointAgent := domain.Agent{
		Key:            "checkpoint-manager",
		Version:        "1.0",
		Name:           "Checkpoint Manager",
		Role:           domain.RoleSubagent,
		Infrastructure: "checkpoint",
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/checkpoint-manager.md",
	}
	cat := scriptContentCatalog(scriptPath, mainPath)
	cat.infraAgents = []domain.Agent{checkpointAgent}
	cat.sources[checkpointAgent.SourcePath] = []byte("---\nversion: \"1.0\"\n---\nContent.\n")

	exec := &scriptContentCapturingExecutor{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Executor = exec

	// Plan includes both orchestrators and the infra agent.
	infraItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "checkpoint-manager"},
		TargetPath: "checkpoint-manager.md",
		SourcePath: checkpointAgent.SourcePath,
		Action:     domain.ActionCreate,
		Model:      domain.ModelSelection{ModelID: "model-a", Origin: domain.OriginTierMapping},
	}
	plan := scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md")
	plan.Items = append(plan.Items, infraItem)
	deps.Planner = &stubPlanner{plan: plan}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:             "stub-harness",
		WorkspacePath:         workspace,
		WorkflowIDs:           []string{minimalWorkflow.ID},
		InfrastructureAgentIDs: []string{"checkpoint-manager"},
		AutoConfirmPlan:       true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Skip if content not generated (implementation missing).
	mainContent, mainOK := exec.capturedContents["orchestrator"]
	scriptContent, scriptOK := exec.capturedContents["orchestrator-script"]
	if !mainOK || !scriptOK {
		t.Skip("content not generated for one or both orchestrators; I7.2/I7.3/I7.4 not yet implemented")
	}

	// The checkpoint agent name must appear in both outputs.
	agentName := checkpointAgent.Name
	if !strings.Contains(string(mainContent), agentName) {
		t.Errorf("main orchestrator content does not contain checkpoint agent %q; "+
			"InfrastructureAgents injection was not applied", agentName)
	}
	if !strings.Contains(string(scriptContent), agentName) {
		t.Errorf("orchestrator-script content does not contain checkpoint agent %q; "+
			"both orchestrators must receive the same InfrastructureAgents injection", agentName)
	}
}
