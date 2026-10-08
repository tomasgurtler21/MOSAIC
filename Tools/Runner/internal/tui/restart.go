package tui

import (
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens/runflow"
)

// restartAsResume is the single restart path used by the done-screen
// Continue, the stop-recovery screen and the exec-override retry. Callers set
// their own selections first (manualDispatch, config.ExecutablePath) and
// return its command.
//
// A run whose artifact exists is resumed: the new-run flag is cleared so the
// session never refuses to "create a new run" over it and the seed inputs are
// not applied again. A run that stopped before its artifact was written keeps
// starting as new. When rebuildSession is true the session is rebuilt through
// the factory (if one is set); otherwise the existing session is reused. The
// progress screen is kept and its rows are replaced by the run history.
func (m *rootModel) restartAsResume(rebuildSession bool) tea.Cmd {
	m.resetStopStateForRestart()
	if m.selections.isNewRun && m.runArtifactExists() {
		m.selections.isNewRun = false
	}
	if rebuildSession && m.sessionFactory != nil {
		m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
	}
	if m.progressScreen == nil {
		m.progressScreen = runflow.NewProgressScreen(m.width, m.height, stylesFromTheme(m.theme))
	}
	m.progressScreen.SetHistory(m.loadHistory())
	m.screen = screenProgress
	return tea.Batch(m.progressScreen.Init(), m.startSession())
}

// runArtifactExists reports whether the run folder holds an artifact: the
// store read returns nil, or an error that is not os.ErrNotExist. An empty
// runFolder reports false.
func (m *rootModel) runArtifactExists() bool {
	if m.selections.runFolder == "" {
		return false
	}
	_, err := m.resolveArtifactStore(m.selections.runFolder).Read(m.ctx)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
