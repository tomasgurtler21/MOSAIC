package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
	"mosaic-run/internal/tui/screens/runflow"
)

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

	case questionSelectMany:
		m.activeQuestion = &qMsg
		m.multiOverlay = newInlineMultiSelect(qMsg.choiceQ, m.theme, m.width, m.height)
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
	if m.multiOverlay != nil {
		if m.multiOverlay.update(msg) {
			ans := m.multiOverlay.answer()
			m.replyToPendingQuestion(answerMsg{multiChoiceAns: ans})
			m.multiOverlay = nil
			m.screen = screenProgress
		}
		return m, nil
	}
	if m.textOverlay != nil {
		cmd := m.textOverlay.update(msg)
		if m.textOverlay.finished() {
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
