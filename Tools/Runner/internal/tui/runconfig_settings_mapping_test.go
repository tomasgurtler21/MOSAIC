package tui

// The values the configuration wizard collected reach the session exactly as
// collected: the review loop limit, and which values the user answered
// explicitly (so a resume compares only those with the artifact).

import (
	"context"
	"testing"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens/runconfig"
)

// startWith runs startSession for a model whose wizard collected sel, and
// returns the RunConfig the session received.
func startWith(t *testing.T, isNewRun bool, sel runconfig.ConfigSelection) domain.RunConfig {
	t.Helper()
	capSess := newCapturingSession()
	m := newRootModel(context.Background(), capSess, Options{Theme: tuicommon.DefaultTheme()})
	m.selections.isNewRun = isNewRun
	m.selections.task = "test task"
	m.selections.config = sel

	cmd := m.startSession()
	if cmd == nil {
		t.Fatal("startSession() returned nil cmd")
	}
	cmd()

	select {
	case cfg := <-capSess.configs:
		return cfg
	default:
		t.Fatal("sess.Start was not called")
		return domain.RunConfig{}
	}
}

func TestStartSession_NewRun_ReviewLoopLimitReachesRunConfig(t *testing.T) {
	tests := []struct {
		name  string
		limit int
	}{
		{"positive limit", 5},
		{"no limit", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := startWith(t, true, runconfig.ConfigSelection{
				Settings: domain.RunSettings{Mode: domain.ExecutionModeAuto, ReviewLoopLimit: tc.limit},
				Supplied: domain.SuppliedSettings{ReviewLoopLimit: true},
			})

			if cfg.ReviewLoopLimit != tc.limit {
				t.Errorf("RunConfig.ReviewLoopLimit = %d, want %d", cfg.ReviewLoopLimit, tc.limit)
			}
			if !cfg.Supplied.ReviewLoopLimit {
				t.Error("RunConfig.Supplied.ReviewLoopLimit = false, want true")
			}
		})
	}
}

func TestStartSession_Resume_SuppliedFlagsReachRunConfig(t *testing.T) {
	want := domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}

	cfg := startWith(t, false, runconfig.ConfigSelection{
		Settings: domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true},
		Supplied: want,
	})

	if cfg.Supplied != want {
		t.Errorf("RunConfig.Supplied = %+v, want %+v", cfg.Supplied, want)
	}
	if cfg.Mode != domain.ExecutionModeAuto || !cfg.PreConsultation {
		t.Errorf("RunConfig settings = (%q, pre=%v), want (auto, true)", cfg.Mode, cfg.PreConsultation)
	}
}

func TestStartSession_Resume_NothingAnswered_NothingSupplied(t *testing.T) {
	cfg := startWith(t, false, runconfig.ConfigSelection{})

	if cfg.Supplied != (domain.SuppliedSettings{}) {
		t.Errorf("RunConfig.Supplied = %+v, want nothing supplied", cfg.Supplied)
	}
}
