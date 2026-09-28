package runconfig

import (
	"fmt"
	"strings"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

func (s *ConfigScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Configuration")
	subtitle := s.styles.Subtitle.Width(s.width).Render("Configure run options.")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))
	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")

	var body strings.Builder
	switch s.step {
	case configStepMode:
		body.WriteString(s.styles.Body.Width(s.width).Render("Execution mode:") + "\n")
		for i, mode := range domain.ExecutionModes() {
			body.WriteString(s.renderOptionCursor(i, string(mode)))
		}
	case configStepHarness:
		body.WriteString(s.styles.Body.Width(s.width).Render("Harness adapter:") + "\n")
		for i, sel := range harness.CLISelections() {
			body.WriteString(s.renderOption(i, sel.Label))
		}
	case configStepHarnessTimeout:
		body.WriteString(s.timeoutInput.View())
	case configStepVersionDrift:
		// Defense in depth: the step machine never lands here unless the prompt is
		// meaningful, but never render it if it somehow does.
		if s.showsVersionDrift() {
			body.WriteString(s.styles.Body.Width(s.width).Render("Allow workflow version drift:") + "\n")
			body.WriteString(s.renderOption(0, "Yes"))
			body.WriteString(s.renderOption(1, "No (default)"))
		}
	case configStepCheckpoints:
		body.WriteString(s.styles.Body.Width(s.width).Render("Checkpoints:") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepCommits:
		body.WriteString(s.styles.Body.Width(s.width).Render("Commits:") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepCommitBranch:
		body.WriteString(s.styles.Body.Width(s.width).Render("Commit branch variant:") + "\n")
		body.WriteString(s.renderOption(0,
			"Create branch mosaic/run/{run_id} for this run; an abandoned attempt is discarded by deleting the branch. (Recommended)",
		))
		body.WriteString(s.renderOption(1,
			"Commit to the branch you are already on; a failed attempt and its undo both stay in history permanently.",
		))
	case configStepPreConsult:
		body.WriteString(s.styles.Body.Width(s.width).Render("Pre-consultation (one-shot run-start consultation):") + "\n")
		body.WriteString(s.renderOption(0, "Disabled"))
		body.WriteString(s.renderOption(1, "Enabled (default)"))
	case configStepManualResolution:
		body.WriteString(s.styles.Body.Width(s.width).Render("Manual resolution (user resolves routing decisions):") + "\n")
		body.WriteString(s.renderOption(0, "Disabled (default)"))
		body.WriteString(s.renderOption(1, "Enabled"))
	case configStepInfraClass:
		if s.infraClassIdx < len(s.infraClassQueue) {
			entry := s.infraClassQueue[s.infraClassIdx]
			prompt := fmt.Sprintf("Select %s agent for this run:", entry.class)
			body.WriteString(s.styles.Body.Width(s.width).Render(prompt) + "\n")
			for i, agentName := range entry.agents {
				body.WriteString(s.renderOption(i, agentName))
			}
		}
	}

	return strings.Join([]string{title, subtitle, border, body.String(), border, help}, "\n")
}

// renderOption renders one selectable option with the current cursor position highlighted.
func (s *ConfigScreen) renderOption(idx int, label string) string {
	prefix := "  "
	if idx == s.cursor {
		prefix = "▶ "
		return prefix + s.styles.Selected.Render(label) + "\n"
	}
	return prefix + s.styles.Body.Render(label) + "\n"
}

// renderOptionCursor renders one selectable option, treating cursor == -1 as "no selection"
// (no option is highlighted). Used for the mode step which starts with no preselection.
func (s *ConfigScreen) renderOptionCursor(idx int, label string) string {
	if s.cursor >= 0 && idx == s.cursor {
		return "▶ " + s.styles.Selected.Render(label) + "\n"
	}
	return "  " + s.styles.Body.Render(label) + "\n"
}

// Done reports whether all configuration prompts have been answered.
