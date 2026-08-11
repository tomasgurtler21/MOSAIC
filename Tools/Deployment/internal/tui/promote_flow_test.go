package tui

// promote_flow_test.go covers the TUI surface for the "promote" mode: mode screen entry,
// keyboard flow from harness selection through promote summary dismissal, and screen-state
// invariants for the promote completion path.
//
// Existing tests (Stage 9.6 — now GREEN):
//
//   These tests verify the promote mode keyboard flow that was wired when ModePromote was
//   added to the mode screen (I9.5) and PromoteSummaryScreen was implemented (I9.6). They
//   are GREEN after those deliveries.
//
//   The workspace screen pre-fill uses a real agent file (not the workspace directory),
//   because Stage 3 (I3.1) makes the workspace screen require a regular file in promote
//   mode. Tests that pre-filled with a directory would fail after I3.1, so they have been
//   updated to pre-fill with a file created by createAgentFile.
//
// Stage 3 tests (T3.1, T3.3 — RED phase):
//
//   T3.1 — Path screen in file kind after selecting Promote:
//     - Promote + file path  → advances to screenRunning (currently FAILS: file rejected by
//       directory validator because I3.1 has not been delivered)
//     - Promote + directory  → stays on screenWorkspace (currently FAILS: directory accepted
//       by directory validator because I3.1 has not been delivered)
//     - Promote path screen view uses file wording (currently FAILS: directory wording shown)
//     - Back-nav from promote to deploy-new: directory is accepted (regression / AC3.6)
//     - Back-nav from deploy-new to promote: directory is rejected (currently FAILS / AC3.6)
//
//   T3.3 — Promote-specific running status text:
//     - statusMsg during a promote run is not deployment wording (currently FAILS: statusMsg
//       is "Running deployment…" for all modes until I3.3 is delivered)

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// createAgentFile creates a regular file named "agent.md" in dir and returns its absolute
// path. Used in promote flow tests to supply a valid file-kind path to the workspace screen.
func createAgentFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "agent.md")
	if err := os.WriteFile(p, []byte("# agent\n"), 0o644); err != nil {
		t.Fatalf("createAgentFile: failed to write %q: %v", p, err)
	}
	return p
}

// newPromoteFlowSvc returns a stubFlowService pre-configured for promote-mode tests.
// It returns a minimal PromoteResult on a successful Promote call.
func newPromoteFlowSvc(workspace string) *stubFlowService {
	svc := newFlowSvc(workspace)
	svc.promote = app.PromoteResult{
		SourcePath:      workspace + "/my-harness-agent.md",
		DestinationPath: "/mosaicroot/Agents/Generic/Agents/TestCategory/my-harness-agent.md",
		Key:             "my-harness-agent",
		NumericID:       "42",
		Category:        "TestCategory",
	}
	return svc
}

// ---------------------------------------------------------------------------
// Mode screen: ModePromote item presence
// ---------------------------------------------------------------------------

// TestModeScreen_IncludesPromoteEntry verifies that the mode screen contains a ModePromote
// entry as one of its items. After I9.5 adds ModePromote to modeItems, the total number of
// items should be four.
func TestModeScreen_IncludesPromoteEntry(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newPromoteFlowSvc(workspace), workspace)

	// Navigate to the mode screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v, want screenMode", m.screen)
	}

	// Navigate Down past all existing entries; if ModePromote exists it is the 4th item.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → update (item 2)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → workflows-only (item 3)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → promote (item 4, only if it exists)

	// Select whatever item the cursor is on now.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: if ModePromote was in the list, the selection should be ModePromote.
	if m.selections.mode != domain.ModePromote {
		t.Errorf("selections.mode = %q after three Down presses + Enter; want ModePromote (%q); "+
			"the mode screen must include a ModePromote entry as the fourth item",
			m.selections.mode, domain.ModePromote)
	}
}

// ---------------------------------------------------------------------------
// Promote keyboard flow — harness → mode → workspace → running → done
// ---------------------------------------------------------------------------

// TestFullKeyboardFlow_Promote_FromLaunchToExit verifies the end-to-end promote flow:
// harness selection → mode selection (ModePromote) → file-path entry →
// screenRunning → screenDone with promoteScreen → 'q' exits.
func TestFullKeyboardFlow_Promote_FromLaunchToExit(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	if m.screen != screenHarness {
		t.Fatalf("precondition: screen = %v, want screenHarness", m.screen)
	}

	// Step 1: Harness screen — Enter selects the single harness.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenMode {
		t.Fatalf("screen = %v after harness Enter; want screenMode", m.screen)
	}

	// Step 2: Mode screen — navigate to ModePromote (4th item: Down x3) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: deploy-new → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: update → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: workflows-only → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after mode Down x3 + Enter; want screenWorkspace", m.screen)
	}
	if m.selections.mode != domain.ModePromote {
		t.Fatalf("selections.mode = %q; want ModePromote", m.selections.mode)
	}

	// Step 3: Workspace screen — Enter confirms the pre-filled file path and starts the service.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after workspace Enter; want screenRunning", m.screen)
	}
	if cmd == nil {
		t.Fatal("cmd = nil after workspace/file-path confirmation; want a promote command")
	}

	// Step 4: Execute the promote command and deliver the promoteDoneMsg to the model.
	_, _ = m.Update(runCmd(cmd))

	// Assert: model transitioned to screenDone.
	if m.screen != screenDone {
		t.Fatalf("screen = %v after promote completion; want screenDone", m.screen)
	}

	// Assert: promoteScreen is set, summaryScreen is nil.
	if m.promoteScreen == nil {
		t.Fatal("promoteScreen is nil after promote completion; want a non-nil PromoteSummaryScreen")
	}
	if m.summaryScreen != nil {
		t.Error("summaryScreen is non-nil after promote completion; the two fields must be mutually exclusive")
	}

	// Step 5: 'q' exits.
	_, quit := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quit == nil {
		t.Error("cmd = nil after 'q' on promote summary; want tea.Quit command")
	}
}

// TestFullKeyboardFlow_Promote_EnterDismissesPromoteSummary verifies that Enter is also
// accepted as an exit key on the promote summary screen.
func TestFullKeyboardFlow_Promote_EnterDismissesPromoteSummary(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to screenDone with promote flow.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → workspace
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → running
	_, _ = m.Update(runCmd(cmd))                        // → done

	if m.screen != screenDone {
		t.Fatalf("precondition: screen = %v, want screenDone", m.screen)
	}
	if m.promoteScreen == nil {
		t.Fatal("precondition: promoteScreen is nil; cannot test Enter-dismiss")
	}

	// Act: Enter should dismiss the promote summary screen.
	_, quit := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert
	if quit == nil {
		t.Error("cmd = nil after Enter on promote summary; want tea.Quit command")
	}
}

// ---------------------------------------------------------------------------
// Screen-state invariants
// ---------------------------------------------------------------------------

// TestFullKeyboardFlow_Promote_ScreenMutuallyExclusive verifies that after a promote run,
// promoteScreen is non-nil while summaryScreen remains nil. The two fields are documented
// as mutually exclusive: only one is set per run.
func TestFullKeyboardFlow_Promote_ScreenMutuallyExclusive(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to screenDone via promote flow.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → workspace
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → running
	_, _ = m.Update(runCmd(cmd))                        // → done

	// Assert
	if m.promoteScreen == nil {
		t.Error("promoteScreen is nil after promote run; must be non-nil")
	}
	if m.summaryScreen != nil {
		t.Error("summaryScreen is non-nil after promote run; must be nil (promote and summary screens are mutually exclusive)")
	}
}

// TestFullKeyboardFlow_Promote_ViewIsNonEmpty verifies that the promote summary screen
// renders a non-empty string at screenDone.
func TestFullKeyboardFlow_Promote_ViewIsNonEmpty(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to screenDone via promote flow.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.Update(runCmd(cmd))

	if m.promoteScreen == nil {
		t.Fatal("promoteScreen is nil; cannot test View()")
	}

	// Act
	view := m.View()

	// Assert
	if view == "" {
		t.Error("View() returned empty string on screenDone after promote run; want promote summary content")
	}
}

// ---------------------------------------------------------------------------
// Promote error path
// ---------------------------------------------------------------------------

// TestFullKeyboardFlow_Promote_ServiceError_IsKeyboardDismissible verifies that when
// Service.Promote returns an error, the model transitions to screenDone without populating
// promoteScreen, and the error state is keyboard-dismissible.
func TestFullKeyboardFlow_Promote_ServiceError_IsKeyboardDismissible(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	svc := newPromoteFlowSvc(workspace)
	svc.promoteErr = app.ErrPromoteNotTransformed
	svc.promote = app.PromoteResult{} // empty on error

	m := newFlowModel(svc, agentFile)

	// Navigate to screenRunning with ModePromote.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → workspace
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → running

	// Execute the promote command — service returns an error.
	_, _ = m.Update(runCmd(cmd))

	// Assert: still reaches screenDone (error path).
	if m.screen != screenDone {
		t.Fatalf("screen = %v after promote error; want screenDone", m.screen)
	}

	// Assert: promoteScreen is nil on error path.
	if m.promoteScreen != nil {
		t.Error("promoteScreen is non-nil on promote error path; must be nil because no result was produced")
	}

	// Assert: error state is keyboard-dismissible with 'q'.
	_, quit := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quit == nil {
		t.Error("cmd = nil after 'q' on promote error; want tea.Quit command")
	}
}

// TestFullKeyboardFlow_Promote_SelectionModeIsPromote verifies that after selecting
// ModePromote on the mode screen, selections.mode is domain.ModePromote. This is the
// minimal wiring check: the mode screen item ID must map correctly to domain.ModePromote.
func TestFullKeyboardFlow_Promote_SelectionModeIsPromote(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newPromoteFlowSvc(workspace), workspace)

	// Navigate to mode screen and select ModePromote (4th item).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm

	// Assert
	if m.selections.mode != domain.ModePromote {
		t.Errorf("selections.mode = %q; want domain.ModePromote (%q); "+
			"the promote mode item must carry ID == string(domain.ModePromote)",
			m.selections.mode, domain.ModePromote)
	}
}

// ---------------------------------------------------------------------------
// Stage 3 — T3.1: Path screen in file kind when Promote is selected
// ---------------------------------------------------------------------------

// TestPromoteMode_FileKind_AcceptsExistingFile verifies that after selecting Promote on the
// mode screen and advancing to the path screen, entering the path to an existing regular file
// accepts the input and transitions to screenRunning.
//
// This test is RED until I3.1 is delivered: without I3.1, the path screen stays in directory
// kind for all modes, so a file path is rejected with "path is not a directory" and the model
// stays on screenWorkspace instead of advancing.
func TestPromoteMode_FileKind_AcceptsExistingFile(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to promote workspace screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}

	// Act: Enter on the file path.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: file path accepted in promote (file kind) mode.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after Enter on file path in promote mode; want screenRunning; "+
			"the path screen must accept an existing regular file when in file kind (promote mode)", m.screen)
	}
	if cmd == nil {
		t.Error("cmd = nil after file path accepted in promote mode; want a promote service command")
	}
}

// TestPromoteMode_FileKind_RejectsDirectory verifies that after selecting Promote on the mode
// screen and advancing to the path screen, entering a directory path keeps the user on the
// path screen and does not transition to screenRunning.
//
// This test is RED until I3.1 is delivered: without I3.1, the path screen is in directory kind
// for all modes, so the directory path is accepted and the model transitions to screenRunning
// instead of staying on screenWorkspace.
func TestPromoteMode_FileKind_RejectsDirectory(t *testing.T) {
	// Arrange: pre-fill with a workspace directory (not a file).
	workspace := t.TempDir()
	m := newFlowModel(newPromoteFlowSvc(workspace), workspace)

	// Navigate to promote workspace screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}

	// Act: Enter on the directory path.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: directory rejected, model stays on screenWorkspace.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after Enter on directory path in promote mode; want screenWorkspace; "+
			"the path screen must reject a directory in file kind (promote mode) and keep the user on the path screen",
			m.screen)
	}
}

// TestPromoteMode_PathScreen_View_ContainsFileWording verifies that after selecting Promote
// and advancing to the path screen, the rendered view uses file-oriented wording and does not
// contain directory-oriented wording.
//
// This test is RED until I3.1 is delivered: without I3.1, the path screen stays in directory
// kind for all modes, so the view shows "Workspace Path" and "directory" wording regardless of
// which mode was selected.
func TestPromoteMode_PathScreen_View_ContainsFileWording(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newPromoteFlowSvc(workspace), workspace)

	// Navigate to promote workspace screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}

	// Act
	lower := strings.ToLower(m.View())

	// Assert: the path screen in promote mode must use file-oriented wording.
	if strings.Contains(lower, "directory") {
		t.Errorf("path screen view in promote mode contains 'directory' wording; want file-oriented wording only:\n%s",
			m.View())
	}
	if strings.Contains(lower, "workspace") {
		t.Errorf("path screen view in promote mode contains 'workspace' wording; want file-oriented wording only:\n%s",
			m.View())
	}
	if !strings.Contains(lower, "file") {
		t.Errorf("path screen view in promote mode does not contain 'file' wording; want file-oriented label:\n%s",
			m.View())
	}
}

// ---------------------------------------------------------------------------
// Stage 3 — T3.1 / AC3.6: Back-navigation re-applies the correct path kind
// ---------------------------------------------------------------------------

// TestBackNavigation_PromoteThenDeployNew_DirectoryAccepted verifies that after navigating
// back from the promote path screen to the mode screen and re-selecting deploy-new, the path
// screen switches back to directory kind and accepts a workspace directory.
//
// This test is GREEN (regression): with or without I3.1, deploy-new always accepts a directory.
// It guards against a regression where the path kind from promote mode persists into the next
// mode selection rather than being re-applied on each mode→path transition.
func TestBackNavigation_PromoteThenDeployNew_DirectoryAccepted(t *testing.T) {
	// Arrange: pre-fill with a real file for the promote leg, then switch to directory
	// for the deploy-new leg.
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to promote workspace screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace (file kind)

	// Esc back to mode screen without confirming the path.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to mode (cursor at promote, index 3)

	// Navigate to deploy-new (Up x3 from promote) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})    // promote → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})    // workflows-only → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})    // update → deploy-new
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select deploy-new → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace after selecting deploy-new", m.screen)
	}

	// Change pre-fill to a workspace directory for the deploy-new leg.
	// The path screen in deploy-new mode (directory kind) must accept it.
	m.wsScreen.SetPrefilledPath(workspace)

	// Act: Enter on the workspace directory.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: directory accepted in deploy-new (directory kind) mode.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after Enter on directory in deploy-new mode; want screenRunning; "+
			"switching from promote to deploy-new must re-apply directory-kind validation so a workspace directory is accepted",
			m.screen)
	}
}

// TestBackNavigation_DeployNewToPromote_DirectoryRejected verifies that after navigating back
// from the deploy-new path screen to the mode screen and re-selecting Promote, the path screen
// switches to file kind and rejects a directory path.
//
// This test is RED until I3.1 is delivered: without I3.1, the path kind is never updated on
// mode transitions, so the directory path is accepted in promote mode and the model transitions
// to screenRunning instead of staying on screenWorkspace.
func TestBackNavigation_DeployNewToPromote_DirectoryRejected(t *testing.T) {
	// Arrange: pre-fill with a workspace directory (the deploy-new leg accepts it; promote rejects it).
	workspace := t.TempDir()
	m := newFlowModel(newPromoteFlowSvc(workspace), workspace)

	// Navigate to deploy-new workspace screen and go back.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select deploy-new → workspace (cursor at 0)

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace (deploy-new)", m.screen)
	}

	// Back to mode screen without confirming the path.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to mode (cursor at deploy-new, index 0)

	// Navigate to promote (Down x3) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // deploy-new → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // update → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // workflows-only → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace after re-selecting promote", m.screen)
	}

	// Act: Enter on the directory path.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: directory rejected in promote (file kind) mode after back-navigation.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after Enter on directory in promote mode via back-navigation path; "+
			"want screenWorkspace; re-entering promote mode must re-apply file-kind validation so a directory is rejected",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// Stage 3 — T3.3: Promote-specific running status text
// ---------------------------------------------------------------------------

// TestPromoteMode_RunningStatus_IsPromoteSpecific verifies that when the model transitions to
// screenRunning after a promote path confirmation, the in-flight status message is
// promote-specific and does not use the deployment wording shown for other modes.
//
// This test is RED until I3.1 and I3.3 are both delivered:
//   - Without I3.1: the file path is rejected (directory validator), the model stays on
//     screenWorkspace and never reaches screenRunning; the screen assertion fires.
//   - With I3.1 but without I3.3: the model reaches screenRunning but statusMsg is still
//     "Running deployment…"; the statusMsg assertion fires.
//   - With both delivered: the model reaches screenRunning with promote-specific statusMsg.
func TestPromoteMode_RunningStatus_IsPromoteSpecific(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)
	m := newFlowModel(newPromoteFlowSvc(workspace), agentFile)

	// Navigate to promote mode.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace

	// Act: confirm the file path to start the service.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → running

	// Precondition: the model must be on screenRunning to check the status text.
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after confirming file path in promote mode; want screenRunning; "+
			"I3.1 (path-kind wiring) must be delivered for this test to reach the statusMsg assertion",
			m.screen)
	}

	// Assert: statusMsg must not use deployment wording.
	if m.statusMsg == "Running deployment…" {
		t.Errorf("statusMsg = %q during promote run; want promote-specific text, not deployment wording; "+
			"I3.3 must make the running status message mode-aware", m.statusMsg)
	}
	if m.statusMsg == "" {
		t.Error("statusMsg is empty during promote run; want promote-specific text")
	}
	// The message should name the promote operation.
	if !strings.Contains(strings.ToLower(m.statusMsg), "promot") {
		t.Errorf("statusMsg = %q during promote run; want text containing 'promot' (e.g. 'Running promote…')",
			m.statusMsg)
	}
}

// ---------------------------------------------------------------------------
// Harness identity threading — TUI promote branch (T1.1)
// ---------------------------------------------------------------------------

// capturingPromoteService wraps a stubFlowService and records the last PromoteRequest
// received by Promote. It is used to verify that the TUI wires the already-selected
// harness id into the promote request without presenting any additional harness question.
type capturingPromoteService struct {
	inner          *stubFlowService
	lastPromoteReq *app.PromoteRequest
}

func (s *capturingPromoteService) ListHarnesses() []domain.HarnessRef {
	return s.inner.ListHarnesses()
}
func (s *capturingPromoteService) DeployNew(ctx context.Context, req app.DeployRequest) (domain.RunSummary, error) {
	return s.inner.DeployNew(ctx, req)
}
func (s *capturingPromoteService) Update(ctx context.Context, req app.UpdateRequest) (domain.RunSummary, error) {
	return s.inner.Update(ctx, req)
}
func (s *capturingPromoteService) UpdateWorkflows(ctx context.Context, req app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return s.inner.UpdateWorkflows(ctx, req)
}
func (s *capturingPromoteService) Promote(ctx context.Context, req app.PromoteRequest) (app.PromoteResult, error) {
	s.lastPromoteReq = &req
	return s.inner.Promote(ctx, req)
}
func (s *capturingPromoteService) TransformHarness(ctx context.Context, req app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	return s.inner.TransformHarness(ctx, req)
}

func (s *capturingPromoteService) DeployUtilityInfrastructure(ctx context.Context, req app.UtilityInfraRequest) (domain.RunSummary, error) {
	return s.inner.DeployUtilityInfrastructure(ctx, req)
}

func (s *capturingPromoteService) RenderAgent(ctx context.Context, req app.RenderAgentRequest) (app.RenderAgentResult, error) {
	return s.inner.RenderAgent(ctx, req)
}

// TestPromoteMode_SuppliesSelectedHarnessIDInRequest verifies that when the TUI executes a
// promote run, the PromoteRequest carries the harness id that was selected on the harness
// screen. The TUI must not present any additional harness question during the promote flow;
// it supplies the already-collected selection directly (AC1.6).
//
// This test is RED until the ModePromote branch of startService sets HarnessID from
// sel.harnessID. Currently startService omits HarnessID from the promote request, so
// spy.lastPromoteReq.HarnessID is empty and the assertion fails.
func TestPromoteMode_SuppliesSelectedHarnessIDInRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createAgentFile(t, workspace)

	inner := newPromoteFlowSvc(workspace)
	spy := &capturingPromoteService{inner: inner}
	m := newFlowModel(spy, agentFile)

	// Navigate to the harness screen and select the single harness (flowTestHarness).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness screen → mode screen
	if m.selections.harnessID != flowTestHarness.ID {
		t.Fatalf("precondition: harnessID = %q after harness selection, want %q", m.selections.harnessID, flowTestHarness.ID)
	}

	// Navigate to ModePromote (4th item: Down x3) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: deploy-new → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: update → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // cursor: workflows-only → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select promote → workspace screen

	if m.selections.mode != domain.ModePromote {
		t.Fatalf("precondition: mode = %q, want ModePromote", m.selections.mode)
	}

	// Confirm the file path to start the service.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // → screenRunning + returns promote cmd

	if m.screen != screenRunning {
		t.Fatalf("precondition: screen = %v after file confirmation, want screenRunning; "+
			"I3.1 (path-kind wiring) must be delivered for this test to reach the HarnessID assertion",
			m.screen)
	}

	// Execute the promote command so the spy captures the request.
	_, _ = m.Update(runCmd(cmd))

	// Assert — Promote must have been called.
	if spy.lastPromoteReq == nil {
		t.Fatal("svc.Promote was not called; the TUI must call svc.Promote after confirming the file path in promote mode")
	}

	// Assert — the request must carry the harness selected on the harness screen.
	if spy.lastPromoteReq.HarnessID != flowTestHarness.ID {
		t.Errorf("PromoteRequest.HarnessID = %q, want %q; "+
			"the TUI promote branch must supply the already-selected harness id without asking again",
			spy.lastPromoteReq.HarnessID, flowTestHarness.ID)
	}
}
