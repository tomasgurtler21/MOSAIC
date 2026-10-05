package tui

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/tui/screens/runconfig"
	"mosaic-run/internal/tui/screens/devtest"
	"mosaic-run/internal/tui/screens/runflow"
	"mosaic-run/internal/tui/screens/setup"
)

// ---------------------------------------------------------------------------
// Run select screen handler
// ---------------------------------------------------------------------------

func (m *rootModel) updateRunSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.runSelectScreen == nil {
		m.screen = screenSetupHarness
		return m, nil
	}
	m.runSelectScreen.Update(msg)
	if m.runSelectScreen.Back() {
		m.runSelectScreen.Reset()
		return m, tea.Quit
	}
	if m.runSelectScreen.Done() {
		if m.runSelectScreen.IsNewRun() {
			m.selections.isNewRun = true
			if m.mintRunIdentity != nil {
				runID, runFolder := m.mintRunIdentity()
				m.selections.runID = runID
				m.selections.runFolder = runFolder
			} else {
				m.selections.runID = ""
				m.selections.runFolder = ""
			}
			if m.onRunIDResolved != nil && m.selections.runID != "" {
				m.onRunIDResolved(m.selections.runID)
			}
		} else if m.runSelectQuestion != nil {
			choiceID := m.runSelectScreen.SelectedChoiceID()
			// Resolve the chosen ID back to a full Identity via the same
			// runselect.Answer function the CLI would use for an explicit
			// --run value -- the screen never holds a second copy of the
			// selection rules. mint is only reachable for NewRunChoiceID,
			// already handled above, so a nil-safe fallback is passed here.
			mint := m.mintRunIdentity
			if mint == nil {
				mint = func() (string, string) { return "", "" }
			}
			id, err := runselect.Answer(*m.runSelectQuestion, choiceID, runselect.Minter(mint))
			if err != nil {
				// The screen should never offer a choice Answer cannot
				// resolve, so this is a defect rather than a user mistake --
				// and it must be shown rather than swallowed. Falling through
				// would leave the run, its folder and its workflow all empty
				// while isNewRun stayed false, which the setup flow reads as a
				// resumed run and walks past every remaining question with
				// nothing selected.
				style := stylesFromTheme(m.theme)
				m.doneScreen = runflow.NewDoneScreen(
					domain.RunOutcome{Status: domain.RunRefused, Message: err.Error()},
					"", m.width, m.height, style,
				)
				m.screen = screenDone
				m.runSelectScreen.Reset()
				return m, nil
			}
			m.selections.runID = id.RunID
			m.selections.runFolder = id.RunFolder
			m.selections.isNewRun = false
			// The chosen run brings its workflow with it. Adopting it here
			// is what makes skipping the workflow question safe: the value
			// is settled the moment the run is, not dropped and asked for
			// again. A run that recorded none carries none, and the
			// session layer refuses it rather than setup substituting one.
			m.selections.workflowID = domain.WorkflowID(id.Workflow)
			// Reconstruct the session with the correct run-scoped store if a factory is available.
			// Harness config is not yet known (config screen has not run); defaults to fake adapter.
			if m.sessionFactory != nil {
				m.sess = m.sessionFactory(id.RunFolder, false, "", runconfig.ConfigSelection{})
			}
			if m.onRunIDResolved != nil && m.selections.runID != "" {
				m.onRunIDResolved(m.selections.runID)
			}
		}
		// When "new run" is selected, the session factory is called with the minted
		// run folder (or empty string when no minter is provided, for backward compat).
		if m.selections.isNewRun && m.sessionFactory != nil {
			m.sess = m.sessionFactory(m.selections.runFolder, true, "", runconfig.ConfigSelection{})
		}
		m.runSelectScreen.Reset()
		m.screen = screenSetupHarness
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Setup screen handlers
// ---------------------------------------------------------------------------

func (m *rootModel) updateSetupHarness(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.harnessScreen == nil {
		m.screen = screenSetupWorkflow
		return m, nil
	}
	m.harnessScreen.Update(msg)
	if m.harnessScreen.Back() {
		m.harnessScreen.Reset()
		return m, tea.Quit
	}
	if m.harnessScreen.Done() {
		harnessID := m.harnessScreen.SelectedID()
		m.harnessScreen.Reset()

		// When DevMode is true and the user selected "Run Tests", enter the
		// test flow instead of the normal run setup sequence.
		if m.devMode && harnessID == setup.RunTestsChoiceID {
			style := stylesFromTheme(m.theme)
			m.testCatalogScreen = devtest.NewTestCatalogScreen(m.width, m.height, style)
			m.screen = screenTestCatalog
			return m, m.testCatalogScreen.InputInit()
		}

		// Auto-discover the orchestrator file from the harness's agents directory.
		// The discoverer is injected via Options so that the TUI package does not
		// import the concrete harness package (import boundary constraint).
		orchPath := ""
		if m.orchestratorDiscoverer != nil {
			workDir, wdErr := os.Getwd()
			if wdErr != nil {
				style := stylesFromTheme(m.theme)
				m.doneScreen = runflow.NewDoneScreen(
					domain.RunOutcome{Status: domain.RunRefused, Message: wdErr.Error()},
					"", m.width, m.height, style,
				)
				m.screen = screenDone
				return m, nil
			}
			discovered, orchErr := m.orchestratorDiscoverer(workDir, harnessID)
			if orchErr != nil {
				style := stylesFromTheme(m.theme)
				m.doneScreen = runflow.NewDoneScreen(
					domain.RunOutcome{Status: domain.RunRefused, Message: orchErr.Error()},
					"", m.width, m.height, style,
				)
				m.screen = screenDone
				return m, nil
			}
			orchPath = discovered
			m.selections.orchestratorFile = orchPath
		}

		// Enumerate workflow regions from the discovered file (when available).
		// When no discoverer is injected (test/backward-compat), the workflow list
		// is empty and the workflow screen renders an empty list.
		var regions []domain.WorkflowRegion
		if orchPath != "" {
			var err error
			regions, err = orchfile.EnumerateWorkflows(orchPath)
			if err != nil {
				style := stylesFromTheme(m.theme)
				m.doneScreen = runflow.NewDoneScreen(
					domain.RunOutcome{Status: domain.RunRefused, Message: err.Error()},
					"", m.width, m.height, style,
				)
				m.screen = screenDone
				return m, nil
			}

			// Enumerate infrastructure agents so the config screen can prompt when
			// multiple agents of the same gated class are declared.
			infraAgents, err := orchfile.EnumerateInfrastructureAgents(orchPath)
			if err != nil {
				infraAgents = nil
			}
			m.configScreen.SetDeclaredAgents(infraAgents)
		}
		m.workflows = regions

		// Tell the config screen the harness is already selected so it skips
		// the harness step in its own wizard.
		m.configScreen.SetPreselectedHarness(harnessID)
		// Propagate into selections so startSession() can read it.
		m.selections.config.Harness = harnessID

		style := stylesFromTheme(m.theme)
		m.workflowScreen = setup.NewWorkflowSelectScreen(regions, m.width, m.height, style)

		// A resumed run is asked neither which workflow to run nor what its
		// task is. Both were settled when the run was created and both are
		// recorded in its artifact, so asking again only invites an answer that
		// contradicts the run being resumed. The harness question is the last
		// one such a run is asked; from here it goes straight to configuration,
		// carrying whatever workflow it recorded -- including none, which the
		// session layer refuses rather than setup papering over.
		if !m.selections.isNewRun {
			m.prepareConfigScreen()
			m.screen = screenSetupConfig
			return m, nil
		}

		m.screen = screenSetupWorkflow
		return m, nil
	}
	return m, nil
}

// prepareConfigScreen tells the configuration screen which run mode it is in
// and how the version the selected workflow declares compares with the one the
// run recorded, so it can decide whether to ask about version drift.
//
// Both facts become knowable only once the workflow is settled, and that
// happens at a different point on each path: at the user's answer for a new
// run, and at the harness question for a resumed one, which never reaches the
// workflow screen at all.
func (m *rootModel) prepareConfigScreen() {
	m.configScreen.SetIsNewRun(m.selections.isNewRun)
	m.configScreen.SetNeedsRunnerAdoption(m.needsRunnerAdoption())
	m.configScreen.SetCommitSetupPending(m.commitSetupPending())
	m.configScreen.SetVersionDriftInfo(
		string(m.recordedWorkflowVersion()),
		string(m.selectedWorkflowVersion()),
	)
}

func (m *rootModel) updateSetupFile(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.fileScreen.Update(msg)
	if m.fileScreen.Back() {
		m.fileScreen.Reset()
		return m, tea.Quit
	}
	if m.fileScreen.Done() {
		path := m.fileScreen.FilePath()
		m.selections.orchestratorFile = path
		m.fileScreen.Reset()

		// Enumerate workflow regions from the file.
		regions, err := orchfile.EnumerateWorkflows(path)
		if err != nil {
			// Show error on the file screen by re-entering with an error overlay.
			// For simplicity, just clear and let the user retry with a new path.
			style := stylesFromTheme(m.theme)
			m.doneScreen = runflow.NewDoneScreen(
				domain.RunOutcome{Status: domain.RunRefused, Message: err.Error()},
				"",
				m.width, m.height, style,
			)
			m.screen = screenDone
			return m, nil
		}
		m.workflows = regions

		// Enumerate infrastructure agents so the config screen can prompt the
		// user to select one when multiple agents of the same gated class are
		// declared. A parse error is treated as zero declared agents; the
		// session layer will enforce a refusal at run start if needed.
		infraAgents, err := orchfile.EnumerateInfrastructureAgents(path)
		if err != nil {
			infraAgents = nil
		}
		m.configScreen.SetDeclaredAgents(infraAgents)

		style := stylesFromTheme(m.theme)
		m.workflowScreen = setup.NewWorkflowSelectScreen(regions, m.width, m.height, style)
		m.screen = screenSetupWorkflow
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupWorkflow(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.workflowScreen.Update(msg)
	if m.workflowScreen.Back() {
		m.workflowScreen.Reset()
		m.screen = screenSetupHarness
		return m, nil
	}
	if m.workflowScreen.Done() {
		selectedID := m.workflowScreen.SelectedID()
		m.selections.workflowID = domain.WorkflowID(selectedID)

		// The version-drift question is only meaningful once both versions are
		// known, and the selected workflow's version is only known here. A new
		// run has no recorded version, so it is never asked.
		m.prepareConfigScreen()

		m.workflowScreen.Reset()
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	return m, nil
}

func (m *rootModel) updateSetupTask(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.taskScreen.Update(msg)
	if m.taskScreen.Back() {
		m.taskScreen.Reset()
		m.screen = screenSetupWorkflow
		return m, nil
	}
	if m.taskScreen.Done() {
		m.selections.task = m.taskScreen.Task()
		m.taskScreen.Reset()
		if m.selections.isNewRun {
			m.screen = screenSetupSeedInput
			return m, m.seedInputScreen.InputInit()
		}
		m.screen = screenSetupConfig
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupSeedInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.seedInputScreen.Update(msg)
	if m.seedInputScreen.Back() {
		m.seedInputScreen.Reset()
		m.screen = screenSetupTask
		return m, m.taskScreen.InputInit()
	}
	if m.seedInputScreen.Done() {
		m.selections.seedInput = m.seedInputScreen.SeedInput()
		m.seedInputScreen.Reset()
		m.screen = screenSetupConfig
		return m, nil
	}
	return m, cmd
}

func (m *rootModel) updateSetupConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.configScreen.Update(msg)
	if m.configScreen.Back() {
		m.configScreen.Reset()
		if m.selections.isNewRun {
			m.screen = screenSetupSeedInput
			return m, m.seedInputScreen.InputInit()
		}
		// Back returns the user to the last question they were actually asked.
		// A resumed run was never asked for its workflow or its task, so that
		// question is the harness one; routing back to a skipped screen would
		// put the very question the forward path exists to suppress.
		m.screen = screenSetupHarness
		return m, nil
	}
	if m.configScreen.Done() {
		m.selections.config = m.configScreen.Selection()
		m.configScreen.Reset()

		// When the selected harness is GHCP CLI, show the permission-mode
		// selection screen before spawning any process. For all other harnesses,
		// proceed directly to the progress screen.
		if m.selections.config.Harness == "ghcp-cli" {
			m.ghcpModeScreen.Reset()
			m.screen = screenSetupGHCPMode
			return m, nil
		}

		return m, m.launchSession()
	}
	return m, nil
}

// launchSession wires the session factory with the current config selection and
// transitions to the progress screen, starting the session in a background goroutine.
// This is the common terminal step from both updateSetupConfig (non-GHCP-CLI harnesses)
// and updateSetupGHCPMode (GHCP CLI harness, after mode is resolved).
func (m *rootModel) launchSession() tea.Cmd {
	// Reconstruct the session with the harness adapter and resolved config.
	// This replaces the placeholder session (which used the fake adapter) with
	// one using the real adapter and all resolved settings.
	if m.sessionFactory != nil {
		m.sess = m.sessionFactory(m.selections.runFolder, m.selections.isNewRun, m.selections.orchestratorFile, m.selections.config)
	}

	// Transition to progress screen and start the session. The chosen run
	// is stated as the initial status line before dispatch, mirroring the
	// CLI's stdout announcement.
	style := stylesFromTheme(m.theme)
	m.progressScreen = runflow.NewProgressScreen(m.width, m.height, style)
	m.progressScreen.SetStatus(runselect.Announce(m.announceIdentity()), false)
	m.screen = screenProgress
	return tea.Batch(m.progressScreen.Init(), m.startSession())
}

func (m *rootModel) updateSetupGHCPMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.ghcpModeScreen == nil {
		// Screen not available; default to blanket and proceed.
		m.selections.config.GHCPCLIMode = string(runconfig.GHCPCLIModeBlanket)
		return m, m.launchSession()
	}
	m.ghcpModeScreen.Update(msg)
	if m.ghcpModeScreen.Back() {
		m.ghcpModeScreen.Reset()
		m.screen = screenSetupConfig
		return m, nil
	}
	if m.ghcpModeScreen.Done() {
		m.selections.config.GHCPCLIMode = string(m.ghcpModeScreen.Mode())
		m.ghcpModeScreen.Reset()
		return m, m.launchSession()
	}
	return m, nil
}

// recordedWorkflowVersion returns the workflow version recorded in the run's
// artifact frontmatter, or the empty version when there is nothing to read: a
// new run has no prior artifact, and a resumed run whose artifact is missing,
// unreadable, or unparseable has no recorded version to compare against.
func (m *rootModel) recordedWorkflowVersion() domain.WorkflowVersion {
	if m.selections.isNewRun || m.selections.runFolder == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(m.selections.runFolder, "Orchestration.md"))
	if err != nil {
		return ""
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return ""
	}
	return state.WorkflowVersion
}

// selectedWorkflowVersion returns the version declared by the workflow region
// the user selected, or the empty version when the orchestrator file declares
// none for it.
func (m *rootModel) selectedWorkflowVersion() domain.WorkflowVersion {
	for _, wf := range m.workflows {
		if wf.Info.ID == m.selections.workflowID {
			return wf.Info.Version
		}
	}
	return ""
}

// announceIdentity builds the runselect.Identity used to render the
// chosen-run announcement (AC2.7) from the currently selected run. For a
// resumed run it attempts to read the recorded position from the run's
// artifact; a read or parse failure simply leaves Position nil, which
// runselect.Announce handles without panicking.
func (m *rootModel) announceIdentity() runselect.Identity {
	id := runselect.Identity{
		RunID:     m.selections.runID,
		RunFolder: m.selections.runFolder,
		IsNewRun:  m.selections.isNewRun,
	}
	if id.IsNewRun || id.RunFolder == "" {
		return id
	}
	data, err := os.ReadFile(filepath.Join(id.RunFolder, "Orchestration.md"))
	if err != nil {
		return id
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return id
	}
	id.Position = &runselect.Position{
		Phase:       state.CurrentState.Phase,
		Stage:       state.CurrentState.Stage,
		LastAgent:   state.CurrentState.LastAgent,
		LastUpdated: state.LastUpdated,
	}
	return id
}

// needsRunnerAdoption reports whether the resumed run's artifact records no
// runner settings (a native-created artifact), so the configuration wizard has
// to ask for them once. A new run, and an artifact that cannot be read or
// parsed, never needs adoption here; the session refuses the latter.
func (m *rootModel) needsRunnerAdoption() bool {
	if m.selections.isNewRun || m.selections.runFolder == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(m.selections.runFolder, "Orchestration.md"))
	if err != nil {
		return false
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return false
	}
	return state.Mode == domain.ExecutionModeUnset
}

// commitSetupPending reports whether the resumed run enables commits but
// records no commit branch, so the wizard must ask for the branch variant
// explicitly. A new run, and an artifact that cannot be read or parsed, never
// has a pending setup here; the session refuses the latter.
func (m *rootModel) commitSetupPending() bool {
	if m.selections.isNewRun || m.selections.runFolder == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(m.selections.runFolder, "Orchestration.md"))
	if err != nil {
		return false
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return false
	}
	return state.Commits && state.CommitBranch == ""
}
