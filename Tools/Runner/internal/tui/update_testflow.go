package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens/runflow"
)

// ---------------------------------------------------------------------------
// Test-flow screen handlers
// ---------------------------------------------------------------------------

func (m *rootModel) updateTestCatalog(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testCatalogScreen == nil {
		return m, nil
	}
	m.testCatalogScreen.Update(msg)
	if m.testCatalogScreen.Back() {
		m.testCatalogScreen.Reset()
		// Return to the harness selection screen (the test flow entry point).
		m.screen = screenSetupHarness
		return m, nil
	}
	if m.testCatalogScreen.Done() {
		m.testSelections.mosaicRoot = m.testCatalogScreen.Value()
		m.testCatalogScreen.Reset()

		// Load the catalog and proceed to the suite selection screen.
		catRoot := filepath.Join(m.testSelections.mosaicRoot, "Tools", "Runner", "TestCatalog")
		loader := m.testCatalogLoader
		if loader == nil {
			loader = loadTestCatalog
		}
		cat, err := loader(catRoot)
		if err != nil {
			style := stylesFromTheme(m.theme)
			m.doneScreen = runflow.NewDoneScreen(
				domain.RunOutcome{Status: domain.RunRefused, Message: fmt.Sprintf("loading test catalog: %v", err)},
				"", m.width, m.height, style,
			)
			m.screen = screenDone
			return m, nil
		}
		style := stylesFromTheme(m.theme)
		m.testSuiteScreen = devtest.NewTestSuiteScreen(m.width, m.height, style, cat)
		m.screen = screenTestSuite
		return m, nil
	}
	return m, nil
}

func (m *rootModel) updateTestSuite(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testSuiteScreen == nil {
		return m, nil
	}
	m.testSuiteScreen.Update(msg)
	if m.testSuiteScreen.Back() {
		m.testSuiteScreen.Reset()
		// Return to catalog path entry.
		style := stylesFromTheme(m.theme)
		m.testCatalogScreen = devtest.NewTestCatalogScreen(m.width, m.height, style)
		m.screen = screenTestCatalog
		return m, m.testCatalogScreen.InputInit()
	}
	if m.testSuiteScreen.Done() {
		m.testSelections.scope = m.testSuiteScreen.Scope()
		m.testSelections.workflows = m.testSuiteScreen.SelectedWorkflows()
		m.testSelections.mode = m.testSuiteScreen.SelectedMode()
		m.testSuiteScreen.Reset()

		style := stylesFromTheme(m.theme)
		m.testHarnessScreen = devtest.NewTestHarnessScreen(m.width, m.height, style)
		m.screen = screenTestHarness
		return m, nil
	}
	return m, nil
}

func (m *rootModel) updateTestHarness(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testHarnessScreen == nil {
		return m, nil
	}
	m.testHarnessScreen.Update(msg)
	if m.testHarnessScreen.Back() {
		m.testHarnessScreen.Reset()
		// Return to suite selection. Re-create the screen with the same catalog.
		catRoot := filepath.Join(m.testSelections.mosaicRoot, "Tools", "Runner", "TestCatalog")
		loader := m.testCatalogLoader
		if loader == nil {
			loader = loadTestCatalog
		}
		cat, err := loader(catRoot)
		if err != nil {
			// Catalog load failed on back navigation — fall back to catalog entry.
			style := stylesFromTheme(m.theme)
			m.testCatalogScreen = devtest.NewTestCatalogScreen(m.width, m.height, style)
			m.screen = screenTestCatalog
			return m, m.testCatalogScreen.InputInit()
		}
		style := stylesFromTheme(m.theme)
		m.testSuiteScreen = devtest.NewTestSuiteScreen(m.width, m.height, style, cat)
		m.screen = screenTestSuite
		return m, nil
	}
	if m.testHarnessScreen.Done() {
		m.testSelections.harnesses = m.testHarnessScreen.SelectedHarnesses()
		hasGHCP := m.testHarnessScreen.HasGHCPCLI()
		m.testHarnessScreen.Reset()

		if hasGHCP {
			style := stylesFromTheme(m.theme)
			m.testGHCPModeScreen = devtest.NewTestGHCPModeScreen(m.width, m.height, style)
			m.screen = screenTestGHCPMode
			return m, nil
		}
		// No GHCP CLI: skip mode screen and launch the test run.
		m.testSelections.ghcpPermissionMode = ""
		return m, m.launchTestRun()
	}
	return m, nil
}

func (m *rootModel) updateTestGHCPMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testGHCPModeScreen == nil {
		return m, nil
	}
	m.testGHCPModeScreen.Update(msg)
	if m.testGHCPModeScreen.Back() {
		m.testGHCPModeScreen.Reset()
		style := stylesFromTheme(m.theme)
		m.testHarnessScreen = devtest.NewTestHarnessScreen(m.width, m.height, style)
		m.screen = screenTestHarness
		return m, nil
	}
	if m.testGHCPModeScreen.Done() {
		m.testSelections.ghcpPermissionMode = m.testGHCPModeScreen.Value()
		m.testGHCPModeScreen.Reset()
		return m, m.launchTestRun()
	}
	return m, nil
}

func (m *rootModel) updateTestProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testProgressScreen == nil {
		return m, nil
	}
	m.testProgressScreen.Update(msg)
	if m.testProgressScreen.Done() {
		// All tests finished and user dismissed progress: show results.
		if m.testResultsScreen != nil {
			m.screen = screenTestResults
		} else {
			// Results screen not built yet (no summary arrived): treat as done.
			m.screen = screenDone
		}
		return m, nil
	}
	return m, nil
}

func (m *rootModel) updateTestResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.testResultsScreen == nil {
		return m, nil
	}
	m.testResultsScreen.Update(msg)
	if m.testResultsScreen.Done() {
		return m, tea.Quit
	}
	return m, nil
}

// launchTestRun starts the test orchestrator in a background goroutine, wiring
// its progress callbacks to the TUI via tea.Program.Send. It transitions the
// screen to screenTestProgress and returns the launch command.
func (m *rootModel) launchTestRun() tea.Cmd {
	style := stylesFromTheme(m.theme)
	m.testProgressScreen = devtest.NewTestProgressScreen(m.width, m.height, style)
	m.screen = screenTestProgress

	workDir, _ := os.Getwd()
	cfg := testrun.TestConfig{
		Scope:              m.testSelections.scope,
		Harnesses:          m.testSelections.harnesses,
		MosaicRoot:         m.testSelections.mosaicRoot,
		Workspace:          workDir,
		Workflows:          m.testSelections.workflows,
		Mode:               m.testSelections.mode,
		GHCPPermissionMode: m.testSelections.ghcpPermissionMode,
	}

	factory := m.testRunnerFactory
	interact := m.interact
	ctx := m.ctx

	return func() tea.Msg {
		if factory == nil {
			return testAllDoneMsg{Summary: &testrun.TestSummary{
				DeployError: fmt.Errorf("test runner not configured"),
			}}
		}

		// Build a reporter that sends progress messages to the TUI via the
		// stored *tea.Program reference. This mirrors how stepCompleteMsg is
		// sent from the session goroutine through m.interact.
		reporter := &tuiTestProgressReporter{interact: interact}
		summary, err := factory(ctx, cfg, reporter)
		if err != nil && summary == nil {
			summary = &testrun.TestSummary{DeployError: err}
		}
		if summary == nil {
			summary = &testrun.TestSummary{}
		}
		return testAllDoneMsg{Summary: summary}
	}
}

// tuiTestProgressReporter implements testrun.ProgressReporter by forwarding
// state transitions to the TUI as tea.Msg values via tea.Program.Send.
type tuiTestProgressReporter struct {
	interact *ProgramRef
}

func (r *tuiTestProgressReporter) OnDeployStart() {
	if p := r.interact.program(); p != nil {
		p.Send(testDeployStartMsg{})
	}
}

func (r *tuiTestProgressReporter) OnDeployDone(err error) {
	if p := r.interact.program(); p != nil {
		p.Send(testDeployDoneMsg{Err: err})
	}
}

func (r *tuiTestProgressReporter) OnTestStart(harness, workflow, mode string) {
	if p := r.interact.program(); p != nil {
		p.Send(testRunStartMsg{Harness: harness, Workflow: workflow, Mode: mode})
	}
}

func (r *tuiTestProgressReporter) OnTestDone(harness, workflow, mode string, result testrun.TestRunResult) {
	if p := r.interact.program(); p != nil {
		p.Send(testRunDoneMsg{Result: result})
	}
}

// OnResolvedPaths implements testrun.ResolvedPathsReporter. It sends a
// testResolvedPathsMsg to the TUI progress screen before any deploy or test
// start notifications arrive. This satisfies FR-7 ("shown at startup"):
// the progress screen displays resolved binaries before the first test runs.
func (r *tuiTestProgressReporter) OnResolvedPaths(paths map[string]string, harnessOrder []string) {
	if p := r.interact.program(); p != nil {
		p.Send(testResolvedPathsMsg{Paths: paths, HarnessOrder: harnessOrder})
	}
}

// loadTestCatalog is a thin helper that returns a testrun.CatalogPort backed
// by a real *testcatalog.Catalog. It is separated from the handler so tests
// can inject fakes without touching the filesystem.
func loadTestCatalog(catRoot string) (testrun.CatalogPort, error) {
	// Import is deferred to avoid a cycle: the caller (app.go) imports testrun,
	// which does not import testcatalog. The catalog package is an implementation
	// detail of the wiring layer; the TUI only depends on testrun's CatalogPort.
	// Because Go does not allow conditional imports, we call the function here
	// and let the linker include testcatalog when this file is compiled.
	//
	// In production, wiring_stub.go injects a real catalog via TestRunnerFactory.
	// In unit tests for the TUI, the TestRunnerFactory is either nil or faked,
	// so loadTestCatalog is only called when a real filesystem path is available.
	//
	// Because the TUI package must not grow a direct testcatalog import (it would
	// drag every catalog dependency into the TUI binary even for non-dev builds),
	// the real catalog load is injected via the TestCatalogLoader option instead.
	// This stub returns an error so the missing injection is surfaced at runtime.
	_ = catRoot
	return nil, fmt.Errorf("test catalog loader not configured; supply Options.TestCatalogLoader")
}
