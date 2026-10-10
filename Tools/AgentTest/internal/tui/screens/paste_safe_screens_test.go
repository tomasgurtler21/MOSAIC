package screens_test

// paste_safe_screens_test.go verifies that every text-entry screen is paste
// safe: a multi-line paste keeps the screen open with all accepted text (each
// line break becoming one space), an Enter after the paste window confirms,
// and Esc still goes back. Numeric screens keep their digit-only filtering,
// so pasted line breaks are dropped there.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/tui/screens"
	"mosaic-common/tui/pastesafe"
)

// pasteScreen is the behaviour every text-entry screen shares.
type pasteScreen interface {
	Update(msg tea.Msg) tea.Cmd
	View() string
	Done() bool
	Back() bool
}

// pasteCase describes one screen under test.
type pasteCase struct {
	name    string
	build   func() pasteScreen
	value   func(s pasteScreen) string // confirmed value rendered as text
	initial string                     // value() before any input
	lines   []string                   // pasted lines; Enter between them
	want    string                     // value() after paste and confirming Enter
}

func pasteCases() []pasteCase {
	text := []string{"ab", "cd"}
	nums := []string{"12", "34"}
	return []pasteCase{
		{"CatalogFolder", func() pasteScreen { return screens.NewCatalogFolderScreen("init", 80, plainStyles()) },
			func(s pasteScreen) string { return s.(*screens.CatalogFolderScreen).Folder() }, "init", text, "ab cd"},
		{"ReportPath", func() pasteScreen { return screens.NewReportPathScreen("init", 80, plainStyles()) },
			func(s pasteScreen) string { return s.(*screens.ReportPathScreen).Path() }, "init", text, "ab cd"},
		{"StoreInput", func() pasteScreen { return screens.NewStoreInputScreen("init", 80, plainStyles()) },
			func(s pasteScreen) string { return s.(*screens.StoreInputScreen).Path() }, "init", text, "ab cd"},
		{"SummaryInput", func() pasteScreen { return screens.NewSummaryInputScreen("init", 80, plainStyles()) },
			func(s pasteScreen) string { return s.(*screens.SummaryInputScreen).VersionFilter() }, "init", text, "ab cd"},
		{"Repetitions", func() pasteScreen { return screens.NewRepetitionsScreen(7, 80, plainStyles()) },
			func(s pasteScreen) string { return fmt.Sprint(s.(*screens.RepetitionsScreen).Value()) }, "7", nums, "1234"},
		{"MaxConcurrentRuns", func() pasteScreen { return screens.NewMaxConcurrentRunsScreen(7, 80, plainStyles()) },
			func(s pasteScreen) string { return fmt.Sprint(s.(*screens.MaxConcurrentRunsScreen).Value()) }, "7", nums, "1234"},
	}
}

// numericCases returns only the digit-only screens.
func numericCases() []pasteCase { return pasteCases()[4:] }

// burstClock never advances, so every key looks like part of one paste.
func burstClock() pastesafe.Clock {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

// installClock sets the process-wide clock for the test and restores it.
func installClock(t *testing.T, c pastesafe.Clock) {
	t.Helper()
	t.Cleanup(pastesafe.SetClock(c))
}

// pasteLines delivers the lines as single-rune key events with an Enter between
// consecutive lines, the way a Windows terminal delivers a multi-line paste.
func pasteLines(s pasteScreen, lines []string) {
	for i, line := range lines {
		if i > 0 {
			s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		for _, r := range line {
			s.Update(runeKey(r))
		}
	}
}

func TestPastedMultiLineTextKeepsScreenOpen(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, burstClock())
			s := tc.build()

			pasteLines(s, tc.lines)

			if s.Done() {
				t.Error("screen is done after a multi-line paste, want it to stay open")
			}
			if s.Back() {
				t.Error("screen went back during a paste")
			}
		})
	}
}

func TestPastedLineBreaksBecomeSpacesAndEnterAfterWindowConfirms(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, burstClock())
			s := tc.build()
			pasteLines(s, tc.lines)
			installClock(t, pastesafe.SeparateKeysClock())

			s.Update(enterKey())

			if !s.Done() {
				t.Fatal("Enter after the paste window must confirm")
			}
			if got := tc.value(s); got != tc.want {
				t.Errorf("confirmed value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBracketedPasteMessageKeepsTextAndStaysOpen(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			paste := strings.Join(tc.lines, "\n")

			s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(paste), Paste: true})
			if s.Done() {
				t.Fatal("a bracketed paste containing a line break must not confirm")
			}
			s.Update(enterKey())

			if !s.Done() {
				t.Fatal("a separate Enter after the paste must confirm")
			}
			if got := tc.value(s); got != tc.want {
				t.Errorf("confirmed value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEscAfterPasteGoesBackAndKeepsConfirmedValue(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, burstClock())
			s := tc.build()
			pasteLines(s, tc.lines)

			s.Update(escKey())

			if !s.Back() {
				t.Error("Esc must still go back")
			}
			if s.Done() {
				t.Error("Esc must not confirm")
			}
			if got := tc.value(s); got != tc.initial {
				t.Errorf("value = %q after Esc, want unchanged %q", got, tc.initial)
			}
		})
	}
}

func TestTypedTextThenEnterConfirmsWhenKeysAreSeparate(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, pastesafe.SeparateKeysClock())
			s := tc.build()

			pasteLines(s, tc.lines[:1])
			s.Update(enterKey())

			if !s.Done() {
				t.Fatal("typed text followed by a separate Enter must confirm")
			}
			if got := tc.value(s); got != tc.lines[0] {
				t.Errorf("confirmed value = %q, want %q", got, tc.lines[0])
			}
		})
	}
}

func TestEnterWithoutInputConfirmsInitialValue(t *testing.T) {
	for _, tc := range pasteCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, pastesafe.SeparateKeysClock())
			s := tc.build()

			s.Update(enterKey())

			if !s.Done() {
				t.Fatal("Enter on an untouched screen must confirm")
			}
			if got := tc.value(s); got != tc.initial {
				t.Errorf("value = %q, want initial %q", got, tc.initial)
			}
		})
	}
}

func TestNumericScreensDropNonDigitsFromPaste(t *testing.T) {
	for _, tc := range numericCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, burstClock())
			s := tc.build()

			pasteLines(s, []string{"x1", "y2z"})
			if s.Done() {
				t.Fatal("a pasted line break must not confirm a numeric screen")
			}
			installClock(t, pastesafe.SeparateKeysClock())
			s.Update(enterKey())

			if got := tc.value(s); got != "12" {
				t.Errorf("confirmed value = %q, want only the pasted digits %q", got, "12")
			}
		})
	}
}

func TestNumericScreenKeepsInitialValueWhenPasteHasNoDigits(t *testing.T) {
	for _, tc := range numericCases() {
		t.Run(tc.name, func(t *testing.T) {
			installClock(t, burstClock())
			s := tc.build()
			pasteLines(s, []string{"abc", "def"})
			if s.Done() {
				t.Fatal("a pasted line break must not confirm a numeric screen")
			}
			installClock(t, pastesafe.SeparateKeysClock())

			s.Update(enterKey())

			if !s.Done() {
				t.Fatal("Enter after the window must confirm")
			}
			if got := tc.value(s); got != tc.initial {
				t.Errorf("value = %q, want initial %q", got, tc.initial)
			}
		})
	}
}

func TestRetentionScreenIgnoresEnterInsidePaste(t *testing.T) {
	installClock(t, burstClock())
	s := screens.NewRetentionScreen(domain.RetainNever, 80, plainStyles())

	pasteLines(s, []string{"ab", "cd"})
	if s.Done() {
		t.Fatal("a pasted line break must not confirm the retention screen")
	}
	installClock(t, pastesafe.SeparateKeysClock())
	s.Update(enterKey())

	if !s.Done() {
		t.Error("Enter after the paste window must confirm")
	}
}

func TestRetentionScreenEscAfterPasteGoesBack(t *testing.T) {
	installClock(t, burstClock())
	s := screens.NewRetentionScreen(domain.RetainOnFailure, 80, plainStyles())
	pasteLines(s, []string{"ab", "cd"})

	s.Update(escKey())

	if !s.Back() || s.Done() {
		t.Errorf("Back() = %v, Done() = %v after Esc, want true, false", s.Back(), s.Done())
	}
}
