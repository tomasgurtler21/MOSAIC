package runconfig

// Tests for the run-configuration values the wizard collects once per run:
// the review loop limit (new runs only) and the runner settings adopted on the
// first Runner resume of a native-created artifact. A resume of a run that
// already records them never asks again.
//
// The tests drive the wizard by what it shows, so they do not depend on the
// order of the steps.
//
// The basic review-loop-limit prompt tests (suggestion, accepted values,
// invalid input, Esc) and the orchestrated-mode pre-consultation skip pin
// wizard behavior that already exists; they are regression guards and are
// expected to be green before any Stage work. The RED tests of this file are
// the last-prompt ordering and the resume/adoption gating tests.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/tui/screens"
)

const (
	limitPromptMarker   = "review loop limit"
	modePromptMarker    = "execution mode"
	preConsultMarker    = "pre-consultation"
	manualResolveMarker = "manual resolution"
	infraPromptMarker   = "select checkpoint agent"
)

// screenText returns the lower-cased current view.
func screenText(s *ConfigScreen) string { return strings.ToLower(s.View()) }

func typeText(s *ConfigScreen, text string) {
	for _, r := range text {
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// clearInput removes whatever the focused text input already holds, so a test
// controls the value whether the prompt is prefilled or only suggests one.
func clearInput(s *ConfigScreen) {
	for i := 0; i < 12; i++ {
		pressKey(s, tea.KeyBackspace)
	}
}

// newDrivenScreen returns a screen with the harness preselected, so that the
// wizard begins at the first prompt that belongs to the run.
func newDrivenScreen(isNewRun bool, agents []domain.DeclaredInfraAgent) *ConfigScreen {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(isNewRun)
	s.SetDeclaredAgents(agents)
	s.SetPreselectedHarness(harness.CLISelections()[0].ID)
	return s
}

// twoCheckpointAgents declares two checkpoint-class agents, which makes the
// wizard ask which one to use on a new run.
func twoCheckpointAgents() []domain.DeclaredInfraAgent {
	return []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
	}
}

// advanceOnce answers the current prompt with its default and moves on. The
// mode prompt has no default, so auto is chosen there.
func advanceOnce(s *ConfigScreen) {
	view := screenText(s)
	switch {
	case strings.Contains(view, "invocation timeout"):
		typeText(s, "30m")
		pressKey(s, tea.KeyEnter)
	case strings.Contains(view, modePromptMarker):
		pressKey(s, tea.KeyDown)
		pressKey(s, tea.KeyDown)
		pressKey(s, tea.KeyEnter)
	default:
		pressKey(s, tea.KeyEnter)
	}
}

// driveUntilShowing advances until the view contains marker, returning false
// when the wizard finishes first.
func driveUntilShowing(s *ConfigScreen, marker string) bool {
	for i := 0; i < 40; i++ {
		if strings.Contains(screenText(s), marker) {
			return true
		}
		if s.Done() {
			return false
		}
		advanceOnce(s)
	}
	return false
}

// driveToDone answers every remaining prompt with its default and returns the
// lower-cased view of every prompt that was shown, including the current one.
func driveToDone(t *testing.T, s *ConfigScreen) []string {
	t.Helper()
	var views []string
	for i := 0; i < 40; i++ {
		if s.Done() {
			return views
		}
		views = append(views, screenText(s))
		advanceOnce(s)
	}
	t.Fatal("the wizard did not finish within 40 prompts")
	return nil
}

func anyContains(views []string, marker string) bool {
	for _, v := range views {
		if strings.Contains(v, marker) {
			return true
		}
	}
	return false
}

// ===== New run: review loop limit =====

func TestConfigScreen_NewRun_ReviewLoopLimitPromptSuggestsThree(t *testing.T) {
	s := newDrivenScreen(true, nil)

	if !driveUntilShowing(s, limitPromptMarker) {
		t.Fatal("a new run must be asked for the review loop limit, but the wizard finished without the prompt")
	}

	if !strings.Contains(screenText(s), "3") {
		t.Errorf("the review loop limit prompt must suggest 3.\nview:\n%s", s.View())
	}
}

func TestConfigScreen_NewRun_ReviewLoopLimit_AcceptingSuggestionRecordsThree(t *testing.T) {
	s := newDrivenScreen(true, nil)
	driveUntilShowing(s, limitPromptMarker)

	pressKey(s, tea.KeyEnter)
	driveToDone(t, s)

	sel := s.Selection()
	if sel.Settings.ReviewLoopLimit != 3 {
		t.Errorf("ReviewLoopLimit = %d, want 3 (the suggestion)", sel.Settings.ReviewLoopLimit)
	}
	if !sel.Supplied.ReviewLoopLimit {
		t.Error("Supplied.ReviewLoopLimit = false, want true: the user answered the prompt")
	}
}

func TestConfigScreen_NewRun_ReviewLoopLimit_AcceptsPositiveIntegerOrNoLimit(t *testing.T) {
	tests := []struct {
		typed string
		want  int
	}{
		{"5", 5},
		{"12", 12},
		{"1", 1},
		{"no limit", 0},
		{"No Limit", 0},
		{"none", 0},
	}
	for _, tc := range tests {
		t.Run(tc.typed, func(t *testing.T) {
			s := newDrivenScreen(true, nil)
			driveUntilShowing(s, limitPromptMarker)

			clearInput(s)
			typeText(s, tc.typed)
			pressKey(s, tea.KeyEnter)
			driveToDone(t, s)

			if got := s.Selection().Settings.ReviewLoopLimit; got != tc.want {
				t.Errorf("typed %q: ReviewLoopLimit = %d, want %d", tc.typed, got, tc.want)
			}
			if !s.Selection().Supplied.ReviewLoopLimit {
				t.Errorf("typed %q: Supplied.ReviewLoopLimit = false, want true (the user answered the prompt)", tc.typed)
			}
		})
	}
}

func TestConfigScreen_NewRun_ReviewLoopLimit_InvalidInputKeepsPromptOpen(t *testing.T) {
	for _, typed := range []string{"0", "-1", "abc", "2.5"} {
		t.Run(typed, func(t *testing.T) {
			s := newDrivenScreen(true, nil)
			driveUntilShowing(s, limitPromptMarker)

			clearInput(s)
			typeText(s, typed)
			pressKey(s, tea.KeyEnter)

			if s.Done() || !strings.Contains(screenText(s), limitPromptMarker) {
				t.Fatalf("typed %q: the prompt must stay open on invalid input.\nview:\n%s", typed, s.View())
			}
			// A valid answer then goes through and is what gets recorded.
			clearInput(s)
			typeText(s, "4")
			pressKey(s, tea.KeyEnter)
			driveToDone(t, s)
			if got := s.Selection().Settings.ReviewLoopLimit; got != 4 {
				t.Errorf("ReviewLoopLimit = %d after correcting the input, want 4", got)
			}
		})
	}
}

func TestConfigScreen_NewRun_ReviewLoopLimit_EscReturnsToAnEarlierPrompt(t *testing.T) {
	s := newDrivenScreen(true, nil)
	driveUntilShowing(s, limitPromptMarker)

	pressKey(s, tea.KeyEsc)

	if s.Done() || s.Back() {
		t.Fatalf("Esc on the limit prompt must go back one prompt (Done=%v Back=%v)", s.Done(), s.Back())
	}
	if strings.Contains(screenText(s), limitPromptMarker) {
		t.Errorf("Esc left the wizard on the limit prompt.\nview:\n%s", s.View())
	}
}

// ===== Resume of a run that records its settings =====

func TestConfigScreen_Resume_RecordedSettings_NeverReAsksRecordedValues(t *testing.T) {
	s := newDrivenScreen(false, twoCheckpointAgents())
	s.SetNeedsRunnerAdoption(false)

	views := driveToDone(t, s)

	for _, marker := range []string{limitPromptMarker, modePromptMarker, preConsultMarker, manualResolveMarker, infraPromptMarker} {
		if anyContains(views, marker) {
			t.Errorf("a resume of a run that records these values asked %q again", marker)
		}
	}
	sel := s.Selection()
	if sel.Supplied.Mode || sel.Supplied.PreConsultation || sel.Supplied.ManualResolution ||
		sel.Supplied.ReviewLoopLimit || sel.Supplied.InfraClassSelections {
		t.Errorf("Supplied = %+v, want none supplied when nothing was asked", sel.Supplied)
	}
	if sel.Settings.ReviewLoopLimit != 0 {
		t.Errorf("ReviewLoopLimit = %d, want 0 (left to the artifact)", sel.Settings.ReviewLoopLimit)
	}
}

// ===== First resume of a native-created artifact =====

func TestConfigScreen_Resume_NativeArtifact_AsksRunnerSettingsOnly(t *testing.T) {
	s := newDrivenScreen(false, twoCheckpointAgents())
	s.SetNeedsRunnerAdoption(true)

	views := driveToDone(t, s)

	for _, marker := range []string{modePromptMarker, preConsultMarker, manualResolveMarker} {
		if !anyContains(views, marker) {
			t.Errorf("adopting a native-created artifact must ask %q", marker)
		}
	}
	for _, marker := range []string{limitPromptMarker, infraPromptMarker} {
		if anyContains(views, marker) {
			t.Errorf("adopting a native-created artifact must not ask %q", marker)
		}
	}
	sel := s.Selection()
	if sel.Settings.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode = %q, want auto (the answer given)", sel.Settings.Mode)
	}
	if !sel.Supplied.Mode || !sel.Supplied.PreConsultation || !sel.Supplied.ManualResolution {
		t.Errorf("Supplied = %+v, want mode, pre-consultation and manual resolution supplied", sel.Supplied)
	}
	if sel.Supplied.ReviewLoopLimit {
		t.Error("Supplied.ReviewLoopLimit = true, want false: a native-created artifact is used as it is")
	}
}

func TestConfigScreen_Resume_NativeArtifact_OrchestratedMode_SkipsPreConsultation(t *testing.T) {
	s := newDrivenScreen(false, nil)
	s.SetNeedsRunnerAdoption(true)
	driveUntilShowing(s, modePromptMarker)
	pressKey(s, tea.KeyDown) // first option: orchestrated
	pressKey(s, tea.KeyEnter)

	views := driveToDone(t, s)

	if anyContains(views, preConsultMarker) {
		t.Error("pre-consultation applies to auto and auto-review only and must not be asked for orchestrated")
	}
	if got := s.Selection().Settings.Mode; got != domain.ExecutionModeOrchestrated {
		t.Errorf("Mode = %q, want orchestrated", got)
	}
}

// The limit prompt is the last prompt of a new run: it follows every other
// question, so the wizard finishes as soon as it is answered.
func TestConfigScreen_NewRun_ReviewLoopLimitIsTheLastPrompt(t *testing.T) {
	s := newDrivenScreen(true, twoCheckpointAgents())
	if !driveUntilShowing(s, infraPromptMarker) {
		t.Fatal("precondition: the agent selection prompt must be shown for two checkpoint agents")
	}

	pressKey(s, tea.KeyEnter) // choose an agent

	if !strings.Contains(screenText(s), limitPromptMarker) {
		t.Fatalf("the review loop limit prompt must follow the agent selection.\nview:\n%s", s.View())
	}
	pressKey(s, tea.KeyEnter)
	if !s.Done() {
		t.Error("the wizard must finish once the review loop limit is answered")
	}
	if got := s.Selection().InfraClassSelections["checkpoint"]; got != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections[checkpoint] = %q, want checkpoint-manager-git (the first option)", got)
	}
}
