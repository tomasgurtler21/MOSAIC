package plan_test

// resolve_orchestrator_script_test.go verifies artifact-resolution behaviour for
// orchestrator-script.md.
//
// Verified behaviours (T7.2):
//
// Inclusion alongside the main orchestrator (ExcludeOrchestrator = false):
//   - ResolveArtifactsFrom includes the script file in ArtifactSet.Agents when
//     ExcludeOrchestrator is false and the catalog reports the file present.
//   - Both the main orchestrator (key "orchestrator") and the script file
//     (key "orchestrator-script") appear in the resolved agent set.
//   - The script file is included in a pure workflow selection (no explicit inclusion needed).
//   - The script file is included in a zero-value Selection (default always-include path).
//
// Exclusion (ExcludeOrchestrator = true):
//   - When ExcludeOrchestrator is true, neither the main orchestrator nor the script file
//     appears in ArtifactSet.Agents.
//   - The ExcludeOrchestrator gate covers both files by the same decision, so they can
//     never disagree on inclusion.
//
// OrchestratorExcludedFor gate:
//   - OrchestratorExcludedFor(ModeDeployAgents) returns true — both files excluded.
//   - OrchestratorExcludedFor(ModeDeployHooks) returns true — both files excluded.
//   - OrchestratorExcludedFor(ModeDeployWorkspace) returns false — both files included.
//   - OrchestratorExcludedFor(ModeUpdateWorkspace) returns true — both files scanned-in-only.
//   - OrchestratorExcludedFor(ModeUpdateWorkflows) returns false — both files included.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Fixture helpers for orchestrator-script tests
// ---------------------------------------------------------------------------

// makeOrchestratorScriptAgent returns a minimal domain.Agent representing the script
// orchestrator, with the key and role the design mandates.
func makeOrchestratorScriptAgent() domain.Agent {
	return domain.Agent{
		Key:            "orchestrator-script",
		Version:        "1.0",
		Name:           "Orchestrator Script",
		Description:    "Script-mode orchestrator.",
		Role:           domain.RoleOrchestrator,
		Tools:          []string{"file_read", "file_edit", "file_search", "content_search"},
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/orchestrator-script.md",
	}
}

// fakeScriptCatalog returns a fakeCatalog that contains both the main orchestrator and
// the script orchestrator. Use this wherever tests need both files present.
func fakeScriptCatalog() *fakeCatalog {
	return &fakeCatalog{
		orchestrator:         makeOrchestrator(),
		orchestratorScript:   makeOrchestratorScriptAgent(),
		orchestratorScriptOK: true,
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — script file included when ExcludeOrchestrator = false
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_ScriptFile_IncludedWithMainOrchestrator verifies that when
// the catalog has an orchestrator-script file and ExcludeOrchestrator is false,
// ResolveArtifactsFrom includes both the main orchestrator and the script file.
func TestResolveArtifactsFrom_ScriptFile_IncludedWithMainOrchestrator(t *testing.T) {
	cat := fakeScriptCatalog()

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator-script") {
		t.Error("ArtifactSet.Agents does not contain \"orchestrator-script\"; " +
			"when ExcludeOrchestrator is false and the catalog has the script file, " +
			"ResolveArtifactsFrom must include it alongside the main orchestrator; " +
			"I7.3 (including the script file in ResolveArtifactsFrom) has not been implemented yet")
	}
}

// TestResolveArtifactsFrom_ScriptFile_MainOrchestratorAlsoPresentWithScript verifies that
// both orchestrator-role files are present in the same resolved artifact set. The main
// orchestrator's inclusion must not be displaced when the script file is also added.
func TestResolveArtifactsFrom_ScriptFile_MainOrchestratorAlsoPresentWithScript(t *testing.T) {
	cat := fakeScriptCatalog()

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents does not contain \"orchestrator\" when the script file is also present; " +
			"adding the script file must not displace the main orchestrator from the artifact set")
	}
}

// TestResolveArtifactsFrom_ScriptFile_PresentInWorkflowSelection verifies that the script
// file is included even when the Selection is workflow-driven (not an explicit orchestrator
// inclusion). Orchestrator inclusion is unconditional when ExcludeOrchestrator is false.
func TestResolveArtifactsFrom_ScriptFile_PresentInWorkflowSelection(t *testing.T) {
	worker := makeAgent("test-runner", "1.0")
	cat := &fakeCatalog{
		orchestrator:         makeOrchestrator(),
		orchestratorScript:   makeOrchestratorScriptAgent(),
		orchestratorScriptOK: true,
		workers:              []domain.Agent{worker},
		workflows:            []domain.Workflow{makeWorkflow("quick-fix", "test-runner")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		WorkflowIDs:         []string{"quick-fix"},
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with workflow selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator-script") {
		t.Error("ArtifactSet.Agents does not contain \"orchestrator-script\" in a workflow-driven selection; " +
			"the script file must join the artifact set whenever the main orchestrator does; " +
			"I7.3 has not been implemented yet")
	}
}

// TestResolveArtifactsFrom_ScriptFile_PresentInZeroValueSelection verifies that the script
// file is included in a zero-value Selection (all fields nil/false). The zero value is the
// default always-include path; both orchestrator files must be part of it.
func TestResolveArtifactsFrom_ScriptFile_PresentInZeroValueSelection(t *testing.T) {
	cat := fakeScriptCatalog()

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom zero-value selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator-script") {
		t.Error("ArtifactSet.Agents does not contain \"orchestrator-script\" for zero-value Selection; " +
			"the zero value of ExcludeOrchestrator (false) must include both orchestrator files; " +
			"I7.3 has not been implemented yet")
	}
}

// TestResolveArtifactsFrom_ScriptFile_AbsentWhenCatalogHasNoScriptFile verifies that when
// the catalog does not have an orchestrator-script file (OrchestratorScript() returns false),
// the script key does not appear in the artifact set.
func TestResolveArtifactsFrom_ScriptFile_AbsentWhenCatalogHasNoScriptFile(t *testing.T) {
	// Only the main orchestrator; no script file.
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom: %v", err)
	}

	if containsAgent(set.Agents, "orchestrator-script") {
		t.Error("ArtifactSet.Agents contains \"orchestrator-script\" even though the catalog has no script file; " +
			"when OrchestratorScript() returns false the key must not appear in the resolved set")
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — both files excluded when ExcludeOrchestrator = true
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_ExcludeOrchestrator_ScriptFileAlsoAbsent verifies that when
// ExcludeOrchestrator is true, the script file is excluded alongside the main orchestrator.
// The same flag governs both files so they can never disagree on inclusion.
func TestResolveArtifactsFrom_ExcludeOrchestrator_ScriptFileAlsoAbsent(t *testing.T) {
	cat := fakeScriptCatalog()

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: %v", err)
	}

	if containsAgent(set.Agents, "orchestrator-script") {
		t.Error("ArtifactSet.Agents contains \"orchestrator-script\" even though ExcludeOrchestrator=true; " +
			"the ExcludeOrchestrator gate must cover both orchestrator files — " +
			"the script file must be excluded by the same flag that excludes the main orchestrator; " +
			"I7.3 has not been implemented yet")
	}
}

// TestResolveArtifactsFrom_ExcludeOrchestrator_BothFilesAbsent verifies that neither
// orchestrator-role file appears in the resolved set when ExcludeOrchestrator is true.
func TestResolveArtifactsFrom_ExcludeOrchestrator_BothFilesAbsent(t *testing.T) {
	cat := fakeScriptCatalog()

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: %v", err)
	}

	for _, a := range set.Agents {
		if a.Role == domain.RoleOrchestrator {
			t.Errorf("ArtifactSet.Agents contains orchestrator-role agent %q with ExcludeOrchestrator=true; "+
				"no orchestrator-role file may appear when ExcludeOrchestrator is set", a.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// OrchestratorExcludedFor — gate covers both files
// ---------------------------------------------------------------------------

// TestOrchestratorExcludedFor_AgentDeployMode_BothFilesExcluded verifies that
// OrchestratorExcludedFor returns true for ModeDeployAgents, which is the mode that must
// exclude both orchestrator-role files from its artifact set.
func TestOrchestratorExcludedFor_AgentDeployMode_BothFilesExcluded(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeDeployAgents) {
		t.Error("OrchestratorExcludedFor(ModeDeployAgents) = false, want true; " +
			"deploy-agents must exclude both orchestrator-role files from the artifact set")
	}
}

// TestOrchestratorExcludedFor_HooksMode_BothFilesExcluded verifies that
// OrchestratorExcludedFor returns true for ModeDeployHooks.
func TestOrchestratorExcludedFor_HooksMode_BothFilesExcluded(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeDeployHooks) {
		t.Error("OrchestratorExcludedFor(ModeDeployHooks) = false, want true; " +
			"deploy-hooks must exclude both orchestrator-role files from the artifact set")
	}
}

// TestOrchestratorExcludedFor_DeployWorkspace_BothFilesIncluded verifies that
// OrchestratorExcludedFor returns false for ModeDeployWorkspace, which means both
// orchestrator-role files are included in the resolved artifact set.
func TestOrchestratorExcludedFor_DeployWorkspace_BothFilesIncluded(t *testing.T) {
	if plan.OrchestratorExcludedFor(domain.ModeDeployWorkspace) {
		t.Error("OrchestratorExcludedFor(ModeDeployWorkspace) = true, want false; " +
			"deploy-workspace must include both orchestrator-role files in the artifact set")
	}
}

// TestOrchestratorExcludedFor_UpdateWorkspace_BothFilesScannedInOnly verifies that
// OrchestratorExcludedFor returns true for ModeUpdateWorkspace. When excluded from forced
// inclusion, both orchestrator-role files enter the artifact set only when the workspace scan
// discovers them on disk and supplies their keys via ScannedAgentKeys — the same path every
// other deployed agent uses.
func TestOrchestratorExcludedFor_UpdateWorkspace_BothFilesScannedInOnly(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeUpdateWorkspace) {
		t.Error("OrchestratorExcludedFor(ModeUpdateWorkspace) = false, want true; " +
			"update-workspace must exclude both orchestrator-role files from forced inclusion; " +
			"both files enter the artifact set only when the workspace scan discovers them via ScannedAgentKeys")
	}
}

// TestOrchestratorExcludedFor_UpdateWorkflows_BothFilesIncluded verifies that
// OrchestratorExcludedFor returns false for ModeUpdateWorkflows.
func TestOrchestratorExcludedFor_UpdateWorkflows_BothFilesIncluded(t *testing.T) {
	if plan.OrchestratorExcludedFor(domain.ModeUpdateWorkflows) {
		t.Error("OrchestratorExcludedFor(ModeUpdateWorkflows) = true, want false; " +
			"update-workflows must include both orchestrator-role files in the artifact set")
	}
}
