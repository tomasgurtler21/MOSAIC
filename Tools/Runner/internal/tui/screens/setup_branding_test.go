package screens_test

// setup_branding_test.go verifies that the Runner's entry screens render the
// tool name and version in their titles when SetToolVersion is called.

import (
	"strings"
	"testing"

	"mosaic-run/internal/runselect"
	"mosaic-run/internal/tui/screens"
)

// TestOrchestratorFileScreen_BrandedTitle_ContainsToolNameAndVersion verifies
// that when SetToolVersion is called with a non-empty version,
// OrchestratorFileScreen.View() renders a title containing both "Runner" and
// the version string.
func TestOrchestratorFileScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	s := screens.NewOrchestratorFileScreen(80, 24, screens.Styles{})
	s.SetToolVersion("1.0.0")

	view := s.View()

	if !strings.Contains(view, "Runner") {
		t.Errorf("branded OrchestratorFileScreen view does not contain \"Runner\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded OrchestratorFileScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestOrchestratorFileScreen_NoVersion_TitleOmitsVersion verifies that when
// SetToolVersion is not called, View() degrades gracefully without a version
// string in the title.
func TestOrchestratorFileScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	s := screens.NewOrchestratorFileScreen(80, 24, screens.Styles{})
	// Do not call SetToolVersion.

	view := s.View()

	if !strings.Contains(view, "Orchestrator File") {
		t.Errorf("unbranded OrchestratorFileScreen view does not contain \"Orchestrator File\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded OrchestratorFileScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}

// TestRunSelectScreen_BrandedTitle_ContainsToolNameAndVersion verifies that
// when SetToolVersion is called with a non-empty version,
// RunSelectScreen.View() renders a title containing both "Runner" and the
// version string.
func TestRunSelectScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	q := runselect.Question{}
	s := screens.NewRunSelectScreen(q, 80, 24, screens.Styles{})
	s.SetToolVersion("1.0.0")

	view := s.View()

	if !strings.Contains(view, "Runner") {
		t.Errorf("branded RunSelectScreen view does not contain \"Runner\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded RunSelectScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestRunSelectScreen_NoVersion_TitleOmitsVersion verifies that when
// SetToolVersion is not called, RunSelectScreen.View() degrades gracefully
// without a version string in the title.
func TestRunSelectScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	q := runselect.Question{}
	s := screens.NewRunSelectScreen(q, 80, 24, screens.Styles{})
	// Do not call SetToolVersion.

	view := s.View()

	if !strings.Contains(view, "Select Run") {
		t.Errorf("unbranded RunSelectScreen view does not contain \"Select Run\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded RunSelectScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}

// TestHarnessSelectScreen_BrandedTitle_ContainsToolNameAndVersion verifies
// that when SetToolVersion is called with a non-empty version,
// HarnessSelectScreen.View() renders a title containing both "Runner" and
// the version string.
func TestHarnessSelectScreen_BrandedTitle_ContainsToolNameAndVersion(t *testing.T) {
	s := screens.NewHarnessSelectScreen(80, 24, screens.Styles{})
	s.SetToolVersion("1.0.0")

	view := s.View()

	if !strings.Contains(view, "Runner") {
		t.Errorf("branded HarnessSelectScreen view does not contain \"Runner\":\n%s", view)
	}
	if !strings.Contains(view, "v1.0.0") {
		t.Errorf("branded HarnessSelectScreen view does not contain \"v1.0.0\":\n%s", view)
	}
}

// TestHarnessSelectScreen_NoVersion_TitleOmitsVersion verifies that when
// SetToolVersion is not called, HarnessSelectScreen.View() degrades gracefully
// without a version string in the title.
func TestHarnessSelectScreen_NoVersion_TitleOmitsVersion(t *testing.T) {
	s := screens.NewHarnessSelectScreen(80, 24, screens.Styles{})
	// Do not call SetToolVersion.

	view := s.View()

	if !strings.Contains(view, "Select Harness") {
		t.Errorf("unbranded HarnessSelectScreen view does not contain \"Select Harness\":\n%s", view)
	}
	if strings.Contains(view, "v1.0.0") {
		t.Errorf("unbranded HarnessSelectScreen view unexpectedly contains \"v1.0.0\":\n%s", view)
	}
}
