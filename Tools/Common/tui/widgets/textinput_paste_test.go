package widgets_test

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
	"mosaic-common/tui/widgets"
)

// inputClock is a manually advanced clock for TextInput paste tests.
type inputClock struct{ now time.Time }

func (c *inputClock) Now() time.Time { return c.now }

type inputDriver struct {
	in  *widgets.TextInput
	clk *inputClock
}

func newInputDriver() *inputDriver {
	clk := &inputClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	in := widgets.NewTextInput("Label", "", 40, widgets.DefaultTextInputStyles())
	in.SetClock(clk.Now)
	return &inputDriver{in: in, clk: clk}
}

const (
	pasteGap  = pastesafe.PasteWindow / 2
	typingGap = pastesafe.PasteWindow * 3
)

func (d *inputDriver) send(gap time.Duration, msg tea.KeyMsg) {
	d.clk.now = d.clk.now.Add(gap)
	d.in.Update(msg)
}

func (d *inputDriver) runes(gap time.Duration, s string) {
	first := true
	for _, r := range s {
		g := pasteGap
		if first {
			g, first = gap, false
		}
		d.send(g, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTextInputPastedBurstKeepsTextAndStaysOpen(t *testing.T) {
	d := newInputDriver()

	d.runes(typingGap, "line1")
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyEnter})
	d.runes(pasteGap, "line2")

	if got := d.in.Value(); got != "line1 line2" {
		t.Fatalf("value = %q, want %q", got, "line1 line2")
	}
	if d.in.Done() {
		t.Fatal("a pasted burst must not submit the input")
	}
}

func TestTextInputCRLFPairYieldsOneSpace(t *testing.T) {
	d := newInputDriver()

	d.runes(typingGap, "a")
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyEnter})
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyCtrlJ})
	d.runes(pasteGap, "b")

	if got := d.in.Value(); got != "a b" {
		t.Fatalf("value = %q, want %q", got, "a b")
	}
	if d.in.Done() {
		t.Fatal("CR+LF must not submit")
	}
}

func TestTextInputPasteFollowedByImmediateEnterStaysOpenThenLaterEnterSubmits(t *testing.T) {
	d := newInputDriver()

	d.runes(typingGap, "a")
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyEnter})
	d.runes(pasteGap, "b")
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyEnter})
	if d.in.Done() {
		t.Fatal("paste plus immediate Enter must stay open")
	}

	d.send(typingGap, tea.KeyMsg{Type: tea.KeyEnter})
	if !d.in.Done() {
		t.Fatal("a later Enter must submit")
	}
	if got := d.in.Value(); got != "a b " {
		t.Fatalf("value = %q, want %q", got, "a b ")
	}
}

func TestTextInputPasteFlaggedRunesReplaceLineBreaks(t *testing.T) {
	d := newInputDriver()

	d.send(typingGap, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\r\nb\nc"), Paste: true})

	if got := d.in.Value(); got != "a b c" {
		t.Fatalf("value = %q, want %q", got, "a b c")
	}
	if d.in.Done() {
		t.Fatal("a paste must not submit")
	}
}

func TestTextInputTypedEnterAfterWindowSubmits(t *testing.T) {
	d := newInputDriver()

	d.runes(typingGap, "abc")
	d.send(typingGap, tea.KeyMsg{Type: tea.KeyEnter})

	if !d.in.Done() {
		t.Fatal("an Enter outside the window must submit")
	}
}

func TestTextInputValidatorBlocksInvalidSubmit(t *testing.T) {
	d := newInputDriver()
	d.in.SetValidate(func(s string) error {
		if s == "" {
			return errors.New("required")
		}
		return nil
	})

	d.send(typingGap, tea.KeyMsg{Type: tea.KeyEnter})
	if d.in.Done() || d.in.ErrMsg() == "" {
		t.Fatalf("empty value must set ErrMsg and not Done (done=%v err=%q)", d.in.Done(), d.in.ErrMsg())
	}

	d.runes(typingGap, "x")
	d.send(typingGap, tea.KeyMsg{Type: tea.KeyEnter})
	if !d.in.Done() || d.in.ErrMsg() != "" {
		t.Fatalf("valid value must set Done and clear ErrMsg (done=%v err=%q)", d.in.Done(), d.in.ErrMsg())
	}
}

func TestTextInputSetValueThenEnterSubmitsAndResetKeepsValue(t *testing.T) {
	d := newInputDriver()

	d.in.SetValue("seed")
	d.send(pasteGap, tea.KeyMsg{Type: tea.KeyEnter})
	if !d.in.Done() {
		t.Fatal("Enter right after SetValue must submit")
	}

	d.in.Reset()
	if d.in.Done() || d.in.Back() || d.in.ErrMsg() != "" {
		t.Fatal("Reset must clear Done, Back and ErrMsg")
	}
	if got := d.in.Value(); got != "seed" {
		t.Fatalf("value = %q, want %q", got, "seed")
	}
}

func TestTextInputEscSetsBack(t *testing.T) {
	d := newInputDriver()

	d.send(typingGap, tea.KeyMsg{Type: tea.KeyEsc})

	if !d.in.Back() {
		t.Fatal("Esc must set Back")
	}
	if d.in.Done() {
		t.Fatal("Esc must not submit")
	}
}

func TestTextInputWithoutClockOverrideUsesProcessWideClock(t *testing.T) {
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	defer restore()
	in := widgets.NewTextInput("Label", "", 40, widgets.DefaultTextInputStyles())

	for _, r := range "abc" {
		in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	in.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !in.Done() {
		t.Fatal("with the separate-keys clock a typed-then-Enter sequence must submit")
	}
	if got := in.Value(); got != "abc" {
		t.Fatalf("value = %q, want %q", got, "abc")
	}
}
