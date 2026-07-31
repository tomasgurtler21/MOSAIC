package app_test

// testhelpers_test.go provides shared stubs, spies, and builder helpers used across
// all app test files. Nothing in this file is a test itself.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// stubCatalog — satisfies catalog.Catalog with configurable data
// ---------------------------------------------------------------------------

type stubCatalog struct {
	root          string
	agents        []domain.Agent
	orchestrator  domain.Agent
	utilityAgents []domain.Agent
	skills        []domain.Skill
	hooks         []domain.HookBundle
	workflows     []domain.Workflow
	categories    []domain.WorkflowCategory
	tiers         []domain.TierInfo
	sections      map[string][]byte
	sources       map[string][]byte
}

func (c *stubCatalog) Root() string                                       { return c.root }
func (c *stubCatalog) Agents() []domain.Agent                             { return c.agents }
func (c *stubCatalog) Agent(key string) (domain.Agent, bool) {
	for _, a := range append(c.agents, append(c.utilityAgents, c.orchestrator)...) {
		if a.Key == key {
			return a, true
		}
	}
	return domain.Agent{}, false
}
func (c *stubCatalog) Orchestrator() domain.Agent                         { return c.orchestrator }
func (c *stubCatalog) UtilityAgents() []domain.Agent                      { return c.utilityAgents }
func (c *stubCatalog) Skills() []domain.Skill                             { return c.skills }
func (c *stubCatalog) Skill(key string) (domain.Skill, bool) {
	for _, sk := range c.skills {
		if sk.Key == key {
			return sk, true
		}
	}
	return domain.Skill{}, false
}
func (c *stubCatalog) Hooks() []domain.HookBundle                         { return c.hooks }
func (c *stubCatalog) Hook(key string) (domain.HookBundle, bool) {
	for _, h := range c.hooks {
		if h.Key == key {
			return h, true
		}
	}
	return domain.HookBundle{}, false
}
func (c *stubCatalog) Workflows() []domain.Workflow                       { return c.workflows }
func (c *stubCatalog) Workflow(id string) (domain.Workflow, bool) {
	for _, w := range c.workflows {
		if w.ID == id {
			return w, true
		}
	}
	return domain.Workflow{}, false
}
func (c *stubCatalog) WorkflowCategories() []domain.WorkflowCategory      { return c.categories }
func (c *stubCatalog) Tiers() []domain.TierInfo                           { return c.tiers }
func (c *stubCatalog) WorkflowSection(id string) ([]byte, error) {
	if c.sections != nil {
		if b, ok := c.sections[id]; ok {
			return b, nil
		}
	}
	return []byte("[[SECTION:Workflow:" + id + "]]\n[[/SECTION:Workflow:" + id + "]]"), nil
}
func (c *stubCatalog) ReadSource(path string) ([]byte, error) {
	if c.sources != nil {
		if b, ok := c.sources[path]; ok {
			return b, nil
		}
	}
	return []byte("stub source"), nil
}
func (c *stubCatalog) Issues() []catalog.Issue { return nil }

// ---------------------------------------------------------------------------
// stubHarnessModule — satisfies domain.HarnessModule with a fixed descriptor
// ---------------------------------------------------------------------------

type stubHarnessModule struct {
	ref        domain.HarnessRef
	descriptor domain.HarnessDescriptor
}

func (m *stubHarnessModule) Ref() domain.HarnessRef                       { return m.ref }
func (m *stubHarnessModule) Descriptor() *domain.HarnessDescriptor        { return &m.descriptor }
func (m *stubHarnessModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *stubHarnessModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *stubHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *stubHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *stubHarnessModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *stubHarnessModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// stubRegistry — satisfies registry.Registry with configurable harnesses
// ---------------------------------------------------------------------------

type stubRegistry struct {
	list    []domain.HarnessRef
	modules map[string]domain.HarnessModule
}

func (r *stubRegistry) List() []domain.HarnessRef { return r.list }
func (r *stubRegistry) Resolve(id string) (domain.HarnessModule, error) {
	if r.modules != nil {
		if m, ok := r.modules[id]; ok {
			return m, nil
		}
	}
	return nil, errors.New("harness not found: " + id)
}

// ---------------------------------------------------------------------------
// stubPlanner — satisfies plan.Planner with a configurable plan
// ---------------------------------------------------------------------------

type stubPlanner struct {
	plan domain.Plan
	err  error
}

func (p *stubPlanner) Build(ctx context.Context, in plan.Input) (domain.Plan, error) {
	return p.plan, p.err
}

// ---------------------------------------------------------------------------
// stubExecutor — satisfies deploy.Executor with a configurable result
// ---------------------------------------------------------------------------

type stubExecutor struct {
	result deploy.ExecResult
	err    error
}

func (e *stubExecutor) Execute(ctx context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	return e.result, e.err
}

// ---------------------------------------------------------------------------
// stubManifestStore — satisfies manifest.Store with a configurable snapshot
// ---------------------------------------------------------------------------

type stubManifestStore struct {
	snap    manifest.Snapshot
	saveErr error
}

func (s *stubManifestStore) Load(workspaceRoot string) (manifest.Snapshot, error) {
	return s.snap, nil
}
func (s *stubManifestStore) Save(workspaceRoot string, m domain.Manifest) error {
	return s.saveErr
}

// ---------------------------------------------------------------------------
// stubToolConfig — satisfies config.ToolConfigStore
// ---------------------------------------------------------------------------

type stubToolConfig struct {
	cfg  config.ToolConfig
	path string
}

func (s *stubToolConfig) Load() (config.ToolConfig, error) { return s.cfg, nil }
func (s *stubToolConfig) Path() string                     { return s.path }

// ---------------------------------------------------------------------------
// spyUserConfig — satisfies config.UserConfigStore and records Save calls
// ---------------------------------------------------------------------------

type spyUserConfig struct {
	cfg   config.UserConfig
	saved []config.UserConfig
	path  string
}

func (s *spyUserConfig) Load() (config.UserConfig, error) { return s.cfg, nil }
func (s *spyUserConfig) Save(cfg config.UserConfig) error {
	s.saved = append(s.saved, cfg)
	s.cfg = cfg
	return nil
}
func (s *spyUserConfig) Path() string { return s.path }

// ---------------------------------------------------------------------------
// stubLogger — satisfies logging.Logger without recording
// ---------------------------------------------------------------------------

type stubLogger struct{}

func (l *stubLogger) StartRun(rc logging.RunContext)  {}
func (l *stubLogger) Event(e logging.Event)           {}
func (l *stubLogger) Action(ar domain.ActionRecord)   {}
func (l *stubLogger) EndRun(outcome domain.Outcome)   {}
func (l *stubLogger) Paths() domain.LogPaths          { return domain.LogPaths{} }
func (l *stubLogger) Degraded() []error               { return nil }

// ---------------------------------------------------------------------------
// spyTodo — satisfies todo.Collector and records items and gaps
// ---------------------------------------------------------------------------

type spyTodo struct {
	items []domain.TodoItem
	gaps  []domain.Gap
}

func (c *spyTodo) Add(item domain.TodoItem)  { c.items = append(c.items, item) }
func (c *spyTodo) AddGap(g domain.Gap)       { c.gaps = append(c.gaps, g) }
func (c *spyTodo) Items() []domain.TodoItem  { return c.items }
func (c *spyTodo) Groups() []todo.Group      { return nil }
func (c *spyTodo) Empty() bool               { return len(c.items) == 0 && len(c.gaps) == 0 }

// hasGapKind reports whether any recorded gap has the given kind.
func (c *spyTodo) hasGapKind(kind domain.GapKind) bool {
	for _, g := range c.gaps {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

// hasTodoCategory reports whether any recorded item has the given category.
func (c *spyTodo) hasTodoCategory(cat domain.TodoCategory) bool {
	for _, item := range c.items {
		if item.Category == cat {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// minimalCatalog — a pre-wired stubCatalog with one harness, workflow, agent, skill
// ---------------------------------------------------------------------------

// minimalAgent is a single worker agent used in stub test scenarios.
var minimalAgent = domain.Agent{
	Key:             "test-runner",
	NumericID:       "1",
	Version:         "1.0",
	Name:            "Test Runner",
	Description:     "Runs tests",
	Role:            domain.RoleWorker,
	Category:        "Execution",
	RecommendedTier: "HIGH",
	TierRationale:   "Needs a capable model to analyse test output",
	Tools:           []string{"bash"},
}

// minimalOrchestrator is a stub orchestrator agent.
var minimalOrchestrator = domain.Agent{
	Key:         "orchestrator",
	NumericID:   "0",
	Version:     "1.0",
	Name:        "Orchestrator",
	Description: "Orchestrates other agents",
	Role:        domain.RoleOrchestrator,
}

// minimalWorkflow is a stub workflow for test scenarios.
var minimalWorkflow = domain.Workflow{
	ID:               "quick-fix",
	Name:             "Quick Fix",
	Description:      "Fix a quick issue",
	Hint:             "Use for minor bugs",
	Version:          "1.0",
	Category:         "Build",
	ReferencedAgents: []string{"test-runner"},
}

// minimalHarness is a stub harness ref for test scenarios.
var minimalHarness = domain.HarnessRef{
	ID:          "stub-harness",
	DisplayName: "Stub Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// newMinimalCatalog returns a stubCatalog configured with one agent, workflow, and tier.
func newMinimalCatalog() *stubCatalog {
	return &stubCatalog{
		root:         "testroot",
		agents:       []domain.Agent{minimalAgent},
		orchestrator: minimalOrchestrator,
		skills:       []domain.Skill{},
		hooks:        []domain.HookBundle{},
		workflows:    []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "Needs a capable model to analyse test output", AgentKeys: []string{"test-runner"}},
		},
	}
}

// newMinimalModule returns a stubHarnessModule configured with the minimal harness identity.
func newMinimalModule() *stubHarnessModule {
	return &stubHarnessModule{
		ref: minimalHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "stub-harness",
			DisplayName: "Stub Harness",
			Models:      domain.ModelCatalog{IDs: []string{"model-a", "model-b"}},
		},
	}
}

// newMinimalRegistry returns a stubRegistry with one harness backed by a stubHarnessModule.
func newMinimalRegistry(mod *stubHarnessModule) *stubRegistry {
	return &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": mod},
	}
}

// newMinimalPlan returns a domain.Plan for the minimal catalog scenario.
func newMinimalPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployNew,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
				Model:      domain.ModelSelection{ModelID: "model-a", Origin: domain.OriginTierMapping},
			},
		},
		Workflows: []string{"quick-fix"},
	}
}

// newMinimalExecResult returns a minimal ExecResult for a successful deployment.
func newMinimalExecResult(workspace string) deploy.ExecResult {
	return deploy.ExecResult{
		DeploymentRoot: workspace,
		Fallback:       domain.FallbackNone,
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: workspace + "/test-runner.md",
				Taken:      domain.TakenCreated,
			},
		},
	}
}

// newBaseDeps returns an app.Deps wired with all minimal stubs and a scripted interaction.
// The workspace is created in t.TempDir(). Callers may replace individual fields before
// passing to app.New().
func newBaseDeps(t *testing.T, stub *interactiontest.Stub) (app.Deps, string) {
	t.Helper()
	workspace := t.TempDir()
	mod := newMinimalModule()
	return app.Deps{
		Catalog:     newMinimalCatalog(),
		Registry:    newMinimalRegistry(mod),
		Planner:     &stubPlanner{plan: newMinimalPlan(workspace)},
		Executor:    &stubExecutor{result: newMinimalExecResult(workspace)},
		Manifest:    &stubManifestStore{snap: manifest.Snapshot{State: manifest.StateAbsent}},
		ToolConfig:  &stubToolConfig{cfg: config.DefaultToolConfig()},
		UserConfig:  &spyUserConfig{},
		Logger:      &stubLogger{},
		Todo:        &spyTodo{},
		Interaction: stub,
		MosaicRoot:  t.TempDir(),
		GOOS:        "linux",
		Now:         func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) },
	}, workspace
}

// newDeployRequest returns a minimal DeployRequest with the given workspace and harness,
// pre-answering the question sequence to drive the flow headlessly without interaction.
func newDeployRequest(workspace, harnessID string, workflowIDs []string) app.DeployRequest {
	return app.DeployRequest{
		HarnessID:     harnessID,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		WorkflowIDs:   workflowIDs,
		AutoConfirmPlan: true,
	}
}

// writeTempFile writes content to a file inside the directory dir and returns its path.
func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := dir + "/" + name
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return p
}
