package tui

// nav_progress_test.go verifies progress-screen navigation (graceful stop,
// artifact view), question-overlay routing (select-one, confirm, ask-text),
// and progress-notice handling.
//
// Tests are in package tui (internal) because screenID and rootModel fields are
// unexported. Tests drive the model through the Bubble Tea model/update cycle with no
// real terminal attached.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-run/internal/session"
)

// ---------------------------------------------------------------------------
// Progress screen
// ---------------------------------------------------------------------------

func TestNavigation_ProgressScreen_GracefulStop_SignalsStopSignalWithoutCancellingContext(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newTestModel()
	m.stopSignal = stopSignal
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if !m.progressScreen.GracefulStop() {
		t.Error("GracefulStop() = false after 's' then 'y'; want true")
	}
	if !stopSignal.Requested() {
		t.Error("stopSignal.Requested() = false after a confirmed graceful stop; want true")
	}
	if m.ctx.Err() != nil {
		t.Errorf("m.ctx.Err() = %v after a confirmed graceful stop; want nil (ctx must remain usable for resume)", m.ctx.Err())
	}
}

func TestNavigation_ProgressScreen_ArtifactView_TransitionsToArtifactScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	// Set the orchestrator file to something that will fail gracefully.
	m.selections.orchestratorFile = "/nonexistent/Orchestrator.md"

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})

	if m.screen != screenArtifact {
		t.Errorf("screen = %v after 'a' key, want screenArtifact (%v)", m.screen, screenArtifact)
	}
}

func TestNavigation_ArtifactScreen_EscReturnsToProgress(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	m.selections.orchestratorFile = "/nonexistent/Orchestrator.md"

	// Enter artifact screen.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.screen != screenArtifact {
		t.Fatalf("precondition: screen = %v, want screenArtifact", m.screen)
	}

	// Press Esc to return.
	sendKey(m, tea.KeyEsc)
	if m.screen != screenProgress {
		t.Errorf("screen = %v after Esc from artifact screen, want screenProgress (%v)", m.screen, screenProgress)
	}
}

// ---------------------------------------------------------------------------
// Question overlay routing
// ---------------------------------------------------------------------------

func TestNavigation_SelectOneQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:    questionSelectOne,
		choiceQ: newTestChoiceQuestion("Pick one", []string{"opt-a", "opt-b"}),
		reply:   reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after SelectOne question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.selectOverlay == nil {
		t.Error("selectOverlay = nil after SelectOne question; must be populated")
	}
}

func TestNavigation_SelectOneQuestion_EnterSendsAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:    questionSelectOne,
		choiceQ: newTestChoiceQuestion("Pick one", []string{"opt-a", "opt-b"}),
		reply:   reply,
	})

	sendKey(m, tea.KeyEnter)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after Enter on select overlay, want screenProgress (%v)", m.screen, screenProgress)
	}
	select {
	case ans := <-reply:
		if ans.choiceAns.Status != interaction.Answered {
			t.Errorf("answer status = %q, want %q", ans.choiceAns.Status, interaction.Answered)
		}
	default:
		t.Error("no answer sent after Enter on select overlay")
	}
}

func TestNavigation_ConfirmQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:     questionConfirm,
		confirmQ: interaction.Question{Title: "Proceed?", Prompt: "Are you sure?"},
		reply:    reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after Confirm question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.confirmOverlay == nil {
		t.Error("confirmOverlay = nil after Confirm question; must be populated")
	}
}

func TestNavigation_ConfirmQuestion_EnterSendsAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:     questionConfirm,
		confirmQ: interaction.Question{Title: "Proceed?", Prompt: "Are you sure?"},
		reply:    reply,
	})

	sendKey(m, tea.KeyEnter)

	select {
	case ans := <-reply:
		if ans.confirmAns.Status != interaction.Answered {
			t.Errorf("confirm answer status = %q, want %q", ans.confirmAns.Status, interaction.Answered)
		}
	default:
		t.Error("no answer sent after Enter on confirm overlay")
	}
}

// ---------------------------------------------------------------------------
// AskText question overlay
// ---------------------------------------------------------------------------

// TestNavigation_AskTextQuestion_TransitionsToQuestionScreen verifies that a
// questionAskText message transitions the model to screenQuestion and populates
// the textOverlay field.
func TestNavigation_AskTextQuestion_TransitionsToQuestionScreen(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:  questionAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{Title: "Enter value:", Prompt: "type here"}},
		reply: reply,
	})

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after AskText question, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.textOverlay == nil {
		t.Error("textOverlay = nil after AskText question; must be populated")
	}
}

// TestNavigation_AskTextQuestion_EnterSendsTextAnswer verifies that pressing Enter on the
// AskText overlay sends a TextAnswer with Status == Answered through the reply channel and
// returns the screen to screenProgress.
func TestNavigation_AskTextQuestion_EnterSendsTextAnswer(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:  questionAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{Title: "Enter value:", Prompt: "type here"}},
		reply: reply,
	})

	if m.screen != screenQuestion {
		t.Fatalf("precondition: screen = %v, want screenQuestion", m.screen)
	}

	// Confirm the text overlay.
	sendKey(m, tea.KeyEnter)

	if m.screen != screenProgress {
		t.Errorf("screen = %v after Enter on text overlay, want screenProgress (%v)", m.screen, screenProgress)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered {
			t.Errorf("text answer Status = %q, want %q", ans.textAns.Status, interaction.Answered)
		}
	default:
		t.Error("no text answer sent after Enter on AskText overlay")
	}
}

// ---------------------------------------------------------------------------
// Progress notices
// ---------------------------------------------------------------------------

func TestNavigation_NoticeMsg_UpdatesProgressStatus(t *testing.T) {
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(questionMsg{
		kind: questionNotice,
		notice: interaction.Notice{
			Level:   interaction.NoticeInfo,
			Title:   "agent#1",
			Message: "phase=PLANNING stage=\"\" status=SUCCESS",
		},
	})

	// View should be non-empty (status was updated).
	view := m.progressScreen.View()
	if view == "" {
		t.Error("progress screen view is empty after notice update")
	}
}
