package tui

// navigation_test.go verifies the rootModel navigation state machine: forward transitions
// between entry screens, back navigation with prior-input preservation, and cancellation.
//
// These tests are in package tui (internal) because the screenID type and rootModel fields
// are unexported. The tests drive the model through the Bubble Tea model/update cycle with
// no real terminal attached.
//
// Navigation contract exercised:
//   harness → (enter) → mode → (enter) → workspace → (enter) → running
//   esc from workspace → mode  (prior mode selection preserved)
//   esc from mode      → harness (prior harness preserved)
//   esc from harness   → quit (nothing to go back to)
//   ctrl+c from any screen → context cancelled, quit command returned

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Stub app.Service for navigation tests
// ---------------------------------------------------------------------------

// stubNavService satisfies app.Service with fixed, non-blocking responses. Navigation tests
// only need ListHarnesses; the deploy methods are never called during entry-screen testing.
type stubNavService struct {
	harnesses []domain.HarnessRef
}

func (s *stubNavService) ListHarnesses() []domain.HarnessRef { return s.harnesses }
func (s *stubNavService) DeployNew(_ context.Context, _ app.DeployRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *stubNavService) Update(_ context.Context, _ app.UpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}
func (s *stubNavService) Promote(_ context.Context, _ app.PromoteRequest) (app.PromoteResult, error) {
	return app.PromoteResult{}, nil
}
func (s *stubNavService) UpdateWorkflows(_ context.Context, _ app.WorkflowUpdateRequest) (domain.RunSummary, error) {
	return domain.RunSummary{}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var navUsableHarness = domain.HarnessRef{
	ID:          "nav-harness",
	DisplayName: "Nav Harness",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// newNavModel creates a rootModel wired with a single usable harness and no pre-fills.
func newNavModel() *rootModel {
	svc := &stubNavService{harnesses: []domain.HarnessRef{navUsableHarness}}
	return newRootModel(context.Background(), svc, Options{
		Theme: DefaultTheme(),
	})
}

// sendKey drives the model with a single key message and returns the updated model and cmd.
// Since rootModel uses pointer receivers, the returned model is always the same *rootModel.
func sendKey(m *rootModel, keyType tea.KeyType) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: keyType})
}

// sendRune drives the model with a single character key message.
func sendRune(m *rootModel, r rune) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// ---------------------------------------------------------------------------
// Initial state
// ---------------------------------------------------------------------------

// TestNavigation_InitialScreen_IsHarness verifies that the root model starts on the harness
// selection screen when no harness is pre-filled.
func TestNavigation_InitialScreen_IsHarness(t *testing.T) {
	// Arrange / Act
	m := newNavModel()

	// Assert
	if m.screen != screenHarness {
		t.Errorf("initial screen = %v, want screenHarness (%v)", m.screen, screenHarness)
	}
}

// TestNavigation_PrefilledHarness_StartsOnModeScreen verifies that when a harness ID is
// pre-filled in the InitialRequest, the TUI skips the harness screen and starts on mode.
func TestNavigation_PrefilledHarness_StartsOnModeScreen(t *testing.T) {
	// Arrange
	svc := &stubNavService{harnesses: []domain.HarnessRef{navUsableHarness}}
	m := newRootModel(context.Background(), svc, Options{
		Theme: DefaultTheme(),
		InitialRequest: &app.DeployRequest{
			HarnessID: "nav-harness",
		},
	})

	// Assert
	if m.screen != screenMode {
		t.Errorf("screen = %v with pre-filled harness, want screenMode (%v)", m.screen, screenMode)
	}
	if m.selections.harnessID != "nav-harness" {
		t.Errorf("selections.harnessID = %q, want %q", m.selections.harnessID, "nav-harness")
	}
}

// ---------------------------------------------------------------------------
// Forward transitions
// ---------------------------------------------------------------------------

// TestNavigation_HarnessEnter_TransitionsToMode verifies the first forward transition:
// pressing Enter on a usable harness advances the root model to the mode selection screen.
func TestNavigation_HarnessEnter_TransitionsToMode(t *testing.T) {
	// Arrange
	m := newNavModel()
	if m.screen != screenHarness {
		t.Fatal("precondition: must start on harness screen")
	}

	// Act — Enter on the harness list (first item is the usable harness)
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen != screenMode {
		t.Errorf("screen = %v after harness Enter, want screenMode (%v)", m.screen, screenMode)
	}
}

// TestNavigation_HarnessEnter_RecordsSelectedHarnessID verifies that the selected harness ID
// is captured in the model's selections so the service call receives the correct harness.
func TestNavigation_HarnessEnter_RecordsSelectedHarnessID(t *testing.T) {
	// Arrange
	m := newNavModel()

	// Act
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.harnessID != "nav-harness" {
		t.Errorf("selections.harnessID = %q after harness selection, want %q", m.selections.harnessID, "nav-harness")
	}
}

// TestNavigation_ModeEnter_TransitionsToWorkspace verifies the second forward transition:
// pressing Enter on a mode option advances the root model to the workspace path entry screen.
func TestNavigation_ModeEnter_TransitionsToWorkspace(t *testing.T) {
	// Arrange — advance to mode screen first
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode
	if m.screen != screenMode {
		t.Fatal("precondition: must be on mode screen")
	}

	// Act — Enter on the mode list (first mode is deploy-new)
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.screen != screenWorkspace {
		t.Errorf("screen = %v after mode Enter, want screenWorkspace (%v)", m.screen, screenWorkspace)
	}
}

// TestNavigation_ModeEnter_RecordsSelectedMode verifies that the chosen run mode is captured
// in the model's selections so the service call uses the correct mode.
func TestNavigation_ModeEnter_RecordsSelectedMode(t *testing.T) {
	// Arrange — advance to mode screen
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode

	// Act — Enter on first mode (deploy-new)
	sendKey(m, tea.KeyEnter)

	// Assert
	if m.selections.mode != domain.ModeDeployNew {
		t.Errorf("selections.mode = %q, want %q (deploy-new is the first mode item)", m.selections.mode, domain.ModeDeployNew)
	}
}

// ---------------------------------------------------------------------------
// Back navigation — state preservation
// ---------------------------------------------------------------------------

// TestNavigation_HarnessEsc_Quits verifies that pressing Esc on the harness screen returns
// a quit command. There is nothing to go back to from the first entry screen.
func TestNavigation_HarnessEsc_Quits(t *testing.T) {
	// Arrange
	m := newNavModel()

	// Act
	_, cmd := sendKey(m, tea.KeyEsc)

	// Assert — a non-nil command signals tea.Quit (quit is a func value, not comparable,
	// but its presence indicates the model has issued a quit command).
	if cmd == nil {
		t.Error("cmd = nil after Esc from harness screen; want tea.Quit (non-nil command)")
	}
}

// TestNavigation_ModeEsc_TransitionsBackToHarness verifies that pressing Esc on the mode
// screen returns the user to the harness selection screen.
func TestNavigation_ModeEsc_TransitionsBackToHarness(t *testing.T) {
	// Arrange — advance to mode screen
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode

	// Act
	sendKey(m, tea.KeyEsc) // mode → harness

	// Assert
	if m.screen != screenHarness {
		t.Errorf("screen = %v after mode Esc, want screenHarness (%v)", m.screen, screenHarness)
	}
}

// TestNavigation_ModeEsc_PreservesHarnessSelection verifies that pressing Esc on the mode
// screen does not lose the harness ID that was selected on the previous screen. The user
// should be able to go back and return without losing prior valid input.
func TestNavigation_ModeEsc_PreservesHarnessSelection(t *testing.T) {
	// Arrange — select harness and advance to mode
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // select harness, advance to mode
	capturedHarness := m.selections.harnessID

	// Act — go back from mode to harness
	sendKey(m, tea.KeyEsc)

	// Assert
	if m.selections.harnessID != capturedHarness {
		t.Errorf("selections.harnessID = %q after mode Esc; want %q (prior selection must survive back navigation)", m.selections.harnessID, capturedHarness)
	}
}

// TestNavigation_WorkspaceEsc_TransitionsBackToMode verifies that pressing Esc on the
// workspace screen returns the user to the mode selection screen.
func TestNavigation_WorkspaceEsc_TransitionsBackToMode(t *testing.T) {
	// Arrange — advance to workspace screen
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode
	sendKey(m, tea.KeyEnter) // mode → workspace

	// Act
	sendKey(m, tea.KeyEsc) // workspace → mode

	// Assert
	if m.screen != screenMode {
		t.Errorf("screen = %v after workspace Esc, want screenMode (%v)", m.screen, screenMode)
	}
}

// TestNavigation_WorkspaceEsc_PreservesModeSelection verifies that pressing Esc on the
// workspace screen does not lose the mode that was chosen on the mode screen.
func TestNavigation_WorkspaceEsc_PreservesModeSelection(t *testing.T) {
	// Arrange
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode
	sendKey(m, tea.KeyEnter) // mode → workspace (first mode = deploy-new)
	capturedMode := m.selections.mode

	// Act
	sendKey(m, tea.KeyEsc) // workspace → mode

	// Assert
	if m.selections.mode != capturedMode {
		t.Errorf("selections.mode = %q after workspace Esc; want %q (prior mode must survive back navigation)", m.selections.mode, capturedMode)
	}
}

// ---------------------------------------------------------------------------
// ctrl+c — global cancel from any entry screen
// ---------------------------------------------------------------------------

// TestNavigation_CtrlC_FromHarness_CancelsContextAndQuits verifies that ctrl+c from the
// harness screen cancels the model's context (which would cancel any running service call)
// and returns a quit command.
func TestNavigation_CtrlC_FromHarness_CancelsContextAndQuits(t *testing.T) {
	// Arrange
	m := newNavModel()

	// Act
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Assert
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c; context must be cancelled so any running service goroutine is stopped")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c; want tea.Quit (non-nil command)")
	}
}

// TestNavigation_CtrlC_FromMode_CancelsContextAndQuits verifies that ctrl+c works from the
// mode selection screen, not only from the harness screen.
func TestNavigation_CtrlC_FromMode_CancelsContextAndQuits(t *testing.T) {
	// Arrange — advance to mode screen
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode

	// Act
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Assert
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c from mode screen; context must be cancelled from any screen")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c from mode screen; want tea.Quit")
	}
}

// TestNavigation_CtrlC_FromWorkspace_CancelsContextAndQuits verifies that ctrl+c works from
// the workspace path entry screen.
func TestNavigation_CtrlC_FromWorkspace_CancelsContextAndQuits(t *testing.T) {
	// Arrange — advance to workspace screen
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode
	sendKey(m, tea.KeyEnter) // mode → workspace

	// Act
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Assert
	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c from workspace screen; context must be cancelled from any screen")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c from workspace screen; want tea.Quit")
	}
}

// ---------------------------------------------------------------------------
// View rendering — screen-specific output
// ---------------------------------------------------------------------------

// TestNavigation_HarnessScreen_ViewContainsTitleText verifies that the harness selection
// screen's view contains a title, confirming the correct screen is rendered.
func TestNavigation_HarnessScreen_ViewContainsTitleText(t *testing.T) {
	// Arrange
	m := newNavModel()

	// Act
	view := m.View()

	// Assert
	if view == "" {
		t.Error("View() returned empty string on harness screen; want non-empty output")
	}
	// The harness screen title is "Select Harness" (from screens/harness.go)
	if !containsAny(view, "Select Harness", "Harness", "harness") {
		t.Errorf("harness screen view does not appear to show the harness selection title:\n%s", view)
	}
}

// TestNavigation_ModeScreen_ViewContainsTitleText verifies that after transitioning to mode,
// the view reflects the mode selection screen.
func TestNavigation_ModeScreen_ViewContainsTitleText(t *testing.T) {
	// Arrange
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode

	// Act
	view := m.View()

	// Assert
	if view == "" {
		t.Error("View() returned empty string on mode screen; want non-empty output")
	}
	if !containsAny(view, "Select Mode", "Mode", "deploy", "update") {
		t.Errorf("mode screen view does not appear to show the mode selection content:\n%s", view)
	}
}

// TestNavigation_WorkspaceScreen_ViewContainsTitleText verifies that after transitioning to
// the workspace screen, the view reflects the workspace path entry screen.
func TestNavigation_WorkspaceScreen_ViewContainsTitleText(t *testing.T) {
	// Arrange
	m := newNavModel()
	sendKey(m, tea.KeyEnter) // harness → mode
	sendKey(m, tea.KeyEnter) // mode → workspace

	// Act
	view := m.View()

	// Assert
	if view == "" {
		t.Error("View() returned empty string on workspace screen; want non-empty output")
	}
	if !containsAny(view, "Workspace", "workspace", "path", "directory") {
		t.Errorf("workspace screen view does not appear to show the workspace entry content:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Window resize propagation
// ---------------------------------------------------------------------------

// TestNavigation_WindowResize_PropagatedToAllScreens verifies that a WindowSizeMsg updates
// the model dimensions without causing a panic. All screens must accept resize events.
func TestNavigation_WindowResize_PropagatedToAllScreens(t *testing.T) {
	// Arrange
	m := newNavModel()

	// Act — send a resize; must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resize message caused a panic: %v", r)
		}
	}()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Assert
	if m.width != 120 || m.height != 40 {
		t.Errorf("after resize: width=%d height=%d, want 120/40", m.width, m.height)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
