package tui

// seed_identity_test.go verifies identity stability (Stage 2 / T2.2): minted
// or candidate run identity values (run ID, run folder) survive unchanged
// from selections through startSession() and config-screen reconstruction to
// domain.RunConfig at the session.Session.Start boundary.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
)

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
	factory := func(runFolder string, isNewRun bool, orchFile string, cfg runconfig.ConfigSelection) session.Session {
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
		newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
		newTestCandidate("20260702T120000Z-b4e8", "/ws/Orchestration-20260702T120000Z-b4e8"),
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
