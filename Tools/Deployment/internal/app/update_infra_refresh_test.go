package app_test

// update_infra_refresh_test.go verifies that Service.Update refreshes the infrastructure
// agent declarations an orchestrator-role file already carries when the catalog has moved on,
// without asking anything, without adding declarations, and without touching hand-added ones.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
)

func refreshFiles(infraInner string) map[string]string {
	return map[string]string{injOrchestratorPath: refreshOrchestrator("1.0", infraInner, refreshCurrentWorkflow)}
}

func TestUpdate_DeclaredAgentAtOlderVersion_RefreshedToCatalogVersion(t *testing.T) {
	stale := injSection(injSecurityBlock("1.0"))

	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(stale)})

	requireUpdateOK(t, run)
	region := refreshInfraRegion(t, run, injOrchestratorPath)
	if want := injSection(injSecurityBlock("1.1")); region != want {
		t.Errorf("region after update =\n%s\nwant\n%s", region, want)
	}
	item, ok := run.planItem(injOrchestratorPath)
	if !ok || item.Action != domain.ActionUpdate {
		t.Fatalf("orchestrator plan item = %+v (found %v), want ActionUpdate", item, ok)
	}
	if !strings.Contains(item.Reason, injSecurityKey) {
		t.Errorf("plan reason does not name the stale key: %q", item.Reason)
	}
}

func TestUpdate_DeclaredAgentWithDifferentClassTriggersOnFailure_RefreshedToCatalog(t *testing.T) {
	drifted := injSecurityBlock("1.1") // same version as the catalog, different content
	drifted.Class = "review"
	drifted.OnFailure = "halt"
	drifted.Triggers = []domain.InfrastructureTrigger{{Trigger: "MANUAL"}}

	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(injSection(drifted))})

	requireUpdateOK(t, run)
	if got, want := refreshInfraRegion(t, run, injOrchestratorPath), injSection(injSecurityBlock("1.1")); got != want {
		t.Errorf("region after update =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdate_RefreshedKey_DeclaredExactlyOnce(t *testing.T) {
	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(injSection(injSecurityBlock("1.0")))})

	requireUpdateOK(t, run)
	if n := declCount(refreshInfraRegion(t, run, injOrchestratorPath), injSecurityKey); n != 1 {
		t.Errorf("%s is declared %d times after refresh, want 1", injSecurityKey, n)
	}
}

func TestUpdate_HandAddedDeclaration_StaysByteIdenticalWhileCatalogOneRefreshes(t *testing.T) {
	hand := injSection(injHandAddedBlock())
	infra := hand + injSection(injSecurityBlock("1.0"))

	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(infra)})

	requireUpdateOK(t, run)
	want := hand + injSection(injSecurityBlock("1.1"))
	if got := refreshInfraRegion(t, run, injOrchestratorPath); got != want {
		t.Errorf("region after update =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdate_UndeclaredInfraAgentWithDeployedFile_IsNeverDeclared(t *testing.T) {
	files := refreshFiles(injSection(injSecurityBlock("1.0")))
	files[filepath.Join(refreshAgentsDir, injCheckpointKey+".md")] =
		"---\nid: \"" + refreshCheckpointNumID + "\"\nversion: \"0.5\"\n---\nDeployed checkpoint writer.\n"

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	region := refreshInfraRegion(t, run, injOrchestratorPath)
	if n := declCount(region, injCheckpointKey); n != 0 {
		t.Errorf("%s was added to the orchestrator declarations", injCheckpointKey)
	}
	if n := declCount(region, injSecurityKey); n != 1 {
		t.Errorf("declared %s appears %d times, want 1", injSecurityKey, n)
	}
}

func TestUpdate_OrchestratorWithNoDeclarations_GainsNone(t *testing.T) {
	files := refreshFiles("")
	files[filepath.Join(refreshAgentsDir, injSecurityKey+".md")] =
		"---\nid: \"88\"\nversion: \"1.0\"\n---\nDeployed.\n"

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	if got, _ := run.readFile(t, injOrchestratorPath); got != files[injOrchestratorPath] {
		t.Error("an orchestrator with no declarations was rewritten by update")
	}
}

func TestUpdate_CurrentDeclarationsAndNoOtherStaleness_OrchestratorUntouched(t *testing.T) {
	files := refreshFiles(injSection(injSecurityBlock("1.1")) + injSection(injCheckpointBlock("1.0")))

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	item, ok := run.planItem(injOrchestratorPath)
	if !ok || item.Action != domain.ActionUnchanged {
		t.Fatalf("orchestrator plan item = %+v (found %v), want ActionUnchanged", item, ok)
	}
	if got, _ := run.readFile(t, injOrchestratorPath); got != files[injOrchestratorPath] {
		t.Error("an orchestrator with current declarations was rewritten")
	}
}

func TestUpdate_OrchestratorRewrittenForOtherReason_CurrentDeclarationsStayByteIdentical(t *testing.T) {
	// Same version, class, triggers and on-failure as the catalog but a locally reworded
	// description: current, so it must survive a rewrite verbatim.
	reworded := injSecurityBlock("1.1")
	reworded.Description = "Reworded locally; still current."
	infra := injSection(reworded)
	files := map[string]string{injOrchestratorPath: refreshOrchestrator("0.5", infra, refreshCurrentWorkflow)}

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	if got == files[injOrchestratorPath] {
		t.Fatal("fixture error: the orchestrator was not rewritten for its stale version")
	}
	if region := injRegion(t, got, "InfrastructureAgents"); region != infra {
		t.Errorf("current declaration changed on rewrite:\n%s\nwant\n%s", region, infra)
	}
}

func TestUpdate_BothOrchestratorFiles_RefreshedAgainstTheirOwnDeclarations(t *testing.T) {
	files := map[string]string{
		injOrchestratorPath: refreshOrchestrator("1.0", injSection(injSecurityBlock("1.0")), refreshCurrentWorkflow),
		injScriptPath:       refreshOrchestrator("1.0", injSection(injCheckpointBlock("0.5")), refreshCurrentWorkflow),
	}

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	if got, want := refreshInfraRegion(t, run, injOrchestratorPath), injSection(injSecurityBlock("1.1")); got != want {
		t.Errorf("orchestrator region =\n%s\nwant\n%s", got, want)
	}
	if got, want := refreshInfraRegion(t, run, injScriptPath), injSection(injCheckpointBlock("1.0")); got != want {
		t.Errorf("script region =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdate_OnlyOneOrchestratorStale_OtherIsNotWritten(t *testing.T) {
	current := refreshOrchestrator("1.0", injSection(injCheckpointBlock("1.0")), refreshCurrentWorkflow)
	files := map[string]string{
		injOrchestratorPath: refreshOrchestrator("1.0", injSection(injSecurityBlock("1.0")), refreshCurrentWorkflow),
		injScriptPath:       current,
	}

	run := runUpdateRefresh(t, updateRefreshConfig{files: files})

	requireUpdateOK(t, run)
	if got, _ := run.readFile(t, injScriptPath); got != current {
		t.Error("the current orchestrator-script was rewritten")
	}
	if got, _ := run.readFile(t, injOrchestratorPath); got == files[injOrchestratorPath] {
		t.Error("the stale orchestrator was not refreshed")
	}
}

func TestUpdate_RefreshedOrchestrator_KeepsAvailableWorkflows(t *testing.T) {
	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(injSection(injSecurityBlock("1.0")))})

	requireUpdateOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	if w := injRegion(t, got, "AvailableWorkflows"); !strings.Contains(w, `name="quick-fix"`) {
		t.Errorf("AvailableWorkflows lost the deployed workflow:\n%s", w)
	}
}

func TestUpdate_DeclarationRefresh_AsksNoQuestionAndWritesNoBackup(t *testing.T) {
	run := runUpdateRefresh(t, updateRefreshConfig{files: refreshFiles(injSection(injSecurityBlock("1.0")))})

	requireUpdateOK(t, run)
	for _, c := range run.stub.Calls() {
		if c.ID == domain.QLocalModification || c.ID == domain.QDeployAgents {
			t.Errorf("unexpected question %q asked during a declaration refresh", c.ID)
		}
	}
	if _, err := os.Stat(filepath.Join(run.workspace, ".mosaic", "backups")); err == nil {
		t.Error("a backup was written for an unmodified orchestrator")
	}
}
