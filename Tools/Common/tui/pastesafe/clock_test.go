package pastesafe_test

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
)

// typeThenEnter sends the runes of s and an Enter with no pause between them.
func typeThenEnter(f *pastesafe.Field, s string) {
	for _, r := range s {
		f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// constantClock never advances, so every key looks like part of one burst.
func constantClock() pastesafe.Clock {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func TestSeparateKeysClockAdvancesBeyondWindowOnEveryRead(t *testing.T) {
	clock := pastesafe.SeparateKeysClock()

	prev := clock()
	for i := 0; i < 3; i++ {
		next := clock()
		if next.Sub(prev) <= pastesafe.PasteWindow {
			t.Fatalf("read %d advanced %v, want more than %v", i, next.Sub(prev), pastesafe.PasteWindow)
		}
		prev = next
	}
}

func TestSeparateKeysClocksAreIndependent(t *testing.T) {
	a := pastesafe.SeparateKeysClock()
	a()
	a()
	first := a()
	b := pastesafe.SeparateKeysClock()

	if !b().Before(first) {
		t.Fatal("a new separate-keys clock must start fresh, not continue another clock")
	}
}

func TestSeparateKeysClockMakesTypedThenEnterSubmit(t *testing.T) {
	restore := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	defer restore()
	f := pastesafe.NewField()

	typeThenEnter(f, "abc")

	if !f.Done() {
		t.Fatal("with the separate-keys clock a typed-then-Enter sequence must submit")
	}
	if got := f.Value(); got != "abc" {
		t.Fatalf("value = %q, want %q", got, "abc")
	}
}

func TestProcessWideClockIsUsedByFieldsWithoutOwnClock(t *testing.T) {
	restore := pastesafe.SetClock(constantClock())
	defer restore()
	f := pastesafe.NewField()

	typeThenEnter(f, "abc")

	if f.Done() {
		t.Fatal("with a constant process-wide clock the Enter is in burst and must not submit")
	}
	if got := f.Value(); got != "abc " {
		t.Fatalf("value = %q, want %q", got, "abc ")
	}
}

func TestPerInstanceClockWinsOverProcessWideClock(t *testing.T) {
	restore := pastesafe.SetClock(constantClock())
	defer restore()
	f := pastesafe.NewField(pastesafe.WithClock(pastesafe.SeparateKeysClock()))

	typeThenEnter(f, "abc")

	if !f.Done() {
		t.Fatal("the per-instance clock must take precedence over the process-wide one")
	}
}

func TestSetClockOnFieldNilFallsBackToProcessWideClock(t *testing.T) {
	restore := pastesafe.SetClock(constantClock())
	defer restore()
	f := pastesafe.NewField(pastesafe.WithClock(pastesafe.SeparateKeysClock()))
	f.SetClock(nil)

	typeThenEnter(f, "abc")

	if f.Done() {
		t.Fatal("a nil per-instance clock must use the process-wide clock")
	}
}

func TestRestoreReinstatesPreviousProcessWideClock(t *testing.T) {
	outer := pastesafe.SetClock(pastesafe.SeparateKeysClock())
	defer outer()
	inner := pastesafe.SetClock(constantClock())

	inner()
	inner() // restore is idempotent
	f := pastesafe.NewField()
	typeThenEnter(f, "abc")

	if !f.Done() {
		t.Fatal("after restore the previous (separate-keys) clock must be in effect")
	}
}
