package tui

// transform_flow_test.go covers the TUI surface for the "transform-harness" mode: mode
// screen entry, keyboard flow from harness selection through the path screen, path-kind
// acceptance rules, running state, and service dispatch.
//
// All tests in this file are RED-phase TDD tests. They assert the intended behaviour that
// will be delivered by I6.5 (TUI wiring) and related items. Tests currently fail for two
// reasons:
//
//  1. The mode screen has four items (deploy-new, update, workflows-only, promote). There
//     is no fifth item for transform-harness. Down x4 wraps to promote rather than
//     landing on transform-harness, so mode-screen navigation assertions fail.
//
//  2. The workspace screen does not yet apply PathKindFileOrDirectory when
//     ModeTransformHarness is selected, so a regular file path is rejected by the default
//     directory validator. Tests that pre-fill with a file path and expect acceptance fail.
//
// Verified behaviours (T6.6):
//
//   Mode screen item (T6.6):
//     - Down x4 on the mode screen selects ModeTransformHarness (5th item).
//     - The mode screen total item count is five after I6.5 is delivered.
//
//   Keyboard flow (T6.6):
//     - Harness → mode (Down x4 + Enter) → workspace (file-or-dir kind) → running →
//       screenDone. The workspace screen accepts an existing regular file in transform mode.
//     - The workspace screen also accepts an existing readable directory in transform mode
//       (PathKindFileOrDirectory allows either).
//     - The workspace screen rejects a non-existent path in transform mode.
//
//   Service dispatch (T6.6):
//     - After path confirmation, startService calls svc.TransformHarness (not svc.DeployNew,
//       svc.Update, svc.UpdateWorkflows, or svc.Promote).
//     - The TransformHarnessRequest carries the harness id selected on the harness screen as
//       SourceHarnessID.
//     - The result transitions the model to screenDone.
//
//   Running status text (T6.6):
//     - The statusMsg in screenRunning after transform path confirmation is not the
//       deployment wording ("Running deployment…"); it is transform-specific.

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

// ---------------------------------------------------------------------------
// Transform flow fixtures
// ---------------------------------------------------------------------------

// createTransformAgentFile creates a regular readable file for use as the transform
// path input. Returns the absolute file path.
func createTransformAgentFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "my-agent.src.md")
	if err := os.WriteFile(p, []byte("# deployed agent\n"), 0o644); err != nil {
		t.Fatalf("createTransformAgentFile: %v", err)
	}
	return p
}

// newTransformFlowSvc returns a stubFlowService pre-configured for transform-mode tests.
// The transform field carries a minimal TransformHarnessResult that the TUI can render
// at screenDone without panicking.
func newTransformFlowSvc(workspace string) *stubFlowService {
	svc := newFlowSvc(workspace)
	svc.transform = app.TransformHarnessResult{
		SourceHarnessID: flowTestHarness.ID,
		TargetHarnessID: "target-harness",
		InputPath:       workspace + "/my-agent.src.md",
		Transformed:     1,
		Files: []app.TransformFileOutcome{
			{
				SourcePath:      workspace + "/my-agent.src.md",
				DestinationPath: workspace + "/.ai/agents/my-agent.tgt.md",
				Status:          app.StatusTransformed,
			},
		},
	}
	return svc
}

// navigateToTransformMode drives the model from the harness screen to the workspace
// screen with ModeTransformHarness selected. It performs:
//  1. Enter to confirm the single harness.
//  2. Down x4 to move the mode cursor from deploy-new to transform-harness (5th item).
//  3. Enter to confirm the mode.
//
// Returns the model at screenWorkspace (or whatever screen the model ends up on).
func navigateToTransformMode(m *rootModel) {
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) //nolint:errcheck — harness → mode
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  //nolint:errcheck — deploy-new → update
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  //nolint:errcheck — update → workflows-only
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  //nolint:errcheck — workflows-only → promote
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  //nolint:errcheck — promote → transform-harness
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) //nolint:errcheck — select transform → workspace
}

// ---------------------------------------------------------------------------
// T6.6: Mode screen item
// ---------------------------------------------------------------------------

// TestModeScreen_IncludesTransformHarnessEntry verifies that the mode screen contains a
// ModeTransformHarness entry as its fifth item, reachable by pressing Down four times
// from the first item and then Enter.
func TestModeScreen_IncludesTransformHarnessEntry(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newTransformFlowSvc(workspace), workspace)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode

	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v, want screenMode", m.screen)
	}

	// Navigate Down x4 to the 5th item (transform-harness).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // → transform-harness (only if it exists)

	// Confirm whatever item the cursor is on.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: the selected mode must be ModeTransformHarness.
	if m.selections.mode != domain.ModeTransformHarness {
		t.Errorf("selections.mode = %q after Down x4 + Enter; want ModeTransformHarness (%q); "+
			"the mode screen must include a ModeTransformHarness entry as the fifth item",
			m.selections.mode, domain.ModeTransformHarness)
	}
}

// TestModeScreen_HasFiveItems verifies that the mode screen has five items total.
// Without I6.5 the screen has four items (deploy-new, update, workflows-only, promote).
func TestModeScreen_HasFiveItems(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newTransformFlowSvc(workspace), workspace)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode

	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v, want screenMode", m.screen)
	}

	// Press Down four times to walk through items 2-5 without committing.
	for i := 0; i < 4; i++ {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if m.screen != screenMode {
			t.Fatalf("Down %d left screenMode; mode screen must have at least %d items", i+1, i+2)
		}
	}

	// Confirm the 5th item — must be ModeTransformHarness.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.selections.mode != domain.ModeTransformHarness {
		t.Errorf("5th mode item selection = %q; want ModeTransformHarness", m.selections.mode)
	}
}

// ---------------------------------------------------------------------------
// T6.6: Workspace screen in transform mode accepts file-or-dir inputs
// ---------------------------------------------------------------------------

// TestTransformMode_PathScreen_AcceptsExistingFile verifies that after selecting
// ModeTransformHarness, entering the path to an existing regular file advances to
// screenRunning. Without I6.5 the path screen keeps the default directory kind, which
// rejects a regular file, so this test is RED.
func TestTransformMode_PathScreen_AcceptsExistingFile(t *testing.T) {
	// Arrange: pre-fill with an existing file.
	workspace := t.TempDir()
	agentFile := createTransformAgentFile(t, workspace)
	m := newFlowModel(newTransformFlowSvc(workspace), agentFile)

	// Navigate to transform workspace screen.
	navigateToTransformMode(m)

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}
	// Guard: confirm we landed on the transform-harness workspace screen, not some other
	// mode's workspace (e.g. promote, which currently receives Down x4 before I6.4 adds the
	// 5th mode item). Without this guard the test passed vacuously because promote's PathKindFile
	// happened to accept a regular file.
	if m.selections.mode != domain.ModeTransformHarness {
		t.Fatalf("precondition: selections.mode = %q; want ModeTransformHarness; "+
			"Down x4 + Enter must land on transform-harness, not on another mode; "+
			"I6.4 must add transform-harness as the 5th mode-screen item",
			m.selections.mode)
	}

	// Act: Enter on the file path.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: file accepted → advances to screenRunning.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after Enter on file path in transform mode; want screenRunning; "+
			"the path screen must accept an existing regular file when in PathKindFileOrDirectory "+
			"(transform mode); I6.5 must wire PathKindFileOrDirectory for ModeTransformHarness",
			m.screen)
	}
	if cmd == nil {
		t.Error("cmd = nil after file path accepted; want a transform service command")
	}
}

// TestTransformMode_PathScreen_AcceptsExistingDirectory verifies that in transform mode
// the path screen accepts an existing readable directory in addition to a regular file.
// This distinguishes PathKindFileOrDirectory from PathKindFile.
func TestTransformMode_PathScreen_AcceptsExistingDirectory(t *testing.T) {
	// Arrange: pre-fill with an existing directory (the workspace itself).
	workspace := t.TempDir()
	m := newFlowModel(newTransformFlowSvc(workspace), workspace)

	// Navigate to transform workspace screen.
	navigateToTransformMode(m)

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}

	// Act: Enter on the directory path.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: directory accepted → advances to screenRunning.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after Enter on directory in transform mode; want screenRunning; "+
			"PathKindFileOrDirectory must accept directories as well as files; "+
			"I6.5 must wire PathKindFileOrDirectory so the workspace screen accepts a readable directory",
			m.screen)
	}
	if cmd == nil {
		t.Error("cmd = nil after directory accepted in transform mode; want a transform service command")
	}
}

// TestTransformMode_PathScreen_View_ContainsFileOrFolderWording verifies that the path
// screen label in transform mode uses file-or-folder wording rather than workspace-directory
// wording. Without I6.5 the screen shows directory wording.
func TestTransformMode_PathScreen_View_ContainsFileOrFolderWording(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newTransformFlowSvc(workspace), workspace)

	navigateToTransformMode(m)

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace", m.screen)
	}
	// Guard: confirm we are on the transform-harness workspace, not promote's workspace.
	// Without this guard the test passed vacuously because promote already shows file-oriented
	// wording. Once I6.4 adds transform-harness as the 5th mode item this guard succeeds.
	if m.selections.mode != domain.ModeTransformHarness {
		t.Fatalf("precondition: selections.mode = %q; want ModeTransformHarness; "+
			"Down x4 + Enter must select transform-harness (I6.4 not yet delivered)",
			m.selections.mode)
	}

	// Act
	lower := strings.ToLower(m.View())

	// Assert: file-or-folder wording present.
	if !strings.Contains(lower, "file") && !strings.Contains(lower, "folder") {
		t.Errorf("path screen in transform mode contains neither 'file' nor 'folder'; "+
			"want 'agent file or folder' wording:\n%s", m.View())
	}
	// Assert: workspace-directory wording absent.
	if strings.Contains(lower, "workspace") {
		t.Errorf("path screen in transform mode contains 'workspace' wording; "+
			"want file-or-folder wording only:\n%s", m.View())
	}
}

// ---------------------------------------------------------------------------
// T6.6: Full keyboard flow — harness → mode → workspace → running → done
// ---------------------------------------------------------------------------

// TestFullKeyboardFlow_Transform_FileInput_FromLaunchToScreenDone verifies that the
// complete transform flow — harness selection, mode selection (ModeTransformHarness),
// file-path entry, service run, and screenDone — is achievable by keyboard alone.
func TestFullKeyboardFlow_Transform_FileInput_FromLaunchToScreenDone(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createTransformAgentFile(t, workspace)
	m := newFlowModel(newTransformFlowSvc(workspace), agentFile)

	if m.screen != screenHarness {
		t.Fatalf("precondition: screen = %v, want screenHarness", m.screen)
	}

	// Step 1: Harness screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenMode {
		t.Fatalf("screen = %v after harness Enter; want screenMode", m.screen)
	}

	// Step 2: Mode screen — Down x4 reaches transform-harness (5th item).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → transform-harness
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select → workspace

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after mode Down x4 + Enter; want screenWorkspace", m.screen)
	}
	if m.selections.mode != domain.ModeTransformHarness {
		t.Fatalf("selections.mode = %q; want ModeTransformHarness", m.selections.mode)
	}

	// Step 3: Workspace screen — Enter on the pre-filled file path starts the service.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenRunning {
		t.Fatalf("screen = %v after workspace Enter; want screenRunning", m.screen)
	}
	if cmd == nil {
		t.Fatal("cmd = nil after file-path confirmation; want a transform service command")
	}

	// Step 4: Execute the service command.
	_, _ = m.Update(runCmd(cmd))

	// Assert: model transitioned to screenDone.
	if m.screen != screenDone {
		if m.errMsg != "" {
			t.Fatalf("screen = %v, errMsg = %q; want screenDone after transform run", m.screen, m.errMsg)
		}
		t.Fatalf("screen = %v after transform run; want screenDone", m.screen)
	}

	// Step 5: 'q' exits.
	_, quit := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quit == nil {
		t.Error("cmd = nil after 'q' on transform screenDone; want tea.Quit command")
	}
}

// ---------------------------------------------------------------------------
// T6.6: Service dispatch wiring
// ---------------------------------------------------------------------------

// capturingTransformService wraps a stubFlowService and records the last
// TransformHarnessRequest received by TransformHarness. It is used to verify that the TUI
// wires the already-selected harness id into the transform request as SourceHarnessID.
type capturingTransformService struct {
	inner            *stubFlowService
	lastTransformReq *app.TransformHarnessRequest
}

func (s *capturingTransformService) ListHarnesses() []domain.HarnessRef {
	return s.inner.ListHarnesses()
}
func (s *capturingTransformService) DeployNew(ctx context.Context, req app.DeployRequest) (domain.RunSummary, error) {
	return s.inner.DeployNew(ctx, req)
}
func (s *capturingTransformService) Update(ctx context.Context, req app.UpdateRequest) (domain.RunSummary, error) {
	return s.inner.Update(ctx, req)
}
func (s *capturingTransformService) UpdateWorkflows(ctx context.Context, req app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return s.inner.UpdateWorkflows(ctx, req)
}
func (s *capturingTransformService) Promote(ctx context.Context, req app.PromoteRequest) (app.PromoteResult, error) {
	return s.inner.Promote(ctx, req)
}
func (s *capturingTransformService) TransformHarness(ctx context.Context, req app.TransformHarnessRequest) (app.TransformHarnessResult, error) {
	s.lastTransformReq = &req
	return s.inner.TransformHarness(ctx, req)
}

func (s *capturingTransformService) DeployUtilityInfrastructure(ctx context.Context, req app.UtilityInfraRequest) (domain.RunSummary, error) {
	return s.inner.DeployUtilityInfrastructure(ctx, req)
}

// TestTransformMode_SuppliesSelectedHarnessIDInRequest verifies that when the TUI executes
// a transform run, the TransformHarnessRequest carries the harness id selected on the
// harness screen as SourceHarnessID. The TUI must not present any additional harness
// question during the transform flow.
func TestTransformMode_SuppliesSelectedHarnessIDInRequest(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createTransformAgentFile(t, workspace)

	inner := newTransformFlowSvc(workspace)
	spy := &capturingTransformService{inner: inner}
	m := newFlowModel(spy, agentFile)

	// Step 1: Harness screen — select the single harness.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.selections.harnessID != flowTestHarness.ID {
		t.Fatalf("precondition: harnessID = %q after harness selection, want %q",
			m.selections.harnessID, flowTestHarness.ID)
	}

	// Step 2: Navigate to ModeTransformHarness (Down x4) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.selections.mode != domain.ModeTransformHarness {
		t.Fatalf("precondition: mode = %q, want ModeTransformHarness; navigate mode screen first", m.selections.mode)
	}

	// Step 3: Confirm the file path.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenRunning {
		t.Fatalf("precondition: screen = %v after file confirmation, want screenRunning; "+
			"I6.5 (path-kind wiring) must be delivered for the request assertion to be tested", m.screen)
	}

	// Execute the transform command so the spy captures the request.
	_, _ = m.Update(runCmd(cmd))

	// Assert: TransformHarness must have been called.
	if spy.lastTransformReq == nil {
		t.Fatal("svc.TransformHarness was not called; startService must dispatch to TransformHarness for ModeTransformHarness")
	}

	// Assert: SourceHarnessID must match the harness selected on the harness screen.
	if spy.lastTransformReq.SourceHarnessID != flowTestHarness.ID {
		t.Errorf("SourceHarnessID = %q, want %q; "+
			"the TUI transform branch must supply the already-selected harness id as SourceHarnessID without asking again",
			spy.lastTransformReq.SourceHarnessID, flowTestHarness.ID)
	}
}

// ---------------------------------------------------------------------------
// T6.6: Running status text
// ---------------------------------------------------------------------------

// TestTransformMode_RunningStatus_IsTransformSpecific verifies that when the model
// transitions to screenRunning after a transform path confirmation, the in-flight status
// message is transform-specific and does not use the deployment wording.
func TestTransformMode_RunningStatus_IsTransformSpecific(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	agentFile := createTransformAgentFile(t, workspace)
	m := newFlowModel(newTransformFlowSvc(workspace), agentFile)

	// Navigate to transform mode workspace screen.
	navigateToTransformMode(m)

	// Confirm the file path — transitions to screenRunning.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenRunning {
		t.Fatalf("screen = %v after transform path confirmation; want screenRunning; "+
			"I6.5 (path-kind wiring) must be delivered for the statusMsg to be tested", m.screen)
	}

	// Assert: statusMsg must not be the generic deployment wording.
	if m.statusMsg == "Running deployment…" {
		t.Errorf("statusMsg = %q during transform run; want transform-specific wording, not 'Running deployment…'",
			m.statusMsg)
	}
	if m.statusMsg == "" {
		t.Error("statusMsg is empty during transform run; want transform-specific text")
	}
	// The message should reference the transform operation.
	if !strings.Contains(strings.ToLower(m.statusMsg), "transform") {
		t.Errorf("statusMsg = %q during transform run; want text containing 'transform'", m.statusMsg)
	}
}

// ---------------------------------------------------------------------------
// T6.6: Target-harness selection step between mode and workspace
// ---------------------------------------------------------------------------

// TestTransformMode_AfterModeSelection_AdvancesToWorkspaceScreen verifies that selecting
// ModeTransformHarness advances directly to screenWorkspace. The target-harness and
// target-model selection steps are handled via the domain.Interaction port overlay during
// service execution — they are not pre-workspace TUI screens.
//
// This is the contracted design: QTransformTargetHarness routes through the existing
// selectOverlay path (verified by TestInteractionRouting_QTransformTargetHarness_ShowsSelectionOverlay
// in interaction_routing_test.go) and QTransformTargetModel routes through modelOverlay
// (verified by TestInteractionRouting_QTransformTargetModel_ShowsModelSelectScreen).
// The TUI's updateMode handler sets PathKindFileOrDirectory and advances to screenWorkspace,
// matching the pattern every other mode follows: mode selection → workspace.
func TestTransformMode_AfterModeSelection_AdvancesToWorkspaceScreen(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	m := newFlowModel(newTransformFlowSvc(workspace), workspace)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → mode
	if m.screen != screenMode {
		t.Fatalf("precondition: screen = %v, want screenMode", m.screen)
	}

	// Navigate to ModeTransformHarness (Down x4) and confirm.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})  // → transform-harness
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode

	if m.selections.mode != domain.ModeTransformHarness {
		t.Fatalf("precondition: selections.mode = %q; ModeTransformHarness item not present; "+
			"TestModeScreen_IncludesTransformHarnessEntry covers this separately", m.selections.mode)
	}

	// Assert: mode selection advances directly to screenWorkspace. Target-harness and
	// target-model selection are overlay interactions during service execution, not
	// pre-workspace TUI screens.
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after selecting ModeTransformHarness; want screenWorkspace; "+
			"the contracted design routes QTransformTargetHarness and QTransformTargetModel "+
			"through the domain.Interaction port overlay, not through pre-workspace TUI screens; "+
			"updateMode must advance to screenWorkspace immediately after mode confirmation",
			m.screen)
	}
}

// ---------------------------------------------------------------------------
// Regression: non-transform modes keep their path kind
// ---------------------------------------------------------------------------

// TestDeployMode_AfterTransformSelection_StillAcceptsDirectory verifies that after
// navigating back from the transform mode workspace screen and re-selecting deploy-new,
// the path screen switches back to directory kind and accepts a workspace directory.
// This guards against the transform path kind persisting into subsequent mode selections.
func TestDeployMode_AfterTransformSelection_StillAcceptsDirectory(t *testing.T) {
	// Arrange: pre-fill with a file for the transform leg; deploy-new needs a directory.
	workspace := t.TempDir()
	agentFile := createTransformAgentFile(t, workspace)
	m := newFlowModel(newTransformFlowSvc(workspace), agentFile)

	// Navigate to transform workspace screen.
	navigateToTransformMode(m)

	if m.screen != screenWorkspace {
		t.Fatalf("precondition: screen = %v, want screenWorkspace (transform)", m.screen)
	}

	// Esc back to mode screen.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenMode {
		t.Fatalf("screen = %v after Esc from transform workspace; want screenMode", m.screen)
	}

	// Navigate to deploy-new (Up x4 from transform = index 0) and select it.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // transform → promote
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // promote → workflows-only
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // workflows-only → update
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // update → deploy-new
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after selecting deploy-new; want screenWorkspace", m.screen)
	}

	// Switch pre-fill to a workspace directory for the deploy-new leg.
	m.wsScreen.SetPrefilledPath(workspace)

	// Act: Enter on the workspace directory.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert: directory accepted in deploy-new (directory kind) after back from transform.
	if m.screen != screenRunning {
		t.Errorf("screen = %v after Enter on directory in deploy-new mode following transform selection; "+
			"want screenRunning; switching from transform to deploy-new must re-apply directory-kind validation",
			m.screen)
	}
}
