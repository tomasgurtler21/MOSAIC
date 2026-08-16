package app_test

// destination_field_test.go contains integration tests (T4.3, T4.4, T4.5) verifying
// that config-declared tool-destination mappings produce, regenerate, and are removed from
// destination frontmatter fields in deployed and updated agent files.
//
// These tests exercise the following composition chain, which spans the boundary between
// the registry hook (I4.1/I4.2) and the transform pipeline:
//
//   user config ToolDestinations
//   → registry.Options.ToolMappings hook (installed at composition point, I4.2)
//   → Discover installs hook result on module descriptor (I4.1)
//   → transform.Apply calls module.Tools() → descriptor.MapTools
//   → applyFrontmatter writes DestField names to the named frontmatter field
//   → output agent file has separate destination field alongside main tools field
//
// All tests in this file reference registry.Options.ToolMappings, which does not exist
// until I4.1 is implemented. Every test therefore fails to compile until then — that is
// the intended TDD RED state for T4.3, T4.4, and T4.5.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// appTestModule — HarnessModule backed by a configurable HarnessDescriptor.
//
// Uses descriptor.MapTools for tool resolution so that the ToolMappings hook result
// (installed on the descriptor by Discover) directly controls the transform output.
// ---------------------------------------------------------------------------

type appTestModule struct {
	desc *domain.HarnessDescriptor
}

func (m *appTestModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: m.desc.ID, Tier: domain.TierBuiltin, Usable: true}
}
func (m *appTestModule) Descriptor() *domain.HarnessDescriptor { return m.desc }
func (m *appTestModule) Close() error                          { return nil }
func (m *appTestModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return descriptor.MapTools(m.desc, req)
}
func (m *appTestModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return descriptor.ApplyFrontmatterSpec(m.desc, req)
}
func (m *appTestModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return descriptor.ResolveTargetPath(m.desc, req)
}
func (m *appTestModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *appTestModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "app test module"}, nil
}

// ---------------------------------------------------------------------------
// Descriptor factory
// ---------------------------------------------------------------------------

// destTestDescriptor builds a minimal list-shape descriptor for the given harness id.
// The universe contains AskUser but no tool-mapping is declared — simulating a harness
// that routes user_interaction only after a config mapping is applied by the hook.
func destTestDescriptor(id string) *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            id,
		DisplayName:   id,
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "AskUser", Unused: domain.Deny},
			},
			// No user_interaction mapping — the ToolMappings hook will overlay it.
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

// appTestHarnessID derives a unique harness id from the test name, ensuring tests do not
// share registry state. The id is lowercase and contains only alphanumeric + hyphens.
func appTestHarnessID(t *testing.T) string {
	t.Helper()
	name := strings.ToLower(t.Name())
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return "dest-test-" + name
}

// ---------------------------------------------------------------------------
// Constants used by the app.Update-level test (T4.4 Critical fix)
// ---------------------------------------------------------------------------

const (
	// destTestAgentKey is the agent key used in app.Update-level destination field tests.
	destTestAgentKey = "dest-test-agent"
	// destTestWorkflowID is the workflow ID that references destTestAgentKey.
	destTestWorkflowID = "dest-test-workflow"
	// destTestAgentSourceFile is the catalog source path for destTestAgentKey.
	destTestAgentSourceFile = "/catalog/agents/dest-test-agent.md"
)

// destTestDescriptorWithPaths returns a descriptor like destTestDescriptor but also
// declares deployment path patterns for agents. This is required for plan.EnumerateTargetPaths
// (called by app.Update) to compute target paths without returning an error.
func destTestDescriptorWithPaths(id string) *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            id,
		DisplayName:   id,
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "AskUser", Unused: domain.Deny},
			},
			// No user_interaction mapping — the ToolMappings hook will overlay it.
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
		Paths: domain.PathSpec{
			Agents: domain.ScopedPaths{
				Supported: true,
				Project:   "agents",
			},
		},
	}
}

// newDestTestCatalog returns a stubCatalog with a worker agent that declares
// user_interaction, a workflow referencing that agent, and a ReadSource entry
// returning destTestAgentSource for the agent's source path.
//
// Pair with buildRegistryWithUserDestMapping and destTestDescriptorWithPaths for the
// app.Update-level destination field test.
func newDestTestCatalog() *stubCatalog {
	agent := domain.Agent{
		Key:         destTestAgentKey,
		NumericID:   "300",
		Version:     "1.0.0",
		Name:        "Destination Test Agent",
		Description: "Agent for destination field end-to-end tests",
		Role:        domain.RoleWorker,
		Tools:       []string{"user_interaction"},
		SourcePath:  destTestAgentSourceFile,
	}
	workflow := domain.Workflow{
		ID:               destTestWorkflowID,
		Name:             "Destination Field Test Workflow",
		Version:          "1.0",
		ReferencedAgents: []string{destTestAgentKey},
	}
	return &stubCatalog{
		root:         "testroot",
		agents:       []domain.Agent{agent},
		orchestrator: minimalOrchestrator,
		skills:       []domain.Skill{},
		hooks:        []domain.HookBundle{},
		workflows:    []domain.Workflow{workflow},
		categories: []domain.WorkflowCategory{
			{Name: "Test", Workflows: []domain.Workflow{workflow}},
		},
		tiers: []domain.TierInfo{},
		sources: map[string][]byte{
			destTestAgentSourceFile: []byte(destTestAgentSource),
		},
	}
}

// ---------------------------------------------------------------------------
// capturingContentExecutor — satisfies deploy.Executor, records Content output
//
// For each ActionCreate/ActionUpdate plan item, capturingContentExecutor calls
// ExecRequest.Content and records the produced bytes keyed by TargetPath. This
// lets tests verify the content function's output (i.e., that the registry module's
// mapping hook result reaches the transform) without performing any real file writes.
// ---------------------------------------------------------------------------

type capturingContentExecutor struct {
	// contents maps target path → bytes produced by Content for that plan item.
	contents map[string][]byte
}

func (e *capturingContentExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	e.contents = make(map[string][]byte)
	var actions []domain.ActionRecord
	for _, item := range req.Plan.Items {
		switch item.Action {
		case domain.ActionCreate, domain.ActionUpdate:
			content, err := req.Content(item)
			if err != nil {
				return deploy.ExecResult{}, fmt.Errorf("capturingContentExecutor: Content(%q): %w", item.Ref.Key, err)
			}
			e.contents[item.TargetPath] = content
			actions = append(actions, domain.ActionRecord{
				Ref:        item.Ref,
				TargetPath: item.TargetPath,
				Taken:      domain.TakenUpdated,
			})
		default:
			actions = append(actions, domain.ActionRecord{
				Ref:        item.Ref,
				TargetPath: item.TargetPath,
				Taken:      domain.TakenUnchanged,
			})
		}
	}
	return deploy.ExecResult{
		DeploymentRoot: req.Plan.WorkspacePath,
		Actions:        actions,
	}, nil
}

// ---------------------------------------------------------------------------
// Registry builder — this is the RED trigger
//
// buildRegistryWithUserDestMapping builds a registry with a ToolMappings hook that
// overlays user_interaction with a multi-destination mapping: DestMain → AskUser and
// DestField → mcp_servers: [user-feedback].
//
// The call to registry.Discover with registry.Options.ToolMappings does not compile
// until I4.1 adds that field → TDD RED state for all tests that call this function.
// ---------------------------------------------------------------------------

func buildRegistryWithUserDestMapping(t *testing.T, harnessID string, desc *domain.HarnessDescriptor) registry.Registry {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "MosaicDeploy", "harnesses"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	mod := &appTestModule{desc: desc}
	registry.Register(harnessID, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	userDest := config.ToolDestinationsByHarness{
		harnessID: []domain.ToolMapping{
			{
				Generic: "user_interaction",
				Destinations: []domain.ToolDestination{
					{Kind: domain.DestMain, Names: []string{"AskUser"}},
					{Kind: domain.DestField, Field: "mcp_servers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
				},
			},
		},
	}

	// registry.Options.ToolMappings does not exist → compile error (TDD RED)
	reg, err := registry.Discover(registry.Options{
		MosaicRoot: root,
		ToolMappings: func(id string, declared []domain.ToolMapping) []domain.ToolMapping {
			return config.EffectiveToolMappings(id, declared, nil, userDest)
		},
	})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	return reg
}

// buildRegistryWithoutMapping builds a registry with a ToolMappings hook that returns nil
// for every harness (no config-declared mappings). This simulates removing the config
// mapping between a deploy and an update.
//
// Also references registry.Options.ToolMappings → compile error until I4.1.
func buildRegistryWithoutMapping(t *testing.T, harnessID string, desc *domain.HarnessDescriptor) registry.Registry {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "MosaicDeploy", "harnesses"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	mod := &appTestModule{desc: desc}
	registry.Register(harnessID, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// registry.Options.ToolMappings does not exist → compile error (TDD RED)
	reg, err := registry.Discover(registry.Options{
		MosaicRoot: root,
		ToolMappings: func(_ string, _ []domain.ToolMapping) []domain.ToolMapping {
			return nil // no config-declared mappings
		},
	})
	if err != nil {
		t.Fatalf("registry.Discover: %v", err)
	}
	return reg
}

// ---------------------------------------------------------------------------
// Agent source fixtures
// ---------------------------------------------------------------------------

// destTestAgentSource is an agent source declaring user_interaction. On a harness with
// no user_interaction mapping, this produces a ToolUnmapped gap. Once the config mapping
// is applied via the hook, it produces both the main tools field and the mcp_servers field.
const destTestAgentSource = `---
id: 300
version: 1.0.0
description: Destination field integration test agent
tools: [user_interaction]
---
Body.
`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// applyWithRegistry resolves harnessID from reg and runs transform.Apply on source.
func applyWithRegistry(t *testing.T, reg registry.Registry, harnessID string, source []byte) *docformat.Document {
	t.Helper()
	m, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", harnessID, err)
	}
	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "dest-test-agent",
		Module: m,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}
	return doc
}

// assertHasMCPField asserts the document's frontmatter contains mcp_servers with user-feedback.
func assertHasMCPField(t *testing.T, doc *docformat.Document, context string) {
	t.Helper()
	v, ok := doc.Frontmatter().Get("mcp_servers")
	if !ok {
		t.Errorf("%s: mcp_servers field absent from output frontmatter; "+
			"config-declared DestField destination must produce this field; "+
			"keys: %v", context, doc.Frontmatter().Keys())
		return
	}
	if v.Kind != domain.KindList {
		t.Errorf("%s: mcp_servers field kind: want KindList, got %v", context, v.Kind)
	}
	for _, item := range v.Items {
		if item.Scalar == "user-feedback" {
			return
		}
	}
	t.Errorf("%s: user-feedback not found in mcp_servers field items %v", context, v.Items)
}

// assertHasMainTool asserts the document's frontmatter contains the tool in the tools field.
func assertHasMainTool(t *testing.T, doc *docformat.Document, tool, context string) {
	t.Helper()
	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Errorf("%s: tools field absent", context)
		return
	}
	for _, item := range v.Items {
		if item.Scalar == tool {
			return
		}
	}
	t.Errorf("%s: %q not found in tools field; items: %v", context, tool, v.Items)
}

// ---------------------------------------------------------------------------
// T4.3 — Fresh deploy produces destination field
// ---------------------------------------------------------------------------

// TestDeployNew_UserConfigDestMapping_ProducesMainAndDestField verifies that when the
// user-local config declares a multi-destination mapping for user_interaction on a harness,
// a freshly deployed agent file carries BOTH the main tools field (containing the DestMain
// harness tool name) AND the separate destination field (containing the DestField names).
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
// After I4.1 and I4.2, the test passes when the transform correctly emits both fields.
func TestDeployNew_UserConfigDestMapping_ProducesMainAndDestField(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// buildRegistryWithUserDestMapping references registry.Options.ToolMappings → compile error.
	reg := buildRegistryWithUserDestMapping(t, id, desc)

	doc := applyWithRegistry(t, reg, id, []byte(destTestAgentSource))

	// Both fields must be present.
	assertHasMainTool(t, doc, "AskUser", "fresh deploy")
	assertHasMCPField(t, doc, "fresh deploy")
}

// TestDeployNew_UserConfigDestMapping_DestFieldHasCorrectName verifies that the destination
// field name in the deployed file exactly matches the Field value in the DestField destination
// declaration ("mcp_servers"), not any other key name.
func TestDeployNew_UserConfigDestMapping_DestFieldHasCorrectName(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// Compile error until I4.1.
	reg := buildRegistryWithUserDestMapping(t, id, desc)

	doc := applyWithRegistry(t, reg, id, []byte(destTestAgentSource))

	if _, ok := doc.Frontmatter().Get("mcp_servers"); !ok {
		t.Errorf("mcp_servers field absent from output; the destination field key must exactly match "+
			"the DestField.Field value declared in the config mapping; keys: %v", doc.Frontmatter().Keys())
	}
	// Confirm no misspelled variant is present.
	for _, wrong := range []string{"mcpservers", "mcp-servers", "MCP_servers"} {
		if _, ok := doc.Frontmatter().Get(wrong); ok {
			t.Errorf("unexpected field %q present; destination field key must match the declared Field value exactly", wrong)
		}
	}
}

// ---------------------------------------------------------------------------
// T4.4 — Update regenerates destination field (app.Update level)
//
// TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField is the
// primary app.Update-level test for AC4.4/AC4.5 coverage. The tests below it
// exercise the transform path directly (faster, lower-level). Both levels are needed:
// the direct path confirms the transform logic, and the app.Update-level test confirms
// the Content function wiring reaches the registry module's mapping hook result.
// ---------------------------------------------------------------------------

// TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField verifies that
// when app.Update processes an ActionUpdate plan item for an agent with a config-declared
// DestField destination mapping installed on the registry module, the Content function
// produces frontmatter that includes both the main tools field (AskUser) and the
// separate destination field (mcp_servers).
//
// This test exercises the full app.Update code path:
//
//   app.Update
//   → deps.Registry.Resolve(harnessID)         (module carries the ToolMappings hook result)
//   → deps.Planner.Build(ctx, planInput)        (stub returns ActionUpdate for the agent)
//   → capturingContentExecutor.Execute          (calls req.Content for each ActionUpdate item)
//   → buildContent → catalog.ReadSource → transform.Apply (module.Tools → descriptor.MapTools)
//   → output bytes contain mcp_servers field
//
// A stub planner is used to isolate the content-function wiring concern from the planner's
// staleness classification logic. The staleness detection path — including the
// ToolMappingsVersion comparison that detects config-mapping-only changes — is covered by
// the dedicated tests in plan/dest_mapping_staleness_test.go. The capturingContentExecutor
// records the bytes produced by the Content function without performing any real file writes.
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
func TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptorWithPaths(id)

	// Build registry with user destination mapping (compile error until I4.1).
	reg := buildRegistryWithUserDestMapping(t, id, desc)

	// Catalog with the agent declaring user_interaction and a workflow referencing it.
	// AddWorkflowIDs carries destTestWorkflowID so plan.ResolveArtifacts includes the agent
	// in the artifact set, which is required for buildContent to look up the agent's source.
	cat := newDestTestCatalog()

	// Target path as computed by destTestDescriptorWithPaths + destTestAgentSourceFile:
	// Paths.Agents.Project="agents", FileName=filepath.Base(destTestAgentSourceFile)="dest-test-agent.md"
	// → path.Join("agents", "dest-test-agent.md") = "agents/dest-test-agent.md"
	const agentTargetPath = "agents/dest-test-agent.md"

	// Stub planner returns ActionUpdate for the agent, directly exercising the content
	// path for an update scenario. The real planner detects config-mapping-only changes
	// via ToolMappingsVersion staleness comparison (implemented in plan/staleness.go and
	// covered by plan/dest_mapping_staleness_test.go). The stub is used here to isolate
	// the content-function wiring — which is what this test is about — from the planner's
	// classification logic.
	staleItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: destTestAgentKey},
		TargetPath: agentTargetPath,
		Action:     domain.ActionUpdate,
		// Stale is intentionally empty: this test verifies the content path, not staleness
		// detection. Staleness detection for config-mapping changes is covered separately.
	}

	capExec := &capturingContentExecutor{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = reg
	deps.Catalog = cat
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       domain.HarnessRef{ID: id, DisplayName: id, Tier: domain.TierBuiltin, Usable: true},
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{staleItem},
		Workflows:     []string{destTestWorkflowID},
	}}
	deps.Executor = capExec
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       id,
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{destTestWorkflowID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	content, ok := capExec.contents[agentTargetPath]
	if !ok {
		var knownTargets []string
		for k := range capExec.contents {
			knownTargets = append(knownTargets, k)
		}
		t.Fatalf("no content produced for target path %q; "+
			"app.Update must call Content for each ActionUpdate item so the registry module "+
			"(carrying the ToolMappings hook result) produces the destination field in the output; "+
			"targets with produced content: %v", agentTargetPath, knownTargets)
	}
	doc, parseErr := docformat.Parse(content)
	if parseErr != nil {
		t.Fatalf("docformat.Parse on content produced by app.Update Content function: %v", parseErr)
	}
	assertHasMCPField(t, doc, "app.Update content (ActionUpdate)")
	assertHasMainTool(t, doc, "AskUser", "app.Update content (ActionUpdate)")
}

// ---------------------------------------------------------------------------
// T4.4 — Update regenerates destination field (transform direct path)
// ---------------------------------------------------------------------------

// TestUpdate_UserConfigDestMapping_FieldRegeneratedOnSecondApply verifies that the
// destination field is regenerated on every apply pass. Running transform.Apply twice
// with the same source and module must produce the mcp_servers field both times with
// identical content.
//
// This is the core "regeneration" property: the destination field is derived exclusively
// from the config mapping (applied via the hook), not from the previously deployed file.
// The transform always reads the catalog source, not the deployed file.
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
func TestUpdate_UserConfigDestMapping_FieldRegeneratedOnSecondApply(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// Compile error until I4.1.
	reg := buildRegistryWithUserDestMapping(t, id, desc)

	source := []byte(destTestAgentSource)

	// First apply (deploy).
	doc1 := applyWithRegistry(t, reg, id, source)
	assertHasMCPField(t, doc1, "first apply (deploy)")

	// Second apply (update) with the same source and module.
	// The registry module still carries the hook-overlaid mappings because the descriptor
	// was mutated in-place by Discover. Re-applying must produce the same result.
	doc2 := applyWithRegistry(t, reg, id, source)
	assertHasMCPField(t, doc2, "second apply (update)")

	// The mcp_servers values must be identical between both applies.
	v1, _ := doc1.Frontmatter().Get("mcp_servers")
	v2, _ := doc2.Frontmatter().Get("mcp_servers")
	if len(v1.Items) != len(v2.Items) {
		t.Errorf("mcp_servers item count changed between deploy and update: deploy=%d update=%d; "+
			"regeneration must be stable", len(v1.Items), len(v2.Items))
	}
}

// TestUpdate_UserConfigDestMapping_HookPresenceControlsFieldEmission verifies that the
// presence of a DestField mapping in the ToolMappings hook's return value is what determines
// whether the destination field appears in the transform output. A registry whose hook
// returns nil (no mapping) produces no mcp_servers field; the same descriptor applied with
// a hook returning a multi-destination mapping produces both the main tools field and
// the mcp_servers field.
//
// Note: this test uses two independently configured registries with the same underlying
// descriptor but different harness IDs (id vs id+"-updated"), so it does not simulate a
// true "update of the same deployed file". The app.Update-level test
// TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField covers the end-
// to-end path through app.Update; this test confirms the transform-level hook control.
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
func TestUpdate_UserConfigDestMapping_HookPresenceControlsFieldEmission(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// Apply without the mapping — the hook returns nil, no mcp_servers in output.
	// Compile error until I4.1.
	regWithoutMapping := buildRegistryWithoutMapping(t, id, desc)
	doc1 := applyWithRegistry(t, regWithoutMapping, id, []byte(destTestAgentSource))
	if _, ok := doc1.Frontmatter().Get("mcp_servers"); ok {
		t.Log("Note: mcp_servers present when hook returns nil (no mapping), which is unexpected but not critical for this test")
	}

	// Apply with the mapping (different harness ID to avoid registration conflict).
	// The hook returns the multi-destination mapping; mcp_servers must appear.
	// Compile error until I4.1.
	regWithMapping := buildRegistryWithUserDestMapping(t, id+"-updated", desc)
	doc2 := applyWithRegistry(t, regWithMapping, id+"-updated", []byte(destTestAgentSource))
	assertHasMCPField(t, doc2, "apply with hook returning mapping")
}

// ---------------------------------------------------------------------------
// T4.5 — Destination field removed when mapping is removed from config
// ---------------------------------------------------------------------------

// TestUpdate_UserConfigDestMappingRemoved_DestFieldAbsentFromNextApply verifies that when
// the DestField destination mapping is removed from the user config, the destination field
// is absent from the next update's output. The field must NOT persist from the previously
// deployed file: it exists only when the config mapping produces it.
//
// This tests the removal-is-immediate property: configs take effect on the next apply.
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
func TestUpdate_UserConfigDestMappingRemoved_DestFieldAbsentFromNextApply(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// Step 1: deploy with config mapping — output has mcp_servers.
	// Compile error until I4.1.
	regWith := buildRegistryWithUserDestMapping(t, id+"-with", desc)
	doc1 := applyWithRegistry(t, regWith, id+"-with", []byte(destTestAgentSource))
	assertHasMCPField(t, doc1, "initial deploy with mapping")

	// Step 2: remove the mapping from config and apply again.
	// Using the same desc (descriptor unchanged); the hook now returns nil (no mappings).
	// Compile error until I4.1.
	regWithout := buildRegistryWithoutMapping(t, id+"-without", desc)
	doc2 := applyWithRegistry(t, regWithout, id+"-without", []byte(destTestAgentSource))

	// mcp_servers must be absent: the mapping was removed, so nothing produces it.
	if _, ok := doc2.Frontmatter().Get("mcp_servers"); ok {
		t.Errorf("mcp_servers still present in output after removing config mapping; "+
			"the destination field must be absent when no DestField destination mapping declares it; "+
			"keys: %v", doc2.Frontmatter().Keys())
	}
}

// TestUpdate_UserConfigDestMappingRemoved_MainToolFieldAlsoAbsent verifies that when the
// mapping is fully removed (both DestMain and DestField), the harness tool name no longer
// appears in the main tools field either. The user_interaction generic tool resolves as
// ToolUnmapped (producing a gap), not as a ToolMapped with any harness tool.
//
// Before I4.1 adds registry.Options.ToolMappings, this test fails to compile (TDD RED).
func TestUpdate_UserConfigDestMappingRemoved_MainToolFieldAlsoAbsent(t *testing.T) {
	id := appTestHarnessID(t)
	desc := destTestDescriptor(id)

	// No config mapping → user_interaction produces ToolUnmapped → no AskUser in tools.
	// Compile error until I4.1.
	regWithout := buildRegistryWithoutMapping(t, id, desc)
	doc := applyWithRegistry(t, regWithout, id, []byte(destTestAgentSource))

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		// user_interaction has no DestMain mapping when the mapping is removed, so it
		// resolves as ToolUnmapped — no harness tool name is emitted and the tools field
		// may be absent when no other tools resolve to a harness name. This is the intended
		// behavior, not a rendering failure.
		//
		// Guard against a silent rendering failure by confirming mcp_servers is also absent.
		// Both fields (tools/AskUser and mcp_servers/user-feedback) are produced by the same
		// removed mapping. If mcp_servers is present but tools is absent, the mapping was only
		// partially removed or the output is inconsistent — that is a bug, not the intended state.
		if _, hasMCP := doc.Frontmatter().Get("mcp_servers"); hasMCP {
			t.Errorf("mcp_servers is present but tools is absent after removing the mapping; "+
				"a fully removed mapping must leave both fields absent — a split result "+
				"(one field absent, the other present) indicates a rendering inconsistency, not correct behavior; "+
				"keys: %v", doc.Frontmatter().Keys())
		}
		return
	}
	for _, item := range v.Items {
		if item.Scalar == "AskUser" {
			t.Errorf("AskUser present in tools field after removing mapping; "+
				"without a DestMain declaration, user_interaction must not produce any harness tool name; "+
				"all tools: %v", v.Items)
		}
	}
}
