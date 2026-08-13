package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/widgets"
)

var modeItems = []widgets.ListItem{
	{
		ID:          string(domain.ModeDeployNew),
		Label:       "Deploy new",
		Description: "Deploy agents and skills to a new workspace for the first time.",
		Detail: "Creates a fresh deployment in the workspace directory.\n" +
			"Use this when setting up a new project or a clean install.\n\n" +
			"The tool will ask you to select:\n" +
			"  • Workflows to include\n" +
			"  • Models for each agent tier\n" +
			"  • Any custom tools or hook bundles",
	},
	{
		ID:          string(domain.ModeUpdate),
		Label:       "Update existing",
		Description: "Update an existing workspace deployment to the latest source versions.",
		Detail: "Compares the deployed files against the current source versions.\n" +
			"Stale files are updated; locally-modified files prompt for a decision.\n\n" +
			"Use this after updating the generic agents or when a harness\n" +
			"descriptor has changed.",
	},
	{
		ID:          string(domain.ModeWorkflowsOnly),
		Label:       "Update workflows only",
		Description: "Replace the workflow set in an already-deployed orchestrator.",
		Detail: "Only the orchestrator file is rewritten — no agent, skill, or hook\n" +
			"artifact is touched, even if stale.\n\n" +
			"The selected workflow set fully replaces the currently deployed set;\n" +
			"workflows that are not reselected are removed from the orchestrator.\n\n" +
			"Use this to add, remove, or swap workflows without disturbing the rest\n" +
			"of the deployment.",
	},
	{
		ID:          string(domain.ModePromote),
		Label:       "Promote harness-only agent",
		Description: "Generate a generic agent source file from an already-transformed harness-only agent.",
		Detail: "Reads a harness-only agent file (one with transform_version and canonical\n" +
			"boundary tags) and generates a generic source file under Catalog/Agents/Generic/.\n\n" +
			"After promotion the new agent is immediately available to the Deploy/Update\n" +
			"flow. The original harness-only source file is left untouched.\n\n" +
			"Use this to turn a one-off harness-specific agent into a reusable generic\n" +
			"agent that can be deployed to any harness.",
	},
	{
		ID:          string(domain.ModeTransformHarness),
		Label:       "Transform harness",
		Description: "Convert one or more deployed agent files from one harness to another.",
		Detail: "Reads an agent file (or a folder of agent files) that was deployed for\n" +
			"a source harness and rewrites each one for a target harness.\n\n" +
			"Input can be a single agent file or a directory. Directory enumeration is\n" +
			"non-recursive — only files directly under the given folder are processed;\n" +
			"subdirectories are skipped.\n\n" +
			"Only files whose name ends with the source harness's extension are included.\n" +
			"Files that belong to a different harness are skipped with a per-file note.\n\n" +
			"Use this to migrate agents between harnesses without losing tool mappings\n" +
			"or injected content.",
	},
	{
		ID:          string(domain.ModeUtilityInfraOnly),
		Label:       "Deploy utility/infrastructure only",
		Description: "Deploy only Utility and Infrastructure agents, skipping workflows and hooks.",
		Detail: "Asks only the Utility and Infrastructure agent selection questions.\n" +
			"Workflow selection, hook configuration, and orchestrator rewriting are\n" +
			"never performed in this mode.\n\n" +
			"The run deploys the selected Utility and Infrastructure agents and the\n" +
			"skills they require — nothing else.\n\n" +
			"Infrastructure-agent model resolution works exactly as in the full deploy,\n" +
			"including the skip path and the resulting gap behaviour.\n\n" +
			"Use this when you want to add or update utility and infrastructure agents\n" +
			"without touching the orchestrator or workflow configuration.",
	},
}

// ModeScreen presents the available run modes: deploy-new, update-existing,
// workflows-only, and promote.
//
// Navigation contract:
//   - Enter -> Done() == true, SelectedMode() returns the chosen RunMode.
//   - Esc   -> Back() == true (caller returns to the harness selection screen, preserving
//              the previously selected harness so prior input is not lost).
type ModeScreen struct {
	list   *widgets.List
	detail *widgets.DetailPane
	width  int
	height int
	styles Styles
}

// NewModeScreen creates the mode selection screen.
func NewModeScreen(width, height int, styles Styles) *ModeScreen {
	listStyles := widgets.ListStyles{
		Normal:   styles.Body,
		Selected: styles.Selected,
		Disabled: styles.Muted,
		Cursor:   "▶",
	}
	listHeight := height - 8
	if listHeight < 2 {
		listHeight = 2
	}
	listWidth := width * 2 / 3
	if listWidth < 30 {
		listWidth = width
	}
	detailWidth := width - listWidth - 1
	if detailWidth < 10 {
		detailWidth = 10
	}
	detailStyles := widgets.DetailPaneStyles{
		Title:  styles.Subtitle,
		Body:   styles.Body,
		Border: styles.Border,
		Empty:  styles.Muted,
	}

	list := widgets.NewList(modeItems, listHeight, listWidth, listStyles)
	detail := widgets.NewDetailPane(listHeight, detailWidth, detailStyles)

	s := &ModeScreen{
		list:   list,
		detail: detail,
		width:  width,
		height: height,
		styles: styles,
	}
	s.updateDetail()
	return s
}

// Update processes a key message and delegates to the list widget.
func (s *ModeScreen) Update(msg tea.Msg) tea.Cmd {
	cmd := s.list.Update(msg)
	if !s.list.Done() && !s.list.Back() {
		s.updateDetail()
	}
	return cmd
}

func (s *ModeScreen) updateDetail() {
	item, ok := s.list.SelectedItem()
	if !ok {
		s.detail.SetContent("", "")
		return
	}
	s.detail.SetContent(item.Label, item.Detail)
}

// View renders the mode selection screen.
func (s *ModeScreen) View() string {
	title := s.styles.Title.Width(s.width).Render("Select Mode")
	subtitle := s.styles.Subtitle.Width(s.width).Render("What do you want to do?")
	border := s.styles.Border.Width(s.width).Render(strings.Repeat("─", s.width))

	listWidth := s.width * 2 / 3
	if listWidth < 30 {
		listWidth = s.width
	}
	sep := s.styles.Border.Render("│")
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(s.list.View()),
		sep,
		s.detail.View(),
	)

	help := s.styles.Help.Width(s.width).Render("↑/k up  ↓/j down  enter select  esc back  ctrl+c quit")

	return strings.Join([]string{title, subtitle, border, content, border, help}, "\n")
}

// Done reports whether the user selected a mode.
func (s *ModeScreen) Done() bool { return s.list.Done() }

// Back reports whether the user pressed back.
func (s *ModeScreen) Back() bool { return s.list.Back() }

// SelectedMode returns the chosen RunMode. Only valid when Done() is true.
func (s *ModeScreen) SelectedMode() domain.RunMode {
	return domain.RunMode(s.list.SelectedID())
}

// Resize updates the screen dimensions.
func (s *ModeScreen) Resize(width, height int) {
	s.width = width
	s.height = height
	listHeight := height - 8
	if listHeight < 2 {
		listHeight = 2
	}
	listWidth := width * 2 / 3
	if listWidth < 30 {
		listWidth = width
	}
	detailWidth := width - listWidth - 1
	if detailWidth < 10 {
		detailWidth = 10
	}
	s.list.Resize(listHeight, listWidth)
	s.detail.Resize(listHeight, detailWidth)
}

// Reset clears the done and back flags.
func (s *ModeScreen) Reset() { s.list.Reset() }
