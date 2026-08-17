package tui

// deploy_agents_routing_test.go covers two concerns:
//
//  1. QDeployAgents routing: the tui rootModel routes a QDeployAgents SelectMany
//     question to the dedicated DeployAgentScreen (tree-based), not the generic
//     inlineSelectOne fallback.
//
//  2. startService dispatch: selecting ModeDeployAgents causes the root model to call
//     svc.DeployAgents; selecting ModeDeployHooks causes it to call svc.DeployHooks.
//
// Routing tests verify that after receiving a QDeployAgents question:
//   - selectOverlay remains nil (the generic fallback is NOT used)
//   - deployAgentOverlay is non-nil (the dedicated DeployAgentScreen IS used)
//   - the rendered view contains the "Select Agents" title
//   - resize does not panic
//
// Round-trip tests navigate into folders (required because agents now live inside
// tree nodes, not at the top level) and verify selection delivery and cancellation.

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// QDeployAgents routing — dedicated screen, not generic fallback
// ---------------------------------------------------------------------------

// mergedAgentOptions returns options for a QDeployAgents question using the
// group-naming convention that askDeployAgents emits. With optionsToAgentTree
// the groups are parsed on "/" to reconstruct folder hierarchy.
func mergedAgentOptions() []domain.Option {
	return []domain.Option{
		{ID: "infra-agent", Label: "Infra Agent", Group: "Subagents/Infrastructure"},
		{ID: "utility-agent", Label: "Utility Agent", Group: "UtilityAgents"},
		{ID: "standalone-agent", Label: "Standalone Agent", Group: "StandaloneAgents"},
	}
}

// twoFolderAgentOptions returns a minimal option set with two single-level folders,
// used by round-trip tests that must navigate into folders before toggling agents.
// The synthetic folder names are deliberately generic so no test assertion depends
// on catalog-specific literals.
func twoFolderAgentOptions() []domain.Option {
	return []domain.Option{
		{ID: "alpha-agent", Label: "Alpha", Group: "FolderA"},
		{ID: "beta-agent", Label: "Beta", Group: "FolderB"},
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
// a QDeployAgents question the dedicated DeployAgentScreen is active and renders its title.
//
// Two assertions are made:
//  1. selectOverlay is nil — the generic inlineSelectOne did not handle the question.
//  2. View() contains "Select Agents" — the DeployAgentScreen title is visible.
//
// Checking "Agent" in the view is insufficient: option labels such as "Infra Agent",
// "Utility Agent", and "Standalone Agent" contain that word regardless of which overlay
// is active. Both checks are needed to confirm the correct screen is in use.
func TestMultiSelectRouting_QDeployAgents_ViewShowsDeployAgentScreenTitle(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, mergedAgentOptions())
	m.Update(qMsg)

	if m.selectOverlay != nil {
		t.Errorf("selectOverlay is non-nil after QDeployAgents;\n"+
			"the dedicated DeployAgentScreen must be used, not the generic overlay.\n"+
			"Wire QDeployAgents to NewDeployAgentScreen in questionSelectMany.\n"+
			"View (rendered by the wrong generic overlay):\n%s", m.View())
	}

	view := m.View()
	if !strings.Contains(view, "Select Agents") {
		t.Errorf("view does not contain expected title \"Select Agents\";\n"+
			"DeployAgentScreen must render this title in View().\n"+
			"Actual view:\n%s", view)
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
// selecting agents from two separate folders and confirming delivers all selected IDs in
// the MultiChoiceAnswer. The test navigates into each folder before toggling, which is
// required because the tree-based screen places agents inside folder nodes — toggling at
// the top level (which shows folder rows) would have no effect.
//
// Key sequence:
//   - Right: enter FolderA
//   - Space: select alpha-agent
//   - Esc: pop back to root (at a deeper level Esc pops rather than cancels)
//   - Down: move cursor to FolderB
//   - Right: enter FolderB
//   - Space: select beta-agent
//   - Enter: confirm cumulative selection from inside FolderB
func TestMultiSelectRoundTrip_QDeployAgents_MultipleSelectionsDeliverAllIDs(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, twoFolderAgentOptions())
	m.Update(qMsg)

	// Precondition: the dedicated DeployAgentScreen must be active.
	if m.selectOverlay != nil {
		t.Fatalf("precondition: selectOverlay is non-nil after QDeployAgents;\n"+
			"the dedicated DeployAgentScreen must be used — wire QDeployAgents to\n"+
			"NewDeployAgentScreen in questionSelectMany")
	}

	// Navigate into FolderA, select alpha-agent, back out to root.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})                      // enter FolderA
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select alpha-agent
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                        // pop back to root (deeper Esc = pop)

	// Navigate into FolderB, select beta-agent, confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})                       // move cursor to FolderB
	m.Update(tea.KeyMsg{Type: tea.KeyRight})                      // enter FolderB
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select beta-agent
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})                      // confirm cumulative selection

	if m.screen != screenRunning {
		t.Errorf("screen = %v after confirming agent selection; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Answered {
			t.Errorf("reply Status = %q; want Answered", ans.multiChoiceAns.Status)
		}
		ids := ans.multiChoiceAns.OptionIDs
		if len(ids) != 2 {
			t.Errorf("reply contains %d IDs %v; want 2 ([alpha-agent beta-agent]) — "+
				"multi-selection across folders must accumulate across the whole tree, "+
				"not reset when navigating between folders",
				len(ids), ids)
			return
		}
		// SelectedIDs() order follows AgentKeys() traversal (FolderA before FolderB).
		if ids[0] != "alpha-agent" || ids[1] != "beta-agent" {
			t.Errorf("reply IDs = %v; want [alpha-agent beta-agent]", ids)
		}
	default:
		t.Error("reply channel has no message after confirming agent selection")
	}
}

// TestMultiSelectRoundTrip_QDeployAgents_Esc_ProducesCancelled verifies that pressing Esc
// at the top level of the DeployAgentScreen sends a MultiChoiceAnswer with Status=Cancelled.
//
// The test navigates into a folder and selects an agent before cancelling, confirming that
// Esc at a deeper level pops rather than cancels, and that only Esc at the top level cancels.
//
// Key sequence:
//   - Right: enter FolderA (now at depth 2 — Esc here will pop, not cancel)
//   - Space: select alpha-agent
//   - Esc: pop back to root (top level)
//   - Esc: cancel at top level — must produce Status=Cancelled, not deliver the selection
func TestMultiSelectRoundTrip_QDeployAgents_Esc_ProducesCancelled(t *testing.T) {
	m := newRoutingModel()
	qMsg := buildSelectManyMsg(domain.QDeployAgents, twoFolderAgentOptions())
	m.Update(qMsg)

	// Precondition: the dedicated DeployAgentScreen must be active.
	if m.selectOverlay != nil {
		t.Fatalf("precondition: selectOverlay is non-nil after QDeployAgents;\n"+
			"wire QDeployAgents to NewDeployAgentScreen in questionSelectMany")
	}

	// Enter FolderA, select alpha-agent, Esc pops to root, Esc at root cancels.
	m.Update(tea.KeyMsg{Type: tea.KeyRight})                      // enter FolderA (deeper level)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}) // select alpha-agent
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                        // pop back to root
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})                        // cancel at top level

	if m.screen != screenRunning {
		t.Errorf("screen = %v after Esc at top level; want screenRunning", m.screen)
	}

	select {
	case ans := <-qMsg.reply:
		if ans.multiChoiceAns.Status != domain.Cancelled {
			t.Errorf("reply Status = %q after Esc at top level; want Cancelled — "+
				"Esc at the top level must not deliver any selection, even a partial one",
				ans.multiChoiceAns.Status)
		}
	default:
		t.Error("reply channel has no message after Esc at top level of DeployAgentScreen")
	}
}

// ---------------------------------------------------------------------------
// startService dispatch — new modes call correct service methods
// ---------------------------------------------------------------------------

// spyDispatchService records which service method is called, for dispatch tests.
type spyDispatchService struct {
	harnesses          []domain.HarnessRef
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

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})  // harness
	m.Update(tea.KeyMsg{Type: tea.KeyDown})           // → position 1
	m.Update(tea.KeyMsg{Type: tea.KeyDown})           // → position 2
	m.Update(tea.KeyMsg{Type: tea.KeyDown})           // → position 3
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})  // select mode

	if m.screen != screenWorkspace || m.selections.mode != domain.ModeDeployAgents {
		t.Skip("deploy-agents not yet at position 3; test will pass after implementation")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // workspace
	_, _ = m.Update(runCmd(cmd))

	if spy.deployNewCalled > 0 {
		t.Errorf("svc.DeployNew called %d time(s) for deploy-agents run; must be 0", spy.deployNewCalled)
	}
}
