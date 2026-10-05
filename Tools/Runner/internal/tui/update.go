package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens/decision"
	"mosaic-run/internal/tui/screens/runflow"
)

// Update processes an incoming message and drives the screen state machine.
func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global ctrl+c: cancel context and quit.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && matchesKey(keyMsg, globalKeys.Cancel) {
		m.ctxCancel()
		m.replyToPendingQuestion(answerMsg{
			confirmAns: interaction.ConfirmAnswer{Status: interaction.Cancelled},
		})
		return m, tea.Quit
	}

	// Window resize: propagate to all screens.
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
		m.resizeScreens()
		return m, nil
	}

	// Question from the Interaction port (service goroutine).
	if qMsg, ok := msg.(questionMsg); ok {
		return m.handleQuestionMsg(qMsg)
	}

	// Session completed.
	if doneMsg, ok := msg.(runDoneMsg); ok {
		return m.handleRunDone(doneMsg)
	}
	if errMsg, ok := msg.(runErrorMsg); ok {
		return m.handleRunError(errMsg)
	}

	// Artifact content update.
	if artMsg, ok := msg.(artifactContentMsg); ok {
		return m.handleArtifactContent(artMsg)
	}

	// Test-flow orchestrator progress messages. These are sent via
	// tea.Program.Send() from the orchestrator goroutine.
	if model, cmd, handled := m.handleTestFlowMsg(msg); handled {
		return model, cmd
	}

	// Delegate to current screen.
	return m.dispatchScreen(msg)
}

// handleRunDone handles the runDoneMsg sent by the session goroutine when the
// run completes. A launch-failure cause routes to the executable-override
// screen instead of the normal done/stop screens, because the user may be
// able to fix it by supplying a working executable path.
func (m *rootModel) handleRunDone(doneMsg runDoneMsg) (tea.Model, tea.Cmd) {
	style := stylesFromTheme(m.theme)
	// Check for a launch-failure cause before any status-based branching.
	// A RunRefused or RunStoppedByConsultant outcome caused by a harness
	// launch failure routes to the override screen instead of the normal
	// done or stop screens, because the user may be able to fix it by
	// supplying a working executable path.
	var launchErr *domain.HarnessLaunchError
	if errors.As(doneMsg.outcome.Cause, &launchErr) {
		m.launchFailureAttempt++
		m.lastLaunchFailureOutcome = &doneMsg.outcome
		m.lastLaunchFailureErr = nil
		m.execOverrideScreen = decision.NewExecOverrideScreen(
			launchErr.Harness, launchErr.Executable,
			m.launchFailureAttempt, m.width, m.height, style,
		)
		m.screen = screenExecOverride
		return m, m.execOverrideScreen.InputInit()
	}
	// Non-launch-failure terminal outcome: reset the consecutive-failure counter.
	m.launchFailureAttempt = 0
	if doneMsg.outcome.Status == domain.RunStoppedByConsultant {
		m.stopScreen = decision.NewStopScreen(doneMsg.outcome.StopReason, m.width, m.height, style)
		m.screen = screenStop
		return m, nil
	}
	// Write the COMPLETED phase marker when the run finished successfully.
	// A non-empty run folder is required; a failed write is non-fatal — the
	// TUI proceeds to the done screen and surfaces the error as a warning.
	markerErrMsg := ""
	if doneMsg.outcome.Status == domain.RunCompleted && m.selections.runFolder != "" {
		store := m.resolveArtifactStore(m.selections.runFolder)
		now := m.resolveClockTime()
		if _, err := store.SetPhase(m.ctx, domain.ArtifactState{}, "COMPLETED", now); err != nil {
			markerErrMsg = err.Error()
		}
	}
	m.doneScreen = runflow.NewDoneScreen(doneMsg.outcome, markerErrMsg, m.width, m.height, style)
	m.screen = screenDone
	return m, nil
}

// handleRunError handles the runErrorMsg sent by the session goroutine when
// Start returns a non-nil error. Launch failures route to the override
// screen; all other errors continue to the existing done screen, because a
// path override cannot fix them.
func (m *rootModel) handleRunError(errMsg runErrorMsg) (tea.Model, tea.Cmd) {
	style := stylesFromTheme(m.theme)
	var launchErr *domain.HarnessLaunchError
	if errors.As(errMsg.err, &launchErr) {
		m.launchFailureAttempt++
		m.lastLaunchFailureErr = errMsg.err
		m.lastLaunchFailureOutcome = nil
		m.execOverrideScreen = decision.NewExecOverrideScreen(
			launchErr.Harness, launchErr.Executable,
			m.launchFailureAttempt, m.width, m.height, style,
		)
		m.screen = screenExecOverride
		return m, m.execOverrideScreen.InputInit()
	}
	// Non-launch-failure error: reset the consecutive-failure counter.
	m.launchFailureAttempt = 0
	m.doneScreen = runflow.NewDoneScreen(domain.RunOutcome{}, errMsg.err.Error(), m.width, m.height, style)
	m.screen = screenDone
	return m, nil
}

// handleArtifactContent applies an artifactContentMsg to the artifact screen,
// when one is active.
func (m *rootModel) handleArtifactContent(artMsg artifactContentMsg) (tea.Model, tea.Cmd) {
	if m.artifactScreen != nil {
		m.artifactScreen.SetContent(artMsg.content)
	}
	return m, nil
}

// handleTestFlowMsg applies one of the test-flow orchestrator progress
// messages to the test progress screen. These are sent via tea.Program.Send()
// from the orchestrator goroutine. handled is false when msg is none of these
// types, so the caller falls through to the screen dispatch switch.
func (m *rootModel) handleTestFlowMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if rpMsg, ok := msg.(testResolvedPathsMsg); ok {
		if m.testProgressScreen != nil {
			m.testProgressScreen.SetResolvedPaths(rpMsg.Paths, rpMsg.HarnessOrder)
		}
		return m, nil, true
	}
	if _, ok := msg.(testDeployStartMsg); ok {
		if m.testProgressScreen != nil {
			m.testProgressScreen.SetDeployRunning()
		}
		return m, nil, true
	}
	if dMsg, ok := msg.(testDeployDoneMsg); ok {
		if m.testProgressScreen != nil {
			m.testProgressScreen.SetDeployDone(dMsg.Err)
		}
		return m, nil, true
	}
	if rsMsg, ok := msg.(testRunStartMsg); ok {
		if m.testProgressScreen != nil {
			m.testProgressScreen.AddTestRunning(rsMsg.Harness, rsMsg.Workflow, rsMsg.Mode)
		}
		return m, nil, true
	}
	if rdMsg, ok := msg.(testRunDoneMsg); ok {
		if m.testProgressScreen != nil {
			r := rdMsg.Result
			m.testProgressScreen.SetTestDone(r.Harness, r.WorkflowID, r.Mode, r.Pass, r.Error != nil)
		}
		return m, nil, true
	}
	if adMsg, ok := msg.(testAllDoneMsg); ok {
		if m.testProgressScreen != nil {
			m.testProgressScreen.SetAllDone()
		}
		// Pre-build the results screen so it is ready when the user dismisses progress.
		style := stylesFromTheme(m.theme)
		m.testResultsScreen = devtest.NewTestResultsScreen(m.width, m.height, style, adMsg.Summary)
		return m, nil, true
	}
	return m, nil, false
}

// dispatchScreen delegates msg to the handler for the current screen.
func (m *rootModel) dispatchScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenRunSelect:
		return m.updateRunSelect(msg)
	case screenSetupHarness:
		return m.updateSetupHarness(msg)
	case screenSetupFile:
		return m.updateSetupFile(msg)
	case screenSetupWorkflow:
		return m.updateSetupWorkflow(msg)
	case screenSetupTask:
		return m.updateSetupTask(msg)
	case screenSetupSeedInput:
		return m.updateSetupSeedInput(msg)
	case screenSetupConfig:
		return m.updateSetupConfig(msg)
	case screenSetupGHCPMode:
		return m.updateSetupGHCPMode(msg)
	case screenProgress:
		return m.updateProgress(msg)
	case screenArtifact:
		return m.updateArtifact(msg)
	case screenQuestion:
		return m.updateQuestion(msg)
	case screenStop:
		return m.updateStop(msg)
	case screenExecOverride:
		return m.updateExecOverride(msg)
	case screenDone:
		return m.updateDone(msg)

	// Test-flow screen handlers.
	case screenTestCatalog:
		return m.updateTestCatalog(msg)
	case screenTestSuite:
		return m.updateTestSuite(msg)
	case screenTestHarness:
		return m.updateTestHarness(msg)
	case screenTestGHCPMode:
		return m.updateTestGHCPMode(msg)
	case screenTestProgress:
		return m.updateTestProgress(msg)
	case screenTestResults:
		return m.updateTestResults(msg)
	}
	return m, nil
}

func (m *rootModel) resizeScreens() {
	if m.runSelectScreen != nil {
		m.runSelectScreen.Resize(m.width, m.height)
	}
	if m.harnessScreen != nil {
		m.harnessScreen.Resize(m.width, m.height)
	}
	if m.fileScreen != nil {
		m.fileScreen.Resize(m.width, m.height)
	}
	if m.workflowScreen != nil {
		m.workflowScreen.Resize(m.width, m.height)
	}
	if m.taskScreen != nil {
		m.taskScreen.Resize(m.width, m.height)
	}
	if m.seedInputScreen != nil {
		m.seedInputScreen.Resize(m.width, m.height)
	}
	if m.configScreen != nil {
		m.configScreen.Resize(m.width, m.height)
	}
	if m.ghcpModeScreen != nil {
		m.ghcpModeScreen.Resize(m.width, m.height)
	}
	if m.progressScreen != nil {
		m.progressScreen.Resize(m.width, m.height)
	}
	if m.artifactScreen != nil {
		m.artifactScreen.Resize(m.width, m.height)
	}
	if m.stopScreen != nil {
		m.stopScreen.Resize(m.width, m.height)
	}
	if m.execOverrideScreen != nil {
		m.execOverrideScreen.Resize(m.width, m.height)
	}
	if m.doneScreen != nil {
		m.doneScreen.Resize(m.width, m.height)
	}
	if m.testCatalogScreen != nil {
		m.testCatalogScreen.Resize(m.width, m.height)
	}
	if m.testSuiteScreen != nil {
		m.testSuiteScreen.Resize(m.width, m.height)
	}
	if m.testHarnessScreen != nil {
		m.testHarnessScreen.Resize(m.width, m.height)
	}
	if m.testGHCPModeScreen != nil {
		m.testGHCPModeScreen.Resize(m.width, m.height)
	}
	if m.testProgressScreen != nil {
		m.testProgressScreen.Resize(m.width, m.height)
	}
	if m.testResultsScreen != nil {
		m.testResultsScreen.Resize(m.width, m.height)
	}
}
