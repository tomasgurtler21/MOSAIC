package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/decision"
	"mosaic-run/internal/tui/screens/runflow"
)

// ---------------------------------------------------------------------------
// Progress screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.progressScreen.Update(msg)

	// Drain the gate transitions this update produced and record them. Draining
	// rather than polling is what keeps the log honest: each transition is
	// reported exactly once, keys the gate ignores record nothing at all, and
	// the arming entry below is driven by the confirmation itself rather than by
	// the per-update poll.
	confirmed := false
	for _, ev := range m.progressScreen.TakeStopGateEvents() {
		switch ev.Kind {
		case runflow.StopGateEntered:
			m.debug.Log(domain.EventTUIStopGateEntered, "stop confirmation gate entered",
				domain.F("key", ev.Key))
		case runflow.StopGateResolved:
			outcome := "cancelled"
			if ev.Confirmed {
				outcome = "confirmed"
				confirmed = true
			}
			m.debug.Log(domain.EventTUIStopGateResolved, "stop confirmation gate resolved",
				domain.F("key", ev.Key), domain.F("outcome", outcome))
		}
	}

	if m.progressScreen.GracefulStop() {
		m.stopSignal.Request()
		if confirmed {
			// Only in the update that drained the confirmation, so the entry
			// follows the arming it records and a resumed run's stop records its
			// own. Request() itself is idempotent and runs on every update.
			m.debug.Log(domain.EventTUIStopSignalArmed, "graceful-stop signal armed")
		}
	}

	if m.progressScreen.ArtifactViewRequested() {
		m.progressScreen.ClearArtifactViewRequest()
		m.prevScreen = screenProgress
		content := m.readArtifactContent()
		style := stylesFromTheme(m.theme)
		m.artifactScreen = runflow.NewArtifactScreen(content, m.width, m.height, style)
		m.screen = screenArtifact
		return m, nil
	}

	return m, cmd
}

// extractStatus parses the status from a session notice message.
// Format: "phase=X stage=\"Y\" status=Z"
func extractStatus(msg string) string {
	return extractField(msg, "status")
}

// extractField parses a specific key=value pair from a session notice message.
// Values may be optionally double-quoted (e.g. stage="Stage-1"); quotes are stripped.
func extractField(msg, key string) string {
	prefix := key + "="
	for _, part := range strings.Fields(msg) {
		if strings.HasPrefix(part, prefix) {
			val := strings.TrimPrefix(part, prefix)
			return strings.Trim(val, "\"")
		}
	}
	return ""
}

// readArtifactContent reads the Orchestration.md file from the canonical
// run-scoped path. The orchestrator file's directory is never consulted.
//
// Decision table:
//   - runFolder empty              → ArtifactNotYetCreatedMessage
//   - runFolder set, file exists   → file content verbatim
//   - runFolder set, file missing  → ArtifactNotYetCreatedMessage
//   - runFolder set, other error   → "(could not read artifact: {err})"
func (m *rootModel) readArtifactContent() string {
	if m.selections.runFolder == "" {
		return ArtifactNotYetCreatedMessage
	}
	artPath := filepath.Join(m.selections.runFolder, "Orchestration.md")
	data, err := os.ReadFile(artPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ArtifactNotYetCreatedMessage
		}
		return fmt.Sprintf("(could not read artifact: %v)", err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// Artifact inspection screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateArtifact(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.artifactScreen == nil {
		return m, nil
	}
	m.artifactScreen.Update(msg)
	if m.artifactScreen.Done() {
		m.artifactScreen.Reset()
		m.screen = m.prevScreen
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Generic question overlay handler (Interaction port)
// ---------------------------------------------------------------------------

func (m *rootModel) handleQuestionMsg(qMsg questionMsg) (tea.Model, tea.Cmd) {
	switch qMsg.kind {
	case questionNotice:
		// Route notices to the progress screen if active.
		if m.progressScreen != nil {
			notice := qMsg.notice
			if notice.Level == interaction.NoticeInfo {
				status := extractStatus(notice.Message)
				if status == "running" {
					// Step is starting — append a new progress row.
					m.progressScreen.AppendRow(runflow.ProgressRow{
						AgentInstance: notice.Title,
						Phase:         extractField(notice.Message, "phase"),
						Stage:         extractField(notice.Message, "stage"),
						Status:        "running",
					})
				} else if status != "" {
					// Step completed — mark the current row complete.
					m.progressScreen.CompleteRow(status)
				}
				m.progressScreen.SetStatus(notice.Title+": "+notice.Message, false)
			} else {
				m.progressScreen.SetStatus(notice.Message, notice.Level == interaction.NoticeError)
			}
		}
		return m, nil

	case questionProgress:
		if m.progressScreen != nil {
			e := qMsg.progress
			label := e.Phase
			if e.Total > 0 {
				label = fmt.Sprintf("%s %d/%d %s", e.Phase, e.Current, e.Total, e.Subject)
			} else if e.Subject != "" {
				label = fmt.Sprintf("%s %s", e.Phase, e.Subject)
			}
			m.progressScreen.SetStatus(label, false)
		}
		return m, nil

	case questionSelectOne:
		m.activeQuestion = &qMsg
		m.selectOverlay = newInlineSelectOne(qMsg.choiceQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, nil

	case questionAskText:
		m.activeQuestion = &qMsg
		m.textOverlay = newInlineText(qMsg.textQ, m.theme, m.width, m.height)
		m.screen = screenQuestion
		return m, m.textOverlay.init()

	case questionConfirm:
		m.activeQuestion = &qMsg
		m.confirmOverlay = newInlineConfirm(qMsg.confirmQ, m.theme, m.width)
		m.screen = screenQuestion
		return m, nil
	}
	return m, nil
}

func (m *rootModel) updateQuestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.selectOverlay != nil {
		done := m.selectOverlay.update(msg)
		if done {
			ans := m.selectOverlay.answer()
			m.replyToPendingQuestion(answerMsg{choiceAns: ans})
			m.selectOverlay = nil
			m.screen = screenProgress
		}
		return m, nil
	}
	if m.textOverlay != nil {
		cmd := m.textOverlay.update(msg)
		if m.textOverlay.done {
			ans := m.textOverlay.answer()
			m.replyToPendingQuestion(answerMsg{textAns: ans})
			m.textOverlay = nil
			m.screen = screenProgress
		}
		return m, cmd
	}
	if m.confirmOverlay != nil {
		done := m.confirmOverlay.update(msg)
		if done {
			ans := m.confirmOverlay.answer()
			m.replyToPendingQuestion(answerMsg{confirmAns: ans})
			m.confirmOverlay = nil
			m.screen = screenProgress
		}
		return m, nil
	}
	return m, nil
}

// replyToPendingQuestion sends an answer to the active question and clears it.
func (m *rootModel) replyToPendingQuestion(ans answerMsg) {
	if m.activeQuestion != nil && m.activeQuestion.reply != nil {
		m.activeQuestion.reply <- ans
		m.activeQuestion = nil
	}
}

// ---------------------------------------------------------------------------
// Restart-path stop-state reset
// ---------------------------------------------------------------------------

// resetStopStateForRestart returns the run to a clean stop state before a
// session is rebuilt or restarted. It disarms the shared stop signal and clears
// the progress screen's own latched confirm/stop state.
//
// Called from every session-restart path: the done-screen continue path, the
// exec-override retry path, and the stop-recovery screen path. Two of the three
// reuse the existing ProgressScreen instance, so disarming the signal alone is
// not sufficient — the screen's latch is separate state. A carried-over latch
// leaves the resumed run showing a notice for a stop that was just cancelled,
// and makes the stop key inert, because the confirmation gate is only entered
// when no stop is already latched.
//
// Idempotent and nil-safe: safe to call when no progress screen exists yet.
func (m *rootModel) resetStopStateForRestart() {
	m.stopSignal.Reset()
	if m.progressScreen != nil {
		m.progressScreen.ResetStopState()
	}
}

// ---------------------------------------------------------------------------
// Stop recovery screen handler
// ---------------------------------------------------------------------------

// updateStop handles input for the stop recovery screen. When the user chooses
// Retry, the session is re-started from the progress screen. When the user
// chooses Manual dispatch, the progress screen is shown so the session goroutine
// can present the ManualResolver question overlay.
func (m *rootModel) updateStop(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.stopScreen == nil {
		return m, nil
	}
	m.stopScreen.Update(msg)
	if m.stopScreen.Back() {
		// Treat Esc on the stop screen as a terminal quit.
		return m, tea.Quit
	}
	if m.stopScreen.Done() {
		// Return the run to a clean stop state before it is restarted.
		//
		// Reachable with an armed stop signal. This screen is reached only on a
		// RunStoppedByConsultant outcome, which is decided within a step, while
		// a user-confirmed graceful stop is only observed at the next dispatch
		// checkpoint. A user who confirms a stop during a step the consultant
		// then ends arrives here with the signal armed and the progress screen's
		// stop latch set. The reset is unconditional regardless.
		m.resetStopStateForRestart()
		// Record which recovery action the user chose before clearing the screen,
		// so startSession() can include ManualDispatch in the RunConfig.
		if m.stopScreen.Choice() == decision.StopChoiceManualDispatch {
			m.selections.manualDispatch = true
		} else {
			m.selections.manualDispatch = false
		}
		style := stylesFromTheme(m.theme)
		if m.progressScreen == nil {
			m.progressScreen = runflow.NewProgressScreen(m.width, m.height, style)
		}
		m.stopScreen = nil
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Executable-override screen handler
// ---------------------------------------------------------------------------

// updateExecOverride handles input for the executable-override recovery screen.
// When the user confirms a non-empty path (Choice() == ExecOverrideChoiceRetry),
// the session is rebuilt with the override path and restarted against the same
// run folder. When the user abandons (Choice() == ExecOverrideChoiceAbandon via Esc),
// the TUI transitions to the done screen with the original failure.
//
// Routing to this handler is performed in the runDoneMsg and runErrorMsg handlers,
// which check errors.As(outcome.Cause, &launchErr) / errors.As(err, &launchErr) and
// transition to screenExecOverride on a *domain.HarnessLaunchError.
func (m *rootModel) updateExecOverride(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.execOverrideScreen == nil {
		return m, nil
	}
	m.execOverrideScreen.Update(msg)
	if m.execOverrideScreen.Back() {
		// Abandon: show the done screen with the original failure.
		style := stylesFromTheme(m.theme)
		if m.lastLaunchFailureOutcome != nil {
			m.doneScreen = runflow.NewDoneScreen(*m.lastLaunchFailureOutcome, "", m.width, m.height, style)
		} else if m.lastLaunchFailureErr != nil {
			m.doneScreen = runflow.NewDoneScreen(domain.RunOutcome{}, m.lastLaunchFailureErr.Error(), m.width, m.height, style)
		} else {
			m.doneScreen = runflow.NewDoneScreen(domain.RunOutcome{}, "launch failed", m.width, m.height, style)
		}
		m.execOverrideScreen = nil
		m.screen = screenDone
		return m, nil
	}
	if m.execOverrideScreen.Done() {
		// Return the run to a clean stop state before the session is rebuilt, so
		// the new session never observes the prior run's stop.
		//
		// Reachable with an armed stop signal. The launch-failure check in the
		// runDoneMsg handler precedes all status-based branching, so any terminal
		// outcome carrying a *domain.HarnessLaunchError routes here — including
		// one produced while a user-confirmed graceful stop was still awaiting
		// its dispatch checkpoint. The reset is unconditional regardless.
		m.resetStopStateForRestart()
		// Retry: hold the override path, rebuild the session, restart.
		m.selections.config.ExecutablePath = m.execOverrideScreen.Path()
		if m.sessionFactory != nil {
			m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
		}
		m.execOverrideScreen = nil
		style := stylesFromTheme(m.theme)
		if m.progressScreen == nil {
			m.progressScreen = runflow.NewProgressScreen(m.width, m.height, style)
		}
		m.screen = screenProgress
		return m, tea.Batch(m.progressScreen.Init(), m.startSession())
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Done screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.doneScreen != nil {
		m.doneScreen.Update(msg)
		if m.doneScreen.Done() {
			return m, tea.Quit
		}
		if m.doneScreen.Continue() {
			// Resume: disarm the prior confirmed stop so the new run's dispatch
			// loop does not see a stale stop signal on its very first boundary
			// check. This path also rebuilds the progress screen below, so the
			// screen half of the reset is redundant here; calling the shared
			// helper anyway keeps one reset expression across all three paths.
			m.resetStopStateForRestart()
			// Rebuild the session via the factory if one is set, mirroring the
			// updateExecOverride retry path. Reuse m.sess directly when no factory
			// is set (test/backward-compat path).
			if m.sessionFactory != nil {
				m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
			}
			// Always construct a new ProgressScreen — the old one still holds
			// the completed run's row history and stop notice.
			style := stylesFromTheme(m.theme)
			m.progressScreen = runflow.NewProgressScreen(m.width, m.height, style)
			m.doneScreen = nil
			m.screen = screenProgress
			// Reuse m.ctx unchanged — Stage 2 stopped cancelling ctx on graceful
			// stop, so the existing context is still valid and reusable.
			return m, tea.Batch(m.progressScreen.Init(), m.startSession())
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Completion-marker helpers
// ---------------------------------------------------------------------------

// resolveArtifactStore returns the artifact store to use for the COMPLETED
// marker write. When an ArtifactStoreFactory was injected it is called with
// the run folder; otherwise the default file-based store is used.
func (m *rootModel) resolveArtifactStore(runFolder string) domain.ArtifactStore {
	if m.artifactStoreFactory != nil {
		return m.artifactStoreFactory(runFolder)
	}
	return artifact.NewFileStore(filepath.Join(runFolder, "Orchestration.md"))
}

// resolveClockTime returns the current timestamp for the COMPLETED marker
// write. When a Clock was injected it is used; otherwise a real UTC clock
// is used.
func (m *rootModel) resolveClockTime() time.Time {
	if m.clock != nil {
		return m.clock.Now()
	}
	return time.Now().UTC()
}

// ---------------------------------------------------------------------------
// Session starter
// ---------------------------------------------------------------------------

// startSession launches the session in a background goroutine and returns a tea.Cmd
// that delivers the result as a runDoneMsg or runErrorMsg.
func (m *rootModel) startSession() tea.Cmd {
	sel := m.selections
	sess := m.sess
	ctx := m.ctx

	return func() (msg tea.Msg) {
		defer func() {
			if p := recover(); p != nil {
				stack := debug.Stack()
				if len(stack) > 4096 {
					stack = stack[:4096]
				}
				msg = runErrorMsg{err: fmt.Errorf("panic in session: %v\n%s", p, stack)}
			}
		}()

		var seedInputs []string
		if sel.isNewRun && sel.seedInput != "" {
			seedInputs = []string{sel.seedInput}
		}
		config := domain.RunConfig{
			OrchestratorFilePath: sel.orchestratorFile,
			HarnessID:            sel.config.Harness,
			WorkflowID:           sel.workflowID,
			Task:                 sel.task,
			RunID:                sel.runID,
			RunFolder:            sel.runFolder,
			IsNewRun:             sel.isNewRun,
			AllowVersionDrift:    sel.config.AllowVersionDrift,
			RunSettings:          sel.config.Settings,
			Supplied:             sel.config.Supplied,
			SeedInputs:           seedInputs,
			ManualDispatch:       sel.manualDispatch,
		}
		config.InfraClassSelections = sel.config.InfraClassSelections
		outcome, err := sess.Start(ctx, config)
		if err != nil {
			return runErrorMsg{err: err}
		}
		return runDoneMsg{outcome: outcome}
	}
}
