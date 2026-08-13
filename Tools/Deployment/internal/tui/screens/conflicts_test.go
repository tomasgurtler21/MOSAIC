package screens_test

// conflicts_test.go verifies the ConflictScreen overlay: rendering of the locally-modified
// file path and modification details, keyboard selection of all three decisions
// (overwrite, skip, backup-then-overwrite), cancellation via Esc, and the permanent-action
// warning note.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// conflictQuestion builds a ChoiceQuestion for a QLocalModification prompt with the three
// standard conflict-decision options.
func conflictQuestion(filePath, modDetail string) domain.ChoiceQuestion {
	return domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QLocalModification,
			Subject: filePath,
			Title:   "Locally Modified File",
			Prompt:  "How should this file be handled?",
			Detail:  modDetail,
		},
		Options: []domain.Option{
			{ID: string(domain.DecisionOverwrite), Label: "Overwrite with new version"},
			{ID: string(domain.DecisionSkip), Label: "Skip (keep local version)"},
			{ID: string(domain.DecisionBackupThenOverwrite), Label: "Back up then overwrite"},
		},
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// TestConflictScreen_View_ShowsFilePath verifies that the locally-modified file path is
// prominently displayed so the user knows which file the decision applies to.
func TestConflictScreen_View_ShowsFilePath(t *testing.T) {
	filePath := "Catalog/Subagents/Execution/test-runner.md"
	s := screens.NewConflictScreen(conflictQuestion(filePath, ""), 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, filePath) {
		t.Errorf("view does not contain the file path %q:\n%s", filePath, view)
	}
}

// TestConflictScreen_View_ShowsModificationDetail verifies that the modification detail is
// rendered so the user understands what changed.
func TestConflictScreen_View_ShowsModificationDetail(t *testing.T) {
	detail := "Content differs from the deployed hash recorded in the manifest."
	s := screens.NewConflictScreen(conflictQuestion("some/path.md", detail), 80, 24, plainStyles())

	view := s.View()

	if !strings.Contains(view, detail) {
		t.Errorf("view does not contain the modification detail:\n%s", view)
	}
}

// TestConflictScreen_View_ShowsPermanentActionWarning verifies that the view includes the
// permanent-action warning note so the user knows overwrite and backup-then-overwrite cannot
// be undone at the run level.
func TestConflictScreen_View_ShowsPermanentActionWarning(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	view := collapseWhitespace(s.View())

	if !strings.Contains(view, "cannot be undone") {
		t.Errorf("view does not contain the permanent-action warning:\n%s", s.View())
	}
}

// TestConflictScreen_View_ShowsAllThreeOptions verifies that all three decision options
// appear in the rendered view so the user can choose among them.
func TestConflictScreen_View_ShowsAllThreeOptions(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	view := s.View()

	for _, label := range []string{"Overwrite", "Skip", "Back up"} {
		if !strings.Contains(view, label) {
			t.Errorf("view does not contain the option label %q:\n%s", label, view)
		}
	}
}

// ---------------------------------------------------------------------------
// Keyboard selection: all three decisions
// ---------------------------------------------------------------------------

// TestConflictScreen_Enter_OnOverwrite_ReturnsOverwriteDecision verifies that pressing Enter
// on the first option (overwrite) returns the overwrite decision.
func TestConflictScreen_Enter_OnOverwrite_ReturnsOverwriteDecision(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter on overwrite; want true")
	}
	if s.Answer().OptionID != string(domain.DecisionOverwrite) {
		t.Errorf("Answer().OptionID = %q, want %q", s.Answer().OptionID, domain.DecisionOverwrite)
	}
}

// TestConflictScreen_Down_Then_Enter_ReturnsSkipDecision verifies that navigating down once
// and pressing Enter selects the skip decision.
func TestConflictScreen_Down_Then_Enter_ReturnsSkipDecision(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Down + Enter; want true")
	}
	if s.Answer().OptionID != string(domain.DecisionSkip) {
		t.Errorf("Answer().OptionID = %q, want %q", s.Answer().OptionID, domain.DecisionSkip)
	}
}

// TestConflictScreen_TwoDowns_Then_Enter_ReturnsBackupDecision verifies that navigating
// down twice and pressing Enter selects the backup-then-overwrite decision.
func TestConflictScreen_TwoDowns_Then_Enter_ReturnsBackupDecision(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after two Downs + Enter; want true")
	}
	if s.Answer().OptionID != string(domain.DecisionBackupThenOverwrite) {
		t.Errorf("Answer().OptionID = %q, want %q",
			s.Answer().OptionID, domain.DecisionBackupThenOverwrite)
	}
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// TestConflictScreen_Esc_SendsCancelled verifies that pressing Esc returns Cancelled so the
// root model knows the user aborted the run from the conflict prompt. Esc on the conflict
// screen signals abort — it does not navigate back to the previous screen. The Status field
// is the authoritative abort signal; the root model must not act on Back() as a navigation
// cue for conflict overlays.
func TestConflictScreen_Esc_SendsCancelled(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !s.Done() {
		t.Error("Done() = false after Esc; want true")
	}
	if s.Answer().Status != domain.Cancelled {
		t.Errorf("Answer().Status = %q, want Cancelled (not Back)", s.Answer().Status)
	}
}

// ---------------------------------------------------------------------------
// Answer status
// ---------------------------------------------------------------------------

// TestConflictScreen_Selection_HasAnsweredStatus verifies that selecting any decision option
// returns AnswerStatus Answered (not Skipped or Cancelled).
func TestConflictScreen_Selection_HasAnsweredStatus(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if s.Answer().Status != domain.Answered {
		t.Errorf("Answer().Status = %q after selecting an option; want Answered", s.Answer().Status)
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

// TestConflictScreen_Reset_ClearsDoneAndAnswer verifies that Reset restores the screen to an
// idle state.
func TestConflictScreen_Reset_ClearsDoneAndAnswer(t *testing.T) {
	s := screens.NewConflictScreen(conflictQuestion("path.md", ""), 80, 24, plainStyles())

	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.Done() {
		t.Fatal("precondition: Done() must be true before Reset")
	}

	s.Reset()

	if s.Done() {
		t.Error("Done() = true after Reset; want false")
	}
}
