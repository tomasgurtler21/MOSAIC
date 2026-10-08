package tui

// paste_overlay_test.go verifies that the text question overlay and the seed
// input field are paste-safe (a multi-line burst never submits and never leaks
// keys to another screen) and that Esc cancels the text question.

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-common/tui/pastesafe"
)

// burstClock never advances, so every key event looks like part of one paste.
func burstClock() pastesafe.Clock {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

// sendRunes delivers each rune as its own key event.
func sendRunes(m *rootModel, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// openTextQuestion shows a text question over the progress screen.
func openTextQuestion(t *testing.T) (*rootModel, chan answerMsg) {
	t.Helper()
	m := newTestModel()
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress
	reply := make(chan answerMsg, 1)
	m.Update(questionMsg{
		kind:  questionAskText,
		textQ: interaction.TextQuestion{Question: interaction.Question{Title: "Enter value:", Prompt: "type here"}},
		reply: reply,
	})
	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("precondition: text overlay not open, screen = %v", m.screen)
	}
	return m, reply
}

func TestTextOverlay_MultiLinePasteStaysOpenWithLineBreaksAsSpaces(t *testing.T) {
	restore := pastesafe.SetClock(burstClock())
	defer restore()
	m, reply := openTextQuestion(t)

	sendRunes(m, "first")
	sendKey(m, tea.KeyEnter)
	sendRunes(m, "second")

	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("overlay closed under a multi-line paste: screen = %v", m.screen)
	}
	select {
	case <-reply:
		t.Fatal("a reply was sent while pasting")
	default:
	}
	if got := m.textOverlay.answer().Text; got != "first second" {
		t.Errorf("pasted text = %q, want %q", got, "first second")
	}
}

func TestTextOverlay_PastedKeysDoNotReachProgressScreen(t *testing.T) {
	restore := pastesafe.SetClock(burstClock())
	defer restore()
	m, _ := openTextQuestion(t)

	// After the line break the letters s, y, a and i are progress-screen
	// shortcuts (stop gate, confirm, artifact view).
	sendRunes(m, "go")
	sendKey(m, tea.KeyEnter)
	sendRunes(m, "stay")

	if m.screen != screenQuestion {
		t.Errorf("screen = %v after paste, want screenQuestion (%v)", m.screen, screenQuestion)
	}
	if m.screen == screenArtifact {
		t.Error("pasted keys opened the artifact view")
	}
	if m.textOverlay == nil {
		t.Fatal("overlay closed under a multi-line paste")
	}
	if got := m.textOverlay.answer().Text; got != "go stay" {
		t.Errorf("pasted text = %q, want %q", got, "go stay")
	}
}

func TestTextOverlay_EscRepliesCancelledAndCloses(t *testing.T) {
	m, reply := openTextQuestion(t)
	sendRunes(m, "abc")

	sendKey(m, tea.KeyEsc)

	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Cancelled {
			t.Errorf("text answer Status = %q, want %q", ans.textAns.Status, interaction.Cancelled)
		}
	default:
		t.Fatal("Esc sent no reply; the waiting session stays blocked")
	}
	if m.textOverlay != nil {
		t.Error("text overlay still open after Esc")
	}
	if m.screen != screenProgress {
		t.Errorf("screen = %v after Esc, want screenProgress (%v)", m.screen, screenProgress)
	}
}

func TestTextOverlay_TypedTextThenEnterStillAnswers(t *testing.T) {
	m, reply := openTextQuestion(t)
	sendRunes(m, "abc")

	sendKey(m, tea.KeyEnter)

	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered || ans.textAns.Text != "abc" {
			t.Errorf("answer = %+v, want Answered with text %q", ans.textAns, "abc")
		}
	default:
		t.Fatal("typed text followed by Enter sent no reply")
	}
}

func TestSeedInputScreen_MultiLinePasteStaysOpenWithText(t *testing.T) {
	restore := pastesafe.SetClock(burstClock())
	defer restore()
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	sendRunes(m, "one")
	sendKey(m, tea.KeyEnter)
	sendRunes(m, "two")

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after multi-line paste, want screenSetupSeedInput (%v)", m.screen, screenSetupSeedInput)
	}
	if got := m.seedInputScreen.SeedInput(); got != "one two" {
		t.Errorf("seed input = %q, want %q", got, "one two")
	}
}
