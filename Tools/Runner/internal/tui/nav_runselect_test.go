package tui

// nav_runselect_test.go verifies the rootModel's routing to and through the
// run-selection screen: when it appears, when it is skipped, and the
// Enter/Esc key behaviors on it.
//
// Tests are in package tui (internal) because screenID and rootModel fields are
// unexported. Tests drive the model through the Bubble Tea model/update cycle with no
// real terminal attached.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
)

// ---------------------------------------------------------------------------
// Run selection screen
// ---------------------------------------------------------------------------

// TestRunSelect_InitialScreen_IsRunSelectWithMultipleCandidates verifies that when the
// scan result carries more than one candidate and no run is pre-resolved, the TUI starts
// on the run-selection screen.
func TestRunSelect_InitialScreen_IsRunSelectWithMultipleCandidates(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Errorf("initial screen = %v, want screenRunSelect (%v)", m.screen, screenRunSelect)
	}
	if m.runSelectScreen == nil {
		t.Error("runSelectScreen = nil; must be constructed when multiple candidates exist")
	}
}

// TestRunSelect_InitialScreen_IsSetupHarnessWithZeroCandidates verifies that an empty scan
// result (no resumable runs) skips the run-selection screen and goes to the harness screen.
func TestRunSelect_InitialScreen_IsSetupHarnessWithZeroCandidates(t *testing.T) {
	m := newModelWithScan(nil)
	if m.screen != screenSetupHarness {
		t.Errorf("initial screen = %v, want screenSetupHarness (%v) for zero candidates", m.screen, screenSetupHarness)
	}
	if m.runSelectScreen != nil {
		t.Error("runSelectScreen should be nil when there are zero candidates")
	}
}

// TestRunSelect_InitialScreen_IsRunSelectWithOneCandidate is the TUI-side
// expression of the core defect this stage removes (AC2.2): a workspace with
// exactly one resumable run must reach the run-selection screen, not bypass
// it. The screen renders "start a new run" plus the single candidate, and
// the user's choice (not the candidate count) decides the outcome.
//
// Currently fails (RED): the TUI still skips the screen for a single
// candidate (the `len(...) > 1` gate in newRootModel).
func TestRunSelect_InitialScreen_IsRunSelectWithOneCandidate(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Errorf("initial screen = %v, want screenRunSelect (%v) for a single candidate; "+
			"exactly one resumable run must not bypass the selection screen", m.screen, screenRunSelect)
	}
	if m.runSelectScreen == nil {
		t.Error("runSelectScreen = nil; must be constructed when exactly one candidate exists")
	}
}

// TestRunSelect_SkippedWhenResolvedRunIDSet verifies that when ResolvedRunID is populated
// (i.e. --run was given), the TUI starts directly on the harness screen.
func TestRunSelect_SkippedWhenResolvedRunIDSet(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:         tuicommon.DefaultTheme(),
		ScanResult:    &runscan.ScanResult{Candidates: candidates},
		ResolvedRunID: "20260701T120000Z-a3f9",
	})
	if m.screen != screenSetupHarness {
		t.Errorf("initial screen = %v, want screenSetupHarness (%v) when ResolvedRunID is set", m.screen, screenSetupHarness)
	}
}

// TestRunSelect_SkippedWhenIsNewRunSet verifies that when IsNewRun is true
// (i.e. --new-run was given), the TUI starts directly on the harness screen.
func TestRunSelect_SkippedWhenIsNewRunSet(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		ScanResult: &runscan.ScanResult{Candidates: candidates},
		IsNewRun:   true,
	})
	if m.screen != screenSetupHarness {
		t.Errorf("initial screen = %v, want screenSetupHarness (%v) when IsNewRun is set", m.screen, screenSetupHarness)
	}
}

// TestRunSelect_ViewShowsCandidates verifies that the run-selection screen view contains
// each candidate's run_id and the "Start a new run" entry.
func TestRunSelect_ViewShowsCandidates(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}
	view := m.View()
	if !containsStr(view, "20260701T120000Z-a3f9") {
		t.Errorf("run select view does not contain first candidate run_id:\n%s", view)
	}
	if !containsStr(view, "20260702T120000Z-b4e8") {
		t.Errorf("run select view does not contain second candidate run_id:\n%s", view)
	}
	if !containsAny(view, "Start", "new run", "new") {
		t.Errorf("run select view does not contain 'Start a new run' entry:\n%s", view)
	}
}

// TestRunSelect_EscQuitsProgram verifies that Esc from the run-selection screen issues
// a quit command (there is no previous screen to go back to).
func TestRunSelect_EscQuitsProgram(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	_, cmd := sendKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Error("cmd = nil after Esc from run-selection screen; want tea.Quit (non-nil)")
	}
}

// TestRunSelect_EnterOnNewRun_SetsIsNewRunAndAdvances verifies that pressing Enter on
// the "Start a new run" entry (the first item, which is always selected initially) sets
// isNewRun=true and transitions to the file screen.
func TestRunSelect_EnterOnNewRun_SetsIsNewRunAndAdvances(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// The first item is "Start a new run" (NewRunSentinelID). Press Enter to select it.
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupHarness {
		t.Errorf("screen = %v after selecting 'Start new run', want screenSetupHarness (%v)", m.screen, screenSetupHarness)
	}
	if !m.selections.isNewRun {
		t.Error("selections.isNewRun = false after selecting 'Start new run'; want true")
	}
}

// TestRunSelect_EnterOnCandidate_SetsRunIDAndAdvances verifies that pressing Enter on
// a candidate entry populates the run identity and transitions to the file screen.
func TestRunSelect_EnterOnCandidate_SetsRunIDAndAdvances(t *testing.T) {
	candidates := []runscan.RunCandidate{
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	m := newModelWithScan(candidates)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Navigate down once to move past "Start a new run" to the first candidate.
	sendKey(m, tea.KeyDown)
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupHarness {
		t.Errorf("screen = %v after selecting candidate, want screenSetupHarness (%v)", m.screen, screenSetupHarness)
	}
	if m.selections.isNewRun {
		t.Error("selections.isNewRun = true after selecting an existing candidate; want false")
	}
	if m.selections.runID != "20260701T120000Z-a3f9" {
		t.Errorf("selections.runID = %q, want %q", m.selections.runID, "20260701T120000Z-a3f9")
	}
	if m.selections.runFolder == "" {
		t.Error("selections.runFolder is empty after selecting candidate; want the candidate's folder path")
	}
}
