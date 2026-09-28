package tui

// devmode_test.go covers DevMode gating and the test-flow entry point:
// whether the "Run Tests" harness-screen option is present, and navigation
// into and back out of the test catalog screen. Tests run in package tui
// (internal) to access unexported screenID constants and rootModel fields.

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newDevModel returns a rootModel with DevMode enabled, a fake catalog loader,
// and a no-op test runner factory. The session stub is the same type used
// throughout this package (stubNavSession).
func newDevModel() *rootModel {
	sess := &stubNavSession{}
	return newRootModel(context.Background(), sess, Options{
		Theme:   tuicommon.DefaultTheme(),
		DevMode: true,
		TestCatalogLoader: func(_ string) (testrun.CatalogPort, error) {
			return &fakeCatalog{}, nil
		},
		TestRunnerFactory: func(_ context.Context, _ testrun.TestConfig, _ testrun.ProgressReporter) (*testrun.TestSummary, error) {
			return &testrun.TestSummary{AllPass: true, TotalPass: 1}, nil
		},
	})
}

// newNonDevModel returns a rootModel with DevMode disabled.
func newNonDevModel() *rootModel {
	sess := &stubNavSession{}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}

// sendString sends a rune-based key message to the model and discards the
// returned cmd. Returns the updated model for optional inspection.
func sendString(m *rootModel, s string) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

func sendEnter(m *rootModel) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

func sendEsc(m *rootModel) *rootModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if rm, ok := updated.(*rootModel); ok {
		return rm
	}
	return m
}

// newTestProgressScreenFor builds a TestProgressScreen sized to match the model.
func newTestProgressScreenFor(m *rootModel) *devtest.TestProgressScreen {
	style := stylesFromTheme(m.theme)
	return devtest.NewTestProgressScreen(m.width, m.height, style)
}

// ---------------------------------------------------------------------------
// fakeCatalog implements testrun.CatalogPort with empty responses.
// ---------------------------------------------------------------------------

type fakeCatalog struct{}

func (f *fakeCatalog) Workflows() []testcatalog.CatalogEntry { return nil }
func (f *fakeCatalog) SmokeSet() []testcatalog.CatalogEntry  { return nil }
func (f *fakeCatalog) FullSuite() []testcatalog.CatalogEntry { return nil }
func (f *fakeCatalog) WorkflowByID(_ string) ([]testcatalog.CatalogEntry, error) {
	return nil, nil
}
func (f *fakeCatalog) WorkflowModes(_ string) ([]string, error) { return nil, nil }
func (f *fakeCatalog) WorkflowIDs() []string                    { return nil }
func (f *fakeCatalog) SidecarPath(_, _ string) string           { return "" }
func (f *fakeCatalog) UnionInfrastructureAgentKeys() []string   { return []string{} }

// ---------------------------------------------------------------------------
// AC9.2: Test flow invisible without --dev
// ---------------------------------------------------------------------------

// TestDevMode_Disabled_HarnessScreenHasNoRunTestsOption verifies that when
// DevMode is false, the "Run Tests" entry is absent from the harness screen.
func TestDevMode_Disabled_HarnessScreenHasNoRunTestsOption(t *testing.T) {
	m := newNonDevModel()
	if m.screen != screenSetupHarness {
		t.Fatalf("expected initial screen screenSetupHarness (%d), got %d", screenSetupHarness, m.screen)
	}
	view := m.harnessScreen.View()
	if strings.Contains(view, "Run Tests") {
		t.Error("harness screen must not contain 'Run Tests' when DevMode is false")
	}
}

// TestDevMode_Disabled_DevModeFieldIsFalse verifies that rootModel.devMode is
// false when Options.DevMode is false.
func TestDevMode_Disabled_DevModeFieldIsFalse(t *testing.T) {
	m := newNonDevModel()
	if m.devMode {
		t.Error("rootModel.devMode must be false when Options.DevMode is false")
	}
}

// ---------------------------------------------------------------------------
// AC9.1: "Run Tests" option visible with --dev
// ---------------------------------------------------------------------------

// TestDevMode_Enabled_HarnessScreenShowsRunTestsOption verifies that when
// DevMode is true, the harness screen lists "Run Tests".
func TestDevMode_Enabled_HarnessScreenShowsRunTestsOption(t *testing.T) {
	m := newDevModel()
	if m.screen != screenSetupHarness {
		t.Fatalf("expected initial screen screenSetupHarness (%d), got %d", screenSetupHarness, m.screen)
	}
	view := m.harnessScreen.View()
	if !strings.Contains(view, "Run Tests") {
		t.Error("harness screen must contain 'Run Tests' when DevMode is true")
	}
}

// TestDevMode_Enabled_DevModeFieldIsTrue verifies that rootModel.devMode is
// true when Options.DevMode is true.
func TestDevMode_Enabled_DevModeFieldIsTrue(t *testing.T) {
	m := newDevModel()
	if !m.devMode {
		t.Error("rootModel.devMode must be true when Options.DevMode is true")
	}
}

// TestDevMode_Enabled_SelectRunTests_TransitionsToTestCatalog verifies that
// pressing Enter on the "Run Tests" entry (first item in dev-mode harness
// screen) transitions the model to screenTestCatalog.
func TestDevMode_Enabled_SelectRunTests_TransitionsToTestCatalog(t *testing.T) {
	m := newDevModel()
	// "Run Tests" is the first item; Enter on it selects it.
	m = sendEnter(m)
	if m.screen != screenTestCatalog {
		t.Errorf("expected screenTestCatalog (%d) after selecting Run Tests, got %d", screenTestCatalog, m.screen)
	}
	if m.testCatalogScreen == nil {
		t.Error("testCatalogScreen must be non-nil after entering test flow")
	}
}

// TestDevMode_Enabled_BackFromTestCatalog_ReturnsToHarnessScreen verifies that
// pressing Esc on the test catalog screen returns to the harness screen.
func TestDevMode_Enabled_BackFromTestCatalog_ReturnsToHarnessScreen(t *testing.T) {
	m := newDevModel()
	m = sendEnter(m) // enter test flow -> screenTestCatalog
	if m.screen != screenTestCatalog {
		t.Fatalf("expected screenTestCatalog, got %d", m.screen)
	}
	m = sendEsc(m) // Esc -> back to harness screen
	if m.screen != screenSetupHarness {
		t.Errorf("expected screenSetupHarness after Esc on catalog screen, got %d", m.screen)
	}
}
