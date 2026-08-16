package tui

// deploy_agents_routing_test.go covers two concerns:
//
//  1. Adapter coverage for the merged QDeployAgents question (T5.2): optionsToAgentCategories
//     handles the specific group-naming scheme that askDeployAgents emits ("Subagents/AI",
//     "UtilityAgents", "StandaloneAgents/Tools", etc.), preserving root ordering and the
//     empty-group-last rule.
//
//  2. QDeployAgents routing (T5.4): the tui rootModel routes a QDeployAgents SelectMany
//     question to the dedicated DeployAgentScreen, not the generic inlineSelectOne fallback.
//
//  3. startService dispatch (T5.4): selecting ModeDeployAgents causes the root model to call
//     svc.DeployAgents; selecting ModeDeployHooks causes it to call svc.DeployHooks.
//
// All tests are RED-phase TDD tests:
//   - T5.2 adapter tests compile and pass once optionsToAgentCategories is in place (it
//     already exists and is generic, so these tests validate the naming convention).
//   - T5.4 routing tests fail until QDeployAgents is added to questionSelectMany.
//   - T5.4 startService tests fail until the new mode cases are added to startService.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T5.2: Adapter — optionsToAgentCategories with merged-question group names
// ---------------------------------------------------------------------------

// deployAgentOptions returns options in the ordering that askDeployAgents emits:
// Subagents roots first, then UtilityAgents, then StandaloneAgents roots.
// Group names use the naming scheme from the design ("Subagents/Infra", "UtilityAgents", etc.).
func deployAgentOptions() []domain.Option {
	return []domain.Option{
		// Subagents with a category
		{ID: "subagent-infra-a", Label: "Infra A", Group: "Subagents/Infrastructure"},
		{ID: "subagent-infra-b", Label: "Infra B", Group: "Subagents/Infrastructure"},
		// Subagents without a category (root group)
		{ID: "subagent-plain", Label: "Plain Subagent", Group: "Subagents"},
		// Utility agents (no sub-categories by design)
		{ID: "utility-a", Label: "Utility A", Group: "UtilityAgents"},
		{ID: "utility-b", Label: "Utility B", Group: "UtilityAgents"},
		// Standalone agents with a category
		{ID: "standalone-tools-a", Label: "Tools A", Group: "StandaloneAgents/Tools"},
		// Standalone agents without a category (root group)
		{ID: "standalone-plain", Label: "Plain Standalone", Group: "StandaloneAgents"},
	}
}

// TestOptionsToAgentCategories_MergedQuestion_RootGroupsInInputOrder verifies that the
// root groups ("Subagents/Infrastructure", "Subagents", "UtilityAgents",
// "StandaloneAgents/Tools", "StandaloneAgents") appear in the output in the same
// first-appearance order as the input. The service emits them in the required root order;
// the adapter must preserve that order verbatim.
func TestOptionsToAgentCategories_MergedQuestion_RootGroupsInInputOrder(t *testing.T) {
	opts := deployAgentOptions()

	cats := optionsToAgentCategories(opts)

	// Expect four named groups (the empty-group category is placed last by the adapter,
	// but none of our options here have an empty Group, so there should be no empty-group cat).
	// Groups in order: Subagents/Infrastructure, Subagents, UtilityAgents, StandaloneAgents/Tools, StandaloneAgents
	if len(cats) != 5 {
		t.Fatalf("got %d categories; want 5 (Subagents/Infrastructure, Subagents, UtilityAgents, "+
			"StandaloneAgents/Tools, StandaloneAgents)", len(cats))
	}

	wantOrder := []string{
		"Subagents/Infrastructure",
		"Subagents",
		"UtilityAgents",
		"StandaloneAgents/Tools",
		"StandaloneAgents",
	}
	for i, want := range wantOrder {
		if cats[i].Name != want {
			t.Errorf("cats[%d].Name = %q; want %q (group order must match first-appearance in options)",
				i, cats[i].Name, want)
		}
	}
}

// TestOptionsToAgentCategories_MergedQuestion_AgentsPerGroupCount verifies that agents are
// placed in the correct group without cross-contamination.
func TestOptionsToAgentCategories_MergedQuestion_AgentsPerGroupCount(t *testing.T) {
	cats := optionsToAgentCategories(deployAgentOptions())

	if len(cats) < 5 {
		t.Fatalf("expected at least 5 categories; got %d", len(cats))
	}

	// Subagents/Infrastructure: 2 agents
	if len(cats[0].Agents) != 2 {
		t.Errorf("Subagents/Infrastructure has %d agents; want 2", len(cats[0].Agents))
	}
	// Subagents: 1 agent
	if len(cats[1].Agents) != 1 {
		t.Errorf("Subagents has %d agents; want 1", len(cats[1].Agents))
	}
	// UtilityAgents: 2 agents
	if len(cats[2].Agents) != 2 {
		t.Errorf("UtilityAgents has %d agents; want 2", len(cats[2].Agents))
	}
	// StandaloneAgents/Tools: 1 agent
	if len(cats[3].Agents) != 1 {
		t.Errorf("StandaloneAgents/Tools has %d agents; want 1", len(cats[3].Agents))
	}
	// StandaloneAgents: 1 agent
	if len(cats[4].Agents) != 1 {
		t.Errorf("StandaloneAgents has %d agents; want 1", len(cats[4].Agents))
	}
}

// TestOptionsToAgentCategories_MergedQuestion_EmptyGroupPlacedLast verifies that when the
// merged question emits an option with an empty Group (which the design does not produce but
// is the general contract), the adapter places it last even if it appeared first in the input.
func TestOptionsToAgentCategories_MergedQuestion_EmptyGroupPlacedLast(t *testing.T) {
	// An option with no group appearing before a named group — must still end up last.
	opts := []domain.Option{
		{ID: "uncategorised-a", Label: "Uncat A", Group: ""},
		{ID: "subagent-x", Label: "Subagent X", Group: "Subagents/AI"},
		{ID: "utility-x", Label: "Utility X", Group: "UtilityAgents"},
	}

	cats := optionsToAgentCategories(opts)

	if len(cats) < 3 {
		t.Fatalf("expected at least 3 categories; got %d", len(cats))
	}
	last := cats[len(cats)-1]
	if last.Name != "" {
		t.Errorf("last category Name = %q; want empty string;\n"+
			"the empty-Group category must always be placed last, "+
			"even when its option appeared first in the input slice",
			last.Name)
	}
}

// TestOptionsToAgentCategories_MergedQuestion_AllOptionIDs_Preserved verifies that no
// option is dropped and that every ID from the input appears exactly once in the output.
func TestOptionsToAgentCategories_MergedQuestion_AllOptionIDs_Preserved(t *testing.T) {
	opts := deployAgentOptions()

	cats := optionsToAgentCategories(opts)

	seen := map[string]int{}
	for _, cat := range cats {
		for _, a := range cat.Agents {
			seen[a.Key]++
		}
	}

	for _, opt := range opts {
		if count := seen[opt.ID]; count != 1 {
			t.Errorf("option ID %q appears %d times in the output; want exactly 1 "+
				"(no option must be dropped or duplicated)",
				opt.ID, count)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.4: QDeployAgents routing — dedicated screen, not generic fallback
// ---------------------------------------------------------------------------

// mergedAgentOptions returns options for a QDeployAgents question.
func mergedAgentOptions() []domain.Option {
	return []domain.Option{
		{ID: "infra-agent", Label: "Infra Agent", Group: "Subagents/Infrastructure"},
		{ID: "utility-agent", Label: "Utility Agent", Group: "UtilityAgents"},
		{ID: "standalone-agent", Label: "Standalone Agent", Group: "StandaloneAgents"},
	}
}

// TestMultiSelectRouting_QDeployAgents_DoesNotUseGenericFallback verifies that a
// QDeployAgents SelectMany question routes to the dedicated DeployAgentScreen, not the
// generic inlineSelectOne fallback. Without an explicit case in questionSelectMany, the
// question silently degrades to the generic overlay.
func TestMultiSelectRouting_QDeployAgents_DoesNotUseGenericFallback(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions())

	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Error("selectOverlay is non-nil after QDeployAgents SelectMany;\n" +
			"the generic inlineSelectOne must not handle QDeployAgents — " +
			"add a case for domain.QDeployAgents in questionSelectMany that creates a DeployAgentScreen")
	}
	if m.screen != screenQuestion {
		t.Errorf("screen = %v after QDeployAgents; want screenQuestion", m.screen)
	}
}

// TestMultiSelectRouting_QDeployAgents_ViewShowsDeployAgentScreenTitle verifies that after
// a QDeployAgents question the dedicated DeployAgentScreen is active — not the generic
// inlineSelectOne fallback.
//
// The original assertion (strings.Contains(view, "Agent")) is not sufficient because option
// labels such as "Infra Agent", "Utility Agent", and "Standalone Agent" contain the word
// "Agent" regardless of which overlay rendered the view. The generic fallback therefore
// satisfies the string check even when no dedicated screen is wired, producing a false GREEN.
// The structurally correct check mirrors DoesNotUseGenericFallback: assert selectOverlay==nil.
func TestMultiSelectRouting_QDeployAgents_ViewShowsDeployAgentScreenTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions())
	m.Update(qMsg)

	// The dedicated DeployAgentScreen must be active. If selectOverlay is non-nil the
	// generic inlineSelectOne handled the question — add a case for domain.QDeployAgents
	// in questionSelectMany that creates a DeployAgentScreen (see ContractsDesign: I5.2).
	if m.selectOverlay != nil {
		t.Errorf("selectOverlay is non-nil after QDeployAgents;\n"+
			"the dedicated DeployAgentScreen must be used, not the generic overlay.\n"+
			"Wire QDeployAgents to NewDeployAgentScreen in questionSelectMany.\n"+
			"View (rendered by the wrong generic overlay):\n%s", m.View())
	}
}

// TestMultiSelectRouting_QDeployAgents_ResizeDoesNotPanic verifies that a WindowSizeMsg
// does not panic when the deploy-agent overlay is active.
func TestMultiSelectRouting_QDeployAgents_ResizeDoesNotPanic(t *testing.T) {
	m := newRoutingModel()
	m.Update(buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions()))

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("WindowSizeMsg panicked with active deploy-agent overlay: %v", r)
		}
	}()

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

// TestMultiSelectRoundTrip_QDeployAgents_MultipleSelectionsDeliverAllIDs verifies that
// selecting multiple agents from the QDeployAgents screen and confirming delivers all
// selected IDs in the MultiChoiceAnswer.
func TestMultiSelectRoundTrip_QDeployAgents_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions())
	m.Update(qMsg)

	// Precondition: verify the dedicated DeployAgentScreen is active and the generic
	// fallback is not. Checking the rendered view for "Agent" is insufficient: option
	// labels ("Infra Agent", "Utility Agent", etc.) satisfy the check even when the
	// generic overlay is rendering them. Assert selectOverlay==nil instead.
	if m.selectOverlay != nil {
		t.Fatalf("precondition: selectOverlay is non-nil after QDeployAgents;\n"+
			"the dedicated DeployAgentScreen must be used — wire QDeployAgents to\n"+
			"NewDeployAgentScreen in questionSelectMany (I5.2)")
	}

	// Select first agent (Space), navigate down (Down), select second (Space), confirm (Enter).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming agent selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		if len(ans.multiChoiceAns.OptionIDs) == 0 {
			t.Errorf("reply contains 0 IDs; want at least one selected agent key")
		}
	default:
		t.Error("reply channel has no message after confirming agent selection")
	}
}

// TestMultiSelectRoundTrip_QDeployAgents_Esc_ProducesCancelled verifies that pressing Esc
// on the DeployAgentScreen sends a MultiChoiceAnswer with Status=Cancelled.
func TestMultiSelectRoundTrip_QDeployAgents_Esc_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions())
	m.Update(qMsg)

	// Precondition: the dedicated DeployAgentScreen must be active, not the generic overlay.
	// Option labels contain "Agent" regardless of which overlay rendered them; check
	// selectOverlay==nil to confirm the generic fallback is not in use.
	if m.selectOverlay != nil {
		t.Fatalf("precondition: selectOverlay is non-nil after QDeployAgents;\n"+
			"wire QDeployAgents to NewDeployAgentScreen in questionSelectMany (I5.2)")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select one
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                       // cancel

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc; want Cancelled "+
				"(Esc must not deliver partial selections)",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc on DeployAgentScreen")
	}
}

// ---------------------------------------------------------------------------
// T5.4: startService dispatch — new modes call correct service methods
// ---------------------------------------------------------------------------

// spyDispatchService records which service method is called, for dispatch tests.
type spyDispatchService struct {
	harnesses       []domain.HarnessRef
	deployAgentsCalled int
	deployHooksCalled  int
	deployNewCalled    int
}

func (s *spyDispatchService) ListHarnesses() []domain.HarnessRef { return s.harnesses }

func (s *spyDispatchService) DeployNew(_ context.Context, _ app.DeployRequest) (domain.RunSummary, error) {
	s.deployNewCalled++
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *spyDispatchService) Update(_ context.Context, _ app.UpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *spyDispatchService) UpdateWorkflows(_ context.Context, _ app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{Outcome: domain.OutcomeSuccess}, nil
}

func (s *spyDispatchService) Promote(_ context.Context, _ app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}

func (s *spyDispatchService) TransformHarness(_ context.Context, _ app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return app.TransformHarnessResult{}, nil
}

func (s *spyDispatchService) DeployAgents(_ context.Context, _ app.DeployAgentsRequest) (domain.RunSummary, error) {
	s.deployAgentsCalled++
	return domain.RunSummary{Mode: domain.ModeDeployAgents, Outcome: domain.OutcomeSuccess}, nil
}

func (s *spyDispatchService) DeployHooks(_ context.Context, _ app.DeployHooksRequest) (domain.RunSummary, error) {
	s.deployHooksCalled++
	return domain.RunSummary{Mode: domain.ModeDeployHooks, Outcome: domain.OutcomeSuccess}, nil
}

func (s *spyDispatchService) RenderAgent(_ context.Context, _ app.RenderAgentRequest) (app.RenderAgentResult, error) {
	return app.RenderAgentResult{}, nil
}

func (s *spyDispatchService) CheckWorkflowIndex(_ context.Context) (app.IndexCheckResult, error) {
	return app.IndexCheckResult{}, nil
}

// dispatchHarness is the test harness used for dispatch flow tests.
var dispatchHarness = domain.HarnessRef{
	ID:          "dispatch-harness",
	DisplayName: "Dispatch Test Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// TestStartServiceDispatch_DeployAgents_CallsDeployAgents verifies that when the user
// selects ModeDeployAgents (position 3 in the mode list) and confirms the workspace,
// startService dispatches to svc.DeployAgents and not to svc.DeployNew or other methods.
//
// In RED phase this test fails because:
//   - Position 3 currently holds ModePromoteToGeneric (not ModeDeployAgents), or
//   - The startService switch has no case for ModeDeployAgents.
func TestStartServiceDispatch_DeployAgents_CallsDeployAgents(t *testing.T) {
	workspace := t.TempDir()
	spy := &spyDispatchService{harnesses: []domain.HarnessRef{dispatchHarness}}
	m := newFlowModel(spy, workspace)

	// Step 1: Select the single harness.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenMode {
		t.Fatalf("screen = %v after harness Enter; want screenMode", m.screen)
	}

	// Step 2: Navigate to position 3 (deploy-agents after implementation) and confirm.
	// In RED phase, position 3 is ModePromoteToGeneric, causing a different flow.
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // position 0 → 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // position 1 → 2
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // position 2 → 3
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after selecting position 3 in mode list; want screenWorkspace.\n"+
			"This fails in RED because position 3 does not yet hold ModeDeployAgents.\n"+
			"Add ModeDeployAgents at position 3 in modeItems and add its startService case.",
			m.screen)
	}
	if m.selections.mode != domain.ModeDeployAgents {
		t.Fatalf("selections.mode = %q; want ModeDeployAgents.\n"+
			"Position 3 in the mode list must map to ModeDeployAgents.",
			m.selections.mode)
	}

	// Step 3: Confirm the workspace (pre-filled by newFlowModel).
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after workspace Enter; want screenRunning", m.screen)
	}

	// Step 4: Execute the service command.
	_, _ = m.Update(runCmd(cmd))

	// Step 5: Verify DeployAgents was called, not DeployNew.
	if spy.deployAgentsCalled == 0 {
		t.Error("svc.DeployAgents was not called after selecting ModeDeployAgents;\n" +
			"startService must dispatch ModeDeployAgents to svc.DeployAgents")
	}
	if spy.deployNewCalled > 0 {
		t.Errorf("svc.DeployNew was called %d time(s); must not be called for ModeDeployAgents",
			spy.deployNewCalled)
	}
}

// TestStartServiceDispatch_DeployHooks_CallsDeployHooks verifies that selecting
// ModeDeployHooks (position 4 in the mode list) dispatches to svc.DeployHooks.
//
// In RED phase this test fails because position 4 is not ModeDeployHooks yet.
func TestStartServiceDispatch_DeployHooks_CallsDeployHooks(t *testing.T) {
	workspace := t.TempDir()
	spy := &spyDispatchService{harnesses: []domain.HarnessRef{dispatchHarness}}
	m := newFlowModel(spy, workspace)

	// Step 1: Select harness.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenMode {
		t.Fatalf("screen = %v after harness Enter; want screenMode", m.screen)
	}

	// Step 2: Navigate to position 4 (deploy-hooks after implementation).
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // 0 → 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // 1 → 2
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // 2 → 3
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // 3 → 4
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after selecting position 4 in mode list; want screenWorkspace.\n"+
			"Position 4 must hold ModeDeployHooks with a startService case routing to DeployHooks.",
			m.screen)
	}
	if m.selections.mode != domain.ModeDeployHooks {
		t.Fatalf("selections.mode = %q; want ModeDeployHooks", m.selections.mode)
	}

	// Step 3: Confirm workspace.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after workspace Enter; want screenRunning", m.screen)
	}

	// Step 4: Execute service.
	_, _ = m.Update(runCmd(cmd))

	// Step 5: Verify.
	if spy.deployHooksCalled == 0 {
		t.Error("svc.DeployHooks was not called after selecting ModeDeployHooks;\n" +
			"startService must dispatch ModeDeployHooks to svc.DeployHooks")
	}
	if spy.deployNewCalled > 0 {
		t.Errorf("svc.DeployNew was called %d time(s); must not be called for ModeDeployHooks",
			spy.deployNewCalled)
	}
	if spy.deployAgentsCalled > 0 {
		t.Errorf("svc.DeployAgents was called %d time(s); must not be called for ModeDeployHooks",
			spy.deployAgentsCalled)
	}
}

// TestStartServiceDispatch_DeployAgents_DoesNotCallDeployNew verifies the mutual exclusion
// property: selecting deploy-agents must never invoke the DeployNew path.
func TestStartServiceDispatch_DeployAgents_DoesNotCallDeployNew(t *testing.T) {
	workspace := t.TempDir()
	spy := &spyDispatchService{harnesses: []domain.HarnessRef{dispatchHarness}}
	m := newFlowModel(spy, workspace)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})   // harness
	m.Update(tea.KeyMsg{Type: tea.KeyDown})            // → position 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown})            // → position 2
	m.Update(tea.KeyMsg{Type: tea.KeyDown})            // → position 3
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})   // select mode

	if m.screen != screenWorkspace || m.selections.mode != domain.ModeDeployAgents {
		t.Skip("deploy-agents not yet at position 3; test will pass after implementation")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // workspace
	_, _ = m.Update(runCmd(cmd))

	if spy.deployNewCalled > 0 {
		t.Errorf("svc.DeployNew called %d time(s) for deploy-agents run; must be 0", spy.deployNewCalled)
	}
}
