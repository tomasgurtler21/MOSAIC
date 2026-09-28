package tui

// seed_runconfig_test.go verifies the RunConfig.SeedInputs mapping:
//   - Non-empty seedInput -> SeedInputs = []string{seedInput} when isNewRun
//   - Empty seedInput     -> SeedInputs = nil/empty
//   - Resume run          -> SeedInputs = nil/empty regardless of seedInput value

import (
	"context"
	"testing"

	tuicommon "mosaic-common/tui"
)

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
