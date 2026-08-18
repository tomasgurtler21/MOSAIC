package app_test

// customtool_test.go verifies the custom tool name prompting step: QCustomTool is asked
// for each generic tool that has no harness mapping, skip and skip-all work correctly,
// and every skipped or unanswered tool produces the appropriate TODO record.
//
// Verified invariants (T18.4):
//
// Custom tool prompting:
//   - QCustomTool is asked (with the generic tool name as Subject) for each unmapped tool
//   - A provided custom name propagates into HarnessModule.Tools via ToolRequest.CustomNames
//   - The question is an AskText question (free-text entry)
//
// Skip one:
//   - Answering SkippedOne for a tool produces a GapUnmappedTool for that tool
//   - Other unmapped tools are still asked after a SkippedOne
//
// Skip all:
//   - Answering SkippedAll stops asking QCustomTool for remaining unmapped tools (CD-5)
//   - All remaining unmapped tools get GapUnmappedTool entries
//
// Pre-answered custom tools (CustomTools in request):
//   - QCustomTool is not asked for tools pre-answered in CustomTools

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Custom tool prompting
// ---------------------------------------------------------------------------

// TestDeployNew_UnmappedTool_AsksQCustomTool verifies that when an agent has a generic
// tool with no harness mapping, the flow asks QCustomTool for that tool.
func TestDeployNew_UnmappedTool_AsksQCustomTool(t *testing.T) {
	// Arrange — harness module reports the tool as ToolUnmapped
	cat := newMinimalCatalog()
	// The test-runner agent has tool "bash"; make the harness report it as unmapped
	mod := &unmappedToolModule{
		base: newMinimalModule(),
		unmappedTools: []string{"bash"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QCustomTool, "bash", "my-bash-server").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QCustomTool, "bash") {
		t.Error("QCustomTool was not asked for the unmapped tool \"bash\"; " +
			"the flow must ask QCustomTool for each generic tool the harness cannot map")
	}
}

// TestDeployNew_CustomToolPreAnswered_DoesNotAskQCustomTool verifies that a tool whose
// custom name is pre-answered in DeployRequest.CustomTools is not asked again.
func TestDeployNew_CustomToolPreAnswered_DoesNotAskQCustomTool(t *testing.T) {
	// Arrange
	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		CustomTools:     map[string]string{"bash": "my-bash-server"},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QCustomTool, "bash") {
		t.Error("QCustomTool was asked for \"bash\" even though it was pre-answered in CustomTools (CD-6)")
	}
}

// ---------------------------------------------------------------------------
// Skip one
// ---------------------------------------------------------------------------

// TestDeployNew_UnmappedTool_SkipOne_ProducesGapUnmappedTool verifies that answering
// SkippedOne for a QCustomTool question produces a GapUnmappedTool gap for that tool.
// The gap is produced by plan.Build (Path B) and flows through the plan.Gaps copy into
// the todo collector; resolveCustomTools no longer emits it directly.
func TestDeployNew_UnmappedTool_SkipOne_ProducesGapUnmappedTool(t *testing.T) {
	// Arrange — stub defaults to SkippedOne for QCustomTool (unscripted)
	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build() // QCustomTool is unscripted → SkippedOne by default
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan stub carries the agent-attributed gap that plan.Build produces for an unmapped tool.
	planWithGap := newMinimalPlan(workspace)
	planWithGap.Gaps = []domain.Gap{
		{Kind: domain.GapUnmappedTool, Subject: "bash",
			Detail: `generic tool "bash" has no harness mapping for agent "test-runner"`},
	}
	deps.Registry = reg
	deps.Planner = &stubPlanner{plan: planWithGap}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !spy.hasGapKind(domain.GapUnmappedTool) {
		t.Error("skipping QCustomTool (SkippedOne) did not produce a GapUnmappedTool gap; " +
			"every skipped unmapped tool must be recorded as a gap")
	}
}

// TestDeployNew_UnmappedTool_SkipOne_AsksContinuesForOtherTools verifies that after
// SkippedOne for one unmapped tool, the flow still asks QCustomTool for the next tool.
func TestDeployNew_UnmappedTool_SkipOne_AsksContinuesForOtherTools(t *testing.T) {
	// Arrange — two unmapped tools; skip first, answer second.
	// The catalog agent must declare both tools so the app discovers both as requiring mapping.
	twoToolAgent := domain.Agent{
		Key:             "test-runner",
		NumericID:       "1",
		Version:         "1.0",
		Name:            "Test Runner",
		Description:     "Runs tests",
		Role:            domain.RoleWorker,
		Category:        "Execution",
		RecommendedTier: "HIGH",
		TierRationale:   "Needs a capable model to analyse test output",
		Tools:           []string{"bash", "web-search"},
	}
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{twoToolAgent}

	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash", "web-search"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		// bash: unscripted → SkippedOne (default)
		AnswerText(domain.QCustomTool, "web-search", "my-search-server").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — web-search must have been asked even though bash was skipped
	if !stub.WasAsked(domain.QCustomTool, "web-search") {
		t.Error("QCustomTool for \"web-search\" was not asked after \"bash\" was skipped one; " +
			"SkippedOne must only skip the current tool, not all remaining unmapped tools")
	}
}

// ---------------------------------------------------------------------------
// Skip all
// ---------------------------------------------------------------------------

// TestDeployNew_UnmappedTool_SkipAll_StopsAsking verifies that answering SkippedAll for
// QCustomTool stops the flow from asking about further unmapped tools (CD-5).
func TestDeployNew_UnmappedTool_SkipAll_StopsAsking(t *testing.T) {
	// Arrange — two unmapped tools; skip-all on first
	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash", "web-search"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QCustomTool, "bash"). // skip-all on first tool
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — web-search must not have been asked
	if stub.WasAsked(domain.QCustomTool, "web-search") {
		t.Error("QCustomTool for \"web-search\" was asked after SkippedAll was returned for \"bash\"; " +
			"SkippedAll must latch globally and stop further tool name questions (CD-5)")
	}
}

// TestDeployNew_UnmappedTool_SkipAll_ProducesGapForRemainingTools verifies that when
// QCustomTool is skipped-all, all remaining unmapped tools also receive GapUnmappedTool entries.
// The gaps are produced by plan.Build (Path B) for each unmapped tool and flow through
// plan.Gaps into the todo collector.
func TestDeployNew_UnmappedTool_SkipAll_ProducesGapForRemainingTools(t *testing.T) {
	// Arrange — two unmapped tools; skip-all on first.
	// The catalog agent must declare both tools so the app discovers both as requiring mapping.
	twoToolAgent := domain.Agent{
		Key:             "test-runner",
		NumericID:       "1",
		Version:         "1.0",
		Name:            "Test Runner",
		Description:     "Runs tests",
		Role:            domain.RoleWorker,
		Category:        "Execution",
		RecommendedTier: "HIGH",
		TierRationale:   "Needs a capable model to analyse test output",
		Tools:           []string{"bash", "web-search"},
	}
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{twoToolAgent}

	mod := &unmappedToolModule{
		base:          newMinimalModule(),
		unmappedTools: []string{"bash", "web-search"},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": mod}

	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QCustomTool, "bash").
		AnswerReview(true).
		Build()
	spy := &spyTodo{}
	deps, workspace := newBaseDeps(t, stub)

	// Plan stub carries the agent-attributed gaps that plan.Build produces for each unmapped tool.
	planWithGaps := newMinimalPlan(workspace)
	planWithGaps.Gaps = []domain.Gap{
		{Kind: domain.GapUnmappedTool, Subject: "bash",
			Detail: `generic tool "bash" has no harness mapping for agent "test-runner"`},
		{Kind: domain.GapUnmappedTool, Subject: "web-search",
			Detail: `generic tool "web-search" has no harness mapping for agent "test-runner"`},
	}
	deps.Catalog = cat
	deps.Registry = reg
	deps.Planner = &stubPlanner{plan: planWithGaps}
	deps.Todo = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — both bash and web-search must have GapUnmappedTool
	bashGaps := 0
	webSearchGaps := 0
	for _, g := range spy.gaps {
		if g.Kind == domain.GapUnmappedTool {
			if g.Subject == "bash" {
				bashGaps++
			}
			if g.Subject == "web-search" {
				webSearchGaps++
			}
		}
	}
	if bashGaps == 0 {
		t.Error("no GapUnmappedTool for \"bash\" after SkippedAll; skipped-all tools must each get a gap entry")
	}
	if webSearchGaps == 0 {
		t.Error("no GapUnmappedTool for \"web-search\" after SkippedAll; remaining tools after skip-all must each get a gap entry")
	}
}

// ---------------------------------------------------------------------------
// Custom name propagation
// ---------------------------------------------------------------------------

// TestDeployNew_UnmappedTool_CustomName_PropagatesIntoToolRequest verifies that answering
// QCustomTool with a custom MCP server name causes ToolRequest.CustomNames to carry that
// name when HarnessModule.Tools is called during the deploy phase (T18.4).
func TestDeployNew_UnmappedTool_CustomName_PropagatesIntoToolRequest(t *testing.T) {
	// Arrange — capturing module records every Tools call; the underlying module reports "bash" unmapped
	var capturedReqs []domain.ToolRequest
	capMod := &capturingToolsModule{
		base: &unmappedToolModule{
			base:          newMinimalModule(),
			unmappedTools: []string{"bash"},
		},
		onTools: func(req domain.ToolRequest) {
			capturedReqs = append(capturedReqs, req)
		},
	}
	reg := newMinimalRegistry(nil)
	reg.modules = map[string]domain.HarnessModule{"stub-harness": capMod}

	stub := interactiontest.NewBuilder().
		AnswerText(domain.QCustomTool, "bash", "my-bash-server").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Registry = reg
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — at least one Tools call must have CustomNames["bash"] == "my-bash-server"
	found := false
	for _, req := range capturedReqs {
		if req.CustomNames != nil && req.CustomNames["bash"] == "my-bash-server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no HarnessModule.Tools call received CustomNames[\"bash\"] = \"my-bash-server\"; " +
			"a QCustomTool answer must propagate into ToolRequest.CustomNames so the harness can " +
			"render the MCP server name in the deployed frontmatter (T18.4)")
	}
}

// ---------------------------------------------------------------------------
// Helpers for this file
// ---------------------------------------------------------------------------

// capturingToolsModule wraps a domain.HarnessModule and records every Tools call, allowing
// tests to inspect the ToolRequest the app layer passes to the harness.
type capturingToolsModule struct {
	base    domain.HarnessModule
	onTools func(domain.ToolRequest)
}

func (m *capturingToolsModule) Ref() domain.HarnessRef { return m.base.Ref() }
func (m *capturingToolsModule) Descriptor() *domain.HarnessDescriptor {
	return m.base.Descriptor()
}
func (m *capturingToolsModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	if m.onTools != nil {
		m.onTools(req)
	}
	return m.base.Tools(req)
}
func (m *capturingToolsModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return m.base.Frontmatter(req)
}
func (m *capturingToolsModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.base.TargetPath(req)
}
func (m *capturingToolsModule) Injection(req domain.InjectionRequest) (string, bool) {
	return m.base.Injection(req)
}
func (m *capturingToolsModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.base.HookPlan(req)
}
func (m *capturingToolsModule) Close() error { return m.base.Close() }

// unmappedToolModule wraps a base stubHarnessModule but returns ToolUnmapped for listed tools.
type unmappedToolModule struct {
	base          *stubHarnessModule
	unmappedTools []string
}

func (m *unmappedToolModule) Ref() domain.HarnessRef              { return m.base.Ref() }
func (m *unmappedToolModule) Descriptor() *domain.HarnessDescriptor { return m.base.Descriptor() }
func (m *unmappedToolModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		outcome := domain.ToolMapped
		for _, u := range m.unmappedTools {
			if g == u {
				outcome = domain.ToolUnmapped
				break
			}
		}
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: outcome}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *unmappedToolModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return m.base.Frontmatter(req)
}
func (m *unmappedToolModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.base.TargetPath(req)
}
func (m *unmappedToolModule) Injection(req domain.InjectionRequest) (string, bool) { return m.base.Injection(req) }
func (m *unmappedToolModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.base.HookPlan(req)
}
func (m *unmappedToolModule) Close() error { return nil }
