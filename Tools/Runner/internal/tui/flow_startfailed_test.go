package tui

// Tests for how the TUI presents a failed run start (commit setup or
// pre-consultation failed after the artifact was created).

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// TestFlow_StartFailedOutcome_ShowsFailureAndResumableOnDoneScreen verifies
// that after a failed start the TUI shows the outcome screen with the failure
// message, an explicit start-failure title, and a statement that the run can be
// resumed.
func TestFlow_StartFailedOutcome_ShowsFailureAndResumableOnDoneScreen(t *testing.T) {
	const msg = "commit setup failed: no branch marker reported"
	m := newFlowModel(domain.RunOutcome{Status: domain.RunStartFailed})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunStartFailed, Message: msg}})

	if m.screen != screenDone {
		t.Fatalf("screen = %v, want screenDone after a failed start", m.screen)
	}
	view := m.View()
	if !strings.Contains(view, msg) {
		t.Errorf("done screen does not show the failure message %q:\n%s", msg, view)
	}
	lower := strings.ToLower(view)
	if !strings.Contains(lower, "start failed") {
		t.Errorf("done screen does not carry a start-failure title:\n%s", view)
	}
	if !strings.Contains(lower, "resum") {
		t.Errorf("done screen does not state that the run is resumable:\n%s", view)
	}
	if strings.Contains(view, "Run Complete") {
		t.Errorf("done screen shows the generic completion title for a failed start:\n%s", view)
	}
}

// TestCompletedMarker_NotWrittenOnRunStartFailed verifies that SetPhase is not
// called for a start failure: the run must stay resumable.
func TestCompletedMarker_NotWrittenOnRunStartFailed(t *testing.T) {
	spy := &tuiSpyStore{}
	m := newCompletionModel(spy, "/some/run/folder")
	m.screen = screenProgress

	m.Update(runDoneMsg{outcome: domain.RunOutcome{Status: domain.RunStartFailed, Message: "pre-consultation failed"}})

	if len(spy.setCalls) != 0 {
		t.Errorf("SetPhase called %d time(s), want 0 when status is RunStartFailed", len(spy.setCalls))
	}
}
