package tui

// app_paste_test.go verifies that a pasted burst carrying carriage-return
// runes, routed through the model's Enter normalisation, neither submits the
// active text screen nor triggers any other screen's key bindings.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/tui/pastesafe"
)

// pasteBurstClock never advances, so every key looks like part of one paste.
func pasteBurstClock() pastesafe.Clock {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

// sendBurst feeds each message to the model with the paste-burst clock installed
// and restores the previous clock before returning.
func sendBurst(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	restore := pastesafe.SetClock(pasteBurstClock())
	defer restore()
	for _, msg := range msgs {
		m, _ = safeUpdate(t, m, msg)
	}
	return m
}

func TestPastedCarriageReturnRunesDoNotSubmitReportPathScreen(t *testing.T) {
	m := navigateToScreenReportPath(t, NewModel(newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "orig.json", nil)))

	m = sendBurst(t, m, keyMsg("a"), keyMsg("b"), keyMsg("\r"), keyMsg("c"), keyMsg("d"))

	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() = %q after a pasted multi-line burst, want it to stay on %q", m.Screen(), ScreenReportPath)
	}
	if got := m.ReportPath(); got != "orig.json" {
		t.Errorf("ReportPath() = %q after the burst, want the unconfirmed initial %q", got, "orig.json")
	}
}

func TestPastedBurstKeepsAllTextWithLineBreaksAsSpaces(t *testing.T) {
	m := navigateToScreenReportPath(t, NewModel(newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "orig.json", nil)))
	m = sendBurst(t, m, keyMsg("a"), keyMsg("b"), keyMsg("\r"), keyMsg("c"), keyMsg("d"))

	m, _ = safeUpdate(t, m, keyMsg("\r")) // typed Enter, after the paste window

	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() = %q after Enter outside the window, want %q", m.Screen(), ScreenCatalogFolder)
	}
	if got := m.ReportPath(); got != "ab cd" {
		t.Errorf("ReportPath() = %q, want %q (line break pasted as one space)", got, "ab cd")
	}
}

func TestPastedBurstDoesNotTriggerOtherScreenBindings(t *testing.T) {
	m := navigateToScreenReportPath(t, NewModel(newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "orig.json", nil)))

	// The burst contains letters bound on other screens (p pauses a run, q/j/k
	// navigate lists) and a carriage return that would otherwise select.
	m = sendBurst(t, m, keyMsg("p"), keyMsg("q"), keyMsg("j"), keyMsg("\r"), keyMsg("k"), keyMsg("p"))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() = %q, want %q: only the final Enter may advance", m.Screen(), ScreenCatalogFolder)
	}
	if got := m.ReportPath(); !strings.Contains(got, "pqj") || !strings.Contains(got, "kp") {
		t.Errorf("ReportPath() = %q, want all pasted letters kept as text", got)
	}
}

func TestPastedCarriageReturnRunesDoNotSubmitCatalogFolderScreen(t *testing.T) {
	m := navigateToScreenReportPath(t, NewModel(newReportPathOptions([]string{"suite.yaml"}, newFakeSuiteRunner(), "orig.json", nil)))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // ReportPath -> CatalogFolder

	m = sendBurst(t, m, keyMsg("x"), keyMsg("\r"), keyMsg("y"))

	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() = %q after a pasted burst, want it to stay on %q", m.Screen(), ScreenCatalogFolder)
	}
}
