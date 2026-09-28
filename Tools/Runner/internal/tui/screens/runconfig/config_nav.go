package runconfig

import (
	"mosaic-run/internal/domain"
)

func (s *ConfigScreen) hasCommitAgent() bool {
	for _, a := range s.declaredAgents {
		if a.Class == "commit" {
			return true
		}
	}
	return false
}

// nextStepAfterCheckpoints returns the next applicable step after checkpoints.
func (s *ConfigScreen) nextStepAfterCheckpoints() configStep {
	if s.hasCommitAgent() {
		return configStepCommits
	}
	return s.nextAfterCommitSection()
}

// showsRunnerSettings reports whether the mode, pre-consultation and
// manual-resolution prompts are asked: for a new run, and for the first resume
// of a native-created artifact that records none of them. A resume of a run
// that records them never asks again.
func (s *ConfigScreen) showsRunnerSettings() bool {
	return s.isNewRun || s.needsAdoption
}

// showsPreConsult reports whether the pre-consultation prompt is asked: only
// with the runner settings and only for the auto and auto-review modes.
func (s *ConfigScreen) showsPreConsult() bool {
	mode := s.sel.Settings.Mode
	return s.showsRunnerSettings() && (mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview)
}

// pendingCommitRetry reports whether this is a resume that must obtain the
// commit-branch variant explicitly before retrying commit setup.
func (s *ConfigScreen) pendingCommitRetry() bool {
	return !s.isNewRun && s.commitSetupPending
}

// firstStep returns the first prompt this screen shows.
func (s *ConfigScreen) firstStep() configStep {
	if s.pendingCommitRetry() {
		return configStepCommitBranch
	}
	switch {
	case s.showsRunnerSettings():
		return configStepMode
	case s.harnessPreselected:
		return configStepHarnessTimeout
	default:
		return configStepHarness
	}
}

// moveToFirstStep positions the screen on its first prompt. Every setter that
// changes which prompts are shown calls it, and all of them run before the
// screen is shown.
func (s *ConfigScreen) moveToFirstStep() {
	s.step = s.firstStep()
	if s.step == configStepMode || s.step == configStepCommitBranch {
		s.cursor = -1 // these steps start with no option preselected
	} else {
		s.cursor = 0
	}
}

// nextAfterCommitSection returns the first applicable step after the
// commits/commit-branch section: pre-consultation, manual resolution, or
// whatever follows them.
func (s *ConfigScreen) nextAfterCommitSection() configStep {
	if s.showsPreConsult() {
		return configStepPreConsult
	}
	if s.showsRunnerSettings() {
		return configStepManualResolution
	}
	return s.nextAfterRunnerSettings()
}

// nextAfterRunnerSettings returns the step that follows the runner settings:
// the agent-per-class selection when one is needed, then the review loop limit
// (new runs only), then done.
func (s *ConfigScreen) nextAfterRunnerSettings() configStep {
	if len(s.infraClassQueue) > 0 {
		return configStepInfraClass
	}
	return s.nextAfterInfraClass()
}

// nextAfterInfraClass returns the step after the agent-per-class selection.
// The review loop limit is asked once, when the run is created.
func (s *ConfigScreen) nextAfterInfraClass() configStep {
	if s.isNewRun {
		return configStepReviewLimit
	}
	return configStepDone
}

// prevBeforeRunnerSettings returns the step before the runner settings: the
// end of the commits section.
func (s *ConfigScreen) prevBeforeRunnerSettings() (configStep, int) {
	if s.pendingCommitRetry() {
		return configStepCommitBranch, -1
	}
	if s.sel.Settings.Commits {
		return configStepCommitBranch, 0
	}
	if s.hasCommitAgent() {
		return configStepCommits, 0
	}
	return configStepCheckpoints, 0
}

// modeIndex returns the cursor index corresponding to the currently selected mode,
// or -1 if no mode has been selected yet.
func (s *ConfigScreen) modeIndex() int {
	modes := domain.ExecutionModes()
	for i, m := range modes {
		if m == s.sel.Settings.Mode {
			return i
		}
	}
	return -1
}

// prevStepAndCursor returns the previous applicable step and its cursor position,
// skipping conditional steps that would not be shown in the forward direction.
func (s *ConfigScreen) prevStepAndCursor() (configStep, int) {
	switch s.step {
	case configStepHarness:
		return configStepMode, s.modeIndex()
	case configStepVersionDrift:
		// configStepHarnessTimeout is handled by the text input widget's Back(),
		// but if we reach here via Esc on version-drift, go to timeout.
		return configStepHarnessTimeout, 0
	case configStepCheckpoints:
		if s.showsVersionDrift() {
			return configStepVersionDrift, 0
		}
		return configStepHarnessTimeout, 0
	case configStepCommits:
		return configStepCheckpoints, 0
	case configStepCommitBranch:
		if s.pendingCommitRetry() {
			return configStepCommitBranch, -1
		}
		return configStepCommits, 0
	case configStepPreConsult:
		return s.prevBeforeRunnerSettings()
	case configStepManualResolution:
		if s.showsPreConsult() {
			return configStepPreConsult, 1
		}
		return s.prevBeforeRunnerSettings()
	case configStepInfraClass:
		if s.infraClassIdx > 0 {
			s.infraClassIdx--
			return configStepInfraClass, 0
		}
		return configStepManualResolution, 0
	}
	return configStepMode, -1
}

// buildInfraClassQueue returns one infraClassEntry per gated class that has
// more than one declared agent, in the order classes first appear in
// declaredAgents. Classes with a single agent are omitted (auto-selected).
//
// Only a new run selects agents. A resumed run reads the selections the
// artifact recorded, so it is never asked again.
func (s *ConfigScreen) buildInfraClassQueue() []infraClassEntry {
	if !s.isNewRun {
		return nil
	}
	// Preserve declaration order using a slice of class names and a map.
	seen := make(map[string]bool)
	classOrder := []string{}
	classAgents := make(map[string][]string)
	for _, a := range s.declaredAgents {
		if !domain.IsGatedInfraClass(a.Class) {
			continue
		}
		if !seen[a.Class] {
			seen[a.Class] = true
			classOrder = append(classOrder, a.Class)
		}
		classAgents[a.Class] = append(classAgents[a.Class], a.Name)
	}
	var queue []infraClassEntry
	for _, class := range classOrder {
		agents := classAgents[class]
		if len(agents) > 1 {
			queue = append(queue, infraClassEntry{class: class, agents: agents})
		}
	}
	return queue
}

// View renders the current configuration prompt.
