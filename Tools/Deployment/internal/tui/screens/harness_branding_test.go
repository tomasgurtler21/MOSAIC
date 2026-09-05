package screens_test

// harness_branding_test.go verifies that HarnessScreen renders the tool name
// and version in its title when SetToolVersion is called before View().

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// minimalHarness is a single usable built-in harness sufficient to satisfy the
// HarnessScreen constructor. Branding tests do not exercise harness selection
// logic, so one minimal entry is all that is needed.
var minimalHarness = domain.HarnessRef{
	ID:          "x",
	DisplayName: "X",
	Tier:        domain.TierBuiltin,
	Usable:      true,
}

// TestHarnessScreen_BrandedTitle_ContainsToolNameAndVersion verifies that when
// SetToolVersion is called with a non-empty version, HarnessScreen.View()
// renders a title that contains both "Deploy" (the tool name) and the version
// string.
func TestHarnessScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{minimalHarness},
		80, 24, plainStyles(),
	)
	s.SetToolVersion("1.0.0")

	// Act
	view := s.View()

	// Assert
	if !strings.Contains(view, "Deploy") {
		t.Errorf("branded HarnessScreen view does not contain \"Deploy\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded HarnessScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestHarnessScreen_NoVersion_TitleOmitsVersion verifies that when
// SetToolVersion is not called, View() renders its title without any version
// substring, so the screen degrades gracefully when no version is supplied.
func TestHarnessScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	// Arrange
	s := screens.NewHarnessScreen(
		[]domain.HarnessRef{minimalHarness},
		80, 24, plainStyles(),
	)
	// Do not call SetToolVersion.

	// Act
	view := s.View()

	// Assert -- the title should be present but without a version string.
	if !strings.Contains(view, "Select Harness") {
		t.Errorf("unbranded HarnessScreen view does not contain \"Select Harness\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded HarnessScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}
