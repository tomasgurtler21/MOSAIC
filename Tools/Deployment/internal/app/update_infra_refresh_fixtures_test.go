package app_test

// update_infra_refresh_fixtures_test.go holds the shared fixtures and the run helper for the
// update-flow infrastructure declaration refresh tests: deployed orchestrator builders whose
// version matches the catalog, and a helper that drives Service.Update with the real planner
// and the real executor against a temporary workspace.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/transform"
)

// refreshProtocolRegion carries the protocol version the default test loader reports, so the
// protocol never makes a deployed orchestrator stale on its own.
const refreshProtocolRegion = `<CommunicationProtocol type="managed" version="1.9">
## Communication Protocol

Orchestrator protocol text.
</CommunicationProtocol>
`

const (
	refreshAgentsDir       = "agents"
	refreshCheckpointNumID = "77"
	// refreshCurrentWorkflow is a deployed workflow at the catalog's version, so workflow
	// drift never makes an orchestrator stale on its own.
	refreshCurrentWorkflow = `<Workflow type="managed" name="quick-fix" version="1.0">
## Quick Fix
</Workflow>
`
)

// refreshOrchestrator returns a deployed orchestrator-role document. version is the
// frontmatter version: "1.0" matches the catalog, anything else makes the file protocol/version
// stale on its own.
func refreshOrchestrator(version, infraInner, workflowInner string) string {
	doc := strings.Replace(injDeployedOrchestrator(infraInner, workflowInner), `version: "0.9"`, `version: "`+version+`"`, 1)
	return strings.Replace(doc, "<IdentityExtension", refreshProtocolRegion+"\n<IdentityExtension", 1)
}

// updateRefreshConfig describes one Update run.
type updateRefreshConfig struct {
	// files maps workspace-relative path to the bytes present before the run.
	files map[string]string
	// modified lists the orchestrator-role files left without a manifest record,
	// which makes the planner classify them as locally modified conflicts.
	modified map[string]bool
	// conflictDefault pre-answers conflicts; empty leaves the question to stub.
	conflictDefault domain.ConflictDecision
	// stub overrides the default interaction script (review confirmed, nothing else answered).
	stub *interactiontest.Stub
}

// runUpdateRefresh executes Service.Update against a temp workspace seeded from cfg. Every
// orchestrator-role file in cfg.files has a manifest record at version 1.0 unless modified.
func runUpdateRefresh(t *testing.T, cfg updateRefreshConfig) *injectionRun {
	t.Helper()
	stub := cfg.stub
	if stub == nil {
		stub = interactiontest.NewBuilder().AnswerReview(true).Build()
	}
	deps, workspace := newBaseDeps(t, stub)
	cat := injCatalog()
	cat.infraAgents[1].NumericID = refreshCheckpointNumID
	deps.Catalog = cat
	deps.Planner = plan.New()
	mod := newModuleWithAgentsDir(refreshAgentsDir)
	mod.descriptor.Frontmatter.ModelKey = "model"
	deps.Registry = newMinimalRegistry(mod)
	if err := os.MkdirAll(filepath.Join(workspace, refreshAgentsDir), 0o755); err != nil {
		t.Fatalf("mkdir agents dir: %v", err)
	}

	var entries []domain.ManifestEntry
	for rel, content := range cfg.files {
		writeInjectionFile(t, workspace, rel, content)
		if rel != injOrchestratorPath && rel != injScriptPath {
			continue
		}
		if cfg.modified[rel] {
			continue // no manifest record: the planner cannot confirm the file is unmodified
		}
		entries = append(entries, domain.ManifestEntry{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: strings.TrimSuffix(rel, ".md")},
			TargetPath: rel, Version: "1.0", ContentHash: manifest.Hash([]byte(content)),
		})
	}
	deps.Manifest = &stubManifestStore{snap: manifest.Snapshot{State: manifest.StatePresent, Manifest: domain.Manifest{
		SchemaVersion: manifest.SchemaVersion, HarnessID: "stub-harness", Entries: entries,
	}}}
	rec := &injectionRecorder{inner: deploy.NewExecutor(deps.Manifest, deps.Logger, deps.Todo)}
	deps.Executor = rec
	spy := deps.Todo.(*spyTodo)

	_, err := app.New(deps).Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: cfg.conflictDefault,
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true, domain.QAgentModel: true},
	})
	return &injectionRun{workspace: workspace, stub: stub, todo: spy, rec: rec, err: err}
}

// requireUpdateOK fails the test when the Update run returned an error.
func requireUpdateOK(t *testing.T, run *injectionRun) {
	t.Helper()
	if run.err != nil {
		t.Fatalf("Update returned error: %v", run.err)
	}
}

// refreshInfraRegion returns the InfrastructureAgents region of a workspace file after the run.
func refreshInfraRegion(t *testing.T, run *injectionRun, rel string) string {
	t.Helper()
	got, ok := run.readFile(t, rel)
	if !ok {
		t.Fatalf("%s is missing after the run", rel)
	}
	return injRegion(t, got, "InfrastructureAgents")
}

// declCount returns how many sections declare the given infrastructure key in region.
func declCount(region, key string) int {
	n := 0
	for _, d := range transform.ReadInfrastructureDeclarations([]byte(injDeployedOrchestrator(region, ""))) {
		if d.Key == key {
			n++
		}
	}
	return n
}
