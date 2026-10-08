package pastesafe_test

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
)

// testClock is a manually advanced clock.
type testClock struct {
	now   time.Time
	reads int
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.reads++
	return c.now
}

func (c *testClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// keyDriver sends keys to a field using a controlled clock.
type keyDriver struct {
	f   *pastesafe.Field
	clk *testClock
}

func newDriver(opts ...pastesafe.Option) *keyDriver {
	clk := newTestClock()
	all := append([]pastesafe.Option{pastesafe.WithClock(clk.Now)}, opts...)
	return &keyDriver{f: pastesafe.NewField(all...), clk: clk}
}

// inBurst is a gap that keeps consecutive keys inside the paste window.
const inBurst = pastesafe.PasteWindow / 2

// typed is a gap that places a key clearly outside the paste window.
const typed = pastesafe.PasteWindow * 3

func (d *keyDriver) send(gap time.Duration, msg tea.KeyMsg) tea.Cmd {
	d.clk.advance(gap)
	return d.f.Update(msg)
}

// runes sends one key per rune; the first after gap, the rest inside the window.
func (d *keyDriver) runes(gap time.Duration, s string) {
	first := true
	for _, r := range s {
		g := inBurst
		if first {
			g = gap
			first = false
		}
		d.send(g, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestBurstWithEnterKeysKeepsTextAndStaysOpen(t *testing.T) {
	d := newDriver()

	d.runes(typed, "line1")
	cmds := []tea.Cmd{d.send(inBurst, key(tea.KeyEnter))}
	d.runes(inBurst, "line2")
	cmds = append(cmds, d.send(inBurst, key(tea.KeyEnter)))
	d.runes(inBurst, "line3")

	if got := d.f.Value(); got != "line1 line2 line3" {
		t.Fatalf("value = %q, want %q", got, "line1 line2 line3")
	}
	if d.f.Done() {
		t.Fatal("a pasted burst must not submit the field")
	}
	for _, c := range cmds {
		if c != nil {
			t.Fatal("Enter handling must not return a paste-detection command")
		}
	}
}

func TestCRLFPairWithinWindowYieldsOneSpace(t *testing.T) {
	d := newDriver()

	d.runes(typed, "a")
	d.send(inBurst, key(tea.KeyEnter))
	d.send(inBurst, key(tea.KeyCtrlJ))
	d.runes(inBurst, "b")

	if got := d.f.Value(); got != "a b" {
		t.Fatalf("value = %q, want %q", got, "a b")
	}
	if d.f.Done() {
		t.Fatal("CR+LF must not submit")
	}
}

func TestLoneCtrlJInsideBurstYieldsOneSpace(t *testing.T) {
	d := newDriver()

	d.runes(typed, "a")
	d.send(inBurst, key(tea.KeyCtrlJ))
	d.runes(inBurst, "b")

	if got := d.f.Value(); got != "a b" {
		t.Fatalf("value = %q, want %q", got, "a b")
	}
	if d.f.Done() {
		t.Fatal("a lone LF must never submit")
	}
}

func TestLoneCtrlJOutsideWindowNeverSubmitsAndYieldsOneSpace(t *testing.T) {
	d := newDriver()

	d.runes(typed, "a")
	d.send(typed, key(tea.KeyCtrlJ))

	if got := d.f.Value(); got != "a " {
		t.Fatalf("value = %q, want %q", got, "a ")
	}
	if d.f.Done() {
		t.Fatal("a lone LF must never submit")
	}
}

func TestMultiLinePasteFollowedByImmediateEnterStaysOpen(t *testing.T) {
	d := newDriver()

	d.runes(typed, "a")
	d.send(inBurst, key(tea.KeyEnter))
	d.runes(inBurst, "b")
	d.send(inBurst, key(tea.KeyEnter)) // trailing line break of the paste

	if d.f.Done() {
		t.Fatal("paste followed immediately by Enter must stay open")
	}
	if got := d.f.Value(); got != "a b " {
		t.Fatalf("value = %q, want %q", got, "a b ")
	}

	d.send(typed, key(tea.KeyEnter))
	if !d.f.Done() {
		t.Fatal("a later Enter outside the window must submit")
	}
}

func TestEnterAfterWindowSubmits(t *testing.T) {
	d := newDriver()

	d.runes(typed, "abc")
	d.send(pastesafe.PasteWindow+time.Nanosecond, key(tea.KeyEnter))

	if !d.f.Done() {
		t.Fatal("Enter just outside the window must submit")
	}
	if got := d.f.Value(); got != "abc" {
		t.Fatalf("value = %q, want %q", got, "abc")
	}
}

func TestEnterExactlyAtWindowEdgeIsPasted(t *testing.T) {
	d := newDriver()

	d.runes(typed, "abc")
	d.send(pastesafe.PasteWindow, key(tea.KeyEnter))

	if d.f.Done() {
		t.Fatal("a gap equal to the window counts as in burst")
	}
	if got := d.f.Value(); got != "abc " {
		t.Fatalf("value = %q, want %q", got, "abc ")
	}
}

func TestIsolatedTypedEnterSubmits(t *testing.T) {
	d := newDriver()

	d.runes(typed, "ab")
	d.send(typed, key(tea.KeyEnter))

	if !d.f.Done() {
		t.Fatal("isolated Enter must submit")
	}
}

func TestEnterRightAfterSetValueSubmits(t *testing.T) {
	d := newDriver()
	d.runes(typed, "a")

	d.f.SetValue("prefilled")
	d.send(inBurst, key(tea.KeyEnter))

	if !d.f.Done() {
		t.Fatal("Enter right after SetValue has no preceding key and must submit")
	}
	if got := d.f.Value(); got != "prefilled" {
		t.Fatalf("value = %q, want %q", got, "prefilled")
	}
}

func TestPasteFlaggedKeysNeverSubmit(t *testing.T) {
	d := newDriver()

	d.send(typed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab"), Paste: true})
	d.send(typed, tea.KeyMsg{Type: tea.KeyEnter, Paste: true})

	if d.f.Done() {
		t.Fatal("a Paste-flagged Enter must never submit")
	}
	if got := d.f.Value(); len(got) < 2 || got[:2] != "ab" {
		t.Fatalf("value = %q, want it to start with %q", got, "ab")
	}
}

func TestPasteFlaggedRunesReplaceLineBreaksWithOneSpace(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"lone CR", "a\rb", "a b"},
		{"lone LF", "a\nb", "a b"},
		{"CRLF", "a\r\nb", "a b"},
		{"several lines", "x\r\ny\nz\rw", "x y z w"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDriver()

			d.send(typed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.in), Paste: true})

			if got := d.f.Value(); got != tc.want {
				t.Fatalf("value = %q, want %q", got, tc.want)
			}
			if d.f.Done() {
				t.Fatal("a paste must not submit")
			}
		})
	}
}

func TestUnflaggedMultiRuneKeyWithLineBreakIsPasted(t *testing.T) {
	d := newDriver()

	d.send(typed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\r\nb")})

	if got := d.f.Value(); got != "a b" {
		t.Fatalf("value = %q, want %q", got, "a b")
	}
	if d.f.Done() {
		t.Fatal("a multi-rune key with a line break must not submit")
	}
}

func TestEscSetsBack(t *testing.T) {
	d := newDriver()

	d.send(typed, key(tea.KeyEsc))

	if !d.f.Back() {
		t.Fatal("Esc must set Back")
	}
	if d.f.Done() {
		t.Fatal("Esc must not submit")
	}
}

func TestValidatorRunsOnSubmitOnly(t *testing.T) {
	d := newDriver(pastesafe.WithValidate(func(s string) error {
		if s == "bad" {
			return errors.New("not allowed")
		}
		return nil
	}))

	d.runes(typed, "bad")
	d.send(typed, key(tea.KeyEnter))
	if d.f.Done() || d.f.ErrMsg() == "" {
		t.Fatalf("invalid value must set ErrMsg and not Done (done=%v err=%q)", d.f.Done(), d.f.ErrMsg())
	}

	d.f.SetValue("good")
	d.send(typed, key(tea.KeyEnter))
	if !d.f.Done() || d.f.ErrMsg() != "" {
		t.Fatalf("valid value must set Done and clear ErrMsg (done=%v err=%q)", d.f.Done(), d.f.ErrMsg())
	}
}

func TestPastedLineBreakDoesNotRunValidator(t *testing.T) {
	calls := 0
	d := newDriver(pastesafe.WithValidate(func(string) error { calls++; return errors.New("no") }))

	d.runes(typed, "a")
	d.send(inBurst, key(tea.KeyEnter))
	d.runes(inBurst, "b")

	if calls != 0 || d.f.ErrMsg() != "" {
		t.Fatalf("a pasted line break must not validate (calls=%d err=%q)", calls, d.f.ErrMsg())
	}
	if got := d.f.Value(); got != "a b" {
		t.Fatalf("value = %q, want %q", got, "a b")
	}
}

func TestResetClearsFlagsKeepsValue(t *testing.T) {
	d := newDriver()
	d.runes(typed, "ab")
	d.send(typed, key(tea.KeyEnter))
	d.send(typed, key(tea.KeyEsc))

	d.f.Reset()

	if d.f.Done() || d.f.Back() || d.f.ErrMsg() != "" {
		t.Fatal("Reset must clear Done, Back and ErrMsg")
	}
	if got := d.f.Value(); got != "ab" {
		t.Fatalf("value = %q, want %q", got, "ab")
	}
}

func TestRuneFilterAppliesToTypedPastedAndLineBreakSpace(t *testing.T) {
	digits := func(r rune) bool { return r >= '0' && r <= '9' }
	d := newDriver(pastesafe.WithRuneFilter(digits))

	d.runes(typed, "1a2")
	d.send(inBurst, key(tea.KeyEnter))
	d.runes(inBurst, "3")
	d.send(typed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4\nx5"), Paste: true})

	if got := d.f.Value(); got != "12345" {
		t.Fatalf("value = %q, want %q", got, "12345")
	}
}

func TestWithValuePrefillsAndEnterSubmits(t *testing.T) {
	d := newDriver(pastesafe.WithValue("seed"))

	d.send(inBurst, key(tea.KeyEnter))

	if got := d.f.Value(); got != "seed" {
		t.Fatalf("value = %q, want %q", got, "seed")
	}
	if !d.f.Done() {
		t.Fatal("Enter on a prefilled field with no preceding key must submit")
	}
}

func TestClockReadOncePerKeyMessageOnly(t *testing.T) {
	d := newDriver()
	d.runes(typed, "ab")
	before := d.clk.reads

	d.f.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	if d.clk.reads != before {
		t.Fatalf("non-key message read the clock (%d reads)", d.clk.reads-before)
	}
	d.send(typed, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if d.clk.reads != before+1 {
		t.Fatalf("a key message must read the clock exactly once, read %d", d.clk.reads-before)
	}
	if got := d.f.Value(); got != "abc" {
		t.Fatalf("value = %q, want %q", got, "abc")
	}
}
