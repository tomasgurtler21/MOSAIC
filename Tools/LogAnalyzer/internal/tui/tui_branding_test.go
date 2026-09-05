package tui

// tui_branding_test.go verifies that SourceScreen renders the tool name and
// version in its title when SetVersion is called before View().

import (
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/tui/screens"
)

// TestSourceScreen_BrandedTitle_ContainsToolNameAndVersion verifies that when
// SetVersion is called with a non-empty version, SourceScreen.View() renders a
// title containing both "Log Analyzer" (the tool name) and the version string.
func TestSourceScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	s := screens.NewSourceScreen(80, 24, plainStyles())
	s.SetVersion("1.0.0")

	view := s.View(80, 24)

	if !strings.Contains(view, "Log Analyzer") {
		t.Errorf("branded SourceScreen view does not contain \"Log Analyzer\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded SourceScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestSourceScreen_NoVersion_TitleOmitsVersion verifies that when SetVersion is
// not called, View() degrades gracefully and still shows the standard title text
// without a version substring.
func TestSourceScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	s := screens.NewSourceScreen(80, 24, plainStyles())
	// Do not call SetVersion.

	view := s.View(80, 24)

	if !strings.Contains(view, "Log Analyzer") {
		t.Errorf("unbranded SourceScreen view does not contain \"Log Analyzer\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded SourceScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}
