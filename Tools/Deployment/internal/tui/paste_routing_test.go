package tui

// paste_routing_test.go verifies, through the root model, that a pasted burst reaches only
// the active text field: shortcut letters and line breaks in the paste neither quit the
// program, answer the question, nor move to another screen.
//
// The paste-detection clock is controlled by each test and restored afterwards. Tests here
// share a process-wide clock and must not run in parallel.

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
	"mosaic-deploy/internal/domain"
)

const (
	routingPasteGap  = pastesafe.PasteWindow / 2
	routingTypingGap = pastesafe.PasteWindow * 3
)

// routingClock advances by a caller-chosen gap before every read.
type routingClock struct {
	mu  sync.Mutex
	now time.Time
	gap time.Duration
}

func (c *routingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.gap)
	return c.now
}

func (c *routingClock) setGap(d time.Duration) {
	c.mu.Lock()
	c.gap = d
	c.mu.Unlock()
}

// installRoutingClock installs a controlled clock and restores the previous one at test end.
func installRoutingClock(t *testing.T) *routingClock {
	t.Helper()
	clk := &routingClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), gap: routingTypingGap}
	restore := pastesafe.SetClock(clk.Now)
	t.Cleanup(restore)
	return clk
}

// pasteThroughRoot feeds text to the root model as one paste burst and reports whether any
// update returned a quit command. Line breaks are delivered as Enter keys.
func pasteThroughRoot(clk *routingClock, m *rootModel, text string) (quit bool) {
	first := true
	for _, r := range text {
		if first {
			clk.setGap(routingTypingGap)
			first = false
		} else {
			clk.setGap(routingPasteGap)
		}
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == '\n' {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		_, cmd := m.Update(msg)
		if cmd != nil {
			if _, isQuit := cmd().(tea.QuitMsg); isQuit {
				quit = true
			}
		}
	}
	return quit
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestRootModel_PasteIntoAskTextKeepsPromptOpenWithAllText(t *testing.T) {
	clk := installRoutingClock(t)
	m := newRoutingModel()
	qMsg := buildTextMsg(domain.QCustomTool)
	m.Update(qMsg)

	quit := pasteThroughRoot(clk, m, "q\nenter\nesc\ns\nc")

	if quit {
		t.Error("a pasted burst returned a quit command; want it held by the text prompt")
	}
	if m.screen != screenQuestion || m.textOverlay == nil {
		t.Fatalf("prompt must stay open, screen=%v overlay=%v", m.screen, m.textOverlay)
	}
	select {
	case ans := <-qMsg.reply:
		t.Fatalf("no reply expected while the prompt is open, got %+v", ans)
	default:
	}
	if view := collapseSpaces(m.View()); !strings.Contains(view, "q enter esc s c") {
		t.Errorf("view does not show the full pasted text with spaces for line breaks:\n%s", view)
	}
}

func TestRootModel_EnterAfterPasteWindowSubmitsPastedAskText(t *testing.T) {
	clk := installRoutingClock(t)
	m := newRoutingModel()
	qMsg := buildTextMsg(domain.QCustomTool)
	m.Update(qMsg)
	pasteThroughRoot(clk, m, "my\ntool")

	clk.setGap(routingTypingGap)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case ans := <-qMsg.reply:
		if ans.textAns.Status != domain.Answered || ans.textAns.Text != "my tool" {
			t.Errorf("reply = %+v, want Answered with text %q", ans.textAns, "my tool")
		}
	default:
		t.Error("no reply after Enter outside the paste window")
	}
}

func TestRootModel_PasteIntoWorkspaceFieldStaysOnWorkspaceScreen(t *testing.T) {
	clk := installRoutingClock(t)
	m := newRoutingModel()
	m.screen = screenWorkspace

	quit := pasteThroughRoot(clk, m, "q\nesc\nalpha")

	if quit {
		t.Error("a pasted burst returned a quit command; want it held by the workspace field")
	}
	if m.screen != screenWorkspace {
		t.Fatalf("screen = %v after a paste; want to stay on the workspace screen", m.screen)
	}
	if m.wsScreen.Done() {
		t.Error("workspace screen finished on a pasted line break")
	}
	if view := collapseSpaces(m.View()); !strings.Contains(view, "q esc alpha") {
		t.Errorf("view does not show the full pasted text with spaces for line breaks:\n%s", view)
	}
}
