package app_test

// id_based_probe_service_test.go verifies that the deploy and update service flows use the
// id-based agent index to locate deployed files by frontmatter id, enabling renamed agents
// to be matched correctly. These are service-level tests that drive the public API rather
// than exercising the internal building blocks in isolation.
//
// Verified behaviors:
//
//   - Update flow: a deployed file whose base name no longer matches the catalog agent's key
//     is still found by its frontmatter id; the planner receives Present: true for the
//     agent's planned path.
//
//   - Update flow: when no deployed file carries the catalog agent's numeric id, the planner
//     receives Present: false for that agent's planned path — even when a file exists at the
//     harness-computed name-derived path with a different id.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// idAwareHarnessModule — stub harness with a declared agents directory
// ---------------------------------------------------------------------------

// idAwareHarnessModule is a test double for domain.HarnessModule that declares an agents
// directory in its PathSpec. This causes the service flows to activate id-based agent
// resolution and scan the declared directory for deployed files.
type idAwareHarnessModule struct {
	agentsSubdir string
	ref          domain.HarnessRef
	descriptor   domain.HarnessDescriptor
}

// newIDAwareHarnessModule returns a stub harness module that declares agentsSubdir as the
// deployed-agents location. TargetPath maps agent artifacts to agentsSubdir/<key>.md.
func newIDAwareHarnessModule(agentsSubdir string) *idAwareHarnessModule {
	return &idAwareHarnessModule{
		agentsSubdir: agentsSubdir,
		ref:          minimalHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "stub-harness",
			DisplayName: "Stub Harness",
			Models:      domain.ModelCatalog{IDs: []string{"model-a", "model-b"}},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   agentsSubdir,
				},
			},
		},
	}
}

func (m *idAwareHarnessModule) Ref() domain.HarnessRef               { return m.ref }
func (m *idAwareHarnessModule) Descriptor() *domain.HarnessDescriptor { return &m.descriptor }
func (m *idAwareHarnessModule) Close() error                           { return nil }

func (m *idAwareHarnessModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}

func (m *idAwareHarnessModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}

func (m *idAwareHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Kind == domain.ArtifactAgent {
		return filepath.Join(m.agentsSubdir, req.Key+".md"), nil
	}
	return req.Key + ".md", nil
}

func (m *idAwareHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }

func (m *idAwareHarnessModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeAgentFileWithID writes a minimal deployed agent file at dir/filename with the given
// frontmatter id. The file is valid for docformat.Parse.
func writeAgentFileWithID(t *testing.T, dir, filename, numericID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeAgentFileWithID: MkdirAll(%q): %v", dir, err)
	}
	content := "---\nid: \"" + numericID + "\"\nversion: \"1.0.0\"\n---\nDeployed agent content.\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writeAgentFileWithID: WriteFile: %v", err)
	}
}

// newIDAwareRegistry returns a registry backed by a single idAwareHarnessModule.
func newIDAwareRegistry(mod *idAwareHarnessModule) *stubRegistry {
	return &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": mod},
	}
}

// ---------------------------------------------------------------------------
// T8.6 service-level tests
// ---------------------------------------------------------------------------

// TestUpdate_RenamedAgent_FoundByIDViaServiceFlow verifies that the update service flow
// locates a deployed agent by its numeric frontmatter id even when the deployed file's
// base name no longer matches the catalog agent's key. The planner must receive
// Present: true for the agent's planned path, confirming the id-based probe wiring is
// active in the service layer.
func TestUpdate_RenamedAgent_FoundByIDViaServiceFlow(t *testing.T) {
	ws := t.TempDir()
	agentsSubdir := ".mosaic"
	agentsDir := filepath.Join(ws, agentsSubdir)
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll agents dir: %v", err)
	}

	// Write the orchestrator in production managed form with a quick-fix workflow section
	// so that the update flow discovers the workflow and includes test-runner in the plan.
	orchContent := []byte("---\nversion: \"1.0.0\"\n---\n" +
		"<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	if err := os.WriteFile(filepath.Join(agentsDir, "orchestrator.md"), orchContent, 0o644); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}

	// Write the agent file under a renamed base name ("old-test-runner.md") but with the
	// catalog agent's numeric id ("1") in frontmatter. The harness-computed planned path
	// for "test-runner" is ".mosaic/test-runner.md" — which does not exist.
	writeAgentFileWithID(t, agentsDir, "old-test-runner.md", "1")

	// Spy planner captures the plan.Input so we can inspect DeployedState.
	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
	}}

	mod := newIDAwareHarnessModule(agentsSubdir)
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Registry = newIDAwareRegistry(mod)
	deps.Planner = spy
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)
	if captured.DeployedState == nil {
		t.Fatal("plan.Input.DeployedState is nil; the update flow must populate it before calling Build")
	}

	// The planned path is ".mosaic/test-runner.md" (harness-computed).
	// The actual deployed file is ".mosaic/old-test-runner.md" (renamed).
	// Id-based resolution must find it and report Present: true at the planned path.
	plannedPath := filepath.Join(agentsSubdir, "test-runner.md")
	state, ok := captured.DeployedState[plannedPath]
	if !ok {
		t.Fatalf("DeployedState does not contain planned path %q; "+
			"the probe must populate DeployedState for every planned agent path", plannedPath)
	}
	if !state.Present {
		t.Errorf("DeployedState[%q].Present = false; "+
			"the renamed agent at old-test-runner.md (id \"1\") must be found by the id index "+
			"and reported as present at the planned path — id-based resolution is not wired into "+
			"the update service flow", plannedPath)
	}
}

// TestUpdate_AgentWithID_NotFoundByID_ReportsNotPresent verifies that when no deployed file
// carries the catalog agent's numeric id, the update service flow reports Present: false for
// that agent's planned path — even when a file exists at the harness-computed name-derived
// path with a different id. This confirms that the no-name-fallback rule is enforced in the
// service layer: the planner sees the agent as not deployed rather than matching it by name.
func TestUpdate_AgentWithID_NotFoundByID_ReportsNotPresent(t *testing.T) {
	ws := t.TempDir()
	agentsSubdir := ".mosaic"
	agentsDir := filepath.Join(ws, agentsSubdir)
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll agents dir: %v", err)
	}

	// Write the orchestrator in production managed form with a quick-fix workflow section so test-runner enters the plan.
	orchContent := []byte("---\nversion: \"1.0.0\"\n---\n" +
		"<Identity type=\"core\">\n" +
		"<AvailableWorkflows type=\"managed\">\n" +
		"<Workflow type=\"managed\" name=\"quick-fix\" version=\"1.0\">\n" +
		"Quick fix content.\n" +
		"</Workflow>\n" +
		"</AvailableWorkflows>\n" +
		"</Identity>\n")
	if err := os.WriteFile(filepath.Join(agentsDir, "orchestrator.md"), orchContent, 0o644); err != nil {
		t.Fatalf("write orchestrator: %v", err)
	}

	// Write a file at the harness-computed name (".mosaic/test-runner.md") but with a
	// different id ("999", not the catalog agent's id "1"). This simulates a name collision
	// where a name-based lookup would incorrectly find this file.
	writeAgentFileWithID(t, agentsDir, "test-runner.md", "999")

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
	}}

	mod := newIDAwareHarnessModule(agentsSubdir)
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Registry = newIDAwareRegistry(mod)
	deps.Planner = spy
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)
	if captured.DeployedState == nil {
		t.Fatal("plan.Input.DeployedState is nil; the update flow must populate it before calling Build")
	}

	// The file at ".mosaic/test-runner.md" has id "999", which does not match the catalog
	// agent "test-runner"'s id "1". Id-based resolution must report Present: false.
	// A name-based fallback would incorrectly return Present: true.
	plannedPath := filepath.Join(agentsSubdir, "test-runner.md")
	state, ok := captured.DeployedState[plannedPath]
	if !ok {
		t.Fatalf("DeployedState does not contain planned path %q", plannedPath)
	}
	if state.Present {
		t.Errorf("DeployedState[%q].Present = true; "+
			"the file test-runner.md has id \"999\" which does not match the catalog agent's id \"1\"; "+
			"id-based resolution must not fall back to name-based matching when the id does not match — "+
			"the file must be treated as not belonging to this agent", plannedPath)
	}
}
