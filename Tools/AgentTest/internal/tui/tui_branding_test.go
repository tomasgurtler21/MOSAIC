package tui

// tui_branding_test.go verifies that the mode-select screen renders the tool
// name and version in its title when ToolVersion is set in Options.

import (
	"strings"
	"testing"
)

// TestModeSelect_BrandedTitle_ContainsToolNameAndVersion verifies that when
// Options.ToolVersion is set, viewModeSelect() renders a title containing
// both "AgentTest" (the tool name) and the version string.
func TestModeSelect_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	opts.ToolVersion = "1.0.0"
	m := NewModel(opts)

	view := safeView(t, m)

	if !strings.Contains(view, "AgentTest") {
		t.Errorf("branded mode-select view does not contain \"AgentTest\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded mode-select view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestModeSelect_NoVersion_TitleOmitsVersion verifies that when
// Options.ToolVersion is not set, viewModeSelect() degrades gracefully and
// still shows "AgentTest" without any version substring.
func TestModeSelect_NoVersion_TitleOmitsVersion(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	// ToolVersion intentionally left empty.
	m := NewModel(opts)

	view := safeView(t, m)

	if !strings.Contains(view, "AgentTest") {
		t.Errorf("unbranded mode-select view does not contain \"AgentTest\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded mode-select view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}
