package runflow

// Tests for the outcome screen's presentation of a failed run start.

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// TestDoneScreen_View_StartFailed_ShowsTitleMessageAndResumable asserts that a
// start-failed outcome is presented as such: its own title, the outcome
// message, and a statement that the run is resumable.
func TestDoneScreen_View_StartFailed_ShowsTitleMessageAndResumable(t *testing.T) {
	outcome := domain.RunOutcome{Status: domain.RunStartFailed, Message: "pre-consultation failed: timed out"}
	s := NewDoneScreen(outcome, "", 80, 24, progressStyles())

	view := s.View()

	lower := strings.ToLower(view)
	if !strings.Contains(lower, "start failed") {
		t.Errorf("View() lacks a start-failure title:\n%s", view)
	}
	if !strings.Contains(view, outcome.Message) {
		t.Errorf("View() does not show the outcome message:\n%s", view)
	}
	if !strings.Contains(lower, "resum") {
		t.Errorf("View() does not state that the run is resumable:\n%s", view)
	}
}

// TestDoneScreen_StartFailed_ExitKeysStillDismiss asserts that a start-failed
// screen is dismissed by the usual exit key and offers no in-screen continue.
func TestDoneScreen_StartFailed_ExitKeysStillDismiss(t *testing.T) {
	s := newDoneScreen(domain.RunStartFailed)

	pressDoneKey(s, "c")
	if s.Continue() {
		t.Error("Continue() = true after 'c' on a RunStartFailed screen; want false (resume goes through run selection)")
	}
	pressDoneKey(s, "q")
	if !s.Done() {
		t.Error("Done() = false after 'q' on a RunStartFailed screen; want true")
	}
}
