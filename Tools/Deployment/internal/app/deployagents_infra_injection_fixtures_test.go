package app_test

// deployagents_infra_injection_fixtures_test.go holds the shared fixtures and the run helper
// for the deploy-agents infrastructure declaration injection tests: a catalog with both
// orchestrator-role agents and two infrastructure agents, deployed orchestrator builders,
// and a helper that drives Service.DeployAgents with the real planner and the real executor
// against a temporary workspace.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/transform"
)

const (
	injOrchestratorPath = "orchestrator.md"
	injScriptPath       = "orchestrator-script.md"
	injSecurityKey      = "security-scanner"
	injCheckpointKey    = "checkpoint-writer"
	injPlainSubagentKey = "plan-review"
)

// injWorkerSource is the generic source document used for every non-orchestrator agent.
const injWorkerSource = `---
id: 99
version: 1.0.0
name: injected-worker
description: Synthetic worker agent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
Static prose.
</Identity>
`

// injWorkflowInner is a deployed AvailableWorkflows region that a catalog rebuild could not
// reproduce: the first workflow carries a version different from the catalog's, with unusual
// spacing; the second has an ID the catalog no longer contains.
const injWorkflowInner = `<Workflow type="managed" name="quick-fix" version="0.0.1-local">
## Quick Fix Workflow (locally edited)

**Use when:**   tiny changes only.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |

</Workflow>
<Workflow type="managed" name="retired-flow" version="9.9">
## A workflow that is no longer in any catalog

</Workflow>
`

// injScriptWorkflowInner is a different deployed workflow region, used to prove each
// orchestrator-role file is handled against its own content.
const injScriptWorkflowInner = `<Workflow type="managed" name="script-only-flow" version="3.1">
## Script only flow

</Workflow>
`

// injSecurityBlock returns the block the catalog agent "security-scanner" produces at the
// given version.
func injSecurityBlock(version string) transform.InfrastructureBlock {
	return transform.InfrastructureBlock{
		Key: injSecurityKey, Name: injSecurityKey, Version: version, Class: "security",
		Description: "Scans for vulnerabilities", OnFailure: "continue",
		Triggers: []domain.InfrastructureTrigger{{Trigger: "STAGE_END"}},
	}
}

// injCheckpointBlock returns the block the catalog agent "checkpoint-writer" produces at
// the given version.
func injCheckpointBlock(version string) transform.InfrastructureBlock {
	return transform.InfrastructureBlock{
		Key: injCheckpointKey, Name: injCheckpointKey, Version: version, Class: "checkpoint",
		Description: "Writes checkpoints", OnFailure: "halt",
		Triggers: []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"}},
	}
}

// injHandAddedBlock is a declaration with no catalog counterpart.
func injHandAddedBlock() transform.InfrastructureBlock {
	return transform.InfrastructureBlock{
		Key: "team-extra", Name: "team-extra", Version: "2.0", Class: "review",
		Description: "Team specific check.", OnFailure: "continue",
		Triggers: []domain.InfrastructureTrigger{{Trigger: "MANUAL"}},
	}
}

// injSection returns the section bytes the assembler writes for the given blocks.
func injSection(blocks ...transform.InfrastructureBlock) string {
	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)
	return string(assembled)
}

// injDeployedOrchestrator returns a previously deployed orchestrator document carrying the
// deployed model "model-b" and the given inner content for both managed regions.
func injDeployedOrchestrator(infraInner, workflowInner string) string {
	return `---
version: "0.9"
model: model-b
---

<Identity type="core">
# Orchestrator

<InfrastructureAgents type="managed">
` + infraInner + `</InfrastructureAgents>

<AvailableWorkflows type="managed">
` + workflowInner + `</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`
}

// injCatalog returns a catalog with both orchestrator-role agents, two infrastructure agents
// (catalog versions: security-scanner 1.1, checkpoint-writer 1.0), one ordinary subagent and
// one workflow whose catalog version (1.0) differs from the deployed one.
func injCatalog() *stubCatalog {
	cat := scriptContentCatalog("src/orchestrator-script.md", "src/orchestrator.md")
	infra := []domain.Agent{
		{Key: injSecurityKey, Version: "1.1", Name: injSecurityKey, Description: "Scans for vulnerabilities",
			Role: domain.RoleSubagent, Infrastructure: "security", OnFailure: "continue",
			Triggers: []domain.InfrastructureTrigger{{Trigger: "STAGE_END"}}, SourcePath: "src/" + injSecurityKey + ".md"},
		{Key: injCheckpointKey, Version: "1.0", Name: injCheckpointKey, Description: "Writes checkpoints",
			Role: domain.RoleSubagent, Infrastructure: "checkpoint", OnFailure: "halt",
			Triggers: []domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"}}, SourcePath: "src/" + injCheckpointKey + ".md"},
	}
	plain := domain.Agent{Key: injPlainSubagentKey, Version: "1.0", Name: "Plan Review", Description: "Reviews plans",
		Role: domain.RoleSubagent, RecommendedTier: "HIGH", SourcePath: "src/" + injPlainSubagentKey + ".md"}
	cat.infraAgents = infra
	cat.agents = append(cat.agents, plain)
	for _, a := range append(append([]domain.Agent{}, infra...), plain) {
		cat.sources[a.SourcePath] = []byte(injWorkerSource)
	}
	return cat
}

// injectionConfig describes one DeployAgents run.
type injectionConfig struct {
	// files maps workspace-relative path to the bytes present before the run.
	files map[string]string
	// manifested records a usable manifest entry for every orchestrator-role file in files, so
	// the planner does not classify those files as conflicts. Leave false to force a conflict.
	manifested bool
	// infraIDs pre-answers the selection (CLI path). Ignored when interactive is non-nil.
	infraIDs []string
	// subagentIDs pre-answers ordinary subagents.
	subagentIDs []string
	// interactive, when non-nil, leaves every ID field nil and lets the stub answer the
	// merged selection question (TUI path) with these keys.
	interactive     []string
	conflictDefault domain.ConflictDecision
	// withoutCatalogOrchestrator removes both orchestrator-role agents from the catalog.
	withoutCatalogOrchestrator bool
	// stub overrides the default interaction script (review confirmed, nothing else answered).
	stub *interactiontest.Stub
}

// injectionRecorder wraps the real executor and keeps the request it received.
type injectionRecorder struct {
	inner  deploy.Executor
	req    deploy.ExecRequest
	called bool
}

func (r *injectionRecorder) Execute(ctx context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	r.called = true
	r.req = req
	return r.inner.Execute(ctx, req)
}

// injectionRun is the observable outcome of runInfraInjection.
type injectionRun struct {
	workspace string
	stub      *interactiontest.Stub
	todo      *spyTodo
	rec       *injectionRecorder
	err       error
}

// runInfraInjection executes Service.DeployAgents against a temp workspace seeded from cfg.
func runInfraInjection(t *testing.T, cfg injectionConfig) *injectionRun {
	t.Helper()
	stub := cfg.stub
	if stub == nil {
		builder := interactiontest.NewBuilder().AnswerReview(true)
		if cfg.interactive != nil {
			builder.AnswerSelectMany(domain.QDeployAgents, "", cfg.interactive)
		}
		stub = builder.Build()
	}
	deps, workspace := newBaseDeps(t, stub)
	cat := injCatalog()
	if cfg.withoutCatalogOrchestrator {
		cat.orchestrator = domain.Agent{}
		cat.orchestratorScriptOK = false
	}
	deps.Catalog = cat
	deps.Planner = plan.New()
	deps.Registry = newInjectionRegistry()
	snap := manifest.Snapshot{State: manifest.StateAbsent}
	var entries []domain.ManifestEntry
	for rel, content := range cfg.files {
		writeInjectionFile(t, workspace, rel, content)
		if cfg.manifested && (rel == injOrchestratorPath || rel == injScriptPath) {
			key := strings.TrimSuffix(rel, ".md")
			entries = append(entries, domain.ManifestEntry{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
				TargetPath: rel, Version: "0.9", ContentHash: manifest.Hash([]byte(content)),
			})
		}
	}
	if cfg.manifested {
		snap = manifest.Snapshot{State: manifest.StatePresent, Manifest: domain.Manifest{
			SchemaVersion: "1", HarnessID: "stub-harness", Entries: entries,
		}}
	}
	deps.Manifest = &stubManifestStore{snap: snap}
	rec := &injectionRecorder{inner: deploy.NewExecutor(deps.Manifest, deps.Logger, deps.Todo)}
	deps.Executor = rec
	spy := deps.Todo.(*spyTodo)

	req := app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		AutoConfirmPlan: true,
		ConflictDefault: cfg.conflictDefault,
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true, domain.QAgentModel: true},
	}
	if cfg.interactive == nil {
		req.SubagentIDs = nonNil(cfg.subagentIDs)
		req.UtilityAgentIDs = []string{}
		req.StandaloneAgentIDs = []string{}
		req.InfrastructureAgentIDs = nonNil(cfg.infraIDs)
	}
	_, err := app.New(deps).DeployAgents(context.Background(), req)
	return &injectionRun{workspace: workspace, stub: stub, todo: spy, rec: rec, err: err}
}

// newInjectionRegistry returns a registry whose harness emits the model under "model", so the
// deployed model of an orchestrator can be probed.
func newInjectionRegistry() *stubRegistry {
	mod := newMinimalModule()
	mod.descriptor.Frontmatter.ModelKey = "model"
	return newMinimalRegistry(mod)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writeInjectionFile(t *testing.T, workspace, rel, content string) {
	t.Helper()
	full := filepath.Join(workspace, rel)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("seed %s: %v", rel, err)
	}
}

// readFile returns the workspace file's content, or "" and false when absent.
func (r *injectionRun) readFile(t *testing.T, rel string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.workspace, rel))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// planItem returns the plan item for a target path, if the executor received one.
func (r *injectionRun) planItem(target string) (domain.PlanItem, bool) {
	for _, item := range r.rec.req.Plan.Items {
		if item.TargetPath == target {
			return item, true
		}
	}
	return domain.PlanItem{}, false
}

// agentKeysInPlan returns the keys of every agent plan item the executor received.
func (r *injectionRun) agentKeysInPlan() []string {
	var keys []string
	for _, item := range r.rec.req.Plan.Items {
		if item.Ref.Kind == domain.ArtifactAgent {
			keys = append(keys, item.Ref.Key)
		}
	}
	return keys
}

// injRegion returns the inner bytes of the named managed region of a document.
func injRegion(t *testing.T, doc, name string) string {
	t.Helper()
	parsed, err := docformat.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := parsed.Body().Deployed(name)
	if !ok {
		t.Fatalf("region %q absent from document", name)
	}
	return string(node.Content())
}
