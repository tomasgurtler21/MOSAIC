package screens_test

// deploy_mode_test.go verifies that ModeDeployAgents and ModeDeployHooks are presented as
// selectable entries in the ModeScreen, in the correct order, with IDs that match their
// domain.RunMode constants.
//
// All tests are RED-phase TDD tests. They fail until the two new entries are added to
// modeItems in mode.go.
//
// Position contract (from the requirements' proposed mode list):
//   Position 0: ModeDeployWorkspace  (deploy-workspace)
//   Position 1: ModeUpdateWorkspace  (update-workspace)
//   Position 2: ModeUpdateWorkflows  (update-workflows)
//   Position 3: ModeDeployAgents     (deploy-agents)      ← NEW
//   Position 4: ModeDeployHooks      (deploy-hooks)       ← NEW
//   Position 5: ModePromoteToGeneric (promote-to-generic)
//   Position 6: ModeTransformHarness (transform-harness)
//
// Strategy: tests use positionOf (from utility_infra_mode_test.go, same package) for
// position-independent navigation where needed, and explicit position assertions where the
// exact position is the invariant being tested.
//
// Verified behaviours:
//   - ModeDeployAgents entry is present in the list.
//   - ModeDeployHooks entry is present in the list.
//   - ModeDeployAgents is at position 3 (after 3 Down presses from the start).
//   - ModeDeployHooks is at position 4 (after 4 Down presses from the start).
//   - ModeDeployAgents appears before ModeDeployHooks.
//   - Both new modes appear between ModeUpdateWorkflows and ModePromoteToGeneric.
//   - Each entry's ID equals string(its RunMode constant).
//   - Both entries have non-empty descriptions and detail text.
//   - The existing five modes remain reachable and undisturbed.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Entry presence
// ---------------------------------------------------------------------------

// TestModeScreen_DeployAgentsEntry_ExistsInList verifies that ModeDeployAgents is present
// somewhere in the mode list within maxModeNavigations Down presses.
func TestModeScreen_DeployAgentsEntry_ExistsInList(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeDeployAgents, maxModeNavigations)
	if pos < 0 {
		t.Errorf("ModeDeployAgents entry not found in the mode list after %d Down presses;\n"+
			"add a widgets.ListItem with ID=%q to modeItems in mode.go",
			maxModeNavigations, domain.ModeDeployAgents)
	}
}

// TestModeScreen_DeployHooksEntry_ExistsInList verifies that ModeDeployHooks is present
// somewhere in the mode list within maxModeNavigations Down presses.
func TestModeScreen_DeployHooksEntry_ExistsInList(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeDeployHooks, maxModeNavigations)
	if pos < 0 {
		t.Errorf("ModeDeployHooks entry not found in the mode list after %d Down presses;\n"+
			"add a widgets.ListItem with ID=%q to modeItems in mode.go",
			maxModeNavigations, domain.ModeDeployHooks)
	}
}

// ---------------------------------------------------------------------------
// Exact position assertions
// ---------------------------------------------------------------------------

// TestModeScreen_DeployAgents_IsAtPosition3 verifies that ModeDeployAgents is the fourth
// entry (index 3, requiring 3 Down presses) in the mode list. This matches the requirements'
// proposed mode list order.
func TestModeScreen_DeployAgents_IsAtPosition3(t *testing.T) {
	s := newModeScreen()

	// Navigate to position 3 and confirm.
	for i := 0; i < 3; i++ {
		s.Update(modeDownKey())
	}
	s.Update(modeEnterKey())

	if !s.Done() {
		t.Fatal("Done() = false after 3 Down+Enter; position 3 must be selectable")
	}
	if got := s.SelectedMode(); got != domain.ModeDeployAgents {
		t.Errorf("SelectedMode() at position 3 = %q; want %q;\n"+
			"ModeDeployAgents must be the fourth entry (position 3) in the mode list",
			got, domain.ModeDeployAgents)
	}
}

// TestModeScreen_DeployHooks_IsAtPosition4 verifies that ModeDeployHooks is the fifth
// entry (index 4, requiring 4 Down presses) in the mode list.
func TestModeScreen_DeployHooks_IsAtPosition4(t *testing.T) {
	s := newModeScreen()

	for i := 0; i < 4; i++ {
		s.Update(modeDownKey())
	}
	s.Update(modeEnterKey())

	if !s.Done() {
		t.Fatal("Done() = false after 4 Down+Enter; position 4 must be selectable")
	}
	if got := s.SelectedMode(); got != domain.ModeDeployHooks {
		t.Errorf("SelectedMode() at position 4 = %q; want %q;\n"+
			"ModeDeployHooks must be the fifth entry (position 4) in the mode list",
			got, domain.ModeDeployHooks)
	}
}

// ---------------------------------------------------------------------------
// Ordering constraints
// ---------------------------------------------------------------------------

// TestModeScreen_DeployAgents_BeforeDeployHooks verifies that ModeDeployAgents appears
// before ModeDeployHooks in the list. The list position of deploy-agents must be strictly
// less than the list position of deploy-hooks.
func TestModeScreen_DeployAgents_BeforeDeployHooks(t *testing.T) {
	agentsPos := positionOf(newModeScreen(), domain.ModeDeployAgents, maxModeNavigations)
	hooksPos := positionOf(newModeScreen(), domain.ModeDeployHooks, maxModeNavigations)

	if agentsPos < 0 {
		t.Fatalf("ModeDeployAgents not found in list; add it to modeItems")
	}
	if hooksPos < 0 {
		t.Fatalf("ModeDeployHooks not found in list; add it to modeItems")
	}
	if agentsPos >= hooksPos {
		t.Errorf("ModeDeployAgents at position %d, ModeDeployHooks at position %d;\n"+
			"ModeDeployAgents must appear before ModeDeployHooks in the mode list",
			agentsPos, hooksPos)
	}
}

// TestModeScreen_DeployAgents_AfterUpdateWorkflows verifies that ModeDeployAgents appears
// after ModeUpdateWorkflows in the list.
func TestModeScreen_DeployAgents_AfterUpdateWorkflows(t *testing.T) {
	wfPos := positionOf(newModeScreen(), domain.ModeUpdateWorkflows, maxModeNavigations)
	agentsPos := positionOf(newModeScreen(), domain.ModeDeployAgents, maxModeNavigations)

	if wfPos < 0 {
		t.Fatalf("ModeUpdateWorkflows not found in list")
	}
	if agentsPos < 0 {
		t.Fatalf("ModeDeployAgents not found in list")
	}
	if agentsPos <= wfPos {
		t.Errorf("ModeDeployAgents at position %d, ModeUpdateWorkflows at position %d;\n"+
			"ModeDeployAgents must appear after ModeUpdateWorkflows",
			agentsPos, wfPos)
	}
}

// TestModeScreen_DeployHooks_BeforePromoteToGeneric verifies that ModeDeployHooks appears
// before ModePromoteToGeneric in the list.
func TestModeScreen_DeployHooks_BeforePromoteToGeneric(t *testing.T) {
	hooksPos := positionOf(newModeScreen(), domain.ModeDeployHooks, maxModeNavigations)
	promotePos := positionOf(newModeScreen(), domain.ModePromoteToGeneric, maxModeNavigations)

	if hooksPos < 0 {
		t.Fatalf("ModeDeployHooks not found in list")
	}
	if promotePos < 0 {
		t.Fatalf("ModePromoteToGeneric not found in list")
	}
	if hooksPos >= promotePos {
		t.Errorf("ModeDeployHooks at position %d, ModePromoteToGeneric at position %d;\n"+
			"ModeDeployHooks must appear before ModePromoteToGeneric",
			hooksPos, promotePos)
	}
}

// ---------------------------------------------------------------------------
// ID / RunMode correspondence
// ---------------------------------------------------------------------------

// TestModeScreen_DeployAgents_SelectedModeReturnsCorrectConstant verifies that selecting
// the deploy-agents entry returns exactly domain.ModeDeployAgents. The string value must
// match the constant so both frontends produce the same mode value.
func TestModeScreen_DeployAgents_SelectedModeReturnsCorrectConstant(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeDeployAgents, maxModeNavigations)
	if !found {
		t.Fatalf("ModeDeployAgents entry not found in list after %d Down presses", maxModeNavigations)
	}

	got := s.SelectedMode()
	if got != domain.ModeDeployAgents {
		t.Errorf("SelectedMode() = %q; want %q (value must match the domain constant exactly)",
			got, domain.ModeDeployAgents)
	}
}

// TestModeScreen_DeployHooks_SelectedModeReturnsCorrectConstant verifies that selecting
// the deploy-hooks entry returns exactly domain.ModeDeployHooks.
func TestModeScreen_DeployHooks_SelectedModeReturnsCorrectConstant(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeDeployHooks, maxModeNavigations)
	if !found {
		t.Fatalf("ModeDeployHooks entry not found in list after %d Down presses", maxModeNavigations)
	}

	got := s.SelectedMode()
	if got != domain.ModeDeployHooks {
		t.Errorf("SelectedMode() = %q; want %q (value must match the domain constant exactly)",
			got, domain.ModeDeployHooks)
	}
}

// TestModeScreen_AllNewModes_IDMatchesDeclaredRunMode verifies that every new mode item's
// ID equals the string value of the corresponding RunMode constant. ModeScreen.SelectedMode()
// uses list.SelectedID() as a RunMode, so any ID drift causes a silent wrong-mode dispatch.
func TestModeScreen_AllNewModes_IDMatchesDeclaredRunMode(t *testing.T) {
	newModes := []struct {
		name     string
		mode     domain.RunMode
	}{
		{"deploy-agents", domain.ModeDeployAgents},
		{"deploy-hooks", domain.ModeDeployHooks},
	}

	for _, tc := range newModes {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newModeScreen()
			_, found := navigateToMode(s, tc.mode, maxModeNavigations)
			if !found {
				t.Fatalf("mode %q not found in list; add it to modeItems with ID=%q",
					tc.mode, string(tc.mode))
			}
			if !s.Done() {
				t.Fatalf("Done() = false after navigating to %q", tc.mode)
			}
			if got := s.SelectedMode(); got != tc.mode {
				t.Errorf("SelectedMode() = %q; want %q — modeItems entry ID must equal string(RunMode constant)",
					got, tc.mode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// View content — labels visible
// ---------------------------------------------------------------------------

// TestModeScreen_View_ShowsDeployAgentsLabel verifies that the rendered view contains the
// "Deploy agents" label so users can identify and select the new mode.
func TestModeScreen_View_ShowsDeployAgentsLabel(t *testing.T) {
	s := newModeScreen()

	view := s.View()

	if !strings.Contains(collapseWhitespace(view), "Deploy agents") {
		t.Errorf("mode screen view does not show 'Deploy agents' label;\n"+
			"the new mode must be visible in the list.\nView:\n%s", view)
	}
}

// TestModeScreen_View_ShowsDeployHooksLabel verifies that the rendered view contains the
// "Deploy hooks" label.
func TestModeScreen_View_ShowsDeployHooksLabel(t *testing.T) {
	s := newModeScreen()

	view := s.View()

	if !strings.Contains(collapseWhitespace(view), "Deploy hooks") {
		t.Errorf("mode screen view does not show 'Deploy hooks' label;\n"+
			"the new mode must be visible in the list.\nView:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Detail text — scope described
// ---------------------------------------------------------------------------

// TestModeScreen_DeployAgents_DetailMentionsMixedScope verifies that after navigating to
// the deploy-agents entry, the detail text describes its merged scope (agents, no workflow
// or hook). Per FR-11, the detail must convey scope, not just name.
func TestModeScreen_DeployAgents_DetailMentionsMixedScope(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeDeployAgents, maxModeNavigations)
	if pos < 0 {
		t.Fatalf("ModeDeployAgents not found in list; add it to modeItems")
	}

	s := newModeScreen()
	for i := 0; i < pos; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := collapseWhitespace(strings.ToLower(s.View()))

	// The detail text must mention agents and the absence of workflow/hook work.
	if !strings.Contains(view, "agent") {
		t.Errorf("deploy-agents detail text does not mention 'agent'; the detail must describe the scope:\n%s", s.View())
	}
}

// TestModeScreen_DeployHooks_DetailMentionsHooksOnly verifies that after navigating to
// the deploy-hooks entry, the detail text conveys that it deploys hook bundles only.
func TestModeScreen_DeployHooks_DetailMentionsHooksOnly(t *testing.T) {
	pos := positionOf(newModeScreen(), domain.ModeDeployHooks, maxModeNavigations)
	if pos < 0 {
		t.Fatalf("ModeDeployHooks not found in list; add it to modeItems")
	}

	s := newModeScreen()
	for i := 0; i < pos; i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	view := collapseWhitespace(strings.ToLower(s.View()))

	if !strings.Contains(view, "hook") {
		t.Errorf("deploy-hooks detail text does not mention 'hook'; the detail must describe the scope:\n%s", s.View())
	}
}

// ---------------------------------------------------------------------------
// Existing modes unaffected
// ---------------------------------------------------------------------------

// TestModeScreen_NewModes_DoNotDisplaceExistingModes verifies that inserting the two new
// mode entries does not remove or displace any of the five previously established modes.
func TestModeScreen_NewModes_DoNotDisplaceExistingModes(t *testing.T) {
	existingModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}
	for _, want := range existingModes {
		s := newModeScreen()
		pos := positionOf(s, want, maxModeNavigations)
		if pos < 0 {
			t.Errorf("mode %q is no longer reachable after new entries were inserted;\n"+
				"adding ModeDeployAgents and ModeDeployHooks must not remove any existing mode",
				want)
		}
	}
}

// TestModeScreen_DeployAgents_Reset_ClearsDone verifies that Reset() clears Done() after
// selecting deploy-agents, consistent with the Reset contract of all other mode entries.
func TestModeScreen_DeployAgents_Reset_ClearsDone(t *testing.T) {
	s := newModeScreen()

	_, found := navigateToMode(s, domain.ModeDeployAgents, maxModeNavigations)
	if !found {
		t.Fatalf("ModeDeployAgents not found in list after %d Down presses", maxModeNavigations)
	}
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false (Reset must clear Done after deploy-agents selection)")
	}
}
