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

// nextAfterCommitSection returns the first applicable step after the commits/commit-branch
// section. For non-orchestrated modes it is pre-consultation; otherwise manual resolution.
func (s *ConfigScreen) nextAfterCommitSection() configStep {
	mode := s.sel.Settings.Mode
	if mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview {
		return configStepPreConsult
	}
	return configStepManualResolution
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
		return configStepCommits, 0
	case configStepPreConsult:
		// If commits were enabled, go back to commit-branch.
		if s.sel.Settings.Commits {
			return configStepCommitBranch, 0
		}
		// If a commit agent is declared (but commits were disabled), go back to commits.
		if s.hasCommitAgent() {
			return configStepCommits, 0
		}
		return configStepCheckpoints, 0
	case configStepManualResolution:
		mode := s.sel.Settings.Mode
		if mode == domain.ExecutionModeAuto || mode == domain.ExecutionModeAutoReview {
			return configStepPreConsult, 1
		}
		if s.sel.Settings.Commits {
			return configStepCommitBranch, 0
		}
		if s.hasCommitAgent() {
			return configStepCommits, 0
		}
		return configStepCheckpoints, 0
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
func (s *ConfigScreen) buildInfraClassQueue() []infraClassEntry {
	// Preserve declaration order using a slice of class names and a map.
	seen := make(map[string]bool)
	classOrder := []string{}
	classAgents := make(map[string][]string)
	for _, a := range s.declaredAgents {
		if !isGatedInfraClass(a.Class) {
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

// isGatedInfraClass reports whether the given class name is a gated
// infrastructure class (checkpoint, commit, or restore). Non-gated classes
// (e.g. "review") have all declared agents evaluated unconditionally.
// Note: gatedInfraClasses in Tools/Runner/internal/session/selection.go
// maintains an equivalent set for the session layer. Keep both in sync when
// adding new gated classes.
func isGatedInfraClass(class string) bool {
	return class == "checkpoint" || class == "commit" || class == "restore"
}

// View renders the current configuration prompt.
