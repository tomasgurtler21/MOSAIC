package tui

import (
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
	"mosaic-run/internal/tui/screens/runflow"
)

// historyRows maps Execution Log entries, in log order, to progress rows.
func historyRows(log []domain.ExecutionLogEntry) []runflow.ProgressRow {
	rows := make([]runflow.ProgressRow, 0, len(log))
	for _, e := range log {
		rows = append(rows, runflow.ProgressRow{
			AgentInstance: e.Agent,
			Phase:         e.Phase,
			Stage:         e.Stage,
			Status:        string(e.Status),
		})
	}
	return rows
}

// loadHistory reads the run's artifact and returns historyRows of its
// Execution Log; any read error yields nil.
func (m *rootModel) loadHistory() []runflow.ProgressRow {
	if m.selections.runFolder == "" {
		return nil
	}
	state, err := m.resolveArtifactStore(m.selections.runFolder).Read(m.ctx)
	if err != nil {
		return nil
	}
	return historyRows(state.ExecutionLog)
}

// newProgressScreenWithHistory returns a new progress screen; for a resumed
// run (selections.isNewRun false) its rows are set to loadHistory().
func (m *rootModel) newProgressScreenWithHistory(style screens.Styles) *runflow.ProgressScreen {
	screen := runflow.NewProgressScreen(m.width, m.height, style)
	if !m.selections.isNewRun {
		screen.SetHistory(m.loadHistory())
	}
	return screen
}
