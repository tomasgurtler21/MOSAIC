package screens_test

// deployagents_test.go verifies the DeployAgentScreen tree-navigation contract:
//
//   - Top level shows the tree's root folders as browsable rows and does NOT show
//     the agents nested inside those folders.
//   - →/l enters the focused folder and reveals its contents.
//   - ←/h and Esc below the top level return one level with the previously focused
//     level restored; selections are preserved.
//   - Depth is data-driven: a synthetic four-level fixture can be walked all the
//     way down and back out.
//   - A level holding only agents presents them as toggleable rows.
//   - A level holding both child folders and own agents shows child folders first,
//     then that level's own agents, with both kinds interactable.
//   - Space toggles an agent; Enter confirms from any level and sets Done(); the
//     confirmed SelectedIDs() contains only agent keys, never folder names.
//   - Selections made in one folder persist after navigating out to another folder
//     and back, and appear together in SelectedIDs() after confirming.
//   - Esc at the top level sets Back() — cancels the whole screen.
//   - Enter with an empty selection does not confirm and shows MsgNoAgentsSelected.
//   - Empty tree renders an informative empty state; only Esc is handled there.
//   - Error state renders the error message; only Esc is handled there.
//   - Reset() clears Done() and Back().
//   - Tab is inert at every level.
//   - Resize does not panic.
//
// All fixtures use synthetic folder/agent names proving no catalog name is
// hardcoded in the screen. No test reads from Catalog/.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Fixtures — synthetic names prove no hardcoding
// ---------------------------------------------------------------------------

// twoLevelRoot is a root with two top-level folders, each containing only agents.
// Used for basic navigation, toggle, and confirm tests.
//
//	root
//	├── SynthRole-Alpha  (agent-alpha-one, agent-alpha-two)
//	└── SynthRole-Beta   (agent-beta-one)
var twoLevelRoot = &screens.AgentTreeNode{
	Children: []*screens.AgentTreeNode{
		{
			Name: "SynthRole-Alpha",
			Agents: []domain.Agent{
				{Key: "agent-alpha-one", Name: "Alpha One"},
				{Key: "agent-alpha-two", Name: "Alpha Two"},
			},
		},
		{
			Name: "SynthRole-Beta",
			Agents: []domain.Agent{
				{Key: "agent-beta-one", Name: "Beta One"},
			},
		},
	},
}

// fourLevelRoot is four folders deep. Used to verify depth is data-driven and
// no fixed-depth assumption exists in the navigation state.
//
//	root
//	└── Level-A
//	    └── Level-B
//	        └── Level-C
//	            └── Level-D (agent-deep)
var fourLevelRoot = &screens.AgentTreeNode{
	Children: []*screens.AgentTreeNode{
		{
			Name: "Level-A",
			Children: []*screens.AgentTreeNode{
				{
					Name: "Level-B",
					Children: []*screens.AgentTreeNode{
						{
							Name: "Level-C",
							Children: []*screens.AgentTreeNode{
								{
									Name:   "Level-D",
									Agents: []domain.Agent{{Key: "agent-deep", Name: "Deep Agent"}},
								},
							},
						},
					},
				},
			},
		},
	},
}

// mixedRoot contains one folder that itself holds both a child folder and its own
// direct agents — the "mixed shape". Used to verify folder-rows-first ordering
// and that folder rows are entered (not toggled) while agent rows are toggled.
//
//	root
//	└── SynthContainer
//	    ├── SynthSubfolder (agent-subfolder-one)
//	    └── (own agents) agent-direct-one
var mixedRoot = &screens.AgentTreeNode{
	Children: []*screens.AgentTreeNode{
		{
			Name: "SynthContainer",
			Children: []*screens.AgentTreeNode{
				{
					Name:   "SynthSubfolder",
					Agents: []domain.Agent{{Key: "agent-subfolder-one", Name: "Subfolder One"}},
				},
			},
			Agents: []domain.Agent{
				{Key: "agent-direct-one", Name: "Direct One"},
			},
		},
	},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newDAScreen(root *screens.AgentTreeNode) *screens.DeployAgentScreen {
	return screens.NewDeployAgentScreen(root, 80, 24, plainStyles(), "")
}

func sendDAKey(s *screens.DeployAgentScreen, keyType tea.KeyType) {
	s.Update(tea.KeyMsg{Type: keyType})
}

func sendDARune(s *screens.DeployAgentScreen, r rune) {
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// enterFolder navigates into the focused folder using the Right arrow key.
func enterDAFolder(s *screens.DeployAgentScreen) { sendDAKey(s, tea.KeyRight) }

// leaveFolder returns to the parent level using the Left arrow key.
func leaveDAFolder(s *screens.DeployAgentScreen) { sendDAKey(s, tea.KeyLeft) }

// toggleDA toggles the focused row's selection with Space.
func toggleDA(s *screens.DeployAgentScreen) { sendDARune(s, ' ') }

// confirmDA attempts to confirm the selection with Enter.
func confirmDA(s *screens.DeployAgentScreen) { sendDAKey(s, tea.KeyEnter) }

// ---------------------------------------------------------------------------
// Top level: shows root folders, not nested agents
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_TopLevel_ShowsRootFolderNames verifies that the initial
// view contains the top-level folder names from the tree. A screen that hardcodes
// catalog names would fail to show the synthetic names used here.
func TestDeployAgentScreen_TopLevel_ShowsRootFolderNames(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	view := s.View()

	if !strings.Contains(view, "SynthRole-Alpha") {
		t.Errorf("top-level view must contain first root folder name 'SynthRole-Alpha';\n"+
			"the screen must render whatever folder names the tree supplies — no hardcoding.\nView:\n%s", view)
	}
	if !strings.Contains(view, "SynthRole-Beta") {
		t.Errorf("top-level view must contain second root folder name 'SynthRole-Beta';\n"+
			"all root folders must be visible at the top level.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_TopLevel_DoesNotShowNestedAgents verifies that agents
// nested inside folders are NOT shown at the top level. The top level shows
// only folder rows; agents are revealed after entering a folder.
func TestDeployAgentScreen_TopLevel_DoesNotShowNestedAgents(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	view := s.View()

	if strings.Contains(view, "Alpha One") {
		t.Errorf("top-level view must NOT show agents nested inside folders;\n"+
			"'Alpha One' (inside SynthRole-Alpha) must not be visible before entering that folder.\nView:\n%s", view)
	}
	if strings.Contains(view, "Alpha Two") {
		t.Errorf("top-level view must NOT show agents nested inside folders;\n"+
			"'Alpha Two' (inside SynthRole-Alpha) must not be visible before entering that folder.\nView:\n%s", view)
	}
	if strings.Contains(view, "Beta One") {
		t.Errorf("top-level view must NOT show agents nested inside folders;\n"+
			"'Beta One' (inside SynthRole-Beta) must not be visible before entering that folder.\nView:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Entering and leaving folders
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Right_EntersFocusedFolder verifies that the Right arrow
// key enters the focused folder and reveals its contents.
func TestDeployAgentScreen_Right_EntersFocusedFolder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // Right on SynthRole-Alpha (focused by default)

	view := s.View()
	if !strings.Contains(view, "Alpha One") {
		t.Errorf("after Right at top level, must show agents inside the focused folder;\n"+
			"'Alpha One' (inside SynthRole-Alpha) should be visible.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_L_EntersFocusedFolder verifies that the vim-style 'l'
// key enters the focused folder (same as Right arrow).
func TestDeployAgentScreen_L_EntersFocusedFolder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	sendDARune(s, 'l') // vim-style open folder

	view := s.View()
	if !strings.Contains(view, "Alpha One") {
		t.Errorf("after 'l' at top level, must show agents inside the focused folder;\n"+
			"'Alpha One' (inside SynthRole-Alpha) should be visible.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_Left_ReturnsToParentLevel verifies that the Left arrow
// key exits a folder and returns to the parent level, restoring its content.
//
// Vacuously passes in RED phase because the stub's Update ignores Left/Right, so
// enterDAFolder and leaveDAFolder are no-ops and the test never leaves the top level.
// This becomes a meaningful regression guard once navigation is implemented.
func TestDeployAgentScreen_Left_ReturnsToParentLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter SynthRole-Alpha
	leaveDAFolder(s) // Left — back to top level

	view := s.View()
	if !strings.Contains(view, "SynthRole-Alpha") {
		t.Errorf("after Left inside folder, must return to top level showing 'SynthRole-Alpha';\nView:\n%s", view)
	}
	if strings.Contains(view, "Alpha One") {
		t.Errorf("after returning to top level via Left, 'Alpha One' must not be visible "+
			"(it lives inside SynthRole-Alpha, not at top level).\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_H_ReturnsToParentLevel verifies that the vim-style 'h'
// key exits a folder (same as Left arrow).
//
// Vacuously passes in RED phase because the stub ignores navigation keys; becomes a
// meaningful regression guard once navigation is implemented.
func TestDeployAgentScreen_H_ReturnsToParentLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s)  // enter SynthRole-Alpha
	sendDARune(s, 'h') // vim-style go back

	view := s.View()
	if !strings.Contains(view, "SynthRole-Alpha") {
		t.Errorf("after 'h' inside folder, must return to top level showing 'SynthRole-Alpha';\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_EscInsideFolder_GoesBackOneLevel verifies that Esc inside
// a folder returns to the parent level without cancelling the whole screen.
// This is distinct from Esc at the top level, which does cancel.
func TestDeployAgentScreen_EscInsideFolder_GoesBackOneLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s)             // enter SynthRole-Alpha
	sendDAKey(s, tea.KeyEsc)    // Esc inside folder = go back one level

	if s.Back() {
		t.Error("Back() = true after Esc inside a folder; Esc inside a folder should go back " +
			"one level, not cancel the whole screen — only Esc at the top level cancels.")
	}
	if s.Done() {
		t.Error("Done() = true after Esc inside folder; want false")
	}
	view := s.View()
	if !strings.Contains(view, "SynthRole-Alpha") {
		t.Errorf("after Esc inside folder, must show parent level with 'SynthRole-Alpha';\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_EscAtTopLevel_SetsBack verifies that Esc at the top level
// cancels the whole screen by setting Back()=true, consistent with the other
// selection screens (WorkflowBrowserScreen, UtilityAgentScreen, etc.).
func TestDeployAgentScreen_EscAtTopLevel_SetsBack(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	sendDAKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc at top level; want true (top-level Esc must cancel the screen)")
	}
	if s.Done() {
		t.Error("Done() = true after Esc; Esc must not confirm the selection")
	}
}

// TestDeployAgentScreen_EscAtTopLevel_DoesNotDeliverSelection verifies that
// pressing Esc after toggling some agents does not confirm (Done() stays false).
//
// Vacuously passes in RED phase: enterDAFolder/toggleDA/leaveDAFolder are no-ops in
// the stub, so the toggle never happens; Esc sets Back() correctly even in the stub.
// Becomes a meaningful regression guard once navigation and toggle are implemented.
func TestDeployAgentScreen_EscAtTopLevel_DoesNotDeliverSelection(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter folder to reach toggleable agents
	toggleDA(s)      // toggle one agent
	leaveDAFolder(s) // back to top level
	sendDAKey(s, tea.KeyEsc) // cancel

	if s.Done() {
		t.Error("Done() = true after partial selection + top-level Esc; Esc must not confirm")
	}
	if !s.Back() {
		t.Error("Back() = false after top-level Esc; want true")
	}
}

// ---------------------------------------------------------------------------
// Arbitrary depth: four-level tree
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_FourLevels_CanWalkAllTheWayDown verifies that a tree four
// levels deep can be navigated down to the leaf level. Any fixed-depth implementation
// (e.g. a two-value enum) will fail because it cannot represent depth 4.
func TestDeployAgentScreen_FourLevels_CanWalkAllTheWayDown(t *testing.T) {
	s := newDAScreen(fourLevelRoot)

	// Walk: root → Level-A → Level-B → Level-C → Level-D
	enterDAFolder(s) // enter Level-A
	enterDAFolder(s) // enter Level-B
	enterDAFolder(s) // enter Level-C
	enterDAFolder(s) // enter Level-D

	view := s.View()
	if !strings.Contains(view, "Deep Agent") {
		t.Errorf("after navigating four levels deep, must show 'Deep Agent' inside Level-D;\n"+
			"a fixed-depth implementation will fail here because it cannot reach level 4.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_FourLevels_CanWalkAllTheWayBack verifies that after
// navigating four levels deep, pressing Left/Esc four times returns to the top level.
//
// Vacuously passes in RED phase because the stub ignores Right/Left, so all
// enterDAFolder/leaveDAFolder calls are no-ops and the test remains at the top level
// the whole time. Becomes a meaningful depth-regression guard once navigation lands.
func TestDeployAgentScreen_FourLevels_CanWalkAllTheWayBack(t *testing.T) {
	s := newDAScreen(fourLevelRoot)

	// Walk down to Level-D.
	enterDAFolder(s) // enter Level-A
	enterDAFolder(s) // enter Level-B
	enterDAFolder(s) // enter Level-C
	enterDAFolder(s) // enter Level-D

	// Walk all the way back out.
	leaveDAFolder(s) // back to Level-C (showing Level-D folder)
	leaveDAFolder(s) // back to Level-B (showing Level-C folder)
	leaveDAFolder(s) // back to Level-A (showing Level-B folder)
	leaveDAFolder(s) // back to root    (showing Level-A folder)

	view := s.View()
	if !strings.Contains(view, "Level-A") {
		t.Errorf("after walking four levels down and four back, must show top-level folder 'Level-A';\nView:\n%s", view)
	}
	if strings.Contains(view, "Deep Agent") {
		t.Errorf("after returning to top level, 'Deep Agent' must not be visible (it is inside Level-D);\nView:\n%s", view)
	}
	if s.Back() {
		t.Error("Back() = true after navigating back to the top level via Left; " +
			"Back() must not be set until the user presses Esc at the top level")
	}
}

// ---------------------------------------------------------------------------
// Agent-only level: toggle and confirm
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_AgentLevel_SpaceTogglesAgent verifies that Space toggles
// the focused agent row inside a folder. After confirming, SelectedIDs() must
// contain the toggled agent's key.
func TestDeployAgentScreen_AgentLevel_SpaceTogglesAgent(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter SynthRole-Alpha
	toggleDA(s)      // Space on agent-alpha-one
	confirmDA(s)     // Enter

	if !s.Done() {
		t.Fatal("Done() = false after toggle+Enter inside a folder; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) == 0 {
		t.Fatal("SelectedIDs() is empty after toggling an agent; want at least one key")
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet["agent-alpha-one"] {
		t.Errorf("SelectedIDs() = %v; want 'agent-alpha-one' to be present — "+
			"Space on the first focused row inside SynthRole-Alpha must toggle 'agent-alpha-one'", ids)
	}
}

// TestDeployAgentScreen_AgentLevel_SelectedIDsContainsAgentKeysOnly verifies that
// SelectedIDs() never contains folder names, heading IDs, or empty strings —
// only agent keys cross the screen boundary.
func TestDeployAgentScreen_AgentLevel_SelectedIDsContainsAgentKeysOnly(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter SynthRole-Alpha
	toggleDA(s)      // select agent-alpha-one
	confirmDA(s)     // confirm

	if !s.Done() {
		t.Fatal("precondition: Done() must be true after toggle+Enter")
	}
	for _, id := range s.SelectedIDs() {
		if id == "" {
			t.Errorf("SelectedIDs() contains empty string; want agent keys only")
		}
		if strings.HasPrefix(id, "SynthRole") {
			t.Errorf("SelectedIDs() contains folder name %q; folder names must never appear in SelectedIDs()", id)
		}
		if strings.HasPrefix(id, "dir:") {
			t.Errorf("SelectedIDs() contains internal folder row ID %q; only agent keys must be returned", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Confirm from any level
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Enter_ConfirmsFromInsideFolder verifies that Enter sets
// Done()=true when called from inside a folder (not just from the top level).
func TestDeployAgentScreen_Enter_ConfirmsFromInsideFolder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter SynthRole-Alpha
	toggleDA(s)      // select agent-alpha-one
	confirmDA(s)     // Enter from inside the folder

	if !s.Done() {
		t.Error("Done() = false after Enter from inside a folder; Enter must confirm from any depth, " +
			"not only from the top level")
	}
}

// TestDeployAgentScreen_Enter_ConfirmsFromTopLevel verifies that Enter sets
// Done()=true when called from the top level (folder view) after selections were
// made in a sub-folder on a previous visit.
func TestDeployAgentScreen_Enter_ConfirmsFromTopLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s) // enter SynthRole-Alpha
	toggleDA(s)      // select agent-alpha-one
	leaveDAFolder(s) // back to top level
	confirmDA(s)     // Enter from top level

	if !s.Done() {
		t.Error("Done() = false after navigating back to top level and pressing Enter; " +
			"Enter must confirm the cumulative selection regardless of current depth")
	}
	if len(s.SelectedIDs()) == 0 {
		t.Error("SelectedIDs() is empty after confirming from top level; " +
			"the selection made in SynthRole-Alpha must survive navigation back to the top")
	}
}

// ---------------------------------------------------------------------------
// Cross-folder selection persistence
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_SelectionPersistsAcrossFolders verifies that selections
// made in one folder survive navigating out to another folder and back, and that
// the cumulative selection is returned together by SelectedIDs().
func TestDeployAgentScreen_SelectionPersistsAcrossFolders(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	// Select an agent in SynthRole-Alpha.
	enterDAFolder(s) // enter SynthRole-Alpha
	toggleDA(s)      // select agent-alpha-one
	leaveDAFolder(s) // back to top level

	// Navigate to SynthRole-Beta and select an agent there.
	sendDAKey(s, tea.KeyDown) // move cursor to SynthRole-Beta
	enterDAFolder(s)          // enter SynthRole-Beta
	toggleDA(s)               // select agent-beta-one

	// Confirm from inside SynthRole-Beta.
	confirmDA(s)

	if !s.Done() {
		t.Fatal("Done() = false after cross-folder selection + Enter; want true")
	}
	ids := s.SelectedIDs()
	if len(ids) < 2 {
		t.Errorf("SelectedIDs() = %v (len %d); want at least 2 (one from each folder); "+
			"selections from SynthRole-Alpha must have survived navigating into SynthRole-Beta",
			ids, len(ids))
	}
	// Verify both keys are present.
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet["agent-alpha-one"] {
		t.Errorf("SelectedIDs() = %v; want 'agent-alpha-one' from SynthRole-Alpha to be present "+
			"after navigating away and confirming from SynthRole-Beta", ids)
	}
	if !idSet["agent-beta-one"] {
		t.Errorf("SelectedIDs() = %v; want 'agent-beta-one' from SynthRole-Beta to be present", ids)
	}
}

// ---------------------------------------------------------------------------
// Empty-selection validation
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_EnterWithNoSelection_DoesNotConfirm verifies that pressing
// Enter without selecting any agents does not confirm (Done() stays false).
func TestDeployAgentScreen_EnterWithNoSelection_DoesNotConfirm(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	confirmDA(s) // Enter with no selection

	if s.Done() {
		t.Error("Done() = true after Enter with no selection; want false — " +
			"Enter must not confirm when no agents are selected")
	}
	if s.Back() {
		t.Error("Back() = true after Enter with no selection; want false — " +
			"Enter must show a validation message, not cancel")
	}
}

// TestDeployAgentScreen_EnterWithNoSelection_ShowsValidationMessage verifies that
// pressing Enter without selecting any agents renders MsgNoAgentsSelected in the
// view, so the user knows what to do next.
func TestDeployAgentScreen_EnterWithNoSelection_ShowsValidationMessage(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	confirmDA(s) // Enter with no selection

	view := s.View()
	if !strings.Contains(view, screens.MsgNoAgentsSelected) {
		t.Errorf("after Enter with no selection, view must contain MsgNoAgentsSelected;\n"+
			"want: %q\ngot view:\n%s", screens.MsgNoAgentsSelected, view)
	}
}

// TestDeployAgentScreen_ValidationMessage_ClearedAfterToggle verifies that the
// transient validation message is cleared after the user makes a selection, so
// it does not linger once an agent has been chosen.
func TestDeployAgentScreen_ValidationMessage_ClearedAfterToggle(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	// Trigger the validation message.
	enterDAFolder(s) // need to be in a folder that has agents
	confirmDA(s)     // Enter with no selection — shows validation message

	// Precondition: the message must actually be visible before we can test clearing it.
	// Without this check the test passes vacuously in RED phase (the stub never shows the
	// message, so the negative assertion trivially holds). Failing here gives the correct
	// RED signal that "show on Enter-with-no-selection" is not yet implemented.
	viewBefore := s.View()
	if !strings.Contains(viewBefore, screens.MsgNoAgentsSelected) {
		t.Fatalf("precondition failed: validation message %q must appear after Enter with no selection;\n"+
			"this test cannot verify clearing behavior unless the message appears first.\nView:\n%s",
			screens.MsgNoAgentsSelected, viewBefore)
	}

	// Now toggle an agent; the message should clear.
	toggleDA(s)

	view := s.View()
	if strings.Contains(view, screens.MsgNoAgentsSelected) {
		t.Errorf("validation message must be cleared after toggling an agent;\n"+
			"it must not linger once the user has made a selection.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_ValidationMessage_ClearedAfterNavigation verifies that the
// transient validation message is cleared when the user navigates (Right, Left, or
// Esc-inside-folder), covering the full design contract: "cleared on any navigation
// or toggle."
func TestDeployAgentScreen_ValidationMessage_ClearedAfterNavigation(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	// Trigger the validation message at the top level (no agents selected, press Enter).
	confirmDA(s)

	// Precondition: the message must be visible before we can test clearing it.
	viewBefore := s.View()
	if !strings.Contains(viewBefore, screens.MsgNoAgentsSelected) {
		t.Fatalf("precondition failed: validation message %q must appear after Enter with no selection;\n"+
			"this test cannot verify clearing-on-navigation unless the message appears first.\nView:\n%s",
			screens.MsgNoAgentsSelected, viewBefore)
	}

	// Navigate into a folder; the message must clear on navigation.
	enterDAFolder(s)

	view := s.View()
	if strings.Contains(view, screens.MsgNoAgentsSelected) {
		t.Errorf("validation message must be cleared after navigating into a folder;\n"+
			"the design contract states it clears on any navigation or toggle.\nView:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Mixed level: folders first, then agents
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_MixedLevel_ShowsFolderBeforeAgents verifies that when a
// level contains both child folders and its own direct agents, the child folders
// are shown above the agents in the rendered list. This is the ordering rule from
// the design: "folders (in Children order), then agents (in Agents order)".
func TestDeployAgentScreen_MixedLevel_ShowsFolderBeforeAgents(t *testing.T) {
	s := newDAScreen(mixedRoot)

	enterDAFolder(s) // enter SynthContainer (which has SynthSubfolder + agent-direct-one)

	view := s.View()
	subfolderIdx := strings.Index(view, "SynthSubfolder")
	agentIdx := strings.Index(view, "Direct One")

	if subfolderIdx < 0 {
		t.Fatalf("after entering SynthContainer, must show child folder 'SynthSubfolder';\nView:\n%s", view)
	}
	if agentIdx < 0 {
		t.Fatalf("after entering SynthContainer, must show direct agent 'Direct One';\nView:\n%s", view)
	}
	if agentIdx < subfolderIdx {
		t.Errorf("'Direct One' appears before 'SynthSubfolder' in the mixed level; "+
			"child folders must appear before agents in a mixed level.\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_MixedLevel_FolderRowEntered verifies that a folder row in a
// mixed level is entered with →, not toggled. The subfolder's agents must appear
// after entering it.
func TestDeployAgentScreen_MixedLevel_FolderRowEntered(t *testing.T) {
	s := newDAScreen(mixedRoot)

	enterDAFolder(s) // enter SynthContainer (cursor on SynthSubfolder, the first row)
	enterDAFolder(s) // enter SynthSubfolder

	view := s.View()
	if !strings.Contains(view, "Subfolder One") {
		t.Errorf("after entering SynthSubfolder inside the mixed level, must show 'Subfolder One';\nView:\n%s", view)
	}
}

// TestDeployAgentScreen_MixedLevel_FolderRowNotInSelectedIDs verifies that folder
// rows in a mixed level never appear in SelectedIDs(), even if the cursor was on a
// folder row when Enter was pressed (after making a valid selection elsewhere).
func TestDeployAgentScreen_MixedLevel_FolderRowNotInSelectedIDs(t *testing.T) {
	s := newDAScreen(mixedRoot)

	// Enter the mixed level and select the direct agent row (below the subfolder row).
	enterDAFolder(s)           // enter SynthContainer
	sendDAKey(s, tea.KeyDown)  // move cursor down past SynthSubfolder to Direct One
	toggleDA(s)                // toggle agent-direct-one
	// Confirm from this level (cursor may still be on a folder row after backing up).
	confirmDA(s)

	if !s.Done() {
		t.Fatal("Done() = false after toggle+Enter in mixed level; want true")
	}
	for _, id := range s.SelectedIDs() {
		if id == "SynthSubfolder" || id == "SynthContainer" || strings.HasPrefix(id, "dir:") {
			t.Errorf("SelectedIDs() contains %q which is a folder name or internal folder row ID; "+
				"folder rows must never appear in SelectedIDs()", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_EmptyTree_RendersInformativeMessage verifies that a nil
// root (no agents in the catalog) renders an informative message rather than a
// blank screen or a panic.
func TestDeployAgentScreen_EmptyTree_RendersInformativeMessage(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	view := s.View()

	if strings.TrimSpace(view) == "" {
		t.Error("empty-state view must not be blank; want an informational message for the user")
	}
}

// TestDeployAgentScreen_EmptyTree_EscSetsBack verifies that Esc in the empty state
// sets Back() so the user can leave the screen.
func TestDeployAgentScreen_EmptyTree_EscSetsBack(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	sendDAKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc in empty state; want true")
	}
}

// TestDeployAgentScreen_EmptyTree_EnterDoesNotConfirm verifies that Enter in the
// empty state does not set Done(). Only Esc is handled in the empty state.
func TestDeployAgentScreen_EmptyTree_EnterDoesNotConfirm(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	confirmDA(s)

	if s.Done() {
		t.Error("Done() = true after Enter in empty state; only Esc should be handled in the empty state")
	}
}

// TestDeployAgentScreen_EmptyTree_RightDoesNothing verifies that Right/enter-folder
// in the empty state does not panic and does not change any state.
func TestDeployAgentScreen_EmptyTree_RightDoesNothing(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Right in empty state panicked: %v", r)
		}
	}()

	enterDAFolder(s)
	if s.Done() || s.Back() {
		t.Error("Right in empty state must not set Done() or Back()")
	}
}

// ---------------------------------------------------------------------------
// Error state
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_ErrorState_RendersErrorMessage verifies that a non-empty
// errorMsg is rendered in the view.
func TestDeployAgentScreen_ErrorState_RendersErrorMessage(t *testing.T) {
	const errMsg = "synth-catalog-load-failure: connection refused"
	s := screens.NewDeployAgentScreen(twoLevelRoot, 80, 24, plainStyles(), errMsg)

	view := s.View()

	if !strings.Contains(view, "synth-catalog-load-failure: connection refused") {
		t.Errorf("error-state view must contain the error message; got:\n%s", view)
	}
}

// TestDeployAgentScreen_ErrorState_EscSetsBack verifies that Esc in the error state
// sets Back(), consistent with all other error states in the TUI.
func TestDeployAgentScreen_ErrorState_EscSetsBack(t *testing.T) {
	s := screens.NewDeployAgentScreen(twoLevelRoot, 80, 24, plainStyles(), "synth-error")

	sendDAKey(s, tea.KeyEsc)

	if !s.Back() {
		t.Error("Back() = false after Esc in error state; want true")
	}
}

// TestDeployAgentScreen_ErrorState_EnterDoesNotConfirm verifies that Enter in the
// error state does not set Done(). Only Esc is handled there.
func TestDeployAgentScreen_ErrorState_EnterDoesNotConfirm(t *testing.T) {
	s := screens.NewDeployAgentScreen(twoLevelRoot, 80, 24, plainStyles(), "synth-error")

	confirmDA(s)

	if s.Done() {
		t.Error("Done() = true after Enter in error state; only Esc should be handled in error state")
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Reset_ClearsDoneAndBack verifies that Reset() restores the
// idle state: Done()=false and Back()=false after a previous confirmation.
func TestDeployAgentScreen_Reset_ClearsDoneAndBack(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	// Reach a Done() state.
	enterDAFolder(s)
	toggleDA(s)
	confirmDA(s)
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset() is called")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset(); want false")
	}
	if s.Back() {
		t.Error("Back() = true after Reset(); want false")
	}
}

// TestDeployAgentScreen_Reset_AfterBack_ClearsBoth verifies that Reset() also clears
// Back() after a cancellation, consistent with how other screens handle Reset.
func TestDeployAgentScreen_Reset_AfterBack_ClearsBoth(t *testing.T) {
	s := newDAScreen(twoLevelRoot)
	sendDAKey(s, tea.KeyEsc) // cancel at top level
	if !s.Back() {
		t.Fatal("precondition: Back() must be true before Reset() is called")
	}

	s.Reset()

	if s.Back() {
		t.Error("Back() = true after Reset() following a Back; want false")
	}
}

// ---------------------------------------------------------------------------
// Tab is inert
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Tab_InertAtTopLevel verifies that Tab does not confirm,
// cancel, or navigate — it is a no-op at the top level. This is the cross-screen
// invariant enforced by keybinding_nonregression_test.go for all screens.
func TestDeployAgentScreen_Tab_InertAtTopLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	sendDAKey(s, tea.KeyTab)

	if s.Done() {
		t.Error("Done() = true after Tab at top level; Tab must be inert")
	}
	if s.Back() {
		t.Error("Back() = true after Tab at top level; Tab must be inert")
	}
}

// TestDeployAgentScreen_Tab_InertInsideFolder verifies that Tab is also inert when
// the user is inside a folder.
//
// Vacuously passes in RED phase because enterDAFolder is a no-op in the stub; the
// test still asserts the correct final state and will catch Tab regressions post-implementation.
func TestDeployAgentScreen_Tab_InertInsideFolder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	enterDAFolder(s)          // enter SynthRole-Alpha
	sendDAKey(s, tea.KeyTab)  // Tab must be ignored

	if s.Done() {
		t.Error("Done() = true after Tab inside folder; Tab must be inert at every level")
	}
	if s.Back() {
		t.Error("Back() = true after Tab inside folder; Tab must not exit the folder or cancel")
	}
}

// ---------------------------------------------------------------------------
// Resize does not panic
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_Resize_DoesNotPanicAtTopLevel verifies that Resize on a
// normal screen does not panic.
func TestDeployAgentScreen_Resize_DoesNotPanicAtTopLevel(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize panicked at top level: %v", r)
		}
	}()

	s.Resize(120, 40)
	_ = s.View()
}

// TestDeployAgentScreen_Resize_DoesNotPanicInsideFolder verifies that Resize does
// not panic while the user is navigated into a folder.
//
// Vacuously passes in RED phase because enterDAFolder is a no-op in the stub (the
// screen stays at the top level). Becomes a meaningful panic-guard once navigation
// is implemented and the screen can hold folder-level render state.
func TestDeployAgentScreen_Resize_DoesNotPanicInsideFolder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)
	enterDAFolder(s) // enter SynthRole-Alpha

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize panicked inside folder: %v", r)
		}
	}()

	s.Resize(120, 40)
	_ = s.View()
}

// TestDeployAgentScreen_Resize_DoesNotPanicInEmptyState verifies that Resize does
// not panic in the empty state.
func TestDeployAgentScreen_Resize_DoesNotPanicInEmptyState(t *testing.T) {
	s := screens.NewDeployAgentScreen(nil, 80, 24, plainStyles(), "")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize panicked in empty state: %v", r)
		}
	}()

	s.Resize(120, 40)
	_ = s.View()
}

// TestDeployAgentScreen_Resize_DoesNotPanicInErrorState verifies that Resize does
// not panic in the error state.
func TestDeployAgentScreen_Resize_DoesNotPanicInErrorState(t *testing.T) {
	s := screens.NewDeployAgentScreen(twoLevelRoot, 80, 24, plainStyles(), "synth-error")

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Resize panicked in error state: %v", r)
		}
	}()

	s.Resize(120, 40)
	_ = s.View()
}

// ---------------------------------------------------------------------------
// SelectedIDs ordering
// ---------------------------------------------------------------------------

// TestDeployAgentScreen_SelectedIDs_FollowsAgentKeysOrder verifies that
// SelectedIDs() returns agent keys in the order produced by root.AgentKeys()
// (depth-first, Children before Agents at each node), regardless of the order in
// which the user visited and selected them. Selecting SynthRole-Beta's agent before
// SynthRole-Alpha's agent must still yield the Alpha key first in SelectedIDs().
func TestDeployAgentScreen_SelectedIDs_FollowsAgentKeysOrder(t *testing.T) {
	s := newDAScreen(twoLevelRoot)

	// Select in reverse tree order: Beta first, then Alpha.
	sendDAKey(s, tea.KeyDown) // move cursor to SynthRole-Beta
	enterDAFolder(s)          // enter SynthRole-Beta
	toggleDA(s)               // select agent-beta-one
	leaveDAFolder(s)          // back to top level

	sendDAKey(s, tea.KeyUp) // move cursor back to SynthRole-Alpha
	enterDAFolder(s)         // enter SynthRole-Alpha
	toggleDA(s)              // select agent-alpha-one
	confirmDA(s)             // confirm

	if !s.Done() {
		t.Fatal("precondition: Done() must be true after cross-folder selection + Enter")
	}
	ids := s.SelectedIDs()
	if len(ids) < 2 {
		t.Fatalf("SelectedIDs() = %v (len %d); want at least 2 (one from each folder)", ids, len(ids))
	}

	// AgentKeys() traverses SynthRole-Alpha before SynthRole-Beta (Children order),
	// so alpha must appear before beta in SelectedIDs() even though beta was selected first.
	alphaIdx := -1
	betaIdx := -1
	for i, id := range ids {
		if id == "agent-alpha-one" {
			alphaIdx = i
		}
		if id == "agent-beta-one" {
			betaIdx = i
		}
	}
	if alphaIdx < 0 {
		t.Errorf("SelectedIDs() = %v; missing 'agent-alpha-one'", ids)
	}
	if betaIdx < 0 {
		t.Errorf("SelectedIDs() = %v; missing 'agent-beta-one'", ids)
	}
	if alphaIdx >= 0 && betaIdx >= 0 && alphaIdx > betaIdx {
		t.Errorf("SelectedIDs() = %v; 'agent-alpha-one' (index %d) appears after 'agent-beta-one' (index %d); "+
			"SelectedIDs() must follow AgentKeys() order — SynthRole-Alpha precedes SynthRole-Beta in Children",
			ids, alphaIdx, betaIdx)
	}
}
