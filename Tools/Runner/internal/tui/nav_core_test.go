package tui

// nav_core_test.go verifies the rootModel navigation state machine: initial screen,
// harness-screen Enter-key outcome paths, window resize propagation, ctrl+c
// cancellation, and done-screen key handling.
//
// Tests are in package tui (internal) because screenID and rootModel fields are
// unexported. Tests drive the model through the Bubble Tea model/update cycle with no
// real terminal attached.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

func TestNavigation_InitialScreen_IsSetupHarness(t *testing.T) {
	m := newTestModel()
	if m.screen != screenSetupHarness {
		t.Errorf("initial screen = %v, want screenSetupHarness (%v)", m.screen, screenSetupHarness)
	}
}

func TestNavigation_HarnessScreen_ViewNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string on harness screen; want non-empty output")
	}
}

func TestNavigation_HarnessScreen_ViewContainsTitleText(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if !containsAny(view, "Harness", "harness", "Select", "select") {
		t.Errorf("harness screen view does not contain expected title text:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Harness screen: Enter-key outcome paths
// ---------------------------------------------------------------------------

// TestHarnessScreen_Enter_DiscoveryFailure_TransitionsToDoneScreen verifies that
// when OrchestratorDiscoverer returns an error after the user presses Enter on
// the harness screen, the model transitions to screenDone with the error
// displayed (AC3.4, TUI side).
func TestHarnessScreen_Enter_DiscoveryFailure_TransitionsToDoneScreen(t *testing.T) {
	failDiscoverer := func(workDir, harnessID string) (string, error) {
		return "", errors.New("workspace not deployed for harness " + harnessID +
			": expected orchestrator-script at " + workDir)
	}
	m := newTestModelWithDiscoverer(failDiscoverer)

	if m.screen != screenSetupHarness {
		t.Fatalf("precondition: screen = %v, want screenSetupHarness", m.screen)
	}

	// The harness screen has at least one item pre-selected; pressing Enter
	// causes Done() == true and triggers auto-discovery.
	sendKey(m, tea.KeyEnter)

	if m.screen != screenDone {
		t.Errorf("screen = %v after Enter with failing discoverer, want screenDone (%v)",
			m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Error("doneScreen = nil after discovery failure; must be populated with the error")
	}
}

// TestHarnessScreen_Enter_DiscoverySuccess_TransitionsToWorkflowScreen verifies
// that when OrchestratorDiscoverer returns a valid orchestrator path, pressing
// Enter on the harness screen transitions to screenSetupWorkflow and populates
// selections.orchestratorFile (AC3.2, TUI side integration).
//
// This is a new-run test: the workflow screen is only ever shown to a run that
// has recorded no workflow of its own. The subject here is orchestrator
// discovery, not run-mode routing, so the run mode is set explicitly rather
// than left at its zero value -- which now means "resumed".
func TestHarnessScreen_Enter_DiscoverySuccess_TransitionsToWorkflowScreen(t *testing.T) {
	// Write a minimal orchestrator file with one workflow region so that
	// orchfile.EnumerateWorkflows returns successfully.
	orchDir := t.TempDir()
	orchFilePath := filepath.Join(orchDir, "orchestrator-script.md")
	orchContent := `<Workflow type="core" name="test-workflow" version="1.0">
## Test Workflow

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | TRUE | - | Plan.md |
</Workflow>
`
	if err := os.WriteFile(orchFilePath, []byte(orchContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	succeedDiscoverer := func(workDir, harnessID string) (string, error) {
		return orchFilePath, nil
	}
	m := newTestModelWithDiscoverer(succeedDiscoverer)
	m.selections.isNewRun = true

	if m.screen != screenSetupHarness {
		t.Fatalf("precondition: screen = %v, want screenSetupHarness", m.screen)
	}

	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupWorkflow {
		t.Errorf("screen = %v after Enter with successful discoverer, want screenSetupWorkflow (%v)",
			m.screen, screenSetupWorkflow)
	}
	if m.selections.orchestratorFile != orchFilePath {
		t.Errorf("selections.orchestratorFile = %q, want %q",
			m.selections.orchestratorFile, orchFilePath)
	}
}

// ---------------------------------------------------------------------------
// ctrl+c cancellation
// ---------------------------------------------------------------------------

func TestNavigation_CtrlC_CancelsContextAndQuits(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c; context must be cancelled")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c; want non-nil tea.Quit command")
	}
}

func TestNavigation_CtrlC_ModelHasCancellableContext(t *testing.T) {
	m := newTestModel()
	if m.ctxCancel == nil {
		t.Error("rootModel must have a non-nil ctxCancel function")
	}
	if m.ctx == nil {
		t.Error("rootModel must have a non-nil ctx")
	}
}

// ---------------------------------------------------------------------------
// Window resize propagation
// ---------------------------------------------------------------------------

func TestNavigation_WindowResize_UpdatesDimensions(t *testing.T) {
	m := newTestModel()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resize message caused a panic: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Errorf("after resize: width=%d height=%d, want 120/40", m.width, m.height)
	}
}

func TestNavigation_WindowResize_WithProgressScreenDoesNotPanic(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resize with progress screen caused a panic: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
}

// ---------------------------------------------------------------------------
// Esc from harness screen
// ---------------------------------------------------------------------------

func TestNavigation_EscFromHarnessScreen_ReturnsQuitCommand(t *testing.T) {
	m := newTestModel()
	_, cmd := sendKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Error("cmd = nil after Esc from harness screen; want tea.Quit (non-nil command)")
	}
}

// ---------------------------------------------------------------------------
// Done screen
// ---------------------------------------------------------------------------

func TestNavigation_RunDoneMsg_TransitionsToDoneScreen(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	if m.screen != screenDone {
		t.Errorf("screen = %v after runDoneMsg, want screenDone (%v)", m.screen, screenDone)
	}
	if m.doneScreen == nil {
		t.Error("doneScreen = nil after runDoneMsg; must be populated")
	}
}

func TestNavigation_RunErrorMsg_TransitionsToDoneScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runErrorMsg{err: errors.New("session failed")})
	if m.screen != screenDone {
		t.Errorf("screen = %v after runErrorMsg, want screenDone (%v)", m.screen, screenDone)
	}
}

func TestNavigation_DoneScreen_QKeyQuits(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone (%v)", m.screen, screenDone)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("cmd = nil after 'q' on done screen; want tea.Quit")
	}
}

func TestNavigation_DoneScreen_EnterKeyQuits(t *testing.T) {
	m := newTestModel()
	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "done"}})
	_, cmd := sendKey(m, tea.KeyEnter)
	if cmd == nil {
		t.Error("cmd = nil after Enter on done screen; want tea.Quit")
	}
}
