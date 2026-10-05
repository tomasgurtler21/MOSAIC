package app_test

// deployagents_infra_injection_test.go verifies that Service.DeployAgents declares newly
// deployed infrastructure agents in every orchestrator-role file already present in the
// workspace: additive and per file, workflows kept verbatim, written only when needed, never
// created when absent, and without any extra question.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// orchestratorFiles returns a seed map holding one deployed orchestrator.
func orchestratorFiles(infraInner, workflowInner string) map[string]string {
	return map[string]string{injOrchestratorPath: injDeployedOrchestrator(infraInner, workflowInner)}
}

func requireRunOK(t *testing.T, run *injectionRun) {
	t.Helper()
	if run.err != nil {
		t.Fatalf("DeployAgents returned error: %v", run.err)
	}
}

func TestDeployAgents_InfraSelected_DeclaredInPresentOrchestratorWithoutExtraQuestion(t *testing.T) {
	cfg := injectionConfig{
		files: orchestratorFiles("", injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	if want := injSection(injSecurityBlock("1.1")); !strings.Contains(injRegion(t, got, "InfrastructureAgents"), want) {
		t.Errorf("orchestrator does not declare %s with the catalog block:\n%s", injSecurityKey, got)
	}
	for _, call := range run.stub.Calls() {
		if call.Subject == "orchestrator" || call.Subject == injOrchestratorPath {
			t.Errorf("a question was asked about the orchestrator: id=%s subject=%s", call.ID, call.Subject)
		}
	}
	for _, id := range run.stub.QuestionOrder() {
		if id != domain.QPlanConfirm {
			t.Errorf("unexpected question %q asked with a fully pre-answered request", id)
		}
	}
}

func TestDeployAgents_InfraSelected_KeepsExistingAndHandAddedDeclarationsByteIdentical(t *testing.T) {
	existing := injSection(injCheckpointBlock("1.0")) + injSection(injHandAddedBlock())
	cfg := injectionConfig{
		files: orchestratorFiles(existing, injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	region := injRegion(t, got, "InfrastructureAgents")
	if !strings.HasPrefix(region, existing) {
		t.Errorf("existing declarations were not kept byte-identical.\nregion:\n%s\nwant prefix:\n%s", region, existing)
	}
	if n := strings.Count(region, injSection(injSecurityBlock("1.1"))); n != 1 {
		t.Errorf("new declaration appears %d times, want 1", n)
	}
}

func TestDeployAgents_RedeployedInfraKey_RefreshedInPlaceNotDuplicated(t *testing.T) {
	stale := injSection(injSecurityBlock("1.0"))
	other := injSection(injCheckpointBlock("1.0"))
	cfg := injectionConfig{
		files: orchestratorFiles(stale+other, injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	region := injRegion(t, got, "InfrastructureAgents")
	if n := strings.Count(region, injSection(injSecurityBlock("1.1"))); n != 1 {
		t.Errorf("refreshed declaration appears %d times, want 1", n)
	}
	if strings.Contains(region, stale) {
		t.Error("the stale declaration is still present")
	}
	if !strings.HasSuffix(region, other) {
		t.Error("the other declaration moved or changed; refresh must happen in place")
	}
}

func TestDeployAgents_InfraSelected_AvailableWorkflowsKeptVerbatim(t *testing.T) {
	// Deployed workflow versions differ from the catalog and one ID is not in the catalog.
	cfg := injectionConfig{
		files: orchestratorFiles("", injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	// The file must actually have been rewritten, otherwise verbatim workflows prove nothing.
	if want := injSection(injSecurityBlock("1.1")); !strings.Contains(injRegion(t, got, "InfrastructureAgents"), want) {
		t.Errorf("orchestrator was not rewritten with the %s declaration:\n%s", injSecurityKey, got)
	}
	if region := injRegion(t, got, "AvailableWorkflows"); region != injWorkflowInner {
		t.Errorf("AvailableWorkflows changed.\ngot:\n%s\nwant:\n%s", region, injWorkflowInner)
	}
}

func TestDeployAgents_OneOrchestratorNeedsDeclaration_CurrentOneNotWritten(t *testing.T) {
	current := injDeployedOrchestrator(injSection(injSecurityBlock("1.1")), injScriptWorkflowInner)
	files := map[string]string{
		injOrchestratorPath: injDeployedOrchestrator("", injWorkflowInner),
		injScriptPath:       current,
	}
	cfg := injectionConfig{files: files, manifested: true, infraIDs: []string{injSecurityKey}}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	main, _ := run.readFile(t, injOrchestratorPath)
	if !strings.Contains(injRegion(t, main, "InfrastructureAgents"), injSection(injSecurityBlock("1.1"))) {
		t.Error("orchestrator.md was not given the declaration")
	}
	if got, _ := run.readFile(t, injScriptPath); got != current {
		t.Error("orchestrator-script.md was rewritten although its declarations were already current")
	}
	if _, ok := run.planItem(injScriptPath); ok {
		t.Error("orchestrator-script.md is in the plan although its declarations were already current")
	}
}

func TestDeployAgents_InfraSelected_ScriptOrchestratorRewrittenAndStamped(t *testing.T) {
	files := map[string]string{
		injOrchestratorPath: injDeployedOrchestrator("", injWorkflowInner),
		injScriptPath:       injDeployedOrchestrator("", injScriptWorkflowInner),
	}
	cfg := injectionConfig{files: files, manifested: true, infraIDs: []string{injSecurityKey}}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	for _, rel := range []string{injOrchestratorPath, injScriptPath} {
		stamp, ok := run.rec.req.VersionStamps[rel]
		if !ok || stamp.Version == "" {
			t.Errorf("no version stamp for %s: %+v (present=%v)", rel, stamp, ok)
		}
	}
	script, _ := run.readFile(t, injScriptPath)
	if !strings.Contains(script, "model: model-b") {
		t.Errorf("rewritten orchestrator-script.md lost its deployed model:\n%s", script)
	}
}

func TestDeployAgents_InfraSelected_EachOrchestratorFileMergedAgainstItsOwnContent(t *testing.T) {
	files := map[string]string{
		injOrchestratorPath: injDeployedOrchestrator(injSection(injCheckpointBlock("1.0")), injWorkflowInner),
		injScriptPath:       injDeployedOrchestrator(injSection(injHandAddedBlock()), injScriptWorkflowInner),
	}
	cfg := injectionConfig{files: files, manifested: true, infraIDs: []string{injSecurityKey}}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	secSection := injSection(injSecurityBlock("1.1"))
	main, _ := run.readFile(t, injOrchestratorPath)
	script, _ := run.readFile(t, injScriptPath)
	mainInfra := injRegion(t, main, "InfrastructureAgents")
	scriptInfra := injRegion(t, script, "InfrastructureAgents")
	if !strings.Contains(mainInfra, secSection) || !strings.Contains(mainInfra, injSection(injCheckpointBlock("1.0"))) {
		t.Errorf("orchestrator.md lost or lacks declarations:\n%s", mainInfra)
	}
	if !strings.Contains(scriptInfra, secSection) || !strings.Contains(scriptInfra, injSection(injHandAddedBlock())) {
		t.Errorf("orchestrator-script.md lost or lacks declarations:\n%s", scriptInfra)
	}
	if strings.Contains(scriptInfra, injCheckpointKey) {
		t.Error("orchestrator-script.md received a declaration from the other file's content")
	}
	if region := injRegion(t, main, "AvailableWorkflows"); region != injWorkflowInner {
		t.Errorf("orchestrator.md workflows changed:\n%s", region)
	}
	if region := injRegion(t, script, "AvailableWorkflows"); region != injScriptWorkflowInner {
		t.Errorf("orchestrator-script.md workflows changed:\n%s", region)
	}
}

func TestDeployAgents_NoInfraSelected_OrchestratorNotWritten(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)
	cfg := injectionConfig{
		files:       map[string]string{injOrchestratorPath: original},
		manifested:  true,
		subagentIDs: []string{injPlainSubagentKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("orchestrator.md was rewritten although no infrastructure agent was selected")
	}
	if _, ok := run.planItem(injOrchestratorPath); ok {
		t.Error("orchestrator.md is in the plan although no infrastructure agent was selected")
	}
}

func TestDeployAgents_DeclarationsAlreadyCurrent_OrchestratorNotWrittenEvenWhenVersionStale(t *testing.T) {
	// The deployed file carries version 0.9 against catalog 1.0: stale for unrelated reasons.
	original := injDeployedOrchestrator(injSection(injSecurityBlock("1.1")), injWorkflowInner)
	cfg := injectionConfig{
		files: map[string]string{injOrchestratorPath: original}, manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("orchestrator.md was rewritten although its declarations were already current")
	}
	if _, ok := run.planItem(injOrchestratorPath); ok {
		t.Error("orchestrator.md was pulled into the plan only because its version is stale")
	}
	if _, ok := run.readFile(t, injSecurityKey+".md"); !ok {
		t.Error("the selected infrastructure agent itself was not deployed")
	}
}

func TestDeployAgents_OrchestratorAbsentFromDisk_NotCreatedAndAgentsStillDeploy(t *testing.T) {
	cfg := injectionConfig{files: map[string]string{}, infraIDs: []string{injSecurityKey}}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	for _, rel := range []string{injOrchestratorPath, injScriptPath} {
		if _, ok := run.readFile(t, rel); ok {
			t.Errorf("%s was created although it was absent from the workspace", rel)
		}
	}
	if _, ok := run.readFile(t, injSecurityKey+".md"); !ok {
		t.Error("the infrastructure agent was not deployed")
	}
}

func TestDeployAgents_CatalogWithoutOrchestrator_RunSucceedsAndLeavesFileAlone(t *testing.T) {
	// A present orchestrator file whose catalog counterpart is missing contributes nothing.
	original := injDeployedOrchestrator("", injWorkflowInner)
	cfg := injectionConfig{
		files: map[string]string{injOrchestratorPath: original}, manifested: true,
		infraIDs: []string{injSecurityKey}, withoutCatalogOrchestrator: true,
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("orchestrator.md changed although the catalog has no orchestrator")
	}
}

func TestDeployAgents_InfraSelected_OrchestratorKeepsDeployedModel(t *testing.T) {
	cfg := injectionConfig{
		files: orchestratorFiles("", injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	item, ok := run.planItem(injOrchestratorPath)
	if !ok {
		t.Fatal("orchestrator.md is not in the plan")
	}
	if item.Model.ModelID != "model-b" || item.Model.Origin != domain.OriginDeployed {
		t.Errorf("orchestrator model = %+v, want model-b with origin deployed", item.Model)
	}
}

func TestDeployAgents_InfraSelected_PlanNamesInfraKeysAndExcludesWorkflowAgents(t *testing.T) {
	cfg := injectionConfig{
		files: orchestratorFiles("", injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	item, ok := run.planItem(injOrchestratorPath)
	if !ok {
		t.Fatal("orchestrator.md is not in the plan")
	}
	if item.Action == domain.ActionCreate || !strings.Contains(item.Reason, injSecurityKey) {
		t.Errorf("orchestrator item action=%s reason=%q, want an update whose reason names %s", item.Action, item.Reason, injSecurityKey)
	}
	keys := strings.Join(run.agentKeysInPlan(), ",")
	if strings.Contains(keys, "test-runner") {
		t.Errorf("a workflow-referenced agent entered the plan: %s", keys)
	}
	if !strings.Contains(keys, injSecurityKey) || !strings.Contains(keys, "orchestrator") {
		t.Errorf("plan agents = %s, want the infra agent and the orchestrator", keys)
	}
}

func TestDeployAgents_InfraSelected_RewrittenOrchestratorIsVersionStamped(t *testing.T) {
	cfg := injectionConfig{
		files: orchestratorFiles("", injWorkflowInner), manifested: true,
		infraIDs: []string{injSecurityKey},
	}

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	stamp, ok := run.rec.req.VersionStamps[injOrchestratorPath]
	if !ok || stamp.Version == "" {
		t.Errorf("no version stamp for the rewritten orchestrator: %+v (present=%v)", stamp, ok)
	}
}

func TestDeployAgents_InteractiveAndPreAnsweredSelection_ProduceIdenticalOrchestrator(t *testing.T) {
	files := orchestratorFiles(injSection(injCheckpointBlock("1.0")), injWorkflowInner)
	pre := runInfraInjection(t, injectionConfig{files: files, manifested: true, infraIDs: []string{injSecurityKey}})
	tui := runInfraInjection(t, injectionConfig{
		files: files, manifested: true, interactive: []string{injSecurityKey},
		stub: interactiontest.NewBuilder().
			AnswerSelectMany(domain.QDeployAgents, "", []string{injSecurityKey}).
			AnswerReview(true).Build(),
	})

	requireRunOK(t, pre)
	requireRunOK(t, tui)
	a, _ := pre.readFile(t, injOrchestratorPath)
	b, _ := tui.readFile(t, injOrchestratorPath)
	if a != b {
		t.Errorf("orchestrator differs between pre-answered and interactive selection:\n%s\n---\n%s", a, b)
	}
	if !strings.Contains(a, injSection(injSecurityBlock("1.1"))) {
		t.Error("the selected infrastructure agent is not declared")
	}
	for _, call := range tui.stub.Calls() {
		if call.Subject == "orchestrator" || call.Subject == injOrchestratorPath {
			t.Errorf("interactive path asked about the orchestrator: id=%s", call.ID)
		}
	}
}
