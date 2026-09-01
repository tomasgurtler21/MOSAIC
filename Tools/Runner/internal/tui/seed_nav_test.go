package tui

// seed_nav_test.go verifies the navigation contract for the new seed-input screen
// and the RunConfig.SeedInputs mapping.
//
// Navigation contract:
//   - Task -> seed screen on forward navigation when isNewRun == true
//   - Esc from seed screen returns to task screen
//   - Seed screen -> config on Enter
//   - Esc from config first prompt returns to seed screen when isNewRun == true
//   - Seed screen is not shown at all when isNewRun == false (the resumed run's
//     forward and backward paths past it are covered in resume_skip_test.go)
//   - Re-entry of the seed screen works (Reset obligation)
//
// RunConfig.SeedInputs mapping:
//   - Non-empty seedInput -> SeedInputs = []string{seedInput} when isNewRun
//   - Empty seedInput     -> SeedInputs = nil/empty
//   - Resume run          -> SeedInputs = nil/empty regardless of seedInput value

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestModelNewRun creates a rootModel with isNewRun = true set in selections,
// so navigation tests can exercise the new-run path without driving the run-select
// screen.
func newTestModelNewRun() *rootModel {
	m := newTestModel()
	m.selections.isNewRun = true
	return m
}

// capturingSession is a session.Session double that stores the domain.RunConfig
// passed to Start so tests can assert on what RunConfig reaches the session boundary.
type capturingSession struct {
	outcome domain.RunOutcome
	err     error
	// configs is a buffered channel of size 1; Start sends the received config here.
	configs chan domain.RunConfig
}

func newCapturingSession() *capturingSession {
	return &capturingSession{
		outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"},
		configs: make(chan domain.RunConfig, 1),
	}
}

func (s *capturingSession) Start(_ context.Context, cfg domain.RunConfig) (domain.RunOutcome, error) {
	s.configs <- cfg
	return s.outcome, s.err
}

// ---------------------------------------------------------------------------
// T1.2 — Navigation: new-run path (isNewRun == true)
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_NewRun_TaskEnterGoesToSeedScreen verifies that
// confirming the task screen transitions to the seed-input screen when isNewRun is true.
func TestNavigation_SeedInputScreen_NewRun_TaskEnterGoesToSeedScreen(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupTask

	// Type one character to satisfy the non-empty task validator, then confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after task Enter (new run), want screenSetupSeedInput (%v)",
			m.screen, screenSetupSeedInput)
	}
}

// TestNavigation_SeedInputScreen_NewRun_EscFromSeedReturnsToTask verifies that
// pressing Esc on the seed-input screen returns to the task screen.
func TestNavigation_SeedInputScreen_NewRun_EscFromSeedReturnsToTask(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupTask {
		t.Errorf("screen = %v after Esc from seed screen, want screenSetupTask (%v)",
			m.screen, screenSetupTask)
	}
}

// TestNavigation_SeedInputScreen_NewRun_EnterFromSeedGoesToConfig verifies that
// confirming the seed-input screen (blank is a legal confirmation) transitions to config.
func TestNavigation_SeedInputScreen_NewRun_EnterFromSeedGoesToConfig(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	// Press Enter without typing — blank is a legal confirmation (field is optional).
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Errorf("screen = %v after seed Enter, want screenSetupConfig (%v)",
			m.screen, screenSetupConfig)
	}
}

// TestNavigation_ConfigScreen_NewRun_EscReturnsToSeedScreen verifies that pressing
// Esc on the first config prompt transitions back to the seed-input screen
// when isNewRun is true (not directly to the task screen).
func TestNavigation_ConfigScreen_NewRun_EscReturnsToSeedScreen(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupConfig

	sendKey(m, tea.KeyEsc)

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after Esc from config (new run), want screenSetupSeedInput (%v)",
			m.screen, screenSetupSeedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Navigation: resume-run path (isNewRun == false)
// ---------------------------------------------------------------------------

// The resume path no longer passes through the task screen in either direction:
// a resumed run is asked neither which workflow to run nor what the task is, so
// there is no task screen for it to advance from or step back to. Both
// directions are covered against the resume path's real shape in
// resume_skip_test.go -- TestSetupFlow_ResumedRun_ReachesConfigurationDirectly
// forward, TestSetupFlow_ResumedRun_BackFromConfiguration_ReturnsToTheHarnessQuestion
// backward. What survives here is the statement those tests do not make: the
// seed screen is not a step on the resume path either.

// TestNavigation_SeedInputScreen_Resume_IsNotShown verifies that a resumed run
// reaching configuration never stops at the seed-input screen.
//
// Seed inputs seed a run at its creation. A resumed run was seeded when it was
// created, and offering the screen again invites input that has nowhere to go.
func TestNavigation_SeedInputScreen_Resume_IsNotShown(t *testing.T) {
	m := newResumedRunModel(t)

	sendKey(m, tea.KeyEnter) // answer the harness question

	if m.screen == screenSetupSeedInput {
		t.Errorf("the setup sequence stopped on the seed-input screen for a resumed run; " +
			"seed inputs belong to a run's creation and cannot be supplied again on resume")
	}
	if m.selections.seedInput != "" {
		t.Errorf("selections.seedInput = %q for a resumed run, want empty",
			m.selections.seedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Re-entry: Reset obligation
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_ReEntry_TaskToSeedToTaskToSeed verifies that the
// seed screen can be entered a second time without auto-advancing due to a stale Done
// or Back flag from the previous visit.
//
// Drive: task -> seed (Enter on task) -> task (Esc on seed) -> seed (Enter on task again).
// After the second entry the model must still be on the seed screen, not auto-advanced
// to config because of a stale Done flag that was not cleared by Reset.
func TestNavigation_SeedInputScreen_ReEntry_TaskToSeedToTaskToSeed(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupTask

	// First visit: Enter on task -> seed.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupSeedInput {
		t.Fatalf("precondition: after first task Enter, screen = %v, want screenSetupSeedInput",
			m.screen)
	}

	// Esc from seed -> task.
	sendKey(m, tea.KeyEsc)
	if m.screen != screenSetupTask {
		t.Fatalf("precondition: after Esc from seed, screen = %v, want screenSetupTask", m.screen)
	}

	// Second visit: Enter on task again -> seed again.
	// Reset the taskScreen so it accepts fresh input.
	m.taskScreen.Reset()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after second task Enter, want screenSetupSeedInput (%v) — "+
			"seed screen must not auto-advance on re-entry; Reset obligation may be violated",
			m.screen, screenSetupSeedInput)
	}
}

// TestNavigation_SeedInputScreen_ReEntry_ConfigToSeedToConfig verifies that the
// seed screen, after being re-entered from the config's Esc path, does not
// immediately auto-return to task because of a stale Back flag.
//
// Drive: seed (Enter, blank) -> config -> seed (Esc from config).
// After the second entry the model must remain on the seed screen waiting for user input,
// not immediately transition back to task.
func TestNavigation_SeedInputScreen_ReEntry_ConfigToSeedToConfig(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	// Forward: Enter on seed -> config.
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: after seed Enter, screen = %v, want screenSetupConfig", m.screen)
	}

	// Esc from config -> back to seed.
	sendKey(m, tea.KeyEsc)

	// The model must be on the seed screen, not auto-returned to task
	// because of a stale Back flag that was not cleared by Reset.
	if m.screen != screenSetupSeedInput {
		t.Errorf("screen = %v after Esc from config, want screenSetupSeedInput (%v) — "+
			"seed screen must not auto-return on re-entry; Reset obligation may be violated",
			m.screen, screenSetupSeedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — selections.seedInput is captured on seed screen confirmation
// ---------------------------------------------------------------------------

// TestNavigation_SeedInputScreen_EnteredPathCapturedInSelections verifies that the
// value typed on the seed screen is stored in m.selections.seedInput when confirmed,
// so that startSession() can map it into domain.RunConfig.SeedInputs.
func TestNavigation_SeedInputScreen_EnteredPathCapturedInSelections(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/path/to/seed")})
	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: screen = %v after seed Enter, want screenSetupConfig", m.screen)
	}
	if m.selections.seedInput != "/path/to/seed" {
		t.Errorf("selections.seedInput = %q, want %q", m.selections.seedInput, "/path/to/seed")
	}
}

// TestNavigation_SeedInputScreen_BlankCapturedAsEmpty verifies that a blank
// confirmation stores an empty string in selections.seedInput.
func TestNavigation_SeedInputScreen_BlankCapturedAsEmpty(t *testing.T) {
	m := newTestModelNewRun()
	m.screen = screenSetupSeedInput

	sendKey(m, tea.KeyEnter)

	if m.screen != screenSetupConfig {
		t.Fatalf("precondition: screen = %v after blank seed Enter, want screenSetupConfig", m.screen)
	}
	if m.selections.seedInput != "" {
		t.Errorf("selections.seedInput = %q after blank confirmation, want empty string",
			m.selections.seedInput)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Full new-run forward navigation sequence
// ---------------------------------------------------------------------------

// TestSetupSequence_NewRun_ForwardNavigation_ReachesProgressScreen verifies the
// complete forward-navigation path for a new run:
// workflow -> task -> seed input -> config -> progress.
// This is the new-run counterpart of TestSetupSequence_ForwardNavigation_ReachesProgressScreen
// (which tests the resumed-run path where the seed screen is skipped).
func TestSetupSequence_NewRun_ForwardNavigation_ReachesProgressScreen(t *testing.T) {
	m := newTestModelNewRun()
	style := stylesFromTheme(m.theme)

	// Bypass file selection: directly populate workflow regions and jump to the workflow
	// screen, replicating what updateSetupFile does after loading.
	testWorkflows := []domain.WorkflowRegion{
		{Info: domain.WorkflowInfo{ID: "test-workflow"}},
	}
	m.workflows = testWorkflows
	m.workflowScreen = screens.NewWorkflowSelectScreen(testWorkflows, m.width, m.height, style)
	m.screen = screenSetupWorkflow

	// Select the only workflow.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupTask {
		t.Fatalf("after workflow selection: screen = %v, want screenSetupTask", m.screen)
	}

	// Type one character to satisfy the non-empty task validator, then confirm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSetupSeedInput {
		t.Fatalf("after task entry (new run): screen = %v, want screenSetupSeedInput", m.screen)
	}

	// Accept the seed screen with a blank entry (field is optional).
	sendKey(m, tea.KeyEnter)
	if m.screen != screenSetupConfig {
		t.Fatalf("after seed entry: screen = %v, want screenSetupConfig", m.screen)
	}

	// Accept all configuration prompts. Mode is now the first step and requires an
	// explicit cursor movement before Enter (no preselection per AC8.3). The timeout
	// step is always present now that the fake harness has been removed. The always-shown
	// manual-resolution step follows checkpoints.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option (orchestrated)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → must advance to timeout

	// Verify we are at the timeout step (not version drift) before supplying the value.
	// This assertion fails until the fake-harness option is removed and advance() always
	// proceeds to configStepHarnessTimeout — mirroring the check in
	// TestSetupSequence_ForwardNavigation_ReachesProgressScreen.
	configView := m.configScreen.View()
	if !containsAny(configView, "timeout", "Timeout", "Invocation", "invocation") {
		t.Fatalf("after harness Enter, expected invocation-timeout step; "+
			"fake harness may still be skipping timeout:\n%s", configView)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual-resolution (always shown; accept default disabled)

	if m.screen != screenProgress {
		t.Errorf("after config completion (new run): screen = %v, want screenProgress", m.screen)
	}
	if m.progressScreen == nil {
		t.Error("progressScreen = nil after reaching progress screen; must be constructed")
	}
	if m.selections.task != "T" {
		t.Errorf("selections.task = %q, want %q", m.selections.task, "T")
	}
}

// ---------------------------------------------------------------------------
// T1.3 — RunConfig.SeedInputs mapping
// ---------------------------------------------------------------------------

// TestSeedRunConfig_NonEmptySeedInput_ReachesRunConfigSeedInputs verifies that a
// non-empty value in selections.seedInput reaches domain.RunConfig.SeedInputs as a
// single-element slice at the session.Session.Start boundary when isNewRun is true.
func TestSeedRunConfig_NonEmptySeedInput_ReachesRunConfigSeedInputs(t *testing.T) {
	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	// Bypass UI navigation: set selections directly as if the new-run flow completed.
	m.selections.isNewRun = true
	m.selections.seedInput = "/path/to/seed"
	m.selections.task = "test task"

	// Invoke startSession() and execute the returned tea.Cmd synchronously.
	// capturingSession.Start() sends to a buffered channel and returns immediately,
	// so the config is available in the channel before cmd() returns.
	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if len(cfg.SeedInputs) != 1 {
			t.Errorf("RunConfig.SeedInputs = %v (len %d), want exactly one entry [%q]",
				cfg.SeedInputs, len(cfg.SeedInputs), "/path/to/seed")
			return
		}
		if cfg.SeedInputs[0] != "/path/to/seed" {
			t.Errorf("RunConfig.SeedInputs[0] = %q, want %q",
				cfg.SeedInputs[0], "/path/to/seed")
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}

// TestSeedRunConfig_BlankSeedInput_LeavesSeedInputsEmpty verifies that an empty
// seedInput (blank confirmation on the seed screen) results in a nil or empty
// SeedInputs slice at the session.Session.Start boundary — blank means no seeding.
func TestSeedRunConfig_BlankSeedInput_LeavesSeedInputsEmpty(t *testing.T) {
	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	m.selections.isNewRun = true
	m.selections.seedInput = "" // blank confirmation
	m.selections.task = "test task"

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if len(cfg.SeedInputs) != 0 {
			t.Errorf("RunConfig.SeedInputs = %v, want nil/empty (blank seed confirmation = no seeding)",
				cfg.SeedInputs)
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}

// TestSeedRunConfig_ResumeRun_SeedInputsAlwaysEmpty verifies that a resumed run
// (isNewRun == false) never propagates SeedInputs. The TUI prevents seedInput from
// being set on a resume by skipping the seed screen, but this test guards the
// RunConfig construction rule independently of the navigation gate.
func TestSeedRunConfig_ResumeRun_SeedInputsAlwaysEmpty(t *testing.T) {
	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	m.selections.isNewRun = false
	m.selections.seedInput = "/some/path" // would never be set by TUI on resume, but guard the mapping
	m.selections.task = "test task"

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if len(cfg.SeedInputs) != 0 {
			t.Errorf("RunConfig.SeedInputs = %v, want nil/empty (resume run must never seed)",
				cfg.SeedInputs)
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}

// ---------------------------------------------------------------------------
// Stage 2 / T2.2 — Identity stability: minted values survive to domain.RunConfig
// ---------------------------------------------------------------------------

// TestRunIdentity_NewRun_MintedSelections_ReachRunConfig verifies that when
// selections.runID and selections.runFolder are populated (as they will be after
// the minter is called by updateRunSelect), those exact values appear unchanged in
// domain.RunConfig at the session.Session.Start boundary.
//
// This test pre-sets selections directly, isolating the mapping logic in startSession()
// from the run-select minting behaviour tested in T2.1.
func TestRunIdentity_NewRun_MintedSelections_ReachRunConfig(t *testing.T) {
	const wantRunID = "20260801T120000Z-ab12"
	const wantRunFolder = "/test/ws/Orchestration-20260801T120000Z-ab12"

	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	// Pre-populate selections as they will be after the minter runs in updateRunSelect.
	m.selections.isNewRun = true
	m.selections.runID = wantRunID
	m.selections.runFolder = wantRunFolder
	m.selections.task = "test task"

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if cfg.RunID != wantRunID {
			t.Errorf("RunConfig.RunID = %q, want %q", cfg.RunID, wantRunID)
		}
		if cfg.RunFolder != wantRunFolder {
			t.Errorf("RunConfig.RunFolder = %q, want %q", cfg.RunFolder, wantRunFolder)
		}
		if !cfg.IsNewRun {
			t.Error("RunConfig.IsNewRun = false, want true for new run")
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}

// TestRunIdentity_NewRun_SelectionsUnchangedByConfigReconstruction verifies that when
// the config screen completes and updateSetupConfig reconstructs the session, the runID
// and runFolder in selections survive unchanged — the config reconstruction must not
// overwrite or clear the minted identity.
//
// It also asserts that the session factory called by updateSetupConfig receives the
// pre-populated run folder (not an empty string).
func TestRunIdentity_NewRun_SelectionsUnchangedByConfigReconstruction(t *testing.T) {
	const wantRunID = "20260801T120000Z-ab12"
	const wantRunFolder = "/test/ws/Orchestration-20260801T120000Z-ab12"

	capSess := newCapturingSession()
	var configFactoryRunFolder string
	factory := func(runFolder string, isNewRun bool, orchFile string, cfg screens.ConfigSelection) session.Session {
		configFactoryRunFolder = runFolder
		return capSess
	}

	m := newRootModel(context.Background(), capSess, Options{
		Theme:          tuicommon.DefaultTheme(),
		SessionFactory: factory,
	})

	// Pre-populate selections as they will be after the minter runs in updateRunSelect.
	m.selections.isNewRun = true
	m.selections.runID = wantRunID
	m.selections.runFolder = wantRunFolder
	m.screen = screenSetupConfig

	// Drive the config screen to completion: mode (Down+Enter), harness, timeout,
	// version drift, checkpoints, manual-resolution (always shown). Mode is now the
	// first step and requires an explicit cursor movement before Enter (no preselection).
	m.Update(tea.KeyMsg{Type: tea.KeyDown})  // move cursor to first mode option (orchestrated)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm mode
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // harness → timeout
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // timeout → version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // version drift
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // checkpoints
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // manual-resolution (always shown; accept default disabled)

	if m.screen != screenProgress {
		t.Fatalf("precondition: screen = %v after config, want screenProgress", m.screen)
	}

	// Config reconstruction must not overwrite the minted identity.
	if m.selections.runID != wantRunID {
		t.Errorf("selections.runID = %q after config reconstruction, want %q; identity must be stable",
			m.selections.runID, wantRunID)
	}
	if m.selections.runFolder != wantRunFolder {
		t.Errorf("selections.runFolder = %q after config reconstruction, want %q; identity must be stable",
			m.selections.runFolder, wantRunFolder)
	}

	// The session factory called by updateSetupConfig must receive the minted folder.
	if configFactoryRunFolder != wantRunFolder {
		t.Errorf("config session factory received runFolder = %q, want %q; "+
			"factory must be passed the minted folder, not an overwritten or empty value",
			configFactoryRunFolder, wantRunFolder)
	}
}

// TestRunIdentity_NewRun_FromRunSelect_MintedValuesReachRunConfig is the end-to-end
// identity stability test. It drives the run-select "new run" choice with an injected
// minter and verifies that the minted run ID and folder appear in domain.RunConfig at
// the session.Session.Start boundary.
//
// In RED: updateRunSelect does not call MintRunIdentity, so selections.runID and
// selections.runFolder remain empty (""). The RunConfig.RunID assertion fails.
// In GREEN: the minter is called and the minted values flow through to RunConfig.
func TestRunIdentity_NewRun_FromRunSelect_MintedValuesReachRunConfig(t *testing.T) {
	candidates := []runscan.RunCandidate{
		makeCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		makeCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
	}
	calls := make([]factoryCall, 0)
	capSess := newCapturingSession()
	m := newModelWithScanMinterFactory(candidates, fixedMinter(), &calls, capSess)
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}

	// Drive "Start a new run" (the first item on the run-select screen).
	sendKey(m, tea.KeyEnter)

	// Provide the minimum required selections so startSession() can fire.
	m.selections.task = "test task"

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd; want a non-nil command")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if cfg.RunID != fixedMintedRunID {
			t.Errorf("RunConfig.RunID = %q, want minted %q; "+
				"minter must be called by updateRunSelect when MintRunIdentity is non-nil",
				cfg.RunID, fixedMintedRunID)
		}
		if cfg.RunFolder != fixedMintedRunFolder {
			t.Errorf("RunConfig.RunFolder = %q, want minted %q",
				cfg.RunFolder, fixedMintedRunFolder)
		}
		if !cfg.IsNewRun {
			t.Error("RunConfig.IsNewRun = false, want true for new run selected at run-select screen")
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}

// TestRunIdentity_Resume_CandidateValuesReachRunConfig verifies that when a candidate
// was selected on the run-select screen (the resume path), the candidate's run ID and
// folder appear unchanged in domain.RunConfig at the session.Session.Start boundary.
// This guards the resume path against any future accidental overwrite.
func TestRunIdentity_Resume_CandidateValuesReachRunConfig(t *testing.T) {
	const candidateRunID = "20260701T120000Z-a3f9"
	const candidateFolder = "/ws/Orchestration-20260701T120000Z-a3f9"

	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{
		Theme: tuicommon.DefaultTheme(),
	})

	// Set selections as they would be after updateRunSelect completed the candidate-selection branch.
	m.selections.isNewRun = false
	m.selections.runID = candidateRunID
	m.selections.runFolder = candidateFolder
	m.selections.task = "test task"

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		if cfg.RunID != candidateRunID {
			t.Errorf("RunConfig.RunID = %q, want candidate %q", cfg.RunID, candidateRunID)
		}
		if cfg.RunFolder != candidateFolder {
			t.Errorf("RunConfig.RunFolder = %q, want candidate %q", cfg.RunFolder, candidateFolder)
		}
		if cfg.IsNewRun {
			t.Error("RunConfig.IsNewRun = true, want false for resume run")
		}
	default:
		t.Error("sess.Start was not called; domain.RunConfig was not captured by the session double")
	}
}
