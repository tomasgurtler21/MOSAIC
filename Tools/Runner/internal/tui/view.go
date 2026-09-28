package tui

import (
	tuicommon "mosaic-common/tui"
)

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

// View renders the current screen to a string.
func (m *rootModel) View() string {
	switch m.screen {
	case screenRunSelect:
		if m.runSelectScreen != nil {
			return m.runSelectScreen.View()
		}
	case screenSetupHarness:
		if m.harnessScreen != nil {
			return m.harnessScreen.View()
		}
	case screenSetupFile:
		return m.fileScreen.View()
	case screenSetupWorkflow:
		if m.workflowScreen != nil {
			return m.workflowScreen.View()
		}
	case screenSetupTask:
		return m.taskScreen.View()
	case screenSetupSeedInput:
		return m.seedInputScreen.View()
	case screenSetupConfig:
		return m.configScreen.View()
	case screenSetupGHCPMode:
		if m.ghcpModeScreen != nil {
			return m.ghcpModeScreen.View()
		}
	case screenProgress:
		if m.progressScreen != nil {
			return m.progressScreen.View()
		}
	case screenArtifact:
		if m.artifactScreen != nil {
			return m.artifactScreen.View()
		}
	case screenQuestion:
		return m.viewQuestion()
	case screenStop:
		if m.stopScreen != nil {
			return m.stopScreen.View()
		}
	case screenExecOverride:
		if m.execOverrideScreen != nil {
			return m.execOverrideScreen.View()
		}
	case screenDone:
		if m.doneScreen != nil {
			return m.doneScreen.View()
		}

	// Test-flow screens.
	case screenTestCatalog:
		if m.testCatalogScreen != nil {
			return m.testCatalogScreen.View()
		}
	case screenTestSuite:
		if m.testSuiteScreen != nil {
			return m.testSuiteScreen.View()
		}
	case screenTestHarness:
		if m.testHarnessScreen != nil {
			return m.testHarnessScreen.View()
		}
	case screenTestGHCPMode:
		if m.testGHCPModeScreen != nil {
			return m.testGHCPModeScreen.View()
		}
	case screenTestProgress:
		if m.testProgressScreen != nil {
			return m.testProgressScreen.View()
		}
	case screenTestResults:
		if m.testResultsScreen != nil {
			return m.testResultsScreen.View()
		}
	}
	return ""
}

func (m *rootModel) viewQuestion() string {
	if m.selectOverlay != nil {
		return m.selectOverlay.view()
	}
	if m.textOverlay != nil {
		return m.textOverlay.view()
	}
	if m.confirmOverlay != nil {
		return m.confirmOverlay.view()
	}
	return m.theme.Style(tuicommon.RoleMuted).Render("Waiting for question…")
}
