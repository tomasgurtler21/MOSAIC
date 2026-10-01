package plan_test

// orchestrator_infra_staleness_test.go covers how Planner.Build classifies orchestrator-role
// items for infrastructure declaration drift.
//
//   - Current versions plus missing or stale declarations make the orchestrator ActionUpdate,
//     with infrastructure deltas and a reason that names the keys.
//   - Current declarations and no other staleness leave it ActionUnchanged.
//   - orchestrator.md and orchestrator-script.md are evaluated independently against their own
//     deployed state.
//   - Non-orchestrator agents never get infrastructure drift.
//   - The refresh intent never adds a key; a zero intent disables the check.
//   - Existing version staleness is unchanged and infrastructure deltas stay out of the generic
//     version-delta reason text.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

const (
	infraOrchPath   = "agents/orchestrator.md"
	infraScriptPath = "agents/orchestrator-script.md"
	infraWorkerPath = "agents/test-worker.md"
)

// currentOrchestratorState returns a deployed state whose version stamps all match the fake
// catalog and module, carrying the given declarations.
func currentOrchestratorState(hash string, decls ...domain.DeployedInfrastructureDeclaration) domain.DeployedArtifactState {
	s := deployedState(hash, "1.0", "1.0", "1.0")
	if len(decls) > 0 {
		s.InfrastructureDeclarations = domain.DeployedInfrastructureDeclarations(decls)
	}
	return s
}

// buildInfraInput builds an update-workspace Input with both orchestrator-role files, one
// worker, the given infrastructure catalog agents, and the given deployed state per path.
// The manifest records every state's content hash so no local-modification conflict arises.
func buildInfraInput(
	intent plan.InfrastructureDeclarationIntent,
	states map[string]domain.DeployedArtifactState,
	infra ...domain.Agent,
) plan.Input {
	cat := fakeScriptCatalog()
	cat.workers = []domain.Agent{makeAgent("test-worker", "1.0")}
	cat.infraAgents = infra

	refs := map[string]string{
		infraOrchPath: "orchestrator", infraScriptPath: "orchestrator-script", infraWorkerPath: "test-worker",
	}
	var entries []domain.ManifestEntry
	var scanned []string
	for path, key := range refs {
		st, ok := states[path]
		if !ok {
			continue
		}
		e := makeManifestEntry(agentRef(key), path, "1.0", st.ContentHash)
		entries = append(entries, e)
		scanned = append(scanned, key)
	}
	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       entries,
	})
	models := map[string]domain.ModelSelection{}
	for _, key := range []string{"orchestrator", "orchestrator-script", "test-worker"} {
		models[key] = domain.ModelSelection{ModelID: "test-model", Origin: domain.OriginHarnessList}
	}
	return plan.Input{
		Catalog:                    cat,
		Module:                     newFakeModule(),
		Mode:                       domain.ModeUpdateWorkspace,
		WorkspacePath:              "/fake/workspace",
		Scope:                      domain.ScopeProject,
		GOOS:                       "linux",
		Manifest:                   snap,
		Models:                     models,
		DeployedState:              states,
		ScannedAgentKeys:           scanned,
		InfrastructureDeclarations: intent,
	}
}

func buildInfraPlan(t *testing.T, in plan.Input) domain.Plan {
	t.Helper()
	p, err := plan.New().Build(context.Background(), in)
	must(t, err)
	return p
}

func mustFindItem(t *testing.T, p domain.Plan, key string) domain.PlanItem {
	t.Helper()
	item, ok := findItem(p.Items, key)
	if !ok {
		t.Fatalf("plan has no item for %q", key)
	}
	return item
}

func hasInfraDelta(item domain.PlanItem, key string) bool {
	for _, d := range item.Stale {
		if d.Field == plan.InfrastructureDeltaFieldPrefix+key {
			return true
		}
	}
	return false
}

func TestBuild_OrchestratorMissingEnsuredDeclaration_BecomesUpdate(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{infraOrchPath: currentOrchestratorState("sha256:o1")}
	in := buildInfraInput(ensureIntent("code-review"), states, infraCatalogAgent("code-review", "1.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUpdate {
		t.Fatalf("Action = %q, want %q for an orchestrator missing an ensured declaration", item.Action, domain.ActionUpdate)
	}
	if !hasInfraDelta(item, "code-review") {
		t.Errorf("Stale = %+v, want an infrastructure delta for code-review", item.Stale)
	}
	if !strings.Contains(item.Reason, "code-review") {
		t.Errorf("Reason = %q, want it to name code-review", item.Reason)
	}
}

func TestBuild_OrchestratorStaleDeclaration_BecomesUpdate(t *testing.T) {
	declared := currentDeclaration("code-review", "1.0")
	states := map[string]domain.DeployedArtifactState{infraOrchPath: currentOrchestratorState("sha256:o1", declared)}
	in := buildInfraInput(refreshIntent, states, infraCatalogAgent("code-review", "2.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUpdate {
		t.Fatalf("Action = %q, want %q for a stale declaration", item.Action, domain.ActionUpdate)
	}
	if !hasInfraDelta(item, "code-review") {
		t.Errorf("Stale = %+v, want an infrastructure delta for code-review", item.Stale)
	}
	if !strings.Contains(item.Reason, "code-review") {
		t.Errorf("Reason = %q, want it to name code-review", item.Reason)
	}
}

func TestBuild_OrchestratorCurrentDeclarations_StaysUnchanged(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath: currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0")),
	}
	in := buildInfraInput(ensureIntent("code-review"), states, infraCatalogAgent("code-review", "1.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUnchanged {
		t.Errorf("Action = %q, want %q; Stale = %+v, Reason = %q", item.Action, domain.ActionUnchanged, item.Stale, item.Reason)
	}
}

func TestBuild_OrchestratorZeroIntent_IgnoresDeclarations(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath: currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "0.1")),
	}
	in := buildInfraInput(plan.InfrastructureDeclarationIntent{}, states, infraCatalogAgent("code-review", "2.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUnchanged {
		t.Errorf("Action = %q, want %q when no infrastructure intent is set", item.Action, domain.ActionUnchanged)
	}
}

func TestBuild_OrchestratorUnknownAndUnparsedDeclarations_StayUnchanged(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath: currentOrchestratorState("sha256:o1",
			currentDeclaration("team-extra", "9.9"),
			domain.DeployedInfrastructureDeclaration{Key: "code-review", Version: "0.1", Parsed: false}),
	}
	in := buildInfraInput(refreshIntent, states, infraCatalogAgent("code-review", "2.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUnchanged {
		t.Errorf("Action = %q, want %q; unknown and unparsed declarations never cause drift", item.Action, domain.ActionUnchanged)
	}
}

func TestBuild_OrchestratorRefreshIntent_NeverAddsUndeclaredKeys(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath: currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0")),
	}
	in := buildInfraInput(refreshIntent, states,
		infraCatalogAgent("code-review", "1.0"), infraCatalogAgent("commit-agent", "1.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUnchanged {
		t.Errorf("Action = %q, want %q; refresh must not treat an undeclared key as drift", item.Action, domain.ActionUnchanged)
	}
}

func TestBuild_BothOrchestratorFiles_EvaluatedAgainstOwnDeployedState(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath:   currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0")),
		infraScriptPath: currentOrchestratorState("sha256:o2"), // declares nothing
	}
	in := buildInfraInput(ensureIntent("code-review"), states, infraCatalogAgent("code-review", "1.0"))

	p := buildInfraPlan(t, in)

	if got := mustFindItem(t, p, "orchestrator").Action; got != domain.ActionUnchanged {
		t.Errorf("orchestrator Action = %q, want %q (its own declarations are current)", got, domain.ActionUnchanged)
	}
	script := mustFindItem(t, p, "orchestrator-script")
	if script.Action != domain.ActionUpdate {
		t.Fatalf("orchestrator-script Action = %q, want %q (it lacks the ensured key)", script.Action, domain.ActionUpdate)
	}
	if !hasInfraDelta(script, "code-review") {
		t.Errorf("orchestrator-script Stale = %+v, want an infrastructure delta for code-review", script.Stale)
	}
}

func TestBuild_BothOrchestratorFiles_RefreshOnlyTouchesStaleOne(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraOrchPath:   currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0")),
		infraScriptPath: currentOrchestratorState("sha256:o2"), // declares nothing
	}
	in := buildInfraInput(refreshIntent, states, infraCatalogAgent("code-review", "2.0"))

	p := buildInfraPlan(t, in)

	if got := mustFindItem(t, p, "orchestrator").Action; got != domain.ActionUpdate {
		t.Errorf("orchestrator Action = %q, want %q (declared key is stale)", got, domain.ActionUpdate)
	}
	if got := mustFindItem(t, p, "orchestrator-script").Action; got != domain.ActionUnchanged {
		t.Errorf("orchestrator-script Action = %q, want %q (refresh never adds)", got, domain.ActionUnchanged)
	}
}

func TestBuild_WorkerAgent_NeverGetsInfrastructureDrift(t *testing.T) {
	states := map[string]domain.DeployedArtifactState{
		infraWorkerPath: currentOrchestratorState("sha256:w1", currentDeclaration("code-review", "0.1")),
	}
	in := buildInfraInput(ensureIntent("code-review"), states, infraCatalogAgent("code-review", "2.0"))

	item, ok := findItem(buildInfraPlan(t, in).Items, "test-worker")
	if !ok {
		t.Skip("worker is not part of this plan")
	}
	if item.Action != domain.ActionUnchanged || hasInfraDelta(item, "code-review") {
		t.Errorf("worker Action = %q, Stale = %+v; infrastructure drift applies to orchestrator-role agents only",
			item.Action, item.Stale)
	}
}

func TestBuild_OrchestratorVersionStaleAndInfraDrift_SingleItemWithBothReasons(t *testing.T) {
	st := currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0"))
	st.Version = "0.5" // catalog orchestrator is 1.0
	states := map[string]domain.DeployedArtifactState{infraOrchPath: st}
	in := buildInfraInput(refreshIntent, states, infraCatalogAgent("code-review", "2.0"))

	p := buildInfraPlan(t, in)
	item := mustFindItem(t, p, "orchestrator")

	if item.Action != domain.ActionUpdate {
		t.Fatalf("Action = %q, want %q", item.Action, domain.ActionUpdate)
	}
	var versionDelta bool
	for _, d := range item.Stale {
		if d.Field == "version" {
			versionDelta = true
		}
	}
	if !versionDelta || !hasInfraDelta(item, "code-review") {
		t.Errorf("Stale = %+v, want both the version delta and the infrastructure delta", item.Stale)
	}
	if !strings.Contains(item.Reason, "code-review") {
		t.Errorf("Reason = %q, want it to name code-review", item.Reason)
	}
	if strings.Contains(item.Reason, plan.InfrastructureDeltaFieldPrefix) {
		t.Errorf("Reason = %q must not render infrastructure deltas through the generic version-delta text", item.Reason)
	}
}

func TestBuild_OrchestratorVersionStaleOnly_ReasonUnchangedByInfrastructureSupport(t *testing.T) {
	st := currentOrchestratorState("sha256:o1", currentDeclaration("code-review", "1.0"))
	st.Version = "0.5"
	states := map[string]domain.DeployedArtifactState{infraOrchPath: st}
	in := buildInfraInput(refreshIntent, states, infraCatalogAgent("code-review", "1.0"))

	item := mustFindItem(t, buildInfraPlan(t, in), "orchestrator")

	if item.Action != domain.ActionUpdate {
		t.Fatalf("Action = %q, want %q", item.Action, domain.ActionUpdate)
	}
	if strings.Contains(item.Reason, "infrastructure") || hasInfraDelta(item, "code-review") {
		t.Errorf("Reason = %q, Stale = %+v; no infrastructure drift exists so none may be reported", item.Reason, item.Stale)
	}
}
