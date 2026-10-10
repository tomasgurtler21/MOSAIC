package tui

// paste_overlay_test.go verifies that the text-entry fields of the LogAnalyzer
// TUI (the generic text question overlay and the log-source path screen) are
// paste-safe: a multi-line paste keeps the field open with all text, line
// breaks becoming spaces, and no other screen reacts to the pasted keys.
//
// The paste-detection clock is controlled by the test and restored afterwards.

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-common/tui/pastesafe"
	"mosaic-log-analyzer/internal/app"
)

const (
	pasteGap  = pastesafe.PasteWindow / 2
	typingGap = pastesafe.PasteWindow * 3
)

// steppedClock advances by a caller-chosen gap before every read.
type steppedClock struct {
	mu  sync.Mutex
	now time.Time
	gap time.Duration
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.gap)
	return c.now
}

func (c *steppedClock) setGap(d time.Duration) {
	c.mu.Lock()
	c.gap = d
	c.mu.Unlock()
}

// installSteppedClock installs a controlled process-wide clock for the test and
// restores the previous one when the test ends.
func installSteppedClock(t *testing.T) *steppedClock {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), gap: typingGap}
	restore := pastesafe.SetClock(clk.Now)
	t.Cleanup(restore)
	return clk
}

// pasteKeys sends text to the model as a burst: the first rune arrives after a
// typing gap, every later key within the paste window. Line breaks in text are
// delivered as Enter keys, as a terminal delivers a pasted CR.
func pasteKeys(t *testing.T, clk *steppedClock, m Model, text string) Model {
	t.Helper()
	first := true
	for _, r := range text {
		if first {
			clk.setGap(typingGap)
			first = false
		} else {
			clk.setGap(pasteGap)
		}
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == '\n' {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		updated, _ := m.Update(msg)
		m = asModel(updated)
	}
	return m
}

func pressAfterTypingGap(clk *steppedClock, m Model, msg tea.KeyMsg) Model {
	clk.setGap(typingGap)
	updated, _ := m.Update(msg)
	return asModel(updated)
}

func assertNoReply(t *testing.T, reply chan answerMsg) {
	t.Helper()
	select {
	case ans := <-reply:
		t.Fatalf("no answer expected while the field is open, got %+v", ans)
	default:
	}
}

func openTextOverlay(t *testing.T, validate func(string) error) (Model, chan answerMsg) {
	t.Helper()
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	p := makePendingText(app.QuestionPricingRate, reply)
	p.textQ.Validate = validate
	updated, _ := m.Update(p)
	m2 := asModel(updated)
	m2.interact.Close()
	if m2.screen != screenQuestion || m2.textOverlay == nil {
		t.Fatalf("expected screenQuestion with textOverlay, got screen=%q", m2.screen)
	}
	return m2, reply
}

func openSourceScreen(t *testing.T) (Model, chan answerMsg) {
	t.Helper()
	m := newNavModel()
	reply := make(chan answerMsg, 1)
	updated, _ := m.Update(makePendingText(app.QuestionLogSourcePath, reply))
	m2 := asModel(updated)
	m2.interact.Close()
	if m2.screen != screenSource {
		t.Fatalf("expected screenSource, got %q", m2.screen)
	}
	return m2, reply
}

func TestTextOverlay_MultiLinePasteKeepsOverlayOpenWithAllText(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openTextOverlay(t, nil)

	m = pasteKeys(t, clk, m, "3.00\n4.00")

	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("a pasted line break must keep the overlay open, screen=%q overlay=%v", m.screen, m.textOverlay)
	}
	assertNoReply(t, reply)
	if got := m.textOverlay.answer().Text; got != "3.00 4.00" {
		t.Errorf("overlay text = %q, want %q", got, "3.00 4.00")
	}
}

func TestTextOverlay_PasteWithKeyShortcutLettersDoesNotReachOtherScreens(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openTextOverlay(t, nil)

	m = pasteKeys(t, clk, m, "q\ns\np\nj\nk")

	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("pasted shortcut letters must stay inside the overlay, screen=%q", m.screen)
	}
	assertNoReply(t, reply)
	if got := m.textOverlay.answer().Text; got != "q s p j k" {
		t.Errorf("overlay text = %q, want %q", got, "q s p j k")
	}
}

func TestTextOverlay_EnterAfterPasteWindowStillSubmitsFullText(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openTextOverlay(t, nil)
	m = pasteKeys(t, clk, m, "a\nb")

	m = pressAfterTypingGap(clk, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenLoading || m.textOverlay != nil {
		t.Fatalf("Enter after the paste must submit, screen=%q overlay=%v", m.screen, m.textOverlay)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered || ans.textAns.Text != "a b" {
			t.Errorf("answer = %+v, want Answered with text %q", ans.textAns, "a b")
		}
	default:
		t.Error("no answer received after Enter following the paste")
	}
}

func TestTextOverlay_FlaggedPasteReplacesLineBreaksAndStaysOpen(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openTextOverlay(t, nil)
	clk.setGap(typingGap)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\r\ny\nz"), Paste: true})
	m = asModel(updated)

	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("a flagged paste must keep the overlay open, screen=%q", m.screen)
	}
	assertNoReply(t, reply)
	if got := m.textOverlay.answer().Text; got != "x y z" {
		t.Errorf("overlay text = %q, want %q", got, "x y z")
	}
}

func TestTextOverlay_PasteDoesNotRunValidatorOrShowError(t *testing.T) {
	clk := installSteppedClock(t)
	m, _ := openTextOverlay(t, func(s string) error {
		if strings.Contains(s, " ") {
			return errors.New("no spaces allowed")
		}
		return nil
	})

	m = pasteKeys(t, clk, m, "a\nb")

	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("overlay must stay open, screen=%q", m.screen)
	}
	if strings.Contains(m.View(), "no spaces allowed") {
		t.Error("a pasted line break must not trigger validation")
	}
}

func TestSourceScreen_MultiLinePasteKeepsFieldOpenWithAllText(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openSourceScreen(t)

	m = pasteKeys(t, clk, m, "logs\nrun1")

	if m.screen != screenSource {
		t.Fatalf("a pasted line break must keep the source screen, screen=%q", m.screen)
	}
	assertNoReply(t, reply)
	if got := m.sourceScreen.Path(); got != "logs run1" {
		t.Errorf("source path = %q, want %q", got, "logs run1")
	}
	if m.sourceScreen.Done() {
		t.Error("source screen must not be done after a paste")
	}
}

func TestSourceScreen_PasteWithShortcutLettersDoesNotReachOtherScreens(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openSourceScreen(t)

	m = pasteKeys(t, clk, m, "q\ns\np\nj\nk")

	if m.screen != screenSource {
		t.Fatalf("pasted shortcut letters must stay inside the source field, screen=%q", m.screen)
	}
	assertNoReply(t, reply)
	if got := m.sourceScreen.Path(); got != "q s p j k" {
		t.Errorf("source path = %q, want %q", got, "q s p j k")
	}
}

func TestSourceScreen_EnterAfterPasteWindowSubmitsFullText(t *testing.T) {
	clk := installSteppedClock(t)
	m, reply := openSourceScreen(t)
	m = pasteKeys(t, clk, m, "logs\nrun1")

	m = pressAfterTypingGap(clk, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenLoading {
		t.Fatalf("Enter after the paste must submit, screen=%q", m.screen)
	}
	select {
	case ans := <-reply:
		if ans.textAns.Status != interaction.Answered || ans.textAns.Text != "logs run1" {
			t.Errorf("answer = %+v, want Answered with text %q", ans.textAns, "logs run1")
		}
	default:
		t.Error("no answer received after Enter following the paste")
	}
}
