package app_test

// update_hooks_helpers_test.go holds the shared builders for the Update-flow hook refresh
// tests: tool-local hook bundle fixtures (testdata/hooks), a stub harness module whose hook
// plan points at those fixtures, spies for the planner input, the reviewed plan and the
// executor request, and a runner that drives Service.Update with the real planner and the
// real executor against a temporary workspace. Nothing here reads Catalog/.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

const (
	uhHooksDir       = ".claude/hooks"
	uhHookKey        = "mosaic-logger"
	uhHookTarget     = uhHooksDir + "/" + uhHookKey
	uhRegTarget      = ".claude/settings.json"
	uhRegStepID      = "claude-settings-hook"
	uhStaleVersion   = "1.3.0"
	uhCatalogVersion = "1.5.1"
	uhHarnessID      = "stub-harness"
)

// uhLayout names a fixture variant directory under testdata/hooks and the target names its
// files are deployed as (relative to the hooks directory).
type uhLayout struct {
	dir   string
	files []string
}

var (
	uhFlat    = uhLayout{dir: "flat", files: []string{"logger.sh", "util.sh"}}
	uhFlatAlt = uhLayout{dir: "flat-alt", files: []string{"logger.sh", "alt-only.sh"}}
	uhNested  = uhLayout{dir: "nested", files: []string{"logger.ts", "lib/util.ts"}}
)

// uhFixture returns the catalog content of one variant file.
func uhFixture(t *testing.T, l uhLayout, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hooks", l.dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read hook fixture %s/%s: %v", l.dir, name, err)
	}
	return string(data)
}

// uhFragment returns the registration fragment of the fixture bundle.
func uhFragment(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hooks", "registration-fragment.json"))
	if err != nil {
		t.Fatalf("read registration fragment fixture: %v", err)
	}
	return string(data)
}

// newTestHookPlan returns the supported HookPlan a harness deploying layout l would return:
// absolute fixture sources, the shared hooks directory and one performable registration step.
func newTestHookPlan(t *testing.T, l uhLayout) domain.HookPlan {
	t.Helper()
	hp := domain.HookPlan{
		Supported: true,
		TargetDir: uhHooksDir,
		Registration: []domain.RegistrationStep{{
			ID: uhRegStepID, TargetPath: uhRegTarget, Fragment: uhFragment(t),
			Performable: true, Instruction: "register the logger hook",
		}},
	}
	for _, name := range l.files {
		abs, err := filepath.Abs(filepath.Join("testdata", "hooks", l.dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("abs fixture path: %v", err)
		}
		hp.Files = append(hp.Files, domain.HookFile{SourcePath: abs, TargetName: name})
	}
	return hp
}

// uhModule is a harness module whose hook bundles plan to <hooks dir>/<key> and whose
// HookPlan is fixed.
type uhModule struct {
	*stubHarnessModule
	hp domain.HookPlan
}

func (m uhModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Kind == domain.ArtifactHook {
		return uhHooksDir + "/" + req.Key, nil
	}
	return m.stubHarnessModule.TargetPath(req)
}

func (m uhModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) { return m.hp, nil }

// newTestHookModule returns a harness module with the given id (agents directory declared so
// Update's workspace scan has a place to look) deploying hp.
func newTestHookModule(id string, hp domain.HookPlan) uhModule {
	base := newModuleWithAgentsDir("agents")
	base.ref = domain.HarnessRef{ID: id, DisplayName: id, Tier: domain.TierBuiltin, Usable: true}
	base.descriptor.ID = id
	base.descriptor.DisplayName = id
	return uhModule{stubHarnessModule: base, hp: hp}
}

// uhPlannerSpy records the plan.Input and delegates to the real planner. The production flows
// leave plan.Input.WorkspaceFileExists unset, so the planner never emits plan-time registration
// gaps; the spy supplies a workspace-backed implementation so those gaps (and the filtering of
// them) are observable in the reviewed plan.
type uhPlannerSpy struct {
	inner     plan.Planner
	workspace string
	in        plan.Input
}

func (p *uhPlannerSpy) Build(ctx context.Context, in plan.Input) (domain.Plan, error) {
	in.WorkspaceFileExists = func(rel string) bool {
		_, err := os.Stat(filepath.Join(p.workspace, filepath.FromSlash(rel)))
		return err == nil
	}
	p.in = in
	return p.inner.Build(ctx, in)
}

// uhReviewSpy records the plan handed to the plan review.
type uhReviewSpy struct {
	*interactiontest.Stub
	reviewed *domain.Plan
}

func (r *uhReviewSpy) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	cp := p
	r.reviewed = &cp
	return r.Stub.Review(ctx, p)
}

// uhExecSpy records the executor request and result and delegates to the real executor.
type uhExecSpy struct {
	inner deploy.Executor
	req   deploy.ExecRequest
	res   deploy.ExecResult
}

func (e *uhExecSpy) Execute(ctx context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.req = req
	res, err := e.inner.Execute(ctx, req)
	e.res = res
	return res, err
}

// uhConfig describes one Update run over the fixture hook bundle.
type uhConfig struct {
	// module is the harness the run targets; default is a flat-layout harness.
	module domain.HarnessModule
	// catalogVersion is the catalog version of the bundle; default uhCatalogVersion.
	catalogVersion string
	// recorded is the manifest-recorded hook version; empty records no hook entry.
	recorded string
	// files maps workspace-relative path to the bytes present before the run.
	files map[string]string
	// conflict pre-answers conflicts; empty leaves the question unanswered.
	conflict domain.ConflictDecision
}

// uhRun is the observable outcome of runUpdateHooks.
type uhRun struct {
	workspace string
	todo      *spyTodo
	planIn    plan.Input
	reviewed  *domain.Plan
	exec      *uhExecSpy
	err       error
}

// runUpdateHooks seeds a temp workspace from cfg and runs Service.Update with the real planner
// and executor, spying on the planner input, the reviewed plan and the executor request.
func runUpdateHooks(t *testing.T, cfg uhConfig) *uhRun {
	t.Helper()
	if cfg.module == nil {
		cfg.module = newTestHookModule(uhHarnessID, newTestHookPlan(t, uhFlat))
	}
	if cfg.catalogVersion == "" {
		cfg.catalogVersion = uhCatalogVersion
	}
	harnessID := cfg.module.Ref().ID
	review := &uhReviewSpy{Stub: interactiontest.NewBuilder().AnswerReview(true).Build()}
	deps, workspace := newBaseDeps(t, review.Stub)
	deps.Interaction = review
	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{{Key: uhHookKey, Version: cfg.catalogVersion}}
	deps.Catalog = cat
	spyPlanner := &uhPlannerSpy{inner: plan.New(), workspace: workspace}
	deps.Planner = spyPlanner
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{cfg.module.Ref()},
		modules: map[string]domain.HarnessModule{harnessID: cfg.module},
	}
	if err := os.MkdirAll(filepath.Join(workspace, "agents"), 0o755); err != nil {
		t.Fatalf("mkdir agents dir: %v", err)
	}
	uhSeedFiles(t, workspace, cfg.files)

	m := domain.Manifest{SchemaVersion: manifest.SchemaVersion, HarnessID: harnessID}
	if cfg.recorded != "" {
		m.Entries = []domain.ManifestEntry{{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: uhHookKey},
			TargetPath: uhHookTarget, Version: cfg.recorded, ContentHash: manifest.Hash(nil),
		}}
	}
	deps.Manifest = &stubManifestStore{snap: manifest.Snapshot{State: manifest.StatePresent, Manifest: m}}
	spyExec := &uhExecSpy{inner: deploy.NewExecutor(deps.Manifest, deps.Logger, deps.Todo)}
	deps.Executor = spyExec

	_, err := app.New(deps).Update(context.Background(), app.UpdateRequest{
		HarnessID:       harnessID,
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		ConflictDefault: cfg.conflict,
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true, domain.QAgentModel: true},
	})
	return &uhRun{
		workspace: workspace, todo: deps.Todo.(*spyTodo), planIn: spyPlanner.in,
		reviewed: review.reviewed, exec: spyExec, err: err,
	}
}

// uhSeedFiles writes files (workspace-relative, slash-separated) into workspace.
func uhSeedFiles(t *testing.T, workspace string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(workspace, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", rel, err)
		}
	}
}

// requireUpdateHooksOK fails the test when the run returned an error.
func requireUpdateHooksOK(t *testing.T, run *uhRun) {
	t.Helper()
	if run.err != nil {
		t.Fatalf("Update returned error: %v", run.err)
	}
}

// read returns the workspace file's content, or "" and false when absent.
func (r *uhRun) read(rel string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(r.workspace, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// hookItem returns the hook plan item of the plan the executor received.
func (r *uhRun) hookItem() (domain.PlanItem, bool) {
	for _, item := range r.exec.req.Plan.Items {
		if item.Ref.Kind == domain.ArtifactHook && item.Ref.Key == uhHookKey {
			return item, true
		}
	}
	return domain.PlanItem{}, false
}

// hookAction returns what the executor recorded for the hook bundle.
func (r *uhRun) hookAction() (domain.ActionRecord, bool) {
	for _, a := range r.exec.res.Actions {
		if a.Ref.Kind == domain.ArtifactHook && a.Ref.Key == uhHookKey {
			return a, true
		}
	}
	return domain.ActionRecord{}, false
}

// hookEntry returns the manifest entry the run wrote for the hook bundle.
func (r *uhRun) hookEntry() (domain.ManifestEntry, bool) {
	for _, e := range r.exec.res.Manifest.Entries {
		if e.Ref.Kind == domain.ArtifactHook && e.Ref.Key == uhHookKey {
			return e, true
		}
	}
	return domain.ManifestEntry{}, false
}

// uhHasRegistrationGap reports whether gaps carry a hook-registration gap for the fixture step.
func uhHasRegistrationGap(gaps []domain.Gap) bool {
	for _, g := range gaps {
		if g.Kind == domain.GapHookRegistration && g.Subject == uhRegStepID {
			return true
		}
	}
	return false
}

// uhHasRegistrationStep reports whether steps contain the fixture registration step.
func uhHasRegistrationStep(steps []domain.RegistrationStep) bool {
	for _, s := range steps {
		if s.ID == uhRegStepID {
			return true
		}
	}
	return false
}

// uhEditedFiles returns, for layout l, a map of workspace-relative path to locally edited
// content for every variant file named in only (all files when only is empty).
func uhEditedFiles(l uhLayout, only ...string) map[string]string {
	want := map[string]bool{}
	for _, n := range only {
		want[n] = true
	}
	files := map[string]string{}
	for _, name := range l.files {
		if len(want) == 0 || want[name] {
			files[uhHooksDir+"/"+name] = "LOCALLY EDITED " + name + "\n"
		}
	}
	return files
}
