package tui

// help_bar_test.go verifies the suite-selection screen's help bar content.
//
// The "tab: configure run" affordance was rendered obsolete when Tab
// navigation was replaced by the Enter-driven sequential settings flow.
// These tests specify that the tab entry must be removed from the help bar
// so users are not misled into pressing Tab, which has no effect in the
// current navigation model.

import (
	"strings"
	"testing"
)

// TestSuiteSelectHelp_HasNoTabEntry asserts that suiteSelectHelp() does not
// return an entry with key "tab". The tab affordance is stale and must be
// removed; advertising a key that does nothing on the current screen is
// misleading.
func TestSuiteSelectHelp_HasNoTabEntry(t *testing.T) {
	entries := suiteSelectHelp()

	for _, e := range entries {
		if strings.EqualFold(e.Key, "tab") {
			t.Errorf("suiteSelectHelp() returned a help entry with Key %q (Desc %q); "+
				"the tab affordance must be removed from the suite-selection help bar", e.Key, e.Desc)
		}
	}
}

// TestSuiteSelectView_DoesNotAdvertiseTab asserts that the rendered output of
// the suite-selection screen does not contain the word "tab". This covers any
// path through which the tab affordance could reach the display — whether via
// the help bar, the status line, or any other rendered element.
func TestSuiteSelectView_DoesNotAdvertiseTab(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	if strings.Contains(strings.ToLower(view), "tab") {
		t.Errorf("suite-selection view contains %q; the tab entry must be removed from the help bar:\n%s", "tab", view)
	}
}
