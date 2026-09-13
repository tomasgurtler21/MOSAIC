package screens_test

// mode_branding_test.go verifies that ModeScreen renders the tool name and
// version in its title when SetToolVersion is called before View().

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/tui/screens"
)

// TestModeScreen_BrandedTitle_ContainsToolNameAndVersion verifies that when
// SetToolVersion is called with a non-empty version, ModeScreen.View() renders
// a title that contains both "Deploy" (the tool name) and the version string.
func TestModeScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	s := screens.NewModeScreen(80, 24, plainStyles())
	s.SetToolVersion("1.0.0")

	view := s.View()

	if !strings.Contains(view, "Deploy") {
		t.Errorf("branded ModeScreen view does not contain \"Deploy\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded ModeScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestModeScreen_NoVersion_TitleOmitsVersion verifies that when SetToolVersion
// is not called, View() renders its title without any version substring, so
// the screen degrades gracefully when no version is supplied.
func TestModeScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	s := screens.NewModeScreen(80, 24, plainStyles())
	// Do not call SetToolVersion.

	view := s.View()

	// The title should still be present but without a version string.
	if !strings.Contains(view, "Select Mode") {
		t.Errorf("unbranded ModeScreen view does not contain \"Select Mode\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded ModeScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}
