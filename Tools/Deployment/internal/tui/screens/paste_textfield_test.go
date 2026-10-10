package screens_test

// paste_textfield_test.go verifies that the Deployment text fields (the workspace path
// field and the AskText prompt) are paste-safe: a multi-line paste leaves the field open
// with all text, line breaks becoming spaces, and an Enter after the paste window still
// submits.
//
// The paste-detection clock is controlled by each test and restored afterwards.

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
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

// installSteppedClock installs a controlled process-wide clock and restores the previous
// one when the test ends.
func installSteppedClock(t *testing.T) *steppedClock {
	t.Helper()
	clk := &steppedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), gap: typingGap}
	restore := pastesafe.SetClock(clk.Now)
	t.Cleanup(restore)
	return clk
}

// pasteBurst feeds text to send as one paste burst: the first rune arrives after a typing
// gap, every later key within the paste window. Line breaks are delivered as Enter keys,
// as a terminal delivers a pasted CR.
func pasteBurst(clk *steppedClock, text string, send func(tea.Msg)) {
	first := true
	for _, r := range text {
		if first {
			clk.setGap(typingGap)
			first = false
		} else {
			clk.setGap(pasteGap)
		}
		if r == '\n' {
			send(tea.KeyMsg{Type: tea.KeyEnter})
			continue
		}
		send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func newPasteWorkspace() *screens.WorkspaceScreen {
	return screens.NewWorkspaceScreen(80, 24, plainStyles())
}

func newPastePrompt() *screens.TextPromptScreen {
	return screens.NewTextPromptScreen(customModelTextQuestion(), 80, 24, plainStyles())
}

// ---------------------------------------------------------------------------
// WorkspaceScreen path field
// ---------------------------------------------------------------------------

func TestWorkspaceScreen_MultiLinePasteKeepsFieldOpenWithAllText(t *testing.T) {
	clk := installSteppedClock(t)
	s := newPasteWorkspace()

	pasteBurst(clk, "alpha\nbeta", func(m tea.Msg) { s.Update(m) })

	if s.Done() {
		t.Fatal("Done() = true after a pasted line break; want the field to stay open")
	}
	if s.Back() {
		t.Error("Back() = true after a paste; want false")
	}
	if view := collapseWhitespace(s.View()); !strings.Contains(view, "alpha beta") {
		t.Errorf("view does not show the full pasted text with a space for the line break:\n%s", view)
	}
}

func TestWorkspaceScreen_PasteDoesNotShowValidationError(t *testing.T) {
	clk := installSteppedClock(t)
	s := newPasteWorkspace()

	pasteBurst(clk, "alpha\nbeta", func(m tea.Msg) { s.Update(m) })

	if view := s.View(); strings.Contains(view, "does not exist") || strings.Contains(view, "not valid") {
		t.Errorf("a pasted line break must not run validation, view:\n%s", view)
	}
}

func TestWorkspaceScreen_EnterAfterPasteWindowStillSubmits(t *testing.T) {
	clk := installSteppedClock(t)
	dir := t.TempDir()
	s := newPasteWorkspace()
	pasteBurst(clk, dir+"\n", func(m tea.Msg) { s.Update(m) })
	if s.Done() {
		t.Fatal("Done() = true while still inside the paste burst; want false")
	}

	clk.setGap(typingGap)
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !s.Done() {
		t.Error("Done() = false after Enter outside the paste window; want true")
	}
}

// ---------------------------------------------------------------------------
// TextPromptScreen (AskText prompt)
// ---------------------------------------------------------------------------

func TestTextPromptScreen_MultiLinePasteKeepsPromptOpenWithAllText(t *testing.T) {
	clk := installSteppedClock(t)
	s := newPastePrompt()

	var final bool
	pasteBurst(clk, "gpt-5\nmini", func(m tea.Msg) { final = s.Update(m) || final })

	if final || s.Done() {
		t.Fatal("prompt finished on a pasted line break; want it to stay open")
	}
	if view := collapseWhitespace(s.View()); !strings.Contains(view, "gpt-5 mini") {
		t.Errorf("view does not show the full pasted text with a space for the line break:\n%s", view)
	}
}

func TestTextPromptScreen_EnterAfterPasteWindowSubmitsFullText(t *testing.T) {
	clk := installSteppedClock(t)
	s := newPastePrompt()
	pasteBurst(clk, "gpt-5\nmini", func(m tea.Msg) { s.Update(m) })

	clk.setGap(typingGap)
	finished := s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !finished || !s.Done() {
		t.Fatal("Enter outside the paste window must submit the prompt")
	}
	ans := s.Answer()
	if ans.Status != domain.Answered || ans.Text != "gpt-5 mini" {
		t.Errorf("Answer() = %+v, want Answered with text %q", ans, "gpt-5 mini")
	}
}

func TestTextPromptScreen_PasteDoesNotRunValidator(t *testing.T) {
	clk := installSteppedClock(t)
	called := false
	q := customModelTextQuestion()
	q.Validate = func(string) error { called = true; return nil }
	s := screens.NewTextPromptScreen(q, 80, 24, plainStyles())

	pasteBurst(clk, "a\nb", func(m tea.Msg) { s.Update(m) })

	if called {
		t.Error("validator ran during a paste burst; a pasted line break must not submit")
	}
	if s.Done() {
		t.Error("prompt finished during a paste burst")
	}
}
